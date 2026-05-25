package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// TranscriptFile represents the contents of a transcript JSON file.
type TranscriptFile struct {
	Transcript  string `json:"transcript"`
	Description string `json:"description"`
}

// LoadTranscript reads and unmarshals a single transcript JSON file.
func LoadTranscript(path string) (*TranscriptFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading %s: %w", path, err)
	}
	var tf TranscriptFile
	if err := json.Unmarshal(data, &tf); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return &tf, nil
}

// LoadAllTranscripts loads all .json files from a directory.
// Returns a map from video ID (filename stem) to transcript data.
func LoadAllTranscripts(dir string) (map[string]*TranscriptFile, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("reading directory %s: %w", dir, err)
	}

	result := make(map[string]*TranscriptFile)
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, entry.Name())
		tf, err := LoadTranscript(path)
		if err != nil {
			return nil, err
		}
		videoID := strings.TrimSuffix(entry.Name(), ".json")
		result[videoID] = tf
	}

	if len(result) == 0 {
		return nil, fmt.Errorf("no .json transcript files found in %s", dir)
	}
	return result, nil
}
