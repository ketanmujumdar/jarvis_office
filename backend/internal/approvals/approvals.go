// Package approvals manages the approval queue (no expiry). Owner: agent B.
//
// An approval is opened when the policy engine says NEEDS_APPROVAL (kind policy) or when the live
// Reap quote drifted past the threshold (kind price_drift). Opening it moves the request to
// pending_approval in the same transaction. Deciding it is plain code: only approver/admin roles may
// decide, only a pending approval of a request that is still pending_approval can be decided, and the
// DecisionHandler (the orchestrator) is called after the commit to move the request on.
package approvals

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Audit event types written by this package (same strings as the audit package constants; the
// audit package is not imported so that the event is appended inside the approval transaction).
const (
	auditApprovalRequested    = "approval.requested"
	auditApprovalDecided      = "approval.decided"
	auditRequestStatusChanged = "request.status_changed"
)

// MaxTopOffers is how many ranked offers per line the approver console shows.
const MaxTopOffers = 3

// DecisionHandler is notified after an approval is decided (implemented by the orchestrator, which
// moves the request to approved -> checking_out, or to rejected). Called after the DB commit.
type DecisionHandler interface {
	OnApprovalDecided(ctx context.Context, a domain.Approval) error
}

// CreateInput opens an approval task.
type CreateInput struct {
	RequestID   string
	Kind        domain.ApprovalKind
	Reasons     []domain.Reason
	AmountCents domain.Cents
	PrevCents   *domain.Cents // price_drift only
}

// ApprovalView is an approval enriched for the approver console (reasons + top 3 quotes per line).
type ApprovalView struct {
	Approval  domain.Approval        `json:"approval"`
	Request   domain.PurchaseRequest `json:"request"`
	Requester domain.User            `json:"requester"`
	Lines     []LineView             `json:"lines"`
}

// LineView is one line with its top offers (max 3, by rank).
type LineView struct {
	LineItem  domain.LineItem     `json:"line_item"`
	Catalog   *domain.CatalogItem `json:"catalog_item,omitempty"`
	TopOffers []domain.Offer      `json:"top_offers"`
}

// Service is the approvals API used by the orchestrator and HTTP handlers.
type Service interface {
	// Create opens a pending approval and moves the request to pending_approval (same tx).
	// Emits audit "approval.requested" and SSE event approval.requested.
	Create(ctx context.Context, in CreateInput) (domain.Approval, error)
	Get(ctx context.Context, id string) (ApprovalView, error)
	List(ctx context.Context, f store.ApprovalFilter) ([]ApprovalView, error)
	// Approve/Reject require approver.Role.CanApprove() (else ErrForbidden) and a pending approval
	// (else ErrConflict). They record audit "approval.decided" and then call DecisionHandler.
	Approve(ctx context.Context, id string, approver domain.User, comment string) (domain.Approval, error)
	Reject(ctx context.Context, id string, approver domain.User, comment string) (domain.Approval, error)
}

// Deps of the service implementation.
type Deps struct {
	Store   store.Store
	Handler DecisionHandler // may be set after construction via SetHandler (cycle with orchestrator)
	Events  events.Bus      // optional; SSE approval.requested / approval.decided / request.status_changed
}

// New returns the Service.
func New(d Deps) *Impl { return &Impl{d: d} }

// Impl implements Service.
type Impl struct{ d Deps }

var _ Service = (*Impl)(nil)

// SetHandler wires the orchestrator after both are constructed.
func (s *Impl) SetHandler(h DecisionHandler) { s.d.Handler = h }

// SetEvents wires the SSE bus after construction (optional).
func (s *Impl) SetEvents(b events.Bus) { s.d.Events = b }

// Create opens a pending approval and moves the request to pending_approval atomically.
func (s *Impl) Create(ctx context.Context, in CreateInput) (domain.Approval, error) {
	if err := validateCreate(in); err != nil {
		return domain.Approval{}, err
	}
	reasons := in.Reasons
	if reasons == nil {
		reasons = []domain.Reason{}
	}
	a := domain.Approval{
		RequestID:   in.RequestID,
		Kind:        in.Kind,
		Status:      domain.ApprovalPending,
		Reasons:     reasons,
		AmountCents: in.AmountCents,
		PrevCents:   in.PrevCents,
	}
	var from domain.RequestStatus
	err := s.d.Store.WithTx(ctx, func(tx store.Store) error {
		req, err := tx.Requests().Get(ctx, in.RequestID)
		if err != nil {
			return fmt.Errorf("approvals: load request: %w", err)
		}
		from = req.Status
		if err := domain.CheckTransition(from, domain.StatusPendingApproval); err != nil {
			return fmt.Errorf("approvals: open approval: %w", err)
		}
		if err := tx.Approvals().Create(ctx, &a); err != nil {
			return fmt.Errorf("approvals: create: %w", err)
		}
		if err := tx.Requests().TransitionStatus(ctx, req.ID, from, domain.StatusPendingApproval, ""); err != nil {
			return fmt.Errorf("approvals: move request to pending_approval: %w", err)
		}
		if err := appendAudit(ctx, tx, req.ID, domain.ActorSystem, "", auditApprovalRequested, map[string]any{
			"approval_id": a.ID, "kind": a.Kind, "reasons": a.Reasons,
			"amount_cents": a.AmountCents, "prev_cents": a.PrevCents,
		}); err != nil {
			return err
		}
		return appendAudit(ctx, tx, req.ID, domain.ActorSystem, "", auditRequestStatusChanged, map[string]any{
			"from": from, "to": domain.StatusPendingApproval,
		})
	})
	if err != nil {
		return domain.Approval{}, err
	}
	s.publish(events.RequestStatusChanged, a.RequestID, map[string]any{
		"request_id": a.RequestID, "from": from, "to": domain.StatusPendingApproval,
	})
	s.publish(events.ApprovalRequested, a.RequestID, map[string]any{"approval": a})
	return a, nil
}

func validateCreate(in CreateInput) error {
	if strings.TrimSpace(in.RequestID) == "" {
		return fmt.Errorf("%w: request_id is required", domain.ErrValidation)
	}
	switch in.Kind {
	case domain.ApprovalKindPolicy:
		if in.PrevCents != nil {
			return fmt.Errorf("%w: prev_cents is only valid for price_drift approvals", domain.ErrValidation)
		}
	case domain.ApprovalKindPriceDrift:
		if in.PrevCents == nil {
			return fmt.Errorf("%w: price_drift approval needs prev_cents", domain.ErrValidation)
		}
		if *in.PrevCents < 0 {
			return fmt.Errorf("%w: prev_cents must not be negative", domain.ErrValidation)
		}
	default:
		return fmt.Errorf("%w: unknown approval kind %q", domain.ErrValidation, in.Kind)
	}
	if in.AmountCents < 0 {
		return fmt.Errorf("%w: amount_cents must not be negative", domain.ErrValidation)
	}
	if len(in.Reasons) == 0 {
		return fmt.Errorf("%w: an approval needs at least one reason", domain.ErrValidation)
	}
	return nil
}

// Get returns one approval with its request, requester and lines (top 3 offers each).
func (s *Impl) Get(ctx context.Context, id string) (ApprovalView, error) {
	a, err := s.d.Store.Approvals().Get(ctx, id)
	if err != nil {
		return ApprovalView{}, fmt.Errorf("approvals: get %s: %w", id, err)
	}
	return s.view(ctx, a)
}

// List returns approvals (newest first) as enriched views.
func (s *Impl) List(ctx context.Context, f store.ApprovalFilter) ([]ApprovalView, error) {
	as, err := s.d.Store.Approvals().List(ctx, f)
	if err != nil {
		return nil, fmt.Errorf("approvals: list: %w", err)
	}
	out := make([]ApprovalView, 0, len(as))
	for _, a := range as {
		v, err := s.view(ctx, a)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func (s *Impl) view(ctx context.Context, a domain.Approval) (ApprovalView, error) {
	st := s.d.Store
	if a.Reasons == nil {
		a.Reasons = []domain.Reason{}
	}
	v := ApprovalView{Approval: a, Lines: []LineView{}}
	req, err := st.Requests().Get(ctx, a.RequestID)
	if err != nil {
		return ApprovalView{}, fmt.Errorf("approvals: load request %s: %w", a.RequestID, err)
	}
	v.Request = req
	u, err := st.Users().Get(ctx, req.RequesterID)
	switch {
	case err == nil:
		v.Requester = u
	case errors.Is(err, domain.ErrNotFound):
		v.Requester = domain.User{ID: req.RequesterID}
	default:
		return ApprovalView{}, fmt.Errorf("approvals: load requester: %w", err)
	}
	items, err := st.LineItems().ListByRequest(ctx, req.ID)
	if err != nil {
		return ApprovalView{}, fmt.Errorf("approvals: load line items: %w", err)
	}
	catCache := map[string]*domain.CatalogItem{}
	for _, li := range items {
		if li.Reasons == nil {
			li.Reasons = []domain.Reason{}
		}
		lv := LineView{LineItem: li}
		if li.CatalogItemID != nil {
			cid := *li.CatalogItemID
			c, ok := catCache[cid]
			if !ok {
				it, err := st.Catalog().Get(ctx, cid)
				switch {
				case err == nil:
					c = &it
				case errors.Is(err, domain.ErrNotFound): // deleted since; show the line without it
				default:
					return ApprovalView{}, fmt.Errorf("approvals: load catalog item: %w", err)
				}
				catCache[cid] = c
			}
			lv.Catalog = c
		}
		offers, err := st.Offers().ListByLineItem(ctx, li.ID)
		if err != nil {
			return ApprovalView{}, fmt.Errorf("approvals: load offers: %w", err)
		}
		lv.TopOffers = TopOffers(offers, MaxTopOffers)
		v.Lines = append(v.Lines, lv)
	}
	return v, nil
}

// TopOffers returns up to n offers ordered by rank (1 = best; rank <= 0 sorts last), keeping the
// input order for ties. Unavailable offers are listed after available ones. It never returns nil.
func TopOffers(offers []domain.Offer, n int) []domain.Offer {
	out := make([]domain.Offer, len(offers))
	copy(out, offers)
	key := func(o domain.Offer) (int, int) {
		avail := 0
		if !o.Available {
			avail = 1
		}
		r := o.Rank
		if r <= 0 {
			r = int(^uint(0) >> 1)
		}
		return avail, r
	}
	sort.SliceStable(out, func(i, j int) bool {
		ai, ri := key(out[i])
		aj, rj := key(out[j])
		if ai != aj {
			return ai < aj
		}
		return ri < rj
	})
	if n >= 0 && len(out) > n {
		out = out[:n]
	}
	return out
}

// Approve records an approval decision and notifies the orchestrator.
func (s *Impl) Approve(ctx context.Context, id string, approver domain.User, comment string) (domain.Approval, error) {
	return s.decide(ctx, id, approver, comment, domain.ApprovalApproved)
}

// Reject records a rejection and notifies the orchestrator.
func (s *Impl) Reject(ctx context.Context, id string, approver domain.User, comment string) (domain.Approval, error) {
	return s.decide(ctx, id, approver, comment, domain.ApprovalRejected)
}

func (s *Impl) decide(ctx context.Context, id string, approver domain.User, comment string, status domain.ApprovalStatus) (domain.Approval, error) {
	if !approver.Role.CanApprove() {
		return domain.Approval{}, fmt.Errorf("%w: role %q may not decide approvals", domain.ErrForbidden, approver.Role)
	}
	if strings.TrimSpace(approver.ID) == "" {
		return domain.Approval{}, fmt.Errorf("%w: approver id is required", domain.ErrValidation)
	}
	comment = strings.TrimSpace(comment)
	var decided domain.Approval
	err := s.d.Store.WithTx(ctx, func(tx store.Store) error {
		a, err := tx.Approvals().Get(ctx, id)
		if err != nil {
			return fmt.Errorf("approvals: get %s: %w", id, err)
		}
		if a.Status != domain.ApprovalPending {
			return fmt.Errorf("%w: approval %s is already %s", domain.ErrConflict, id, a.Status)
		}
		req, err := tx.Requests().Get(ctx, a.RequestID)
		if err != nil {
			return fmt.Errorf("approvals: load request: %w", err)
		}
		if req.Status != domain.StatusPendingApproval {
			return fmt.Errorf("%w: request %s is %s, not pending_approval", domain.ErrConflict, req.ID, req.Status)
		}
		decided, err = tx.Approvals().Decide(ctx, id, status, approver.ID, comment)
		if err != nil {
			return fmt.Errorf("approvals: decide: %w", err)
		}
		return appendAudit(ctx, tx, a.RequestID, domain.ActorUser, approver.ID, auditApprovalDecided, map[string]any{
			"approval_id": decided.ID, "kind": decided.Kind, "status": decided.Status,
			"comment": decided.Comment, "amount_cents": decided.AmountCents,
		})
	})
	if err != nil {
		return domain.Approval{}, err
	}
	s.publish(events.ApprovalDecided, decided.RequestID, map[string]any{"approval": decided})
	if h := s.d.Handler; h != nil {
		if err := h.OnApprovalDecided(ctx, decided); err != nil {
			// The decision is committed; surface the follow-up failure to the caller.
			return decided, fmt.Errorf("approvals: decision recorded but handler failed: %w", err)
		}
	}
	return decided, nil
}

func appendAudit(ctx context.Context, tx store.Store, requestID string, actor domain.ActorType, actorID, typ string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("approvals: audit payload: %w", err)
	}
	e := &domain.AuditEvent{ActorType: actor, ActorID: actorID, Type: typ, Payload: raw}
	if requestID != "" {
		rid := requestID
		e.RequestID = &rid
	}
	if err := tx.Audit().Append(ctx, e); err != nil {
		return fmt.Errorf("approvals: audit %s: %w", typ, err)
	}
	return nil
}

func (s *Impl) publish(t events.Type, requestID string, data any) {
	if s.d.Events != nil {
		s.d.Events.Publish(t, requestID, data)
	}
}
