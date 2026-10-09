package llm

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

const testKey = "sk-test-not-a-real-key"

type fakeOpenAI struct {
	t        *testing.T
	calls    atomic.Int32
	lastBody atomic.Value // map[string]any
	handler  func(n int, w http.ResponseWriter, body map[string]any)
}

func (f *fakeOpenAI) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	n := int(f.calls.Add(1))
	if r.URL.Path != "/v1/chat/completions" || r.Method != http.MethodPost {
		f.t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
		f.t.Errorf("Authorization header = %q", got)
	}
	if ct := r.Header.Get("Content-Type"); ct != "application/json" {
		f.t.Errorf("Content-Type = %q", ct)
	}
	raw, _ := io.ReadAll(r.Body)
	var body map[string]any
	if err := json.Unmarshal(raw, &body); err != nil {
		f.t.Errorf("bad body: %v", err)
	}
	f.lastBody.Store(body)
	f.handler(n, w, body)
}

func (f *fakeOpenAI) body() map[string]any {
	b, _ := f.lastBody.Load().(map[string]any)
	return b
}

func okText(content string) func(int, http.ResponseWriter, map[string]any) {
	return func(_ int, w http.ResponseWriter, _ map[string]any) {
		writeJSON(w, 200, map[string]any{
			"model": "gpt-5.1-2025-11-13",
			"choices": []any{map[string]any{
				"message":       map[string]any{"role": "assistant", "content": content},
				"finish_reason": "stop",
			}},
			"usage": map[string]any{"prompt_tokens": 11, "completion_tokens": 3},
		})
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func newTestClient(t *testing.T, h func(int, http.ResponseWriter, map[string]any)) (*OpenAI, *fakeOpenAI) {
	t.Helper()
	f := &fakeOpenAI{t: t, handler: h}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	c := NewOpenAI(OpenAIOptions{APIKey: testKey, BaseURL: srv.URL + "/v1/", Model: "gpt-5.1", RetryBackoff: time.Millisecond})
	return c, f
}

func TestChat_TextResponse(t *testing.T) {
	c, f := newTestClient(t, okText("pong"))
	resp, err := c.Chat(context.Background(), ChatRequest{
		Messages:        []Message{{Role: RoleSystem, Content: "sys"}, {Role: RoleUser, Content: "ping"}},
		MaxOutputTokens: 50,
	})
	if err != nil {
		t.Fatal(err)
	}
	if resp.Message.Role != RoleAssistant || resp.Message.Content != "pong" {
		t.Fatalf("message = %+v", resp.Message)
	}
	if resp.FinishReason != "stop" || resp.Model != "gpt-5.1-2025-11-13" {
		t.Fatalf("finish/model = %q %q", resp.FinishReason, resp.Model)
	}
	if resp.Usage != (Usage{InputTokens: 11, OutputTokens: 3}) {
		t.Fatalf("usage = %+v", resp.Usage)
	}
	b := f.body()
	if b["model"] != "gpt-5.1" {
		t.Errorf("model = %v", b["model"])
	}
	if b["max_completion_tokens"] != float64(50) {
		t.Errorf("max_completion_tokens = %v", b["max_completion_tokens"])
	}
	for _, k := range []string{"tools", "tool_choice", "response_format"} {
		if _, ok := b[k]; ok {
			t.Errorf("%s should be omitted", k)
		}
	}
}

func TestChat_RequestModelOverride(t *testing.T) {
	c, f := newTestClient(t, okText("x"))
	if _, err := c.Chat(context.Background(), ChatRequest{Model: "gpt-5-mini", Messages: []Message{{Role: RoleUser, Content: "hi"}}}); err != nil {
		t.Fatal(err)
	}
	if f.body()["model"] != "gpt-5-mini" {
		t.Fatalf("model = %v", f.body()["model"])
	}
}

func TestChat_MessageMapping(t *testing.T) {
	c, f := newTestClient(t, okText("done"))
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{
		{Role: RoleSystem, Content: "sys"},
		{Role: RoleUser, Content: "order paper"},
		{Role: RoleAssistant, ToolCalls: []ToolCall{
			{ID: "call_1", Name: "create_order_request", Arguments: json.RawMessage(`{"utterance":"order paper"}`)},
			{ID: "call_2", Name: "list_addresses"},
		}},
		{Role: RoleTool, ToolCallID: "call_1", Name: "create_order_request", Content: `{"id":"r1"}`},
		{Role: RoleTool, ToolCallID: "call_2", Name: "list_addresses", Content: `[]`},
	}})
	if err != nil {
		t.Fatal(err)
	}
	msgs := f.body()["messages"].([]any)
	if len(msgs) != 5 {
		t.Fatalf("len(messages) = %d", len(msgs))
	}
	asst := msgs[2].(map[string]any)
	if asst["content"] != nil {
		t.Errorf("assistant tool-call content should be null, got %v", asst["content"])
	}
	tcs := asst["tool_calls"].([]any)
	tc0 := tcs[0].(map[string]any)
	fn0 := tc0["function"].(map[string]any)
	if tc0["id"] != "call_1" || tc0["type"] != "function" || fn0["name"] != "create_order_request" || fn0["arguments"] != `{"utterance":"order paper"}` {
		t.Errorf("tool call 0 = %v", tc0)
	}
	if fn1 := tcs[1].(map[string]any)["function"].(map[string]any); fn1["arguments"] != "{}" {
		t.Errorf("empty args should be {}, got %v", fn1["arguments"])
	}
	tool := msgs[3].(map[string]any)
	if tool["role"] != "tool" || tool["tool_call_id"] != "call_1" || tool["content"] != `{"id":"r1"}` {
		t.Errorf("tool message = %v", tool)
	}
}

