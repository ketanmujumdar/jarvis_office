package reap

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

const testKey = "unit-test-key-not-a-secret"

type seen struct {
	Method, Path, Auth, Version, Idem, Sim, CT string
	Body                                       string
}

// recorder is a scripted server: responses[i] answers attempt i (the last one repeats).
type recorder struct {
	mu        sync.Mutex
	reqs      []seen
	responses []func(w http.ResponseWriter)
}

func (rc *recorder) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		rc.mu.Lock()
		rc.reqs = append(rc.reqs, seen{
			Method: r.Method, Path: r.URL.EscapedPath(), Auth: r.Header.Get("Authorization"),
			Version: r.Header.Get("Reap-Version"), Idem: r.Header.Get("Idempotency-Key"),
			Sim: r.Header.Get("X-Simulate-Checkout"), CT: r.Header.Get("Content-Type"), Body: string(b),
		})
		i := len(rc.reqs) - 1
		if i >= len(rc.responses) {
			i = len(rc.responses) - 1
		}
		resp := rc.responses[i]
		rc.mu.Unlock()
		resp(w)
	}
}

func (rc *recorder) all() []seen {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	return append([]seen(nil), rc.reqs...)
}

func jsonResp(status int, body string) func(http.ResponseWriter) {
	return func(w http.ResponseWriter) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}
}

func errResp(status int, code string) func(http.ResponseWriter) {
	return jsonResp(status, `{"error":{"code":"`+code+`","message":"m","detail":null}}`)
}

func newTestClient(t *testing.T, rc *recorder, mut ...func(*Options)) *HTTPClient {
	t.Helper()
	srv := httptest.NewServer(rc.handler())
	t.Cleanup(srv.Close)
	o := Options{BaseURL: srv.URL + "/", APIKey: testKey, RetryBackoff: time.Millisecond, Timeout: 2 * time.Second}
	for _, m := range mut {
		m(&o)
	}
	return NewHTTPClient(o)
}

const quoteJSON = `{"id":"q1","shippingOptions":[{"id":"s1","name":"Standard","selected":true,"price":{"amount":4,"currency":"SGD"}}],
"amountBreakdown":{"itemsSubtotal":{"amount":35.5,"currency":"SGD"},"discounts":[],"additionalCharges":[],"finalAmount":{"amount":39.5,"currency":"SGD"}},
"expiresAt":"2026-10-09T10:00:00Z"}`

