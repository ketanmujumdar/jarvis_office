package agents

import (
	"context"
	"fmt"
	"hash/fnv"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// MockProduct is one canned product for MockAdapter.
type MockProduct struct {
	VendorDomain  string
	Title         string
	Keywords      []string // matched against query tokens; Title tokens are used too
	PriceCents    domain.Cents
	ETA           string
	ShippingCents domain.Cents
	Unavailable   bool
}

// MockAdapter is an in-memory VendorAdapter for offline demos (FAKES=true) and tests. It matches
// canned products by query tokens; with Synthesize it invents one deterministic offer per vendor
// when nothing matches, so every search in a demo finds something.
type MockAdapter struct {
	Products   []MockProduct
	Synthesize bool
	Latency    time.Duration // simulated per-search latency (respects ctx)
	// Fail makes searches for these vendor domains (or "open" for open searches) return an error.
	Fail map[string]error
	// Slow makes searches for these vendor domains block until ctx is done.
	Slow map[string]bool

	mu    sync.Mutex
	Calls []SearchQuery
}

var _ VendorAdapter = (*MockAdapter)(nil)

func (m *MockAdapter) Name() string           { return "mock" }
func (m *MockAdapter) SupportsCheckout() bool { return true }

// Search returns offers from Products for q.Vendor (or all allowed vendors when q.Open).
func (m *MockAdapter) Search(ctx context.Context, q SearchQuery) ([]domain.Offer, error) {
	m.mu.Lock()
	m.Calls = append(m.Calls, q)
	m.mu.Unlock()
	key := q.Vendor.Domain
	if q.Open {
		key = "open"
	}
	if m.Slow[key] {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if m.Latency > 0 {
		select {
		case <-time.After(m.Latency):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	if err := m.Fail[key]; err != nil {
		return nil, err
	}
	vendors := map[string]domain.Vendor{}
	if q.Open {
		for _, v := range q.Allowed {
			if v.Allowed {
				vendors[v.Domain] = v
			}
		}
	} else {
		vendors[q.Vendor.Domain] = q.Vendor
	}
	qt := tokenSet(normalize(q.Query))
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	var out []domain.Offer
	for i, p := range m.Products {
		v, ok := vendors[p.VendorDomain]
		if !ok || p.Unavailable || !mockMatches(qt, p) {
			continue
		}
		out = append(out, mockOffer(v, fmt.Sprintf("mock-%s-%d", p.VendorDomain, i), p, q.CatalogUnit))
		if len(out) == limit {
			break
		}
	}
	if len(out) == 0 && m.Synthesize && !q.Open {
		h := fnv.New32a()
		_, _ = h.Write([]byte(q.Vendor.Domain + "|" + strings.ToLower(q.Query)))
		price := domain.Cents(500 + h.Sum32()%9500) // S$5.00 .. S$99.99
		p := MockProduct{VendorDomain: q.Vendor.Domain, Title: strings.TrimSpace(q.Query), PriceCents: price, ETA: "2-3 business days"}
		out = append(out, mockOffer(q.Vendor, fmt.Sprintf("mock-%s-%x", q.Vendor.Domain, h.Sum32()), p, q.CatalogUnit))
	}
	if out == nil {
		out = []domain.Offer{}
	}
	return out, nil
}

func mockMatches(qt map[string]bool, p MockProduct) bool {
	pt := tokenSet(normalize(p.Title + " " + strings.Join(p.Keywords, " ")))
	hits := 0
	for t := range qt {
		if pt[t] {
			hits++
		}
	}
	return hits > 0 && hits*2 >= len(qt)
}

func mockOffer(v domain.Vendor, id string, p MockProduct, catalogUnit string) domain.Offer {
	vid := v.ID
	merchant := v.ReapMerchantName
	if merchant == "" {
		merchant = v.Name
	}
	return domain.Offer{
		VendorID:       &vid,
		MerchantName:   merchant,
		ReapProductID:  id,
		ReapVariantID:  id + "-v",
		Title:          p.Title,
		UnitPriceCents: p.PriceCents,
		Currency:       domain.Currency,
		PackSize:       UnitsPerVariant(p.Title, catalogUnit),
		ShippingCents:  p.ShippingCents,
		ETA:            p.ETA,
		Available:      true,
	}
}
