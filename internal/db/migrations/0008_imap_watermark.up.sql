-- Per-account IMAP UID watermark. The reply poller now reads messages with UID
-- greater than this value instead of only UNSEEN mail, so a reply that was
-- already opened in the mailbox (e.g. read in Gmail) is still detected.
ALTER TABLE email_accounts ADD COLUMN IF NOT EXISTS imap_last_uid BIGINT NOT NULL DEFAULT 0;
