// Package fakereap is an in-memory, stateful fake of the Reap Agentic Payments API served over
// httptest. Point the real reap.HTTPClient at it (Server.Client()) so tests exercise the real wire
// format: headers, idempotency, error envelopes and retries.
//
// Behaviour mirrors docs/reap:
//   - every request needs "Authorization: Bearer <APIKey>" and "Reap-Version: 2025-02-14";
//   - POST quotes/checkouts/enrollments need Idempotency-Key; a repeat with the same key and body
//     replays the first response, a repeat with a different body is rejected;
//   - search runs over a fixture catalogue of SG products (fixtures.go); merchantPreference takes the
//     merchant DOMAIN, unknown names fail with MERCHANT_NOT_RESOLVED;
//   - quotes carry Standard/Express shipping options (Standard preselected), GST-inclusive tax and
//     an expiry (Options.QuoteTTL);
//   - enrollments start REQUIRES_ACTION with a hosted card-entry URL and become ACTIVE when the
//     hosted URL is opened or a test calls ActivateEnrollment;
//   - checkouts start REQUIRES_ACTION with a hosted approval URL; ApproveCheckout (or opening the
//     hosted URL) moves them to PROCESSING and the next GET reports COMPLETED with an orderId.
//     X-Simulate-Checkout: COMPLETED makes the first GET report COMPLETED without approval.
//
// Hosted pages (GET /hosted/enrollments/{id}, GET /hosted/checkouts/{id}) need no auth, like the real
// Reap-hosted pages; they apply the action and 303-redirect to presentation.returnUrl. Append
// ?decision=decline to fail instead.
package fakereap

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/reap"
)

// Defaults.
const (
	DefaultAPIKey    = "fake-reap-key"
	DefaultQuoteTTL  = 15 * time.Minute
	DefaultActionTTL = 30 * time.Minute

	StandardShippingCents  = 400
	ExpressShippingCents   = 1200
	FreeShippingOverCents  = 6000 // Standard is free at or above this items subtotal
	gstPercent             = 9
	ShippingStandardSuffix = "_standard"
	ShippingExpressSuffix  = "_express"
)

// Offer codes understood by the fake.
const (
	OfferCodeValid   = "SAVE10"    // 10% off items
	OfferCodeExpired = "EXPIRED10" // OFFER_CODE_EXPIRED
)

// Options configure the fake. The zero value is usable.
type Options struct {
	APIKey   string           // expected bearer token; default DefaultAPIKey
	Clock    func() time.Time // default time.Now; Server.Advance shifts it
	QuoteTTL time.Duration    // default 15m
	// ActionTTL is how long hosted enrollment/checkout URLs stay valid before EXPIRED (default 30m).
	ActionTTL time.Duration
	Products  []FixtureProduct // default DefaultProducts()

	AutoActivateEnrollments bool // enrollments are ACTIVE immediately (no hosted step)
	AutoApproveCheckouts    bool // checkouts behave as if X-Simulate-Checkout: COMPLETED was sent
	// CacheServerErrors caches 5xx first responses under their idempotency key too (Reap documents
	// that the first response for a key is replayed for 24h). Default false: 5xx are not cached.
	CacheServerErrors bool
	// AllowHTTPReturnURL accepts http:// return URLs (local demo). Real Reap requires HTTPS.
	AllowHTTPReturnURL bool
}

// RecordedRequest is one request the fake received (the Authorization value is never kept).
type RecordedRequest struct {
	Method           string
	Path             string
	IdempotencyKey   string
	ReapVersion      string
	SimulateCheckout string
	Authorized       bool
	Body             []byte
}

// Fault is an injected failure returned before any other processing.
type Fault struct {
	Method string // "" matches any
	Path   string // exact path, or prefix when it ends with "*"
	Status int    // e.g. 503
	Code   string // e.g. reap.CodeServiceUnavailable
	Times  int    // how many matching requests fail; <=0 means 1
	// RetryAfter, if set, is sent as the Retry-After header (seconds).
	RetryAfter int
	// Stage selects when the fault fires on idempotent endpoints (quotes, checkouts,
	// enrollments). Ignored (treated as FaultBefore) elsewhere.
	Stage FaultStage
	// Skip lets this many matching requests through before the fault starts firing.
	Skip int
	// Drop closes the connection without a response (a transport error for the client) instead
	// of writing Status/Code.
	Drop bool
}

// FaultStage is when an injected fault fires.
type FaultStage string

const (
	// FaultBefore (default): before auth and idempotency; nothing is processed or cached.
	FaultBefore FaultStage = ""
	// FaultInHandler: the fault IS the handler's response, so it goes through the idempotency
	// cache like any first response (cached when Options.CacheServerErrors or Status < 500).
	FaultInHandler FaultStage = "handler"
	// FaultAfterProcess: the request is processed and its real response cached, but the client
	// gets the fault ("Reap did it, the response was lost"). Replays of the same key also get the
	// fault while it has Times left.
	FaultAfterProcess FaultStage = "after"
)

// Server is the fake. Create with New, stop with Close.
type Server struct {
	URL string

	srv  *httptest.Server
	opts Options

	mu          sync.Mutex
	offset      time.Duration
	products    []*FixtureProduct
	productByID map[string]*FixtureProduct
	variantByID map[string]variantRef
	domains     map[string]bool
	quotes      map[string]*quoteState
	checkouts   map[string]*checkoutState
	enrollments map[string]*enrollmentState
	idem        map[string]idemEntry
	faults      []*Fault
	requests    []RecordedRequest
	orderSeq    int
	maxQty      map[string]int // variant id -> max quantity a quote may ask for (0 = unlimited)
}

type variantRef struct {
	p *FixtureProduct
	i int
}

type quoteItem struct {
	VariantID string
	Quantity  int
	UnitCents int64
}

type quoteState struct {
	ID          string
	Domain      string
	Merchant    string
	Items       []quoteItem
	OfferCode   string
	DiscountPct int64
	Selected    string // shipping option id
	ExpiresAt   time.Time
	Consumed    bool // a checkout was opened against it
}

