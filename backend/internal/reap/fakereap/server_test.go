package fakereap_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/reaptest"
)

const returnURL = "https://jarvis.example/reap-return"

var ctx = context.Background()

func sgAddress() *reap.ShippingAddress {
	return &reap.ShippingAddress{
		FirstName: "Maya", LastName: "Tan", Phone: "+6591234567",
		AddressLine1: "1 Raffles Place", AddressLine2: "#20-01 One Raffles Place Tower 1",
		City: "Singapore", PostalCode: "048616", Country: "SG",
	}
}

// fixedClock returns a controllable clock.
func fixedClock() func() time.Time {
	t0 := time.Date(2026, 10, 9, 9, 0, 0, 0, time.UTC)
	return func() time.Time { return t0 }
}

func newFake(t *testing.T, o fakereap.Options) (*fakereap.Server, *reap.HTTPClient) {
	t.Helper()
	if o.Clock == nil {
		o.Clock = fixedClock()
	}
	s := fakereap.New(o)
	t.Cleanup(s.Close)
	return s, s.Client()
}

func activeEnrollment(t *testing.T, s *fakereap.Server, c reap.Client) string {
	t.Helper()
	e, err := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), reap.CreateEnrollmentRequest{
		Owner: reap.Owner{ID: "user-1", Email: "maya@jarvis.example"}, Presentation: reap.Presentation{ReturnURL: returnURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ActivateEnrollment(e.ID); err != nil {
		t.Fatal(err)
	}
	return e.ID
}

func quoteFor(t *testing.T, c reap.Client, variantID string, qty int) reap.Quote {
	t.Helper()
	q, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{
		Email: "maya@jarvis.example", Items: []reap.QuoteItem{{VariantID: variantID, Quantity: qty}}, ShippingAddress: sgAddress(),
	})
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func cents(m reap.Money) domain.Cents { return domain.CentsFromFloat(m.Amount) }

// The full happy path from the docs: search -> details -> variant -> quote -> shipping ->
// enrollment -> checkout (REQUIRES_ACTION + hosted URL) -> approve -> poll COMPLETED.
func TestHappyPathPurchase(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})

	sr, err := c.SearchProducts(ctx, reap.SearchRequest{
		Query:              "USB C to USB C cable 100W",
		MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "ugreen.com.sg"},
		Context:            &reap.SearchContext{Country: "SG", Currency: "SGD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sr.Products) == 0 || sr.ID == "" {
		t.Fatalf("no products: %+v", sr)
	}
	p := sr.Products[0]
	if p.Merchant.Name != fakereap.MerchantUgreen || p.PreviewVariant == nil || p.PriceRange.Min.Currency != "SGD" {
		t.Fatalf("product %+v", p)
	}
	for _, sp := range sr.Products {
		if sp.Merchant.Name != fakereap.MerchantUgreen {
			t.Errorf("ONLY returned other merchant %q", sp.Merchant.Name)
		}
	}

	det, err := c.GetProductDetails(ctx, []string{p.ID, "prd_does_not_exist"})
	if err != nil {
		t.Fatal(err)
	}
	if len(det.Products) != 1 || len(det.Errors) != 1 || det.Errors[0].ProductID != "prd_does_not_exist" {
		t.Fatalf("details %+v", det)
	}
	d := det.Products[0]
	if len(d.Options) != 1 || d.Options[0].Name != "Length" || len(d.Options[0].Values) != 3 || d.DefaultVariant.ID == "" {
		t.Fatalf("details product %+v", d)
	}

	// Pick the 2m option.
	var twoM string
	for _, v := range d.Options[0].Values {
		if v.Label == "2m" {
			twoM = v.OptionID
		}
	}
	v, err := c.ResolveVariant(ctx, reap.VariantRequest{ProductID: d.ID, OptionIDs: []string{twoM}})
	if err != nil {
		t.Fatal(err)
	}
	if v.Price.Amount != 12.99 || v.Available == nil || !*v.Available || v.Options[0].Value != "2m" {
		t.Fatalf("variant %+v", v)
	}

	q := quoteFor(t, c, v.ID, 3) // 3 x 12.99 = 38.97 -> below free shipping
	if cents(q.AmountBreakdown.ItemsSubtotal) != 3897 || len(q.ShippingOptions) != 2 {
		t.Fatalf("quote %+v", q)
	}
	if !q.ShippingOptions[0].Selected || cents(q.ShippingOptions[0].Price) != fakereap.StandardShippingCents {
		t.Fatalf("standard should be preselected at S$4: %+v", q.ShippingOptions)
	}
	if cents(q.AmountBreakdown.FinalAmount) != 3897+400 || q.AmountBreakdown.Tax == nil || !q.AmountBreakdown.Tax.IncludedInPrices {
		t.Fatalf("breakdown %+v", q.AmountBreakdown)
	}
	if !q.ExpiresAt.Equal(s.Now().Add(fakereap.DefaultQuoteTTL)) {
		t.Fatalf("expiresAt %v", q.ExpiresAt)
	}

	q2, err := c.SelectShippingOption(ctx, q.ID, q.ShippingOptions[1].ID)
	if err != nil {
		t.Fatal(err)
	}
	if !q2.ShippingOptions[1].Selected || q2.ShippingOptions[0].Selected || cents(q2.AmountBreakdown.FinalAmount) != 3897+1200 {
		t.Fatalf("express %+v", q2)
	}
	if got, _ := c.GetQuote(ctx, q.ID); cents(got.AmountBreakdown.FinalAmount) != 5097 {
		t.Fatalf("get quote final %v", got.AmountBreakdown.FinalAmount)
	}

	enr := activeEnrollment(t, s, c)
	co, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{
		QuoteID: q.ID, EnrollmentID: enr, Presentation: reap.Presentation{ReturnURL: returnURL},
	})
	if err != nil {
		t.Fatal(err)
	}
	if co.Status != reap.CheckoutRequiresAction || co.NextAction == nil || !strings.HasPrefix(co.NextAction.URL, s.URL+"/hosted/checkouts/") ||
		co.Amount == nil || cents(*co.Amount) != 5097 {
		t.Fatalf("checkout %+v", co)
	}
	if got, _ := c.GetCheckout(ctx, co.ID); got.Status != reap.CheckoutRequiresAction || got.OrderID != nil {
		t.Fatalf("before approval %+v", got)
	}
	// Quote is locked once a checkout exists.
	if _, err := c.SelectShippingOption(ctx, q.ID, q.ShippingOptions[0].ID); !reap.IsCode(err, reap.CodeQuoteNotMutable) {
		t.Fatalf("err = %v, want QUOTE_NOT_MUTABLE", err)
	}

	if err := s.ApproveCheckout(co.ID); err != nil {
		t.Fatal(err)
	}
	done, err := c.GetCheckout(ctx, co.ID)
	if err != nil {
		t.Fatal(err)
	}
	if done.Status != reap.CheckoutCompleted || done.OrderID == nil || *done.OrderID == "" || done.FinalAmount == nil ||
		cents(*done.FinalAmount) != 5097 || done.NextAction != nil || !done.Status.Terminal() {
		t.Fatalf("completed %+v", done)
	}
	if err := s.FailCheckout(co.ID); err == nil {
		t.Fatal("terminal checkout must not change")
	}
}

