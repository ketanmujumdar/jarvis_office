// Package orchestrator owns the PurchaseRequest state machine: parse -> search fan-out -> rank ->
// policy -> approval -> Reap quote + drift check -> checkout -> poll -> ordered. Owner: agent E.
//
// All state lives in Postgres; jobs run on queue.Runner and are idempotent, so a restart resumes
// via Resume(). Every transition writes an audit event and publishes an SSE event.
//
// LLM proposes, code decides: only the parse step calls the LLM (through agents.Parser). Catalog
// matching, ranking, policy, approvals, drift checks and checkout are deterministic code.
package orchestrator

import (
	"context"
	"errors"
	"fmt"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/allowlist"
	"io"
	"log/slog"
	"strings"
	"sync"
	"time"

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

// CreateRequestInput starts a request. If Items is non-empty the LLM parse step is skipped.
type CreateRequestInput struct {
	RequesterID string
	Utterance   string
	Items       []agents.RequestedItem
}

// ConfirmInput is the manager's confirmation (the agent must have asked which address).
type ConfirmInput struct {
	UserID    string
	AddressID string // required; must exist
}

// Service is the orchestrator API used by HTTP handlers and agent tools.
type Service interface {
	// CreateRequest inserts the request (status parsing) and enqueues parse/search; returns at once.
	CreateRequest(ctx context.Context, in CreateRequestInput) (domain.PurchaseRequest, error)
	GetRequest(ctx context.Context, id string) (domain.RequestDetail, error)
	ListRequests(ctx context.Context, f store.RequestFilter) ([]domain.PurchaseRequest, error)
	// Confirm is valid only in status quoted. Sets address + confirmed_by, then moves to approved
	// (all lines AUTO_APPROVE and order AUTO_APPROVE) and enqueues checkout, or creates a policy
	// approval (pending_approval). ErrNoActiveEnrollment if no ACTIVE card enrollment exists.
	Confirm(ctx context.Context, id string, in ConfirmInput) (domain.RequestDetail, error)
	// Cancel is allowed before paying; ErrInvalidTransition otherwise.
	Cancel(ctx context.Context, id, userID string) (domain.RequestDetail, error)
	// RefreshPayments polls Reap GET /agentic/checkouts/:id for in-flight payments now and applies
	// status changes (used by the UI "I've approved, check now" button and the poll job).
	RefreshPayments(ctx context.Context, requestID string) (domain.RequestDetail, error)

	// Card enrollment (Reap EXTERNAL hosted card-entry page). No card data touches our system.
	StartEnrollment(ctx context.Context, user domain.User) (domain.Enrollment, error)
	// CurrentEnrollment returns the latest enrollment, refreshing its status from Reap if not terminal.
	CurrentEnrollment(ctx context.Context) (domain.Enrollment, error)

	// OnApprovalDecided continues the flow after an approver decides.
	approvals.DecisionHandler

	// Resume re-enqueues work for non-terminal requests and in-flight payments (call on boot).
	Resume(ctx context.Context) error
}

// Deps are the orchestrator's collaborators.
type Deps struct {
	Store     store.Store
	Queue     queue.Runner
	Bus       events.Bus
	Audit     audit.Logger
	Approvals approvals.Service
	Reap      reap.Client
	Parser    agents.Parser
	Adapter   agents.VendorAdapter
	Clock     func() time.Time

	SearchDeadline       time.Duration // default 20s
	ResultsPerVendor     int           // default 5
	CheckoutPollInterval time.Duration // default 3s
	CheckoutPollTimeout  time.Duration // default 30m
	ReapReturnURL        string

	// Optional (additive, see docs/CONTRACTS.md change log).
	Evaluate               func(policy.Input) policy.Result // default policy.Evaluate
	MaxSearchConcurrency   int                              // global cap on concurrent vendor searches, default 4
	MaxOffListVendors      int                              // vendor-specific searches for off-list items, default 6
	EnrollmentPollInterval time.Duration                    // default 5s
	EnrollmentPollTimeout  time.Duration                    // default 30m
	NewID                  func() string                    // idempotency keys, session ids; default UUIDv4
	// Matcher filters search hits to the requested item and sets pack sizes (nil = no filtering).
	Matcher OfferMatcher
	Logger  *slog.Logger
	// AllowedMerchants is the merchant allow-list (default: allowlist.Default(), i.e.
	// backend/seed/allowed_merchants.tsv). Vendors off it are never searched, approved or paid.
	AllowedMerchants *allowlist.Set
}

// Impl implements Service.
type Impl struct {
	d    Deps
	log  *slog.Logger
	sem  chan struct{} // global vendor-search concurrency
	mu   sync.Mutex
	reqs map[string]*sync.Mutex // per-request locks for checkout/refresh
	// budgetMu serialises "read committed spend -> evaluate -> commit" across requests (Confirm
	// and the checkout-time limit re-check) so two concurrent orders cannot both pass the
	// monthly budget. Lock order: request lock, then budgetMu. Single process (MVP).
	budgetMu sync.Mutex
	allow    allowlist.Set
}

var _ Service = (*Impl)(nil)

// New returns the orchestrator and registers its job handlers on d.Queue (call before Start).
func New(d Deps) *Impl {
	if d.Clock == nil {
		d.Clock = time.Now
	}
	if d.SearchDeadline <= 0 {
		d.SearchDeadline = 20 * time.Second
	}
	if d.ResultsPerVendor <= 0 {
		d.ResultsPerVendor = 5
	}
	if d.CheckoutPollInterval <= 0 {
		d.CheckoutPollInterval = 3 * time.Second
	}
	if d.CheckoutPollTimeout <= 0 {
		d.CheckoutPollTimeout = 30 * time.Minute
	}
	if d.EnrollmentPollInterval <= 0 {
		d.EnrollmentPollInterval = 5 * time.Second
	}
	if d.EnrollmentPollTimeout <= 0 {
		d.EnrollmentPollTimeout = 30 * time.Minute
	}
	if d.Evaluate == nil {
		d.Evaluate = policy.Evaluate
	}
	if d.MaxSearchConcurrency <= 0 {
		d.MaxSearchConcurrency = 4
	}
	if d.MaxOffListVendors <= 0 {
		d.MaxOffListVendors = 6
	}
	if d.NewID == nil {
		d.NewID = queue.NewID
	}
	log := d.Logger
	if log == nil {
		log = slog.New(slog.NewTextHandler(io.Discard, nil))
	}
	allow := allowlist.Default()
	if d.AllowedMerchants != nil {
		allow = *d.AllowedMerchants
	}
	o := &Impl{d: d, log: log, sem: make(chan struct{}, d.MaxSearchConcurrency), reqs: map[string]*sync.Mutex{}, allow: allow}
	if d.Queue != nil {
		d.Queue.Register(queue.KindParse, o.handleParse)
		d.Queue.Register(queue.KindSearch, o.handleSearch)
		d.Queue.Register(queue.KindRank, o.handleSearch) // re-rank = re-run search+rank for the request
		d.Queue.Register(queue.KindCheckout, o.handleCheckout)
		d.Queue.Register(queue.KindPollCheckout, o.handlePollCheckout)
		d.Queue.Register(queue.KindPollEnrollment, o.handlePollEnrollment)
	}
	return o
}

// CreateRequest inserts the request in status parsing and enqueues the parse job.
func (o *Impl) CreateRequest(ctx context.Context, in CreateRequestInput) (domain.PurchaseRequest, error) {
	in.Utterance = strings.TrimSpace(in.Utterance)
	if in.RequesterID == "" {
		return domain.PurchaseRequest{}, fmt.Errorf("%w: requester is required", domain.ErrValidation)
	}
	var items []agents.RequestedItem
	for _, it := range in.Items {
		it.Description = strings.TrimSpace(it.Description)
		if it.Description == "" {
			continue
		}
		if it.Qty != nil && *it.Qty < 1 {
			return domain.PurchaseRequest{}, fmt.Errorf("%w: quantity must be at least 1", domain.ErrValidation)
		}
		items = append(items, it)
	}
	if in.Utterance == "" && len(items) == 0 {
		return domain.PurchaseRequest{}, fmt.Errorf("%w: utterance or items required", domain.ErrValidation)
	}
	utter := in.Utterance
	if utter == "" {
		parts := make([]string, 0, len(items))
		for _, it := range items {
			if it.Qty != nil {
				parts = append(parts, fmt.Sprintf("%d x %s", *it.Qty, it.Description))
			} else {
				parts = append(parts, it.Description)
			}
		}
		utter = strings.Join(parts, ", ")
	}
	r := &domain.PurchaseRequest{
		RequesterID: in.RequesterID, RawUtterance: utter, Status: domain.StatusParsing, Currency: domain.Currency,
	}
	if err := o.d.Store.Requests().Create(ctx, r); err != nil {
		return domain.PurchaseRequest{}, fmt.Errorf("create request: %w", err)
	}
	o.record(ctx, r.ID, domain.ActorUser, in.RequesterID, audit.RequestCreated, map[string]any{"utterance": utter, "items": len(items)})
	o.publish(events.RequestCreated, r.ID, map[string]any{"request": r})
	if _, err := o.d.Queue.Enqueue(ctx, queue.Job{
		JobID: "parse:" + r.ID, Kind: queue.KindParse, RequestID: r.ID, Payload: mustJSON(parsePayload{Items: items}),
	}); err != nil {
		_ = o.failRequest(ctx, r.ID, "could not schedule parsing: "+err.Error())
		return *r, fmt.Errorf("enqueue parse: %w", err)
	}
	return *r, nil
}

// GetRequest returns the request aggregate.
//
// For a failed or cancelled request the Reap approval links are blanked: Reap has no
// checkout-cancel API, so a still-open checkout could otherwise be approved (and charged) for an
// order Jarvis no longer tracks as live.
func (o *Impl) GetRequest(ctx context.Context, id string) (domain.RequestDetail, error) {
	d, err := o.d.Store.Requests().Detail(ctx, id)
	if err != nil {
		return d, err
	}
	if requestClosed(d.Request.Status) {
		for i := range d.Payments {
			d.Payments[i].ApprovalURL = ""
		}
	}
	return d, nil
}

// requestClosed: the request ended without (fully) ordering, so no new payment may be started.
func requestClosed(s domain.RequestStatus) bool {
	return s == domain.StatusFailed || s == domain.StatusCancelled || s == domain.StatusRejected
}

// ListRequests lists requests.
func (o *Impl) ListRequests(ctx context.Context, f store.RequestFilter) ([]domain.PurchaseRequest, error) {
	return o.d.Store.Requests().List(ctx, f)
}

// Cancel moves the request to cancelled if the state machine allows it (not from paying).
func (o *Impl) Cancel(ctx context.Context, id, userID string) (domain.RequestDetail, error) {
	unlock := o.lock(id)
	defer unlock()
	r, err := o.d.Store.Requests().Get(ctx, id)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	if err := domain.CheckTransition(r.Status, domain.StatusCancelled); err != nil {
		return domain.RequestDetail{}, err
	}
	// Reap cannot cancel a checkout, so once one exists the request can no longer be cancelled.
	ps, err := o.d.Store.Payments().ListByRequest(ctx, id)
	if err != nil {
		return domain.RequestDetail{}, err
	}
	for _, p := range ps {
		if p.ReapCheckoutID != "" && p.Status != domain.PaymentFailed && p.Status != domain.PaymentExpired {
			return domain.RequestDetail{}, fmt.Errorf("%w: a Reap payment link is already open for %s; it cannot be cancelled",
				domain.ErrInvalidTransition, p.MerchantName)
		}
	}
	if err := o.transition(ctx, id, r.Status, domain.StatusCancelled, "", domain.ActorUser, userID); err != nil {
		return domain.RequestDetail{}, err
	}
	o.record(ctx, id, domain.ActorUser, userID, audit.RequestCancelled, map[string]any{"from": r.Status})
	return o.GetRequest(ctx, id)
}

// Resume re-enqueues work for non-terminal requests (call on boot after the runner starts).
func (o *Impl) Resume(ctx context.Context) error {
	rs, err := o.d.Store.Requests().List(ctx, store.RequestFilter{Limit: 1000, Statuses: []domain.RequestStatus{
		domain.StatusParsing, domain.StatusSearching, domain.StatusApproved, domain.StatusCheckingOut,
		domain.StatusAwaitingPayment, domain.StatusPaying,
	}})
	if err != nil {
		return fmt.Errorf("resume: list requests: %w", err)
	}
	var errs []error
	polled := map[string]bool{}
	for _, r := range rs {
		var job queue.Job
		switch r.Status {
		case domain.StatusParsing:
			job = queue.Job{JobID: "parse:" + r.ID, Kind: queue.KindParse, Payload: mustJSON(parsePayload{})}
		case domain.StatusSearching:
			job = queue.Job{JobID: "search:" + r.ID, Kind: queue.KindSearch}
		case domain.StatusApproved, domain.StatusCheckingOut:
			job = queue.Job{JobID: fmt.Sprintf("checkout:%s:resume:%d", r.ID, o.d.Clock().UnixNano()), Kind: queue.KindCheckout}
		default:
			job = o.pollJob(r.ID)
			polled[r.ID] = true
		}
		job.RequestID = r.ID
		if _, err := o.d.Queue.Enqueue(ctx, job); err != nil {
			errs = append(errs, fmt.Errorf("resume %s: %w", r.ID, err))
		}
	}
	// Open Reap checkouts whose request already ended (failed/cancelled) can still be paid on
	// Reap; keep polling them so a late charge is recorded and counted.
	inflight, err := o.d.Store.Payments().ListInFlight(ctx)
	if err != nil {
		errs = append(errs, fmt.Errorf("resume: list in-flight payments: %w", err))
	}
	for _, p := range inflight {
		if p.ReapCheckoutID == "" || polled[p.RequestID] {
			continue
		}
		polled[p.RequestID] = true
		if _, err := o.d.Queue.Enqueue(ctx, o.pollJob(p.RequestID)); err != nil {
			errs = append(errs, fmt.Errorf("resume poll %s: %w", p.RequestID, err))
		}
	}
	return errors.Join(errs...)
}

// ---------- shared helpers ----------

// transition moves a request between statuses (compare-and-set), audits and publishes it.
func (o *Impl) transition(ctx context.Context, id string, from, to domain.RequestStatus, reason string, actor domain.ActorType, actorID string) error {
	if err := o.d.Store.Requests().TransitionStatus(ctx, id, from, to, reason); err != nil {
		return fmt.Errorf("transition %s -> %s: %w", from, to, err)
	}
	payload := map[string]any{"request_id": id, "from": from, "to": to}
	if reason != "" {
		payload["failure_reason"] = reason
	}
	o.record(ctx, id, actor, actorID, audit.RequestStatusChanged, payload)
	o.publish(events.RequestStatusChanged, id, payload)
	return nil
}

// failRequest moves a non-terminal request to failed. Already-terminal requests are left alone.
func (o *Impl) failRequest(ctx context.Context, id, reason string) error {
	for i := 0; i < 3; i++ {
		r, err := o.d.Store.Requests().Get(ctx, id)
		if err != nil {
			return err
		}
		if r.Status.Terminal() || !domain.CanTransition(r.Status, domain.StatusFailed) {
			return nil
		}
		err = o.transition(ctx, id, r.Status, domain.StatusFailed, reason, domain.ActorSystem, "")
		if err == nil || !errors.Is(err, domain.ErrConflict) {
			return err
		}
	}
	return fmt.Errorf("fail request %s: %w", id, domain.ErrConflict)
}

func (o *Impl) record(ctx context.Context, requestID string, actor domain.ActorType, actorID, typ string, payload any) {
	if o.d.Audit == nil {
		return
	}
	if err := o.d.Audit.Record(ctx, requestID, actor, actorID, typ, payload); err != nil {
		o.log.Warn("audit record failed", "type", typ, "request_id", requestID, "err", err)
	}
}

func (o *Impl) publish(t events.Type, requestID string, data any) {
	if o.d.Bus != nil {
		o.d.Bus.Publish(t, requestID, data)
	}
}

// lock serialises checkout/refresh/cancel work on one request within this process.
func (o *Impl) lock(requestID string) func() {
	o.mu.Lock()
	m, ok := o.reqs[requestID]
	if !ok {
		m = &sync.Mutex{}
		o.reqs[requestID] = m
	}
	o.mu.Unlock()
	m.Lock()
	return m.Unlock
}
