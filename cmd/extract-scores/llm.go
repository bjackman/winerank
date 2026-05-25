package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
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
    "mappings": {
      "type": "array",
      "items": {
        "type": "object",
        "properties": {
          "wine_name": { "type": "string" },
          "sentence_indices": { "type": "string" }
        },
        "required": ["wine_name", "sentence_indices"],
        "additionalProperties": false
      }
    }
  },
  "required": ["mappings"],
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

// Extract implements the two-step segmentation & extraction pipeline
func (c *Client) Extract(ctx context.Context, tf *TranscriptFile, showSegments bool) ([]WineScore, *segmentResponse, error) {
	sentences := splitIntoSentences(tf.Transcript)
	var sb strings.Builder
	for i, s := range sentences {
		sb.WriteString(fmt.Sprintf("[%d] %s\n", i+1, s))
	}
	numberedTranscript := sb.String()

	log.Printf("Segmenting transcript into individual wine reviews...")
	segmentReqMsg := BuildSegmentUserMessage(tf.Description, numberedTranscript)

	var segResp segmentResponse
	err := c.doChatRequest(ctx, segmentSystemPrompt, segmentReqMsg, 2048, segmentSchema, &segResp)
	if err != nil {
		return nil, nil, fmt.Errorf("step 1 segmentation failed: %w", err)
	}

	log.Printf("Segmented into %d wine mappings. Extracting scores...", len(segResp.Mappings))
	if showSegments {
		printSegmentedTranscript(tf, segResp.Mappings)
	}

	var wines []WineScore
	for idx, m := range segResp.Mappings {
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
			log.Printf("  [%d/%d] Extracting score for: %s", idx+1, len(segResp.Mappings), m.WineName)
			extractReqMsg := BuildExtractUserMessage(m.WineName, focusedText)
			err := c.doChatRequest(ctx, extractSystemPrompt, extractReqMsg, 1024, extractSchema, &extResp)
			if err != nil {
				log.Printf("Warning: step 2 extraction failed for wine %q: %v. Using defaults.", m.WineName, err)
			}
		} else {
			log.Printf("  [%d/%d] No transcript segment mapped for: %s (will default to no score)", idx+1, len(segResp.Mappings), m.WineName)
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

