package orchestrator

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
)

// PRD example 1: "We're out of printer paper and coffee pods, reorder the usual." Both items are on
// the approved list and under the limits, so the order goes ahead after the payment approval.
func TestFlow_PaperAndCoffee_AutoApproved(t *testing.T) { runPaperAndCoffee(t, newEnv(t)) }

func runPaperAndCoffee(t *testing.T, e *testEnv) {
	r := e.create("We're out of printer paper and coffee pods, reorder the usual.")
	d := e.wantStatus(r.ID, domain.StatusQuoted)

	if len(d.LineItems) != 2 {
		t.Fatalf("line items = %+v", d.LineItems)
	}
	wantSKU := map[string]int{"a4-copier-paper-ream": 10, "coffee-capsules": 6}
	for _, li := range d.LineItems {
		if li.CatalogItemID == nil {
			t.Fatalf("line %q not matched to catalog", li.Description)
		}
		it, err := e.st.Catalog().Get(e.ctx, *li.CatalogItemID)
		if err != nil {
			t.Fatal(err)
		}
		if q, ok := wantSKU[it.SKU]; !ok || li.Qty != q {
			t.Fatalf("line %s qty %d, want %v", it.SKU, li.Qty, wantSKU)
		}
		if li.PolicyDecision != domain.DecisionAutoApprove {
			t.Fatalf("line %s decision %s reasons %+v", it.SKU, li.PolicyDecision, li.Reasons)
		}
		best := offerByID(d, li.SelectedOfferID)
		if best.Rank != 1 || best.VendorID == nil || !slices.Contains(it.PreferredVendorIDs, *best.VendorID) {
			t.Fatalf("line %s best offer %+v not from a preferred vendor", it.SKU, best)
		}
	}
	if d.Request.Decision != domain.DecisionAutoApprove || d.Request.TotalCents <= 0 || d.Request.TotalCents > 50000 {
		t.Fatalf("request decision %s total %d", d.Request.Decision, d.Request.TotalCents)
	}
	// Only allow-listed merchants in offers (fixtures include ShopMustafa etc.).
	for _, of := range d.Offers {
		if of.MerchantName == fakereap.MerchantMustafa || of.MerchantName == fakereap.MerchantStationeryPal || of.MerchantName == fakereap.MerchantWaangoo {
			t.Fatalf("non-allow-listed offer kept: %+v", of)
		}
	}

	d, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Request.ConfirmedBy == nil || *d.Request.ConfirmedBy != e.manager.ID || d.Request.AddressID == nil || *d.Request.AddressID != e.addr.ID {
		t.Fatalf("confirmation not stored: %+v", d.Request)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if len(d.Payments) != 2 {
		t.Fatalf("payments = %+v", d.Payments)
	}
	merchants := []string{}
	for _, p := range d.Payments {
		if p.Status != domain.PaymentRequiresAction || p.ApprovalURL == "" || p.ReapQuoteID == "" || p.QuotedCents <= 0 {
			t.Fatalf("payment %+v", p)
		}
		merchants = append(merchants, p.MerchantName)
	}
	slices.Sort(merchants)
	if !slices.Equal(merchants, []string{fakereap.MerchantCommonMan, fakereap.MerchantPopular}) {
		t.Fatalf("merchants = %v", merchants)
	}
	if len(e.pendingApprovals(r.ID)) != 0 {
		t.Fatal("auto-approved request must not create an approval")
	}

	e.approveCheckouts(d)
	e.refresh(r.ID) // what the poll job / "check now" button does
	d = e.wantStatus(r.ID, domain.StatusOrdered)
	for _, p := range d.Payments {
		if p.Status != domain.PaymentCompleted || p.ReapOrderID == "" || p.FinalCents == nil {
			t.Fatalf("payment not completed: %+v", p)
		}
	}

	wantPath := []domain.RequestStatus{domain.StatusSearching, domain.StatusQuoted, domain.StatusApproved,
		domain.StatusCheckingOut, domain.StatusAwaitingPayment}
	path := e.statusPath(r.ID)
	if len(path) < len(wantPath) || !slices.Equal(path[:len(wantPath)], wantPath) || path[len(path)-1] != domain.StatusOrdered {
		t.Fatalf("status path = %v", path)
	}
	at := e.auditTypes(r.ID)
	for _, typ := range []string{audit.RequestCreated, audit.LineItemsParsed, audit.SearchCompleted, audit.PolicyEvaluated,
		audit.RequestConfirmed, audit.ReapQuoteCreated, audit.ReapCheckoutCreated, audit.ReapCheckoutStatus, audit.OrderCompleted} {
		if at[typ] == 0 {
			t.Errorf("missing audit %s (have %v)", typ, at)
		}
	}
	evs := e.eventTypes(r.ID)
	for _, typ := range []events.Type{events.RequestCreated, events.LineItemsParsed, events.SearchStarted, events.SearchVendorResult,
		events.OffersRanked, events.PolicyEvaluated, events.CheckoutQuoted, events.PaymentActionNeeded, events.PaymentStatusChanged, events.OrderCompleted} {
		if !slices.Contains(evs, typ) {
			t.Errorf("missing SSE event %s", typ)
		}
	}
}

// PRD example 2: "Get us a standing desk for the new hire." Off-list, so it goes to the approval
// queue with the best quotes; after approval the checkout proceeds.
func TestFlow_StandingDesk_NeedsApproval(t *testing.T) { runStandingDesk(t, newEnv(t, deskProducts())) }

func deskProducts() envOpt {
	return withProducts(
		fakereap.FixtureProduct{ID: "ergo_standing_desk", Domain: "ergotune.com", Merchant: fakereap.MerchantErgoTune,
			Name: "ErgoTune Standing Desk Pro", Variants: []fakereap.FixtureVariant{{ID: "ergo_standing_desk_v1", PriceCents: 59900, Available: true, RequiresShipping: true}}},
		fakereap.FixtureProduct{ID: "ergo_standing_desk_lite", Domain: "ergotune.com", Merchant: fakereap.MerchantErgoTune,
			Name: "ErgoTune Standing Desk Lite", Variants: []fakereap.FixtureVariant{{ID: "ergo_standing_desk_lite_v1", PriceCents: 45900, Available: true, RequiresShipping: true}}},
		fakereap.FixtureProduct{ID: "mustafa_standing_desk", Domain: "mustafa.com.sg", Merchant: fakereap.MerchantMustafa,
			Name: "Standing Desk Budget", Variants: []fakereap.FixtureVariant{{ID: "mustafa_standing_desk_v1", PriceCents: 19900, Available: true, RequiresShipping: true}}},
	)
}

func runStandingDesk(t *testing.T, e *testEnv) {
	r := e.create("Get us a standing desk for the new hire.")
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	if len(d.LineItems) != 1 || d.LineItems[0].CatalogItemID != nil {
		t.Fatalf("want one off-list line, got %+v", d.LineItems)
	}
	li := d.LineItems[0]
	if !strings.Contains(strings.ToLower(li.Description), "standing desk") || li.Qty != 1 {
		t.Fatalf("line = %+v", li)
	}
	if li.PolicyDecision != domain.DecisionNeedsApproval || !hasReason(li.Reasons, domain.ReasonOffList) {
		t.Fatalf("line decision %s reasons %+v", li.PolicyDecision, li.Reasons)
	}
	best := offerByID(d, li.SelectedOfferID)
	if best.MerchantName != fakereap.MerchantErgoTune || best.UnitPriceCents != 45900 {
		t.Fatalf("best offer = %+v (cheaper non-allow-listed offers must be ignored)", best)
	}
	if len(d.Offers) < 2 {
		t.Fatalf("want several quotes for the approver, got %d", len(d.Offers))
	}

	d, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID})
	if err != nil {
		t.Fatal(err)
	}
	if d.Request.Status != domain.StatusPendingApproval {
		t.Fatalf("status after confirm = %s", d.Request.Status)
	}
	a := e.pendingApproval(r.ID)
	if a.Kind != domain.ApprovalKindPolicy || !hasReason(a.Reasons, domain.ReasonOffList) || a.AmountCents != d.Request.TotalCents {
		t.Fatalf("approval = %+v", a)
	}
	view, err := e.appr.Get(e.ctx, a.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(view.Lines) != 1 || len(view.Lines[0].TopOffers) < 2 || len(view.Lines[0].TopOffers) > 3 {
		t.Fatalf("approver view = %+v", view.Lines)
	}

	if _, err := e.appr.Approve(e.ctx, a.ID, e.approver, "ok for new hire"); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if len(d.Payments) != 1 || d.Payments[0].ApprovalURL == "" || d.Payments[0].MerchantName != fakereap.MerchantErgoTune {
		t.Fatalf("payments = %+v", d.Payments)
	}
	e.approveCheckouts(d)
	e.refresh(r.ID)
	e.wantStatus(r.ID, domain.StatusOrdered)
}

