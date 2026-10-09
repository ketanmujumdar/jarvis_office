// Package reap is the typed client for the Reap Agentic Payments API (docs/reap). Owner: agent C.
//
// Every call sends:
//
//	Authorization: Bearer $REAP_API_KEY
//	Reap-Version: 2025-02-14
//	Accept: application/json
//	Content-Type: application/json            (POST)
//	Idempotency-Key: <key>                    (POST quotes, checkouts, enrollments; required by Reap)
//	X-Simulate-Checkout: COMPLETED            (POST checkouts, only when Options.SimulateCheckout; sandbox)
//
// Non-2xx responses decode {"error":{"code","message","detail"}} into *APIError.
//
// Retries: transport errors, 429 and 5xx are retried with exponential backoff (honouring
// Retry-After) up to Options.MaxRetries times, except QUOTE_TEMPORARILY_UNAVAILABLE: Reap replays
// the first response for a key, so that one is returned to the caller to retry under a new key. Every retry of a POST that takes an idempotency key
// reuses the SAME key, so Reap replays the first result instead of executing twice. The other POSTs
// (search, details, variant, select shipping option) are read-only or naturally idempotent.
package reap

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
)

// Client is the Reap API surface Jarvis uses. Implementations: HTTPClient (real) and the
// httptest fake server in reap/fakereap (agent C). Mandates are NOT live and intentionally absent.
type Client interface {
	SearchProducts(ctx context.Context, req SearchRequest) (SearchResponse, error)
	GetProductDetails(ctx context.Context, productIDs []string) (DetailsResponse, error)
	ResolveVariant(ctx context.Context, req VariantRequest) (Variant, error)

	CreateQuote(ctx context.Context, idempotencyKey string, req CreateQuoteRequest) (Quote, error)
	GetQuote(ctx context.Context, quoteID string) (Quote, error)
	SelectShippingOption(ctx context.Context, quoteID, shippingOptionID string) (Quote, error)

	CreateCheckout(ctx context.Context, idempotencyKey string, req CreateCheckoutRequest) (Checkout, error)
	GetCheckout(ctx context.Context, checkoutID string) (Checkout, error)

	CreateEnrollment(ctx context.Context, idempotencyKey string, req CreateEnrollmentRequest) (Enrollment, error)
	GetEnrollment(ctx context.Context, enrollmentID string) (Enrollment, error)
}

// Defaults applied by NewHTTPClient when the corresponding Options field is zero.
const (
	DefaultBaseURL      = "https://sg.sandbox.api.reap.global"
	DefaultVersion      = "2025-02-14"
	DefaultTimeout      = 30 * time.Second
	DefaultMaxRetries   = 3
	DefaultRetryBackoff = 250 * time.Millisecond
	maxRetryWait        = 10 * time.Second
	maxErrorBody        = 4 << 10
	maxResponseBody     = 8 << 20
)

// Options configure HTTPClient.
type Options struct {
	BaseURL          string // https://sg.sandbox.api.reap.global
	APIKey           string // secret; never log
	Version          string // 2025-02-14
	SimulateCheckout bool   // sandbox: send X-Simulate-Checkout: COMPLETED on CreateCheckout
	Timeout          time.Duration
	HTTPClient       *http.Client // optional; tests inject httptest server client

	// MaxRetries is the number of retries after the first attempt (0 => DefaultMaxRetries,
	// negative => no retries).
	MaxRetries int
	// RetryBackoff is the base delay; attempt n waits RetryBackoff*2^(n-1) unless the server sent
	// Retry-After (capped at 10s). 0 => DefaultRetryBackoff.
	RetryBackoff time.Duration
}

// HTTPClient implements Client over HTTP.
type HTTPClient struct {
	opts Options
	hc   *http.Client
}

var _ Client = (*HTTPClient)(nil)

