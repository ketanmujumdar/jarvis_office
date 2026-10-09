package api

import (
	"context"
	"errors"
	"math"
	"net/http"
	"net/mail"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/audit"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/realtime"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// recordAdmin writes an admin.changed audit event; failures are logged, not returned.
func (s *Server) recordAdmin(ctx context.Context, user domain.User, entity, id, action string) {
	if err := s.d.Audit.Record(ctx, "", domain.ActorUser, user.ID, audit.AdminChanged,
		map[string]string{"entity": entity, "id": id, "action": action}); err != nil {
		s.log.Warn("audit admin.changed", "entity", entity, "err", err)
	}
}

// ---------- catalog ----------

// CatalogItemInput is POST/PUT /admin/catalog.
type CatalogItemInput struct {
	SKU                string   `json:"sku"`
	Name               string   `json:"name"`
	Aliases            []string `json:"aliases"`
	Category           string   `json:"category"`
	Unit               string   `json:"unit"`
	DefaultQty         *int     `json:"default_qty"`
	MaxUnitPriceCents  *int64   `json:"max_unit_price_cents"`
	PreferredVendorIDs []string `json:"preferred_vendor_ids"`
	AutoApprove        *bool    `json:"auto_approve"`
	SearchQuery        string   `json:"search_query"`
	Active             *bool    `json:"active"`
}

var skuRe = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (in CatalogItemInput) toItem() (domain.CatalogItem, error) {
	it := domain.CatalogItem{
		SKU: strings.TrimSpace(in.SKU), Name: strings.TrimSpace(in.Name), Category: strings.TrimSpace(in.Category),
		Unit: strings.TrimSpace(in.Unit), SearchQuery: strings.TrimSpace(in.SearchQuery),
		AutoApprove: true, Active: true, Aliases: []string{},
	}
	switch {
	case it.SKU == "":
		return it, validationf("sku is required")
	case !skuRe.MatchString(it.SKU):
		return it, validationf("sku may contain letters, digits, '.', '_' and '-' (max 64)")
	case it.Name == "":
		return it, validationf("name is required")
	case it.Category == "":
		return it, validationf("category is required")
	case in.DefaultQty == nil:
		return it, validationf("default_qty is required")
	case *in.DefaultQty < 1:
		return it, validationf("default_qty must be at least 1")
	case in.MaxUnitPriceCents == nil:
		return it, validationf("max_unit_price_cents is required")
	case *in.MaxUnitPriceCents < 0:
		return it, validationf("max_unit_price_cents must be >= 0")
	case len(in.PreferredVendorIDs) == 0:
		return it, validationf("preferred_vendor_ids needs at least one vendor")
	}
	it.DefaultQty = *in.DefaultQty
	it.MaxUnitPriceCents = domain.Cents(*in.MaxUnitPriceCents)
	if in.AutoApprove != nil {
		it.AutoApprove = *in.AutoApprove
	}
	if in.Active != nil {
		it.Active = *in.Active
	}
	if it.Unit == "" {
		it.Unit = "each"
	}
	seen := map[string]bool{}
	for _, a := range in.Aliases {
		a = strings.TrimSpace(a)
		if a != "" && !seen[strings.ToLower(a)] {
			seen[strings.ToLower(a)] = true
			it.Aliases = append(it.Aliases, a)
		}
	}
	seenV := map[string]bool{}
	for i, v := range in.PreferredVendorIDs {
		v = strings.TrimSpace(v)
		if !uuidRe.MatchString(v) {
			return it, validationf("preferred_vendor_ids[%d] must be a UUID", i)
		}
		if seenV[v] {
			return it, validationf("preferred_vendor_ids[%d] is a duplicate", i)
		}
		seenV[v] = true
		it.PreferredVendorIDs = append(it.PreferredVendorIDs, v)
	}
	return it, nil
}

// checkVendors ensures every preferred vendor exists and is allowed.
func (s *Server) checkVendors(ctx context.Context, ids []string) error {
	for _, id := range ids {
		v, err := s.d.Store.Vendors().Get(ctx, id)
		if errors.Is(err, domain.ErrNotFound) {
			return validationf("vendor %s does not exist", id)
		}
		if err != nil {
			return err
		}
		if !v.Allowed {
			return validationf("vendor %s (%s) is not on the allow-list", v.Name, v.Domain)
		}
	}
	return nil
}

func (s *Server) adminListCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	items, err := s.d.Store.Catalog().List(r.Context(), store.CatalogFilter{})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": nonNilCatalog(items)})
}

