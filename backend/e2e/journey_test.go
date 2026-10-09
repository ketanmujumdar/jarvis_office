//go:build e2e

package e2e

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/e2e/harness"
)

// TestJourney runs the whole product journey against one api process. Steps are ordered: card
// enrollment happens in step 2, so later steps can check out.
func TestJourney(t *testing.T) {
	requireSuite(t)
	a := loginAll(t)
	var spentCents int64 // completed spend this run (for the monthly spend check)

	t.Run("01 auth, roles and lookups", func(t *testing.T) {
		r, err := a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/auth/me", nil)
		must(t, r, err, http.StatusOK, "me")
		if me := decode[user](t, r); me.Role != "manager" || me.ID != a.managerUser.ID {
			t.Fatalf("me = %+v", me)
		}
		r, err = suite.anon.Do(ctxT(t), http.MethodGet, "/api/v1/requests", nil)
		must(t, r, err, http.StatusUnauthorized, "requests without token")
		r, err = a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/admin/policy", nil)
		must(t, r, err, http.StatusForbidden, "manager reading admin policy")
		r, err = a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/approvals", nil)
		must(t, r, err, http.StatusForbidden, "manager listing approvals")

		addrs := addresses(t, a.manager)
		if len(addrs) != len(suite.seed.Addresses) || len(addrs) < 3 {
			t.Fatalf("addresses = %d, want %d seeded", len(addrs), len(suite.seed.Addresses))
		}
		if !addrs[0].IsDefault {
			t.Fatalf("default address must come first: %+v", addrs[0])
		}
		r, err = a.admin.Do(ctxT(t), http.MethodGet, "/api/v1/admin/policy", nil)
		must(t, r, err, http.StatusOK, "admin policy")
		pol := decode[struct {
			Currency   string  `json:"currency"`
			PerOrder   int64   `json:"per_order_limit_cents"`
			Monthly    int64   `json:"monthly_budget_cents"`
			PriceDrift float64 `json:"price_drift_pct"`
		}](t, r)
		if pol.Currency != "SGD" || pol.PerOrder != cents(suite.seed.Policy.PerOrderLimitSGD) || pol.PriceDrift != suite.seed.Policy.PriceDriftPct {
			t.Fatalf("policy = %+v", pol)
		}
	})

	// The pens request is reused by step 03.
	var pens purchaseRequest

	t.Run("02 confirm needs a card; enroll via the Reap-hosted page", func(t *testing.T) {
		pens = createRequest(t, a.manager, "Order 10 ballpoint pens", []map[string]any{{"description": "ballpoint pens", "qty": 10}})
		waitStatus(t, a.manager, pens.ID, "quoted")
		hq := addressByLabel(t, a.manager, seedAddressLabel(t, "hq-marina"))
		r := confirm(t, a.manager, pens.ID, hq.ID)
		if r.Status != http.StatusConflict || r.ErrorCode() != "no_active_enrollment" {
			t.Fatalf("confirm without card: %d %s, want 409 no_active_enrollment", r.Status, trim(r.Body))
		}

		r, err := a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/enrollments", nil)
		must(t, r, err, http.StatusCreated, "start enrollment")
		enr := decode[struct {
			ID               string `json:"id"`
			ReapEnrollmentID string `json:"reap_enrollment_id"`
			Status           string `json:"status"`
			NextActionURL    string `json:"next_action_url"`
		}](t, r)
		if enr.Status != "REQUIRES_ACTION" || !strings.HasPrefix(enr.NextActionURL, suite.reap.URL()+"/hosted/enrollments/") {
			t.Fatalf("enrollment = %+v", enr)
		}
		var sawEnroll bool
		for _, c := range suite.reap.Calls() {
			if c.Method == http.MethodPost && c.Path == "/agentic/enrollments" {
				sawEnroll = true
				var body struct {
					Source string `json:"source"`
					Owner  struct {
						Type string `json:"type"`
						ID   string `json:"id"`
					} `json:"owner"`
				}
				_ = json.Unmarshal(c.Body, &body)
				if body.Source != "EXTERNAL" || body.Owner.Type != "CLIENT_REFERENCE" || body.Owner.ID == "" || c.IdempotencyKey == "" {
					t.Fatalf("enrollment call to Reap: %s (idempotency key set: %v)", c.Body, c.IdempotencyKey != "")
				}
			}
		}
		if !sawEnroll {
			t.Fatal("api never called POST /agentic/enrollments")
		}

		// The user enters the card on Reap's page (simulated); the api refreshes on read.
		if suite.reap.ActivateEnrollment(enr.ReapEnrollmentID) != 1 {
			t.Fatalf("fake reap has no pending enrollment %q", enr.ReapEnrollmentID)
		}
		deadline := time.Now().Add(15 * time.Second)
		for {
			r, err = a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/enrollments/current", nil)
			must(t, r, err, http.StatusOK, "current enrollment")
			if strings.Contains(string(r.Body), `"ACTIVE"`) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("enrollment never became ACTIVE: %s", trim(r.Body))
			}
			time.Sleep(150 * time.Millisecond)
		}
		if body := string(r.Body); strings.Contains(body, harness.FakeCardLast4) || strings.Contains(strings.ToLower(body), "last4") {
			t.Fatalf("enrollment response leaks card data: %s", body)
		}
	})

	t.Run("03 delivery address selection reaches the Reap quote", func(t *testing.T) {
		if pens.ID == "" {
			t.Skip("step 02 did not create the request")
		}
		r := confirm(t, a.manager, pens.ID, harness.NewUUID())
		if r.Status != http.StatusBadRequest && r.Status != http.StatusNotFound {
			t.Fatalf("confirm with unknown address: %d %s, want 400/404", r.Status, trim(r.Body))
		}
		if d := getDetail(t, a.manager, pens.ID); d.Request.Status != "quoted" {
			t.Fatalf("bad confirm changed status to %s", d.Request.Status)
		}

		label := seedAddressLabel(t, "one-north")
		chosen := addressByLabel(t, a.manager, label)
		r = confirm(t, a.manager, pens.ID, chosen.ID)
		must(t, r, nil, http.StatusOK, "confirm with one-north")
		d := decode[requestDetail](t, r)
		if d.Request.AddressID == nil || *d.Request.AddressID != chosen.ID {
			t.Fatalf("address_id = %v, want %s", d.Request.AddressID, chosen.ID)
		}
		if d.Address != nil && d.Address.Label != label {
			t.Fatalf("detail address = %+v", d.Address)
		}
		if d.Request.ConfirmedBy == nil || *d.Request.ConfirmedBy != a.managerUser.ID {
			t.Fatalf("confirmed_by = %v", d.Request.ConfirmedBy)
		}

		d = waitStatus(t, a.manager, pens.ID, "awaiting_payment")
		if len(d.Payments) != 1 {
			t.Fatalf("payments = %d, want 1", len(d.Payments))
		}
		dom, email, ship, ok := suite.reap.Quote(d.Payments[0].ReapQuoteID)
		if !ok {
			t.Fatalf("payment quote %q unknown to fake reap", d.Payments[0].ReapQuoteID)
		}
		if dom != "popular.com.sg" || email != chosen.Email || ship["postalCode"] != chosen.PostalCode ||
			ship["phone"] != chosen.Phone || ship["firstName"] != chosen.FirstName || ship["country"] != "SG" {
			t.Fatalf("quote went to %s / %s / %v; want one-north (%s, %s)", dom, email, ship, chosen.PostalCode, chosen.Email)
		}
		if d.Payments[0].QuotedCents != 10*cents(harness.SeedPrice(2.50, 0)) {
			t.Fatalf("quoted_cents = %d", d.Payments[0].QuotedCents)
		}

		// Reap has no checkout-cancel API: once the approval link exists the request can no
		// longer be cancelled (the link could still be paid while Jarvis says "cancelled").
		r, err := a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/requests/"+pens.ID+"/cancel", nil)
		must(t, r, err, http.StatusConflict, "cancel with an open Reap checkout")
		if s := getDetail(t, a.manager, pens.ID).Request.Status; s != "awaiting_payment" {
			t.Fatalf("status after refused cancel = %s", s)
		}
		r = confirm(t, a.manager, pens.ID, chosen.ID)
		if r.Status != http.StatusConflict {
			t.Fatalf("second confirm: %d, want 409", r.Status)
		}
	})

	t.Run("04 PRD example 1: printer paper and coffee pods, the usual", func(t *testing.T) {
		stream, err := harness.Subscribe(ctxT(t), suite.api.BaseURL, a.manager.Token, "")
		if err != nil {
			t.Fatalf("subscribe SSE: %v%s", err, logTail())
		}
		defer stream.Close()

		utterance := "We're out of printer paper and coffee pods, reorder the usual."
		pr := createRequest(t, a.manager, utterance, nil)
		if pr.Status != "parsing" || pr.Currency != "SGD" || pr.RawUtterance != utterance {
			t.Fatalf("created = %+v", pr)
		}
		d := waitStatus(t, a.manager, pr.ID, "quoted")

		parsed := false
		for _, c := range suite.llm.Calls() {
			if c.HasSchema && strings.Contains(c.LastUser, "printer paper") {
				parsed = true
			}
		}
		if !parsed {
			t.Fatal("the LLM parser was never called with the utterance")
		}

		paper, _ := harness.CatalogItem(suite.seed, "a4-copier-paper-ream")
		pods, _ := harness.CatalogItem(suite.seed, "coffee-capsules")
		want := map[string]struct {
			qty      int
			merchant string
			unit     int64
		}{
			catalogID(t, a.manager, paper.SKU): {paper.DefaultQty, "Popular Bookstore", cents(harness.SeedPrice(paper.MaxUnitPriceSGD, 0))},
			catalogID(t, a.manager, pods.SKU):  {pods.DefaultQty, "Common Man Coffee Roasters SG", cents(harness.SeedPrice(pods.MaxUnitPriceSGD, 0))},
		}
		if len(d.LineItems) != 2 {
			t.Fatalf("line items = %d, want 2: %+v", len(d.LineItems), d.LineItems)
		}
		var total int64
		for _, li := range d.LineItems {
			if li.CatalogItemID == nil {
				t.Fatalf("line %q did not match the catalog", li.Description)
			}
			w, ok := want[*li.CatalogItemID]
			if !ok {
				t.Fatalf("line %q matched unexpected catalog item %s", li.Description, *li.CatalogItemID)
			}
			if li.Qty != w.qty {
				t.Errorf("line %q qty = %d, want catalog default %d", li.Description, li.Qty, w.qty)
			}
			if li.PolicyDecision != "AUTO_APPROVE" {
				t.Errorf("line %q decision %s %+v", li.Description, li.PolicyDecision, li.Reasons)
			}
			so := selectedOffer(d, li)
			if so == nil {
				t.Fatalf("line %q has no selected offer", li.Description)
			}
			if so.MerchantName != w.merchant || so.UnitPriceCents != w.unit || so.Rank != 1 {
				t.Errorf("line %q selected %+v, want %s @ %d rank 1", li.Description, *so, w.merchant, w.unit)
			}
			for _, o := range offersFor(d, li.ID) {
				if o.MerchantName == harness.LeakyMerchantName {
					t.Errorf("offer from non-allow-listed merchant %q survived: %+v", o.MerchantName, o)
				}
			}
			total += int64(w.qty) * w.unit
		}
		if d.Request.Decision != "AUTO_APPROVE" || d.Request.TotalCents != total {
			t.Fatalf("request decision %s total %d, want AUTO_APPROVE %d", d.Request.Decision, d.Request.TotalCents, total)
		}

		hq := addressByLabel(t, a.manager, seedAddressLabel(t, "hq-marina"))
		must(t, confirm(t, a.manager, pr.ID, hq.ID), nil, http.StatusOK, "confirm")
		d = waitStatus(t, a.manager, pr.ID, "awaiting_payment")
		if len(d.Payments) != 2 {
			t.Fatalf("payments = %d, want one per merchant (2)", len(d.Payments))
		}
		if len(d.Approvals) != 0 {
			t.Fatalf("auto-approved request has approvals: %+v", d.Approvals)
		}
		d = payAll(t, a.manager, pr.ID)
		var final int64
		for _, p := range d.Payments {
			if p.Status != "completed" || p.ReapOrderID == "" || p.FinalCents == nil {
				t.Fatalf("payment not completed: %+v", p)
			}
			final += *p.FinalCents
		}
		if final != total {
			t.Fatalf("charged %d, want %d", final, total)
		}
		spentCents += final

		// SSE: the full sequence for this request, in order.
		wantSeq := []string{
			"request.created", "line_items.parsed", "offers.ranked", "policy.evaluated", "status:quoted",
			"status:approved", "status:checking_out", "checkout.quoted", "payment.action_required",
			"order.completed",
		}
		ok := stream.WaitFor(10*time.Second, func([]harness.SSEEvent) bool {
			got, _ := harness.IsSubsequence(harness.Seq(stream.ForRequest(pr.ID)), wantSeq)
			return got
		})
		got := harness.Seq(stream.ForRequest(pr.ID))
		if !ok {
			_, at := harness.IsSubsequence(got, wantSeq)
			t.Fatalf("SSE sequence missing %q (step %d)\n got: %v", wantSeq[at], at, got)
		}
		for _, must := range []string{"status:searching", "search.started", "search.vendor_result", "status:awaiting_payment", "payment.status_changed", "status:ordered"} {
			if !contains(got, must) {
				t.Errorf("SSE never sent %s; got %v", must, got)
			}
		}
		if got[0] != "request.created" || got[len(got)-1] == "request.created" {
			t.Errorf("SSE order: %v", got)
		}
		// payment.action_required must carry the hosted approval URL.
		for _, e := range stream.ForRequest(pr.ID) {
			if e.Type == "payment.action_required" && !strings.Contains(string(e.Data), "/hosted/checkouts/") {
				t.Errorf("payment.action_required without approval_url: %s", e.Data)
			}
		}

		// Audit trail.
		evs := auditFor(t, a.manager, pr.ID)
		types := auditTypes(evs)
		for _, w := range []string{"request.created", "line_items.parsed", "policy.evaluated", "request.confirmed",
			"reap.quote_created", "reap.checkout_created", "reap.checkout_status", "order.completed"} {
			if !contains(types, w) {
				t.Errorf("audit trail lacks %s; got %v", w, types)
			}
		}
		if len(types) > 0 && types[0] != "request.created" {
			t.Errorf("audit must be oldest first, starting with request.created: %v", types)
		}
		for _, e := range evs {
			if e.Type == "request.confirmed" {
				if !strings.Contains(string(e.Payload), hq.ID) || e.ActorID != a.managerUser.ID || e.ActorType != "user" {
					t.Errorf("request.confirmed = %+v payload %s", e, e.Payload)
				}
			}
		}
	})

	t.Run("05 PRD example 2: standing desk goes to approval with 3 quotes", func(t *testing.T) {
		pr := createRequest(t, a.manager, "Get us a standing desk for the new hire.", nil)
		d := waitStatus(t, a.manager, pr.ID, "quoted")
		if len(d.LineItems) != 1 {
			t.Fatalf("line items = %+v", d.LineItems)
		}
		li := d.LineItems[0]
		if li.CatalogItemID != nil {
			t.Fatalf("standing desk matched catalog item %s; it is off-list", *li.CatalogItemID)
		}
		if li.PolicyDecision != "NEEDS_APPROVAL" || !hasReason(li.Reasons, "OFF_LIST") || d.Request.Decision != "NEEDS_APPROVAL" {
			t.Fatalf("decision %s/%s reasons %+v", li.PolicyDecision, d.Request.Decision, li.Reasons)
		}
		cheapest := cents(harness.DeskPrices["ErgoTune Standing Desk Lite"])
		if d.Request.TotalCents != cheapest {
			t.Fatalf("total %d, want cheapest desk %d", d.Request.TotalCents, cheapest)
		}

		hq := addressByLabel(t, a.manager, seedAddressLabel(t, "hq-marina"))
		must(t, confirm(t, a.manager, pr.ID, hq.ID), nil, http.StatusOK, "confirm")
		waitStatus(t, a.manager, pr.ID, "pending_approval")

		views := approvalsFor(t, a.approver, pr.ID, "pending")
		if len(views) != 1 {
			t.Fatalf("pending approvals for request = %d, want 1", len(views))
		}
		v := views[0]
		if v.Approval.Kind != "policy" || !hasReason(v.Approval.Reasons, "OFF_LIST") || v.Approval.AmountCents != cheapest {
			t.Fatalf("approval = %+v", v.Approval)
		}
		if len(v.Lines) != 1 || len(v.Lines[0].TopOffers) != 3 {
			t.Fatalf("approval must show the 3 best quotes: %+v", v.Lines)
		}
		for i, o := range v.Lines[0].TopOffers {
			if o.Rank != i+1 {
				t.Errorf("top offer %d has rank %d", i, o.Rank)
			}
		}
		if v.Lines[0].TopOffers[0].UnitPriceCents != cheapest {
			t.Errorf("best quote %+v, want %d", v.Lines[0].TopOffers[0], cheapest)
		}
		r, err := a.approver.Do(ctxT(t), http.MethodGet, "/api/v1/approvals/"+v.Approval.ID, nil)
		must(t, r, err, http.StatusOK, "get approval")

		r, err = a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/approvals/"+v.Approval.ID+"/approve", map[string]any{"comment": "self-approve"})
		must(t, r, err, http.StatusForbidden, "manager approving")
		r, err = a.approver.Do(ctxT(t), http.MethodPost, "/api/v1/approvals/"+v.Approval.ID+"/approve", map[string]any{"comment": "ok for the new hire"})
		must(t, r, err, http.StatusOK, "approve")
		if ap := decode[approval](t, r); ap.Status != "approved" || ap.ApproverID == nil || *ap.ApproverID != a.approverUser.ID {
			t.Fatalf("approved = %+v", ap)
		}
		r, err = a.approver.Do(ctxT(t), http.MethodPost, "/api/v1/approvals/"+v.Approval.ID+"/approve", nil)
		must(t, r, err, http.StatusConflict, "approving twice")

		d = payAll(t, a.manager, pr.ID)
		if len(d.Payments) != 1 || d.Payments[0].MerchantName == "" {
			t.Fatalf("payments %+v", d.Payments)
		}
		spentCents += *d.Payments[0].FinalCents
		types := auditTypes(auditFor(t, a.manager, pr.ID))
		for _, w := range []string{"approval.requested", "approval.decided", "order.completed"} {
			if !contains(types, w) {
				t.Errorf("audit lacks %s: %v", w, types)
			}
		}
	})

	t.Run("06 rejected approval never reaches Reap checkout", func(t *testing.T) {
		pr := createRequest(t, a.manager, "Another standing desk please", []map[string]any{{"description": "standing desk", "qty": 1}})
		waitStatus(t, a.manager, pr.ID, "quoted")
		hq := addressByLabel(t, a.manager, seedAddressLabel(t, "hq-marina"))
		must(t, confirm(t, a.manager, pr.ID, hq.ID), nil, http.StatusOK, "confirm")
		waitStatus(t, a.manager, pr.ID, "pending_approval")
		before := checkoutsCreated()
		views := approvalsFor(t, a.approver, pr.ID, "pending")
		if len(views) != 1 {
			t.Fatalf("pending approvals = %d", len(views))
		}
		r, err := a.approver.Do(ctxT(t), http.MethodPost, "/api/v1/approvals/"+views[0].Approval.ID+"/reject", map[string]any{"comment": "one desk is enough"})
		must(t, r, err, http.StatusOK, "reject")
		waitStatus(t, a.manager, pr.ID, "rejected")
		time.Sleep(300 * time.Millisecond)
		if after := checkoutsCreated(); after != before {
			t.Fatalf("rejected request created %d Reap checkouts", after-before)
		}
	})

	t.Run("07 live price drift", func(t *testing.T) {
		notes, _ := harness.CatalogItem(suite.seed, "sticky-notes")
		unit := harness.SeedPrice(notes.MaxUnitPriceSGD, 0)
		approved := 2 * cents(unit)
		tests := []struct {
			name       string
			multiplier float64
			reapproval bool
		}{
			{"4% stays within policy", 1.04, false},
			{"10% goes back to approval", 1.10, true},
		}
		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				stream, err := harness.Subscribe(ctxT(t), suite.api.BaseURL, a.manager.Token, "")
				if err != nil {
					t.Fatal(err)
				}
				defer stream.Close()
				suite.reap.SetQuoteMultiplier("popular.com.sg", tc.multiplier)
				defer suite.reap.SetQuoteMultiplier("popular.com.sg", 1)

				pr := createRequest(t, a.manager, "2 packs of sticky notes", []map[string]any{{"description": "sticky notes", "qty": 2}})
				d := waitStatus(t, a.manager, pr.ID, "quoted")
				if d.Request.TotalCents != approved || d.Request.Decision != "AUTO_APPROVE" {
					t.Fatalf("quoted total %d decision %s, want %d AUTO_APPROVE", d.Request.TotalCents, d.Request.Decision, approved)
				}
				live := 2 * cents(harness.SeedPrice(notes.MaxUnitPriceSGD, 0)*tc.multiplier)
				hq := addressByLabel(t, a.manager, seedAddressLabel(t, "hq-marina"))
				must(t, confirm(t, a.manager, pr.ID, hq.ID), nil, http.StatusOK, "confirm")

				if !tc.reapproval {
					d = payAll(t, a.manager, pr.ID)
					if f := *d.Payments[0].FinalCents; f != live {
						t.Fatalf("final %d, want live %d", f, live)
					}
					spentCents += live
					if seq := harness.Seq(stream.ForRequest(pr.ID)); contains(seq, "checkout.price_drift") {
						t.Fatalf("drift within policy still raised checkout.price_drift: %v", seq)
					}
					return
				}

				d = waitStatus(t, a.manager, pr.ID, "pending_approval")
				views := approvalsFor(t, a.approver, pr.ID, "pending")
				if len(views) != 1 {
					t.Fatalf("pending approvals = %d", len(views))
				}
				ap := views[0].Approval
				if ap.Kind != "price_drift" || ap.AmountCents != live || ap.PrevCents == nil || *ap.PrevCents != approved || !hasReason(ap.Reasons, "PRICE_DRIFT") {
					t.Fatalf("drift approval = %+v (want amount %d prev %d)", ap, live, approved)
				}
				if !stream.WaitFor(5*time.Second, func([]harness.SSEEvent) bool {
					return contains(harness.Seq(stream.ForRequest(pr.ID)), "checkout.price_drift")
				}) {
					t.Fatalf("no checkout.price_drift event: %v", harness.Seq(stream.ForRequest(pr.ID)))
				}
				for _, e := range stream.ForRequest(pr.ID) {
					if e.Type != "checkout.price_drift" {
						continue
					}
					var dd struct {
						Approved int64   `json:"approved_cents"`
						Live     int64   `json:"live_cents"`
						Pct      float64 `json:"pct"`
					}
					_ = json.Unmarshal(e.Data, &dd)
					if dd.Approved != approved || dd.Live != live || dd.Pct <= 5 {
						t.Errorf("price_drift data %s", e.Data)
					}
				}
				for _, p := range getDetail(t, a.manager, pr.ID).Payments {
					if p.ReapCheckoutID != "" {
						t.Fatalf("checkout was created before the drift was re-approved: %+v", p)
					}
				}
				r, err := a.approver.Do(ctxT(t), http.MethodPost, "/api/v1/approvals/"+ap.ID+"/approve", map[string]any{"comment": "fine"})
				must(t, r, err, http.StatusOK, "approve drift")
				d = payAll(t, a.manager, pr.ID)
				if f := *d.Payments[0].FinalCents; f != live {
					t.Fatalf("final %d, want re-approved live total %d", f, live)
				}
				spentCents += live
				if !contains(auditTypes(auditFor(t, a.manager, pr.ID)), "reap.price_drift") {
					t.Errorf("audit lacks reap.price_drift")
				}
			})
		}
	})

	t.Run("08 voice tool relay: create, list addresses, confirm, cancel", func(t *testing.T) {
		tool := func(name string, args any) map[string]any {
			t.Helper()
			r, err := a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/agent/tools/"+name,
				map[string]any{"call_id": "call_" + name, "session_id": "rt_e2e", "arguments": args})
			must(t, r, err, http.StatusOK, "tool "+name)
			out := decode[struct {
				CallID string         `json:"call_id"`
				Output map[string]any `json:"output"`
			}](t, r)
			if out.CallID != "call_"+name {
				t.Errorf("tool %s call_id = %q", name, out.CallID)
			}
			return out.Output
		}

		out := tool("list_addresses", map[string]any{})
		b := jsonNoEscape(out)
		for _, a := range suite.seed.Addresses {
			if !strings.Contains(b, a.Label) {
				t.Errorf("list_addresses output lacks %q", a.Label)
			}
		}

		// Arguments may arrive as a JSON-encoded string (raw Realtime payload).
		out = tool("create_order_request", `{"utterance":"Order 3 boxes of staples","items":[{"description":"staples","qty":3}]}`)
		id := findRequestID(out)
		if id == "" {
			t.Fatalf("create_order_request output has no request id: %v", out)
		}
		deadline := time.Now().Add(30 * time.Second)
		for {
			st := tool("get_request_status", map[string]any{"request_id": id})
			sb, _ := json.Marshal(st)
			if strings.Contains(string(sb), `"quoted"`) {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("get_request_status never showed quoted: %s", sb)
			}
			time.Sleep(150 * time.Millisecond)
		}
		label := seedAddressLabel(t, "paya-lebar")
		pl := addressByLabel(t, a.manager, label)
		out = tool("confirm_order", map[string]any{"request_id": id, "address_id": pl.ID})
		if e, ok := out["error"]; ok {
			t.Fatalf("confirm_order error: %v", e)
		}
		d := getDetail(t, a.manager, id)
		if d.Request.AddressID == nil || *d.Request.AddressID != pl.ID {
			t.Fatalf("confirm_order did not use %s: %+v", label, d.Request)
		}
		waitStatus(t, a.manager, id, "awaiting_payment")
		// The payment link is open on Reap, so the agent cannot cancel any more.
		out = tool("cancel_request", map[string]any{"request_id": id})
		if out["code"] != "invalid_transition" {
			t.Fatalf("cancel_request with an open checkout = %v, want invalid_transition", out)
		}
		waitStatus(t, a.manager, id, "awaiting_payment")

		// Cancelling through the agent works before checkout.
		out = tool("create_order_request", map[string]any{"utterance": "Order 2 boxes of staples", "items": []map[string]any{{"description": "staples", "qty": 2}}})
		id2 := findRequestID(out)
		if id2 == "" {
			t.Fatalf("create_order_request output has no request id: %v", out)
		}
		out = tool("cancel_request", map[string]any{"request_id": id2})
		if e, ok := out["error"]; ok {
			t.Fatalf("cancel_request error: %v", e)
		}
		waitStatus(t, a.manager, id2, "cancelled")

		// Approvers cannot buy through the agent either (REST is manager|admin only).
		r, err := a.approver.Do(ctxT(t), http.MethodPost, "/api/v1/agent/tools/create_order_request",
			map[string]any{"call_id": "call_x", "session_id": "rt_e2e", "arguments": map[string]any{"utterance": "10 reams of paper"}})
		must(t, r, err, http.StatusOK, "approver tool call")
		if !strings.Contains(string(r.Body), `"forbidden"`) {
			t.Fatalf("approver create_order_request = %s, want code forbidden", trim(r.Body))
		}

		// Tool-level errors are 200 with output.error.
		out = tool("get_request_status", map[string]any{"request_id": harness.NewUUID()})
		if _, ok := out["error"]; !ok {
			t.Errorf("unknown request should give output.error, got %v", out)
		}
		r, err = a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/agent/tools/transfer_money", map[string]any{"arguments": map[string]any{}})
		must(t, r, err, http.StatusNotFound, "unknown tool")
	})

	var newPrompt string

	t.Run("09 admin system prompt edit reaches the realtime session", func(t *testing.T) {
		r, err := a.admin.Do(ctxT(t), http.MethodGet, "/api/v1/admin/system-prompt", nil)
		must(t, r, err, http.StatusOK, "get prompt")
		type promptT struct {
			Key     string `json:"key"`
			Content string `json:"content"`
			Version int    `json:"version"`
		}
		before := decode[promptT](t, r)
		if before.Key != "agent" || !strings.Contains(before.Content, "Jarvis") {
			t.Fatalf("seeded prompt = %+v", before)
		}

		newPrompt = "You are Jarvis (e2e " + harness.NewUUID() + "). Always ask which delivery address to use before confirming."
		r, err = a.manager.Do(ctxT(t), http.MethodPut, "/api/v1/admin/system-prompt", map[string]any{"content": newPrompt})
		must(t, r, err, http.StatusForbidden, "manager editing prompt")
		r, err = a.admin.Do(ctxT(t), http.MethodPut, "/api/v1/admin/system-prompt", map[string]any{"content": ""})
		must(t, r, err, http.StatusBadRequest, "empty prompt")
		r, err = a.admin.Do(ctxT(t), http.MethodPut, "/api/v1/admin/system-prompt", map[string]any{"content": newPrompt})
		must(t, r, err, http.StatusOK, "put prompt")
		after := decode[promptT](t, r)
		if after.Content != newPrompt || after.Version != before.Version+1 {
			t.Fatalf("after edit = %+v (before version %d)", after, before.Version)
		}

		n := len(suite.llm.RealtimeSessions())
		r, err = a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/realtime/session", nil)
		must(t, r, err, http.StatusOK, "realtime session")
		sess := decode[struct {
			ClientSecret string `json:"client_secret"`
			Model        string `json:"model"`
			CallsURL     string `json:"calls_url"`
		}](t, r)
		if !strings.HasPrefix(sess.ClientSecret, "ek_") || sess.Model == "" || sess.CallsURL == "" {
			t.Fatalf("session = %+v", sess)
		}
		if strings.Contains(string(r.Body), suite.llmKey) {
			t.Fatal("realtime session response leaks the server API key")
		}
		ss := suite.llm.RealtimeSessions()
		if len(ss) != n+1 {
			t.Fatalf("api minted %d sessions upstream, want 1", len(ss)-n)
		}
		cfg := ss[len(ss)-1]
		// The minter may append a voice-specific addendum, but the admin's prompt must lead.
		if instr, _ := cfg["instructions"].(string); !strings.HasPrefix(instr, newPrompt) {
			t.Fatalf("realtime instructions = %q, want them to start with the edited prompt", instr)
		}
		tb, _ := json.Marshal(cfg["tools"])
		for _, name := range []string{"create_order_request", "get_request_status", "list_addresses", "confirm_order", "cancel_request"} {
			if !strings.Contains(string(tb), `"`+name+`"`) {
				t.Errorf("realtime session tools lack %s: %s", name, tb)
			}
		}
	})

	t.Run("10 text agent uses the edited prompt and the same tools", func(t *testing.T) {
		if newPrompt == "" {
			t.Skip("step 09 did not edit the prompt")
		}
		n := len(suite.llm.Calls())
		r, err := a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/agent/chat", map[string]any{"message": "Please order 4 highlighter sets"})
		must(t, r, err, http.StatusOK, "agent chat")
		out := decode[struct {
			SessionID  string   `json:"session_id"`
			Reply      string   `json:"reply"`
			RequestIDs []string `json:"request_ids"`
			ToolCalls  []struct {
				Name string `json:"name"`
			} `json:"tool_calls"`
		}](t, r)
		if out.SessionID == "" || out.Reply == "" || len(out.RequestIDs) != 1 || len(out.ToolCalls) == 0 || out.ToolCalls[0].Name != "create_order_request" {
			t.Fatalf("chat output = %+v", out)
		}
		usedPrompt := false
		for _, c := range suite.llm.Calls()[n:] {
			if len(c.ToolNames) > 0 && strings.Contains(c.System, newPrompt) {
				usedPrompt = true
			}
		}
		if !usedPrompt {
			t.Fatal("text agent did not send the edited system prompt to the model")
		}
		r, err = a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/agent/sessions/"+out.SessionID+"/messages", nil)
		must(t, r, err, http.StatusOK, "transcript")
		if !strings.Contains(string(r.Body), "highlighter") {
			t.Fatalf("transcript lacks the user turn: %s", trim(r.Body))
		}
		waitStatus(t, a.manager, out.RequestIDs[0], "quoted")
		r, err = a.manager.Do(ctxT(t), http.MethodPost, "/api/v1/requests/"+out.RequestIDs[0]+"/cancel", nil)
		must(t, r, err, http.StatusOK, "cancel chat request")
	})

	t.Run("11 orders, monthly spend and global audit", func(t *testing.T) {
		r, err := a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/orders", nil)
		must(t, r, err, http.StatusOK, "orders")
		orders := decode[struct {
			Orders []struct {
				Request  purchaseRequest `json:"request"`
				Payments []payment       `json:"payments"`
			} `json:"orders"`
		}](t, r).Orders
		ordered := 0
		for _, o := range orders {
			if o.Request.Status == "ordered" {
				ordered++
			}
		}
		if ordered < 4 {
			t.Fatalf("orders list has %d ordered requests, want >= 4", ordered)
		}

		r, err = a.manager.Do(ctxT(t), http.MethodGet, "/api/v1/spend/monthly?months=3", nil)
		must(t, r, err, http.StatusOK, "spend")
		sp := decode[struct {
			Currency string `json:"currency"`
			Budget   int64  `json:"monthly_budget_cents"`
			MTD      int64  `json:"month_to_date_cents"`
			Months   []struct {
				Month  string `json:"month"`
				Spend  int64  `json:"spend_cents"`
				Orders int    `json:"orders"`
			} `json:"months"`
		}](t, r)
		if sp.Currency != "SGD" || sp.Budget != cents(suite.seed.Policy.MonthlyBudgetSGD) || len(sp.Months) == 0 {
			t.Fatalf("spend = %+v", sp)
		}
		if sp.MTD != spentCents {
			t.Fatalf("month_to_date_cents = %d, want %d (sum of completed payments this run)", sp.MTD, spentCents)
		}

		r, err = a.approver.Do(ctxT(t), http.MethodGet, "/api/v1/audit?type=reap.&limit=200", nil)
		must(t, r, err, http.StatusOK, "global audit")
		evs := decode[struct {
			Events []auditEvent `json:"events"`
		}](t, r).Events
		if len(evs) == 0 {
			t.Fatal("no reap.* audit events")
		}
		for _, e := range evs {
			if !strings.HasPrefix(e.Type, "reap.") {
				t.Fatalf("type filter leaked %s", e.Type)
			}
		}
		for i := 1; i < len(evs); i++ {
			if evs[i].ID > evs[i-1].ID {
				t.Fatalf("global audit must be newest first")
			}
		}
	})

	t.Run("12 no secrets or card data leak", func(t *testing.T) {
		r, err := a.admin.Do(ctxT(t), http.MethodGet, "/api/v1/audit?limit=200", nil)
		must(t, r, err, http.StatusOK, "audit")
		haystacks := map[string]string{"api logs": suite.api.Logs(), "audit": string(r.Body)}
		for where, h := range haystacks {
			for name, secret := range map[string]string{"REAP_API_KEY": suite.reapKey, "OPENAI_API_KEY": suite.llmKey} {
				if strings.Contains(h, secret) {
					t.Errorf("%s contain the %s value", where, name)
				}
			}
		}
		if strings.Contains(string(r.Body), `"last4"`) {
			t.Error("audit contains card last4")
		}
	})
}

// jsonNoEscape marshals without HTML escaping (labels contain "&").
func jsonNoEscape(v any) string {
	var sb strings.Builder
	enc := json.NewEncoder(&sb)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
	return sb.String()
}

func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}

// findRequestID digs a request id out of a tool result: request_id anywhere, else request.id.
func findRequestID(v any) string {
	switch m := v.(type) {
	case map[string]any:
		if s, ok := m["request_id"].(string); ok && s != "" {
			return s
		}
		if r, ok := m["request"].(map[string]any); ok {
			if s, ok := r["id"].(string); ok && s != "" {
				return s
			}
		}
		for _, x := range m {
			if s := findRequestID(x); s != "" {
				return s
			}
		}
	case []any:
		for _, x := range m {
			if s := findRequestID(x); s != "" {
				return s
			}
		}
	}
	return ""
}
