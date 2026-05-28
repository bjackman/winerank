package main

import (
	"testing"
)

func TestNormalizeWine(t *testing.T) {
	tests := []struct {
		name            string
		input           string
		wantVintage     string
		wantTokenSubset []string
		wantTokenAbsent []string
	}{
		{
			name:            "strips punctuation and filler words",
			input:           "2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA",
			wantVintage:     "2022",
			wantTokenSubset: []string{"daou", "cabernet", "sauvignon", "paso", "robles", "usa"},
			wantTokenAbsent: []string{"2022", "vineyards"},
		},
		{
			name:            "strips estate and chateau",
			input:           "2018 Chateau Le Boscq, Saint-Estephe, France",
			wantVintage:     "2018",
			wantTokenSubset: []string{"le", "boscq", "france"},
			wantTokenAbsent: []string{"chateau", "2018"},
		},
		{
			name:            "no vintage",
			input:           "Daou Cabernet Sauvignon",
			wantVintage:     "",
			wantTokenSubset: []string{"daou", "cabernet", "sauvignon"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := normalizeWine(tc.input)
			if k.vintage != tc.wantVintage {
				t.Errorf("vintage: got %q, want %q", k.vintage, tc.wantVintage)
			}
			for _, tok := range tc.wantTokenSubset {
				if !k.tokens[tok] {
					t.Errorf("expected token %q in set %v", tok, k.tokens)
				}
			}
			for _, tok := range tc.wantTokenAbsent {
				if k.tokens[tok] {
					t.Errorf("unexpected token %q in set %v", tok, k.tokens)
				}
			}
		})
	}
}

func TestWinesMatch(t *testing.T) {
	tests := []struct {
		name      string
		a, b      string
		wantMatch bool
	}{
		{
			name:      "exact same name matches",
			a:         "2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA",
			b:         "2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA",
			wantMatch: true,
		},
		{
			name:      "same wine different punctuation/filler matches",
			a:         "2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA",
			b:         "2022 Daou Cabernet Sauvignon Paso Robles USA",
			wantMatch: true,
		},
		{
			name:      "partial token overlap above threshold matches",
			a:         "2023 Te Mata Estate 'Awatea' Cabernets - Merlot, Hawke's Bay, New Zealand",
			b:         "2023 Te Mata Awatea Cabernets Merlot New Zealand",
			wantMatch: true,
		},
		{
			name:      "vintage mismatch does not match",
			a:         "2022 Daou Vineyards Cabernet Sauvignon",
			b:         "2018 Daou Vineyards Cabernet Sauvignon",
			wantMatch: false,
		},
		{
			name:      "missing vintage on one side still matches",
			a:         "2022 Daou Vineyards Cabernet Sauvignon",
			b:         "Daou Vineyards Cabernet Sauvignon",
			wantMatch: true,
		},
		{
			name:      "completely different wines do not match",
			a:         "2022 Daou Vineyards Cabernet Sauvignon, Paso Robles, USA",
			b:         "2019 VIK Milla Cala, Millahue, Chile",
			wantMatch: false,
		},
		{
			name:      "token overlap too low does not match",
			a:         "2022 Chateau Margaux, Margaux, France",
			b:         "2022 Petrus, Pomerol, France",
			wantMatch: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := winesMatch(tc.a, tc.b)
			if got != tc.wantMatch {
				sim := wineSimilarity(tc.a, tc.b)
				t.Errorf("winesMatch(%q, %q) = %v (similarity=%.3f), want %v",
					tc.a, tc.b, got, sim, tc.wantMatch)
			}
		})
	}
}
