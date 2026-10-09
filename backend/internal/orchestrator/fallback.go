package orchestrator

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
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

// Item-level quote rejections.
//
// Reap rejects a whole quote when one item cannot be supplied (the sandbox answers a quantity above
// the merchant's stock with 409, or 400 AGENTIC_REQUEST_REJECTED {errors: [{field: items}]}), and
// search results only say "available", not how many. So when a merchant's quote is rejected for
// its items, checkout:
//  1. finds the offending line(s) by quoting that merchant's items one at a time (a quote charges
//     nothing; every distinct probe body gets its own idempotency key);
//  2. moves each rejected line to its next-ranked available offer at another allowed merchant,
//     re-runs the deterministic policy for the changed basket and re-groups by merchant;
//  3. fails the request with a plain-language reason when a line has no such offer.
//
// A merchant never gets a second checkout: a fallback only goes to a merchant that has no checkout
// and no quote a checkout may have been sent for.

// errBasketChanged: a line moved to another merchant, so the checkout round runs again with the
// new basket (new merchant groups, new quotes, a new drift check).
var errBasketChanged = errors.New("basket changed")

// maxBasketChanges bounds the fallback rounds of one checkout job.
const maxBasketChanges = 8

// itemsUnavailableError carries the human-readable failure reason (spoken by the voice agent).
type itemsUnavailableError struct{ msg string }

func (e *itemsUnavailableError) Error() string { return e.msg }

// groupLine is one request line inside a merchant group.
type groupLine struct {
	line  domain.LineItem
	offer domain.Offer
	qty   int // purchasable variants of offer
}

// checkoutRun is the state of one checkout job (all its rounds).
type checkoutRun struct {
	// unchecked holds payments whose current quote was created by this job and that no checkout
	// was sent for yet: such a quote can be retired safely when that merchant's items change.
	unchecked map[string]bool
}

func newCheckoutRun() *checkoutRun { return &checkoutRun{unchecked: map[string]bool{}} }

func offerKey(of domain.Offer) string {
	if of.VendorID != nil {
		return "v:" + *of.VendorID
	}
	return "m:" + of.MerchantName
}

// quoteReused mirrors quoteGroup's reuse rules: true when quoteGroup returns an existing payment
// without creating a new Reap quote.
func quoteReused(ps []domain.Payment, key string) bool {
	for _, p := range ps {
		if paymentKey(p) != key || p.Status == domain.PaymentFailed || p.Status == domain.PaymentExpired {
			continue
		}
		return p.ReapCheckoutID != "" || (p.Status == domain.PaymentQuoted && p.ReapQuoteID != "")
	}
	return false
}

// probeKey is the idempotency key of a one-item probe quote: stable per distinct body, so a replay
// returns the same answer and a different body never reuses a key.
func probeKey(requestID string, req reap.CreateQuoteRequest) string {
	b, _ := json.Marshal(req)
	sum := sha256.Sum256(b)
	return requestID + ":probe:" + hex.EncodeToString(sum[:16])
}

type lineRejection struct {
	gl          groupLine
	merchant    string
	merchantKey string
	qty         int // units of the variant asked from the merchant
	err         error
}

func (lr lineRejection) title() string {
	if t := strings.TrimSpace(lr.gl.offer.Title); t != "" {
		return t
	}
	return lr.gl.line.Description
}

// message is the failure sentence spoken to the user at checkout time.
func (lr lineRejection) message() string {
	merchant := lr.merchant
	title := lr.title()
	why := "likely limited stock"
	if reap.IsCode(lr.err, reap.CodeVariantUnavailable) {
		why = "it is out of stock"
	}
	return fmt.Sprintf("%s could not supply %d × %s (%s).", merchant, lr.qty, title, why)
}

// isolateRejected finds the variants of group g the merchant cannot supply, quoting its items one
// at a time when there are several (a quote charges nothing). qerr is the rejection of the whole
// group. It returns an empty map when every item is fine alone, and an error when a probe failed
// for another reason.
func (o *Impl) isolateRejected(ctx context.Context, keyPrefix string, g merchantGroup, email string, addr domain.Address, qerr error) (map[string]error, error) {
	bad := map[string]error{}
	if len(g.items) == 1 {
		bad[g.items[0].VariantID] = qerr
		return bad, nil
	}
	for _, it := range g.items {
		req := reap.CreateQuoteRequest{Email: email, Items: []reap.QuoteItem{it}, ShippingAddress: shippingAddress(addr)}
		_, err := o.d.Reap.CreateQuote(ctx, probeKey(keyPrefix, req), req)
		switch {
		case err == nil:
		case reap.IsItemRejection(err) && !retryableReap(err):
			bad[it.VariantID] = err
		default:
			return nil, fmt.Errorf("reap quote for %s (one item at a time): %w", g.merchantName, err)
		}
	}
	return bad, nil
}

