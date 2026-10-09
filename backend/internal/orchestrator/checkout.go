package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/policy"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// merchantGroup is the part of the basket bought from one merchant (one Reap quote + checkout).
type merchantGroup struct {
	key          string
	vendorID     *string
	merchantName string
	items        []reap.QuoteItem
	itemsCents   domain.Cents
}

// handleCheckout: approved -> checking_out -> per merchant Reap quote -> drift + limit checks ->
// checkout -> awaiting_payment (or back to pending_approval). Idempotent: payments already quoted
// or checked out are reused (a merchant never gets a second checkout), and every Reap checkout
// POST uses the payment's stored idempotency key.
func (o *Impl) handleCheckout(ctx context.Context, job queue.Job) error {
	unlock := o.lock(job.RequestID)
	defer unlock()
	r, err := o.d.Store.Requests().Get(ctx, job.RequestID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return queue.Permanent(err)
		}
		return err
	}
	switch r.Status {
	case domain.StatusApproved:
		if err := o.transition(ctx, r.ID, domain.StatusApproved, domain.StatusCheckingOut, "", domain.ActorSystem, ""); err != nil {
			if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalidTransition) {
				return nil
			}
			return err
		}
	case domain.StatusCheckingOut:
		// resumed or retried
	default:
		return nil
	}
	err = o.checkout(ctx, job, r.ID)
	if err == nil {
		return nil
	}
	if retryableReap(err) && job.Attempt+1 < 3 && !queue.IsPermanent(err) {
		return err // retried: checkouts reuse their idempotency keys, open checkouts are reused
	}
	if ctx.Err() != nil && !errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return err // shutting down; Resume picks it up
	}
	_ = o.failRequest(context.WithoutCancel(ctx), r.ID, checkoutFailure(err))
	return queue.Permanent(err)
}

// retryableReap: a Reap 429/5xx, or a transport failure (timeout, reset) where Reap may or may not
// have processed the call. Both are safe to retry because a retry reuses the same idempotency key
// for checkouts and never opens a second checkout for a merchant.
func retryableReap(err error) bool {
	var ae *reap.APIError
	if errors.As(err, &ae) {
		return ae.Retryable()
	}
	var te *reap.TransportError
	return errors.As(err, &te)
}

func checkoutFailure(err error) string {
	switch {
	case errors.Is(err, domain.ErrNoActiveEnrollment):
		return "no active card enrollment: add a card and try again"
	case reap.IsCode(err, reap.CodeEnrollmentNotActive), reap.IsCode(err, reap.CodeEnrollmentNotFound):
		return "the card enrollment is not active: add a card and try again"
	case reap.IsReason(err, reap.ReasonInvalidPhone):
		return "the delivery address has a phone number the vendor cannot use: fix the phone number in the delivery address and try again"
	case reap.IsReason(err, reap.ReasonStateRequired), reap.IsReason(err, reap.ReasonAddressLine2Required):
		return "the vendor needs more detail in the delivery address (unit number or region): update the address and try again"
	case reap.IsReason(err, reap.ReasonItemsUnshippable):
		return "the vendor cannot ship a selected item to this delivery address"
	case reap.IsCode(err, reap.CodeVariantUnavailable), reap.IsCode(err, reap.CodeQuoteUnfulfillable):
		return "a selected item is no longer available from the vendor: " + err.Error()
	}
	return "checkout failed: " + err.Error()
}

// errQuoteStale: Reap rejected a checkout because the stored quote is expired, gone or must be
// replaced. The payment is marked for re-quoting and the round is run again (new drift check).
var errQuoteStale = errors.New("reap quote is stale")

func quoteStale(err error) bool {
	return reap.IsCode(err, reap.CodeQuoteExpired) || reap.IsCode(err, reap.CodeQuoteNotFound) ||
		reap.IsCode(err, reap.CodeQuoteReplacement)
}

func (o *Impl) checkout(ctx context.Context, job queue.Job, requestID string) error {
	for round := 0; ; round++ {
		err := o.checkoutRound(ctx, requestID)
		if errors.Is(err, errQuoteStale) && round < 2 {
			continue
		}
		return err
	}
}

