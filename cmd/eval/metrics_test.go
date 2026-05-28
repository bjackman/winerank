package main

import (
	"strings"
	"testing"
)

func intPtr(n int) *int { return &n }

func TestComputeMetrics(t *testing.T) {
	t.Run("perfect match", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
			{VideoID: "v1", WineName: "2018 Chateau Le Boscq", Score: intPtr(91)},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{{
			VideoID: "v1",
			Wines: []ExtractedWine{
				{Name: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
				{Name: "2018 Chateau Le Boscq", Score: intPtr(91)},
			},
		}}}
		m := computeMetrics(gt, output)
		if m.GTCount != 2 || m.MatchedCount != 2 || m.ScoreMatchCount != 2 || m.UnjudgedCount != 0 {
			t.Errorf("unexpected metrics: %+v", m)
		}
	})

	t.Run("score mismatch counted separately from recall", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{{
			VideoID: "v1",
			Wines:   []ExtractedWine{{Name: "2022 Daou Cabernet Sauvignon", Score: intPtr(90)}},
		}}}
		m := computeMetrics(gt, output)
		if m.MatchedCount != 1 {
			t.Errorf("want 1 matched, got %d", m.MatchedCount)
		}
		if m.ScoreMatchCount != 0 {
			t.Errorf("want 0 score matches, got %d", m.ScoreMatchCount)
		}
	})

	t.Run("unmatched gt and unjudged extraction", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
			{VideoID: "v1", WineName: "2019 VIK Milla Cala Millahue Chile", Score: intPtr(95)},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{{
			VideoID: "v1",
			Wines: []ExtractedWine{
				{Name: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
				{Name: "2017 Completely Different Wine", Score: intPtr(88)},
			},
		}}}
		m := computeMetrics(gt, output)
		if m.GTCount != 2 || m.MatchedCount != 1 || m.UnjudgedCount != 1 {
			t.Errorf("unexpected metrics: %+v", m)
		}
	})

	t.Run("unjudged wines in videos without any gt", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{
			{VideoID: "v1", Wines: []ExtractedWine{
				{Name: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
			}},
			{VideoID: "v2", Wines: []ExtractedWine{
				{Name: "2020 Some Other Wine", Score: intPtr(88)},
				{Name: "2021 Another Wine", Score: intPtr(90)},
			}},
		}}
		m := computeMetrics(gt, output)
		if m.UnjudgedCount != 2 {
			t.Errorf("want 2 unjudged (from no-GT video), got %d", m.UnjudgedCount)
		}
	})

	t.Run("extractor output missing a gt video", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: intPtr(91)},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{}} // empty
		m := computeMetrics(gt, output)
		if m.GTCount != 1 || m.MatchedCount != 0 {
			t.Errorf("unexpected metrics: %+v", m)
		}
	})

	t.Run("null scores both sides count as match", func(t *testing.T) {
		gt := GroundTruth{Entries: []GroundTruthEntry{
			{VideoID: "v1", WineName: "2022 Daou Cabernet Sauvignon", Score: nil},
		}}
		output := ExtractorOutput{Results: []ExtractedVideo{{
			VideoID: "v1",
			Wines:   []ExtractedWine{{Name: "2022 Daou Cabernet Sauvignon", Score: nil}},
		}}}
		m := computeMetrics(gt, output)
		if m.ScoreMatchCount != 1 {
			t.Errorf("want null==null as score match, got ScoreMatchCount=%d", m.ScoreMatchCount)
		}
	})
}

func TestMetricsSummary(t *testing.T) {
	m := metrics{GTCount: 7, MatchedCount: 6, ScoreMatchCount: 5, UnjudgedCount: 3}
	s := m.Summary()
	for _, want := range []string{"6/7", "85.7%", "5/6", "83.3%", "3"} {
		if !strings.Contains(s, want) {
			t.Errorf("summary %q missing %q", s, want)
		}
	}
}
