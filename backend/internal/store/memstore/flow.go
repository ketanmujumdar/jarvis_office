package memstore

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// ---------------- purchase requests ----------------

type requestRepo struct{ s *Store }

func checkRequestRefs(d *state, p *domain.PurchaseRequest) error {
	if _, ok := d.users[p.RequesterID]; !ok {
		return invalid("request", "unknown requester "+p.RequesterID)
	}
	if p.AddressID != nil {
		if _, ok := d.addresses[*p.AddressID]; !ok {
			return invalid("request", "unknown address "+*p.AddressID)
		}
	}
	if p.ConfirmedBy != nil {
		if _, ok := d.users[*p.ConfirmedBy]; !ok {
			return invalid("request", "unknown confirmer "+*p.ConfirmedBy)
		}
	}
	switch p.Decision {
	case "", domain.DecisionAutoApprove, domain.DecisionNeedsApproval, domain.DecisionReject:
	default:
		return invalid("request", "unknown decision "+string(p.Decision))
	}
	return nil
}

func (r requestRepo) Create(_ context.Context, p *domain.PurchaseRequest) error {
	if p.Status == "" {
		p.Status = domain.StatusParsing
	}
	if !p.Status.Valid() {
		return invalid("request", "unknown status "+string(p.Status))
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	return r.s.do(func(d *state) error {
		if err := checkRequestRefs(d, p); err != nil {
			return err
		}
		now := r.s.now()
		p.ID, p.CreatedAt, p.UpdatedAt = newID(), now, now
		d.requests[p.ID] = cpRequest(*p)
		d.track(p.ID)
		*p = cpRequest(*p)
		return nil
	})
}

func (r requestRepo) Get(_ context.Context, id string) (p domain.PurchaseRequest, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.requests[id]
		if !ok {
			return notFound("request", id)
		}
		p = cpRequest(x)
		return nil
	})
	return p, err
}

func (r requestRepo) List(_ context.Context, f store.RequestFilter) (out []domain.PurchaseRequest, err error) {
	err = r.s.do(func(d *state) error {
		all := newestFirst(d, d.requests, func(p domain.PurchaseRequest) (string, time.Time) { return p.ID, p.CreatedAt })
		var match []domain.PurchaseRequest
		for _, p := range all {
			if len(f.Statuses) > 0 && !slices.Contains(f.Statuses, p.Status) {
				continue
			}
			if f.RequesterID != "" && p.RequesterID != f.RequesterID {
				continue
			}
			match = append(match, cpRequest(p))
		}
		out = slices.Clone(page(match, f.Limit, f.Offset, 50))
		return nil
	})
	return out, err
}

func (r requestRepo) Update(_ context.Context, p *domain.PurchaseRequest) error {
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	return r.s.do(func(d *state) error {
		old, ok := d.requests[p.ID]
		if !ok {
			return notFound("request", p.ID)
		}
		next := cpRequest(*p)
		next.RequesterID, next.RawUtterance, next.Status, next.CreatedAt = old.RequesterID, old.RawUtterance, old.Status, old.CreatedAt
		if err := checkRequestRefs(d, &next); err != nil {
			return err
		}
		next.UpdatedAt = r.s.now()
		d.requests[p.ID] = next
		*p = cpRequest(next)
		return nil
	})
}

func (r requestRepo) TransitionStatus(_ context.Context, id string, from, to domain.RequestStatus, failureReason string) error {
	if err := domain.CheckTransition(from, to); err != nil {
		return err
	}
	return r.s.do(func(d *state) error {
		p, ok := d.requests[id]
		if !ok {
			return notFound("request", id)
		}
		if p.Status != from {
			return fmt.Errorf("request %s is %s, not %s: %w", id, p.Status, from, domain.ErrConflict)
		}
		p.Status, p.UpdatedAt = to, r.s.now()
		if failureReason != "" {
			p.FailureReason = failureReason
		}
		d.requests[id] = p
		return nil
	})
}

