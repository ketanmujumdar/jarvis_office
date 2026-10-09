// Package memstore is an in-memory store.Store for unit tests of other packages. Owner: agent A.
//
// It mirrors the Postgres implementation's observable behaviour (ErrNotFound / ErrConflict /
// ErrValidation semantics, unique keys, foreign keys, ordering, the request state machine,
// one pending approval per request, one default address, append-only audit). The shared
// conformance suite in store/storetest runs against both implementations.
//
// Concurrency: every call takes one mutex. WithTx holds the mutex for the whole callback and
// restores a snapshot when the callback returns an error, so a transaction is atomic and isolated.
// Inside WithTx use only the tx-bound Store passed to the callback (calling the outer Store from
// the callback, or from a goroutine the callback waits on, deadlocks).
package memstore

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Store implements store.Store in memory.
type Store struct {
	sh   *shared
	inTx bool
}

type shared struct {
	mu    sync.Mutex
	data  *state
	clock func() time.Time
}

var _ store.Store = (*Store)(nil)

// New returns an empty store that stamps rows with time.Now().
func New() *Store { return NewWithClock(time.Now) }

// NewWithClock returns an empty store that stamps rows with clock().
func NewWithClock(clock func() time.Time) *Store {
	return &Store{sh: &shared{data: newState(), clock: clock}}
}

// SetPaymentCompletedAt overrides when a completed payment counts toward monthly spend
// (test helper; Postgres tests do the same with an UPDATE of payments.completed_at).
func (s *Store) SetPaymentCompletedAt(paymentID string, at time.Time) {
	_ = s.do(func(d *state) error {
		d.completedAt[paymentID] = at.UTC()
		return nil
	})
}

func (s *Store) do(fn func(d *state) error) error {
	if s.inTx {
		return fn(s.sh.data)
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock()
	return fn(s.sh.data)
}

// atomic is do + rollback on error (so multi-step repo methods are all-or-nothing).
func (s *Store) atomic(fn func(d *state) error) error {
	return s.do(func(d *state) error {
		snap := d.clone()
		if err := fn(d); err != nil {
			*d = *snap
			return err
		}
		return nil
	})
}

func (s *Store) now() time.Time { return s.sh.clock().UTC() }

func (s *Store) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	if s.inTx {
		return fn(s)
	}
	s.sh.mu.Lock()
	defer s.sh.mu.Unlock()
	snap := s.sh.data.clone()
	if err := fn(&Store{sh: s.sh, inTx: true}); err != nil {
		*s.sh.data = *snap
		return err
	}
	return nil
}

func (s *Store) Ping(context.Context) error { return nil }

func (s *Store) Users() store.UserRepo                 { return userRepo{s} }
func (s *Store) Vendors() store.VendorRepo             { return vendorRepo{s} }
func (s *Store) Catalog() store.CatalogRepo            { return catalogRepo{s} }
func (s *Store) Policy() store.PolicyRepo              { return policyRepo{s} }
func (s *Store) Addresses() store.AddressRepo          { return addressRepo{s} }
func (s *Store) SystemPrompts() store.SystemPromptRepo { return promptRepo{s} }
func (s *Store) Enrollments() store.EnrollmentRepo     { return enrollmentRepo{s} }
func (s *Store) Requests() store.RequestRepo           { return requestRepo{s} }
func (s *Store) LineItems() store.LineItemRepo         { return lineItemRepo{s} }
func (s *Store) Offers() store.OfferRepo               { return offerRepo{s} }
func (s *Store) Approvals() store.ApprovalRepo         { return approvalRepo{s} }
func (s *Store) Payments() store.PaymentRepo           { return paymentRepo{s} }
func (s *Store) Audit() store.AuditRepo                { return auditRepo{s} }
func (s *Store) Chat() store.ChatRepo                  { return chatRepo{s} }
func (s *Store) Spend() store.SpendRepo                { return spendRepo{s} }

// ---------------- state ----------------

