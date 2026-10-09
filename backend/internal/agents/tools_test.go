package agents

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// fakeBackend is an in-memory OrderBackend.
type fakeBackend struct {
	reqs       map[string]domain.RequestDetail
	confirmErr error
	created    []CreateOrderRequestArgs
}

func newFakeBackend() *fakeBackend { return &fakeBackend{reqs: map[string]domain.RequestDetail{}} }

func (b *fakeBackend) CreateOrder(ctx context.Context, userID string, a CreateOrderRequestArgs) (domain.PurchaseRequest, error) {
	b.created = append(b.created, a)
	id := fmt.Sprintf("req-%d", len(b.created))
	off := "off-1"
	cat := "cat-1"
	b.reqs[id] = domain.RequestDetail{
		Request: domain.PurchaseRequest{ID: id, RequesterID: userID, Status: domain.StatusQuoted, SubtotalCents: 7100, TotalCents: 7100, Decision: domain.DecisionAutoApprove},
		LineItems: []domain.LineItem{{ID: "li-1", Description: "A4 Copier Paper", Qty: 10, CatalogItemID: &cat, PolicyDecision: domain.DecisionAutoApprove,
			SelectedOfferID: &off, Reasons: []domain.Reason{}}},
		Offers: []domain.Offer{{ID: off, MerchantName: "Popular Bookstore", Title: "IK Copier Paper", UnitPriceCents: 710, PackSize: 1, LandedCostCents: 7100}},
	}
	return b.reqs[id].Request, nil
}

func (b *fakeBackend) RequestDetail(ctx context.Context, id string) (domain.RequestDetail, error) {
	d, ok := b.reqs[id]
	if !ok {
		return d, fmt.Errorf("request %s: %w", id, domain.ErrNotFound)
	}
	return d, nil
}

func (b *fakeBackend) ConfirmOrder(ctx context.Context, id, userID, addressID string) (domain.RequestDetail, error) {
	if b.confirmErr != nil {
		return domain.RequestDetail{}, b.confirmErr
	}
	d, err := b.RequestDetail(ctx, id)
	if err != nil {
		return d, err
	}
	d.Request.Status = domain.StatusAwaitingPayment
	d.Payments = []domain.Payment{{MerchantName: "Popular Bookstore", Status: domain.PaymentRequiresAction, QuotedCents: 7100, ApprovalURL: "https://reap.example/approve/1"}}
	b.reqs[id] = d
	return d, nil
}

func (b *fakeBackend) CancelOrder(ctx context.Context, id, userID string) (domain.RequestDetail, error) {
	d, err := b.RequestDetail(ctx, id)
	if err != nil {
		return d, err
	}
	if d.Request.Status == domain.StatusPaying {
		return d, fmt.Errorf("%w: paying -> cancelled", domain.ErrInvalidTransition)
	}
	d.Request.Status = domain.StatusCancelled
	b.reqs[id] = d
	return d, nil
}

func TestToolDefinitions(t *testing.T) {
	defs := (&Tools{}).Definitions()
	want := []string{ToolCreateOrderRequest, ToolGetRequestStatus, ToolListAddresses, ToolConfirmOrder, ToolCancelRequest}
	if len(defs) != len(want) {
		t.Fatalf("defs = %d", len(defs))
	}
	for i, d := range defs {
		if d.Name != want[i] || d.Description == "" {
			t.Fatalf("def %d = %+v", i, d)
		}
		var schema map[string]any
		if err := json.Unmarshal(d.Parameters, &schema); err != nil || schema["type"] != "object" {
			t.Fatalf("%s schema invalid: %v", d.Name, err)
		}
	}
}

