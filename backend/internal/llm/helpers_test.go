package llm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
)

func intp(n int) *int { return &n }

var hints = []llm.CatalogHint{
	{SKU: "a4-copier-paper-ream", Name: "A4 Copier Paper 80gsm (500 sheets)", Aliases: []string{"printer paper", "a4 paper"}, Unit: "ream (500 sheets)", DefaultQty: 10},
	{SKU: "ballpoint-pen", Name: "Ballpoint Pen 0.7mm", Aliases: []string{"pens", "biro"}, Unit: "pen", DefaultQty: 20},
}

func TestExtractLineItems_Sanitises(t *testing.T) {
	f := fakellm.New().PushText(`{"items":[
	  {"description":"  A4 paper ","qty":5,"urgency":"URGENT","catalog_sku":"a4-copier-paper-ream"},
	  {"description":"pens","qty":0,"urgency":"normal","catalog_sku":"made-up-sku"},
	  {"description":"","qty":3,"urgency":"normal","catalog_sku":null},
	  {"description":"stapler","qty":999999,"urgency":"whenever","catalog_sku":null}
	]}`)
	items, err := llm.ExtractLineItems(context.Background(), f, llm.ExtractInput{Utterance: "x", Catalog: hints})
	if err != nil {
		t.Fatal(err)
	}
	want := []llm.ExtractedItem{
		{Description: "A4 paper", Qty: intp(5), Urgency: domain.UrgencyUrgent, CatalogSKU: "a4-copier-paper-ream"},
		{Description: "pens", Urgency: domain.UrgencyNormal},
		{Description: "stapler", Urgency: domain.UrgencyNormal},
	}
	if g, w := mustJSON(items), mustJSON(want); g != w {
		t.Fatalf("items\n got %s\nwant %s", g, w)
	}
	reqs := f.Requests()
	if len(reqs) != 1 || reqs[0].ResponseSchemaName != llm.SchemaNameLineItems || len(reqs[0].ResponseSchema) == 0 {
		t.Fatalf("request = %+v", reqs)
	}
	var in llm.ExtractInput
	if err := json.Unmarshal([]byte(reqs[0].Messages[1].Content), &in); err != nil || in.Utterance != "x" || len(in.Catalog) != 2 {
		t.Fatalf("user payload = %q (%v)", reqs[0].Messages[1].Content, err)
	}
}

func TestExtractLineItems_Errors(t *testing.T) {
	cases := []struct {
		name string
		f    *fakellm.Fake
		utt  string
		want error
	}{
		{"empty utterance", fakellm.New(), "  ", domain.ErrValidation},
		{"bad json", fakellm.New().PushText("not json"), "x", domain.ErrUpstream},
		{"client error", fakellm.New().PushError(domain.ErrUpstream), "x", domain.ErrUpstream},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := llm.ExtractLineItems(context.Background(), tc.f, llm.ExtractInput{Utterance: tc.utt})
			if !errors.Is(err, tc.want) {
				t.Fatalf("err = %v want %v", err, tc.want)
			}
		})
	}
}