type state struct {
	seq         int64
	order       map[string]int64 // insertion sequence per row id (stable tie-break for ordering)
	users       map[string]domain.User
	vendors     map[string]domain.Vendor
	catalog     map[string]domain.CatalogItem
	policy      *domain.PolicyConfig
	addresses   map[string]domain.Address
	prompts     map[string]domain.SystemPrompt
	enrollments map[string]domain.Enrollment
	requests    map[string]domain.PurchaseRequest
	lineItems   map[string]domain.LineItem
	offers      map[string]domain.Offer
	approvals   map[string]domain.Approval
	payments    map[string]domain.Payment
	completedAt map[string]time.Time
	audit       []domain.AuditEvent
	chat        []domain.ChatMessage
}

func newState() *state {
	return &state{
		order: map[string]int64{}, users: map[string]domain.User{}, vendors: map[string]domain.Vendor{},
		catalog: map[string]domain.CatalogItem{}, addresses: map[string]domain.Address{},
		prompts: map[string]domain.SystemPrompt{}, enrollments: map[string]domain.Enrollment{},
		requests: map[string]domain.PurchaseRequest{}, lineItems: map[string]domain.LineItem{},
		offers: map[string]domain.Offer{}, approvals: map[string]domain.Approval{},
		payments: map[string]domain.Payment{}, completedAt: map[string]time.Time{},
	}
}

func cloneMap[V any](m map[string]V, cp func(V) V) map[string]V {
	out := make(map[string]V, len(m))
	for k, v := range m {
		out[k] = cp(v)
	}
	return out
}

func ident[V any](v V) V { return v }

func (d *state) clone() *state {
	c := &state{
		seq:         d.seq,
		order:       cloneMap(d.order, ident[int64]),
		users:       cloneMap(d.users, ident[domain.User]),
		vendors:     cloneMap(d.vendors, ident[domain.Vendor]),
		catalog:     cloneMap(d.catalog, cpCatalog),
		addresses:   cloneMap(d.addresses, ident[domain.Address]),
		prompts:     cloneMap(d.prompts, ident[domain.SystemPrompt]),
		enrollments: cloneMap(d.enrollments, ident[domain.Enrollment]),
		requests:    cloneMap(d.requests, cpRequest),
		lineItems:   cloneMap(d.lineItems, cpLineItem),
		offers:      cloneMap(d.offers, cpOffer),
		approvals:   cloneMap(d.approvals, cpApproval),
		payments:    cloneMap(d.payments, cpPayment),
		completedAt: cloneMap(d.completedAt, ident[time.Time]),
		audit:       make([]domain.AuditEvent, len(d.audit)),
		chat:        make([]domain.ChatMessage, len(d.chat)),
	}
	if d.policy != nil {
		p := *d.policy
		c.policy = &p
	}
	for i, e := range d.audit {
		c.audit[i] = cpAudit(e)
	}
	for i, m := range d.chat {
		c.chat[i] = cpChat(m)
	}
	return c
}

func (d *state) track(id string) {
	d.seq++
	d.order[id] = d.seq
}

// ---------------- copy helpers ----------------

func cp[T any](p *T) *T {
	if p == nil {
		return nil
	}
	v := *p
	return &v
}

func cpStrings(s []string) []string {
	if s == nil {
		return []string{}
	}
	return slices.Clone(s)
}

func cpReasons(r []domain.Reason) []domain.Reason {
	if r == nil {
		return []domain.Reason{}
	}
	return slices.Clone(r)
}

func cpRaw(r json.RawMessage) json.RawMessage {
	if r == nil {
		return nil
	}
	return slices.Clone(r)
}

func cpCatalog(it domain.CatalogItem) domain.CatalogItem {
	it.Aliases = cpStrings(it.Aliases)
	it.PreferredVendorIDs = cpStrings(it.PreferredVendorIDs)
	return it
}

func cpRequest(r domain.PurchaseRequest) domain.PurchaseRequest {
	r.AddressID, r.ConfirmedBy, r.ConfirmedAt = cp(r.AddressID), cp(r.ConfirmedBy), cp(r.ConfirmedAt)
	return r
}

