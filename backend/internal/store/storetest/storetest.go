// Package storetest is a conformance suite for store.Store implementations. The Postgres store
// (integration, needs TEST_DATABASE_URL) and the in-memory store (offline) both run it, which keeps
// memstore's behaviour faithful to production for other packages' unit tests.
package storetest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Env is one fresh, empty store plus implementation-specific hooks.
type Env struct {
	Store store.Store
	// SetCompletedAt moves a completed payment's completion time (for monthly spend tests).
	SetCompletedAt func(t *testing.T, paymentID string, at time.Time)
}

// Factory returns a fresh, empty Env for each call.
type Factory func(t *testing.T) Env

// Run runs every conformance test against f.
func Run(t *testing.T, f Factory) {
	tests := []struct {
		name string
		fn   func(t *testing.T, e Env)
	}{
		{"Users", testUsers},
		{"Vendors", testVendors},
		{"Catalog", testCatalog},
		{"Policy", testPolicy},
		{"Addresses", testAddresses},
		{"SystemPrompts", testSystemPrompts},
		{"Enrollments", testEnrollments},
		{"Requests", testRequests},
		{"TransitionStatus", testTransitionStatus},
		{"TransitionRace", testTransitionRace},
		{"LineItemsAndOffers", testLineItemsAndOffers},
		{"Approvals", testApprovals},
		{"Payments", testPayments},
		{"Audit", testAudit},
		{"Chat", testChat},
		{"Spend", testSpend},
		{"WithTx", testWithTx},
		{"Detail", testDetail},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) { tt.fn(t, f(t)) })
	}
}

var ctx = context.Background()

const missingID = "00000000-0000-4000-8000-000000000000"

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func wantErr(t *testing.T, err, target error) {
	t.Helper()
	if !errors.Is(err, target) {
		t.Fatalf("want %v, got %v", target, err)
	}
}

func ptr[T any](v T) *T { return &v }

// ---- fixtures ----

func mkUser(t *testing.T, s store.Store, email string, role domain.Role) domain.User {
	t.Helper()
	u := domain.User{Name: "User " + email, Email: email, Role: role}
	must(t, s.Users().Upsert(ctx, &u))
	return u
}

func mkVendor(t *testing.T, s store.Store, dom, merchant string, prio int) domain.Vendor {
	t.Helper()
	v := domain.Vendor{Domain: dom, Name: dom, ReapMerchantName: merchant, Category: "Office", Allowed: true, Priority: prio}
	must(t, s.Vendors().Create(ctx, &v))
	return v
}

func mkAddress(t *testing.T, s store.Store, label string, def bool) domain.Address {
	t.Helper()
	a := domain.Address{Label: label, FirstName: "Maya", LastName: "Tan", Phone: "+6562001001", Email: "m@x.example",
		AddressLine1: "7 Straits View", PostalCode: "018936", IsDefault: def}
	must(t, s.Addresses().Create(ctx, &a))
	return a
}

func mkRequest(t *testing.T, s store.Store, requester string) domain.PurchaseRequest {
	t.Helper()
	r := domain.PurchaseRequest{RequesterID: requester, RawUtterance: "order paper"}
	must(t, s.Requests().Create(ctx, &r))
	return r
}

func mkPayment(t *testing.T, s store.Store, reqID, key string, st domain.PaymentStatus, final *domain.Cents) domain.Payment {
	t.Helper()
	// One merchant per payment: at most one open payment per request and merchant is allowed.
	p := domain.Payment{RequestID: reqID, MerchantName: "Merchant " + key, IdempotencyKey: key, Status: st, QuotedCents: 1000, FinalCents: final}
	must(t, s.Payments().Create(ctx, &p))
	return p
}

// ---- tests ----

func testUsers(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	if u.ID == "" || u.CreatedAt.IsZero() {
		t.Fatalf("server fields not filled: %+v", u)
	}
	again := domain.User{Name: "Maya T", Email: "maya@x.example", Role: domain.RoleAdmin}
	must(t, s.Users().Upsert(ctx, &again))
	if again.ID != u.ID || again.Role != domain.RoleAdmin || again.Name != "Maya T" {
		t.Fatalf("upsert by email: %+v", again)
	}
	got, err := s.Users().GetByEmail(ctx, "MAYA@x.example")
	must(t, err)
	if got.ID != u.ID {
		t.Fatal("GetByEmail should be case-insensitive")
	}
	mkUser(t, s, "daniel@x.example", domain.RoleApprover)
	list, err := s.Users().List(ctx)
	must(t, err)
	if len(list) != 2 {
		t.Fatalf("list = %d", len(list))
	}
	for _, id := range []string{missingID, "not-a-uuid", ""} {
		_, err = s.Users().Get(ctx, id)
		wantErr(t, err, domain.ErrNotFound)
	}
	_, err = s.Users().GetByEmail(ctx, "nobody@x.example")
	wantErr(t, err, domain.ErrNotFound)
	bad := domain.User{Name: "x", Email: "bad@x.example", Role: "boss"}
	wantErr(t, s.Users().Upsert(ctx, &bad), domain.ErrValidation)
}