func (r requestRepo) Detail(ctx context.Context, id string) (det domain.RequestDetail, err error) {
	err = r.s.WithTx(ctx, func(tx store.Store) error {
		var err error
		if det.Request, err = tx.Requests().Get(ctx, id); err != nil {
			return err
		}
		if det.LineItems, err = tx.LineItems().ListByRequest(ctx, id); err != nil {
			return err
		}
		if det.Offers, err = tx.Offers().ListByRequest(ctx, id); err != nil {
			return err
		}
		if det.Approvals, err = tx.Approvals().ListByRequest(ctx, id); err != nil {
			return err
		}
		if det.Payments, err = tx.Payments().ListByRequest(ctx, id); err != nil {
			return err
		}
		if det.Request.AddressID != nil {
			a, err := tx.Addresses().Get(ctx, *det.Request.AddressID)
			if err != nil {
				return err
			}
			det.Address = &a
		}
		return nil
	})
	return det, err
}

// ---------------- line items ----------------

type lineItemRepo struct{ s *Store }

func checkLineItem(d *state, li *domain.LineItem) error {
	if li.Urgency == "" {
		li.Urgency = domain.UrgencyNormal
	}
	li.Reasons = cpReasons(li.Reasons)
	if li.Qty <= 0 {
		return invalid("line item", "qty must be > 0")
	}
	if li.Urgency != domain.UrgencyNormal && li.Urgency != domain.UrgencyUrgent {
		return invalid("line item", "unknown urgency "+string(li.Urgency))
	}
	if li.CatalogItemID != nil {
		if _, ok := d.catalog[*li.CatalogItemID]; !ok {
			return invalid("line item", "unknown catalog item "+*li.CatalogItemID)
		}
	}
	if li.SelectedOfferID != nil {
		if _, ok := d.offers[*li.SelectedOfferID]; !ok {
			return invalid("line item", "unknown offer "+*li.SelectedOfferID)
		}
	}
	return nil
}

func (r lineItemRepo) CreateBatch(_ context.Context, items []*domain.LineItem) error {
	return r.s.atomic(func(d *state) error {
		for i, li := range items {
			if _, ok := d.requests[li.RequestID]; !ok {
				return invalid("line item", "unknown request "+li.RequestID)
			}
			if err := checkLineItem(d, li); err != nil {
				return err
			}
			now := r.s.now()
			li.ID, li.Position, li.CreatedAt, li.UpdatedAt = newID(), i, now, now
			d.lineItems[li.ID] = cpLineItem(*li)
			d.track(li.ID)
		}
		return nil
	})
}

func lineItemsOf(d *state, requestID string) []domain.LineItem {
	var out []domain.LineItem
	for _, li := range oldestFirst(d, d.lineItems, func(li domain.LineItem) (string, time.Time) { return li.ID, li.CreatedAt }) {
		if li.RequestID == requestID {
			out = append(out, li)
		}
	}
	slices.SortStableFunc(out, func(a, b domain.LineItem) int { return a.Position - b.Position })
	return out
}

func (r lineItemRepo) ListByRequest(_ context.Context, requestID string) (out []domain.LineItem, err error) {
	err = r.s.do(func(d *state) error {
		out = mapSlice(lineItemsOf(d, requestID), cpLineItem)
		return nil
	})
	return out, err
}

func (r lineItemRepo) Get(_ context.Context, id string) (li domain.LineItem, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.lineItems[id]
		if !ok {
			return notFound("line item", id)
		}
		li = cpLineItem(x)
		return nil
	})
	return li, err
}

func (r lineItemRepo) Update(_ context.Context, li *domain.LineItem) error {
	return r.s.do(func(d *state) error {
		old, ok := d.lineItems[li.ID]
		if !ok {
			return notFound("line item", li.ID)
		}
		next := cpLineItem(*li)
		next.RequestID, next.Position, next.CreatedAt = old.RequestID, old.Position, old.CreatedAt
		if err := checkLineItem(d, &next); err != nil {
			return err
		}
		next.UpdatedAt = r.s.now()
		d.lineItems[li.ID] = next
		*li = cpLineItem(next)
		return nil
	})
}

func (r lineItemRepo) Delete(_ context.Context, id string) error {
	return r.s.do(func(d *state) error {
		if _, ok := d.lineItems[id]; !ok {
			return notFound("line item", id)
		}
		delete(d.lineItems, id)
		for k, o := range d.offers { // ON DELETE CASCADE
			if o.LineItemID == id {
				delete(d.offers, k)
			}
		}
		return nil
	})
}