func cpLineItem(li domain.LineItem) domain.LineItem {
	li.CatalogItemID, li.SelectedOfferID = cp(li.CatalogItemID), cp(li.SelectedOfferID)
	li.Reasons = cpReasons(li.Reasons)
	return li
}

func cpOffer(o domain.Offer) domain.Offer {
	o.VendorID = cp(o.VendorID)
	return o
}

func cpApproval(a domain.Approval) domain.Approval {
	a.Reasons = cpReasons(a.Reasons)
	a.PrevCents, a.ApproverID, a.DecidedAt = cp(a.PrevCents), cp(a.ApproverID), cp(a.DecidedAt)
	return a
}

func cpPayment(p domain.Payment) domain.Payment {
	p.VendorID, p.EnrollmentID, p.FinalCents, p.QuoteExpiresAt = cp(p.VendorID), cp(p.EnrollmentID), cp(p.FinalCents), cp(p.QuoteExpiresAt)
	return p
}

func cpAudit(e domain.AuditEvent) domain.AuditEvent {
	e.RequestID = cp(e.RequestID)
	e.Payload = cpRaw(e.Payload)
	return e
}

func cpChat(m domain.ChatMessage) domain.ChatMessage {
	m.ToolCalls = cpRaw(m.ToolCalls)
	return m
}

// ---------------- misc helpers ----------------

func newID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func notFound(entity, key string) error {
	return fmt.Errorf("%s %q: %w", entity, key, domain.ErrNotFound)
}

func conflict(entity, what string) error {
	return fmt.Errorf("%s: %w (%s)", entity, domain.ErrConflict, what)
}

func invalid(entity, msg string) error {
	return fmt.Errorf("%s: %w: %s", entity, domain.ErrValidation, msg)
}

// sorted returns the map values sorted by less, ties broken by insertion order.
func sorted[V any](d *state, m map[string]V, id func(V) string, less func(a, b V) int) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if c := less(out[i], out[j]); c != 0 {
			return c < 0
		}
		return d.order[id(out[i])] < d.order[id(out[j])]
	})
	return out
}

func page[V any](in []V, limit, offset, def int) []V {
	if limit <= 0 {
		limit = def
	}
	if offset < 0 {
		offset = 0
	}
	if offset >= len(in) {
		return []V{}
	}
	end := min(offset+limit, len(in))
	return in[offset:end]
}

func mapSlice[V any](in []V, f func(V) V) []V {
	out := make([]V, len(in))
	for i, v := range in {
		out[i] = f(v)
	}
	return out
}

// newestFirst sorts by created time descending, ties broken by most recently inserted first.
func newestFirst[V any](d *state, m map[string]V, key func(V) (string, time.Time)) []V {
	out := make([]V, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool {
		ai, at := key(out[i])
		bi, bt := key(out[j])
		if !at.Equal(bt) {
			return at.After(bt)
		}
		return d.order[ai] > d.order[bi]
	})
	return out
}

// oldestFirst is the reverse of newestFirst.
func oldestFirst[V any](d *state, m map[string]V, key func(V) (string, time.Time)) []V {
	out := newestFirst(d, m, key)
	slices.Reverse(out)
	return out
}

// ---------------- users ----------------

type userRepo struct{ s *Store }

func (r userRepo) Get(_ context.Context, id string) (u domain.User, err error) {
	err = r.s.do(func(d *state) error {
		var ok bool
		if u, ok = d.users[id]; !ok {
			return notFound("user", id)
		}
		return nil
	})
	return u, err
}

func (r userRepo) GetByEmail(_ context.Context, email string) (u domain.User, err error) {
	email = strings.ToLower(strings.TrimSpace(email))
	err = r.s.do(func(d *state) error {
		for _, x := range d.users {
			if strings.ToLower(x.Email) == email {
				u = x
				return nil
			}
		}
		return notFound("user", email)
	})
	return u, err
}

func (r userRepo) List(context.Context) (out []domain.User, err error) {
	err = r.s.do(func(d *state) error {
		out = sorted(d, d.users, func(u domain.User) string { return u.ID }, func(a, b domain.User) int {
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Email, b.Email)
		})
		return nil
	})
	return out, err
}

