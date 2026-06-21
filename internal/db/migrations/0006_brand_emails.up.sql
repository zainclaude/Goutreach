-- Brand-level email cache. Once a domain has been routed (template discovered)
-- and an email written, it's saved here so other contacts at the same company
-- reuse it — no regeneration, no tokens. The name is stored tokenized
-- ({{first_name}} etc.) so it can be swapped per contact. Keyed per step so
-- follow-ups cache independently. "Write once, reuse forever" => INSERT only.
CREATE TABLE brand_emails (
    id             BIGSERIAL PRIMARY KEY,
    user_id        BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain         TEXT NOT NULL,            -- email domain = the company key
    step_index     INT  NOT NULL DEFAULT 0,
    brand_name     TEXT NOT NULL DEFAULT '', -- resolved brand name (display only)
    template_used  TEXT NOT NULL DEFAULT '',
    subject        TEXT NOT NULL,
    body           TEXT NOT NULL,
    research_notes TEXT NOT NULL DEFAULT '',
    created_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, domain, step_index)
);
