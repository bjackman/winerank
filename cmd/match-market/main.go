// Command match-market joins rated wines (scores.json) to the local-market
// inventory (wines.json) so the highest-rated wines you can actually buy surface
// at the top. Matching is producer-first: find the inventory producer best
// covered by the scored name, then pick that producer's closest wine, preferring
// the exact vintage and falling back to other vintages (flagged).
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
)

// producerEntry caches a producer's content-token set alongside its wines.
type producerEntry struct {
	tokens map[string]bool
	wines  []inventoryWine
}

func load[T any](path string, v *T) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("reading %s: %w", path, err)
	}
	if err := json.Unmarshal(data, v); err != nil {
		return fmt.Errorf("parsing %s: %w", path, err)
	}
	return nil
}

func main() {
	scoresPath := flag.String("scores", "scores.json", "path to extractor scores")
	winesPath := flag.String("wines", "wines.json", "path to combined merchant inventory")
	outPath := flag.String("output", "matches.json", "path to write detailed matches (JSON)")
	textPath := flag.String("text-output", "matches.txt", "path to write the dense per-wine digest")
	affordPath := flag.String("affordable-output", "affordable.txt", "path to write the full cheapest-per-score view")
	topN := flag.Int("top", 3, "cheapest wines shown per score on stdout")
	minConf := flag.Float64("min-confidence", 0.45, "below this a wine is reported unmatched")
	flag.Parse()

	var scores scoresInput
	var wines []inventoryWine
	if err := load(*scoresPath, &scores); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := load(*winesPath, &wines); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Index inventory into identity groups. Most merchants populate a structured
	// producer field, so we group their wines by producer and identify on it. But
	// ~15% of rows (whole merchants like realwines, bauraulac) leave producer null
	// and embed it in the name instead; for those each wine is its own group,
	// identified by its name tokens. Either way, e.tokens is what must be covered
	// by the scored name for the group to be a candidate.
	var entries []*producerEntry
	byProducer := make(map[string]*producerEntry)
	for i := range wines {
		w := wines[i]
		if w.Producer != "" {
			e := byProducer[w.Producer]
			if e == nil {
				e = &producerEntry{tokens: tokenize(w.Producer, "")}
				byProducer[w.Producer] = e
				entries = append(entries, e)
			}
			e.wines = append(e.wines, w)
		} else {
			entries = append(entries, &producerEntry{
				tokens: tokenize(w.Name, ""),
				wines:  []inventoryWine{w},
			})
		}
	}

	// Document frequency of each token across identity groups. A match carried only
	// by a token shared with many groups ("clos", "san", "reserva") is not evidence;
	// we require at least one distinctive (low-DF) shared token.
	producerDF := make(map[string]int)
	for _, e := range entries {
		for t := range e.tokens {
			producerDF[t]++
		}
	}
	const maxDistinctiveDF = 20

	var out output
	scored, withMatch, totalCand := 0, 0, 0
	for _, r := range scores.Results {
		for _, sw := range r.Wines {
			if sw.Score == nil {
				continue
			}
			scored++
			ps := parseScoreName(sw.Name)
			entry := scoredWine{
				VideoID:       r.VideoID,
				ScoredName:    sw.Name,
				Score:         *sw.Score,
				ScoredVintage: ps.vintage,
			}

			// Gather every candidate producer: one sharing a distinctive token
			// (so a match can't hinge on a word common to many producers) with
			// enough of its tokens covered by the scored name.
			for _, e := range entries {
				if len(e.tokens) == 0 {
					continue
				}
				ov := 0
				distinctive := false
				for t := range e.tokens {
					if ps.middle[t] {
						ov++
						if producerDF[t] <= maxDistinctiveDF {
							distinctive = true
						}
					}
				}
				if ov == 0 || !distinctive {
					continue
				}
				pscore := float64(ov) / float64(len(e.tokens))
				if pscore < 0.5 {
					continue
				}

				// Every wine of a candidate producer is a potential match; score
				// each by how well the leftover cuvee tokens line up and whether
				// the vintage matches the rated one.
				cuvee := diff(ps.middle, e.tokens)
				for i := range e.wines {
					w := &e.wines[i]
					wt := tokenize(w.Name, "")
					cscore := 1.0
					if len(cuvee) > 0 {
						cscore = coverage(cuvee, wt)
					}
					vexact := w.Vintage != nil && ps.vintage != "" && itoa(*w.Vintage) == ps.vintage
					countryOK := ps.country != "" && normCountry(w.Country) == ps.country
					conf := 0.55*pscore + 0.35*cscore
					if vexact {
						conf += 0.10
					}
					if countryOK {
						conf += 0.05
					}
					if conf > 1.0 {
						conf = 1.0
					}
					conf = round3(conf)
					if conf < *minConf {
						continue
					}
					entry.Matches = append(entry.Matches, candidate{
						Confidence:   conf,
						VintageExact: vexact,
						CuveeScore:   round3(cscore),
						Merchant:     w.Merchant,
						Producer:     w.Producer,
						Name:         w.Name,
						Vintage:      w.Vintage,
						Price:        w.Price,
						PricePer750:  pricePer750(w.Price, w.BottleSizeML),
						Currency:     w.Currency,
						InStock:      w.InStock,
						BottleSizeML: w.BottleSizeML,
						URL:          w.URL,
					})
				}
			}

			// Best candidate first within each scored wine.
			sort.SliceStable(entry.Matches, func(i, j int) bool {
				return entry.Matches[i].Confidence > entry.Matches[j].Confidence
			})
			if len(entry.Matches) > 0 {
				withMatch++
				totalCand += len(entry.Matches)
			}
			out.Wines = append(out.Wines, entry)
		}
	}

	// Highest-rated wine first; break ties by best available match.
	sort.SliceStable(out.Wines, func(i, j int) bool {
		if out.Wines[i].Score != out.Wines[j].Score {
			return out.Wines[i].Score > out.Wines[j].Score
		}
		return bestConf(out.Wines[i]) > bestConf(out.Wines[j])
	})

	data, err := json.MarshalIndent(out, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	if err := os.WriteFile(*outPath, data, 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Full per-wine digest to its file (file written before any stdout so a broken
	// stdout pipe can't lose it).
	if err := writeFile(*textPath, func(w io.Writer) { renderDigest(w, out, 0) }); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Affordability view: cheapest wines per score. Full version to its file, the
	// top-N-per-score headline to stdout.
	afford := affordable(out)
	if err := writeFile(*affordPath, func(w io.Writer) { renderAffordable(w, afford, 0) }); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
	renderAffordable(os.Stdout, afford, *topN)

	fmt.Fprintf(os.Stderr, "scored wines: %d\n", scored)
	fmt.Fprintf(os.Stderr, "with >=1 match (conf >= %.2f): %d\n", *minConf, withMatch)
	fmt.Fprintf(os.Stderr, "total candidate matches: %d\n", totalCand)
	fmt.Fprintf(os.Stderr, "-> %s, %s, %s\n", *outPath, *textPath, *affordPath)
}

func writeFile(path string, render func(io.Writer)) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	render(f)
	return nil
}

// renderDigest writes a dense, skim-friendly view: each scored wine (already
// sorted highest-rated first) as a header "<name> (<score>) (<video URL>):"
// followed by its matches as "merchant - url - vintage". When limit > 0, at most
// that many matches are shown per wine with a trailing "..." if more exist;
// limit <= 0 shows all. Only wines with a match are listed.
func renderDigest(w io.Writer, out output, limit int) {
	for _, sw := range out.Wines {
		if len(sw.Matches) == 0 {
			continue
		}
		fmt.Fprintf(w, "%s (%d) (https://www.youtube.com/watch?v=%s):\n", sw.ScoredName, sw.Score, sw.VideoID)
		for i, m := range sw.Matches {
			if limit > 0 && i >= limit {
				fmt.Fprintln(w, "  ...")
				break
			}
			fmt.Fprintf(w, "  %s - %s - %s\n", m.Merchant, m.URL, vintageStr(m.Vintage))
		}
		fmt.Fprintln(w)
	}
}

// affordableWine is a scored wine paired with its cheapest available bottle,
// plus the video it was rated in.
type affordableWine struct {
	scoredName string
	videoID    string
	chosen     candidate
}

// scoreGroup holds the wines available at one score, cheapest (per-750ml) first.
type scoreGroup struct {
	score int
	wines []affordableWine
}

// affordable reduces each scored wine to its cheapest available bottle, then
// groups by score (deduping wines that recur across videos) so each score lists
// the cheapest ways to drink at that quality.
func affordable(out output) []scoreGroup {
	byScore := make(map[int][]affordableWine)
	seen := make(map[string]bool) // score+name, to drop cross-video duplicates
	for _, sw := range out.Wines {
		m := cheapestAvailable(sw.Matches)
		if m == nil {
			continue
		}
		key := fmt.Sprintf("%d|%s", sw.Score, sw.ScoredName)
		if seen[key] {
			continue
		}
		seen[key] = true
		byScore[sw.Score] = append(byScore[sw.Score], affordableWine{sw.ScoredName, sw.VideoID, *m})
	}

	groups := make([]scoreGroup, 0, len(byScore))
	for score, wines := range byScore {
		sort.SliceStable(wines, func(i, j int) bool {
			return *wines[i].chosen.PricePer750 < *wines[j].chosen.PricePer750
		})
		groups = append(groups, scoreGroup{score, wines})
	}
	sort.SliceStable(groups, func(i, j int) bool { return groups[i].score > groups[j].score })
	return groups
}

// cheapestAvailable returns the cheapest in-stock, priced bottle (by per-750ml
// price), preferring an exact-vintage match and falling back to other vintages.
// Returns nil if no available bottle exists.
func cheapestAvailable(matches []candidate) *candidate {
	var bestExact, bestOther *candidate
	for i := range matches {
		m := &matches[i]
		if m.Price == nil || m.PricePer750 == nil {
			continue
		}
		if m.InStock != nil && !*m.InStock {
			continue
		}
		if m.VintageExact {
			if bestExact == nil || *m.PricePer750 < *bestExact.PricePer750 {
				bestExact = m
			}
		} else if bestOther == nil || *m.PricePer750 < *bestOther.PricePer750 {
			bestOther = m
		}
	}
	if bestExact != nil {
		return bestExact
	}
	return bestOther
}

// renderAffordable writes the cheapest-per-score view. When topN > 0 each score
// shows at most that many wines with a trailing "..." if more exist.
func renderAffordable(w io.Writer, groups []scoreGroup, topN int) {
	for _, g := range groups {
		fmt.Fprintf(w, "%d:\n", g.score)
		for i, aw := range g.wines {
			if topN > 0 && i >= topN {
				fmt.Fprintln(w, "  ...")
				break
			}
			m := aw.chosen
			fmt.Fprintf(w, "  %s - %s %s (%s, %s%s)%s — rated: https://www.youtube.com/watch?v=%s\n",
				aw.scoredName, moneyStr(*m.Price), m.Currency, m.Merchant,
				vintageNote(m), sizeNote(m), urlSuffix(m.URL), aw.videoID)
		}
		fmt.Fprintln(w)
	}
}

func vintageNote(m candidate) string {
	if m.VintageExact {
		return vintageStr(m.Vintage)
	}
	return vintageStr(m.Vintage) + " — diff vintage"
}

func sizeNote(m candidate) string {
	if m.BottleSizeML == nil {
		return ", 750ml?" // unknown size, treated as 750ml for ranking
	}
	s := fmt.Sprintf(", %dml", *m.BottleSizeML)
	if *m.BottleSizeML != 750 && m.PricePer750 != nil {
		s += fmt.Sprintf(", ~%s %s/750ml", moneyStr(*m.PricePer750), m.Currency)
	}
	return s
}

func urlSuffix(u string) string {
	if u == "" {
		return ""
	}
	return " " + u
}

// pricePer750 normalizes a price (in cents) to a 750ml-equivalent, treating an
// unknown bottle size as a standard 750ml bottle.
func pricePer750(price, sizeML *int) *int {
	if price == nil {
		return nil
	}
	size := 750
	if sizeML != nil && *sizeML > 0 {
		size = *sizeML
	}
	v := *price * 750 / size
	return &v
}

// moneyStr formats integer cents as francs, dropping the decimals when whole.
func moneyStr(cents int) string {
	if cents%100 == 0 {
		return itoa(cents / 100)
	}
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

func vintageStr(v *int) string {
	if v == nil {
		return "?"
	}
	return itoa(*v)
}

func bestConf(s scoredWine) float64 {
	if len(s.Matches) == 0 {
		return -1
	}
	return s.Matches[0].Confidence
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

func round3(f float64) float64 {
	return float64(int(f*1000+0.5)) / 1000
}