func TestHeadersAndPaths(t *testing.T) {
	ok := jsonResp(200, `{}`)
	tests := []struct {
		name     string
		resp     func(http.ResponseWriter)
		call     func(c *HTTPClient) error
		sim      bool
		method   string
		path     string
		wantIdem string
		wantSim  string
		bodyHas  string
	}{
		{"search", jsonResp(200, `{"id":"s","products":[],"pagination":{"nextCursor":null,"hasNextPage":false,"returnedCount":0},"warnings":[]}`),
			func(c *HTTPClient) error {
				_, err := c.SearchProducts(context.Background(), SearchRequest{Query: "pens",
					MerchantPreference: &MerchantPreference{Mode: MerchantOnly, MerchantName: "popular.com.sg"}})
				return err
			}, false, "POST", "/agentic/products/search", "", "", `"merchantPreference":{"mode":"ONLY","merchantName":"popular.com.sg"}`},
		{"details", jsonResp(200, `{"products":[],"errors":[]}`), func(c *HTTPClient) error {
			_, err := c.GetProductDetails(context.Background(), []string{"p1", "p2"})
			return err
		}, false, "POST", "/agentic/products/details", "", "", `{"productIds":["p1","p2"]}`},
		{"variant nil options sends []", ok, func(c *HTTPClient) error {
			_, err := c.ResolveVariant(context.Background(), VariantRequest{ProductID: "p1"})
			return err
		}, false, "POST", "/agentic/products/variant", "", "", `"optionIds":[]`},
		{"create quote", jsonResp(200, quoteJSON), func(c *HTTPClient) error {
			_, err := c.CreateQuote(context.Background(), "idem-q", CreateQuoteRequest{Email: "a@b.sg", Items: []QuoteItem{{VariantID: "v", Quantity: 2}}})
			return err
		}, false, "POST", "/agentic/quotes", "idem-q", "", `"items":[{"variantId":"v","quantity":2}]`},
		{"get quote escapes id", jsonResp(200, quoteJSON), func(c *HTTPClient) error {
			_, err := c.GetQuote(context.Background(), "a/b")
			return err
		}, false, "GET", "/agentic/quotes/a%2Fb", "", "", ""},
		{"select shipping", jsonResp(200, quoteJSON), func(c *HTTPClient) error {
			_, err := c.SelectShippingOption(context.Background(), "q1", "s2")
			return err
		}, false, "POST", "/agentic/quotes/q1/shipping-option", "", "", `{"shippingOptionId":"s2"}`},
		{"create checkout", ok, func(c *HTTPClient) error {
			_, err := c.CreateCheckout(context.Background(), "idem-c", CreateCheckoutRequest{QuoteID: "q", EnrollmentID: "e",
				Presentation: Presentation{ReturnURL: "https://x.test/r"}})
			return err
		}, false, "POST", "/agentic/checkouts", "idem-c", "", `"presentation":{"type":"REDIRECT","returnUrl":"https://x.test/r"}`},
		{"create checkout simulate", ok, func(c *HTTPClient) error {
			_, err := c.CreateCheckout(context.Background(), "idem-c", CreateCheckoutRequest{QuoteID: "q", EnrollmentID: "e"})
			return err
		}, true, "POST", "/agentic/checkouts", "idem-c", "COMPLETED", ""},
		{"get checkout", ok, func(c *HTTPClient) error {
			_, err := c.GetCheckout(context.Background(), "c1")
			return err
		}, false, "GET", "/agentic/checkouts/c1", "", "", ""},
		{"create enrollment defaults", ok, func(c *HTTPClient) error {
			_, err := c.CreateEnrollment(context.Background(), "idem-e", CreateEnrollmentRequest{Owner: Owner{ID: "u1", Email: "u@x.sg"},
				Presentation: Presentation{ReturnURL: "https://x.test/r"}})
			return err
		}, false, "POST", "/agentic/enrollments", "idem-e", "", `"source":"EXTERNAL","owner":{"type":"CLIENT_REFERENCE","id":"u1","email":"u@x.sg"}`},
		{"get enrollment", ok, func(c *HTTPClient) error {
			_, err := c.GetEnrollment(context.Background(), "e1")
			return err
		}, false, "GET", "/agentic/enrollments/e1", "", "", ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := &recorder{responses: []func(http.ResponseWriter){tc.resp}}
			c := newTestClient(t, rc, func(o *Options) { o.SimulateCheckout = tc.sim })
			if err := tc.call(c); err != nil {
				t.Fatalf("call: %v", err)
			}
			r := rc.all()
			if len(r) != 1 {
				t.Fatalf("requests = %d, want 1", len(r))
			}
			got := r[0]
			if got.Method != tc.method || got.Path != tc.path {
				t.Errorf("%s %s, want %s %s", got.Method, got.Path, tc.method, tc.path)
			}
			if got.Auth != "Bearer "+testKey {
				t.Errorf("Authorization header wrong")
			}
			if got.Version != "2025-02-14" {
				t.Errorf("Reap-Version = %q", got.Version)
			}
			if got.Idem != tc.wantIdem {
				t.Errorf("Idempotency-Key = %q, want %q", got.Idem, tc.wantIdem)
			}
			if got.Sim != tc.wantSim {
				t.Errorf("X-Simulate-Checkout = %q, want %q", got.Sim, tc.wantSim)
			}
			if tc.method == "POST" && got.CT != "application/json" {
				t.Errorf("Content-Type = %q", got.CT)
			}
			if tc.method == "GET" && got.Body != "" {
				t.Errorf("GET had body %q", got.Body)
			}
			if tc.bodyHas != "" && !strings.Contains(got.Body, tc.bodyHas) {
				t.Errorf("body %s missing %s", got.Body, tc.bodyHas)
			}
		})
	}
}