func TestFlow_Rejected_ByApprover(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "ergonomic chair"})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	if d.LineItems[0].PolicyDecision != domain.DecisionNeedsApproval || !hasReason(d.LineItems[0].Reasons, domain.ReasonNotAutoApprove) {
		t.Fatalf("line = %+v", d.LineItems[0])
	}
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	a := e.pendingApproval(r.ID)
	if _, err := e.appr.Reject(e.ctx, a.ID, e.approver, "not this quarter"); err != nil {
		t.Fatal(err)
	}
	e.idle()
	e.wantStatus(r.ID, domain.StatusRejected)
	if q, c, _ := e.reapSrv.Counts(); q != 0 || c != 0 {
		t.Fatalf("rejected request touched Reap: quotes=%d checkouts=%d", q, c)
	}
}

// The live Reap quote is more than price_drift_pct above the approved total: back to approval
// (kind price_drift); after approval the checkout reuses the quote.
func TestFlow_PriceDrift_BackToApproval(t *testing.T) { runPriceDriftBackToApproval(t, newEnv(t)) }

func runPriceDriftBackToApproval(t *testing.T, e *testEnv) {
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	approved := d.Request.TotalCents
	best := offerByID(d, d.LineItems[0].SelectedOfferID)
	if err := e.reapSrv.SetVariantPrice(best.ReapVariantID, int64(best.UnitPriceCents)*120/100); err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusPendingApproval)
	a := e.pendingApproval(r.ID)
	if a.Kind != domain.ApprovalKindPriceDrift || a.PrevCents == nil || *a.PrevCents != approved || a.AmountCents <= approved ||
		!hasReason(a.Reasons, domain.ReasonPriceDrift) {
		t.Fatalf("drift approval = %+v (approved %d)", a, approved)
	}
	if !slices.Contains(e.eventTypes(r.ID), events.CheckoutPriceDrift) || e.auditTypes(r.ID)[audit.ReapPriceDrift] == 0 {
		t.Fatal("drift not published/audited")
	}
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatal("checkout created despite drift")
	}
	if _, err := e.appr.Approve(e.ctx, a.ID, e.approver, "fine"); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if q, c, _ := e.reapSrv.Counts(); q != 1 || c != 1 {
		t.Fatalf("quotes=%d checkouts=%d, want the quote reused", q, c)
	}
	if d.Request.TotalCents != a.AmountCents {
		t.Fatalf("total %d, want approved drift amount %d", d.Request.TotalCents, a.AmountCents)
	}
}