type checkoutState struct {
	ID              string
	QuoteID         string
	EnrollmentID    string
	Status          reap.CheckoutStatus
	AmountCents     int64
	ReturnURL       string
	ActionURL       string
	ActionExpiresAt time.Time
	Simulated       bool
	OrderID         string
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type enrollmentState struct {
	ID              string
	Status          reap.EnrollmentStatus
	Owner           reap.Owner
	ReturnURL       string
	ActionURL       string
	ActionExpiresAt time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type idemEntry struct {
	fingerprint [32]byte
	status      int
	body        []byte
}

// New starts a fake Reap server.
func New(opts Options) *Server {
	if opts.APIKey == "" {
		opts.APIKey = DefaultAPIKey
	}
	if opts.Clock == nil {
		opts.Clock = time.Now
	}
	if opts.QuoteTTL <= 0 {
		opts.QuoteTTL = DefaultQuoteTTL
	}
	if opts.ActionTTL <= 0 {
		opts.ActionTTL = DefaultActionTTL
	}
	if opts.Products == nil {
		opts.Products = DefaultProducts()
	}
	s := &Server{
		opts:        opts,
		productByID: map[string]*FixtureProduct{},
		variantByID: map[string]variantRef{},
		domains:     map[string]bool{},
		quotes:      map[string]*quoteState{},
		checkouts:   map[string]*checkoutState{},
		enrollments: map[string]*enrollmentState{},
		idem:        map[string]idemEntry{},
	}
	for i := range opts.Products {
		p := opts.Products[i]
		pp := &p
		s.products = append(s.products, pp)
		s.productByID[p.ID] = pp
		s.domains[strings.ToLower(p.Domain)] = true
		for vi := range pp.Variants {
			s.variantByID[pp.Variants[vi].ID] = variantRef{pp, vi}
		}
	}
	s.srv = httptest.NewServer(s.routes())
	s.URL = s.srv.URL
	return s
}

// Close shuts the server down.
func (s *Server) Close() { s.srv.Close() }

// APIKey is the bearer token the fake accepts.
func (s *Server) APIKey() string { return s.opts.APIKey }

// Client returns a real reap.HTTPClient pointed at the fake, with fast retries. mutate, if given,
// can adjust the options (e.g. SimulateCheckout, MaxRetries).
func (s *Server) Client(mutate ...func(*reap.Options)) *reap.HTTPClient {
	o := reap.Options{
		BaseURL:      s.URL,
		APIKey:       s.opts.APIKey,
		Version:      reap.DefaultVersion,
		Timeout:      5 * time.Second,
		RetryBackoff: time.Millisecond,
		HTTPClient:   s.srv.Client(),
	}
	for _, m := range mutate {
		m(&o)
	}
	return reap.NewHTTPClient(o)
}

// Now is the fake's current time (Options.Clock plus Advance offsets).
func (s *Server) Now() time.Time {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.now()
}

func (s *Server) now() time.Time { return s.opts.Clock().Add(s.offset).UTC() }

// Advance moves the fake's clock forward (quote and hosted-page expiry).
func (s *Server) Advance(d time.Duration) {
	s.mu.Lock()
	s.offset += d
	s.mu.Unlock()
}

// ---------- test hooks ----------

// InjectFault makes the next f.Times matching requests fail with f.Status / f.Code.
func (s *Server) InjectFault(f Fault) {
	if f.Times <= 0 {
		f.Times = 1
	}
	s.mu.Lock()
	s.faults = append(s.faults, &f)
	s.mu.Unlock()
}

// Requests returns a copy of every request received so far.
func (s *Server) Requests() []RecordedRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]RecordedRequest(nil), s.requests...)
}

// RequestsTo returns the recorded requests for method+path.
func (s *Server) RequestsTo(method, path string) []RecordedRequest {
	var out []RecordedRequest
	for _, r := range s.Requests() {
		if r.Method == method && r.Path == path {
			out = append(out, r)
		}
	}
	return out
}

// Counts reports how many quotes, checkouts and enrollments exist (idempotent replays do not add).
func (s *Server) Counts() (quotes, checkouts, enrollments int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.quotes), len(s.checkouts), len(s.enrollments)
}

// SetVariantPrice changes a variant's live price (simulate price drift between search and quote).
func (s *Server) SetVariantPrice(variantID string, cents int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.variantByID[variantID]
	if !ok {
		return fmt.Errorf("fakereap: unknown variant %q", variantID)
	}
	ref.p.Variants[ref.i].PriceCents = cents
	return nil
}

// SetVariantMaxQuantity limits how many units of a variant one quote may ask for (the merchant's
// stock). A quote asking for more is rejected exactly like the real sandbox does:
// 400 AGENTIC_REQUEST_REJECTED with detail {"errors":[{"field":"items"}]}. max <= 0 removes the limit.
func (s *Server) SetVariantMaxQuantity(variantID string, max int) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.variantByID[variantID]; !ok {
		return fmt.Errorf("fakereap: unknown variant %q", variantID)
	}
	if s.maxQty == nil {
		s.maxQty = map[string]int{}
	}
	if max <= 0 {
		delete(s.maxQty, variantID)
	} else {
		s.maxQty[variantID] = max
	}
	return nil
}

// SetVariantAvailable toggles stock for a variant.
func (s *Server) SetVariantAvailable(variantID string, available bool) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	ref, ok := s.variantByID[variantID]
	if !ok {
		return fmt.Errorf("fakereap: unknown variant %q", variantID)
	}
	ref.p.Variants[ref.i].Available = available
	return nil
}

// ActivateEnrollment simulates the user entering a card on the hosted page.
func (s *Server) ActivateEnrollment(id string) error {
	return s.setEnrollment(id, reap.EnrollmentActive)
}

// FailEnrollment simulates a declined card capture.
func (s *Server) FailEnrollment(id string) error { return s.setEnrollment(id, reap.EnrollmentFailed) }

// RevokeEnrollment simulates the stored card being revoked.
func (s *Server) RevokeEnrollment(id string) error {
	return s.setEnrollment(id, reap.EnrollmentRevoked)
}

func (s *Server) setEnrollment(id string, st reap.EnrollmentStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.enrollments[id]
	if !ok {
		return fmt.Errorf("fakereap: unknown enrollment %q", id)
	}
	s.refreshEnrollment(e)
	if st == reap.EnrollmentRevoked {
		if e.Status != reap.EnrollmentActive {
			return fmt.Errorf("fakereap: enrollment %s is %s, only ACTIVE can be revoked", id, e.Status)
		}
	} else if e.Status != reap.EnrollmentRequiresAction {
		return fmt.Errorf("fakereap: enrollment %s is %s, not REQUIRES_ACTION", id, e.Status)
	}
	e.Status = st
	e.UpdatedAt = s.now()
	return nil
}

