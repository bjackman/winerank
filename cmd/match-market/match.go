package main

import (
	"regexp"
	"strings"
)

// fillerWords are dropped from token sets: they describe the kind of producer
// ("Domaine", "Estate") rather than identifying it, so a match must not hinge on
// them. Mirrors the list in cmd/eval but extended for the multilingual inventory.
var fillerWords = map[string]bool{
	"the": true, "le": true, "la": true, "les": true, "el": true, "los": true,
	"de": true, "del": true, "della": true, "du": true, "des": true, "di": true,
	"da": true, "y": true, "e": true, "et": true, "und": true, "von": true,
	"vino": true, "vin": true, "wine": true, "wines": true, "winery": true,
	"vineyard": true, "vineyards": true, "estate": true, "domaine": true,
	"domaines": true, "chateau": true, "cellar": true, "cellars": true,
	"cantina": true, "tenuta": true, "weingut": true, "bodega": true,
	"bodegas": true, "aoc": true, "doc": true, "docg": true, "cl": true, "ml": true,
}

// countryAliases folds the inventory's localized country names onto the English
// names used in scores.json, so country can serve as a confidence signal.
var countryAliases = map[string]string{
	"schweiz": "switzerland", "suisse": "switzerland", "svizzera": "switzerland",
	"deutschland": "germany", "allemagne": "germany",
	"frankreich": "france",
	"italien":    "italy", "italie": "italy", "italia": "italy",
	"spanien": "spain", "espagne": "spain", "espana": "spain",
	"osterreich": "austria", "autriche": "austria",
	"vereinigte staaten": "usa", "etats unis": "usa", "united states": "usa",
	"neuseeland": "new zealand", "nouvelle zelande": "new zealand",
	"sudafrika": "south africa", "afrique du sud": "south africa",
	"argentinien": "argentina", "australien": "australia", "australie": "australia",
	"griechenland": "greece",
}

var vintageRe = regexp.MustCompile(`\b(1[89]\d\d|20\d\d)\b`)
var leadingVintageRe = regexp.MustCompile(`^\s*(1[89]\d\d|20\d\d)\b`)
var nonAlphanumRe = regexp.MustCompile(`[^a-z0-9]+`)

// foldAccents maps common Latin accented runes to their base letter so that
// e.g. "Viñedos" and "Vinedos" tokenize identically. Stdlib-only (no x/text),
// to keep the module dependency-free (vendorHash = null).
func foldAccents(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case 'á', 'à', 'â', 'ä', 'ã', 'å', 'ā':
			b.WriteRune('a')
		case 'ç', 'č':
			b.WriteRune('c')
		case 'é', 'è', 'ê', 'ë', 'ē':
			b.WriteRune('e')
		case 'í', 'ì', 'î', 'ï', 'ī':
			b.WriteRune('i')
		case 'ñ':
			b.WriteRune('n')
		case 'ó', 'ò', 'ô', 'ö', 'õ', 'ø', 'ō':
			b.WriteRune('o')
		case 'ú', 'ù', 'û', 'ü', 'ū':
			b.WriteRune('u')
		case 'ý', 'ÿ':
			b.WriteRune('y')
		case 'ß':
			b.WriteString("ss")
		case 'æ':
			b.WriteString("ae")
		case 'œ':
			b.WriteString("oe")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// tokenize lowercases, folds accents, and splits into a content-token set,
// dropping filler words and any token equal to the given vintage string.
func tokenize(s, vintage string) map[string]bool {
	clean := nonAlphanumRe.ReplaceAllString(foldAccents(strings.ToLower(s)), " ")
	toks := make(map[string]bool)
	for _, t := range strings.Fields(clean) {
		if t == vintage || fillerWords[t] || len(t) < 3 && !isDigits(t) {
			continue
		}
		toks[t] = true
	}
	return toks
}

func isDigits(s string) bool {
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return s != ""
}

func normCountry(s string) string {
	clean := strings.TrimSpace(nonAlphanumRe.ReplaceAllString(foldAccents(strings.ToLower(s)), " "))
	if a, ok := countryAliases[clean]; ok {
		return a
	}
	return clean
}

// parsedScore is a scores.json name decomposed into its parts. The producer is
// not separated from the cuvee (we don't know the boundary); both live in Middle.
type parsedScore struct {
	vintage string // "" if absent
	middle  map[string]bool
	country string // normalized, "" if absent
}

func parseScoreName(name string) parsedScore {
	vintage := ""
	if m := leadingVintageRe.FindStringSubmatch(name); m != nil {
		vintage = m[1]
	}
	parts := strings.Split(name, ",")
	country := ""
	if len(parts) >= 2 {
		country = normCountry(parts[len(parts)-1])
	}
	// Producer + cuvee live before the first comma.
	return parsedScore{
		vintage: vintage,
		middle:  tokenize(parts[0], vintage),
		country: country,
	}
}

// coverage returns the fraction of needle tokens present in haystack.
func coverage(needle, haystack map[string]bool) float64 {
	if len(needle) == 0 {
		return 0
	}
	hit := 0
	for t := range needle {
		if haystack[t] {
			hit++
		}
	}
	return float64(hit) / float64(len(needle))
}

func intersect(a, b map[string]bool) int {
	n := 0
	for t := range a {
		if b[t] {
			n++
		}
	}
	return n
}

func diff(a, b map[string]bool) map[string]bool {
	out := make(map[string]bool)
	for t := range a {
		if !b[t] {
			out[t] = true
		}
	}
	return out
}
