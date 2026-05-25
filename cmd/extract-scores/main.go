package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
)

func main() {
	serverURL := flag.String("server", "http://localhost:8080", "URL of the OpenAI-compatible API server")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	model := flag.String("model", "", "model name to send in API requests (may be empty)")
	outputPath := flag.String("output", "./scores.json", "path to write the output JSON file")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	log.Printf("Loading transcripts from %s", *transcriptsDir)
	transcripts, err := LoadAllTranscripts(*transcriptsDir)
	if err != nil {
		log.Fatalf("Failed to load transcripts: %v", err)
	}
	log.Printf("Loaded %d transcript(s)", len(transcripts))

	client := NewClient(*serverURL, *model)

	// Sort video IDs for deterministic output order.
	videoIDs := make([]string, 0, len(transcripts))
	for id := range transcripts {
		videoIDs = append(videoIDs, id)
	}
	sort.Strings(videoIDs)

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
