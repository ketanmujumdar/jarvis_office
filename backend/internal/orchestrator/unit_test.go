package orchestrator

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
)

func sp(s string) *string { return &s }

func TestRankOffers(t *testing.T) {
	prio := map[string]int{"pref": 0, "other": 150}
	cases := []struct {
		name   string
		qty    int
		offers []domain.Offer
		want   []string // titles in rank order
		landed []domain.Cents
	}{
		{
			name: "landed cost first",
			qty:  2,
			offers: []domain.Offer{
				{Title: "B", UnitPriceCents: 500, ShippingCents: 400, Available: true, VendorID: sp("pref")},
				{Title: "A", UnitPriceCents: 600, Available: true, VendorID: sp("other")},
			},
			want: []string{"A", "B"}, landed: []domain.Cents{1200, 1400},
		},
		{
			name: "ETA presence breaks ties",
			qty:  1,
			offers: []domain.Offer{
				{Title: "A", UnitPriceCents: 500, Available: true, VendorID: sp("pref")},
				{Title: "B", UnitPriceCents: 500, ETA: "tomorrow", Available: true, VendorID: sp("other")},
			},
			want: []string{"B", "A"}, landed: []domain.Cents{500, 500},
		},
		{
			name: "vendor preference then title",
			qty:  1,
			offers: []domain.Offer{
				{Title: "Z", UnitPriceCents: 500, Available: true, VendorID: sp("other")},
				{Title: "Y", UnitPriceCents: 500, Available: true},
				{Title: "X", UnitPriceCents: 500, Available: true, VendorID: sp("pref")},
				{Title: "W", UnitPriceCents: 500, Available: true, VendorID: sp("pref")},
			},
			want: []string{"W", "X", "Z", "Y"}, landed: []domain.Cents{500, 500, 500, 500},
		},
		{
			name: "pack size normalised (10 units wanted)",
			qty:  10,
			offers: []domain.Offer{
				{Title: "single", UnitPriceCents: 100, PackSize: 1, Available: true},
				{Title: "pack of 4", UnitPriceCents: 350, PackSize: 4, Available: true}, // 3 packs = 1050
				{Title: "pack of 10", UnitPriceCents: 900, PackSize: 10, Available: true},
			},
			want: []string{"pack of 10", "single", "pack of 4"}, landed: []domain.Cents{900, 1000, 1050},
		},
		{
			name: "unavailable last",
			qty:  1,
			offers: []domain.Offer{
				{Title: "cheap but gone", UnitPriceCents: 1, Available: false},
				{Title: "ok", UnitPriceCents: 999, Available: true},
			},
			want: []string{"ok", "cheap but gone"}, landed: []domain.Cents{999, 1},
		},
		{name: "empty", qty: 1, offers: nil, want: nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := slices.Clone(tc.offers)
			got := RankOffers(tc.offers, tc.qty, prio)
			var titles []string
			for i, o := range got {
				titles = append(titles, o.Title)
				if o.Rank != i+1 {
					t.Fatalf("rank %d at %d", o.Rank, i)
				}
				if o.LandedCostCents != tc.landed[i] {
					t.Fatalf("%s landed %d, want %d", o.Title, o.LandedCostCents, tc.landed[i])
				}
			}
			if !slices.Equal(titles, tc.want) {
				t.Fatalf("order %v, want %v", titles, tc.want)
			}
			for i := range in {
				if in[i].Rank != 0 || in[i].LandedCostCents != 0 {
					t.Fatal("input mutated")
				}
			}
		})
	}
}

func TestPlanSearch(t *testing.T) {
	vendors := []domain.Vendor{
		{ID: "v-pop", Domain: "popular.com.sg", ReapMerchantName: "Popular Bookstore", Allowed: true, OfficeRelevant: true, Priority: 10, Category: "Books & Stationery"},
		{ID: "v-kino", Domain: "kinokuniya.com.sg", ReapMerchantName: "Books Kinokuniya", Allowed: true, OfficeRelevant: true, Priority: 20},
		{ID: "v-ergo", Domain: "ergotune.com", ReapMerchantName: "ErgoTune", Allowed: true, OfficeRelevant: true, Priority: 50, Notes: "ergonomic chairs, monitor arms, desks"},
		{ID: "v-none", Domain: "greatjonesgoods.com", Allowed: true, OfficeRelevant: true}, // no merchant name: not searchable
		{ID: "v-gift", Domain: "gifts.sg", ReapMerchantName: "Gifts", Allowed: true, OfficeRelevant: false},
	}
	paper := &domain.CatalogItem{ID: "c1", SearchQuery: "A4 copier paper", Unit: "ream (500 sheets)", PreferredVendorIDs: []string{"v-kino", "v-pop", "v-none", "v-missing"}}
	li := domain.LineItem{ID: "l1", Description: "A4 Copier Paper", Qty: 10}

	t.Run("catalog item: preferred vendors in order, then open", func(t *testing.T) {
		tasks := PlanSearch(li, paper, vendors, 5, 6)
		var labels []string
		for _, tk := range tasks {
			labels = append(labels, tk.Label)
			if tk.Query.Query != "A4 copier paper" || tk.Query.CatalogUnit != "ream (500 sheets)" || tk.Query.Limit != 5 || tk.Query.Qty != 10 {
				t.Fatalf("query = %+v", tk.Query)
			}
		}
		if !slices.Equal(labels, []string{"kinokuniya.com.sg", "popular.com.sg", OpenSearchLabel}) {
			t.Fatalf("labels = %v", labels)
		}
		open := tasks[len(tasks)-1].Query
		if !open.Open || len(open.Allowed) != len(vendors) {
			t.Fatalf("open task = %+v", open)
		}
	})
	t.Run("off-list: relevant office vendors first, capped", func(t *testing.T) {
		desk := domain.LineItem{ID: "l2", Description: "standing desk", Qty: 1}
		tasks := PlanSearch(desk, nil, vendors, 5, 2)
		var labels []string
		for _, tk := range tasks {
			labels = append(labels, tk.Label)
			if tk.Query.Query != "standing desk" {
				t.Fatalf("query %q", tk.Query.Query)
			}
		}
		if !slices.Equal(labels, []string{"ergotune.com", "popular.com.sg", OpenSearchLabel}) {
			t.Fatalf("labels = %v", labels)
		}
	})
}

