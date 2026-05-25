package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible chat completions API.
type Client struct {
	ServerURL  string
	Model      string
	HTTPClient *http.Client
}

// NewClient creates a new LLM client.
func NewClient(serverURL, model string) *Client {
	return &Client{
		ServerURL: serverURL,
		Model:     model,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute, // LLM inference can be slow on local hardware
		},
	}
}

// chatRequest is the OpenAI chat completions request body.
type chatRequest struct {
	Model          string        `json:"model,omitempty"`
	Messages       []chatMessage `json:"messages"`
	Temperature    float64       `json:"temperature"`
	ResponseFormat *respFormat   `json:"response_format,omitempty"`
	MaxTokens      int           `json:"max_tokens,omitempty"`
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type       string      `json:"type"`
	JSONSchema *jsonSchema `json:"json_schema,omitempty"`
}

type jsonSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict,omitempty"`
}

const segmentSchema = `{
  "type": "object",
  "properties": {
    "placeholder_mappings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "placeholder": { "type": "string" },
          "wine_name": { "type": "string" }
        },
        "required": ["placeholder", "wine_name"],
        "additionalProperties": false
      }
    },
    "tasting_segments": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "placeholder": { "type": "string" },
          "start": { "type": "integer" }
        },
        "required": ["placeholder", "start"],
        "additionalProperties": false
      }
    },
    "reveal_segments": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "placeholder": { "type": "string" },
          "start": { "type": "integer" }
        },
        "required": ["placeholder", "start"],
        "additionalProperties": false
      }
    }
  },
  "required": ["placeholder_mappings", "tasting_segments", "reveal_segments"],
  "additionalProperties": false
}`

const extractSchema = `{
  "type": "object",
  "properties": {
    "producer": { "type": "string" },
    "vintage": { "type": "string" },
    "region": { "type": "string" },
    "score": { "type": ["integer", "null"] },
    "notes_summary": { "type": "string" },
    "matching_snippet": { "type": "string" }
  },
  "required": ["producer", "vintage", "region", "score", "notes_summary", "matching_snippet"],
  "additionalProperties": false
}`

// chatResponse is the relevant subset of the OpenAI chat completions response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Extract implements the single-pass segmentation & score extraction pipeline
func (c *Client) Extract(ctx context.Context, tf *TranscriptFile) ([]WineScore, *segmentResponse, error) {
	sentences := splitIntoSentences(tf.Transcript)
	totalSents := len(sentences)
	if totalSents == 0 {
		return nil, nil, fmt.Errorf("empty transcript")
	}

	// Build Full Transcript (original 1-based index numbers prefixing sentences)
	var fullSb strings.Builder
	for idx := 0; idx < totalSents; idx++ {
		fullSb.WriteString(fmt.Sprintf("[%d] %s\n", idx+1, sentences[idx]))
	}
	fullTranscript := fullSb.String()

	log.Printf("Segmenting transcript (single-pass)...")
	segmentReqMsg := BuildSegmentUserMessage(tf.Description, fullTranscript)
	var segResp segmentResponse
	err := c.doChatRequest(ctx, segmentSystemPrompt, segmentReqMsg, 4096, segmentSchema, &segResp)
	if err != nil {
		return nil, nil, fmt.Errorf("transcript segmentation failed: %w", err)
	}

	segJSON, _ := json.MarshalIndent(segResp, "", "  ")
	log.Printf("Segmenter LLM Response:\n%s\n", string(segJSON))

	mappings := combineMappings(segResp, totalSents)

	log.Printf("Segmented into %d wine mappings. Extracting scores...", len(mappings))

	var wines []WineScore
	for idx, m := range mappings {
		var segmentParts []string
		indices := parseRanges(m.SentenceIndices)
		for _, sIdx := range indices {
			if sIdx >= 1 && sIdx <= len(sentences) {
				segmentParts = append(segmentParts, sentences[sIdx-1])
			}
		}
		focusedText := strings.Join(segmentParts, " ")

		var extResp extractResponse
		if len(focusedText) > 0 {
			log.Printf("  [%d/%d] Extracting score for: %s", idx+1, len(mappings), m.WineName)
			extractReqMsg := BuildExtractUserMessage(m.WineName, focusedText)
			err := c.doChatRequest(ctx, extractSystemPrompt, extractReqMsg, 1024, extractSchema, &extResp)
			if err != nil {
				log.Printf("Warning: step 2 extraction failed for wine %q: %v. Using defaults.", m.WineName, err)
			}
		} else {
			log.Printf("  [%d/%d] No transcript segment mapped for: %s (will default to no score)", idx+1, len(mappings), m.WineName)
		}

		wines = append(wines, WineScore{
			Name:            m.WineName,
			Producer:        extResp.Producer,
			Vintage:         extResp.Vintage,
			Region:          extResp.Region,
			Score:           extResp.Score,
			NotesSummary:    extResp.NotesSummary,
			MatchingSnippet: extResp.MatchingSnippet,
			ReviewStatus:    "pending",
		})
	}

	return wines, &segResp, nil
}

