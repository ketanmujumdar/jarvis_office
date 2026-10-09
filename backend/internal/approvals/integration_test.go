package approvals

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
)

// TestIntegrationPostgres exercises Create/Approve/Reject/Get against real Postgres when
// TEST_DATABASE_URL is set. It uses its own schema (approvals_it) so it never clobbers other
// packages' tests sharing the database, and skips while the postgres repos are still stubs.
func TestIntegrationPostgres(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	const schema = "approvals_it"

	admin, err := pgxpool.New(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	if _, err := admin.Exec(ctx, `DROP SCHEMA IF EXISTS `+schema+` CASCADE; CREATE SCHEMA `+schema); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = admin.Exec(context.Background(), `DROP SCHEMA IF EXISTS `+schema+` CASCADE`) })

	pcfg, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatal(err)
	}
	pcfg.ConnConfig.RuntimeParams["search_path"] = schema + ",public"
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	if _, err := store.Migrate(ctx, pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	st := postgres.New(pool)

	skipIfStub := func(err error) {
		t.Helper()
		if errors.Is(err, domain.ErrNotImplemented) {
			t.Skip("postgres repos not implemented yet")
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	requester := domain.User{Name: "Maya", Email: "maya-it@example.com", Role: domain.RoleManager}
	skipIfStub(st.Users().Upsert(ctx, &requester))
	approver := domain.User{Name: "Daniel", Email: "daniel-it@example.com", Role: domain.RoleApprover}
	skipIfStub(st.Users().Upsert(ctx, &approver))

	newQuoted := func() string {
		r := domain.PurchaseRequest{RequesterID: requester.ID, RawUtterance: "standing desk", Status: domain.StatusParsing, Currency: "SGD"}
		skipIfStub(st.Requests().Create(ctx, &r))
		skipIfStub(st.Requests().TransitionStatus(ctx, r.ID, domain.StatusParsing, domain.StatusSearching, ""))
		skipIfStub(st.Requests().TransitionStatus(ctx, r.ID, domain.StatusSearching, domain.StatusQuoted, ""))
		return r.ID
	}

	h := &recordingHandler{}
	svc := New(Deps{Store: st, Handler: h})

	// Approve path.
	rid := newQuoted()
	a, err := svc.Create(ctx, CreateInput{RequestID: rid, Kind: domain.ApprovalKindPolicy, Reasons: policyReasons, AmountCents: 120000})
	skipIfStub(err)
	if got, _ := st.Requests().Get(ctx, rid); got.Status != domain.StatusPendingApproval {
		t.Fatalf("status = %s", got.Status)
	}
	if _, err := svc.Create(ctx, CreateInput{RequestID: rid, Kind: domain.ApprovalKindPolicy, Reasons: policyReasons}); err == nil {
		t.Fatal("second Create on pending request should fail")
	}
	v, err := svc.Get(ctx, a.ID)
	skipIfStub(err)
	if v.Requester.ID != requester.ID || v.Approval.Status != domain.ApprovalPending || len(v.Approval.Reasons) != 1 {
		t.Errorf("view = %+v", v)
	}
	if _, err := svc.Approve(ctx, a.ID, requester, ""); !errors.Is(err, domain.ErrForbidden) {
		t.Errorf("manager approve err = %v", err)
	}
	got, err := svc.Approve(ctx, a.ID, approver, "ok")
	skipIfStub(err)
	if got.Status != domain.ApprovalApproved || got.ApproverID == nil || *got.ApproverID != approver.ID || got.DecidedAt == nil {
		t.Errorf("approved = %+v", got)
	}
	if _, err := svc.Reject(ctx, a.ID, approver, ""); !errors.Is(err, domain.ErrConflict) {
		t.Errorf("re-decide err = %v", err)
	}
	if len(h.calls) != 1 {
		t.Errorf("handler calls = %d", len(h.calls))
	}
	evs, err := st.Audit().ListByRequest(ctx, rid)
	skipIfStub(err)
	types := map[string]int{}
	for _, e := range evs {
		types[e.Type]++
	}
	if types[auditApprovalRequested] != 1 || types[auditApprovalDecided] != 1 {
		t.Errorf("audit types = %v", types)
	}

	// Reject path + list filter.
	rid2 := newQuoted()
	a2, err := svc.Create(ctx, CreateInput{RequestID: rid2, Kind: domain.ApprovalKindPolicy, Reasons: policyReasons})
	skipIfStub(err)
	if _, err := svc.Reject(ctx, a2.ID, approver, "no"); err != nil {
		t.Fatal(err)
	}
	rejected, err := svc.List(ctx, store.ApprovalFilter{Status: domain.ApprovalRejected})
	skipIfStub(err)
	if len(rejected) != 1 || rejected[0].Approval.ID != a2.ID {
		t.Errorf("rejected list = %+v", rejected)
	}
}