func TestToolsExecute(t *testing.T) {
	st := seeded(t)
	b := newFakeBackend()
	rec := &recAudit{}
	tools := NewTools(b, st, rec)
	ctx := context.Background()
	tc := ToolContext{UserID: "u1", Role: domain.RoleManager, SessionID: "s1", Channel: "text"}
	run := func(name, args string) map[string]any {
		t.Helper()
		out, err := tools.Execute(ctx, tc, name, json.RawMessage(args))
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		if err := json.Unmarshal(out, &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	cases := []struct {
		name, tool, args string
		check            func(t *testing.T, m map[string]any)
	}{
		{"create", ToolCreateOrderRequest, `{"utterance":"10 reams of paper"}`, func(t *testing.T, m map[string]any) {
			if m["request_id"] != "req-1" || m["status"] != "quoted" {
				t.Fatalf("%v", m)
			}
		}},
		{"create needs utterance", ToolCreateOrderRequest, `{}`, wantCode("validation_failed")},
		{"bad json", ToolCreateOrderRequest, `{"utterance":`, wantCode("validation_failed")},
		{"status", ToolGetRequestStatus, `{"request_id":"req-1"}`, func(t *testing.T, m map[string]any) {
			lines := m["lines"].([]any)
			l := lines[0].(map[string]any)
			if m["status"] != "quoted" || m["total_sgd"] != "71.00" || l["vendor"] != "Popular Bookstore" || l["unit_price_sgd"] != "7.10" ||
				l["line_total_sgd"] != "71.00" || l["buy_qty"] != float64(10) || !strings.Contains(m["next_step"].(string), "address") {
				t.Fatalf("%v", m)
			}
		}},
		{"status unknown", ToolGetRequestStatus, `{"request_id":"nope"}`, wantCode("not_found")},
		{"status missing id", ToolGetRequestStatus, `null`, wantCode("validation_failed")},
		{"addresses", ToolListAddresses, ``, func(t *testing.T, m map[string]any) {
			as := m["addresses"].([]any)
			if len(as) < 3 {
				t.Fatalf("%v", m)
			}
			first := as[0].(map[string]any)
			if first["is_default"] != true || !strings.Contains(first["address"].(string), "Singapore") {
				t.Fatalf("first = %v", first)
			}
		}},
		{"confirm needs address", ToolConfirmOrder, `{"request_id":"req-1"}`, wantCode("validation_failed")},
		{"confirm (double-encoded args)", ToolConfirmOrder, `"{\"request_id\":\"req-1\",\"address_id\":\"a1\"}"`, func(t *testing.T, m map[string]any) {
			ps := m["payments"].([]any)
			if m["status"] != "awaiting_payment" || ps[0].(map[string]any)["payment_approval_url"] == "" {
				t.Fatalf("%v", m)
			}
		}},
		{"cancel", ToolCancelRequest, `{"request_id":"req-1"}`, func(t *testing.T, m map[string]any) {
			if m["status"] != "cancelled" {
				t.Fatalf("%v", m)
			}
		}},
		{"unknown tool", "launch_rocket", `{}`, wantCode("not_found")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) { c.check(t, run(c.tool, c.args)) })
	}
	b.confirmErr = domain.ErrNoActiveEnrollment
	wantCode("no_active_enrollment")(t, run(ToolConfirmOrder, `{"request_id":"req-1","address_id":"a1"}`))

	if len(rec.types) != len(cases)+1 || rec.types[0] != audit.AgentToolCall || rec.reqIDs[0] != "req-1" {
		t.Fatalf("tool calls not audited: %v %v", rec.types, rec.reqIDs)
	}
}

type recAudit struct {
	types, reqIDs []string
}

func (r *recAudit) Record(ctx context.Context, requestID string, actor domain.ActorType, actorID, typ string, payload any) error {
	r.types = append(r.types, typ)
	r.reqIDs = append(r.reqIDs, requestID)
	return nil
}

func anyUser(t *testing.T, st store.Store, role domain.Role) domain.User {
	t.Helper()
	us, err := st.Users().List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range us {
		if u.Role == role {
			return u
		}
	}
	t.Fatalf("no %s user", role)
	return domain.User{}
}

func wantCode(code string) func(t *testing.T, m map[string]any) {
	return func(t *testing.T, m map[string]any) {
		t.Helper()
		if m["code"] != code || m["error"] == nil {
			t.Fatalf("want code %s, got %v", code, m)
		}
	}
}

func TestChatAgent(t *testing.T) {
	st := seeded(t)
	ctx := context.Background()
	admin, mgr := anyUser(t, st, domain.RoleAdmin), anyUser(t, st, domain.RoleManager)
	if _, err := st.SystemPrompts().Update(ctx, "agent", "CUSTOM PROMPT FROM ADMIN", admin.ID); err != nil {
		t.Fatal(err)
	}
	b := newFakeBackend()
	f := fakellm.New().
		PushToolCall(ToolCreateOrderRequest, map[string]any{"utterance": "10 reams of paper"}).
		PushToolCall(ToolGetRequestStatus, map[string]any{"request_id": "req-1"}).
		PushText("10 reams of A4 paper from Popular Bookstore, S$71.00. Which address should I use?")
	a := &ChatAgent{LLM: f, Store: st, Tools: NewTools(b, st, nil), Audit: audit.New(st), Clock: func() time.Time { return time.Date(2026, 10, 9, 2, 0, 0, 0, time.UTC) }}

	out, err := a.Chat(ctx, ChatInput{UserID: mgr.ID, Message: "We need 10 reams of paper"})
	if err != nil {
		t.Fatal(err)
	}
	if out.SessionID == "" || !strings.Contains(out.Reply, "Which address") || len(out.ToolCalls) != 2 ||
		len(out.RequestIDs) != 1 || out.RequestIDs[0] != "req-1" {
		t.Fatalf("out = %+v", out)
	}
	reqs := f.Requests()
	if len(reqs) != 3 || reqs[0].Messages[0].Role != llm.RoleSystem || !strings.Contains(reqs[0].Messages[0].Content, "CUSTOM PROMPT FROM ADMIN") ||
		!strings.Contains(reqs[0].Messages[0].Content, "Friday 9 October 2026") || len(reqs[0].Tools) != 5 {
		t.Fatalf("first llm request = %+v", reqs[0])
	}
	// Second request carries the tool result for the first call.
	last := reqs[1].Messages[len(reqs[1].Messages)-1]
	if last.Role != llm.RoleTool || !strings.Contains(last.Content, "req-1") {
		t.Fatalf("tool result not fed back: %+v", last)
	}

	hist, err := a.History(ctx, out.SessionID)
	if err != nil {
		t.Fatal(err)
	}
	// user, assistant(tool call), tool, assistant(tool call), tool, assistant(text)
	if len(hist) != 6 || hist[0].Role != domain.ChatRoleUser || hist[5].Role != domain.ChatRoleAssistant || len(hist[1].ToolCalls) == 0 {
		t.Fatalf("history = %+v", hist)
	}

	// Next turn in the same session sends the history.
	f.PushText("Done.")
	if _, err := a.Chat(ctx, ChatInput{SessionID: out.SessionID, UserID: mgr.ID, Message: "Use the default address"}); err != nil {
		t.Fatal(err)
	}
	reqs = f.Requests()
	if n := len(reqs[3].Messages); n != 1+6+1 {
		t.Fatalf("second turn messages = %d", n)
	}
}

func TestChatAgent_StepLimitAndErrors(t *testing.T) {
	st := seeded(t)
	ctx := context.Background()
	mgr := anyUser(t, st, domain.RoleManager)
	f := fakellm.New()
	for i := 0; i < 3; i++ {
		f.PushToolCall(ToolListAddresses, map[string]any{})
	}
	f.PushText("Here are the addresses.")
	a := &ChatAgent{LLM: f, Store: st, Tools: NewTools(newFakeBackend(), st, nil), MaxSteps: 3}
	out, err := a.Chat(ctx, ChatInput{UserID: mgr.ID, Message: "addresses?"})
	if err != nil {
		t.Fatal(err)
	}
	reqs := f.Requests()
	if len(reqs) != 3 || reqs[2].ToolChoice != llm.ToolChoiceNone || out.Reply == "" {
		t.Fatalf("requests %d, last choice %q, reply %q", len(reqs), reqs[len(reqs)-1].ToolChoice, out.Reply)
	}
	if _, err := a.Chat(ctx, ChatInput{UserID: mgr.ID, Message: "  "}); !errors.Is(err, domain.ErrValidation) {
		t.Fatalf("empty message err = %v", err)
	}
	a.LLM = fakellm.New().PushError(errors.New("openai down"))
	if _, err := a.Chat(ctx, ChatInput{UserID: mgr.ID, Message: "hi"}); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("llm error = %v", err)
	}
}