func testVendors(t *testing.T, e Env) {
	s := e.Store
	a := mkVendor(t, s, "Popular.com.sg", "Popular Bookstore SG", 10)
	if a.Domain != "popular.com.sg" || a.Country != "SG" {
		t.Fatalf("normalisation: %+v", a)
	}
	b := mkVendor(t, s, "anker.com.sg", "---", 5)
	dup := domain.Vendor{Domain: "popular.com.sg", Name: "dup"}
	wantErr(t, s.Vendors().Create(ctx, &dup), domain.ErrConflict)

	c := domain.Vendor{Domain: "greatjonesgoods.com", Name: "GJG", Category: "Kitchen", Allowed: false, Priority: 1}
	must(t, s.Vendors().Upsert(ctx, &c))
	c2 := c
	c2.ID = ""
	c2.Notes = "not found in sandbox"
	must(t, s.Vendors().Upsert(ctx, &c2))
	if c2.ID != c.ID || c2.Notes != "not found in sandbox" {
		t.Fatalf("upsert by domain: %+v", c2)
	}

	all, err := s.Vendors().List(ctx, store.VendorFilter{})
	must(t, err)
	if len(all) != 3 || all[0].ID != c.ID || all[1].ID != b.ID || all[2].ID != a.ID {
		t.Fatalf("list order by priority: %+v", all)
	}
	allowed, err := s.Vendors().List(ctx, store.VendorFilter{AllowedOnly: true, Category: "Office"})
	must(t, err)
	if len(allowed) != 2 {
		t.Fatalf("filtered list: %d", len(allowed))
	}
	none, err := s.Vendors().List(ctx, store.VendorFilter{Category: "Nope"})
	must(t, err)
	if none == nil || len(none) != 0 {
		t.Fatalf("empty list must be non-nil: %#v", none)
	}

	got, err := s.Vendors().GetByMerchantName(ctx, "---")
	must(t, err)
	if got.ID != b.ID {
		t.Fatal("GetByMerchantName")
	}
	_, err = s.Vendors().GetByMerchantName(ctx, "popular bookstore sg") // exact, case-sensitive
	wantErr(t, err, domain.ErrNotFound)
	_, err = s.Vendors().GetByMerchantName(ctx, "")
	wantErr(t, err, domain.ErrNotFound)
	got, err = s.Vendors().GetByDomain(ctx, "POPULAR.com.sg")
	must(t, err)
	if got.ID != a.ID {
		t.Fatal("GetByDomain")
	}

	a.Priority, a.Notes = 1, "edited"
	must(t, s.Vendors().Update(ctx, &a))
	got, err = s.Vendors().Get(ctx, a.ID)
	must(t, err)
	if got.Priority != 1 || got.Notes != "edited" || got.UpdatedAt.Before(got.CreatedAt) {
		t.Fatalf("update: %+v", got)
	}
	a.Domain = "anker.com.sg"
	wantErr(t, s.Vendors().Update(ctx, &a), domain.ErrConflict)
	ghost := domain.Vendor{ID: missingID, Domain: "ghost.example"}
	wantErr(t, s.Vendors().Update(ctx, &ghost), domain.ErrNotFound)

	// Deleting a vendor removes it from catalog preferences.
	it := domain.CatalogItem{SKU: "x", Name: "X", Category: "C", DefaultQty: 1, MaxUnitPriceCents: 100,
		PreferredVendorIDs: []string{b.ID, c.ID}}
	must(t, s.Catalog().Create(ctx, &it))
	must(t, s.Vendors().Delete(ctx, b.ID))
	wantErr(t, s.Vendors().Delete(ctx, b.ID), domain.ErrNotFound)
	it, err = s.Catalog().Get(ctx, it.ID)
	must(t, err)
	if len(it.PreferredVendorIDs) != 1 || it.PreferredVendorIDs[0] != c.ID {
		t.Fatalf("cascade: %v", it.PreferredVendorIDs)
	}
}

func testCatalog(t *testing.T, e Env) {
	s := e.Store
	v1 := mkVendor(t, s, "popular.com.sg", "Popular", 10)
	v2 := mkVendor(t, s, "kinokuniya.com.sg", "Kinokuniya", 20)
	paper := domain.CatalogItem{SKU: "a4-ream", Name: "A4 Copier Paper", Aliases: []string{"printer paper", "copy paper"},
		Category: "Paper & Stationery", Unit: "ream", DefaultQty: 10, MaxUnitPriceCents: 900,
		PreferredVendorIDs: []string{v2.ID, v1.ID}, AutoApprove: true, SearchQuery: "A4 paper", Active: true}
	must(t, s.Catalog().Create(ctx, &paper))
	if paper.ID == "" || len(paper.PreferredVendorIDs) != 2 || paper.PreferredVendorIDs[0] != v2.ID {
		t.Fatalf("create: %+v", paper)
	}
	coffee := domain.CatalogItem{SKU: "coffee-beans", Name: "Coffee Beans 1kg", Category: "Coffee & Tea", DefaultQty: 2,
		MaxUnitPriceCents: 6000, Active: false}
	must(t, s.Catalog().Create(ctx, &coffee))
	if coffee.Aliases == nil || coffee.PreferredVendorIDs == nil {
		t.Fatal("nil slices must come back empty")
	}
	dup := domain.CatalogItem{SKU: "a4-ream", Name: "dup", Category: "x", DefaultQty: 1, MaxUnitPriceCents: 1}
	wantErr(t, s.Catalog().Create(ctx, &dup), domain.ErrConflict)
	badVendor := domain.CatalogItem{SKU: "bad", Name: "bad", Category: "x", DefaultQty: 1, MaxUnitPriceCents: 1,
		PreferredVendorIDs: []string{missingID}}
	wantErr(t, s.Catalog().Create(ctx, &badVendor), domain.ErrValidation)
	noQty := domain.CatalogItem{SKU: " pens ", Name: "Pens", Category: "Paper & Stationery", MaxUnitPriceCents: 500}
	must(t, s.Catalog().Create(ctx, &noQty))
	if noQty.DefaultQty != 1 || noQty.SKU != "pens" {
		t.Fatalf("defaults: %+v", noQty)
	}
	must(t, s.Catalog().Delete(ctx, noQty.ID))

	tests := []struct {
		name string
		f    store.CatalogFilter
		want []string
	}{
		{"all ordered by category then name", store.CatalogFilter{}, []string{"coffee-beans", "a4-ream"}},
		{"active only", store.CatalogFilter{ActiveOnly: true}, []string{"a4-ream"}},
		{"category", store.CatalogFilter{Category: "Coffee & Tea"}, []string{"coffee-beans"}},
		{"query alias", store.CatalogFilter{Query: "PRINTER"}, []string{"a4-ream"}},
		{"query sku", store.CatalogFilter{Query: "beans"}, []string{"coffee-beans"}},
		{"query like metachar", store.CatalogFilter{Query: "%"}, []string{}},
		{"no match", store.CatalogFilter{Query: "stapler"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Catalog().List(ctx, tt.f)
			must(t, err)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d items, want %v", len(got), tt.want)
			}
			for i := range got {
				if got[i].SKU != tt.want[i] {
					t.Fatalf("item %d = %s, want %s", i, got[i].SKU, tt.want[i])
				}
			}
		})
	}

	paper.PreferredVendorIDs = []string{v1.ID}
	paper.MaxUnitPriceCents = 950
	must(t, s.Catalog().Update(ctx, &paper))
	got, err := s.Catalog().GetBySKU(ctx, "a4-ream")
	must(t, err)
	if got.MaxUnitPriceCents != 950 || len(got.PreferredVendorIDs) != 1 || got.PreferredVendorIDs[0] != v1.ID {
		t.Fatalf("update: %+v", got)
	}
	up := domain.CatalogItem{SKU: "a4-ream", Name: "A4 Paper (seed)", Category: "Paper & Stationery", DefaultQty: 5,
		MaxUnitPriceCents: 800, PreferredVendorIDs: []string{v2.ID, v1.ID}, Active: true}
	must(t, s.Catalog().Upsert(ctx, &up))
	if up.ID != paper.ID || up.Name != "A4 Paper (seed)" || up.PreferredVendorIDs[0] != v2.ID {
		t.Fatalf("upsert: %+v", up)
	}
	must(t, s.Catalog().Delete(ctx, paper.ID))
	_, err = s.Catalog().Get(ctx, paper.ID)
	wantErr(t, err, domain.ErrNotFound)
	wantErr(t, s.Catalog().Delete(ctx, paper.ID), domain.ErrNotFound)
	_, err = s.Catalog().GetBySKU(ctx, "nope")
	wantErr(t, err, domain.ErrNotFound)
}