// ApproveCheckout simulates the user approving on the hosted page: REQUIRES_ACTION -> PROCESSING.
// The next GET /agentic/checkouts/{id} reports COMPLETED with an orderId and finalAmount.
func (s *Server) ApproveCheckout(id string) error {
	return s.setCheckout(id, reap.CheckoutProcessing)
}

// CompleteCheckout jumps straight to COMPLETED.
func (s *Server) CompleteCheckout(id string) error { return s.setCheckout(id, reap.CheckoutCompleted) }

// FailCheckout simulates a declined charge or merchant failure.
func (s *Server) FailCheckout(id string) error { return s.setCheckout(id, reap.CheckoutFailed) }

// ExpireCheckout simulates the approval window lapsing.
func (s *Server) ExpireCheckout(id string) error { return s.setCheckout(id, reap.CheckoutExpired) }

func (s *Server) setCheckout(id string, st reap.CheckoutStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checkouts[id]
	if !ok {
		return fmt.Errorf("fakereap: unknown checkout %q", id)
	}
	s.refreshCheckout(c)
	if c.Status.Terminal() {
		return fmt.Errorf("fakereap: checkout %s is already %s", id, c.Status)
	}
	if c.Status == reap.CheckoutProcessing && st == reap.CheckoutProcessing {
		return nil
	}
	s.moveCheckout(c, st)
	return nil
}

// LatestCheckoutID returns the most recently created checkout id ("" if none).
func (s *Server) LatestCheckoutID() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var best *checkoutState
	for _, c := range s.checkouts {
		if best == nil || c.CreatedAt.After(best.CreatedAt) || (c.CreatedAt.Equal(best.CreatedAt) && c.ID > best.ID) {
			best = c
		}
	}
	if best == nil {
		return ""
	}
	return best.ID
}

// CheckoutIDs returns all checkout ids (sorted).
func (s *Server) CheckoutIDs() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	ids := make([]string, 0, len(s.checkouts))
	for id := range s.checkouts {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// ---------- HTTP plumbing ----------

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /agentic/products/search", s.api(s.handleSearch))
	mux.HandleFunc("POST /agentic/products/details", s.api(s.handleDetails))
	mux.HandleFunc("POST /agentic/products/variant", s.api(s.handleVariant))
	mux.HandleFunc("POST /agentic/quotes", s.api(s.idempotent(s.handleCreateQuote)))
	mux.HandleFunc("GET /agentic/quotes/{id}", s.api(s.handleGetQuote))
	mux.HandleFunc("POST /agentic/quotes/{id}/shipping-option", s.api(s.handleSelectShipping))
	mux.HandleFunc("POST /agentic/checkouts", s.api(s.idempotent(s.handleCreateCheckout)))
	mux.HandleFunc("GET /agentic/checkouts/{id}", s.api(s.handleGetCheckout))
	mux.HandleFunc("POST /agentic/enrollments", s.api(s.idempotent(s.handleCreateEnrollment)))
	mux.HandleFunc("GET /agentic/enrollments/{id}", s.api(s.handleGetEnrollment))
	mux.HandleFunc("GET /hosted/enrollments/{id}", s.hostedEnrollment)
	mux.HandleFunc("GET /hosted/checkouts/{id}", s.hostedCheckout)
	mux.HandleFunc("/agentic/mandates/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, reap.CodeResourceNotFound, "Mandates are not available yet", nil)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		writeErr(w, http.StatusNotFound, reap.CodeResourceNotFound, "No route for "+r.Method+" "+r.URL.Path, nil)
	})
	return mux
}

// apiHandler runs with s.mu held and the body already read.
type apiHandler func(w http.ResponseWriter, r *http.Request, body []byte)

func (s *Server) api(h apiHandler) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		s.mu.Lock()
		defer s.mu.Unlock()

		authorized := r.Header.Get("Authorization") == "Bearer "+s.opts.APIKey
		s.requests = append(s.requests, RecordedRequest{
			Method: r.Method, Path: r.URL.Path,
			IdempotencyKey:   r.Header.Get("Idempotency-Key"),
			ReapVersion:      r.Header.Get("Reap-Version"),
			SimulateCheckout: r.Header.Get("X-Simulate-Checkout"),
			Authorized:       authorized,
			Body:             body,
		})
		if f := s.takeFault(r.Method, r.URL.Path, FaultBefore, idempotentPath(r.Method, r.URL.Path)); f != nil {
			writeFault(w, f)
			return
		}
		if !authorized {
			writeErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "Missing or invalid API key", nil)
			return
		}
		if r.Header.Get("Reap-Version") != reap.DefaultVersion {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Missing or unsupported Reap-Version header", nil)
			return
		}
		if r.Method == http.MethodPost && len(body) > 0 && !strings.HasPrefix(r.Header.Get("Content-Type"), "application/json") {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Content-Type must be application/json", nil)
			return
		}
		h(w, r, body)
	}
}

// idempotentPath reports whether method+path is wrapped by idempotent (staged faults apply).
func idempotentPath(method, path string) bool {
	return method == http.MethodPost && (path == "/agentic/quotes" || path == "/agentic/checkouts" || path == "/agentic/enrollments")
}

// writeFault writes an injected fault (or drops the connection).
func writeFault(w http.ResponseWriter, f *Fault) {
	if f.Drop {
		if hj, ok := w.(http.Hijacker); ok {
			if conn, _, err := hj.Hijack(); err == nil {
				_ = conn.Close()
				return
			}
		}
		panic(http.ErrAbortHandler) // fallback: net/http aborts the response
	}
	if f.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.Itoa(f.RetryAfter))
	}
	writeErr(w, f.Status, f.Code, "Injected fault", nil)
}

// takeFault consumes a matching fault for stage. On non-idempotent paths every fault fires
// before processing.
func (s *Server) takeFault(method, path string, stage FaultStage, idempotent bool) *Fault {
	for i, f := range s.faults {
		fs := f.Stage
		if !idempotent {
			fs = FaultBefore
		}
		if fs != stage {
			continue
		}
		if f.Method != "" && f.Method != method {
			continue
		}
		if !pathMatches(f.Path, path) {
			continue
		}
		if f.Skip > 0 {
			f.Skip--
			return nil
		}
		f.Times--
		if f.Times <= 0 {
			s.faults = append(s.faults[:i], s.faults[i+1:]...)
		}
		return f
	}
	return nil
}