func (o *Impl) checkoutRound(ctx context.Context, requestID string) error {
	d, err := o.d.Store.Requests().Detail(ctx, requestID)
	if err != nil {
		return err
	}
	r := d.Request
	enr, err := o.activeEnrollment(ctx)
	if err != nil {
		return queue.Permanent(err)
	}
	addr := d.Address
	if addr == nil && r.AddressID != nil {
		a, err := o.d.Store.Addresses().Get(ctx, *r.AddressID)
		if err != nil {
			return queue.Permanent(fmt.Errorf("load delivery address: %w", err))
		}
		addr = &a
	}
	if addr == nil {
		return queue.Permanent(fmt.Errorf("%w: no delivery address on the request", domain.ErrValidation))
	}
	groups := basketGroups(d)
	if len(groups) == 0 {
		return queue.Permanent(fmt.Errorf("%w: nothing to buy (no approved lines with an offer)", domain.ErrValidation))
	}
	if err := o.checkGroupVendors(ctx, d, groups); err != nil {
		return queue.Permanent(err)
	}

	cfg, err := o.d.Store.Policy().Get(ctx)
	if err != nil {
		return err
	}

	// 1. Quote every merchant (reusing still-valid quotes and existing checkouts).
	payments := make([]domain.Payment, 0, len(groups))
	var live, shipping domain.Cents
	for _, g := range groups {
		p, err := o.quoteGroup(ctx, d, g, enr, *addr)
		if err != nil {
			return err
		}
		payments = append(payments, p)
		live += p.QuotedCents
		shipping += p.ShippingCents
	}
	// Drift is measured on what was priced at approval time (items, discounts, tax): shipping is
	// only known from Reap's quote, so it is shown separately and never counts as drift.
	liveItems := live - shipping

	// 2. Drift check against the approved total (over every open payment of the request).
	approved := approvedItemsCents(r)
	if policy.DriftExceeded(approved, liveItems, cfg.PriceDriftPct) {
		pct := 0.0
		if approved > 0 {
			pct = float64(liveItems-approved) * 100 / float64(approved)
		}
		data := map[string]any{"request_id": r.ID, "approved_cents": approved, "live_cents": liveItems, "shipping_cents": shipping, "pct": pct}
		o.record(ctx, r.ID, domain.ActorSystem, "checkout", audit.ReapPriceDrift, data)
		o.publish(events.CheckoutPriceDrift, r.ID, data)
		if o.d.Approvals == nil {
			return queue.Permanent(fmt.Errorf("price drift needs approval but no approvals service is configured"))
		}
		prev := approved
		msg := fmt.Sprintf("Live vendor prices total %s, %.1f%% above the approved %s (limit %.4g%%).", liveItems.SGD(), pct, approved.SGD(), cfg.PriceDriftPct)
		if shipping > 0 {
			msg += fmt.Sprintf(" Shipping of %s is charged on top.", shipping.SGD())
		}
		_, err := o.d.Approvals.Create(ctx, approvals.CreateInput{
			RequestID: r.ID, Kind: domain.ApprovalKindPriceDrift, AmountCents: liveItems, PrevCents: &prev,
			Reasons: []domain.Reason{{Code: domain.ReasonPriceDrift, Message: msg}},
		})
		if err != nil {
			return fmt.Errorf("open price drift approval: %w", err)
		}
		return nil
	}

	// 3. Hard limits on the live total for orders no human approved (an auto-approved order must
	// not be charged above the per-order limit or monthly budget just because drift is allowed).
	if opened, err := o.recheckLimits(ctx, r, cfg, live); err != nil || opened {
		return err
	}

	// 4. Create checkouts (Reap-hosted approval page per merchant).
	for i := range payments {
		if err := o.createCheckout(ctx, &payments[i], enr); err != nil {
			if quoteStale(err) {
				o.markQuoteStale(ctx, &payments[i])
				return fmt.Errorf("%w: %v", errQuoteStale, err)
			}
			return err
		}
	}
	cur, err := o.d.Store.Requests().Get(ctx, r.ID)
	if err != nil {
		return err
	}
	cur.ShippingCents, cur.TotalCents = shipping, live
	if err := o.d.Store.Requests().Update(ctx, &cur); err != nil {
		return err
	}
	if err := o.transition(ctx, r.ID, domain.StatusCheckingOut, domain.StatusAwaitingPayment, "", domain.ActorSystem, ""); err != nil {
		return err
	}
	// Apply any status the checkout already has (e.g. simulated COMPLETED in the sandbox), then poll.
	if _, err := o.refreshLocked(ctx, r.ID); err != nil {
		o.log.Warn("initial checkout refresh failed", "request_id", r.ID, "err", err)
	}
	cur, err = o.d.Store.Requests().Get(ctx, r.ID)
	if err == nil && (cur.Status == domain.StatusAwaitingPayment || cur.Status == domain.StatusPaying) {
		if _, err := o.d.Queue.EnqueueAfter(ctx, o.pollJob(r.ID), o.d.CheckoutPollInterval); err != nil {
			o.log.Warn("schedule checkout poll failed", "request_id", r.ID, "err", err)
		}
	}
	return nil
}

