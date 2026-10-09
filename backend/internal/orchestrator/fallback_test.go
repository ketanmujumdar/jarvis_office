package orchestrator

import (
	"context"
	"encoding/json"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/pgtest"
)

// Regression: Reap rejected a whole quote (400 AGENTIC_REQUEST_REJECTED {errors:[{field:items}]})
// because one line asked for more than the merchant's stock, and the request failed with a raw
// Reap error. Lines the merchant cannot supply now fall back to another merchant.

const (
	varPopPaper  = "var_pop_ik_copier_a4_80g" // best paper offer (Popular Bookstore, S$7.10)
	varKinoPaper = "var_kino_a4_copier_80g"   // next paper offer at another merchant (Kinokuniya)
)

var allPaperVariants = []string{varPopPaper, "var_pop_paperone_a4_80g", "var_pop_ik_copier_carton", varKinoPaper}

func (e *testEnv) lineFor(d domain.RequestDetail, desc string) domain.LineItem {
	e.t.Helper()
	for _, li := range d.LineItems {
		if strings.Contains(strings.ToLower(li.Description), desc) {
			return li
		}
	}
	e.t.Fatalf("no line %q in %+v", desc, d.LineItems)
	return domain.LineItem{}
}

func (e *testEnv) capVariants(max int, ids ...string) {
	e.t.Helper()
	for _, id := range ids {
		if err := e.reapSrv.SetVariantMaxQuantity(id, max); err != nil {
			e.t.Fatal(err)
		}
	}
}

// quoteBodies returns the item lists of every POST /agentic/quotes the fake received whose
// idempotency key contains (or, with exclude, does not contain) sub.
func (e *testEnv) quoteBodies(sub string, exclude bool) [][]reap.QuoteItem {
	var out [][]reap.QuoteItem
	for _, rr := range e.reapSrv.RequestsTo("POST", "/agentic/quotes") {
		if strings.Contains(rr.IdempotencyKey, sub) == exclude {
			continue
		}
		var req reap.CreateQuoteRequest
		_ = json.Unmarshal(rr.Body, &req)
		out = append(out, req.Items)
	}
	return out
}

func hasNote(li domain.LineItem, code domain.ReasonCode, parts ...string) bool {
	for _, r := range li.Reasons {
		if r.Code != code {
			continue
		}
		ok := true
		for _, p := range parts {
			ok = ok && strings.Contains(r.Message, p)
		}
		if ok {
			return true
		}
	}
	return false
}

// (a) + (c): paper and pens are both bought at Popular Bookstore; Popular cannot supply 10 reams.
// The paper line falls back to the next merchant, the pen line is still checked out at Popular
// exactly once, and the request reaches awaiting_payment (then ordered).
func TestFlow_ItemRejected_FallsBackToOtherMerchant(t *testing.T) { runItemFallback(t, newEnv(t)) }

func TestPostgres_ItemRejected_FallsBackToOtherMerchant(t *testing.T) {
	pgtest.DSN(t)
	runItemFallback(t, newEnv(t, withStore(pgStore)))
}

