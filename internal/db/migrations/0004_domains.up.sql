-- Sending domains managed by PipelineBuilder. A domain is purchased (or imported)
-- via a registrar (Porkbun), has its cold-email DNS applied, and is later verified
-- with Google Workspace so mailboxes can be provisioned on it (phase 2).
CREATE TABLE domains (
    id          BIGSERIAL PRIMARY KEY,
    user_id     BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    domain      TEXT NOT NULL,
    registrar   TEXT NOT NULL DEFAULT 'porkbun',
    -- pending | purchased | dns_ready | verified | error
    status      TEXT NOT NULL DEFAULT 'pending',
    dns_applied BOOLEAN NOT NULL DEFAULT false,
    -- DKIM TXT value supplied by Google Workspace (filled in during phase 2).
    dkim_record TEXT NOT NULL DEFAULT '',
    last_error  TEXT NOT NULL DEFAULT '',
    notes       TEXT NOT NULL DEFAULT '',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, domain)
);
