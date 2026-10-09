package harness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"time"
)

// FakeOpenAI is a wire-level stand-in for the OpenAI endpoints the api may call:
//
//   - POST {base}/chat/completions        (Chat Completions, tools + response_format json_schema)
//   - POST {base}/responses               (Responses API, tools + text.format json_schema)
//   - POST {base}/realtime/client_secrets (GA realtime ephemeral key)
//   - POST {base}/realtime/sessions       (beta realtime ephemeral key)
//
// Behaviour is deterministic:
//   - A call carrying a JSON schema is the utterance parser: it answers with the items registered via
//     SetParse for the first (longest) phrase contained in the last user message, wrapped in the
//     schema's array property (default "items"); unknown utterances become one item.
//   - A call with tools whose last message is from the user gets one tool call: list_addresses if
//     the message mentions "address", otherwise create_order_request{utterance}.
//   - A call whose last message is a tool result gets a short text reply asking for the address.
//
// Every call is captured (system prompt, last user message, tool names) for assertions.
type FakeOpenAI struct {
	Server *httptest.Server
	apiKey string

	mu       sync.Mutex
	parse    map[string][]map[string]any
	calls    []LLMCall
	sessions []map[string]any
	seq      int
}

// LLMCall is one captured model call.
type LLMCall struct {
	API        string // "chat" | "responses"
	Model      string
	System     string // system/developer messages + instructions, joined with "\n"
	LastUser   string
	LastRole   string
	ToolNames  []string
	HasSchema  bool
	ReplyKind  string // "parse" | "tool_call" | "text"
	ToolCalled string
}

// NewFakeOpenAI starts the fake. apiKey is the Bearer token it requires.
func NewFakeOpenAI(apiKey string) *FakeOpenAI {
	f := &FakeOpenAI{apiKey: apiKey, parse: map[string][]map[string]any{}}
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// BaseURL is what to give the api as OPENAI_BASE_URL (".../v1", like the real default).
func (f *FakeOpenAI) BaseURL() string { return f.Server.URL + "/v1" }

// Close stops the server.
func (f *FakeOpenAI) Close() { f.Server.Close() }

// SetParse registers the parser output for utterances containing phrase (case-insensitive).
// Each item is {"description", "qty"?, "urgency"?, "catalog_sku"?}.
func (f *FakeOpenAI) SetParse(phrase string, items ...map[string]any) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.parse[strings.ToLower(phrase)] = items
}

// Calls returns captured model calls.
func (f *FakeOpenAI) Calls() []LLMCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]LLMCall(nil), f.calls...)
}

// RealtimeSessions returns the captured realtime session configs (the "session" object).
func (f *FakeOpenAI) RealtimeSessions() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sessions...)
}

func (f *FakeOpenAI) serve(w http.ResponseWriter, r *http.Request) {
	if f.apiKey == "" || bearer(r) != f.apiKey {
		writeJSON(w, http.StatusUnauthorized, map[string]any{"error": map[string]any{
			"message": "Incorrect API key provided.", "type": "invalid_request_error", "code": "invalid_api_key"}})
		return
	}
	body, _ := io.ReadAll(r.Body)
	p := r.URL.Path
	switch {
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/chat/completions"):
		f.chat(w, body)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/responses"):
		f.responses(w, body)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/realtime/client_secrets"):
		f.realtime(w, body, true)
	case r.Method == http.MethodPost && strings.HasSuffix(p, "/realtime/sessions"):
		f.realtime(w, body, false)
	default:
		writeJSON(w, http.StatusNotFound, map[string]any{"error": map[string]any{"message": "unknown url " + p, "type": "invalid_request_error"}})
	}
}

// normalised view of a request.
type nreq struct {
	model     string
	system    []string
	lastRole  string
	lastUser  string
	tools     []string
	schema    map[string]any
	toolCalls int // assistant tool calls already in the transcript
}

type plan struct {
	kind string // parse | tool_call | text
	text string
	tool string
	args string
}

func (f *FakeOpenAI) decide(n nreq) plan {
	switch {
	case n.schema != nil && hasProperty(n.schema, "matches"):
		return plan{kind: "parse", text: matchAll(n.lastUser)}
	case n.schema != nil:
		return plan{kind: "parse", text: f.parseJSON(n.lastUser, n.schema)}
	case len(n.tools) > 0 && n.lastRole == "user":
		if strings.Contains(strings.ToLower(n.lastUser), "address") && hasTool(n.tools, "list_addresses") {
			return plan{kind: "tool_call", tool: "list_addresses", args: "{}"}
		}
		if hasTool(n.tools, "create_order_request") {
			a, _ := json.Marshal(map[string]any{"utterance": n.lastUser})
			return plan{kind: "tool_call", tool: "create_order_request", args: string(a)}
		}
	case n.lastRole == "tool":
		return plan{kind: "text", text: "I'm checking prices now. Which delivery address should I use? HQ - Marina One is the default."}
	}
	return plan{kind: "text", text: "How can I help with office supplies today?"}
}