func (s *Server) adminGetCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	it, err := s.d.Store.Catalog().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, nonNilCatalog([]domain.CatalogItem{it})[0])
}

func (s *Server) saveCatalog(w http.ResponseWriter, r *http.Request, id string) {
	if !s.st(w, r) {
		return
	}
	var in CatalogItemInput
	if err := decode(r, &in, false); err != nil {
		s.fail(w, r, err)
		return
	}
	it, err := in.toItem()
	if err == nil {
		err = s.checkVendors(r.Context(), it.PreferredVendorIDs)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status, action := http.StatusCreated, "create"
	if id == "" {
		err = s.d.Store.Catalog().Create(r.Context(), &it)
	} else {
		status, action = http.StatusOK, "update"
		it.ID = id
		err = s.d.Store.Catalog().Update(r.Context(), &it)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "catalog_item", it.ID, action)
	writeJSON(w, status, nonNilCatalog([]domain.CatalogItem{it})[0])
}

func (s *Server) adminCreateCatalog(w http.ResponseWriter, r *http.Request) { s.saveCatalog(w, r, "") }
func (s *Server) adminUpdateCatalog(w http.ResponseWriter, r *http.Request) {
	s.saveCatalog(w, r, r.PathValue("id"))
}

func (s *Server) adminDeleteCatalog(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.d.Store.Catalog().Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "catalog_item", id, "delete")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- vendors ----------

// VendorInput is POST/PUT /admin/vendors.
type VendorInput struct {
	Domain           string `json:"domain"`
	Name             string `json:"name"`
	ReapMerchantName string `json:"reap_merchant_name"`
	Category         string `json:"category"`
	Country          string `json:"country"`
	Allowed          *bool  `json:"allowed"`
	OfficeRelevant   bool   `json:"office_relevant"`
	Priority         *int   `json:"priority"`
	Notes            string `json:"notes"`
}

var domainRe = regexp.MustCompile(`^([a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?\.)+[a-z]{2,}$`)

func (in VendorInput) toVendor() (domain.Vendor, error) {
	v := domain.Vendor{
		Domain: strings.ToLower(strings.TrimSpace(in.Domain)), Name: strings.TrimSpace(in.Name),
		// The merchant name must match Reap exactly; keep internal spacing as given.
		ReapMerchantName: strings.TrimSpace(in.ReapMerchantName),
		Category:         strings.TrimSpace(in.Category), Country: strings.ToUpper(strings.TrimSpace(in.Country)),
		Allowed: true, OfficeRelevant: in.OfficeRelevant, Priority: 50, Notes: strings.TrimSpace(in.Notes),
	}
	v.Domain = strings.TrimPrefix(strings.TrimPrefix(v.Domain, "https://"), "http://")
	v.Domain = strings.TrimPrefix(strings.TrimSuffix(v.Domain, "/"), "www.")
	switch {
	case v.Domain == "":
		return v, validationf("domain is required")
	case !domainRe.MatchString(v.Domain):
		return v, validationf("domain must look like example.com.sg")
	case v.Name == "":
		return v, validationf("name is required")
	}
	if v.Country == "" {
		v.Country = "SG"
	}
	if len(v.Country) != 2 {
		return v, validationf("country must be a 2-letter code")
	}
	if in.Allowed != nil {
		v.Allowed = *in.Allowed
	}
	if in.Priority != nil {
		if *in.Priority < 0 || *in.Priority > 1000 {
			return v, validationf("priority must be between 0 and 1000")
		}
		v.Priority = *in.Priority
	}
	return v, nil
}

func (s *Server) adminListVendors(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	vs, err := s.d.Store.Vendors().List(r.Context(), store.VendorFilter{})
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if vs == nil {
		vs = []domain.Vendor{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"vendors": vs})
}

func (s *Server) adminGetVendor(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	v, err := s.d.Store.Vendors().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, v)
}

func (s *Server) saveVendor(w http.ResponseWriter, r *http.Request, id string) {
	if !s.st(w, r) {
		return
	}
	var in VendorInput
	if err := decode(r, &in, false); err != nil {
		s.fail(w, r, err)
		return
	}
	v, err := in.toVendor()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if v.Allowed && !s.d.AllowedMerchants.Contains(v.Domain) {
		s.fail(w, r, validationf("domain %s is not on the allowed merchant list, so it cannot be an allowed vendor", v.Domain))
		return
	}
	status, action := http.StatusCreated, "create"
	if id == "" {
		err = s.d.Store.Vendors().Create(r.Context(), &v)
	} else {
		status, action = http.StatusOK, "update"
		v.ID = id
		err = s.d.Store.Vendors().Update(r.Context(), &v)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "vendor", v.ID, action)
	writeJSON(w, status, v)
}

