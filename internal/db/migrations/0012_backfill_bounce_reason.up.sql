-- Re-scan inboxes so existing bounces get their reason captured (stored in
-- messages.error and shown in the UI). Resets the IMAP UID watermark; the
-- next poll re-reads the last 30 days and backfills bounce reasons.
UPDATE email_accounts SET imap_last_uid = 0;
