package harness

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

func TestParseSSE(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []SSEEvent
	}{
		{"single", "id: 1\nevent: request.created\ndata: {\"a\":1}\n\n",
			[]SSEEvent{{ID: "1", Event: "request.created", Data: json.RawMessage(`{"a":1}`)}}},
		{"multi data lines + comment + crlf", ": hi\r\nevent: x\r\ndata: a\r\ndata: b\r\n\r\n",
			[]SSEEvent{{Event: "x", Data: json.RawMessage("a\nb")}}},
		{"default event name", "data: {}\n\n", []SSEEvent{{Event: "message", Data: json.RawMessage(`{}`)}}},
		{"incomplete trailing event is dropped", "event: a\ndata: 1\n\nevent: b\ndata: 2\n",
			[]SSEEvent{{Event: "a", Data: json.RawMessage("1")}}},
		{"two events", "id: 1\nevent: a\ndata: 1\n\nid: 2\nevent: b\ndata: 2\n\n",
			[]SSEEvent{{ID: "1", Event: "a", Data: json.RawMessage("1")}, {ID: "2", Event: "b", Data: json.RawMessage("2")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var got []SSEEvent
			if err := ParseSSE(strings.NewReader(tc.in), func(e SSEEvent) bool { got = append(got, e); return true }); err != nil {
				t.Fatal(err)
			}
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("got %+v, want %+v", got, tc.want)
			}
		})
	}
}

func TestIsSubsequence(t *testing.T) {
	tests := []struct {
		got, want []string
		ok        bool
		at        int
	}{
		{[]string{"a", "x", "b", "c"}, []string{"a", "b", "c"}, true, 3},
		{[]string{"b", "a"}, []string{"a", "b"}, false, 1},
		{nil, nil, true, 0},
		{[]string{"a"}, []string{"a", "a"}, false, 1},
	}
	for _, tc := range tests {
		ok, at := IsSubsequence(tc.got, tc.want)
		if ok != tc.ok || at != tc.at {
			t.Errorf("IsSubsequence(%v,%v) = %v,%d; want %v,%d", tc.got, tc.want, ok, at, tc.ok, tc.at)
		}
	}
}

func TestSeq(t *testing.T) {
	envs := []Envelope{
		{Type: "request.created"},
		{Type: "request.status_changed", Data: json.RawMessage(`{"from":"parsing","to":"searching"}`)},
	}
	if got := Seq(envs); !reflect.DeepEqual(got, []string{"request.created", "status:searching"}) {
		t.Fatalf("Seq = %v", got)
	}
}

func TestArrayProperty(t *testing.T) {
	tests := []struct {
		schema string
		want   string
	}{
		{`{"type":"object","properties":{"items":{"type":"array"}}}`, "items"},
		{`{"type":"object","properties":{"note":{"type":"string"},"line_items":{"type":"array"}}}`, "line_items"},
		{`{"type":"array","items":{}}`, ""},
		{`{"type":"object"}`, "items"},
	}
	for _, tc := range tests {
		var m map[string]any
		_ = json.Unmarshal([]byte(tc.schema), &m)
		if got := arrayProperty(m); got != tc.want {
			t.Errorf("arrayProperty(%s) = %q, want %q", tc.schema, got, tc.want)
		}
	}
}

func TestValidIdent(t *testing.T) {
	for s, want := range map[string]bool{"jarvis_e2e": true, "a1": true, "1a": false, "": false, "x;drop": false, "Upper": false} {
		if validIdent(s) != want {
			t.Errorf("validIdent(%q) != %v", s, want)
		}
	}
}

func seedData(t *testing.T) seed.Data {
	t.Helper()
	d, err := seed.Parse(filepath.Join("..", "..", "seed"))
	if err != nil {
		t.Fatalf("seed.Parse: %v", err)
	}
	return d
}

func TestProductsFromSeed(t *testing.T) {
	d := seedData(t)
	ps := ProductsFromSeed(d)
	merchant := map[string]string{}
	for _, v := range d.Vendors {
		merchant[v.Domain] = v.ReapMerchantName
	}
	for _, it := range d.Items {
		for i, dom := range it.PreferredVendors {
			found := false
			for _, p := range ps {
				if p.Domain == dom && p.MerchantName == merchant[dom] && strings.HasPrefix(p.Name, it.Name) {
					found = true
					if p.PriceSGD != SeedPrice(it.MaxUnitPriceSGD, i) || p.PriceSGD > it.MaxUnitPriceSGD {
						t.Errorf("%s @ %s price %.2f", it.SKU, dom, p.PriceSGD)
					}
				}
			}
			if !found && merchant[dom] != "" {
				t.Errorf("no fake product for %s at %s", it.SKU, dom)
			}
		}
	}
	desks := 0
	for _, p := range ps {
		if p.Domain == "ergotune.com" && strings.Contains(p.Name, "Standing Desk") {
			desks++
		}
	}
	if desks != 3 {
		t.Errorf("desks = %d, want 3", desks)
	}
}

