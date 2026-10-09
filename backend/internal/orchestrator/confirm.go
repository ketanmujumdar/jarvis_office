package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/policy"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Confirm records the delivery address and the confirming user, re-runs policy on the stored
// basket (month-to-date spend may have changed since quoting) and then either approves and starts
// checkout, opens a policy approval, or rejects.
func (o *Impl) Confirm(ctx context.Context, id string, in ConfirmInput) (domain.RequestDetail, error) {
	if strings.TrimSpace(in.AddressID) == "" {
		return domain.RequestDetail{}, fmt.Errorf("%w: address_id is required: ask which delivery address to use", domain.ErrValidation)
	}
	unlock := o.lock(id)
	defer unlock()
	r, err := o.d.Store.Requests().Get(ctx, id)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	if r.Status != domain.StatusQuoted {
		return domain.RequestDetail{}, fmt.Errorf("%w: request is %s, not quoted", domain.ErrInvalidTransition, r.Status)
	}
	addr, err := o.d.Store.Addresses().Get(ctx, in.AddressID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return domain.RequestDetail{}, fmt.Errorf("%w: unknown delivery address %q", domain.ErrValidation, in.AddressID)
		}
		return domain.RequestDetail{}, err
	}
	if _, err := o.activeEnrollment(ctx); err != nil {
		return domain.RequestDetail{}, err
	}

	// Serialise "read committed spend -> evaluate -> approve" across requests so two concurrent
	// confirms cannot both pass the monthly budget.
	o.budgetMu.Lock()
	defer o.budgetMu.Unlock()
	res, err := o.evaluateStored(ctx, r.ID)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	now := o.d.Clock().UTC()
	uid := in.UserID
	r.AddressID, r.ConfirmedAt, r.Decision, r.TotalCents = &addr.ID, &now, res.Decision, res.TotalCents
	if uid != "" {
		r.ConfirmedBy = &uid
	}
	if err := o.d.Store.Requests().Update(ctx, &r); err != nil {
		return domain.RequestDetail{}, fmt.Errorf("save confirmation: %w", err)
	}
	o.record(ctx, r.ID, domain.ActorUser, uid, audit.RequestConfirmed, map[string]any{"address_id": addr.ID, "address_label": addr.Label, "decision": res.Decision, "total_cents": res.TotalCents})

	switch res.Decision {
	case domain.DecisionAutoApprove:
		if err := o.transition(ctx, r.ID, domain.StatusQuoted, domain.StatusApproved, "", domain.ActorSystem, "policy"); err != nil {
			return domain.RequestDetail{}, err
		}
		if err := o.enqueueCheckout(ctx, r.ID, "auto"); err != nil {
			return domain.RequestDetail{}, err
		}
	case domain.DecisionReject:
		if err := o.transition(ctx, r.ID, domain.StatusQuoted, domain.StatusRejected, "", domain.ActorSystem, "policy"); err != nil {
			return domain.RequestDetail{}, err
		}
	default:
		if o.d.Approvals == nil {
			return domain.RequestDetail{}, fmt.Errorf("%w: approvals service not configured", domain.ErrNotImplemented)
		}
		if _, err := o.d.Approvals.Create(ctx, approvals.CreateInput{
			RequestID: r.ID, Kind: domain.ApprovalKindPolicy, Reasons: nonNil(res.Reasons), AmountCents: res.TotalCents,
		}); err != nil {
			return domain.RequestDetail{}, fmt.Errorf("open approval: %w", err)
		}
	}
	return o.GetRequest(ctx, r.ID)
}