func testPolicy(t *testing.T, e Env) {
	s := e.Store
	_, err := s.Policy().Get(ctx)
	wantErr(t, err, domain.ErrNotFound)
	p := domain.PolicyConfig{PerOrderLimitCents: 50000, MonthlyBudgetCents: 300000, PriceDriftPct: 5}
	must(t, s.Policy().Update(ctx, &p, ""))
	if p.Currency != "SGD" || p.UpdatedAt.IsZero() {
		t.Fatalf("defaults: %+v", p)
	}
	admin := mkUser(t, s, "priya@x.example", domain.RoleAdmin)
	p2 := domain.PolicyConfig{Currency: "SGD", PerOrderLimitCents: 60000, MonthlyBudgetCents: 250000, PriceDriftPct: 7.5}
	must(t, s.Policy().Update(ctx, &p2, admin.ID))
	got, err := s.Policy().Get(ctx)
	must(t, err)
	if got.PerOrderLimitCents != 60000 || got.MonthlyBudgetCents != 250000 || got.PriceDriftPct != 7.5 || got.UpdatedBy != admin.ID {
		t.Fatalf("roundtrip: %+v", got)
	}
	neg := domain.PolicyConfig{PerOrderLimitCents: -1, MonthlyBudgetCents: 1}
	wantErr(t, s.Policy().Update(ctx, &neg, ""), domain.ErrValidation)
}

func testAddresses(t *testing.T, e Env) {
	s := e.Store
	_, err := s.Addresses().GetDefault(ctx)
	wantErr(t, err, domain.ErrNotFound)
	hq := mkAddress(t, s, "HQ", true)
	if hq.City != "Singapore" || hq.Country != "SG" {
		t.Fatalf("defaults: %+v", hq)
	}
	rd := mkAddress(t, s, "R&D", true) // steals default
	def, err := s.Addresses().GetDefault(ctx)
	must(t, err)
	if def.ID != rd.ID {
		t.Fatal("new default should win")
	}
	list, err := s.Addresses().List(ctx)
	must(t, err)
	if len(list) != 2 || list[0].ID != rd.ID || list[1].IsDefault {
		t.Fatalf("list: %+v", list)
	}
	hq.IsDefault = true
	hq.AddressLine2 = "#20-01"
	must(t, s.Addresses().Update(ctx, &hq))
	def, _ = s.Addresses().GetDefault(ctx)
	if def.ID != hq.ID || def.AddressLine2 != "#20-01" {
		t.Fatalf("update default: %+v", def)
	}
	bad := domain.Address{Label: "Bad", FirstName: "a", LastName: "b", Phone: "62001001", Email: "e", AddressLine1: "x"}
	wantErr(t, s.Addresses().Create(ctx, &bad), domain.ErrValidation)
	dupLabel := domain.Address{Label: "HQ", FirstName: "a", LastName: "b", Phone: "+6562001001", Email: "e", AddressLine1: "x"}
	wantErr(t, s.Addresses().Create(ctx, &dupLabel), domain.ErrConflict)

	up := domain.Address{Label: "R&D", FirstName: "Arjun", LastName: "Menon", Phone: "+6562001002", Email: "a@x.example",
		AddressLine1: "1 Fusionopolis Way", IsDefault: true}
	must(t, s.Addresses().Upsert(ctx, &up))
	if up.ID != rd.ID || up.FirstName != "Arjun" {
		t.Fatalf("upsert: %+v", up)
	}
	def, _ = s.Addresses().GetDefault(ctx)
	if def.ID != rd.ID {
		t.Fatal("upserted default should win")
	}

	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	r := mkRequest(t, s, u.ID)
	r.AddressID = &hq.ID
	must(t, s.Requests().Update(ctx, &r))
	wantErr(t, s.Addresses().Delete(ctx, hq.ID), domain.ErrConflict)
	must(t, s.Addresses().Delete(ctx, rd.ID))
	wantErr(t, s.Addresses().Delete(ctx, rd.ID), domain.ErrNotFound)
}

func testSystemPrompts(t *testing.T, e Env) {
	s := e.Store
	_, err := s.SystemPrompts().Get(ctx, "agent")
	wantErr(t, err, domain.ErrNotFound)
	must(t, s.SystemPrompts().SeedIfMissing(ctx, "agent", "v1 prompt"))
	must(t, s.SystemPrompts().SeedIfMissing(ctx, "agent", "other"))
	p, err := s.SystemPrompts().Get(ctx, "agent")
	must(t, err)
	if p.Content != "v1 prompt" || p.Version != 1 {
		t.Fatalf("seed: %+v", p)
	}
	admin := mkUser(t, s, "priya@x.example", domain.RoleAdmin)
	p, err = s.SystemPrompts().Update(ctx, "agent", "edited", admin.ID)
	must(t, err)
	if p.Version != 2 || p.Content != "edited" || p.UpdatedBy != admin.ID {
		t.Fatalf("update: %+v", p)
	}
	must(t, s.SystemPrompts().SeedIfMissing(ctx, "agent", "seed again"))
	p, _ = s.SystemPrompts().Get(ctx, "agent")
	if p.Content != "edited" {
		t.Fatal("SeedIfMissing clobbered an edit")
	}
	n, err := s.SystemPrompts().Update(ctx, "other", "new", "")
	must(t, err)
	if n.Version != 1 || n.UpdatedBy != "" {
		t.Fatalf("insert via update: %+v", n)
	}
}

