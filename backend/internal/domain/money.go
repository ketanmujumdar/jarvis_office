package domain

import (
	"fmt"
	"math"
	"strconv"
	"strings"
)

// Currency is the only currency the MVP handles.
const Currency = "SGD"

// Cents is an integer amount of SGD cents. All money in our system uses Cents.
type Cents int64

// CentsFromFloat converts a decimal major-unit amount (as returned by Reap, e.g. 35.5) to cents,
// rounding half away from zero.
func CentsFromFloat(amount float64) Cents {
	return Cents(math.Round(amount * 100))
}

// ParseCents parses a decimal string like "12.30", "12.3", "12" or "-1.05" into cents.
func ParseCents(s string) (Cents, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("parse cents: empty")
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0, fmt.Errorf("parse cents %q: %w", s, err)
	}
	return CentsFromFloat(f), nil
}

// Float returns the major-unit amount (for sending to Reap filters and LLM tool output).
func (c Cents) Float() float64 { return float64(c) / 100 }

// String formats as a plain decimal with 2 places, e.g. "35.50".
func (c Cents) String() string {
	sign := ""
	v := int64(c)
	if v < 0 {
		sign, v = "-", -v
	}
	return fmt.Sprintf("%s%d.%02d", sign, v/100, v%100)
}

// SGD formats for humans, e.g. "S$35.50".
func (c Cents) SGD() string {
	if c < 0 {
		return "-S$" + (-c).String()
	}
	return "S$" + c.String()
}

// MulQty returns c * qty.
func (c Cents) MulQty(qty int) Cents { return c * Cents(qty) }