func (r userRepo) Upsert(_ context.Context, u *domain.User) error {
	switch u.Role {
	case domain.RoleManager, domain.RoleApprover, domain.RoleAdmin:
	default:
		return invalid("user", "unknown role "+string(u.Role))
	}
	return r.s.do(func(d *state) error {
		for id, x := range d.users {
			if x.Email == u.Email {
				x.Name, x.Role = u.Name, u.Role
				d.users[id] = x
				*u = x
				return nil
			}
		}
		u.ID, u.CreatedAt = newID(), r.s.now()
		d.users[u.ID] = *u
		d.track(u.ID)
		return nil
	})
}

// ---------------- vendors ----------------

type vendorRepo struct{ s *Store }

func normVendor(v *domain.Vendor) {
	v.Domain = strings.ToLower(strings.TrimSpace(v.Domain))
	if v.Country == "" {
		v.Country = "SG"
	}
}

func (r vendorRepo) List(_ context.Context, f store.VendorFilter) (out []domain.Vendor, err error) {
	err = r.s.do(func(d *state) error {
		all := sorted(d, d.vendors, func(v domain.Vendor) string { return v.ID }, func(a, b domain.Vendor) int {
			if a.Priority != b.Priority {
				return a.Priority - b.Priority
			}
			if c := strings.Compare(a.Name, b.Name); c != 0 {
				return c
			}
			return strings.Compare(a.Domain, b.Domain)
		})
		out = []domain.Vendor{}
		for _, v := range all {
			if (f.AllowedOnly && !v.Allowed) || (f.Category != "" && v.Category != f.Category) {
				continue
			}
			out = append(out, v)
		}
		return nil
	})
	return out, err
}

func (r vendorRepo) Get(_ context.Context, id string) (v domain.Vendor, err error) {
	err = r.s.do(func(d *state) error {
		var ok bool
		if v, ok = d.vendors[id]; !ok {
			return notFound("vendor", id)
		}
		return nil
	})
	return v, err
}

func (r vendorRepo) find(pred func(domain.Vendor) bool, key string) (v domain.Vendor, err error) {
	err = r.s.do(func(d *state) error {
		var best *domain.Vendor
		for _, x := range d.vendors {
			if !pred(x) {
				continue
			}
			if best == nil || (x.Allowed && !best.Allowed) ||
				(x.Allowed == best.Allowed && (x.Priority < best.Priority || (x.Priority == best.Priority && x.Domain < best.Domain))) {
				x := x
				best = &x
			}
		}
		if best == nil {
			return notFound("vendor", key)
		}
		v = *best
		return nil
	})
	return v, err
}

func (r vendorRepo) GetByDomain(_ context.Context, domainName string) (domain.Vendor, error) {
	dn := strings.ToLower(strings.TrimSpace(domainName))
	return r.find(func(v domain.Vendor) bool { return v.Domain == dn }, domainName)
}

func (r vendorRepo) GetByMerchantName(_ context.Context, merchantName string) (domain.Vendor, error) {
	if merchantName == "" {
		return domain.Vendor{}, notFound("vendor", merchantName)
	}
	return r.find(func(v domain.Vendor) bool { return v.ReapMerchantName == merchantName }, merchantName)
}

func vendorDomainTaken(d *state, dom, exceptID string) bool {
	for id, x := range d.vendors {
		if x.Domain == dom && id != exceptID {
			return true
		}
	}
	return false
}

func (r vendorRepo) Create(_ context.Context, v *domain.Vendor) error {
	normVendor(v)
	return r.s.do(func(d *state) error {
		if vendorDomainTaken(d, v.Domain, "") {
			return conflict("vendor", "domain "+v.Domain)
		}
		now := r.s.now()
		v.ID, v.CreatedAt, v.UpdatedAt = newID(), now, now
		d.vendors[v.ID] = *v
		d.track(v.ID)
		return nil
	})
}

