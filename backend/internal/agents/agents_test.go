package agents

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func seeded(t *testing.T) *memstore.Store {
	t.Helper()
	st := memstore.New()
	if err := seed.Load(context.Background(), st, "../../seed", seed.Options{}); err != nil {
		t.Fatal(err)
	}
	return st
}

func seedCatalog(t *testing.T, st store.Store) []domain.CatalogItem {
	t.Helper()
	c, err := st.Catalog().List(context.Background(), store.CatalogFilter{ActiveOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	return c
}

func TestMatchCatalog_SeedCatalog(t *testing.T) {
	catalog := seedCatalog(t, seeded(t))
	cases := []struct {
		desc, suggested string
		wantSKU         string
		wantKind        MatchKind
	}{
		{"a4-copier-paper-ream", "", "a4-copier-paper-ream", MatchSKU},
		{"printer paper", "", "a4-copier-paper-ream", MatchExact},
		{"Coffee Pods", "", "coffee-capsules", MatchExact},
		{"coffee pods!", "", "coffee-capsules", MatchExact},
		{"some printer paper please", "", "a4-copier-paper-ream", MatchFuzzy},
		{"post it notes", "", "sticky-notes", MatchExact},
		{"standing desk", "", "", MatchOffList},
		{"standing desk", "stapler", "", MatchOffList}, // implausible LLM suggestion is ignored
		{"paper", "a4-copier-paper-ream", "a4-copier-paper-ream", MatchSKU},
		{"", "", "", MatchOffList},
		{"unobtainium widget", "", "", MatchOffList},
	}
	for _, tc := range cases {
		t.Run(tc.desc+"/"+tc.suggested, func(t *testing.T) {
			m := MatchCatalog(tc.desc, tc.suggested, catalog)
			got := ""
			if m.Item != nil {
				got = m.Item.SKU
			}
			if got != tc.wantSKU || m.Kind != tc.wantKind {
				t.Fatalf("got %q/%s (score %.2f), want %q/%s", got, m.Kind, m.Score, tc.wantSKU, tc.wantKind)
			}
		})
	}
}

func TestMatchCatalog_IgnoresInactive(t *testing.T) {
	c := []domain.CatalogItem{{ID: "1", SKU: "pen", Name: "Pen", Active: false}}
	if m := MatchCatalog("pen", "pen", c); m.Item != nil {
		t.Fatalf("matched inactive item: %+v", m)
	}
}

func TestPackHelpers(t *testing.T) {
	counts := []struct {
		in   string
		want int
	}{
		{"IK Signature Copier Paper 80g A4 500's", 500},
		{"box of 10 capsules", 10},
		{"Pack of 12 Pens", 12},
		{"Capsules 50 pcs", 50},
		{"Drip Bags El Diviso (6 pcs)", 6},
		{"Coffee Beans 250g", 1},
		{"PILOT Rexgrip Ballpoint Pen 0.7mm", 1},
		{"Paper towels x 6", 6},
		{"", 1},
	}
	for _, c := range counts {
		if got := PackCount(c.in); got != c.want {
			t.Errorf("PackCount(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	units := []struct {
		title, unit string
		want        int
	}{
		{"Daily Driver Capsules (10 pcs)", "box of 10 capsules", 1},
		{"Capsules value pack 50 pcs", "box of 10 capsules", 5},
		{"Capsules 15 pcs", "box of 10 capsules", 1}, // not a whole multiple
		{"IK Copier Paper 500's", "ream (500 sheets)", 1},
		{"IK Copier Paper 500's (1 Carton)", "carton (5 reams)", 1}, // unit count unknown -> 1 (never under-buy)
		{"Pilot pens pack of 12", "pen", 1},
	}
	for _, c := range units {
		if got := UnitsPerVariant(c.title, c.unit); got != c.want {
			t.Errorf("UnitsPerVariant(%q, %q) = %d, want %d", c.title, c.unit, got, c.want)
		}
	}
	for _, c := range []struct{ qty, pack, want int }{{10, 1, 10}, {10, 4, 3}, {10, 10, 1}, {0, 1, 1}, {7, 0, 7}} {
		if got := PurchaseQty(c.qty, c.pack); got != c.want {
			t.Errorf("PurchaseQty(%d,%d) = %d, want %d", c.qty, c.pack, got, c.want)
		}
	}
}

func vendorByDomain(t *testing.T, st store.Store, d string) domain.Vendor {
	t.Helper()
	v, err := st.Vendors().GetByDomain(context.Background(), d)
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func TestReapAdapter(t *testing.T) {
	st := seeded(t)
	srv := fakereap.New(fakereap.Options{})
	t.Cleanup(srv.Close)
	a := NewReapAdapter(srv.Client())
	allowed, err := st.Vendors().List(context.Background(), store.VendorFilter{AllowedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()

	t.Run("vendor ONLY search keeps that merchant and resolves variants", func(t *testing.T) {
		pop := vendorByDomain(t, st, "popular.com.sg")
		offers, err := a.Search(ctx, SearchQuery{Query: "A4 copier paper 80g 500 sheets", Vendor: pop, Limit: 5, CatalogUnit: "ream (500 sheets)"})
		if err != nil {
			t.Fatal(err)
		}
		if len(offers) == 0 {
			t.Fatal("no offers")
		}
		for _, o := range offers {
			if o.MerchantName != "Popular Bookstore" || o.VendorID == nil || *o.VendorID != pop.ID || o.ReapVariantID == "" ||
				o.UnitPriceCents <= 0 || o.Currency != "SGD" || !o.Available || o.PackSize < 1 {
				t.Fatalf("offer = %+v", o)
			}
			if strings.Contains(o.Title, "Double A") {
				t.Fatalf("unavailable product kept: %+v", o)
			}
		}
		reqs := srv.RequestsTo("POST", "/agentic/products/search")
		var body map[string]any
		_ = json.Unmarshal(reqs[len(reqs)-1].Body, &body)
		mp := body["merchantPreference"].(map[string]any)
		if mp["mode"] != "ONLY" || mp["merchantName"] != "popular.com.sg" {
			t.Fatalf("merchantPreference = %v", mp)
		}
		if len(srv.RequestsTo("POST", "/agentic/products/details")) == 0 {
			t.Fatal("details not called")
		}
	})
	t.Run("open search keeps only allow-listed merchants", func(t *testing.T) {
		offers, err := a.Search(ctx, SearchQuery{Query: "A4 copier paper", Open: true, Allowed: allowed, Limit: 10})
		if err != nil {
			t.Fatal(err)
		}
		if len(offers) == 0 {
			t.Fatal("no offers")
		}
		for _, o := range offers {
			if o.MerchantName == fakereap.MerchantMustafa || o.MerchantName == fakereap.MerchantStationeryPal || o.MerchantName == fakereap.MerchantWaangoo {
				t.Fatalf("non-allow-listed offer: %+v", o)
			}
		}
	})
	t.Run("anker '---' trusted only under ONLY", func(t *testing.T) {
		anker := vendorByDomain(t, st, "anker.com.sg")
		offers, err := a.Search(ctx, SearchQuery{Query: "Anker USB C charger 65W", Vendor: anker})
		if err != nil || len(offers) == 0 || *offers[0].VendorID != anker.ID {
			t.Fatalf("ONLY anker: %+v %v", offers, err)
		}
		open, err := a.Search(ctx, SearchQuery{Query: "Anker USB C charger 65W", Open: true, Allowed: allowed})
		if err != nil {
			t.Fatal(err)
		}
		for _, o := range open {
			if o.MerchantName == "---" {
				t.Fatalf("'---' offer kept in open search: %+v", o)
			}
		}
	})
	t.Run("unsearchable vendor", func(t *testing.T) {
		for _, v := range []domain.Vendor{{Domain: "x.sg", Allowed: false, ReapMerchantName: "X"}, {Domain: "y.sg", Allowed: true}} {
			if _, err := a.Search(ctx, SearchQuery{Query: "paper", Vendor: v}); !errors.Is(err, ErrVendorNotSearchable) {
				t.Fatalf("err = %v", err)
			}
		}
		if _, err := a.Search(ctx, SearchQuery{Query: "  ", Open: true, Allowed: allowed}); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("empty query err = %v", err)
		}
	})
	t.Run("upstream error surfaces", func(t *testing.T) {
		srv.InjectFault(fakereap.Fault{Path: "/agentic/products/search", Status: 400, Code: "AGENTIC_REQUEST_REJECTED"})
		if _, err := a.Search(ctx, SearchQuery{Query: "paper", Vendor: vendorByDomain(t, st, "popular.com.sg")}); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("details failure falls back to preview variant", func(t *testing.T) {
		srv.InjectFault(fakereap.Fault{Path: "/agentic/products/details", Status: 400, Code: "AGENTIC_REQUEST_REJECTED"})
		offers, err := a.Search(ctx, SearchQuery{Query: "A4 copier paper 80g 500 sheets", Vendor: vendorByDomain(t, st, "popular.com.sg")})
		if err != nil || len(offers) == 0 || offers[0].ReapVariantID == "" {
			t.Fatalf("offers %+v err %v", offers, err)
		}
	})
}

func TestMockAdapter(t *testing.T) {
	v := domain.Vendor{ID: "v1", Domain: "shop.sg", ReapMerchantName: "Shop", Allowed: true}
	m := &MockAdapter{Products: []MockProduct{
		{VendorDomain: "shop.sg", Title: "A4 Copier Paper 500's", PriceCents: 700},
		{VendorDomain: "shop.sg", Title: "Gone Paper", PriceCents: 100, Unavailable: true},
		{VendorDomain: "other.sg", Title: "A4 Copier Paper", PriceCents: 600},
	}}
	ctx := context.Background()
	got, err := m.Search(ctx, SearchQuery{Query: "copier paper", Vendor: v})
	if err != nil || len(got) != 1 || got[0].UnitPriceCents != 700 || *got[0].VendorID != "v1" {
		t.Fatalf("got %+v %v", got, err)
	}
	if got, _ := m.Search(ctx, SearchQuery{Query: "standing desk", Vendor: v}); len(got) != 0 {
		t.Fatalf("unexpected %+v", got)
	}
	m.Synthesize = true
	a, _ := m.Search(ctx, SearchQuery{Query: "standing desk", Vendor: v})
	b, _ := m.Search(ctx, SearchQuery{Query: "standing desk", Vendor: v})
	if len(a) != 1 || a[0].UnitPriceCents != b[0].UnitPriceCents {
		t.Fatalf("synthesised offers not deterministic: %+v %+v", a, b)
	}
	m.Fail = map[string]error{"shop.sg": errors.New("down")}
	if _, err := m.Search(ctx, SearchQuery{Query: "paper", Vendor: v}); err == nil {
		t.Fatal("want error")
	}
	if len(m.Calls) != 5 {
		t.Fatalf("calls = %d", len(m.Calls))
	}
}

func TestLLMParser(t *testing.T) {
	catalog := seedCatalog(t, seeded(t))
	ctx := context.Background()
	t.Run("rule-based fake on the PRD examples", func(t *testing.T) {
		p := NewLLMParser(fakellm.New())
		items, err := p.Parse(ctx, "We're out of printer paper and coffee pods, reorder the usual.", catalog)
		if err != nil {
			t.Fatal(err)
		}
		if len(items) != 2 {
			t.Fatalf("items = %+v", items)
		}
		for _, it := range items {
			if it.Qty != nil {
				t.Fatalf("'the usual' must leave qty empty: %+v", it)
			}
			if m := MatchCatalog(it.Description, it.CatalogSKU, catalog); m.Item == nil {
				t.Fatalf("%q did not match the catalog", it.Description)
			}
		}
		items, err = p.Parse(ctx, "Get us a standing desk for the new hire.", catalog)
		if err != nil || len(items) != 1 {
			t.Fatalf("items = %+v err %v", items, err)
		}
		if m := MatchCatalog(items[0].Description, items[0].CatalogSKU, catalog); m.Item != nil {
			t.Fatalf("standing desk matched %s", m.Item.SKU)
		}
	})
	t.Run("scripted structured output", func(t *testing.T) {
		f := fakellm.New().PushText(`{"items":[{"description":"pens","qty":20,"urgency":"urgent","catalog_sku":"ballpoint-pen"},{"description":"","qty":null,"urgency":"normal","catalog_sku":""}]}`)
		items, err := NewLLMParser(f).Parse(ctx, "20 pens asap", catalog)
		if err != nil || len(items) != 1 || *items[0].Qty != 20 || items[0].Urgency != domain.UrgencyUrgent || items[0].CatalogSKU != "ballpoint-pen" {
			t.Fatalf("items = %+v err %v", items, err)
		}
		req := f.Requests()[0]
		if req.ResponseSchemaName != llm.SchemaNameLineItems || len(req.ResponseSchema) == 0 {
			t.Fatalf("request = %+v", req)
		}
	})
	t.Run("errors", func(t *testing.T) {
		if _, err := NewLLMParser(fakellm.New()).Parse(ctx, "  ", catalog); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("empty: %v", err)
		}
		if _, err := NewLLMParser(fakellm.New().PushText("not json")).Parse(ctx, "x", catalog); !errors.Is(err, domain.ErrUpstream) {
			t.Fatalf("bad json: %v", err)
		}
		if _, err := NewLLMParser(fakellm.New().PushError(errors.New("boom"))).Parse(ctx, "x", catalog); err == nil {
			t.Fatal("want error")
		}
	})
}

func TestShortDescription(t *testing.T) {
	tests := []struct {
		in   string
		max  int
		want string
	}{
		{"", 10, ""},
		{"<p>Height-adjustable <b>standing desk</b>&nbsp;with dual motors.</p>", 300, "Height-adjustable standing desk with dual motors."},
		{"  many\n\n  spaces\there ", 300, "many spaces here"},
		{"one two three four five", 12, "one two…"},
		{"abcdefghijklmnop", 5, "abcde…"},
	}
	for _, tc := range tests {
		if got := ShortDescription(tc.in, tc.max); got != tc.want {
			t.Errorf("ShortDescription(%q, %d) = %q, want %q", tc.in, tc.max, got, tc.want)
		}
	}
}