func TestExtractLineItems_RuleBasedFake(t *testing.T) {
	items, err := llm.ExtractLineItems(context.Background(), fakellm.New(), llm.ExtractInput{
		Utterance: "We need 5 reams of printer paper and the usual pens, urgently", Catalog: hints,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items = %s", mustJSON(items))
	}
	if items[0].CatalogSKU != "a4-copier-paper-ream" || items[0].Qty == nil || *items[0].Qty != 5 || items[0].Urgency != domain.UrgencyUrgent {
		t.Errorf("item 0 = %s", mustJSON(items[0]))
	}
	if items[1].CatalogSKU != "ballpoint-pen" || items[1].Qty != nil {
		t.Errorf("item 1 = %s", mustJSON(items[1]))
	}
}

func TestCatalogHints_SkipsInactive(t *testing.T) {
	got := llm.CatalogHints([]domain.CatalogItem{
		{SKU: "a", Name: "A", Active: true, Unit: "u", DefaultQty: 2, Aliases: []string{"x"}},
		{SKU: "b", Name: "B", Active: false},
	})
	if len(got) != 1 || got[0].SKU != "a" || got[0].DefaultQty != 2 || got[0].Aliases[0] != "x" {
		t.Fatalf("hints = %+v", got)
	}
}

func TestMatchOffers_ComputesUnitPrice(t *testing.T) {
	target := llm.MatchTarget{SKU: "ballpoint-pen", Name: "Ballpoint Pen 0.7mm", Unit: "pen", MaxUnitPriceCents: 250}
	offers := []llm.OfferCandidate{
		{Title: "PILOT Rexgrip Ballpoint Pen 0.7mm", PriceCents: 155},
		{Title: "Ballpoint pens pack of 12", PriceCents: 1200},
		{Title: "Luxury fountain pen", PriceCents: 9900},
		{Title: "Unjudged pen", PriceCents: 300},
	}
	// Model: index 0 match pack 1; index 1 match pack 12; index 2 no match; index 3 skipped;
	// out-of-range and duplicate entries are ignored; bad pack size falls back to the hint.
	f := fakellm.New().PushText(`{"matches":[
	  {"index":0,"is_match":true,"pack_size":1,"reason":"same pen"},
	  {"index":1,"is_match":true,"pack_size":12,"reason":"12 pens"},
	  {"index":1,"is_match":false,"pack_size":1,"reason":"duplicate ignored"},
	  {"index":2,"is_match":false,"pack_size":0,"reason":"fountain pen"},
	  {"index":9,"is_match":true,"pack_size":1,"reason":"out of range"}
	]}`)
	got, err := llm.MatchOffers(context.Background(), f, target, offers)
	if err != nil {
		t.Fatal(err)
	}
	want := []struct {
		match  bool
		pack   int
		unit   domain.Cents
		within bool
	}{
		{true, 1, 155, true},
		{true, 12, 100, true},
		{false, 1, 9900, false},
		{false, 1, 300, false},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d results", len(got))
	}
	for i, w := range want {
		g := got[i]
		if g.Index != i || g.IsMatch != w.match || g.PackSize != w.pack || g.UnitPriceCents != w.unit || g.WithinMaxUnitPrice != w.within {
			t.Errorf("offer %d = %+v, want %+v", i, g, w)
		}
	}
	if got[1].PackSizeHint != 12 {
		t.Errorf("hint for pack of 12 = %d", got[1].PackSizeHint)
	}
	// Prices and price ceilings are never sent to the model.
	payload := f.Requests()[0].Messages[1].Content
	if strings.Contains(payload, "1200") || strings.Contains(payload, "max_unit") {
		t.Errorf("payload leaks prices: %s", payload)
	}
}

func TestMatchOffers_EmptyAndErrors(t *testing.T) {
	got, err := llm.MatchOffers(context.Background(), fakellm.New(), llm.MatchTarget{Name: "x"}, nil)
	if err != nil || got != nil {
		t.Fatalf("empty = %v %v", got, err)
	}
	_, err = llm.MatchOffers(context.Background(), fakellm.New().PushText("{bad"), llm.MatchTarget{Name: "x"}, []llm.OfferCandidate{{Title: "x"}})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
}

func TestMatchOffers_RuleBasedFake(t *testing.T) {
	target := llm.TargetFromCatalog(domain.CatalogItem{
		SKU: "a4-copier-paper-ream", Name: "A4 Copier Paper 80gsm (500 sheets)", Aliases: []string{"copy paper"},
		Unit: "ream (500 sheets)", MaxUnitPriceCents: 900,
	})
	got, err := llm.MatchOffers(context.Background(), fakellm.New(), target, []llm.OfferCandidate{
		{Title: "IK Signature Copier Paper 80g A4 500's", PriceCents: 710},
		{Title: "PaperOne A4 Copier Paper 80gsm, 5 reams", PriceCents: 3950},
		{Title: "Stapler No. 10", PriceCents: 450},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !got[0].IsMatch || got[0].PackSize != 1 || got[0].UnitPriceCents != 710 || !got[0].WithinMaxUnitPrice {
		t.Errorf("offer 0 = %+v", got[0])
	}
	if !got[1].IsMatch || got[1].PackSize != 5 || got[1].UnitPriceCents != 790 {
		t.Errorf("offer 1 = %+v", got[1])
	}
	if got[2].IsMatch {
		t.Errorf("stapler matched paper: %+v", got[2])
	}
}

func TestUnitPrice(t *testing.T) {
	cases := []struct {
		price domain.Cents
		pack  int
		want  domain.Cents
	}{
		{1000, 1, 1000}, {1000, 0, 1000}, {1000, 3, 333}, {1001, 2, 501}, {5, 10, 1}, {3950, 5, 790},
	}
	for _, tc := range cases {
		if got := llm.UnitPrice(tc.price, tc.pack); got != tc.want {
			t.Errorf("UnitPrice(%d,%d) = %d want %d", tc.price, tc.pack, got, tc.want)
		}
	}
}

func TestGuessPackSize(t *testing.T) {
	cases := []struct {
		title, unit string
		want        int
	}{
		{"PILOT Rexgrip Ballpoint Pen 0.7mm", "pen", 1},
		{"Ballpoint pens pack of 12", "pen", 12},
		{"Gel pen 10 pcs", "pen", 10},
		{"Highlighter x6", "pen", 6},
		{"Highlighter 4 x", "pen", 4},
		{"IK Signature Copier Paper 80g A4 500's", "ream (500 sheets)", 1},
		{"PaperOne A4 80gsm 5 reams", "ream (500 sheets)", 5},
		{"IK Copier Paper 80g A4 500's (1 Carton)", "carton (5 reams)", 1},
		{"Post-it Super Sticky Notes 3x3 5 pads", "pack of 5 pads", 1},
		{"Post-it Super Sticky Notes 10 pads", "pack of 5 pads", 2},
		{"Post-it Notes 3x3in", "pack of 5 pads", 1},
		{"Common Man Coffee Beans 1kg", "bag (1kg)", 1},
		{"Nespresso compatible capsules 50 count", "capsule", 50},
		{"Tissue box 2000 sheets", "box", 1},
		{"", "pen", 1},
	}
	for _, tc := range cases {
		t.Run(tc.title, func(t *testing.T) {
			if got := llm.GuessPackSize(tc.title, tc.unit); got != tc.want {
				t.Errorf("GuessPackSize(%q, %q) = %d want %d", tc.title, tc.unit, got, tc.want)
			}
		})
	}
}

func TestNormalizeText(t *testing.T) {
	cases := map[string]string{
		"Ballpoint Pens, 0.7mm!": "ballpoint pen 0 7mm",
		"Boxes of Batteries":     "box of battery",
		"Glass":                  "glass",
		"Post-its":               "post its",
	}
	for in, want := range cases {
		if got := llm.NormalizeText(in); got != want {
			t.Errorf("NormalizeText(%q) = %q want %q", in, got, want)
		}
	}
	if !llm.ContainsPhrase("a4 copier paper", "copier paper") || llm.ContainsPhrase("a4 copier paper", "copier pap") {
		t.Error("ContainsPhrase word boundaries")
	}
}

func TestRedactKeys(t *testing.T) {
	msg := "Incorrect API key provided: sk-proj-abc***********xyz. You can find your key..."
	if got := llm.RedactKeys(msg); strings.Contains(got, "sk-proj") || !strings.Contains(got, "[redacted]") {
		t.Fatalf("RedactKeys = %q", got)
	}
}

func mustJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(err)
	}
	return string(b)
}
