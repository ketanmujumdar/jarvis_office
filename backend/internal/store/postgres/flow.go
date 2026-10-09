package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// ---------------- purchase requests ----------------

type requestRepo struct{ s *Store }

const requestCols = `id, requester_id, raw_utterance, status, address_id::text, subtotal_cents, shipping_cents,
	total_cents, currency, decision, failure_reason, confirmed_by::text, confirmed_at, created_at, updated_at`

func scanRequest(r scanner) (domain.PurchaseRequest, error) {
	var p domain.PurchaseRequest
	err := r.Scan(&p.ID, &p.RequesterID, &p.RawUtterance, &p.Status, &p.AddressID, &p.SubtotalCents, &p.ShippingCents,
		&p.TotalCents, &p.Currency, &p.Decision, &p.FailureReason, &p.ConfirmedBy, &p.ConfirmedAt, &p.CreatedAt, &p.UpdatedAt)
	p.ConfirmedAt = utcPtr(p.ConfirmedAt)
	p.CreatedAt, p.UpdatedAt = utc(p.CreatedAt), utc(p.UpdatedAt)
	return p, err
}

func (r requestRepo) Create(ctx context.Context, p *domain.PurchaseRequest) error {
	if p.Status == "" {
		p.Status = domain.StatusParsing
	}
	if !p.Status.Valid() {
		return fmt.Errorf("request: %w: unknown status %q", domain.ErrValidation, p.Status)
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	got, err := one(ctx, r.s.db, "request", scanRequest, `
		INSERT INTO purchase_requests (requester_id, raw_utterance, status, address_id, subtotal_cents, shipping_cents,
			total_cents, currency, decision, failure_reason, confirmed_by, confirmed_at, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, clock_timestamp(), clock_timestamp())
		RETURNING `+requestCols,
		p.RequesterID, p.RawUtterance, p.Status, p.AddressID, p.SubtotalCents, p.ShippingCents, p.TotalCents,
		p.Currency, p.Decision, p.FailureReason, p.ConfirmedBy, p.ConfirmedAt)
	if err != nil {
		return err
	}
	*p = got
	return nil
}

func (r requestRepo) Get(ctx context.Context, id string) (domain.PurchaseRequest, error) {
	if !validUUID(id) {
		return domain.PurchaseRequest{}, notFound("request", id)
	}
	return one(ctx, r.s.db, "request", scanRequest, `SELECT `+requestCols+` FROM purchase_requests WHERE id = $1`, id)
}

func (r requestRepo) List(ctx context.Context, f store.RequestFilter) ([]domain.PurchaseRequest, error) {
	statuses := make([]string, 0, len(f.Statuses))
	for _, st := range f.Statuses {
		statuses = append(statuses, string(st))
	}
	if f.RequesterID != "" && !validUUID(f.RequesterID) {
		return []domain.PurchaseRequest{}, nil
	}
	return collect(ctx, r.s.db, "request", scanRequest, `SELECT `+requestCols+` FROM purchase_requests
		WHERE (cardinality($1::text[]) = 0 OR status = ANY($1)) AND ($2::uuid IS NULL OR requester_id = $2)
		ORDER BY created_at DESC, id DESC LIMIT $3 OFFSET $4`,
		statuses, nullStr(f.RequesterID), limitOr(f.Limit, 50), max(f.Offset, 0))
}

func (r requestRepo) Update(ctx context.Context, p *domain.PurchaseRequest) error {
	if !validUUID(p.ID) {
		return notFound("request", p.ID)
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	got, err := one(ctx, r.s.db, "request", scanRequest, `
		UPDATE purchase_requests SET address_id = $2, subtotal_cents = $3, shipping_cents = $4, total_cents = $5,
			currency = $6, decision = $7, failure_reason = $8, confirmed_by = $9, confirmed_at = $10,
			updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+requestCols,
		p.ID, p.AddressID, p.SubtotalCents, p.ShippingCents, p.TotalCents, p.Currency, p.Decision, p.FailureReason,
		p.ConfirmedBy, p.ConfirmedAt)
	if err != nil {
		return err
	}
	*p = got
	return nil
}

func (r requestRepo) TransitionStatus(ctx context.Context, id string, from, to domain.RequestStatus, failureReason string) error {
	if err := domain.CheckTransition(from, to); err != nil {
		return err
	}
	if !validUUID(id) {
		return notFound("request", id)
	}
	tag, err := r.s.db.Exec(ctx, `UPDATE purchase_requests
		SET status = $3, failure_reason = CASE WHEN $4 <> '' THEN $4 ELSE failure_reason END, updated_at = clock_timestamp()
		WHERE id = $1 AND status = $2`, id, from, to, failureReason)
	if err != nil {
		return mapErr("request", err)
	}
	if tag.RowsAffected() == 1 {
		return nil
	}
	var cur domain.RequestStatus
	if err := r.s.db.QueryRow(ctx, `SELECT status FROM purchase_requests WHERE id = $1`, id).Scan(&cur); err != nil {
		return mapErr("request", err)
	}
	return fmt.Errorf("request %s is %s, not %s: %w", id, cur, from, domain.ErrConflict)
}

func (r requestRepo) Detail(ctx context.Context, id string) (domain.RequestDetail, error) {
	var d domain.RequestDetail
	err := r.s.atomic(ctx, func(db DB) error {
		tx := &Store{pool: r.s.pool, db: db, inTx: true}
		var err error
		if d.Request, err = tx.Requests().Get(ctx, id); err != nil {
			return err
		}
		if d.LineItems, err = tx.LineItems().ListByRequest(ctx, id); err != nil {
			return err
		}
		if d.Offers, err = tx.Offers().ListByRequest(ctx, id); err != nil {
			return err
		}
		if d.Approvals, err = tx.Approvals().ListByRequest(ctx, id); err != nil {
			return err
		}
		if d.Payments, err = tx.Payments().ListByRequest(ctx, id); err != nil {
			return err
		}
		if d.Request.AddressID != nil {
			a, err := tx.Addresses().Get(ctx, *d.Request.AddressID)
			if err != nil {
				return err
			}
			d.Address = &a
		}
		return nil
	})
	return d, err
}

// ---------------- line items ----------------

type lineItemRepo struct{ s *Store }

const lineItemCols = `id, request_id, position, catalog_item_id::text, description, qty, urgency, policy_decision,
	reasons, selected_offer_id::text, created_at, updated_at`

func scanLineItem(r scanner) (domain.LineItem, error) {
	var li domain.LineItem
	err := r.Scan(&li.ID, &li.RequestID, &li.Position, &li.CatalogItemID, &li.Description, &li.Qty, &li.Urgency,
		&li.PolicyDecision, &li.Reasons, &li.SelectedOfferID, &li.CreatedAt, &li.UpdatedAt)
	li.Reasons = emptyIfNil(li.Reasons)
	li.CreatedAt, li.UpdatedAt = utc(li.CreatedAt), utc(li.UpdatedAt)
	return li, err
}

func normLineItem(li *domain.LineItem) {
	if li.Urgency == "" {
		li.Urgency = domain.UrgencyNormal
	}
	li.Reasons = emptyIfNil(li.Reasons)
}

func (r lineItemRepo) CreateBatch(ctx context.Context, items []*domain.LineItem) error {
	if len(items) == 0 {
		return nil
	}
	return r.s.atomic(ctx, func(db DB) error {
		for i, li := range items {
			normLineItem(li)
			li.Position = i
			got, err := one(ctx, db, "line item", scanLineItem, `
				INSERT INTO line_items (request_id, position, catalog_item_id, description, qty, urgency, policy_decision,
					reasons, selected_offer_id)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+lineItemCols,
				li.RequestID, li.Position, li.CatalogItemID, li.Description, li.Qty, li.Urgency, li.PolicyDecision,
				li.Reasons, li.SelectedOfferID)
			if err != nil {
				return err
			}
			*li = got
		}
		return nil
	})
}

