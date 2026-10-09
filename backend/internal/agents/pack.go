package agents

import (
	"crypto/rand"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Pack-size patterns over normalised-ish titles. Only count units are recognised (not grams/ml),
// so "250g" or "0.7mm" never become a pack size.
var packPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(?:pack|box|set|carton|bundle|case)\s+of\s+(\d{1,4})\b`),
	regexp.MustCompile(`(?i)\b(\d{1,4})\s*(?:'s|’s)`),
	regexp.MustCompile(`(?i)\b(\d{1,4})\s*-?\s*(?:pcs|pc|pieces|piece|pack|pk|packs|count|ct|capsules|capsule|pods|pod|sheets|sheet|rolls|roll|sachets|sachet|bags|tea bags|units|pads|pens|tins|bottles|cans)\b`),
	regexp.MustCompile(`(?i)\bx\s*(\d{1,4})\b`),
	regexp.MustCompile(`(?i)\b(\d{1,4})\s*x\b`),
}

// PackCount extracts a count-pack size from free text ("box of 10 capsules" -> 10, "500's" -> 500,
// "Pack of 12" -> 12). It returns 1 when no count is recognised.
func PackCount(s string) int {
	for _, re := range packPatterns {
		if m := re.FindStringSubmatch(s); m != nil {
			if n, err := strconv.Atoi(m[1]); err == nil && n > 0 {
				return n
			}
		}
	}
	return 1
}

// UnitsPerVariant is how many catalog units one purchasable variant contains. It is the ratio of
// the offer's pack count to the catalog unit's pack count, applied only when the catalog unit states
// a count (e.g. "box of 10 capsules") and the offer pack is a whole multiple of it; otherwise 1.
// Conservative by design: an unrecognised or incompatible pack counts as one unit, so we may
// over-buy but never under-buy.
func UnitsPerVariant(offerTitle, catalogUnit string) int {
	cp := PackCount(catalogUnit)
	if cp <= 1 {
		return 1
	}
	op := PackCount(offerTitle)
	if op < cp || op%cp != 0 {
		return 1
	}
	return op / cp
}

// PurchaseQty is the number of variants to buy so that at least qty catalog units arrive.
func PurchaseQty(qty, packSize int) int {
	if qty < 1 {
		qty = 1
	}
	if packSize <= 1 {
		return qty
	}
	return (qty + packSize - 1) / packSize
}

// cleanQuery trims and collapses whitespace in a search query.
func cleanQuery(q string) string { return strings.Join(strings.Fields(q), " ") }

// newUUID returns a random version 4 UUID.
func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
