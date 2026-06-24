-- Per-account reply-poller health, surfaced on the Accounts page so a mailbox
-- whose IMAP is failing (and therefore can't detect replies) is visible.
ALTER TABLE email_accounts ADD COLUMN IF NOT EXISTS imap_last_polled_at TIMESTAMPTZ;
ALTER TABLE email_accounts ADD COLUMN IF NOT EXISTS imap_last_error TEXT NOT NULL DEFAULT '';
