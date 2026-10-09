package harness

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ReapVersion is the only Reap-Version the fake accepts.
const ReapVersion = "2025-02-14"

// Product is one purchasable product in the fake Reap catalog (one default variant).
type Product struct {
	ID           string
	VariantID    string
	Domain       string // merchant domain (what merchantPreference.merchantName carries)
	MerchantName string // what search returns in merchant.name
	Name         string
	PriceSGD     float64
	// Match: the product is returned when the lower-cased search query contains any of these.
	Match []string
	// Leaky products are returned even when the search is restricted to another merchant with
	// mode ONLY (models sandbox noise), so the api's allow-list filter is exercised.
	Leaky bool
}

// ReapCall is one request the fake received (for assertions).
type ReapCall struct {
	Method         string
	Path           string
	IdempotencyKey string
	Simulate       string
	Body           json.RawMessage
}

// FakeReap is an httptest server implementing the Reap agentic endpoints the api uses
// (search, details, variant, quotes, shipping option, checkouts, enrollments) with in-memory state.
// Hosted pages are simulated by the control methods (ActivateEnrollment, ApproveCheckout, ...) and
// by GET /hosted/... links so a human can click through in an offline demo.
type FakeReap struct {
	Server *httptest.Server
	apiKey string

	mu          sync.Mutex
	products    []Product
	byVariant   map[string]Product
	multiplier  map[string]float64 // domain -> quote price multiplier (price drift simulation)
	shipping    map[string]float64 // domain -> shipping fee SGD
	quotes      map[string]*fakeQuote
	checkouts   map[string]*fakeCheckout
	enrollments map[string]*fakeEnrollment
	idem        map[string][]byte // method+path+key -> cached response body
	calls       []ReapCall
	orderSeq    int
}

type fakeQuote struct {
	ID       string
	Domain   string
	Items    []map[string]any
	Address  map[string]any
	Email    string
	Subtotal float64
	Shipping float64
	Created  time.Time
}

type fakeCheckout struct {
	ID           string
	QuoteID      string
	EnrollmentID string
	Status       string
	Amount       float64
	OrderID      string
}

type fakeEnrollment struct {
	ID     string
	Status string
	Owner  map[string]any
}

// NewFakeReap starts the fake. apiKey is the Bearer token it requires.
func NewFakeReap(apiKey string, products []Product) *FakeReap {
	f := &FakeReap{
		apiKey: apiKey, byVariant: map[string]Product{}, multiplier: map[string]float64{},
		shipping: map[string]float64{}, quotes: map[string]*fakeQuote{}, checkouts: map[string]*fakeCheckout{},
		enrollments: map[string]*fakeEnrollment{}, idem: map[string][]byte{},
	}
	f.SetProducts(products)
	f.Server = httptest.NewServer(http.HandlerFunc(f.serve))
	return f
}

// URL is the base URL to give the api as REAP_BASE_URL.
func (f *FakeReap) URL() string { return f.Server.URL }

// Close stops the server.
func (f *FakeReap) Close() { f.Server.Close() }

// SetProducts replaces the catalog. Missing IDs are generated.
func (f *FakeReap) SetProducts(ps []Product) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.products = nil
	f.byVariant = map[string]Product{}
	for _, p := range ps {
		if p.ID == "" {
			p.ID = NewUUID()
		}
		if p.VariantID == "" {
			p.VariantID = NewUUID()
		}
		f.products = append(f.products, p)
		f.byVariant[p.VariantID] = p
	}
}

// SetQuoteMultiplier makes quotes for a merchant domain cost m times the search price (1 = none).
func (f *FakeReap) SetQuoteMultiplier(domain string, m float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if m == 1 {
		delete(f.multiplier, domain)
		return
	}
	f.multiplier[domain] = m
}

// SetShipping sets a shipping fee for a merchant domain (default free).
func (f *FakeReap) SetShipping(domain string, sgd float64) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shipping[domain] = sgd
}

// Calls returns a copy of every request received.
func (f *FakeReap) Calls() []ReapCall {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]ReapCall(nil), f.calls...)
}