// ---------------- offers ----------------

type offerRepo struct{ s *Store }

func (r offerRepo) ReplaceForLineItem(_ context.Context, lineItemID string, offers []*domain.Offer) error {
	return r.s.atomic(func(d *state) error {
		li, ok := d.lineItems[lineItemID]
		if !ok {
			return notFound("line item", lineItemID)
		}
		li.SelectedOfferID, li.UpdatedAt = nil, r.s.now()
		d.lineItems[lineItemID] = li
		for k, o := range d.offers {
			if o.LineItemID == lineItemID {
				delete(d.offers, k)
			}
		}
		for _, o := range offers {
			if o.Currency == "" {
				o.Currency = domain.Currency
			}
			if o.PackSize <= 0 {
				o.PackSize = 1
			}
			if o.UnitPriceCents < 0 {
				return invalid("offer", "unit_price_cents must be >= 0")
			}
			if o.VendorID != nil {
				if _, ok := d.vendors[*o.VendorID]; !ok {
					return invalid("offer", "unknown vendor "+*o.VendorID)
				}
			}
			o.ID, o.LineItemID, o.CreatedAt = newID(), lineItemID, r.s.now()
			d.offers[o.ID] = cpOffer(*o)
			d.track(o.ID)
		}
		return nil
	})
}

func sortOffers(d *state, in []domain.Offer) {
	slices.SortStableFunc(in, func(a, b domain.Offer) int {
		if a.Rank != b.Rank {
			return a.Rank - b.Rank
		}
		return int(d.order[a.ID] - d.order[b.ID])
	})
}

func (r offerRepo) ListByLineItem(_ context.Context, lineItemID string) (out []domain.Offer, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.Offer{}
		for _, o := range d.offers {
			if o.LineItemID == lineItemID {
				out = append(out, cpOffer(o))
			}
		}
		sortOffers(d, out)
		return nil
	})
	return out, err
}

func (r offerRepo) ListByRequest(_ context.Context, requestID string) (out []domain.Offer, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.Offer{}
		for _, li := range lineItemsOf(d, requestID) {
			var per []domain.Offer
			for _, o := range d.offers {
				if o.LineItemID == li.ID {
					per = append(per, cpOffer(o))
				}
			}
			sortOffers(d, per)
			out = append(out, per...)
		}
		return nil
	})
	return out, err
}

func (r offerRepo) Get(_ context.Context, id string) (o domain.Offer, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.offers[id]
		if !ok {
			return notFound("offer", id)
		}
		o = cpOffer(x)
		return nil
	})
	return o, err
}

// ---------------- approvals ----------------

type approvalRepo struct{ s *Store }

func (r approvalRepo) Create(_ context.Context, a *domain.Approval) error {
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	if a.Kind != domain.ApprovalKindPolicy && a.Kind != domain.ApprovalKindPriceDrift {
		return invalid("approval", "unknown kind "+string(a.Kind))
	}
	return r.s.do(func(d *state) error {
		if _, ok := d.requests[a.RequestID]; !ok {
			return invalid("approval", "unknown request "+a.RequestID)
		}
		if a.Status == domain.ApprovalPending {
			for _, x := range d.approvals {
				if x.RequestID == a.RequestID && x.Status == domain.ApprovalPending {
					return conflict("approval", "request already has a pending approval")
				}
			}
		}
		a.ID, a.CreatedAt = newID(), r.s.now()
		a.Reasons = cpReasons(a.Reasons)
		d.approvals[a.ID] = cpApproval(*a)
		d.track(a.ID)
		return nil
	})
}

func (r approvalRepo) Get(_ context.Context, id string) (a domain.Approval, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.approvals[id]
		if !ok {
			return notFound("approval", id)
		}
		a = cpApproval(x)
		return nil
	})
	return a, err
}

func (r approvalRepo) List(_ context.Context, f store.ApprovalFilter) (out []domain.Approval, err error) {
	err = r.s.do(func(d *state) error {
		var match []domain.Approval
		for _, a := range newestFirst(d, d.approvals, func(a domain.Approval) (string, time.Time) { return a.ID, a.CreatedAt }) {
			if f.Status == "" || a.Status == f.Status {
				match = append(match, cpApproval(a))
			}
		}
		out = slices.Clone(page(match, f.Limit, f.Offset, 50))
		return nil
	})
	return out, err
}