func runItemFallback(t *testing.T, e *testEnv) {
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)},
		agents.RequestedItem{Description: "ballpoint pens", Qty: ptr(20)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	paper, pens := e.lineFor(d, "paper"), e.lineFor(d, "pen")
	pen0, paper0 := offerByID(d, pens.SelectedOfferID), offerByID(d, paper.SelectedOfferID)
	if pen0.MerchantName != fakereap.MerchantPopular || paper0.MerchantName != fakereap.MerchantPopular {
		t.Fatalf("precondition: want both lines at Popular, got pen %q paper %q", pen0.MerchantName, paper0.MerchantName)
	}
	e.capVariants(5, paper0.ReapVariantID) // Popular has 5 reams of this paper

	e.confirm(r.ID)
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)

	// The paper line moved to another allowed merchant, with a note.
	paper = e.lineFor(d, "paper")
	pen1 := offerByID(d, paper.SelectedOfferID) // the merchant the moved line went to
	if pen1.MerchantName != fakereap.MerchantKinokuniya {
		t.Fatalf("paper line at %q", pen1.MerchantName)
	}
	if !hasNote(paper, domain.ReasonVendorSwitched, "Switched from Popular Bookstore (not enough stock for 10) to "+pen1.MerchantName) {
		t.Fatalf("paper line reasons = %+v", paper.Reasons)
	}
	if offerByID(d, e.lineFor(d, "pen").SelectedOfferID).ID != pen0.ID {
		t.Fatal("pen line must keep its Popular offer")
	}

	// (c) exactly one checkout per merchant; Popular's checked-out quote has only the paper.
	co := checkedOut(t, d)
	if len(co) != 2 || co[fakereap.MerchantPopular].ID == "" || co[pen1.MerchantName].ID == "" {
		t.Fatalf("checked-out merchants = %v", co)
	}
	if _, c, _ := e.reapSrv.Counts(); c != 2 {
		t.Fatalf("reap checkouts = %d, want 2", c)
	}
	open := map[string]int{}
	for _, p := range d.Payments {
		if p.Status != domain.PaymentFailed && p.Status != domain.PaymentExpired {
			open[p.MerchantName]++
		}
	}
	if open[fakereap.MerchantPopular] != 1 || open[pen1.MerchantName] != 1 {
		t.Fatalf("open payments per merchant = %v (%+v)", open, d.Payments)
	}
	popKey := quoteKey(co[fakereap.MerchantPopular])
	var popItems []reap.QuoteItem
	for _, rr := range e.reapSrv.RequestsTo("POST", "/agentic/quotes") {
		if rr.IdempotencyKey == popKey {
			var req reap.CreateQuoteRequest
			_ = json.Unmarshal(rr.Body, &req)
			popItems = req.Items
		}
	}
	if len(popItems) != 1 || popItems[0].VariantID != pen0.ReapVariantID || popItems[0].Quantity != 20 {
		t.Fatalf("Popular checkout quote items = %+v, want only 20 x pens", popItems)
	}
	// Every checkout went to a quote that holds the line it should (no quote with the 20 pens).
	for _, rr := range e.reapSrv.RequestsTo("POST", "/agentic/checkouts") {
		var req reap.CreateCheckoutRequest
		_ = json.Unmarshal(rr.Body, &req)
		if req.QuoteID != co[fakereap.MerchantPopular].ReapQuoteID && req.QuoteID != co[pen1.MerchantName].ReapQuoteID {
			t.Fatalf("checkout for unexpected quote %s", req.QuoteID)
		}
	}

	at := e.auditTypes(r.ID)
	if at[audit.CheckoutItemRejected] != 1 || at[audit.CheckoutOfferReplaced] != 1 {
		t.Fatalf("audit = %v", at)
	}
	evs := e.eventTypes(r.ID)
	for _, typ := range []events.Type{events.CheckoutItemRejected, events.CheckoutOfferReplaced, events.PolicyEvaluated, events.PaymentActionNeeded} {
		if !slices.Contains(evs, typ) {
			t.Errorf("missing SSE event %s", typ)
		}
	}
	// The voice agent's summary carries the switch.
	sum := agents.SummarizeRequest(d)
	found := false
	for _, ls := range sum.Lines {
		if strings.Contains(ls.Note, "Switched from Popular Bookstore") {
			found = true
		}
	}
	if !found {
		t.Fatalf("summary lines = %+v", sum.Lines)
	}

	e.approveCheckouts(d)
	e.refresh(r.ID)
	e.wantStatus(r.ID, domain.StatusOrdered)
}