func (r lineItemRepo) ListByRequest(ctx context.Context, requestID string) ([]domain.LineItem, error) {
	if !validUUID(requestID) {
		return []domain.LineItem{}, nil
	}
	return collect(ctx, r.s.db, "line item", scanLineItem,
		`SELECT `+lineItemCols+` FROM line_items WHERE request_id = $1 ORDER BY position, created_at`, requestID)
}

func (r lineItemRepo) Get(ctx context.Context, id string) (domain.LineItem, error) {
	if !validUUID(id) {
		return domain.LineItem{}, notFound("line item", id)
	}
	return one(ctx, r.s.db, "line item", scanLineItem, `SELECT `+lineItemCols+` FROM line_items WHERE id = $1`, id)
}

func (r lineItemRepo) Update(ctx context.Context, li *domain.LineItem) error {
	if !validUUID(li.ID) {
		return notFound("line item", li.ID)
	}
	normLineItem(li)
	got, err := one(ctx, r.s.db, "line item", scanLineItem, `
		UPDATE line_items SET qty = $2, catalog_item_id = $3, policy_decision = $4, reasons = $5, selected_offer_id = $6,
			description = $7, urgency = $8, updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+lineItemCols,
		li.ID, li.Qty, li.CatalogItemID, li.PolicyDecision, li.Reasons, li.SelectedOfferID, li.Description, li.Urgency)
	if err != nil {
		return err
	}
	*li = got
	return nil
}

func (r lineItemRepo) Delete(ctx context.Context, id string) error {
	if !validUUID(id) {
		return notFound("line item", id)
	}
	tag, err := r.s.db.Exec(ctx, `DELETE FROM line_items WHERE id = $1`, id)
	if err != nil {
		return mapDeleteErr("line item", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("line item", id)
	}
	return nil
}

// ---------------- offers ----------------

type offerRepo struct{ s *Store }

const offerCols = `o.id, o.line_item_id, o.vendor_id::text, o.merchant_name, o.reap_product_id, o.reap_variant_id, o.title,
	o.variant_name, o.image_url, o.url, o.unit_price_cents, o.currency, o.pack_size, o.shipping_cents,
	o.landed_cost_cents, o.eta, o.available, o.rank, o.created_at`

func scanOffer(r scanner) (domain.Offer, error) {
	var o domain.Offer
	err := r.Scan(&o.ID, &o.LineItemID, &o.VendorID, &o.MerchantName, &o.ReapProductID, &o.ReapVariantID, &o.Title,
		&o.VariantName, &o.ImageURL, &o.URL, &o.UnitPriceCents, &o.Currency, &o.PackSize, &o.ShippingCents,
		&o.LandedCostCents, &o.ETA, &o.Available, &o.Rank, &o.CreatedAt)
	o.CreatedAt = utc(o.CreatedAt)
	return o, err
}

func (r offerRepo) ReplaceForLineItem(ctx context.Context, lineItemID string, offers []*domain.Offer) error {
	if !validUUID(lineItemID) {
		return notFound("line item", lineItemID)
	}
	return r.s.atomic(ctx, func(db DB) error {
		tag, err := db.Exec(ctx, `UPDATE line_items SET selected_offer_id = NULL, updated_at = clock_timestamp()
			WHERE id = $1`, lineItemID)
		if err != nil {
			return mapErr("line item", err)
		}
		if tag.RowsAffected() == 0 {
			return notFound("line item", lineItemID)
		}
		if _, err := db.Exec(ctx, `DELETE FROM offers WHERE line_item_id = $1`, lineItemID); err != nil {
			return mapErr("offer", err)
		}
		for _, o := range offers {
			o.LineItemID = lineItemID
			if o.Currency == "" {
				o.Currency = domain.Currency
			}
			if o.PackSize <= 0 {
				o.PackSize = 1
			}
			got, err := one(ctx, db, "offer", scanOffer, `
				INSERT INTO offers AS o (line_item_id, vendor_id, merchant_name, reap_product_id, reap_variant_id, title,
					variant_name, image_url, url, unit_price_cents, currency, pack_size, shipping_cents, landed_cost_cents,
					eta, available, rank)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17) RETURNING `+offerCols,
				o.LineItemID, o.VendorID, o.MerchantName, o.ReapProductID, o.ReapVariantID, o.Title, o.VariantName,
				o.ImageURL, o.URL, o.UnitPriceCents, o.Currency, o.PackSize, o.ShippingCents, o.LandedCostCents, o.ETA,
				o.Available, o.Rank)
			if err != nil {
				return err
			}
			*o = got
		}
		return nil
	})
}

func (r offerRepo) ListByLineItem(ctx context.Context, lineItemID string) ([]domain.Offer, error) {
	if !validUUID(lineItemID) {
		return []domain.Offer{}, nil
	}
	return collect(ctx, r.s.db, "offer", scanOffer,
		`SELECT `+offerCols+` FROM offers o WHERE o.line_item_id = $1 ORDER BY o.rank, o.created_at, o.id`, lineItemID)
}

func (r offerRepo) ListByRequest(ctx context.Context, requestID string) ([]domain.Offer, error) {
	if !validUUID(requestID) {
		return []domain.Offer{}, nil
	}
	return collect(ctx, r.s.db, "offer", scanOffer, `SELECT `+offerCols+` FROM offers o
		JOIN line_items li ON li.id = o.line_item_id
		WHERE li.request_id = $1 ORDER BY li.position, o.rank, o.created_at, o.id`, requestID)
}

func (r offerRepo) Get(ctx context.Context, id string) (domain.Offer, error) {
	if !validUUID(id) {
		return domain.Offer{}, notFound("offer", id)
	}
	return one(ctx, r.s.db, "offer", scanOffer, `SELECT `+offerCols+` FROM offers o WHERE o.id = $1`, id)
}

// ---------------- approvals ----------------

type approvalRepo struct{ s *Store }

const approvalCols = `id, request_id, kind, status, reasons, amount_cents, prev_cents, approver_id::text, comment,
	decided_at, created_at`

func scanApproval(r scanner) (domain.Approval, error) {
	var a domain.Approval
	err := r.Scan(&a.ID, &a.RequestID, &a.Kind, &a.Status, &a.Reasons, &a.AmountCents, &a.PrevCents, &a.ApproverID,
		&a.Comment, &a.DecidedAt, &a.CreatedAt)
	a.Reasons = emptyIfNil(a.Reasons)
	a.DecidedAt = utcPtr(a.DecidedAt)
	a.CreatedAt = utc(a.CreatedAt)
	return a, err
}

func (r approvalRepo) Create(ctx context.Context, a *domain.Approval) error {
	if a.Status == "" {
		a.Status = domain.ApprovalPending
	}
	got, err := one(ctx, r.s.db, "approval", scanApproval, `
		INSERT INTO approvals (request_id, kind, status, reasons, amount_cents, prev_cents, approver_id, comment,
			decided_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, clock_timestamp()) RETURNING `+approvalCols,
		a.RequestID, a.Kind, a.Status, emptyIfNil(a.Reasons), a.AmountCents, a.PrevCents, a.ApproverID, a.Comment, a.DecidedAt)
	if err != nil {
		return err
	}
	*a = got
	return nil
}

func (r approvalRepo) Get(ctx context.Context, id string) (domain.Approval, error) {
	if !validUUID(id) {
		return domain.Approval{}, notFound("approval", id)
	}
	return one(ctx, r.s.db, "approval", scanApproval, `SELECT `+approvalCols+` FROM approvals WHERE id = $1`, id)
}

func (r approvalRepo) List(ctx context.Context, f store.ApprovalFilter) ([]domain.Approval, error) {
	return collect(ctx, r.s.db, "approval", scanApproval, `SELECT `+approvalCols+` FROM approvals
		WHERE ($1 = '' OR status = $1) ORDER BY created_at DESC, id DESC LIMIT $2 OFFSET $3`,
		string(f.Status), limitOr(f.Limit, 50), max(f.Offset, 0))
}

func (r approvalRepo) ListByRequest(ctx context.Context, requestID string) ([]domain.Approval, error) {
	if !validUUID(requestID) {
		return []domain.Approval{}, nil
	}
	return collect(ctx, r.s.db, "approval", scanApproval,
		`SELECT `+approvalCols+` FROM approvals WHERE request_id = $1 ORDER BY created_at DESC, id DESC`, requestID)
}

func (r approvalRepo) Decide(ctx context.Context, id string, status domain.ApprovalStatus, approverID, comment string) (domain.Approval, error) {
	if status != domain.ApprovalApproved && status != domain.ApprovalRejected {
		return domain.Approval{}, fmt.Errorf("approval: %w: decision must be approved or rejected, got %q", domain.ErrValidation, status)
	}
	if !validUUID(id) {
		return domain.Approval{}, notFound("approval", id)
	}
	a, err := one(ctx, r.s.db, "approval", scanApproval, `
		UPDATE approvals SET status = $2, approver_id = $3, comment = $4, decided_at = clock_timestamp()
		WHERE id = $1 AND status = 'pending' RETURNING `+approvalCols, id, status, nullStr(approverID), comment)
	if err == nil {
		return a, nil
	}
	if !isNotFound(err) {
		return domain.Approval{}, err
	}
	cur, gerr := r.Get(ctx, id)
	if gerr != nil {
		return domain.Approval{}, gerr
	}
	return domain.Approval{}, fmt.Errorf("approval %s already %s: %w", id, cur.Status, domain.ErrConflict)
}

// ---------------- payments ----------------

type paymentRepo struct{ s *Store }

const paymentCols = `id, request_id, vendor_id::text, merchant_name, enrollment_id::text, reap_quote_id, reap_checkout_id,
	reap_order_id, items_cents, shipping_cents, tax_cents, quoted_cents, final_cents, currency, status, approval_url,
	quote_expires_at, idempotency_key, error, created_at, updated_at, quote_attempt`

func scanPayment(r scanner) (domain.Payment, error) {
	var p domain.Payment
	err := r.Scan(&p.ID, &p.RequestID, &p.VendorID, &p.MerchantName, &p.EnrollmentID, &p.ReapQuoteID, &p.ReapCheckoutID,
		&p.ReapOrderID, &p.ItemsCents, &p.ShippingCents, &p.TaxCents, &p.QuotedCents, &p.FinalCents, &p.Currency,
		&p.Status, &p.ApprovalURL, &p.QuoteExpiresAt, &p.IdempotencyKey, &p.Error, &p.CreatedAt, &p.UpdatedAt, &p.QuoteAttempt)
	p.QuoteExpiresAt = utcPtr(p.QuoteExpiresAt)
	p.CreatedAt, p.UpdatedAt = utc(p.CreatedAt), utc(p.UpdatedAt)
	return p, err
}

func (r paymentRepo) Create(ctx context.Context, p *domain.Payment) error {
	if p.Status == "" {
		p.Status = domain.PaymentQuoting
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	if p.IdempotencyKey == "" {
		return fmt.Errorf("payment: %w: idempotency key required", domain.ErrValidation)
	}
	got, err := one(ctx, r.s.db, "payment", scanPayment, `
		INSERT INTO payments (request_id, vendor_id, merchant_name, enrollment_id, reap_quote_id, reap_checkout_id,
			reap_order_id, items_cents, shipping_cents, tax_cents, quoted_cents, final_cents, currency, status,
			approval_url, quote_expires_at, idempotency_key, error, completed_at, created_at, updated_at, quote_attempt)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18,
			CASE WHEN $14 = 'completed' THEN clock_timestamp() END, clock_timestamp(), clock_timestamp(), $19)
		RETURNING `+paymentCols,
		p.RequestID, p.VendorID, p.MerchantName, p.EnrollmentID, p.ReapQuoteID, p.ReapCheckoutID, p.ReapOrderID,
		p.ItemsCents, p.ShippingCents, p.TaxCents, p.QuotedCents, p.FinalCents, p.Currency, string(p.Status),
		p.ApprovalURL, p.QuoteExpiresAt, p.IdempotencyKey, p.Error, p.QuoteAttempt)
	if err != nil {
		return err
	}
	*p = got
	return nil
}

func (r paymentRepo) Get(ctx context.Context, id string) (domain.Payment, error) {
	if !validUUID(id) {
		return domain.Payment{}, notFound("payment", id)
	}
	return one(ctx, r.s.db, "payment", scanPayment, `SELECT `+paymentCols+` FROM payments WHERE id = $1`, id)
}

func (r paymentRepo) GetByCheckoutID(ctx context.Context, reapCheckoutID string) (domain.Payment, error) {
	if reapCheckoutID == "" {
		return domain.Payment{}, notFound("payment", reapCheckoutID)
	}
	return one(ctx, r.s.db, "payment", scanPayment,
		`SELECT `+paymentCols+` FROM payments WHERE reap_checkout_id = $1`, reapCheckoutID)
}

func (r paymentRepo) ListByRequest(ctx context.Context, requestID string) ([]domain.Payment, error) {
	if !validUUID(requestID) {
		return []domain.Payment{}, nil
	}
	return collect(ctx, r.s.db, "payment", scanPayment,
		`SELECT `+paymentCols+` FROM payments WHERE request_id = $1 ORDER BY created_at, id`, requestID)
}

func (r paymentRepo) ListInFlight(ctx context.Context) ([]domain.Payment, error) {
	return collect(ctx, r.s.db, "payment", scanPayment, `SELECT `+paymentCols+` FROM payments
		WHERE status IN ('requires_action', 'processing') ORDER BY created_at, id`)
}

func (r paymentRepo) Update(ctx context.Context, p *domain.Payment) error {
	if !validUUID(p.ID) {
		return notFound("payment", p.ID)
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	got, err := one(ctx, r.s.db, "payment", scanPayment, `
		UPDATE payments SET vendor_id = $2, merchant_name = $3, enrollment_id = $4, reap_quote_id = $5,
			reap_checkout_id = $6, reap_order_id = $7, items_cents = $8, shipping_cents = $9, tax_cents = $10,
			quoted_cents = $11, final_cents = $12, currency = $13, status = $14, approval_url = $15,
			quote_expires_at = $16, error = $17, quote_attempt = $18,
			completed_at = CASE WHEN $14 = 'completed' THEN COALESCE(completed_at, clock_timestamp()) END,
			updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+paymentCols,
		p.ID, p.VendorID, p.MerchantName, p.EnrollmentID, p.ReapQuoteID, p.ReapCheckoutID, p.ReapOrderID, p.ItemsCents,
		p.ShippingCents, p.TaxCents, p.QuotedCents, p.FinalCents, p.Currency, string(p.Status), p.ApprovalURL,
		p.QuoteExpiresAt, p.Error, p.QuoteAttempt)
	if err != nil {
		return err
	}
	*p = got
	return nil
}