func testEnrollments(t *testing.T, e Env) {
	s := e.Store
	_, err := s.Enrollments().Latest(ctx)
	wantErr(t, err, domain.ErrNotFound)
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	e1 := domain.Enrollment{ReapEnrollmentID: "enr_1", OwnerRef: u.ID, OwnerEmail: u.Email, NextActionURL: "https://reap/1", CreatedBy: u.ID}
	must(t, s.Enrollments().Create(ctx, &e1))
	if e1.Status != domain.EnrollmentRequiresAction || e1.ID == "" {
		t.Fatalf("create: %+v", e1)
	}
	_, err = s.Enrollments().LatestActive(ctx)
	wantErr(t, err, domain.ErrNotFound)
	must(t, s.Enrollments().UpdateStatus(ctx, e1.ID, domain.EnrollmentActive, ""))
	e2 := domain.Enrollment{ReapEnrollmentID: "enr_2", OwnerRef: u.ID, OwnerEmail: u.Email}
	must(t, s.Enrollments().Create(ctx, &e2))
	latest, err := s.Enrollments().Latest(ctx)
	must(t, err)
	if latest.ID != e2.ID {
		t.Fatal("Latest should be newest")
	}
	active, err := s.Enrollments().LatestActive(ctx)
	must(t, err)
	if active.ID != e1.ID || active.NextActionURL != "" {
		t.Fatalf("LatestActive: %+v", active)
	}
	got, err := s.Enrollments().GetByReapID(ctx, "enr_2")
	must(t, err)
	if got.ID != e2.ID {
		t.Fatal("GetByReapID")
	}
	dup := domain.Enrollment{ReapEnrollmentID: "enr_1", OwnerRef: "x", OwnerEmail: "x"}
	wantErr(t, s.Enrollments().Create(ctx, &dup), domain.ErrConflict)
	wantErr(t, s.Enrollments().UpdateStatus(ctx, missingID, domain.EnrollmentActive, ""), domain.ErrNotFound)
}

func testRequests(t *testing.T, e Env) {
	s := e.Store
	maya := mkUser(t, s, "maya@x.example", domain.RoleManager)
	dan := mkUser(t, s, "dan@x.example", domain.RoleApprover)
	r1 := mkRequest(t, s, maya.ID)
	if r1.Status != domain.StatusParsing || r1.Currency != "SGD" || r1.ID == "" {
		t.Fatalf("defaults: %+v", r1)
	}
	r2 := mkRequest(t, s, dan.ID)
	r3 := mkRequest(t, s, maya.ID)
	must(t, s.Requests().TransitionStatus(ctx, r3.ID, domain.StatusParsing, domain.StatusSearching, ""))

	bad := domain.PurchaseRequest{RequesterID: missingID, RawUtterance: "x"}
	wantErr(t, s.Requests().Create(ctx, &bad), domain.ErrValidation)

	tests := []struct {
		name string
		f    store.RequestFilter
		want []string
	}{
		{"newest first", store.RequestFilter{}, []string{r3.ID, r2.ID, r1.ID}},
		{"by requester", store.RequestFilter{RequesterID: maya.ID}, []string{r3.ID, r1.ID}},
		{"by status", store.RequestFilter{Statuses: []domain.RequestStatus{domain.StatusSearching}}, []string{r3.ID}},
		{"limit offset", store.RequestFilter{Limit: 1, Offset: 1}, []string{r2.ID}},
		{"offset past end", store.RequestFilter{Offset: 10}, []string{}},
		{"bad requester id", store.RequestFilter{RequesterID: "nope"}, []string{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Requests().List(ctx, tt.f)
			must(t, err)
			if got == nil || len(got) != len(tt.want) {
				t.Fatalf("got %d, want %d", len(got), len(tt.want))
			}
			for i := range got {
				if got[i].ID != tt.want[i] {
					t.Fatalf("item %d mismatch", i)
				}
			}
		})
	}

	addr := mkAddress(t, s, "HQ", true)
	now := time.Now().UTC().Truncate(time.Second)
	r1.AddressID, r1.ConfirmedBy, r1.ConfirmedAt = &addr.ID, &maya.ID, &now
	r1.SubtotalCents, r1.ShippingCents, r1.TotalCents, r1.Decision = 7100, 500, 7600, domain.DecisionAutoApprove
	r1.Status = domain.StatusOrdered // ignored by Update
	must(t, s.Requests().Update(ctx, &r1))
	got, err := s.Requests().Get(ctx, r1.ID)
	must(t, err)
	if got.Status != domain.StatusParsing || got.TotalCents != 7600 || *got.AddressID != addr.ID ||
		*got.ConfirmedBy != maya.ID || !got.ConfirmedAt.Equal(now) || got.Decision != domain.DecisionAutoApprove {
		t.Fatalf("update: %+v", got)
	}
	ghost := domain.PurchaseRequest{ID: missingID}
	wantErr(t, s.Requests().Update(ctx, &ghost), domain.ErrNotFound)
	_, err = s.Requests().Get(ctx, "garbage")
	wantErr(t, err, domain.ErrNotFound)
}

func testTransitionStatus(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	r := mkRequest(t, s, u.ID)
	tests := []struct {
		name     string
		id       string
		from, to domain.RequestStatus
		reason   string
		want     error
		after    domain.RequestStatus
	}{
		{"valid", r.ID, domain.StatusParsing, domain.StatusSearching, "", nil, domain.StatusSearching},
		{"forbidden by machine", r.ID, domain.StatusSearching, domain.StatusOrdered, "", domain.ErrInvalidTransition, domain.StatusSearching},
		{"stale from", r.ID, domain.StatusParsing, domain.StatusSearching, "", domain.ErrConflict, domain.StatusSearching},
		{"missing", missingID, domain.StatusParsing, domain.StatusSearching, "", domain.ErrNotFound, domain.StatusSearching},
		{"to failed with reason", r.ID, domain.StatusSearching, domain.StatusFailed, "reap down", nil, domain.StatusFailed},
		{"terminal", r.ID, domain.StatusFailed, domain.StatusSearching, "", domain.ErrInvalidTransition, domain.StatusFailed},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := s.Requests().TransitionStatus(ctx, tt.id, tt.from, tt.to, tt.reason)
			if tt.want == nil {
				must(t, err)
			} else {
				wantErr(t, err, tt.want)
			}
			got, err := s.Requests().Get(ctx, r.ID)
			must(t, err)
			if got.Status != tt.after {
				t.Fatalf("status = %s, want %s", got.Status, tt.after)
			}
		})
	}
	got, _ := s.Requests().Get(ctx, r.ID)
	if got.FailureReason != "reap down" {
		t.Fatalf("failure reason = %q", got.FailureReason)
	}
}