// ---------------- fake Reap ----------------

type reapT struct {
	t   *testing.T
	f   *FakeReap
	key string
}

func (r reapT) do(method, path string, body any, hdr map[string]string) (int, map[string]any) {
	r.t.Helper()
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, r.f.URL()+path, rd)
	req.Header.Set("Authorization", "Bearer "+r.key)
	req.Header.Set("Reap-Version", ReapVersion)
	for k, v := range hdr {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		r.t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	return resp.StatusCode, m
}

func errCode(m map[string]any) string {
	e, _ := m["error"].(map[string]any)
	s, _ := e["code"].(string)
	return s
}

func TestFakeReapAuthAndHeaders(t *testing.T) {
	f := NewFakeReap("k1", nil)
	defer f.Close()
	tests := []struct {
		name   string
		key    string
		hdr    map[string]string
		path   string
		status int
		code   string
	}{
		{"bad key", "nope", nil, "/agentic/products/search", 401, "UNAUTHORIZED"},
		{"bad version", "k1", map[string]string{"Reap-Version": "2020-01-01"}, "/agentic/products/search", 400, "AGENTIC_REQUEST_REJECTED"},
		{"missing idempotency", "k1", nil, "/agentic/quotes", 400, "AGENTIC_REQUEST_REJECTED"},
		{"mandates not live", "k1", nil, "/agentic/mandates", 400, "AGENTIC_PAYMENTS_NOT_ENABLED"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			st, m := reapT{t, f, tc.key}.do("POST", tc.path, map[string]any{"query": "x"}, tc.hdr)
			if st != tc.status || errCode(m) != tc.code {
				t.Fatalf("got %d %s, want %d %s", st, errCode(m), tc.status, tc.code)
			}
		})
	}
}

