package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

func main() {
	defaultServer := "http://localhost:8080"
	if v := os.Getenv("LLAMA_SERVER_URL"); v != "" {
		defaultServer = v
	}
	serverURL := flag.String("server", defaultServer, "URL of the OpenAI-compatible API server (env: LLAMA_SERVER_URL)")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	model := flag.String("model", "", "model name to send in API requests (may be empty)")
	outputPath := flag.String("output", "./scores.json", "path to write the output JSON file")
	gtPath := flag.String("groundtruth", "./scores_groundtruth.json", "path to write/read the ground truth JSON file")
	limit := flag.Int("limit", 0, "limit the number of transcripts to process (0 means no limit)")
	video := flag.String("video", "", "process only this video ID (mutually exclusive with --limit)")
	review := flag.Bool("review", false, "interactive review mode to approve/reject extracted scores")
	segmentOnly := flag.Bool("segment", false, "run segmentation only, show GT diff if available, then exit")
	structuredOutput := flag.Bool("structured-output", true, "request a JSON schema response_format (disable to work around llama.cpp grammar crashes)")
	reasoning := flag.String("reasoning", "", `control model thinking via chat_template_kwargs: "off" disables it, "on" forces it, empty leaves the template default`)
	observeAfter := flag.Duration("observe-after", 20*time.Second, "if an LLM request is still streaming after this long, echo the model's live output to stderr (0 disables)")
	strategy := flag.String("strategy", "multi-pass", `extraction strategy: "multi-pass" (segment then per-wine extract) or "single-pass" (one prompt for all wines)`)
	flag.Parse()

	switch *reasoning {
	case "", "on", "off":
	default:
		log.Fatalf("--reasoning must be one of \"\", \"on\", \"off\" (got %q)", *reasoning)
	}

	switch *strategy {
	case "multi-pass", "single-pass":
	default:
		log.Fatalf(`--strategy must be "multi-pass" or "single-pass" (got %q)`, *strategy)
	}

	if *segmentOnly && *strategy == "single-pass" {
		log.Fatal("--segment is only valid with --strategy=multi-pass")
	}

	if *video != "" && *limit > 0 {
		log.Fatal("--video and --limit are mutually exclusive")
	}

	var normalExit bool
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	go func() {
		<-ctx.Done()
		if !normalExit {
			os.Exit(1)
		}
	}()

	// Load ground truths
	gtMap, gtRecords, err := LoadGroundTruth(*gtPath)
	if err != nil {
		log.Fatalf("Failed to load ground truth from %s: %v", *gtPath, err)
	}
	log.Printf("Loaded %d ground truth records from %s", len(gtRecords), *gtPath)

	// Load existing results to preserve clean results and skip already-reviewed videos
	existingResults := make(map[string]FinalVideoResult)
	if data, err := os.ReadFile(*outputPath); err == nil {
		var out FinalOutput
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
	} else if *video != "" {
		path := filepath.Join(*transcriptsDir, *video+".json")
		tf, err := LoadTranscript(path)
		if err != nil {
			log.Fatalf("Failed to load transcript %q: %v", path, err)
		}
		transcripts = map[string]*TranscriptFile{*video: tf}
	} else {
		log.Printf("Loading transcripts from %s", *transcriptsDir)
		transcripts, err = LoadAllTranscripts(*transcriptsDir)
		if err != nil {
			log.Fatalf("Failed to load transcripts: %v", err)
		}
	}
	log.Printf("Loaded %d transcript(s)", len(transcripts))

	client := NewClient(*serverURL, *model, *structuredOutput)
	client.Reasoning = *reasoning
	client.ObserveAfter = *observeAfter
	client.Strategy = *strategy

	// Sort video IDs for deterministic output order and apply limit.
	videoIDs := make([]string, 0, len(transcripts))
	for id := range transcripts {
		videoIDs = append(videoIDs, id)
	}
	sort.Strings(videoIDs)

	if *limit > 0 && *limit < len(videoIDs) {
		log.Printf("Limiting processing to first %d transcript(s)", *limit)
		videoIDs = videoIDs[:*limit]
	}

	if *segmentOnly {
		for _, videoID := range videoIDs {
			tf := transcripts[videoID]
			log.Printf("Segmenting %s...", videoID)
			segResp, err := client.Segment(ctx, tf)
			if err != nil {
				log.Printf("Error segmenting %s: %v", videoID, err)
				continue
			}
			sentences := splitIntoSentences(tf.Transcript)
			got := classifySentencesFromResponse(*segResp, len(sentences))
			var segGT *segmentGroundTruth
			if g, err := parseSegmentGroundTruth(segmentGroundTruthPath(videoID)); err == nil {
				segGT = g
			}
			printSegmentedTranscriptUnified(videoID, tf, got, *segResp, segGT)
		}
		normalExit = true
		return
	}

	reader := bufio.NewReader(os.Stdin)
	var results []FinalVideoResult
	processedVideoIDs := make(map[string]bool)

	for i, videoID := range videoIDs {
		processedVideoIDs[videoID] = true
		tf := transcripts[videoID]

		// Check if we have an existing result for this video ID
		res, exists := existingResults[videoID]
		isForced := flag.NArg() > 0 || *video != "" // explicit video selection forces reload

		var wines []WineScore
		var reviewSentences []string
		var reviewGot map[int]segmentClassification
		var reviewSegGT *segmentGroundTruth
		var reviewSegResp *segmentResponse

		// Determine if we need to call LLM or if we can use existing results
		useExisting := false
		if exists && !isForced {
			if !*review {
				useExisting = true
			} else {
				// In review mode, reuse existing ONLY if all of its wines are already in ground truth
				hasPending := false
				if len(res.Wines) == 0 {
					hasPending = false
				} else {
					for _, w := range res.Wines {
						gtRec, hasGT := gtMap[videoID][w.Name]
						if !hasGT || !scoresMatch(w.Score, gtRec.Score) {
							hasPending = true
							break
						}
					}
				}
				if !hasPending {
					useExisting = true
				}
			}
		}

		if useExisting {
			log.Printf("[%d/%d] Using existing results for %s (contains %d wines)", i+1, len(videoIDs), videoID, len(res.Wines))
			wines = make([]WineScore, len(res.Wines))
			for idx, fw := range res.Wines {
				wines[idx] = WineScore{
					Name:         fw.Name,
					Producer:     fw.Producer,
					Vintage:      fw.Vintage,
					Region:       fw.Region,
					Score:        fw.Score,
					NotesSummary: fw.NotesSummary,
					ReviewStatus: "approved",
				}
			}
		} else {
			log.Printf("[%d/%d] Querying LLM for %s...", i+1, len(videoIDs), videoID)
			var segResp *segmentResponse
			wines, segResp, err = client.Extract(ctx, tf)
			if err != nil {
				log.Printf("[%d/%d] Error processing %s: %v", i+1, len(videoIDs), videoID, err)
				if exists {
					log.Printf("[%d/%d] Falling back to existing results for %s", i+1, len(videoIDs), videoID)
					wines = make([]WineScore, len(res.Wines))
					for idx, fw := range res.Wines {
						wines[idx] = WineScore{
							Name:         fw.Name,
							Producer:     fw.Producer,
							Vintage:      fw.Vintage,
							Region:       fw.Region,
							Score:        fw.Score,
							NotesSummary: fw.NotesSummary,
							ReviewStatus: "approved",
						}
					}
				} else {
					continue
				}
			} else {
				if segResp != nil {
					reviewSentences = splitIntoSentences(tf.Transcript)
					reviewGot = classifySentencesFromResponse(*segResp, len(reviewSentences))
					reviewSegResp = segResp
					if g, err := parseSegmentGroundTruth(segmentGroundTruthPath(videoID)); err == nil {
						reviewSegGT = g
					}
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
					if w.ReviewStatus != "" && w.ReviewStatus != "approved" && w.ReviewStatus != "pending" {
						continue
					}
					if w.ReviewStatus == "approved" {
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

					showDescriptionSnippet(tf.Description, w.Name)
					printWineSegmentContext(w.Name, reviewGot, reviewSentences, reviewSegResp, reviewSegGT)

					for {
						fmt.Print("Is this extraction correct? [y/n/c/t/q]: ")
						input, err := reader.ReadString('\n')
						if err != nil {
							input = "q"
						}
						input = strings.ToLower(strings.TrimSpace(input))
						if input == "y" {
							w.ReviewStatus = "approved"
							fmt.Println("-> Approved.")

							// Extract the actual transcript snippet
							start, end := findSnippetIndex(tf.Transcript, w.MatchingSnippet)
							actualSnippet := w.MatchingSnippet
							if start != -1 {
								actualSnippet = tf.Transcript[start:end]
							}
							actualSnippet = strings.TrimSpace(actualSnippet)

							// Update ground truth records
							gtRecords = updateGroundTruth(gtRecords, videoID, w.Name, actualSnippet, w.Score)
							if gtMap[videoID] == nil {
								gtMap[videoID] = make(map[string]GroundTruthRecord)
							}
							gtMap[videoID][w.Name] = GroundTruthRecord{
								VideoID:           videoID,
								WineName:          w.Name,
								TranscriptSnippet: actualSnippet,
								Score:             w.Score,
							}

							break
						} else if input == "n" {
							w.ReviewStatus = "rejected"
							fmt.Println("-> Rejected.")
							break
						} else if input == "c" {
							fmt.Print("Correct score (integer, or Enter for unscored): ")
							scoreInput, err := reader.ReadString('\n')
							if err != nil {
								scoreInput = ""
							}
							scoreInput = strings.TrimSpace(scoreInput)
							var correctedScore *int
							if scoreInput != "" {
								n, err := strconv.Atoi(scoreInput)
								if err != nil {
									fmt.Printf("Invalid score %q, try again.\n", scoreInput)
									continue
								}
								correctedScore = &n
							}
							w.ReviewStatus = "rejected"
							scoreLabel := "unscored"
							if correctedScore != nil {
								scoreLabel = fmt.Sprintf("%d", *correctedScore)
							}
							fmt.Printf("-> Rejected (GT updated with score: %s).\n", scoreLabel)

							start, end := findSnippetIndex(tf.Transcript, w.MatchingSnippet)
							actualSnippet := w.MatchingSnippet
							if start != -1 {
								actualSnippet = tf.Transcript[start:end]
							}
							actualSnippet = strings.TrimSpace(actualSnippet)

							gtRecords = updateGroundTruth(gtRecords, videoID, w.Name, actualSnippet, correctedScore)
							if gtMap[videoID] == nil {
								gtMap[videoID] = make(map[string]GroundTruthRecord)
							}
							gtMap[videoID][w.Name] = GroundTruthRecord{
								VideoID:           videoID,
								WineName:          w.Name,
								TranscriptSnippet: actualSnippet,
								Score:             correctedScore,
							}
							break
						} else if input == "t" {
							viewInPager(tf.Transcript)

							// Reprint context after exiting pager
							fmt.Println(strings.Repeat("=", 60))
							fmt.Printf("Wine:     %s\n", w.Name)
							fmt.Printf("Producer: %s\n", w.Producer)
							fmt.Printf("Vintage:  %s\n", w.Vintage)
							fmt.Printf("Region:   %s\n", w.Region)
							fmt.Printf("Score:    %s\n", scoreStr)
							fmt.Printf("Summary:  %s\n", w.NotesSummary)
							showDescriptionSnippet(tf.Description, w.Name)
							printWineSegmentContext(w.Name, reviewGot, reviewSentences, reviewSegResp, reviewSegGT)
							continue
						} else if input == "q" {
							fmt.Println("Quitting review and saving progress...")
							// Save ground truth progress
							if err := SaveGroundTruth(*gtPath, gtRecords); err != nil {
								log.Printf("Error saving ground truth: %v", err)
							}

							// Convert wines to FinalWineScore (only approved ones)
							var finalWines []FinalWineScore
							for _, win := range wines {
								if win.ReviewStatus == "approved" {
									finalWines = append(finalWines, FinalWineScore{
										Name:         win.Name,
										Producer:     win.Producer,
										Vintage:      win.Vintage,
										Region:       win.Region,
										Score:        win.Score,
										NotesSummary: win.NotesSummary,
									})
								}
							}
							results = append(results, FinalVideoResult{
								VideoID: videoID,
								Wines:   finalWines,
							})

							// Save output and quit
							saveAndQuit(results, existingResults, videoIDs[i+1:], *outputPath)
							return
						} else {
							fmt.Println("Invalid input. Please type 'y' (yes), 'n' (no), 'c' (correct score), 't' (transcript), or 'q' (quit).")
						}
					}
				}
			}
		}

		// Convert wines to FinalWineScore
		var finalWines []FinalWineScore
		for _, win := range wines {
			if !*review || win.ReviewStatus == "approved" {
				finalWines = append(finalWines, FinalWineScore{
					Name:         win.Name,
					Producer:     win.Producer,
					Vintage:      win.Vintage,
					Region:       win.Region,
					Score:        win.Score,
					NotesSummary: win.NotesSummary,
				})
			}
		}

		results = append(results, FinalVideoResult{
			VideoID: videoID,
			Wines:   finalWines,
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

	output := FinalOutput{Results: results}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	if err := os.WriteFile(*outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", *outputPath, err)
	}
	log.Printf("Wrote %d result(s) to %s", len(results), *outputPath)

	// Save ground truth progress
	if err := SaveGroundTruth(*gtPath, gtRecords); err != nil {
		log.Printf("Error saving ground truth: %v", err)
	}
	normalExit = true
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

func saveAndQuit(currentResults []FinalVideoResult, existingResults map[string]FinalVideoResult, remainingVideoIDs []string, outputPath string) {
	finalResults := append([]FinalVideoResult(nil), currentResults...)

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

	output := FinalOutput{Results: finalResults}
	data, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		log.Fatalf("Failed to marshal output: %v", err)
	}

	if err := os.WriteFile(outputPath, data, 0644); err != nil {
		log.Fatalf("Failed to write %s: %v", outputPath, err)
	}
	log.Printf("Wrote %d result(s) to %s", len(finalResults), outputPath)
}

func LoadGroundTruth(path string) (map[string]map[string]GroundTruthRecord, []GroundTruthRecord, error) {
	m := make(map[string]map[string]GroundTruthRecord)
	var records []GroundTruthRecord

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return m, nil, nil
		}
		return nil, nil, err
	}

	var gt GroundTruth
	if err := json.Unmarshal(data, &gt); err != nil {
		return nil, nil, err
	}

	records = gt.Records
	for _, rec := range records {
		if m[rec.VideoID] == nil {
			m[rec.VideoID] = make(map[string]GroundTruthRecord)
		}
		m[rec.VideoID][rec.WineName] = rec
	}
	return m, records, nil
}

func SaveGroundTruth(path string, records []GroundTruthRecord) error {
	gt := GroundTruth{Records: records}
	data, err := json.MarshalIndent(gt, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}

func updateGroundTruth(records []GroundTruthRecord, videoID, wineName, transcriptSnippet string, score *int) []GroundTruthRecord {
	for i, rec := range records {
		if rec.VideoID == videoID && rec.WineName == wineName {
			records[i].TranscriptSnippet = transcriptSnippet
			records[i].Score = score
			return records
		}
	}
	return append(records, GroundTruthRecord{
		VideoID:           videoID,
		WineName:          wineName,
		TranscriptSnippet: transcriptSnippet,
		Score:             score,
	})
}

func scoresMatch(s1, s2 *int) bool {
	if s1 == nil && s2 == nil {
		return true
	}
	if s1 != nil && s2 != nil && *s1 == *s2 {
		return true
	}
	return false
}

func showDescriptionSnippet(description, wineName string) {
	lines := strings.Split(description, "\n")
	target := strings.ToLower(strings.TrimSpace(wineName))
	if len(target) == 0 {
		return
	}

	matchedIdx := -1
	for i, line := range lines {
		if strings.Contains(strings.ToLower(line), target) {
			matchedIdx = i
			break
		}
	}

	// Try matching first 2 words if exact match fails
	if matchedIdx == -1 {
		words := strings.Fields(target)
		if len(words) >= 2 {
			phrase := strings.Join(words[:2], " ")
			for i, line := range lines {
				if strings.Contains(strings.ToLower(line), phrase) {
					matchedIdx = i
					break
				}
			}
		}
	}

	if matchedIdx != -1 {
		start := matchedIdx - 1
		if start < 0 {
			start = 0
		}
		end := matchedIdx + 2
		if end > len(lines) {
			end = len(lines)
		}

		fmt.Println("\n--- DESCRIPTION CONTEXT ---")
		for idx := start; idx < end; idx++ {
			line := lines[idx]
			if idx == matchedIdx {
				fmt.Printf(">> \x1b[1;36m%s\x1b[0m\n", line)
			} else {
				fmt.Printf("   %s\n", line)
			}
		}
		fmt.Println("---------------------------")
	} else {
		fmt.Printf("\n--- DESCRIPTION CONTEXT ---\n[Wine name not matched in description]\n---------------------------\n")
	}
}

func viewInPager(text string) {
	tmpFile, err := os.CreateTemp("", "winerank-transcript-*.txt")
	if err != nil {
		fmt.Println("\n--- FULL TRANSCRIPT ---")
		fmt.Println(text)
		fmt.Println("-----------------------\n")
		return
	}
	defer os.Remove(tmpFile.Name())
	defer tmpFile.Close()

	if _, err := tmpFile.WriteString(text); err != nil {
		fmt.Println("\n--- FULL TRANSCRIPT ---")
		fmt.Println(text)
		fmt.Println("-----------------------\n")
		return
	}

	pager := os.Getenv("PAGER")
	if pager == "" {
		pager = "less"
	}

	args := strings.Fields(pager)
	if len(args) == 0 {
		args = []string{"less"}
	}
	args = append(args, tmpFile.Name())

	cmd := exec.Command(args[0], args[1:]...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr

	if err := cmd.Run(); err != nil {
		fmt.Println("\n--- FULL TRANSCRIPT ---")
		fmt.Println(text)
		fmt.Println("-----------------------\n")
	}
}

// printWineSegmentContext prints the transcript sentences assigned to a wine by the model,
// grouped by phase (blind/reveal). Sentences are colored green/red against GT if available.
func printWineSegmentContext(wineName string, got map[int]segmentClassification, sentences []string, segResp *segmentResponse, gt *segmentGroundTruth) {
	if got == nil || segResp == nil || len(sentences) == 0 {
		return
	}

	placeholder := ""
	for _, pm := range segResp.PlaceholderMappings {
		if strings.EqualFold(strings.TrimSpace(pm.WineName), strings.TrimSpace(wineName)) {
			placeholder = normalizePlaceholder(pm.Placeholder)
			break
		}
	}
	if placeholder == "" {
		fmt.Printf("\n--- TRANSCRIPT SEGMENTS ---\n[No segment found for %q]\n---------------------------\n", wineName)
		return
	}

	fmt.Println("\n--- TRANSCRIPT SEGMENTS ---")
	prevPhase := ""
	for i := 1; i <= len(sentences); i++ {
		cls := got[i]
		if cls.Placeholder != placeholder {
			continue
		}
		if cls.Phase != prevPhase {
			if prevPhase != "" {
				fmt.Println()
			}
			fmt.Printf("  -- %s --\n", cls.Phase)
			prevPhase = cls.Phase
		}
		if gt != nil {
			gtCls, hasGT := gt.Sentences[i]
			if hasGT && gtCls.Phase == cls.Phase && gtCls.Placeholder == cls.Placeholder {
				fmt.Printf("  \x1b[32m[%d] %s\x1b[0m\n", i, sentences[i-1])
			} else if hasGT {
				fmt.Printf("  \x1b[31m[%d] %s\x1b[0m\n", i, sentences[i-1])
			} else {
				fmt.Printf("  [%d] %s\n", i, sentences[i-1])
			}
		} else {
			fmt.Printf("  [%d] %s\n", i, sentences[i-1])
		}
	}
	fmt.Println("---------------------------")
}

// classificationLabel builds a display label for a segment classification,
// enriched with the actual wine name when available.
func classificationLabel(c segmentClassification, wineNames map[string]string) string {
	if c.Phase == "unmapped" || c.Phase == "" {
		return "Unmapped"
	}
	label := c.Placeholder
	if c.Phase != "open" {
		label += " " + c.Phase
	}
	if name := wineNames[c.Placeholder]; name != "" {
		label += ": " + name
	}
	return label
}

// printSegmentedTranscriptUnified prints the transcript with segment delimiters from the
// model, showing ground-truth classifications inline at each boundary. When the model and
// GT agree the header is printed normally; when they differ both are printed in red with
// the model's label indented.
func printSegmentedTranscriptUnified(videoID string, tf *TranscriptFile, got map[int]segmentClassification, segResp segmentResponse, gt *segmentGroundTruth) {
	sentences := splitIntoSentences(tf.Transcript)

	modelWineNames := make(map[string]string)
	for _, pm := range segResp.PlaceholderMappings {
		modelWineNames[normalizePlaceholder(pm.Placeholder)] = pm.WineName
	}

	gtWineNames := make(map[string]string)
	if gt != nil {
		for k, v := range gt.PlaceholderMappings {
			gtWineNames[k] = v
		}
	}

	fmt.Println()
	fmt.Println("\x1b[1;34m============================================================\x1b[0m")
	fmt.Printf("\x1b[1;34m  SEGMENTED TRANSCRIPT: %s\x1b[0m\n", videoID)
	fmt.Println("\x1b[1;34m============================================================\x1b[0m")

	// Sentinel values that won't match any real classification.
	prevModel := segmentClassification{Phase: ""}
	prevGT := segmentClassification{Phase: ""}

	for i := 1; i <= len(sentences); i++ {
		modelClass := got[i]
		if modelClass.Phase == "" {
			modelClass = segmentClassification{Phase: "unmapped"}
		}

		hasGT := false
		var gtClass segmentClassification
		if gt != nil {
			if gc, ok := gt.Sentences[i]; ok {
				gtClass = gc
				hasGT = true
			}
		}

		modelChanged := modelClass != prevModel
		gtChanged := hasGT && gtClass != prevGT
		agree := hasGT && modelClass.Phase == gtClass.Phase && modelClass.Placeholder == gtClass.Placeholder

		if modelChanged || gtChanged {
			fmt.Println()
			switch {
			case !hasGT || agree:
				fmt.Printf("=== %s ===\n", classificationLabel(modelClass, modelWineNames))
			case gtChanged && modelChanged:
				// Both changed to different things: one line each.
				fmt.Printf("GT:    === %s ===\n", classificationLabel(gtClass, gtWineNames))
				fmt.Printf("MODEL: === %s ===\n", classificationLabel(modelClass, modelWineNames))
			case gtChanged:
				fmt.Printf("GT:    === %s ===\n", classificationLabel(gtClass, gtWineNames))
			default:
				fmt.Printf("MODEL: === %s ===\n", classificationLabel(modelClass, modelWineNames))
			}
			fmt.Println()
		}

		switch {
		case !hasGT:
			fmt.Printf("[%d] %s\n", i, sentences[i-1])
		case agree:
			fmt.Printf("\x1b[32m[%d] %s\x1b[0m\n", i, sentences[i-1])
		default:
			fmt.Printf("\x1b[31m[%d] %s\x1b[0m\n", i, sentences[i-1])
		}

		prevModel = modelClass
		if hasGT {
			prevGT = gtClass
		}
	}

	if gt != nil {
		evalResult := compareSegmentation(got, gt, len(sentences), sentences, segResp)
		accuracy := float64(0)
		if evalResult.TotalSentences > 0 {
			accuracy = float64(evalResult.Correct) / float64(evalResult.TotalSentences) * 100
		}
		color := "\x1b[32m"
		if accuracy < 95 {
			color = "\x1b[33m"
		}
		if accuracy < 80 {
			color = "\x1b[31m"
		}
		fmt.Println()
		fmt.Printf("\x1b[1mSentence accuracy:\x1b[0m %s%d/%d (%.1f%%)\x1b[0m\n", color, evalResult.Correct, evalResult.TotalSentences, accuracy)
		if evalResult.MappingTotal > 0 {
			mColor := "\x1b[32m"
			if evalResult.MappingCorrect < evalResult.MappingTotal {
				mColor = "\x1b[31m"
			}
			fmt.Printf("\x1b[1mMapping accuracy:\x1b[0m  %s%d/%d\x1b[0m\n", mColor, evalResult.MappingCorrect, evalResult.MappingTotal)
		}
	}

	fmt.Println("\x1b[1;34m============================================================\x1b[0m\n")
}