// hasProperty reports whether an object schema declares a top-level property.
func hasProperty(schema map[string]any, name string) bool {
	props, _ := schema["properties"].(map[string]any)
	_, ok := props[name]
	return ok
}

// matchAll answers an offer-matching call (llm.MatchOffers): every offer is a match and its pack
// size is the deterministic hint the client sent.
func matchAll(user string) string {
	var in struct {
		Offers []struct {
			Index        int `json:"index"`
			PackSizeHint int `json:"pack_size_hint"`
		} `json:"offers"`
	}
	_ = json.Unmarshal([]byte(user), &in)
	matches := make([]map[string]any, 0, len(in.Offers))
	for _, o := range in.Offers {
		ps := o.PackSizeHint
		if ps < 1 {
			ps = 1
		}
		matches = append(matches, map[string]any{"index": o.Index, "is_match": true, "pack_size": ps, "reason": "e2e fake: accepted"})
	}
	b, _ := json.Marshal(map[string]any{"matches": matches})
	return string(b)
}

func hasTool(ts []string, name string) bool {
	for _, t := range ts {
		if t == name {
			return true
		}
	}
	return false
}

func (f *FakeOpenAI) parseJSON(utterance string, schema map[string]any) string {
	u := strings.ToLower(utterance)
	f.mu.Lock()
	phrases := make([]string, 0, len(f.parse))
	for k := range f.parse {
		phrases = append(phrases, k)
	}
	sort.Slice(phrases, func(i, j int) bool {
		if len(phrases[i]) != len(phrases[j]) {
			return len(phrases[i]) > len(phrases[j])
		}
		return phrases[i] < phrases[j]
	})
	var items []map[string]any
	for _, ph := range phrases {
		if strings.Contains(u, ph) {
			items = f.parse[ph]
			break
		}
	}
	f.mu.Unlock()
	if items == nil {
		items = []map[string]any{{"description": utterance, "qty": nil, "urgency": "normal", "catalog_sku": ""}}
	}
	// Fill every field the schema declares so strict schemas are satisfied.
	full := make([]map[string]any, 0, len(items))
	for _, it := range items {
		m := map[string]any{"description": "", "qty": nil, "urgency": "normal", "catalog_sku": ""}
		for k, v := range it {
			m[k] = v
		}
		full = append(full, m)
	}
	key := arrayProperty(schema)
	if key == "" {
		b, _ := json.Marshal(full)
		return string(b)
	}
	b, _ := json.Marshal(map[string]any{key: full})
	return string(b)
}

// arrayProperty finds the top-level array property of an object schema ("items" by default).
func arrayProperty(schema map[string]any) string {
	if t, _ := schema["type"].(string); t == "array" {
		return ""
	}
	props, _ := schema["properties"].(map[string]any)
	if _, ok := props["items"]; ok {
		return "items"
	}
	keys := make([]string, 0, len(props))
	for k := range props {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if pm, ok := props[k].(map[string]any); ok {
			if t, _ := pm["type"].(string); t == "array" {
				return k
			}
		}
	}
	return "items"
}

func (f *FakeOpenAI) record(api string, n nreq, p plan) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	f.calls = append(f.calls, LLMCall{
		API: api, Model: n.model, System: strings.Join(n.system, "\n"), LastUser: n.lastUser, LastRole: n.lastRole,
		ToolNames: n.tools, HasSchema: n.schema != nil, ReplyKind: p.kind, ToolCalled: p.tool,
	})
}

func (f *FakeOpenAI) nextID(prefix string) string {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.seq++
	return prefix + "_e2e_" + strings.ReplaceAll(NewUUID(), "-", "")[:16]
}

// textOf flattens string or [{type, text}] content.
func textOf(v any) string {
	switch c := v.(type) {
	case string:
		return c
	case []any:
		var parts []string
		for _, p := range c {
			if m, ok := p.(map[string]any); ok {
				if s, ok := m["text"].(string); ok {
					parts = append(parts, s)
				}
			}
		}
		return strings.Join(parts, "")
	}
	return ""
}

// ---------------- Chat Completions ----------------

