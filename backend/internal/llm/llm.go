// Package llm is the provider-neutral tool-calling chat interface. OpenAI first. Owner: agent D.
//
// The LLM only proposes (parse utterances, pick tool calls, phrase replies). Policy, approvals and
// payments never depend on model output alone.
package llm

import (
	"context"
	"encoding/json"
	"time"
)

// Role of a chat message.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ToolCall is a model-requested function call. Arguments is the raw JSON object string.
type ToolCall struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

// Message is one chat turn.
type Message struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // assistant only
	ToolCallID string     `json:"tool_call_id,omitempty"` // tool only
	Name       string     `json:"name,omitempty"`         // tool name for tool messages
}

// Tool is a function the model may call. Parameters is a JSON Schema object.
type Tool struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"`
}

// ToolChoice: "auto" (default), "none", "required", or a specific tool name.
type ToolChoice string

const (
	ToolChoiceAuto     ToolChoice = "auto"
	ToolChoiceNone     ToolChoice = "none"
	ToolChoiceRequired ToolChoice = "required"
)

// ChatRequest is one completion call.
type ChatRequest struct {
	Model      string     `json:"model,omitempty"` // "" = client default (OPENAI_MODEL)
	Messages   []Message  `json:"messages"`
	Tools      []Tool     `json:"tools,omitempty"`
	ToolChoice ToolChoice `json:"tool_choice,omitempty"`
	// ResponseSchema, if set, requests strict JSON output matching this schema (structured outputs).
	ResponseSchema     json.RawMessage `json:"response_schema,omitempty"`
	ResponseSchemaName string          `json:"response_schema_name,omitempty"`
	MaxOutputTokens    int             `json:"max_output_tokens,omitempty"`
	// Metadata is attached to traces (request_id etc.), not sent to the model.
	Metadata map[string]string `json:"-"`
}

// Usage is token accounting for tracing/cost.
type Usage struct {
	InputTokens  int `json:"input_tokens"`
	OutputTokens int `json:"output_tokens"`
}

// ChatResponse is the assistant turn.
type ChatResponse struct {
	Message      Message       `json:"message"` // Role=assistant; Content and/or ToolCalls
	FinishReason string        `json:"finish_reason"`
	Usage        Usage         `json:"usage"`
	Model        string        `json:"model"`
	Latency      time.Duration `json:"latency"`
}

// Client is a tool-calling chat model. Implementations: OpenAI (real) and a scripted fake
// (llm/fakellm, agent D) for offline tests.
type Client interface {
	Chat(ctx context.Context, req ChatRequest) (ChatResponse, error)
}
