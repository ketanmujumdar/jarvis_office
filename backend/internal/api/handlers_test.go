package api

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/approvals"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
)

func TestApprovals(t *testing.T) {
	h := newHarness(t)
	var list struct{ Approvals []approvals.ApprovalView }
	r := h.do("GET", "/api/v1/approvals?status=pending", approverID, nil)
	r.json(t, &list)
	if r.code != 200 || len(list.Approvals) != 1 {
		t.Fatalf("list = %d %s", r.code, r.body)
	}
	if n := len(list.Approvals[0].Lines[0].TopOffers); n != 3 {
		t.Fatalf("top offers capped at 3, got %d", n)
	}
	if r := h.do("GET", "/api/v1/approvals?status=maybe", approverID, nil); r.code != 400 {
		t.Fatalf("bad status = %d", r.code)
	}
	if r := h.do("GET", "/api/v1/approvals/"+approvalID, managerID, nil); r.code != 200 {
		t.Fatalf("get = %d", r.code)
	}
	if r := h.do("GET", "/api/v1/approvals/"+missingID, approverID, nil); r.code != 404 {
		t.Fatalf("get missing = %d", r.code)
	}

	var a domain.Approval
	r = h.do("POST", "/api/v1/approvals/"+approvalID+"/approve", approverID, `{"comment":" ok for Q4 "}`)
	r.json(t, &a)
	if r.code != 200 || a.Status != domain.ApprovalApproved || a.Comment != "ok for Q4" {
		t.Fatalf("approve = %d %s", r.code, r.body)
	}
	if h.appr.decided[0] != "approved:"+approvalID+":"+approverID+":ok for Q4" {
		t.Fatalf("decided = %v", h.appr.decided)
	}
	// Second decision conflicts.
	if r := h.do("POST", "/api/v1/approvals/"+approvalID+"/reject", adminID, nil); r.code != 409 || r.errCode(t) != "conflict" {
		t.Fatalf("second decision = %d %s", r.code, r.body)
	}
	long := strings.Repeat("x", 2001)
	if r := h.do("POST", "/api/v1/approvals/"+approvalID+"/reject", adminID, map[string]string{"comment": long}); r.code != 400 {
		t.Fatalf("long comment = %d", r.code)
	}
}

func TestOrders(t *testing.T) {
	h := newHarness(t)
	apprID := approverID
	offerID := "o1"
	h.addRequest(domain.RequestDetail{
		Request:   domain.PurchaseRequest{ID: requestID, RequesterID: managerID, Status: domain.StatusOrdered, TotalCents: 4200},
		LineItems: []domain.LineItem{{ID: "l1"}, {ID: "l2"}},
		Approvals: []domain.Approval{{ID: approvalID, Status: domain.ApprovalApproved, ApproverID: &apprID}},
		Payments:  []domain.Payment{{ID: "p1", MerchantName: "Popular"}, {ID: "p2", MerchantName: "Popular"}, {ID: "p3", MerchantName: "Common Man"}},
	})
	h.addRequest(domain.RequestDetail{
		Request:   domain.PurchaseRequest{ID: "aaaaaaaa-0000-4000-8000-000000000001", RequesterID: managerID, Status: domain.StatusCheckingOut},
		LineItems: []domain.LineItem{{ID: "l3", SelectedOfferID: &offerID}},
		Offers:    []domain.Offer{{ID: "o1", MerchantName: "Bettr"}, {ID: "o2", MerchantName: "Other"}},
	})
	// Not an order: still quoted; failed without payments.
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: "aaaaaaaa-0000-4000-8000-000000000002", Status: domain.StatusQuoted}})
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: "aaaaaaaa-0000-4000-8000-000000000003", Status: domain.StatusFailed}})

	var out struct{ Orders []OrderSummary }
	r := h.do("GET", "/api/v1/orders", managerID, nil)
	r.json(t, &out)
	if r.code != 200 || len(out.Orders) != 2 {
		t.Fatalf("orders = %d %s", r.code, r.body)
	}
	byID := map[string]OrderSummary{}
	for _, o := range out.Orders {
		byID[o.Request.ID] = o
	}
	o := byID[requestID]
	if o.ItemCount != 2 || strings.Join(o.Vendors, ",") != "Popular,Common Man" || o.Requester == nil || o.Requester.ID != managerID ||
		o.Approver == nil || o.Approver.ID != approverID || len(o.Payments) != 3 {
		t.Fatalf("ordered summary = %+v", o)
	}
	if v := byID["aaaaaaaa-0000-4000-8000-000000000001"].Vendors; strings.Join(v, ",") != "Bettr" {
		t.Fatalf("vendors from selected offers = %v", v)
	}
}

