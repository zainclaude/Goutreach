ALTER TABLE campaigns DROP COLUMN IF EXISTS approval_count;
ALTER TABLE campaigns DROP COLUMN IF EXISTS require_approval;
ALTER TABLE messages DROP COLUMN IF EXISTS approved;
ALTER TABLE messages DROP COLUMN IF EXISTS research_notes;
ALTER TABLE messages DROP COLUMN IF EXISTS template_used;
DROP TABLE IF EXISTS settings;
DROP TABLE IF EXISTS email_templates;