// NewHTTPClient builds the real client.
func NewHTTPClient(opts Options) *HTTPClient {
	if opts.BaseURL == "" {
		opts.BaseURL = DefaultBaseURL
	}
	opts.BaseURL = strings.TrimRight(opts.BaseURL, "/")
	if opts.Version == "" {
		opts.Version = DefaultVersion
	}
	if opts.Timeout <= 0 {
		opts.Timeout = DefaultTimeout
	}
	if opts.MaxRetries == 0 {
		opts.MaxRetries = DefaultMaxRetries
	} else if opts.MaxRetries < 0 {
		opts.MaxRetries = 0
	}
	if opts.RetryBackoff <= 0 {
		opts.RetryBackoff = DefaultRetryBackoff
	}
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: opts.Timeout}
	}
	return &HTTPClient{opts: opts, hc: hc}
}

// NewIdempotencyKey returns a random UUIDv4, the format Reap recommends for Idempotency-Key.
// Callers must persist it (payments.idempotency_key) and reuse it for every attempt of one
// logical operation.
func NewIdempotencyKey() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("reap: crypto/rand failed: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// ---------- endpoints ----------

func (c *HTTPClient) SearchProducts(ctx context.Context, req SearchRequest) (SearchResponse, error) {
	var out SearchResponse
	if strings.TrimSpace(req.Query) == "" {
		return out, fmt.Errorf("reap: search query is empty: %w", domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodPost, "/agentic/products/search", "", nil, req, &out)
	return out, err
}

func (c *HTTPClient) GetProductDetails(ctx context.Context, productIDs []string) (DetailsResponse, error) {
	var out DetailsResponse
	if len(productIDs) < 1 || len(productIDs) > 10 {
		return out, fmt.Errorf("reap: details takes 1..10 product ids, got %d: %w", len(productIDs), domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodPost, "/agentic/products/details", "", nil, DetailsRequest{ProductIDs: productIDs}, &out)
	return out, err
}

func (c *HTTPClient) ResolveVariant(ctx context.Context, req VariantRequest) (Variant, error) {
	var out Variant
	if req.ProductID == "" {
		return out, fmt.Errorf("reap: variant needs productId: %w", domain.ErrValidation)
	}
	if req.OptionIDs == nil {
		req.OptionIDs = []string{}
	}
	err := c.do(ctx, http.MethodPost, "/agentic/products/variant", "", nil, req, &out)
	return out, err
}

func (c *HTTPClient) CreateQuote(ctx context.Context, idempotencyKey string, req CreateQuoteRequest) (Quote, error) {
	var out Quote
	if err := checkIdempotencyKey(idempotencyKey); err != nil {
		return out, err
	}
	err := c.do(ctx, http.MethodPost, "/agentic/quotes", idempotencyKey, nil, req, &out)
	return out, err
}

func (c *HTTPClient) GetQuote(ctx context.Context, quoteID string) (Quote, error) {
	var out Quote
	if quoteID == "" {
		return out, fmt.Errorf("reap: empty quote id: %w", domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodGet, "/agentic/quotes/"+url.PathEscape(quoteID), "", nil, nil, &out)
	return out, err
}

func (c *HTTPClient) SelectShippingOption(ctx context.Context, quoteID, shippingOptionID string) (Quote, error) {
	var out Quote
	if quoteID == "" || shippingOptionID == "" {
		return out, fmt.Errorf("reap: quote id and shipping option id are required: %w", domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodPost, "/agentic/quotes/"+url.PathEscape(quoteID)+"/shipping-option", "", nil,
		SelectShippingRequest{ShippingOptionID: shippingOptionID}, &out)
	return out, err
}

func (c *HTTPClient) CreateCheckout(ctx context.Context, idempotencyKey string, req CreateCheckoutRequest) (Checkout, error) {
	var out Checkout
	if err := checkIdempotencyKey(idempotencyKey); err != nil {
		return out, err
	}
	if req.Presentation.Type == "" {
		req.Presentation.Type = "REDIRECT"
	}
	var extra map[string]string
	if c.opts.SimulateCheckout {
		extra = map[string]string{"X-Simulate-Checkout": "COMPLETED"}
	}
	err := c.do(ctx, http.MethodPost, "/agentic/checkouts", idempotencyKey, extra, req, &out)
	return out, err
}

func (c *HTTPClient) GetCheckout(ctx context.Context, checkoutID string) (Checkout, error) {
	var out Checkout
	if checkoutID == "" {
		return out, fmt.Errorf("reap: empty checkout id: %w", domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodGet, "/agentic/checkouts/"+url.PathEscape(checkoutID), "", nil, nil, &out)
	return out, err
}

func (c *HTTPClient) CreateEnrollment(ctx context.Context, idempotencyKey string, req CreateEnrollmentRequest) (Enrollment, error) {
	var out Enrollment
	if err := checkIdempotencyKey(idempotencyKey); err != nil {
		return out, err
	}
	if req.Source == "" {
		req.Source = "EXTERNAL"
	}
	if req.Owner.Type == "" {
		req.Owner.Type = "CLIENT_REFERENCE"
	}
	if req.Presentation.Type == "" {
		req.Presentation.Type = "REDIRECT"
	}
	err := c.do(ctx, http.MethodPost, "/agentic/enrollments", idempotencyKey, nil, req, &out)
	return out, err
}

func (c *HTTPClient) GetEnrollment(ctx context.Context, enrollmentID string) (Enrollment, error) {
	var out Enrollment
	if enrollmentID == "" {
		return out, fmt.Errorf("reap: empty enrollment id: %w", domain.ErrValidation)
	}
	err := c.do(ctx, http.MethodGet, "/agentic/enrollments/"+url.PathEscape(enrollmentID), "", nil, nil, &out)
	return out, err
}

func checkIdempotencyKey(k string) error {
	if strings.TrimSpace(k) == "" || len(k) > 255 {
		return fmt.Errorf("reap: Idempotency-Key must be 1..255 chars: %w", domain.ErrValidation)
	}
	return nil
}

// ---------- transport ----------

// do performs one logical call with retries. body is JSON-encoded once so every attempt sends the
// identical bytes with the identical Idempotency-Key.
func (c *HTTPClient) do(ctx context.Context, method, path, idemKey string, extra map[string]string, body, out any) error {
	var payload []byte
	if body != nil {
		var err error
		if payload, err = json.Marshal(body); err != nil {
			return fmt.Errorf("reap: encode %s %s: %w", method, path, err)
		}
	}
	var lastErr error
	for attempt := 0; ; attempt++ {
		retryAfter, err := c.once(ctx, method, path, idemKey, extra, payload, out)
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt >= c.opts.MaxRetries || !shouldRetry(ctx, err) {
			return lastErr
		}
		wait := c.opts.RetryBackoff << attempt
		if retryAfter > 0 {
			wait = retryAfter
		}
		if wait > maxRetryWait {
			wait = maxRetryWait
		}
		t := time.NewTimer(wait)
		select {
		case <-ctx.Done():
			t.Stop()
			return fmt.Errorf("%w (gave up retrying: %v)", lastErr, ctx.Err())
		case <-t.C:
		}
	}
}

// once performs a single HTTP attempt. It returns the server's Retry-After hint (0 if none).
func (c *HTTPClient) once(ctx context.Context, method, path, idemKey string, extra map[string]string, payload []byte, out any) (time.Duration, error) {
	actx, cancel := context.WithTimeout(ctx, c.opts.Timeout)
	defer cancel()

	var rdr io.Reader
	if payload != nil {
		rdr = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(actx, method, c.opts.BaseURL+path, rdr)
	if err != nil {
		return 0, fmt.Errorf("reap: build %s %s: %w", method, path, err)
	}
	req.Header.Set("Authorization", "Bearer "+c.opts.APIKey)
	req.Header.Set("Reap-Version", c.opts.Version)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	for k, v := range extra {
		req.Header.Set(k, v)
	}

	resp, err := c.hc.Do(req)
	if err != nil {
		// Never include the request (headers carry the API key); url.Error only has method+URL.
		return 0, &TransportError{Method: method, Path: path, Err: err}
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, maxErrorBody))
		return parseRetryAfter(resp.Header.Get("Retry-After")), decodeAPIError(resp.StatusCode, raw)
	}
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if err != nil {
		return 0, &TransportError{Method: method, Path: path, Err: err}
	}
	if out != nil && len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, out); err != nil {
			return 0, fmt.Errorf("reap: decode %s %s response: %v: %w", method, path, err, domain.ErrUpstream)
		}
	}
	return 0, nil
}

func shouldRetry(ctx context.Context, err error) bool {
	if ctx.Err() != nil {
		return false
	}
	var ae *APIError
	if errors.As(err, &ae) {
		// Reap caches the first response for an idempotency key (24h), so retrying
		// QUOTE_TEMPORARILY_UNAVAILABLE under the same key would only replay it. The caller
		// retries with a new key instead (a quote charges nothing).
		if ae.Code == CodeQuoteTempUnavailable {
			return false
		}
		return ae.Retryable()
	}
	var te *TransportError
	return errors.As(err, &te)
}

func parseRetryAfter(v string) time.Duration {
	v = strings.TrimSpace(v)
	if v == "" {
		return 0
	}
	if secs, err := strconv.Atoi(v); err == nil && secs >= 0 {
		return time.Duration(secs) * time.Second
	}
	if t, err := http.ParseTime(v); err == nil {
		if d := time.Until(t); d > 0 {
			return d
		}
	}
	return 0
}

func decodeAPIError(status int, raw []byte) *APIError {
	var env struct {
		Error *struct {
			Code    string          `json:"code"`
			Message string          `json:"message"`
			Detail  json.RawMessage `json:"detail"`
		} `json:"error"`
	}
	ae := &APIError{HTTPStatus: status}
	if err := json.Unmarshal(raw, &env); err == nil && env.Error != nil && env.Error.Code != "" {
		ae.Code = env.Error.Code
		ae.Message = env.Error.Message
		if d := bytes.TrimSpace(env.Error.Detail); len(d) > 0 && !bytes.Equal(d, []byte("null")) {
			var detail any
			if json.Unmarshal(d, &detail) == nil {
				ae.Detail = detail
			}
		}
		return ae
	}
	ae.Code = "HTTP_" + strconv.Itoa(status)
	msg := strings.TrimSpace(string(raw))
	if len(msg) > 200 {
		msg = msg[:200] + "..."
	}
	if msg == "" {
		msg = http.StatusText(status)
	}
	ae.Message = msg
	return ae
}

// TransportError is a network-level failure (DNS, connection reset, timeout). It is retried and
// unwraps to domain.ErrUpstream.
type TransportError struct {
	Method string
	Path   string
	Err    error
}

func (e *TransportError) Error() string {
	return fmt.Sprintf("reap: %s %s: %v", e.Method, e.Path, e.Err)
}

// Unwrap exposes both the cause (e.g. context.DeadlineExceeded) and domain.ErrUpstream.
func (e *TransportError) Unwrap() []error { return []error{e.Err, domain.ErrUpstream} }

// Error codes documented in docs/reap (agentic endpoints).
const (
	CodeRequestRejected        = "AGENTIC_REQUEST_REJECTED"
	CodePaymentsNotEnabled     = "AGENTIC_PAYMENTS_NOT_ENABLED"
	CodeResourceNotFound       = "AGENTIC_RESOURCE_NOT_FOUND"
	CodeServiceUnavailable     = "AGENTIC_SERVICE_UNAVAILABLE" // 503; observed intermittently in sandbox -> retry with backoff
	CodeMerchantNotResolved    = "MERCHANT_NOT_RESOLVED"
	CodeCardPaymentUnavailable = "CARD_PAYMENT_UNAVAILABLE"
	CodeCheckoutURLInvalid     = "CHECKOUT_URL_INVALID"
	CodeOfferCodeExpired       = "OFFER_CODE_EXPIRED"
	CodeOfferCodeInvalid       = "OFFER_CODE_INVALID"
	CodeQuoteExpired           = "QUOTE_EXPIRED"
	CodeQuoteNotFound          = "QUOTE_NOT_FOUND"
	CodeQuoteNotMutable        = "QUOTE_NOT_MUTABLE"
	CodeQuoteReplacement       = "QUOTE_REPLACEMENT_REQUIRED"
	CodeQuoteTempUnavailable   = "QUOTE_TEMPORARILY_UNAVAILABLE"
	CodeQuoteUnfulfillable     = "QUOTE_UNFULFILLABLE"
	CodeShippingOptionInvalid  = "SHIPPING_OPTION_INVALID"
	CodeVariantUnavailable     = "VARIANT_UNAVAILABLE"
	CodeCheckoutTempUnavail    = "CHECKOUT_TEMPORARILY_UNAVAILABLE"
	CodeEnrollmentNotActive    = "ENROLLMENT_NOT_ACTIVE"
	CodeEnrollmentNotFound     = "ENROLLMENT_NOT_FOUND"
	CodeCheckoutNotFound       = "CHECKOUT_NOT_FOUND"
	CodeCardNotFound           = "AGENTIC_CARD_NOT_FOUND"
)

// Values of error.detail.reason (NOT error codes): match them with IsReason, never IsCode.
// QUOTE_UNFULFILLABLE carries INVALID_PHONE, ITEMS_UNSHIPPABLE, ADDRESS_LINE_2_REQUIRED and
// STATE_OR_PROVINCE_REQUIRED; ENROLLMENT_NOT_ACTIVE carries CARD_NOT_CAPTURED.
const (
	ReasonInvalidPhone         = "INVALID_PHONE"
	ReasonItemsUnshippable     = "ITEMS_UNSHIPPABLE"
	ReasonAddressLine2Required = "ADDRESS_LINE_2_REQUIRED"
	ReasonStateRequired        = "STATE_OR_PROVINCE_REQUIRED"
	ReasonCardNotCaptured      = "CARD_NOT_CAPTURED"
)

// APIError is a non-2xx Reap response.
type APIError struct {
	HTTPStatus int
	Code       string
	Message    string
	Detail     any // decoded JSON (usually map[string]any{"reason":..., "message":...}) or nil
}

func (e *APIError) Error() string {
	if r := e.Reason(); r != "" {
		return fmt.Sprintf("reap: %d %s (%s): %s", e.HTTPStatus, e.Code, r, e.Message)
	}
	return fmt.Sprintf("reap: %d %s: %s", e.HTTPStatus, e.Code, e.Message)
}

// Unwrap lets errors.Is(err, domain.ErrUpstream) / domain.ErrNotFound work.
func (e *APIError) Unwrap() error {
	if e.HTTPStatus == http.StatusNotFound {
		return domain.ErrNotFound
	}
	return domain.ErrUpstream
}

// Retryable reports whether retrying the same request (same idempotency key) may succeed.
func (e *APIError) Retryable() bool {
	switch e.Code {
	case CodeServiceUnavailable, CodeQuoteTempUnavailable, CodeCheckoutTempUnavail:
		return true
	}
	return e.HTTPStatus >= 500 || e.HTTPStatus == http.StatusTooManyRequests
}

// Reason returns detail.reason when Reap sent a structured detail (e.g. QUOTE_UNFULFILLABLE ->
// INVALID_PHONE, ENROLLMENT_NOT_ACTIVE -> CARD_NOT_CAPTURED), else "".
func (e *APIError) Reason() string {
	if m, ok := e.Detail.(map[string]any); ok {
		if r, ok := m["reason"].(string); ok {
			return r
		}
	}
	return ""
}

// IsCode reports whether err is an *APIError with the given code.
func IsCode(err error, code string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Code == code
}

// IsReason reports whether err is an *APIError whose detail.reason equals reason.
func IsReason(err error, reason string) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.Reason() == reason
}