// ---------------- audit ----------------

type auditRepo struct{ s *Store }

const auditCols = `id, request_id::text, actor_type, actor_id, type, payload, at`

func scanAudit(r scanner) (domain.AuditEvent, error) {
	var e domain.AuditEvent
	var payload []byte
	err := r.Scan(&e.ID, &e.RequestID, &e.ActorType, &e.ActorID, &e.Type, &payload, &e.At)
	e.Payload = json.RawMessage(payload)
	e.At = utc(e.At)
	return e, err
}

func (r auditRepo) Append(ctx context.Context, e *domain.AuditEvent) error {
	payload := []byte(e.Payload)
	if len(payload) == 0 {
		payload = []byte(`{}`)
	}
	if !json.Valid(payload) {
		return fmt.Errorf("audit: %w: payload is not valid JSON", domain.ErrValidation)
	}
	var at *time.Time
	if !e.At.IsZero() {
		t := e.At
		at = &t
	}
	got, err := one(ctx, r.s.db, "audit event", scanAudit, `
		INSERT INTO audit_events (request_id, actor_type, actor_id, type, payload, at)
		VALUES ($1, $2, $3, $4, $5::jsonb, COALESCE($6, clock_timestamp())) RETURNING `+auditCols,
		e.RequestID, e.ActorType, e.ActorID, e.Type, string(payload), at)
	if err != nil {
		return err
	}
	*e = got
	return nil
}