func TestFakeReapPurchaseFlow(t *testing.T) {
	d := seedData(t)
	f := NewFakeReap("k1", ProductsFromSeed(d))
	defer f.Close()
	r := reapT{t, f, "k1"}

	// Search restricted to Popular returns Popular + the leaky decoy.
	st, res := r.do("POST", "/agentic/products/search", map[string]any{
		"query":              "A4 copier paper 80g 500 sheets",
		"merchantPreference": map[string]any{"mode": "ONLY", "merchantName": "popular.com.sg"},
	}, nil)
	if st != 200 {
		t.Fatalf("search %d %v", st, res)
	}
	prods, _ := res["products"].([]any)
	names := map[string]bool{}
	var popularID string
	for _, p := range prods {
		pm := p.(map[string]any)
		n := pm["merchant"].(map[string]any)["name"].(string)
		names[n] = true
		if n == "Popular Bookstore" && popularID == "" {
			popularID = pm["id"].(string)
		}
	}
	if !names["Popular Bookstore"] || !names[LeakyMerchantName] || names["Kinokuniya Book Stores of Singapore"] {
		t.Fatalf("unexpected merchants %v", names)
	}
	// No real hit -> no leaky noise either.
	_, res = r.do("POST", "/agentic/products/search", map[string]any{
		"query": "A4 copier paper", "merchantPreference": map[string]any{"mode": "ONLY", "merchantName": "ergotune.com"},
	}, nil)
	if n := len(res["products"].([]any)); n != 0 {
		t.Fatalf("ergotune paper search returned %d", n)
	}
	// Display name instead of domain fails like the sandbox.
	st, res = r.do("POST", "/agentic/products/search", map[string]any{
		"query": "paper", "merchantPreference": map[string]any{"mode": "ONLY", "merchantName": "Popular Bookstore"},
	}, nil)
	if st != 400 || errCode(res) != "MERCHANT_NOT_RESOLVED" {
		t.Fatalf("display-name search: %d %v", st, res)
	}

	_, det := r.do("POST", "/agentic/products/details", map[string]any{"productIds": []string{popularID}}, nil)
	dv := det["products"].([]any)[0].(map[string]any)["defaultVariant"].(map[string]any)
	variantID := dv["id"].(string)
	price := dv["price"].(map[string]any)["amount"].(float64)

	addr := map[string]any{"firstName": "Maya", "lastName": "Tan", "phone": "+6562001001", "addressLine1": "7 Straits View",
		"city": "Singapore", "postalCode": "018936", "country": "SG"}
	f.SetQuoteMultiplier("popular.com.sg", 1.1)
	st, q := r.do("POST", "/agentic/quotes", map[string]any{
		"email": "maya@example.com", "items": []any{map[string]any{"variantId": variantID, "quantity": 10}}, "shippingAddress": addr,
	}, map[string]string{"Idempotency-Key": "q1"})
	if st != 200 {
		t.Fatalf("quote %d %v", st, q)
	}
	final := q["amountBreakdown"].(map[string]any)["finalAmount"].(map[string]any)["amount"].(float64)
	if want := round2(round2(price*1.1) * 10); final != want {
		t.Fatalf("final %.2f want %.2f", final, want)
	}
	// Idempotent replay returns the same quote.
	_, q2 := r.do("POST", "/agentic/quotes", map[string]any{"email": "x", "items": []any{}}, map[string]string{"Idempotency-Key": "q1"})
	if q2["id"] != q["id"] {
		t.Fatalf("idempotency replay gave a different quote")
	}
	if dom, email, a, ok := f.Quote(q["id"].(string)); !ok || dom != "popular.com.sg" || email != "maya@example.com" || a["postalCode"] != "018936" {
		t.Fatalf("Quote snapshot: %v %v %v %v", dom, email, a, ok)
	}
	// Bad phone is rejected.
	bad := map[string]any{}
	for k, v := range addr {
		bad[k] = v
	}
	bad["phone"] = "62001001"
	if st, m := r.do("POST", "/agentic/quotes", map[string]any{"email": "a@b", "items": []any{map[string]any{"variantId": variantID, "quantity": 1}}, "shippingAddress": bad},
		map[string]string{"Idempotency-Key": "q2"}); st != 400 || errCode(m) != "INVALID_PHONE" {
		t.Fatalf("bad phone: %d %v", st, m)
	}

	// Enrollment, then checkout needs ACTIVE.
	pres := map[string]any{"type": "REDIRECT", "returnUrl": "https://example.com/r"}
	_, enr := r.do("POST", "/agentic/enrollments", map[string]any{"source": "EXTERNAL", "owner": map[string]any{"type": "CLIENT_REFERENCE", "id": "u1"}, "presentation": pres},
		map[string]string{"Idempotency-Key": "e1"})
	enrID := enr["id"].(string)
	if enr["status"] != "REQUIRES_ACTION" || enr["nextAction"] == nil {
		t.Fatalf("enrollment %v", enr)
	}
	st, m := r.do("POST", "/agentic/checkouts", map[string]any{"quoteId": q["id"], "enrollmentId": enrID, "presentation": pres}, map[string]string{"Idempotency-Key": "c0"})
	if st != 400 || errCode(m) != "ENROLLMENT_NOT_ACTIVE" {
		t.Fatalf("checkout before active: %d %v", st, m)
	}
	if f.ActivateEnrollment("") != 1 {
		t.Fatal("activate")
	}
	_, enr = r.do("GET", "/agentic/enrollments/"+enrID, nil, nil)
	if enr["status"] != "ACTIVE" {
		t.Fatalf("enrollment %v", enr)
	}
	_, co := r.do("POST", "/agentic/checkouts", map[string]any{"quoteId": q["id"], "enrollmentId": enrID, "presentation": pres}, map[string]string{"Idempotency-Key": "c1"})
	if co["status"] != "REQUIRES_ACTION" || co["nextAction"].(map[string]any)["url"] == "" {
		t.Fatalf("checkout %v", co)
	}
	if got := f.PendingCheckouts(); len(got) != 1 || got[0] != co["id"] {
		t.Fatalf("pending %v", got)
	}
	// Approving through the hosted link completes it.
	resp, err := http.Get(co["nextAction"].(map[string]any)["url"].(string) + "/approve")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	_, got := r.do("GET", "/agentic/checkouts/"+co["id"].(string), nil, nil)
	if got["status"] != "COMPLETED" || got["orderId"] == nil || got["finalAmount"].(map[string]any)["amount"].(float64) != final {
		t.Fatalf("checkout after approve %v", got)
	}
	// Simulated checkout completes immediately.
	_, q3 := r.do("POST", "/agentic/quotes", map[string]any{"email": "a@b", "items": []any{map[string]any{"variantId": variantID, "quantity": 1}}, "shippingAddress": addr},
		map[string]string{"Idempotency-Key": "q3"})
	_, co2 := r.do("POST", "/agentic/checkouts", map[string]any{"quoteId": q3["id"], "enrollmentId": enrID, "presentation": pres},
		map[string]string{"Idempotency-Key": "c2", "X-Simulate-Checkout": "COMPLETED"})
	if co2["status"] != "COMPLETED" {
		t.Fatalf("simulated checkout %v", co2)
	}
	if st, _ := r.do("GET", "/agentic/checkouts/"+NewUUID(), nil, nil); st != 404 {
		t.Fatalf("unknown checkout status %d", st)
	}
}