func (s *Server) adminCreateVendor(w http.ResponseWriter, r *http.Request) { s.saveVendor(w, r, "") }
func (s *Server) adminUpdateVendor(w http.ResponseWriter, r *http.Request) {
	s.saveVendor(w, r, r.PathValue("id"))
}

func (s *Server) adminDeleteVendor(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.d.Store.Vendors().Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "vendor", id, "delete")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- policy ----------

// PolicyInput is PUT /admin/policy.
type PolicyInput struct {
	PerOrderLimitCents *int64   `json:"per_order_limit_cents"`
	MonthlyBudgetCents *int64   `json:"monthly_budget_cents"`
	PriceDriftPct      *float64 `json:"price_drift_pct"`
}

func (in PolicyInput) toPolicy() (domain.PolicyConfig, error) {
	switch {
	case in.PerOrderLimitCents == nil || in.MonthlyBudgetCents == nil || in.PriceDriftPct == nil:
		return domain.PolicyConfig{}, validationf("per_order_limit_cents, monthly_budget_cents and price_drift_pct are required")
	case *in.PerOrderLimitCents < 0:
		return domain.PolicyConfig{}, validationf("per_order_limit_cents must be >= 0")
	case *in.MonthlyBudgetCents < 0:
		return domain.PolicyConfig{}, validationf("monthly_budget_cents must be >= 0")
	case math.IsNaN(*in.PriceDriftPct) || *in.PriceDriftPct < 0 || *in.PriceDriftPct > 100:
		return domain.PolicyConfig{}, validationf("price_drift_pct must be between 0 and 100")
	}
	return domain.PolicyConfig{
		Currency:           domain.Currency,
		PerOrderLimitCents: domain.Cents(*in.PerOrderLimitCents),
		MonthlyBudgetCents: domain.Cents(*in.MonthlyBudgetCents),
		PriceDriftPct:      *in.PriceDriftPct,
	}, nil
}

func (s *Server) adminGetPolicy(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	p, err := s.d.Store.Policy().Get(r.Context())
	if err != nil {
		s.fail(w, r, err)
		return
	}
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *Server) adminUpdatePolicy(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	var in PolicyInput
	if err := decode(r, &in, false); err != nil {
		s.fail(w, r, err)
		return
	}
	p, err := in.toPolicy()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	u := currentUser(r)
	if err := s.d.Store.Policy().Update(r.Context(), &p, u.ID); err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), u, "policy", "policy", "update")
	if fresh, err := s.d.Store.Policy().Get(r.Context()); err == nil {
		p = fresh
	}
	writeJSON(w, http.StatusOK, p)
}

// ---------- addresses ----------

// AddressInput is POST/PUT /admin/addresses.
type AddressInput struct {
	Label        string `json:"label"`
	FirstName    string `json:"first_name"`
	LastName     string `json:"last_name"`
	Phone        string `json:"phone"`
	Email        string `json:"email"`
	AddressLine1 string `json:"address_line1"`
	AddressLine2 string `json:"address_line2"`
	City         string `json:"city"`
	Region       string `json:"region"`
	PostalCode   string `json:"postal_code"`
	Country      string `json:"country"`
	IsDefault    bool   `json:"is_default"`
}

var (
	phoneRe    = regexp.MustCompile(`^\+[1-9]\d{6,14}$`)
	sgPostalRe = regexp.MustCompile(`^\d{6}$`)
)

