package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// Defaults for OpenAIOptions.
const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-5.1"
)

// OpenAIOptions configure the OpenAI client.
type OpenAIOptions struct {
	APIKey     string // secret; never log
	BaseURL    string // https://api.openai.com/v1
	Model      string // default model, e.g. gpt-5.1
	Timeout    time.Duration
	HTTPClient *http.Client
	// MaxRetries for 429 and 5xx responses (default 2; negative disables retries).
	MaxRetries int
	// RetryBackoff is the base delay between retries (default 500ms, doubled each attempt).
	RetryBackoff time.Duration
}

// OpenAI implements Client on the Chat Completions API (POST {BaseURL}/chat/completions) with
// function tools and strict JSON-schema structured outputs.
type OpenAI struct {
	opts OpenAIOptions
	http *http.Client
}

var _ Client = (*OpenAI)(nil)

// NewOpenAI builds the real client. Missing options get defaults.
func NewOpenAI(opts OpenAIOptions) *OpenAI {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Model == "" {
		opts.Model = DefaultModel
	}
	if opts.Timeout <= 0 {
		opts.Timeout = 60 * time.Second
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = 2
	}
	if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = 500 * time.Millisecond
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: opts.Timeout}
	}
	return &OpenAI{opts: opts, http: hc}
}

// APIError is a non-2xx response from OpenAI. It never contains the API key.
// errors.Is(err, domain.ErrUpstream) is true.
type APIError struct {
	StatusCode int
	Type       string
	Code       string
	Message    string
}

func (e *APIError) Error() string {
	code := e.Code
	if code == "" {
		code = e.Type
	}
	return fmt.Sprintf("openai: http %d %s: %s", e.StatusCode, code, e.Message)
}

func (e *APIError) Unwrap() error { return domain.ErrUpstream }

// Retryable reports whether the request may succeed on retry.
func (e *APIError) Retryable() bool {
	return e.StatusCode == http.StatusTooManyRequests || e.StatusCode >= 500
}

// ---- wire types (Chat Completions) ----

type wireFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type wireTool struct {
	Type     string       `json:"type"`
	Function wireFunction `json:"function"`
}

type wireToolCallFn struct {
	Name      string `json:"name"`
	Arguments string `json:"arguments"`
}

type wireToolCall struct {
	ID       string         `json:"id"`
	Type     string         `json:"type"`
	Function wireToolCallFn `json:"function"`
}

type wireMessage struct {
	Role       string         `json:"role"`
	Content    *string        `json:"content"`
	ToolCalls  []wireToolCall `json:"tool_calls,omitempty"`
	ToolCallID string         `json:"tool_call_id,omitempty"`
	Refusal    *string        `json:"refusal,omitempty"`
}

type wireJSONSchema struct {
	Name   string          `json:"name"`
	Schema json.RawMessage `json:"schema"`
	Strict bool            `json:"strict"`
}

type wireResponseFormat struct {
	Type       string          `json:"type"`
	JSONSchema *wireJSONSchema `json:"json_schema,omitempty"`
}

type wireRequest struct {
	Model               string              `json:"model"`
	Messages            []wireMessage       `json:"messages"`
	Tools               []wireTool          `json:"tools,omitempty"`
	ToolChoice          any                 `json:"tool_choice,omitempty"`
	ResponseFormat      *wireResponseFormat `json:"response_format,omitempty"`
	MaxCompletionTokens int                 `json:"max_completion_tokens,omitempty"`
}