// approvedItemsCents is the drift baseline: the approved total without shipping. ShippingCents is
// only set together with the live total right before awaiting_payment, so before that it is 0.
func approvedItemsCents(r domain.PurchaseRequest) domain.Cents {
	if r.ShippingCents > 0 && r.ShippingCents < r.TotalCents {
		return r.TotalCents - r.ShippingCents
	}
	return r.TotalCents
}

// checkGroupVendors refuses to quote a merchant that is no longer an allowed vendor (or was never
// one), unless a checkout for it already exists (it cannot be cancelled on Reap).
func (o *Impl) checkGroupVendors(ctx context.Context, d domain.RequestDetail, groups []merchantGroup) error {
	vendors, err := o.d.Store.Vendors().List(ctx, store.VendorFilter{})
	if err != nil {
		return err
	}
	byID := map[string]domain.Vendor{}
	for _, v := range vendors {
		byID[v.ID] = v
	}
	for _, g := range groups {
		if openCheckout(d.Payments, g.key) {
			continue
		}
		if g.vendorID == nil {
			return fmt.Errorf("%w: %s is not an allowed vendor", domain.ErrValidation, g.merchantName)
		}
		v, ok := byID[*g.vendorID]
		if !ok || !o.vendorAllowed(v) {
			return fmt.Errorf("%w: %s is no longer an allowed vendor", domain.ErrValidation, g.merchantName)
		}
	}
	return nil
}

func openCheckout(ps []domain.Payment, key string) bool {
	for _, p := range ps {
		if paymentKey(p) == key && p.ReapCheckoutID != "" && p.Status != domain.PaymentFailed && p.Status != domain.PaymentExpired {
			return true
		}
	}
	return false
}

// recheckLimits re-applies the per-order limit and monthly budget to the live Reap total when no
// approver has approved this request. It opens a policy approval and returns opened=true when a
// limit is broken.
func (o *Impl) recheckLimits(ctx context.Context, r domain.PurchaseRequest, cfg domain.PolicyConfig, live domain.Cents) (bool, error) {
	as, err := o.d.Store.Approvals().ListByRequest(ctx, r.ID)
	if err != nil {
		return false, err
	}
	for _, a := range as {
		if a.Status == domain.ApprovalApproved {
			return false, nil // a human approved this request; drift is the only remaining check
		}
	}
	o.budgetMu.Lock()
	defer o.budgetMu.Unlock()
	committed, err := o.committedSpend(ctx, r.ID)
	if err != nil {
		return false, err
	}
	reasons := limitReasons(cfg, committed, live)
	if len(reasons) == 0 {
		return false, nil
	}
	o.record(ctx, r.ID, domain.ActorSystem, "checkout", audit.CheckoutLimitExceeded, map[string]any{"live_cents": live, "committed_cents": committed, "reasons": reasons})
	if o.d.Approvals == nil {
		return false, queue.Permanent(fmt.Errorf("checkout total breaks a policy limit but no approvals service is configured"))
	}
	if _, err := o.d.Approvals.Create(ctx, approvals.CreateInput{
		RequestID: r.ID, Kind: domain.ApprovalKindPolicy, AmountCents: live, Reasons: reasons,
	}); err != nil {
		return false, fmt.Errorf("open limit approval: %w", err)
	}
	return true, nil
}

// limitReasons applies the order-level hard limits to a live total.
func limitReasons(cfg domain.PolicyConfig, committed, live domain.Cents) []domain.Reason {
	var out []domain.Reason
	if cfg.PerOrderLimitCents > 0 && live > cfg.PerOrderLimitCents {
		out = append(out, domain.Reason{Code: domain.ReasonOverOrderLimit, Message: fmt.Sprintf(
			"The live checkout total of %s (including shipping) is above the per-order limit of %s.", live.SGD(), cfg.PerOrderLimitCents.SGD())})
	}
	if cfg.MonthlyBudgetCents > 0 && committed+live > cfg.MonthlyBudgetCents {
		out = append(out, domain.Reason{Code: domain.ReasonOverMonthlyBudget, Message: fmt.Sprintf(
			"This month's spend and committed orders (%s) plus this order (%s) exceed the monthly budget of %s.",
			committed.SGD(), live.SGD(), cfg.MonthlyBudgetCents.SGD())})
	}
	return out
}

