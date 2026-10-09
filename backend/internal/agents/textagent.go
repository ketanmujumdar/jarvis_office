package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// DefaultSystemPrompt is used only if the DB has no "agent" prompt.
const DefaultSystemPrompt = "You are Jarvis, the office procurement assistant for a Singapore office. Act only through your tools, " +
	"read back line items and totals before confirming, ask which delivery address to use, and never say an order is placed until its status is ordered."

// ChatAgent implements TextAgent: a bounded tool-calling loop over llm.Client with the DB system
// prompt, persisting every message in chat_messages.
type ChatAgent struct {
	LLM          llm.Client
	Store        store.Store
	Tools        ToolExecutor
	Audit        audit.Logger // optional
	Model        string       // "" = client default
	MaxSteps     int          // max LLM calls per turn, default 6
	HistoryLimit int          // messages of history sent to the model, default 40
	Clock        func() time.Time
	NewID        func() string
}

var _ TextAgent = (*ChatAgent)(nil)

// Chat runs one user turn.
func (a *ChatAgent) Chat(ctx context.Context, in ChatInput) (ChatOutput, error) {
	msg := strings.TrimSpace(in.Message)
	if msg == "" {
		return ChatOutput{}, fmt.Errorf("%w: message is required", domain.ErrValidation)
	}
	sessionID := in.SessionID
	if sessionID == "" {
		sessionID = a.newID()
	}
	prompt := DefaultSystemPrompt
	if sp, err := a.Store.SystemPrompts().Get(ctx, domain.SystemPromptKeyAgent); err == nil && strings.TrimSpace(sp.Content) != "" {
		prompt = sp.Content
	} else if err != nil && !errors.Is(err, domain.ErrNotFound) {
		return ChatOutput{}, fmt.Errorf("load system prompt: %w", err)
	}
	limit := a.HistoryLimit
	if limit <= 0 {
		limit = 40
	}
	history, err := a.Store.Chat().ListBySession(ctx, sessionID, limit)
	if err != nil {
		return ChatOutput{}, fmt.Errorf("load chat history: %w", err)
	}

	messages := []llm.Message{{Role: llm.RoleSystem, Content: prompt + "\n\nToday is " + a.now().In(store.SingaporeLocation).Format("Monday 2 January 2006") + " (Singapore time)."}}
	messages = append(messages, historyToLLM(history)...)
	user := llm.Message{Role: llm.RoleUser, Content: msg}
	messages = append(messages, user)
	if err := a.persist(ctx, sessionID, in.UserID, user); err != nil {
		return ChatOutput{}, err
	}

	out := ChatOutput{SessionID: sessionID, RequestIDs: []string{}, ToolCalls: []ToolCallTrace{}}
	seenReq := map[string]bool{}
	steps := a.MaxSteps
	if steps <= 0 {
		steps = 6
	}
	tc := ToolContext{UserID: in.UserID, Role: in.Role, SessionID: sessionID, Channel: "text"}
	for i := 0; i < steps; i++ {
		tools := a.Tools.Definitions()
		choice := llm.ToolChoiceAuto
		if i == steps-1 {
			choice = llm.ToolChoiceNone // last step: force a text answer
		}
		resp, err := a.LLM.Chat(ctx, llm.ChatRequest{Model: a.Model, Messages: messages, Tools: tools, ToolChoice: choice,
			Metadata: map[string]string{"session_id": sessionID}})
		if err != nil {
			return out, fmt.Errorf("%w: agent llm call: %v", domain.ErrUpstream, err)
		}
		if a.Audit != nil {
			_ = a.Audit.Record(ctx, "", domain.ActorAgent, in.UserID, audit.AgentLLMCall, map[string]any{
				"session_id": sessionID, "model": resp.Model, "input_tokens": resp.Usage.InputTokens,
				"output_tokens": resp.Usage.OutputTokens, "latency_ms": resp.Latency.Milliseconds(),
			})
		}
		am := resp.Message
		am.Role = llm.RoleAssistant
		if choice == llm.ToolChoiceNone {
			am.ToolCalls = nil
		}
		messages = append(messages, am)
		if err := a.persist(ctx, sessionID, "", am); err != nil {
			return out, err
		}
		if len(am.ToolCalls) == 0 {
			out.Reply = strings.TrimSpace(am.Content)
			break
		}
		for _, call := range am.ToolCalls {
			res, err := a.Tools.Execute(ctx, tc, call.Name, call.Arguments)
			trace := ToolCallTrace{Name: call.Name, Arguments: call.Arguments, Result: res}
			if err != nil {
				trace.Error = err.Error()
				res = mustJSON(map[string]string{"error": err.Error()})
				trace.Result = res
			}
			out.ToolCalls = append(out.ToolCalls, trace)
			for _, id := range requestIDsIn(call.Arguments, res) {
				if !seenReq[id] {
					seenReq[id] = true
					out.RequestIDs = append(out.RequestIDs, id)
				}
			}
			tm := llm.Message{Role: llm.RoleTool, ToolCallID: call.ID, Name: call.Name, Content: string(res)}
			messages = append(messages, tm)
			if err := a.persist(ctx, sessionID, "", tm); err != nil {
				return out, err
			}
		}
	}
	if out.Reply == "" {
		out.Reply = "Sorry, I could not finish that. Please try again."
	}
	return out, nil
}

