package approvals

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// fakeStore is a minimal in-memory store.Store covering only the repos approvals uses. WithTx
// snapshots state and restores it on error, so rollback behaviour is testable. Unused repos are
// left nil (embedded interface) and panic if called.
type fakeStore struct {
	store.Store // nil; unused repos panic

	mu     *sync.Mutex
	st     *fakeState
	inTx   bool
	failOn map[string]error // "op" -> error injected (e.g. "Requests.TransitionStatus")
	seq    *int
}

type fakeState struct {
	users     map[string]domain.User
	catalog   map[string]domain.CatalogItem
	requests  map[string]domain.PurchaseRequest
	lines     []domain.LineItem
	offers    []domain.Offer
	approvals []domain.Approval
	audit     []domain.AuditEvent
}

func (s *fakeState) clone() *fakeState {
	c := &fakeState{
		users: map[string]domain.User{}, catalog: map[string]domain.CatalogItem{}, requests: map[string]domain.PurchaseRequest{},
		lines: append([]domain.LineItem{}, s.lines...), offers: append([]domain.Offer{}, s.offers...),
		approvals: append([]domain.Approval{}, s.approvals...), audit: append([]domain.AuditEvent{}, s.audit...),
	}
	for k, v := range s.users {
		c.users[k] = v
	}
	for k, v := range s.catalog {
		c.catalog[k] = v
	}
	for k, v := range s.requests {
		c.requests[k] = v
	}
	return c
}

func newFakeStore() *fakeStore {
	n := 0
	return &fakeStore{
		mu: &sync.Mutex{}, seq: &n, failOn: map[string]error{},
		st: &fakeState{users: map[string]domain.User{}, catalog: map[string]domain.CatalogItem{}, requests: map[string]domain.PurchaseRequest{}},
	}
}

func (f *fakeStore) id(prefix string) string { *f.seq++; return fmt.Sprintf("%s-%d", prefix, *f.seq) }

func (f *fakeStore) fail(op string) error {
	if err, ok := f.failOn[op]; ok {
		return err
	}
	return nil
}

func (f *fakeStore) lock() func() {
	if f.inTx {
		return func() {}
	}
	f.mu.Lock()
	return f.mu.Unlock
}

func (f *fakeStore) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	if f.inTx {
		return fn(f)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	snap := f.st.clone()
	tx := *f
	tx.inTx = true
	if err := fn(&tx); err != nil {
		*f.st = *snap
		return err
	}
	return nil
}

func (f *fakeStore) Users() store.UserRepo         { return fakeUsers{f: f} }
func (f *fakeStore) Catalog() store.CatalogRepo    { return fakeCatalog{f: f} }
func (f *fakeStore) Requests() store.RequestRepo   { return fakeRequests{f: f} }
func (f *fakeStore) LineItems() store.LineItemRepo { return fakeLines{f: f} }
func (f *fakeStore) Offers() store.OfferRepo       { return fakeOffers{f: f} }
func (f *fakeStore) Approvals() store.ApprovalRepo { return fakeApprovals{f: f} }
func (f *fakeStore) Audit() store.AuditRepo        { return fakeAudit{f: f} }

// --- users ---
type fakeUsers struct {
	store.UserRepo
	f *fakeStore
}

func (r fakeUsers) Get(ctx context.Context, id string) (domain.User, error) {
	defer r.f.lock()()
	if err := r.f.fail("Users.Get"); err != nil {
		return domain.User{}, err
	}
	u, ok := r.f.st.users[id]
	if !ok {
		return u, domain.ErrNotFound
	}
	return u, nil
}

// --- catalog ---
type fakeCatalog struct {
	store.CatalogRepo
	f *fakeStore
}

func (r fakeCatalog) Get(ctx context.Context, id string) (domain.CatalogItem, error) {
	defer r.f.lock()()
	if err := r.f.fail("Catalog.Get"); err != nil {
		return domain.CatalogItem{}, err
	}
	c, ok := r.f.st.catalog[id]
	if !ok {
		return c, domain.ErrNotFound
	}
	return c, nil
}

// --- requests ---
type fakeRequests struct {
	store.RequestRepo
	f *fakeStore
}

func (r fakeRequests) Get(ctx context.Context, id string) (domain.PurchaseRequest, error) {
	defer r.f.lock()()
	q, ok := r.f.st.requests[id]
	if !ok {
		return q, domain.ErrNotFound
	}
	return q, nil
}

