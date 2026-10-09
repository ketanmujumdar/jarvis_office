package orchestrator

import (
	"context"
	"errors"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/pgtest"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
)

// Integration: the PRD flows and the Reap failure paths against real Postgres (skipped unless TEST_DATABASE_URL is set).
func pgStore(t *testing.T) store.Store {
	t.Helper()
	pool := pgtest.NewDatabase(t)
	if _, err := store.Migrate(context.Background(), pool); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	return postgres.New(pool)
}

func TestPostgres_PaperAndCoffee_AutoApproved(t *testing.T) {
	pgtest.DSN(t)
	runPaperAndCoffee(t, newEnv(t, withStore(pgStore)))
}

func TestPostgres_StandingDesk_NeedsApproval(t *testing.T) {
	pgtest.DSN(t)
	runStandingDesk(t, newEnv(t, withStore(pgStore), deskProducts()))
}

func TestPostgres_PriceDrift_BackToApproval(t *testing.T) {
	pgtest.DSN(t)
	runPriceDriftBackToApproval(t, newEnv(t, withStore(pgStore)))
}

func TestPostgres_CheckoutFailedOrExpired(t *testing.T) {
	pgtest.DSN(t)
	runCheckoutFailedOrExpired(t, func(t *testing.T) *testEnv { return newEnv(t, withStore(pgStore)) })
}

// Two concurrent confirms that together exceed the monthly budget, on the real SQL repositories.
func TestPostgres_ConcurrentBudget(t *testing.T) {
	pgtest.DSN(t)
	runConcurrentBudget(t, newEnv(t, withStore(pgStore)))
}

// The unique partial index payments_open_per_merchant_idx refuses a second open payment.
func TestPostgres_OneOpenPaymentPerMerchant(t *testing.T) {
	pgtest.DSN(t)
	e := newEnv(t, withStore(pgStore))
	r := e.create("", agents.RequestedItem{Description: "printer paper", Qty: ptr(10)})
	e.confirm(r.ID)
	d := e.wantStatus(r.ID, domain.StatusAwaitingPayment)
	p0 := d.Payments[0]
	dup := domain.Payment{RequestID: r.ID, VendorID: p0.VendorID, MerchantName: p0.MerchantName, Status: domain.PaymentQuoting, IdempotencyKey: "dup-key"}
	if err := e.st.Payments().Create(e.ctx, &dup); !errors.Is(err, domain.ErrConflict) {
		t.Fatalf("second open payment: err = %v, want ErrConflict", err)
	}
}
