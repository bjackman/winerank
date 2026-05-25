package main

import (
	"bufio"
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
	review := flag.Bool("review", false, "interactive review mode to approve/reject extracted scores")
	flag.Parse()

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		<-ctx.Done()
		os.Exit(1)
	}()

	// Load existing results to preserve reviews and skip already-reviewed videos
	existingResults := make(map[string]VideoResult)
	if data, err := os.ReadFile(*outputPath); err == nil {
		var out Output
		if err := json.Unmarshal(data, &out); err == nil {
			for _, res := range out.Results {
				existingResults[res.VideoID] = res
			}
			log.Printf("Loaded %d existing results from %s", len(existingResults), *outputPath)
		} else {
			log.Printf("Warning: failed to parse existing %s (will overwrite): %v", *outputPath, err)
		}
	}

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

	reader := bufio.NewReader(os.Stdin)
	var results []VideoResult
	processedVideoIDs := make(map[string]bool)

	for i, videoID := range videoIDs {
		processedVideoIDs[videoID] = true
		tf := transcripts[videoID]

		// Check if we have an existing result for this video ID
		res, exists := existingResults[videoID]
		isForced := flag.NArg() > 0 // If user requested specific video(s), treat as forced reload

		var wines []WineScore

		// Determine if we need to call LLM or if we can use existing results
		useExisting := false
		if exists && !isForced {
			if !*review {
				useExisting = true
			} else {
				// In review mode, reuse existing if there are no pending wines
				hasPending := false
				for _, w := range res.Wines {
					if w.ReviewStatus == "" || w.ReviewStatus == "pending" {
						hasPending = true
						break
					}
				}
				if !hasPending {
					useExisting = true
				}
			}
		}

		if useExisting {
			log.Printf("[%d/%d] Using existing results for %s (contains %d wines)", i+1, len(videoIDs), videoID, len(res.Wines))
			wines = res.Wines
		} else {
			log.Printf("[%d/%d] Querying LLM for %s...", i+1, len(videoIDs), videoID)
			wines, err = client.Extract(ctx, tf)
			if err != nil {
				log.Printf("[%d/%d] Error processing %s: %v", i+1, len(videoIDs), videoID, err)
				if exists {
					log.Printf("[%d/%d] Falling back to existing results for %s", i+1, len(videoIDs), videoID)
					wines = res.Wines
				} else {
					continue
				}
			} else {
				// Initialize newly extracted wines with "pending" review status
				for idx := range wines {
					wines[idx].ReviewStatus = "pending"
				}
			}
		}

		// Perform interactive review if requested
		if *review {
			hasPending := false
			for _, w := range wines {
				if w.ReviewStatus == "" || w.ReviewStatus == "pending" {
					hasPending = true
					break
				}
			}

			if hasPending {
				log.Printf("[%d/%d] Entering interactive review for %s...", i+1, len(videoIDs), videoID)
				for idx := range wines {
					w := &wines[idx]
					if w.ReviewStatus != "" && w.ReviewStatus != "pending" {
						continue
					}

					fmt.Println(strings.Repeat("=", 60))
					fmt.Printf("Wine:     %s\n", w.Name)
					fmt.Printf("Producer: %s\n", w.Producer)
					fmt.Printf("Vintage:  %s\n", w.Vintage)
					fmt.Printf("Region:   %s\n", w.Region)
					scoreStr := "no score"
					if w.Score != nil {
						scoreStr = fmt.Sprintf("%d pts", *w.Score)
					}
					fmt.Printf("Score:    %s\n", scoreStr)
					fmt.Printf("Summary:  %s\n", w.NotesSummary)

					showSnippetContext(tf.Transcript, w.MatchingSnippet)

					for {
						fmt.Print("Is this extraction correct? [y/n/q]: ")
						input, err := reader.ReadString('\n')
						if err != nil {
							input = "q"
						}
						input = strings.ToLower(strings.TrimSpace(input))
						if input == "y" {
							w.ReviewStatus = "approved"
							fmt.Println("-> Approved.")
							break
						} else if input == "n" {
							w.ReviewStatus = "rejected"
							fmt.Println("-> Rejected.")
							break
						} else if input == "q" {
							fmt.Println("Quitting review and saving progress...")
							// Add the current video result (with whatever we reviewed so far)
							results = append(results, VideoResult{
								VideoID: videoID,
								Wines:   wines,
							})
							saveAndQuit(results, existingResults, videoIDs[i+1:], *outputPath)
							return
						} else {
							fmt.Println("Invalid input. Please type 'y' (yes), 'n' (no), or 'q' (quit).")
						}
					}
				}
			}
		}

		results = append(results, VideoResult{
			VideoID: videoID,
			Wines:   wines,
		})
	}

	// Merge in any loaded results for videos we did NOT process in this run
	for id, res := range existingResults {
		if !processedVideoIDs[id] {
			results = append(results, res)
		}
	}

	// Sort final output by VideoID
	sort.Slice(results, func(i, j int) bool {
		return results[i].VideoID < results[j].VideoID
	})

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

