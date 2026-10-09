package orchestrator

import (
	"context"
	"errors"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/allowlist"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
)

// Money-safety regression tests for the Reap checkout path. Every test runs offline against
// fakereap through the real reap.HTTPClient.

func (e *testEnv) confirm(id string) {
	e.t.Helper()
	if _, err := e.o.Confirm(e.ctx, id, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); err != nil {
		e.t.Fatalf("confirm: %v", err)
	}
	e.idle()
}

func (e *testEnv) setPolicy(f func(*domain.PolicyConfig)) {
	e.t.Helper()
	cfg, err := e.st.Policy().Get(e.ctx)
	if err != nil {
		e.t.Fatal(err)
	}
	f(&cfg)
	if err := e.st.Policy().Update(e.ctx, &cfg, e.manager.ID); err != nil {
		e.t.Fatal(err)
	}
}

// checkedOut returns the payments that have a Reap checkout, keyed by merchant.
func checkedOut(t *testing.T, d domain.RequestDetail) map[string]domain.Payment {
	t.Helper()
	out := map[string]domain.Payment{}
	for _, p := range d.Payments {
		if p.ReapCheckoutID == "" {
			continue
		}
		if _, dup := out[p.MerchantName]; dup {
			t.Fatalf("two checkouts for merchant %s: %+v", p.MerchantName, d.Payments)
		}
		out[p.MerchantName] = p
	}
	return out
}

// A 2-merchant basket where the second merchant's checkout fails retryably on the first job
// attempt: the retry must reuse merchant A's open checkout, never open a second one (double charge).
func TestFlow_PartialCheckoutRetry_OneCheckoutPerMerchant(t *testing.T) {
	e := newEnv(t)
	r := e.create("We're out of printer paper and coffee pods, reorder the usual.")
	e.wantStatus(r.ID, domain.StatusQuoted)
	// Let the first CreateCheckout through, then fail the next 4 calls (1 + client MaxRetries 3),
	// so the first job attempt ends with merchant A checked out and merchant B not.
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/checkouts", Status: 503,
		Code: reap.CodeServiceUnavailable, Skip: 1, Times: 4})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if co := checkedOut(t, d); len(co) != 2 {
		t.Fatalf("checked-out merchants = %v", co)
	}
	if _, c, _ := e.reapSrv.Counts(); c != 2 {
		t.Fatalf("reap checkouts = %d, want exactly 2 (one per merchant)", c)
	}
	open := 0
	for _, p := range d.Payments {
		if p.Status != domain.PaymentFailed && p.Status != domain.PaymentExpired {
			open++
		}
	}
	if open != 2 {
		t.Fatalf("open payments = %d, want 2: %+v", open, d.Payments)
	}
	// Drift was checked over every payment of the request (total = both merchants).
	var sum domain.Cents
	for _, p := range d.Payments {
		sum += p.QuotedCents
	}
	if d.Request.TotalCents != sum {
		t.Fatalf("request total %d, want sum of all payments %d", d.Request.TotalCents, sum)
	}
}

// quoteGroup is called again (as on a crash + Resume) for a merchant that already has an open
// checkout: it must return that payment, not create a new one.
func TestQuoteGroup_ReusesCheckedOutPayment(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	enr, err := e.o.activeEnrollment(e.ctx)
	if err != nil {
		t.Fatal(err)
	}
	g := basketGroups(d)
	if len(g) != 1 {
		t.Fatalf("groups = %+v", g)
	}
	p, err := e.o.quoteGroup(e.ctx, d, g[0], enr, e.addr)
	if err != nil {
		t.Fatal(err)
	}
	if p.ID != d.Payments[0].ID || p.ReapCheckoutID == "" {
		t.Fatalf("quoteGroup returned %+v, want the checked-out payment %s", p, d.Payments[0].ID)
	}
	if q, c, _ := e.reapSrv.Counts(); q != 1 || c != 1 {
		t.Fatalf("quotes=%d checkouts=%d, want 1/1", q, c)
	}
}