func TestSearchBehaviour(t *testing.T) {
	_, c := newFake(t, fakereap.Options{})
	tests := []struct {
		name         string
		req          reap.SearchRequest
		wantCode     string
		wantMerchant string // every result must be from this merchant ("" = no check)
		wantFirst    string // first product name contains
		wantMin      int
		wantMax      int
		wantForeign  bool // includes non-allow-listed merchants
	}{
		{name: "domain resolves", req: reap.SearchRequest{Query: "A4 copier paper 80g 500 sheets",
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "popular.com.sg"}},
			wantMerchant: fakereap.MerchantPopular, wantFirst: "Copier Paper", wantMin: 2, wantMax: 10},
		{name: "display name fails like the sandbox", req: reap.SearchRequest{Query: "paper",
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "Popular Bookstore"}},
			wantCode: reap.CodeMerchantNotResolved},
		{name: "anker returns ---", req: reap.SearchRequest{Query: "Anker 65W USB C charger",
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "anker.com.sg"}},
			wantMerchant: "---", wantFirst: "Anker Charger 735", wantMin: 1, wantMax: 2},
		{name: "no preference includes non-allowlisted merchants", req: reap.SearchRequest{Query: "ballpoint pen 0.7mm"},
			wantMin: 2, wantMax: 10, wantForeign: true},
		{name: "prefer ranks merchant first", req: reap.SearchRequest{Query: "earl grey tea",
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantPrefer, MerchantName: "prycetea.com"}},
			wantFirst: "Pryce", wantMin: 2, wantMax: 10},
		{name: "availability filter drops out-of-stock", req: reap.SearchRequest{Query: "copier paper 70g",
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: "popular.com.sg"},
			Filters:            &reap.SearchFilters{Availability: "AVAILABLE_ONLY"}},
			wantMerchant: fakereap.MerchantPopular, wantMin: 1, wantMax: 10},
		{name: "price filter", req: reap.SearchRequest{Query: "ergonomic chair",
			Filters: &reap.SearchFilters{Price: &reap.PriceFilter{Max: "400"}}},
			wantFirst: "Joobie", wantMin: 1, wantMax: 1},
		{name: "no match", req: reap.SearchRequest{Query: "submarine periscope"}, wantMin: 0, wantMax: 0},
		{name: "bad limit", req: reap.SearchRequest{Query: "pen", Pagination: &reap.Pagination{Limit: 51}}, wantCode: reap.CodeRequestRejected},
		{name: "bad mode", req: reap.SearchRequest{Query: "pen",
			MerchantPreference: &reap.MerchantPreference{Mode: "MAYBE", MerchantName: "popular.com.sg"}}, wantCode: reap.CodeRequestRejected},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res, err := c.SearchProducts(ctx, tc.req)
			if tc.wantCode != "" {
				if !reap.IsCode(err, tc.wantCode) {
					t.Fatalf("err = %v, want %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if n := len(res.Products); n < tc.wantMin || n > tc.wantMax || res.Pagination.ReturnedCount != n {
				t.Fatalf("got %d products (returnedCount %d), want %d..%d", n, res.Pagination.ReturnedCount, tc.wantMin, tc.wantMax)
			}
			if res.Warnings == nil {
				t.Errorf("warnings must be [] not null")
			}
			foreign := false
			for _, p := range res.Products {
				if tc.wantMerchant != "" && p.Merchant.Name != tc.wantMerchant {
					t.Errorf("merchant %q", p.Merchant.Name)
				}
				if tc.req.Filters != nil && tc.req.Filters.Availability != "" && (p.Available == nil || !*p.Available) {
					t.Errorf("unavailable product %s returned", p.Name)
				}
				switch p.Merchant.Name {
				case fakereap.MerchantMustafa, fakereap.MerchantStationeryPal, fakereap.MerchantWaangoo:
					foreign = true
				}
			}
			if foreign != tc.wantForeign {
				t.Errorf("foreign merchants present = %v, want %v", foreign, tc.wantForeign)
			}
			if tc.wantFirst != "" && !strings.Contains(res.Products[0].Name, tc.wantFirst) {
				t.Errorf("first = %q, want contains %q", res.Products[0].Name, tc.wantFirst)
			}
		})
	}
}

