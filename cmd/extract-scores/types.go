package main

// WineScore represents a single wine's extracted review data.
type WineScore struct {
	Name         string `json:"name"`
	Producer     string `json:"producer"`
	Vintage      string `json:"vintage"`
	Region       string `json:"region"`
	Score        *int   `json:"score"`          // nil if wine was discussed but not scored
	NotesSummary string `json:"notes_summary"`
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
