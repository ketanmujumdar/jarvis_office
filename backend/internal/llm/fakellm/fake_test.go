package fakellm_test

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/agents"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/llm/fakellm"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func seedCatalog(t *testing.T) []llm.CatalogHint {
	t.Helper()
	d, err := seed.Parse("../../../seed")
	if err != nil {
		t.Fatalf("seed.Parse: %v", err)
	}
	var out []llm.CatalogHint
	for _, it := range d.Items {
		out = append(out, llm.CatalogHint{SKU: it.SKU, Name: it.Name, Aliases: it.Aliases, Unit: it.Unit, DefaultQty: it.DefaultQty})
	}
	return out
}

type wantItem struct {
	sku string
	qty int // 0 = nil
}

func TestParseUtterance_SeedCatalog(t *testing.T) {
	cat := seedCatalog(t)
	cases := []struct {
		utt    string
		urgent bool
		want   []wantItem
	}{
		{"We need 10 reams of printer paper and 20 pens", false, []wantItem{{"a4-copier-paper-ream", 10}, {"ballpoint-pen", 20}}},
		{"Order the usual coffee beans, 3 boxes of drip coffee bags and some tea", false, []wantItem{{"coffee-beans-espresso-250g", 0}, {"drip-coffee-bags", 3}, {"tea-earl-grey", 0}}},
		{"urgent: two USB-C hubs and an HDMI cable", true, []wantItem{{"usb-c-hub", 2}, {"hdmi-cable", 1}}},
		{"get us a dozen whiteboard markers please", false, []wantItem{{"whiteboard-marker", 12}}},
		{"Running low on hand sanitizer; also 5 rolls of bin bags.", false, []wantItem{{"hand-sanitizer", 0}, {"garbage-bags", 5}}},
		{"Could you buy 2 ergonomic chairs ASAP", true, []wantItem{{"ergonomic-chair", 2}}},
		{"3 x post-it notes", false, []wantItem{{"sticky-notes", 3}}},
		{"aa batteries x8 and triple a batteries", false, []wantItem{{"battery-aa-4", 8}, {"battery-aaa-4", 0}}},
		{"a standing desk", false, []wantItem{{"", 1}}},
		{"order battery-aaa-4", false, []wantItem{{"battery-aaa-4", 0}}},
		{"hi jarvis, thanks", false, nil},
	}
	for _, tc := range cases {
		t.Run(tc.utt, func(t *testing.T) {
			got := fakellm.ParseUtterance(tc.utt, cat)
			if len(got) != len(tc.want) {
				t.Fatalf("got %d items %s, want %d", len(got), js(got), len(tc.want))
			}
			for i, w := range tc.want {
				g := got[i]
				if g.CatalogSKU != w.sku {
					t.Errorf("item %d sku = %q want %q (desc %q)", i, g.CatalogSKU, w.sku, g.Description)
				}
				switch {
				case w.qty == 0 && g.Qty != nil:
					t.Errorf("item %d qty = %d want nil", i, *g.Qty)
				case w.qty != 0 && (g.Qty == nil || *g.Qty != w.qty):
					t.Errorf("item %d qty = %v want %d", i, g.Qty, w.qty)
				}
				wantU := domain.UrgencyNormal
				if tc.urgent {
					wantU = domain.UrgencyUrgent
				}
				if g.Urgency != wantU {
					t.Errorf("item %d urgency = %q", i, g.Urgency)
				}
				if g.Description == "" {
					t.Errorf("item %d empty description", i)
				}
			}
		})
	}
}

func TestRuleMatch(t *testing.T) {
	target := llm.MatchTarget{Name: "Espresso Coffee Beans 250g", Aliases: []string{"coffee beans"}, Unit: "bag (250g)"}
	cases := []struct {
		title string
		want  bool
	}{
		{"Common Man Coffee Roasters Espresso Beans 250g", true},
		{"Bettr Barista whole coffee beans", true},
		{"Espresso machine descaler", false}, // only 1 of 3 significant name words
		{"Ceramic mug", false},
	}
	for _, tc := range cases {
		if got, reason := fakellm.RuleMatch(target, tc.title); got != tc.want {
			t.Errorf("RuleMatch(%q) = %v (%s) want %v", tc.title, got, reason, tc.want)
		}
	}
}