func (r approvalRepo) ListByRequest(_ context.Context, requestID string) (out []domain.Approval, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.Approval{}
		for _, a := range newestFirst(d, d.approvals, func(a domain.Approval) (string, time.Time) { return a.ID, a.CreatedAt }) {
			if a.RequestID == requestID {
				out = append(out, cpApproval(a))
			}
		}
		return nil
	})
	return out, err
}

func (r approvalRepo) Decide(_ context.Context, id string, status domain.ApprovalStatus, approverID, comment string) (a domain.Approval, err error) {
	if status != domain.ApprovalApproved && status != domain.ApprovalRejected {
		return a, invalid("approval", "decision must be approved or rejected")
	}
	err = r.s.do(func(d *state) error {
		x, ok := d.approvals[id]
		if !ok {
			return notFound("approval", id)
		}
		if x.Status != domain.ApprovalPending {
			return fmt.Errorf("approval %s already %s: %w", id, x.Status, domain.ErrConflict)
		}
		if approverID != "" {
			if _, ok := d.users[approverID]; !ok {
				return invalid("approval", "unknown approver "+approverID)
			}
			x.ApproverID = &approverID
		}
		now := r.s.now()
		x.Status, x.Comment, x.DecidedAt = status, comment, &now
		d.approvals[id] = x
		a = cpApproval(x)
		return nil
	})
	return a, err
}

// ---------------- payments ----------------

type paymentRepo struct{ s *Store }

func validPaymentStatus(st domain.PaymentStatus) bool {
	switch st {
	case domain.PaymentQuoting, domain.PaymentQuoted, domain.PaymentRequiresAction, domain.PaymentProcessing,
		domain.PaymentCompleted, domain.PaymentFailed, domain.PaymentExpired:
		return true
	}
	return false
}

func paymentOpen(s domain.PaymentStatus) bool {
	return s != domain.PaymentFailed && s != domain.PaymentExpired
}

func paymentMerchantKey(p domain.Payment) string {
	if p.VendorID != nil {
		return "v:" + *p.VendorID
	}
	return "m:" + p.MerchantName
}

func checkPayment(d *state, p *domain.Payment) error {
	if !validPaymentStatus(p.Status) {
		return invalid("payment", "unknown status "+string(p.Status))
	}
	if p.VendorID != nil {
		if _, ok := d.vendors[*p.VendorID]; !ok {
			return invalid("payment", "unknown vendor "+*p.VendorID)
		}
	}
	if p.EnrollmentID != nil {
		if _, ok := d.enrollments[*p.EnrollmentID]; !ok {
			return invalid("payment", "unknown enrollment "+*p.EnrollmentID)
		}
	}
	for id, x := range d.payments {
		if id == p.ID {
			continue
		}
		if x.IdempotencyKey == p.IdempotencyKey {
			return conflict("payment", "idempotency key")
		}
		if p.ReapCheckoutID != "" && x.ReapCheckoutID == p.ReapCheckoutID {
			return conflict("payment", "reap checkout id")
		}
		// Mirrors payments_open_per_merchant_idx: one open payment per request and merchant.
		if x.RequestID == p.RequestID && paymentOpen(x.Status) && paymentOpen(p.Status) && paymentMerchantKey(x) == paymentMerchantKey(*p) {
			return conflict("payment", "open payment for this merchant already exists")
		}
	}
	return nil
}