func testTransitionRace(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	r := mkRequest(t, s, u.ID)
	const n = 8
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			errs[i] = s.Requests().TransitionStatus(ctx, r.ID, domain.StatusParsing, domain.StatusSearching, "")
		}()
	}
	wg.Wait()
	ok := 0
	for _, err := range errs {
		switch {
		case err == nil:
			ok++
		case !errors.Is(err, domain.ErrConflict):
			t.Fatalf("unexpected error %v", err)
		}
	}
	if ok != 1 {
		t.Fatalf("%d winners, want exactly 1", ok)
	}
}

func testLineItemsAndOffers(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	v := mkVendor(t, s, "popular.com.sg", "Popular", 1)
	cat := domain.CatalogItem{SKU: "a4", Name: "A4", Category: "P", DefaultQty: 1, MaxUnitPriceCents: 900}
	must(t, s.Catalog().Create(ctx, &cat))
	r := mkRequest(t, s, u.ID)
	items := []*domain.LineItem{
		{RequestID: r.ID, CatalogItemID: &cat.ID, Description: "A4 paper", Qty: 10, Position: 9},
		{RequestID: r.ID, Description: "standing desk", Qty: 1, Urgency: domain.UrgencyUrgent},
	}
	must(t, s.LineItems().CreateBatch(ctx, items))
	if items[0].ID == "" || items[0].Position != 0 || items[1].Position != 1 || items[0].Urgency != domain.UrgencyNormal {
		t.Fatalf("batch: %+v %+v", items[0], items[1])
	}
	bad := []*domain.LineItem{{RequestID: r.ID, Description: "x", Qty: 0}}
	wantErr(t, s.LineItems().CreateBatch(ctx, bad), domain.ErrValidation)
	list, err := s.LineItems().ListByRequest(ctx, r.ID)
	must(t, err)
	if len(list) != 2 || list[0].ID != items[0].ID || list[0].Reasons == nil {
		t.Fatalf("list: %+v", list)
	}

	offers := []*domain.Offer{
		{VendorID: &v.ID, MerchantName: "Popular", ReapProductID: "p2", ReapVariantID: "v2", Title: "Paper B", UnitPriceCents: 790, Rank: 2, Available: true},
		{VendorID: &v.ID, MerchantName: "Popular", ReapProductID: "p1", ReapVariantID: "v1", Title: "Paper A", UnitPriceCents: 710, Rank: 1, Available: true, LandedCostCents: 7100},
	}
	must(t, s.Offers().ReplaceForLineItem(ctx, items[0].ID, offers))
	if offers[0].ID == "" || offers[0].LineItemID != items[0].ID || offers[0].Currency != "SGD" || offers[0].PackSize != 1 {
		t.Fatalf("offer fill: %+v", offers[0])
	}
	li := *items[0]
	li.SelectedOfferID = &offers[1].ID
	li.PolicyDecision = domain.DecisionAutoApprove
	li.Reasons = []domain.Reason{{Code: domain.ReasonOverUnitCeiling, Message: "too pricey"}}
	li.Qty = 12
	must(t, s.LineItems().Update(ctx, &li))
	got, err := s.LineItems().Get(ctx, li.ID)
	must(t, err)
	if *got.SelectedOfferID != offers[1].ID || got.Qty != 12 || len(got.Reasons) != 1 || got.Reasons[0].Code != domain.ReasonOverUnitCeiling {
		t.Fatalf("line update: %+v", got)
	}
	byLine, err := s.Offers().ListByLineItem(ctx, li.ID)
	must(t, err)
	if len(byLine) != 2 || byLine[0].Title != "Paper A" {
		t.Fatalf("rank order: %+v", byLine)
	}
	o, err := s.Offers().Get(ctx, offers[1].ID)
	must(t, err)
	if o.LandedCostCents != 7100 || *o.VendorID != v.ID {
		t.Fatalf("offer get: %+v", o)
	}

	// Offers for the second line, then replace line 1 again: selection is cleared.
	must(t, s.Offers().ReplaceForLineItem(ctx, items[1].ID, []*domain.Offer{
		{MerchantName: "Unknown", ReapProductID: "d", ReapVariantID: "d", Title: "Desk", UnitPriceCents: 30000, Rank: 1},
	}))
	must(t, s.Offers().ReplaceForLineItem(ctx, items[0].ID, []*domain.Offer{
		{MerchantName: "Popular", ReapProductID: "p3", ReapVariantID: "v3", Title: "Paper C", UnitPriceCents: 700, Rank: 1},
	}))
	got, _ = s.LineItems().Get(ctx, li.ID)
	if got.SelectedOfferID != nil {
		t.Fatal("ReplaceForLineItem must clear selected_offer_id")
	}
	_, err = s.Offers().Get(ctx, offers[1].ID)
	wantErr(t, err, domain.ErrNotFound)
	all, err := s.Offers().ListByRequest(ctx, r.ID)
	must(t, err)
	if len(all) != 2 || all[0].Title != "Paper C" || all[1].Title != "Desk" {
		t.Fatalf("by request: %+v", all)
	}
	wantErr(t, s.Offers().ReplaceForLineItem(ctx, missingID, nil), domain.ErrNotFound)

	must(t, s.LineItems().Delete(ctx, items[1].ID))
	wantErr(t, s.LineItems().Delete(ctx, items[1].ID), domain.ErrNotFound)
	all, _ = s.Offers().ListByRequest(ctx, r.ID)
	if len(all) != 1 {
		t.Fatalf("offers should cascade, got %d", len(all))
	}
}