func TestScriptedQueueAndRecording(t *testing.T) {
	f := fakellm.New().PushText("one").PushToolCall("list_addresses", map[string]any{}).PushError(domain.ErrUpstream)
	ctx := context.Background()
	req := llm.ChatRequest{Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}
	r1, err := f.Chat(ctx, req)
	if err != nil || r1.Message.Content != "one" || r1.Model != fakellm.Model {
		t.Fatalf("r1 = %+v %v", r1, err)
	}
	r2, err := f.Chat(ctx, req)
	if err != nil || len(r2.Message.ToolCalls) != 1 || r2.Message.ToolCalls[0].Name != "list_addresses" || r2.FinishReason != "tool_calls" {
		t.Fatalf("r2 = %+v %v", r2, err)
	}
	if _, err := f.Chat(ctx, req); !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("r3 err = %v", err)
	}
	if f.Pending() != 0 || len(f.Requests()) != 3 {
		t.Fatalf("pending=%d requests=%d", f.Pending(), len(f.Requests()))
	}
	// Queue empty: falls back to rules.
	r4, err := f.Chat(ctx, req)
	if err != nil || r4.Message.Content == "" {
		t.Fatalf("r4 = %+v %v", r4, err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := f.Chat(cctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled ctx err = %v", err)
	}
}

// ---- agent conversation ----

var agentTools = func() []llm.Tool {
	var ts []llm.Tool
	for _, n := range []string{agents.ToolCreateOrderRequest, agents.ToolGetRequestStatus, agents.ToolListAddresses, agents.ToolConfirmOrder, agents.ToolCancelRequest} {
		ts = append(ts, llm.Tool{Name: n, Parameters: json.RawMessage(`{"type":"object"}`)})
	}
	return ts
}()

type convo struct {
	t    *testing.T
	f    *fakellm.Fake
	msgs []llm.Message
}

func (c *convo) user(s string) llm.ChatResponse {
	c.msgs = append(c.msgs, llm.Message{Role: llm.RoleUser, Content: s})
	return c.turn()
}

func (c *convo) turn() llm.ChatResponse {
	r, err := c.f.Chat(context.Background(), llm.ChatRequest{Messages: c.msgs, Tools: agentTools})
	if err != nil {
		c.t.Fatal(err)
	}
	c.msgs = append(c.msgs, r.Message)
	return r
}

func (c *convo) toolResult(r llm.ChatResponse, result string) llm.ChatResponse {
	c.t.Helper()
	if len(r.Message.ToolCalls) != 1 {
		c.t.Fatalf("expected one tool call, got %+v", r.Message)
	}
	tc := r.Message.ToolCalls[0]
	c.msgs = append(c.msgs, llm.Message{Role: llm.RoleTool, ToolCallID: tc.ID, Name: tc.Name, Content: result})
	return c.turn()
}

func wantCall(t *testing.T, r llm.ChatResponse, name string, args map[string]string) {
	t.Helper()
	if len(r.Message.ToolCalls) != 1 || r.Message.ToolCalls[0].Name != name {
		t.Fatalf("want call %s, got %+v", name, r.Message)
	}
	var got map[string]any
	if err := json.Unmarshal(r.Message.ToolCalls[0].Arguments, &got); err != nil {
		t.Fatalf("args not JSON: %s", r.Message.ToolCalls[0].Arguments)
	}
	for k, v := range args {
		if got[k] != v {
			t.Errorf("%s arg %s = %v want %q", name, k, got[k], v)
		}
	}
}

const reqID = "11111111-2222-3333-4444-555555555555"

const addrJSON = `{"addresses":[
 {"id":"a-hq","label":"HQ - Marina One","is_default":true,"postal_code":"018936"},
 {"id":"a-rd","label":"R&D Studio - one-north","postal_code":"138522"},
 {"id":"a-west","label":"West Hub - Jurong East","postal_code":"609434"}]}`

func TestAgent_FullConversation(t *testing.T) {
	c := &convo{t: t, f: fakellm.New(), msgs: []llm.Message{{Role: llm.RoleSystem, Content: "You are Jarvis."}}}

	r := c.user("We need 10 reams of A4 paper and coffee beans")
	wantCall(t, r, agents.ToolCreateOrderRequest, map[string]string{"utterance": "We need 10 reams of A4 paper and coffee beans"})
	r = c.toolResult(r, `{"id":"`+reqID+`","status":"parsing"}`)
	// IDs are never read to the user; the next step (address) is announced.
	if strings.Contains(r.Message.Content, reqID) || !strings.Contains(r.Message.Content, "delivery address") {
		t.Fatalf("reply after create = %q", r.Message.Content)
	}

	r = c.user("what's the status?")
	wantCall(t, r, agents.ToolGetRequestStatus, map[string]string{"request_id": reqID})
	r = c.toolResult(r, `{"request":{"id":"`+reqID+`","status":"quoted","total_cents":12345}}`)
	if !strings.Contains(r.Message.Content, "S$123.45") || !strings.Contains(r.Message.Content, "address") {
		t.Fatalf("status reply = %q", r.Message.Content)
	}

	// "yes" without an address must not confirm: it lists addresses first.
	r = c.user("Yes, go ahead")
	wantCall(t, r, agents.ToolListAddresses, nil)
	r = c.toolResult(r, addrJSON)
	if !strings.Contains(r.Message.Content, "HQ - Marina One (default)") {
		t.Fatalf("address question = %q", r.Message.Content)
	}

	r = c.user("Send it to the one-north studio")
	wantCall(t, r, agents.ToolConfirmOrder, map[string]string{"request_id": reqID, "address_id": "a-rd"})
	r = c.toolResult(r, `{"request_id":"`+reqID+`","status":"pending_approval"}`)
	if !strings.Contains(r.Message.Content, "waiting for an approver") {
		t.Fatalf("confirm reply = %q", r.Message.Content)
	}
}

func TestAgent_Decisions(t *testing.T) {
	listCall := llm.Message{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: agents.ToolListAddresses, Arguments: json.RawMessage(`{}`)}}}
	listResult := llm.Message{Role: llm.RoleTool, ToolCallID: "c1", Name: agents.ToolListAddresses, Content: addrJSON}
	withReq := llm.Message{Role: llm.RoleAssistant, Content: "Request " + reqID + " is quoted."}
	cases := []struct {
		name     string
		history  []llm.Message
		user     string
		wantTool string
		wantArgs map[string]string
		wantText string
	}{
		{"chit-chat", nil, "hello there", "", nil, "office supplies"},
		{"order", nil, "restock 5 hand wash", agents.ToolCreateOrderRequest, map[string]string{"utterance": "restock 5 hand wash"}, ""},
		{"status needs id", nil, "status please", "", nil, "Which request"},
		{"status by uuid in text", nil, "status of " + reqID, agents.ToolGetRequestStatus, map[string]string{"request_id": reqID}, ""},
		{"addresses", nil, "which addresses do we have?", agents.ToolListAddresses, nil, ""},
		{"cancel", []llm.Message{withReq}, "cancel it", agents.ToolCancelRequest, map[string]string{"request_id": reqID}, ""},
		{"default address", []llm.Message{withReq, listCall, listResult}, "yes, the default address", agents.ToolConfirmOrder, map[string]string{"address_id": "a-hq", "request_id": reqID}, ""},
		{"postal code", []llm.Message{withReq, listCall, listResult}, "deliver to 609434", agents.ToolConfirmOrder, map[string]string{"address_id": "a-west"}, ""},
		{"yes but unknown address", []llm.Message{withReq, listCall, listResult}, "yes", "", nil, "Which delivery address"},
		{"new order after addresses", []llm.Message{withReq, listCall, listResult}, "also order 4 staplers", agents.ToolCreateOrderRequest, nil, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			msgs := append([]llm.Message{{Role: llm.RoleSystem, Content: "sys"}}, tc.history...)
			msgs = append(msgs, llm.Message{Role: llm.RoleUser, Content: tc.user})
			r, err := fakellm.New().Chat(context.Background(), llm.ChatRequest{Messages: msgs, Tools: agentTools})
			if err != nil {
				t.Fatal(err)
			}
			if tc.wantTool != "" {
				wantCall(t, r, tc.wantTool, tc.wantArgs)
				return
			}
			if len(r.Message.ToolCalls) != 0 || !strings.Contains(r.Message.Content, tc.wantText) {
				t.Fatalf("reply = %+v, want text containing %q", r.Message, tc.wantText)
			}
		})
	}
}

