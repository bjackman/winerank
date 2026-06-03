package main

import "encoding/json"

// GroundTruthEntry is one record from scores_groundtruth.json.
// TranscriptSnippet is stored for human auditing; we ignore it here.
type GroundTruthEntry struct {
	VideoID  string `json:"video_id"`
	WineName string `json:"wine_name"`
	Score    *int   `json:"score"`
}

// GroundTruth is the top-level structure of scores_groundtruth.json.
type GroundTruth struct {
	Entries []GroundTruthEntry `json:"ground_truths"`
}

// ExtractedWine holds the fields we grade from the extractor's output.
// Other fields (producer, vintage, region, notes) are present in the real
// output but irrelevant to the eval.
type ExtractedWine struct {
	Name  string `json:"name"`
	Score *int   `json:"score"`
}

// ExtractedVideo groups extracted wines for one video.
type ExtractedVideo struct {
	VideoID string          `json:"video_id"`
	Wines   []ExtractedWine `json:"wines"`
}

// ExtractorOutput is the top-level structure written by the extractor.
type ExtractorOutput struct {
	Results []ExtractedVideo `json:"results"`
}

// matchedPairReport is one matched (GT, extracted) pair for the report.
type matchedPairReport struct {
	GTWine        string `json:"gt_wine"`
	GTScore       *int   `json:"gt_score"`
	ExtractedWine string `json:"extracted_wine"`
	ExtractedScore *int  `json:"extracted_score"`
	ScoreMatch    bool   `json:"score_match"`
	Similarity    float64 `json:"similarity"`
}

// videoReport holds per-video matching detail for the report.
type videoReport struct {
	VideoID     string              `json:"video_id"`
	Matched     []matchedPairReport `json:"matched"`
	UnmatchedGT []GroundTruthEntry  `json:"unmatched_gt"`
	Unjudged    []ExtractedWine     `json:"unjudged"`
}

// Provenance captures the parameters that influence a run's results so the
// experiment can be reproduced. ServerProps and ServerModels hold the raw JSON
// from the llama.cpp server's /props and /v1/models endpoints, so whatever the
// server reports (model path, context size, build, chat template, default
// sampling) is preserved verbatim regardless of llama.cpp version.
type Provenance struct {
	Timestamp    string          `json:"timestamp"`
	GitCommit    string          `json:"git_commit"`
	GitDirty     bool            `json:"git_dirty"`
	Extractor    string          `json:"extractor"`
	ServerURL    string          `json:"server_url"`
	ModelID      string          `json:"model_id,omitempty"`
	ServerProps  json.RawMessage `json:"server_props,omitempty"`
	ServerModels json.RawMessage `json:"server_models,omitempty"`
}

// evalReport is the top-level structure written to the report file.
type evalReport struct {
	Provenance      *Provenance   `json:"provenance,omitempty"`
	Summary         string        `json:"summary"`
	GTCount         int           `json:"gt_count"`
	MatchedCount    int           `json:"matched_count"`
	ScoreMatchCount int           `json:"score_match_count"`
	UnjudgedCount   int           `json:"unjudged_count"`
	Videos          []videoReport `json:"videos"`
}
