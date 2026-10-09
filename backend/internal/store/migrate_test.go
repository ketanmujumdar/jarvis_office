package store

import (
	"context"
	"sync"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/pgtest"
)

// TestMigrateIntegration runs against a real Postgres when TEST_DATABASE_URL is set
// (e.g. `make up` then TEST_DATABASE_URL=postgres://jarvis:jarvis@localhost:5432/jarvis_test?sslmode=disable).
// It works in a throw-away database created by pgtest.
func TestMigrateIntegration(t *testing.T) {
	pool := pgtest.NewDatabase(t)
	ctx := context.Background()
	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) == 0 {
		t.Fatal("expected migrations to apply")
	}
	again, err := Migrate(ctx, pool)
	if err != nil || len(again) != 0 {
		t.Fatalf("second run: %v %v", again, err)
	}
	for _, tbl := range []string{"users", "vendors", "catalog_items", "catalog_item_vendors", "policy_config",
		"addresses", "system_prompts", "enrollments", "purchase_requests", "line_items", "offers",
		"approvals", "payments", "audit_events", "chat_messages"} {
		var ok bool
		if err := pool.QueryRow(ctx, `SELECT to_regclass('public.'||$1) IS NOT NULL`, tbl).Scan(&ok); err != nil || !ok {
			t.Errorf("table %s missing (%v)", tbl, err)
		}
	}
	// audit_events is append-only.
	if _, err := pool.Exec(ctx, `INSERT INTO audit_events (actor_type, type) VALUES ('system', 'test.event')`); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `UPDATE audit_events SET type = 'x'`); err == nil {
		t.Error("audit update should fail")
	}
	if _, err := pool.Exec(ctx, `DELETE FROM audit_events`); err == nil {
		t.Error("audit delete should fail")
	}
}

// TestMigrateConcurrent checks the advisory lock: parallel migrators apply each file exactly once.
func TestMigrateConcurrent(t *testing.T) {
	pool := pgtest.NewDatabase(t)
	ctx := context.Background()
	var wg sync.WaitGroup
	results := make([][]string, 4)
	errs := make([]error, 4)
	for i := range results {
		wg.Add(1)
		go func() {
			defer wg.Done()
			results[i], errs[i] = Migrate(ctx, pool)
		}()
	}
	wg.Wait()
	total := 0
	for i := range results {
		if errs[i] != nil {
			t.Fatalf("migrator %d: %v", i, errs[i])
		}
		total += len(results[i])
	}
	var n int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM schema_migrations`).Scan(&n); err != nil {
		t.Fatal(err)
	}
	if total != n || n < 2 {
		t.Fatalf("applied %d across migrators, %d recorded", total, n)
	}
	var hasCol bool
	if err := pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM information_schema.columns
		WHERE table_name = 'payments' AND column_name = 'completed_at')`).Scan(&hasCol); err != nil || !hasCol {
		t.Fatalf("payments.completed_at missing (%v)", err)
	}
}
