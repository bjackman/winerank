package main

import "sort"

type matchedPair struct {
	GT         GroundTruthEntry
	Extracted  ExtractedWine
	Similarity float64
}

type matchResult struct {
	Matched     []matchedPair
	UnmatchedGT []GroundTruthEntry
	Unjudged    []ExtractedWine
}

// matchVideo greedily matches GT entries to extracted wines for one video by
// descending similarity, ensuring each wine appears in at most one pair.
func matchVideo(gt []GroundTruthEntry, extracted []ExtractedWine) matchResult {
	type candidate struct {
		gtIdx  int
		exIdx  int
		sim    float64
	}

	var candidates []candidate
	for gi, g := range gt {
		for ei, e := range extracted {
			sim := wineSimilarity(g.WineName, e.Name)
			if sim >= jaccardThreshold {
				candidates = append(candidates, candidate{gi, ei, sim})
			}
		}
	}
	sort.Slice(candidates, func(i, j int) bool {
		return candidates[i].sim > candidates[j].sim
	})

	usedGT := make([]bool, len(gt))
	usedEx := make([]bool, len(extracted))

	var result matchResult
	for _, c := range candidates {
		if usedGT[c.gtIdx] || usedEx[c.exIdx] {
			continue
		}
		usedGT[c.gtIdx] = true
		usedEx[c.exIdx] = true
		result.Matched = append(result.Matched, matchedPair{
			GT:        gt[c.gtIdx],
			Extracted: extracted[c.exIdx],
			Similarity: c.sim,
		})
	}

	for i, g := range gt {
		if !usedGT[i] {
			result.UnmatchedGT = append(result.UnmatchedGT, g)
		}
	}
	for i, e := range extracted {
		if !usedEx[i] {
			result.Unjudged = append(result.Unjudged, e)
		}
	}
	return result
}
