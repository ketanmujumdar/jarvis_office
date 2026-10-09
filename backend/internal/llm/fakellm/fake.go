// Package fakellm is a deterministic, offline llm.Client for tests and FAKES=true demos.
//
// It answers in three ways, in this order:
//  1. Scripted: responses queued with Push/PushError are returned first (FIFO).
//  2. Structured tasks: requests whose ResponseSchemaName is llm.SchemaNameLineItems or
//     llm.SchemaNameOfferMatches are answered by a rule-based parser / matcher, so the whole
//     system works without an OpenAI key.
//  3. Agent turns: requests with tools get rule-based tool calls (create_order_request,
//     get_request_status, list_addresses, confirm_order, cancel_request) and short replies.
//
// Every request is recorded (Requests) so tests can assert on prompts and tool definitions.
package fakellm

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Model is the model name reported by the fake.
const Model = "fake-llm"

type scripted struct {
	resp llm.ChatResponse
	err  error
}

// Fake implements llm.Client. The zero value is not usable; call New.
type Fake struct {
	mu       sync.Mutex
	queue    []scripted
	requests []llm.ChatRequest
	callSeq  int
}

var _ llm.Client = (*Fake)(nil)

// New returns a rule-based fake with an empty script.
func New() *Fake { return &Fake{} }

// Push queues a scripted response (returned before any rule-based answer).
func (f *Fake) Push(resp llm.ChatResponse) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	if resp.Message.Role == "" {
		resp.Message.Role = llm.RoleAssistant
	}
	f.queue = append(f.queue, scripted{resp: resp})
	return f
}

// PushText queues an assistant text reply.
func (f *Fake) PushText(content string) *Fake {
	return f.Push(llm.ChatResponse{Message: llm.Message{Role: llm.RoleAssistant, Content: content}, FinishReason: "stop"})
}

// PushToolCall queues a single tool call with the given arguments (marshalled to JSON).
func (f *Fake) PushToolCall(name string, args any) *Fake {
	b, err := json.Marshal(args)
	if err != nil {
		panic(fmt.Sprintf("fakellm: marshal args: %v", err))
	}
	f.mu.Lock()
	f.callSeq++
	id := fmt.Sprintf("call_script_%d", f.callSeq)
	f.mu.Unlock()
	return f.Push(llm.ChatResponse{
		Message:      llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: id, Name: name, Arguments: b}}},
		FinishReason: "tool_calls",
	})
}

// PushError queues an error.
func (f *Fake) PushError(err error) *Fake {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.queue = append(f.queue, scripted{err: err})
	return f
}

// Requests returns a copy of every request received so far.
func (f *Fake) Requests() []llm.ChatRequest {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]llm.ChatRequest(nil), f.requests...)
}

// Pending reports how many scripted responses are still queued.
func (f *Fake) Pending() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.queue)
}

// Chat implements llm.Client.
func (f *Fake) Chat(ctx context.Context, req llm.ChatRequest) (llm.ChatResponse, error) {
	if err := ctx.Err(); err != nil {
		return llm.ChatResponse{}, err
	}
	f.mu.Lock()
	f.requests = append(f.requests, req)
	var s *scripted
	if len(f.queue) > 0 {
		s = &f.queue[0]
		f.queue = f.queue[1:]
	}
	f.mu.Unlock()
	if s != nil {
		if s.err != nil {
			return llm.ChatResponse{}, s.err
		}
		r := s.resp
		if r.Model == "" {
			r.Model = Model
		}
		return r, nil
	}

	var (
		msg llm.Message
		err error
	)
	switch {
	case req.ResponseSchemaName == llm.SchemaNameLineItems:
		msg, err = answerExtract(req)
	case req.ResponseSchemaName == llm.SchemaNameOfferMatches:
		msg, err = answerMatch(req)
	case len(req.ResponseSchema) > 0:
		msg = llm.Message{Role: llm.RoleAssistant, Content: "{}"}
	case len(req.Tools) > 0 && req.ToolChoice != llm.ToolChoiceNone:
		msg = f.answerAgent(req)
	default:
		msg = llm.Message{Role: llm.RoleAssistant, Content: replyWithoutTools(req)}
	}
	if err != nil {
		return llm.ChatResponse{}, err
	}
	finish := "stop"
	if len(msg.ToolCalls) > 0 {
		finish = "tool_calls"
	}
	return llm.ChatResponse{
		Message: msg, FinishReason: finish, Model: Model,
		Usage:   llm.Usage{InputTokens: approxTokens(req), OutputTokens: len(msg.Content) / 4},
		Latency: time.Millisecond,
	}, nil
}

