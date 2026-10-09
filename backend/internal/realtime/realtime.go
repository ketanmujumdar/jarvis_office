// Package realtime mints short-lived OpenAI Realtime client secrets for the browser. Owner: agent D.
//
// Flow: the Flutter app calls POST /api/v1/realtime/session. The backend loads the system prompt
// (system_prompts key "agent") and the agent tool definitions (agents.ToolDefinitions) and calls
// OpenAI POST /v1/realtime/client_secrets with a session config {type: "realtime", model,
// instructions, tools, audio.output.voice}. The browser receives only the ephemeral secret and
// connects via WebRTC (POST https://api.openai.com/v1/realtime/calls with the SDP offer).
// Tool calls emitted by the model are relayed by the browser to POST /api/v1/agent/tools/{name};
// the backend executes them through the same agents.ToolExecutor the text agent uses.
//
// Service (service.go) does the prompt + tools assembly; OpenAIMinter talks to OpenAI; Fake is the
// offline Minter for tests and FAKES=true.
package realtime

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
)

// Defaults for OpenAIOptions.
const (
	DefaultBaseURL            = "https://api.openai.com/v1"
	DefaultModel              = "gpt-realtime"
	DefaultVoice              = "marin"
	DefaultTranscriptionModel = "gpt-4o-mini-transcribe"
	DefaultExpiresAfter       = 10 * time.Minute
)

// SessionParams configure one realtime session.
type SessionParams struct {
	Instructions string     // system prompt content
	Tools        []llm.Tool // same tools as the text agent
	Voice        string     // e.g. "marin"
	Model        string     // "" = default OPENAI_REALTIME_MODEL
	UserID       string     // for tracing
}

// Session is what the browser receives. ClientSecret is ephemeral (about 1 minute to connect).
type Session struct {
	ClientSecret string    `json:"client_secret"`
	ExpiresAt    time.Time `json:"expires_at"`
	Model        string    `json:"model"`
	Voice        string    `json:"voice"`
	// CallsURL is where the browser POSTs its SDP offer (https://api.openai.com/v1/realtime/calls).
	CallsURL string `json:"calls_url"`
	// Tools echoes the tool definitions configured on the session (openapi RealtimeSession.tools).
	Tools []llm.Tool `json:"tools,omitempty"`
}

// Minter creates realtime sessions. Implementations: OpenAIMinter (real) and Fake.
type Minter interface {
	Mint(ctx context.Context, p SessionParams) (Session, error)
}

// OpenAIOptions configure OpenAIMinter.
type OpenAIOptions struct {
	APIKey     string // secret; never log
	BaseURL    string // https://api.openai.com/v1
	Model      string // gpt-realtime
	Voice      string
	HTTPClient *http.Client
	// TranscriptionModel transcribes the user's audio so the UI can show a transcript
	// (default gpt-4o-mini-transcribe; "-" disables input transcription).
	TranscriptionModel string
	// ExpiresAfter is the client secret lifetime (default 10m; OpenAI allows 10s..2h).
	ExpiresAfter time.Duration
	// Now is the clock used when the response lacks expires_at (default time.Now).
	Now func() time.Time
}

// OpenAIMinter implements Minter with POST {BaseURL}/realtime/client_secrets.
type OpenAIMinter struct {
	opts OpenAIOptions
	http *http.Client
}

var _ Minter = (*OpenAIMinter)(nil)

// NewOpenAIMinter builds the real minter. Missing options get defaults.
func NewOpenAIMinter(opts OpenAIOptions) *OpenAIMinter {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Model == "" {
		opts.Model = DefaultModel
	}
	if opts.Voice == "" {
		opts.Voice = DefaultVoice
	}
	if opts.TranscriptionModel == "" {
		opts.TranscriptionModel = DefaultTranscriptionModel
	}
	if opts.ExpiresAfter <= 0 {
		opts.ExpiresAfter = DefaultExpiresAfter
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 20 * time.Second}
	}
	return &OpenAIMinter{opts: opts, http: hc}
}

// CallsURL is where the browser posts its WebRTC SDP offer.
func (m *OpenAIMinter) CallsURL() string { return m.opts.BaseURL + "/realtime/calls" }

// ---- wire types ----

type wireTool struct {
	Type        string          `json:"type"`
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters"`
}

type wireAudio struct {
	Input  *wireAudioInput `json:"input,omitempty"`
	Output wireAudioOutput `json:"output"`
}

type wireAudioInput struct {
	Transcription *wireTranscription `json:"transcription,omitempty"`
}

type wireTranscription struct {
	Model string `json:"model"`
}

type wireAudioOutput struct {
	Voice string `json:"voice"`
}

type wireSession struct {
	Type         string     `json:"type"`
	Model        string     `json:"model"`
	Instructions string     `json:"instructions,omitempty"`
	Audio        wireAudio  `json:"audio"`
	Tools        []wireTool `json:"tools,omitempty"`
	ToolChoice   string     `json:"tool_choice,omitempty"`
}

type wireExpiresAfter struct {
	Anchor  string `json:"anchor"`
	Seconds int    `json:"seconds"`
}

type wireRequest struct {
	ExpiresAfter wireExpiresAfter `json:"expires_after"`
	Session      wireSession      `json:"session"`
}