func TestSearchPagination(t *testing.T) {
	_, c := newFake(t, fakereap.Options{})
	req := reap.SearchRequest{Query: "coffee", Pagination: &reap.Pagination{Limit: 2}}
	seen := map[string]bool{}
	pages := 0
	for {
		res, err := c.SearchProducts(ctx, req)
		if err != nil {
			t.Fatal(err)
		}
		pages++
		for _, p := range res.Products {
			if seen[p.ID] {
				t.Fatalf("duplicate %s across pages", p.ID)
			}
			seen[p.ID] = true
		}
		if !res.Pagination.HasNextPage {
			if res.Pagination.NextCursor != nil {
				t.Fatal("nextCursor set on last page")
			}
			break
		}
		req.Pagination.Cursor = res.Pagination.NextCursor
	}
	if pages < 2 || len(seen) < 4 {
		t.Fatalf("pages=%d products=%d", pages, len(seen))
	}
	bad := "garbage"
	if _, err := c.SearchProducts(ctx, reap.SearchRequest{Query: "coffee", Pagination: &reap.Pagination{Cursor: &bad}}); !reap.IsCode(err, reap.CodeRequestRejected) {
		t.Fatalf("err = %v", err)
	}
}

func TestCatalogQueriesFindPreferredVendors(t *testing.T) {
	// Every seed catalog search_query must find a product at its first preferred vendor (mirrors the probe).
	_, c := newFake(t, fakereap.Options{})
	cases := map[string]string{
		"A4 copier paper 80g 500 sheets": "popular.com.sg", "ballpoint pen 0.7mm": "kinokuniya.com.sg",
		"Post-it super sticky notes": "popular.com.sg", "MAX HD-10 stapler": "popular.com.sg", "MAX staples 10-1M": "popular.com.sg",
		"A4 data envelope": "popular.com.sg", "KOKUYO A4 campus notebook": "kinokuniya.com.sg", "Energizer Max AAA 4pcs": "popular.com.sg",
		"espresso blend coffee beans": "commonmancoffeeroasters.com", "coffee capsules": "commonmancoffeeroasters.com",
		"drip bags": "pppcoffee.com", "earl grey tea": "gryphontea.com", "genmaicha green tea": "gryphontea.com",
		"fancy mixed nuts 1kg": "camelnuts.com", "healthylicious variety box": "boxgreen.co",
		"IRVINS salted egg potato chips 95g": "irvinsaltedegg.com", "box of 6 mixed cookies": "plainvanilla.com.sg",
		"hand sanitizer": "pupsik.sg", "Selleys multi purpose cleaner": "intertech-hardware.com", "garbage bag": "shoppy.sg",
		"hand wash 500ml": "spectrumstore.sg", "microfiber cleaning cloth": "shoppy.sg", "USB C to USB C cable 100W": "anker.com.sg",
		"HDMI cable 4K": "ugreen.com.sg", "USB C hub": "ugreen.com.sg", "monitor arm": "ergotune.com",
		"PRISM+ X240 monitor": "prismplus.sg", "ErgoTune ergonomic chair": "ergotune.com",
		"STABILO highlighter set of 4": "popular.com.sg", "whiteboard marker": "popular.com.sg", "A4 2D ring file": "popular.com.sg",
		"PENTEL correction tape": "popular.com.sg", "copier paper 80g A4 500's 1 carton": "popular.com.sg",
	}
	for q, dom := range cases {
		res, err := c.SearchProducts(ctx, reap.SearchRequest{Query: q,
			MerchantPreference: &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: dom}})
		if err != nil {
			t.Errorf("%q @ %s: %v", q, dom, err)
			continue
		}
		if len(res.Products) == 0 {
			t.Errorf("%q @ %s: no products", q, dom)
		}
	}
}