func (f *Fake) nextCallID() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.callSeq++
	return fmt.Sprintf("call_fake_%d", f.callSeq)
}

func approxTokens(req llm.ChatRequest) int {
	n := 0
	for _, m := range req.Messages {
		n += len(m.Content) / 4
	}
	return n
}

func lastUser(req llm.ChatRequest) (llm.Message, bool) {
	for i := len(req.Messages) - 1; i >= 0; i-- {
		if req.Messages[i].Role == llm.RoleUser {
			return req.Messages[i], true
		}
	}
	return llm.Message{}, false
}

func replyWithoutTools(req llm.ChatRequest) string {
	if _, ok := lastUser(req); ok {
		return "Understood."
	}
	return "OK."
}

func answerExtract(req llm.ChatRequest) (llm.Message, error) {
	u, ok := lastUser(req)
	if !ok {
		return llm.Message{}, fmt.Errorf("fakellm: extract: no user message")
	}
	var in llm.ExtractInput
	if err := json.Unmarshal([]byte(u.Content), &in); err != nil {
		// Plain-text utterance without catalog.
		in = llm.ExtractInput{Utterance: u.Content}
	}
	items := ParseUtterance(in.Utterance, in.Catalog)
	type outItem struct {
		Description string  `json:"description"`
		Qty         *int    `json:"qty"`
		Urgency     string  `json:"urgency"`
		CatalogSKU  *string `json:"catalog_sku"`
	}
	out := struct {
		Items []outItem `json:"items"`
	}{Items: []outItem{}}
	for _, it := range items {
		oi := outItem{Description: it.Description, Qty: it.Qty, Urgency: string(it.Urgency)}
		if it.CatalogSKU != "" {
			sku := it.CatalogSKU
			oi.CatalogSKU = &sku
		}
		out.Items = append(out.Items, oi)
	}
	b, _ := json.Marshal(out)
	return llm.Message{Role: llm.RoleAssistant, Content: string(b)}, nil
}

func answerMatch(req llm.ChatRequest) (llm.Message, error) {
	u, ok := lastUser(req)
	if !ok {
		return llm.Message{}, fmt.Errorf("fakellm: match: no user message")
	}
	var in llm.MatchInput
	if err := json.Unmarshal([]byte(u.Content), &in); err != nil {
		return llm.Message{}, fmt.Errorf("fakellm: match: decode input: %w", err)
	}
	type outMatch struct {
		Index    int    `json:"index"`
		IsMatch  bool   `json:"is_match"`
		PackSize int    `json:"pack_size"`
		Reason   string `json:"reason"`
	}
	out := struct {
		Matches []outMatch `json:"matches"`
	}{Matches: []outMatch{}}
	for _, o := range in.Offers {
		ok, reason := RuleMatch(in.Target, o.Title+" "+o.VariantName)
		pack := o.PackSizeHint
		if pack < 1 {
			pack = llm.GuessPackSize(o.Title+" "+o.VariantName, in.Target.Unit)
		}
		out.Matches = append(out.Matches, outMatch{Index: o.Index, IsMatch: ok, PackSize: pack, Reason: reason})
	}
	b, _ := json.Marshal(out)
	return llm.Message{Role: llm.RoleAssistant, Content: string(b)}, nil
}