func testApprovals(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	dan := mkUser(t, s, "dan@x.example", domain.RoleApprover)
	r1 := mkRequest(t, s, u.ID)
	r2 := mkRequest(t, s, u.ID)
	a := domain.Approval{RequestID: r1.ID, Kind: domain.ApprovalKindPolicy, AmountCents: 60000,
		Reasons: []domain.Reason{{Code: domain.ReasonOverOrderLimit, Message: "over S$500"}}}
	must(t, s.Approvals().Create(ctx, &a))
	if a.Status != domain.ApprovalPending || a.ID == "" || len(a.Reasons) != 1 {
		t.Fatalf("create: %+v", a)
	}
	second := domain.Approval{RequestID: r1.ID, Kind: domain.ApprovalKindPriceDrift, AmountCents: 1}
	wantErr(t, s.Approvals().Create(ctx, &second), domain.ErrConflict)
	b := domain.Approval{RequestID: r2.ID, Kind: domain.ApprovalKindPriceDrift, AmountCents: 52000, PrevCents: ptr(domain.Cents(50000))}
	must(t, s.Approvals().Create(ctx, &b))

	_, err := s.Approvals().Decide(ctx, a.ID, "maybe", dan.ID, "")
	wantErr(t, err, domain.ErrValidation)
	dec, err := s.Approvals().Decide(ctx, a.ID, domain.ApprovalApproved, dan.ID, "ok")
	must(t, err)
	if dec.Status != domain.ApprovalApproved || dec.ApproverID == nil || *dec.ApproverID != dan.ID || dec.DecidedAt == nil || dec.Comment != "ok" {
		t.Fatalf("decide: %+v", dec)
	}
	_, err = s.Approvals().Decide(ctx, a.ID, domain.ApprovalRejected, dan.ID, "")
	wantErr(t, err, domain.ErrConflict)
	_, err = s.Approvals().Decide(ctx, missingID, domain.ApprovalRejected, dan.ID, "")
	wantErr(t, err, domain.ErrNotFound)
	// After a decision a new pending approval is allowed (e.g. price drift).
	must(t, s.Approvals().Create(ctx, &second))

	pending, err := s.Approvals().List(ctx, store.ApprovalFilter{Status: domain.ApprovalPending})
	must(t, err)
	if len(pending) != 2 || pending[0].ID != second.ID || pending[1].ID != b.ID {
		t.Fatalf("pending newest first: %+v", pending)
	}
	all, err := s.Approvals().List(ctx, store.ApprovalFilter{Limit: 2})
	must(t, err)
	if len(all) != 2 {
		t.Fatalf("limit: %d", len(all))
	}
	byReq, err := s.Approvals().ListByRequest(ctx, r1.ID)
	must(t, err)
	if len(byReq) != 2 || byReq[0].ID != second.ID {
		t.Fatalf("by request: %+v", byReq)
	}
	got, err := s.Approvals().Get(ctx, b.ID)
	must(t, err)
	if got.PrevCents == nil || *got.PrevCents != 50000 || got.Reasons == nil {
		t.Fatalf("get: %+v", got)
	}
}

func testPayments(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	v := mkVendor(t, s, "popular.com.sg", "Popular", 1)
	enr := domain.Enrollment{ReapEnrollmentID: "enr", OwnerRef: u.ID, OwnerEmail: u.Email, Status: domain.EnrollmentActive}
	must(t, s.Enrollments().Create(ctx, &enr))
	r := mkRequest(t, s, u.ID)
	noKey := domain.Payment{RequestID: r.ID, MerchantName: "x"}
	wantErr(t, s.Payments().Create(ctx, &noKey), domain.ErrValidation)

	p := domain.Payment{RequestID: r.ID, VendorID: &v.ID, MerchantName: "Popular", EnrollmentID: &enr.ID, IdempotencyKey: "idem-1",
		ReapQuoteID: "q_1", ItemsCents: 7100, ShippingCents: 500, QuotedCents: 7600}
	must(t, s.Payments().Create(ctx, &p))
	if p.Status != domain.PaymentQuoting || p.Currency != "SGD" || p.IdempotencyKey != "idem-1" {
		t.Fatalf("create: %+v", p)
	}
	dup := domain.Payment{RequestID: r.ID, MerchantName: "x", IdempotencyKey: "idem-1"}
	wantErr(t, s.Payments().Create(ctx, &dup), domain.ErrConflict)
	p2 := mkPayment(t, s, r.ID, "idem-2", domain.PaymentQuoted, nil)

	exp := time.Now().UTC().Add(time.Hour).Truncate(time.Second)
	p.Status, p.ReapCheckoutID, p.ApprovalURL, p.QuoteExpiresAt = domain.PaymentRequiresAction, "chk_1", "https://reap/approve", &exp
	must(t, s.Payments().Update(ctx, &p))
	got, err := s.Payments().GetByCheckoutID(ctx, "chk_1")
	must(t, err)
	if got.ID != p.ID || got.ApprovalURL != "https://reap/approve" || !got.QuoteExpiresAt.Equal(exp) || *got.EnrollmentID != enr.ID {
		t.Fatalf("checkout lookup: %+v", got)
	}
	_, err = s.Payments().GetByCheckoutID(ctx, "")
	wantErr(t, err, domain.ErrNotFound)
	p2.ReapCheckoutID = "chk_1"
	wantErr(t, s.Payments().Update(ctx, &p2), domain.ErrConflict)

	inflight, err := s.Payments().ListInFlight(ctx)
	must(t, err)
	if len(inflight) != 1 || inflight[0].ID != p.ID {
		t.Fatalf("in flight: %+v", inflight)
	}
	final := domain.Cents(7650)
	p.Status, p.FinalCents, p.ReapOrderID = domain.PaymentCompleted, &final, "ord_1"
	must(t, s.Payments().Update(ctx, &p))
	inflight, _ = s.Payments().ListInFlight(ctx)
	if len(inflight) != 0 {
		t.Fatal("completed payment still in flight")
	}
	list, err := s.Payments().ListByRequest(ctx, r.ID)
	must(t, err)
	if len(list) != 2 || list[0].ID != p.ID || *list[0].FinalCents != 7650 || list[0].ReapOrderID != "ord_1" {
		t.Fatalf("by request: %+v", list)
	}
	ghost := domain.Payment{ID: missingID, Status: domain.PaymentFailed}
	wantErr(t, s.Payments().Update(ctx, &ghost), domain.ErrNotFound)

	// At most one open (not failed/expired) payment per request and merchant (guards against a
	// second Reap checkout for a merchant); failed/expired ones do not count.
	r2 := mkRequest(t, s, u.ID)
	a := domain.Payment{RequestID: r2.ID, VendorID: &v.ID, MerchantName: "Popular", IdempotencyKey: "open-1", QuoteAttempt: 2}
	must(t, s.Payments().Create(ctx, &a))
	if a.QuoteAttempt != 2 {
		t.Fatalf("quote attempt not stored: %+v", a)
	}
	b := domain.Payment{RequestID: r2.ID, VendorID: &v.ID, MerchantName: "Popular", IdempotencyKey: "open-2"}
	wantErr(t, s.Payments().Create(ctx, &b), domain.ErrConflict)
	sameName := domain.Payment{RequestID: r2.ID, MerchantName: "Other", IdempotencyKey: "open-3"}
	must(t, s.Payments().Create(ctx, &sameName))
	a.Status, a.QuoteAttempt = domain.PaymentExpired, 3
	must(t, s.Payments().Update(ctx, &a))
	if got, _ := s.Payments().Get(ctx, a.ID); got.QuoteAttempt != 3 {
		t.Fatalf("quote attempt update lost: %+v", got)
	}
	must(t, s.Payments().Create(ctx, &b))
	a.Status = domain.PaymentQuoted
	wantErr(t, s.Payments().Update(ctx, &a), domain.ErrConflict)
}

