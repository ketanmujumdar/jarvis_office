package fakellm

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

var (
	reUrgent     = regexp.MustCompile(`(?i)\b(urgent(?:ly)?|asap|a\.s\.a\.p\.?|immediately|right away|today|rush)\b`)
	reSentence   = regexp.MustCompile(`[\n;!?]+|\.\s+|\.$`)
	reConj       = regexp.MustCompile(`(?i)\s+(?:and|&|plus|also|as well as)\s+`)
	reLeadDigits = regexp.MustCompile(`^(\d{1,5})\s*(?:x\s+|×\s*)?`)
	reTrailX     = regexp.MustCompile(`\s+[x×]\s*(\d{1,5})$`)
	reSpaces     = regexp.MustCompile(`\s+`)
)

var leadingFiller = []string{
	"hi jarvis", "hey jarvis", "jarvis", "hi", "hey", "hello", "ok", "okay", "so",
	"please", "pls", "can you", "could you", "would you", "can we", "could we",
	"i'd like to", "i would like to", "we'd like to", "i'd like", "i would like", "we'd like",
	"i want to", "i want", "we want", "i need", "we need", "need", "we're out of", "we are out of",
	"out of", "running low on", "running out of", "we're running low on", "we're running out of",
	"low on", "order us", "order me", "order", "get us", "get me", "get", "buy us", "buy", "restock",
	"reorder", "purchase", "grab", "pick up", "send us", "us", "me", "also", "then", "some", "more",
	"extra", "another", "additional", "new",
}

var trailingFiller = []string{
	"please", "pls", "thanks", "thank you", "too", "as well", "again", "for the office",
	"for the pantry", "for the team", "for the kitchen", "for us", "this week", "tomorrow", "now",
}

var numberWords = map[string]int{
	"one": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8,
	"nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13, "fourteen": 14,
	"fifteen": 15, "sixteen": 16, "seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20,
	"thirty": 30, "forty": 40, "fifty": 50, "hundred": 100, "a hundred": 100, "a hundred and": 100,
	"a dozen": 12, "dozen": 12, "a couple of": 2, "a couple": 2, "couple of": 2, "a pair of": 2,
	"a": 1, "an": 1, "single": 1, "a single": 1,
}

// numberWordKeys sorted longest first so "a dozen" wins over "a".
var numberWordKeys = func() []string {
	keys := make([]string, 0, len(numberWords))
	for k := range numberWords {
		keys = append(keys, k)
	}
	// simple insertion sort by length desc, then lexical for determinism
	for i := 1; i < len(keys); i++ {
		for j := i; j > 0 && (len(keys[j]) > len(keys[j-1]) || len(keys[j]) == len(keys[j-1]) && keys[j] < keys[j-1]); j-- {
			keys[j], keys[j-1] = keys[j-1], keys[j]
		}
	}
	return keys
}()

var stopWords = map[string]bool{
	"the": true, "and": true, "for": true, "with": true, "pack": true, "box": true, "set": true,
	"pcs": true, "piece": true, "some": true, "more": true, "our": true, "office": true, "new": true,
	"sheet": true, "unit": true, "per": true, "from": true, "each": true,
}

// ParseUtterance is the deterministic stand-in for LLM line-item extraction. It splits the
// utterance on commas, sentence breaks and conjunctions, reads a leading quantity (digits or
// words, "the usual" = nil), strips filler words and suggests a catalog SKU by name/alias.
// Any urgency word anywhere marks every item urgent.
func ParseUtterance(utterance string, catalog []llm.CatalogHint) []llm.ExtractedItem {
	urgency := domain.UrgencyNormal
	if reUrgent.MatchString(utterance) {
		urgency = domain.UrgencyUrgent
	}
	text := strings.ToLower(utterance)
	text = reSentence.ReplaceAllString(text, ",")
	var segs []string
	for _, part := range strings.Split(text, ",") {
		segs = append(segs, reConj.Split(" "+part+" ", -1)...)
	}
	items := []llm.ExtractedItem{}
	for _, seg := range segs {
		desc, qty := cleanSegment(seg)
		if desc == "" {
			continue
		}
		it := llm.ExtractedItem{Description: desc, Qty: qty, Urgency: urgency}
		it.CatalogSKU = MatchCatalog(desc, catalog)
		items = append(items, it)
	}
	return items
}

