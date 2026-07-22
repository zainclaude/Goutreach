ALTER TABLE brand_emails DROP CONSTRAINT brand_emails_user_domain_step_provider_key;
ALTER TABLE brand_emails DROP COLUMN provider;
ALTER TABLE brand_emails ADD CONSTRAINT brand_emails_user_id_domain_step_index_key
    UNIQUE (user_id, domain, step_index);