func TestHistoryToLLM_DropsDanglingFragments(t *testing.T) {
	calls, _ := json.Marshal([]llm.ToolCall{{ID: "c2", Name: ToolListAddresses}})
	h := []domain.ChatMessage{
		{Role: domain.ChatRoleTool, ToolCallID: "c1", Content: "{}"}, // window starts mid exchange
		{Role: domain.ChatRoleAssistant, Content: "partial"},
		{Role: domain.ChatRoleUser, Content: "hi"},
		{Role: domain.ChatRoleAssistant, ToolCalls: calls},
		{Role: domain.ChatRoleTool, ToolCallID: "c2", Content: "{}"},
		{Role: domain.ChatRoleTool, ToolCallID: "c9", Content: "{}"}, // orphan
		{Role: domain.ChatRoleSystem, Content: "ignored"},
	}
	got := historyToLLM(h)
	if len(got) != 3 || got[0].Role != llm.RoleUser || len(got[1].ToolCalls) != 1 || got[2].ToolCallID != "c2" {
		t.Fatalf("got %+v", got)
	}
}

// The agent must not let an approver (or an unknown user) create, confirm or cancel orders: the
// REST endpoints are manager|admin only and the tools enforce the same rule.
func TestToolsExecute_RoleGate(t *testing.T) {
	st := seeded(t)
	ctx := context.Background()
	approver := anyUser(t, st, domain.RoleApprover)
	mgr := anyUser(t, st, domain.RoleManager)
	cases := []struct {
		name     string
		tc       ToolContext
		tool     string
		args     string
		wantCode string // "" = allowed
	}{
		{"approver create", ToolContext{UserID: approver.ID, Role: domain.RoleApprover}, ToolCreateOrderRequest, `{"utterance":"paper"}`, "forbidden"},
		{"approver confirm", ToolContext{UserID: approver.ID, Role: domain.RoleApprover}, ToolConfirmOrder, `{"request_id":"req-1","address_id":"a"}`, "forbidden"},
		{"approver cancel", ToolContext{UserID: approver.ID, Role: domain.RoleApprover}, ToolCancelRequest, `{"request_id":"req-1"}`, "forbidden"},
		{"approver role looked up", ToolContext{UserID: approver.ID}, ToolCreateOrderRequest, `{"utterance":"paper"}`, "forbidden"},
		{"unknown user", ToolContext{UserID: "nobody"}, ToolCreateOrderRequest, `{"utterance":"paper"}`, "forbidden"},
		{"approver may read status", ToolContext{UserID: approver.ID, Role: domain.RoleApprover}, ToolListAddresses, `{}`, ""},
		{"manager role looked up", ToolContext{UserID: mgr.ID}, ToolCreateOrderRequest, `{"utterance":"paper"}`, ""},
		{"admin create", ToolContext{UserID: "x", Role: domain.RoleAdmin}, ToolCreateOrderRequest, `{"utterance":"paper"}`, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b := newFakeBackend()
			out, err := NewTools(b, st, nil).Execute(ctx, tc.tc, tc.tool, json.RawMessage(tc.args))
			if err != nil {
				t.Fatal(err)
			}
			var m map[string]any
			if err := json.Unmarshal(out, &m); err != nil {
				t.Fatal(err)
			}
			got, _ := m["code"].(string)
			if got != tc.wantCode {
				t.Fatalf("code = %q, want %q (%v)", got, tc.wantCode, m)
			}
			if tc.wantCode == "forbidden" && len(b.created) != 0 {
				t.Fatal("forbidden call reached the backend")
			}
		})
	}
}

