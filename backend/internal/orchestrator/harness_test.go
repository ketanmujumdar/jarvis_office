package orchestrator

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap/fakereap"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

const seedDir = "../../seed"

// testEnv wires the real orchestrator to offline fakes: memstore (seeded from backend/seed),
// fakereap (httptest) through the real reap.HTTPClient, fakellm through the real parser, the real
// in-process queue, approvals, audit and events.
type testEnv struct {
	t        *testing.T
	ctx      context.Context
	st       store.Store
	reapSrv  *fakereap.Server
	reap     reap.Client
	q        *queue.InProcess
	bus      *events.Memory
	appr     *approvals.Impl
	o        *Impl
	adapter  agents.VendorAdapter
	manager  domain.User
	approver domain.User
	addr     domain.Address

	evMu   sync.Mutex
	events []events.Event
}

type envOpt func(*envConfig)

type envConfig struct {
	products       []fakereap.FixtureProduct
	adapter        agents.VendorAdapter
	searchDeadline time.Duration
	noEnrollment   bool
	parser         agents.Parser
	store          func(t *testing.T) store.Store
	matcher        OfferMatcher
	reapOpts       func(*fakereap.Options)
}

func withReapOptions(f func(*fakereap.Options)) envOpt { return func(c *envConfig) { c.reapOpts = f } }

func withStore(f func(t *testing.T) store.Store) envOpt { return func(c *envConfig) { c.store = f } }

func withProducts(p ...fakereap.FixtureProduct) envOpt {
	return func(c *envConfig) { c.products = append(fakereap.DefaultProducts(), p...) }
}
func withAdapter(a agents.VendorAdapter) envOpt { return func(c *envConfig) { c.adapter = a } }
func withDeadline(d time.Duration) envOpt       { return func(c *envConfig) { c.searchDeadline = d } }
func withoutEnrollment() envOpt                 { return func(c *envConfig) { c.noEnrollment = true } }
func withParser(p agents.Parser) envOpt         { return func(c *envConfig) { c.parser = p } }
func withMatcher(m OfferMatcher) envOpt         { return func(c *envConfig) { c.matcher = m } }