func TestVariantResolution(t *testing.T) {
	_, c := newFake(t, fakereap.Options{})
	det, err := c.GetProductDetails(ctx, []string{"prd_pop_pilot_rexgrip", "prd_pop_max_hd10nx"})
	if err != nil {
		t.Fatal(err)
	}
	pen, stapler := det.Products[0], det.Products[1]
	ids := map[string]string{}
	for _, v := range pen.Options[0].Values {
		ids[v.Label] = v.OptionID
		if v.Label == "Red" && (v.Available == nil || *v.Available) {
			t.Errorf("Red should be unavailable")
		}
	}
	if len(stapler.Options) != 0 || stapler.DefaultVariant.Options == nil {
		t.Fatalf("simple product options %+v", stapler)
	}
	tests := []struct {
		name     string
		req      reap.VariantRequest
		wantCode string
		wantVal  string
		avail    bool
	}{
		{"black", reap.VariantRequest{ProductID: pen.ID, OptionIDs: []string{ids["Black"]}}, "", "Black", true},
		{"red resolves but unavailable", reap.VariantRequest{ProductID: pen.ID, OptionIDs: []string{ids["Red"]}}, "", "Red", false},
		{"two values for one option", reap.VariantRequest{ProductID: pen.ID, OptionIDs: []string{ids["Black"], ids["Blue"]}}, reap.CodeRequestRejected, "", false},
		{"missing option", reap.VariantRequest{ProductID: pen.ID, OptionIDs: []string{}}, reap.CodeRequestRejected, "", false},
		{"unknown option", reap.VariantRequest{ProductID: pen.ID, OptionIDs: []string{"opt_nope"}}, reap.CodeRequestRejected, "", false},
		{"simple product default", reap.VariantRequest{ProductID: stapler.ID}, "", "", true},
		{"unknown product", reap.VariantRequest{ProductID: "prd_x"}, reap.CodeResourceNotFound, "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v, err := c.ResolveVariant(ctx, tc.req)
			if tc.wantCode != "" {
				if !reap.IsCode(err, tc.wantCode) {
					t.Fatalf("err = %v, want %s", err, tc.wantCode)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantVal != "" && v.Options[0].Value != tc.wantVal {
				t.Errorf("value %+v", v.Options)
			}
			if *v.Available != tc.avail {
				t.Errorf("available = %v", *v.Available)
			}
		})
	}
}

func TestQuoteValidation(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	addr := sgAddress()
	badPhone := *addr
	badPhone.Phone = "91234567"
	abroad := *addr
	abroad.Country = "MY"
	tests := []struct {
		name       string
		req        reap.CreateQuoteRequest
		wantCode   string
		wantReason string
		wantStatus int
	}{
		{"unknown variant", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_nope", Quantity: 1}}, ShippingAddress: addr},
			reap.CodeVariantUnavailable, "", 409},
		{"out of stock", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_double_a_a4_70g", Quantity: 1}}, ShippingAddress: addr},
			reap.CodeVariantUnavailable, "", 409},
		{"mixed merchants", reap.CreateQuoteRequest{Email: "a@b.sg", ShippingAddress: addr,
			Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}, {VariantID: "var_camel_fancy_mixed_1kg", Quantity: 1}}},
			reap.CodeRequestRejected, "", 400},
		{"no address", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}},
			reap.CodeRequestRejected, "", 400},
		{"bad phone", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: &badPhone},
			reap.CodeQuoteUnfulfillable, reap.ReasonInvalidPhone, 400},
		{"outside SG", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: &abroad},
			reap.CodeQuoteUnfulfillable, reap.ReasonItemsUnshippable, 400},
		{"expired offer", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: addr, OfferCode: fakereap.OfferCodeExpired},
			reap.CodeOfferCodeExpired, "", 400},
		{"invalid offer", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: addr, OfferCode: "FREE"},
			reap.CodeOfferCodeInvalid, "", 400},
		{"external checkout", reap.CreateQuoteRequest{Email: "a@b.sg", ExternalCheckout: &reap.ExternalCheckout{MerchantDomain: "popular.com.sg", CheckoutURL: "https://popular.com.sg/cart"}, ShippingAddress: addr},
			reap.CodeCheckoutURLInvalid, "", 400},
		{"zero qty", reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 0}}, ShippingAddress: addr},
			reap.CodeRequestRejected, "", 400},
		{"no email", reap.CreateQuoteRequest{Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: addr},
			reap.CodeRequestRejected, "", 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), tc.req)
			var ae *reap.APIError
			if !errors.As(err, &ae) || ae.Code != tc.wantCode || ae.Reason() != tc.wantReason || ae.HTTPStatus != tc.wantStatus {
				t.Fatalf("err = %v, want %d %s/%s", err, tc.wantStatus, tc.wantCode, tc.wantReason)
			}
		})
	}
	if q, _, _ := s.Counts(); q != 0 {
		t.Fatalf("failed quotes were stored: %d", q)
	}
}

func TestQuotePricing(t *testing.T) {
	_, c := newFake(t, fakereap.Options{})
	// 10 reams x 7.10 = 71.00 >= 60 -> free standard shipping; SAVE10 -> 7.10 off.
	q, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{
		Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_ik_copier_a4_80g", Quantity: 10}},
		ShippingAddress: sgAddress(), OfferCode: "save10",
	})
	if err != nil {
		t.Fatal(err)
	}
	b := q.AmountBreakdown
	if cents(b.ItemsSubtotal) != 7100 || cents(*b.Shipping) != 0 || len(b.Discounts) != 1 || cents(b.Discounts[0].Amount) != 710 ||
		cents(b.FinalAmount) != 6390 {
		t.Fatalf("breakdown %+v", b)
	}
	// GST-inclusive: 63.90 * 9/109 = 5.276 -> 5.28
	if cents(b.Tax.Amount) != 528 {
		t.Fatalf("gst %v", b.Tax.Amount)
	}
}

func TestPriceDriftHook(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	if err := s.SetVariantPrice("var_pop_max_hd10nx", 990); err != nil {
		t.Fatal(err)
	}
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	if cents(q.AmountBreakdown.ItemsSubtotal) != 990 {
		t.Fatalf("subtotal %v", q.AmountBreakdown.ItemsSubtotal)
	}
	if err := s.SetVariantAvailable("var_pop_max_hd10nx", false); err != nil {
		t.Fatal(err)
	}
	_, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{Email: "a@b.sg",
		Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: sgAddress()})
	if !reap.IsCode(err, reap.CodeVariantUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if s.SetVariantPrice("nope", 1) == nil || s.SetVariantAvailable("nope", true) == nil {
		t.Fatal("unknown variant must error")
	}
}