// The store refuses a second open payment for the same request and merchant.
func TestStore_OneOpenPaymentPerMerchant(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	p0 := d.Payments[0]
	dup := domain.Payment{RequestID: r.ID, VendorID: p0.VendorID, MerchantName: p0.MerchantName, Status: domain.PaymentQuoting, IdempotencyKey: "dup-key"}
	if err := e.st.Payments().Create(e.ctx, &dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second open payment: err = %v, want ErrConflict", err)
	}
	failed := domain.Payment{RequestID: r.ID, VendorID: p0.VendorID, MerchantName: p0.MerchantName, Status: domain.PaymentFailed, IdempotencyKey: "dup-key-2"}
	if err := e.st.Payments().Create(e.ctx, &failed); err != nil {
		t.Fatalf("failed payments are not limited: %v", err)
	}
}

// Real Reap quotes include shipping; the approved total is items only. A small order (below the
// free-shipping threshold) must not raise a false price-drift approval.
func TestFlow_ShippingIsNotDrift(t *testing.T) {
	cases := []struct {
		name      string
		qty       int
		pricePct  int64 // live price as % of the searched price
		wantDrift bool
	}{
		{"small order, same price, shipping charged", 1, 100, false},
		{"small order, +4% within tolerance", 1, 104, false},
		{"small order, +20% drift", 1, 120, true},
		{"large order (free shipping), +20% drift", 10, 120, true},
		{"large order, same price", 10, 100, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(tc.qty)})
			d := e.wantStatus(r.ID, domain.StatusQuoted)
			best := offerByID(d, d.LineItems[0].SelectedOfferID)
			if tc.pricePct != 100 {
				if err := e.reapSrv.SetVariantPrice(best.ReapVariantID, int64(best.UnitPriceCents)*tc.pricePct/100); err != nil {
					t.Fatal(err)
				}
			}
			e.confirm(r.ID)
			if tc.wantDrift {
				e.wantStatus(r.ID, domain.StatusPendingApproval)
				if a := e.pendingApproval(r.ID); a.Kind != domain.ApprovalKindPriceDrift {
					t.Fatalf("approval = %+v", a)
				}
				return
			}
			d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
			if n := len(e.pendingApprovals(r.ID)); n != 0 {
				t.Fatalf("pending approvals = %d", n)
			}
			if tc.qty == 1 && (d.Request.ShippingCents != fakereap.StandardShippingCents || d.Payments[0].ShippingCents != fakereap.StandardShippingCents) {
				t.Fatalf("shipping not recorded: request %d payment %d", d.Request.ShippingCents, d.Payments[0].ShippingCents)
			}
		})
	}
}

// An auto-approved order whose live total is within the drift tolerance but over a hard limit
// must go to approval instead of being charged.
func TestFlow_WithinDriftButOverLimit(t *testing.T) {
	cases := []struct {
		name       string
		policy     func(*domain.PolicyConfig, domain.Cents)
		wantReason domain.ReasonCode // "" = no approval, checkout goes ahead
	}{
		{"over per-order limit", func(c *domain.PolicyConfig, approved domain.Cents) { c.PerOrderLimitCents = approved + 100 }, domain.ReasonOverOrderLimit},
		{"over monthly budget", func(c *domain.PolicyConfig, approved domain.Cents) { c.MonthlyBudgetCents = approved + 100 }, domain.ReasonOverMonthlyBudget},
		{"within limits", func(c *domain.PolicyConfig, approved domain.Cents) { c.PerOrderLimitCents = approved + 1000 }, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
			d := e.wantStatus(r.ID, domain.StatusQuoted)
			approved := d.Request.TotalCents
			e.setPolicy(func(c *domain.PolicyConfig) { tc.policy(c, approved) })
			best := offerByID(d, d.LineItems[0].SelectedOfferID)
			// +4%: inside the 5% drift tolerance, but above approved + S$1.
			if err := e.reapSrv.SetVariantPrice(best.ReapVariantID, int64(best.UnitPriceCents)*104/100); err != nil {
				t.Fatal(err)
			}
			e.confirm(r.ID)
			if tc.wantReason == "" {
				e.wantStatus(r.ID, domain.StatusAwaitingPayment)
				return
			}
			e.wantStatus(r.ID, domain.StatusPendingApproval)
			a := e.pendingApproval(r.ID)
			if a.Kind != domain.ApprovalKindPolicy || !hasReason(a.Reasons, tc.wantReason) || a.AmountCents <= approved {
				t.Fatalf("approval = %+v", a)
			}
			if _, c, _ := e.reapSrv.Counts(); c != 0 {
				t.Fatal("checkout created over a hard limit")
			}
			if e.auditTypes(r.ID)[audit.CheckoutLimitExceeded] != 1 {
				t.Fatal("limit breach not audited")
			}
			// The approver takes responsibility: approving lets the checkout go ahead.
			if _, err := e.appr.Approve(e.ctx, a.ID, e.approver, "ok"); err != nil {
				t.Fatal(err)
			}
			e.idle()
			e.wantStatus(r.ID, domain.StatusAwaitingPayment)
		})
	}
}