// evaluateStored re-runs policy over the stored basket and persists the per-line decisions.
func (o *Impl) evaluateStored(ctx context.Context, requestID string) (policy.Result, error) {
	d, err := o.d.Store.Requests().Detail(ctx, requestID)
	if err != nil {
		return policy.Result{}, err
	}
	vendors, err := o.d.Store.Vendors().List(ctx, store.VendorFilter{})
	if err != nil {
		return policy.Result{}, err
	}
	vendors = o.withAllowList(vendors)
	items, err := o.catalogFor(ctx, d.LineItems)
	if err != nil {
		return policy.Result{}, err
	}
	cfg, err := o.d.Store.Policy().Get(ctx)
	if err != nil {
		return policy.Result{}, err
	}
	// Completed spend this month plus money other approved/in-flight requests already committed.
	mtd, err := o.committedSpend(ctx, requestID)
	if err != nil {
		return policy.Result{}, err
	}
	offers := map[string]domain.Offer{}
	for _, of := range d.Offers {
		offers[of.ID] = of
	}
	in := policy.Input{Config: cfg, MonthToDateCents: mtd}
	for _, li := range d.LineItems {
		var sel *domain.Offer
		if li.SelectedOfferID != nil {
			if of, ok := offers[*li.SelectedOfferID]; ok {
				sel = &of
			}
		}
		in.Lines = append(in.Lines, lineInput(li, items[li.ID], sel, vendors))
	}
	res := o.d.Evaluate(in)
	byLine := map[string]policy.LineResult{}
	for _, lr := range res.Lines {
		byLine[lr.LineItemID] = lr
	}
	for _, li := range d.LineItems {
		lr, ok := byLine[li.ID]
		if !ok {
			continue
		}
		reasons := mergeNotes(li, lr) // keep vendor-switch / unavailable notes
		for i := range res.Lines {
			if res.Lines[i].LineItemID == li.ID {
				res.Lines[i].Reasons = reasons
			}
		}
		if lr.Decision == li.PolicyDecision && sameReasons(reasons, li.Reasons) {
			continue
		}
		li.PolicyDecision, li.Reasons = lr.Decision, reasons
		if err := o.d.Store.LineItems().Update(ctx, &li); err != nil {
			return policy.Result{}, err
		}
	}
	o.record(ctx, requestID, domain.ActorSystem, "policy", audit.PolicyEvaluated, res)
	return res, nil
}

// OnApprovalDecided continues the flow after an approver decides (called by approvals after commit).
func (o *Impl) OnApprovalDecided(ctx context.Context, a domain.Approval) error {
	unlock := o.lock(a.RequestID)
	defer unlock()
	r, err := o.d.Store.Requests().Get(ctx, a.RequestID)
	if err != nil {
		return err
	}
	if r.Status != domain.StatusPendingApproval {
		o.log.Info("approval decided for request not pending approval; ignoring", "request_id", r.ID, "status", r.Status)
		return nil
	}
	actorID := ""
	if a.ApproverID != nil {
		actorID = *a.ApproverID
	}
	switch a.Status {
	case domain.ApprovalApproved:
		if a.Kind == domain.ApprovalKindPriceDrift || a.AmountCents > 0 {
			// The approver approved this amount (for drift: the live total), which becomes the
			// baseline for the next drift check.
			r.TotalCents = a.AmountCents
			if err := o.d.Store.Requests().Update(ctx, &r); err != nil {
				return err
			}
		}
		if err := o.transition(ctx, r.ID, domain.StatusPendingApproval, domain.StatusApproved, "", domain.ActorUser, actorID); err != nil {
			return err
		}
		return o.enqueueCheckout(ctx, r.ID, a.ID)
	case domain.ApprovalRejected:
		return o.transition(ctx, r.ID, domain.StatusPendingApproval, domain.StatusRejected, "", domain.ActorUser, actorID)
	}
	return nil
}

func (o *Impl) enqueueCheckout(ctx context.Context, requestID, round string) error {
	_, err := o.d.Queue.Enqueue(ctx, queue.Job{JobID: "checkout:" + requestID + ":" + round, Kind: queue.KindCheckout, RequestID: requestID})
	if err != nil {
		return fmt.Errorf("enqueue checkout: %w", err)
	}
	return nil
}
