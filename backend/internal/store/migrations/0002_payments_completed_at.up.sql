-- Monthly spend is bucketed by when a payment completed (Asia/Singapore calendar month).
-- PaymentRepo.Update stamps completed_at the first time a payment reaches status 'completed'.
ALTER TABLE payments ADD COLUMN completed_at TIMESTAMPTZ;
UPDATE payments SET completed_at = updated_at WHERE status = 'completed' AND completed_at IS NULL;
CREATE INDEX payments_completed_at_idx ON payments (completed_at) WHERE status = 'completed';
