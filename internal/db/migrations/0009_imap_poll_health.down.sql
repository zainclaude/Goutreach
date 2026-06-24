ALTER TABLE email_accounts DROP COLUMN IF EXISTS imap_last_polled_at;
ALTER TABLE email_accounts DROP COLUMN IF EXISTS imap_last_error;
