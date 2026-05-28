package main

import (
	"regexp"
	"strings"
)

var fillerWords = map[string]bool{
	"the":      true,
	"vineyard": true, "vineyards": true,
	"estate": true,
	"chateau": true,
	"winery": true,
	"cellar": true, "cellars": true,
	"domaine": true, "domaines": true,
}

var vintageRe = regexp.MustCompile(`\b(1[89]\d\d|20\d\d)\b`)
var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

type wineKey struct {
	tokens  map[string]bool
	vintage string // empty if not found
}

func normalizeWine(name string) wineKey {
	lower := strings.ToLower(name)

	vintage := ""
	if m := vintageRe.FindString(lower); m != "" {
		vintage = m
	}

	clean := nonAlphanumRe.ReplaceAllString(lower, " ")
	clean = strings.TrimSpace(clean)

	tokens := make(map[string]bool)
	for _, tok := range strings.Fields(clean) {
		if tok == vintage || fillerWords[tok] {
			continue
		}
		tokens[tok] = true
	}
	return wineKey{tokens: tokens, vintage: vintage}
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 1.0
	}
	intersect := 0
	for tok := range a {
		if b[tok] {
			intersect++
		}
	}
	union := len(a) + len(b) - intersect
	if union == 0 {
		return 0.0
	}
	return float64(intersect) / float64(union)
}

const jaccardThreshold = 0.5

// wineSimilarity returns the Jaccard similarity between two wine name token
// sets, or -1 if both names carry a vintage and they disagree.
func wineSimilarity(a, b string) float64 {
	ka, kb := normalizeWine(a), normalizeWine(b)
	if ka.vintage != "" && kb.vintage != "" && ka.vintage != kb.vintage {
		return -1
	}
	return jaccard(ka.tokens, kb.tokens)
}

// winesMatch reports whether two wine names refer to the same wine.
func winesMatch(a, b string) bool {
	return wineSimilarity(a, b) >= jaccardThreshold
}