// rejectionsFor maps rejected variants back to the request lines of the group.
func rejectionsFor(g merchantGroup, bad map[string]error) []lineRejection {
	units := map[string]int{}
	for _, it := range g.items {
		units[it.VariantID] = it.Quantity
	}
	var out []lineRejection
	for _, gl := range g.lines {
		if err, ok := bad[gl.offer.ReapVariantID]; ok {
			out = append(out, lineRejection{gl: gl, merchant: g.merchantName, merchantKey: g.key, qty: units[gl.offer.ReapVariantID], err: err})
		}
	}
	return out
}

// recordRejection writes the audit event (which is also the memory of which merchant rejected
// which line) and publishes the SSE event.
func (o *Impl) recordRejection(ctx context.Context, requestID, stage string, lr lineRejection) {
	msg := lr.message()
	o.record(ctx, requestID, domain.ActorSystem, stage, audit.CheckoutItemRejected, map[string]any{
		"line_item_id": lr.gl.line.ID, "offer_id": lr.gl.offer.ID, "merchant": lr.merchant, "merchant_key": lr.merchantKey,
		"variant_id": lr.gl.offer.ReapVariantID, "quantity": lr.qty, "title": lr.gl.offer.Title, "error": lr.err.Error(),
		"message": msg, "stage": stage,
	})
	o.publish(events.CheckoutItemRejected, requestID, map[string]any{
		"request_id": requestID, "line_item_id": lr.gl.line.ID, "merchant": lr.merchant, "quantity": lr.qty,
		"title": lr.gl.offer.Title, "message": msg, "stage": stage,
	})
}

// allowedVendorIDs: vendor id -> allowed (vendor row allowed and on the merchant allow-list).
func (o *Impl) allowedVendorIDs(ctx context.Context) (map[string]bool, error) {
	vendors, err := o.d.Store.Vendors().List(ctx, store.VendorFilter{})
	if err != nil {
		return nil, err
	}
	allowed := map[string]bool{}
	for _, v := range vendors {
		allowed[v.ID] = o.vendorAllowed(v)
	}
	return allowed, nil
}

// nextOffer is the best-ranked available offer of the line at an allowed merchant that has not
// rejected the line and that ok accepts.
func nextOffer(offers []domain.Offer, lineID string, rejectedBy map[string]bool, allowed map[string]bool, ok func(key string) bool) (domain.Offer, bool) {
	cands := make([]domain.Offer, 0, len(offers))
	for _, of := range offers {
		if of.LineItemID == lineID {
			cands = append(cands, of)
		}
	}
	sort.SliceStable(cands, func(i, j int) bool { return cands[i].Rank < cands[j].Rank })
	for _, of := range cands {
		k := offerKey(of)
		if !of.Available || of.ReapVariantID == "" || of.VendorID == nil || !allowed[*of.VendorID] || rejectedBy[k] || (ok != nil && !ok(k)) {
			continue
		}
		return of, true
	}
	return domain.Offer{}, false
}

// switchLine moves a rejected line to offer to, with a note the voice agent can read out.
func (o *Impl) switchLine(ctx context.Context, requestID, stage string, lr lineRejection, to domain.Offer) error {
	li := lr.gl.line
	id := to.ID
	li.SelectedOfferID = &id
	why := fmt.Sprintf("not enough stock for %d", lr.qty)
	if reap.IsCode(lr.err, reap.CodeVariantUnavailable) {
		why = "out of stock"
	}
	note := domain.Reason{Code: domain.ReasonVendorSwitched, Message: fmt.Sprintf("Switched from %s (%s) to %s.", lr.merchant, why, to.MerchantName)}
	li.Reasons = append(withoutNotes(li.Reasons), note)
	if err := o.d.Store.LineItems().Update(ctx, &li); err != nil {
		return fmt.Errorf("switch offer: %w", err)
	}
	qty := agents.PurchaseQty(li.Qty, to.PackSize)
	o.record(ctx, requestID, domain.ActorSystem, stage, audit.CheckoutOfferReplaced, map[string]any{
		"stage": stage, "line_item_id": li.ID, "from_offer_id": lr.gl.offer.ID, "from_merchant": lr.merchant,
		"to_offer_id": to.ID, "to_merchant": to.MerchantName, "quantity": qty, "note": note.Message,
	})
	o.publish(events.CheckoutOfferReplaced, requestID, map[string]any{
		"request_id": requestID, "line_item_id": li.ID, "from_offer": lr.gl.offer, "to_offer": to, "note": note.Message, "stage": stage,
	})
	return nil
}