func TestFlow_SmallDriftWithinTolerance(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	best := offerByID(d, d.LineItems[0].SelectedOfferID)
	// +4% on items (paper 10 x 7.10 = 71.00 is above the free-shipping threshold).
	if err := e.reapSrv.SetVariantPrice(best.ReapVariantID, int64(best.UnitPriceCents)*104/100); err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	e.idle()
	e.wantStatus(r.ID, domain.StatusAwaitingPayment)
}

func TestFlow_CheckoutFailedOrExpired(t *testing.T) {
	runCheckoutFailedOrExpired(t, func(t *testing.T) *testEnv { return newEnv(t) })
}

func runCheckoutFailedOrExpired(t *testing.T, mk func(t *testing.T) *testEnv) {
	for _, tc := range []struct {
		name string
		act  func(s *fakereap.Server, id string) error
	}{
		{"failed", (*fakereap.Server).FailCheckout},
		{"expired", (*fakereap.Server).ExpireCheckout},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := mk(t)
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
			if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
				t.Fatal(err)
			}
			e.idle()
			d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
			if err := tc.act(e.reapSrv, d.Payments[0].ReapCheckoutID); err != nil {
				t.Fatal(err)
			}
			e.refresh(r.ID)
			d = e.wantStatus(r.ID, domain.StatusFailed)
			if !strings.Contains(d.Request.FailureReason, tc.name) {
				t.Fatalf("failure reason %q", d.Request.FailureReason)
			}
		})
	}
}

func TestFlow_QuoteErrorFailsRequest(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	best := offerByID(d, d.LineItems[0].SelectedOfferID)
	if err := e.reapSrv.SetVariantAvailable(best.ReapVariantID, false); err != nil {
		t.Fatal(err)
	}
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusFailed)
	if d.Request.FailureReason == "" || len(d.Payments) != 1 || d.Payments[0].Status != domain.PaymentFailed {
		t.Fatalf("request %+v payments %+v", d.Request, d.Payments)
	}
}

func TestFlow_Reap503IsRetried(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/checkouts", Status: 503, Code: "AGENTIC_SERVICE_UNAVAILABLE", Times: 5})
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		t.Fatal(err)
	}
	e.idle()
	e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if _, c, _ := e.reapSrv.Counts(); c != 1 {
		t.Fatalf("checkouts = %d, want exactly 1 (idempotent retries)", c)
	}
}