func (r vendorRepo) Update(_ context.Context, v *domain.Vendor) error {
	normVendor(v)
	return r.s.do(func(d *state) error {
		old, ok := d.vendors[v.ID]
		if !ok {
			return notFound("vendor", v.ID)
		}
		if vendorDomainTaken(d, v.Domain, v.ID) {
			return conflict("vendor", "domain "+v.Domain)
		}
		v.CreatedAt, v.UpdatedAt = old.CreatedAt, r.s.now()
		d.vendors[v.ID] = *v
		return nil
	})
}

func (r vendorRepo) Delete(_ context.Context, id string) error {
	return r.s.do(func(d *state) error {
		if _, ok := d.vendors[id]; !ok {
			return notFound("vendor", id)
		}
		delete(d.vendors, id)
		// ON DELETE CASCADE (catalog_item_vendors) / SET NULL (offers, payments).
		for k, it := range d.catalog {
			it.PreferredVendorIDs = slices.DeleteFunc(cpStrings(it.PreferredVendorIDs), func(s string) bool { return s == id })
			d.catalog[k] = it
		}
		for k, o := range d.offers {
			if o.VendorID != nil && *o.VendorID == id {
				o.VendorID = nil
				d.offers[k] = o
			}
		}
		for k, p := range d.payments {
			if p.VendorID != nil && *p.VendorID == id {
				p.VendorID = nil
				d.payments[k] = p
			}
		}
		return nil
	})
}

func (r vendorRepo) Upsert(_ context.Context, v *domain.Vendor) error {
	normVendor(v)
	return r.s.do(func(d *state) error {
		now := r.s.now()
		for id, x := range d.vendors {
			if x.Domain == v.Domain {
				v.ID, v.CreatedAt, v.UpdatedAt = id, x.CreatedAt, now
				d.vendors[id] = *v
				return nil
			}
		}
		v.ID, v.CreatedAt, v.UpdatedAt = newID(), now, now
		d.vendors[v.ID] = *v
		d.track(v.ID)
		return nil
	})
}

// ---------------- catalog ----------------

type catalogRepo struct{ s *Store }

func (r catalogRepo) List(_ context.Context, f store.CatalogFilter) (out []domain.CatalogItem, err error) {
	q := strings.ToLower(strings.TrimSpace(f.Query))
	err = r.s.do(func(d *state) error {
		all := sorted(d, d.catalog, func(it domain.CatalogItem) string { return it.ID }, func(a, b domain.CatalogItem) int {
			if c := strings.Compare(a.Category, b.Category); c != 0 {
				return c
			}
			return strings.Compare(a.Name, b.Name)
		})
		out = []domain.CatalogItem{}
		for _, it := range all {
			if (f.ActiveOnly && !it.Active) || (f.Category != "" && it.Category != f.Category) {
				continue
			}
			if q != "" && !catalogMatches(it, q) {
				continue
			}
			out = append(out, cpCatalog(it))
		}
		return nil
	})
	return out, err
}

func catalogMatches(it domain.CatalogItem, q string) bool {
	if strings.Contains(strings.ToLower(it.Name), q) || strings.Contains(strings.ToLower(it.SKU), q) {
		return true
	}
	for _, a := range it.Aliases {
		if strings.Contains(strings.ToLower(a), q) {
			return true
		}
	}
	return false
}

func (r catalogRepo) Get(_ context.Context, id string) (it domain.CatalogItem, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.catalog[id]
		if !ok {
			return notFound("catalog item", id)
		}
		it = cpCatalog(x)
		return nil
	})
	return it, err
}

func (r catalogRepo) GetBySKU(_ context.Context, sku string) (it domain.CatalogItem, err error) {
	sku = strings.TrimSpace(sku)
	err = r.s.do(func(d *state) error {
		for _, x := range d.catalog {
			if x.SKU == sku {
				it = cpCatalog(x)
				return nil
			}
		}
		return notFound("catalog item", sku)
	})
	return it, err
}