func (r auditRepo) ListByRequest(ctx context.Context, requestID string) ([]domain.AuditEvent, error) {
	if !validUUID(requestID) {
		return []domain.AuditEvent{}, nil
	}
	return collect(ctx, r.s.db, "audit event", scanAudit,
		`SELECT `+auditCols+` FROM audit_events WHERE request_id = $1 ORDER BY id`, requestID)
}

func (r auditRepo) List(ctx context.Context, f store.AuditFilter) ([]domain.AuditEvent, error) {
	var since *time.Time
	if !f.Since.IsZero() {
		since = &f.Since
	}
	return collect(ctx, r.s.db, "audit event", scanAudit, `SELECT `+auditCols+` FROM audit_events
		WHERE ($1 = '' OR (right($1, 1) = '.' AND starts_with(type, $1)) OR type = $1)
		  AND ($2::timestamptz IS NULL OR at >= $2)
		ORDER BY id DESC LIMIT $3 OFFSET $4`, f.Type, since, limitOr(f.Limit, 100), max(f.Offset, 0))
}

// ---------------- chat ----------------

type chatRepo struct{ s *Store }

const chatCols = `id, session_id, COALESCE(user_id::text, ''), role, content, tool_calls, tool_call_id, created_at`

func scanChat(r scanner) (domain.ChatMessage, error) {
	var m domain.ChatMessage
	var calls []byte
	err := r.Scan(&m.ID, &m.SessionID, &m.UserID, &m.Role, &m.Content, &calls, &m.ToolCallID, &m.CreatedAt)
	if len(calls) > 0 {
		m.ToolCalls = json.RawMessage(calls)
	}
	m.CreatedAt = utc(m.CreatedAt)
	return m, err
}