func (r paymentRepo) Create(_ context.Context, p *domain.Payment) error {
	if p.Status == "" {
		p.Status = domain.PaymentQuoting
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	if p.IdempotencyKey == "" {
		return invalid("payment", "idempotency key required")
	}
	return r.s.do(func(d *state) error {
		if _, ok := d.requests[p.RequestID]; !ok {
			return invalid("payment", "unknown request "+p.RequestID)
		}
		p.ID = newID()
		if err := checkPayment(d, p); err != nil {
			return err
		}
		now := r.s.now()
		p.CreatedAt, p.UpdatedAt = now, now
		d.payments[p.ID] = cpPayment(*p)
		d.track(p.ID)
		if p.Status == domain.PaymentCompleted {
			d.completedAt[p.ID] = now
		}
		*p = cpPayment(*p)
		return nil
	})
}

func (r paymentRepo) Get(_ context.Context, id string) (p domain.Payment, err error) {
	err = r.s.do(func(d *state) error {
		x, ok := d.payments[id]
		if !ok {
			return notFound("payment", id)
		}
		p = cpPayment(x)
		return nil
	})
	return p, err
}

func (r paymentRepo) GetByCheckoutID(_ context.Context, reapCheckoutID string) (p domain.Payment, err error) {
	err = r.s.do(func(d *state) error {
		for _, x := range d.payments {
			if reapCheckoutID != "" && x.ReapCheckoutID == reapCheckoutID {
				p = cpPayment(x)
				return nil
			}
		}
		return notFound("payment", reapCheckoutID)
	})
	return p, err
}

func (r paymentRepo) list(pred func(domain.Payment) bool) (out []domain.Payment, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.Payment{}
		for _, p := range oldestFirst(d, d.payments, func(p domain.Payment) (string, time.Time) { return p.ID, p.CreatedAt }) {
			if pred(p) {
				out = append(out, cpPayment(p))
			}
		}
		return nil
	})
	return out, err
}

func (r paymentRepo) ListByRequest(_ context.Context, requestID string) ([]domain.Payment, error) {
	return r.list(func(p domain.Payment) bool { return p.RequestID == requestID })
}

func (r paymentRepo) ListInFlight(context.Context) ([]domain.Payment, error) {
	return r.list(func(p domain.Payment) bool {
		return p.Status == domain.PaymentRequiresAction || p.Status == domain.PaymentProcessing
	})
}

func (r paymentRepo) Update(_ context.Context, p *domain.Payment) error {
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	return r.s.do(func(d *state) error {
		old, ok := d.payments[p.ID]
		if !ok {
			return notFound("payment", p.ID)
		}
		next := cpPayment(*p)
		next.RequestID, next.IdempotencyKey, next.CreatedAt = old.RequestID, old.IdempotencyKey, old.CreatedAt
		if err := checkPayment(d, &next); err != nil {
			return err
		}
		now := r.s.now()
		next.UpdatedAt = now
		if next.Status == domain.PaymentCompleted {
			if _, ok := d.completedAt[p.ID]; !ok {
				d.completedAt[p.ID] = now
			}
		} else {
			delete(d.completedAt, p.ID)
		}
		d.payments[p.ID] = next
		*p = cpPayment(next)
		return nil
	})
}

// ---------------- audit (append-only) ----------------

type auditRepo struct{ s *Store }

func validActor(a domain.ActorType) bool {
	return a == domain.ActorUser || a == domain.ActorAgent || a == domain.ActorSystem
}

func (r auditRepo) Append(_ context.Context, e *domain.AuditEvent) error {
	if !validActor(e.ActorType) {
		return invalid("audit event", "unknown actor type "+string(e.ActorType))
	}
	if len(e.Payload) == 0 {
		e.Payload = json.RawMessage(`{}`)
	}
	if !json.Valid(e.Payload) {
		return invalid("audit event", "payload is not valid JSON")
	}
	return r.s.do(func(d *state) error {
		if e.RequestID != nil {
			if _, ok := d.requests[*e.RequestID]; !ok {
				return invalid("audit event", "unknown request "+*e.RequestID)
			}
		}
		d.seq++
		e.ID = d.seq
		if e.At.IsZero() {
			e.At = r.s.now()
		} else {
			e.At = e.At.UTC()
		}
		e.Payload = cpRaw(e.Payload)
		d.audit = append(d.audit, cpAudit(*e))
		return nil
	})
}

func (r auditRepo) ListByRequest(_ context.Context, requestID string) (out []domain.AuditEvent, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.AuditEvent{}
		for _, e := range d.audit {
			if e.RequestID != nil && *e.RequestID == requestID {
				out = append(out, cpAudit(e))
			}
		}
		return nil
	})
	return out, err
}

