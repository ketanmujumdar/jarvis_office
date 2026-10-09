package allowlist

import (
	"os"
	"strings"
	"testing"
)

func TestDefault_MatchesSeedFile(t *testing.T) {
	raw, err := os.ReadFile("../../seed/allowed_merchants.tsv")
	if err != nil {
		t.Fatal(err)
	}
	rows := 0
	for _, l := range strings.Split(strings.TrimSpace(string(raw)), "\n") {
		if strings.TrimSpace(l) != "" {
			rows++
		}
	}
	if got := Default().Len(); got != rows-1 || got == 0 {
		t.Fatalf("Default has %d domains, file has %d data rows", got, rows-1)
	}
}

func TestContains(t *testing.T) {
	s := Parse("domain\tcategory\tcountry\nalchemist.com.sg\tCoffee\tSG\n\n# comment\nAnker.com.sg\tTech\tSG\n")
	cases := []struct {
		in   string
		want bool
	}{
		{"alchemist.com.sg", true},
		{"https://www.Alchemist.com.sg/", true},
		{"anker.com.sg", true},
		{"domain", false},
		{"shopmustafa.com.sg", false},
		{"", false},
		{"alchemist.com", false},
	}
	for _, tc := range cases {
		if got := s.Contains(tc.in); got != tc.want {
			t.Errorf("Contains(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
	if (Set{}).Contains("alchemist.com.sg") {
		t.Fatal("zero Set must allow nothing")
	}
}