func TestVariantMaxQuantity(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	if err := s.SetVariantMaxQuantity("var_pop_max_hd10nx", 5); err != nil {
		t.Fatal(err)
	}
	quoteFor(t, c, "var_pop_max_hd10nx", 5)
	_, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{Email: "a@b.sg",
		Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 6}}, ShippingAddress: sgAddress()})
	var ae *reap.APIError
	if !errors.As(err, &ae) || ae.HTTPStatus != 400 || ae.Code != reap.CodeRequestRejected || !reap.IsItemRejection(err) {
		t.Fatalf("err = %#v", err)
	}
	if err := s.SetVariantMaxQuantity("var_pop_max_hd10nx", 0); err != nil {
		t.Fatal(err)
	}
	quoteFor(t, c, "var_pop_max_hd10nx", 6)
	if s.SetVariantMaxQuantity("nope", 1) == nil {
		t.Fatal("unknown variant must error")
	}
}

func TestQuoteShippingErrorsAndExpiry(t *testing.T) {
	s, c := newFake(t, fakereap.Options{QuoteTTL: 10 * time.Minute})
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	if _, err := c.SelectShippingOption(ctx, q.ID, "ship_bogus"); !reap.IsCode(err, reap.CodeShippingOptionInvalid) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.GetQuote(ctx, "00000000-0000-4000-8000-000000000000"); !reap.IsCode(err, reap.CodeQuoteNotFound) || !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	if _, err := c.SelectShippingOption(ctx, "nope", "x"); !reap.IsCode(err, reap.CodeQuoteNotFound) {
		t.Fatalf("err = %v", err)
	}
	s.Advance(11 * time.Minute)
	if _, err := c.SelectShippingOption(ctx, q.ID, q.ShippingOptions[1].ID); !reap.IsCode(err, reap.CodeQuoteExpired) {
		t.Fatalf("err = %v", err)
	}
	enr := activeEnrollment(t, s, c)
	_, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if !reap.IsCode(err, reap.CodeQuoteExpired) {
		t.Fatalf("err = %v", err)
	}
	q2 := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	if err := s.ExpireQuote(q2.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q2.ID, EnrollmentID: enr,
		Presentation: reap.Presentation{ReturnURL: returnURL}}); !reap.IsCode(err, reap.CodeQuoteExpired) {
		t.Fatalf("err = %v", err)
	}
}

func TestEnrollmentLifecycle(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	req := reap.CreateEnrollmentRequest{Owner: reap.Owner{ID: "user-1", Email: "maya@jarvis.example"},
		Presentation: reap.Presentation{ReturnURL: returnURL}}
	e, err := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), req)
	if err != nil {
		t.Fatal(err)
	}
	if e.Status != reap.EnrollmentRequiresAction || e.NextAction == nil || e.Source != "EXTERNAL" || e.Owner.Type != "CLIENT_REFERENCE" ||
		e.PaymentMethod != nil {
		t.Fatalf("created %+v", e)
	}
	// Checkout against a not-yet-active enrollment: 409 ENROLLMENT_NOT_ACTIVE / CARD_NOT_CAPTURED.
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	_, err = c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: e.ID,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if !reap.IsCode(err, reap.CodeEnrollmentNotActive) || !reap.IsReason(err, reap.ReasonCardNotCaptured) {
		t.Fatalf("err = %v", err)
	}
	if err := s.ActivateEnrollment(e.ID); err != nil {
		t.Fatal(err)
	}
	got, err := c.GetEnrollment(ctx, e.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != reap.EnrollmentActive || got.NextAction != nil || got.PaymentMethod == nil || got.CreatedAt == nil {
		t.Fatalf("active %+v", got)
	}
	if err := s.ActivateEnrollment(e.ID); err == nil {
		t.Fatal("double activate should fail")
	}
	if err := s.RevokeEnrollment(e.ID); err != nil {
		t.Fatal(err)
	}
	if got, _ := c.GetEnrollment(ctx, e.ID); got.Status != reap.EnrollmentRevoked {
		t.Fatalf("revoked %+v", got)
	}

	// Expiry of the hosted step.
	e2, _ := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), req)
	s.Advance(fakereap.DefaultActionTTL + time.Second)
	if got, _ := c.GetEnrollment(ctx, e2.ID); got.Status != reap.EnrollmentExpired || got.NextAction != nil {
		t.Fatalf("expired %+v", got)
	}
	if _, err := c.GetEnrollment(ctx, "nope"); !reap.IsCode(err, reap.CodeEnrollmentNotFound) {
		t.Fatalf("err = %v", err)
	}

	bad := []reap.CreateEnrollmentRequest{
		{Source: "REAP_CARD", Owner: req.Owner, Presentation: req.Presentation},
		{Owner: reap.Owner{ID: "u"}, Presentation: req.Presentation},
		{Owner: req.Owner, Presentation: reap.Presentation{ReturnURL: "http://insecure.example/r"}},
	}
	for i, b := range bad {
		if _, err := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), b); !reap.IsCode(err, reap.CodeRequestRejected) {
			t.Errorf("bad[%d]: err = %v", i, err)
		}
	}
}

