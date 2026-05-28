package main

import "fmt"

type metrics struct {
	GTCount         int // total GT entries across all GT videos
	MatchedCount    int // GT entries matched to an extraction
	ScoreMatchCount int // matched pairs where scores agree
	UnjudgedCount   int // extracted wines with no GT match
}

// computeMetrics runs matchVideo for every video that has GT entries and
// aggregates the results. Videos present in the extractor output but absent
// from GT are ignored (their wines all become unjudged).
func computeMetrics(gt GroundTruth, output ExtractorOutput) metrics {
	// Group GT entries by video ID.
	byVideo := make(map[string][]GroundTruthEntry)
	for _, e := range gt.Entries {
		byVideo[e.VideoID] = append(byVideo[e.VideoID], e)
	}

	// Index extractor output by video ID.
	extractedByVideo := make(map[string][]ExtractedWine)
	for _, v := range output.Results {
		extractedByVideo[v.VideoID] = v.Wines
	}

	var m metrics
	for videoID, gtEntries := range byVideo {
		extracted := extractedByVideo[videoID] // nil if video not in output
		r := matchVideo(gtEntries, extracted)

		m.GTCount += len(gtEntries)
		m.MatchedCount += len(r.Matched)
		m.UnjudgedCount += len(r.Unjudged)

		for _, pair := range r.Matched {
			if scoresEqual(pair.GT.Score, pair.Extracted.Score) {
				m.ScoreMatchCount++
			}
		}
	}

	// Count unjudged wines from videos that have no GT at all.
	for videoID, wines := range extractedByVideo {
		if _, hasGT := byVideo[videoID]; !hasGT {
			m.UnjudgedCount += len(wines)
		}
	}

	return m
}

func scoresEqual(a, b *int) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	return *a == *b
}

// Summary returns a one-line human-readable summary suitable for CI logs.
func (m metrics) Summary() string {
	recallPct := 0.0
	if m.GTCount > 0 {
		recallPct = 100 * float64(m.MatchedCount) / float64(m.GTCount)
	}
	scorePct := 0.0
	if m.MatchedCount > 0 {
		scorePct = 100 * float64(m.ScoreMatchCount) / float64(m.MatchedCount)
	}
	return fmt.Sprintf("recall %d/%d (%.1f%%)  score-match %d/%d (%.1f%%)  unjudged %d",
		m.MatchedCount, m.GTCount, recallPct,
		m.ScoreMatchCount, m.MatchedCount, scorePct,
		m.UnjudgedCount,
	)
}