func (r auditRepo) List(_ context.Context, f store.AuditFilter) (out []domain.AuditEvent, err error) {
	err = r.s.do(func(d *state) error {
		var match []domain.AuditEvent
		for i := len(d.audit) - 1; i >= 0; i-- {
			e := d.audit[i]
			if f.Type != "" && e.Type != f.Type && !(strings.HasSuffix(f.Type, ".") && strings.HasPrefix(e.Type, f.Type)) {
				continue
			}
			if !f.Since.IsZero() && e.At.Before(f.Since) {
				continue
			}
			match = append(match, cpAudit(e))
		}
		out = slices.Clone(page(match, f.Limit, f.Offset, 100))
		return nil
	})
	return out, err
}

// ---------------- chat ----------------

type chatRepo struct{ s *Store }

func (r chatRepo) Append(_ context.Context, m *domain.ChatMessage) error {
	switch m.Role {
	case domain.ChatRoleSystem, domain.ChatRoleUser, domain.ChatRoleAssistant, domain.ChatRoleTool:
	default:
		return invalid("chat message", "unknown role "+string(m.Role))
	}
	if len(m.ToolCalls) > 0 && !json.Valid(m.ToolCalls) {
		return invalid("chat message", "tool_calls is not valid JSON")
	}
	return r.s.do(func(d *state) error {
		if m.UserID != "" {
			if _, ok := d.users[m.UserID]; !ok {
				return invalid("chat message", "unknown user "+m.UserID)
			}
		}
		d.seq++
		m.ID, m.CreatedAt = d.seq, r.s.now()
		if len(m.ToolCalls) == 0 {
			m.ToolCalls = nil
		}
		d.chat = append(d.chat, cpChat(*m))
		return nil
	})
}

func (r chatRepo) ListBySession(_ context.Context, sessionID string, limit int) (out []domain.ChatMessage, err error) {
	err = r.s.do(func(d *state) error {
		out = []domain.ChatMessage{}
		for _, m := range d.chat {
			if m.SessionID == sessionID {
				out = append(out, cpChat(m))
			}
		}
		if limit > 0 && len(out) > limit {
			out = out[len(out)-limit:]
		}
		return nil
	})
	return out, err
}

// ---------------- spend ----------------

type spendRepo struct{ s *Store }

func monthStart(at time.Time) time.Time {
	l := at.In(store.SingaporeLocation)
	return time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, store.SingaporeLocation)
}

func paymentAmount(p domain.Payment) domain.Cents {
	if p.FinalCents != nil {
		return *p.FinalCents
	}
	return p.QuotedCents
}

func (r spendRepo) MonthToDate(_ context.Context, at time.Time) (c domain.Cents, err error) {
	start := monthStart(at)
	end := start.AddDate(0, 1, 0)
	err = r.s.do(func(d *state) error {
		for id, t := range d.completedAt {
			p, ok := d.payments[id]
			if ok && p.Status == domain.PaymentCompleted && !t.Before(start) && t.Before(end) {
				c += paymentAmount(p)
			}
		}
		return nil
	})
	return c, err
}

func (r spendRepo) ByMonth(_ context.Context, months int, now time.Time) (out []domain.MonthSpend, err error) {
	if months <= 0 {
		months = 12
	}
	cur := monthStart(now)
	err = r.s.do(func(d *state) error {
		type agg struct {
			cents domain.Cents
			reqs  map[string]bool
		}
		byMonth := map[string]*agg{}
		for id, t := range d.completedAt {
			p, ok := d.payments[id]
			if !ok || p.Status != domain.PaymentCompleted {
				continue
			}
			key := t.In(store.SingaporeLocation).Format("2006-01")
			a := byMonth[key]
			if a == nil {
				a = &agg{reqs: map[string]bool{}}
				byMonth[key] = a
			}
			a.cents += paymentAmount(p)
			a.reqs[p.RequestID] = true
		}
		out = make([]domain.MonthSpend, 0, months)
		for i := months - 1; i >= 0; i-- {
			key := cur.AddDate(0, -i, 0).Format("2006-01")
			m := domain.MonthSpend{Month: key}
			if a := byMonth[key]; a != nil {
				m.SpendCents, m.Orders = a.cents, len(a.reqs)
			}
			out = append(out, m)
		}
		return nil
	})
	return out, err
}
