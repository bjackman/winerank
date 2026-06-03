package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
)

// gatherProvenance collects everything that influences a run's results: the
// winerank git state, the extractor command, and the live server's reported
// config. Server/git lookups are best-effort — a failure leaves that field
// empty rather than aborting the eval.
func gatherProvenance(timestamp, extractorCmd, serverURL string) *Provenance {
	p := &Provenance{
		Timestamp: timestamp,
		Extractor: extractorCmd,
		ServerURL: serverURL,
	}

	if out, err := exec.Command("git", "rev-parse", "HEAD").Output(); err == nil {
		p.GitCommit = strings.TrimSpace(string(out))
	}
	if out, err := exec.Command("git", "status", "--porcelain").Output(); err == nil {
		p.GitDirty = len(strings.TrimSpace(string(out))) > 0
	}

	if props, err := httpGetRaw(serverURL + "/props"); err == nil {
		p.ServerProps = props
	}
	if models, err := httpGetRaw(serverURL + "/v1/models"); err == nil {
		p.ServerModels = models
		p.ModelID = firstModelID(models)
	}

	return p
}

// httpGetRaw fetches a URL and returns its body as raw JSON, validating that it
// parses so we never embed garbage into the report.
func httpGetRaw(url string) (json.RawMessage, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("status %d", resp.StatusCode)
	}
	if !json.Valid(body) {
		return nil, fmt.Errorf("response from %s was not valid JSON", url)
	}
	return json.RawMessage(body), nil
}

// firstModelID pulls the first model id out of an OpenAI /v1/models response.
func firstModelID(raw json.RawMessage) string {
	var m struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &m); err == nil && len(m.Data) > 0 {
		return m.Data[0].ID
	}
	return ""
}