// Two managers confirm orders at the same time that together exceed the monthly budget: only one
// may be auto-approved.
func TestConfirm_ConcurrentBudget(t *testing.T) { runConcurrentBudget(t, newEnv(t)) }

func runConcurrentBudget(t *testing.T, e *testEnv) {
	r1 := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	r2 := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	d1 := e.wantStatus(r1.ID, domain.StatusQuoted)
	e.wantStatus(r2.ID, domain.StatusQuoted)
	e.setPolicy(func(c *domain.PolicyConfig) { c.MonthlyBudgetCents = d1.Request.TotalCents * 3 / 2 })
	var wg sync.WaitGroup
	errs := make([]error, 2)
	for i, id := range []string{r1.ID, r2.ID} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = e.o.Confirm(e.ctx, id, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID})
		}()
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	e.idle()
	var statuses []domain.RequestStatus
	for _, id := range []string{r1.ID, r2.ID} {
		statuses = append(statuses, e.detail(id).Request.Status)
	}
	slices.Sort(statuses)
	if !slices.Equal(statuses, []domain.RequestStatus{domain.StatusAwaitingPayment, domain.StatusPendingApproval}) {
		t.Fatalf("statuses = %v, want one awaiting_payment and one pending_approval", statuses)
	}
	for _, id := range []string{r1.ID, r2.ID} {
		if e.detail(id).Request.Status == domain.StatusPendingApproval {
			if a := e.pendingApproval(id); !hasReason(a.Reasons, domain.ReasonOverMonthlyBudget) {
				t.Fatalf("approval reasons = %+v", a.Reasons)
			}
		}
	}
}

// committedSpend counts completed payments, unpaid approved/in-flight requests and open checkouts
// of closed requests, but never the request being evaluated.
func TestCommittedSpend(t *testing.T) {
	e := newEnv(t)
	r1 := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r1.ID)
	d1 := e.wantStatus(r1.ID, domain.StatusAwaitingPayment)
	r2 := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	got, err := e.o.committedSpend(e.ctx, r2.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got != d1.Request.TotalCents {
		t.Fatalf("committed = %d, want r1 total %d", got, d1.Request.TotalCents)
	}
	if self, _ := e.o.committedSpend(e.ctx, r1.ID); self != 0 {
		t.Fatalf("committed excluding r1 = %d, want 0", self)
	}
	e.approveCheckouts(d1)
	e.refresh(r1.ID)
	e.wantStatus(r1.ID, domain.StatusOrdered)
	if got, _ := e.o.committedSpend(e.ctx, r2.ID); got != d1.Request.TotalCents {
		t.Fatalf("committed after payment = %d, want %d (counted once)", got, d1.Request.TotalCents)
	}
}