func TestChat_ToolsAndToolChoice(t *testing.T) {
	tools := []Tool{
		{Name: "get_request_status", Description: "status", Parameters: json.RawMessage(`{"type":"object","properties":{"request_id":{"type":"string"}}}`)},
		{Name: "list_addresses", Description: "addresses"},
	}
	cases := []struct {
		name   string
		choice ToolChoice
		want   any
	}{
		{"default omitted", "", nil},
		{"auto", ToolChoiceAuto, "auto"},
		{"none", ToolChoiceNone, "none"},
		{"required", ToolChoiceRequired, "required"},
		{"named", "list_addresses", map[string]any{"type": "function", "function": map[string]any{"name": "list_addresses"}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newTestClient(t, okText("x"))
			if _, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, Tools: tools, ToolChoice: tc.choice}); err != nil {
				t.Fatal(err)
			}
			b := f.body()
			got, ok := b["tool_choice"]
			if tc.want == nil {
				if ok {
					t.Fatalf("tool_choice should be omitted, got %v", got)
				}
			} else {
				gb, _ := json.Marshal(got)
				wb, _ := json.Marshal(tc.want)
				if string(gb) != string(wb) {
					t.Fatalf("tool_choice = %s want %s", gb, wb)
				}
			}
			ts := b["tools"].([]any)
			if len(ts) != 2 {
				t.Fatalf("tools = %v", ts)
			}
			t1 := ts[1].(map[string]any)
			fn := t1["function"].(map[string]any)
			if t1["type"] != "function" || fn["name"] != "list_addresses" {
				t.Fatalf("tool 1 = %v", t1)
			}
			if p := fn["parameters"].(map[string]any); p["type"] != "object" {
				t.Fatalf("default parameters = %v", p)
			}
		})
	}
}

func TestChat_ToolChoiceIgnoredWithoutTools(t *testing.T) {
	c, f := newTestClient(t, okText("x"))
	if _, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "hi"}}, ToolChoice: ToolChoiceRequired}); err != nil {
		t.Fatal(err)
	}
	if _, ok := f.body()["tool_choice"]; ok {
		t.Fatal("tool_choice without tools must be omitted (OpenAI rejects it)")
	}
}

func TestChat_ResponseSchema(t *testing.T) {
	c, f := newTestClient(t, okText(`{"items":[]}`))
	_, err := c.Chat(context.Background(), ChatRequest{
		Messages:           []Message{{Role: RoleUser, Content: "x"}},
		ResponseSchema:     LineItemsSchema,
		ResponseSchemaName: SchemaNameLineItems,
	})
	if err != nil {
		t.Fatal(err)
	}
	rf := f.body()["response_format"].(map[string]any)
	js := rf["json_schema"].(map[string]any)
	if rf["type"] != "json_schema" || js["name"] != SchemaNameLineItems || js["strict"] != true {
		t.Fatalf("response_format = %v", rf)
	}
	if js["schema"].(map[string]any)["type"] != "object" {
		t.Fatalf("schema = %v", js["schema"])
	}
}

func TestChat_ToolCallResponse(t *testing.T) {
	c, _ := newTestClient(t, func(_ int, w http.ResponseWriter, _ map[string]any) {
		writeJSON(w, 200, map[string]any{"model": "m", "choices": []any{map[string]any{
			"finish_reason": "tool_calls",
			"message": map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{
				map[string]any{"id": "c1", "type": "function", "function": map[string]any{"name": "a", "arguments": `{"x":1}`}},
				map[string]any{"id": "c2", "type": "function", "function": map[string]any{"name": "b", "arguments": ""}},
				map[string]any{"id": "c3", "type": "function", "function": map[string]any{"name": "c", "arguments": "{broken"}},
			}},
		}}})
	})
	resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "go"}}})
	if err != nil {
		t.Fatal(err)
	}
	want := []struct{ id, name, args string }{
		{"c1", "a", `{"x":1}`}, {"c2", "b", `{}`}, {"c3", "c", `"{broken"`},
	}
	if len(resp.Message.ToolCalls) != len(want) {
		t.Fatalf("tool calls = %+v", resp.Message.ToolCalls)
	}
	for i, w := range want {
		got := resp.Message.ToolCalls[i]
		if got.ID != w.id || got.Name != w.name || string(got.Arguments) != w.args {
			t.Errorf("call %d = %+v (%s), want %+v", i, got, got.Arguments, w)
		}
	}
	if resp.Message.Content != "" || resp.FinishReason != "tool_calls" {
		t.Errorf("content/finish = %q %q", resp.Message.Content, resp.FinishReason)
	}
}