func testAudit(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	r := mkRequest(t, s, u.ID)
	t0 := time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC)
	events := []domain.AuditEvent{
		{RequestID: &r.ID, ActorType: domain.ActorUser, ActorID: u.ID, Type: "request.created", Payload: json.RawMessage(`{"raw":"paper"}`), At: t0},
		{RequestID: &r.ID, ActorType: domain.ActorSystem, Type: "checkout.quoted", At: t0.Add(time.Minute)},
		{ActorType: domain.ActorAgent, Type: "checkout.price_drift", Payload: json.RawMessage(`{"pct":7}`), At: t0.Add(2 * time.Minute)},
		{RequestID: &r.ID, ActorType: domain.ActorSystem, Type: "checkoutx"},
	}
	for i := range events {
		must(t, s.Audit().Append(ctx, &events[i]))
	}
	if events[0].ID == 0 || events[1].ID <= events[0].ID || string(events[1].Payload) != "{}" || events[3].At.IsZero() {
		t.Fatalf("append fill: %+v %+v", events[0], events[1])
	}
	bad := domain.AuditEvent{ActorType: domain.ActorSystem, Type: "x", Payload: json.RawMessage(`{nope`)}
	wantErr(t, s.Audit().Append(ctx, &bad), domain.ErrValidation)

	byReq, err := s.Audit().ListByRequest(ctx, r.ID)
	must(t, err)
	if len(byReq) != 3 || byReq[0].Type != "request.created" || !byReq[0].At.Equal(t0) {
		t.Fatalf("by request oldest first: %+v", byReq)
	}
	var payload map[string]string
	must(t, json.Unmarshal(byReq[0].Payload, &payload))
	if payload["raw"] != "paper" {
		t.Fatalf("payload: %s", byReq[0].Payload)
	}

	tests := []struct {
		name string
		f    store.AuditFilter
		want []string
	}{
		{"all newest first", store.AuditFilter{}, []string{"checkoutx", "checkout.price_drift", "checkout.quoted", "request.created"}},
		{"prefix", store.AuditFilter{Type: "checkout."}, []string{"checkout.price_drift", "checkout.quoted"}},
		{"exact", store.AuditFilter{Type: "checkout.quoted"}, []string{"checkout.quoted"}},
		{"exact no prefix semantics", store.AuditFilter{Type: "checkout"}, []string{}},
		{"since", store.AuditFilter{Since: t0.Add(30 * time.Second), Type: "checkout."}, []string{"checkout.price_drift", "checkout.quoted"}},
		{"limit offset", store.AuditFilter{Limit: 1, Offset: 1}, []string{"checkout.price_drift"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := s.Audit().List(ctx, tt.f)
			must(t, err)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d, want %v", len(got), tt.want)
			}
			for i := range got {
				if got[i].Type != tt.want[i] {
					t.Fatalf("event %d = %s, want %s", i, got[i].Type, tt.want[i])
				}
			}
		})
	}
}

func testChat(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	for i := 0; i < 5; i++ {
		m := domain.ChatMessage{SessionID: "s1", UserID: u.ID, Role: domain.ChatRoleUser, Content: fmt.Sprintf("m%d", i)}
		must(t, s.Chat().Append(ctx, &m))
		if m.ID == 0 || m.CreatedAt.IsZero() {
			t.Fatalf("append fill: %+v", m)
		}
	}
	tool := domain.ChatMessage{SessionID: "s1", Role: domain.ChatRoleAssistant, ToolCalls: json.RawMessage(`[{"id":"c1","name":"search"}]`)}
	must(t, s.Chat().Append(ctx, &tool))
	other := domain.ChatMessage{SessionID: "s2", Role: domain.ChatRoleTool, Content: "{}", ToolCallID: "c1"}
	must(t, s.Chat().Append(ctx, &other))

	last, err := s.Chat().ListBySession(ctx, "s1", 3)
	must(t, err)
	if len(last) != 3 || last[0].Content != "m3" || last[2].ToolCalls == nil {
		t.Fatalf("last 3 oldest first: %+v", last)
	}
	var calls []map[string]string
	must(t, json.Unmarshal(last[2].ToolCalls, &calls))
	all, err := s.Chat().ListBySession(ctx, "s1", 0)
	must(t, err)
	if len(all) != 6 || all[0].ToolCalls != nil || all[0].UserID != u.ID {
		t.Fatalf("all: %d %+v", len(all), all[0])
	}
	s2, _ := s.Chat().ListBySession(ctx, "s2", 10)
	if len(s2) != 1 || s2[0].ToolCallID != "c1" || s2[0].UserID != "" {
		t.Fatalf("s2: %+v", s2)
	}
	empty, err := s.Chat().ListBySession(ctx, "none", 10)
	must(t, err)
	if empty == nil || len(empty) != 0 {
		t.Fatal("empty session must be non-nil empty")
	}
}