// idempotent enforces Idempotency-Key: first execution is cached (unless 5xx and not
// Options.CacheServerErrors), a replay with the
// same body returns the cached response, a replay with a different body is rejected.
func (s *Server) idempotent(h apiHandler) apiHandler {
	return func(w http.ResponseWriter, r *http.Request, body []byte) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 255 {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Idempotency-Key header is required (1..255 chars)", nil)
			return
		}
		scope := r.URL.Path + "\x00" + key
		fp := sha256.Sum256(append(append([]byte(nil), body...), []byte("\x00"+r.Header.Get("X-Simulate-Checkout"))...))
		after := s.takeFault(r.Method, r.URL.Path, FaultAfterProcess, true)
		if prev, ok := s.idem[scope]; ok {
			if prev.fingerprint != fp {
				writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Idempotency-Key was reused with a different request body", nil)
				return
			}
			if after != nil {
				writeFault(w, after)
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(prev.status)
			_, _ = w.Write(prev.body)
			return
		}
		rec := httptest.NewRecorder()
		if f := s.takeFault(r.Method, r.URL.Path, FaultInHandler, true); f != nil && !f.Drop {
			writeFault(rec, f)
		} else {
			h(rec, r, body)
		}
		if rec.Code < 500 || s.opts.CacheServerErrors {
			s.idem[scope] = idemEntry{fingerprint: fp, status: rec.Code, body: rec.Body.Bytes()}
		}
		if after != nil {
			writeFault(w, after)
			return
		}
		for k, v := range rec.Header() {
			w.Header()[k] = v
		}
		w.WriteHeader(rec.Code)
		_, _ = w.Write(rec.Body.Bytes())
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeErr(w http.ResponseWriter, status int, code, msg string, detail any) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg, "detail": detail}})
}

func reason(r, msg string) map[string]any { return map[string]any{"reason": r, "message": msg} }

func decode(w http.ResponseWriter, body []byte, v any) bool {
	dec := json.NewDecoder(bytes.NewReader(body))
	if err := dec.Decode(v); err != nil {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Invalid JSON body: "+err.Error(), nil)
		return false
	}
	return true
}

// ---------- products ----------

func money(cents int64) reap.Money { return reap.Money{Amount: float64(cents) / 100, Currency: "SGD"} }
func moneyPtr(cents int64) *reap.Money {
	m := money(cents)
	return &m
}
func boolPtr(b bool) *bool { return &b }

func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.SearchRequest
	if !decode(w, body, &req) {
		return
	}
	qtoks := tokenize(req.Query)
	if strings.TrimSpace(req.Query) == "" || len(qtoks) == 0 {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "query must not be empty", nil)
		return
	}
	var prefDomain string
	only := false
	if mp := req.MerchantPreference; mp != nil {
		if mp.Mode != reap.MerchantPrefer && mp.Mode != reap.MerchantOnly {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "merchantPreference.mode must be PREFER or ONLY", nil)
			return
		}
		d := strings.ToLower(strings.TrimSpace(mp.MerchantName))
		d = strings.TrimPrefix(strings.TrimPrefix(d, "https://"), "http://")
		d = strings.TrimPrefix(strings.TrimSuffix(d, "/"), "www.")
		if !s.domains[d] {
			writeErr(w, http.StatusBadRequest, reap.CodeMerchantNotResolved,
				"Merchant preference could not be resolved. Correct the merchant name or remove the restriction.", nil)
			return
		}
		prefDomain, only = d, mp.Mode == reap.MerchantOnly
	}
	var minC, maxC int64 = -1, -1
	if f := req.Filters; f != nil {
		if f.Availability != "" && f.Availability != "AVAILABLE_ONLY" {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "filters.availability must be AVAILABLE_ONLY", nil)
			return
		}
		if f.Price != nil {
			var err error
			if minC, err = parseDecimal(f.Price.Min); err != nil {
				writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "filters.price.min must be a decimal string", nil)
				return
			}
			if maxC, err = parseDecimal(f.Price.Max); err != nil {
				writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "filters.price.max must be a decimal string", nil)
				return
			}
		}
	}
	limit, offset := 10, 0
	if pg := req.Pagination; pg != nil {
		if pg.Limit != 0 {
			if pg.Limit < 1 || pg.Limit > 50 {
				writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "pagination.limit must be 1..50", nil)
				return
			}
			limit = pg.Limit
		}
		if pg.Cursor != nil && *pg.Cursor != "" {
			n, err := strconv.Atoi(strings.TrimPrefix(*pg.Cursor, "cur_"))
			if err != nil || n < 0 || !strings.HasPrefix(*pg.Cursor, "cur_") {
				writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "invalid pagination cursor", nil)
				return
			}
			offset = n
		}
	}

	type hit struct {
		p     *FixtureProduct
		score float64
		pref  bool
	}
	var hits []hit
	for _, p := range s.products {
		if only && !strings.EqualFold(p.Domain, prefDomain) {
			continue
		}
		score := matchScore(qtoks, p)
		if score < 0.5 {
			continue
		}
		lo, hi := priceRange(p)
		if req.Filters != nil {
			if req.Filters.Availability == "AVAILABLE_ONLY" && !productAvailable(p) {
				continue
			}
			if minC >= 0 && hi < minC {
				continue
			}
			if maxC >= 0 && lo > maxC {
				continue
			}
		}
		hits = append(hits, hit{p, score, strings.EqualFold(p.Domain, prefDomain)})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].pref != hits[j].pref {
			return hits[i].pref
		}
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		return hits[i].p.Name < hits[j].p.Name
	})

	resp := reap.SearchResponse{ID: "srch_" + newUUID(), Products: []reap.SearchProduct{}, Warnings: []string{}}
	if c := req.Context; c != nil && c.Currency != "" && !strings.EqualFold(c.Currency, "SGD") {
		resp.Warnings = append(resp.Warnings, "Requested currency "+c.Currency+" is not available; prices are in SGD")
	}
	end := offset + limit
	if offset > len(hits) {
		offset = len(hits)
	}
	if end > len(hits) {
		end = len(hits)
	}
	for _, h := range hits[offset:end] {
		resp.Products = append(resp.Products, searchProduct(h.p))
	}
	resp.Pagination.ReturnedCount = len(resp.Products)
	if end < len(hits) {
		next := "cur_" + strconv.Itoa(end)
		resp.Pagination.NextCursor = &next
		resp.Pagination.HasNextPage = true
	}
	writeJSON(w, http.StatusOK, resp)
}

