// Package seed loads backend/seed/*.yaml (+ system_prompt.md) into the store. Owner: agent A.
//
// Files (all in one directory, default backend/seed):
//
//	users.yaml          -> users          (upsert by email)
//	vendors.yaml        -> vendors        (upsert by domain)
//	catalog.yaml        -> catalog_items  (upsert by sku; preferred_vendors domains -> vendor ids)
//	policy.yaml         -> policy_config  (only if missing, unless Options.Overwrite)
//	addresses.yaml      -> addresses      (upsert by label)
//	system_prompt.md    -> system_prompts key "agent" (SeedIfMissing; never clobbers admin edits)
//
// The loader is idempotent: running it twice yields the same DB state.
package seed

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// File names inside the seed directory.
const (
	UsersFile        = "users.yaml"
	VendorsFile      = "vendors.yaml"
	CatalogFile      = "catalog.yaml"
	PolicyFile       = "policy.yaml"
	AddressesFile    = "addresses.yaml"
	SystemPromptFile = "system_prompt.md"
)

// UserSeed is one entry of users.yaml.
type UserSeed struct {
	Key   string      `yaml:"key"`
	Name  string      `yaml:"name"`
	Email string      `yaml:"email"`
	Role  domain.Role `yaml:"role"`
}

// VendorSeed is one entry of vendors.yaml.
type VendorSeed struct {
	Domain           string `yaml:"domain"`
	Name             string `yaml:"name"`
	ReapMerchantName string `yaml:"reap_merchant_name"`
	Category         string `yaml:"category"`
	Country          string `yaml:"country"`
	Allowed          bool   `yaml:"allowed"`
	OfficeRelevant   bool   `yaml:"office_relevant"`
	Priority         int    `yaml:"priority"`
	ProbeNotes       string `yaml:"probe_notes"`
}

// CatalogSeed is one entry of catalog.yaml.
type CatalogSeed struct {
	SKU              string   `yaml:"sku"`
	Name             string   `yaml:"name"`
	Aliases          []string `yaml:"aliases"`
	Category         string   `yaml:"category"`
	Unit             string   `yaml:"unit"`
	DefaultQty       int      `yaml:"default_qty"`
	MaxUnitPriceSGD  float64  `yaml:"max_unit_price_sgd"`
	PreferredVendors []string `yaml:"preferred_vendors"` // vendor domains, in preference order
	AutoApprove      bool     `yaml:"auto_approve"`
	SearchQuery      string   `yaml:"search_query"`
	ProbeSeen        string   `yaml:"probe_seen"`
}

// PolicySeed is policy.yaml's `policy` object.
type PolicySeed struct {
	Currency         string  `yaml:"currency"`
	PerOrderLimitSGD float64 `yaml:"per_order_limit_sgd"`
	MonthlyBudgetSGD float64 `yaml:"monthly_budget_sgd"`
	PriceDriftPct    float64 `yaml:"price_drift_pct"`
}

// AddressSeed is one entry of addresses.yaml.
type AddressSeed struct {
	Key          string `yaml:"key"`
	Label        string `yaml:"label"`
	FirstName    string `yaml:"first_name"`
	LastName     string `yaml:"last_name"`
	Phone        string `yaml:"phone"`
	Email        string `yaml:"email"`
	AddressLine1 string `yaml:"address_line1"`
	AddressLine2 string `yaml:"address_line2"`
	City         string `yaml:"city"`
	Region       string `yaml:"region"`
	PostalCode   string `yaml:"postal_code"`
	Country      string `yaml:"country"`
	IsDefault    bool   `yaml:"is_default"`
}

// Data is the parsed content of a seed directory.
type Data struct {
	Users        []UserSeed    `yaml:"users"`
	Vendors      []VendorSeed  `yaml:"vendors"`
	Items        []CatalogSeed `yaml:"items"`
	Policy       PolicySeed    `yaml:"policy"`
	Addresses    []AddressSeed `yaml:"addresses"`
	SystemPrompt string        `yaml:"-"`
}

// Options control loading.
//
// Default behaviour keeps admin edits: users are upserted by email (demo logins must exist), and
// vendors, catalog items and addresses are only seeded when their table is empty (so edits and
// deletions survive restarts with AUTO_SEED). The policy row is only inserted when missing and the
// system prompt only when key "agent" is missing.
type Options struct {
	// Overwrite policy even if a row exists (default: keep admin edits).
	OverwritePolicy bool
	// Overwrite upserts vendors (by domain), catalog items (by sku) and addresses (by label) from the
	// seed files even when the tables already have rows, and implies OverwritePolicy. Rows that are
	// not in the seed files are left alone; the system prompt is never overwritten.
	Overwrite bool
}

