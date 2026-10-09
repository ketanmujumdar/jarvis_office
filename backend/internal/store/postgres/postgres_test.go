package postgres_test

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/pgtest"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/storetest"
)

// newDB returns a migrated, empty, throw-away database (skips without TEST_DATABASE_URL).
func newDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

func TestConformance(t *testing.T) {
	pgtest.DSN(t)
	storetest.Run(t, func(t *testing.T) storetest.Env {
		pool := newDB(t)
		return storetest.Env{
			Store: postgres.New(pool),
			SetCompletedAt: func(t *testing.T, id string, at time.Time) {
				if _, err := pool.Exec(context.Background(), `UPDATE payments SET completed_at = $2 WHERE id = $1`, id, at); err != nil {
					t.Fatal(err)
				}
			},
		}
	})
}

// TestAuditAppendOnly checks the DB trigger: audit rows can be inserted but never changed or removed,
// while deleting the referenced request still nulls request_id (ON DELETE SET NULL).
func TestAuditAppendOnly(t *testing.T) {
	pool := newDB(t)
	ctx := context.Background()
	s := postgres.New(pool)
	u := domain.User{Name: "M", Email: "m@x.example", Role: domain.RoleManager}
	if err := s.Users().Upsert(ctx, &u); err != nil {
		t.Fatal(err)
	}
	r := domain.PurchaseRequest{RequesterID: u.ID, RawUtterance: "x"}
	if err := s.Requests().Create(ctx, &r); err != nil {
		t.Fatal(err)
	}
	ev := domain.AuditEvent{RequestID: &r.ID, ActorType: domain.ActorSystem, Type: "request.created"}
	if err := s.Audit().Append(ctx, &ev); err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`UPDATE audit_events SET type = 'tampered'`,
		`UPDATE audit_events SET payload = '{"x":1}'`,
		`DELETE FROM audit_events`,
	} {
		if _, err := pool.Exec(ctx, sql); err == nil {
			t.Errorf("%q should be rejected", sql)
		}
	}
	if _, err := pool.Exec(ctx, `DELETE FROM purchase_requests WHERE id = $1`, r.ID); err != nil {
		t.Fatalf("request delete should cascade-null audit rows: %v", err)
	}
	all, err := s.Audit().List(ctx, store.AuditFilter{})
	if err != nil || len(all) != 1 || all[0].RequestID != nil || all[0].Type != "request.created" {
		t.Fatalf("audit after request delete: %+v %v", all, err)
	}
}

func TestFillMonths(t *testing.T) {
	now := time.Date(2026, 1, 20, 0, 0, 0, 0, store.SingaporeLocation)
	tests := []struct {
		name   string
		got    []domain.MonthSpend
		months int
		want   []string
	}{
		{"year boundary", nil, 3, []string{"2025-11", "2025-12", "2026-01"}},
		{"single", []domain.MonthSpend{{Month: "2026-01", SpendCents: 5, Orders: 1}}, 1, []string{"2026-01"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			out := postgres.FillMonths(tt.got, tt.months, now)
			if len(out) != len(tt.want) {
				t.Fatalf("len %d", len(out))
			}
			for i, m := range out {
				if m.Month != tt.want[i] {
					t.Fatalf("%d: %s", i, m.Month)
				}
			}
		})
	}
	if out := postgres.FillMonths([]domain.MonthSpend{{Month: "2026-01", SpendCents: 5, Orders: 1}}, 1, now); out[0].SpendCents != 5 {
		t.Fatal("values not carried")
	}
}

func TestMonthBounds(t *testing.T) {
	tests := []struct {
		at         time.Time
		start, end string
	}{
		{time.Date(2026, 9, 30, 16, 30, 0, 0, time.UTC), "2026-09-30T16:00:00Z", "2026-10-31T16:00:00Z"},
		{time.Date(2026, 9, 30, 15, 59, 0, 0, time.UTC), "2026-08-31T16:00:00Z", "2026-09-30T16:00:00Z"},
		{time.Date(2026, 12, 31, 20, 0, 0, 0, time.UTC), "2026-12-31T16:00:00Z", "2027-01-31T16:00:00Z"},
	}
	for _, tt := range tests {
		s, e := postgres.MonthBounds(tt.at)
		if s.Format(time.RFC3339) != tt.start || e.Format(time.RFC3339) != tt.end {
			t.Errorf("%v: got %v..%v", tt.at, s, e)
		}
	}
}
