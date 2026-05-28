package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
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

func main() {
	extractor := flag.String("extractor", "nix run .#extract-scores --", "command used to invoke the extractor")
	groundtruth := flag.String("groundtruth", "scores_groundtruth.json", "path to ground truth file")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	report := flag.String("report", "eval-report.json", "path to write the detailed eval report")
	flag.Parse()

	fmt.Printf("extractor:       %s\n", *extractor)
	fmt.Printf("groundtruth:     %s\n", *groundtruth)
	fmt.Printf("transcripts-dir: %s\n", *transcriptsDir)
	fmt.Printf("report:          %s\n", *report)

	gt, err := loadGroundTruth(*groundtruth)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("loaded %d ground truth entries\n", len(gt.Entries))
}