func testSpend(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	sgt := store.SingaporeLocation
	now := time.Date(2026, 10, 15, 12, 0, 0, 0, sgt)

	r1 := mkRequest(t, s, u.ID)
	r2 := mkRequest(t, s, u.ID)
	r3 := mkRequest(t, s, u.ID)
	type pay struct {
		req   string
		st    domain.PaymentStatus
		final domain.Cents
		at    time.Time
	}
	pays := []pay{
		{r1.ID, domain.PaymentCompleted, 10000, time.Date(2026, 10, 1, 0, 30, 0, 0, sgt)},    // Oct (UTC: Sep 30)
		{r1.ID, domain.PaymentCompleted, 2500, time.Date(2026, 10, 14, 9, 0, 0, 0, sgt)},     // Oct, same request
		{r2.ID, domain.PaymentCompleted, 4000, time.Date(2026, 9, 30, 23, 59, 0, 0, sgt)},    // Sep
		{r3.ID, domain.PaymentCompleted, 999, time.Date(2026, 7, 3, 10, 0, 0, 0, sgt)},       // Jul
		{r3.ID, domain.PaymentFailed, 777777, time.Date(2026, 10, 2, 10, 0, 0, 0, sgt)},      // not completed
		{r2.ID, domain.PaymentCompleted, 123, time.Date(2025, 10, 10, 10, 0, 0, 0, sgt)},     // a year ago
		{r3.ID, domain.PaymentRequiresAction, 5000, time.Date(2026, 10, 3, 1, 0, 0, 0, sgt)}, // in flight
	}
	for i, p := range pays {
		fc := p.final
		pm := mkPayment(t, s, p.req, fmt.Sprintf("k%d", i), p.st, &fc)
		if p.st == domain.PaymentCompleted {
			e.SetCompletedAt(t, pm.ID, p.at)
		}
	}

	mtd, err := s.Spend().MonthToDate(ctx, now)
	must(t, err)
	if mtd != 12500 {
		t.Fatalf("October MTD = %d, want 12500", mtd)
	}
	sep, err := s.Spend().MonthToDate(ctx, time.Date(2026, 9, 30, 16, 30, 0, 0, time.UTC)) // = Oct 1 00:30 SGT
	must(t, err)
	if sep != 12500 {
		t.Fatalf("UTC Sep 30 16:30 is October in SGT; got %d", sep)
	}
	empty, err := s.Spend().MonthToDate(ctx, time.Date(2026, 8, 5, 0, 0, 0, 0, sgt))
	must(t, err)
	if empty != 0 {
		t.Fatalf("August = %d", empty)
	}

	months, err := s.Spend().ByMonth(ctx, 4, now)
	must(t, err)
	want := []domain.MonthSpend{
		{Month: "2026-07", SpendCents: 999, Orders: 1},
		{Month: "2026-08"},
		{Month: "2026-09", SpendCents: 4000, Orders: 1},
		{Month: "2026-10", SpendCents: 12500, Orders: 1},
	}
	if len(months) != len(want) {
		t.Fatalf("months: %+v", months)
	}
	for i := range want {
		if months[i] != want[i] {
			t.Fatalf("month %d = %+v, want %+v", i, months[i], want[i])
		}
	}
	year, err := s.Spend().ByMonth(ctx, 13, now)
	must(t, err)
	if len(year) != 13 || year[0].Month != "2025-10" || year[0].SpendCents != 123 {
		t.Fatalf("13 months: %+v", year[0])
	}
}

func testWithTx(t *testing.T, e Env) {
	s := e.Store
	boom := errors.New("boom")
	err := s.WithTx(ctx, func(tx store.Store) error {
		mkUser(t, tx, "rolled@x.example", domain.RoleManager)
		return tx.WithTx(ctx, func(inner store.Store) error { // nested reuses the outer tx
			mkVendor(t, inner, "rolled.example", "R", 1)
			return boom
		})
	})
	if !errors.Is(err, boom) {
		t.Fatalf("want boom, got %v", err)
	}
	if _, err := s.Users().GetByEmail(ctx, "rolled@x.example"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("rollback did not undo the user")
	}
	if _, err := s.Vendors().GetByDomain(ctx, "rolled.example"); !errors.Is(err, domain.ErrNotFound) {
		t.Fatal("rollback did not undo the vendor")
	}
	must(t, s.WithTx(ctx, func(tx store.Store) error {
		u := mkUser(t, tx, "kept@x.example", domain.RoleManager)
		r := mkRequest(t, tx, u.ID)
		return tx.Requests().TransitionStatus(ctx, r.ID, domain.StatusParsing, domain.StatusSearching, "")
	}))
	u, err := s.Users().GetByEmail(ctx, "kept@x.example")
	must(t, err)
	list, _ := s.Requests().List(ctx, store.RequestFilter{RequesterID: u.ID})
	if len(list) != 1 || list[0].Status != domain.StatusSearching {
		t.Fatalf("commit: %+v", list)
	}
	// A failing multi-statement repo call inside a tx leaves no partial writes after rollback.
	r := list[0]
	err = s.WithTx(ctx, func(tx store.Store) error {
		return tx.LineItems().CreateBatch(ctx, []*domain.LineItem{
			{RequestID: r.ID, Description: "ok", Qty: 1},
			{RequestID: r.ID, Description: "bad", Qty: -1},
		})
	})
	wantErr(t, err, domain.ErrValidation)
	items, _ := s.LineItems().ListByRequest(ctx, r.ID)
	if len(items) != 0 {
		t.Fatalf("partial batch persisted: %d", len(items))
	}
	must(t, s.Ping(ctx))
}

func testDetail(t *testing.T, e Env) {
	s := e.Store
	u := mkUser(t, s, "maya@x.example", domain.RoleManager)
	addr := mkAddress(t, s, "HQ", true)
	r := mkRequest(t, s, u.ID)
	_, err := s.Requests().Detail(ctx, missingID)
	wantErr(t, err, domain.ErrNotFound)

	d, err := s.Requests().Detail(ctx, r.ID)
	must(t, err)
	if d.Address != nil || d.LineItems == nil || d.Offers == nil || d.Approvals == nil || d.Payments == nil {
		t.Fatalf("empty detail must have non-nil slices: %+v", d)
	}
	items := []*domain.LineItem{{RequestID: r.ID, Description: "paper", Qty: 2}}
	must(t, s.LineItems().CreateBatch(ctx, items))
	must(t, s.Offers().ReplaceForLineItem(ctx, items[0].ID, []*domain.Offer{
		{MerchantName: "Popular", ReapProductID: "p", ReapVariantID: "v", Title: "Paper", UnitPriceCents: 700, Rank: 1},
	}))
	a := domain.Approval{RequestID: r.ID, Kind: domain.ApprovalKindPolicy, AmountCents: 1400}
	must(t, s.Approvals().Create(ctx, &a))
	mkPayment(t, s, r.ID, "k", domain.PaymentQuoted, nil)
	r.AddressID = &addr.ID
	must(t, s.Requests().Update(ctx, &r))

	d, err = s.Requests().Detail(ctx, r.ID)
	must(t, err)
	if d.Request.ID != r.ID || len(d.LineItems) != 1 || len(d.Offers) != 1 || len(d.Approvals) != 1 ||
		len(d.Payments) != 1 || d.Address == nil || d.Address.ID != addr.ID {
		t.Fatalf("detail: %+v", d)
	}
}
