package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// SchemaNameOfferMatches is the ResponseSchemaName used by MatchOffers. Fakes dispatch on it.
const SchemaNameOfferMatches = "offer_matches"

// MaxPackSize caps a pack size proposed by the model.
const MaxPackSize = 1000

// MatchTarget is the catalog item (or off-list line description) offers are matched against.
type MatchTarget struct {
	SKU               string       `json:"sku,omitempty"`
	Name              string       `json:"name"` // catalog name, or the line description when off-list
	Aliases           []string     `json:"aliases,omitempty"`
	Unit              string       `json:"unit,omitempty"` // what one catalog unit is, e.g. "ream (500 sheets)"
	MaxUnitPriceCents domain.Cents `json:"-"`              // 0 = no ceiling; never shown to the model
}

// TargetFromCatalog builds a MatchTarget from a catalog item.
func TargetFromCatalog(it domain.CatalogItem) MatchTarget {
	return MatchTarget{SKU: it.SKU, Name: it.Name, Aliases: it.Aliases, Unit: it.Unit, MaxUnitPriceCents: it.MaxUnitPriceCents}
}

// OfferCandidate is one vendor offer to judge.
type OfferCandidate struct {
	Title        string       `json:"title"`
	VariantName  string       `json:"variant_name,omitempty"`
	MerchantName string       `json:"merchant_name,omitempty"`
	Description  string       `json:"description,omitempty"` // short plain-text product summary
	PriceCents   domain.Cents `json:"-"`                     // price of one purchasable variant
}

// OfferMatch is the decision for one candidate (same order as the input).
type OfferMatch struct {
	Index    int    `json:"index"`
	IsMatch  bool   `json:"is_match"`
	PackSize int    `json:"pack_size"` // catalog units per purchasable variant, >= 1
	Reason   string `json:"reason,omitempty"`
	// Computed by code, not by the model:
	UnitPriceCents     domain.Cents `json:"unit_price_cents"`      // PriceCents / PackSize, rounded
	WithinMaxUnitPrice bool         `json:"within_max_unit_price"` // true if no ceiling
	// PackSizeHint is the deterministic GuessPackSize value, kept for auditing.
	PackSizeHint int `json:"pack_size_hint"`
}

// OfferMatchesSchema is the strict JSON schema of the matching output.
var OfferMatchesSchema = json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["matches"],
  "properties": {
    "matches": {
      "type": "array",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["index", "is_match", "pack_size", "reason"],
        "properties": {
          "index": {"type": "integer", "description": "Index of the offer in the input list."},
          "is_match": {"type": "boolean", "description": "True when the offer is the requested item (same product type; brand may differ)."},
          "pack_size": {"type": "integer", "description": "How many target units one purchase of this offer contains (1 if it is exactly one unit)."},
          "reason": {"type": "string", "description": "One short sentence."}
        }
      }
    }
  }
}`)

// MatchSystemPrompt instructs the model for offer matching.
const MatchSystemPrompt = `You check whether vendor product offers are the office-supply item that was requested.
The user message is JSON: {"target": {sku, name, aliases, unit}, "offers": [{index, title, variant_name, merchant_name, description, pack_size_hint}]}.
Titles are often just brand or model names; use variant_name and description to tell what the product is.
For every offer return {index, is_match, pack_size, reason}.
- is_match: true only if the offer is the same kind of product as the target (brand may differ). Accessories, refills for a different product, or unrelated items are false. Be strict and answer false when unsure: a product that merely shares words with the target is not a match (e.g. for "standing desk": a laptop table, float/bed table, desk converter or riser is false; for "A4 copier paper": letter-size, coloured or photo paper is false).
- pack_size: how many target units ("unit") one purchase of the offer contains. Example: target unit "pen" and offer "Pack of 12 pens" -> 12; target unit "ream (500 sheets)" and offer "A4 paper 500 sheets" -> 1; offer "carton of 5 reams" -> 5. pack_size_hint is a regex guess; override it when the title clearly says otherwise.
Return one entry per offer.`

// MatchInputOffer is one offer row of MatchInput.
type MatchInputOffer struct {
	Index        int    `json:"index"`
	Title        string `json:"title"`
	VariantName  string `json:"variant_name,omitempty"`
	MerchantName string `json:"merchant_name,omitempty"`
	Description  string `json:"description,omitempty"`
	PackSizeHint int    `json:"pack_size_hint"`
}

// MatchInput is the JSON payload of a matching call (exported for fakes).
type MatchInput struct {
	Target MatchTarget       `json:"target"`
	Offers []MatchInputOffer `json:"offers"`
}

type matchOutput struct {
	Matches []struct {
		Index    int    `json:"index"`
		IsMatch  bool   `json:"is_match"`
		PackSize int    `json:"pack_size"`
		Reason   string `json:"reason"`
	} `json:"matches"`
}

// MatchOffers asks the model which offers match the target and their pack sizes, then computes
// unit prices and the price-ceiling check in code. The result has one entry per offer, in input
// order; offers the model skipped are returned as non-matches with the hinted pack size.
func MatchOffers(ctx context.Context, c Client, target MatchTarget, offers []OfferCandidate) ([]OfferMatch, error) {
	if len(offers) == 0 {
		return nil, nil
	}
	in := MatchInput{Target: target}
	hints := make([]int, len(offers))
	for i, o := range offers {
		hints[i] = GuessPackSize(o.Title+" "+o.VariantName, target.Unit)
		in.Offers = append(in.Offers, MatchInputOffer{
			Index: i, Title: o.Title, VariantName: o.VariantName, MerchantName: o.MerchantName, Description: o.Description, PackSizeHint: hints[i],
		})
	}
	payload, err := json.Marshal(in)
	if err != nil {
		return nil, fmt.Errorf("match: encode input: %w", err)
	}
	resp, err := c.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: MatchSystemPrompt},
			{Role: RoleUser, Content: string(payload)},
		},
		ResponseSchema:     OfferMatchesSchema,
		ResponseSchemaName: SchemaNameOfferMatches,
		Metadata:           map[string]string{"task": SchemaNameOfferMatches},
	})
	if err != nil {
		return nil, fmt.Errorf("match: %w", err)
	}
	var out matchOutput
	if err := json.Unmarshal([]byte(resp.Message.Content), &out); err != nil {
		return nil, fmt.Errorf("match: decode model output: %w: %v", domain.ErrUpstream, err)
	}
	res := make([]OfferMatch, len(offers))
	for i := range offers {
		res[i] = OfferMatch{Index: i, PackSize: hints[i], PackSizeHint: hints[i], Reason: "not judged by model"}
	}
	seen := make(map[int]bool)
	for _, m := range out.Matches {
		if m.Index < 0 || m.Index >= len(offers) || seen[m.Index] {
			continue
		}
		seen[m.Index] = true
		r := &res[m.Index]
		r.IsMatch = m.IsMatch
		r.Reason = strings.TrimSpace(m.Reason)
		if m.PackSize >= 1 && m.PackSize <= MaxPackSize {
			r.PackSize = m.PackSize
		}
	}
	for i, o := range offers {
		r := &res[i]
		r.UnitPriceCents = UnitPrice(o.PriceCents, r.PackSize)
		r.WithinMaxUnitPrice = target.MaxUnitPriceCents <= 0 || r.UnitPriceCents <= target.MaxUnitPriceCents
	}
	return res, nil
}

// UnitPrice divides a variant price by its pack size, rounding half away from zero.
func UnitPrice(price domain.Cents, packSize int) domain.Cents {
	if packSize <= 1 {
		return price
	}
	return domain.Cents(math.Round(float64(price) / float64(packSize)))
}