func TestMonthlySpend(t *testing.T) {
	h := newHarness(t)
	h.st.mtd = 12345
	h.st.spend = []domain.MonthSpend{{Month: "2026-08", SpendCents: 1}, {Month: "2026-09", SpendCents: 2}, {Month: "2026-10", SpendCents: 12345, Orders: 3}}
	tests := []struct {
		q     string
		code  int
		count int
	}{{"", 200, 3}, {"?months=2", 200, 2}, {"?months=0", 400, 0}, {"?months=25", 400, 0}, {"?months=x", 400, 0}}
	for _, tt := range tests {
		r := h.do("GET", "/api/v1/spend/monthly"+tt.q, managerID, nil)
		if r.code != tt.code {
			t.Errorf("%s = %d", tt.q, r.code)
			continue
		}
		if tt.code != 200 {
			continue
		}
		var s SpendSummary
		r.json(t, &s)
		if s.Currency != "SGD" || s.MonthlyBudgetCents != 300000 || s.MonthToDateCents != 12345 || len(s.Months) != tt.count {
			t.Errorf("%s: %+v", tt.q, s)
		}
	}
	// No policy row yet: budget 0, still 200.
	h.st.policy = nil
	if r := h.do("GET", "/api/v1/spend/monthly", managerID, nil); r.code != 200 {
		t.Fatalf("no policy = %d", r.code)
	}
}

func TestGlobalAudit(t *testing.T) {
	h := newHarness(t)
	h.st.audit = []domain.AuditEvent{{ID: 1, Type: "reap.quote_created"}, {ID: 2, Type: "request.created"}, {ID: 3, Type: "reap.checkout_created"}}
	tests := []struct {
		q    string
		want []int64
	}{{"", []int64{3, 2, 1}}, {"?type=reap.", []int64{3, 1}}, {"?type=request.created", []int64{2}}}
	for _, tt := range tests {
		var out struct{ Events []domain.AuditEvent }
		h.do("GET", "/api/v1/audit"+tt.q, approverID, nil).json(t, &out)
		var got []int64
		for _, e := range out.Events {
			got = append(got, e.ID)
		}
		if fmt.Sprint(got) != fmt.Sprint(tt.want) {
			t.Errorf("%s = %v, want %v", tt.q, got, tt.want)
		}
	}
}

func TestReadOnlyCatalogAndAddresses(t *testing.T) {
	h := newHarness(t)
	h.st.catalog["c1"] = domain.CatalogItem{ID: "c1", SKU: "a4", Name: "A4 Paper", Category: "Paper", Active: true}
	h.st.catalog["c2"] = domain.CatalogItem{ID: "c2", SKU: "old", Name: "Old Pens", Category: "Paper", Active: false}
	h.st.catalog["c3"] = domain.CatalogItem{ID: "c3", SKU: "pods", Name: "Coffee Pods", Category: "Coffee", Active: true}
	for q, want := range map[string]int{"": 2, "?category=Paper": 1, "?q=coffee": 1, "?q=pens": 0} {
		var out struct{ Items []domain.CatalogItem }
		r := h.do("GET", "/api/v1/catalog"+q, managerID, nil)
		r.json(t, &out)
		if len(out.Items) != want {
			t.Errorf("catalog%s = %d items", q, len(out.Items))
		}
		if want > 0 && !strings.Contains(string(r.body), `"aliases":[]`) {
			t.Errorf("aliases should be [] not null: %s", r.body)
		}
	}
	var addr struct{ Addresses []domain.Address }
	h.do("GET", "/api/v1/addresses", managerID, nil).json(t, &addr)
	if len(addr.Addresses) != 1 || !addr.Addresses[0].IsDefault {
		t.Fatalf("addresses = %+v", addr)
	}
}