// (b) no merchant can supply the paper: the request fails with a plain-language reason naming the
// merchant, quantity and item, and nothing is checked out.
func TestFlow_ItemRejected_NoFallback_FailsReadably(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)},
		agents.RequestedItem{Description: "ballpoint pens", Qty: ptr(20)})
	e.wantStatus(r.ID, domain.StatusQuoted)
	e.capVariants(5, allPaperVariants...)
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusFailed)
	reason := d.Request.FailureReason
	last := offerByID(d, e.lineFor(d, "paper").SelectedOfferID)
	want := last.MerchantName + " could not supply 10 × " + last.Title + " (likely limited stock). Try a smaller quantity or another item."
	if reason != want {
		t.Fatalf("failure reason = %q\nwant %q", reason, want)
	}
	if strings.Contains(reason, "reap") || strings.Contains(reason, "AGENTIC") {
		t.Fatalf("failure reason leaks the Reap error: %q", reason)
	}
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatalf("reap checkouts = %d, want 0", c)
	}
	if at := e.auditTypes(r.ID); at[audit.CheckoutItemRejected] < 1 {
		t.Fatalf("audit = %v", at)
	}
}

// Popular cannot supply 20 pens; the only other pen (Kinokuniya, S$2.60) is above the catalog
// ceiling of S$2.50, so the changed basket goes through the normal approval path first.
func TestFlow_ItemRejected_FallbackNeedsApproval(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "ballpoint pens", Qty: ptr(20)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	pen0 := offerByID(d, d.LineItems[0].SelectedOfferID)
	if pen0.MerchantName != fakereap.MerchantPopular {
		t.Fatalf("precondition: pen at %q", pen0.MerchantName)
	}
	e.capVariants(5, pen0.ReapVariantID)
	e.confirm(r.ID)
	d = e.wantStatus(r.ID, domain.StatusPendingApproval)
	a := e.pendingApproval(r.ID)
	if a.Kind != domain.ApprovalKindPolicy || len(a.Reasons) == 0 || a.Reasons[0].Code != domain.ReasonOverUnitCeiling {
		t.Fatalf("approval = %+v", a)
	}
	if of := offerByID(d, d.LineItems[0].SelectedOfferID); of.MerchantName != fakereap.MerchantKinokuniya || a.AmountCents != d.Request.TotalCents {
		t.Fatalf("line offer %+v, approval %d, request total %d", of, a.AmountCents, d.Request.TotalCents)
	}
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatalf("checkouts before approval = %d", c)
	}
	if _, err := e.appr.Approve(e.ctx, a.ID, e.approver, "ok"); err != nil {
		t.Fatal(err)
	}
	e.idle()
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if co := checkedOut(t, d); len(co) != 1 || co[fakereap.MerchantKinokuniya].ID == "" {
		t.Fatalf("checked out = %v", co)
	}
}

// Every paper offer is out of stock (409 VARIANT_UNAVAILABLE at quote time): after falling back
// through the merchants the request fails with a readable reason and nothing is checked out.
func TestFlow_QuoteErrorFailsRequest(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.wantStatus(r.ID, domain.StatusQuoted)
	for _, v := range allPaperVariants {
		if err := e.reapSrv.SetVariantAvailable(v, false); err != nil {
			t.Fatal(err)
		}
	}
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusFailed)
	if !strings.Contains(d.Request.FailureReason, "could not supply 10 × ") || !strings.Contains(d.Request.FailureReason, "out of stock") {
		t.Fatalf("failure reason %q", d.Request.FailureReason)
	}
	for _, p := range d.Payments {
		if p.Status != domain.PaymentFailed || p.ReapCheckoutID != "" {
			t.Fatalf("payments %+v", d.Payments)
		}
	}
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatalf("reap checkouts = %d", c)
	}
}