func (c *Client) doChatRequest(ctx context.Context, systemMsg, userMsg string, maxTokens int, schema string, target interface{}) error {
	var respFmt *respFormat
	if schema != "" {
		respFmt = &respFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchema{
				Name:   "response_schema",
				Schema: json.RawMessage(schema),
				Strict: true,
			},
		}
	} else {
		respFmt = &respFormat{Type: "json_object"}
	}

	req := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemMsg},
			{Role: "user", Content: userMsg},
		},
		Temperature:    0.1,
		ResponseFormat: respFmt,
		MaxTokens:      maxTokens,
	}

	body, err := json.Marshal(req)
	if err != nil {
		return fmt.Errorf("marshaling request: %w", err)
	}

	url := c.ServerURL + "/v1/chat/completions"

	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(delay):
			}
		}

		err := c.doSingleRequest(ctx, url, body, target)
		if err == nil {
			return nil
		}
		lastErr = err
	}
	return fmt.Errorf("after 3 attempts: %w", lastErr)
}

func (c *Client) doSingleRequest(ctx context.Context, url string, body []byte, target interface{}) error {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return fmt.Errorf("parsing response JSON: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return fmt.Errorf("no choices in response")
	}

	content := chatResp.Choices[0].Message.Content
	content = stripCodeFences(content)

	if err := json.Unmarshal([]byte(content), target); err != nil {
		return fmt.Errorf("parsing LLM content as JSON: %w\nraw content: %s", err, content)
	}

	return nil
}

// splitIntoSentences divides text by standard punctuation, preserving sentence structure
func splitIntoSentences(text string) []string {
	var sentences []string
	var current strings.Builder

	text = strings.ReplaceAll(text, "\n", " ")
	text = strings.ReplaceAll(text, "\r", " ")

	runes := []rune(text)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		current.WriteRune(r)
		if (r == '.' || r == '?' || r == '!') && (i+1 == len(runes) || runes[i+1] == ' ' || runes[i+1] == '\n' || runes[i+1] == '\r') {
			s := strings.TrimSpace(current.String())
			if len(s) > 0 {
				sentences = append(sentences, s)
			}
			current.Reset()
			if i+1 < len(runes) && runes[i+1] == ' ' {
				i++
			}
		}
	}
	s := strings.TrimSpace(current.String())
	if len(s) > 0 {
		sentences = append(sentences, s)
	}
	return sentences
}

// stripCodeFences removes markdown code fences (```json ... ```) that LLMs
// frequently wrap around JSON output despite being told not to.
func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	// Handle ```json or ``` prefix
	if strings.HasPrefix(s, "```") {
		// Remove first line (the opening fence)
		if idx := strings.Index(s, "\n"); idx != -1 {
			s = s[idx+1:]
		}
		// Remove trailing ```
		if strings.HasSuffix(strings.TrimSpace(s), "```") {
			s = strings.TrimSpace(s)
			s = s[:len(s)-3]
		}
	}
	return strings.TrimSpace(s)
}

// parseRanges parses a comma-separated string of ranges or numbers (e.g. "1-5, 8, 12-15") into an slice of individual ints.
func parseRanges(s string) []int {
	var indices []int
	parts := strings.Split(s, ",")
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		if strings.Contains(part, "-") {
			subParts := strings.Split(part, "-")
			if len(subParts) == 2 {
				var start, end int
				if _, err := fmt.Sscanf(strings.TrimSpace(subParts[0]), "%d", &start); err == nil {
					if _, err := fmt.Sscanf(strings.TrimSpace(subParts[1]), "%d", &end); err == nil {
						for i := start; i <= end; i++ {
							indices = append(indices, i)
						}
					}
				}
			}
		} else {
			var val int
			if _, err := fmt.Sscanf(part, "%d", &val); err == nil {
				indices = append(indices, val)
			}
		}
	}
	return indices
}