func searchProduct(p *FixtureProduct) reap.SearchProduct {
	lo, hi := priceRange(p)
	v := p.Variants[0]
	return reap.SearchProduct{
		ID: p.ID, Merchant: reap.Merchant{Name: p.Merchant}, Name: p.Name,
		ImageURL:   imageURL(p.ID),
		PriceRange: reap.PriceRange{Min: money(lo), Max: money(hi)},
		Available:  boolPtr(productAvailable(p)),
		PreviewVariant: &reap.PreviewVariant{
			ID: v.ID, Name: v.Name, Price: money(v.PriceCents), Available: boolPtr(v.Available),
		},
	}
}

func imageURL(id string) string { return "https://cdn.fakereap.test/img/" + id + ".jpg" }

func priceRange(p *FixtureProduct) (lo, hi int64) {
	lo, hi = p.Variants[0].PriceCents, p.Variants[0].PriceCents
	for _, v := range p.Variants[1:] {
		lo, hi = min(lo, v.PriceCents), max(hi, v.PriceCents)
	}
	return lo, hi
}

func productAvailable(p *FixtureProduct) bool {
	for _, v := range p.Variants {
		if v.Available {
			return true
		}
	}
	return false
}

func optionID(p *FixtureProduct, name, label string) string {
	return "opt_" + strings.TrimPrefix(p.ID, "prd_") + "_" + slug(name) + "_" + slug(label)
}

func variantOut(p *FixtureProduct, v FixtureVariant) reap.Variant {
	out := reap.Variant{
		ID: v.ID, Name: v.Name, Options: []reap.SelectedOption{}, Price: money(v.PriceCents),
		Available: boolPtr(v.Available), RequiresShipping: boolPtr(v.RequiresShipping),
		Media: []reap.Media{{Type: "IMAGE", URL: imageURL(v.ID), AltText: p.Name}},
	}
	for _, n := range p.OptionNames {
		out.Options = append(out.Options, reap.SelectedOption{Name: n, Value: v.Options[n]})
	}
	return out
}

func (s *Server) handleDetails(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.DetailsRequest
	if !decode(w, body, &req) {
		return
	}
	if len(req.ProductIDs) < 1 || len(req.ProductIDs) > 10 {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "productIds must contain 1..10 ids", nil)
		return
	}
	resp := reap.DetailsResponse{Products: []reap.ProductDetails{}, Errors: []reap.DetailsError{}}
	for _, id := range req.ProductIDs {
		p, ok := s.productByID[id]
		if !ok {
			resp.Errors = append(resp.Errors, reap.DetailsError{ProductID: id, Code: "PRODUCT_NOT_FOUND", Message: "Product could not be resolved"})
			continue
		}
		d := reap.ProductDetails{
			ID: p.ID, Merchant: reap.Merchant{Name: p.Merchant}, Name: p.Name, Description: p.Description,
			Media:          []reap.Media{{Type: "IMAGE", URL: imageURL(p.ID), AltText: p.Name}},
			Options:        []reap.ProductOption{},
			DefaultVariant: variantOut(p, p.Variants[0]),
		}
		for _, n := range p.OptionNames {
			po := reap.ProductOption{Name: n}
			seen := map[string]int{}
			for _, v := range p.Variants {
				l := v.Options[n]
				if idx, ok := seen[l]; ok {
					if v.Available {
						po.Values[idx].Available = boolPtr(true)
					}
					continue
				}
				seen[l] = len(po.Values)
				po.Values = append(po.Values, reap.OptionValue{OptionID: optionID(p, n, l), Label: l, Available: boolPtr(v.Available)})
			}
			d.Options = append(d.Options, po)
		}
		resp.Products = append(resp.Products, d)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleVariant(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.VariantRequest
	if !decode(w, body, &req) {
		return
	}
	p, ok := s.productByID[req.ProductID]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeResourceNotFound, "Product not found", nil)
		return
	}
	if len(p.OptionNames) == 0 {
		if len(req.OptionIDs) != 0 {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Product has no options", nil)
			return
		}
		writeJSON(w, http.StatusOK, variantOut(p, p.Variants[0]))
		return
	}
	want := map[string]string{} // option name -> label
	for _, oid := range req.OptionIDs {
		found := false
		for _, n := range p.OptionNames {
			for _, v := range p.Variants {
				if optionID(p, n, v.Options[n]) == oid {
					if prev, dup := want[n]; dup && prev != v.Options[n] {
						writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "More than one value selected for option "+n, nil)
						return
					}
					want[n], found = v.Options[n], true
				}
			}
		}
		if !found {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Unknown option id "+oid, nil)
			return
		}
	}
	if len(want) != len(p.OptionNames) {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Select exactly one value for every option", nil)
		return
	}
	for _, v := range p.Variants {
		match := true
		for n, l := range want {
			if v.Options[n] != l {
				match = false
				break
			}
		}
		if match {
			writeJSON(w, http.StatusOK, variantOut(p, v))
			return
		}
	}
	writeErr(w, http.StatusConflict, reap.CodeVariantUnavailable, "No variant exists for the selected options", nil)
}

// ---------- quotes ----------

var phoneRE = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)