// ActivateEnrollment simulates the user entering a card on the hosted page ("" = all pending).
func (f *FakeReap) ActivateEnrollment(id string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, e := range f.enrollments {
		if (id == "" || e.ID == id) && e.Status == "REQUIRES_ACTION" {
			e.Status = "ACTIVE"
			n++
		}
	}
	return n
}

// ApproveCheckout simulates the user approving payment on the hosted page; the checkout
// completes with a merchant order id. Returns the number of checkouts approved ("" = all pending).
func (f *FakeReap) ApproveCheckout(id string) int { return f.decide(id, "COMPLETED") }

// DeclineCheckout makes a pending checkout FAILED.
func (f *FakeReap) DeclineCheckout(id string) int { return f.decide(id, "FAILED") }

func (f *FakeReap) decide(id, status string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.checkouts {
		if (id == "" || c.ID == id) && c.Status == "REQUIRES_ACTION" {
			f.completeLocked(c, status)
			n++
		}
	}
	return n
}

func (f *FakeReap) completeLocked(c *fakeCheckout, status string) {
	c.Status = status
	if status == "COMPLETED" {
		f.orderSeq++
		c.OrderID = "ORD-E2E-" + strconv.Itoa(1000+f.orderSeq)
	}
}

// PendingCheckouts returns ids of checkouts waiting for the hosted approval.
func (f *FakeReap) PendingCheckouts() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.checkouts {
		if c.Status == "REQUIRES_ACTION" {
			out = append(out, c.ID)
		}
	}
	sort.Strings(out)
	return out
}

// Quote returns a snapshot of a quote's shipping address, email and merchant domain.
func (f *FakeReap) Quote(id string) (domain, email string, address map[string]any, ok bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.quotes[id]
	if !ok {
		return "", "", nil, false
	}
	return q.Domain, q.Email, q.Address, true
}

// ---------------------------------------------------------------------------------------------

var (
	reCheckout   = regexp.MustCompile(`^/agentic/checkouts/([^/]+)$`)
	reQuote      = regexp.MustCompile(`^/agentic/quotes/([^/]+)$`)
	reQuoteShip  = regexp.MustCompile(`^/agentic/quotes/([^/]+)/shipping-option$`)
	reEnrollment = regexp.MustCompile(`^/agentic/enrollments/([^/]+)$`)
	reHosted     = regexp.MustCompile(`^/hosted/(checkouts|enrollments)/([^/]+)(/approve|/decline)?$`)
	rePhone      = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
)