// A quote rejection that is not about items (e.g. idempotency or validation) still fails as before.
func TestFlow_NonItemQuoteRejectionStillFails(t *testing.T) {
	e := newEnv(t)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.reapSrv.InjectFault(fakereap.Fault{Method: "POST", Path: "/agentic/quotes", Status: 400, Code: reap.CodeRequestRejected})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusFailed)
	if !strings.HasPrefix(d.Request.FailureReason, "checkout failed:") {
		t.Fatalf("failure reason %q", d.Request.FailureReason)
	}
	if at := e.auditTypes(r.ID); at[audit.CheckoutItemRejected] != 0 {
		t.Fatalf("audit = %v", at)
	}
}

// ---------- pre-confirm quote check ----------

// A preflight rejection switches the line to another merchant before the request is quoted, and
// the agent summary says so. No checkout is created by the probe.
func TestPreflight_SwitchesLineBeforeQuoted(t *testing.T) {
	e := newEnv(t, withPreflight(5*time.Second))
	e.capVariants(5, varPopPaper)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)},
		agents.RequestedItem{Description: "coffee capsules", Qty: ptr(6)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatalf("preflight created %d checkouts", c)
	}
	paper := e.lineFor(d, "paper")
	of := offerByID(d, paper.SelectedOfferID)
	if of.ReapVariantID != varKinoPaper || paper.PolicyDecision != domain.DecisionAutoApprove {
		t.Fatalf("paper line = %+v offer %+v", paper, of)
	}
	if !hasNote(paper, domain.ReasonVendorSwitched, "Switched from Popular Bookstore (not enough stock for 10) to "+fakereap.MerchantKinokuniya) {
		t.Fatalf("paper reasons = %+v", paper.Reasons)
	}
	sum := agents.SummarizeRequest(d)
	var note string
	for _, ls := range sum.Lines {
		if ls.Note != "" {
			note = ls.Note
		}
	}
	if !strings.Contains(note, "Popular Bookstore") || !strings.Contains(note, "Kinokuniya") {
		t.Fatalf("summary note = %q (%+v)", note, sum.Lines)
	}
	at := e.auditTypes(r.ID)
	if at[audit.CheckoutItemRejected] != 1 || at[audit.CheckoutOfferReplaced] != 1 {
		t.Fatalf("audit = %v", at)
	}
	// The probe quotes are never reused: confirm creates its own live quote and checks out.
	probeQuotes := e.quoteBodies(":preflight", false)
	if len(probeQuotes) == 0 {
		t.Fatal("no preflight quotes recorded")
	}
	e.confirm(r.ID)
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if co := checkedOut(t, d); co[fakereap.MerchantKinokuniya].ID == "" || co[fakereap.MerchantPopular].ID != "" {
		t.Fatalf("checked out = %v", co)
	}
	if !hasNote(e.lineFor(d, "paper"), domain.ReasonVendorSwitched) {
		t.Fatal("note lost on confirm")
	}
	if got := len(e.quoteBodies(":preflight", false)); got != len(probeQuotes) {
		t.Fatalf("confirm sent preflight quotes (%d -> %d)", len(probeQuotes), got)
	}
	if at := e.auditTypes(r.ID); at[audit.CheckoutItemRejected] != 1 {
		t.Fatalf("checkout hit another rejection: %v", at)
	}
}