func (s *Server) handleCreateQuote(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.CreateQuoteRequest
	if !decode(w, body, &req) {
		return
	}
	if req.ExternalCheckout != nil && len(req.Items) > 0 {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Send exactly one of items and externalCheckout", nil)
		return
	}
	if req.ExternalCheckout != nil {
		writeErr(w, http.StatusBadRequest, reap.CodeCheckoutURLInvalid, "External checkout URLs are not allowlisted for this integration", nil)
		return
	}
	if len(req.Items) < 1 || len(req.Items) > 20 {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "items must contain 1..20 entries", nil)
		return
	}
	if !strings.Contains(req.Email, "@") {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "email is required", nil)
		return
	}
	q := &quoteState{ID: newUUID(), ExpiresAt: s.now().Add(s.opts.QuoteTTL)}
	needsShipping := false
	for _, it := range req.Items {
		ref, ok := s.variantByID[it.VariantID]
		if !ok {
			writeErr(w, http.StatusConflict, reap.CodeVariantUnavailable, "Variant "+it.VariantID+" is unavailable", nil)
			return
		}
		v := ref.p.Variants[ref.i]
		if !v.Available {
			writeErr(w, http.StatusConflict, reap.CodeVariantUnavailable, "Variant "+it.VariantID+" is unavailable", nil)
			return
		}
		if it.Quantity < 1 {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "quantity must be at least 1", nil)
			return
		}
		if max := s.maxQty[it.VariantID]; max > 0 && it.Quantity > max {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "The request was rejected",
				map[string]any{"errors": []any{map[string]any{"field": "items"}}})
			return
		}
		if q.Domain == "" {
			q.Domain, q.Merchant = ref.p.Domain, ref.p.Merchant
		} else if q.Domain != ref.p.Domain {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "All items in a quote must come from one merchant", nil)
			return
		}
		needsShipping = needsShipping || v.RequiresShipping
		q.Items = append(q.Items, quoteItem{VariantID: v.ID, Quantity: it.Quantity, UnitCents: v.PriceCents})
	}
	if a := req.ShippingAddress; a != nil {
		if a.FirstName == "" || a.LastName == "" || a.AddressLine1 == "" || a.City == "" || a.Country == "" {
			writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "shippingAddress requires firstName, lastName, addressLine1, city and country", nil)
			return
		}
		if !phoneRE.MatchString(a.Phone) {
			writeErr(w, http.StatusBadRequest, reap.CodeQuoteUnfulfillable, "The merchant cannot fulfil this quote",
				reason(reap.ReasonInvalidPhone, "Phone is invalid"))
			return
		}
		if !strings.EqualFold(a.Country, "SG") {
			writeErr(w, http.StatusBadRequest, reap.CodeQuoteUnfulfillable, "The merchant cannot fulfil this quote",
				reason(reap.ReasonItemsUnshippable, "Your cart has been updated and the items you added can't be shipped to your address. Remove the items to complete your order."))
			return
		}
	} else if needsShipping {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "shippingAddress is required when any item requires shipping", nil)
		return
	}
	switch strings.ToUpper(strings.TrimSpace(req.OfferCode)) {
	case "":
	case OfferCodeValid:
		q.OfferCode, q.DiscountPct = OfferCodeValid, 10
	case OfferCodeExpired:
		writeErr(w, http.StatusBadRequest, reap.CodeOfferCodeExpired, "The offer code has expired", nil)
		return
	default:
		writeErr(w, http.StatusBadRequest, reap.CodeOfferCodeInvalid, "The offer code is not valid", nil)
		return
	}
	q.Selected = q.ID + ShippingStandardSuffix
	s.quotes[q.ID] = q
	writeJSON(w, http.StatusOK, s.quoteOut(q))
}

func (s *Server) quoteOut(q *quoteState) reap.Quote {
	var subtotal int64
	for _, it := range q.Items {
		subtotal += it.UnitCents * int64(it.Quantity)
	}
	discount := subtotal * q.DiscountPct / 100
	std := int64(StandardShippingCents)
	if subtotal >= FreeShippingOverCents {
		std = 0
	}
	opts := []reap.ShippingOption{
		{ID: q.ID + ShippingStandardSuffix, Name: "Standard Delivery", Price: money(std),
			Details: []reap.KeyValue{{Key: "estimatedDelivery", Value: "3-5 working days"}}},
		{ID: q.ID + ShippingExpressSuffix, Name: "Express Delivery", Price: money(ExpressShippingCents),
			Details: []reap.KeyValue{{Key: "estimatedDelivery", Value: "Next working day"}}},
	}
	var ship int64
	for i := range opts {
		if opts[i].ID == q.Selected {
			opts[i].Selected = true
			ship = int64(opts[i].Price.Amount*100 + 0.5)
		}
	}
	final := subtotal - discount + ship
	gst := (final*gstPercent + 54) / (100 + gstPercent) // GST-inclusive portion, rounded
	b := reap.AmountBreakdown{
		ItemsSubtotal:     money(subtotal),
		Shipping:          moneyPtr(ship),
		Tax:               &reap.Tax{Amount: money(gst), IncludedInPrices: true},
		Discounts:         []reap.NamedAmount{},
		AdditionalCharges: []reap.NamedAmount{},
		FinalAmount:       money(final),
	}
	if discount > 0 {
		b.Discounts = append(b.Discounts, reap.NamedAmount{Name: q.OfferCode + " (10% off)", Amount: money(discount)})
	}
	return reap.Quote{ID: q.ID, ShippingOptions: opts, AmountBreakdown: b, ExpiresAt: q.ExpiresAt}
}

func (s *Server) quoteFinalCents(q *quoteState) int64 {
	return int64(s.quoteOut(q).AmountBreakdown.FinalAmount.Amount*100 + 0.5)
}

func (s *Server) handleGetQuote(w http.ResponseWriter, r *http.Request, _ []byte) {
	q, ok := s.quotes[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeQuoteNotFound, "Quote not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.quoteOut(q))
}

func (s *Server) handleSelectShipping(w http.ResponseWriter, r *http.Request, body []byte) {
	q, ok := s.quotes[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeQuoteNotFound, "Quote not found", nil)
		return
	}
	var req reap.SelectShippingRequest
	if !decode(w, body, &req) {
		return
	}
	if !s.now().Before(q.ExpiresAt) {
		writeErr(w, http.StatusConflict, reap.CodeQuoteExpired, "Quote has expired; create a new quote", nil)
		return
	}
	if q.Consumed {
		writeErr(w, http.StatusConflict, reap.CodeQuoteNotMutable, "Quote already has a checkout and cannot change", nil)
		return
	}
	if req.ShippingOptionID != q.ID+ShippingStandardSuffix && req.ShippingOptionID != q.ID+ShippingExpressSuffix {
		writeErr(w, http.StatusBadRequest, reap.CodeShippingOptionInvalid, "Shipping option is not valid for this quote", nil)
		return
	}
	q.Selected = req.ShippingOptionID
	writeJSON(w, http.StatusOK, s.quoteOut(q))
}