// handleItemRejection runs after merchant group g's quote was rejected for its items (qerr). It
// returns errBasketChanged (run the round again), nil (a policy approval was opened), qerr (no
// single item is at fault: fail as before) or a permanent itemsUnavailableError.
func (o *Impl) handleItemRejection(ctx context.Context, run *checkoutRun, requestID string, g merchantGroup, addr domain.Address, qerr error) error {
	// 1. Isolate the offending variant(s).
	bad, err := o.isolateRejected(ctx, requestID, g, addr.Email, addr, qerr)
	if err != nil {
		return err
	}
	if len(bad) == 0 {
		return qerr // every item is fine alone: not an item-level problem we can fix
	}
	rejected := rejectionsFor(g, bad)

	// 2. Record the rejections.
	prior, err := o.rejectedMerchants(ctx, requestID)
	if err != nil {
		return err
	}
	for _, lr := range rejected {
		o.recordRejection(ctx, requestID, "checkout", lr)
		addRejected(prior, lr)
	}

	// 3. Pick the next-ranked offer at another merchant for every rejected line.
	d, err := o.d.Store.Requests().Detail(ctx, requestID)
	if err != nil {
		return err
	}
	allowed, err := o.allowedVendorIDs(ctx)
	if err != nil {
		return err
	}
	var missing []string
	choice := map[string]domain.Offer{} // line id -> fallback offer
	for _, lr := range rejected {
		of, ok := nextOffer(d.Offers, lr.gl.line.ID, prior[lr.gl.line.ID], allowed, func(k string) bool {
			return o.fallbackMerchantOK(run, d.Payments, k)
		})
		if !ok {
			missing = append(missing, lr.message())
			continue
		}
		choice[lr.gl.line.ID] = of
	}
	if len(missing) > 0 {
		return queue.Permanent(&itemsUnavailableError{msg: strings.Join(missing, " ") + " Try a smaller quantity or another item."})
	}

	// 4. Switch the lines and retire quotes of the merchants whose items change.
	targets := map[string]bool{}
	for _, lr := range rejected {
		to := choice[lr.gl.line.ID]
		if err := o.switchLine(ctx, requestID, "checkout", lr, to); err != nil {
			return err
		}
		targets[offerKey(to)] = true
	}
	for i := range d.Payments {
		p := &d.Payments[i]
		if !targets[paymentKey(*p)] || p.ReapCheckoutID != "" || p.Status == domain.PaymentFailed || p.Status == domain.PaymentExpired {
			continue
		}
		if p.ReapQuoteID != "" {
			o.markQuoteStale(ctx, p) // in run.unchecked (fallbackMerchantOK): no checkout was sent for it
			delete(run.unchecked, p.ID)
			continue
		}
		p.QuoteAttempt++ // half-done quote: the next quote has a different body, so a new key
		if err := o.d.Store.Payments().Update(ctx, p); err != nil {
			return err
		}
	}

	// 5. Re-run the deterministic policy for the changed basket.
	return o.reevaluateAfterFallback(ctx, requestID, rejected, choice)
}

func addRejected(prior map[string]map[string]bool, lr lineRejection) {
	if prior[lr.gl.line.ID] == nil {
		prior[lr.gl.line.ID] = map[string]bool{}
	}
	prior[lr.gl.line.ID][lr.merchantKey] = true
}

// fallbackMerchantOK: merchant key k may receive a fallback line. Refused when it has a checkout,
// or a quote that a checkout may already have been sent for (only quotes this job created and has
// not checked out can be replaced).
func (o *Impl) fallbackMerchantOK(run *checkoutRun, ps []domain.Payment, k string) bool {
	for _, p := range ps {
		if paymentKey(p) != k || p.Status == domain.PaymentFailed || p.Status == domain.PaymentExpired {
			continue
		}
		if p.ReapCheckoutID != "" {
			return false
		}
		if p.ReapQuoteID != "" && !run.unchecked[p.ID] {
			return false
		}
	}
	return true
}

// rejectedMerchants reads the audit trail: line id -> merchant keys that rejected that line.
func (o *Impl) rejectedMerchants(ctx context.Context, requestID string) (map[string]map[string]bool, error) {
	out := map[string]map[string]bool{}
	evs, err := o.d.Store.Audit().ListByRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	for _, ev := range evs {
		if ev.Type != audit.CheckoutItemRejected {
			continue
		}
		var p struct {
			LineItemID  string `json:"line_item_id"`
			MerchantKey string `json:"merchant_key"`
		}
		if json.Unmarshal(ev.Payload, &p) != nil || p.LineItemID == "" {
			continue
		}
		if out[p.LineItemID] == nil {
			out[p.LineItemID] = map[string]bool{}
		}
		out[p.LineItemID][p.MerchantKey] = true
	}
	return out, nil
}