func (r chatRepo) Append(ctx context.Context, m *domain.ChatMessage) error {
	var calls any
	if len(m.ToolCalls) > 0 {
		if !json.Valid(m.ToolCalls) {
			return fmt.Errorf("chat: %w: tool_calls is not valid JSON", domain.ErrValidation)
		}
		calls = string(m.ToolCalls)
	}
	got, err := one(ctx, r.s.db, "chat message", scanChat, `
		INSERT INTO chat_messages (session_id, user_id, role, content, tool_calls, tool_call_id, created_at)
		VALUES ($1, $2, $3, $4, $5::jsonb, $6, clock_timestamp()) RETURNING `+chatCols,
		m.SessionID, nullStr(m.UserID), m.Role, m.Content, calls, m.ToolCallID)
	if err != nil {
		return err
	}
	*m = got
	return nil
}

func (r chatRepo) ListBySession(ctx context.Context, sessionID string, limit int) ([]domain.ChatMessage, error) {
	var lim any
	if limit > 0 {
		lim = limit
	}
	return collect(ctx, r.s.db, "chat message", scanChat, `SELECT * FROM (
			SELECT `+chatCols+` FROM chat_messages WHERE session_id = $1 ORDER BY id DESC LIMIT $2
		) last ORDER BY id`, sessionID, lim)
}

