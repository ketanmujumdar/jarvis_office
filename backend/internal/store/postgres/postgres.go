// Package postgres implements store.Store on pgx/v5. Owner: agent A.
//
// Error mapping (see store.go): pgx.ErrNoRows and malformed ids -> domain.ErrNotFound,
// unique violations -> domain.ErrConflict, check/FK/not-null/format violations ->
// domain.ErrValidation (FK violations on delete -> domain.ErrConflict).
package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// DB is the subset of pgxpool.Pool / pgx.Tx used by repositories.
type DB interface {
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Begin(ctx context.Context) (pgx.Tx, error)
}

// Store implements store.Store.
type Store struct {
	pool *pgxpool.Pool
	db   DB // pool or tx
	inTx bool
}

var _ store.Store = (*Store)(nil)

// Open connects a pool to dsn.
func Open(ctx context.Context, dsn string) (*pgxpool.Pool, error) {
	return pgxpool.New(ctx, dsn)
}

// New wraps a pool.
func New(pool *pgxpool.Pool) *Store { return &Store{pool: pool, db: pool} }

func (s *Store) Ping(ctx context.Context) error { return s.pool.Ping(ctx) }

func (s *Store) WithTx(ctx context.Context, fn func(tx store.Store) error) error {
	if s.inTx {
		return fn(s)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return fn(&Store{pool: s.pool, db: tx, inTx: true})
	})
}

// atomic runs fn inside a transaction: the current one when the store is tx-bound, otherwise a new
// one. Used by repo methods that issue more than one statement.
func (s *Store) atomic(ctx context.Context, fn func(db DB) error) error {
	if s.inTx {
		return fn(s.db)
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error { return fn(tx) })
}

func (s *Store) Users() store.UserRepo                 { return userRepo{s} }
func (s *Store) Vendors() store.VendorRepo             { return vendorRepo{s} }
func (s *Store) Catalog() store.CatalogRepo            { return catalogRepo{s} }
func (s *Store) Policy() store.PolicyRepo              { return policyRepo{s} }
func (s *Store) Addresses() store.AddressRepo          { return addressRepo{s} }
func (s *Store) SystemPrompts() store.SystemPromptRepo { return systemPromptRepo{s} }
func (s *Store) Enrollments() store.EnrollmentRepo     { return enrollmentRepo{s} }
func (s *Store) Requests() store.RequestRepo           { return requestRepo{s} }
func (s *Store) LineItems() store.LineItemRepo         { return lineItemRepo{s} }
func (s *Store) Offers() store.OfferRepo               { return offerRepo{s} }
func (s *Store) Approvals() store.ApprovalRepo         { return approvalRepo{s} }
func (s *Store) Payments() store.PaymentRepo           { return paymentRepo{s} }
func (s *Store) Audit() store.AuditRepo                { return auditRepo{s} }
func (s *Store) Chat() store.ChatRepo                  { return chatRepo{s} }
func (s *Store) Spend() store.SpendRepo                { return spendRepo{s} }

// ---- helpers ----

type scanner interface {
	Scan(dest ...any) error
}

var uuidRE = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func validUUID(id string) bool { return uuidRE.MatchString(id) }

// notFound returns a wrapped ErrNotFound for an entity.
func notFound(entity, key string) error {
	return fmt.Errorf("%s %q: %w", entity, key, domain.ErrNotFound)
}

// mapErr translates pgx/Postgres errors into domain sentinels.
func mapErr(entity string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("%s: %w", entity, domain.ErrNotFound)
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		switch pe.Code {
		case "23505": // unique_violation
			return fmt.Errorf("%s: %w (%s)", entity, domain.ErrConflict, pe.ConstraintName)
		case "23503", "23514", "23502", "22P02", "22001", "22003", "22007", "22008":
			return fmt.Errorf("%s: %w: %s", entity, domain.ErrValidation, pe.Message)
		}
	}
	return fmt.Errorf("%s: %w", entity, err)
}

// mapDeleteErr is mapErr but FK violations mean "still referenced" (409).
func mapDeleteErr(entity string, err error) error {
	var pe *pgconn.PgError
	if errors.As(err, &pe) && pe.Code == "23503" {
		return fmt.Errorf("%s is still referenced: %w", entity, domain.ErrConflict)
	}
	return mapErr(entity, err)
}

// nullStr maps "" to SQL NULL (for optional uuid columns).
func nullStr(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// strOrEmpty dereferences an optional id.
func strOrEmpty(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

func utc(t time.Time) time.Time { return t.UTC() }

func utcPtr(t *time.Time) *time.Time {
	if t == nil {
		return nil
	}
	u := t.UTC()
	return &u
}

func emptyIfNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

// collect runs a query and scans every row with scan. It always returns a non-nil slice.
func collect[T any](ctx context.Context, db DB, entity string, scan func(scanner) (T, error), sql string, args ...any) ([]T, error) {
	rows, err := db.Query(ctx, sql, args...)
	if err != nil {
		return nil, mapErr(entity, err)
	}
	defer rows.Close()
	out := []T{}
	for rows.Next() {
		v, err := scan(rows)
		if err != nil {
			return nil, mapErr(entity, err)
		}
		out = append(out, v)
	}
	if err := rows.Err(); err != nil {
		return nil, mapErr(entity, err)
	}
	return out, nil
}

// one runs a single-row query; ErrNoRows becomes ErrNotFound.
func one[T any](ctx context.Context, db DB, entity string, scan func(scanner) (T, error), sql string, args ...any) (T, error) {
	v, err := scan(db.QueryRow(ctx, sql, args...))
	if err != nil {
		var zero T
		return zero, mapErr(entity, err)
	}
	return v, nil
}

// likePattern escapes LIKE metacharacters and wraps q in %...%.
func likePattern(q string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return "%" + r.Replace(q) + "%"
}

func limitOr(n, def int) int {
	if n <= 0 {
		return def
	}
	return n
}

func isNotFound(err error) bool { return errors.Is(err, domain.ErrNotFound) }