func (f *FakeReap) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	path := r.URL.Path

	if m := reHosted.FindStringSubmatch(path); m != nil {
		f.hosted(w, m[1], m[2], m[3])
		return
	}

	f.mu.Lock()
	f.calls = append(f.calls, ReapCall{Method: r.Method, Path: path, IdempotencyKey: r.Header.Get("Idempotency-Key"),
		Simulate: r.Header.Get("X-Simulate-Checkout"), Body: append(json.RawMessage(nil), body...)})
	f.mu.Unlock()

	if bearer(r) != f.apiKey || f.apiKey == "" {
		reapErr(w, http.StatusUnauthorized, "UNAUTHORIZED", "missing or invalid bearer token")
		return
	}
	if r.Header.Get("Reap-Version") != ReapVersion {
		reapErr(w, http.StatusBadRequest, "AGENTIC_REQUEST_REJECTED", "Reap-Version header must be "+ReapVersion)
		return
	}

	needsIdem := r.Method == http.MethodPost && (path == "/agentic/quotes" || path == "/agentic/checkouts" || path == "/agentic/enrollments")
	key := r.Header.Get("Idempotency-Key")
	if needsIdem {
		if key == "" {
			reapErr(w, http.StatusBadRequest, "AGENTIC_REQUEST_REJECTED", "Idempotency-Key header is required")
			return
		}
		f.mu.Lock()
		cached, hit := f.idem[path+"|"+key]
		f.mu.Unlock()
		if hit {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(cached)
			return
		}
	}

	var (
		status = http.StatusOK
		resp   any
		code   string
		msg    string
	)
	switch {
	case r.Method == http.MethodPost && path == "/agentic/products/search":
		resp, code, msg = f.search(body)
	case r.Method == http.MethodPost && path == "/agentic/products/details":
		resp, code, msg = f.details(body)
	case r.Method == http.MethodPost && path == "/agentic/products/variant":
		resp, code, msg = f.variant(body)
	case r.Method == http.MethodPost && path == "/agentic/quotes":
		resp, code, msg = f.createQuote(body)
	case r.Method == http.MethodGet && reQuote.MatchString(path):
		resp, code, msg = f.getQuote(reQuote.FindStringSubmatch(path)[1])
	case r.Method == http.MethodPost && reQuoteShip.MatchString(path):
		resp, code, msg = f.getQuote(reQuoteShip.FindStringSubmatch(path)[1])
	case r.Method == http.MethodPost && path == "/agentic/checkouts":
		resp, code, msg = f.createCheckout(body, r.Header.Get("X-Simulate-Checkout"))
	case r.Method == http.MethodGet && reCheckout.MatchString(path):
		resp, code, msg = f.getCheckout(reCheckout.FindStringSubmatch(path)[1])
	case r.Method == http.MethodPost && path == "/agentic/enrollments":
		resp, code, msg = f.createEnrollment(body)
	case r.Method == http.MethodGet && reEnrollment.MatchString(path):
		resp, code, msg = f.getEnrollment(reEnrollment.FindStringSubmatch(path)[1])
	case strings.HasPrefix(path, "/agentic/mandates"):
		code, msg = "AGENTIC_PAYMENTS_NOT_ENABLED", "mandates are not live"
	default:
		reapErr(w, http.StatusNotFound, "AGENTIC_RESOURCE_NOT_FOUND", "no route "+r.Method+" "+path)
		return
	}
	if code != "" {
		switch {
		case strings.HasSuffix(code, "_NOT_FOUND"):
			status = http.StatusNotFound
		default:
			status = http.StatusBadRequest
		}
		reapErr(w, status, code, msg)
		return
	}
	b, _ := json.Marshal(resp)
	if needsIdem {
		f.mu.Lock()
		f.idem[path+"|"+key] = b
		f.mu.Unlock()
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(b)
}

func reapErr(w http.ResponseWriter, status int, code, msg string) {
	writeJSON(w, status, map[string]any{"error": map[string]any{"code": code, "message": msg}})
}

func money(a float64) map[string]any { return map[string]any{"amount": round2(a), "currency": "SGD"} }