// committedSpend is the money this month that is spent or already committed, excluding request
// excludeID: completed payments this month, plus the unpaid remainder of every other request that
// is approved or further along (approved, checking_out, awaiting_payment, paying), plus open
// checkouts of requests that already ended (they can still be paid on Reap).
func (o *Impl) committedSpend(ctx context.Context, excludeID string) (domain.Cents, error) {
	total, err := o.d.Store.Spend().MonthToDate(ctx, o.d.Clock())
	if err != nil {
		return 0, err
	}
	rs, err := o.d.Store.Requests().List(ctx, store.RequestFilter{Limit: 1000, Statuses: []domain.RequestStatus{
		domain.StatusApproved, domain.StatusCheckingOut, domain.StatusAwaitingPayment, domain.StatusPaying,
	}})
	if err != nil {
		return 0, err
	}
	live := map[string]bool{excludeID: true}
	for _, r := range rs {
		if r.ID == excludeID {
			continue
		}
		live[r.ID] = true
		ps, err := o.d.Store.Payments().ListByRequest(ctx, r.ID)
		if err != nil {
			return 0, err
		}
		var paid domain.Cents
		for _, p := range ps {
			if p.Status == domain.PaymentCompleted {
				if p.FinalCents != nil {
					paid += *p.FinalCents
				} else {
					paid += p.QuotedCents
				}
			}
		}
		if rem := r.TotalCents - paid; rem > 0 {
			total += rem
		}
	}
	inflight, err := o.d.Store.Payments().ListInFlight(ctx)
	if err != nil {
		return 0, err
	}
	for _, p := range inflight {
		if !live[p.RequestID] {
			total += p.QuotedCents
		}
	}
	return total, nil
}

// vendorAllowed: the vendor row is allowed AND its domain is on the merchant allow-list.
func (o *Impl) vendorAllowed(v domain.Vendor) bool { return v.Allowed && o.allow.Contains(v.Domain) }

// markQuoteStale retires the payment's quote after Reap definitively rejected a checkout for it
// (QUOTE_EXPIRED / QUOTE_NOT_FOUND / QUOTE_REPLACEMENT_REQUIRED): no checkout exists for that quote,
// so the next round re-quotes under a new quote key and opens the checkout under a new checkout key.
func (o *Impl) markQuoteStale(ctx context.Context, p *domain.Payment) {
	p.ReapQuoteID, p.QuoteExpiresAt = "", nil
	p.QuoteAttempt++
	p.Status, p.Error = domain.PaymentQuoting, ""
	if err := o.d.Store.Payments().Update(ctx, p); err != nil {
		o.log.Warn("mark quote stale failed", "payment_id", p.ID, "err", err)
	}
}

// basketGroups groups the selected offers of non-REJECT lines by merchant. Deterministic order.
func basketGroups(d domain.RequestDetail) []merchantGroup {
	offers := map[string]domain.Offer{}
	for _, of := range d.Offers {
		offers[of.ID] = of
	}
	byKey := map[string]*merchantGroup{}
	var keys []string
	for _, li := range d.LineItems {
		if li.PolicyDecision == domain.DecisionReject || li.SelectedOfferID == nil {
			continue
		}
		of, ok := offers[*li.SelectedOfferID]
		if !ok || of.ReapVariantID == "" {
			continue
		}
		key := "m:" + of.MerchantName
		if of.VendorID != nil {
			key = "v:" + *of.VendorID
		}
		g, ok := byKey[key]
		if !ok {
			g = &merchantGroup{key: key, vendorID: of.VendorID, merchantName: of.MerchantName}
			byKey[key] = g
			keys = append(keys, key)
		}
		qty := agents.PurchaseQty(li.Qty, of.PackSize)
		merged := false
		for i := range g.items {
			if g.items[i].VariantID == of.ReapVariantID {
				g.items[i].Quantity += qty
				merged = true
			}
		}
		if !merged {
			g.items = append(g.items, reap.QuoteItem{VariantID: of.ReapVariantID, Quantity: qty})
		}
		g.itemsCents += of.UnitPriceCents.MulQty(qty)
	}
	sort.Strings(keys)
	out := make([]merchantGroup, 0, len(keys))
	for _, k := range keys {
		out = append(out, *byKey[k])
	}
	return out
}

func paymentKey(p domain.Payment) string {
	if p.VendorID != nil {
		return "v:" + *p.VendorID
	}
	return "m:" + p.MerchantName
}

