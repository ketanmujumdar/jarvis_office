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

// OrderBackend is the slice of the orchestrator the tools need. The orchestrator implements it
// (orchestrator.Impl.Tools()); defining it here keeps agents free of an orchestrator import.
type OrderBackend interface {
	CreateOrder(ctx context.Context, userID string, args CreateOrderRequestArgs) (domain.PurchaseRequest, error)
	RequestDetail(ctx context.Context, requestID string) (domain.RequestDetail, error)
	ConfirmOrder(ctx context.Context, requestID, userID, addressID string) (domain.RequestDetail, error)
	CancelOrder(ctx context.Context, requestID, userID string) (domain.RequestDetail, error)
}

// Tools implements ToolExecutor over an OrderBackend and the address book.
type Tools struct {
	Backend OrderBackend
	Store   store.Store
	Audit   audit.Logger // optional
	Clock   func() time.Time
}

var _ ToolExecutor = (*Tools)(nil)

// NewTools builds the executor.
func NewTools(b OrderBackend, s store.Store, a audit.Logger) *Tools {
	return &Tools{Backend: b, Store: s, Audit: a, Clock: time.Now}
}

var toolDefs = []llm.Tool{
	{
		Name:        ToolCreateOrderRequest,
		Description: "Start a purchase request from what the user asked for. The system parses items, matches the approved catalog, searches allowed vendors and runs policy. Returns the request id; then poll get_request_status until status is quoted.",
		Parameters: json.RawMessage(`{"type":"object","additionalProperties":false,"required":["utterance"],"properties":{
"utterance":{"type":"string","description":"the user's request, verbatim"},
"items":{"type":"array","description":"optional pre-parsed items","items":{"type":"object","additionalProperties":false,"required":["description"],"properties":{
"description":{"type":"string"},"qty":{"type":"integer","minimum":1,"description":"omit for the catalog default ('the usual')"},"urgency":{"type":"string","enum":["normal","urgent"]}}}}}}`),
	},
	{
		Name:        ToolGetRequestStatus,
		Description: "Read the current state of a request: status, line items with best offer, unit price and line total, policy decisions with reasons, approvals and payment approval links.",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["request_id"],"properties":{"request_id":{"type":"string"}}}`),
	},
	{
		Name:        ToolListAddresses,
		Description: "List the saved Singapore delivery addresses (id, label, address, whether it is the default).",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false,"properties":{}}`),
	},
	{
		Name:        ToolConfirmOrder,
		Description: "Confirm a quoted request for delivery to the chosen address. Only call after the user explicitly agreed to the items and picked an address.",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["request_id","address_id"],"properties":{"request_id":{"type":"string"},"address_id":{"type":"string"}}}`),
	},
	{
		Name:        ToolCancelRequest,
		Description: "Cancel a request the user no longer wants. Not possible once the Reap payment links are open (status awaiting_payment or later).",
		Parameters:  json.RawMessage(`{"type":"object","additionalProperties":false,"required":["request_id"],"properties":{"request_id":{"type":"string"}}}`),
	},
}

// Definitions returns the tool JSON schemas.
func (t *Tools) Definitions() []llm.Tool {
	out := make([]llm.Tool, len(toolDefs))
	copy(out, toolDefs)
	return out
}

// Execute runs one tool. Tool-level failures (bad args, not found, wrong state, upstream errors)
// come back as {"error": ..., "code": ...} so the model can explain them.
func (t *Tools) Execute(ctx context.Context, tc ToolContext, name string, args json.RawMessage) (json.RawMessage, error) {
	start := t.now()
	args = normalizeArgs(args)
	result, err := t.dispatch(ctx, tc, name, args)
	if err != nil {
		result = map[string]any{"error": err.Error(), "code": errorCode(err)}
	}
	out, mErr := json.Marshal(result)
	if mErr != nil {
		return nil, fmt.Errorf("marshal tool result: %w", mErr)
	}
	if t.Audit != nil {
		reqID := requestIDOf(args, result)
		_ = t.Audit.Record(ctx, reqID, domain.ActorAgent, tc.UserID, audit.AgentToolCall, map[string]any{
			"session_id": tc.SessionID, "channel": tc.Channel, "tool": name, "args": json.RawMessage(args),
			"ok": err == nil, "latency_ms": t.now().Sub(start).Milliseconds(),
		})
	}
	return out, nil
}

func (t *Tools) now() time.Time {
	if t.Clock != nil {
		return t.Clock()
	}
	return time.Now()
}

// requireBuyer enforces the REST rule that only managers and admins create, confirm or cancel
// purchase requests, so the agent cannot be used to bypass it.
func (t *Tools) requireBuyer(ctx context.Context, tc ToolContext) error {
	role := tc.Role
	if role == "" && tc.UserID != "" && t.Store != nil {
		if u, err := t.Store.Users().Get(ctx, tc.UserID); err == nil {
			role = u.Role
		}
	}
	if role != domain.RoleManager && role != domain.RoleAdmin {
		return fmt.Errorf("%w: only an office manager or admin can create, confirm or cancel orders", domain.ErrForbidden)
	}
	return nil
}

func (t *Tools) dispatch(ctx context.Context, tc ToolContext, name string, args json.RawMessage) (any, error) {
	switch name {
	case ToolCreateOrderRequest, ToolConfirmOrder, ToolCancelRequest:
		if err := t.requireBuyer(ctx, tc); err != nil {
			return nil, err
		}
	}
	switch name {
	case ToolCreateOrderRequest:
		var a CreateOrderRequestArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if strings.TrimSpace(a.Utterance) == "" && len(a.Items) == 0 {
			return nil, fmt.Errorf("%w: utterance is required", domain.ErrValidation)
		}
		r, err := t.Backend.CreateOrder(ctx, tc.UserID, a)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"request_id": r.ID, "status": r.Status,
			"message": "Request created. Checking the catalog and allowed vendors now; call get_request_status until the status is quoted.",
		}, nil
	case ToolGetRequestStatus:
		var a RequestIDArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.RequestID == "" {
			return nil, fmt.Errorf("%w: request_id is required", domain.ErrValidation)
		}
		d, err := t.Backend.RequestDetail(ctx, a.RequestID)
		if err != nil {
			return nil, err
		}
		return SummarizeRequest(d), nil
	case ToolListAddresses:
		addrs, err := t.Store.Addresses().List(ctx)
		if err != nil {
			return nil, err
		}
		out := make([]map[string]any, 0, len(addrs))
		for _, a := range addrs {
			out = append(out, map[string]any{
				"id": a.ID, "label": a.Label, "is_default": a.IsDefault,
				"address": formatAddress(a), "recipient": strings.TrimSpace(a.FirstName + " " + a.LastName),
			})
		}
		return map[string]any{"addresses": out}, nil
	case ToolConfirmOrder:
		var a ConfirmOrderArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		if a.RequestID == "" || a.AddressID == "" {
			return nil, fmt.Errorf("%w: request_id and address_id are required (ask the user which delivery address to use)", domain.ErrValidation)
		}
		d, err := t.Backend.ConfirmOrder(ctx, a.RequestID, tc.UserID, a.AddressID)
		if err != nil {
			return nil, err
		}
		return SummarizeRequest(d), nil
	case ToolCancelRequest:
		var a RequestIDArgs
		if err := decodeArgs(args, &a); err != nil {
			return nil, err
		}
		d, err := t.Backend.CancelOrder(ctx, a.RequestID, tc.UserID)
		if err != nil {
			return nil, err
		}
		return SummarizeRequest(d), nil
	}
	return nil, fmt.Errorf("%w: unknown tool %q", domain.ErrNotFound, name)
}

// normalizeArgs accepts an object or a JSON-encoded string containing an object (Realtime relays
// may double-encode) and maps empty input to {}.
func normalizeArgs(args json.RawMessage) json.RawMessage {
	s := strings.TrimSpace(string(args))
	if s == "" || s == "null" {
		return json.RawMessage(`{}`)
	}
	if strings.HasPrefix(s, `"`) {
		var inner string
		if json.Unmarshal([]byte(s), &inner) == nil {
			if inner = strings.TrimSpace(inner); inner == "" {
				return json.RawMessage(`{}`)
			}
			return json.RawMessage(inner)
		}
	}
	return json.RawMessage(s)
}

