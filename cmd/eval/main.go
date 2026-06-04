package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func loadGroundTruth(path string) (GroundTruth, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return GroundTruth{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var gt GroundTruth
	if err := json.Unmarshal(data, &gt); err != nil {
		return GroundTruth{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return gt, nil
}

func loadExtractorOutput(path string) (ExtractorOutput, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return ExtractorOutput{}, fmt.Errorf("reading %s: %w", path, err)
	}
	var out ExtractorOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return ExtractorOutput{}, fmt.Errorf("parsing %s: %w", path, err)
	}
	return out, nil
}

func runExtractor(extractorCmd, transcriptsDir, outputPath string, videoIDs []string) error {
	parts := strings.Fields(extractorCmd)
	if len(parts) == 0 {
		return fmt.Errorf("empty extractor command")
	}
	args := parts[1:]
	args = append(args, "--transcripts-dir", transcriptsDir, "--output", outputPath)
	args = append(args, videoIDs...)

	cmd := exec.Command(parts[0], args...)
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

func main() {
	extractor := flag.String("extractor", "nix run .#extract-scores --", "command used to invoke the extractor")
	groundtruth := flag.String("groundtruth", "scores_groundtruth.json", "path to ground truth file")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	report := flag.String("report", "", "path to write the detailed eval report (default: evals/<timestamp>.json)")
	defaultServer := "http://localhost:8080"
	if v := os.Getenv("LLAMA_SERVER_URL"); v != "" {
		defaultServer = v
	}
	server := flag.String("server", defaultServer, "llama.cpp server URL, queried for run provenance (env: LLAMA_SERVER_URL)")
	flag.Parse()

	// One timestamp shared by the report filename and the provenance block.
	runTimestamp := time.Now().UTC().Format(time.RFC3339)
	reportPath := *report
	if reportPath == "" {
		reportPath = filepath.Join("evals", runTimestamp+".json")
	}

	gt, err := loadGroundTruth(*groundtruth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	seen := make(map[string]bool)
	var videoIDs []string
	for _, e := range gt.Entries {
		if !seen[e.VideoID] {
			seen[e.VideoID] = true
			videoIDs = append(videoIDs, e.VideoID)
		}
	}
	fmt.Fprintf(os.Stderr, "Running extractor on %d video(s): %s\n", len(videoIDs), strings.Join(videoIDs, ", "))

	// Snapshot git/server provenance *before* the long extractor run, so a commit
	// made while the eval is running can't bleed into the recorded git state. The
	// run reflects the code as it was at launch, not whatever HEAD is when the
	// report is finally written.
	// TODO: This is dumb, I want the code as it was at build. But whatever.
	prov := gatherProvenance(runTimestamp, *extractor, *server)

	tmpFile, err := os.CreateTemp("", "winerank-eval-*.json")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error creating temp file: %v\n", err)
		os.Exit(1)
	}
	tmpFile.Close()
	defer os.Remove(tmpFile.Name())

	if err := runExtractor(*extractor, *transcriptsDir, tmpFile.Name(), videoIDs); err != nil {
		fmt.Fprintf(os.Stderr, "extractor failed: %v\n", err)
		os.Exit(1)
	}

	output, err := loadExtractorOutput(tmpFile.Name())
	if err != nil {
		fmt.Fprintf(os.Stderr, "error loading extractor output: %v\n", err)
		os.Exit(1)
	}

	m, videos := computeMetrics(gt, output)
	fmt.Println(m.Summary())

	perf := summarizePerf(output)
	if perf != nil {
		fmt.Fprintf(os.Stderr, "perf: %d video(s) in %s, %d prompt + %d completion tokens (%.1f completion tok/s)\n",
			perf.Videos, (time.Duration(perf.TotalDurationMS) * time.Millisecond).Round(time.Millisecond),
			perf.TotalPromptTokens, perf.TotalCompletionTokens, perf.CompletionTokensPerSec)
	}

	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		fmt.Fprintf(os.Stderr, "error creating report directory: %v\n", err)
		os.Exit(1)
	}
	if err := writeReport(reportPath, m, videos, perf, prov); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Report written to %s\n", reportPath)
}