// checkoutKey is the Reap idempotency key for the checkout of the payment's current quote. It only
// changes after Reap definitively rejected the previous quote (markQuoteStale), so a retry after an
// unknown outcome always replays the original checkout.
func checkoutKey(p domain.Payment) string {
	if p.QuoteAttempt == 0 {
		return p.IdempotencyKey + ":checkout"
	}
	return p.IdempotencyKey + ":checkout:" + strconv.Itoa(p.QuoteAttempt)
}

// quoteKey is the Reap idempotency key for the payment's current quote attempt.
func quoteKey(p domain.Payment) string {
	if p.QuoteAttempt == 0 {
		return p.IdempotencyKey + ":quote"
	}
	return p.IdempotencyKey + ":quote:" + strconv.Itoa(p.QuoteAttempt)
}

// quoteGroup returns a quoted payment for the group. Reuse rules, per merchant:
//   - a payment that already has an open (or completed) Reap checkout is returned as is: a merchant
//     never gets a second checkout, whatever happened to the job;
//   - a quoted payment is returned as is (Reap decides whether its quote is still usable);
//   - a half-done (quoting) payment, including one whose quote Reap rejected, is re-quoted;
//   - otherwise a new payment is created.
func (o *Impl) quoteGroup(ctx context.Context, d domain.RequestDetail, g merchantGroup, enr domain.Enrollment, addr domain.Address) (domain.Payment, error) {
	var p *domain.Payment
	for i := range d.Payments {
		ex := d.Payments[i]
		if paymentKey(ex) != g.key || ex.Status == domain.PaymentFailed || ex.Status == domain.PaymentExpired {
			continue
		}
		if ex.ReapCheckoutID != "" {
			return ex, nil
		}
		if ex.Status == domain.PaymentQuoted && ex.ReapQuoteID != "" {
			// Reuse the quote even if our clock says it expired: a checkout may already have been
			// sent for it with an unknown outcome, so only Reap's definitive QUOTE_EXPIRED (see
			// markQuoteStale) may retire it. Re-quoting here could open a second checkout.
			return ex, nil
		}
		p = &ex
		break
	}
	if p == nil {
		enrID := enr.ID
		p = &domain.Payment{
			RequestID: d.Request.ID, VendorID: g.vendorID, MerchantName: g.merchantName, EnrollmentID: &enrID,
			ItemsCents: g.itemsCents, Currency: domain.Currency, Status: domain.PaymentQuoting, IdempotencyKey: o.d.NewID(),
		}
		if err := o.d.Store.Payments().Create(ctx, p); err != nil {
			return domain.Payment{}, fmt.Errorf("create payment: %w", err)
		}
	}
	var q reap.Quote
	var err error
	for attempt := 0; ; attempt++ {
		if err = o.d.Store.Payments().Update(ctx, p); err != nil { // persist the key generation first
			return domain.Payment{}, err
		}
		q, err = o.d.Reap.CreateQuote(ctx, quoteKey(*p), reap.CreateQuoteRequest{
			Email: addr.Email, Items: g.items, ShippingAddress: shippingAddress(addr),
		})
		if err == nil {
			q, err = o.ensureShipping(ctx, q)
		}
		if err != nil && attempt == 0 && (reap.IsCode(err, reap.CodeQuoteReplacement) || reap.IsCode(err, reap.CodeQuoteExpired)) {
			p.QuoteAttempt++ // Reap wants a new quote: create one under a new key
			continue
		}
		break
	}
	if err == nil && q.AmountBreakdown.FinalAmount.Currency != "" && !strings.EqualFold(q.AmountBreakdown.FinalAmount.Currency, domain.Currency) {
		err = queue.Permanent(fmt.Errorf("%w: quote currency %s is not %s", domain.ErrUpstream, q.AmountBreakdown.FinalAmount.Currency, domain.Currency))
	}
	if err != nil {
		if retryableReap(err) {
			// Creating a quote charges nothing. Reap replays the first response for a key for
			// 24h, so a QUOTE_TEMPORARILY_UNAVAILABLE would come back forever: the next attempt
			// uses a new key.
			if reap.IsCode(err, reap.CodeQuoteTempUnavailable) {
				p.QuoteAttempt++
				_ = o.d.Store.Payments().Update(ctx, p)
			}
		} else {
			p.Status, p.Error = domain.PaymentFailed, err.Error()
			_ = o.d.Store.Payments().Update(ctx, p)
			o.publish(events.PaymentStatusChanged, d.Request.ID, map[string]any{"request_id": d.Request.ID, "payment": p})
		}
		return domain.Payment{}, fmt.Errorf("reap quote for %s: %w", g.merchantName, err)
	}
	ab := q.AmountBreakdown
	p.ReapQuoteID = q.ID
	p.ItemsCents = domain.CentsFromFloat(ab.ItemsSubtotal.Amount)
	p.ShippingCents = 0
	if ab.Shipping != nil {
		p.ShippingCents = domain.CentsFromFloat(ab.Shipping.Amount)
	}
	p.TaxCents = 0
	if ab.Tax != nil && !ab.Tax.IncludedInPrices {
		p.TaxCents = domain.CentsFromFloat(ab.Tax.Amount.Amount)
	}
	p.QuotedCents = domain.CentsFromFloat(ab.FinalAmount.Amount)
	p.QuoteExpiresAt = nil
	if !q.ExpiresAt.IsZero() {
		exp := q.ExpiresAt
		p.QuoteExpiresAt = &exp
	}
	p.Status, p.Error = domain.PaymentQuoted, ""
	if err := o.d.Store.Payments().Update(ctx, p); err != nil {
		return domain.Payment{}, err
	}
	o.record(ctx, d.Request.ID, domain.ActorSystem, "checkout", audit.ReapQuoteCreated, map[string]any{
		"payment_id": p.ID, "reap_quote_id": q.ID, "merchant": p.MerchantName, "final_cents": p.QuotedCents,
		"items_cents": p.ItemsCents, "shipping_cents": p.ShippingCents, "tax_cents": p.TaxCents,
	})
	o.publish(events.CheckoutQuoted, d.Request.ID, map[string]any{"request_id": d.Request.ID, "payment": p})
	return *p, nil
}