func newEnv(t *testing.T, opts ...envOpt) *testEnv {
	t.Helper()
	cfg := envConfig{searchDeadline: 5 * time.Second}
	for _, o := range opts {
		o(&cfg)
	}
	ctx := context.Background()
	var st store.Store = memstore.New()
	if cfg.store != nil {
		st = cfg.store(t)
	}
	if err := seed.Load(ctx, st, seedDir, seed.Options{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	ro := fakereap.Options{Products: cfg.products}
	if cfg.reapOpts != nil {
		cfg.reapOpts(&ro)
	}
	rs := fakereap.New(ro)
	t.Cleanup(rs.Close)
	rc := rs.Client()
	discard := slog.New(slog.NewTextHandler(io.Discard, nil))
	q := queue.NewInProcess(queue.Options{Workers: 4, Backoff: time.Millisecond, Logger: discard})
	bus := events.NewMemory(4096)
	appr := approvals.New(approvals.Deps{Store: st, Events: bus})
	adapter := cfg.adapter
	if adapter == nil {
		adapter = agents.NewReapAdapter(rc)
	}
	parser := cfg.parser
	if parser == nil {
		parser = agents.NewLLMParser(fakellm.New())
	}
	o := New(Deps{
		Store: st, Queue: q, Bus: bus, Audit: audit.New(st), Approvals: appr, Reap: rc,
		Parser: parser, Adapter: adapter, Clock: time.Now,
		SearchDeadline: cfg.searchDeadline, ResultsPerVendor: 5,
		CheckoutPollInterval: time.Hour, CheckoutPollTimeout: time.Minute,
		EnrollmentPollInterval: time.Hour,
		ReapReturnURL:          "https://example.com/jarvis/reap-return", Logger: discard,
		Matcher: cfg.matcher,
	})
	appr.SetHandler(o)
	if err := q.Start(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = q.Stop(sctx)
	})

	e := &testEnv{t: t, ctx: ctx, st: st, reapSrv: rs, reap: rc, q: q, bus: bus, appr: appr, o: o, adapter: adapter}
	ch, cancel := bus.Subscribe("")
	t.Cleanup(cancel)
	go func() {
		for ev := range ch {
			e.evMu.Lock()
			e.events = append(e.events, ev)
			e.evMu.Unlock()
		}
	}()

	users, err := st.Users().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range users {
		switch u.Role {
		case domain.RoleManager:
			e.manager = u
		case domain.RoleApprover:
			e.approver = u
		}
	}
	if e.addr, err = st.Addresses().GetDefault(ctx); err != nil {
		t.Fatal(err)
	}
	if !cfg.noEnrollment {
		e.enroll()
	}
	return e
}

// enroll starts a card enrollment and activates it on the fake hosted page.
func (e *testEnv) enroll() domain.Enrollment {
	e.t.Helper()
	en, err := e.o.StartEnrollment(e.ctx, e.manager)
	if err != nil {
		e.t.Fatalf("start enrollment: %v", err)
	}
	if en.Status != domain.EnrollmentRequiresAction || en.NextActionURL == "" {
		e.t.Fatalf("enrollment = %+v", en)
	}
	if err := e.reapSrv.ActivateEnrollment(en.ReapEnrollmentID); err != nil {
		e.t.Fatal(err)
	}
	e.idle()
	cur, err := e.o.CurrentEnrollment(e.ctx)
	if err != nil || cur.Status != domain.EnrollmentActive {
		e.t.Fatalf("current enrollment = %+v, %v", cur, err)
	}
	return cur
}

func (e *testEnv) idle() {
	e.t.Helper()
	ctx, cancel := context.WithTimeout(e.ctx, 15*time.Second)
	defer cancel()
	if err := e.q.WaitIdle(ctx); err != nil {
		e.t.Fatal(err)
	}
}

func (e *testEnv) create(utterance string, items ...agents.RequestedItem) domain.PurchaseRequest {
	e.t.Helper()
	r, err := e.o.CreateRequest(e.ctx, CreateRequestInput{RequesterID: e.manager.ID, Utterance: utterance, Items: items})
	if err != nil {
		e.t.Fatalf("create request: %v", err)
	}
	e.idle()
	return r
}

func (e *testEnv) detail(id string) domain.RequestDetail {
	e.t.Helper()
	d, err := e.o.GetRequest(e.ctx, id)
	if err != nil {
		e.t.Fatal(err)
	}
	return d
}

func (e *testEnv) wantStatus(id string, want domain.RequestStatus) domain.RequestDetail {
	e.t.Helper()
	d := e.detail(id)
	if d.Request.Status != want {
		e.t.Fatalf("status = %s (failure %q), want %s", d.Request.Status, d.Request.FailureReason, want)
	}
	return d
}

func (e *testEnv) eventTypes(requestID string) []events.Type {
	e.evMu.Lock()
	defer e.evMu.Unlock()
	var out []events.Type
	for _, ev := range e.events {
		if ev.RequestID == requestID {
			out = append(out, ev.Type)
		}
	}
	return out
}

func (e *testEnv) auditTypes(requestID string) map[string]int {
	e.t.Helper()
	evs, err := e.st.Audit().ListByRequest(e.ctx, requestID)
	if err != nil {
		e.t.Fatal(err)
	}
	m := map[string]int{}
	for _, ev := range evs {
		m[ev.Type]++
	}
	return m
}

// statusPath returns the sequence of request.status_changed "to" values from the audit trail.
func (e *testEnv) statusPath(requestID string) []domain.RequestStatus {
	e.t.Helper()
	evs, err := e.st.Audit().ListByRequest(e.ctx, requestID)
	if err != nil {
		e.t.Fatal(err)
	}
	var out []domain.RequestStatus
	seen := map[string]bool{}
	for _, ev := range evs {
		if ev.Type != audit.RequestStatusChanged {
			continue
		}
		var p struct {
			From domain.RequestStatus `json:"from"`
			To   domain.RequestStatus `json:"to"`
		}
		_ = jsonUnmarshal(ev.Payload, &p)
		key := string(p.From) + ">" + string(p.To)
		if seen[key] {
			continue // approvals and orchestrator may both record the same transition
		}
		seen[key] = true
		out = append(out, p.To)
	}
	return out
}

func (e *testEnv) approveCheckouts(d domain.RequestDetail) {
	e.t.Helper()
	for _, p := range d.Payments {
		if p.ReapCheckoutID == "" {
			continue
		}
		if err := e.reapSrv.ApproveCheckout(p.ReapCheckoutID); err != nil {
			e.t.Fatal(err)
		}
	}
}

func (e *testEnv) pendingApproval(requestID string) domain.Approval {
	e.t.Helper()
	as, err := e.st.Approvals().ListByRequest(e.ctx, requestID)
	if err != nil {
		e.t.Fatal(err)
	}
	for _, a := range as {
		if a.Status == domain.ApprovalPending {
			return a
		}
	}
	e.t.Fatalf("no pending approval for %s (have %d)", requestID, len(as))
	return domain.Approval{}
}

func offerByID(d domain.RequestDetail, id *string) domain.Offer {
	if id == nil {
		return domain.Offer{}
	}
	for _, o := range d.Offers {
		if o.ID == *id {
			return o
		}
	}
	return domain.Offer{}
}

func vendorsAllowed(t *testing.T, st store.Store) []domain.Vendor {
	t.Helper()
	vs, err := st.Vendors().List(context.Background(), store.VendorFilter{AllowedOnly: true})
	if err != nil {
		t.Fatal(err)
	}
	return vs
}

func jsonUnmarshal(b []byte, v any) error { return json.Unmarshal(b, v) }

// refresh simulates the poll tick (the poll job is scheduled an hour out in tests).
func (e *testEnv) refresh(id string) domain.RequestDetail {
	e.t.Helper()
	d, err := e.o.RefreshPayments(e.ctx, id)
	if err != nil {
		e.t.Fatalf("refresh: %v", err)
	}
	return d
}
