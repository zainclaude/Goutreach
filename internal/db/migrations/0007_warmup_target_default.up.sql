-- Backfill accounts whose warmup_target_per_day was stored as 0 (e.g. saved via
-- the account-edit form before it defaulted the field). A 0 cap pinned the
-- warmup ramp at its 2/day floor forever, so it never grew with mailbox age.
UPDATE email_accounts SET warmup_target_per_day = 20
WHERE warmup_target_per_day IS NULL OR warmup_target_per_day <= 0;

ALTER TABLE email_accounts ALTER COLUMN warmup_target_per_day SET DEFAULT 20;