// ensureShipping keeps Reap's preselected shipping option, or selects the cheapest when none is.
func (o *Impl) ensureShipping(ctx context.Context, q reap.Quote) (reap.Quote, error) {
	if len(q.ShippingOptions) == 0 {
		return q, nil
	}
	cheapest := 0
	for i, so := range q.ShippingOptions {
		if so.Selected {
			return q, nil
		}
		if so.Price.Amount < q.ShippingOptions[cheapest].Price.Amount {
			cheapest = i
		}
	}
	return o.d.Reap.SelectShippingOption(ctx, q.ID, q.ShippingOptions[cheapest].ID)
}

func (o *Impl) createCheckout(ctx context.Context, p *domain.Payment, enr domain.Enrollment) error {
	if p.ReapCheckoutID != "" {
		return nil
	}
	c, err := o.d.Reap.CreateCheckout(ctx, checkoutKey(*p), reap.CreateCheckoutRequest{
		QuoteID: p.ReapQuoteID, EnrollmentID: enr.ReapEnrollmentID,
		Presentation: reap.Presentation{Type: "REDIRECT", ReturnURL: o.d.ReapReturnURL},
	})
	if err != nil {
		if !retryableReap(err) && !quoteStale(err) {
			p.Status, p.Error = domain.PaymentFailed, err.Error()
			_ = o.d.Store.Payments().Update(ctx, p)
		}
		return fmt.Errorf("reap checkout for %s: %w", p.MerchantName, err)
	}
	p.ReapCheckoutID = c.ID
	if c.NextAction != nil {
		p.ApprovalURL = c.NextAction.URL
	}
	p.Status = paymentStatus(c.Status)
	if err := o.d.Store.Payments().Update(ctx, p); err != nil {
		return err
	}
	o.record(ctx, p.RequestID, domain.ActorSystem, "checkout", audit.ReapCheckoutCreated, map[string]any{
		"payment_id": p.ID, "reap_checkout_id": c.ID, "status": c.Status, "merchant": p.MerchantName,
	})
	if p.ApprovalURL != "" && p.Status == domain.PaymentRequiresAction {
		o.publish(events.PaymentActionNeeded, p.RequestID, map[string]any{"request_id": p.RequestID, "payment": p, "approval_url": p.ApprovalURL})
	} else {
		o.publish(events.PaymentStatusChanged, p.RequestID, map[string]any{"request_id": p.RequestID, "payment": p})
	}
	return nil
}

func paymentStatus(s reap.CheckoutStatus) domain.PaymentStatus {
	switch s {
	case reap.CheckoutProcessing:
		return domain.PaymentProcessing
	case reap.CheckoutCompleted:
		return domain.PaymentCompleted
	case reap.CheckoutFailed:
		return domain.PaymentFailed
	case reap.CheckoutExpired:
		return domain.PaymentExpired
	}
	return domain.PaymentRequiresAction
}