// The checkout response is lost after Reap processed it (or the connection drops): every retry,
// in-process and job-level, must send the same Idempotency-Key so exactly one checkout exists.
func TestFlow_CheckoutResponseLost_SameKey(t *testing.T) {
	for _, tc := range []struct {
		name  string
		fault fakereap.Fault
	}{
		{"503 after processing", fakereap.Fault{Status: 503, Code: reap.CodeServiceUnavailable}},
		{"connection dropped after processing", fakereap.Fault{Drop: true}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
			f := tc.fault
			f.Method, f.Path, f.Stage, f.Times = "POST", "/agentic/checkouts", fakereap.FaultAfterProcess, 5 // > client MaxRetries+1
			e.reapSrv.InjectFault(f)
			e.confirm(r.ID)
			d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
			if _, c, _ := e.reapSrv.Counts(); c != 1 {
				t.Fatalf("checkouts = %d, want 1", c)
			}
			reqs := e.reapSrv.RequestsTo("POST", "/agentic/checkouts")
			if len(reqs) < 6 {
				t.Fatalf("checkout requests = %d, want the job-level retry to have run", len(reqs))
			}
			want := d.Payments[0].IdempotencyKey + ":checkout"
			for _, rq := range reqs {
				if rq.IdempotencyKey != want {
					t.Fatalf("checkout key %q, want %q", rq.IdempotencyKey, want)
				}
			}
		})
	}
}

// Same for quotes with a generic 503: the key is kept, so Reap replays the quote it created.
func TestFlow_QuoteResponseLost_SameKey(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/quotes", Status: 503, Code: reap.CodeServiceUnavailable,
		Stage: fakereap.FaultAfterProcess, Times: 5})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if q, c, _ := e.reapSrv.Counts(); q != 1 || c != 1 {
		t.Fatalf("quotes=%d checkouts=%d", q, c)
	}
	want := d.Payments[0].IdempotencyKey + ":quote"
	for _, rq := range e.reapSrv.RequestsTo("POST", "/agentic/quotes") {
		if rq.IdempotencyKey != want {
			t.Fatalf("quote key %q, want %q", rq.IdempotencyKey, want)
		}
	}
}

// QUOTE_TEMPORARILY_UNAVAILABLE is replayed by Reap for the same key, so the next attempt must use
// a new key (a quote charges nothing).
func TestFlow_QuoteTemporarilyUnavailable_NewKey(t *testing.T) {
	e := newEnv(t, withReapOptions(func(o *fakereap.Options) { o.CacheServerErrors = true }))
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/quotes", Status: 503, Code: reap.CodeQuoteTempUnavailable,
		Stage: fakereap.FaultInHandler})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	base := d.Payments[0].IdempotencyKey
	var keys []string
	for _, rq := range e.reapSrv.RequestsTo("POST", "/agentic/quotes") {
		keys = append(keys, rq.IdempotencyKey)
	}
	if !slices.Equal(keys, []string{base + ":quote", base + ":quote:1"}) {
		t.Fatalf("quote keys = %v", keys)
	}
}

// A transport failure on POST /agentic/checkouts that outlasts the client's own retries is retried
// by the job (not failed), and still ends with one checkout.
func TestFlow_CheckoutTransportErrorIsRetried(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/checkouts", Drop: true, Times: 5})
	e.confirm(r.ID)
	e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if _, c, _ := e.reapSrv.Counts(); c != 1 {
		t.Fatalf("checkouts = %d", c)
	}
}

// Approvals never expire, Reap quotes do (15 min): approving a drift approval later must re-quote,
// not fail the request.
func TestFlow_QuoteExpiresWhileApprovalPending(t *testing.T) {
	for _, tc := range []struct {
		name   string
		expire func(e *testEnv, d domain.RequestDetail)
	}{
		{"both clocks moved 20 minutes", func(e *testEnv, _ domain.RequestDetail) {
			e.reapSrv.Advance(20 * time.Minute)
			e.o.d.Clock = func() time.Time { return time.Now().Add(20 * time.Minute) }
		}},
		{"reap expired the quote early", func(e *testEnv, d domain.RequestDetail) {
			if err := e.reapSrv.ExpireQuote(d.Payments[0].ReapQuoteID); err != nil {
				e.t.Fatal(err)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t)
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
			d := e.wantStatus(r.ID, domain.StatusQuoted)
			best := offerByID(d, d.LineItems[0].SelectedOfferID)
			if err := e.reapSrv.SetVariantPrice(best.ReapVariantID, int64(best.UnitPriceCents)*120/100); err != nil {
				t.Fatal(err)
			}
			e.confirm(r.ID)
			d = e.wantStatus(r.ID, domain.StatusPendingApproval)
			a := e.pendingApproval(r.ID)
			tc.expire(e, d)
			if _, err := e.appr.Approve(e.ctx, a.ID, e.approver, "fine"); err != nil {
				t.Fatal(err)
			}
			e.idle()
			e.wantStatus(r.ID, domain.StatusAwaitingPayment)
			if q, c, _ := e.reapSrv.Counts(); q != 2 || c != 1 {
				t.Fatalf("quotes=%d checkouts=%d, want a fresh quote and one checkout", q, c)
			}
		})
	}
}