func (f *FakeReap) search(body []byte) (any, string, string) {
	var req struct {
		Query              string `json:"query"`
		MerchantPreference *struct {
			Mode         string `json:"mode"`
			MerchantName string `json:"merchantName"`
		} `json:"merchantPreference"`
		Pagination *struct {
			Limit int `json:"limit"`
		} `json:"pagination"`
	}
	if err := json.Unmarshal(body, &req); err != nil || strings.TrimSpace(req.Query) == "" {
		return nil, "AGENTIC_REQUEST_REJECTED", "query is required"
	}
	q := strings.ToLower(req.Query)
	limit := 20
	if req.Pagination != nil && req.Pagination.Limit > 0 {
		limit = req.Pagination.Limit
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	only := ""
	if req.MerchantPreference != nil {
		known := false
		for _, p := range f.products {
			if p.Domain == req.MerchantPreference.MerchantName {
				known = true
				break
			}
		}
		if !known && !strings.Contains(req.MerchantPreference.MerchantName, ".") {
			return nil, "MERCHANT_NOT_RESOLVED", "merchant not resolved: use the merchant domain"
		}
		if req.MerchantPreference.Mode == "ONLY" {
			only = req.MerchantPreference.MerchantName
		}
	}
	var hits []Product
	for _, p := range f.products {
		if only != "" && p.Domain != only && !p.Leaky {
			continue
		}
		for _, m := range p.Match {
			if m != "" && strings.Contains(q, strings.ToLower(m)) {
				hits = append(hits, p)
				break
			}
		}
	}
	// Leaky noise only shows up next to real hits, like the sandbox.
	if only != "" {
		real := false
		for _, h := range hits {
			if h.Domain == only {
				real = true
			}
		}
		if !real {
			hits = nil
		}
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	products := make([]map[string]any, 0, len(hits))
	for _, p := range hits {
		products = append(products, map[string]any{
			"id": p.ID, "merchant": map[string]any{"name": p.MerchantName}, "name": p.Name,
			"imageUrl":       "https://example.com/img/" + p.ID + ".png",
			"priceRange":     map[string]any{"min": money(p.PriceSGD), "max": money(p.PriceSGD)},
			"available":      true,
			"previewVariant": map[string]any{"id": p.VariantID, "name": "Default", "price": money(p.PriceSGD), "available": true},
		})
	}
	return map[string]any{
		"id": NewUUID(), "products": products, "warnings": []string{},
		"pagination": map[string]any{"nextCursor": nil, "hasNextPage": false, "returnedCount": len(products)},
	}, "", ""
}

func (f *FakeReap) variantJSON(p Product) map[string]any {
	return map[string]any{
		"id": p.VariantID, "name": "Default", "options": []any{}, "price": money(p.PriceSGD),
		"available": true, "requiresShipping": true, "media": []any{},
	}
}

func (f *FakeReap) details(body []byte) (any, string, string) {
	var req struct {
		ProductIDs []string `json:"productIds"`
	}
	if err := json.Unmarshal(body, &req); err != nil || len(req.ProductIDs) == 0 || len(req.ProductIDs) > 10 {
		return nil, "AGENTIC_REQUEST_REJECTED", "productIds must have 1..10 ids"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	products := []map[string]any{}
	errs := []map[string]any{}
	for _, id := range req.ProductIDs {
		var found *Product
		for i := range f.products {
			if f.products[i].ID == id {
				found = &f.products[i]
				break
			}
		}
		if found == nil {
			errs = append(errs, map[string]any{"productId": id, "code": "PRODUCT_NOT_FOUND", "message": "unknown product"})
			continue
		}
		products = append(products, map[string]any{
			"id": found.ID, "merchant": map[string]any{"name": found.MerchantName}, "name": found.Name,
			"description": found.Name, "media": []any{}, "options": []any{}, "defaultVariant": f.variantJSON(*found),
		})
	}
	return map[string]any{"products": products, "errors": errs}, "", ""
}

func (f *FakeReap) variant(body []byte) (any, string, string) {
	var req struct {
		ProductID string `json:"productId"`
	}
	_ = json.Unmarshal(body, &req)
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.products {
		if p.ID == req.ProductID {
			return f.variantJSON(p), "", ""
		}
	}
	return nil, "VARIANT_UNAVAILABLE", "unknown product"
}

func (f *FakeReap) createQuote(body []byte) (any, string, string) {
	var req struct {
		Email string `json:"email"`
		Items []struct {
			VariantID string `json:"variantId"`
			Quantity  int    `json:"quantity"`
		} `json:"items"`
		ShippingAddress map[string]any `json:"shippingAddress"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "AGENTIC_REQUEST_REJECTED", "bad json"
	}
	if len(req.Items) == 0 || len(req.Items) > 20 {
		return nil, "AGENTIC_REQUEST_REJECTED", "items must have 1..20 entries"
	}
	if req.Email == "" {
		return nil, "AGENTIC_REQUEST_REJECTED", "email is required"
	}
	if req.ShippingAddress == nil {
		return nil, "ITEMS_UNSHIPPABLE", "shippingAddress is required for items that require shipping"
	}
	if ph, _ := req.ShippingAddress["phone"].(string); !rePhone.MatchString(ph) {
		return nil, "INVALID_PHONE", "phone must be E.164"
	}
	for _, k := range []string{"firstName", "lastName", "addressLine1", "city", "country"} {
		if s, _ := req.ShippingAddress[k].(string); s == "" {
			return nil, "AGENTIC_REQUEST_REJECTED", "shippingAddress." + k + " is required"
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q := &fakeQuote{ID: NewUUID(), Address: req.ShippingAddress, Email: req.Email, Created: time.Now().UTC()}
	for _, it := range req.Items {
		p, ok := f.byVariant[it.VariantID]
		if !ok {
			return nil, "VARIANT_UNAVAILABLE", "unknown variant " + it.VariantID
		}
		if it.Quantity < 1 {
			return nil, "AGENTIC_REQUEST_REJECTED", "quantity must be >= 1"
		}
		if q.Domain != "" && q.Domain != p.Domain {
			return nil, "AGENTIC_REQUEST_REJECTED", "all items must come from one merchant"
		}
		q.Domain = p.Domain
		m := f.multiplier[p.Domain]
		if m == 0 {
			m = 1
		}
		unit := round2(p.PriceSGD * m)
		q.Subtotal = round2(q.Subtotal + unit*float64(it.Quantity))
		q.Items = append(q.Items, map[string]any{"variantId": it.VariantID, "quantity": it.Quantity, "unitPrice": money(unit)})
	}
	q.Shipping = f.shipping[q.Domain]
	f.quotes[q.ID] = q
	return f.quoteJSON(q), "", ""
}

func (f *FakeReap) quoteJSON(q *fakeQuote) map[string]any {
	return map[string]any{
		"id": q.ID,
		"shippingOptions": []any{map[string]any{
			"id": "ship-standard", "name": "Standard delivery", "selected": true, "price": money(q.Shipping),
			"details": []any{map[string]any{"key": "eta", "value": "2-3 business days"}},
		}},
		"amountBreakdown": map[string]any{
			"itemsSubtotal": money(q.Subtotal), "shipping": money(q.Shipping),
			"tax":       map[string]any{"amount": money(0), "includedInPrices": true},
			"discounts": []any{}, "additionalCharges": []any{},
			"finalAmount": money(q.Subtotal + q.Shipping),
		},
		"expiresAt": q.Created.Add(30 * time.Minute).Format(time.RFC3339),
	}
}

func (f *FakeReap) getQuote(id string) (any, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.quotes[id]
	if !ok {
		return nil, "QUOTE_NOT_FOUND", "quote not found"
	}
	return f.quoteJSON(q), "", ""
}

func (f *FakeReap) createCheckout(body []byte, simulate string) (any, string, string) {
	var req struct {
		QuoteID      string `json:"quoteId"`
		EnrollmentID string `json:"enrollmentId"`
		Presentation struct {
			Type      string `json:"type"`
			ReturnURL string `json:"returnUrl"`
		} `json:"presentation"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "AGENTIC_REQUEST_REJECTED", "bad json"
	}
	if !strings.HasPrefix(req.Presentation.ReturnURL, "https://") {
		return nil, "AGENTIC_REQUEST_REJECTED", "presentation.returnUrl must be HTTPS"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	q, ok := f.quotes[req.QuoteID]
	if !ok {
		return nil, "QUOTE_NOT_FOUND", "quote not found"
	}
	e, ok := f.enrollments[req.EnrollmentID]
	if !ok {
		return nil, "ENROLLMENT_NOT_FOUND", "enrollment not found"
	}
	if e.Status != "ACTIVE" {
		return nil, "ENROLLMENT_NOT_ACTIVE", "enrollment is not ACTIVE"
	}
	c := &fakeCheckout{ID: NewUUID(), QuoteID: q.ID, EnrollmentID: e.ID, Status: "REQUIRES_ACTION", Amount: round2(q.Subtotal + q.Shipping)}
	if simulate == "COMPLETED" {
		f.completeLocked(c, "COMPLETED")
	}
	f.checkouts[c.ID] = c
	return f.checkoutJSON(c), "", ""
}

func (f *FakeReap) checkoutJSON(c *fakeCheckout) map[string]any {
	out := map[string]any{
		"id": c.ID, "status": c.Status, "quoteId": c.QuoteID, "enrollmentId": c.EnrollmentID,
		"amount": money(c.Amount), "nextAction": nil,
		"createdAt": time.Now().UTC().Format(time.RFC3339), "updatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if c.Status == "REQUIRES_ACTION" {
		out["nextAction"] = map[string]any{"type": "REDIRECT", "url": f.Server.URL + "/hosted/checkouts/" + c.ID}
	}
	if c.Status == "COMPLETED" {
		out["orderId"] = c.OrderID
		out["finalAmount"] = money(c.Amount)
	} else {
		out["orderId"] = nil
	}
	return out
}

func (f *FakeReap) getCheckout(id string) (any, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.checkouts[id]
	if !ok {
		return nil, "CHECKOUT_NOT_FOUND", "checkout not found"
	}
	return f.checkoutJSON(c), "", ""
}

func (f *FakeReap) createEnrollment(body []byte) (any, string, string) {
	var req struct {
		Source       string         `json:"source"`
		Owner        map[string]any `json:"owner"`
		Presentation struct {
			ReturnURL string `json:"returnUrl"`
		} `json:"presentation"`
	}
	if err := json.Unmarshal(body, &req); err != nil {
		return nil, "AGENTIC_REQUEST_REJECTED", "bad json"
	}
	if req.Source != "EXTERNAL" {
		return nil, "AGENTIC_REQUEST_REJECTED", "only source EXTERNAL is supported"
	}
	if req.Owner == nil || req.Owner["id"] == nil {
		return nil, "AGENTIC_REQUEST_REJECTED", "owner.id is required"
	}
	if !strings.HasPrefix(req.Presentation.ReturnURL, "https://") {
		return nil, "AGENTIC_REQUEST_REJECTED", "presentation.returnUrl must be HTTPS"
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	e := &fakeEnrollment{ID: NewUUID(), Status: "REQUIRES_ACTION", Owner: req.Owner}
	f.enrollments[e.ID] = e
	return f.enrollmentJSON(e), "", ""
}

func (f *FakeReap) enrollmentJSON(e *fakeEnrollment) map[string]any {
	out := map[string]any{
		"id": e.ID, "status": e.Status, "source": "EXTERNAL", "owner": e.Owner, "nextAction": nil,
		"createdAt": time.Now().UTC().Format(time.RFC3339), "updatedAt": time.Now().UTC().Format(time.RFC3339),
	}
	if e.Status == "REQUIRES_ACTION" {
		out["nextAction"] = map[string]any{"type": "REDIRECT", "url": f.Server.URL + "/hosted/enrollments/" + e.ID}
	}
	if e.Status == "ACTIVE" {
		// Real Reap returns card metadata; the api must never persist or echo it.
		out["paymentMethod"] = map[string]any{"type": "CARD", "network": "VISA", "last4": FakeCardLast4, "expiryMonth": 12, "expiryYear": 2030}
	}
	return out
}

// FakeCardLast4 is what the fake returns as paymentMethod.last4; it must never show up in api output.
const FakeCardLast4 = "4242"

func (f *FakeReap) getEnrollment(id string) (any, string, string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	e, ok := f.enrollments[id]
	if !ok {
		return nil, "ENROLLMENT_NOT_FOUND", "enrollment not found"
	}
	return f.enrollmentJSON(e), "", ""
}

// hosted serves a minimal stand-in for Reap's hosted pages (GET .../approve completes it).
func (f *FakeReap) hosted(w http.ResponseWriter, kind, id, action string) {
	switch {
	case kind == "enrollments" && action == "/approve":
		f.ActivateEnrollment(id)
	case kind == "checkouts" && action == "/approve":
		f.ApproveCheckout(id)
	case kind == "checkouts" && action == "/decline":
		f.DeclineCheckout(id)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = io.WriteString(w, `<!doctype html><title>Fake Reap</title><p>Fake Reap hosted `+kind+` page for `+id+
		`.</p><p><a href="/hosted/`+kind+`/`+id+`/approve">Approve</a></p>`)
}