// ---------------- fake OpenAI ----------------

func postJSON(t *testing.T, url, key string, body any) map[string]any {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", url, bytes.NewReader(b))
	req.Header.Set("Authorization", "Bearer "+key)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var m map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&m)
	if resp.StatusCode != 200 {
		t.Fatalf("POST %s: %d %v", url, resp.StatusCode, m)
	}
	return m
}

func TestFakeOpenAIChatCompletions(t *testing.T) {
	f := NewFakeOpenAI("ok")
	defer f.Close()
	f.SetParse("printer paper", map[string]any{"description": "printer paper"}, map[string]any{"description": "coffee pods", "catalog_sku": "coffee-capsules"})
	schema := map[string]any{"type": "object", "properties": map[string]any{"items": map[string]any{"type": "array"}}}

	m := postJSON(t, f.BaseURL()+"/chat/completions", "ok", map[string]any{
		"model": "gpt-5.1",
		"messages": []any{
			map[string]any{"role": "system", "content": "parse"},
			map[string]any{"role": "user", "content": "We're out of Printer Paper and coffee pods"},
		},
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "items", "schema": schema, "strict": true}},
	})
	content := m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)["content"].(string)
	var parsed struct {
		Items []map[string]any `json:"items"`
	}
	if err := json.Unmarshal([]byte(content), &parsed); err != nil || len(parsed.Items) != 2 || parsed.Items[1]["catalog_sku"] != "coffee-capsules" || parsed.Items[0]["urgency"] != "normal" {
		t.Fatalf("parse content %q", content)
	}

	tools := []any{map[string]any{"type": "function", "function": map[string]any{"name": "create_order_request"}},
		map[string]any{"type": "function", "function": map[string]any{"name": "list_addresses"}}}
	tests := []struct {
		name     string
		messages []any
		tool     string
	}{
		{"order", []any{map[string]any{"role": "system", "content": "PROMPT-X"}, map[string]any{"role": "user", "content": "get staples"}}, "create_order_request"},
		{"address", []any{map[string]any{"role": "user", "content": "which address options?"}}, "list_addresses"},
		{"after tool", []any{map[string]any{"role": "user", "content": "x"},
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": "c1"}}},
			map[string]any{"role": "tool", "tool_call_id": "c1", "content": "{}"}}, ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := postJSON(t, f.BaseURL()+"/chat/completions", "ok", map[string]any{"messages": tc.messages, "tools": tools})
			msg := m["choices"].([]any)[0].(map[string]any)["message"].(map[string]any)
			if tc.tool == "" {
				if s, _ := msg["content"].(string); s == "" {
					t.Fatalf("want text, got %v", msg)
				}
				return
			}
			tc0 := msg["tool_calls"].([]any)[0].(map[string]any)["function"].(map[string]any)
			if tc0["name"] != tc.tool {
				t.Fatalf("tool %v want %s", tc0["name"], tc.tool)
			}
		})
	}
	calls := f.Calls()
	if len(calls) != 4 || calls[1].System != "PROMPT-X" || calls[0].ReplyKind != "parse" {
		t.Fatalf("calls %+v", calls)
	}
}

