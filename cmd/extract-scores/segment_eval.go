package main

import (
	"bufio"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// segmentClassification represents the expected segment for a sentence.
type segmentClassification struct {
	Phase       string // "blind", "reveal", "open", or "unmapped"
	Placeholder string // e.g. "Wine 1" (empty for unmapped)
}

func (c segmentClassification) String() string {
	switch c.Phase {
	case "blind":
		return c.Placeholder + " blind"
	case "reveal":
		return c.Placeholder + " reveal"
	case "open":
		return c.Placeholder + " open"
	default:
		return "Unmapped"
	}
}

// segmentGroundTruth holds parsed ground truth for one video.
type segmentGroundTruth struct {
	// Per-sentence classification (1-based sentence index -> classification)
	Sentences map[int]segmentClassification
	// Placeholder -> wine name mapping (from reveal headers)
	PlaceholderMappings map[string]string
}

var (
	gtHeaderRe   = regexp.MustCompile(`^===\s*(.+?)(?:\s*===)?\s*$`)
	gtSentenceRe = regexp.MustCompile(`^\[(\d+)\]`)
	gtBlindRe    = regexp.MustCompile(`(?i)^Wine\s+(\d+)\s+blind$`)
	gtRevealRe   = regexp.MustCompile(`(?i)^Wine\s+(\d+)\s+reveal:\s*(.+)$`)
	gtOpenRe     = regexp.MustCompile(`(?i)^Wine\s+(\d+)\s+open:\s*(.+)$`)
)

// parseSegmentGroundTruth parses a segment ground truth file.
func parseSegmentGroundTruth(path string) (*segmentGroundTruth, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	gt := &segmentGroundTruth{
		Sentences:           make(map[int]segmentClassification),
		PlaceholderMappings: make(map[string]string),
	}

	current := segmentClassification{Phase: "unmapped"}

	scanner := bufio.NewScanner(f)
	// Set a large buffer for long lines (some transcript lines are very long).
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()

		if m := gtHeaderRe.FindStringSubmatch(line); m != nil {
			content := strings.TrimSpace(m[1])
			if strings.EqualFold(content, "Unmapped") {
				current = segmentClassification{Phase: "unmapped"}
			} else if bm := gtBlindRe.FindStringSubmatch(content); bm != nil {
				placeholder := fmt.Sprintf("Wine %s", bm[1])
				current = segmentClassification{Phase: "blind", Placeholder: placeholder}
			} else if rm := gtRevealRe.FindStringSubmatch(content); rm != nil {
				placeholder := fmt.Sprintf("Wine %s", rm[1])
				wineName := strings.TrimSpace(rm[2])
				current = segmentClassification{Phase: "reveal", Placeholder: placeholder}
				gt.PlaceholderMappings[placeholder] = wineName
			} else if om := gtOpenRe.FindStringSubmatch(content); om != nil {
				placeholder := fmt.Sprintf("Wine %s", om[1])
				wineName := strings.TrimSpace(om[2])
				current = segmentClassification{Phase: "open", Placeholder: placeholder}
				gt.PlaceholderMappings[placeholder] = wineName
			}
			continue
		}

		if m := gtSentenceRe.FindStringSubmatch(line); m != nil {
			var idx int
			fmt.Sscanf(m[1], "%d", &idx)
			gt.Sentences[idx] = current
		}
	}

	return gt, scanner.Err()
}