func TestConfirm_Errors(t *testing.T) {
	t.Run("no active enrollment", func(t *testing.T) {
		e := newEnv(t, withoutEnrollment())
		r := e.create("", agents.RequestedItem{Description: "printer paper"})
		_, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID})
		if !errors.Is(err, domain.ErrNoActiveEnrollment) {
			t.Fatalf("err = %v", err)
		}
		e.wantStatus(r.ID, domain.StatusQuoted)
		// Enrollment pending on Reap's page: still not active.
		en, err := e.o.StartEnrollment(e.ctx, e.manager)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); !errors.Is(err, domain.ErrNoActiveEnrollment) {
			t.Fatalf("err = %v", err)
		}
		// Card entered on the hosted page: confirm refreshes the enrollment and proceeds.
		if err := e.reapSrv.ActivateEnrollment(en.ReapEnrollmentID); err != nil {
			t.Fatal(err)
		}
		if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("validation", func(t *testing.T) {
		e := newEnv(t)
		r := e.create("", agents.RequestedItem{Description: "printer paper"})
		cases := []struct {
			name, id, addr string
			want           error
		}{
			{"missing address", r.ID, "", domain.ErrValidation},
			{"unknown address", r.ID, "00000000-0000-4000-8000-000000000000", domain.ErrValidation},
			{"unknown request", "00000000-0000-4000-8000-000000000001", e.addr.ID, domain.ErrNotFound},
		}
		for _, tc := range cases {
			if _, err := e.o.Confirm(e.ctx, tc.id, ConfirmInput{UserID: e.manager.ID, AddressID: tc.addr}); !errors.Is(err, tc.want) {
				t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
			}
		}
		if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
			t.Fatal(err)
		}
		e.idle()
		if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); !errors.Is(err, domain.ErrInvalidTransition) {
			t.Fatalf("second confirm err = %v", err)
		}
	})
}

func TestCancel(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper"})
	d, err := e.o.Cancel(e.ctx, r.ID, e.manager.ID)
	if err != nil || d.Request.Status != domain.StatusCancelled {
		t.Fatalf("cancel: %v %s", err, d.Request.Status)
	}
	if _, err := e.o.Cancel(e.ctx, r.ID, e.manager.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("second cancel err = %v", err)
	}
	if e.auditTypes(r.ID)[audit.RequestCancelled] != 1 {
		t.Fatal("cancel not audited")
	}
}

func TestCreateRequest_Validation(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	cases := []struct {
		name string
		in   CreateRequestInput
	}{
		{"no requester", CreateRequestInput{Utterance: "paper"}},
		{"empty", CreateRequestInput{RequesterID: e.manager.ID}},
		{"blank items", CreateRequestInput{RequesterID: e.manager.ID, Items: []agents.RequestedItem{{Description: "  "}}}},
		{"zero qty", CreateRequestInput{RequesterID: e.manager.ID, Items: []agents.RequestedItem{{Description: "paper", Qty: ptr(0)}}}},
	}
	for _, tc := range cases {
		if _, err := e.o.CreateRequest(e.ctx, tc.in); !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err = %v", tc.name, err)
		}
	}
}

func TestFlow_NothingUnderstood_Fails(t *testing.T) {
	e := newEnv(t, withoutEnrollment(), withParser(stubParser{}))
	r := e.create("hello there")
	d := e.wantStatus(r.ID, domain.StatusFailed)
	if d.Request.FailureReason == "" {
		t.Fatal("no failure reason")
	}
}

func TestFlow_ParserErrorFailsAfterRetries(t *testing.T) {
	p := stubParser{err: errors.New("model unavailable")}
	e := newEnv(t, withoutEnrollment(), withParser(p))
	r := e.create("printer paper")
	d := e.wantStatus(r.ID, domain.StatusFailed)
	if !strings.Contains(d.Request.FailureReason, "model unavailable") {
		t.Fatalf("reason %q", d.Request.FailureReason)
	}
}

func TestFlow_NoOffer_Rejected(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	r := e.create("", agents.RequestedItem{Description: "unobtainium widget"})
	d := e.wantStatus(r.ID, domain.StatusRejected)
	if d.LineItems[0].PolicyDecision != domain.DecisionReject || !hasReason(d.LineItems[0].Reasons, domain.ReasonNoOffer) {
		t.Fatalf("line = %+v", d.LineItems[0])
	}
}

func TestResume_RequeuesSearch(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	// Simulate a crash after parsing: the request is in searching with no search job.
	r := &domain.PurchaseRequest{RequesterID: e.manager.ID, RawUtterance: "printer paper", Status: domain.StatusParsing, Currency: "SGD"}
	if err := e.st.Requests().Create(e.ctx, r); err != nil {
		t.Fatal(err)
	}
	if err := e.o.Resume(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.idle()
	e.wantStatus(r.ID, domain.StatusQuoted)
}

func hasReason(rs []domain.Reason, code domain.ReasonCode) bool {
	for _, r := range rs {
		if r.Code == code {
			return true
		}
	}
	return false
}

func ptr[T any](v T) *T { return &v }

func (e *testEnv) pendingApprovals(requestID string) []domain.Approval {
	as, _ := e.st.Approvals().ListByRequest(e.ctx, requestID)
	var out []domain.Approval
	for _, a := range as {
		if a.Status == domain.ApprovalPending {
			out = append(out, a)
		}
	}
	return out
}
