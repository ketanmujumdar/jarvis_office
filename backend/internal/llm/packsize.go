package llm

import (
	"regexp"
	"strconv"
	"strings"
)

// Deterministic text helpers shared by the rule-based fake and by the real matcher (as a hint and
// as a sanity check on the model's pack size).

var (
	reDimension = regexp.MustCompile(`\d+(?:\.\d+)?\s*[x×*]\s*\d+(?:\.\d+)?(?:\s*(?:mm|cm|m|in|inch|"))?`)
	reNumber    = regexp.MustCompile(`\d+`)
	rePackOf    = regexp.MustCompile(`\b(?:pack|box|set|bag|carton|case|tub|tin|bundle|pkt|packet)\s+of\s+(\d+)\b`)
	reCountWord = regexp.MustCompile(`\b(\d+)\s*(?:pcs|pc|pieces|piece|pack|pk|pkt|ct|count|units|unit|'s|’s|s)\b`)
	reTimes     = regexp.MustCompile(`(?:^|[\s(])[x×]\s*(\d+)\b|\b(\d+)\s*[x×](?:\s|$|\))`)
	reWord      = regexp.MustCompile(`[a-z0-9]+`)
)

// NormalizeText lowercases, keeps letters/digits, and singularises simple plurals, joining the
// words with single spaces. "Ballpoint Pens, 0.7mm!" -> "ballpoint pen 0 7mm".
func NormalizeText(s string) string {
	words := reWord.FindAllString(strings.ToLower(s), -1)
	for i, w := range words {
		words[i] = Singular(w)
	}
	return strings.Join(words, " ")
}

// Singular strips a simple English plural suffix.
func Singular(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && (strings.HasSuffix(w, "ches") || strings.HasSuffix(w, "shes") || strings.HasSuffix(w, "xes") || strings.HasSuffix(w, "sses")):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") && !strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us"):
		return w[:len(w)-1]
	}
	return w
}

// ContainsPhrase reports whether the normalised phrase occurs in the normalised text on word
// boundaries.
func ContainsPhrase(normText, normPhrase string) bool {
	if normPhrase == "" {
		return false
	}
	return strings.Contains(" "+normText+" ", " "+normPhrase+" ")
}

// GuessPackSize estimates how many catalog units one purchasable variant contains, from the offer
// title and the catalog unit description (e.g. unit "pen" and title "Pilot pen pack of 12" -> 12;
// unit "ream (500 sheets)" and title "Copier paper 500's" -> 1; unit "ream" and "5 reams" -> 5).
// It returns 1 when nothing reliable is found.
func GuessPackSize(title, unit string) int {
	t := strings.ToLower(title)
	t = reDimension.ReplaceAllString(t, " ")
	u := strings.ToLower(unit)
	unitNoun, nounIdx, numIdx := "", -1, -1
	ws := reWord.FindAllString(u, -1)
	for i, w := range ws {
		if _, err := strconv.Atoi(w); err == nil {
			if numIdx < 0 {
				numIdx = i
			}
			continue
		}
		if unitNoun == "" && w != "of" && w != "pack" && w != "box" && w != "set" {
			unitNoun, nounIdx = Singular(w), i
		}
	}
	unitCount := 0
	if m := reNumber.FindString(u); m != "" {
		unitCount, _ = strconv.Atoi(m)
	}
	// counted: the unit's number counts the noun ("pack of 5 pads"), so 10 pads = 2 units.
	counted := unitCount > 0 && numIdx >= 0 && numIdx < nounIdx

	// 1. An explicit count of the unit noun itself: "5 reams" for unit "ream".
	if unitNoun != "" {
		re := regexp.MustCompile(`\b(\d+)\s*` + regexp.QuoteMeta(unitNoun) + `(?:s|es)?\b`)
		if m := re.FindStringSubmatch(t); m != nil {
			n, _ := strconv.Atoi(m[1])
			switch {
			case n <= 0 || n > 10000:
			case counted && n%unitCount == 0:
				return n / unitCount
			case counted:
				return 1
			case n <= 1000:
				return n
			}
		}
	}

	// 2. A generic count: "pack of 12", "12 pcs", "x12", "500's".
	count := 0
	for _, re := range []*regexp.Regexp{rePackOf, reTimes, reCountWord} {
		if m := re.FindStringSubmatch(t); m != nil {
			for _, g := range m[1:] {
				if g != "" {
					count, _ = strconv.Atoi(g)
					break
				}
			}
			if count > 0 {
				break
			}
		}
	}
	if count <= 0 || count > 10000 {
		return 1
	}
	if unitCount > 0 {
		if count == unitCount {
			return 1
		}
		if count > unitCount && count%unitCount == 0 {
			return count / unitCount
		}
		return 1
	}
	if count > 1000 {
		return 1
	}
	return count
}
