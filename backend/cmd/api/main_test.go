package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/config"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/memstore"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func fakeConfig() config.Config {
	return config.Config{
		UseFakes: true, CORSOrigins: []string{"*"}, SeedDir: "../../seed",
		OpenAIModel: "gpt-5.1", OpenAIRealtimeModel: "gpt-realtime", OpenAIRealtimeVoice: "marin",
		ReapReturnURL: "https://example.com/jarvis/reap-return", ReapVersion: "2025-02-14",
		SearchDeadline: 10 * time.Second, SearchResultsPerVendor: 5,
		CheckoutPollInterval: 50 * time.Millisecond, CheckoutPollTimeout: time.Minute, QueueWorkers: 4,
	}
}

type client struct {
	t     *testing.T
	base  string
	token string
}

func (c *client) call(method, path string, body any, out any) int {
	c.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, c.base+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if out != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			c.t.Fatalf("%s %s: decode %s: %v", method, path, raw, err)
		}
	}
	if res.StatusCode >= 400 {
		c.t.Logf("%s %s -> %d %s", method, path, res.StatusCode, raw)
	}
	return res.StatusCode
}

func waitStatus(t *testing.T, c *client, id string, want ...domain.RequestStatus) domain.RequestDetail {
	t.Helper()
	deadline := time.Now().Add(20 * time.Second)
	var d domain.RequestDetail
	for time.Now().Before(deadline) {
		c.call("GET", "/api/v1/requests/"+id, nil, &d)
		for _, w := range want {
			if d.Request.Status == w {
				return d
			}
		}
		if d.Request.Status.Terminal() {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("request %s stuck in %s (failure_reason=%q), want %v", id, d.Request.Status, d.Request.FailureReason, want)
	return d
}

// openHosted simulates the user opening a Reap-hosted page (card entry or payment approval).
func openHosted(t *testing.T, url string) {
	t.Helper()
	if url == "" {
		t.Fatal("no hosted url")
	}
	res, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	res.Body.Close()
}

// TestBuildWithFakesEndToEnd wires the real packages (orchestrator, approvals, agents, api) over
// memstore + the fake Reap server + the fake LLM and drives the demo flow over HTTP.
func TestBuildWithFakesEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	st := memstore.New()
	cfg := fakeConfig()
	if err := seed.Load(ctx, st, cfg.SeedDir, seed.Options{}); err != nil {
		t.Fatalf("seed: %v", err)
	}
	a, err := build(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err != nil {
		t.Fatal(err)
	}
	defer a.Close()
	if a.FakeReap == nil {
		t.Fatal("FAKES=true must use the fake Reap server")
	}
	if err := a.Start(ctx); err != nil {
		t.Fatal(err)
	}
	defer a.Stop(context.Background())
	ts := httptest.NewServer(a.Handler)
	defer ts.Close()

	c := &client{t: t, base: ts.URL}
	if code := c.call("GET", "/healthz", nil, nil); code != 200 {
		t.Fatalf("healthz = %d", code)
	}
	var users struct{ Users []domain.User }
	c.call("GET", "/api/v1/auth/users", nil, &users)
	var manager, approver domain.User
	for _, u := range users.Users {
		switch u.Role {
		case domain.RoleManager:
			manager = u
		case domain.RoleApprover:
			approver = u
		}
	}
	if manager.ID == "" || approver.ID == "" {
		t.Fatalf("seed users = %+v", users.Users)
	}
	var login struct {
		Token string `json:"token"`
	}
	if code := c.call("POST", "/api/v1/auth/login", map[string]string{"email": manager.Email}, &login); code != 200 {
		t.Fatalf("login = %d", code)
	}
	c.token = login.Token

	// Confirm without a card -> 409 no_active_enrollment comes later; enroll now via the hosted page.
	var enr domain.Enrollment
	if code := c.call("POST", "/api/v1/enrollments", nil, &enr); code != 201 {
		t.Fatalf("start enrollment = %d", code)
	}
	openHosted(t, enr.NextActionURL)
	deadline := time.Now().Add(10 * time.Second)
	for enr.Status != domain.EnrollmentActive && time.Now().Before(deadline) {
		c.call("GET", "/api/v1/enrollments/current", nil, &enr)
		time.Sleep(50 * time.Millisecond)
	}
	if enr.Status != domain.EnrollmentActive {
		t.Fatalf("enrollment = %s", enr.Status)
	}

	var addrs struct{ Addresses []domain.Address }
	c.call("GET", "/api/v1/addresses", nil, &addrs)
	if len(addrs.Addresses) < 3 {
		t.Fatalf("addresses = %d", len(addrs.Addresses))
	}

	var pr domain.PurchaseRequest
	if code := c.call("POST", "/api/v1/requests", map[string]any{
		"utterance": "We're out of A4 printer paper, reorder the usual.",
	}, &pr); code != 201 {
		t.Fatalf("create = %d", code)
	}
	d := waitStatus(t, c, pr.ID, domain.StatusQuoted)
	if len(d.LineItems) == 0 || len(d.Offers) == 0 {
		t.Fatalf("quoted detail = %+v", d)
	}

	if code := c.call("POST", "/api/v1/requests/"+pr.ID+"/confirm", map[string]string{"address_id": addrs.Addresses[0].ID}, &d); code != 200 {
		t.Fatalf("confirm = %d", code)
	}
	if d.Request.Status == domain.StatusPendingApproval {
		ac := &client{t: t, base: ts.URL, token: approver.ID}
		var list struct {
			Approvals []struct {
				Approval domain.Approval `json:"approval"`
			} `json:"approvals"`
		}
		ac.call("GET", "/api/v1/approvals?status=pending", nil, &list)
		if len(list.Approvals) == 0 {
			t.Fatal("no pending approval")
		}
		if code := ac.call("POST", "/api/v1/approvals/"+list.Approvals[0].Approval.ID+"/approve", map[string]string{"comment": "ok"}, nil); code != 200 {
			t.Fatalf("approve = %d", code)
		}
	}
	d = waitStatus(t, c, pr.ID, domain.StatusAwaitingPayment, domain.StatusOrdered)
	for _, p := range d.Payments {
		if p.Status == domain.PaymentRequiresAction {
			openHosted(t, p.ApprovalURL)
		}
	}
	c.call("POST", "/api/v1/requests/"+pr.ID+"/checkout/refresh", nil, nil)
	d = waitStatus(t, c, pr.ID, domain.StatusOrdered)
	for _, p := range d.Payments {
		if p.Status != domain.PaymentCompleted || p.ReapOrderID == "" {
			t.Fatalf("payment = %+v", p)
		}
	}

	var orders struct{ Orders []json.RawMessage }
	c.call("GET", "/api/v1/orders", nil, &orders)
	if len(orders.Orders) != 1 {
		t.Fatalf("orders = %d", len(orders.Orders))
	}
	var audit struct{ Events []domain.AuditEvent }
	c.call("GET", "/api/v1/requests/"+pr.ID+"/audit", nil, &audit)
	if len(audit.Events) == 0 {
		t.Fatal("no audit trail")
	}

	// Text agent and realtime session are wired with the same tools.
	var chat struct {
		SessionID string `json:"session_id"`
		Reply     string `json:"reply"`
	}
	if code := c.call("POST", "/api/v1/agent/chat", map[string]string{"message": "what addresses can you deliver to?"}, &chat); code != 200 || chat.Reply == "" {
		t.Fatalf("chat = %d %+v", code, chat)
	}
	var sess map[string]any
	if code := c.call("POST", "/api/v1/realtime/session", nil, &sess); code != 200 || sess["client_secret"] == "" {
		t.Fatalf("realtime = %d %v", code, sess)
	}
	if tools, _ := sess["tools"].([]any); len(tools) != 5 {
		t.Fatalf("realtime tools = %d", len(tools))
	}
}