// A vendor disallowed (or taken off the allow-list) between approval and checkout is never quoted.
func TestFlow_VendorDisallowedBeforeCheckout(t *testing.T) {
	for _, tc := range []struct {
		name    string
		disable func(e *testEnv, vendorID string)
	}{
		{"vendor row disallowed", func(e *testEnv, vendorID string) {
			v, err := e.st.Vendors().Get(e.ctx, vendorID)
			if err != nil {
				e.t.Fatal(err)
			}
			v.Allowed = false
			if err := e.st.Vendors().Update(e.ctx, &v); err != nil {
				e.t.Fatal(err)
			}
		}},
		{"domain off the allow-list", func(e *testEnv, _ string) { e.o.allow = allowlist.Of("popular.com.sg") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, deskProducts())
			r := e.create("Get us a standing desk for the new hire.")
			e.confirm(r.ID)
			d := e.wantStatus(r.ID, domain.StatusPendingApproval)
			best := offerByID(d, d.LineItems[0].SelectedOfferID)
			tc.disable(e, *best.VendorID)
			if _, err := e.appr.Approve(e.ctx, e.pendingApproval(r.ID).ID, e.approver, "ok"); err != nil {
				t.Fatal(err)
			}
			e.idle()
			d = e.wantStatus(r.ID, domain.StatusFailed)
			if q, c, _ := e.reapSrv.Counts(); q != 0 || c != 0 {
				t.Fatalf("quotes=%d checkouts=%d, want none", q, c)
			}
			if len(d.Payments) != 0 {
				t.Fatalf("payments = %+v", d.Payments)
			}
		})
	}
}

// Off-allow-list vendors fail closed in policy even when their vendor row says allowed.
func TestPolicy_OffAllowListVendorRejected(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	e.o.allow = allowlist.Of("unrelated.example.sg")
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	d := e.wantStatus(r.ID, domain.StatusRejected)
	if !hasReason(d.LineItems[0].Reasons, domain.ReasonNoOffer) && !hasReason(d.LineItems[0].Reasons, domain.ReasonVendorNotAllowed) {
		t.Fatalf("reasons = %+v", d.LineItems[0].Reasons)
	}
}

// Reap cannot cancel a checkout: once one is open the request cannot be cancelled.
func TestCancel_RefusedOnceCheckoutOpen(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r.ID)
	e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if _, err := e.o.Cancel(e.ctx, r.ID, e.manager.ID); !errors.Is(err, domain.ErrInvalidTransition) {
		t.Fatalf("cancel err = %v", err)
	}
	e.wantStatus(r.ID, domain.StatusAwaitingPayment)
}