// reevaluateAfterFallback re-runs policy (price ceilings, per-order limit, monthly budget including
// committed spend) over the changed basket and stores the new totals. Auto-approved: run the round
// again. Needs approval: open a policy approval (the request goes to pending_approval). A rejected
// fallback line: fail with a readable reason.
func (o *Impl) reevaluateAfterFallback(ctx context.Context, requestID string, rejected []lineRejection, choice map[string]domain.Offer) error {
	o.budgetMu.Lock()
	defer o.budgetMu.Unlock()
	res, err := o.evaluateStored(ctx, requestID)
	if err != nil {
		return err
	}
	o.publish(events.PolicyEvaluated, requestID, map[string]any{
		"request_id": requestID, "decision": res.Decision, "reasons": nonNil(res.Reasons), "lines": res.Lines, "total_cents": res.TotalCents,
	})
	for _, lr := range rejected {
		for _, l := range res.Lines {
			if l.LineItemID != lr.gl.line.ID || l.Decision != domain.DecisionReject {
				continue
			}
			why := "it is not allowed by the purchasing policy"
			if len(l.Reasons) > 0 {
				why = l.Reasons[0].Message
			}
			return queue.Permanent(&itemsUnavailableError{msg: fmt.Sprintf("%s The other offer, from %s, cannot be used: %s",
				lr.message(), choice[lr.gl.line.ID].MerchantName, why)})
		}
	}

	if err := o.storeBasketTotals(ctx, requestID, res); err != nil {
		return err
	}
	switch res.Decision {
	case domain.DecisionAutoApprove:
		return errBasketChanged
	case domain.DecisionReject:
		return queue.Permanent(&itemsUnavailableError{msg: "No item in this order can be bought within the purchasing policy after the vendor change."})
	}
	if o.d.Approvals == nil {
		return queue.Permanent(fmt.Errorf("the changed basket needs approval but no approvals service is configured"))
	}
	if _, err := o.d.Approvals.Create(ctx, approvals.CreateInput{
		RequestID: requestID, Kind: domain.ApprovalKindPolicy, Reasons: nonNil(res.Reasons), AmountCents: res.TotalCents,
	}); err != nil {
		return fmt.Errorf("open approval for the changed basket: %w", err)
	}
	return nil
}

// storeBasketTotals stores the policy result and the basket totals (as the search step does) after
// the selected offers changed.
func (o *Impl) storeBasketTotals(ctx context.Context, requestID string, res policy.Result) error {
	d, err := o.d.Store.Requests().Detail(ctx, requestID)
	if err != nil {
		return err
	}
	offers := map[string]domain.Offer{}
	for _, of := range d.Offers {
		offers[of.ID] = of
	}
	var subtotal, shipping domain.Cents
	for _, li := range d.LineItems {
		if li.PolicyDecision == domain.DecisionReject || li.SelectedOfferID == nil {
			continue
		}
		if of, ok := offers[*li.SelectedOfferID]; ok {
			subtotal += of.UnitPriceCents.MulQty(agents.PurchaseQty(li.Qty, of.PackSize))
			shipping += of.ShippingCents
		}
	}
	r := d.Request
	r.SubtotalCents, r.ShippingCents, r.TotalCents, r.Decision = subtotal, shipping, res.TotalCents, res.Decision
	return o.d.Store.Requests().Update(ctx, &r)
}

// Line notes are reasons that are not policy findings but facts about the line the user should
// hear (a vendor switch, a merchant that cannot supply the quantity). They survive policy re-runs.
func isNote(c domain.ReasonCode) bool {
	return c == domain.ReasonVendorSwitched || c == domain.ReasonUnavailable
}

func withoutNotes(rs []domain.Reason) []domain.Reason {
	out := []domain.Reason{}
	for _, r := range rs {
		if !isNote(r.Code) {
			out = append(out, r)
		}
	}
	return out
}

func notesOf(rs []domain.Reason) []domain.Reason {
	var out []domain.Reason
	for _, r := range rs {
		if isNote(r.Code) {
			out = append(out, r)
		}
	}
	return out
}

// mergeNotes combines fresh policy reasons for a line with the line's stored notes. An
// "unavailable" line (no offer left) shows only its plain reason instead of policy's NO_OFFER.
func mergeNotes(li domain.LineItem, lr policy.LineResult) []domain.Reason {
	notes := notesOf(li.Reasons)
	if len(notes) == 0 {
		return nonNil(lr.Reasons)
	}
	var unavailable, other []domain.Reason
	for _, n := range notes {
		if n.Code == domain.ReasonUnavailable {
			unavailable = append(unavailable, n)
		} else {
			other = append(other, n)
		}
	}
	if len(unavailable) > 0 && li.SelectedOfferID == nil && lr.Decision == domain.DecisionReject {
		return unavailable
	}
	return append(append([]domain.Reason{}, nonNil(lr.Reasons)...), other...)
}

func sameReasons(a, b []domain.Reason) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