func shippingAddress(a domain.Address) *reap.ShippingAddress {
	city := a.City
	if city == "" {
		city = "Singapore"
	}
	country := a.Country
	if country == "" {
		country = "SG"
	}
	return &reap.ShippingAddress{
		FirstName: a.FirstName, LastName: a.LastName, Phone: a.Phone, AddressLine1: a.AddressLine1,
		AddressLine2: a.AddressLine2, City: city, Region: a.Region, PostalCode: a.PostalCode, Country: country,
	}
}

// ---------- polling ----------

func (o *Impl) pollJob(requestID string) queue.Job {
	return queue.Job{JobID: fmt.Sprintf("poll:%s:%d", requestID, o.d.Clock().UnixNano()), Kind: queue.KindPollCheckout, RequestID: requestID}
}

// handlePollCheckout refreshes payments and reschedules itself while the request is awaiting
// payment, or while any of its Reap checkouts is still open (requires_action/processing) even if
// the request already failed or was cancelled: Reap cannot cancel a checkout, so a late approval
// still moves money and must be recorded. It stops after the poll timeout (measured from the
// oldest open checkout).
func (o *Impl) handlePollCheckout(ctx context.Context, job queue.Job) error {
	d, err := o.RefreshPayments(ctx, job.RequestID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return queue.Permanent(err)
		}
		// transient: try again on the next tick
		o.log.Warn("checkout poll failed", "request_id", job.RequestID, "err", err)
	}
	if err == nil {
		waiting := d.Request.Status == domain.StatusAwaitingPayment || d.Request.Status == domain.StatusPaying
		if !waiting && !hasOpenCheckout(d.Payments) {
			return nil
		}
		oldest := o.d.Clock()
		for _, p := range d.Payments {
			if p.ReapCheckoutID != "" && p.CreatedAt.Before(oldest) && !p.CreatedAt.IsZero() {
				oldest = p.CreatedAt
			}
		}
		if o.d.Clock().Sub(oldest) > o.d.CheckoutPollTimeout {
			o.log.Info("checkout poll timeout; stopping automatic polling", "request_id", job.RequestID)
			return nil
		}
	}
	_, qerr := o.d.Queue.EnqueueAfter(ctx, o.pollJob(job.RequestID), o.d.CheckoutPollInterval)
	return qerr
}

func hasOpenCheckout(ps []domain.Payment) bool {
	for _, p := range ps {
		if p.ReapCheckoutID != "" && (p.Status == domain.PaymentRequiresAction || p.Status == domain.PaymentProcessing) {
			return true
		}
	}
	return false
}

// RefreshPayments polls Reap for in-flight checkouts and applies status changes.
func (o *Impl) RefreshPayments(ctx context.Context, requestID string) (domain.RequestDetail, error) {
	unlock := o.lock(requestID)
	defer unlock()
	return o.refreshLocked(ctx, requestID)
}

func (o *Impl) refreshLocked(ctx context.Context, requestID string) (domain.RequestDetail, error) {
	ps, err := o.d.Store.Payments().ListByRequest(ctx, requestID)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	req, err := o.d.Store.Requests().Get(ctx, requestID)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	var errs []error
	for i := range ps {
		p := &ps[i]
		if p.ReapCheckoutID == "" || (p.Status != domain.PaymentRequiresAction && p.Status != domain.PaymentProcessing) {
			continue
		}
		c, err := o.d.Reap.GetCheckout(ctx, p.ReapCheckoutID)
		if err != nil {
			errs = append(errs, fmt.Errorf("get checkout %s: %w", p.ReapCheckoutID, err))
			continue
		}
		ns := paymentStatus(c.Status)
		if ns == p.Status {
			continue
		}
		p.Status = ns
		if c.OrderID != nil {
			p.ReapOrderID = *c.OrderID
		}
		if c.FinalAmount != nil {
			fc := domain.CentsFromFloat(c.FinalAmount.Amount)
			p.FinalCents = &fc
		} else if ns == domain.PaymentCompleted {
			fc := p.QuotedCents
			p.FinalCents = &fc
		}
		if c.NextAction != nil && c.NextAction.URL != "" {
			p.ApprovalURL = c.NextAction.URL
		}
		if err := o.d.Store.Payments().Update(ctx, p); err != nil {
			errs = append(errs, err)
			continue
		}
		o.record(ctx, requestID, domain.ActorSystem, "reap", audit.ReapCheckoutStatus, map[string]any{
			"payment_id": p.ID, "reap_checkout_id": p.ReapCheckoutID, "status": c.Status, "reap_order_id": p.ReapOrderID,
		})
		o.publish(events.PaymentStatusChanged, requestID, map[string]any{"request_id": requestID, "payment": p})
		if ns == domain.PaymentCompleted {
			o.alertCompletion(ctx, req, *p)
		}
	}
	if err := o.applyPaymentOutcome(ctx, requestID, ps); err != nil {
		errs = append(errs, err)
	}
	d, err := o.GetRequest(ctx, requestID)
	if err != nil {
		return d, err
	}
	return d, errors.Join(errs...)
}