func TestAgentChat(t *testing.T) {
	h := newHarness(t)
	var out struct {
		SessionID  string   `json:"session_id"`
		Reply      string   `json:"reply"`
		RequestIDs []string `json:"request_ids"`
		ToolCalls  []struct {
			Name      string          `json:"name"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"tool_calls"`
	}
	r := h.do("POST", "/api/v1/agent/chat", managerID, `{"message":"order paper"}`)
	r.json(t, &out)
	if r.code != 200 || out.SessionID != "sess-1" || out.RequestIDs == nil || string(out.ToolCalls[0].Arguments) != "{}" {
		t.Fatalf("chat = %d %s", r.code, r.body)
	}
	if h.agent.inputs[0].UserID != managerID {
		t.Fatalf("input = %+v", h.agent.inputs[0])
	}
	if r := h.do("POST", "/api/v1/agent/chat", managerID, `{"message":"  "}`); r.code != 400 {
		t.Fatalf("empty message = %d", r.code)
	}
	h.agent.err = fmt.Errorf("openai: %w", domain.ErrUpstream)
	if r := h.do("POST", "/api/v1/agent/chat", managerID, `{"message":"hi"}`); r.code != 502 {
		t.Fatalf("upstream = %d", r.code)
	}
	var msgs struct{ Messages []domain.ChatMessage }
	h.do("GET", "/api/v1/agent/sessions/sess-1/messages", managerID, nil).json(t, &msgs)
	if len(msgs.Messages) != 1 {
		t.Fatalf("messages = %+v", msgs)
	}
	r = h.do("GET", "/api/v1/agent/sessions/unknown/messages", managerID, nil)
	if r.code != 200 || !strings.Contains(string(r.body), `"messages":[]`) {
		t.Fatalf("empty session = %d %s", r.code, r.body)
	}
}

func TestAgentToolRelay(t *testing.T) {
	tests := []struct {
		name     string
		tool     string
		body     string
		result   json.RawMessage
		want     int
		wantArgs string
		wantOut  string
	}{
		{"object args", "list_addresses", `{"call_id":"c1","session_id":"rt1","arguments":{}}`, nil, 200, `{}`, `{"ok":true}`},
		{"string args", "confirm_order", `{"call_id":"c2","arguments":"{\"request_id\":\"r\",\"address_id\":\"a\"}"}`, nil, 200, `{"request_id":"r","address_id":"a"}`, `{"ok":true}`},
		{"empty string args", "list_addresses", `{"arguments":""}`, nil, 200, `{}`, `{"ok":true}`},
		{"array result wrapped", "list_addresses", `{"arguments":{}}`, json.RawMessage(`[1,2]`), 200, `{}`, `{"result":[1,2]}`},
		{"unknown tool", "delete_everything", `{"arguments":{}}`, nil, 404, "", ""},
		{"missing args", "list_addresses", `{"call_id":"c"}`, nil, 400, "", ""},
		{"array args", "list_addresses", `{"arguments":[1]}`, nil, 400, "", ""},
		{"bad string args", "list_addresses", `{"arguments":"{oops"}`, nil, 400, "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			h.tools.result = tt.result
			r := h.do("POST", "/api/v1/agent/tools/"+tt.tool, managerID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
			if tt.want != 200 {
				if len(h.tools.calls) != 0 {
					t.Fatal("executor called")
				}
				return
			}
			c := h.tools.calls[0]
			if c.name != tt.tool || c.args != tt.wantArgs || c.tc.UserID != managerID || c.tc.Channel != "voice" {
				t.Fatalf("call = %+v", c)
			}
			var out struct {
				CallID string          `json:"call_id"`
				Output json.RawMessage `json:"output"`
			}
			r.json(t, &out)
			if string(out.Output) != tt.wantOut {
				t.Fatalf("output = %s", out.Output)
			}
		})
	}
}

func TestAgentToolsListAndRealtime(t *testing.T) {
	h := newHarness(t)
	r := h.do("GET", "/api/v1/agent/tools", managerID, nil)
	if r.code != 200 || !strings.Contains(string(r.body), `"name":"list_addresses"`) || !strings.Contains(string(r.body), `"parameters":{"type":"object"`) {
		t.Fatalf("tools = %d %s", r.code, r.body)
	}

	var sess map[string]any
	r = h.do("POST", "/api/v1/realtime/session", managerID, nil)
	r.json(t, &sess)
	if r.code != 200 || sess["client_secret"] != "ek_test" || sess["calls_url"] == "" || sess["voice"] != "marin" {
		t.Fatalf("session = %d %s", r.code, r.body)
	}
	if tools, _ := sess["tools"].([]any); len(tools) != 1 {
		t.Fatalf("session tools = %v", sess["tools"])
	}
	if !strings.HasPrefix(h.minter.got.Instructions, "DB PROMPT") || len(h.minter.got.Tools) != 1 || h.minter.got.UserID != managerID {
		t.Fatalf("mint params = %+v", h.minter.got)
	}
	// The prompt is read on every new session: an admin edit applies immediately.
	h.do("PUT", "/api/v1/admin/system-prompt", adminID, `{"content":"EDITED"}`)
	h.do("POST", "/api/v1/realtime/session", managerID, nil)
	if !strings.HasPrefix(h.minter.got.Instructions, "EDITED") {
		t.Fatalf("instructions after edit = %q", h.minter.got.Instructions)
	}
	// No prompt in DB: fallback, still works.
	delete(h.st.prompts, domain.SystemPromptKeyAgent)
	h.do("POST", "/api/v1/realtime/session", managerID, nil)
	if !strings.HasPrefix(h.minter.got.Instructions, realtime.FallbackInstructions) {
		t.Fatalf("fallback = %q", h.minter.got.Instructions)
	}
	h.minter.err = fmt.Errorf("openai: %w", domain.ErrUpstream)
	if r := h.do("POST", "/api/v1/realtime/session", managerID, nil); r.code != 502 {
		t.Fatalf("mint error = %d", r.code)
	}
}

func TestEnrollments(t *testing.T) {
	h := newHarness(t)
	if r := h.do("GET", "/api/v1/enrollments/current", managerID, nil); r.code != 404 {
		t.Fatalf("none yet = %d", r.code)
	}
	var e domain.Enrollment
	r := h.do("POST", "/api/v1/enrollments", managerID, nil)
	r.json(t, &e)
	if r.code != 201 || e.NextActionURL == "" || e.Status != domain.EnrollmentRequiresAction || e.OwnerRef != managerID {
		t.Fatalf("start = %d %s", r.code, r.body)
	}
	for _, banned := range []string{"last4", "expiry", "pan", "card_number"} {
		if strings.Contains(strings.ToLower(string(r.body)), banned) {
			t.Fatalf("enrollment response leaks %q: %s", banned, r.body)
		}
	}
	if r := h.do("GET", "/api/v1/enrollments/current", managerID, nil); r.code != 200 {
		t.Fatalf("current = %d", r.code)
	}
	h.orch.err = fmt.Errorf("reap: %w", domain.ErrUpstream)
	if r := h.do("POST", "/api/v1/enrollments", managerID, nil); r.code != 502 {
		t.Fatalf("upstream = %d", r.code)
	}
}
