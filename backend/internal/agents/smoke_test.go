//go:build smoke

package agents

import (
	"context"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Read-only smoke test against the real Reap SG sandbox: the adapter's per-vendor ONLY search and
// the open allow-listed search return attributed SGD offers. Run with `make smoke` (needs
// backend/.env). Never prints secrets.
func TestSmoke_ReapAdapterSearch(t *testing.T) {
	cfg, err := config.Load("../../.env")
	if err != nil || cfg.ReapAPIKey == "" {
		t.Skip("REAP_API_KEY not configured")
	}
	client := reap.NewHTTPClient(reap.Options{BaseURL: cfg.ReapBaseURL, APIKey: cfg.ReapAPIKey, Version: cfg.ReapVersion, Timeout: 30 * time.Second})
	a := NewReapAdapter(client)
	st := seeded(t)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	pop := vendorByDomain(t, st, "popular.com.sg")
	offers, err := a.Search(ctx, SearchQuery{Query: "A4 copier paper 80g 500 sheets", Vendor: pop, Limit: 3, CatalogUnit: "ream (500 sheets)"})
	if err != nil {
		t.Fatalf("vendor search: %v", err)
	}
	if len(offers) == 0 {
		t.Fatal("no offers from popular.com.sg")
	}
	for _, o := range offers {
		if o.MerchantName != pop.ReapMerchantName || o.ReapVariantID == "" || o.UnitPriceCents <= 0 {
			t.Fatalf("offer = %+v", o)
		}
		t.Logf("popular: %s @ %s (pack %d)", o.Title, o.UnitPriceCents.SGD(), o.PackSize)
	}

	allowed, err := st.Vendors().List(ctx, store.VendorFilter{AllowedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	open, err := a.Search(ctx, SearchQuery{Query: "coffee capsules", Open: true, Allowed: allowed, Limit: 5})
	if err != nil {
		t.Fatalf("open search: %v", err)
	}
	for _, o := range open {
		t.Logf("open: %s / %s @ %s", o.MerchantName, o.Title, o.UnitPriceCents.SGD())
	}
}
