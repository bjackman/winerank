package main

import (
	"testing"
)

func gtEntry(name string, score int) GroundTruthEntry {
	return GroundTruthEntry{VideoID: "v1", WineName: name, Score: &score}
}

func exWine(name string, score int) ExtractedWine {
	return ExtractedWine{Name: name, Score: &score}
}

func TestMatchVideo(t *testing.T) {
	t.Run("all matched", func(t *testing.T) {
		gt := []GroundTruthEntry{
			gtEntry("2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA", 91),
			gtEntry("2018 Chateau Le Boscq, Saint-Estephe, France", 91),
		}
		ex := []ExtractedWine{
			exWine("2018 Chateau Le Boscq Saint-Estephe France", 91),
			exWine("2022 Daou Cabernet Sauvignon Paso Robles USA", 91),
		}
		r := matchVideo(gt, ex)
		if len(r.Matched) != 2 {
			t.Errorf("want 2 matched, got %d", len(r.Matched))
		}
		if len(r.UnmatchedGT) != 0 {
			t.Errorf("want 0 unmatched GT, got %d: %v", len(r.UnmatchedGT), r.UnmatchedGT)
		}
		if len(r.Unjudged) != 0 {
			t.Errorf("want 0 unjudged, got %d: %v", len(r.Unjudged), r.Unjudged)
		}
	})

	t.Run("unmatched gt and unjudged extraction", func(t *testing.T) {
		gt := []GroundTruthEntry{
			gtEntry("2022 Daou Vineyards Cabernet Sauvignon", 91),
			gtEntry("2019 VIK Milla Cala, Millahue, Chile", 95),
		}
		ex := []ExtractedWine{
			exWine("2022 Daou Cabernet Sauvignon", 91),
			exWine("2017 Some Completely Different Wine", 88),
		}
		r := matchVideo(gt, ex)
		if len(r.Matched) != 1 {
			t.Errorf("want 1 matched, got %d", len(r.Matched))
		}
		if r.Matched[0].GT.WineName != gt[0].WineName {
			t.Errorf("wrong GT matched: %q", r.Matched[0].GT.WineName)
		}
		if len(r.UnmatchedGT) != 1 || r.UnmatchedGT[0].WineName != gt[1].WineName {
			t.Errorf("want unmatched GT %q, got %v", gt[1].WineName, r.UnmatchedGT)
		}
		if len(r.Unjudged) != 1 || r.Unjudged[0].Name != ex[1].Name {
			t.Errorf("want unjudged %q, got %v", ex[1].Name, r.Unjudged)
		}
	})

	t.Run("greedy picks highest similarity, no double assignment", func(t *testing.T) {
		// Both GT entries are similar to ex[0]; ex[1] only matches gt[1].
		// Greedy should assign ex[0] to whichever GT has higher similarity,
		// leaving the other GT to match ex[1] if possible.
		gt := []GroundTruthEntry{
			gtEntry("2022 Daou Cabernet Sauvignon Paso Robles", 91),
			gtEntry("2022 Daou Cabernet Sauvignon Reserve Paso Robles", 93),
		}
		ex := []ExtractedWine{
			exWine("2022 Daou Cabernet Sauvignon Reserve Paso Robles", 93),
			exWine("2022 Daou Cabernet Sauvignon Paso Robles", 91),
		}
		r := matchVideo(gt, ex)
		if len(r.Matched) != 2 {
			t.Errorf("want 2 matched, got %d; unmatched GT: %v, unjudged: %v",
				len(r.Matched), r.UnmatchedGT, r.Unjudged)
		}
		if len(r.UnmatchedGT) != 0 {
			t.Errorf("want 0 unmatched GT, got %v", r.UnmatchedGT)
		}
	})

	t.Run("empty inputs", func(t *testing.T) {
		r := matchVideo(nil, nil)
		if len(r.Matched) != 0 || len(r.UnmatchedGT) != 0 || len(r.Unjudged) != 0 {
			t.Errorf("expected all empty, got %+v", r)
		}
	})
}