// Parse reads and validates every seed file in dir. It does not touch the database.
func Parse(dir string) (Data, error) {
	var d Data
	for _, f := range []string{UsersFile, VendorsFile, CatalogFile, PolicyFile, AddressesFile} {
		b, err := os.ReadFile(filepath.Join(dir, f))
		if err != nil {
			return Data{}, fmt.Errorf("seed: read %s: %w", f, err)
		}
		if err := yaml.Unmarshal(b, &d); err != nil {
			return Data{}, fmt.Errorf("seed: parse %s: %w", f, err)
		}
	}
	b, err := os.ReadFile(filepath.Join(dir, SystemPromptFile))
	if err != nil {
		return Data{}, fmt.Errorf("seed: read %s: %w", SystemPromptFile, err)
	}
	d.SystemPrompt = string(b)
	return d, d.Validate()
}

// Validate checks referential integrity and invariants of the seed data.
func (d Data) Validate() error {
	vendors := map[string]VendorSeed{}
	for _, v := range d.Vendors {
		if v.Domain == "" {
			return fmt.Errorf("%w: vendor with empty domain", domain.ErrValidation)
		}
		if _, dup := vendors[v.Domain]; dup {
			return fmt.Errorf("%w: duplicate vendor %s", domain.ErrValidation, v.Domain)
		}
		vendors[v.Domain] = v
	}
	skus := map[string]bool{}
	for _, it := range d.Items {
		if it.SKU == "" || skus[it.SKU] {
			return fmt.Errorf("%w: empty or duplicate sku %q", domain.ErrValidation, it.SKU)
		}
		skus[it.SKU] = true
		if it.DefaultQty <= 0 || it.MaxUnitPriceSGD <= 0 || len(it.PreferredVendors) == 0 {
			return fmt.Errorf("%w: item %s needs default_qty, max_unit_price_sgd and preferred_vendors", domain.ErrValidation, it.SKU)
		}
		for _, dom := range it.PreferredVendors {
			v, ok := vendors[dom]
			if !ok || !v.Allowed || v.ReapMerchantName == "" {
				return fmt.Errorf("%w: item %s prefers unknown/disallowed vendor %s", domain.ErrValidation, it.SKU, dom)
			}
		}
	}
	defaults := 0
	for _, a := range d.Addresses {
		if a.IsDefault {
			defaults++
		}
	}
	if len(d.Addresses) > 0 && defaults != 1 {
		return fmt.Errorf("%w: exactly one default address required, got %d", domain.ErrValidation, defaults)
	}
	if d.Policy.PerOrderLimitSGD <= 0 || d.Policy.MonthlyBudgetSGD <= 0 {
		return fmt.Errorf("%w: policy limits must be > 0", domain.ErrValidation)
	}
	if d.SystemPrompt == "" {
		return fmt.Errorf("%w: empty system prompt", domain.ErrValidation)
	}
	return nil
}

// Load parses dir and writes it into s in one transaction. It is idempotent.
func Load(ctx context.Context, s store.Store, dir string, opts Options) error {
	d, err := Parse(dir)
	if err != nil {
		return err
	}
	return Apply(ctx, s, d, opts)
}

// Apply writes already-parsed seed data into s in one transaction (see Options for semantics).
func Apply(ctx context.Context, s store.Store, d Data, opts Options) error {
	if err := d.Validate(); err != nil {
		return err
	}
	err := s.WithTx(ctx, func(tx store.Store) error {
		for _, u := range d.Users {
			user := domain.User{Name: u.Name, Email: u.Email, Role: u.Role}
			if err := tx.Users().Upsert(ctx, &user); err != nil {
				return fmt.Errorf("user %s: %w", u.Email, err)
			}
		}
		if err := applyVendors(ctx, tx, d, opts); err != nil {
			return err
		}
		if err := applyCatalog(ctx, tx, d, opts); err != nil {
			return err
		}
		if err := applyAddresses(ctx, tx, d, opts); err != nil {
			return err
		}
		if err := applyPolicy(ctx, tx, d, opts); err != nil {
			return err
		}
		return tx.SystemPrompts().SeedIfMissing(ctx, domain.SystemPromptKeyAgent, d.SystemPrompt)
	})
	if err != nil {
		return fmt.Errorf("seed: %w", err)
	}
	return nil
}

