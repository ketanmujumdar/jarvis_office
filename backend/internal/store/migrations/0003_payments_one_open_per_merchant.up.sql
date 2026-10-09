-- At most one open (not failed/expired) payment per request and merchant. This is the store-level
-- guard against a retried or resumed checkout job creating a second Reap checkout (double charge)
-- for a merchant that already has one.
-- Older duplicates that never reached a Reap checkout are retired first so the index can be built.
UPDATE payments p SET status = 'expired', error = 'superseded by a newer payment for the same merchant',
	updated_at = clock_timestamp()
WHERE p.status NOT IN ('failed', 'expired') AND p.reap_checkout_id = ''
	AND EXISTS (
		SELECT 1 FROM payments q
		WHERE q.request_id = p.request_id AND q.id <> p.id
			AND COALESCE(q.vendor_id::text, q.merchant_name) = COALESCE(p.vendor_id::text, p.merchant_name)
			AND q.status NOT IN ('failed', 'expired')
			AND (q.reap_checkout_id <> '' OR q.created_at > p.created_at));
CREATE UNIQUE INDEX payments_open_per_merchant_idx ON payments (request_id, COALESCE(vendor_id::text, merchant_name))
	WHERE status NOT IN ('failed', 'expired');

-- Generation of the Reap quote idempotency key. A quote creates no charge, so after
-- QUOTE_TEMPORARILY_UNAVAILABLE (which Reap may replay for the same key) or a stale/replaced quote
-- the next attempt uses a fresh key: <idempotency_key>:quote:<quote_attempt>.
ALTER TABLE payments ADD COLUMN quote_attempt INT NOT NULL DEFAULT 0;