// wireResponse covers the GA shape {value, expires_at, session} and the older
// {client_secret: {value, expires_at}} shape.
type wireResponse struct {
	Value        string `json:"value"`
	ExpiresAt    int64  `json:"expires_at"`
	ClientSecret *struct {
		Value     string `json:"value"`
		ExpiresAt int64  `json:"expires_at"`
	} `json:"client_secret"`
	Session struct {
		Model string `json:"model"`
		Audio struct {
			Output struct {
				Voice string `json:"voice"`
			} `json:"output"`
		} `json:"audio"`
	} `json:"session"`
}

// APIError is a non-2xx response from OpenAI. It never contains the API key.
// errors.Is(err, domain.ErrUpstream) is true.
type APIError struct {
	StatusCode int
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("openai realtime: http %d %s: %s", e.StatusCode, e.Code, e.Message)
}

func (e *APIError) Unwrap() error { return domain.ErrUpstream }

// BuildRequest returns the JSON body sent to /realtime/client_secrets (exported for tests).
func (m *OpenAIMinter) BuildRequest(p SessionParams) ([]byte, error) {
	model := p.Model
	if model == "" {
		model = m.opts.Model
	}
	voice := p.Voice
	if voice == "" {
		voice = m.opts.Voice
	}
	s := wireSession{
		Type: "realtime", Model: model, Instructions: p.Instructions,
		Audio: wireAudio{Output: wireAudioOutput{Voice: voice}},
	}
	if m.opts.TranscriptionModel != "-" {
		s.Audio.Input = &wireAudioInput{Transcription: &wireTranscription{Model: m.opts.TranscriptionModel}}
	}
	for _, t := range p.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		s.Tools = append(s.Tools, wireTool{Type: "function", Name: t.Name, Description: t.Description, Parameters: params})
	}
	if len(s.Tools) > 0 {
		s.ToolChoice = "auto"
	}
	secs := int(m.opts.ExpiresAfter / time.Second)
	if secs < 10 {
		secs = 10
	}
	if secs > 7200 {
		secs = 7200
	}
	return json.Marshal(wireRequest{ExpiresAfter: wireExpiresAfter{Anchor: "created_at", Seconds: secs}, Session: s})
}

// Mint creates an ephemeral client secret. The returned ClientSecret must only be sent to the
// browser that asked for it; never log it.
func (m *OpenAIMinter) Mint(ctx context.Context, p SessionParams) (Session, error) {
	if m.opts.APIKey == "" {
		return Session{}, fmt.Errorf("openai realtime: OPENAI_API_KEY is not set: %w", domain.ErrUpstream)
	}
	body, err := m.BuildRequest(p)
	if err != nil {
		return Session{}, fmt.Errorf("openai realtime: encode: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, m.opts.BaseURL+"/realtime/client_secrets", bytes.NewReader(body))
	if err != nil {
		return Session{}, fmt.Errorf("openai realtime: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+m.opts.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := m.http.Do(req)
	if err != nil {
		return Session{}, fmt.Errorf("openai realtime: %w: %v", domain.ErrUpstream, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 4<<20))
	if err != nil {
		return Session{}, fmt.Errorf("openai realtime: read body: %w: %v", domain.ErrUpstream, err)
	}
	if res.StatusCode/100 != 2 {
		return Session{}, decodeError(res.StatusCode, raw)
	}
	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return Session{}, &APIError{StatusCode: res.StatusCode, Code: "decode_error", Message: err.Error()}
	}
	secret, exp := wr.Value, wr.ExpiresAt
	if secret == "" && wr.ClientSecret != nil {
		secret, exp = wr.ClientSecret.Value, wr.ClientSecret.ExpiresAt
	}
	if secret == "" {
		return Session{}, &APIError{StatusCode: res.StatusCode, Code: "empty_secret", Message: "response had no client secret"}
	}
	out := Session{
		ClientSecret: secret,
		Model:        firstNonEmpty(wr.Session.Model, p.Model, m.opts.Model),
		Voice:        firstNonEmpty(wr.Session.Audio.Output.Voice, p.Voice, m.opts.Voice),
		CallsURL:     m.CallsURL(),
		Tools:        p.Tools,
	}
	if exp > 0 {
		out.ExpiresAt = time.Unix(exp, 0).UTC()
	} else {
		out.ExpiresAt = m.opts.Now().Add(m.opts.ExpiresAfter).UTC()
	}
	return out, nil
}

func decodeError(status int, raw []byte) *APIError {
	e := &APIError{StatusCode: status}
	var we struct {
		Error struct {
			Message string `json:"message"`
			Type    string `json:"type"`
			Code    any    `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(raw, &we) == nil && we.Error.Message != "" {
		e.Message = we.Error.Message
		switch v := we.Error.Code.(type) {
		case string:
			e.Code = v
		case float64:
			e.Code = strconv.Itoa(int(v))
		default:
			e.Code = we.Error.Type
		}
	} else {
		msg := strings.TrimSpace(string(raw))
		if len(msg) > 300 {
			msg = msg[:300]
		}
		e.Message = msg
	}
	if e.Message == "" {
		e.Message = http.StatusText(status)
	}
	e.Message = llm.RedactKeys(e.Message)
	return e
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
