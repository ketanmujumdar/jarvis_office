package harness

import (
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

// Products the fake Reap sells, derived from the seed so the api's catalog matches.
//
// For every catalog item and each of its preferred vendors (index i) there is one product priced
// at 80% of the item's ceiling, +5% per step down the preference list. On top of that:
//   - three standing desks at ErgoTune (off-list item for PRD example 2), and
//   - a cheaper "leaky" copier paper from a merchant that is NOT allow-listed, returned even for
//     merchant-restricted searches; it must never become an offer.

// LeakyMerchantName is the non-allow-listed merchant that must be filtered out.
const LeakyMerchantName = "Stationery Pal"

// Desk prices (SGD) at ErgoTune; the cheapest one wins.
var DeskPrices = map[string]float64{
	"ErgoTune Standing Desk Lite":    399,
	"ErgoTune Standing Desk Classic": 449,
	"ErgoTune Standing Desk Pro":     549,
}

// SeedPrice returns the fake price for a catalog item at preference index i.
func SeedPrice(maxUnitSGD float64, i int) float64 {
	return round2(maxUnitSGD * 0.8 * (1 + 0.05*float64(i)))
}

// ProductsFromSeed builds the fake Reap catalog from parsed seed data.
func ProductsFromSeed(d seed.Data) []Product {
	merchant := map[string]string{}
	for _, v := range d.Vendors {
		merchant[v.Domain] = v.ReapMerchantName
	}
	var out []Product
	for _, it := range d.Items {
		match := []string{strings.ToLower(it.SearchQuery), strings.ToLower(it.Name)}
		for i, dom := range it.PreferredVendors {
			name := merchant[dom]
			if name == "" {
				continue
			}
			out = append(out, Product{
				Domain: dom, MerchantName: name, Name: it.Name + " (" + name + ")",
				PriceSGD: SeedPrice(it.MaxUnitPriceSGD, i), Match: match,
			})
		}
		if it.SKU == "a4-copier-paper-ream" {
			out = append(out, Product{
				Domain: "stationerypal.example", MerchantName: LeakyMerchantName, Name: "Cheap A4 Paper 500s",
				PriceSGD: 2.50, Match: match, Leaky: true,
			})
		}
	}
	for name, price := range DeskPrices {
		out = append(out, Product{
			Domain: "ergotune.com", MerchantName: merchant["ergotune.com"], Name: name, PriceSGD: price,
			Match: []string{"standing desk", "sit-stand desk", "height adjustable desk"},
		})
	}
	return out
}

// CatalogItem returns the seed item with the given sku.
func CatalogItem(d seed.Data, sku string) (seed.CatalogSeed, bool) {
	for _, it := range d.Items {
		if it.SKU == sku {
			return it, true
		}
	}
	return seed.CatalogSeed{}, false
}