// classifySentencesFromResponse converts a segmentResponse into per-sentence
// classifications, using the same logic as combineMappings but preserving the
// blind/reveal/open phase distinction.
func classifySentencesFromResponse(segResp segmentResponse, totalSentences int) map[int]segmentClassification {
	result := make(map[int]segmentClassification)

	tastingPhase := "blind"
	if segResp.VideoType == "open" {
		tastingPhase = "open"
	}

	// Sort tasting segments by start index
	tastSegs := append([]tastingSegment(nil), segResp.TastingSegments...)
	sort.Slice(tastSegs, func(i, j int) bool {
		return tastSegs[i].Start < tastSegs[j].Start
	})

	// Sort reveal segments by start index
	revSegs := append([]revealSegment(nil), segResp.RevealSegments...)
	sort.Slice(revSegs, func(i, j int) bool {
		return revSegs[i].Start < revSegs[j].Start
	})

	// Find boundary between tasting and reveal phases
	minRevealStart := totalSentences + 1
	if len(revSegs) > 0 {
		minRevealStart = revSegs[0].Start
	}

	// Classify tasting sentences
	for i, seg := range tastSegs {
		end := minRevealStart - 1
		if i+1 < len(tastSegs) {
			end = tastSegs[i+1].Start - 1
		}
		placeholder := normalizePlaceholder(seg.Placeholder)
		for sIdx := seg.Start; sIdx <= end && sIdx <= totalSentences; sIdx++ {
			result[sIdx] = segmentClassification{
				Phase:       tastingPhase,
				Placeholder: placeholder,
			}
		}
	}

	// Classify reveal sentences
	for j, seg := range revSegs {
		end := totalSentences
		if j+1 < len(revSegs) {
			end = revSegs[j+1].Start - 1
		}
		placeholder := normalizePlaceholder(seg.Placeholder)
		for sIdx := seg.Start; sIdx <= end && sIdx <= totalSentences; sIdx++ {
			result[sIdx] = segmentClassification{
				Phase:       "reveal",
				Placeholder: placeholder,
			}
		}
	}

	// All remaining sentences are unmapped
	for sIdx := 1; sIdx <= totalSentences; sIdx++ {
		if _, exists := result[sIdx]; !exists {
			result[sIdx] = segmentClassification{Phase: "unmapped"}
		}
	}

	return result
}

// normalizePlaceholder converts various placeholder formats to "Wine N".
func normalizePlaceholder(s string) string {
	s = strings.TrimSpace(s)
	// Already in "Wine N" format
	re := regexp.MustCompile(`(?i)wine\s*(?:number\s*)?(\d+)`)
	if m := re.FindStringSubmatch(s); m != nil {
		return fmt.Sprintf("Wine %s", m[1])
	}
	return s
}

// segmentEvalResult holds the comparison results.
type segmentEvalResult struct {
	TotalSentences int
	Correct        int
	Errors         []segmentError

	// Placeholder mapping comparison
	MappingCorrect int
	MappingTotal   int
	MappingErrors  []mappingError
}

type segmentError struct {
	SentenceIdx int
	Sentence    string
	Expected    segmentClassification
	Got         segmentClassification
}

type mappingError struct {
	Placeholder  string
	ExpectedWine string
	GotWine      string
}

// compareSegmentation compares LLM output against ground truth.
func compareSegmentation(
	got map[int]segmentClassification,
	gt *segmentGroundTruth,
	totalSentences int,
	sentences []string,
	segResp segmentResponse,
) segmentEvalResult {
	result := segmentEvalResult{
		TotalSentences: totalSentences,
	}

	// Compare per-sentence classifications
	for sIdx := 1; sIdx <= totalSentences; sIdx++ {
		expected, hasExpected := gt.Sentences[sIdx]
		actual, hasActual := got[sIdx]

		if !hasExpected {
			// Sentence not in ground truth, skip
			result.TotalSentences--
			continue
		}
		if !hasActual {
			actual = segmentClassification{Phase: "unmapped"}
		}

		if expected.Phase == actual.Phase && expected.Placeholder == actual.Placeholder {
			result.Correct++
		} else {
			sentText := ""
			if sIdx >= 1 && sIdx <= len(sentences) {
				sentText = sentences[sIdx-1]
			}
			result.Errors = append(result.Errors, segmentError{
				SentenceIdx: sIdx,
				Sentence:    sentText,
				Expected:    expected,
				Got:         actual,
			})
		}
	}

	// Compare placeholder mappings
	llmMappings := make(map[string]string)
	for _, pm := range segResp.PlaceholderMappings {
		key := normalizePlaceholder(pm.Placeholder)
		llmMappings[key] = pm.WineName
	}

	// Collect all placeholder keys from ground truth
	var placeholders []string
	for p := range gt.PlaceholderMappings {
		placeholders = append(placeholders, p)
	}
	sort.Strings(placeholders)

	result.MappingTotal = len(placeholders)
	for _, p := range placeholders {
		expectedWine := gt.PlaceholderMappings[p]
		gotWine := llmMappings[p]
		if strings.EqualFold(strings.TrimSpace(expectedWine), strings.TrimSpace(gotWine)) {
			result.MappingCorrect++
		} else {
			result.MappingErrors = append(result.MappingErrors, mappingError{
				Placeholder:  p,
				ExpectedWine: expectedWine,
				GotWine:      gotWine,
			})
		}
	}

	return result
}

// segmentGroundTruthPath returns the expected path for a segment ground truth file.
func segmentGroundTruthPath(videoID string) string {
	return fmt.Sprintf("segment_groundtruth_%s.txt", videoID)
}
