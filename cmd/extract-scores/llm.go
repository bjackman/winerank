package main

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"
)

// Client talks to an OpenAI-compatible chat completions API.
type Client struct {
	ServerURL        string
	Model            string
	StructuredOutput bool
	// Reasoning controls the chat_template_kwargs enable_thinking flag:
	// "off" disables thinking, "on" forces it, "" leaves the template default.
	Reasoning string
	// ObserveAfter: if a streaming request is still running after this long,
	// echo the model's live output to stderr. Zero disables live echoing.
	ObserveAfter time.Duration
	// Strategy selects the extraction pipeline: "multi-pass" (segment then
	// per-wine extract) or "single-pass" (one prompt for all wines).
	Strategy   string
	HTTPClient *http.Client
}

// NewClient creates a new LLM client. When structuredOutput is false the client
// omits the json_schema response_format, avoiding the server-side grammar
// sampler (a workaround for llama.cpp grammar crashes).
func NewClient(serverURL, model string, structuredOutput bool) *Client {
	return &Client{
		ServerURL:        serverURL,
		Model:            model,
		StructuredOutput: structuredOutput,
		HTTPClient: &http.Client{
			Timeout: 10 * time.Minute, // LLM inference can be slow on local hardware
		},
	}
}

// chatRequest is the OpenAI chat completions request body.
type chatRequest struct {
	Model              string         `json:"model,omitempty"`
	Messages           []chatMessage  `json:"messages"`
	Temperature        float64        `json:"temperature"`
	ResponseFormat     *respFormat    `json:"response_format,omitempty"`
	MaxTokens          int            `json:"max_tokens,omitempty"`
	ChatTemplateKwargs map[string]any `json:"chat_template_kwargs,omitempty"`
	Stream             bool           `json:"stream,omitempty"`
	StreamOptions      *streamOptions `json:"stream_options,omitempty"`
}

