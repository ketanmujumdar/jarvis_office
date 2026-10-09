package postgres

import (
	"context"
	"errors"
	"strings"

	"github.com/ketanmujumdar/jarvis_office/backend/internal/domain"
	"github.com/ketanmujumdar/jarvis_office/backend/internal/store"
)

// ---------------- users ----------------

type userRepo struct{ s *Store }

const userCols = `id, name, email, role, created_at`

func scanUser(r scanner) (domain.User, error) {
	var u domain.User
	err := r.Scan(&u.ID, &u.Name, &u.Email, &u.Role, &u.CreatedAt)
	u.CreatedAt = utc(u.CreatedAt)
	return u, err
}

func (r userRepo) Get(ctx context.Context, id string) (domain.User, error) {
	if !validUUID(id) {
		return domain.User{}, notFound("user", id)
	}
	return one(ctx, r.s.db, "user", scanUser, `SELECT `+userCols+` FROM users WHERE id = $1`, id)
}

func (r userRepo) GetByEmail(ctx context.Context, email string) (domain.User, error) {
	return one(ctx, r.s.db, "user", scanUser,
		`SELECT `+userCols+` FROM users WHERE lower(email) = lower($1)`, strings.TrimSpace(email))
}

func (r userRepo) List(ctx context.Context) ([]domain.User, error) {
	return collect(ctx, r.s.db, "user", scanUser, `SELECT `+userCols+` FROM users ORDER BY name, email`)
}

func (r userRepo) Upsert(ctx context.Context, u *domain.User) error {
	got, err := one(ctx, r.s.db, "user", scanUser, `
		INSERT INTO users (name, email, role) VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET name = EXCLUDED.name, role = EXCLUDED.role
		RETURNING `+userCols, u.Name, u.Email, u.Role)
	if err != nil {
		return err
	}
	*u = got
	return nil
}

// ---------------- vendors ----------------

type vendorRepo struct{ s *Store }

const vendorCols = `id, domain, name, reap_merchant_name, category, country, allowed, office_relevant,
	priority, notes, created_at, updated_at`

func scanVendor(r scanner) (domain.Vendor, error) {
	var v domain.Vendor
	err := r.Scan(&v.ID, &v.Domain, &v.Name, &v.ReapMerchantName, &v.Category, &v.Country, &v.Allowed,
		&v.OfficeRelevant, &v.Priority, &v.Notes, &v.CreatedAt, &v.UpdatedAt)
	v.CreatedAt, v.UpdatedAt = utc(v.CreatedAt), utc(v.UpdatedAt)
	return v, err
}

func normVendor(v *domain.Vendor) {
	v.Domain = strings.ToLower(strings.TrimSpace(v.Domain))
	if v.Country == "" {
		v.Country = "SG"
	}
}

func (r vendorRepo) List(ctx context.Context, f store.VendorFilter) ([]domain.Vendor, error) {
	return collect(ctx, r.s.db, "vendor", scanVendor, `SELECT `+vendorCols+` FROM vendors
		WHERE ($1::bool = false OR allowed) AND ($2 = '' OR category = $2)
		ORDER BY priority, name, domain`, f.AllowedOnly, f.Category)
}

