package realtime_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
)

const testKey = "sk-test-not-a-real-key"

func newServer(t *testing.T, status int, resp string, seen *map[string]any) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/v1/realtime/client_secrets" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer "+testKey {
			t.Errorf("Authorization = %q", got)
		}
		raw, _ := io.ReadAll(r.Body)
		if seen != nil {
			if err := json.Unmarshal(raw, seen); err != nil {
				t.Errorf("body: %v", err)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, resp)
	}))
	t.Cleanup(srv.Close)
	return srv.URL + "/v1"
}

func TestMint_GAResponse(t *testing.T) {
	var body map[string]any
	base := newServer(t, 200, `{"value":"ek_abc123","expires_at":1760000000,"session":{"type":"realtime","model":"gpt-realtime-2025-08-28","audio":{"output":{"voice":"marin"}}}}`, &body)
	m := realtime.NewOpenAIMinter(realtime.OpenAIOptions{APIKey: testKey, BaseURL: base, Model: "gpt-realtime", Voice: "marin", ExpiresAfter: 2 * time.Minute})
	tools := realtime.ToolDefinitions()
	s, err := m.Mint(context.Background(), realtime.SessionParams{Instructions: "Be Jarvis.", Tools: tools, UserID: "u1"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ClientSecret != "ek_abc123" || !s.ExpiresAt.Equal(time.Unix(1760000000, 0)) || s.Model != "gpt-realtime-2025-08-28" || s.Voice != "marin" {
		t.Fatalf("session = %+v", s)
	}
	if s.CallsURL != base+"/realtime/calls" || len(s.Tools) != len(tools) {
		t.Fatalf("calls url / tools = %q %d", s.CallsURL, len(s.Tools))
	}

	exp := body["expires_after"].(map[string]any)
	if exp["anchor"] != "created_at" || exp["seconds"] != float64(120) {
		t.Errorf("expires_after = %v", exp)
	}
	sess := body["session"].(map[string]any)
	if sess["type"] != "realtime" || sess["model"] != "gpt-realtime" || sess["instructions"] != "Be Jarvis." || sess["tool_choice"] != "auto" {
		t.Errorf("session = %v", sess)
	}
	audio := sess["audio"].(map[string]any)
	if audio["output"].(map[string]any)["voice"] != "marin" {
		t.Errorf("audio.output = %v", audio["output"])
	}
	if audio["input"].(map[string]any)["transcription"].(map[string]any)["model"] != realtime.DefaultTranscriptionModel {
		t.Errorf("audio.input = %v", audio["input"])
	}
	wt := sess["tools"].([]any)
	if len(wt) != len(tools) {
		t.Fatalf("tools = %v", wt)
	}
	// Realtime tools are flat: {type, name, description, parameters} (no nested "function").
	t0 := wt[0].(map[string]any)
	if t0["type"] != "function" || t0["name"] != tools[0].Name || t0["parameters"] == nil || t0["function"] != nil {
		t.Errorf("tool 0 = %v", t0)
	}
}

func TestMint_ParamsOverrideAndLegacyShape(t *testing.T) {
	var body map[string]any
	base := newServer(t, 200, `{"client_secret":{"value":"ek_legacy","expires_at":1760000100}}`, &body)
	fixed := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	m := realtime.NewOpenAIMinter(realtime.OpenAIOptions{APIKey: testKey, BaseURL: base, TranscriptionModel: "-", Now: func() time.Time { return fixed }})
	s, err := m.Mint(context.Background(), realtime.SessionParams{Model: "gpt-realtime-mini", Voice: "cedar"})
	if err != nil {
		t.Fatal(err)
	}
	if s.ClientSecret != "ek_legacy" || s.Model != "gpt-realtime-mini" || s.Voice != "cedar" || s.ExpiresAt.Unix() != 1760000100 {
		t.Fatalf("session = %+v", s)
	}
	sess := body["session"].(map[string]any)
	if _, ok := sess["tools"]; ok {
		t.Error("tools should be omitted when none")
	}
	if _, ok := sess["tool_choice"]; ok {
		t.Error("tool_choice should be omitted when no tools")
	}
	if _, ok := sess["audio"].(map[string]any)["input"]; ok {
		t.Error("transcription disabled with \"-\"")
	}
	if body["expires_after"].(map[string]any)["seconds"] != float64(600) {
		t.Errorf("default ttl = %v", body["expires_after"])
	}
}

func TestMint_Errors(t *testing.T) {
	cases := []struct {
		name   string
		status int
		resp   string
		code   string
	}{
		{"401", 401, `{"error":{"message":"Incorrect API key provided: sk-abc*****xyz","type":"invalid_request_error","code":"invalid_api_key"}}`, "invalid_api_key"},
		{"400 bad model", 400, `{"error":{"message":"Invalid model","type":"invalid_request_error","code":null}}`, "invalid_request_error"},
		{"500 non-json", 500, `upstream died`, ""},
		{"200 empty secret", 200, `{"session":{}}`, "empty_secret"},
		{"200 bad json", 200, `{`, "decode_error"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := realtime.NewOpenAIMinter(realtime.OpenAIOptions{APIKey: testKey, BaseURL: newServer(t, tc.status, tc.resp, nil)})
			_, err := m.Mint(context.Background(), realtime.SessionParams{Instructions: "x"})
			var apiErr *realtime.APIError
			if !errors.As(err, &apiErr) || !errors.Is(err, domain.ErrUpstream) {
				t.Fatalf("err = %v", err)
			}
			if apiErr.Code != tc.code {
				t.Errorf("code = %q want %q", apiErr.Code, tc.code)
			}
			if strings.Contains(err.Error(), "sk-abc") || strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks key material: %v", err)
			}
		})
	}
}

