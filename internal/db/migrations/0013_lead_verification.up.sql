-- Email-verification result per lead. Populated by a third-party verifier so we
-- can skip undeliverable addresses before sending (Apollo-sourced emails still
-- bounce as "address not found"). Status: unknown|valid|invalid|risky|catch_all.
ALTER TABLE leads ADD COLUMN IF NOT EXISTS verification_status TEXT NOT NULL DEFAULT 'unknown';
ALTER TABLE leads ADD COLUMN IF NOT EXISTS verified_at TIMESTAMPTZ;
