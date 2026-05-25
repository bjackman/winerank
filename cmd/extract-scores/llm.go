package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type respFormat struct {
	Type string `json:"type"`
}

// chatResponse is the relevant subset of the OpenAI chat completions response.
type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

// Extract sends the transcript to the LLM and parses the structured response.
func (c *Client) Extract(ctx context.Context, tf *TranscriptFile) ([]WineScore, error) {
	req := chatRequest{
		Model: c.Model,
		Messages: []chatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: BuildUserMessage(tf)},
		},
		Temperature:    0.1, // Low temperature for deterministic extraction
		ResponseFormat: &respFormat{Type: "json_object"},
	}

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}

	url := c.ServerURL + "/v1/chat/completions"

	// Retry loop with exponential backoff for transient errors.
	var lastErr error
	for attempt := range 3 {
		if attempt > 0 {
			delay := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}

		wines, err := c.doRequest(ctx, url, body)
		if err == nil {
			return wines, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("after 3 attempts: %w", lastErr)
}

func (c *Client) doRequest(ctx context.Context, url string, body []byte) ([]WineScore, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.HTTPClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("reading response: %w", err)
	}

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("server returned %d: %s", resp.StatusCode, string(respBody))
	}

	var chatResp chatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("parsing response JSON: %w", err)
	}

	if len(chatResp.Choices) == 0 {
		return nil, fmt.Errorf("no choices in response")
	}

	content := chatResp.Choices[0].Message.Content
	content = stripCodeFences(content)

	var llmResp llmResponse
	if err := json.Unmarshal([]byte(content), &llmResp); err != nil {
		return nil, fmt.Errorf("parsing LLM output as JSON: %w\nraw content: %s", err, content)
	}

	return llmResp.Wines, nil
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