type wireResponse struct {
	Model   string `json:"model"`
	Choices []struct {
		Message      wireMessage `json:"message"`
		FinishReason string      `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		PromptTokens     int `json:"prompt_tokens"`
		CompletionTokens int `json:"completion_tokens"`
	} `json:"usage"`
}

type wireError struct {
	Error struct {
		Message string `json:"message"`
		Type    string `json:"type"`
		Code    any    `json:"code"`
	} `json:"error"`
}

// buildWireRequest maps a provider-neutral request onto Chat Completions JSON.
func (c *OpenAI) buildWireRequest(req ChatRequest) (wireRequest, error) {
	model := req.Model
	if model == "" {
		model = c.opts.Model
	}
	if len(req.Messages) == 0 {
		return wireRequest{}, fmt.Errorf("openai: no messages: %w", domain.ErrValidation)
	}
	w := wireRequest{Model: model, MaxCompletionTokens: req.MaxOutputTokens}
	for _, m := range req.Messages {
		wm := wireMessage{Role: string(m.Role)}
		switch m.Role {
		case RoleSystem, RoleUser:
			wm.Content = strPtr(m.Content)
		case RoleAssistant:
			if m.Content != "" || len(m.ToolCalls) == 0 {
				wm.Content = strPtr(m.Content)
			}
			for _, tc := range m.ToolCalls {
				args := string(tc.Arguments)
				if strings.TrimSpace(args) == "" {
					args = "{}"
				}
				wm.ToolCalls = append(wm.ToolCalls, wireToolCall{
					ID: tc.ID, Type: "function", Function: wireToolCallFn{Name: tc.Name, Arguments: args},
				})
			}
		case RoleTool:
			if m.ToolCallID == "" {
				return wireRequest{}, fmt.Errorf("openai: tool message without tool_call_id: %w", domain.ErrValidation)
			}
			wm.Content = strPtr(m.Content)
			wm.ToolCallID = m.ToolCallID
		default:
			return wireRequest{}, fmt.Errorf("openai: unknown role %q: %w", m.Role, domain.ErrValidation)
		}
		w.Messages = append(w.Messages, wm)
	}
	for _, t := range req.Tools {
		params := t.Parameters
		if len(params) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		w.Tools = append(w.Tools, wireTool{Type: "function", Function: wireFunction{
			Name: t.Name, Description: t.Description, Parameters: params,
		}})
	}
	if len(w.Tools) > 0 {
		switch req.ToolChoice {
		case "":
		case ToolChoiceAuto, ToolChoiceNone, ToolChoiceRequired:
			w.ToolChoice = string(req.ToolChoice)
		default:
			w.ToolChoice = map[string]any{"type": "function", "function": map[string]string{"name": string(req.ToolChoice)}}
		}
	}
	if len(req.ResponseSchema) > 0 {
		name := req.ResponseSchemaName
		if name == "" {
			name = "response"
		}
		w.ResponseFormat = &wireResponseFormat{Type: "json_schema", JSONSchema: &wireJSONSchema{
			Name: name, Schema: req.ResponseSchema, Strict: true,
		}}
	}
	return w, nil
}

// Chat runs one completion. Retries 429/5xx with exponential backoff.
func (c *OpenAI) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	if c.opts.APIKey == "" {
		return ChatResponse{}, fmt.Errorf("openai: OPENAI_API_KEY is not set: %w", domain.ErrUpstream)
	}
	w, err := c.buildWireRequest(req)
	if err != nil {
		return ChatResponse{}, err
	}
	body, err := json.Marshal(w)
	if err != nil {
		return ChatResponse{}, fmt.Errorf("openai: encode request: %w", err)
	}
	start := time.Now()
	var lastErr error
	for attempt := 0; attempt <= c.opts.MaxRetries; attempt++ {
		if attempt > 0 {
			delay := c.opts.RetryBackoff << (attempt - 1)
			var apiErr *APIError
			if errors.As(lastErr, &apiErr) && apiErr.StatusCode == http.StatusTooManyRequests {
				delay *= 2
			}
			select {
			case <-ctx.Done():
				return ChatResponse{}, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := c.do(ctx, body)
		if err == nil {
			out, perr := decodeResponse(resp)
			if perr != nil {
				return ChatResponse{}, perr
			}
			out.Latency = time.Since(start)
			return out, nil
		}
		lastErr = err
		var apiErr *APIError
		if errors.As(err, &apiErr) && !apiErr.Retryable() {
			return ChatResponse{}, err
		}
		if ctx.Err() != nil {
			return ChatResponse{}, ctx.Err()
		}
	}
	return ChatResponse{}, lastErr
}

func (c *OpenAI) do(ctx context.Context, body []byte) (wireResponse, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.opts.BaseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return wireResponse{}, fmt.Errorf("openai: build request: %w", err)
	}
	httpReq.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	httpReq.Header.Set("Content-Type", "application/json")
	res, err := c.http.Do(httpReq)
	if err != nil {
		// Network errors are retried; the URL never contains secrets.
		return wireResponse{}, fmt.Errorf("openai: %w: %v", domain.ErrUpstream, err)
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if err != nil {
		return wireResponse{}, fmt.Errorf("openai: read body: %w: %v", domain.ErrUpstream, err)
	}
	if res.StatusCode/100 != 2 {
		return wireResponse{}, decodeAPIError(res.StatusCode, raw)
	}
	var wr wireResponse
	if err := json.Unmarshal(raw, &wr); err != nil {
		return wireResponse{}, &APIError{StatusCode: res.StatusCode, Type: "decode_error", Message: err.Error()}
	}
	return wr, nil
}

// decodeAPIError turns an OpenAI error body into *APIError.
func decodeAPIError(status int, raw []byte) *APIError {
	e := &APIError{StatusCode: status}
	var we wireError
	if json.Unmarshal(raw, &we) == nil && we.Error.Message != "" {
		e.Message = we.Error.Message
		e.Type = we.Error.Type
		switch v := we.Error.Code.(type) {
		case string:
			e.Code = v
		case float64:
			e.Code = strconv.Itoa(int(v))
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
	e.Message = RedactKeys(e.Message)
	return e
}

func decodeResponse(wr wireResponse) (ChatResponse, error) {
	if len(wr.Choices) == 0 {
		return ChatResponse{}, &APIError{StatusCode: 200, Type: "empty_response", Message: "no choices"}
	}
	ch := wr.Choices[0]
	if ch.Message.Refusal != nil && *ch.Message.Refusal != "" {
		return ChatResponse{}, &APIError{StatusCode: 200, Type: "refusal", Message: *ch.Message.Refusal}
	}
	msg := Message{Role: RoleAssistant}
	if ch.Message.Content != nil {
		msg.Content = *ch.Message.Content
	}
	for _, tc := range ch.Message.ToolCalls {
		msg.ToolCalls = append(msg.ToolCalls, ToolCall{
			ID: tc.ID, Name: tc.Function.Name, Arguments: rawArgs(tc.Function.Arguments),
		})
	}
	return ChatResponse{
		Message:      msg,
		FinishReason: ch.FinishReason,
		Usage:        Usage{InputTokens: wr.Usage.PromptTokens, OutputTokens: wr.Usage.CompletionTokens},
		Model:        wr.Model,
	}, nil
}

// rawArgs keeps valid JSON as is; an empty string becomes {} and invalid JSON a JSON string.
func rawArgs(s string) json.RawMessage {
	s = strings.TrimSpace(s)
	if s == "" {
		return json.RawMessage(`{}`)
	}
	if json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	b, _ := json.Marshal(s)
	return b
}

var reKeyLike = regexp.MustCompile(`\b(?:sk|ek|rk)-[A-Za-z0-9_\-*.]{4,}`)

// RedactKeys masks anything that looks like an OpenAI key (sk-..., ek-...) in error text, since
// OpenAI echoes a partially masked key on 401s.
func RedactKeys(s string) string { return reKeyLike.ReplaceAllString(s, "[redacted]") }

func strPtr(s string) *string { return &s }