func findSnippetIndex(transcript, snippet string) (int, int) {
	snippet = strings.TrimSpace(snippet)
	if len(snippet) == 0 {
		return -1, -1
	}

	// Try exact match first
	if idx := strings.Index(transcript, snippet); idx != -1 {
		return idx, idx + len(snippet)
	}

	// Try case-insensitive exact match
	lowerTranscript := strings.ToLower(transcript)
	lowerSnippet := strings.ToLower(snippet)
	if idx := strings.Index(lowerTranscript, lowerSnippet); idx != -1 {
		return idx, idx + len(snippet)
	}

	// Try matching with sliding offsets to bypass minor start hallucinations (e.g. "And", "So", "Yeah")
	prefixLen := 30
	offsets := []int{0, 5, 10, 15, 20, 25, 30}
	for _, off := range offsets {
		if off+prefixLen > len(lowerSnippet) {
			continue
		}
		prefix := lowerSnippet[off : off+prefixLen]
		if idx := strings.Index(lowerTranscript, prefix); idx != -1 {
			start := idx - off
			if start < 0 {
				start = 0
			}

			// Try to find a suffix (last 30 chars of snippet) to align the end accurately
			suffixLen := 30
			if len(lowerSnippet) < suffixLen {
				suffixLen = len(lowerSnippet)
			}
			suffix := lowerSnippet[len(lowerSnippet)-suffixLen:]

			// Search for suffix starting from the matched prefix location
			if sIdx := strings.Index(lowerTranscript[idx:], suffix); sIdx != -1 {
				return start, idx + sIdx + suffixLen
			}

			// Fallback: use the length of the snippet
			end := start + len(snippet)
			if end > len(transcript) {
				end = len(transcript)
			}
			return start, end
		}
	}

	return -1, -1
}

func showSnippetContext(transcript, snippet string) {
	start, end := findSnippetIndex(transcript, snippet)
	if start == -1 {
		fmt.Printf("\n[WARNING: Matching snippet not found verbatim in transcript]\n")
		fmt.Printf("Snippet: \x1b[1;33m%s\x1b[0m\n\n", snippet)
		return
	}

	contextBefore := 250
	contextAfter := 250

	printStart := start - contextBefore
	if printStart < 0 {
		printStart = 0
	}
	printEnd := end + contextAfter
	if printEnd > len(transcript) {
		printEnd = len(transcript)
	}

	beforeStr := transcript[printStart:start]
	matchStr := transcript[start:end]
	afterStr := transcript[end:printEnd]

	// Clean up newlines for a more compact output context display
	beforeStr = strings.ReplaceAll(beforeStr, "\n", " ")
	matchStr = strings.ReplaceAll(matchStr, "\n", " ")
	afterStr = strings.ReplaceAll(afterStr, "\n", " ")

	fmt.Println("\n--- TRANSCRIPT CONTEXT ---")
	fmt.Printf("... %s\x1b[1;33;1m%s\x1b[0m%s ...\n", beforeStr, matchStr, afterStr)
	fmt.Println("--------------------------")
	fmt.Printf("Transcript (Approx): %s\n", matchStr)
	fmt.Printf("Model (Actual):      %s\n\n", strings.ReplaceAll(snippet, "\n", " "))
}

func saveAndQuit(currentResults []VideoResult, existingResults map[string]VideoResult, remainingVideoIDs []string, outputPath string) {
	finalResults := append([]VideoResult(nil), currentResults...)

	// For any remaining video IDs that were not processed in this run, preserve their existing data
	for _, id := range remainingVideoIDs {
		if res, exists := existingResults[id]; exists {
			finalResults = append(finalResults, res)
		}
	}

	// Sort finalResults by VideoID for consistency
	sort.Slice(finalResults, func(i, j int) bool {
		return finalResults[i].VideoID < finalResults[j].VideoID
	})

	output := Output{Results: finalResults}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", outputPath, err)
	}
	log.Printf("Wrote %d result(s) to %s", len(finalResults), outputPath)
}
