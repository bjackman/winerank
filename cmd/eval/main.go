package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
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
	report := flag.String("report", "eval-report.json", "path to write the detailed eval report")
	flag.Parse()

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

	if err := writeReport(*report, m, videos); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report: %v\n", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Report written to %s\n", *report)
}