func TestVendorPriority(t *testing.T) {
	vs := []domain.Vendor{{ID: "a", Priority: 5}, {ID: "b", Priority: 1}}
	p := VendorPriority(&domain.CatalogItem{PreferredVendorIDs: []string{"a"}}, vs)
	if p["a"] != 0 || p["b"] != 101 {
		t.Fatalf("priority = %v", p)
	}
}

func TestBuildLineItems(t *testing.T) {
	catalog := []domain.CatalogItem{
		{ID: "c-paper", SKU: "a4-copier-paper-ream", Name: "A4 Copier Paper", Aliases: []string{"printer paper"}, DefaultQty: 10, Active: true},
		{ID: "c-pods", SKU: "coffee-capsules", Name: "Coffee Capsules", Aliases: []string{"coffee pods"}, DefaultQty: 6, Active: true},
	}
	items := BuildLineItems("r1", []agents.ParsedItem{
		{Description: "printer paper"},
		{Description: "coffee pods", Qty: ptr(3), Urgency: domain.UrgencyUrgent},
		{Description: "standing desk"},
		{Description: "printer paper", Qty: ptr(2)},
	}, catalog)
	if len(items) != 3 {
		t.Fatalf("items = %d", len(items))
	}
	want := []struct {
		cat  string
		desc string
		qty  int
		urg  domain.Urgency
	}{
		{"c-paper", "A4 Copier Paper", 12, domain.UrgencyNormal},
		{"c-pods", "Coffee Capsules", 3, domain.UrgencyUrgent},
		{"", "standing desk", 1, domain.UrgencyNormal},
	}
	for i, w := range want {
		it := items[i]
		cat := ""
		if it.CatalogItemID != nil {
			cat = *it.CatalogItemID
		}
		if cat != w.cat || it.Description != w.desc || it.Qty != w.qty || it.Urgency != w.urg || it.Position != i || it.RequestID != "r1" {
			t.Fatalf("item %d = %+v", i, it)
		}
	}
}

// Partial results: one vendor errors, one never answers before the deadline; the request is still
// ranked from what arrived.
func TestSearch_DeadlineAndPartialResults(t *testing.T) {
	mock := &agents.MockAdapter{
		Products: []agents.MockProduct{
			{VendorDomain: "kinokuniya.com.sg", Title: "Kinokuniya A4 Copier Paper 80gsm 500 sheets", PriceCents: 850},
		},
		Fail: map[string]error{"open": errors.New("open search exploded")},
		Slow: map[string]bool{"popular.com.sg": true},
	}
	e := newEnv(t, withoutEnrollment(), withAdapter(mock), withDeadline(150*time.Millisecond))
	start := time.Now()
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	if el := time.Since(start); el > 5*time.Second {
		t.Fatalf("search took %v", el)
	}
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	best := offerByID(d, d.LineItems[0].SelectedOfferID)
	if best.UnitPriceCents != 850 || d.LineItems[0].PolicyDecision != domain.DecisionAutoApprove {
		t.Fatalf("best = %+v decision %s", best, d.LineItems[0].PolicyDecision)
	}
	e.evMu.Lock()
	defer e.evMu.Unlock()
	results := map[string]string{}
	for _, ev := range e.events {
		if ev.Type == events.SearchVendorResult && ev.RequestID == r.ID {
			m := ev.Data.(map[string]any)
			errS, _ := m["error"].(string)
			results[m["vendor_domain"].(string)] = errS
		}
	}
	if results["open"] == "" || results["kinokuniya.com.sg"] != "" {
		t.Fatalf("vendor results = %v", results)
	}
}

