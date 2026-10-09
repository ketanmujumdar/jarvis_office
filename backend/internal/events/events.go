// Package events is the in-process pub/sub that feeds the SSE stream (GET /api/v1/events).
// Implemented by the foundation agent; extend additively (owner for changes: agent F).
//
// Delivery is best-effort: a slow subscriber whose buffer is full drops events (and the client
// recovers by re-fetching GET /requests/{id}). Persistent history lives in audit_events.
package events

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"
)

// Type is an SSE event name (the SSE `event:` field).
type Type string

// SSE event types. Data payloads are documented in docs/openapi.yaml (components/schemas/*Event).
const (
	RequestCreated       Type = "request.created"         // {request}
	RequestStatusChanged Type = "request.status_changed"  // {request_id, from, to, failure_reason?}
	LineItemsParsed      Type = "line_items.parsed"       // {request_id, line_items}
	SearchStarted        Type = "search.started"          // {request_id, line_item_id, vendors[]}
	SearchVendorResult   Type = "search.vendor_result"    // {request_id, line_item_id, vendor_domain, offers_found, error?}
	OffersRanked         Type = "offers.ranked"           // {request_id, line_item_id, offers (top 3)}
	PolicyEvaluated      Type = "policy.evaluated"        // {request_id, decision, reasons, lines[]}
	ApprovalRequested    Type = "approval.requested"      // {approval}
	ApprovalDecided      Type = "approval.decided"        // {approval}
	CheckoutQuoted       Type = "checkout.quoted"         // {request_id, payment}
	CheckoutPriceDrift   Type = "checkout.price_drift"    // {request_id, approved_cents, live_cents, pct}
	PaymentActionNeeded  Type = "payment.action_required" // {request_id, payment, approval_url}
	// CheckoutItemRejected: a merchant rejected one line at quote time (e.g. not enough stock).
	CheckoutItemRejected Type = "checkout.item_rejected" // {request_id, line_item_id, merchant, quantity, title, message}
	// CheckoutOfferReplaced: a rejected line moved to the next-ranked offer at another merchant.
	CheckoutOfferReplaced Type = "checkout.offer_replaced" // {request_id, line_item_id, from_offer, to_offer}
	PaymentStatusChanged  Type = "payment.status_changed"  // {request_id, payment}
	OrderCompleted        Type = "order.completed"         // {request_id, payments}
	PaymentAlert          Type = "payment.alert"           // {request_id, payment, kind, message}
	EnrollmentUpdated     Type = "enrollment.updated"      // {enrollment}
	AgentMessage          Type = "agent.message"           // {session_id, role, content}
	Heartbeat             Type = "heartbeat"               // {} every 15s (sent by the SSE handler)
)

// Event is one published event. ID is monotonically increasing per process (SSE `id:`).
type Event struct {
	ID        int64     `json:"id"`
	Type      Type      `json:"type"`
	RequestID string    `json:"request_id,omitempty"`
	At        time.Time `json:"at"`
	Data      any       `json:"data"`
}

// MarshalData returns the JSON for the SSE `data:` line (the whole Event).
func (e Event) MarshalData() ([]byte, error) { return json.Marshal(e) }

// Bus publishes and subscribes.
type Bus interface {
	Publish(t Type, requestID string, data any) Event
	// Subscribe returns a channel of events. requestID "" receives all events. Call cancel to
	// unsubscribe; the channel is closed after cancel.
	Subscribe(requestID string) (ch <-chan Event, cancel func())
}

// Memory is the in-process Bus.
type Memory struct {
	mu     sync.RWMutex
	subs   map[int64]*sub
	nextID atomic.Int64
	subID  int64
	buf    int
	now    func() time.Time
}

type sub struct {
	requestID string
	ch        chan Event
}

var _ Bus = (*Memory)(nil)

// NewMemory creates a bus with a per-subscriber buffer (default 64 if <= 0).
func NewMemory(buffer int) *Memory {
	if buffer <= 0 {
		buffer = 64
	}
	return &Memory{subs: map[int64]*sub{}, buf: buffer, now: time.Now}
}

func (m *Memory) Publish(t Type, requestID string, data any) Event {
	e := Event{ID: m.nextID.Add(1), Type: t, RequestID: requestID, At: m.now().UTC(), Data: data}
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, s := range m.subs {
		if s.requestID != "" && s.requestID != requestID {
			continue
		}
		select {
		case s.ch <- e:
		default: // drop for slow subscriber
		}
	}
	return e
}

func (m *Memory) Subscribe(requestID string) (<-chan Event, func()) {
	m.mu.Lock()
	m.subID++
	id := m.subID
	s := &sub{requestID: requestID, ch: make(chan Event, m.buf)}
	m.subs[id] = s
	m.mu.Unlock()
	var once sync.Once
	return s.ch, func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.subs, id)
			m.mu.Unlock()
			close(s.ch)
		})
	}
}
