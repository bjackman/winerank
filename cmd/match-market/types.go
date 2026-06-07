package main

// scoresInput mirrors the relevant subset of scores.json (extract-scores output).
type scoresInput struct {
	Results []struct {
		VideoID string `json:"video_id"`
		Wines   []struct {
			Name  string `json:"name"`
			Score *int   `json:"score"` // nil if discussed but not scored
		} `json:"wines"`
	} `json:"results"`
}

// inventoryWine is one row of wines.json (combined merchant inventory). Producer
// is a separate field here (unlike scores.json, where it's embedded in Name),
// and Country is localized (e.g. "Schweiz", "Frankreich").
type inventoryWine struct {
	Merchant     string `json:"merchant"`
	Name         string `json:"name"`
	Producer     string `json:"producer"`
	Vintage      *int   `json:"vintage"`
	Price        *int   `json:"price"`
	Currency     string `json:"currency"`
	InStock      *bool  `json:"in_stock"`
	BottleSizeML *int   `json:"bottle_size_ml"`
	Country      string `json:"country"`
	URL          string `json:"url"`
}

// candidate is one bottle on sale that might be the scored wine. PricePer750 is
// Price normalized to a 750ml-equivalent (null bottle size treated as 750ml), so
// magnums and half-bottles can be compared on affordability.
type candidate struct {
	Confidence   float64 `json:"confidence"`
	VintageExact bool    `json:"vintage_exact"`
	CuveeScore   float64 `json:"cuvee_score"`
	Merchant     string  `json:"merchant"`
	Producer     string  `json:"producer"`
	Name         string  `json:"name"`
	Vintage      *int    `json:"vintage"`
	Price        *int    `json:"price"`
	PricePer750  *int    `json:"price_per_750"`
	Currency     string  `json:"currency"`
	InStock      *bool   `json:"in_stock"`
	BottleSizeML *int    `json:"bottle_size_ml"`
	URL          string  `json:"url"`
}

// scoredWine is one rated wine and every potential market match found for it.
type scoredWine struct {
	VideoID       string      `json:"video_id"`
	ScoredName    string      `json:"scored_name"`
	Score         int         `json:"score"`
	ScoredVintage string      `json:"scored_vintage,omitempty"`
	Matches       []candidate `json:"matches"`
}

type output struct {
	Wines []scoredWine `json:"wines"`
}