func TestCheckoutOutcomes(t *testing.T) {
	tests := []struct {
		name  string
		act   func(s *fakereap.Server, id string) error
		want  reap.CheckoutStatus
		order bool
	}{
		{"approve", (*fakereap.Server).ApproveCheckout, reap.CheckoutCompleted, true},
		{"complete", (*fakereap.Server).CompleteCheckout, reap.CheckoutCompleted, true},
		{"fail", (*fakereap.Server).FailCheckout, reap.CheckoutFailed, false},
		{"expire hook", (*fakereap.Server).ExpireCheckout, reap.CheckoutExpired, false},
		{"approval window lapses", func(s *fakereap.Server, _ string) error { s.Advance(fakereap.DefaultActionTTL); return nil }, reap.CheckoutExpired, false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s, c := newFake(t, fakereap.Options{})
			enr := activeEnrollment(t, s, c)
			q := quoteFor(t, c, "var_pop_max_hd10nx", 2)
			co, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr,
				Presentation: reap.Presentation{ReturnURL: returnURL}})
			if err != nil {
				t.Fatal(err)
			}
			if err := tc.act(s, co.ID); err != nil {
				t.Fatal(err)
			}
			got, err := c.GetCheckout(ctx, co.ID)
			if err != nil {
				t.Fatal(err)
			}
			if got.Status != tc.want || (got.OrderID != nil) != tc.order || got.NextAction != nil {
				t.Fatalf("got %+v", got)
			}
			if s.LatestCheckoutID() != co.ID || len(s.CheckoutIDs()) != 1 {
				t.Errorf("checkout id helpers")
			}
		})
	}
}

func TestCheckoutErrors(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	enr := activeEnrollment(t, s, c)
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	pres := reap.Presentation{ReturnURL: returnURL}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: "missing", EnrollmentID: enr, Presentation: pres}); !reap.IsCode(err, reap.CodeQuoteNotFound) {
		t.Errorf("quote not found: %v", err)
	}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: "missing", Presentation: pres}); !reap.IsCode(err, reap.CodeEnrollmentNotFound) {
		t.Errorf("enrollment not found: %v", err)
	}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr,
		Presentation: reap.Presentation{ReturnURL: "not a url"}}); !reap.IsCode(err, reap.CodeRequestRejected) {
		t.Errorf("bad return url: %v", err)
	}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr, Presentation: pres}); err != nil {
		t.Fatal(err)
	}
	if _, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr, Presentation: pres}); !reap.IsCode(err, reap.CodeRequestRejected) {
		t.Errorf("second checkout on same quote with new key: %v", err)
	}
	if _, err := c.GetCheckout(ctx, "nope"); !reap.IsCode(err, reap.CodeCheckoutNotFound) || !errors.Is(err, domain.ErrNotFound) {
		t.Errorf("get missing: %v", err)
	}
	if s.ApproveCheckout("nope") == nil || s.ActivateEnrollment("nope") == nil || s.ExpireQuote("nope") == nil {
		t.Error("hooks must reject unknown ids")
	}
}

func TestSimulateCheckoutHeader(t *testing.T) {
	s, _ := newFake(t, fakereap.Options{})
	c := s.Client(func(o *reap.Options) { o.SimulateCheckout = true })
	enr := activeEnrollment(t, s, c)
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	co, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if err != nil {
		t.Fatal(err)
	}
	if co.NextAction == nil {
		t.Fatal("create response always carries nextAction")
	}
	if r := s.RequestsTo("POST", "/agentic/checkouts"); len(r) != 1 || r[0].SimulateCheckout != "COMPLETED" {
		t.Fatalf("simulate header not sent: %+v", r)
	}
	got, _ := c.GetCheckout(ctx, co.ID)
	if got.Status != reap.CheckoutCompleted || got.OrderID == nil {
		t.Fatalf("simulated %+v", got)
	}
}

func TestAutoOptions(t *testing.T) {
	s, c := newFake(t, fakereap.Options{AutoActivateEnrollments: true, AutoApproveCheckouts: true})
	e, err := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), reap.CreateEnrollmentRequest{
		Owner: reap.Owner{ID: "u", Email: "u@x.sg"}, Presentation: reap.Presentation{ReturnURL: returnURL}})
	if err != nil || e.Status != reap.EnrollmentActive {
		t.Fatalf("auto enrollment %+v %v", e, err)
	}
	q := quoteFor(t, c, "var_irvins_chips_95g", 4)
	co, err := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: e.ID,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := c.GetCheckout(ctx, co.ID); got.Status != reap.CheckoutCompleted {
		t.Fatalf("auto approve %+v", got)
	}
	_ = s
}