func combineMappings(segResp segmentResponse, totalSentences int) []segmentMapping {
	placeholderToWine := make(map[string]string)
	for _, pm := range segResp.PlaceholderMappings {
		key := cleanPlaceholder(pm.Placeholder)
		placeholderToWine[key] = pm.WineName
	}

	wineSentences := make(map[string][]int)

	// Helper to resolve placeholder to wine name
	resolveWine := func(placeholder string) string {
		key := cleanPlaceholder(placeholder)
		if name, exists := placeholderToWine[key]; exists {
			return name
		}
		// Try fallback substring match
		for _, pm := range segResp.PlaceholderMappings {
			pClean := cleanPlaceholder(placeholder)
			pmClean := cleanPlaceholder(pm.Placeholder)
			if pClean == pmClean || strings.Contains(pClean, pmClean) || strings.Contains(pmClean, pClean) {
				return pm.WineName
			}
		}
		// Also try substring matching the wine name
		for _, pm := range segResp.PlaceholderMappings {
			wName := strings.ToLower(pm.WineName)
			pName := strings.ToLower(placeholder)
			if strings.Contains(wName, pName) || strings.Contains(pName, wName) {
				return pm.WineName
			}
		}
		return ""
	}

	// Sort tasting segments
	tastSegs := append([]tastingSegment(nil), segResp.TastingSegments...)
	sort.Slice(tastSegs, func(i, j int) bool {
		return tastSegs[i].Start < tastSegs[j].Start
	})

	// Sort reveal segments
	revSegs := append([]revealSegment(nil), segResp.RevealSegments...)
	sort.Slice(revSegs, func(i, j int) bool {
		return revSegs[i].Start < revSegs[j].Start
	})

	minRevealStart := totalSentences + 1
	if len(revSegs) > 0 {
		minRevealStart = revSegs[0].Start
	}

	// Add tasting sentences
	for i, seg := range tastSegs {
		wineName := resolveWine(seg.Placeholder)
		if wineName == "" {
			continue
		}
		start := seg.Start
		end := minRevealStart - 1
		if i+1 < len(tastSegs) {
			end = tastSegs[i+1].Start - 1
		}
		if start < 1 {
			start = 1
		}
		if end > totalSentences {
			end = totalSentences
		}
		for sIdx := start; sIdx <= end; sIdx++ {
			wineSentences[wineName] = append(wineSentences[wineName], sIdx)
		}
	}

	// Add reveal sentences
	for j, seg := range revSegs {
		wineName := resolveWine(seg.Placeholder)
		if wineName == "" {
			continue
		}
		start := seg.Start
		end := totalSentences
		if j+1 < len(revSegs) {
			end = revSegs[j+1].Start - 1
		}
		if start < 1 {
			start = 1
		}
		if end > totalSentences {
			end = totalSentences
		}
		for sIdx := start; sIdx <= end; sIdx++ {
			wineSentences[wineName] = append(wineSentences[wineName], sIdx)
		}
	}

	// Convert to segmentMapping and format ranges
	var mappings []segmentMapping
	for wineName, indices := range wineSentences {
		uniqueIndices := uniqueAndSort(indices)
		mappings = append(mappings, segmentMapping{
			WineName:        wineName,
			SentenceIndices: formatRanges(uniqueIndices),
		})
	}

	// Sort mappings by their first sentence index to preserve chronological order
	sort.Slice(mappings, func(i, j int) bool {
		idxI := parseRanges(mappings[i].SentenceIndices)
		idxJ := parseRanges(mappings[j].SentenceIndices)
		if len(idxI) == 0 {
			return false
		}
		if len(idxJ) == 0 {
			return true
		}
		return idxI[0] < idxJ[0]
	})

	return mappings
}

func uniqueAndSort(slice []int) []int {
	keys := make(map[int]bool)
	var list []int
	for _, entry := range slice {
		if _, value := keys[entry]; !value {
			keys[entry] = true
			list = append(list, entry)
		}
	}
	sort.Ints(list)
	return list
}

func cleanPlaceholder(s string) string {
	s = strings.ToLower(s)
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, "_", "")
	s = strings.ReplaceAll(s, "number", "")
	return s
}

func formatRanges(indices []int) string {
	if len(indices) == 0 {
		return ""
	}
	sort.Ints(indices)
	var ranges []string
	start := indices[0]
	prev := indices[0]
	for i := 1; i < len(indices); i++ {
		if indices[i] == prev+1 {
			prev = indices[i]
		} else {
			if start == prev {
				ranges = append(ranges, fmt.Sprintf("%d", start))
			} else {
				ranges = append(ranges, fmt.Sprintf("%d-%d", start, prev))
			}
			start = indices[i]
			prev = indices[i]
		}
	}
	if start == prev {
		ranges = append(ranges, fmt.Sprintf("%d", start))
	} else {
		ranges = append(ranges, fmt.Sprintf("%d-%d", start, prev))
	}
	return strings.Join(ranges, ", ")
}

