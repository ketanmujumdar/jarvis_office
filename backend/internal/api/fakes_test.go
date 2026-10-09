package api

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/orchestrator"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// ---------- fake store (only what the api package uses; the rest panics via nil embeds) ----------

type fakeStore struct {
	store.Store
	mu        sync.Mutex
	seq       int
	users     map[string]domain.User
	vendors   map[string]domain.Vendor
	catalog   map[string]domain.CatalogItem
	policy    *domain.PolicyConfig
	addresses map[string]domain.Address
	prompts   map[string]domain.SystemPrompt
	requests  map[string]domain.RequestDetail
	audit     []domain.AuditEvent
	chat      []domain.ChatMessage
	payments  map[string]domain.Payment // by checkout id
	enrolls   map[string]domain.Enrollment
	spend     []domain.MonthSpend
	mtd       domain.Cents
	pingErr   error
}

func newFakeStore() *fakeStore {
	return &fakeStore{
		users: map[string]domain.User{}, vendors: map[string]domain.Vendor{}, catalog: map[string]domain.CatalogItem{},
		addresses: map[string]domain.Address{}, prompts: map[string]domain.SystemPrompt{},
		requests: map[string]domain.RequestDetail{}, payments: map[string]domain.Payment{}, enrolls: map[string]domain.Enrollment{},
	}
}

func (s *fakeStore) newID() string {
	s.seq++
	return fmt.Sprintf("00000000-0000-4000-8000-%012d", s.seq)
}

func (s *fakeStore) Ping(ctx context.Context) error { return s.pingErr }

func (s *fakeStore) Users() store.UserRepo                 { return fUsers{s: s} }
func (s *fakeStore) Vendors() store.VendorRepo             { return fVendors{s: s} }
func (s *fakeStore) Catalog() store.CatalogRepo            { return fCatalog{s: s} }
func (s *fakeStore) Policy() store.PolicyRepo              { return fPolicy{s: s} }
func (s *fakeStore) Addresses() store.AddressRepo          { return fAddresses{s: s} }
func (s *fakeStore) SystemPrompts() store.SystemPromptRepo { return fPrompts{s: s} }
func (s *fakeStore) Requests() store.RequestRepo           { return fRequests{s: s} }
func (s *fakeStore) Audit() store.AuditRepo                { return fAudit{s: s} }
func (s *fakeStore) Chat() store.ChatRepo                  { return fChat{s: s} }
func (s *fakeStore) Spend() store.SpendRepo                { return fSpend{s: s} }
func (s *fakeStore) Payments() store.PaymentRepo           { return fPayments{s: s} }
func (s *fakeStore) Enrollments() store.EnrollmentRepo     { return fEnrolls{s: s} }

func notFound(what, id string) error { return fmt.Errorf("%s %s: %w", what, id, domain.ErrNotFound) }

type fUsers struct {
	store.UserRepo
	s *fakeStore
}

func (r fUsers) Get(_ context.Context, id string) (domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	u, ok := r.s.users[id]
	if !ok {
		return u, notFound("user", id)
	}
	return u, nil
}
func (r fUsers) GetByEmail(_ context.Context, email string) (domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, u := range r.s.users {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return domain.User{}, notFound("user", email)
}
func (r fUsers) List(context.Context) ([]domain.User, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.User{}
	for _, u := range r.s.users {
		out = append(out, u)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Email < out[j].Email })
	return out, nil
}
func (r fUsers) Upsert(_ context.Context, u *domain.User) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for id, x := range r.s.users {
		if strings.EqualFold(x.Email, u.Email) {
			u.ID = id
			r.s.users[id] = *u
			return nil
		}
	}
	u.ID = r.s.newID()
	r.s.users[u.ID] = *u
	return nil
}

type fVendors struct {
	store.VendorRepo
	s *fakeStore
}

