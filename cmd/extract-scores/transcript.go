package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

var asrNumberRe = regexp.MustCompile(`\b(10|20|30|40|50|60|70|80|90)\s+(one|two|three|four|five|six|seven|eight|nine)\b`)

var asrUnitWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5,
	"six": 6, "seven": 7, "eight": 8, "nine": 9,
}

// Workaround for the failure mode when the transcript says "90 one" and the
// extractor sees this as 90: just replace stuff like "90 one" with "91".
func normalizeASRNumbers(s string) string {
	return asrNumberRe.ReplaceAllStringFunc(s, func(match string) string {
		parts := asrNumberRe.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		var decade int
		fmt.Sscanf(parts[1], "%d", &decade)
		return fmt.Sprintf("%d", decade+asrUnitWords[parts[2]])
	})
}

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
	tf.Transcript = normalizeASRNumbers(tf.Transcript)
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