func TestDecodesQuote(t *testing.T) {
	rc := &recorder{responses: []func(http.ResponseWriter){jsonResp(200, quoteJSON)}}
	q, err := newTestClient(t, rc).GetQuote(context.Background(), "q1")
	if err != nil {
		t.Fatal(err)
	}
	if q.ID != "q1" || q.AmountBreakdown.FinalAmount.Amount != 39.5 || !q.ShippingOptions[0].Selected {
		t.Fatalf("decoded %+v", q)
	}
	if domain.CentsFromFloat(q.AmountBreakdown.FinalAmount.Amount) != 3950 {
		t.Fatalf("cents conversion")
	}
	if !q.ExpiresAt.Equal(time.Date(2026, 10, 9, 10, 0, 0, 0, time.UTC)) {
		t.Fatalf("expiresAt %v", q.ExpiresAt)
	}
}

func TestErrorDecoding(t *testing.T) {
	tests := []struct {
		name       string
		resp       func(http.ResponseWriter)
		wantStatus int
		wantCode   string
		wantReason string
		notFound   bool
	}{
		{"envelope with structured detail", jsonResp(400, `{"error":{"code":"QUOTE_UNFULFILLABLE","message":"nope","detail":{"reason":"INVALID_PHONE","message":"Phone is invalid"}}}`),
			400, CodeQuoteUnfulfillable, ReasonInvalidPhone, false},
		{"enrollment not active", jsonResp(409, `{"error":{"code":"ENROLLMENT_NOT_ACTIVE","message":"x","detail":{"reason":"CARD_NOT_CAPTURED"}}}`),
			409, CodeEnrollmentNotActive, ReasonCardNotCaptured, false},
		{"404 maps to ErrNotFound", errResp(404, CodeQuoteNotFound), 404, CodeQuoteNotFound, "", true},
		{"non-JSON body", func(w http.ResponseWriter) { w.WriteHeader(418); _, _ = io.WriteString(w, "<html>teapot</html>") },
			418, "HTTP_418", "", false},
		{"empty body", func(w http.ResponseWriter) { w.WriteHeader(403) }, 403, "HTTP_403", "", false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := &recorder{responses: []func(http.ResponseWriter){tc.resp}}
			_, err := newTestClient(t, rc).GetQuote(context.Background(), "q1")
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if ae.HTTPStatus != tc.wantStatus || ae.Code != tc.wantCode || ae.Reason() != tc.wantReason {
				t.Fatalf("got %d %s %q", ae.HTTPStatus, ae.Code, ae.Reason())
			}
			if !IsCode(err, tc.wantCode) {
				t.Errorf("IsCode false")
			}
			if tc.wantReason != "" && !IsReason(err, tc.wantReason) {
				t.Errorf("IsReason false")
			}
			if errors.Is(err, domain.ErrNotFound) != tc.notFound {
				t.Errorf("ErrNotFound mismatch")
			}
			if !tc.notFound && !errors.Is(err, domain.ErrUpstream) {
				t.Errorf("want ErrUpstream")
			}
			if strings.Contains(err.Error(), testKey) {
				t.Errorf("error leaks api key")
			}
			if len(rc.all()) != 1 {
				t.Errorf("4xx must not be retried, got %d attempts", len(rc.all()))
			}
		})
	}
}