type flipBackend struct {
	*fakeBackend
	mu    sync.Mutex
	calls int
}

// RequestDetail reports "searching" for the first two reads, then the real (quoted) state.
func (b *flipBackend) RequestDetail(ctx context.Context, id string) (domain.RequestDetail, error) {
	b.mu.Lock()
	b.calls++
	n := b.calls
	b.mu.Unlock()
	d, err := b.fakeBackend.RequestDetail(ctx, id)
	if err == nil && n <= 2 {
		d.Request.Status = domain.StatusSearching
	}
	return d, err
}

// One get_request_status call waits out the search instead of the model polling in a loop.
func TestGetRequestStatus_WaitsWhileSearching(t *testing.T) {
	b := &flipBackend{fakeBackend: newFakeBackend()}
	r, _ := b.CreateOrder(context.Background(), "u1", CreateOrderRequestArgs{Utterance: "paper"})
	tools := NewTools(b, seeded(t), nil)
	out, err := tools.Execute(context.Background(), ToolContext{UserID: "u1", Role: domain.RoleManager, Channel: "voice"},
		ToolGetRequestStatus, json.RawMessage(`{"request_id":"`+r.ID+`"}`))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"status":"quoted"`) {
		t.Fatalf("want quoted after waiting, got %s", out)
	}
	if b.calls != 3 {
		t.Fatalf("detail reads = %d, want 3", b.calls)
	}

	old := StatusWait
	StatusWait = 0
	defer func() { StatusWait = old }()
	b.calls = 0
	out, _ = tools.Execute(context.Background(), ToolContext{UserID: "u1", Role: domain.RoleManager, Channel: "voice"},
		ToolGetRequestStatus, json.RawMessage(`{"request_id":"`+r.ID+`"}`))
	if !strings.Contains(string(out), `"status":"searching"`) {
		t.Fatalf("deadline passed: want searching returned as-is, got %s", out)
	}
}
