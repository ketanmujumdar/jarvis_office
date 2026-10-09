package orchestrator

import (
	"context"
	"sync"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// OfferMatcher judges which search hits are actually the requested item and how many catalog
// units one purchase contains (llm.MatchOffers bound to a client). The model only proposes;
// filtering, pack-size bounds, ranking and policy stay in code.
type OfferMatcher func(ctx context.Context, target llm.MatchTarget, offers []llm.OfferCandidate) ([]llm.OfferMatch, error)

// LLMOfferMatcher binds llm.MatchOffers to a client.
func LLMOfferMatcher(c llm.Client) OfferMatcher {
	return func(ctx context.Context, t llm.MatchTarget, offers []llm.OfferCandidate) ([]llm.OfferMatch, error) {
		return llm.MatchOffers(ctx, c, t, offers)
	}
}

// matchTarget builds the matcher target for a line: the catalog item when known, otherwise the
// line's own description (off-list).
func matchTarget(li domain.LineItem, item *domain.CatalogItem) llm.MatchTarget {
	if item != nil {
		return llm.TargetFromCatalog(*item)
	}
	return llm.MatchTarget{Name: li.Description}
}

// filterRelevant drops offers the matcher says are not the requested item and applies its pack
// size. Without a matcher, or when the matcher fails, the offers are returned unchanged (search
// already constrained them to allow-listed merchants; policy still applies).
func (o *Impl) filterRelevant(ctx context.Context, requestID string, li domain.LineItem, item *domain.CatalogItem, offers []domain.Offer) []domain.Offer {
	if o.d.Matcher == nil || len(offers) == 0 {
		return offers
	}
	cands := make([]llm.OfferCandidate, len(offers))
	for i, of := range offers {
		cands[i] = llm.OfferCandidate{Title: of.Title, VariantName: of.VariantName, MerchantName: of.MerchantName, Description: of.Description, PriceCents: of.UnitPriceCents}
	}
	matches, err := o.d.Matcher(ctx, matchTarget(li, item), cands)
	if err != nil || len(matches) != len(offers) {
		o.log.Warn("offer matching unavailable; keeping unfiltered offers", "request_id", requestID, "line_item_id", li.ID, "error", err)
		return offers
	}
	out := make([]domain.Offer, 0, len(offers))
	var dropped []string
	for i, m := range matches {
		if !m.IsMatch {
			if len(dropped) < 20 {
				dropped = append(dropped, offers[i].Title+" ("+m.Reason+")")
			}
			continue
		}
		of := offers[i]
		if m.PackSize >= 1 && m.PackSize <= llm.MaxPackSize {
			of.PackSize = m.PackSize
		}
		out = append(out, of)
	}
	o.log.Info("offers matched", "request_id", requestID, "line_item_id", li.ID, "candidates", len(offers), "kept", len(out), "dropped", dropped)
	return out
}

// filterAllRelevant runs filterRelevant for every line in parallel.
func (o *Impl) filterAllRelevant(ctx context.Context, requestID string, lines []domain.LineItem, items map[string]*domain.CatalogItem, offers map[string][]domain.Offer) map[string][]domain.Offer {
	if o.d.Matcher == nil {
		return offers
	}
	out := make(map[string][]domain.Offer, len(offers))
	var mu sync.Mutex
	var wg sync.WaitGroup
	for _, li := range lines {
		wg.Add(1)
		go func(li domain.LineItem) {
			defer wg.Done()
			kept := o.filterRelevant(ctx, requestID, li, items[li.ID], offers[li.ID])
			mu.Lock()
			out[li.ID] = kept
			mu.Unlock()
		}(li)
	}
	wg.Wait()
	return out
}
