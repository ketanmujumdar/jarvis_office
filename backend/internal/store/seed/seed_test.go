package seed

import (
	"path/filepath"
	"runtime"
	"testing"
)

func seedDir(t *testing.T) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "seed")
}

// TestParseRepoSeed guards the committed seed files: they must parse and be referentially valid.
func TestParseRepoSeed(t *testing.T) {
	d, err := Parse(seedDir(t))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Items) < 30 {
		t.Errorf("want ~30 catalog items, got %d", len(d.Items))
	}
	if len(d.Addresses) < 3 || len(d.Addresses) > 5 {
		t.Errorf("want 3-5 addresses, got %d", len(d.Addresses))
	}
	if len(d.Vendors) == 0 || len(d.Users) == 0 {
		t.Error("vendors/users empty")
	}
	if d.Policy.PriceDriftPct != 5 {
		t.Errorf("price_drift_pct = %v", d.Policy.PriceDriftPct)
	}
}

func TestValidateRejectsBadData(t *testing.T) {
	good, err := Parse(seedDir(t))
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		mutate func(d *Data)
	}{
		{"unknown vendor", func(d *Data) { d.Items[0].PreferredVendors = []string{"nope.example"} }},
		{"duplicate sku", func(d *Data) { d.Items[1].SKU = d.Items[0].SKU }},
		{"two defaults", func(d *Data) { d.Addresses[1].IsDefault = true }},
		{"zero limit", func(d *Data) { d.Policy.PerOrderLimitSGD = 0 }},
		{"empty prompt", func(d *Data) { d.SystemPrompt = "" }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := good
			d.Items = append([]CatalogSeed(nil), good.Items...)
			d.Addresses = append([]AddressSeed(nil), good.Addresses...)
			tt.mutate(&d)
			if d.Validate() == nil {
				t.Fatal("expected validation error")
			}
		})
	}
}
