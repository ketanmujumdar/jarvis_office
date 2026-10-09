package api

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
)

func TestAdminCatalogCRUD(t *testing.T) {
	valid := `{"sku":"a4-paper","name":"A4 Paper","category":"Paper","default_qty":5,"max_unit_price_cents":1500,"preferred_vendor_ids":["` + vendorID + `"],"aliases":["copy paper"," Copy Paper ",""]}`
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", valid, 201},
		{"missing sku", `{"name":"x","category":"c","default_qty":1,"max_unit_price_cents":1,"preferred_vendor_ids":["` + vendorID + `"]}`, 400},
		{"bad sku", strings.Replace(valid, `"a4-paper"`, `"a4 paper!"`, 1), 400},
		{"qty zero", strings.Replace(valid, `"default_qty":5`, `"default_qty":0`, 1), 400},
		{"missing max price", strings.Replace(valid, `"max_unit_price_cents":1500,`, ``, 1), 400},
		{"negative price", strings.Replace(valid, `1500`, `-1`, 1), 400},
		{"no vendors", strings.Replace(valid, `["`+vendorID+`"]`, `[]`, 1), 400},
		{"unknown vendor", strings.Replace(valid, vendorID, missingID, 1), 400},
		{"not allowed vendor", strings.Replace(valid, vendorID, blockedID, 1), 400},
		{"vendor not uuid", strings.Replace(valid, vendorID, "popular", 1), 400},
		{"duplicate vendor", strings.Replace(valid, `["`+vendorID+`"]`, `["`+vendorID+`","`+vendorID+`"]`, 1), 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			r := h.do("POST", "/api/v1/admin/catalog", adminID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
			if tt.want != 201 {
				return
			}
			var it domain.CatalogItem
			r.json(t, &it)
			if it.ID == "" || !it.AutoApprove || !it.Active || it.Unit != "each" || len(it.Aliases) != 1 {
				t.Fatalf("item = %+v", it)
			}
			if got := h.audit.Types(); len(got) != 1 || got[0] != audit.AdminChanged {
				t.Fatalf("audit = %v", got)
			}
		})
	}

	// Full lifecycle.
	h := newHarness(t)
	var it domain.CatalogItem
	h.do("POST", "/api/v1/admin/catalog", adminID, valid).json(t, &it)
	if r := h.do("POST", "/api/v1/admin/catalog", adminID, valid); r.code != 409 {
		t.Fatalf("duplicate sku = %d", r.code)
	}
	upd := strings.Replace(valid, `"A4 Paper"`, `"A4 Paper 80gsm"`, 1)
	upd = strings.Replace(upd, `}`, `,"auto_approve":false,"active":false}`, 1)
	var got domain.CatalogItem
	r := h.do("PUT", "/api/v1/admin/catalog/"+it.ID, adminID, upd)
	r.json(t, &got)
	if r.code != 200 || got.Name != "A4 Paper 80gsm" || got.AutoApprove || got.Active || got.ID != it.ID {
		t.Fatalf("update = %d %s", r.code, r.body)
	}
	var all struct{ Items []domain.CatalogItem }
	h.do("GET", "/api/v1/admin/catalog", adminID, nil).json(t, &all)
	if len(all.Items) != 1 {
		t.Fatalf("admin list includes inactive: %+v", all)
	}
	h.do("GET", "/api/v1/catalog", managerID, nil).json(t, &all)
	if len(all.Items) != 0 {
		t.Fatalf("public list hides inactive: %+v", all)
	}
	if r := h.do("GET", "/api/v1/admin/catalog/"+it.ID, adminID, nil); r.code != 200 {
		t.Fatalf("get = %d", r.code)
	}
	if r := h.do("PUT", "/api/v1/admin/catalog/"+missingID, adminID, valid); r.code != 404 {
		t.Fatalf("update missing = %d", r.code)
	}
	if r := h.do("DELETE", "/api/v1/admin/catalog/"+it.ID, adminID, nil); r.code != 204 {
		t.Fatalf("delete = %d", r.code)
	}
	if r := h.do("DELETE", "/api/v1/admin/catalog/"+it.ID, adminID, nil); r.code != 404 {
		t.Fatalf("delete again = %d", r.code)
	}
}

