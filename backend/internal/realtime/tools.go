package realtime

import (
	"encoding/json"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Tool names. They equal the agents package constants (a test enforces it); realtime does not
// import agents to keep the dependency graph flat.
const (
	ToolCreateOrderRequest = "create_order_request"
	ToolGetRequestStatus   = "get_request_status"
	ToolListAddresses      = "list_addresses"
	ToolConfirmOrder       = "confirm_order"
	ToolCancelRequest      = "cancel_request"
)

// ToolDefinitions is the default JSON-schema tool set for the agent (text and voice). The api
// layer should prefer agents.ToolExecutor.Definitions() and falls back to this when the executor
// returns none; agents may also reuse it directly.
func ToolDefinitions() []llm.Tool {
	return []llm.Tool{
		{
			Name: ToolCreateOrderRequest,
			Description: "Start a purchase request for office supplies from what the user asked for. " +
				"Pass the user's words verbatim in utterance. Optionally include the items you understood; " +
				"omit qty when the user gave none or said 'the usual'. Returns the request id and status.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["utterance"],
  "properties": {
    "utterance": {"type": "string", "description": "The user's request, verbatim."},
    "items": {
      "type": "array",
      "description": "Optional pre-parsed items.",
      "items": {
        "type": "object",
        "additionalProperties": false,
        "required": ["description"],
        "properties": {
          "description": {"type": "string"},
          "qty": {"type": "integer", "minimum": 1},
          "urgency": {"type": "string", "enum": ["normal", "urgent"]}
        }
      }
    }
  }
}`),
		},
		{
			Name: ToolGetRequestStatus,
			Description: "Get the current state of a purchase request: status, line items, best offers, " +
				"policy decisions, approvals and payment status. Call it until the request is quoted or failed.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["request_id"],
  "properties": {
    "request_id": {"type": "string", "description": "The request id returned by create_order_request."}
  }
}`),
		},
		{
			Name:        ToolListAddresses,
			Description: "List the saved Singapore delivery addresses (id, label, address, is_default). Use it to ask the user which address to deliver to.",
			Parameters:  json.RawMessage(`{"type": "object", "additionalProperties": false, "properties": {}}`),
		},
		{
			Name: ToolConfirmOrder,
			Description: "Confirm a quoted request for delivery to the chosen address. Only call after the user " +
				"explicitly confirmed the items and picked an address. Returns the new status (approved or pending_approval).",
			Parameters: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["request_id", "address_id"],
  "properties": {
    "request_id": {"type": "string"},
    "address_id": {"type": "string", "description": "id from list_addresses that the user chose."}
  }
}`),
		},
		{
			Name:        ToolCancelRequest,
			Description: "Cancel a request when the user asks to. Not possible while a payment is in progress.",
			Parameters: json.RawMessage(`{
  "type": "object",
  "additionalProperties": false,
  "required": ["request_id"],
  "properties": {
    "request_id": {"type": "string"}
  }
}`),
		},
	}
}