func (r fVendors) List(context.Context, store.VendorFilter) ([]domain.Vendor, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.Vendor{}
	for _, v := range r.s.vendors {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Domain < out[j].Domain })
	return out, nil
}
func (r fVendors) Get(_ context.Context, id string) (domain.Vendor, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	v, ok := r.s.vendors[id]
	if !ok {
		return v, notFound("vendor", id)
	}
	return v, nil
}
func (r fVendors) Create(_ context.Context, v *domain.Vendor) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, x := range r.s.vendors {
		if x.Domain == v.Domain {
			return fmt.Errorf("vendor domain: %w", domain.ErrConflict)
		}
	}
	v.ID = r.s.newID()
	r.s.vendors[v.ID] = *v
	return nil
}
func (r fVendors) Update(_ context.Context, v *domain.Vendor) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.vendors[v.ID]; !ok {
		return notFound("vendor", v.ID)
	}
	r.s.vendors[v.ID] = *v
	return nil
}
func (r fVendors) Delete(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.vendors[id]; !ok {
		return notFound("vendor", id)
	}
	delete(r.s.vendors, id)
	return nil
}

type fCatalog struct {
	store.CatalogRepo
	s *fakeStore
}

func (r fCatalog) List(_ context.Context, f store.CatalogFilter) ([]domain.CatalogItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.CatalogItem{}
	for _, it := range r.s.catalog {
		if f.ActiveOnly && !it.Active {
			continue
		}
		if f.Category != "" && it.Category != f.Category {
			continue
		}
		if f.Query != "" && !strings.Contains(strings.ToLower(it.Name), strings.ToLower(f.Query)) {
			continue
		}
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SKU < out[j].SKU })
	return out, nil
}
func (r fCatalog) Get(_ context.Context, id string) (domain.CatalogItem, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	it, ok := r.s.catalog[id]
	if !ok {
		return it, notFound("catalog item", id)
	}
	return it, nil
}
func (r fCatalog) Create(_ context.Context, it *domain.CatalogItem) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	for _, x := range r.s.catalog {
		if x.SKU == it.SKU {
			return fmt.Errorf("sku: %w", domain.ErrConflict)
		}
	}
	it.ID = r.s.newID()
	r.s.catalog[it.ID] = *it
	return nil
}
func (r fCatalog) Update(_ context.Context, it *domain.CatalogItem) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.catalog[it.ID]; !ok {
		return notFound("catalog item", it.ID)
	}
	r.s.catalog[it.ID] = *it
	return nil
}
func (r fCatalog) Delete(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.catalog[id]; !ok {
		return notFound("catalog item", id)
	}
	delete(r.s.catalog, id)
	return nil
}

type fPolicy struct {
	store.PolicyRepo
	s *fakeStore
}

func (r fPolicy) Get(context.Context) (domain.PolicyConfig, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if r.s.policy == nil {
		return domain.PolicyConfig{}, notFound("policy", "")
	}
	return *r.s.policy, nil
}
func (r fPolicy) Update(_ context.Context, p *domain.PolicyConfig, by string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	cp := *p
	cp.UpdatedBy = by
	cp.UpdatedAt = time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	r.s.policy = &cp
	return nil
}

type fAddresses struct {
	store.AddressRepo
	s *fakeStore
}

func (r fAddresses) List(context.Context) ([]domain.Address, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.Address{}
	for _, a := range r.s.addresses {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].IsDefault != out[j].IsDefault {
			return out[i].IsDefault
		}
		return out[i].Label < out[j].Label
	})
	return out, nil
}
func (r fAddresses) Get(_ context.Context, id string) (domain.Address, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a, ok := r.s.addresses[id]
	if !ok {
		return a, notFound("address", id)
	}
	return a, nil
}
func (r fAddresses) Create(_ context.Context, a *domain.Address) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	a.ID = r.s.newID()
	r.s.addresses[a.ID] = *a
	return nil
}
func (r fAddresses) Update(_ context.Context, a *domain.Address) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.addresses[a.ID]; !ok {
		return notFound("address", a.ID)
	}
	r.s.addresses[a.ID] = *a
	return nil
}
func (r fAddresses) Delete(_ context.Context, id string) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	if _, ok := r.s.addresses[id]; !ok {
		return notFound("address", id)
	}
	for _, d := range r.s.requests {
		if d.Request.AddressID != nil && *d.Request.AddressID == id {
			return fmt.Errorf("address in use: %w", domain.ErrConflict)
		}
	}
	delete(r.s.addresses, id)
	return nil
}

type fPrompts struct {
	store.SystemPromptRepo
	s *fakeStore
}