func TestIdempotency(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	key := reap.NewIdempotencyKey()
	req := reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: sgAddress()}
	q1, err := c.CreateQuote(ctx, key, req)
	if err != nil {
		t.Fatal(err)
	}
	q2, err := c.CreateQuote(ctx, key, req)
	if err != nil {
		t.Fatal(err)
	}
	if q1.ID != q2.ID {
		t.Fatalf("replay created a new quote: %s vs %s", q1.ID, q2.ID)
	}
	if n, _, _ := s.Counts(); n != 1 {
		t.Fatalf("quotes = %d", n)
	}
	req.Items[0].Quantity = 2
	if _, err := c.CreateQuote(ctx, key, req); !reap.IsCode(err, reap.CodeRequestRejected) {
		t.Fatalf("changed body with same key: %v", err)
	}
	// Errors are replayed too (Reap caches the first response).
	bad := reap.CreateQuoteRequest{Email: "a@b.sg", Items: []reap.QuoteItem{{VariantID: "var_nope", Quantity: 1}}, ShippingAddress: sgAddress()}
	k2 := reap.NewIdempotencyKey()
	_, e1 := c.CreateQuote(ctx, k2, bad)
	_, e2 := c.CreateQuote(ctx, k2, bad)
	if !reap.IsCode(e1, reap.CodeVariantUnavailable) || !reap.IsCode(e2, reap.CodeVariantUnavailable) {
		t.Fatalf("%v / %v", e1, e2)
	}
	// Same key on a different endpoint is a different scope.
	e, err := c.CreateEnrollment(ctx, key, reap.CreateEnrollmentRequest{Owner: reap.Owner{ID: "u", Email: "u@x.sg"},
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if err != nil || e.ID == "" {
		t.Fatalf("enrollment with reused key on other endpoint: %v", err)
	}
}

// Retries after injected 503s must reuse the same Idempotency-Key and create exactly one resource.
func TestRetryAfter503ReusesIdempotencyKey(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	enr := activeEnrollment(t, s, c)
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	s.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/checkouts", Status: 503, Code: reap.CodeCheckoutTempUnavail, Times: 2})
	key := reap.NewIdempotencyKey()
	co, err := c.CreateCheckout(ctx, key, reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	if err != nil {
		t.Fatal(err)
	}
	reqs := s.RequestsTo("POST", "/agentic/checkouts")
	if len(reqs) != 3 {
		t.Fatalf("attempts = %d, want 3", len(reqs))
	}
	for _, r := range reqs {
		if r.IdempotencyKey != key {
			t.Fatalf("key changed across retries")
		}
	}
	if _, n, _ := s.Counts(); n != 1 || co.ID == "" {
		t.Fatalf("checkouts = %d", n)
	}

	// Search with a wildcard fault and a client that does not retry surfaces the 503.
	s.InjectFault(fakereap.Fault{Path: "/agentic/products/*", Status: 503, Code: reap.CodeServiceUnavailable})
	nr := s.Client(func(o *reap.Options) { o.MaxRetries = -1 })
	_, err = nr.SearchProducts(ctx, reap.SearchRequest{Query: "pen"})
	var ae *reap.APIError
	if !errors.As(err, &ae) || !ae.Retryable() || !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
	if _, err := nr.SearchProducts(ctx, reap.SearchRequest{Query: "pen"}); err != nil {
		t.Fatalf("fault should be consumed: %v", err)
	}
}

func TestAuthAndVersionEnforced(t *testing.T) {
	s, _ := newFake(t, fakereap.Options{})
	tests := []struct {
		name     string
		mut      func(*reap.Options)
		wantCode string
		status   int
	}{
		{"wrong key", func(o *reap.Options) { o.APIKey = "wrong" }, "UNAUTHORIZED", 401},
		{"wrong version", func(o *reap.Options) { o.Version = "2024-01-01" }, reap.CodeRequestRejected, 400},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s.Client(tc.mut).SearchProducts(ctx, reap.SearchRequest{Query: "pen"})
			var ae *reap.APIError
			if !errors.As(err, &ae) || ae.Code != tc.wantCode || ae.HTTPStatus != tc.status {
				t.Fatalf("err = %v", err)
			}
		})
	}
	for _, r := range s.Requests() {
		if strings.Contains(string(r.Body), s.APIKey()) {
			t.Fatal("recorded body contains key")
		}
	}
	// Missing Idempotency-Key straight over HTTP.
	req, _ := http.NewRequest("POST", s.URL+"/agentic/quotes", strings.NewReader(`{}`))
	req.Header.Set("Authorization", "Bearer "+s.APIKey())
	req.Header.Set("Reap-Version", reap.DefaultVersion)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if resp.StatusCode != 400 || !strings.Contains(string(b), "Idempotency-Key") {
		t.Fatalf("%d %s", resp.StatusCode, b)
	}
	// Mandates are not live.
	req, _ = http.NewRequest("GET", s.URL+"/agentic/mandates/m1", nil)
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 404 {
		t.Fatalf("mandates status %d", resp.StatusCode)
	}
}

func TestHostedPagesRedirectBack(t *testing.T) {
	_, c := newFake(t, fakereap.Options{})
	noFollow := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}

	e, _ := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), reap.CreateEnrollmentRequest{
		Owner: reap.Owner{ID: "u", Email: "u@x.sg"}, Presentation: reap.Presentation{ReturnURL: returnURL + "?step=card"}})
	resp, err := noFollow.Get(e.NextAction.URL)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther || !strings.Contains(resp.Header.Get("Location"), "enrollment_id="+e.ID) ||
		!strings.Contains(resp.Header.Get("Location"), "step=card") {
		t.Fatalf("enrollment redirect %d %s", resp.StatusCode, resp.Header.Get("Location"))
	}
	if got, _ := c.GetEnrollment(ctx, e.ID); got.Status != reap.EnrollmentActive {
		t.Fatalf("hosted page did not activate: %s", got.Status)
	}

	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	co, _ := c.CreateCheckout(ctx, reap.NewIdempotencyKey(), reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: e.ID,
		Presentation: reap.Presentation{ReturnURL: returnURL}})
	resp, err = noFollow.Get(co.NextAction.URL + "?decision=decline")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusSeeOther {
		t.Fatalf("checkout redirect %d", resp.StatusCode)
	}
	if got, _ := c.GetCheckout(ctx, co.ID); got.Status != reap.CheckoutFailed {
		t.Fatalf("declined status %s", got.Status)
	}
}

func TestAllowHTTPReturnURL(t *testing.T) {
	_, c := newFake(t, fakereap.Options{AllowHTTPReturnURL: true})
	if _, err := c.CreateEnrollment(ctx, reap.NewIdempotencyKey(), reap.CreateEnrollmentRequest{
		Owner: reap.Owner{ID: "u", Email: "u@x.sg"}, Presentation: reap.Presentation{ReturnURL: "http://localhost:5173/return"}}); err != nil {
		t.Fatal(err)
	}
}