func TestAdminVendors(t *testing.T) {
	tests := []struct {
		name       string
		body       string
		want       int
		wantDomain string
	}{
		{"defaults", `{"domain":"https://www.BettrCoffee.COM/","name":"Bettr Coffee"}`, 201, "bettrcoffee.com"},
		// Only merchants on backend/seed/allowed_merchants.tsv may be allowed vendors.
		{"off allow-list", `{"domain":"shopmustafa.com.sg","name":"Mustafa","reap_merchant_name":"ShopMustafa"}`, 400, ""},
		{"off allow-list explicit allowed", `{"domain":"shopmustafa.com.sg","name":"Mustafa","allowed":true}`, 400, ""},
		{"off allow-list blocked entry ok", `{"domain":"shopmustafa.com.sg","name":"Mustafa","allowed":false}`, 201, "shopmustafa.com.sg"},
		{"full", `{"domain":"anker.com.sg","name":"Anker","reap_merchant_name":"---","category":"electronics","country":"sg","allowed":false,"office_relevant":true,"priority":10}`, 201, "anker.com.sg"},
		{"missing name", `{"domain":"x.com"}`, 400, ""},
		{"bad domain", `{"domain":"not a domain","name":"x"}`, 400, ""},
		{"bad country", `{"domain":"x.com","name":"x","country":"SGP"}`, 400, ""},
		{"bad priority", `{"domain":"x.com","name":"x","priority":-1}`, 400, ""},
		{"duplicate domain", `{"domain":"popular.com.sg","name":"dup"}`, 409, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			r := h.do("POST", "/api/v1/admin/vendors", adminID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
			if tt.want != 201 {
				return
			}
			var v domain.Vendor
			r.json(t, &v)
			if v.Domain != tt.wantDomain || v.Country != "SG" || v.ID == "" {
				t.Fatalf("vendor = %+v", v)
			}
			if tt.name == "defaults" && (!v.Allowed || v.Priority != 50) {
				t.Fatalf("defaults not applied: %+v", v)
			}
			if tt.name == "full" && (v.Allowed || v.Priority != 10 || v.ReapMerchantName != "---") {
				t.Fatalf("explicit values lost: %+v", v)
			}
		})
	}
	h := newHarness(t)
	var v domain.Vendor
	r := h.do("PUT", "/api/v1/admin/vendors/"+vendorID, adminID, `{"domain":"popular.com.sg","name":"Popular Bookstore","reap_merchant_name":"Popular Bookstore SG"}`)
	r.json(t, &v)
	if r.code != 200 || v.ReapMerchantName != "Popular Bookstore SG" {
		t.Fatalf("update = %d %s", r.code, r.body)
	}
	var list struct{ Vendors []domain.Vendor }
	h.do("GET", "/api/v1/admin/vendors", adminID, nil).json(t, &list)
	if len(list.Vendors) != 2 {
		t.Fatalf("list = %+v", list)
	}
	if r := h.do("GET", "/api/v1/admin/vendors/"+vendorID, adminID, nil); r.code != 200 {
		t.Fatalf("get = %d", r.code)
	}
	if r := h.do("DELETE", "/api/v1/admin/vendors/"+vendorID, adminID, nil); r.code != 204 {
		t.Fatalf("delete = %d", r.code)
	}
}

func TestAdminPolicy(t *testing.T) {
	tests := []struct {
		body string
		want int
	}{
		{`{"per_order_limit_cents":60000,"monthly_budget_cents":400000,"price_drift_pct":7.5}`, 200},
		{`{"per_order_limit_cents":0,"monthly_budget_cents":0,"price_drift_pct":0}`, 200},
		{`{"per_order_limit_cents":60000,"monthly_budget_cents":400000}`, 400},
		{`{"per_order_limit_cents":-1,"monthly_budget_cents":400000,"price_drift_pct":5}`, 400},
		{`{"per_order_limit_cents":1,"monthly_budget_cents":-5,"price_drift_pct":5}`, 400},
		{`{"per_order_limit_cents":1,"monthly_budget_cents":1,"price_drift_pct":101}`, 400},
		{`{"per_order_limit_cents":"lots","monthly_budget_cents":1,"price_drift_pct":1}`, 400},
	}
	for _, tt := range tests {
		h := newHarness(t)
		r := h.do("PUT", "/api/v1/admin/policy", adminID, tt.body)
		if r.code != tt.want {
			t.Errorf("%s = %d %s", tt.body, r.code, r.body)
			continue
		}
		if tt.want == 200 {
			var p domain.PolicyConfig
			r.json(t, &p)
			if p.Currency != "SGD" || p.UpdatedBy != adminID {
				t.Errorf("policy = %+v", p)
			}
		}
	}
	h := newHarness(t)
	var p domain.PolicyConfig
	h.do("GET", "/api/v1/admin/policy", adminID, nil).json(t, &p)
	if p.PerOrderLimitCents != 50000 || p.PriceDriftPct != 5 {
		t.Fatalf("get policy = %+v", p)
	}
}

