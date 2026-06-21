-- Email templates A–E, selected by the brand-research decision tree.
-- key: 'A' (TikTok Shop), 'B' (Amazon), 'C' (Meta ads), 'D' (big retail), 'E' (generic).
CREATE TABLE email_templates (
    id         BIGSERIAL PRIMARY KEY,
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    name       TEXT NOT NULL DEFAULT '',
    subject    TEXT NOT NULL DEFAULT '',
    body       TEXT NOT NULL DEFAULT '',
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, key)
);

-- Generic key/value settings, used for integration credentials (e.g. kalodata).
-- Secret values are AES-GCM encrypted by the application; is_secret marks them.
CREATE TABLE settings (
    user_id    BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    key        TEXT NOT NULL,
    value      TEXT NOT NULL DEFAULT '',
    is_secret  BOOLEAN NOT NULL DEFAULT false,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, key)
);

-- Record which template the research step selected for each message, plus the
-- reasoning, so previews and analytics can show why an email was written that way.
ALTER TABLE messages ADD COLUMN template_used TEXT NOT NULL DEFAULT '';
ALTER TABLE messages ADD COLUMN research_notes TEXT NOT NULL DEFAULT '';

-- Approval gate: a campaign can require the first N messages to be approved
-- before anything sends. Tracked per message.
ALTER TABLE messages ADD COLUMN approved BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE campaigns ADD COLUMN require_approval BOOLEAN NOT NULL DEFAULT true;
ALTER TABLE campaigns ADD COLUMN approval_count INT NOT NULL DEFAULT 5;