// No merchant can supply the paper: the line is marked unavailable with a plain reason before
// quoted; the rest of the order is still bought.
func TestPreflight_NoFallback_ReadableReason(t *testing.T) {
	e := newEnv(t, withPreflight(5*time.Second))
	e.capVariants(5, allPaperVariants...)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)},
		agents.RequestedItem{Description: "coffee capsules", Qty: ptr(6)})
	d := e.wantStatus(r.ID, domain.StatusQuoted)
	paper := e.lineFor(d, "paper")
	if paper.PolicyDecision != domain.DecisionReject || paper.SelectedOfferID != nil {
		t.Fatalf("paper line = %+v", paper)
	}
	if len(paper.Reasons) != 1 || paper.Reasons[0].Code != domain.ReasonUnavailable ||
		!strings.Contains(paper.Reasons[0].Message, "can't supply 10 × ") || !strings.Contains(paper.Reasons[0].Message, "(limited stock)") {
		t.Fatalf("paper reasons = %+v", paper.Reasons)
	}
	sum := agents.SummarizeRequest(d)
	ok := false
	for _, ls := range sum.Lines {
		ok = ok || strings.Contains(ls.Note, "can't supply 10 × ")
	}
	if !ok {
		t.Fatalf("summary = %+v", sum.Lines)
	}
	if _, c, _ := e.reapSrv.Counts(); c != 0 {
		t.Fatalf("preflight created %d checkouts", c)
	}
	e.confirm(r.ID)
	d = e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	paper = e.lineFor(d, "paper")
	if paper.PolicyDecision != domain.DecisionReject || len(paper.Reasons) != 1 || paper.Reasons[0].Code != domain.ReasonUnavailable {
		t.Fatalf("after confirm paper line = %+v", paper)
	}
	if co := checkedOut(t, d); len(co) != 1 || co[fakereap.MerchantCommonMan].ID == "" {
		t.Fatalf("checked out = %v", co)
	}
}

// probeFaults makes the preflight quotes fail; checkout-time quotes go through.
type probeFaults struct {
	reap.Client
	fail func(ctx context.Context) error
}

func (p probeFaults) CreateQuote(ctx context.Context, key string, req reap.CreateQuoteRequest) (reap.Quote, error) {
	if strings.Contains(key, ":preflight") {
		return reap.Quote{}, p.fail(ctx)
	}
	return p.Client.CreateQuote(ctx, key, req)
}

// A probe that times out or fails with a non-item error never blocks: the request is quoted as
// before and checkout proceeds.
func TestPreflight_ProbeErrorsStillQuoted(t *testing.T) {
	cases := map[string]func(ctx context.Context) error{
		"timeout": func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() },
		"5xx": func(context.Context) error {
			return &reap.APIError{HTTPStatus: 503, Code: reap.CodeServiceUnavailable, Message: "down"}
		},
		"quote temporarily unavailable": func(context.Context) error {
			return &reap.APIError{HTTPStatus: 503, Code: reap.CodeQuoteTempUnavailable, Message: "later"}
		},
	}
	for name, fail := range cases {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t, withPreflight(300*time.Millisecond), withReapWrapper(func(c reap.Client) reap.Client {
				return probeFaults{Client: c, fail: fail}
			}))
			start := time.Now()
			r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
			if time.Since(start) > 4*time.Second {
				t.Fatalf("preflight blocked for %s", time.Since(start))
			}
			d := e.wantStatus(r.ID, domain.StatusQuoted)
			paper := d.LineItems[0]
			if of := offerByID(d, paper.SelectedOfferID); of.ReapVariantID != varPopPaper || len(notesOf(paper.Reasons)) != 0 {
				t.Fatalf("paper line changed: %+v", paper)
			}
			if _, c, _ := e.reapSrv.Counts(); c != 0 {
				t.Fatalf("checkouts = %d", c)
			}
			e.confirm(r.ID)
			e.wantStatus(r.ID, domain.StatusAwaitingPayment)
		})
	}
}

// When the preflight could not see the problem (probe timed out), the checkout-time fallback still
// switches the line.
func TestPreflight_TimeoutThenCheckoutFallback(t *testing.T) {
	e := newEnv(t, withPreflight(200*time.Millisecond), withReapWrapper(func(c reap.Client) reap.Client {
		return probeFaults{Client: c, fail: func(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }}
	}))
	e.capVariants(5, varPopPaper)
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.wantStatus(r.ID, domain.StatusQuoted)
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	if co := checkedOut(t, d); len(co) != 1 || co[fakereap.MerchantKinokuniya].ID == "" {
		t.Fatalf("checked out = %v", co)
	}
}
