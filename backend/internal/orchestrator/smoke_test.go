//go:build smoke

// Real Reap SG sandbox smoke test for the quote step. It creates QUOTES only (no checkout, no
// enrollment, no money movement) for every seeded delivery address, using the same
// shippingAddress mapping and contact email the checkout path sends.
//
//	go test -tags smoke -run Smoke -v ./internal/orchestrator/
package orchestrator

import (
	"context"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func TestSmokeQuoteSeedAddresses(t *testing.T) {
	cfg, err := config.Load("../../.env")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.ReapAPIKey == "" {
		t.Skip("REAP_API_KEY not set")
	}
	c := reap.NewHTTPClient(reap.Options{
		BaseURL: cfg.ReapBaseURL, APIKey: cfg.ReapAPIKey, Version: cfg.ReapVersion,
		Timeout: 30 * time.Second, MaxRetries: 3, RetryBackoff: 500 * time.Millisecond,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	st := memstore.New()
	if err := seed.Load(ctx, st, seedDir, seed.Options{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	addrs, err := st.Addresses().List(ctx)
	if err != nil || len(addrs) == 0 {
		t.Fatalf("addresses: %v (%d)", err, len(addrs))
	}

	res, err := c.SearchProducts(ctx, reap.SearchRequest{
		Query:              "A4 copier paper 80g 500 sheets",
		MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "popular.com.sg"},
		Context:            &reap.SearchContext{Country: "SG", Currency: "SGD"},
		Pagination:         &reap.Pagination{Limit: 3},
	})
	if err != nil || len(res.Products) == 0 {
		t.Fatalf("search: %v (%d products)", err, len(res.Products))
	}
	det, err := c.GetProductDetails(ctx, []string{res.Products[0].ID})
	if err != nil || len(det.Products) != 1 || det.Products[0].DefaultVariant.ID == "" {
		t.Fatalf("details: %v %+v", err, det)
	}
	variant := det.Products[0].DefaultVariant.ID

	for _, a := range addrs {
		t.Run(a.Label, func(t *testing.T) {
			q, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{
				Email: a.Email, Items: []reap.QuoteItem{{VariantID: variant, Quantity: 1}}, ShippingAddress: shippingAddress(a),
			})
			if err != nil {
				t.Fatalf("quote for %s (%s): %v", a.Label, a.PostalCode, err)
			}
			fa := q.AmountBreakdown.FinalAmount
			t.Logf("quote %s final %.2f %s, %d shipping options", q.ID, fa.Amount, fa.Currency, len(q.ShippingOptions))
			if q.ID == "" || fa.Currency != "SGD" || fa.Amount <= 0 {
				t.Fatalf("unexpected quote %+v", q)
			}
		})
	}
}
