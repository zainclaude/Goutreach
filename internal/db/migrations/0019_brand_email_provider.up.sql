-- The brand email cache becomes per-provider so switching the AI provider
-- (Claude vs Kimi) generates a fresh copy per brand instead of silently
-- reusing the other provider's cached email — required for A/B testing.
ALTER TABLE brand_emails ADD COLUMN provider TEXT NOT NULL DEFAULT 'claude';
ALTER TABLE brand_emails DROP CONSTRAINT brand_emails_user_id_domain_step_index_key;
ALTER TABLE brand_emails ADD CONSTRAINT brand_emails_user_domain_step_provider_key
    UNIQUE (user_id, domain, step_index, provider);