func TestConcurrentUse(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	var wg sync.WaitGroup
	errs := make(chan error, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.SearchProducts(ctx, reap.SearchRequest{Query: "coffee beans"}); err != nil {
				errs <- err
			}
			if _, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), reap.CreateQuoteRequest{Email: "a@b.sg",
				Items: []reap.QuoteItem{{VariantID: "var_bettr_espresso_250g", Quantity: 1}}, ShippingAddress: sgAddress()}); err != nil {
				errs <- err
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Error(err)
	}
	if n, _, _ := s.Counts(); n != 20 {
		t.Fatalf("quotes = %d", n)
	}
}

func TestFixturesConsistent(t *testing.T) {
	ids := map[string]bool{}
	for _, p := range fakereap.DefaultProducts() {
		if ids[p.ID] || len(p.Variants) == 0 || p.Domain == "" || p.Merchant == "" {
			t.Errorf("bad product %+v", p)
		}
		ids[p.ID] = true
		for _, v := range p.Variants {
			if ids[v.ID] || v.PriceCents <= 0 {
				t.Errorf("bad variant %+v", v)
			}
			ids[v.ID] = true
			for _, n := range p.OptionNames {
				if v.Options[n] == "" {
					t.Errorf("%s missing option %s", v.ID, n)
				}
			}
		}
	}
	// The alias package exposes the same server.
	s := reaptest.New(reaptest.Options{})
	defer s.Close()
	if _, err := s.Client().SearchProducts(ctx, reap.SearchRequest{Query: "stapler"}); err != nil {
		t.Fatal(err)
	}
}

// Reap replays the first response for an idempotency key, including a 503. A quote that failed
// with QUOTE_TEMPORARILY_UNAVAILABLE must therefore be retried under a NEW key.
func TestQuote503ReplayedForSameKey(t *testing.T) {
	s, c := newFake(t, fakereap.Options{CacheServerErrors: true})
	s.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/quotes", Status: 503, Code: reap.CodeQuoteTempUnavailable, Stage: fakereap.FaultInHandler})
	req := reap.CreateQuoteRequest{Email: "maya@jarvis.example", Items: []reap.QuoteItem{{VariantID: "var_pop_max_hd10nx", Quantity: 1}}, ShippingAddress: sgAddress()}
	key := reap.NewIdempotencyKey()
	for i := 0; i < 2; i++ {
		if _, err := c.CreateQuote(ctx, key, req); !reap.IsCode(err, reap.CodeQuoteTempUnavailable) {
			t.Fatalf("attempt %d with the same key: err = %v, want replayed 503", i, err)
		}
	}
	if n := len(s.RequestsTo("POST", "/agentic/quotes")); n != 2 {
		t.Fatalf("requests = %d, want 2 (the client must not retry this code in-process)", n)
	}
	q, err := c.CreateQuote(ctx, reap.NewIdempotencyKey(), req)
	if err != nil || q.ID == "" {
		t.Fatalf("new key: %+v %v", q, err)
	}
	if n, _, _ := s.Counts(); n != 1 {
		t.Fatalf("quotes = %d", n)
	}
}

// FaultAfterProcess: the checkout is created and cached, but the response is lost. A retry with the
// same key gets the original checkout back (no second checkout).
func TestFaultAfterProcess_ResponseLost(t *testing.T) {
	s, c := newFake(t, fakereap.Options{})
	enr := activeEnrollment(t, s, c)
	q := quoteFor(t, c, "var_pop_max_hd10nx", 1)
	s.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/checkouts", Status: 503, Code: reap.CodeServiceUnavailable, Stage: fakereap.FaultAfterProcess, Times: 2})
	nr := s.Client(func(o *reap.Options) { o.MaxRetries = -1 })
	key := reap.NewIdempotencyKey()
	req := reap.CreateCheckoutRequest{QuoteID: q.ID, EnrollmentID: enr, Presentation: reap.Presentation{ReturnURL: returnURL}}
	if _, err := nr.CreateCheckout(ctx, key, req); !reap.IsCode(err, reap.CodeServiceUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if _, n, _ := s.Counts(); n != 1 {
		t.Fatalf("checkouts = %d, want 1 (processed despite the 503)", n)
	}
	if _, err := nr.CreateCheckout(ctx, key, req); !reap.IsCode(err, reap.CodeServiceUnavailable) {
		t.Fatalf("replay while fault active: err = %v", err)
	}
	co, err := nr.CreateCheckout(ctx, key, req)
	if err != nil || co.ID != s.LatestCheckoutID() {
		t.Fatalf("replay: %+v %v", co, err)
	}
	if _, n, _ := s.Counts(); n != 1 {
		t.Fatalf("checkouts = %d after replay", n)
	}
}

func TestFaultDropConnection(t *testing.T) {
	s, _ := newFake(t, fakereap.Options{})
	s.InjectFault(fakereap.Fault{Path: "/agentic/products/search", Drop: true})
	nr := s.Client(func(o *reap.Options) { o.MaxRetries = -1 })
	_, err := nr.SearchProducts(ctx, reap.SearchRequest{Query: "pen"})
	var te *reap.TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v, want *reap.TransportError", err)
	}
	if _, err := nr.SearchProducts(ctx, reap.SearchRequest{Query: "pen"}); err != nil {
		t.Fatalf("fault should be consumed: %v", err)
	}
}
