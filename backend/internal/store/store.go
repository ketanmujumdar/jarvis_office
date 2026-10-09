// Package store defines the persistence contract (repository interfaces) and the migration runner.
// The Postgres implementation lives in store/postgres; the seed loader in store/seed.
//
// Rules for implementations:
//   - Get/Update/Delete of a missing row returns an error wrapping domain.ErrNotFound.
//   - Unique violations return an error wrapping domain.ErrConflict.
//   - Create methods fill server-generated fields (ID, CreatedAt, UpdatedAt) on the passed struct.
//   - List methods return an empty (non-nil) slice when nothing matches.
package store

import (
	"context"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// Store is the aggregate of all repositories. Obtain it from postgres.New(pool).
type Store interface {
	Users() UserRepo
	Vendors() VendorRepo
	Catalog() CatalogRepo
	Policy() PolicyRepo
	Addresses() AddressRepo
	SystemPrompts() SystemPromptRepo
	Enrollments() EnrollmentRepo
	Requests() RequestRepo
	LineItems() LineItemRepo
	Offers() OfferRepo
	Approvals() ApprovalRepo
	Payments() PaymentRepo
	Audit() AuditRepo
	Chat() ChatRepo
	Spend() SpendRepo

	// WithTx runs fn with a Store bound to one transaction. Commit on nil error, rollback otherwise.
	// Nested WithTx calls on a tx-bound Store reuse the outer transaction.
	WithTx(ctx context.Context, fn func(tx Store) error) error
	// Ping checks connectivity (used by /healthz).
	Ping(ctx context.Context) error
}

type UserRepo interface {
	Get(ctx context.Context, id string) (domain.User, error)
	GetByEmail(ctx context.Context, email string) (domain.User, error)
	List(ctx context.Context) ([]domain.User, error)
	// Upsert by email (seed loader).
	Upsert(ctx context.Context, u *domain.User) error
}

type VendorRepo interface {
	List(ctx context.Context, f VendorFilter) ([]domain.Vendor, error)
	Get(ctx context.Context, id string) (domain.Vendor, error)
	GetByDomain(ctx context.Context, domainName string) (domain.Vendor, error)
	// GetByMerchantName matches reap_merchant_name exactly. ErrNotFound if none.
	GetByMerchantName(ctx context.Context, merchantName string) (domain.Vendor, error)
	Create(ctx context.Context, v *domain.Vendor) error
	Update(ctx context.Context, v *domain.Vendor) error
	Delete(ctx context.Context, id string) error
	// Upsert by domain (seed loader).
	Upsert(ctx context.Context, v *domain.Vendor) error
}

type VendorFilter struct {
	AllowedOnly bool
	Category    string
}

type CatalogRepo interface {
	List(ctx context.Context, f CatalogFilter) ([]domain.CatalogItem, error) // ordered by category, name
	Get(ctx context.Context, id string) (domain.CatalogItem, error)
	GetBySKU(ctx context.Context, sku string) (domain.CatalogItem, error)
	// Create/Update also replace catalog_item_vendors from PreferredVendorIDs (order = rank).
	Create(ctx context.Context, it *domain.CatalogItem) error
	Update(ctx context.Context, it *domain.CatalogItem) error
	Delete(ctx context.Context, id string) error
	// Upsert by SKU (seed loader).
	Upsert(ctx context.Context, it *domain.CatalogItem) error
}

type CatalogFilter struct {
	ActiveOnly bool
	Category   string
	Query      string // case-insensitive substring over name, sku and aliases
}

type PolicyRepo interface {
	Get(ctx context.Context) (domain.PolicyConfig, error)
	// Update writes the single row (insert if missing). updatedBy may be "".
	Update(ctx context.Context, p *domain.PolicyConfig, updatedBy string) error
}

type AddressRepo interface {
	List(ctx context.Context) ([]domain.Address, error) // default first, then label
	Get(ctx context.Context, id string) (domain.Address, error)
	GetDefault(ctx context.Context) (domain.Address, error)
	// Create/Update: if IsDefault is true, clear the flag on all other rows in the same tx.
	Create(ctx context.Context, a *domain.Address) error
	Update(ctx context.Context, a *domain.Address) error
	Delete(ctx context.Context, id string) error // ErrConflict if referenced by a request
	// Upsert by label (seed loader).
	Upsert(ctx context.Context, a *domain.Address) error
}

type SystemPromptRepo interface {
	Get(ctx context.Context, key string) (domain.SystemPrompt, error)
	// Update sets content and increments version. Inserts with version 1 if missing.
	Update(ctx context.Context, key, content, updatedBy string) (domain.SystemPrompt, error)
	// SeedIfMissing inserts only when the key does not exist (seed loader must not clobber edits).
	SeedIfMissing(ctx context.Context, key, content string) error
}

type EnrollmentRepo interface {
	Create(ctx context.Context, e *domain.Enrollment) error
	Get(ctx context.Context, id string) (domain.Enrollment, error)
	GetByReapID(ctx context.Context, reapID string) (domain.Enrollment, error)
	// Latest returns the most recently created enrollment (any status). ErrNotFound if none.
	Latest(ctx context.Context) (domain.Enrollment, error)
	// LatestActive returns the most recent ACTIVE enrollment. ErrNotFound if none.
	LatestActive(ctx context.Context) (domain.Enrollment, error)
	// ListPending returns REQUIRES_ACTION enrollments, newest first.
	ListPending(ctx context.Context) ([]domain.Enrollment, error)
	UpdateStatus(ctx context.Context, id string, status domain.EnrollmentStatus, nextActionURL string) error
}

type RequestRepo interface {
	Create(ctx context.Context, r *domain.PurchaseRequest) error
	Get(ctx context.Context, id string) (domain.PurchaseRequest, error)
	List(ctx context.Context, f RequestFilter) ([]domain.PurchaseRequest, error) // newest first
	// Update writes all mutable fields except status (address_id, totals, decision, failure_reason,
	// confirmed_by/at) and bumps updated_at.
	Update(ctx context.Context, r *domain.PurchaseRequest) error
	// TransitionStatus atomically moves from -> to (UPDATE ... WHERE id=$1 AND status=$from).
	// Returns ErrInvalidTransition if the state machine forbids it, ErrConflict if the current
	// status is not `from` (lost race), ErrNotFound if missing.
	TransitionStatus(ctx context.Context, id string, from, to domain.RequestStatus, failureReason string) error
	// Detail loads the request with line items, offers, approvals, payments and address.
	Detail(ctx context.Context, id string) (domain.RequestDetail, error)
}

type RequestFilter struct {
	Statuses    []domain.RequestStatus
	RequesterID string
	Limit       int // 0 = default 50
	Offset      int
}

type LineItemRepo interface {
	// CreateBatch inserts items for a request, assigning Position by slice order.
	CreateBatch(ctx context.Context, items []*domain.LineItem) error
	ListByRequest(ctx context.Context, requestID string) ([]domain.LineItem, error)
	Get(ctx context.Context, id string) (domain.LineItem, error)
	// Update writes qty, catalog_item_id, policy_decision, reasons, selected_offer_id, description, urgency.
	Update(ctx context.Context, li *domain.LineItem) error
	Delete(ctx context.Context, id string) error
}

type OfferRepo interface {
	// ReplaceForLineItem deletes existing offers of the line item and inserts the new ones
	// (clearing selected_offer_id first). Fills IDs.
	ReplaceForLineItem(ctx context.Context, lineItemID string, offers []*domain.Offer) error
	ListByLineItem(ctx context.Context, lineItemID string) ([]domain.Offer, error) // by rank
	ListByRequest(ctx context.Context, requestID string) ([]domain.Offer, error)
	Get(ctx context.Context, id string) (domain.Offer, error)
}

type ApprovalRepo interface {
	// Create fails with ErrConflict if the request already has a pending approval.
	Create(ctx context.Context, a *domain.Approval) error
	Get(ctx context.Context, id string) (domain.Approval, error)
	List(ctx context.Context, f ApprovalFilter) ([]domain.Approval, error) // newest first
	ListByRequest(ctx context.Context, requestID string) ([]domain.Approval, error)
	// Decide sets status/approver/comment/decided_at only if currently pending; else ErrConflict.
	Decide(ctx context.Context, id string, status domain.ApprovalStatus, approverID, comment string) (domain.Approval, error)
}

type ApprovalFilter struct {
	Status domain.ApprovalStatus // "" = all
	Limit  int
	Offset int
}

type PaymentRepo interface {
	Create(ctx context.Context, p *domain.Payment) error
	Get(ctx context.Context, id string) (domain.Payment, error)
	GetByCheckoutID(ctx context.Context, reapCheckoutID string) (domain.Payment, error)
	ListByRequest(ctx context.Context, requestID string) ([]domain.Payment, error)
	// ListInFlight returns payments in requires_action or processing (for the poller on boot).
	ListInFlight(ctx context.Context) ([]domain.Payment, error)
	// Update writes all mutable fields and bumps updated_at.
	Update(ctx context.Context, p *domain.Payment) error
}

type AuditRepo interface {
	// Append inserts; fills ID and At (if zero). Never updates.
	Append(ctx context.Context, e *domain.AuditEvent) error
	ListByRequest(ctx context.Context, requestID string) ([]domain.AuditEvent, error) // oldest first
	List(ctx context.Context, f AuditFilter) ([]domain.AuditEvent, error)             // newest first
}

type AuditFilter struct {
	Type   string // exact or prefix ending in "." e.g. "checkout."
	Since  time.Time
	Limit  int
	Offset int
}

type ChatRepo interface {
	Append(ctx context.Context, m *domain.ChatMessage) error
	ListBySession(ctx context.Context, sessionID string, limit int) ([]domain.ChatMessage, error) // oldest first, last `limit`
}

type SpendRepo interface {
	// MonthToDate returns completed spend (sum of payments.final_cents with status completed) in the
	// calendar month (Asia/Singapore) containing `at`.
	MonthToDate(ctx context.Context, at time.Time) (domain.Cents, error)
	// ByMonth returns the last `months` calendar months (oldest first, including empty months).
	ByMonth(ctx context.Context, months int, now time.Time) ([]domain.MonthSpend, error)
}

// SingaporeLocation is the budget-month timezone.
var SingaporeLocation = mustLoadLocation("Asia/Singapore")

func mustLoadLocation(name string) *time.Location {
	loc, err := time.LoadLocation(name)
	if err != nil {
		return time.FixedZone("SGT", 8*3600)
	}
	return loc
}
