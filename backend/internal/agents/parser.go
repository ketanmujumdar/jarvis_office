package agents

import (
	"context"
	"fmt"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// LLMParser implements Parser with one structured-output LLM call (llm.ExtractLineItems). The
// catalog is given as context so the model can suggest a SKU, but MatchCatalog has the final say.
type LLMParser struct {
	LLM llm.Client
}

var _ Parser = (*LLMParser)(nil)

// NewLLMParser returns a parser over c.
func NewLLMParser(c llm.Client) *LLMParser { return &LLMParser{LLM: c} }

// Parse calls the LLM and returns the items. Empty utterances are a validation error.
func (p *LLMParser) Parse(ctx context.Context, utterance string, catalog []domain.CatalogItem) ([]ParsedItem, error) {
	utterance = strings.TrimSpace(utterance)
	if utterance == "" {
		return nil, fmt.Errorf("%w: empty utterance", domain.ErrValidation)
	}
	items, err := llm.ExtractLineItems(ctx, p.LLM, llm.ExtractInput{Utterance: utterance, Catalog: llm.CatalogHints(catalog)})
	if err != nil {
		return nil, fmt.Errorf("parse utterance: %w", err)
	}
	out := make([]ParsedItem, 0, len(items))
	for _, it := range items {
		urg := it.Urgency
		if urg != domain.UrgencyUrgent {
			urg = domain.UrgencyNormal
		}
		out = append(out, ParsedItem{Description: it.Description, Qty: it.Qty, Urgency: urg, CatalogSKU: it.CatalogSKU})
	}
	return out, nil
}