func (f *FakeOpenAI) chat(w http.ResponseWriter, body []byte) {
	var req struct {
		Model    string           `json:"model"`
		Messages []map[string]any `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
			Name string `json:"name"`
		} `json:"tools"`
		ResponseFormat *struct {
			Type       string `json:"type"`
			JSONSchema *struct {
				Schema map[string]any `json:"schema"`
			} `json:"json_schema"`
		} `json:"response_format"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.Messages) == 0 {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "messages required", "type": "invalid_request_error"}})
		return
	}
	n := nreq{model: req.Model}
	for _, m := range req.Messages {
		role, _ := m["role"].(string)
		content := textOf(m["content"])
		switch role {
		case "system", "developer":
			n.system = append(n.system, content)
		case "user":
			n.lastUser = content
		case "assistant":
			if tc, ok := m["tool_calls"].([]any); ok {
				n.toolCalls += len(tc)
			}
		}
		n.lastRole = role
	}
	if n.lastRole == "developer" || n.lastRole == "system" {
		n.lastRole = "user"
	}
	for _, t := range req.Tools {
		name := t.Function.Name
		if name == "" {
			name = t.Name
		}
		n.tools = append(n.tools, name)
	}
	if req.ResponseFormat != nil && req.ResponseFormat.JSONSchema != nil {
		n.schema = req.ResponseFormat.JSONSchema.Schema
		if n.schema == nil {
			n.schema = map[string]any{}
		}
	}
	p := f.decide(n)
	f.record("chat", n, p)

	msg := map[string]any{"role": "assistant", "content": nil}
	finish := "stop"
	if p.kind == "tool_call" {
		finish = "tool_calls"
		msg["tool_calls"] = []any{map[string]any{
			"id": f.nextID("call"), "type": "function",
			"function": map[string]any{"name": p.tool, "arguments": p.args},
		}}
	} else {
		msg["content"] = p.text
	}
	model := req.Model
	if model == "" {
		model = "gpt-5.1"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": f.nextID("chatcmpl"), "object": "chat.completion", "created": time.Now().Unix(), "model": model,
		"choices": []any{map[string]any{"index": 0, "message": msg, "finish_reason": finish}},
		"usage":   map[string]any{"prompt_tokens": 100, "completion_tokens": 20, "total_tokens": 120},
	})
}

// ---------------- Responses API ----------------

func (f *FakeOpenAI) responses(w http.ResponseWriter, body []byte) {
	var req struct {
		Model        string            `json:"model"`
		Instructions string            `json:"instructions"`
		Input        json.RawMessage   `json:"input"`
		Tools        []json.RawMessage `json:"tools"`
		Text         *struct {
			Format *struct {
				Type   string         `json:"type"`
				Schema map[string]any `json:"schema"`
			} `json:"format"`
		} `json:"text"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "bad json", "type": "invalid_request_error"}})
		return
	}
	n := nreq{model: req.Model}
	if req.Instructions != "" {
		n.system = append(n.system, req.Instructions)
	}
	var s string
	if json.Unmarshal(req.Input, &s) == nil {
		n.lastUser, n.lastRole = s, "user"
	} else {
		var items []map[string]any
		_ = json.Unmarshal(req.Input, &items)
		for _, it := range items {
			typ, _ := it["type"].(string)
			role, _ := it["role"].(string)
			switch {
			case typ == "function_call_output":
				n.lastRole = "tool"
			case typ == "function_call":
				n.toolCalls++
				n.lastRole = "assistant"
			case role == "system" || role == "developer":
				n.system = append(n.system, textOf(it["content"]))
			case role == "user":
				n.lastUser, n.lastRole = textOf(it["content"]), "user"
			case role == "assistant":
				n.lastRole = "assistant"
			}
		}
	}
	for _, raw := range req.Tools {
		var t struct {
			Name     string `json:"name"`
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		}
		_ = json.Unmarshal(raw, &t)
		if t.Name == "" {
			t.Name = t.Function.Name
		}
		n.tools = append(n.tools, t.Name)
	}
	if req.Text != nil && req.Text.Format != nil && req.Text.Format.Type == "json_schema" {
		n.schema = req.Text.Format.Schema
		if n.schema == nil {
			n.schema = map[string]any{}
		}
	}
	p := f.decide(n)
	f.record("responses", n, p)

	var output []any
	outText := ""
	if p.kind == "tool_call" {
		output = append(output, map[string]any{
			"type": "function_call", "id": f.nextID("fc"), "call_id": f.nextID("call"),
			"name": p.tool, "arguments": p.args, "status": "completed",
		})
	} else {
		outText = p.text
		output = append(output, map[string]any{
			"type": "message", "id": f.nextID("msg"), "role": "assistant", "status": "completed",
			"content": []any{map[string]any{"type": "output_text", "text": p.text, "annotations": []any{}}},
		})
	}
	model := req.Model
	if model == "" {
		model = "gpt-5.1"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": f.nextID("resp"), "object": "response", "created_at": time.Now().Unix(), "status": "completed",
		"model": model, "output": output, "output_text": outText,
		"usage": map[string]any{"input_tokens": 100, "output_tokens": 20, "total_tokens": 120},
	})
}

// ---------------- Realtime ----------------

func (f *FakeOpenAI) realtime(w http.ResponseWriter, body []byte, ga bool) {
	var req map[string]any
	if err := json.Unmarshal(body, &req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": map[string]any{"message": "bad json", "type": "invalid_request_error"}})
		return
	}
	session, _ := req["session"].(map[string]any)
	if session == nil {
		session = req // beta shape: session fields at the top level
	}
	f.mu.Lock()
	f.sessions = append(f.sessions, session)
	f.mu.Unlock()
	secret := "ek_e2e_" + strings.ReplaceAll(NewUUID(), "-", "")
	exp := time.Now().Add(time.Minute).Unix()
	if ga {
		writeJSON(w, http.StatusOK, map[string]any{"value": secret, "expires_at": exp, "session": session})
		return
	}
	out := map[string]any{}
	for k, v := range session {
		out[k] = v
	}
	out["client_secret"] = map[string]any{"value": secret, "expires_at": exp}
	writeJSON(w, http.StatusOK, out)
}
