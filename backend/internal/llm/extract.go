package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// SchemaNameLineItems is the ResponseSchemaName used by ExtractLineItems. Fakes dispatch on it.
const SchemaNameLineItems = "line_items"

// MaxExtractQty caps a single extracted quantity; anything larger is treated as a parse error
// and left for the catalog default (nil).
const MaxExtractQty = 10000

// CatalogHint is the slice of a catalog item the model sees as context.
type CatalogHint struct {
	SKU        string   `json:"sku"`
	Name       string   `json:"name"`
	Aliases    []string `json:"aliases,omitempty"`
	Unit       string   `json:"unit,omitempty"`
	DefaultQty int      `json:"default_qty,omitempty"`
}

// CatalogHints converts active catalog items into hints (inactive items are skipped).
func CatalogHints(items []domain.CatalogItem) []CatalogHint {
	out := make([]CatalogHint, 0, len(items))
	for _, it := range items {
		if !it.Active {
			continue
		}
		out = append(out, CatalogHint{SKU: it.SKU, Name: it.Name, Aliases: it.Aliases, Unit: it.Unit, DefaultQty: it.DefaultQty})
	}
	return out
}

// ExtractInput is the JSON payload sent as the user message of an extraction call.
type ExtractInput struct {
	Utterance string        `json:"utterance"`
	Catalog   []CatalogHint `json:"catalog"`
}

// ExtractedItem is one requested item proposed by the model. CatalogSKU is only a suggestion:
// the deterministic matcher in agents has the final say.
type ExtractedItem struct {
	Description string         `json:"description"`
	Qty         *int           `json:"qty"`
	Urgency     domain.Urgency `json:"urgency"`
	CatalogSKU  string         `json:"catalog_sku,omitempty"`
}

// LineItemsSchema is the strict JSON schema of the extraction output.
var LineItemsSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["items"],
  "properties": {
    "items": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["description", "qty", "urgency", "catalog_sku"],
        "properties": {
          "description": {"type": "string", "description": "Short item description in the user's words, without the quantity."},
          "qty": {"type": ["integer", "null"], "description": "Number of catalog units; null when not stated or when the user says 'the usual'."},
          "urgency": {"type": "string", "enum": ["normal", "urgent"]},
          "catalog_sku": {"type": ["string", "null"], "description": "SKU of the matching catalog item, or null if none matches."}
        }
      }
    }
  }
}`)

// ExtractSystemPrompt instructs the model for line-item extraction.
const ExtractSystemPrompt = `You extract office-supply purchase line items for a Singapore office.
The user message is JSON: {"utterance": string, "catalog": [{sku, name, aliases, unit, default_qty}]}.
Return every distinct item the utterance asks to buy.
- description: the product itself in the user's words, without the quantity, recipient, purpose or delivery details (e.g. "standing desk", not "standing desk for the new hire").
- qty: the number of catalog units requested (convert "a dozen" to 12, "a couple" to 2). Use null when no quantity is stated or the user says "the usual".
- urgency: "urgent" only when the user signals urgency (urgent, asap, today, running out now); otherwise "normal".
- catalog_sku: the sku of the catalog item that clearly matches (by name or alias), else null. Never invent skus.
Ignore chit-chat. Do not add items the user did not ask for.`

type extractOutput struct {
	Items []struct {
		Description string  `json:"description"`
		Qty         *int    `json:"qty"`
		Urgency     string  `json:"urgency"`
		CatalogSKU  *string `json:"catalog_sku"`
	} `json:"items"`
}

// ExtractLineItems asks the model for structured line items and sanitises the result
// (empty descriptions dropped, bad quantities cleared, unknown SKUs cleared, urgency normalised).
func ExtractLineItems(ctx context.Context, c Client, in ExtractInput) ([]ExtractedItem, error) {
	if strings.TrimSpace(in.Utterance) == "" {
		return nil, fmt.Errorf("extract: empty utterance: %w", domain.ErrValidation)
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("extract: encode input: %w", err)
	}
	resp, err := c.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: ExtractSystemPrompt},
			{Role: RoleUser, Content: string(payload)},
		},
		ResponseSchema:     LineItemsSchema,
		ResponseSchemaName: SchemaNameLineItems,
		Metadata:           map[string]string{"task": SchemaNameLineItems},
	})
	if err != nil {
		return nil, fmt.Errorf("extract: %w", err)
	}
	var out extractOutput
	if err := json.Unmarshal([]byte(resp.Message.Content), &out); err != nil {
		return nil, fmt.Errorf("extract: decode model output: %w: %v", domain.ErrUpstream, err)
	}
	known := make(map[string]bool, len(in.Catalog))
	for _, h := range in.Catalog {
		known[h.SKU] = true
	}
	items := make([]ExtractedItem, 0, len(out.Items))
	for _, it := range out.Items {
		desc := strings.TrimSpace(it.Description)
		if desc == "" {
			continue
		}
		ei := ExtractedItem{Description: desc, Urgency: domain.UrgencyNormal}
		if it.Qty != nil && *it.Qty > 0 && *it.Qty <= MaxExtractQty {
			q := *it.Qty
			ei.Qty = &q
		}
		if strings.EqualFold(strings.TrimSpace(it.Urgency), string(domain.UrgencyUrgent)) {
			ei.Urgency = domain.UrgencyUrgent
		}
		if it.CatalogSKU != nil && known[*it.CatalogSKU] {
			ei.CatalogSKU = *it.CatalogSKU
		}
		items = append(items, ei)
	}
	return items, nil
}