func TestFakeOpenAIResponsesAndRealtime(t *testing.T) {
	f := NewFakeOpenAI("ok")
	defer f.Close()
	f.SetParse("standing desk", map[string]any{"description": "standing desk", "qty": 1})

	m := postJSON(t, f.BaseURL()+"/responses", "ok", map[string]any{
		"model": "gpt-5.1", "instructions": "parse it", "input": "Get us a standing desk for the new hire.",
		"text": map[string]any{"format": map[string]any{"type": "json_schema", "name": "x",
			"schema": map[string]any{"type": "object", "properties": map[string]any{"line_items": map[string]any{"type": "array"}}}}},
	})
	if !strings.Contains(m["output_text"].(string), `"line_items":[{`) || !strings.Contains(m["output_text"].(string), `"qty":1`) {
		t.Fatalf("responses parse: %v", m["output_text"])
	}
	m = postJSON(t, f.BaseURL()+"/responses", "ok", map[string]any{
		"input": []any{map[string]any{"role": "developer", "content": "SYS"}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "input_text", "text": "order pens"}}}},
		"tools": []any{map[string]any{"type": "function", "name": "create_order_request"}},
	})
	out := m["output"].([]any)[0].(map[string]any)
	if out["type"] != "function_call" || out["name"] != "create_order_request" || !strings.Contains(out["arguments"].(string), "order pens") {
		t.Fatalf("responses tool call %v", out)
	}
	if c := f.Calls(); c[1].System != "SYS" || c[1].LastUser != "order pens" {
		t.Fatalf("captured %+v", c[1])
	}

	r := postJSON(t, f.BaseURL()+"/realtime/client_secrets", "ok", map[string]any{
		"session": map[string]any{"type": "realtime", "model": "gpt-realtime", "instructions": "VOICE PROMPT"}})
	if v, _ := r["value"].(string); !strings.HasPrefix(v, "ek_") {
		t.Fatalf("client secret %v", r)
	}
	r = postJSON(t, f.BaseURL()+"/realtime/sessions", "ok", map[string]any{"model": "gpt-realtime", "instructions": "BETA"})
	if r["client_secret"] == nil {
		t.Fatalf("beta session %v", r)
	}
	ss := f.RealtimeSessions()
	if len(ss) != 2 || ss[0]["instructions"] != "VOICE PROMPT" || ss[1]["instructions"] != "BETA" {
		t.Fatalf("sessions %v", ss)
	}

	req, _ := http.NewRequest("POST", f.BaseURL()+"/responses", strings.NewReader("{}"))
	req.Header.Set("Authorization", "Bearer wrong")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != 401 {
		t.Fatalf("bad key status %d", resp.StatusCode)
	}
}

func TestSubscribeAgainstFakeServer(t *testing.T) {
	srv := http.NewServeMux()
	srv.HandleFunc("GET /api/v1/events", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("token") != "tok" {
			http.Error(w, `{"error":{"code":"unauthorized"}}`, 401)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		fl := w.(http.Flusher)
		fl.Flush()
		for i, typ := range []string{"request.created", "heartbeat", "request.status_changed"} {
			env, _ := json.Marshal(map[string]any{"id": i + 1, "type": typ, "request_id": "r1", "data": map[string]any{"to": "searching"}})
			_, _ = io.WriteString(w, "id: "+string(rune('1'+i))+"\nevent: "+typ+"\ndata: "+string(env)+"\n\n")
			fl.Flush()
		}
		<-r.Context().Done()
	})
	hs := &http.Server{Handler: srv}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = hs.Serve(ln) }()
	defer hs.Close()
	base := "http://" + ln.Addr().String()

	if _, err := Subscribe(context.Background(), base, "bad", ""); err == nil {
		t.Fatal("want error for bad token")
	}
	s, err := Subscribe(context.Background(), base, "tok", "r1")
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if !s.WaitFor(2*time.Second, func(es []SSEEvent) bool { return len(es) == 3 }) {
		t.Fatalf("events %v", s.Events())
	}
	if got := Seq(s.ForRequest("r1")); !reflect.DeepEqual(got, []string{"request.created", "status:searching"}) {
		t.Fatalf("seq %v", got)
	}
}

func TestResetDatabase(t *testing.T) {
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("TEST_DATABASE_URL not set")
	}
	ctx := context.Background()
	got, err := ResetDatabase(ctx, dsn, "jarvis_harness_selftest")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "/jarvis_harness_selftest") {
		t.Fatalf("dsn %s", redactDSN(got))
	}
	if err := DropDatabase(ctx, dsn, "jarvis_harness_selftest"); err != nil {
		t.Fatal(err)
	}
	if _, err := ResetDatabase(ctx, dsn, "bad;name"); err == nil {
		t.Fatal("want invalid name error")
	}
}