// History returns the stored transcript of a session.
func (a *ChatAgent) History(ctx context.Context, sessionID string) ([]domain.ChatMessage, error) {
	return a.Store.Chat().ListBySession(ctx, sessionID, 500)
}

func (a *ChatAgent) persist(ctx context.Context, sessionID, userID string, m llm.Message) error {
	cm := &domain.ChatMessage{SessionID: sessionID, UserID: userID, Role: domain.ChatRole(m.Role), Content: m.Content, ToolCallID: m.ToolCallID}
	if len(m.ToolCalls) > 0 {
		cm.ToolCalls = mustJSON(m.ToolCalls)
	}
	if err := a.Store.Chat().Append(ctx, cm); err != nil {
		return fmt.Errorf("persist chat message: %w", err)
	}
	return nil
}

func (a *ChatAgent) now() time.Time {
	if a.Clock != nil {
		return a.Clock()
	}
	return time.Now()
}

func (a *ChatAgent) newID() string {
	if a.NewID != nil {
		return a.NewID()
	}
	return newUUID()
}

// historyToLLM converts stored messages, dropping system rows and any leading fragment that does not
// start at a user message (a truncated window may begin mid tool exchange), and tool results whose
// call is not in the window.
func historyToLLM(h []domain.ChatMessage) []llm.Message {
	start := len(h)
	for i, m := range h {
		if m.Role == domain.ChatRoleUser {
			start = i
			break
		}
	}
	out := make([]llm.Message, 0, len(h)-start)
	calls := map[string]bool{}
	for _, m := range h[start:] {
		switch m.Role {
		case domain.ChatRoleSystem:
			continue
		case domain.ChatRoleAssistant:
			lm := llm.Message{Role: llm.RoleAssistant, Content: m.Content}
			if len(m.ToolCalls) > 0 {
				_ = json.Unmarshal(m.ToolCalls, &lm.ToolCalls)
				for _, c := range lm.ToolCalls {
					calls[c.ID] = true
				}
			}
			out = append(out, lm)
		case domain.ChatRoleTool:
			if !calls[m.ToolCallID] {
				continue
			}
			out = append(out, llm.Message{Role: llm.RoleTool, ToolCallID: m.ToolCallID, Content: m.Content})
		default:
			out = append(out, llm.Message{Role: llm.RoleUser, Content: m.Content})
		}
	}
	return out
}

func requestIDsIn(args, result json.RawMessage) []string {
	var ids []string
	for _, raw := range []json.RawMessage{args, result} {
		var v struct {
			RequestID string `json:"request_id"`
		}
		if json.Unmarshal(raw, &v) == nil && v.RequestID != "" {
			ids = append(ids, v.RequestID)
		}
	}
	return ids
}

func mustJSON(v any) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		return json.RawMessage(`{}`)
	}
	return b
}