func pathMatches(pattern, path string) bool {
	if strings.HasSuffix(pattern, "*") {
		return strings.HasPrefix(path, strings.TrimSuffix(pattern, "*"))
	}
	return pattern == "" || pattern == path
}

// SetCheckoutAmount changes the amount a checkout will report as finalAmount (simulate Reap
// charging more than it quoted).
func (s *Server) SetCheckoutAmount(id string, cents int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.checkouts[id]
	if !ok {
		return fmt.Errorf("fakereap: unknown checkout %q", id)
	}
	c.AmountCents = cents
	return nil
}

// ExpireQuote forces a quote past its expiry.
func (s *Server) ExpireQuote(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	q, ok := s.quotes[id]
	if !ok {
		return fmt.Errorf("fakereap: unknown quote %q", id)
	}
	q.ExpiresAt = s.now().Add(-time.Second)
	return nil
}

// ---------- enrollments ----------

func (s *Server) validReturnURL(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	return u.Scheme == "https" || (s.opts.AllowHTTPReturnURL && u.Scheme == "http")
}

func (s *Server) handleCreateEnrollment(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.CreateEnrollmentRequest
	if !decode(w, body, &req) {
		return
	}
	switch req.Source {
	case "EXTERNAL":
	case "REAP_CARD", "BIN_SPONSOR":
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "Enrollment source "+req.Source+" is coming soon", nil)
		return
	default:
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "source must be EXTERNAL", nil)
		return
	}
	if req.Owner.Type != "CLIENT_REFERENCE" || req.Owner.ID == "" || !strings.Contains(req.Owner.Email, "@") {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "owner must be {type: CLIENT_REFERENCE, id, email}", nil)
		return
	}
	if req.Presentation.Type != "REDIRECT" || !s.validReturnURL(req.Presentation.ReturnURL) {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "presentation must be {type: REDIRECT, returnUrl: https URL}", nil)
		return
	}
	now := s.now()
	e := &enrollmentState{
		ID: newUUID(), Status: reap.EnrollmentRequiresAction,
		Owner:     reap.Owner{Type: "CLIENT_REFERENCE", ID: req.Owner.ID, Email: req.Owner.Email},
		ReturnURL: req.Presentation.ReturnURL, ActionExpiresAt: now.Add(s.opts.ActionTTL),
		CreatedAt: now, UpdatedAt: now,
	}
	e.ActionURL = s.URL + "/hosted/enrollments/" + e.ID
	if s.opts.AutoActivateEnrollments {
		e.Status = reap.EnrollmentActive
	}
	s.enrollments[e.ID] = e
	out := s.enrollmentOut(e)
	out.PaymentMethod, out.CreatedAt, out.UpdatedAt = nil, nil, nil // create response has none of these
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) refreshEnrollment(e *enrollmentState) {
	if e.Status == reap.EnrollmentRequiresAction && !s.now().Before(e.ActionExpiresAt) {
		e.Status, e.UpdatedAt = reap.EnrollmentExpired, s.now()
	}
}

func (s *Server) enrollmentOut(e *enrollmentState) reap.Enrollment {
	s.refreshEnrollment(e)
	created, updated := e.CreatedAt, e.UpdatedAt
	out := reap.Enrollment{
		ID: e.ID, Status: e.Status, Source: "EXTERNAL", Owner: e.Owner,
		CreatedAt: &created, UpdatedAt: &updated,
	}
	if e.Status == reap.EnrollmentRequiresAction {
		exp := e.ActionExpiresAt
		out.NextAction = &reap.NextAction{Type: "REDIRECT", URL: e.ActionURL, ExpiresAt: &exp}
	}
	if e.Status == reap.EnrollmentActive || e.Status == reap.EnrollmentRevoked {
		out.PaymentMethod = &reap.PaymentMethod{Type: "CARD", Network: "VISA", Last4: "4242", ExpiryMonth: 12, ExpiryYear: 2030}
	}
	return out
}

func (s *Server) handleGetEnrollment(w http.ResponseWriter, r *http.Request, _ []byte) {
	e, ok := s.enrollments[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeEnrollmentNotFound, "Enrollment not found", nil)
		return
	}
	writeJSON(w, http.StatusOK, s.enrollmentOut(e))
}

// ---------- checkouts ----------

func (s *Server) handleCreateCheckout(w http.ResponseWriter, r *http.Request, body []byte) {
	var req reap.CreateCheckoutRequest
	if !decode(w, body, &req) {
		return
	}
	if req.QuoteID == "" || req.EnrollmentID == "" {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "quoteId and enrollmentId are required", nil)
		return
	}
	if req.Presentation.Type != "REDIRECT" || !s.validReturnURL(req.Presentation.ReturnURL) {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "presentation must be {type: REDIRECT, returnUrl: https URL}", nil)
		return
	}
	sim := r.Header.Get("X-Simulate-Checkout")
	if sim != "" && sim != "COMPLETED" {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "X-Simulate-Checkout must be COMPLETED", nil)
		return
	}
	e, ok := s.enrollments[req.EnrollmentID]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeEnrollmentNotFound, "Enrollment not found", nil)
		return
	}
	q, ok := s.quotes[req.QuoteID]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeQuoteNotFound, "Quote not found", nil)
		return
	}
	s.refreshEnrollment(e)
	if e.Status != reap.EnrollmentActive {
		var detail any
		if e.Status == reap.EnrollmentRequiresAction {
			detail = map[string]any{"reason": reap.ReasonCardNotCaptured}
		}
		writeErr(w, http.StatusConflict, reap.CodeEnrollmentNotActive, "Enrollment is not active.", detail)
		return
	}
	if !s.now().Before(q.ExpiresAt) {
		writeErr(w, http.StatusConflict, reap.CodeQuoteExpired, "Quote has expired; create a new quote", nil)
		return
	}
	if q.Consumed {
		writeErr(w, http.StatusBadRequest, reap.CodeRequestRejected, "A checkout already exists for this quote", nil)
		return
	}
	q.Consumed = true
	now := s.now()
	c := &checkoutState{
		ID: newUUID(), QuoteID: q.ID, EnrollmentID: e.ID, Status: reap.CheckoutRequiresAction,
		AmountCents: s.quoteFinalCents(q), ReturnURL: req.Presentation.ReturnURL,
		ActionExpiresAt: now.Add(s.opts.ActionTTL), Simulated: sim == "COMPLETED" || s.opts.AutoApproveCheckouts,
		CreatedAt: now, UpdatedAt: now,
	}
	c.ActionURL = s.URL + "/hosted/checkouts/" + c.ID
	s.checkouts[c.ID] = c
	exp := c.ActionExpiresAt
	writeJSON(w, http.StatusOK, reap.Checkout{
		ID: c.ID, Status: c.Status, QuoteID: c.QuoteID, EnrollmentID: c.EnrollmentID,
		Amount:     moneyPtr(c.AmountCents),
		NextAction: &reap.NextAction{Type: "REDIRECT", URL: c.ActionURL, ExpiresAt: &exp},
	})
}

