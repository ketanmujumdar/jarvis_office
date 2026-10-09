package agents

import (
	"context"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
)

// ReapAdapter searches the Reap agentic product index (search -> details). Per-vendor searches use
// merchantPreference {ONLY, vendor.Domain} and keep only products whose merchant.name equals the
// vendor's ReapMerchantName. Open searches keep only products from allowed vendors.
type ReapAdapter struct {
	Client   reap.Client
	Country  string // default "SG"
	Currency string // default "SGD"
}

var _ VendorAdapter = (*ReapAdapter)(nil)

// NewReapAdapter returns an adapter with SG/SGD defaults.
func NewReapAdapter(c reap.Client) *ReapAdapter {
	return &ReapAdapter{Client: c, Country: "SG", Currency: domain.Currency}
}

func (a *ReapAdapter) Name() string           { return "reap" }
func (a *ReapAdapter) SupportsCheckout() bool { return true }

// ErrVendorNotSearchable is returned for vendors that cannot be searched (not allowed, or no known
// Reap merchant name).
var ErrVendorNotSearchable = errors.New("vendor not searchable")

const detailsBatch = 10

// Search runs one Reap search and resolves default variants via product details.
func (a *ReapAdapter) Search(ctx context.Context, q SearchQuery) ([]domain.Offer, error) {
	query := cleanQuery(q.Query)
	if query == "" {
		return nil, fmt.Errorf("%w: empty search query", domain.ErrValidation)
	}
	limit := q.Limit
	if limit <= 0 {
		limit = 5
	}
	req := reap.SearchRequest{
		Query:      query,
		Context:    &reap.SearchContext{Country: or(a.Country, "SG"), Currency: or(a.Currency, domain.Currency)},
		Filters:    &reap.SearchFilters{Availability: "AVAILABLE_ONLY"},
		Pagination: &reap.Pagination{Limit: min(50, limit*2)}, // over-fetch: some results get filtered out
	}
	if q.MaxUnitPriceCents > 0 {
		req.Filters.Price = &reap.PriceFilter{Max: q.MaxUnitPriceCents.String()}
	}

	// byName maps merchant.name -> vendor for attribution.
	byName := map[string]domain.Vendor{}
	if q.Open {
		for _, v := range q.Allowed {
			// "---" (Anker) is only trustworthy under merchantPreference ONLY.
			if v.Allowed && v.ReapMerchantName != "" && v.ReapMerchantName != "---" {
				byName[v.ReapMerchantName] = v
			}
		}
		if len(byName) == 0 {
			return nil, nil
		}
	} else {
		v := q.Vendor
		if !v.Allowed || v.ReapMerchantName == "" || v.Domain == "" {
			return nil, fmt.Errorf("%w: %s", ErrVendorNotSearchable, v.Domain)
		}
		req.MerchantPreference = &reap.MerchantPreference{Mode: reap.MerchantOnly, MerchantName: v.Domain}
		byName[v.ReapMerchantName] = v
	}

	res, err := a.Client.SearchProducts(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("reap search %q: %w", query, err)
	}

	type kept struct {
		p reap.SearchProduct
		v domain.Vendor
	}
	var keep []kept
	seen := map[string]bool{}
	for _, p := range res.Products {
		v, ok := byName[p.Merchant.Name]
		if !ok || p.ID == "" || seen[p.ID] {
			continue
		}
		if p.Available != nil && !*p.Available {
			continue
		}
		seen[p.ID] = true
		keep = append(keep, kept{p, v})
		if len(keep) == limit {
			break
		}
	}
	if len(keep) == 0 {
		return []domain.Offer{}, nil
	}

	// Resolve default variants. Details failures fall back to the search preview variant.
	details := map[string]reap.ProductDetails{}
	for i := 0; i < len(keep); i += detailsBatch {
		ids := make([]string, 0, detailsBatch)
		for _, k := range keep[i:min(i+detailsBatch, len(keep))] {
			ids = append(ids, k.p.ID)
		}
		d, err := a.Client.GetProductDetails(ctx, ids)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			continue
		}
		for _, pd := range d.Products {
			details[pd.ID] = pd
		}
	}

	offers := make([]domain.Offer, 0, len(keep))
	for _, k := range keep {
		o, ok := toOffer(k.p, details[k.p.ID], k.v, q.CatalogUnit)
		if ok {
			offers = append(offers, o)
		}
	}
	return offers, nil
}

func toOffer(p reap.SearchProduct, d reap.ProductDetails, v domain.Vendor, catalogUnit string) (domain.Offer, bool) {
	var (
		variantID, variantName string
		price                  reap.Money
		available              = true
	)
	switch {
	case d.ID != "" && d.DefaultVariant.ID != "":
		variantID, variantName, price = d.DefaultVariant.ID, d.DefaultVariant.Name, d.DefaultVariant.Price
		if d.DefaultVariant.Available != nil {
			available = *d.DefaultVariant.Available
		}
	case p.PreviewVariant != nil && p.PreviewVariant.ID != "":
		variantID, variantName, price = p.PreviewVariant.ID, p.PreviewVariant.Name, p.PreviewVariant.Price
		if p.PreviewVariant.Available != nil {
			available = *p.PreviewVariant.Available
		}
	default:
		return domain.Offer{}, false
	}
	if !available || price.Amount <= 0 {
		return domain.Offer{}, false
	}
	image := p.ImageURL
	if image == "" && len(d.Media) > 0 {
		image = d.Media[0].URL
	}
	if strings.EqualFold(variantName, "default title") || strings.EqualFold(variantName, "default") {
		variantName = ""
	}
	vid := v.ID
	cur := strings.ToUpper(price.Currency)
	if cur == "" {
		cur = domain.Currency
	}
	return domain.Offer{
		VendorID:       &vid,
		MerchantName:   p.Merchant.Name,
		ReapProductID:  p.ID,
		ReapVariantID:  variantID,
		Title:          p.Name,
		VariantName:    variantName,
		ImageURL:       image,
		UnitPriceCents: domain.CentsFromFloat(price.Amount),
		Currency:       cur,
		PackSize:       UnitsPerVariant(p.Name+" "+variantName, catalogUnit),
		Available:      true,
		Description:    ShortDescription(d.Description, 300),
	}, true
}

func or(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

var (
	htmlTagRe = regexp.MustCompile(`<[^>]*>`)
	spaceRe   = regexp.MustCompile(`[\s\x{00a0}]+`)
)

// ShortDescription turns a merchant's (often HTML) product description into one line of plain
// text of at most max runes, cut at a word boundary.
func ShortDescription(s string, max int) string {
	s = html.UnescapeString(htmlTagRe.ReplaceAllString(s, " "))
	s = strings.TrimSpace(spaceRe.ReplaceAllString(s, " "))
	r := []rune(s)
	if max <= 0 || len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