func TestAdminAddresses(t *testing.T) {
	valid := `{"label":"Changi Office","first_name":"Wei","last_name":"Ling","phone":"+65 6200 1003","email":"wei@jarvis.example.com","address_line1":"1 Changi Business Park","postal_code":"486015","is_default":true}`
	tests := []struct {
		name string
		body string
		want int
	}{
		{"valid", valid, 201},
		{"missing label", strings.Replace(valid, `"Changi Office"`, `""`, 1), 400},
		{"bad phone", strings.Replace(valid, `+65 6200 1003`, `62001003`, 1), 400},
		{"bad email", strings.Replace(valid, `wei@jarvis.example.com`, `wei`, 1), 400},
		{"reserved email tld", strings.Replace(valid, `wei@jarvis.example.com`, `wei@jarvis.example`, 1), 400},
		{"bad sg postal", strings.Replace(valid, `486015`, `4860`, 1), 400},
		{"missing line1", strings.Replace(valid, `"1 Changi Business Park"`, `" "`, 1), 400},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t)
			r := h.do("POST", "/api/v1/admin/addresses", adminID, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
			if tt.want == 201 {
				var a domain.Address
				r.json(t, &a)
				if a.City != "Singapore" || a.Country != "SG" || a.Phone != "+6562001003" || !a.IsDefault {
					t.Fatalf("address = %+v", a)
				}
			}
		})
	}
	h := newHarness(t)
	upd := strings.Replace(valid, "Changi Office", "HQ Renamed", 1)
	if r := h.do("PUT", "/api/v1/admin/addresses/"+addressID, adminID, upd); r.code != 200 {
		t.Fatalf("update = %d %s", r.code, r.body)
	}
	if r := h.do("GET", "/api/v1/admin/addresses/"+addressID, adminID, nil); r.code != 200 || !strings.Contains(string(r.body), "HQ Renamed") {
		t.Fatalf("get = %d %s", r.code, r.body)
	}
	if r := h.do("GET", "/api/v1/admin/addresses", adminID, nil); r.code != 200 {
		t.Fatalf("list = %d", r.code)
	}
	// In use by a request -> 409.
	aid := addressID
	h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID, AddressID: &aid}})
	if r := h.do("DELETE", "/api/v1/admin/addresses/"+addressID, adminID, nil); r.code != 409 {
		t.Fatalf("delete in use = %d", r.code)
	}
	delete(h.st.requests, requestID)
	if r := h.do("DELETE", "/api/v1/admin/addresses/"+addressID, adminID, nil); r.code != 204 {
		t.Fatalf("delete = %d", r.code)
	}
}

func TestAdminSystemPrompt(t *testing.T) {
	h := newHarness(t)
	var p domain.SystemPrompt
	h.do("GET", "/api/v1/admin/system-prompt", adminID, nil).json(t, &p)
	if p.Content != "DB PROMPT" || p.Version != 1 {
		t.Fatalf("get = %+v", p)
	}
	r := h.do("PUT", "/api/v1/admin/system-prompt", adminID, `{"content":"Always ask for the address."}`)
	r.json(t, &p)
	if r.code != 200 || p.Version != 2 || p.UpdatedBy != adminID {
		t.Fatalf("put = %d %+v", r.code, p)
	}
	for _, body := range []string{`{"content":"  "}`, `{}`, `{"content":"` + strings.Repeat("x", maxPromptLen+1) + `"}`} {
		if r := h.do("PUT", "/api/v1/admin/system-prompt", adminID, body); r.code != 400 {
			t.Errorf("invalid prompt = %d", r.code)
		}
	}
}

// ---------- webhook ----------

