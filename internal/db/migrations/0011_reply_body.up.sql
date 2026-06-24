-- Store the lead's actual reply text so the Inbox can show it (previously only
-- the original outbound email was kept).
ALTER TABLE messages ADD COLUMN IF NOT EXISTS reply_body TEXT NOT NULL DEFAULT '';

-- Reset the IMAP UID watermark so the next poll re-scans the last 30 days and
-- backfills reply bodies for replies that were detected before this column
-- existed. (One-time re-scan; warmup auto-replies stay suppressed for seen mail.)
UPDATE email_accounts SET imap_last_uid = 0;