func TestMint_MissingKeyAndNetwork(t *testing.T) {
	_, err := realtime.NewOpenAIMinter(realtime.OpenAIOptions{}).Mint(context.Background(), realtime.SessionParams{})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("missing key err = %v", err)
	}
	_, err = realtime.NewOpenAIMinter(realtime.OpenAIOptions{APIKey: testKey, BaseURL: "http://127.0.0.1:1/v1"}).Mint(context.Background(), realtime.SessionParams{})
	if !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("network err = %v", err)
	}
}

func TestDefaults(t *testing.T) {
	m := realtime.NewOpenAIMinter(realtime.OpenAIOptions{})
	if m.CallsURL() != "https://api.openai.com/v1/realtime/calls" {
		t.Fatalf("calls url = %s", m.CallsURL())
	}
	b, err := m.BuildRequest(realtime.SessionParams{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"model":"gpt-realtime"`) || !strings.Contains(string(b), `"voice":"marin"`) {
		t.Fatalf("defaults body = %s", b)
	}
}

// ---- tools ----

func TestToolDefinitions(t *testing.T) {
	tools := realtime.ToolDefinitions()
	byName := map[string]llm.Tool{}
	for _, tl := range tools {
		byName[tl.Name] = tl
		var schema map[string]any
		if err := json.Unmarshal(tl.Parameters, &schema); err != nil {
			t.Fatalf("%s parameters not JSON: %v", tl.Name, err)
		}
		if schema["type"] != "object" || tl.Description == "" {
			t.Errorf("%s: schema type %v, description %q", tl.Name, schema["type"], tl.Description)
		}
	}
	required := map[string][]string{
		agents.ToolCreateOrderRequest: {"utterance"},
		agents.ToolGetRequestStatus:   {"request_id"},
		agents.ToolListAddresses:      nil,
		agents.ToolConfirmOrder:       {"request_id", "address_id"},
		agents.ToolCancelRequest:      {"request_id"},
	}
	if len(tools) != len(required) {
		t.Fatalf("got %d tools", len(tools))
	}
	for name, req := range required {
		tl, ok := byName[name]
		if !ok {
			t.Errorf("missing tool %s (names must equal agents constants)", name)
			continue
		}
		var schema struct {
			Required []string `json:"required"`
		}
		_ = json.Unmarshal(tl.Parameters, &schema)
		if strings.Join(schema.Required, ",") != strings.Join(req, ",") {
			t.Errorf("%s required = %v want %v", name, schema.Required, req)
		}
	}
	// Argument structs in agents must decode what the schema describes.
	var args agents.ConfirmOrderArgs
	if err := json.Unmarshal([]byte(`{"request_id":"r","address_id":"a"}`), &args); err != nil || args.AddressID != "a" {
		t.Fatalf("ConfirmOrderArgs = %+v %v", args, err)
	}
}

// ---- service ----

type prompts struct {
	p   domain.SystemPrompt
	err error
	n   int
}

func (p *prompts) Get(_ context.Context, key string) (domain.SystemPrompt, error) {
	p.n++
	if key != domain.SystemPromptKeyAgent {
		return domain.SystemPrompt{}, domain.ErrNotFound
	}
	return p.p, p.err
}

func TestService_Start(t *testing.T) {
	custom := []llm.Tool{{Name: "only_tool", Parameters: json.RawMessage(`{"type":"object"}`)}}
	cases := []struct {
		name       string
		src        *prompts
		tools      func() []llm.Tool
		noAddendum bool
		wantPrefix string
		wantTools  []string
		wantErr    bool
	}{
		{"db prompt + default tools", &prompts{p: domain.SystemPrompt{Content: "  DB PROMPT v3 "}}, nil, false, "DB PROMPT v3\n\n## Voice session", []string{"create_order_request", "get_request_status", "list_addresses", "confirm_order", "cancel_request"}, false},
		{"verbatim prompt", &prompts{p: domain.SystemPrompt{Content: "DB"}}, nil, true, "DB", nil, false},
		{"not found falls back", &prompts{err: domain.ErrNotFound}, nil, false, "You are Jarvis", nil, false},
		{"empty prompt falls back", &prompts{p: domain.SystemPrompt{Content: "  "}}, nil, false, "You are Jarvis", nil, false},
		{"executor tools win", &prompts{p: domain.SystemPrompt{Content: "P"}}, func() []llm.Tool { return custom }, false, "P", []string{"only_tool"}, false},
		{"empty executor tools fall back", &prompts{p: domain.SystemPrompt{Content: "P"}}, func() []llm.Tool { return nil }, false, "P", []string{"create_order_request", "get_request_status", "list_addresses", "confirm_order", "cancel_request"}, false},
		{"db error", &prompts{err: errors.New("db down")}, nil, false, "", nil, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := realtime.NewFake()
			svc, err := realtime.NewService(realtime.ServiceOptions{Minter: fake, Prompts: tc.src, Tools: tc.tools, Voice: "cedar", DisableVoiceAddendum: tc.noAddendum})
			if err != nil {
				t.Fatal(err)
			}
			s, err := svc.Start(context.Background(), "user-1")
			if tc.wantErr {
				if err == nil {
					t.Fatal("want error")
				}
				if len(fake.Calls()) != 0 {
					t.Fatal("must not mint when the prompt cannot be loaded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			calls := fake.Calls()
			if len(calls) != 1 {
				t.Fatalf("calls = %d", len(calls))
			}
			p := calls[0]
			if !strings.HasPrefix(p.Instructions, tc.wantPrefix) {
				t.Errorf("instructions = %q", p.Instructions)
			}
			if tc.noAddendum && p.Instructions != tc.wantPrefix {
				t.Errorf("verbatim instructions = %q", p.Instructions)
			}
			if p.UserID != "user-1" || p.Voice != "cedar" || s.Voice != "cedar" {
				t.Errorf("params = %+v session = %+v", p, s)
			}
			if tc.wantTools != nil {
				var names []string
				for _, tl := range s.Tools {
					names = append(names, tl.Name)
				}
				if strings.Join(names, ",") != strings.Join(tc.wantTools, ",") {
					t.Errorf("tools = %v want %v", names, tc.wantTools)
				}
			}
			if s.ClientSecret == "" || s.CallsURL != realtime.FakeCallsURL {
				t.Errorf("session = %+v", s)
			}
		})
	}
}

func TestService_ReloadsPromptEverySession(t *testing.T) {
	src := &prompts{p: domain.SystemPrompt{Content: "v1"}}
	fake := realtime.NewFake()
	svc, _ := realtime.NewService(realtime.ServiceOptions{Minter: fake, Prompts: src, DisableVoiceAddendum: true})
	if _, err := svc.Start(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	src.p.Content = "v2 (edited by admin)"
	if _, err := svc.Start(context.Background(), "u"); err != nil {
		t.Fatal(err)
	}
	calls := fake.Calls()
	if src.n != 2 || calls[0].Instructions != "v1" || calls[1].Instructions != "v2 (edited by admin)" {
		t.Fatalf("calls = %+v loads=%d", calls, src.n)
	}
}

func TestService_RequiresMinterAndPropagatesMintError(t *testing.T) {
	if _, err := realtime.NewService(realtime.ServiceOptions{}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("err = %v", err)
	}
	fake := realtime.NewFake()
	fake.Err = domain.ErrUpstream
	svc, _ := realtime.NewService(realtime.ServiceOptions{Minter: fake})
	if _, err := svc.Start(context.Background(), "u"); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
}

func TestFake(t *testing.T) {
	now := time.Date(2026, 10, 9, 12, 0, 0, 0, time.UTC)
	f := &realtime.Fake{Now: func() time.Time { return now }, TTL: 30 * time.Second}
	s1, _ := f.Mint(context.Background(), realtime.SessionParams{})
	s2, _ := f.Mint(context.Background(), realtime.SessionParams{Model: "m", Voice: "v"})
	if s1.ClientSecret == s2.ClientSecret || !s1.ExpiresAt.Equal(now.Add(30*time.Second)) {
		t.Fatalf("s1=%+v s2=%+v", s1, s2)
	}
	if s1.Model != realtime.DefaultModel || s1.Voice != realtime.DefaultVoice || s2.Model != "m" || s2.Voice != "v" {
		t.Fatalf("defaults: s1=%+v s2=%+v", s1, s2)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.Mint(ctx, realtime.SessionParams{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
