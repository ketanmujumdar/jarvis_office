// Package audit writes the append-only audit trail (FR6.6). Owner: agent F.
//
// Every step that changes request, approval, payment or enrollment state records one event.
// Payloads must never contain secrets or card data: Record scrubs well-known sensitive keys
// (card numbers, CVV, expiry, last4, API keys, tokens) before writing, as a safety net.
package audit

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// Audit event types (dotted; the prefix groups them in the UI).
const (
	RequestCreated       = "request.created"
	RequestStatusChanged = "request.status_changed"
	RequestConfirmed     = "request.confirmed" // payload: {address_id}
	RequestCancelled     = "request.cancelled"
	LineItemsParsed      = "line_items.parsed"
	SearchCompleted      = "search.completed" // payload: {line_item_id, vendor_domain, offers, error?}
	PolicyEvaluated      = "policy.evaluated" // payload: policy.Result
	ApprovalRequested    = "approval.requested"
	ApprovalDecided      = "approval.decided"
	ReapQuoteCreated     = "reap.quote_created" // payload: {payment_id, reap_quote_id, final_cents}
	ReapPriceDrift       = "reap.price_drift"
	ReapCheckoutCreated  = "reap.checkout_created"
	ReapCheckoutStatus   = "reap.checkout_status"
	ReapWebhookReceived  = "reap.webhook_received" // payload: {event_id, type, resource_id}
	OrderCompleted       = "order.completed"
	// ReapChargeAfterClose: a checkout completed (money moved) after its request was already
	// failed or cancelled. payload: {payment_id, reap_checkout_id, request_status, final_cents}
	ReapChargeAfterClose = "reap.charge_after_close"
	// ReapFinalAmountMismatch: Reap charged more than the quoted amount. payload:
	// {payment_id, quoted_cents, final_cents}
	ReapFinalAmountMismatch = "reap.final_amount_mismatch"
	// CheckoutLimitExceeded: the live Reap total broke a hard policy limit at checkout time.
	// payload: {live_cents, reasons}
	CheckoutLimitExceeded = "checkout.limit_exceeded"
	EnrollmentStarted     = "enrollment.started"
	EnrollmentStatus      = "enrollment.status"
	AgentToolCall         = "agent.tool_call" // payload: {session_id, tool, args, ok, latency_ms}
	AgentLLMCall          = "agent.llm_call"  // payload: {model, input_tokens, output_tokens, latency_ms}
	AdminChanged          = "admin.changed"   // payload: {entity, id, action}
	UserLoggedIn          = "user.logged_in"  // payload: {role}
)

// Logger records audit events.
type Logger interface {
	// Record appends one event. payload is marshalled to JSON. requestID may be "".
	Record(ctx context.Context, requestID string, actor domain.ActorType, actorID, typ string, payload any) error
}

// New returns a Logger backed by the store's AuditRepo.
func New(s store.Store) Logger { return &storeLogger{s: s} }

type storeLogger struct{ s store.Store }

func (l *storeLogger) Record(ctx context.Context, requestID string, actor domain.ActorType, actorID, typ string, payload any) error {
	e, err := Build(requestID, actor, actorID, typ, payload)
	if err != nil {
		return err
	}
	return l.s.Audit().Append(ctx, &e)
}

// Build validates and assembles an AuditEvent (payload scrubbed). Exported for other Logger
// implementations and tests.
func Build(requestID string, actor domain.ActorType, actorID, typ string, payload any) (domain.AuditEvent, error) {
	if strings.TrimSpace(typ) == "" {
		return domain.AuditEvent{}, fmt.Errorf("%w: audit type is required", domain.ErrValidation)
	}
	switch actor {
	case domain.ActorUser, domain.ActorAgent, domain.ActorSystem:
	default:
		return domain.AuditEvent{}, fmt.Errorf("%w: unknown actor type %q", domain.ErrValidation, actor)
	}
	raw, err := marshalPayload(payload)
	if err != nil {
		return domain.AuditEvent{}, err
	}
	e := domain.AuditEvent{ActorType: actor, ActorID: actorID, Type: typ, Payload: raw}
	if requestID != "" {
		id := requestID
		e.RequestID = &id
	}
	return e, nil
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage(`{}`), nil
	}
	var b []byte
	switch p := payload.(type) {
	case json.RawMessage:
		b = p
	case []byte:
		b = p
	default:
		var err error
		if b, err = json.Marshal(payload); err != nil {
			return nil, fmt.Errorf("audit payload: %w", err)
		}
	}
	if len(b) == 0 {
		return json.RawMessage(`{}`), nil
	}
	var v any
	if err := json.Unmarshal(b, &v); err != nil {
		return nil, fmt.Errorf("%w: audit payload is not JSON: %v", domain.ErrValidation, err)
	}
	if _, ok := v.(map[string]any); !ok {
		// The column holds an object; wrap scalars and arrays.
		v = map[string]any{"value": v}
	}
	out, err := json.Marshal(Scrub(v))
	if err != nil {
		return nil, fmt.Errorf("audit payload: %w", err)
	}
	return out, nil
}

// Redacted replaces scrubbed values.
const Redacted = "[redacted]"

// sensitiveKeys are matched case-insensitively after removing '_' and '-'.
var sensitiveKeys = map[string]bool{
	"pan": true, "cardnumber": true, "number": true, "cvv": true, "cvc": true, "cvv2": true,
	"securitycode": true, "expiry": true, "expirymonth": true, "expiryyear": true, "expmonth": true,
	"expyear": true, "last4": true, "paymentmethod": true, "card": true,
	"apikey": true, "authorization": true, "secret": true, "clientsecret": true, "password": true,
	"signingsecret": true, "webhooksecret": true, "accesstoken": true, "refreshtoken": true,
}

// IsSensitiveKey reports whether a JSON key must never be stored.
func IsSensitiveKey(k string) bool {
	n := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(k))
	return sensitiveKeys[n]
}

// Scrub returns a deep copy of v (decoded JSON) with sensitive keys redacted.
func Scrub(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if IsSensitiveKey(k) {
				out[k] = Redacted
				continue
			}
			out[k] = Scrub(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, val := range t {
			out[i] = Scrub(val)
		}
		return out
	default:
		return v
	}
}

// Memory is an in-memory Logger for tests in other packages.
type Memory struct {
	mu     sync.Mutex
	events []domain.AuditEvent
	now    func() time.Time
}

// NewMemory returns an empty in-memory Logger.
func NewMemory() *Memory { return &Memory{now: time.Now} }

func (m *Memory) Record(ctx context.Context, requestID string, actor domain.ActorType, actorID, typ string, payload any) error {
	e, err := Build(requestID, actor, actorID, typ, payload)
	if err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	e.ID = int64(len(m.events) + 1)
	e.At = m.now().UTC()
	m.events = append(m.events, e)
	return nil
}

// Events returns a copy of everything recorded, oldest first.
func (m *Memory) Events() []domain.AuditEvent {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]domain.AuditEvent(nil), m.events...)
}

// Types returns the recorded event types in order (handy for assertions).
func (m *Memory) Types() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.events))
	for i, e := range m.events {
		out[i] = e.Type
	}
	return out
}

// Nop discards events.
type Nop struct{}

func (Nop) Record(context.Context, string, domain.ActorType, string, string, any) error { return nil }