func (r vendorRepo) Get(ctx context.Context, id string) (domain.Vendor, error) {
	if !validUUID(id) {
		return domain.Vendor{}, notFound("vendor", id)
	}
	return one(ctx, r.s.db, "vendor", scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE id = $1`, id)
}

func (r vendorRepo) GetByDomain(ctx context.Context, domainName string) (domain.Vendor, error) {
	return one(ctx, r.s.db, "vendor", scanVendor, `SELECT `+vendorCols+` FROM vendors WHERE domain = $1`,
		strings.ToLower(strings.TrimSpace(domainName)))
}

func (r vendorRepo) GetByMerchantName(ctx context.Context, merchantName string) (domain.Vendor, error) {
	if merchantName == "" {
		return domain.Vendor{}, notFound("vendor", merchantName)
	}
	// Several vendors could share a name in theory; prefer allowed, then most preferred.
	return one(ctx, r.s.db, "vendor", scanVendor, `SELECT `+vendorCols+` FROM vendors
		WHERE reap_merchant_name = $1 ORDER BY allowed DESC, priority, domain LIMIT 1`, merchantName)
}

func (r vendorRepo) Create(ctx context.Context, v *domain.Vendor) error {
	normVendor(v)
	got, err := one(ctx, r.s.db, "vendor", scanVendor, `
		INSERT INTO vendors (domain, name, reap_merchant_name, category, country, allowed, office_relevant, priority, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING `+vendorCols,
		v.Domain, v.Name, v.ReapMerchantName, v.Category, v.Country, v.Allowed, v.OfficeRelevant, v.Priority, v.Notes)
	if err != nil {
		return err
	}
	*v = got
	return nil
}

func (r vendorRepo) Update(ctx context.Context, v *domain.Vendor) error {
	if !validUUID(v.ID) {
		return notFound("vendor", v.ID)
	}
	normVendor(v)
	got, err := one(ctx, r.s.db, "vendor", scanVendor, `
		UPDATE vendors SET domain = $2, name = $3, reap_merchant_name = $4, category = $5, country = $6,
			allowed = $7, office_relevant = $8, priority = $9, notes = $10, updated_at = clock_timestamp()
		WHERE id = $1 RETURNING `+vendorCols,
		v.ID, v.Domain, v.Name, v.ReapMerchantName, v.Category, v.Country, v.Allowed, v.OfficeRelevant, v.Priority, v.Notes)
	if err != nil {
		return err
	}
	*v = got
	return nil
}

func (r vendorRepo) Delete(ctx context.Context, id string) error {
	if !validUUID(id) {
		return notFound("vendor", id)
	}
	tag, err := r.s.db.Exec(ctx, `DELETE FROM vendors WHERE id = $1`, id)
	if err != nil {
		return mapDeleteErr("vendor", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("vendor", id)
	}
	return nil
}

func (r vendorRepo) Upsert(ctx context.Context, v *domain.Vendor) error {
	normVendor(v)
	got, err := one(ctx, r.s.db, "vendor", scanVendor, `
		INSERT INTO vendors (domain, name, reap_merchant_name, category, country, allowed, office_relevant, priority, notes)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		ON CONFLICT (domain) DO UPDATE SET name = EXCLUDED.name, reap_merchant_name = EXCLUDED.reap_merchant_name,
			category = EXCLUDED.category, country = EXCLUDED.country, allowed = EXCLUDED.allowed,
			office_relevant = EXCLUDED.office_relevant, priority = EXCLUDED.priority, notes = EXCLUDED.notes,
			updated_at = clock_timestamp()
		RETURNING `+vendorCols,
		v.Domain, v.Name, v.ReapMerchantName, v.Category, v.Country, v.Allowed, v.OfficeRelevant, v.Priority, v.Notes)
	if err != nil {
		return err
	}
	*v = got
	return nil
}

// ---------------- catalog ----------------

type catalogRepo struct{ s *Store }

const catalogCols = `c.id, c.sku, c.name, c.aliases, c.category, c.unit, c.default_qty, c.max_unit_price_cents,
	COALESCE((SELECT array_agg(civ.vendor_id::text ORDER BY civ.rank, civ.vendor_id)
	          FROM catalog_item_vendors civ WHERE civ.catalog_item_id = c.id), '{}'::text[]),
	c.auto_approve, c.search_query, c.active, c.created_at, c.updated_at`

func scanCatalog(r scanner) (domain.CatalogItem, error) {
	var it domain.CatalogItem
	err := r.Scan(&it.ID, &it.SKU, &it.Name, &it.Aliases, &it.Category, &it.Unit, &it.DefaultQty,
		&it.MaxUnitPriceCents, &it.PreferredVendorIDs, &it.AutoApprove, &it.SearchQuery, &it.Active,
		&it.CreatedAt, &it.UpdatedAt)
	it.Aliases = emptyIfNil(it.Aliases)
	it.PreferredVendorIDs = emptyIfNil(it.PreferredVendorIDs)
	it.CreatedAt, it.UpdatedAt = utc(it.CreatedAt), utc(it.UpdatedAt)
	return it, err
}

func normCatalog(it *domain.CatalogItem) error {
	it.SKU = strings.TrimSpace(it.SKU)
	it.Aliases = emptyIfNil(it.Aliases)
	if it.DefaultQty == 0 {
		it.DefaultQty = 1
	}
	for _, id := range it.PreferredVendorIDs {
		if !validUUID(id) {
			return errors.Join(domain.ErrValidation, errors.New("catalog: preferred vendor id "+id+" is not a uuid"))
		}
	}
	return nil
}

func (r catalogRepo) List(ctx context.Context, f store.CatalogFilter) ([]domain.CatalogItem, error) {
	q := strings.TrimSpace(f.Query)
	return collect(ctx, r.s.db, "catalog item", scanCatalog, `SELECT `+catalogCols+` FROM catalog_items c
		WHERE ($1::bool = false OR c.active) AND ($2 = '' OR c.category = $2)
		  AND ($3 = '' OR c.name ILIKE $4 OR c.sku ILIKE $4 OR EXISTS (SELECT 1 FROM unnest(c.aliases) a WHERE a ILIKE $4))
		ORDER BY c.category, c.name`, f.ActiveOnly, f.Category, q, likePattern(q))
}

func (r catalogRepo) Get(ctx context.Context, id string) (domain.CatalogItem, error) {
	if !validUUID(id) {
		return domain.CatalogItem{}, notFound("catalog item", id)
	}
	return one(ctx, r.s.db, "catalog item", scanCatalog, `SELECT `+catalogCols+` FROM catalog_items c WHERE c.id = $1`, id)
}

func (r catalogRepo) GetBySKU(ctx context.Context, sku string) (domain.CatalogItem, error) {
	return one(ctx, r.s.db, "catalog item", scanCatalog, `SELECT `+catalogCols+` FROM catalog_items c WHERE c.sku = $1`,
		strings.TrimSpace(sku))
}

func replaceItemVendors(ctx context.Context, db DB, itemID string, vendorIDs []string) error {
	if _, err := db.Exec(ctx, `DELETE FROM catalog_item_vendors WHERE catalog_item_id = $1`, itemID); err != nil {
		return mapErr("catalog item vendors", err)
	}
	seen := map[string]bool{}
	rank := 0
	for _, vid := range vendorIDs {
		if seen[vid] {
			continue
		}
		seen[vid] = true
		if _, err := db.Exec(ctx, `INSERT INTO catalog_item_vendors (catalog_item_id, vendor_id, rank) VALUES ($1, $2, $3)`,
			itemID, vid, rank); err != nil {
			return mapErr("catalog item vendors", err)
		}
		rank++
	}
	return nil
}

// writeCatalog runs the given single-row statement (returning id), replaces vendors and re-reads.
// Args are built from it after normalisation; withID appends it.ID as the last argument.
func (r catalogRepo) writeCatalog(ctx context.Context, it *domain.CatalogItem, sql string, withID bool) error {
	if err := normCatalog(it); err != nil {
		return err
	}
	args := catalogArgs(it)
	if withID {
		args = append(args, it.ID)
	}
	return r.s.atomic(ctx, func(db DB) error {
		var id string
		if err := db.QueryRow(ctx, sql, args...).Scan(&id); err != nil {
			return mapErr("catalog item", err)
		}
		if err := replaceItemVendors(ctx, db, id, it.PreferredVendorIDs); err != nil {
			return err
		}
		got, err := one(ctx, db, "catalog item", scanCatalog, `SELECT `+catalogCols+` FROM catalog_items c WHERE c.id = $1`, id)
		if err != nil {
			return err
		}
		*it = got
		return nil
	})
}

func catalogArgs(it *domain.CatalogItem) []any {
	return []any{it.SKU, it.Name, emptyIfNil(it.Aliases), it.Category, it.Unit, it.DefaultQty, it.MaxUnitPriceCents,
		it.AutoApprove, it.SearchQuery, it.Active}
}

func (r catalogRepo) Create(ctx context.Context, it *domain.CatalogItem) error {
	return r.writeCatalog(ctx, it, `INSERT INTO catalog_items
		(sku, name, aliases, category, unit, default_qty, max_unit_price_cents, auto_approve, search_query, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`, false)
}

func (r catalogRepo) Update(ctx context.Context, it *domain.CatalogItem) error {
	if !validUUID(it.ID) {
		return notFound("catalog item", it.ID)
	}
	return r.writeCatalog(ctx, it, `UPDATE catalog_items SET sku = $1, name = $2, aliases = $3, category = $4,
		unit = $5, default_qty = $6, max_unit_price_cents = $7, auto_approve = $8, search_query = $9, active = $10,
		updated_at = clock_timestamp() WHERE id = $11 RETURNING id`, true)
}

func (r catalogRepo) Delete(ctx context.Context, id string) error {
	if !validUUID(id) {
		return notFound("catalog item", id)
	}
	tag, err := r.s.db.Exec(ctx, `DELETE FROM catalog_items WHERE id = $1`, id)
	if err != nil {
		return mapDeleteErr("catalog item", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("catalog item", id)
	}
	return nil
}

func (r catalogRepo) Upsert(ctx context.Context, it *domain.CatalogItem) error {
	return r.writeCatalog(ctx, it, `INSERT INTO catalog_items
		(sku, name, aliases, category, unit, default_qty, max_unit_price_cents, auto_approve, search_query, active)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
		ON CONFLICT (sku) DO UPDATE SET name = EXCLUDED.name, aliases = EXCLUDED.aliases, category = EXCLUDED.category,
			unit = EXCLUDED.unit, default_qty = EXCLUDED.default_qty, max_unit_price_cents = EXCLUDED.max_unit_price_cents,
			auto_approve = EXCLUDED.auto_approve, search_query = EXCLUDED.search_query, active = EXCLUDED.active,
			updated_at = clock_timestamp()
		RETURNING id`, false)
}

// ---------------- policy ----------------

type policyRepo struct{ s *Store }

const policyCols = `currency, per_order_limit_cents, monthly_budget_cents, price_drift_pct::float8, updated_at,
	COALESCE(updated_by::text, '')`

func scanPolicy(r scanner) (domain.PolicyConfig, error) {
	var p domain.PolicyConfig
	err := r.Scan(&p.Currency, &p.PerOrderLimitCents, &p.MonthlyBudgetCents, &p.PriceDriftPct, &p.UpdatedAt, &p.UpdatedBy)
	p.UpdatedAt = utc(p.UpdatedAt)
	return p, err
}

func (r policyRepo) Get(ctx context.Context) (domain.PolicyConfig, error) {
	return one(ctx, r.s.db, "policy", scanPolicy, `SELECT `+policyCols+` FROM policy_config WHERE id = 1`)
}

func (r policyRepo) Update(ctx context.Context, p *domain.PolicyConfig, updatedBy string) error {
	if p.Currency == "" {
		p.Currency = domain.Currency
	}
	got, err := one(ctx, r.s.db, "policy", scanPolicy, `
		INSERT INTO policy_config (id, currency, per_order_limit_cents, monthly_budget_cents, price_drift_pct, updated_by)
		VALUES (1, $1, $2, $3, $4, $5)
		ON CONFLICT (id) DO UPDATE SET currency = EXCLUDED.currency, per_order_limit_cents = EXCLUDED.per_order_limit_cents,
			monthly_budget_cents = EXCLUDED.monthly_budget_cents, price_drift_pct = EXCLUDED.price_drift_pct,
			updated_by = EXCLUDED.updated_by, updated_at = clock_timestamp()
		RETURNING `+policyCols, p.Currency, p.PerOrderLimitCents, p.MonthlyBudgetCents, p.PriceDriftPct, nullStr(updatedBy))
	if err != nil {
		return err
	}
	*p = got
	return nil
}

// ---------------- addresses ----------------

type addressRepo struct{ s *Store }

const addressCols = `id, label, first_name, last_name, phone, email, address_line1, address_line2, city, region,
	postal_code, country, is_default, created_at, updated_at`

func scanAddress(r scanner) (domain.Address, error) {
	var a domain.Address
	err := r.Scan(&a.ID, &a.Label, &a.FirstName, &a.LastName, &a.Phone, &a.Email, &a.AddressLine1, &a.AddressLine2,
		&a.City, &a.Region, &a.PostalCode, &a.Country, &a.IsDefault, &a.CreatedAt, &a.UpdatedAt)
	a.CreatedAt, a.UpdatedAt = utc(a.CreatedAt), utc(a.UpdatedAt)
	return a, err
}

func normAddress(a *domain.Address) {
	a.Label = strings.TrimSpace(a.Label)
	if a.City == "" {
		a.City = "Singapore"
	}
	if a.Country == "" {
		a.Country = "SG"
	}
}

func addressArgs(a *domain.Address) []any {
	return []any{a.Label, a.FirstName, a.LastName, a.Phone, a.Email, a.AddressLine1, a.AddressLine2, a.City,
		a.Region, a.PostalCode, a.Country, a.IsDefault}
}

func (r addressRepo) List(ctx context.Context) ([]domain.Address, error) {
	return collect(ctx, r.s.db, "address", scanAddress,
		`SELECT `+addressCols+` FROM addresses ORDER BY is_default DESC, label`)
}

func (r addressRepo) Get(ctx context.Context, id string) (domain.Address, error) {
	if !validUUID(id) {
		return domain.Address{}, notFound("address", id)
	}
	return one(ctx, r.s.db, "address", scanAddress, `SELECT `+addressCols+` FROM addresses WHERE id = $1`, id)
}

func (r addressRepo) GetDefault(ctx context.Context) (domain.Address, error) {
	return one(ctx, r.s.db, "address", scanAddress, `SELECT `+addressCols+` FROM addresses WHERE is_default`)
}

// write clears other defaults (when a.IsDefault) and runs the statement, all in one tx.
// keep identifies the row that may keep its default flag: "id" or "label" column.
// Callers must normAddress(a) before building args.
func (r addressRepo) write(ctx context.Context, a *domain.Address, keepCol, keepVal, sql string, args ...any) error {
	return r.s.atomic(ctx, func(db DB) error {
		if a.IsDefault {
			if _, err := db.Exec(ctx, `UPDATE addresses SET is_default = false, updated_at = clock_timestamp()
				WHERE is_default AND `+keepCol+`::text <> $1`, keepVal); err != nil {
				return mapErr("address", err)
			}
		}
		got, err := one(ctx, db, "address", scanAddress, sql, args...)
		if err != nil {
			return err
		}
		*a = got
		return nil
	})
}

func (r addressRepo) Create(ctx context.Context, a *domain.Address) error {
	normAddress(a)
	return r.write(ctx, a, "id", "", `INSERT INTO addresses (label, first_name, last_name, phone, email,
		address_line1, address_line2, city, region, postal_code, country, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12) RETURNING `+addressCols, addressArgs(a)...)
}

func (r addressRepo) Update(ctx context.Context, a *domain.Address) error {
	if !validUUID(a.ID) {
		return notFound("address", a.ID)
	}
	normAddress(a)
	return r.write(ctx, a, "id", a.ID, `UPDATE addresses SET label = $1, first_name = $2, last_name = $3, phone = $4,
		email = $5, address_line1 = $6, address_line2 = $7, city = $8, region = $9, postal_code = $10, country = $11,
		is_default = $12, updated_at = clock_timestamp() WHERE id = $13 RETURNING `+addressCols,
		append(addressArgs(a), a.ID)...)
}

func (r addressRepo) Delete(ctx context.Context, id string) error {
	if !validUUID(id) {
		return notFound("address", id)
	}
	tag, err := r.s.db.Exec(ctx, `DELETE FROM addresses WHERE id = $1`, id)
	if err != nil {
		return mapDeleteErr("address", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("address", id)
	}
	return nil
}

func (r addressRepo) Upsert(ctx context.Context, a *domain.Address) error {
	normAddress(a)
	return r.write(ctx, a, "label", a.Label, `INSERT INTO addresses (label, first_name, last_name, phone, email,
		address_line1, address_line2, city, region, postal_code, country, is_default)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (label) DO UPDATE SET first_name = EXCLUDED.first_name, last_name = EXCLUDED.last_name,
			phone = EXCLUDED.phone, email = EXCLUDED.email, address_line1 = EXCLUDED.address_line1,
			address_line2 = EXCLUDED.address_line2, city = EXCLUDED.city, region = EXCLUDED.region,
			postal_code = EXCLUDED.postal_code, country = EXCLUDED.country, is_default = EXCLUDED.is_default,
			updated_at = clock_timestamp()
		RETURNING `+addressCols, addressArgs(a)...)
}

// ---------------- system prompts ----------------

type systemPromptRepo struct{ s *Store }

const promptCols = `key, content, version, updated_at, COALESCE(updated_by::text, '')`

func scanPrompt(r scanner) (domain.SystemPrompt, error) {
	var p domain.SystemPrompt
	err := r.Scan(&p.Key, &p.Content, &p.Version, &p.UpdatedAt, &p.UpdatedBy)
	p.UpdatedAt = utc(p.UpdatedAt)
	return p, err
}

func (r systemPromptRepo) Get(ctx context.Context, key string) (domain.SystemPrompt, error) {
	return one(ctx, r.s.db, "system prompt", scanPrompt, `SELECT `+promptCols+` FROM system_prompts WHERE key = $1`, key)
}

func (r systemPromptRepo) Update(ctx context.Context, key, content, updatedBy string) (domain.SystemPrompt, error) {
	return one(ctx, r.s.db, "system prompt", scanPrompt, `
		INSERT INTO system_prompts (key, content, version, updated_by) VALUES ($1, $2, 1, $3)
		ON CONFLICT (key) DO UPDATE SET content = EXCLUDED.content, version = system_prompts.version + 1,
			updated_by = EXCLUDED.updated_by, updated_at = clock_timestamp()
		RETURNING `+promptCols, key, content, nullStr(updatedBy))
}

func (r systemPromptRepo) SeedIfMissing(ctx context.Context, key, content string) error {
	_, err := r.s.db.Exec(ctx, `INSERT INTO system_prompts (key, content, version) VALUES ($1, $2, 1)
		ON CONFLICT (key) DO NOTHING`, key, content)
	return mapErr("system prompt", err)
}

// ---------------- enrollments ----------------

type enrollmentRepo struct{ s *Store }

const enrollmentCols = `id, reap_enrollment_id, status, owner_ref, owner_email, next_action_url,
	COALESCE(created_by::text, ''), created_at, updated_at`

func scanEnrollment(r scanner) (domain.Enrollment, error) {
	var e domain.Enrollment
	err := r.Scan(&e.ID, &e.ReapEnrollmentID, &e.Status, &e.OwnerRef, &e.OwnerEmail, &e.NextActionURL, &e.CreatedBy,
		&e.CreatedAt, &e.UpdatedAt)
	e.CreatedAt, e.UpdatedAt = utc(e.CreatedAt), utc(e.UpdatedAt)
	return e, err
}

func (r enrollmentRepo) Create(ctx context.Context, e *domain.Enrollment) error {
	if e.Status == "" {
		e.Status = domain.EnrollmentRequiresAction
	}
	got, err := one(ctx, r.s.db, "enrollment", scanEnrollment, `
		INSERT INTO enrollments (reap_enrollment_id, status, owner_ref, owner_email, next_action_url, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, clock_timestamp(), clock_timestamp()) RETURNING `+enrollmentCols,
		e.ReapEnrollmentID, e.Status, e.OwnerRef, e.OwnerEmail, e.NextActionURL, nullStr(e.CreatedBy))
	if err != nil {
		return err
	}
	*e = got
	return nil
}

func (r enrollmentRepo) Get(ctx context.Context, id string) (domain.Enrollment, error) {
	if !validUUID(id) {
		return domain.Enrollment{}, notFound("enrollment", id)
	}
	return one(ctx, r.s.db, "enrollment", scanEnrollment, `SELECT `+enrollmentCols+` FROM enrollments WHERE id = $1`, id)
}

func (r enrollmentRepo) GetByReapID(ctx context.Context, reapID string) (domain.Enrollment, error) {
	return one(ctx, r.s.db, "enrollment", scanEnrollment,
		`SELECT `+enrollmentCols+` FROM enrollments WHERE reap_enrollment_id = $1`, reapID)
}

func (r enrollmentRepo) Latest(ctx context.Context) (domain.Enrollment, error) {
	return one(ctx, r.s.db, "enrollment", scanEnrollment,
		`SELECT `+enrollmentCols+` FROM enrollments ORDER BY created_at DESC, id DESC LIMIT 1`)
}

func (r enrollmentRepo) LatestActive(ctx context.Context) (domain.Enrollment, error) {
	return one(ctx, r.s.db, "enrollment", scanEnrollment,
		`SELECT `+enrollmentCols+` FROM enrollments WHERE status = 'ACTIVE' ORDER BY created_at DESC, id DESC LIMIT 1`)
}

func (r enrollmentRepo) ListPending(ctx context.Context) ([]domain.Enrollment, error) {
	return collect(ctx, r.s.db, "enrollment", scanEnrollment,
		`SELECT `+enrollmentCols+` FROM enrollments WHERE status = 'REQUIRES_ACTION' ORDER BY created_at DESC, id DESC`)
}

func (r enrollmentRepo) UpdateStatus(ctx context.Context, id string, status domain.EnrollmentStatus, nextActionURL string) error {
	if !validUUID(id) {
		return notFound("enrollment", id)
	}
	tag, err := r.s.db.Exec(ctx, `UPDATE enrollments SET status = $2, next_action_url = $3, updated_at = clock_timestamp()
		WHERE id = $1`, id, status, nextActionURL)
	if err != nil {
		return mapErr("enrollment", err)
	}
	if tag.RowsAffected() == 0 {
		return notFound("enrollment", id)
	}
	return nil
}