func (r fPrompts) Get(_ context.Context, key string) (domain.SystemPrompt, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	p, ok := r.s.prompts[key]
	if !ok {
		return p, notFound("prompt", key)
	}
	return p, nil
}
func (r fPrompts) Update(_ context.Context, key, content, by string) (domain.SystemPrompt, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	p := r.s.prompts[key]
	p.Key, p.Content, p.UpdatedBy = key, content, by
	p.Version++
	r.s.prompts[key] = p
	return p, nil
}

type fRequests struct {
	store.RequestRepo
	s *fakeStore
}

func (r fRequests) Get(_ context.Context, id string) (domain.PurchaseRequest, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	d, ok := r.s.requests[id]
	if !ok {
		return domain.PurchaseRequest{}, notFound("request", id)
	}
	return d.Request, nil
}
func (r fRequests) Detail(_ context.Context, id string) (domain.RequestDetail, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	d, ok := r.s.requests[id]
	if !ok {
		return d, notFound("request", id)
	}
	return d, nil
}
func (r fRequests) List(_ context.Context, f store.RequestFilter) ([]domain.PurchaseRequest, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.PurchaseRequest{}
	for _, d := range r.s.requests {
		if len(f.Statuses) > 0 {
			ok := false
			for _, st := range f.Statuses {
				ok = ok || st == d.Request.Status
			}
			if !ok {
				continue
			}
		}
		out = append(out, d.Request)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

type fAudit struct {
	store.AuditRepo
	s *fakeStore
}

func (r fAudit) Append(_ context.Context, e *domain.AuditEvent) error {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	e.ID = int64(len(r.s.audit) + 1)
	r.s.audit = append(r.s.audit, *e)
	return nil
}
func (r fAudit) ListByRequest(_ context.Context, id string) ([]domain.AuditEvent, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	var out []domain.AuditEvent
	for _, e := range r.s.audit {
		if e.RequestID != nil && *e.RequestID == id {
			out = append(out, e)
		}
	}
	return out, nil
}
func (r fAudit) List(_ context.Context, f store.AuditFilter) ([]domain.AuditEvent, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	out := []domain.AuditEvent{}
	for i := len(r.s.audit) - 1; i >= 0; i-- {
		e := r.s.audit[i]
		if f.Type != "" {
			if strings.HasSuffix(f.Type, ".") {
				if !strings.HasPrefix(e.Type, f.Type) {
					continue
				}
			} else if e.Type != f.Type {
				continue
			}
		}
		out = append(out, e)
	}
	return out, nil
}

type fChat struct {
	store.ChatRepo
	s *fakeStore
}

func (r fChat) ListBySession(_ context.Context, id string, limit int) ([]domain.ChatMessage, error) {
	var out []domain.ChatMessage
	for _, m := range r.s.chat {
		if m.SessionID == id {
			out = append(out, m)
		}
	}
	return out, nil
}

type fSpend struct {
	store.SpendRepo
	s *fakeStore
}

func (r fSpend) MonthToDate(context.Context, time.Time) (domain.Cents, error) { return r.s.mtd, nil }
func (r fSpend) ByMonth(_ context.Context, months int, _ time.Time) ([]domain.MonthSpend, error) {
	if months < len(r.s.spend) {
		return r.s.spend[len(r.s.spend)-months:], nil
	}
	return r.s.spend, nil
}

type fPayments struct {
	store.PaymentRepo
	s *fakeStore
}

func (r fPayments) GetByCheckoutID(_ context.Context, id string) (domain.Payment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	p, ok := r.s.payments[id]
	if !ok {
		return p, notFound("payment", id)
	}
	return p, nil
}

type fEnrolls struct {
	store.EnrollmentRepo
	s *fakeStore
}

func (r fEnrolls) GetByReapID(_ context.Context, id string) (domain.Enrollment, error) {
	r.s.mu.Lock()
	defer r.s.mu.Unlock()
	e, ok := r.s.enrolls[id]
	if !ok {
		return e, notFound("enrollment", id)
	}
	return e, nil
}

// ---------- fake orchestrator ----------

type fakeOrch struct {
	mu         sync.Mutex
	st         *fakeStore
	created    []orchestrator.CreateRequestInput
	confirmed  []orchestrator.ConfirmInput
	refreshed  []string
	enrollment *domain.Enrollment
	err        error // returned by every mutating call when set
}

var _ orchestrator.Service = (*fakeOrch)(nil)

func (o *fakeOrch) CreateRequest(ctx context.Context, in orchestrator.CreateRequestInput) (domain.PurchaseRequest, error) {
	if o.err != nil {
		return domain.PurchaseRequest{}, o.err
	}
	o.mu.Lock()
	o.created = append(o.created, in)
	o.mu.Unlock()
	o.st.mu.Lock()
	defer o.st.mu.Unlock()
	pr := domain.PurchaseRequest{ID: o.st.newID(), RequesterID: in.RequesterID, RawUtterance: in.Utterance,
		Status: domain.StatusParsing, Currency: domain.Currency}
	o.st.requests[pr.ID] = domain.RequestDetail{Request: pr}
	return pr, nil
}
func (o *fakeOrch) GetRequest(ctx context.Context, id string) (domain.RequestDetail, error) {
	return o.st.Requests().Detail(ctx, id)
}
func (o *fakeOrch) ListRequests(ctx context.Context, f store.RequestFilter) ([]domain.PurchaseRequest, error) {
	rs, err := o.st.Requests().List(ctx, f)
	if f.RequesterID == "" {
		return rs, err
	}
	out := []domain.PurchaseRequest{}
	for _, r := range rs {
		if r.RequesterID == f.RequesterID {
			out = append(out, r)
		}
	}
	return out, err
}
func (o *fakeOrch) Confirm(ctx context.Context, id string, in orchestrator.ConfirmInput) (domain.RequestDetail, error) {
	if o.err != nil {
		return domain.RequestDetail{}, o.err
	}
	o.mu.Lock()
	o.confirmed = append(o.confirmed, in)
	o.mu.Unlock()
	o.st.mu.Lock()
	defer o.st.mu.Unlock()
	d, ok := o.st.requests[id]
	if !ok {
		return d, notFound("request", id)
	}
	if d.Request.Status != domain.StatusQuoted {
		return d, domain.CheckTransition(d.Request.Status, domain.StatusApproved)
	}
	d.Request.Status = domain.StatusApproved
	d.Request.AddressID = &in.AddressID
	o.st.requests[id] = d
	return d, nil
}
func (o *fakeOrch) Cancel(ctx context.Context, id, userID string) (domain.RequestDetail, error) {
	o.st.mu.Lock()
	defer o.st.mu.Unlock()
	d, ok := o.st.requests[id]
	if !ok {
		return d, notFound("request", id)
	}
	if err := domain.CheckTransition(d.Request.Status, domain.StatusCancelled); err != nil {
		return d, err
	}
	d.Request.Status = domain.StatusCancelled
	o.st.requests[id] = d
	return d, nil
}
func (o *fakeOrch) RefreshPayments(ctx context.Context, id string) (domain.RequestDetail, error) {
	o.mu.Lock()
	o.refreshed = append(o.refreshed, id)
	o.mu.Unlock()
	if o.err != nil {
		return domain.RequestDetail{}, o.err
	}
	return o.st.Requests().Detail(ctx, id)
}
func (o *fakeOrch) StartEnrollment(ctx context.Context, u domain.User) (domain.Enrollment, error) {
	if o.err != nil {
		return domain.Enrollment{}, o.err
	}
	e := domain.Enrollment{ID: "00000000-0000-4000-8000-0000000e0001", ReapEnrollmentID: "enr_1",
		Status: domain.EnrollmentRequiresAction, OwnerRef: u.ID, OwnerEmail: u.Email,
		NextActionURL: "https://sandbox.reap.example/enroll/enr_1", CreatedBy: u.ID}
	o.mu.Lock()
	o.enrollment = &e
	o.mu.Unlock()
	return e, nil
}
func (o *fakeOrch) CurrentEnrollment(ctx context.Context) (domain.Enrollment, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.enrollment == nil {
		return domain.Enrollment{}, notFound("enrollment", "latest")
	}
	return *o.enrollment, nil
}
func (o *fakeOrch) OnApprovalDecided(ctx context.Context, a domain.Approval) error { return nil }
func (o *fakeOrch) Resume(ctx context.Context) error                               { return nil }

// ---------- fake approvals ----------

type fakeApprovals struct {
	mu      sync.Mutex
	items   map[string]domain.Approval
	decided []string // "approve:<id>:<user>:<comment>"
}

var _ approvals.Service = (*fakeApprovals)(nil)

func (f *fakeApprovals) Create(ctx context.Context, in approvals.CreateInput) (domain.Approval, error) {
	return domain.Approval{}, domain.ErrNotImplemented
}
func (f *fakeApprovals) view(a domain.Approval) approvals.ApprovalView {
	return approvals.ApprovalView{Approval: a, Lines: []approvals.LineView{{
		LineItem:  domain.LineItem{ID: "li1"},
		TopOffers: []domain.Offer{{Rank: 1}, {Rank: 2}, {Rank: 3}, {Rank: 4}},
	}}}
}
func (f *fakeApprovals) Get(ctx context.Context, id string) (approvals.ApprovalView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.items[id]
	if !ok {
		return approvals.ApprovalView{}, notFound("approval", id)
	}
	return f.view(a), nil
}
func (f *fakeApprovals) List(ctx context.Context, flt store.ApprovalFilter) ([]approvals.ApprovalView, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []approvals.ApprovalView
	for _, a := range f.items {
		if flt.Status == "" || a.Status == flt.Status {
			out = append(out, f.view(a))
		}
	}
	return out, nil
}
func (f *fakeApprovals) decide(id string, u domain.User, comment string, st domain.ApprovalStatus) (domain.Approval, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	a, ok := f.items[id]
	if !ok {
		return a, notFound("approval", id)
	}
	if a.Status != domain.ApprovalPending {
		return a, fmt.Errorf("already decided: %w", domain.ErrConflict)
	}
	a.Status, a.Comment, a.ApproverID = st, comment, &u.ID
	f.items[id] = a
	f.decided = append(f.decided, fmt.Sprintf("%s:%s:%s:%s", st, id, u.ID, comment))
	return a, nil
}
func (f *fakeApprovals) Approve(ctx context.Context, id string, u domain.User, c string) (domain.Approval, error) {
	return f.decide(id, u, c, domain.ApprovalApproved)
}
func (f *fakeApprovals) Reject(ctx context.Context, id string, u domain.User, c string) (domain.Approval, error) {
	return f.decide(id, u, c, domain.ApprovalRejected)
}

// ---------- fake agent, tools and minter ----------

type fakeAgent struct {
	inputs []agents.ChatInput
	err    error
}

func (a *fakeAgent) Chat(ctx context.Context, in agents.ChatInput) (agents.ChatOutput, error) {
	if a.err != nil {
		return agents.ChatOutput{}, a.err
	}
	a.inputs = append(a.inputs, in)
	sid := in.SessionID
	if sid == "" {
		sid = "sess-1"
	}
	return agents.ChatOutput{SessionID: sid, Reply: "Which delivery address should I use?",
		ToolCalls: []agents.ToolCallTrace{{Name: agents.ToolListAddresses}}}, nil
}
func (a *fakeAgent) History(ctx context.Context, id string) ([]domain.ChatMessage, error) {
	if id != "sess-1" {
		return nil, nil
	}
	return []domain.ChatMessage{{ID: 1, SessionID: id, Role: domain.ChatRoleUser, Content: "hi"}}, nil
}

type toolCall struct {
	tc   agents.ToolContext
	name string
	args string
}

type fakeTools struct {
	calls  []toolCall
	result json.RawMessage
	err    error
}

func (t *fakeTools) Execute(ctx context.Context, tc agents.ToolContext, name string, args json.RawMessage) (json.RawMessage, error) {
	t.calls = append(t.calls, toolCall{tc: tc, name: name, args: string(args)})
	if t.err != nil {
		return nil, t.err
	}
	if t.result != nil {
		return t.result, nil
	}
	return json.RawMessage(`{"ok":true}`), nil
}
func (t *fakeTools) Definitions() []llm.Tool {
	return []llm.Tool{{Name: agents.ToolListAddresses, Description: "List delivery addresses",
		Parameters: json.RawMessage(`{"type":"object","properties":{}}`)}}
}

type fakeMinter struct {
	got realtime.SessionParams
	err error
}

func (m *fakeMinter) Mint(ctx context.Context, p realtime.SessionParams) (realtime.Session, error) {
	m.got = p
	if m.err != nil {
		return realtime.Session{}, m.err
	}
	return realtime.Session{ClientSecret: "ek_test", ExpiresAt: time.Date(2026, 10, 9, 1, 0, 0, 0, time.UTC),
		Model: "gpt-realtime", Voice: p.Voice, CallsURL: "https://api.openai.com/v1/realtime/calls"}, nil
}