// prepCatalog normalises and validates it against d (vendor FKs, sku uniqueness).
func prepCatalog(d *state, it *domain.CatalogItem, exceptID string) error {
	it.SKU = strings.TrimSpace(it.SKU)
	it.Aliases = cpStrings(it.Aliases)
	if it.DefaultQty == 0 {
		it.DefaultQty = 1
	}
	if it.DefaultQty < 0 || it.MaxUnitPriceCents < 0 {
		return invalid("catalog item", "default_qty must be > 0 and max_unit_price_cents >= 0")
	}
	var vids []string
	seen := map[string]bool{}
	for _, vid := range it.PreferredVendorIDs {
		if _, ok := d.vendors[vid]; !ok {
			return invalid("catalog item", "unknown preferred vendor "+vid)
		}
		if !seen[vid] {
			seen[vid] = true
			vids = append(vids, vid)
		}
	}
	it.PreferredVendorIDs = cpStrings(vids)
	for id, x := range d.catalog {
		if x.SKU == it.SKU && id != exceptID {
			return conflict("catalog item", "sku "+it.SKU)
		}
	}
	return nil
}

func (r catalogRepo) Create(_ context.Context, it *domain.CatalogItem) error {
	return r.s.do(func(d *state) error {
		if err := prepCatalog(d, it, ""); err != nil {
			return err
		}
		now := r.s.now()
		it.ID, it.CreatedAt, it.UpdatedAt = newID(), now, now
		d.catalog[it.ID] = cpCatalog(*it)
		d.track(it.ID)
		return nil
	})
}

func (r catalogRepo) Update(_ context.Context, it *domain.CatalogItem) error {
	return r.s.do(func(d *state) error {
		old, ok := d.catalog[it.ID]
		if !ok {
			return notFound("catalog item", it.ID)
		}
		if err := prepCatalog(d, it, it.ID); err != nil {
			return err
		}
		it.CreatedAt, it.UpdatedAt = old.CreatedAt, r.s.now()
		d.catalog[it.ID] = cpCatalog(*it)
		return nil
	})
}

func (r catalogRepo) Delete(_ context.Context, id string) error {
	return r.s.do(func(d *state) error {
		if _, ok := d.catalog[id]; !ok {
			return notFound("catalog item", id)
		}
		delete(d.catalog, id)
		for k, li := range d.lineItems { // ON DELETE SET NULL
			if li.CatalogItemID != nil && *li.CatalogItemID == id {
				li.CatalogItemID = nil
				d.lineItems[k] = li
			}
		}
		return nil
	})
}

func (r catalogRepo) Upsert(_ context.Context, it *domain.CatalogItem) error {
	return r.s.do(func(d *state) error {
		var existing *domain.CatalogItem
		for _, x := range d.catalog {
			if x.SKU == strings.TrimSpace(it.SKU) {
				x := x
				existing = &x
			}
		}
		exceptID := ""
		if existing != nil {
			exceptID = existing.ID
		}
		if err := prepCatalog(d, it, exceptID); err != nil {
			return err
		}
		now := r.s.now()
		if existing != nil {
			it.ID, it.CreatedAt, it.UpdatedAt = existing.ID, existing.CreatedAt, now
		} else {
			it.ID, it.CreatedAt, it.UpdatedAt = newID(), now, now
			d.track(it.ID)
		}
		d.catalog[it.ID] = cpCatalog(*it)
		return nil
	})
}

// ---------------- policy ----------------

type policyRepo struct{ s *Store }

func (r policyRepo) Get(context.Context) (p domain.PolicyConfig, err error) {
	err = r.s.do(func(d *state) error {
		if d.policy == nil {
			return notFound("policy", "1")
		}
		p = *d.policy
		return nil
	})
	return p, err
}

