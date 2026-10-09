-- Jarvis Office initial schema. Money columns are BIGINT cents (SGD). Times are timestamptz.
-- Migrations are applied in filename order by store.Migrate and recorded in schema_migrations.

CREATE EXTENSION IF NOT EXISTS pgcrypto; -- gen_random_uuid()

CREATE TABLE users (
    id         UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name       TEXT NOT NULL,
    email      TEXT NOT NULL UNIQUE,
    role       TEXT NOT NULL CHECK (role IN ('manager', 'approver', 'admin')),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE vendors (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    domain             TEXT NOT NULL UNIQUE,
    name               TEXT NOT NULL,
    reap_merchant_name TEXT NOT NULL DEFAULT '',
    category           TEXT NOT NULL DEFAULT '',
    country            TEXT NOT NULL DEFAULT 'SG',
    allowed            BOOLEAN NOT NULL DEFAULT TRUE,
    office_relevant    BOOLEAN NOT NULL DEFAULT FALSE,
    priority           INT NOT NULL DEFAULT 50,
    notes              TEXT NOT NULL DEFAULT '',
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX vendors_reap_merchant_name_idx ON vendors (reap_merchant_name);

CREATE TABLE catalog_items (
    id                   UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sku                  TEXT NOT NULL UNIQUE,
    name                 TEXT NOT NULL,
    aliases              TEXT[] NOT NULL DEFAULT '{}',
    category             TEXT NOT NULL,
    unit                 TEXT NOT NULL DEFAULT '',
    default_qty          INT NOT NULL DEFAULT 1 CHECK (default_qty > 0),
    max_unit_price_cents BIGINT NOT NULL CHECK (max_unit_price_cents >= 0),
    auto_approve         BOOLEAN NOT NULL DEFAULT TRUE,
    search_query         TEXT NOT NULL DEFAULT '',
    active               BOOLEAN NOT NULL DEFAULT TRUE,
    created_at           TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at           TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Ordered preferred vendors per catalog item (rank 0 = most preferred).
CREATE TABLE catalog_item_vendors (
    catalog_item_id UUID NOT NULL REFERENCES catalog_items (id) ON DELETE CASCADE,
    vendor_id       UUID NOT NULL REFERENCES vendors (id) ON DELETE CASCADE,
    rank            INT NOT NULL DEFAULT 0,
    PRIMARY KEY (catalog_item_id, vendor_id)
);

-- Single-row policy table (id is always 1).
CREATE TABLE policy_config (
    id                    INT PRIMARY KEY DEFAULT 1 CHECK (id = 1),
    currency              TEXT NOT NULL DEFAULT 'SGD',
    per_order_limit_cents BIGINT NOT NULL CHECK (per_order_limit_cents >= 0),
    monthly_budget_cents  BIGINT NOT NULL CHECK (monthly_budget_cents >= 0),
    price_drift_pct       NUMERIC(5, 2) NOT NULL DEFAULT 5 CHECK (price_drift_pct >= 0),
    updated_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by            UUID REFERENCES users (id)
);

CREATE TABLE addresses (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    label         TEXT NOT NULL UNIQUE,
    first_name    TEXT NOT NULL,
    last_name     TEXT NOT NULL,
    phone         TEXT NOT NULL CHECK (phone ~ '^\+[1-9][0-9]{6,14}$'),
    email         TEXT NOT NULL,
    address_line1 TEXT NOT NULL,
    address_line2 TEXT NOT NULL DEFAULT '',
    city          TEXT NOT NULL DEFAULT 'Singapore',
    region        TEXT NOT NULL DEFAULT '',
    postal_code   TEXT NOT NULL DEFAULT '',
    country       TEXT NOT NULL DEFAULT 'SG',
    is_default    BOOLEAN NOT NULL DEFAULT FALSE,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
-- At most one default address.
CREATE UNIQUE INDEX addresses_one_default_idx ON addresses (is_default) WHERE is_default;

CREATE TABLE system_prompts (
    key        TEXT PRIMARY KEY,
    content    TEXT NOT NULL,
    version    INT NOT NULL DEFAULT 1,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_by UUID REFERENCES users (id)
);

CREATE TABLE enrollments (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    reap_enrollment_id TEXT NOT NULL UNIQUE,
    status             TEXT NOT NULL CHECK (status IN ('REQUIRES_ACTION', 'ACTIVE', 'FAILED', 'EXPIRED', 'REVOKED')),
    owner_ref          TEXT NOT NULL,
    owner_email        TEXT NOT NULL,
    next_action_url    TEXT NOT NULL DEFAULT '',
    created_by         UUID REFERENCES users (id),
    created_at         TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE purchase_requests (
    id             UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    requester_id   UUID NOT NULL REFERENCES users (id),
    raw_utterance  TEXT NOT NULL,
    status         TEXT NOT NULL CHECK (status IN ('parsing', 'searching', 'quoted', 'pending_approval', 'approved',
                                                   'checking_out', 'awaiting_payment', 'paying', 'ordered',
                                                   'rejected', 'failed', 'cancelled')),
    address_id     UUID REFERENCES addresses (id),
    subtotal_cents BIGINT NOT NULL DEFAULT 0,
    shipping_cents BIGINT NOT NULL DEFAULT 0,
    total_cents    BIGINT NOT NULL DEFAULT 0,
    currency       TEXT NOT NULL DEFAULT 'SGD',
    decision       TEXT NOT NULL DEFAULT '' CHECK (decision IN ('', 'AUTO_APPROVE', 'NEEDS_APPROVAL', 'REJECT')),
    failure_reason TEXT NOT NULL DEFAULT '',
    confirmed_by   UUID REFERENCES users (id),
    confirmed_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX purchase_requests_status_idx ON purchase_requests (status);
CREATE INDEX purchase_requests_created_at_idx ON purchase_requests (created_at DESC);

CREATE TABLE line_items (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id        UUID NOT NULL REFERENCES purchase_requests (id) ON DELETE CASCADE,
    position          INT NOT NULL DEFAULT 0,
    catalog_item_id   UUID REFERENCES catalog_items (id) ON DELETE SET NULL,
    description       TEXT NOT NULL,
    qty               INT NOT NULL CHECK (qty > 0),
    urgency           TEXT NOT NULL DEFAULT 'normal' CHECK (urgency IN ('normal', 'urgent')),
    policy_decision   TEXT NOT NULL DEFAULT '' CHECK (policy_decision IN ('', 'AUTO_APPROVE', 'NEEDS_APPROVAL', 'REJECT')),
    reasons           JSONB NOT NULL DEFAULT '[]', -- []domain.Reason
    selected_offer_id UUID, -- FK added below (offers references line_items)
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX line_items_request_idx ON line_items (request_id, position);

CREATE TABLE offers (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    line_item_id      UUID NOT NULL REFERENCES line_items (id) ON DELETE CASCADE,
    vendor_id         UUID REFERENCES vendors (id) ON DELETE SET NULL,
    merchant_name     TEXT NOT NULL,
    reap_product_id   TEXT NOT NULL,
    reap_variant_id   TEXT NOT NULL,
    title             TEXT NOT NULL,
    variant_name      TEXT NOT NULL DEFAULT '',
    image_url         TEXT NOT NULL DEFAULT '',
    url               TEXT NOT NULL DEFAULT '',
    unit_price_cents  BIGINT NOT NULL CHECK (unit_price_cents >= 0),
    currency          TEXT NOT NULL DEFAULT 'SGD',
    pack_size         INT NOT NULL DEFAULT 1 CHECK (pack_size > 0),
    shipping_cents    BIGINT NOT NULL DEFAULT 0,
    landed_cost_cents BIGINT NOT NULL DEFAULT 0,
    eta               TEXT NOT NULL DEFAULT '',
    available         BOOLEAN NOT NULL DEFAULT TRUE,
    rank              INT NOT NULL DEFAULT 0,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX offers_line_item_idx ON offers (line_item_id, rank);

ALTER TABLE line_items
    ADD CONSTRAINT line_items_selected_offer_fk FOREIGN KEY (selected_offer_id) REFERENCES offers (id) ON DELETE SET NULL;

CREATE TABLE approvals (
    id           UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id   UUID NOT NULL REFERENCES purchase_requests (id) ON DELETE CASCADE,
    kind         TEXT NOT NULL CHECK (kind IN ('policy', 'price_drift')),
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'approved', 'rejected')),
    reasons      JSONB NOT NULL DEFAULT '[]', -- []domain.Reason
    amount_cents BIGINT NOT NULL DEFAULT 0,
    prev_cents   BIGINT,
    approver_id  UUID REFERENCES users (id),
    comment      TEXT NOT NULL DEFAULT '',
    decided_at   TIMESTAMPTZ,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX approvals_status_idx ON approvals (status, created_at DESC);
-- At most one pending approval per request.
CREATE UNIQUE INDEX approvals_one_pending_idx ON approvals (request_id) WHERE status = 'pending';

CREATE TABLE payments (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    request_id       UUID NOT NULL REFERENCES purchase_requests (id) ON DELETE CASCADE,
    vendor_id        UUID REFERENCES vendors (id) ON DELETE SET NULL,
    merchant_name    TEXT NOT NULL,
    enrollment_id    UUID REFERENCES enrollments (id),
    reap_quote_id    TEXT NOT NULL DEFAULT '',
    reap_checkout_id TEXT NOT NULL DEFAULT '',
    reap_order_id    TEXT NOT NULL DEFAULT '',
    items_cents      BIGINT NOT NULL DEFAULT 0,
    shipping_cents   BIGINT NOT NULL DEFAULT 0,
    tax_cents        BIGINT NOT NULL DEFAULT 0,
    quoted_cents     BIGINT NOT NULL DEFAULT 0,
    final_cents      BIGINT,
    currency         TEXT NOT NULL DEFAULT 'SGD',
    status           TEXT NOT NULL CHECK (status IN ('quoting', 'quoted', 'requires_action', 'processing',
                                                     'completed', 'failed', 'expired')),
    approval_url     TEXT NOT NULL DEFAULT '',
    quote_expires_at TIMESTAMPTZ,
    idempotency_key  TEXT NOT NULL UNIQUE,
    error            TEXT NOT NULL DEFAULT '',
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX payments_request_idx ON payments (request_id);
CREATE INDEX payments_status_idx ON payments (status);
CREATE UNIQUE INDEX payments_checkout_idx ON payments (reap_checkout_id) WHERE reap_checkout_id <> '';

-- Append-only. The application never UPDATEs or DELETEs; the trigger enforces it.
CREATE TABLE audit_events (
    id         BIGSERIAL PRIMARY KEY,
    request_id UUID REFERENCES purchase_requests (id) ON DELETE SET NULL,
    actor_type TEXT NOT NULL CHECK (actor_type IN ('user', 'agent', 'system')),
    actor_id   TEXT NOT NULL DEFAULT '',
    type       TEXT NOT NULL,
    payload    JSONB NOT NULL DEFAULT '{}',
    at         TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX audit_events_request_idx ON audit_events (request_id, id);
CREATE INDEX audit_events_at_idx ON audit_events (at DESC);

CREATE FUNCTION audit_events_immutable() RETURNS trigger AS $$
BEGIN
    -- Allow the FK's ON DELETE SET NULL cascade (only request_id changes); block everything else.
    IF TG_OP = 'UPDATE' AND NEW.id = OLD.id AND NEW.type = OLD.type AND NEW.payload = OLD.payload
       AND NEW.actor_type = OLD.actor_type AND NEW.actor_id = OLD.actor_id AND NEW.at = OLD.at
       AND NEW.request_id IS NULL THEN
        RETURN NEW;
    END IF;
    RAISE EXCEPTION 'audit_events is append-only';
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER audit_events_no_update BEFORE UPDATE OR DELETE ON audit_events
    FOR EACH ROW EXECUTE FUNCTION audit_events_immutable();

CREATE TABLE chat_messages (
    id           BIGSERIAL PRIMARY KEY,
    session_id   TEXT NOT NULL,
    user_id      UUID REFERENCES users (id),
    role         TEXT NOT NULL CHECK (role IN ('system', 'user', 'assistant', 'tool')),
    content      TEXT NOT NULL DEFAULT '',
    tool_calls   JSONB,
    tool_call_id TEXT NOT NULL DEFAULT '',
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX chat_messages_session_idx ON chat_messages (session_id, id);
