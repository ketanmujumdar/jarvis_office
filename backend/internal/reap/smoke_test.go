//go:build smoke

// Real Reap SG sandbox smoke test. READ-ONLY: it only calls POST /agentic/products/search and
// POST /agentic/products/details (no quotes, checkouts or enrollments).
//
//	go test -tags smoke -run Smoke -v ./internal/reap/
//
// REAP_API_KEY comes from the environment or backend/.env (via config.Load). The key is never printed.
package reap

import (
	"context"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
)

func smokeClient(t *testing.T) *HTTPClient {
	t.Helper()
	cfg, err := config.Load("../../.env")
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	if cfg.ReapAPIKey == "" {
		t.Skip("REAP_API_KEY not set; skipping Reap sandbox smoke test")
	}
	return NewHTTPClient(Options{
		BaseURL: cfg.ReapBaseURL, APIKey: cfg.ReapAPIKey, Version: cfg.ReapVersion,
		Timeout: 30 * time.Second, MaxRetries: 3, RetryBackoff: 500 * time.Millisecond,
	})
}

func TestSmokeSearchSandbox(t *testing.T) {
	c := smokeClient(t)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	tests := []struct {
		name         string
		query        string
		domain       string
		merchantName string // expected products[].merchant.name ("" = any)
	}{
		{"popular paper", "A4 copier paper 80g 500 sheets", "popular.com.sg", "Popular Bookstore"},
		{"common man coffee", "espresso blend coffee beans", "commonmancoffeeroasters.com", "Common Man Coffee Roasters SG"},
		{"ugreen cable", "USB C to USB C cable 100W", "ugreen.com.sg", "UGREEN SG"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.SearchProducts(ctx, SearchRequest{
				Query:              tc.query,
				MerchantPreference: &MerchantPreference{Mode: MerchantOnly, MerchantName: tc.domain},
				Context:            &SearchContext{Country: "SG", Currency: "SGD"},
				Pagination:         &Pagination{Limit: 5},
			})
			if err != nil {
				t.Fatalf("search: %v", err)
			}
			if len(res.Products) == 0 {
				t.Fatalf("no products for %q at %s", tc.query, tc.domain)
			}
			for _, p := range res.Products {
				t.Logf("%s | %s | %.2f %s", p.Merchant.Name, p.Name, p.PriceRange.Min.Amount, p.PriceRange.Min.Currency)
				if p.ID == "" || p.PriceRange.Min.Currency == "" {
					t.Errorf("incomplete product %+v", p)
				}
				if tc.merchantName != "" && p.Merchant.Name != tc.merchantName {
					t.Errorf("merchant.name = %q, want %q (update vendors.yaml reap_merchant_name?)", p.Merchant.Name, tc.merchantName)
				}
			}

			det, err := c.GetProductDetails(ctx, []string{res.Products[0].ID})
			if err != nil {
				t.Fatalf("details: %v", err)
			}
			if len(det.Products) != 1 || det.Products[0].DefaultVariant.ID == "" {
				t.Fatalf("details: %+v", det)
			}
		})
	}

	t.Run("display name is not resolved", func(t *testing.T) {
		_, err := c.SearchProducts(ctx, SearchRequest{Query: "paper",
			MerchantPreference: &MerchantPreference{Mode: MerchantOnly, MerchantName: "Popular Bookstore"}})
		if err == nil {
			t.Log("note: display name resolved this time (probe saw MERCHANT_NOT_RESOLVED)")
			return
		}
		if !IsCode(err, CodeMerchantNotResolved) {
			t.Errorf("err = %v, want %s", err, CodeMerchantNotResolved)
		}
	})
}
