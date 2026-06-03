package main

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
)

type metrics struct {
	GTCount         int
	MatchedCount    int
	ScoreMatchCount int
	UnjudgedCount   int
}

// computeMetrics runs matchVideo for every video that has GT entries and
// aggregates the results. Videos present in the extractor output but absent
// from GT are ignored (their wines all become unjudged).
func computeMetrics(gt GroundTruth, output ExtractorOutput) (metrics, []videoReport) {
	byVideo := make(map[string][]GroundTruthEntry)
	for _, e := range gt.Entries {
		byVideo[e.VideoID] = append(byVideo[e.VideoID], e)
	}

	extractedByVideo := make(map[string][]ExtractedWine)
	for _, v := range output.Results {
		extractedByVideo[v.VideoID] = v.Wines
	}

	var m metrics
	var reports []videoReport

	videoIDs := make([]string, 0, len(byVideo))
	for id := range byVideo {
		videoIDs = append(videoIDs, id)
	}
	sort.Strings(videoIDs)

	for _, videoID := range videoIDs {
		gtEntries := byVideo[videoID]
		extracted := extractedByVideo[videoID]
		r := matchVideo(gtEntries, extracted)

		m.GTCount += len(gtEntries)
		m.MatchedCount += len(r.Matched)
		m.UnjudgedCount += len(r.Unjudged)

		vr := videoReport{
			VideoID:     videoID,
			UnmatchedGT: r.UnmatchedGT,
			Unjudged:    r.Unjudged,
		}
		for _, pair := range r.Matched {
			sm := scoresEqual(pair.GT.Score, pair.Extracted.Score)
			if sm {
				m.ScoreMatchCount++
			}
			vr.Matched = append(vr.Matched, matchedPairReport{
				GTWine:         pair.GT.WineName,
				GTScore:        pair.GT.Score,
				ExtractedWine:  pair.Extracted.Name,
				ExtractedScore: pair.Extracted.Score,
				ScoreMatch:     sm,
				Similarity:     pair.Similarity,
			})
		}
		reports = append(reports, vr)
	}

	// Unjudged wines from videos with no GT at all.
	for videoID, wines := range extractedByVideo {
		if _, hasGT := byVideo[videoID]; !hasGT {
			m.UnjudgedCount += len(wines)
			reports = append(reports, videoReport{
				VideoID:  videoID,
				Unjudged: wines,
			})
		}
	}

	return m, reports
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

func writeReport(path string, m metrics, videos []videoReport, prov *Provenance) error {
	r := evalReport{
		Provenance:      prov,
		Summary:         m.Summary(),
		GTCount:         m.GTCount,
		MatchedCount:    m.MatchedCount,
		ScoreMatchCount: m.ScoreMatchCount,
		UnjudgedCount:   m.UnjudgedCount,
		Videos:          videos,
	}
	data, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