func TestReapWebhook(t *testing.T) {
	okVerify := func(header string, body []byte, secret string, now time.Time) error {
		if header != "t=1,v1=good" || secret != "whsec" {
			return errors.New("bad")
		}
		return nil
	}
	enabled := func(d *Deps) {
		d.Config.ReapWebhooksEnabled = true
		d.Config.ReapWebhookSecret = "whsec"
		d.VerifyWebhook = okVerify
	}
	t.Run("disabled is 404", func(t *testing.T) {
		h := newHarness(t)
		if r := h.do("POST", "/api/v1/webhooks/reap", "", `{"id":"evt_1"}`); r.code != 404 {
			t.Fatalf("code %d", r.code)
		}
	})

	tests := []struct {
		name string
		sig  string
		body string
		want int
	}{
		{"missing signature", "", `{"id":"evt_1","type":"X","data":{}}`, 400},
		{"bad signature", "t=1,v1=bad", `{"id":"evt_1","type":"X","data":{}}`, 400},
		{"bad envelope", "t=1,v1=good", `{"type":"X"}`, 400},
		{"accepted", "t=1,v1=good", `{"id":"evt_1","type":"CHECKOUT_STATUS_UPDATED","data":{"id":"chk_1"}}`, 200},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := newHarness(t, enabled)
			r := h.doWebhook(tt.sig, tt.body)
			if r.code != tt.want {
				t.Fatalf("code %d %s", r.code, r.body)
			}
		})
	}

	t.Run("reconciles payment and dedupes", func(t *testing.T) {
		h := newHarness(t, enabled)
		h.st.payments["chk_1"] = domain.Payment{ID: "p1", RequestID: requestID, ReapCheckoutID: "chk_1"}
		h.addRequest(domain.RequestDetail{Request: domain.PurchaseRequest{ID: requestID}})
		body := `{"id":"evt_9","type":"CHECKOUT_STATUS_UPDATED","data":{"id":"chk_1","status":"COMPLETED"}}`
		if r := h.doWebhook("t=1,v1=good", body); r.code != 200 || !strings.Contains(string(r.body), "accepted") {
			t.Fatalf("first = %d %s", r.code, r.body)
		}
		if r := h.doWebhook("t=1,v1=good", body); r.code != 200 || !strings.Contains(string(r.body), "duplicate") {
			t.Fatalf("second = %d %s", r.code, r.body)
		}
		waitFor(t, func() bool {
			h.orch.mu.Lock()
			defer h.orch.mu.Unlock()
			return len(h.orch.refreshed) == 1 && h.orch.refreshed[0] == requestID
		})
		if got := h.audit.Types(); len(got) != 1 || got[0] != audit.ReapWebhookReceived {
			t.Fatalf("audit = %v", got)
		}
	})
}

func (h *harness) doWebhook(sig, body string) resp {
	h.t.Helper()
	req := newReq("POST", "/api/v1/webhooks/reap", body)
	if sig != "" {
		req.Header.Set("X-Reap-Webhook-Signature", sig)
	}
	return h.serve(req)
}

func waitFor(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition not met")
}

func TestAdminDefaultSystemPrompt(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, SystemPromptFile), []byte("SEEDED DEFAULT"), 0o600); err != nil {
		t.Fatal(err)
	}
	h := newHarness(t, func(d *Deps) { d.Config.SeedDir = dir })
	var out map[string]string
	r := h.do("GET", "/api/v1/admin/system-prompt/default", adminID, nil)
	r.json(t, &out)
	if r.code != 200 || out["key"] != "agent" || out["content"] != "SEEDED DEFAULT" {
		t.Fatalf("default = %d %s", r.code, r.body)
	}
	if r := h.do("GET", "/api/v1/admin/system-prompt/default", managerID, nil); r.code != 403 {
		t.Fatalf("manager = %d", r.code)
	}
	// Missing file: built-in fallback.
	h2 := newHarness(t, func(d *Deps) { d.Config.SeedDir = t.TempDir() })
	h2.do("GET", "/api/v1/admin/system-prompt/default", adminID, nil).json(t, &out)
	if out["content"] != realtime.FallbackInstructions {
		t.Fatalf("fallback = %q", out["content"])
	}
}

// The repository's real seed prompt is served when SeedDir points at backend/seed.
func TestAdminDefaultSystemPromptRealSeed(t *testing.T) {
	h := newHarness(t, func(d *Deps) { d.Config.SeedDir = filepath.Join("..", "..", "seed") })
	var out map[string]string
	h.do("GET", "/api/v1/admin/system-prompt/default", adminID, nil).json(t, &out)
	if out["content"] == realtime.FallbackInstructions || !strings.Contains(strings.ToLower(out["content"]), "address") {
		t.Fatalf("seed prompt not loaded: %.80q", out["content"])
	}
}

func TestApproveWithFollowUpErrorStillOK(t *testing.T) {
	h := newHarness(t, func(d *Deps) {})
	h.srv.d.Approvals = followUpFails{h.appr}
	r := h.do("POST", "/api/v1/approvals/"+approvalID+"/approve", approverID, nil)
	if r.code != 200 || !strings.Contains(string(r.body), `"status":"approved"`) {
		t.Fatalf("approve = %d %s", r.code, r.body)
	}
}

type followUpFails struct{ *fakeApprovals }

func (f followUpFails) Approve(ctx context.Context, id string, u domain.User, c string) (domain.Approval, error) {
	a, err := f.fakeApprovals.Approve(ctx, id, u, c)
	if err != nil {
		return a, err
	}
	return a, errors.New("orchestrator: checkout enqueue failed")
}
