package api

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/events"
)

const (
	managerID  = "11111111-1111-4111-8111-111111111111"
	approverID = "22222222-2222-4222-8222-222222222222"
	adminID    = "33333333-3333-4333-8333-333333333333"
	vendorID   = "44444444-4444-4444-8444-444444444444"
	blockedID  = "55555555-5555-4555-8555-555555555555"
	addressID  = "66666666-6666-4666-8666-666666666666"
	requestID  = "77777777-7777-4777-8777-777777777777"
	approvalID = "88888888-8888-4888-8888-888888888888"
	missingID  = "99999999-9999-4999-8999-999999999999"
)

type harness struct {
	t      *testing.T
	st     *fakeStore
	orch   *fakeOrch
	appr   *fakeApprovals
	agent  *fakeAgent
	tools  *fakeTools
	minter *fakeMinter
	bus    *events.Memory
	audit  *audit.Memory
	srv    *Server
	h      http.Handler
}

func newHarness(t *testing.T, mutate ...func(*Deps)) *harness {
	t.Helper()
	st := newFakeStore()
	st.users[managerID] = domain.User{ID: managerID, Name: "Maya", Email: "maya@jarvis.example", Role: domain.RoleManager}
	st.users[approverID] = domain.User{ID: approverID, Name: "Daniel", Email: "daniel@jarvis.example", Role: domain.RoleApprover}
	st.users[adminID] = domain.User{ID: adminID, Name: "Priya", Email: "priya@jarvis.example", Role: domain.RoleAdmin}
	st.vendors[vendorID] = domain.Vendor{ID: vendorID, Domain: "popular.com.sg", Name: "Popular", Allowed: true, Country: "SG"}
	st.vendors[blockedID] = domain.Vendor{ID: blockedID, Domain: "blocked.example.com", Name: "Blocked", Allowed: false}
	st.addresses[addressID] = domain.Address{ID: addressID, Label: "HQ - Marina One", IsDefault: true, Country: "SG"}
	st.policy = &domain.PolicyConfig{Currency: "SGD", PerOrderLimitCents: 50000, MonthlyBudgetCents: 300000, PriceDriftPct: 5}
	st.prompts[domain.SystemPromptKeyAgent] = domain.SystemPrompt{Key: "agent", Content: "DB PROMPT", Version: 1}
	h := &harness{
		t: t, st: st, orch: &fakeOrch{st: st},
		appr:  &fakeApprovals{items: map[string]domain.Approval{approvalID: {ID: approvalID, RequestID: requestID, Kind: domain.ApprovalKindPolicy, Status: domain.ApprovalPending}}},
		agent: &fakeAgent{}, tools: &fakeTools{}, minter: &fakeMinter{}, bus: events.NewMemory(16), audit: audit.NewMemory(),
	}
	d := Deps{
		Config: config.Config{CORSOrigins: []string{"http://localhost:5173"}, OpenAIRealtimeVoice: "marin", OpenAIRealtimeModel: "gpt-realtime"},
		Store:  st, Orchestrator: h.orch, Approvals: h.appr, Agent: h.agent, Tools: h.tools, Realtime: h.minter,
		Bus: h.bus, Audit: h.audit, Clock: func() time.Time { return time.Date(2026, 10, 9, 8, 0, 0, 0, time.UTC) },
		HeartbeatInterval: 50 * time.Millisecond,
	}
	for _, m := range mutate {
		m(&d)
	}
	h.srv = NewServer(d)
	h.h = h.srv.Handler()
	return h
}

func (h *harness) addRequest(d domain.RequestDetail) {
	h.st.mu.Lock()
	defer h.st.mu.Unlock()
	h.st.requests[d.Request.ID] = d
}

type resp struct {
	code int
	hdr  http.Header
	body []byte
}

func (r resp) json(t *testing.T, v any) {
	t.Helper()
	if err := json.Unmarshal(r.body, v); err != nil {
		t.Fatalf("decode %s: %v", r.body, err)
	}
}

func (r resp) errCode(t *testing.T) string {
	t.Helper()
	var e ErrorBody
	r.json(t, &e)
	return e.Error.Code
}

func newReq(method, path, body string) *http.Request {
	var rd io.Reader
	if body != "" {
		rd = bytes.NewBufferString(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func (h *harness) serve(req *http.Request) resp {
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return resp{code: rec.Code, hdr: rec.Header(), body: rec.Body.Bytes()}
}

// do sends a request as the user with the given token ("" = anonymous). body may be a string or a value.
func (h *harness) do(method, path, token string, body any) resp {
	h.t.Helper()
	var rd io.Reader
	switch b := body.(type) {
	case nil:
	case string:
		rd = bytes.NewBufferString(b)
	default:
		raw, err := json.Marshal(b)
		if err != nil {
			h.t.Fatal(err)
		}
		rd = bytes.NewReader(raw)
	}
	req := httptest.NewRequest(method, path, rd)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if rd != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	h.h.ServeHTTP(rec, req)
	return resp{code: rec.Code, hdr: rec.Header(), body: rec.Body.Bytes()}
}