func decodeArgs(args json.RawMessage, v any) error {
	if err := json.Unmarshal(args, v); err != nil {
		return fmt.Errorf("%w: invalid arguments: %v", domain.ErrValidation, err)
	}
	return nil
}

func errorCode(err error) string {
	switch {
	case errors.Is(err, domain.ErrNoActiveEnrollment):
		return "no_active_enrollment"
	case errors.Is(err, domain.ErrValidation):
		return "validation_failed"
	case errors.Is(err, domain.ErrNotFound):
		return "not_found"
	case errors.Is(err, domain.ErrInvalidTransition):
		return "invalid_transition"
	case errors.Is(err, domain.ErrConflict):
		return "conflict"
	case errors.Is(err, domain.ErrForbidden):
		return "forbidden"
	case errors.Is(err, domain.ErrUpstream):
		return "upstream_error"
	}
	return "internal_error"
}

func requestIDOf(args json.RawMessage, result any) string {
	var a struct {
		RequestID string `json:"request_id"`
	}
	_ = json.Unmarshal(args, &a)
	if a.RequestID != "" {
		return a.RequestID
	}
	switch r := result.(type) {
	case map[string]any:
		if id, ok := r["request_id"].(string); ok {
			return id
		}
	case RequestSummary:
		return r.RequestID
	}
	return ""
}