func (s *Server) moveCheckout(c *checkoutState, st reap.CheckoutStatus) {
	c.Status, c.UpdatedAt = st, s.now()
	if st == reap.CheckoutCompleted && c.OrderID == "" {
		s.orderSeq++
		c.OrderID = fmt.Sprintf("#SG%05d", 1000+s.orderSeq)
	}
}

// refreshCheckout applies time- and poll-driven transitions.
func (s *Server) refreshCheckout(c *checkoutState) {
	if c.Status == reap.CheckoutRequiresAction && !s.now().Before(c.ActionExpiresAt) && !c.Simulated {
		s.moveCheckout(c, reap.CheckoutExpired)
	}
}

func (s *Server) handleGetCheckout(w http.ResponseWriter, r *http.Request, _ []byte) {
	c, ok := s.checkouts[r.PathValue("id")]
	if !ok {
		writeErr(w, http.StatusNotFound, reap.CodeCheckoutNotFound, "Checkout not found", nil)
		return
	}
	s.refreshCheckout(c)
	switch {
	case c.Status == reap.CheckoutRequiresAction && c.Simulated, c.Status == reap.CheckoutProcessing:
		s.moveCheckout(c, reap.CheckoutCompleted)
	}
	created, updated := c.CreatedAt, c.UpdatedAt
	out := reap.Checkout{
		ID: c.ID, Status: c.Status, QuoteID: c.QuoteID, EnrollmentID: c.EnrollmentID,
		CreatedAt: &created, UpdatedAt: &updated,
	}
	if c.Status == reap.CheckoutRequiresAction {
		exp := c.ActionExpiresAt
		out.NextAction = &reap.NextAction{Type: "REDIRECT", URL: c.ActionURL, ExpiresAt: &exp}
	}
	if c.Status == reap.CheckoutCompleted {
		oid := c.OrderID
		out.OrderID = &oid
		out.FinalAmount = moneyPtr(c.AmountCents)
	}
	writeJSON(w, http.StatusOK, out)
}

// ---------- hosted pages ----------

func (s *Server) hostedEnrollment(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	e, ok := s.enrollments[r.PathValue("id")]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "enrollment not found", http.StatusNotFound)
		return
	}
	s.refreshEnrollment(e)
	if e.Status == reap.EnrollmentRequiresAction {
		if r.URL.Query().Get("decision") == "decline" {
			e.Status = reap.EnrollmentFailed
		} else {
			e.Status = reap.EnrollmentActive
		}
		e.UpdatedAt = s.now()
	}
	ret, status := e.ReturnURL, e.Status
	s.mu.Unlock()
	redirectBack(w, r, ret, "enrollment_id", e.ID, string(status))
}

func (s *Server) hostedCheckout(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	c, ok := s.checkouts[r.PathValue("id")]
	if !ok {
		s.mu.Unlock()
		http.Error(w, "checkout not found", http.StatusNotFound)
		return
	}
	s.refreshCheckout(c)
	if c.Status == reap.CheckoutRequiresAction {
		if r.URL.Query().Get("decision") == "decline" {
			s.moveCheckout(c, reap.CheckoutFailed)
		} else {
			s.moveCheckout(c, reap.CheckoutProcessing)
		}
	}
	ret, status := c.ReturnURL, c.Status
	s.mu.Unlock()
	redirectBack(w, r, ret, "checkout_id", c.ID, string(status))
}

func redirectBack(w http.ResponseWriter, r *http.Request, ret, param, id, status string) {
	u, err := url.Parse(ret)
	if err != nil || ret == "" {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		fmt.Fprintf(w, "Fake Reap: %s %s is now %s. You can close this tab.\n", param, id, status)
		return
	}
	q := u.Query()
	q.Set(param, id)
	u.RawQuery = q.Encode()
	http.Redirect(w, r, u.String(), http.StatusSeeOther)
}

// ---------- helpers ----------

func newUUID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

func slug(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ' || r == '-' || r == '.' || r == '/':
			b.WriteByte('_')
		}
	}
	return b.String()
}

var stopwords = map[string]bool{"of": true, "the": true, "and": true, "for": true, "to": true, "a": true, "with": true, "x": true}

// tokenize lowercases, splits on anything but [a-z0-9.+], trims dots and drops a plural "s".
func tokenize(s string) []string {
	f := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '+')
	})
	out := f[:0]
	for _, t := range f {
		t = strings.Trim(t, ".+")
		if t == "" || stopwords[t] {
			continue
		}
		if len(t) > 3 && strings.HasSuffix(t, "s") && !strings.HasSuffix(t, "ss") {
			t = strings.TrimSuffix(t, "s")
		}
		out = append(out, t)
	}
	return out
}

// matchScore is the fraction of query tokens found in the product name or variant labels.
func matchScore(q []string, p *FixtureProduct) float64 {
	have := map[string]bool{}
	for _, t := range tokenize(p.Name) {
		have[t] = true
	}
	for _, v := range p.Variants {
		for _, l := range v.Options {
			for _, t := range tokenize(l) {
				have[t] = true
			}
		}
	}
	n := 0
	for _, t := range q {
		if have[t] {
			n++
		}
	}
	return float64(n) / float64(len(q))
}

// parseDecimal parses "12.5" into cents; "" means no bound (-1).
func parseDecimal(s string) (int64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return -1, nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || f < 0 {
		return 0, fmt.Errorf("bad decimal %q", s)
	}
	return int64(f*100 + 0.5), nil
}
