package main

import (
	"flag"
	"fmt"
)

func main() {
	extractor := flag.String("extractor", "nix run .#extract-scores --", "command used to invoke the extractor")
	groundtruth := flag.String("groundtruth", "scores_groundtruth.json", "path to ground truth file")
	transcriptsDir := flag.String("transcripts-dir", "./transcripts", "directory containing transcript JSON files")
	report := flag.String("report", "eval-report.json", "path to write the detailed eval report")
	flag.Parse()

	fmt.Printf("extractor:      %s\n", *extractor)
	fmt.Printf("groundtruth:    %s\n", *groundtruth)
	fmt.Printf("transcripts-dir: %s\n", *transcriptsDir)
	fmt.Printf("report:         %s\n", *report)
}