func TestPollCheckoutJob(t *testing.T) {
	e := newEnv(t)
	base := e.q.Scheduled() // the enrollment poll from setup
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if e.q.Scheduled()-base != 1 {
		t.Fatalf("scheduled polls = %d, want 1", e.q.Scheduled())
	}
	// Still awaiting: the poll reschedules itself.
	if err := e.o.handlePollCheckout(e.ctx, queue.Job{RequestID: r.ID}); err != nil {
		t.Fatal(err)
	}
	if e.q.Scheduled()-base != 2 {
		t.Fatalf("scheduled polls = %d, want 2", e.q.Scheduled()-base)
	}
	e.approveCheckouts(d)
	if err := e.o.handlePollCheckout(e.ctx, queue.Job{RequestID: r.ID}); err != nil {
		t.Fatal(err)
	}
	e.wantStatus(r.ID, domain.StatusOrdered)
	if e.q.Scheduled()-base != 2 {
		t.Fatalf("poll rescheduled after ordered: %d", e.q.Scheduled()-base)
	}
	if err := e.o.handlePollCheckout(e.ctx, queue.Job{RequestID: "00000000-0000-4000-8000-000000000009"}); !queue.IsPermanent(err) {
		t.Fatalf("unknown request poll err = %v", err)
	}
}

func TestEnrollmentPolling(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	if _, err := e.o.CurrentEnrollment(e.ctx); !errors.Is(err, domain.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
	en, err := e.o.StartEnrollment(e.ctx, e.manager)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.StartEnrollment(e.ctx, domain.User{ID: "x"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("no email: %v", err)
	}
	if _, err := e.o.StartEnrollment(e.ctx, domain.User{ID: "x", Email: "ops@corp.example"}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("reserved email domain: %v", err)
	}
	job := e.o.enrollmentPollJob(en.ID)
	if err := e.o.handlePollEnrollment(e.ctx, job); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.Enrollments().Get(e.ctx, en.ID); got.Status != domain.EnrollmentRequiresAction {
		t.Fatalf("status = %s", got.Status)
	}
	if err := e.reapSrv.ActivateEnrollment(en.ReapEnrollmentID); err != nil {
		t.Fatal(err)
	}
	before := e.q.Scheduled()
	if err := e.o.handlePollEnrollment(e.ctx, job); err != nil {
		t.Fatal(err)
	}
	if got, _ := e.st.Enrollments().Get(e.ctx, en.ID); got.Status != domain.EnrollmentActive {
		t.Fatalf("status = %s", got.Status)
	}
	if e.q.Scheduled() != before {
		t.Fatal("active enrollment poll rescheduled")
	}
}

func TestOnApprovalDecided_IgnoresStaleRequest(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	r := e.create("", agents.RequestedItem{Description: "printer paper"})
	if err := e.o.OnApprovalDecided(context.Background(), domain.Approval{RequestID: r.ID, Status: domain.ApprovalApproved}); err != nil {
		t.Fatal(err)
	}
	e.wantStatus(r.ID, domain.StatusQuoted)
}

// Text/voice tools drive the orchestrator end to end (create -> status -> addresses -> confirm).
func TestToolsBackend(t *testing.T) {
	e := newEnv(t)
	tools := agents.NewTools(e.o.Tools(), e.st, nil)
	tc := agents.ToolContext{UserID: e.manager.ID, SessionID: "s1", Channel: "voice"}
	exec := func(name, args string) map[string]any {
		t.Helper()
		out, err := tools.Execute(e.ctx, tc, name, []byte(args))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := jsonUnmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	created := exec(agents.ToolCreateOrderRequest, `{"utterance":"We're out of printer paper and coffee pods, reorder the usual."}`)
	id, _ := created["request_id"].(string)
	if id == "" {
		t.Fatalf("create = %v", created)
	}
	e.idle()
	st := exec(agents.ToolGetRequestStatus, `{"request_id":"`+id+`"}`)
	if st["status"] != "quoted" || len(st["lines"].([]any)) != 2 {
		t.Fatalf("status = %v", st)
	}
	addrs := exec(agents.ToolListAddresses, `{}`)["addresses"].([]any)
	if len(addrs) < 3 {
		t.Fatalf("addresses = %v", addrs)
	}
	noAddr := exec(agents.ToolConfirmOrder, `{"request_id":"`+id+`"}`)
	if noAddr["code"] != "validation_failed" {
		t.Fatalf("confirm without address = %v", noAddr)
	}
	conf := exec(agents.ToolConfirmOrder, `"{\"request_id\":\"`+id+`\",\"address_id\":\"`+e.addr.ID+`\"}"`)
	if conf["error"] != nil {
		t.Fatalf("confirm = %v", conf)
	}
	e.idle()
	st = exec(agents.ToolGetRequestStatus, `{"request_id":"`+id+`"}`)
	if st["status"] != "awaiting_payment" || len(st["payments"].([]any)) != 2 {
		t.Fatalf("status = %v", st)
	}
	// Reap has no checkout-cancel API: once the payment links are open the request cannot be
	// cancelled (the link could still be paid while Jarvis shows "cancelled").
	cancelled := exec(agents.ToolCancelRequest, `{"request_id":"`+id+`"}`)
	if cancelled["code"] != "invalid_transition" {
		t.Fatalf("cancel = %v", cancelled)
	}
}
