package agents

import (
	"strings"
	"unicode"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// MatchKind says how a requested item was matched to the catalog.
type MatchKind string

const (
	MatchSKU     MatchKind = "sku"      // description (or LLM suggestion) equals a SKU and is plausible
	MatchExact   MatchKind = "exact"    // normalised description equals the name or an alias
	MatchFuzzy   MatchKind = "fuzzy"    // token similarity above FuzzyThreshold
	MatchOffList MatchKind = "off_list" // no catalog item
)

// FuzzyThreshold is the minimum similarity for a fuzzy catalog match.
const FuzzyThreshold = 0.6

// suggestionThreshold is the minimum similarity for accepting the LLM's catalog_sku suggestion.
// The model proposes; the matcher decides.
const suggestionThreshold = 0.5

// Match is the deterministic catalog-matching result for one requested item.
type Match struct {
	Item  *domain.CatalogItem
	Kind  MatchKind
	Score float64
}

// MatchCatalog matches a description to the catalog: exact SKU, then exact name/alias, then fuzzy
// name/alias, then the LLM's SKU suggestion if it is plausible, else off-list. Inactive items are
// ignored. Pure and deterministic (ties go to the first catalog item in slice order).
func MatchCatalog(description, suggestedSKU string, catalog []domain.CatalogItem) Match {
	desc := normalize(description)
	if desc == "" {
		return Match{Kind: MatchOffList}
	}
	active := make([]*domain.CatalogItem, 0, len(catalog))
	for i := range catalog {
		if catalog[i].Active {
			active = append(active, &catalog[i])
		}
	}
	for _, it := range active {
		if strings.EqualFold(strings.TrimSpace(description), it.SKU) {
			return Match{Item: it, Kind: MatchSKU, Score: 1}
		}
	}
	for _, it := range active {
		for _, n := range names(it) {
			if normalize(n) == desc {
				return Match{Item: it, Kind: MatchExact, Score: 1}
			}
		}
	}
	var best *domain.CatalogItem
	bestScore := 0.0
	for _, it := range active {
		if s := itemScore(desc, it); s > bestScore {
			best, bestScore = it, s
		}
	}
	if best != nil && bestScore >= FuzzyThreshold {
		return Match{Item: best, Kind: MatchFuzzy, Score: bestScore}
	}
	if suggestedSKU != "" {
		for _, it := range active {
			if it.SKU == suggestedSKU {
				if s := itemScore(desc, it); s >= suggestionThreshold {
					return Match{Item: it, Kind: MatchSKU, Score: s}
				}
			}
		}
	}
	return Match{Kind: MatchOffList, Score: bestScore}
}

func names(it *domain.CatalogItem) []string {
	return append([]string{it.Name, it.SKU}, it.Aliases...)
}

// itemScore is the best similarity of desc against the item's name, SKU and aliases.
func itemScore(desc string, it *domain.CatalogItem) float64 {
	best := 0.0
	for _, n := range names(it) {
		if s := similarity(desc, normalize(n)); s > best {
			best = s
		}
	}
	return best
}

// similarity of two normalised strings: if every token of the (multi-token) candidate appears in
// the description, it is a containment match (0.9); otherwise the Jaccard index of the token sets.
func similarity(desc, cand string) float64 {
	if desc == "" || cand == "" {
		return 0
	}
	dt, ct := tokenSet(desc), tokenSet(cand)
	if len(ct) >= 2 || len(dt) == 1 {
		contained := true
		for t := range ct {
			if !dt[t] {
				contained = false
				break
			}
		}
		if contained {
			return 0.9
		}
	}
	inter := 0
	for t := range dt {
		if ct[t] {
			inter++
		}
	}
	union := len(dt) + len(ct) - inter
	if union == 0 {
		return 0
	}
	return float64(inter) / float64(union)
}

var stopwords = map[string]bool{
	"a": true, "an": true, "the": true, "of": true, "for": true, "and": true, "some": true,
	"more": true, "our": true, "us": true, "usual": true, "please": true, "new": true, "x": true,
	"with": true, "box": false,
}

func tokenSet(s string) map[string]bool {
	out := map[string]bool{}
	for _, t := range strings.Fields(s) {
		if stopwords[t] {
			continue
		}
		out[stem(t)] = true
	}
	return out
}

// stem strips simple English plurals so "pods" matches "pod".
func stem(t string) string {
	switch {
	case len(t) > 4 && strings.HasSuffix(t, "ies"):
		return t[:len(t)-3] + "y"
	case len(t) > 3 && strings.HasSuffix(t, "es") && (strings.HasSuffix(t, "shes") || strings.HasSuffix(t, "ches") || strings.HasSuffix(t, "xes")):
		return t[:len(t)-2]
	case len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss"):
		return t[:len(t)-1]
	}
	return t
}

// normalize lowercases, maps punctuation to spaces and collapses whitespace.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		} else {
			b.WriteRune(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// Tokens returns the normalised, stemmed token set of s (stopwords removed). Exposed for the
// orchestrator's search planner.
func Tokens(s string) map[string]bool { return tokenSet(normalize(s)) }
