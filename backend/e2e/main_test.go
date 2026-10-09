//go:build e2e

package e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/e2e/harness"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store/seed"
)

// suite is the shared state of one e2e run (one api process, one database).
var suite struct {
	api     *harness.API
	reap    *harness.FakeReap
	llm     *harness.FakeOpenAI
	seed    seed.Data
	reapKey string
	llmKey  string
	anon    *harness.Client
	skip    string
}

const e2eDBName = "jarvis_e2e"

func TestMain(m *testing.M) { os.Exit(runMain(m)) }

func runMain(m *testing.M) int {
	base := os.Getenv("TEST_DATABASE_URL")
	if base == "" && os.Getenv("E2E_DATABASE_URL") == "" {
		suite.skip = "TEST_DATABASE_URL (or E2E_DATABASE_URL) not set; run `make e2e`"
		return m.Run()
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()

	backendDir, err := filepath.Abs("..")
	if err != nil {
		return fail("abs: %v", err)
	}
	seedDir := filepath.Join(backendDir, "seed")
	if suite.seed, err = seed.Parse(seedDir); err != nil {
		return fail("seed: %v", err)
	}

	suite.reapKey, suite.llmKey = harness.RandomKey(), harness.RandomKey()
	suite.reap = harness.NewFakeReap(suite.reapKey, harness.ProductsFromSeed(suite.seed))
	defer suite.reap.Close()
	suite.llm = harness.NewFakeOpenAI(suite.llmKey)
	defer suite.llm.Close()
	registerParses(suite.llm)

	dsn := os.Getenv("E2E_DATABASE_URL")
	if dsn == "" {
		if dsn, err = harness.ResetDatabase(ctx, base, e2eDBName); err != nil {
			return fail("reset database: %v", err)
		}
	}

	tmp, err := os.MkdirTemp("", "jarvis-e2e-*")
	if err != nil {
		return fail("tmp: %v", err)
	}
	defer os.RemoveAll(tmp)
	bin, err := harness.BuildAPI(ctx, backendDir, tmp)
	if err != nil {
		return fail("%v", err)
	}
	suite.api, err = harness.StartAPI(ctx, bin, tmp, map[string]string{
		"DATABASE_URL":           dsn,
		"AUTO_MIGRATE":           "true",
		"AUTO_SEED":              "true",
		"SEED_DIR":               seedDir,
		"FAKES":                  "false", // real clients, fake servers
		"OPENAI_API_KEY":         suite.llmKey,
		"OPENAI_BASE_URL":        suite.llm.BaseURL(),
		"REAP_API_KEY":           suite.reapKey,
		"REAP_BASE_URL":          suite.reap.URL(),
		"REAP_SIMULATE_CHECKOUT": "false",
		"REAP_RETURN_URL":        "https://example.com/jarvis/reap-return",
		"REAP_WEBHOOKS_ENABLED":  "false",
		"REAP_TIMEOUT":           "10s",
		"LLM_TIMEOUT":            "10s",
		"SEARCH_DEADLINE":        "10s",
		"CHECKOUT_POLL_INTERVAL": "200ms",
		"CHECKOUT_POLL_TIMEOUT":  "2m",
		"QUEUE_WORKERS":          "4",
		"CORS_ORIGINS":           "*",
	}, 60*time.Second)
	if err != nil {
		return fail("start api: %v", err)
	}
	defer suite.api.Stop()
	suite.anon = &harness.Client{Base: suite.api.BaseURL}
	return m.Run()
}

func fail(format string, a ...any) int {
	fmt.Fprintf(os.Stderr, "e2e setup failed: "+format+"\n", a...)
	return 1
}

// registerParses scripts the fake LLM's parser output for the utterances used below.
func registerParses(l *harness.FakeOpenAI) {
	l.SetParse("printer paper and coffee pods",
		map[string]any{"description": "printer paper", "catalog_sku": "a4-copier-paper-ream"},
		map[string]any{"description": "coffee pods", "catalog_sku": "coffee-capsules"})
	l.SetParse("standing desk", map[string]any{"description": "standing desk", "qty": 1})
	l.SetParse("highlighter", map[string]any{"description": "highlighters", "qty": 4, "catalog_sku": "highlighter-set"})
	l.SetParse("staples", map[string]any{"description": "staples", "qty": 3, "catalog_sku": "staples-no10"})
}

// ---------------------------------------------------------------------------------------------
// wire types (decoded per docs/openapi.yaml, deliberately independent of internal/domain)

type reason struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type purchaseRequest struct {
	ID            string  `json:"id"`
	RequesterID   string  `json:"requester_id"`
	RawUtterance  string  `json:"raw_utterance"`
	Status        string  `json:"status"`
	AddressID     *string `json:"address_id"`
	SubtotalCents int64   `json:"subtotal_cents"`
	ShippingCents int64   `json:"shipping_cents"`
	TotalCents    int64   `json:"total_cents"`
	Currency      string  `json:"currency"`
	Decision      string  `json:"decision"`
	FailureReason string  `json:"failure_reason"`
	ConfirmedBy   *string `json:"confirmed_by"`
}

type lineItem struct {
	ID              string   `json:"id"`
	CatalogItemID   *string  `json:"catalog_item_id"`
	Description     string   `json:"description"`
	Qty             int      `json:"qty"`
	PolicyDecision  string   `json:"policy_decision"`
	Reasons         []reason `json:"reasons"`
	SelectedOfferID *string  `json:"selected_offer_id"`
}

type offer struct {
	ID              string `json:"id"`
	LineItemID      string `json:"line_item_id"`
	MerchantName    string `json:"merchant_name"`
	Title           string `json:"title"`
	UnitPriceCents  int64  `json:"unit_price_cents"`
	LandedCostCents int64  `json:"landed_cost_cents"`
	Currency        string `json:"currency"`
	Rank            int    `json:"rank"`
}

type approval struct {
	ID          string   `json:"id"`
	RequestID   string   `json:"request_id"`
	Kind        string   `json:"kind"`
	Status      string   `json:"status"`
	Reasons     []reason `json:"reasons"`
	AmountCents int64    `json:"amount_cents"`
	PrevCents   *int64   `json:"prev_cents"`
	ApproverID  *string  `json:"approver_id"`
}

type payment struct {
	ID             string `json:"id"`
	MerchantName   string `json:"merchant_name"`
	ReapQuoteID    string `json:"reap_quote_id"`
	ReapCheckoutID string `json:"reap_checkout_id"`
	ReapOrderID    string `json:"reap_order_id"`
	QuotedCents    int64  `json:"quoted_cents"`
	FinalCents     *int64 `json:"final_cents"`
	Status         string `json:"status"`
	ApprovalURL    string `json:"approval_url"`
}

type address struct {
	ID         string `json:"id"`
	Label      string `json:"label"`
	FirstName  string `json:"first_name"`
	Phone      string `json:"phone"`
	Email      string `json:"email"`
	PostalCode string `json:"postal_code"`
	IsDefault  bool   `json:"is_default"`
}

type requestDetail struct {
	Request   purchaseRequest `json:"request"`
	LineItems []lineItem      `json:"line_items"`
	Offers    []offer         `json:"offers"`
	Approvals []approval      `json:"approvals"`
	Payments  []payment       `json:"payments"`
	Address   *address        `json:"address"`
}

type approvalView struct {
	Approval approval        `json:"approval"`
	Request  purchaseRequest `json:"request"`
	Lines    []struct {
		LineItem  lineItem `json:"line_item"`
		TopOffers []offer  `json:"top_offers"`
	} `json:"lines"`
}

type auditEvent struct {
	ID        int64           `json:"id"`
	RequestID *string         `json:"request_id"`
	ActorType string          `json:"actor_type"`
	ActorID   string          `json:"actor_id"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type user struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
}

// ---------------------------------------------------------------------------------------------
// helpers

func requireSuite(t *testing.T) {
	t.Helper()
	if suite.skip != "" {
		t.Skip(suite.skip)
	}
}

func ctxT(t *testing.T) context.Context {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	t.Cleanup(cancel)
	return ctx
}

// logTail is appended to fatal messages so a failing step shows what the api said.
func logTail() string {
	if suite.api == nil {
		return ""
	}
	l := suite.api.Logs()
	if suite.api.Exited() {
		l += "\n[api process has exited]"
	}
	if len(l) > 4000 {
		l = l[len(l)-4000:]
	}
	return "\n--- api log tail ---\n" + l
}

func must(t *testing.T, r harness.Response, err error, want int, what string) harness.Response {
	t.Helper()
	if err != nil {
		t.Fatalf("%s: %v%s", what, err, logTail())
	}
	if r.Status != want {
		t.Fatalf("%s: status %d, want %d: %s%s", what, r.Status, want, trim(r.Body), logTail())
	}
	return r
}

func decode[T any](t *testing.T, r harness.Response) T {
	t.Helper()
	var v T
	if err := r.Decode(&v); err != nil {
		t.Fatalf("decode %T: %v: %s", v, err, trim(r.Body))
	}
	return v
}

func trim(b []byte) string {
	s := string(b)
	if len(s) > 1500 {
		return s[:1500] + "..."
	}
	return s
}

func login(t *testing.T, email string) (*harness.Client, user) {
	t.Helper()
	r, err := suite.anon.Do(ctxT(t), http.MethodPost, "/api/v1/auth/login", map[string]any{"email": email})
	must(t, r, err, http.StatusOK, "login "+email)
	out := decode[struct {
		Token string `json:"token"`
		User  user   `json:"user"`
	}](t, r)
	if out.Token == "" {
		t.Fatalf("login %s: empty token", email)
	}
	return suite.anon.As(out.Token), out.User
}

func seedEmail(t *testing.T, role string) string {
	t.Helper()
	for _, u := range suite.seed.Users {
		if string(u.Role) == role {
			return u.Email
		}
	}
	t.Fatalf("no seed user with role %s", role)
	return ""
}

type actors struct {
	manager, approver, admin *harness.Client
	managerUser              user
	approverUser             user
}

func loginAll(t *testing.T) actors {
	t.Helper()
	var a actors
	a.manager, a.managerUser = login(t, seedEmail(t, "manager"))
	a.approver, a.approverUser = login(t, seedEmail(t, "approver"))
	a.admin, _ = login(t, seedEmail(t, "admin"))
	return a
}

func getDetail(t *testing.T, c *harness.Client, id string) requestDetail {
	t.Helper()
	r, err := c.Do(ctxT(t), http.MethodGet, "/api/v1/requests/"+id, nil)
	must(t, r, err, http.StatusOK, "get request")
	return decode[requestDetail](t, r)
}

var terminal = map[string]bool{"ordered": true, "rejected": true, "failed": true, "cancelled": true}

// waitStatus polls GET /requests/{id} until status == want. It fails early if the request lands
// in a different terminal state.
func waitStatus(t *testing.T, c *harness.Client, id, want string) requestDetail {
	t.Helper()
	deadline := time.Now().Add(45 * time.Second)
	var d requestDetail
	for time.Now().Before(deadline) {
		d = getDetail(t, c, id)
		if d.Request.Status == want {
			return d
		}
		if terminal[d.Request.Status] {
			t.Fatalf("request %s reached %s (failure_reason=%q), want %s%s", id, d.Request.Status, d.Request.FailureReason, want, logTail())
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("request %s stuck in %s, want %s%s", id, d.Request.Status, want, logTail())
	return d
}

func createRequest(t *testing.T, c *harness.Client, utterance string, items []map[string]any) purchaseRequest {
	t.Helper()
	body := map[string]any{"utterance": utterance}
	if items != nil {
		body["items"] = items
	}
	r, err := c.Do(ctxT(t), http.MethodPost, "/api/v1/requests", body)
	must(t, r, err, http.StatusCreated, "create request")
	pr := decode[purchaseRequest](t, r)
	if pr.ID == "" {
		t.Fatalf("create request: no id: %s", trim(r.Body))
	}
	return pr
}

func confirm(t *testing.T, c *harness.Client, id, addressID string) harness.Response {
	t.Helper()
	r, err := c.Do(ctxT(t), http.MethodPost, "/api/v1/requests/"+id+"/confirm", map[string]any{"address_id": addressID})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func addresses(t *testing.T, c *harness.Client) []address {
	t.Helper()
	r, err := c.Do(ctxT(t), http.MethodGet, "/api/v1/addresses", nil)
	must(t, r, err, http.StatusOK, "list addresses")
	return decode[struct {
		Addresses []address `json:"addresses"`
	}](t, r).Addresses
}

func addressByLabel(t *testing.T, c *harness.Client, label string) address {
	t.Helper()
	for _, a := range addresses(t, c) {
		if a.Label == label {
			return a
		}
	}
	t.Fatalf("no address labelled %q", label)
	return address{}
}

func seedAddressLabel(t *testing.T, key string) string {
	t.Helper()
	for _, a := range suite.seed.Addresses {
		if a.Key == key {
			return a.Label
		}
	}
	t.Fatalf("no seed address %s", key)
	return ""
}

// payAll approves every requires_action payment of a request on the fake hosted page, asks the api
// to refresh, and waits for ordered.
func payAll(t *testing.T, c *harness.Client, id string) requestDetail {
	t.Helper()
	d := waitStatus(t, c, id, "awaiting_payment")
	if len(d.Payments) == 0 {
		t.Fatalf("awaiting_payment without payments")
	}
	for _, p := range d.Payments {
		if p.Status != "requires_action" {
			continue
		}
		if !strings.HasPrefix(p.ApprovalURL, suite.reap.URL()+"/hosted/checkouts/") {
			t.Fatalf("payment %s approval_url %q is not the Reap-hosted page", p.ID, p.ApprovalURL)
		}
		if suite.reap.ApproveCheckout(p.ReapCheckoutID) != 1 {
			t.Fatalf("fake reap has no pending checkout %q", p.ReapCheckoutID)
		}
	}
	r, err := c.Do(ctxT(t), http.MethodPost, "/api/v1/requests/"+id+"/checkout/refresh", nil)
	must(t, r, err, http.StatusOK, "checkout refresh")
	return waitStatus(t, c, id, "ordered")
}

func approvalsFor(t *testing.T, c *harness.Client, requestID, status string) []approvalView {
	t.Helper()
	path := "/api/v1/approvals"
	if status != "" {
		path += "?status=" + status
	}
	r, err := c.Do(ctxT(t), http.MethodGet, path, nil)
	must(t, r, err, http.StatusOK, "list approvals")
	all := decode[struct {
		Approvals []approvalView `json:"approvals"`
	}](t, r).Approvals
	var out []approvalView
	for _, a := range all {
		if a.Approval.RequestID == requestID {
			out = append(out, a)
		}
	}
	return out
}

func auditFor(t *testing.T, c *harness.Client, id string) []auditEvent {
	t.Helper()
	r, err := c.Do(ctxT(t), http.MethodGet, "/api/v1/requests/"+id+"/audit", nil)
	must(t, r, err, http.StatusOK, "request audit")
	return decode[struct {
		Events []auditEvent `json:"events"`
	}](t, r).Events
}

func auditTypes(evs []auditEvent) []string {
	out := make([]string, len(evs))
	for i, e := range evs {
		out[i] = e.Type
	}
	return out
}

func hasReason(rs []reason, code string) bool {
	for _, r := range rs {
		if r.Code == code {
			return true
		}
	}
	return false
}

func offersFor(d requestDetail, lineID string) []offer {
	var out []offer
	for _, o := range d.Offers {
		if o.LineItemID == lineID {
			out = append(out, o)
		}
	}
	return out
}

func selectedOffer(d requestDetail, li lineItem) *offer {
	if li.SelectedOfferID == nil {
		return nil
	}
	for i := range d.Offers {
		if d.Offers[i].ID == *li.SelectedOfferID {
			return &d.Offers[i]
		}
	}
	return nil
}

func cents(sgd float64) int64 { return int64(sgd*100 + 0.5) }

func catalogID(t *testing.T, c *harness.Client, sku string) string {
	t.Helper()
	r, err := c.Do(ctxT(t), http.MethodGet, "/api/v1/catalog", nil)
	must(t, r, err, http.StatusOK, "catalog")
	items := decode[struct {
		Items []struct {
			ID  string `json:"id"`
			SKU string `json:"sku"`
		} `json:"items"`
	}](t, r).Items
	for _, it := range items {
		if it.SKU == sku {
			return it.ID
		}
	}
	t.Fatalf("catalog has no sku %s", sku)
	return ""
}

func checkoutsCreated() int {
	n := 0
	for _, c := range suite.reap.Calls() {
		if c.Method == http.MethodPost && c.Path == "/agentic/checkouts" {
			n++
		}
	}
	return n
}
