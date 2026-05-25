package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
)

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "URL of the OpenAI-compatible API server")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	model := flag.String("model", "", "model name to send in API requests (may be empty)")
	outputPath := flag.String("output", "./scores.json", "path to write the output JSON file")
	limit := flag.Int("limit", 0, "limit the number of transcripts to process (0 means no limit)")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	var transcripts map[string]*TranscriptFile
	var err error

	if flag.NArg() > 0 {
		transcripts = make(map[string]*TranscriptFile)
		for _, arg := range flag.Args() {
			var path string
			var videoID string
			if _, err := os.Stat(arg); err == nil {
				path = arg
				videoID = strings.TrimSuffix(filepath.Base(arg), ".json")
			} else {
				videoID = arg
				path = filepath.Join(*transcriptsDir, videoID+".json")
			}
			tf, err := LoadTranscript(path)
			if err != nil {
				log.Fatalf("Failed to load transcript %q: %v", path, err)
			}
			transcripts[videoID] = tf
		}
	} else {
		log.Printf("Loading transcripts from %s", *transcriptsDir)
		transcripts, err = LoadAllTranscripts(*transcriptsDir)
		if err != nil {
			log.Fatalf("Failed to load transcripts: %v", err)
		}
	}
	log.Printf("Loaded %d transcript(s)", len(transcripts))

	client := NewClient(*serverURL, *model)

	// Sort video IDs for deterministic output order.
	videoIDs := make([]string, 0, len(transcripts))
	for id := range transcripts {
		videoIDs = append(videoIDs, id)
	}
	sort.Strings(videoIDs)

	if *limit > 0 && *limit < len(videoIDs) {
		log.Printf("Limiting processing to first %d transcript(s)", *limit)
		videoIDs = videoIDs[:*limit]
	}

	var results []VideoResult
	for i, videoID := range videoIDs {
		tf := transcripts[videoID]
		log.Printf("[%d/%d] Processing %s...", i+1, len(videoIDs), videoID)

		wines, err := client.Extract(ctx, tf)
		if err != nil {
			log.Printf("[%d/%d] Error processing %s: %v", i+1, len(videoIDs), videoID, err)
			continue
		}

		log.Printf("[%d/%d] Extracted %d wine(s) from %s", i+1, len(videoIDs), len(wines), videoID)
		for _, w := range wines {
			scoreStr := "no score"
			if w.Score != nil {
				scoreStr = fmt.Sprintf("%d pts", *w.Score)
			}
			log.Printf("  - %s: %s", w.Name, scoreStr)
		}

		results = append(results, VideoResult{
			VideoID: videoID,
			Wines:   wines,
		})
	}

	output := Output{Results: results}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	if err := os.WriteFile(*outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", *outputPath, err)
	}
	log.Printf("Wrote %d result(s) to %s", len(results), *outputPath)
}