func TestChat_Errors(t *testing.T) {
	cases := []struct {
		name      string
		status    []int // per attempt
		body      string
		wantCalls int32
		wantCode  string
		wantOK    bool
	}{
		{"400 not retried", []int{400}, `{"error":{"message":"bad schema","type":"invalid_request_error","code":"invalid_value"}}`, 1, "invalid_value", false},
		{"401 not retried", []int{401}, `{"error":{"message":"Incorrect API key provided","type":"invalid_request_error","code":"invalid_api_key"}}`, 1, "invalid_api_key", false},
		{"429 then ok", []int{429, 200}, `{"error":{"message":"slow down","type":"rate_limit"}}`, 2, "", true},
		{"503 twice then ok", []int{503, 503, 200}, `oops`, 3, "", true},
		{"500 exhausts retries", []int{500, 500, 500, 500}, `{"error":{"message":"boom","type":"server_error"}}`, 3, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, f := newTestClient(t, func(n int, w http.ResponseWriter, b map[string]any) {
				st := tc.status[min(n-1, len(tc.status)-1)]
				if st == 200 {
					okText("ok")(n, w, b)
					return
				}
				w.WriteHeader(st)
				_, _ = io.WriteString(w, tc.body)
			})
			resp, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
			if got := f.calls.Load(); got != tc.wantCalls {
				t.Errorf("calls = %d want %d", got, tc.wantCalls)
			}
			if tc.wantOK {
				if err != nil || resp.Message.Content != "ok" {
					t.Fatalf("resp=%+v err=%v", resp, err)
				}
				return
			}
			var apiErr *APIError
			if !errors.As(err, &apiErr) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if !errors.Is(err, domain.ErrUpstream) {
				t.Errorf("errors.Is(err, ErrUpstream) = false")
			}
			if tc.wantCode != "" && apiErr.Code != tc.wantCode {
				t.Errorf("code = %q want %q", apiErr.Code, tc.wantCode)
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks API key")
			}
		})
	}
}

func TestChat_RefusalAndEmpty(t *testing.T) {
	cases := []struct {
		name string
		resp map[string]any
		typ  string
	}{
		{"refusal", map[string]any{"choices": []any{map[string]any{"message": map[string]any{"role": "assistant", "refusal": "no"}}}}, "refusal"},
		{"no choices", map[string]any{"choices": []any{}}, "empty_response"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, _ := newTestClient(t, func(_ int, w http.ResponseWriter, _ map[string]any) { writeJSON(w, 200, tc.resp) })
			_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
			var apiErr *APIError
			if !errors.As(err, &apiErr) || apiErr.Type != tc.typ {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestChat_Validation(t *testing.T) {
	c, f := newTestClient(t, okText("x"))
	cases := []struct {
		name string
		req  ChatRequest
	}{
		{"no messages", ChatRequest{}},
		{"tool without id", ChatRequest{Messages: []Message{{Role: RoleTool, Content: "{}"}}}},
		{"unknown role", ChatRequest{Messages: []Message{{Role: "developer2", Content: "x"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := c.Chat(context.Background(), tc.req); !errors.Is(err, domain.ErrValidation) {
				t.Fatalf("err = %v, want ErrValidation", err)
			}
		})
	}
	if f.calls.Load() != 0 {
		t.Fatal("invalid requests must not hit the network")
	}
}

func TestChat_MissingKey(t *testing.T) {
	c := NewOpenAI(OpenAIOptions{BaseURL: "http://127.0.0.1:1"})
	_, err := c.Chat(context.Background(), ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
}

func TestChat_ContextCancelDuringBackoff(t *testing.T) {
	f := &fakeOpenAI{t: t, handler: func(_ int, w http.ResponseWriter, _ map[string]any) { w.WriteHeader(503) }}
	srv := httptest.NewServer(f)
	defer srv.Close()
	c := NewOpenAI(OpenAIOptions{APIKey: testKey, BaseURL: srv.URL + "/v1", RetryBackoff: time.Hour})
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, err := c.Chat(ctx, ChatRequest{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestNewOpenAIDefaults(t *testing.T) {
	c := NewOpenAI(OpenAIOptions{})
	if c.opts.BaseURL != DefaultBaseURL || c.opts.Model != DefaultModel || c.opts.MaxRetries != 2 {
		t.Fatalf("defaults = %+v", c.opts)
	}
	if NewOpenAI(OpenAIOptions{MaxRetries: -1}).opts.MaxRetries != 0 {
		t.Fatal("negative MaxRetries should disable retries")
	}
}