func (r fakeRequests) TransitionStatus(ctx context.Context, id string, from, to domain.RequestStatus, reason string) error {
	defer r.f.lock()()
	if err := r.f.fail("Requests.TransitionStatus"); err != nil {
		return err
	}
	q, ok := r.f.st.requests[id]
	if !ok {
		return domain.ErrNotFound
	}
	if err := domain.CheckTransition(from, to); err != nil {
		return err
	}
	if q.Status != from {
		return domain.ErrConflict
	}
	q.Status = to
	q.FailureReason = reason
	r.f.st.requests[id] = q
	return nil
}

// --- line items ---
type fakeLines struct {
	store.LineItemRepo
	f *fakeStore
}

func (r fakeLines) ListByRequest(ctx context.Context, requestID string) ([]domain.LineItem, error) {
	defer r.f.lock()()
	out := []domain.LineItem{}
	for _, l := range r.f.st.lines {
		if l.RequestID == requestID {
			out = append(out, l)
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Position < out[j].Position })
	return out, nil
}

// --- offers ---
type fakeOffers struct {
	store.OfferRepo
	f *fakeStore
}

func (r fakeOffers) ListByLineItem(ctx context.Context, lineItemID string) ([]domain.Offer, error) {
	defer r.f.lock()()
	if err := r.f.fail("Offers.ListByLineItem"); err != nil {
		return nil, err
	}
	out := []domain.Offer{}
	for _, o := range r.f.st.offers {
		if o.LineItemID == lineItemID {
			out = append(out, o)
		}
	}
	// Deliberately NOT sorted by rank: the service must order them.
	return out, nil
}

// --- approvals ---
type fakeApprovals struct {
	store.ApprovalRepo
	f *fakeStore
}

func (r fakeApprovals) Create(ctx context.Context, a *domain.Approval) error {
	defer r.f.lock()()
	if err := r.f.fail("Approvals.Create"); err != nil {
		return err
	}
	for _, x := range r.f.st.approvals {
		if x.RequestID == a.RequestID && x.Status == domain.ApprovalPending {
			return fmt.Errorf("pending approval exists: %w", domain.ErrConflict)
		}
	}
	a.ID = r.f.id("appr")
	a.CreatedAt = time.Date(2026, 10, 9, 10, 0, *r.f.seq, 0, time.UTC)
	r.f.st.approvals = append(r.f.st.approvals, *a)
	return nil
}

func (r fakeApprovals) Get(ctx context.Context, id string) (domain.Approval, error) {
	defer r.f.lock()()
	for _, a := range r.f.st.approvals {
		if a.ID == id {
			return a, nil
		}
	}
	return domain.Approval{}, domain.ErrNotFound
}

func (r fakeApprovals) List(ctx context.Context, f store.ApprovalFilter) ([]domain.Approval, error) {
	defer r.f.lock()()
	out := []domain.Approval{}
	for i := len(r.f.st.approvals) - 1; i >= 0; i-- { // newest first
		a := r.f.st.approvals[i]
		if f.Status == "" || a.Status == f.Status {
			out = append(out, a)
		}
	}
	return out, nil
}

func (r fakeApprovals) Decide(ctx context.Context, id string, status domain.ApprovalStatus, approverID, comment string) (domain.Approval, error) {
	defer r.f.lock()()
	if err := r.f.fail("Approvals.Decide"); err != nil {
		return domain.Approval{}, err
	}
	for i, a := range r.f.st.approvals {
		if a.ID != id {
			continue
		}
		if a.Status != domain.ApprovalPending {
			return domain.Approval{}, domain.ErrConflict
		}
		now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
		a.Status, a.ApproverID, a.Comment, a.DecidedAt = status, &approverID, comment, &now
		r.f.st.approvals[i] = a
		return a, nil
	}
	return domain.Approval{}, domain.ErrNotFound
}

// --- audit ---
type fakeAudit struct {
	store.AuditRepo
	f *fakeStore
}

func (r fakeAudit) Append(ctx context.Context, e *domain.AuditEvent) error {
	defer r.f.lock()()
	if err := r.f.fail("Audit.Append"); err != nil {
		return err
	}
	e.ID = int64(len(r.f.st.audit) + 1)
	r.f.st.audit = append(r.f.st.audit, *e)
	return nil
}

var errBoom = errors.New("boom")
