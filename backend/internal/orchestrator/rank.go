package orchestrator

import (
	"sort"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// RankOffers orders offers for a line item by landed cost (unit*qty + shipping), then ETA
// presence, then vendor priority (lower first), then title for determinism. It sets Rank (1 = best)
// and LandedCostCents. Pure function. Owner: agent E.
//
// Pack sizes are normalised: an offer whose variant holds PackSize catalog units is bought
// ceil(qty/PackSize) times, so a 50-capsule box competes fairly with a 10-capsule box. Unavailable
// offers rank last. The input slice is not modified.
func RankOffers(offers []domain.Offer, qty int, vendorPriority map[string]int) []domain.Offer {
	out := make([]domain.Offer, len(offers))
	copy(out, offers)
	for i := range out {
		if out[i].PackSize < 1 {
			out[i].PackSize = 1
		}
		buy := agents.PurchaseQty(qty, out[i].PackSize)
		out[i].LandedCostCents = out[i].UnitPriceCents.MulQty(buy) + out[i].ShippingCents
	}
	prio := func(o domain.Offer) int {
		if o.VendorID != nil {
			if p, ok := vendorPriority[*o.VendorID]; ok {
				return p
			}
		}
		return 1 << 30
	}
	sort.SliceStable(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Available != b.Available {
			return a.Available
		}
		if a.LandedCostCents != b.LandedCostCents {
			return a.LandedCostCents < b.LandedCostCents
		}
		if (a.ETA != "") != (b.ETA != "") {
			return a.ETA != ""
		}
		if pa, pb := prio(a), prio(b); pa != pb {
			return pa < pb
		}
		if a.Title != b.Title {
			return a.Title < b.Title
		}
		return a.ReapVariantID < b.ReapVariantID
	})
	for i := range out {
		out[i].Rank = i + 1
	}
	return out
}
