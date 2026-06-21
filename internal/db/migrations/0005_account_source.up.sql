-- Track where a connected mailbox came from so done-for-you (vendor-provisioned)
-- inboxes can be filtered and synced idempotently. source: manual|csv|oauth|maildoso.
-- external_id holds the vendor's mailbox id (empty for manually added accounts).
ALTER TABLE email_accounts ADD COLUMN source TEXT NOT NULL DEFAULT 'manual';
ALTER TABLE email_accounts ADD COLUMN external_id TEXT NOT NULL DEFAULT '';