func TestRetries(t *testing.T) {
	tests := []struct {
		name         string
		responses    []func(http.ResponseWriter)
		maxRetries   int
		wantAttempts int
		wantErrCode  string
	}{
		{"503 then success", []func(http.ResponseWriter){errResp(503, CodeServiceUnavailable), errResp(503, CodeCheckoutTempUnavail), jsonResp(200, quoteJSON)}, 0, 3, ""},
		// Reap replays the first response for a key, so this one goes back to the caller, which
		// retries under a new key.
		{"quote temporarily unavailable not retried in-process", []func(http.ResponseWriter){errResp(503, CodeQuoteTempUnavailable), jsonResp(200, quoteJSON)}, 0, 1, CodeQuoteTempUnavailable},
		{"429 then success", []func(http.ResponseWriter){errResp(429, "RATE_LIMITED"), jsonResp(200, quoteJSON)}, 0, 2, ""},
		{"500 non-envelope then success", []func(http.ResponseWriter){func(w http.ResponseWriter) { w.WriteHeader(502) }, jsonResp(200, quoteJSON)}, 0, 2, ""},
		{"gives up after max retries", []func(http.ResponseWriter){errResp(503, CodeServiceUnavailable)}, 2, 3, CodeServiceUnavailable},
		{"retries disabled", []func(http.ResponseWriter){errResp(503, CodeServiceUnavailable)}, -1, 1, CodeServiceUnavailable},
		{"409 not retried", []func(http.ResponseWriter){errResp(409, CodeQuoteExpired), jsonResp(200, quoteJSON)}, 0, 1, CodeQuoteExpired},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rc := &recorder{responses: tc.responses}
			c := newTestClient(t, rc, func(o *Options) { o.MaxRetries = tc.maxRetries })
			_, err := c.CreateQuote(context.Background(), "same-key", CreateQuoteRequest{Email: "a@b.sg", Items: []QuoteItem{{VariantID: "v", Quantity: 1}}})
			if tc.wantErrCode == "" && err != nil {
				t.Fatalf("err = %v", err)
			}
			if tc.wantErrCode != "" && !IsCode(err, tc.wantErrCode) {
				t.Fatalf("err = %v, want %s", err, tc.wantErrCode)
			}
			reqs := rc.all()
			if len(reqs) != tc.wantAttempts {
				t.Fatalf("attempts = %d, want %d", len(reqs), tc.wantAttempts)
			}
			for _, r := range reqs {
				if r.Idem != "same-key" {
					t.Errorf("retry changed Idempotency-Key to %q", r.Idem)
				}
				if r.Body != reqs[0].Body {
					t.Errorf("retry changed body")
				}
			}
		})
	}
}

func TestRetryAfterHonouredAndContextCancel(t *testing.T) {
	rc := &recorder{responses: []func(http.ResponseWriter){func(w http.ResponseWriter) {
		w.Header().Set("Retry-After", "5")
		errResp(503, CodeServiceUnavailable)(w)
	}}}
	c := newTestClient(t, rc)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, err := c.GetCheckout(ctx, "c1")
	if err == nil || !IsCode(err, CodeServiceUnavailable) {
		t.Fatalf("err = %v", err)
	}
	if !errors.Is(err, domain.ErrUpstream) {
		t.Errorf("want ErrUpstream in chain")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("did not stop at ctx deadline: %v", d)
	}
	if n := len(rc.all()); n != 1 {
		t.Fatalf("attempts = %d, want 1 (Retry-After 5s > ctx)", n)
	}
}

func TestTimeoutRetriedThenSucceeds(t *testing.T) {
	var calls atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			time.Sleep(300 * time.Millisecond)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "status": "COMPLETED", "quoteId": "q", "orderId": "#1", "nextAction": nil})
	}))
	defer srv.Close()
	c := NewHTTPClient(Options{BaseURL: srv.URL, APIKey: testKey, Timeout: 50 * time.Millisecond, RetryBackoff: time.Millisecond})
	co, err := c.GetCheckout(context.Background(), "c1")
	if err != nil {
		t.Fatalf("err = %v", err)
	}
	if co.Status != CheckoutCompleted || co.OrderID == nil || *co.OrderID != "#1" || co.NextAction != nil {
		t.Fatalf("checkout %+v", co)
	}
	if calls.Load() != 2 {
		t.Fatalf("calls = %d, want 2", calls.Load())
	}
}

