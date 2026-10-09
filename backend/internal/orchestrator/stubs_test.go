package orchestrator

import (
	"context"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

type stubParser struct {
	items []agents.ParsedItem
	err   error
}

func (s stubParser) Parse(ctx context.Context, utterance string, catalog []domain.CatalogItem) ([]agents.ParsedItem, error) {
	return s.items, s.err
}
