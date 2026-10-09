package orchestrator

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/policy"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
)

// Pre-confirm quote check ("preflight").
//
// While a request moves to quoted, the selected basket is quoted with Reap once per merchant (in
// parallel, bounded by Deps.QuoteProbeTimeout) using the default delivery address and the
// requester's email. This only validates that each merchant can supply its lines: the quotes are
// never checked out and never reused (confirm creates its own live quote for the address the user
// picks). A line the merchant rejects moves to its next-ranked offer at another allowed merchant
// (with a note the voice agent can read out), or is marked unavailable with a plain reason when
// there is none. Any other probe failure (timeout, 5xx, QUOTE_TEMPORARILY_UNAVAILABLE, ...) is
// ignored: the request is quoted as before and the checkout-time fallback still applies.

const (
	defaultQuoteProbeTimeout = 6 * time.Second
	maxPreflightRounds       = 3
)

type probeResult struct {
	g   merchantGroup
	bad map[string]error
	err error
}

// preflightQuotes returns the new policy result when the basket changed, or nil.
func (o *Impl) preflightQuotes(ctx context.Context, requestID string) (*policy.Result, error) {
	if o.d.Reap == nil || o.d.QuoteProbeTimeout < 0 {
		return nil, nil
	}
	pctx, cancel := context.WithTimeout(ctx, o.d.QuoteProbeTimeout)
	defer cancel()

	d, err := o.d.Store.Requests().Detail(ctx, requestID)
	if err != nil {
		return nil, err
	}
	addr, err := o.d.Store.Addresses().GetDefault(ctx)
	if err != nil {
		o.log.Info("quote preflight skipped: no default address", "request_id", requestID, "err", err)
		return nil, nil
	}
	email := addr.Email
	if d.Request.RequesterID != "" {
		if u, err := o.d.Store.Users().Get(ctx, d.Request.RequesterID); err == nil && u.Email != "" && !domain.ReservedEmailDomain(u.Email) {
			email = u.Email
		}
	}
	allowed, err := o.allowedVendorIDs(ctx)
	if err != nil {
		return nil, err
	}
	prior, err := o.rejectedMerchants(ctx, requestID)
	if err != nil {
		return nil, err
	}
	keyPrefix := requestID + ":preflight"

	changed := false
	for round := 0; round < maxPreflightRounds && pctx.Err() == nil; round++ {
		if round > 0 {
			if d, err = o.d.Store.Requests().Detail(ctx, requestID); err != nil {
				return nil, err
			}
		}
		groups := basketGroups(d)
		if len(groups) == 0 {
			break
		}
		results := make([]probeResult, len(groups))
		var wg sync.WaitGroup
		for i, g := range groups {
			wg.Add(1)
			go func(i int, g merchantGroup) {
				defer wg.Done()
				results[i] = o.probeGroup(pctx, keyPrefix, g, email, addr)
			}(i, g)
		}
		wg.Wait()

		var rejected []lineRejection
		for _, pr := range results {
			if pr.err != nil {
				o.log.Info("quote preflight probe failed; continuing", "request_id", requestID, "merchant", pr.g.merchantName, "err", pr.err)
				continue
			}
			rejected = append(rejected, rejectionsFor(pr.g, pr.bad)...)
		}
		if len(rejected) == 0 {
			break
		}
		changed = true
		for _, lr := range rejected {
			o.recordRejection(ctx, requestID, "preflight", lr)
			addRejected(prior, lr)
		}
		for _, lr := range rejected {
			if to, ok := nextOffer(d.Offers, lr.gl.line.ID, prior[lr.gl.line.ID], allowed, nil); ok {
				if err := o.switchLine(ctx, requestID, "preflight", lr, to); err != nil {
					return nil, err
				}
				continue
			}
			if err := o.markUnavailable(ctx, lr); err != nil {
				return nil, err
			}
		}
	}
	if !changed {
		return nil, nil
	}
	o.budgetMu.Lock()
	defer o.budgetMu.Unlock()
	res, err := o.evaluateStored(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if err := o.storeBasketTotals(ctx, requestID, res); err != nil {
		return nil, err
	}
	return &res, nil
}

// probeGroup quotes one merchant group (and, when the merchant rejects its items, each item alone).
func (o *Impl) probeGroup(ctx context.Context, keyPrefix string, g merchantGroup, email string, addr domain.Address) probeResult {
	req := reap.CreateQuoteRequest{Email: email, Items: g.items, ShippingAddress: shippingAddress(addr)}
	_, err := o.d.Reap.CreateQuote(ctx, probeKey(keyPrefix, req), req)
	if err == nil {
		return probeResult{g: g}
	}
	if !reap.IsItemRejection(err) || retryableReap(err) {
		return probeResult{g: g, err: err}
	}
	bad, perr := o.isolateRejected(ctx, keyPrefix, g, email, addr, err)
	if perr != nil {
		return probeResult{g: g, err: perr}
	}
	return probeResult{g: g, bad: bad}
}

// markUnavailable drops the line's offer and stores the plain reason (the line becomes REJECT, so
// it is not bought; policy re-runs keep the reason).
func (o *Impl) markUnavailable(ctx context.Context, lr lineRejection) error {
	li := lr.gl.line
	why := "limited stock"
	if reap.IsCode(lr.err, reap.CodeVariantUnavailable) {
		why = "out of stock"
	}
	li.SelectedOfferID = nil
	li.PolicyDecision = domain.DecisionReject
	li.Reasons = []domain.Reason{{Code: domain.ReasonUnavailable,
		Message: fmt.Sprintf("%s can't supply %d × %s (%s), and no other approved vendor has it.", lr.merchant, lr.qty, lr.title(), why)}}
	if err := o.d.Store.LineItems().Update(ctx, &li); err != nil {
		return fmt.Errorf("mark line unavailable: %w", err)
	}
	return nil
}