// A timed-out CreateCheckout may have been processed by Reap: the retry must carry the same
// Idempotency-Key and the identical body so Reap replays it instead of opening a second checkout.
func TestTimeoutOnCheckoutRetriedWithSameKey(t *testing.T) {
	type seen struct{ key, body string }
	var (
		mu   sync.Mutex
		reqs []seen
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		reqs = append(reqs, seen{r.Header.Get("Idempotency-Key"), string(b)})
		n := len(reqs)
		mu.Unlock()
		if n == 1 {
			time.Sleep(300 * time.Millisecond) // processed, but the response is too late
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"id": "c1", "status": "REQUIRES_ACTION", "quoteId": "q1",
			"nextAction": map[string]any{"type": "REDIRECT", "url": "https://pay.example/c1"}})
	}))
	defer srv.Close()
	c := NewHTTPClient(Options{BaseURL: srv.URL, APIKey: testKey, Timeout: 50 * time.Millisecond, RetryBackoff: time.Millisecond})
	co, err := c.CreateCheckout(context.Background(), "pay-1:checkout", CreateCheckoutRequest{QuoteID: "q1", EnrollmentID: "e1",
		Presentation: Presentation{Type: "REDIRECT", ReturnURL: "https://app.example/return"}})
	if err != nil || co.ID != "c1" {
		t.Fatalf("checkout %+v err %v", co, err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(reqs) != 2 {
		t.Fatalf("attempts = %d, want 2", len(reqs))
	}
	for _, r := range reqs {
		if r.key != "pay-1:checkout" || r.body != reqs[0].body {
			t.Fatalf("retry changed key/body: %+v", reqs)
		}
	}
}

func TestDeadServerTransportError(t *testing.T) {
	// A dead server: transport errors are retried, then surface as *TransportError + ErrUpstream.
	dead := httptest.NewServer(http.NotFoundHandler())
	url := dead.URL
	dead.Close()
	c2 := NewHTTPClient(Options{BaseURL: url, APIKey: testKey, MaxRetries: 1, RetryBackoff: time.Millisecond})
	_, err := c2.GetEnrollment(context.Background(), "e1")
	var te *TransportError
	if !errors.As(err, &te) || !errors.Is(err, domain.ErrUpstream) {
		t.Fatalf("err = %v", err)
	}
	if strings.Contains(err.Error(), testKey) {
		t.Fatal("transport error leaks key")
	}
}

func TestClientSideValidation(t *testing.T) {
	rc := &recorder{responses: []func(http.ResponseWriter){jsonResp(200, `{}`)}}
	c := newTestClient(t, rc)
	ctx := context.Background()
	long := strings.Repeat("k", 256)
	checks := map[string]error{}
	_, checks["empty query"] = c.SearchProducts(ctx, SearchRequest{Query: "  "})
	_, checks["no ids"] = c.GetProductDetails(ctx, nil)
	_, checks["11 ids"] = c.GetProductDetails(ctx, make([]string, 11))
	_, checks["no product"] = c.ResolveVariant(ctx, VariantRequest{})
	_, checks["quote no key"] = c.CreateQuote(ctx, "", CreateQuoteRequest{})
	_, checks["checkout long key"] = c.CreateCheckout(ctx, long, CreateCheckoutRequest{})
	_, checks["enrollment blank key"] = c.CreateEnrollment(ctx, " ", CreateEnrollmentRequest{})
	_, checks["get quote empty"] = c.GetQuote(ctx, "")
	_, checks["shipping empty"] = c.SelectShippingOption(ctx, "q", "")
	_, checks["get checkout empty"] = c.GetCheckout(ctx, "")
	_, checks["get enrollment empty"] = c.GetEnrollment(ctx, "")
	for name, err := range checks {
		if !errors.Is(err, domain.ErrValidation) {
			t.Errorf("%s: err = %v, want ErrValidation", name, err)
		}
	}
	if n := len(rc.all()); n != 0 {
		t.Errorf("validation failures sent %d requests", n)
	}
}

func TestNewIdempotencyKey(t *testing.T) {
	seen := map[string]bool{}
	for range 100 {
		k := NewIdempotencyKey()
		if len(k) != 36 || k[14] != '4' || seen[k] {
			t.Fatalf("bad/duplicate key %q", k)
		}
		seen[k] = true
	}
}

func TestDefaults(t *testing.T) {
	c := NewHTTPClient(Options{APIKey: "k"})
	if c.opts.BaseURL != DefaultBaseURL || c.opts.Version != DefaultVersion || c.opts.Timeout != DefaultTimeout ||
		c.opts.MaxRetries != DefaultMaxRetries || c.opts.RetryBackoff != DefaultRetryBackoff {
		t.Fatalf("defaults %+v", c.opts)
	}
}

func TestParseRetryAfter(t *testing.T) {
	tests := map[string]time.Duration{"": 0, "2": 2 * time.Second, "junk": 0, "-1": 0}
	for in, want := range tests {
		if got := parseRetryAfter(in); got != want {
			t.Errorf("parseRetryAfter(%q) = %v, want %v", in, got, want)
		}
	}
	if d := parseRetryAfter(time.Now().Add(3 * time.Second).UTC().Format(http.TimeFormat)); d <= 0 || d > 4*time.Second {
		t.Errorf("http-date retry-after = %v", d)
	}
}

func TestCheckoutStatusTerminal(t *testing.T) {
	for st, want := range map[CheckoutStatus]bool{
		CheckoutRequiresAction: false, CheckoutProcessing: false,
		CheckoutCompleted: true, CheckoutFailed: true, CheckoutExpired: true,
	} {
		if st.Terminal() != want {
			t.Errorf("%s.Terminal() = %v", st, !want)
		}
	}
}

func TestIsItemRejection(t *testing.T) {
	sandboxStock := decodeAPIError(400, []byte(`{"error":{"code":"AGENTIC_REQUEST_REJECTED","message":"The request was rejected","detail":{"errors":[{"field":"items"}]}}}`))
	if got := sandboxStock.FieldErrors(); len(got) != 1 || got[0] != "items" {
		t.Fatalf("field errors = %v", got)
	}
	cases := []struct {
		err  error
		want bool
	}{
		{sandboxStock, true},
		{fmt.Errorf("wrapped: %w", sandboxStock), true},
		{&APIError{HTTPStatus: 409, Code: "SOMETHING"}, true},
		{&APIError{HTTPStatus: 409, Code: CodeVariantUnavailable}, true},
		{&APIError{HTTPStatus: 409, Code: CodeQuoteExpired}, false},
		{&APIError{HTTPStatus: 400, Code: CodeRequestRejected, Detail: map[string]any{"errors": []any{map[string]any{"field": "email"}}}}, false},
		{&APIError{HTTPStatus: 400, Code: CodeRequestRejected}, false},
		{&APIError{HTTPStatus: 400, Code: CodeQuoteUnfulfillable, Detail: map[string]any{"reason": ReasonItemsUnshippable}}, false},
		{&APIError{HTTPStatus: 503, Code: CodeServiceUnavailable}, false},
		{errors.New("plain"), false},
	}
	for i, tc := range cases {
		if got := IsItemRejection(tc.err); got != tc.want {
			t.Errorf("case %d (%v): got %v want %v", i, tc.err, got, tc.want)
		}
	}
}
