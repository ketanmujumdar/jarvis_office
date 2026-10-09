package api

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
)

// ToolNames are the tools the voice relay may call (docs/openapi.yaml enum).
var ToolNames = []string{
	agents.ToolCreateOrderRequest, agents.ToolGetRequestStatus, agents.ToolListAddresses,
	agents.ToolConfirmOrder, agents.ToolCancelRequest,
}

func isTool(name string) bool {
	for _, n := range ToolNames {
		if n == name {
			return true
		}
	}
	return false
}

func (s *Server) agentChat(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Agent != nil, "text agent"); err != nil {
		s.fail(w, r, err)
		return
	}
	var body struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err := decode(r, &body, false); err != nil {
		s.fail(w, r, err)
		return
	}
	body.Message = strings.TrimSpace(body.Message)
	if body.Message == "" {
		s.fail(w, r, validationf("message is required"))
		return
	}
	if len(body.Message) > 8000 {
		s.fail(w, r, validationf("message is too long (max 8000 characters)"))
		return
	}
	out, err := s.d.Agent.Chat(r.Context(), agents.ChatInput{
		SessionID: strings.TrimSpace(body.SessionID), UserID: currentUser(r).ID, Role: currentUser(r).Role, Message: body.Message,
	})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if out.RequestIDs == nil {
		out.RequestIDs = []string{}
	}
	if out.ToolCalls == nil {
		out.ToolCalls = []agents.ToolCallTrace{}
	}
	for i := range out.ToolCalls {
		out.ToolCalls[i].Arguments = jsonObjectOrEmpty(out.ToolCalls[i].Arguments)
		out.ToolCalls[i].Result = jsonObjectOrEmpty(out.ToolCalls[i].Result)
	}
	writeJSON(w, http.StatusOK, out)
}

// jsonObjectOrEmpty keeps the response valid JSON when a trace field is empty.
func jsonObjectOrEmpty(b json.RawMessage) json.RawMessage {
	if len(bytes.TrimSpace(b)) == 0 || !json.Valid(b) {
		return json.RawMessage(`{}`)
	}
	return b
}

func (s *Server) agentMessages(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	var (
		msgs []domain.ChatMessage
		err  error
	)
	switch {
	case s.d.Agent != nil:
		msgs, err = s.d.Agent.History(r.Context(), id)
	case s.d.Store != nil:
		msgs, err = s.d.Store.Chat().ListBySession(r.Context(), id, 500)
	default:
		err = need(false, "text agent")
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if msgs == nil {
		msgs = []domain.ChatMessage{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"messages": msgs})
}

func (s *Server) toolDefs() []llm.Tool {
	if s.d.Tools == nil {
		return []llm.Tool{}
	}
	defs := s.d.Tools.Definitions()
	if defs == nil {
		return []llm.Tool{}
	}
	return defs
}

func (s *Server) agentTools(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Tools != nil, "tool executor"); err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"tools": s.toolDefs()})
}

// ToolCallBody is POST /agent/tools/{name} (relayed from the browser's Realtime session).
type ToolCallBody struct {
	CallID    string          `json:"call_id"`
	SessionID string          `json:"session_id"`
	Arguments json.RawMessage `json:"arguments"`
}

// normalizeArgs accepts a JSON object or a JSON-encoded string containing an object.
func normalizeArgs(raw json.RawMessage) (json.RawMessage, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 || bytes.Equal(raw, []byte("null")) {
		return nil, validationf("arguments is required")
	}
	if raw[0] == '"' {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return nil, validationf("arguments is not a valid JSON string")
		}
		s = strings.TrimSpace(s)
		if s == "" {
			s = "{}"
		}
		raw = json.RawMessage(s)
	}
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil || obj == nil {
		return nil, validationf("arguments must be a JSON object")
	}
	return raw, nil
}

func (s *Server) agentToolCall(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !isTool(name) {
		s.fail(w, r, fmt.Errorf("%w: unknown tool %q", domain.ErrNotFound, name))
		return
	}
	if err := need(s.d.Tools != nil, "tool executor"); err != nil {
		s.fail(w, r, err)
		return
	}
	var body ToolCallBody
	if err := decode(r, &body, false); err != nil {
		s.fail(w, r, err)
		return
	}
	args, err := normalizeArgs(body.Arguments)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	res, err := s.d.Tools.Execute(r.Context(), agents.ToolContext{
		UserID: currentUser(r).ID, Role: currentUser(r).Role, SessionID: body.SessionID, Channel: "voice",
	}, name, args)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"call_id": body.CallID, "output": toolOutput(res)})
}

// toolOutput guarantees an object for `output` (the schema says object).
func toolOutput(res json.RawMessage) json.RawMessage {
	res = bytes.TrimSpace(res)
	if len(res) == 0 || !json.Valid(res) {
		return json.RawMessage(`{}`)
	}
	if res[0] == '{' {
		return res
	}
	b, _ := json.Marshal(map[string]json.RawMessage{"result": res})
	return b
}

// realtimeSession mints an ephemeral OpenAI Realtime secret through realtime.Service, which loads
// the DB system prompt (key "agent") on every new session, so admin edits apply immediately, and
// attaches the same tool definitions the text agent uses.
func (s *Server) realtimeSession(w http.ResponseWriter, r *http.Request) {
	if err := need(s.d.Realtime != nil, "realtime minter"); err != nil {
		s.fail(w, r, err)
		return
	}
	opts := realtime.ServiceOptions{
		Minter: s.d.Realtime, Voice: s.d.Config.OpenAIRealtimeVoice, Model: s.d.Config.OpenAIRealtimeModel,
	}
	if s.d.Store != nil {
		opts.Prompts = s.d.Store.SystemPrompts()
	}
	if s.d.Tools != nil {
		opts.Tools = s.d.Tools.Definitions
	}
	svc, err := realtime.NewService(opts)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	sess, err := svc.Start(r.Context(), currentUser(r).ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if sess.Tools == nil {
		sess.Tools = []llm.Tool{}
	}
	writeJSON(w, http.StatusOK, sess)
}

// ---------- card enrollment ----------

func (s *Server) startEnrollment(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	e, err := s.d.Orchestrator.StartEnrollment(r.Context(), currentUser(r))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *Server) currentEnrollment(w http.ResponseWriter, r *http.Request) {
	if !s.orch(w, r) {
		return
	}
	e, err := s.d.Orchestrator.CurrentEnrollment(r.Context())
	if err != nil {
		if errors.Is(err, domain.ErrNotFound) {
			err = fmt.Errorf("%w: no card enrolled yet (POST /api/v1/enrollments)", domain.ErrNotFound)
		}
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, e)
}