func cleanSegment(seg string) (string, *int) {
	s := reUrgent.ReplaceAllString(seg, " ")
	s = strings.Trim(reSpaces.ReplaceAllString(s, " "), " .:-'\"")
	usual := false
	if strings.Contains(s, "the usual") {
		usual = true
		s = strings.ReplaceAll(s, "the usual", " ")
		s = strings.TrimSpace(reSpaces.ReplaceAllString(s, " "))
	}
	s = stripLeading(s, leadingFiller)

	var qty *int
	if m := reLeadDigits.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			qty = &n
		}
		s = strings.TrimSpace(s[len(m[0]):])
	} else if m := reTrailX.FindStringSubmatch(s); m != nil {
		if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
			qty = &n
		}
		s = strings.TrimSpace(s[:len(s)-len(m[0])])
	} else {
		for _, k := range numberWordKeys {
			if s == k || strings.HasPrefix(s, k+" ") {
				n := numberWords[k]
				qty = &n
				s = strings.TrimSpace(s[len(k):])
				break
			}
		}
	}
	s = strings.TrimPrefix(s, "of ")
	s = stripLeading(s, []string{"more", "extra", "additional", "new", "of"})
	for changed := true; changed; {
		changed = false
		for _, f := range trailingFiller {
			if s == f {
				s, changed = "", true
			} else if strings.HasSuffix(s, " "+f) {
				s, changed = strings.TrimSpace(strings.TrimSuffix(s, " "+f)), true
			}
		}
	}
	s = strings.Trim(s, " .:-'\"")
	if usual {
		qty = nil
	}
	// A segment that is only filler ("thanks") has no letters left worth ordering.
	if len(llm.NormalizeText(s)) < 2 {
		return "", nil
	}
	return s, qty
}

func stripLeading(s string, fillers []string) string {
	for changed := true; changed; {
		changed = false
		for _, f := range fillers {
			if s == f {
				return ""
			}
			if strings.HasPrefix(s, f+" ") {
				s = strings.TrimSpace(s[len(f)+1:])
				changed = true
			}
		}
	}
	return s
}

// candidates returns the normalised phrases that identify a catalog item.
func candidates(name, sku string, aliases []string) []string {
	var out []string
	add := func(s string) {
		if n := llm.NormalizeText(s); n != "" {
			out = append(out, n)
		}
	}
	add(name)
	if i := strings.Index(name, "("); i > 0 {
		add(name[:i])
	}
	for _, a := range aliases {
		add(a)
	}
	if sku != "" {
		add(strings.ReplaceAll(sku, "-", " "))
	}
	return out
}

func significant(norm string) []string {
	var out []string
	for _, w := range strings.Fields(norm) {
		if len(w) < 3 || stopWords[w] {
			continue
		}
		if _, err := strconv.Atoi(w); err == nil {
			continue
		}
		out = append(out, w)
	}
	return out
}

// MatchCatalog suggests the SKU whose name or alias best matches the description, or "".
// The longest phrase contained in the description wins; otherwise an item whose phrases contain
// every significant word of the description.
func MatchCatalog(desc string, catalog []llm.CatalogHint) string {
	nd := llm.NormalizeText(desc)
	if nd == "" {
		return ""
	}
	best, bestLen := "", 0
	for _, h := range catalog {
		if ContainsSKU(desc, h.SKU) {
			return h.SKU
		}
		for _, c := range candidates(h.Name, h.SKU, h.Aliases) {
			if ContainsPhraseOrEqual(nd, c) && len(c) > bestLen {
				best, bestLen = h.SKU, len(c)
			}
		}
	}
	if best != "" {
		return best
	}
	words := significant(nd)
	if len(words) == 0 {
		return ""
	}
	for _, h := range catalog {
		for _, c := range candidates(h.Name, h.SKU, h.Aliases) {
			all := true
			for _, w := range words {
				if !llm.ContainsPhrase(c, w) {
					all = false
					break
				}
			}
			if all {
				return h.SKU
			}
		}
	}
	return ""
}

// ContainsSKU reports whether the raw text mentions the SKU verbatim.
func ContainsSKU(text, sku string) bool {
	if sku == "" {
		return false
	}
	return regexp.MustCompile(`(?i)(?:^|[^a-z0-9-])` + regexp.QuoteMeta(sku) + `(?:$|[^a-z0-9-])`).MatchString(text)
}

// ContainsPhraseOrEqual is llm.ContainsPhrase that also accepts equality.
func ContainsPhraseOrEqual(text, phrase string) bool {
	return text == phrase || llm.ContainsPhrase(text, phrase)
}

// RuleMatch is the deterministic stand-in for LLM offer matching: the offer matches when its
// title contains the target name or an alias, or at least half of the target name's significant
// words (and at least one).
func RuleMatch(t llm.MatchTarget, title string) (bool, string) {
	nt := llm.NormalizeText(title)
	for _, c := range candidates(t.Name, "", t.Aliases) {
		if ContainsPhraseOrEqual(nt, c) {
			return true, "title contains \"" + c + "\""
		}
	}
	base := t.Name
	if i := strings.Index(base, "("); i > 0 {
		base = base[:i]
	}
	words := significant(llm.NormalizeText(base))
	if len(words) == 0 {
		return false, "no comparable words"
	}
	hit := 0
	for _, w := range words {
		if llm.ContainsPhrase(nt, w) {
			hit++
		}
	}
	if hit*2 >= len(words) && hit > 0 {
		return true, strconv.Itoa(hit) + "/" + strconv.Itoa(len(words)) + " name words in title"
	}
	return false, "only " + strconv.Itoa(hit) + "/" + strconv.Itoa(len(words)) + " name words in title"
}