// One merchant's payment fails, so the request fails; the other merchant's checkout is still open
// on Reap. Its link is hidden, polling continues, and a late charge is recorded and alerted.
func TestFlow_LateChargeAfterRequestFailed(t *testing.T) {
	e := newEnv(t)
	r := e.create("We're out of printer paper and coffee pods, reorder the usual.")
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if len(d.Payments) != 2 {
		t.Fatalf("payments = %+v", d.Payments)
	}
	if err := e.reapSrv.FailCheckout(d.Payments[0].ReapCheckoutID); err != nil {
		t.Fatal(err)
	}
	e.refresh(r.ID)
	d = e.wantStatus(r.ID, domain.StatusFailed)
	other := d.Payments[1]
	if other.Status != domain.PaymentRequiresAction || other.ApprovalURL != "" {
		t.Fatalf("open payment of a failed request must hide its link: %+v", other)
	}
	if !hasOpenCheckout(d.Payments) {
		t.Fatal("hasOpenCheckout = false")
	}

	// The user approves the stale link on Reap anyway; on restart Resume keeps polling it.
	if err := e.reapSrv.ApproveCheckout(other.ReapCheckoutID); err != nil {
		t.Fatal(err)
	}
	if err := e.o.Resume(e.ctx); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusFailed)
	var got domain.Payment
	for _, p := range d.Payments {
		if p.ID == other.ID {
			got = p
		}
	}
	if got.Status != domain.PaymentCompleted || got.FinalCents == nil {
		t.Fatalf("late charge not recorded: %+v", got)
	}
	if e.auditTypes(r.ID)[audit.ReapChargeAfterClose] != 1 {
		t.Fatal("late charge not audited")
	}
	if !slices.Contains(e.eventTypes(r.ID), events.PaymentAlert) {
		t.Fatal("late charge not alerted")
	}
	mtd, err := e.st.Spend().MonthToDate(e.ctx, time.Now())
	if err != nil {
		t.Fatal(err)
	}
	if mtd < *got.FinalCents {
		t.Fatalf("month to date %d does not include the late charge %d", mtd, *got.FinalCents)
	}
}

func TestFlow_FinalAmountAboveQuoteIsAlerted(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	p := d.Payments[0]
	if err := e.reapSrv.SetCheckoutAmount(p.ReapCheckoutID, int64(p.QuotedCents)+500); err != nil {
		t.Fatal(err)
	}
	e.approveCheckouts(d)
	e.refresh(r.ID)
	e.wantStatus(r.ID, domain.StatusOrdered)
	if e.auditTypes(r.ID)[audit.ReapFinalAmountMismatch] != 1 {
		t.Fatal("overcharge not audited")
	}
}

func TestCheckoutFailureMessages(t *testing.T) {
	quoteErr := func(reason string) error {
		return &reap.APIError{HTTPStatus: 400, Code: reap.CodeQuoteUnfulfillable, Message: "m", Detail: map[string]any{"reason": reason}}
	}
	cases := []struct {
		err  error
		want string
	}{
		{quoteErr(reap.ReasonInvalidPhone), "phone number"},
		{quoteErr(reap.ReasonStateRequired), "delivery address"},
		{quoteErr(reap.ReasonAddressLine2Required), "delivery address"},
		{quoteErr(reap.ReasonItemsUnshippable), "cannot ship"},
		{&reap.APIError{HTTPStatus: 400, Code: reap.CodeQuoteUnfulfillable, Message: "m"}, "no longer available"},
		{&reap.APIError{HTTPStatus: 404, Code: reap.CodeEnrollmentNotFound, Message: "m"}, "card enrollment"},
		{context.DeadlineExceeded, "checkout failed"},
	}
	for _, tc := range cases {
		if got := checkoutFailure(tc.err); !containsFold(got, tc.want) {
			t.Errorf("checkoutFailure(%v) = %q, want it to mention %q", tc.err, got, tc.want)
		}
	}
}

func containsFold(s, sub string) bool {
	return strings.Contains(strings.ToLower(s), strings.ToLower(sub))
}

// An enrollment Reap no longer knows (ENROLLMENT_NOT_FOUND) means "no card", not an upstream error.
func TestConfirm_StaleEnrollmentIsNoActiveEnrollment(t *testing.T) {
	e := newEnv(t, withoutEnrollment())
	en := domain.Enrollment{ReapEnrollmentID: "enr_unknown_to_reap", OwnerRef: e.manager.ID, OwnerEmail: e.manager.Email,
		Status: domain.EnrollmentRequiresAction, NextActionURL: "https://reap.example/enroll"}
	if err := e.st.Enrollments().Create(e.ctx, &en); err != nil {
		t.Fatal(err)
	}
	r := e.create("", agents.RequestedItem{Description: "printer paper"})
	if _, err := e.o.Confirm(e.ctx, r.ID, ConfirmInput{UserID: e.manager.ID, AddressID: e.addr.ID}); !errors.Is(err, domain.ErrNoActiveEnrollment) {
		t.Fatalf("err = %v, want ErrNoActiveEnrollment", err)
	}
}