// alertCompletion audits and publishes a completed charge that needs a human look: money moved
// for a request that had already failed/cancelled, or Reap charged more than it quoted.
func (o *Impl) alertCompletion(ctx context.Context, r domain.PurchaseRequest, p domain.Payment) {
	final := p.QuotedCents
	if p.FinalCents != nil {
		final = *p.FinalCents
	}
	if requestClosed(r.Status) {
		msg := fmt.Sprintf("%s charged %s on Reap after this request was %s.", p.MerchantName, final.SGD(), r.Status)
		o.record(ctx, r.ID, domain.ActorSystem, "reap", audit.ReapChargeAfterClose, map[string]any{
			"payment_id": p.ID, "reap_checkout_id": p.ReapCheckoutID, "request_status": r.Status, "final_cents": final,
		})
		o.publish(events.PaymentAlert, r.ID, map[string]any{"request_id": r.ID, "payment": p, "kind": "charge_after_close", "message": msg})
		o.log.Warn("reap charge completed after request closed", "request_id", r.ID, "payment_id", p.ID, "status", r.Status)
	}
	if p.QuotedCents > 0 && final > p.QuotedCents {
		msg := fmt.Sprintf("%s charged %s, more than the quoted %s.", p.MerchantName, final.SGD(), p.QuotedCents.SGD())
		o.record(ctx, r.ID, domain.ActorSystem, "reap", audit.ReapFinalAmountMismatch, map[string]any{
			"payment_id": p.ID, "quoted_cents": p.QuotedCents, "final_cents": final,
		})
		o.publish(events.PaymentAlert, r.ID, map[string]any{"request_id": r.ID, "payment": p, "kind": "final_amount_mismatch", "message": msg})
		o.log.Warn("reap final amount above quote", "request_id", r.ID, "payment_id", p.ID, "quoted_cents", p.QuotedCents, "final_cents", final)
	}
}

// applyPaymentOutcome moves the request according to its checked-out payments: any failed/expired
// -> failed; all completed -> ordered; any processing -> paying.
func (o *Impl) applyPaymentOutcome(ctx context.Context, requestID string, ps []domain.Payment) error {
	r, err := o.d.Store.Requests().Get(ctx, requestID)
	if err != nil {
		return err
	}
	if r.Status != domain.StatusAwaitingPayment && r.Status != domain.StatusPaying {
		return nil
	}
	var active []domain.Payment
	for _, p := range ps {
		if p.ReapCheckoutID != "" {
			active = append(active, p)
		}
	}
	if len(active) == 0 {
		return nil
	}
	completed, processing := 0, 0
	for _, p := range active {
		switch p.Status {
		case domain.PaymentFailed, domain.PaymentExpired:
			return o.transition(ctx, requestID, r.Status, domain.StatusFailed,
				fmt.Sprintf("payment to %s %s", p.MerchantName, p.Status), domain.ActorSystem, "reap")
		case domain.PaymentCompleted:
			completed++
		case domain.PaymentProcessing:
			processing++
		}
	}
	if completed == len(active) {
		if err := o.transition(ctx, requestID, r.Status, domain.StatusOrdered, "", domain.ActorSystem, "reap"); err != nil {
			return err
		}
		var total domain.Cents
		for _, p := range active {
			if p.FinalCents != nil {
				total += *p.FinalCents
			}
		}
		o.record(ctx, requestID, domain.ActorSystem, "reap", audit.OrderCompleted, map[string]any{"payments": len(active), "final_cents": total})
		o.publish(events.OrderCompleted, requestID, map[string]any{"request_id": requestID, "payments": active})
		return nil
	}
	if (processing > 0 || completed > 0) && r.Status == domain.StatusAwaitingPayment {
		return o.transition(ctx, requestID, r.Status, domain.StatusPaying, "", domain.ActorSystem, "reap")
	}
	return nil
}