func (r policyRepo) Update(_ context.Context, p *domain.PolicyConfig, updatedBy string) error {
	if p.PerOrderLimitCents < 0 || p.MonthlyBudgetCents < 0 || p.PriceDriftPct < 0 {
		return invalid("policy", "limits must be >= 0")
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	return r.s.do(func(d *state) error {
		if updatedBy != "" {
			if _, ok := d.users[updatedBy]; !ok {
				return invalid("policy", "unknown user "+updatedBy)
			}
		}
		p.UpdatedAt, p.UpdatedBy = r.s.now(), updatedBy
		v := *p
		d.policy = &v
		return nil
	})
}

// ---------------- addresses ----------------

type addressRepo struct{ s *Store }

func normAddress(a *domain.Address) {
	a.Label = strings.TrimSpace(a.Label)
	if a.City == "" {
		a.City = "Singapore"
	}
	if a.Country == "" {
		a.Country = "SG"
	}
}

func validPhone(p string) bool {
	if len(p) < 8 || len(p) > 16 || p[0] != '+' || p[1] < '1' || p[1] > '9' {
		return false
	}
	for _, c := range p[2:] {
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

func (r addressRepo) List(context.Context) (out []domain.Address, err error) {
	err = r.s.do(func(d *state) error {
		out = sorted(d, d.addresses, func(a domain.Address) string { return a.ID }, func(a, b domain.Address) int {
			if a.IsDefault != b.IsDefault {
				if a.IsDefault {
					return -1
				}
				return 1
			}
			return strings.Compare(a.Label, b.Label)
		})
		return nil
	})
	return out, err
}

func (r addressRepo) Get(_ context.Context, id string) (a domain.Address, err error) {
	err = r.s.do(func(d *state) error {
		var ok bool
		if a, ok = d.addresses[id]; !ok {
			return notFound("address", id)
		}
		return nil
	})
	return a, err
}

func (r addressRepo) GetDefault(context.Context) (a domain.Address, err error) {
	err = r.s.do(func(d *state) error {
		for _, x := range d.addresses {
			if x.IsDefault {
				a = x
				return nil
			}
		}
		return notFound("address", "default")
	})
	return a, err
}

// put validates a, clears other defaults and stores it.
func (r addressRepo) put(d *state, a *domain.Address) error {
	if !validPhone(a.Phone) {
		return invalid("address", "phone must be E.164")
	}
	for id, x := range d.addresses {
		if id != a.ID && x.Label == a.Label {
			return conflict("address", "label "+a.Label)
		}
	}
	now := r.s.now()
	if a.IsDefault {
		for id, x := range d.addresses {
			if id != a.ID && x.IsDefault {
				x.IsDefault, x.UpdatedAt = false, now
				d.addresses[id] = x
			}
		}
	}
	a.UpdatedAt = now
	if old, ok := d.addresses[a.ID]; ok {
		a.CreatedAt = old.CreatedAt
	} else {
		a.CreatedAt = now
		d.track(a.ID)
	}
	d.addresses[a.ID] = *a
	return nil
}

func (r addressRepo) Create(_ context.Context, a *domain.Address) error {
	normAddress(a)
	return r.s.atomic(func(d *state) error {
		a.ID = newID()
		return r.put(d, a)
	})
}

func (r addressRepo) Update(_ context.Context, a *domain.Address) error {
	normAddress(a)
	return r.s.atomic(func(d *state) error {
		if _, ok := d.addresses[a.ID]; !ok {
			return notFound("address", a.ID)
		}
		return r.put(d, a)
	})
}

func (r addressRepo) Delete(_ context.Context, id string) error {
	return r.s.do(func(d *state) error {
		if _, ok := d.addresses[id]; !ok {
			return notFound("address", id)
		}
		for _, p := range d.requests {
			if p.AddressID != nil && *p.AddressID == id {
				return conflict("address", "referenced by a request")
			}
		}
		delete(d.addresses, id)
		return nil
	})
}

func (r addressRepo) Upsert(_ context.Context, a *domain.Address) error {
	normAddress(a)
	return r.s.atomic(func(d *state) error {
		a.ID = ""
		for id, x := range d.addresses {
			if x.Label == a.Label {
				a.ID = id
			}
		}
		if a.ID == "" {
			a.ID = newID()
		}
		return r.put(d, a)
	})
}

// ---------------- system prompts ----------------

type promptRepo struct{ s *Store }

func (r promptRepo) Get(_ context.Context, key string) (p domain.SystemPrompt, err error) {
	err = r.s.do(func(d *state) error {
		var ok bool
		if p, ok = d.prompts[key]; !ok {
			return notFound("system prompt", key)
		}
		return nil
	})
	return p, err
}

func (r promptRepo) Update(_ context.Context, key, content, updatedBy string) (p domain.SystemPrompt, err error) {
	err = r.s.do(func(d *state) error {
		if updatedBy != "" {
			if _, ok := d.users[updatedBy]; !ok {
				return invalid("system prompt", "unknown user "+updatedBy)
			}
		}
		p = d.prompts[key]
		p.Key, p.Content, p.UpdatedAt, p.UpdatedBy = key, content, r.s.now(), updatedBy
		p.Version++
		d.prompts[key] = p
		return nil
	})
	return p, err
}

func (r promptRepo) SeedIfMissing(_ context.Context, key, content string) error {
	return r.s.do(func(d *state) error {
		if _, ok := d.prompts[key]; !ok {
			d.prompts[key] = domain.SystemPrompt{Key: key, Content: content, Version: 1, UpdatedAt: r.s.now()}
		}
		return nil
	})
}

// ---------------- enrollments ----------------

type enrollmentRepo struct{ s *Store }

func validEnrollmentStatus(st domain.EnrollmentStatus) bool {
	switch st {
	case domain.EnrollmentRequiresAction, domain.EnrollmentActive, domain.EnrollmentFailed, domain.EnrollmentExpired, domain.EnrollmentRevoked:
		return true
	}
	return false
}

func (r enrollmentRepo) Create(_ context.Context, e *domain.Enrollment) error {
	if e.Status == "" {
		e.Status = domain.EnrollmentRequiresAction
	}
	if !validEnrollmentStatus(e.Status) {
		return invalid("enrollment", "unknown status "+string(e.Status))
	}
	return r.s.do(func(d *state) error {
		for _, x := range d.enrollments {
			if x.ReapEnrollmentID == e.ReapEnrollmentID {
				return conflict("enrollment", "reap id "+e.ReapEnrollmentID)
			}
		}
		if e.CreatedBy != "" {
			if _, ok := d.users[e.CreatedBy]; !ok {
				return invalid("enrollment", "unknown user "+e.CreatedBy)
			}
		}
		now := r.s.now()
		e.ID, e.CreatedAt, e.UpdatedAt = newID(), now, now
		d.enrollments[e.ID] = *e
		d.track(e.ID)
		return nil
	})
}

func (r enrollmentRepo) Get(_ context.Context, id string) (e domain.Enrollment, err error) {
	err = r.s.do(func(d *state) error {
		var ok bool
		if e, ok = d.enrollments[id]; !ok {
			return notFound("enrollment", id)
		}
		return nil
	})
	return e, err
}

func (r enrollmentRepo) latest(pred func(domain.Enrollment) bool, key string) (e domain.Enrollment, err error) {
	err = r.s.do(func(d *state) error {
		for _, x := range newestFirst(d, d.enrollments, func(e domain.Enrollment) (string, time.Time) { return e.ID, e.CreatedAt }) {
			if pred(x) {
				e = x
				return nil
			}
		}
		return notFound("enrollment", key)
	})
	return e, err
}

func (r enrollmentRepo) GetByReapID(_ context.Context, reapID string) (domain.Enrollment, error) {
	return r.latest(func(e domain.Enrollment) bool { return e.ReapEnrollmentID == reapID }, reapID)
}

func (r enrollmentRepo) Latest(context.Context) (domain.Enrollment, error) {
	return r.latest(func(domain.Enrollment) bool { return true }, "latest")
}

func (r enrollmentRepo) LatestActive(context.Context) (domain.Enrollment, error) {
	return r.latest(func(e domain.Enrollment) bool { return e.Status == domain.EnrollmentActive }, "latest active")
}

func (r enrollmentRepo) UpdateStatus(_ context.Context, id string, status domain.EnrollmentStatus, nextActionURL string) error {
	if !validEnrollmentStatus(status) {
		return invalid("enrollment", "unknown status "+string(status))
	}
	return r.s.do(func(d *state) error {
		e, ok := d.enrollments[id]
		if !ok {
			return notFound("enrollment", id)
		}
		e.Status, e.NextActionURL, e.UpdatedAt = status, nextActionURL, r.s.now()
		d.enrollments[id] = e
		return nil
	})
}
