package main

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