func applyVendors(ctx context.Context, tx store.Store, d Data, opts Options) error {
	existing, err := tx.Vendors().List(ctx, store.VendorFilter{})
	if err != nil {
		return err
	}
	if len(existing) > 0 && !opts.Overwrite {
		return nil
	}
	for _, v := range d.Vendors {
		vendor := domain.Vendor{
			Domain: v.Domain, Name: v.Name, ReapMerchantName: v.ReapMerchantName, Category: v.Category,
			Country: v.Country, Allowed: v.Allowed, OfficeRelevant: v.OfficeRelevant, Priority: v.Priority,
			Notes: v.ProbeNotes,
		}
		if vendor.Country == "" {
			vendor.Country = "SG"
		}
		if err := tx.Vendors().Upsert(ctx, &vendor); err != nil {
			return fmt.Errorf("vendor %s: %w", v.Domain, err)
		}
	}
	return nil
}

func applyCatalog(ctx context.Context, tx store.Store, d Data, opts Options) error {
	existing, err := tx.Catalog().List(ctx, store.CatalogFilter{})
	if err != nil {
		return err
	}
	if len(existing) > 0 && !opts.Overwrite {
		return nil
	}
	vendors, err := tx.Vendors().List(ctx, store.VendorFilter{})
	if err != nil {
		return err
	}
	idByDomain := make(map[string]string, len(vendors))
	for _, v := range vendors {
		idByDomain[v.Domain] = v.ID
	}
	for _, it := range d.Items {
		var vids []string
		for _, dom := range it.PreferredVendors {
			// A vendor an admin deleted is skipped rather than resurrected.
			if id, ok := idByDomain[dom]; ok {
				vids = append(vids, id)
			}
		}
		item := domain.CatalogItem{
			SKU: it.SKU, Name: it.Name, Aliases: append([]string{}, it.Aliases...), Category: it.Category,
			Unit: it.Unit, DefaultQty: it.DefaultQty, MaxUnitPriceCents: domain.CentsFromFloat(it.MaxUnitPriceSGD),
			PreferredVendorIDs: vids, AutoApprove: it.AutoApprove, SearchQuery: it.SearchQuery, Active: true,
		}
		if err := tx.Catalog().Upsert(ctx, &item); err != nil {
			return fmt.Errorf("catalog item %s: %w", it.SKU, err)
		}
	}
	return nil
}

func applyAddresses(ctx context.Context, tx store.Store, d Data, opts Options) error {
	existing, err := tx.Addresses().List(ctx)
	if err != nil {
		return err
	}
	if len(existing) > 0 && !opts.Overwrite {
		return nil
	}
	// Non-default addresses first so the default one wins the single-default constraint last.
	ordered := make([]AddressSeed, 0, len(d.Addresses))
	for _, a := range d.Addresses {
		if !a.IsDefault {
			ordered = append(ordered, a)
		}
	}
	for _, a := range d.Addresses {
		if a.IsDefault {
			ordered = append(ordered, a)
		}
	}
	for _, a := range ordered {
		addr := domain.Address{
			Label: a.Label, FirstName: a.FirstName, LastName: a.LastName, Phone: a.Phone, Email: a.Email,
			AddressLine1: a.AddressLine1, AddressLine2: a.AddressLine2, City: a.City, Region: a.Region,
			PostalCode: a.PostalCode, Country: a.Country, IsDefault: a.IsDefault,
		}
		if err := tx.Addresses().Upsert(ctx, &addr); err != nil {
			return fmt.Errorf("address %s: %w", a.Label, err)
		}
	}
	return nil
}

func applyPolicy(ctx context.Context, tx store.Store, d Data, opts Options) error {
	_, err := tx.Policy().Get(ctx)
	switch {
	case err == nil && !opts.OverwritePolicy && !opts.Overwrite:
		return nil
	case err != nil && !errors.Is(err, domain.ErrNotFound):
		return err
	}
	cur := d.Policy.Currency
	if cur == "" {
		cur = domain.Currency
	}
	p := domain.PolicyConfig{
		Currency:           cur,
		PerOrderLimitCents: domain.CentsFromFloat(d.Policy.PerOrderLimitSGD),
		MonthlyBudgetCents: domain.CentsFromFloat(d.Policy.MonthlyBudgetSGD),
		PriceDriftPct:      d.Policy.PriceDriftPct,
	}
	return tx.Policy().Update(ctx, &p, "")
}