func (in AddressInput) toAddress() (domain.Address, error) {
	a := domain.Address{
		Label: strings.TrimSpace(in.Label), FirstName: strings.TrimSpace(in.FirstName),
		LastName: strings.TrimSpace(in.LastName), Phone: strings.ReplaceAll(strings.TrimSpace(in.Phone), " ", ""),
		Email: strings.TrimSpace(in.Email), AddressLine1: strings.TrimSpace(in.AddressLine1),
		AddressLine2: strings.TrimSpace(in.AddressLine2), City: strings.TrimSpace(in.City),
		Region: strings.TrimSpace(in.Region), PostalCode: strings.TrimSpace(in.PostalCode),
		Country: strings.ToUpper(strings.TrimSpace(in.Country)), IsDefault: in.IsDefault,
	}
	for _, f := range []struct{ name, val string }{
		{"label", a.Label}, {"first_name", a.FirstName}, {"last_name", a.LastName}, {"phone", a.Phone},
		{"email", a.Email}, {"address_line1", a.AddressLine1}, {"postal_code", a.PostalCode},
	} {
		if f.val == "" {
			return a, validationf("%s is required", f.name)
		}
	}
	if !phoneRe.MatchString(a.Phone) {
		return a, validationf("phone must be E.164, e.g. +6562001001")
	}
	if addr, err := mail.ParseAddress(a.Email); err != nil || addr.Address != a.Email {
		return a, validationf("email is not valid")
	}
	if domain.ReservedEmailDomain(a.Email) {
		return a, validationf("email domain is reserved (e.g. .example) and Reap rejects it; use a real domain")
	}
	if a.City == "" {
		a.City = "Singapore"
	}
	if a.Country == "" {
		a.Country = "SG"
	}
	if len(a.Country) != 2 {
		return a, validationf("country must be a 2-letter code")
	}
	if a.Country == "SG" && !sgPostalRe.MatchString(a.PostalCode) {
		return a, validationf("postal_code must be 6 digits for Singapore")
	}
	return a, nil
}

func (s *Server) adminListAddresses(w http.ResponseWriter, r *http.Request) { s.listAddresses(w, r) }

func (s *Server) adminGetAddress(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	a, err := s.d.Store.Addresses().Get(r.Context(), r.PathValue("id"))
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, a)
}

func (s *Server) saveAddress(w http.ResponseWriter, r *http.Request, id string) {
	if !s.st(w, r) {
		return
	}
	var in AddressInput
	if err := decode(r, &in, false); err != nil {
		s.fail(w, r, err)
		return
	}
	a, err := in.toAddress()
	if err != nil {
		s.fail(w, r, err)
		return
	}
	status, action := http.StatusCreated, "create"
	if id == "" {
		err = s.d.Store.Addresses().Create(r.Context(), &a)
	} else {
		status, action = http.StatusOK, "update"
		a.ID = id
		err = s.d.Store.Addresses().Update(r.Context(), &a)
	}
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "address", a.ID, action)
	writeJSON(w, status, a)
}

func (s *Server) adminCreateAddress(w http.ResponseWriter, r *http.Request) { s.saveAddress(w, r, "") }
func (s *Server) adminUpdateAddress(w http.ResponseWriter, r *http.Request) {
	s.saveAddress(w, r, r.PathValue("id"))
}

func (s *Server) adminDeleteAddress(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	id := r.PathValue("id")
	if err := s.d.Store.Addresses().Delete(r.Context(), id); err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), currentUser(r), "address", id, "delete")
	w.WriteHeader(http.StatusNoContent)
}

// ---------- system prompt ----------

func (s *Server) adminGetSystemPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	p, err := s.d.Store.SystemPrompts().Get(r.Context(), domain.SystemPromptKeyAgent)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, p)
}

const maxPromptLen = 32000

func (s *Server) adminUpdateSystemPrompt(w http.ResponseWriter, r *http.Request) {
	if !s.st(w, r) {
		return
	}
	var body struct {
		Content string `json:"content"`
	}
	if err := decode(r, &body, false); err != nil {
		s.fail(w, r, err)
		return
	}
	if strings.TrimSpace(body.Content) == "" {
		s.fail(w, r, validationf("content is required"))
		return
	}
	if len(body.Content) > maxPromptLen {
		s.fail(w, r, validationf("content is too long (max %d characters)", maxPromptLen))
		return
	}
	u := currentUser(r)
	p, err := s.d.Store.SystemPrompts().Update(r.Context(), domain.SystemPromptKeyAgent, body.Content, u.ID)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	s.recordAdmin(r.Context(), u, "system_prompt", p.Key, "update")
	writeJSON(w, http.StatusOK, p)
}

// SystemPromptFile is the seeded default prompt inside Config.SeedDir.
const SystemPromptFile = "system_prompt.md"

// adminDefaultSystemPrompt returns the seeded default prompt (read-only) for "Reset to default".
func (s *Server) adminDefaultSystemPrompt(w http.ResponseWriter, r *http.Request) {
	content := realtime.FallbackInstructions
	dir := s.d.Config.SeedDir
	if dir == "" {
		dir = "seed"
	}
	if b, err := os.ReadFile(filepath.Join(dir, SystemPromptFile)); err == nil && strings.TrimSpace(string(b)) != "" {
		content = string(b)
	} else if err != nil && !os.IsNotExist(err) {
		s.log.Warn("read default system prompt", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": domain.SystemPromptKeyAgent, "content": content})
}