func formatAddress(a domain.Address) string {
	parts := []string{a.AddressLine1}
	if a.AddressLine2 != "" {
		parts = append(parts, a.AddressLine2)
	}
	parts = append(parts, strings.TrimSpace("Singapore "+a.PostalCode))
	return strings.Join(parts, ", ")
}

// RequestSummary is the compact, model-friendly view of a request returned by tools.
type RequestSummary struct {
	RequestID     string           `json:"request_id"`
	Status        string           `json:"status"`
	NextStep      string           `json:"next_step"`
	Decision      string           `json:"decision,omitempty"`
	Lines         []LineSummary    `json:"lines"`
	Subtotal      string           `json:"subtotal_sgd"`
	Shipping      string           `json:"shipping_sgd"`
	Total         string           `json:"total_sgd"`
	Address       string           `json:"delivery_address,omitempty"`
	Approval      *ApprovalSummary `json:"approval,omitempty"`
	Payments      []PaymentSummary `json:"payments,omitempty"`
	FailureReason string           `json:"failure_reason,omitempty"`
}

// LineSummary is one line item in a RequestSummary.
type LineSummary struct {
	Item      string   `json:"item"`
	Qty       int      `json:"qty"`
	OnCatalog bool     `json:"on_catalog"`
	Decision  string   `json:"decision,omitempty"`
	Reasons   []string `json:"reasons,omitempty"`
	Vendor    string   `json:"vendor,omitempty"`
	Product   string   `json:"product,omitempty"`
	UnitPrice string   `json:"unit_price_sgd,omitempty"`
	BuyQty    int      `json:"buy_qty,omitempty"` // variants to buy (pack-size aware)
	LineTotal string   `json:"line_total_sgd,omitempty"`
}

// ApprovalSummary is the latest approval.
type ApprovalSummary struct {
	Status  string   `json:"status"`
	Kind    string   `json:"kind"`
	Amount  string   `json:"amount_sgd"`
	Prev    string   `json:"previous_amount_sgd,omitempty"`
	Reasons []string `json:"reasons,omitempty"`
}

