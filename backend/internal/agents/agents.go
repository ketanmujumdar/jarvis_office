// Package agents holds the LLM-facing pieces: the shared tool set (used by both the text agent and
// the realtime voice session), the utterance parser, the text chat agent, and the search agent with
// its pluggable VendorAdapter. Owner: agent E.
package agents

import (
	"context"
	"encoding/json"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Tool names exposed to the model (text and voice). Keep in sync with docs/openapi.yaml
// (POST /api/v1/agent/tools/{name}) and seed/system_prompt.md.
const (
	ToolCreateOrderRequest = "create_order_request"
	ToolGetRequestStatus   = "get_request_status"
	ToolListAddresses      = "list_addresses"
	ToolConfirmOrder       = "confirm_order"
	ToolCancelRequest      = "cancel_request"
)

// Tool argument payloads (JSON the model sends).
type CreateOrderRequestArgs struct {
	Utterance string          `json:"utterance"`       // the user's words, verbatim
	Items     []RequestedItem `json:"items,omitempty"` // optional pre-parsed items
}

type RequestedItem struct {
	Description string         `json:"description"`
	Qty         *int           `json:"qty,omitempty"` // nil = catalog default_qty
	Urgency     domain.Urgency `json:"urgency,omitempty"`
}

type RequestIDArgs struct {
	RequestID string `json:"request_id"`
}

type ConfirmOrderArgs struct {
	RequestID string `json:"request_id"`
	AddressID string `json:"address_id"`
}

// ToolContext identifies who is calling a tool.
type ToolContext struct {
	UserID string
	// Role of the caller. Tools that create, confirm or cancel purchases require manager or admin
	// (same as the REST endpoints). When empty, the role is looked up from the user record.
	Role      domain.Role
	SessionID string // text chat session id or realtime session id
	Channel   string // "text" | "voice"
}

// ToolExecutor runs a tool by name with raw JSON args and returns a JSON result for the model.
// Errors that the model should see (validation, not found) are returned as a JSON
// {"error": "..."} result with nil error; only infrastructure failures return error.
type ToolExecutor interface {
	Execute(ctx context.Context, tc ToolContext, name string, args json.RawMessage) (json.RawMessage, error)
	// Definitions returns the JSON-schema tool definitions given to the model.
	Definitions() []llm.Tool
}

// ChatInput is one user turn for the text agent.
type ChatInput struct {
	SessionID string // "" = new session (agent generates one)
	UserID    string
	Role      domain.Role // caller's role (forwarded to tools); "" = looked up
	Message   string
}

// ToolCallTrace is a tool call made while answering (for the UI transcript).
type ToolCallTrace struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"`
	Error     string          `json:"error,omitempty"`
}

// ChatOutput is the agent's reply.
type ChatOutput struct {
	SessionID  string          `json:"session_id"`
	Reply      string          `json:"reply"`
	RequestIDs []string        `json:"request_ids"` // requests created/touched this turn
	ToolCalls  []ToolCallTrace `json:"tool_calls"`
}

// TextAgent runs the tool-calling loop with the DB system prompt (key "agent") and persists the
// conversation in chat_messages.
type TextAgent interface {
	Chat(ctx context.Context, in ChatInput) (ChatOutput, error)
	History(ctx context.Context, sessionID string) ([]domain.ChatMessage, error)
}

// ParsedItem is the parser's output for one requested item, before catalog matching.
type ParsedItem struct {
	Description string         `json:"description"`
	Qty         *int           `json:"qty,omitempty"`
	Urgency     domain.Urgency `json:"urgency"`
	// CatalogSKU is the model's suggestion; the deterministic matcher has the final say.
	CatalogSKU string `json:"catalog_sku,omitempty"`
}

// Parser turns an utterance into items (LLM with structured output; catalog names given as context).
type Parser interface {
	Parse(ctx context.Context, utterance string, catalog []domain.CatalogItem) ([]ParsedItem, error)
}

// SearchQuery asks one vendor (or, with Open, the whole Reap index filtered to Allowed) for offers
// for one line item.
type SearchQuery struct {
	LineItemID        string
	Query             string // catalog search_query, else the line description
	Qty               int
	Vendor            domain.Vendor // the vendor searched with merchantPreference {ONLY, Vendor.Domain}
	MaxUnitPriceCents domain.Cents  // 0 = no filter
	Limit             int
	// Open searches without a merchant preference and keeps only products whose merchant.name
	// matches one of Allowed (by ReapMerchantName). Vendor is ignored when Open is true.
	Open    bool
	Allowed []domain.Vendor
	// CatalogUnit is the catalog item's unit (e.g. "box of 10 capsules"), used to normalise pack
	// sizes: Offer.PackSize is the number of catalog units one purchasable variant contains.
	CatalogUnit string
}

// VendorAdapter searches one kind of source. MVP has one implementation: Reap (search -> details,
// merchantPreference {ONLY, vendor.Domain}, keep products whose merchant.name == vendor.ReapMerchantName).
type VendorAdapter interface {
	Name() string
	// Search returns normalised offers (no IDs, no Rank). Offers are SGD, available only.
	Search(ctx context.Context, q SearchQuery) ([]domain.Offer, error)
	SupportsCheckout() bool
}