func TestAgent_ToolErrorAndChoiceNone(t *testing.T) {
	f := fakellm.New()
	msgs := []llm.Message{
		{Role: llm.RoleUser, Content: "order 5 pens"},
		{Role: llm.RoleAssistant, ToolCalls: []llm.ToolCall{{ID: "c1", Name: agents.ToolCreateOrderRequest, Arguments: json.RawMessage(`{}`)}}},
		{Role: llm.RoleTool, ToolCallID: "c1", Content: `{"error":"no_active_enrollment"}`},
	}
	r, err := f.Chat(context.Background(), llm.ChatRequest{Messages: msgs, Tools: agentTools})
	if err != nil || !strings.Contains(r.Message.Content, "no_active_enrollment") {
		t.Fatalf("r = %+v %v", r, err)
	}
	r, err = f.Chat(context.Background(), llm.ChatRequest{Messages: msgs[:1], Tools: agentTools, ToolChoice: llm.ToolChoiceNone})
	if err != nil || len(r.Message.ToolCalls) != 0 {
		t.Fatalf("tool_choice none produced tool calls: %+v", r)
	}
}

func TestToolNamesMatchAgents(t *testing.T) {
	// The fake's heuristics key on these literal names; keep them in sync with agents.
	for _, n := range []string{agents.ToolCreateOrderRequest, agents.ToolGetRequestStatus, agents.ToolListAddresses, agents.ToolConfirmOrder, agents.ToolCancelRequest} {
		want := map[string]bool{"create_order_request": true, "get_request_status": true, "list_addresses": true, "confirm_order": true, "cancel_request": true}
		if !want[n] {
			t.Errorf("agents tool %q unknown to fakellm", n)
		}
	}
}

func js(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