type streamOptions struct {
	IncludeUsage bool `json:"include_usage"`
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
    "video_type": { "type": "string", "enum": ["blind", "open"] },
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
  "required": ["video_type", "placeholder_mappings", "tasting_segments", "reveal_segments"],
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

const singlePassSchema = `{
  "type": "object",
  "properties": {
    "wines": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "name": { "type": "string" },
          "producer": { "type": "string" },
          "vintage": { "type": "string" },
          "region": { "type": "string" },
          "score": { "type": ["integer", "null"] },
          "notes_summary": { "type": "string" },
          "matching_snippet": { "type": "string" }
        },
        "required": ["name", "producer", "vintage", "region", "score", "notes_summary", "matching_snippet"],
        "additionalProperties": false
      }
    }
  },
  "required": ["wines"],
  "additionalProperties": false
}`

// usage accumulates token counts and LLM request counts across one or more
// chat completions. The zero value is ready to use; Add folds another usage in.
type usage struct {
	Requests         int `json:"requests"`
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
}

func (u *usage) Add(o usage) {
	u.Requests += o.Requests
	u.PromptTokens += o.PromptTokens
	u.CompletionTokens += o.CompletionTokens
}

// streamChunk is the relevant subset of one server-sent event from a streaming
// chat completion. reasoning_content carries the model's chain-of-thought when
// the server splits it out (e.g. llama.cpp's peg-native format).
type streamChunk struct {
	Choices []struct {
		Delta struct {
			Content          string `json:"content"`
			ReasoningContent string `json:"reasoning_content"`
		} `json:"delta"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

// Segment runs only the transcript segmentation step and returns the raw LLM
// response along with the token usage of the segmentation request.
func (c *Client) Segment(ctx context.Context, tf *TranscriptFile) (*segmentResponse, usage, error) {
	sentences := splitIntoSentences(tf.Transcript)
	if len(sentences) == 0 {
		return nil, usage{}, fmt.Errorf("empty transcript")
	}

	var fullSb strings.Builder
	for idx, s := range sentences {
		fullSb.WriteString(fmt.Sprintf("[%d] %s\n", idx+1, s))
	}

	log.Printf("Segmenting transcript...")
	var segResp segmentResponse
	u, err := c.doChatRequest(ctx, segmentSystemPrompt, BuildSegmentUserMessage(tf.Description, fullSb.String()), 4096, segmentSchema, &segResp)
	if err != nil {
		return nil, u, fmt.Errorf("transcript segmentation failed: %w", err)
	}
	return &segResp, u, nil
}

// Extract runs the configured extraction strategy. The returned segmentResponse
// is nil for the single-pass strategy, which does not segment. The returned
// usage aggregates token counts across every LLM request the strategy made.
func (c *Client) Extract(ctx context.Context, tf *TranscriptFile) ([]WineScore, *segmentResponse, usage, error) {
	if c.Strategy == "single-pass" {
		wines, u, err := c.extractSinglePass(ctx, tf)
		return wines, nil, u, err
	}
	return c.extractMultiPass(ctx, tf)
}

// extractSinglePass asks the model for every wine and its score in one request,
// with no segmentation.
func (c *Client) extractSinglePass(ctx context.Context, tf *TranscriptFile) ([]WineScore, usage, error) {
	if strings.TrimSpace(tf.Transcript) == "" {
		return nil, usage{}, fmt.Errorf("empty transcript")
	}

	log.Printf("Extracting all wines in a single pass...")
	var resp llmResponse
	userMsg := BuildSinglePassUserMessage(tf.Description, tf.Transcript)
	u, err := c.doChatRequest(ctx, singlePassSystemPrompt, userMsg, 4096, singlePassSchema, &resp)
	if err != nil {
		return nil, u, fmt.Errorf("single-pass extraction failed: %w", err)
	}

	wines := make([]WineScore, len(resp.Wines))
	for i, w := range resp.Wines {
		w.ReviewStatus = "pending"
		wines[i] = w
	}
	log.Printf("Extracted %d wines.", len(wines))
	return wines, u, nil
}

// extractMultiPass implements the two-pass segmentation + score extraction pipeline.
func (c *Client) extractMultiPass(ctx context.Context, tf *TranscriptFile) ([]WineScore, *segmentResponse, usage, error) {
	sentences := splitIntoSentences(tf.Transcript)
	totalSents := len(sentences)
	if totalSents == 0 {
		return nil, nil, usage{}, fmt.Errorf("empty transcript")
	}

	segResp, total, err := c.Segment(ctx, tf)
	if err != nil {
		return nil, nil, total, err
	}

	mappings := combineMappings(*segResp, totalSents)

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
			u, err := c.doChatRequest(ctx, extractSystemPrompt, extractReqMsg, 1024, extractSchema, &extResp)
			total.Add(u)
			if err != nil {
				return nil, nil, total, fmt.Errorf("extracting score for wine %q: %w", m.WineName, err)
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

	return wines, segResp, total, nil
}

func (c *Client) doChatRequest(ctx context.Context, systemMsg, userMsg string, maxTokens int, schema string, target interface{}) (usage, error) {
	// When structured output is disabled, omit response_format entirely so the
	// server never engages its grammar sampler. We rely on the prompt plus
	// stripCodeFences to recover JSON from the response.
	var respFmt *respFormat
	switch {
	case !c.StructuredOutput:
		respFmt = nil
	case schema != "":
		respFmt = &respFormat{
			Type: "json_schema",
			JSONSchema: &jsonSchema{
				Name:   "response_schema",
				Schema: json.RawMessage(schema),
				Strict: true,
			},
		}
	default:
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

	switch c.Reasoning {
	case "off":
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": false}
	case "on":
		req.ChatTemplateKwargs = map[string]any{"enable_thinking": true}
	}

	// Always stream so we can watch a slow request token-by-token and surface
	// its live output if it overruns ObserveAfter.
	req.Stream = true
	req.StreamOptions = &streamOptions{IncludeUsage: true}

	body, err := json.Marshal(req)
	if err != nil {
		return usage{}, fmt.Errorf("marshaling request: %w", err)
	}

	url := c.ServerURL + "/v1/chat/completions"

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return usage{}, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return usage{}, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		errBody, _ := io.ReadAll(resp.Body)
		return usage{}, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(errBody))
	}

	content, u, err := c.consumeStream(resp.Body)
	if err != nil {
		return u, err
	}

	content = stripCodeFences(content)
	if err := json.Unmarshal([]byte(content), target); err != nil {
		return u, fmt.Errorf("parsing LLM content as JSON: %w\nraw content: %s", err, content)
	}

	return u, nil
}

// consumeStream reads a streamed chat completion, returning the accumulated
// answer content. If the request runs longer than ObserveAfter, it begins
// echoing the model's live output (reasoning then answer) to stderr so a slow
// or runaway generation can be observed in real time. It also logs token
// counts and throughput once the stream finishes.
func (c *Client) consumeStream(r io.Reader) (string, usage, error) {
	reader := bufio.NewReader(r)
	var content, reasoning strings.Builder
	var promptTokens, completionTokens int

	start := time.Now()
	observing := false
	startObserving := func() {
		observing = true
		fmt.Fprintf(os.Stderr, "\n--- still running after %s, live model output: ---\n", c.ObserveAfter)
		fmt.Fprint(os.Stderr, reasoning.String(), content.String())
	}

	for {
		line, err := reader.ReadString('\n')
		if line = strings.TrimSpace(line); strings.HasPrefix(line, "data:") {
			data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
			if data != "[DONE]" {
				var chunk streamChunk
				if jsonErr := json.Unmarshal([]byte(data), &chunk); jsonErr == nil {
					if chunk.Usage != nil {
						promptTokens = chunk.Usage.PromptTokens
						completionTokens = chunk.Usage.CompletionTokens
					}
					if len(chunk.Choices) > 0 {
						d := chunk.Choices[0].Delta
						reasoning.WriteString(d.ReasoningContent)
						content.WriteString(d.Content)
						if observing {
							fmt.Fprint(os.Stderr, d.ReasoningContent, d.Content)
						}
					}
				}
				if !observing && c.ObserveAfter > 0 && time.Since(start) > c.ObserveAfter {
					startObserving()
				}
			}
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", usage{Requests: 1, PromptTokens: promptTokens, CompletionTokens: completionTokens}, fmt.Errorf("reading stream: %w", err)
		}
	}

	if observing {
		fmt.Fprintln(os.Stderr, "\n--- end live output ---")
	}

	elapsed := time.Since(start)
	if completionTokens > 0 {
		log.Printf("  LLM: %d completion tokens (%d prompt) in %s = %.1f tok/s",
			completionTokens, promptTokens, elapsed.Round(time.Millisecond), float64(completionTokens)/elapsed.Seconds())
	}

	return content.String(), usage{Requests: 1, PromptTokens: promptTokens, CompletionTokens: completionTokens}, nil
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

