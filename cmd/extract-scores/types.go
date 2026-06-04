package main

// WineScore represents a single wine's extracted review data.
type WineScore struct {
	Name            string `json:"name"`
	Producer        string `json:"producer"`
	Vintage         string `json:"vintage"`
	Region          string `json:"region"`
	Score           *int   `json:"score"`            // nil if wine was discussed but not scored
	NotesSummary    string `json:"notes_summary"`
	MatchingSnippet string `json:"matching_snippet"` // verbatim snippet of transcript matching the review
	ReviewStatus    string `json:"review_status"`    // review status: "pending", "approved", "rejected"
}

// VideoResult groups the extracted wine scores for a single video.
type VideoResult struct {
	VideoID string      `json:"video_id"`
	Wines   []WineScore `json:"wines"`
}

// Output is the top-level structure written to scores.json.
type Output struct {
	Results []VideoResult `json:"results"`
}

// llmResponse is the JSON schema we ask the LLM to return.
type llmResponse struct {
	Wines []WineScore `json:"wines"`
}

// Structures for Step 1 segmentation mappings
type placeholderMapping struct {
	Placeholder string `json:"placeholder"`
	WineName    string `json:"wine_name"`
}

type tastingSegment struct {
	Placeholder string `json:"placeholder"`
	Start       int    `json:"start"` // 1-based start sentence index
}

type revealSegment struct {
	Placeholder string `json:"placeholder"`
	Start       int    `json:"start"` // 1-based start sentence index
}

type segmentResponse struct {
	VideoType           string               `json:"video_type"` // "blind" or "open"
	PlaceholderMappings []placeholderMapping `json:"placeholder_mappings"`
	TastingSegments     []tastingSegment     `json:"tasting_segments"`
	RevealSegments      []revealSegment      `json:"reveal_segments"`
}

type segmentMapping struct {
	WineName        string `json:"wine_name"`
	SentenceIndices string `json:"sentence_indices"` // comma-separated ranges or numbers, e.g. "48-67, 84-87"
}

// Structures for Step 2 score extraction response
type extractResponse struct {
	Producer        string `json:"producer"`
	Vintage         string `json:"vintage"`
	Region          string `json:"region"`
	Score           *int   `json:"score"`
	NotesSummary    string `json:"notes_summary"`
	MatchingSnippet string `json:"matching_snippet"`
}

// Structures for final scores.json output (omitting internal review metadata)
type FinalWineScore struct {
	Name         string `json:"name"`
	Producer     string `json:"producer"`
	Vintage      string `json:"vintage"`
	Region       string `json:"region"`
	Score        *int   `json:"score"` // nil if wine was discussed but not scored
	NotesSummary string `json:"notes_summary"`
}

// ExtractStats records the cost of extracting one video: wall-clock time and
// the token usage of the LLM request(s) involved. Wall-clock is influenced by
// server load and hardware (llama-server, ROCm), so it is reported alongside
// token counts to let throughput be assessed independently of that noise. It is
// omitted for videos served from cache (no LLM call was made).
type ExtractStats struct {
	Requests         int   `json:"requests"`
	PromptTokens     int   `json:"prompt_tokens"`
	CompletionTokens int   `json:"completion_tokens"`
	DurationMS       int64 `json:"duration_ms"`
}

type FinalVideoResult struct {
	VideoID string           `json:"video_id"`
	Wines   []FinalWineScore `json:"wines"`
	Stats   *ExtractStats    `json:"stats,omitempty"`
}

type FinalOutput struct {
	Results []FinalVideoResult `json:"results"`
}

// Structures for scores_groundtruth.json
type GroundTruthRecord struct {
	VideoID           string `json:"video_id"`
	WineName          string `json:"wine_name"`
	TranscriptSnippet string `json:"transcript_snippet"`
	Score             *int   `json:"score"`
}

type GroundTruth struct {
	Records []GroundTruthRecord `json:"ground_truths"`
}