// PaymentSummary is one merchant checkout.
type PaymentSummary struct {
	Merchant    string `json:"merchant"`
	Status      string `json:"status"`
	Amount      string `json:"amount_sgd"`
	ApprovalURL string `json:"payment_approval_url,omitempty"`
}

// SummarizeRequest builds the tool view of a request detail.
func SummarizeRequest(d domain.RequestDetail) RequestSummary {
	r := d.Request
	s := RequestSummary{
		RequestID: r.ID, Status: string(r.Status), Decision: string(r.Decision),
		Subtotal: r.SubtotalCents.String(), Shipping: r.ShippingCents.String(), Total: r.TotalCents.String(),
		FailureReason: r.FailureReason, Lines: []LineSummary{},
	}
	offers := map[string]domain.Offer{}
	for _, o := range d.Offers {
		offers[o.ID] = o
	}
	for _, li := range d.LineItems {
		ls := LineSummary{Item: li.Description, Qty: li.Qty, OnCatalog: li.CatalogItemID != nil, Decision: string(li.PolicyDecision)}
		for _, rs := range li.Reasons {
			ls.Reasons = append(ls.Reasons, rs.Message)
		}
		if li.SelectedOfferID != nil {
			if o, ok := offers[*li.SelectedOfferID]; ok {
				ls.Vendor, ls.Product = o.MerchantName, o.Title
				ls.UnitPrice = o.UnitPriceCents.String()
				ls.BuyQty = PurchaseQty(li.Qty, o.PackSize)
				ls.LineTotal = o.LandedCostCents.String()
			}
		}
		s.Lines = append(s.Lines, ls)
	}
	if d.Address != nil {
		s.Address = d.Address.Label + " (" + formatAddress(*d.Address) + ")"
	}
	if n := len(d.Approvals); n > 0 {
		a := latestApproval(d.Approvals)
		as := &ApprovalSummary{Status: string(a.Status), Kind: string(a.Kind), Amount: a.AmountCents.String()}
		if a.PrevCents != nil {
			as.Prev = a.PrevCents.String()
		}
		for _, rs := range a.Reasons {
			as.Reasons = append(as.Reasons, rs.Message)
		}
		s.Approval = as
	}
	for _, p := range d.Payments {
		amt := p.QuotedCents
		if p.FinalCents != nil {
			amt = *p.FinalCents
		}
		s.Payments = append(s.Payments, PaymentSummary{Merchant: p.MerchantName, Status: string(p.Status), Amount: amt.String(), ApprovalURL: p.ApprovalURL})
	}
	s.NextStep = nextStep(r.Status)
	return s
}

func latestApproval(as []domain.Approval) domain.Approval {
	best := as[0]
	for _, a := range as[1:] {
		if a.CreatedAt.After(best.CreatedAt) {
			best = a
		}
	}
	return best
}

func nextStep(s domain.RequestStatus) string {
	switch s {
	case domain.StatusParsing, domain.StatusSearching:
		return "Still checking prices. Call get_request_status again shortly."
	case domain.StatusQuoted:
		return "Read back the lines and total, say which need approval, ask which delivery address to use, then call confirm_order after the user agrees."
	case domain.StatusPendingApproval:
		return "Waiting for an approver to decide."
	case domain.StatusApproved, domain.StatusCheckingOut:
		return "Approved. Getting live quotes from the vendors."
	case domain.StatusAwaitingPayment:
		return "The user must open the payment approval page(s) to approve the charge with Reap. The order is not placed yet."
	case domain.StatusPaying:
		return "Payment is processing. The order is not placed yet."
	case domain.StatusOrdered:
		return "The order is placed and paid."
	case domain.StatusRejected:
		return "The request was rejected."
	case domain.StatusFailed:
		return "The request failed; explain the failure reason and offer to try again."
	case domain.StatusCancelled:
		return "The request was cancelled."
	}
	return ""
}
