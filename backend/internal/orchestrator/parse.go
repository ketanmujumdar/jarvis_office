package orchestrator

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/queue"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

type parsePayload struct {
	Items []agents.RequestedItem `json:"items,omitempty"`
}

// handleParse: utterance -> items (LLM, unless items were given) -> deterministic catalog match ->
// line items -> searching. Idempotent: does nothing unless the request is in parsing, and reuses
// line items already written by an earlier attempt.
func (o *Impl) handleParse(ctx context.Context, job queue.Job) error {
	r, err := o.d.Store.Requests().Get(ctx, job.RequestID)
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			return queue.Permanent(err)
		}
		return err
	}
	if r.Status != domain.StatusParsing {
		return nil
	}
	var p parsePayload
	if len(job.Payload) > 0 {
		_ = json.Unmarshal(job.Payload, &p)
	}

	existing, err := o.d.Store.LineItems().ListByRequest(ctx, r.ID)
	if err != nil {
		return err
	}
	lines := existing
	if len(existing) == 0 {
		catalog, err := o.d.Store.Catalog().List(ctx, store.CatalogFilter{ActiveOnly: true})
		if err != nil {
			return err
		}
		var parsed []agents.ParsedItem
		if len(p.Items) > 0 {
			for _, it := range p.Items {
				parsed = append(parsed, agents.ParsedItem{Description: it.Description, Qty: it.Qty, Urgency: it.Urgency})
			}
		} else {
			if o.d.Parser == nil {
				return queue.Permanent(o.failRequest(ctx, r.ID, "no parser configured"))
			}
			parsed, err = o.d.Parser.Parse(ctx, r.RawUtterance, catalog)
			if err != nil {
				if job.Attempt+1 >= 3 || errors.Is(err, domain.ErrValidation) {
					_ = o.failRequest(ctx, r.ID, "could not understand the request: "+err.Error())
					return queue.Permanent(err)
				}
				return err
			}
		}
		items := BuildLineItems(r.ID, parsed, catalog)
		if len(items) == 0 {
			_ = o.failRequest(ctx, r.ID, "no items found in the request")
			return nil
		}
		if err := o.d.Store.LineItems().CreateBatch(ctx, items); err != nil {
			return fmt.Errorf("create line items: %w", err)
		}
		lines = make([]domain.LineItem, 0, len(items))
		for _, it := range items {
			lines = append(lines, *it)
		}
		o.record(ctx, r.ID, domain.ActorAgent, "parser", audit.LineItemsParsed, map[string]any{"line_items": lines})
	}
	o.publish(events.LineItemsParsed, r.ID, map[string]any{"request_id": r.ID, "line_items": lines})
	if err := o.transition(ctx, r.ID, domain.StatusParsing, domain.StatusSearching, "", domain.ActorSystem, ""); err != nil {
		if errors.Is(err, domain.ErrConflict) || errors.Is(err, domain.ErrInvalidTransition) {
			return nil // cancelled or advanced concurrently
		}
		return err
	}
	_, err = o.d.Queue.Enqueue(ctx, queue.Job{JobID: "search:" + r.ID, Kind: queue.KindSearch, RequestID: r.ID})
	return err
}

// BuildLineItems turns parsed items into line items using the deterministic catalog matcher.
// Missing quantities default to the catalog default_qty (1 for off-list items). Items matching the
// same catalog item are merged (quantities added). Pure.
func BuildLineItems(requestID string, parsed []agents.ParsedItem, catalog []domain.CatalogItem) []*domain.LineItem {
	var out []*domain.LineItem
	byCatalog := map[string]*domain.LineItem{}
	for _, p := range parsed {
		m := agents.MatchCatalog(p.Description, p.CatalogSKU, catalog)
		qty := 0
		if p.Qty != nil && *p.Qty > 0 {
			qty = *p.Qty
		}
		urg := p.Urgency
		if urg != domain.UrgencyUrgent {
			urg = domain.UrgencyNormal
		}
		li := &domain.LineItem{RequestID: requestID, Description: p.Description, Urgency: urg, Reasons: []domain.Reason{}}
		if m.Item != nil {
			if qty == 0 {
				qty = max(1, m.Item.DefaultQty)
			}
			if prev, ok := byCatalog[m.Item.ID]; ok {
				prev.Qty += qty
				if urg == domain.UrgencyUrgent {
					prev.Urgency = urg
				}
				continue
			}
			id := m.Item.ID
			li.CatalogItemID = &id
			li.Description = m.Item.Name
			byCatalog[id] = li
		}
		if qty == 0 {
			qty = 1
		}
		li.Qty = qty
		li.Position = len(out)
		out = append(out, li)
	}
	return out
}