// ---------------- spend ----------------

type spendRepo struct{ s *Store }

// MonthBounds returns [start, end) of the Asia/Singapore calendar month containing at, in UTC.
func MonthBounds(at time.Time) (time.Time, time.Time) {
	l := at.In(store.SingaporeLocation)
	start := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, store.SingaporeLocation)
	return start.UTC(), start.AddDate(0, 1, 0).UTC()
}

func (r spendRepo) MonthToDate(ctx context.Context, at time.Time) (domain.Cents, error) {
	start, end := MonthBounds(at)
	var c domain.Cents
	err := r.s.db.QueryRow(ctx, `SELECT COALESCE(SUM(COALESCE(final_cents, quoted_cents)), 0) FROM payments
		WHERE status = 'completed' AND completed_at >= $1 AND completed_at < $2`, start, end).Scan(&c)
	return c, mapErr("spend", err)
}

func (r spendRepo) ByMonth(ctx context.Context, months int, now time.Time) ([]domain.MonthSpend, error) {
	if months <= 0 {
		months = 12
	}
	_, end := MonthBounds(now)
	firstStart, _ := MonthBounds(end.In(store.SingaporeLocation).AddDate(0, -months, 0))
	rows, err := collect(ctx, r.s.db, "spend", func(sc scanner) (domain.MonthSpend, error) {
		var m domain.MonthSpend
		err := sc.Scan(&m.Month, &m.SpendCents, &m.Orders)
		return m, err
	}, `SELECT to_char(completed_at AT TIME ZONE 'Asia/Singapore', 'YYYY-MM') AS month,
			COALESCE(SUM(COALESCE(final_cents, quoted_cents)), 0), COUNT(DISTINCT request_id)::int
		FROM payments WHERE status = 'completed' AND completed_at >= $1 AND completed_at < $2
		GROUP BY 1`, firstStart, end)
	if err != nil {
		return nil, err
	}
	return FillMonths(rows, months, now), nil
}

// FillMonths returns exactly `months` entries ending at the month containing now (oldest first),
// taking values from got and zero for missing months.
func FillMonths(got []domain.MonthSpend, months int, now time.Time) []domain.MonthSpend {
	byMonth := map[string]domain.MonthSpend{}
	for _, m := range got {
		byMonth[m.Month] = m
	}
	l := now.In(store.SingaporeLocation)
	cur := time.Date(l.Year(), l.Month(), 1, 0, 0, 0, 0, store.SingaporeLocation)
	out := make([]domain.MonthSpend, 0, months)
	for i := months - 1; i >= 0; i-- {
		key := cur.AddDate(0, -i, 0).Format("2006-01")
		m := byMonth[key]
		m.Month = key
		out = append(out, m)
	}
	return out
}
