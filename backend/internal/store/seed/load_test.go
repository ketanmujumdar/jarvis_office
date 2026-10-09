package seed

import (
	"context"
	"errors"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/pgtest"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/postgres"
)

type counts struct{ users, vendors, items, addresses int }

func countAll(t *testing.T, s store.Store) counts {
	t.Helper()
	ctx := context.Background()
	u, err := s.Users().List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	v, _ := s.Vendors().List(ctx, store.VendorFilter{})
	c, _ := s.Catalog().List(ctx, store.CatalogFilter{})
	a, _ := s.Addresses().List(ctx)
	return counts{len(u), len(v), len(c), len(a)}
}

// loadSuite runs the seed loader scenarios against any store.
func loadSuite(t *testing.T, newStore func(t *testing.T) store.Store) {
	ctx := context.Background()
	dir := seedDir(t)
	data, err := Parse(dir)
	if err != nil {
		t.Fatal(err)
	}

	t.Run("loads everything and is idempotent", func(t *testing.T) {
		s := newStore(t)
		if err := Load(ctx, s, dir, Options{}); err != nil {
			t.Fatal(err)
		}
		first := countAll(t, s)
		want := counts{len(data.Users), len(data.Vendors), len(data.Items), len(data.Addresses)}
		if first != want {
			t.Fatalf("counts %+v, want %+v", first, want)
		}
		paper, err := s.Catalog().GetBySKU(ctx, data.Items[0].SKU)
		if err != nil {
			t.Fatal(err)
		}
		if paper.MaxUnitPriceCents != domain.CentsFromFloat(data.Items[0].MaxUnitPriceSGD) ||
			len(paper.PreferredVendorIDs) != len(data.Items[0].PreferredVendors) || !paper.Active {
			t.Fatalf("catalog item: %+v", paper)
		}
		v0, err := s.Vendors().Get(ctx, paper.PreferredVendorIDs[0])
		if err != nil || v0.Domain != data.Items[0].PreferredVendors[0] {
			t.Fatalf("preferred vendor order: %+v %v", v0, err)
		}
		pol, err := s.Policy().Get(ctx)
		if err != nil || pol.PerOrderLimitCents != 50000 || pol.MonthlyBudgetCents != 300000 || pol.PriceDriftPct != 5 {
			t.Fatalf("policy: %+v %v", pol, err)
		}
		def, err := s.Addresses().GetDefault(ctx)
		if err != nil || !def.IsDefault {
			t.Fatalf("default address: %v", err)
		}
		pr, err := s.SystemPrompts().Get(ctx, domain.SystemPromptKeyAgent)
		if err != nil || pr.Content != data.SystemPrompt || pr.Version != 1 {
			t.Fatalf("prompt: %v", err)
		}

		if err := Load(ctx, s, dir, Options{}); err != nil {
			t.Fatal(err)
		}
		if again := countAll(t, s); again != first {
			t.Fatalf("second load changed counts: %+v vs %+v", again, first)
		}
		paper2, _ := s.Catalog().GetBySKU(ctx, data.Items[0].SKU)
		if paper2.ID != paper.ID {
			t.Fatal("ids must be stable across loads")
		}
		if err := Load(ctx, s, dir, Options{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
		if again := countAll(t, s); again != first {
			t.Fatalf("overwrite load changed counts: %+v vs %+v", again, first)
		}
	})

	t.Run("keeps admin edits unless overwriting", func(t *testing.T) {
		s := newStore(t)
		if err := Load(ctx, s, dir, Options{}); err != nil {
			t.Fatal(err)
		}
		admin, err := s.Users().GetByEmail(ctx, data.Users[len(data.Users)-1].Email)
		if err != nil {
			t.Fatal(err)
		}
		it, _ := s.Catalog().GetBySKU(ctx, data.Items[0].SKU)
		it.MaxUnitPriceCents = 1
		if err := s.Catalog().Update(ctx, &it); err != nil {
			t.Fatal(err)
		}
		gone, _ := s.Catalog().GetBySKU(ctx, data.Items[1].SKU)
		if err := s.Catalog().Delete(ctx, gone.ID); err != nil {
			t.Fatal(err)
		}
		pol, _ := s.Policy().Get(ctx)
		pol.PerOrderLimitCents = 12345
		if err := s.Policy().Update(ctx, &pol, admin.ID); err != nil {
			t.Fatal(err)
		}
		if _, err := s.SystemPrompts().Update(ctx, domain.SystemPromptKeyAgent, "custom prompt", admin.ID); err != nil {
			t.Fatal(err)
		}

		if err := Load(ctx, s, dir, Options{}); err != nil {
			t.Fatal(err)
		}
		it, _ = s.Catalog().GetBySKU(ctx, data.Items[0].SKU)
		if it.MaxUnitPriceCents != 1 {
			t.Fatal("default load clobbered a catalog edit")
		}
		if _, err := s.Catalog().GetBySKU(ctx, data.Items[1].SKU); !errors.Is(err, domain.ErrNotFound) {
			t.Fatal("default load resurrected a deleted catalog item")
		}
		if pol, _ = s.Policy().Get(ctx); pol.PerOrderLimitCents != 12345 {
			t.Fatal("default load clobbered the policy")
		}

		if err := Load(ctx, s, dir, Options{OverwritePolicy: true}); err != nil {
			t.Fatal(err)
		}
		if pol, _ = s.Policy().Get(ctx); pol.PerOrderLimitCents != 50000 {
			t.Fatal("OverwritePolicy did not restore the policy")
		}
		if it, _ = s.Catalog().GetBySKU(ctx, data.Items[0].SKU); it.MaxUnitPriceCents != 1 {
			t.Fatal("OverwritePolicy must not touch the catalog")
		}

		if err := Load(ctx, s, dir, Options{Overwrite: true}); err != nil {
			t.Fatal(err)
		}
		it, _ = s.Catalog().GetBySKU(ctx, data.Items[0].SKU)
		if it.MaxUnitPriceCents == 1 {
			t.Fatal("Overwrite did not restore the catalog item")
		}
		if _, err := s.Catalog().GetBySKU(ctx, data.Items[1].SKU); err != nil {
			t.Fatal("Overwrite should re-create seed items")
		}
		pr, _ := s.SystemPrompts().Get(ctx, domain.SystemPromptKeyAgent)
		if pr.Content != "custom prompt" || pr.Version != 2 {
			t.Fatalf("the system prompt must never be overwritten: %+v", pr)
		}
	})

	t.Run("invalid data writes nothing", func(t *testing.T) {
		s := newStore(t)
		bad := data
		bad.Items = append([]CatalogSeed(nil), data.Items...)
		bad.Items[0].PreferredVendors = []string{"nope.example"}
		if err := Apply(ctx, s, bad, Options{}); !errors.Is(err, domain.ErrValidation) {
			t.Fatalf("want validation error, got %v", err)
		}
		if c := countAll(t, s); c != (counts{}) {
			t.Fatalf("partial write: %+v", c)
		}
	})

	t.Run("missing dir", func(t *testing.T) {
		if err := Load(ctx, newStore(t), t.TempDir(), Options{}); err == nil {
			t.Fatal("expected error")
		}
	})
}

func TestLoadMemstore(t *testing.T) {
	loadSuite(t, func(*testing.T) store.Store { return memstore.New() })
}

func TestLoadPostgres(t *testing.T) {
	pgtest.DSN(t)
	loadSuite(t, func(t *testing.T) store.Store {
		pool := pgtest.NewDatabase(t)
		if _, err := store.Migrate(context.Background(), pool); err != nil {
			t.Fatal(err)
		}
		return postgres.New(pool)
	})
}
