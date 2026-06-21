-- Users: dashboard login (single workspace MVP)
CREATE TABLE users (
    id            BIGSERIAL PRIMARY KEY,
    email         TEXT NOT NULL UNIQUE,
    password_hash TEXT NOT NULL,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Email accounts: SMTP for sending, IMAP for reply/bounce tracking.
-- Passwords are stored encrypted (AES-256-GCM) by the application.
CREATE TABLE email_accounts (
    id                    BIGSERIAL PRIMARY KEY,
    user_id               BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email                 TEXT NOT NULL,
    from_name             TEXT NOT NULL DEFAULT '',
    smtp_host             TEXT NOT NULL,
    smtp_port             INT  NOT NULL DEFAULT 587,
    smtp_username         TEXT NOT NULL,
    smtp_password_enc     TEXT NOT NULL,
    imap_host             TEXT NOT NULL,
    imap_port             INT  NOT NULL DEFAULT 993,
    imap_username         TEXT NOT NULL,
    imap_password_enc     TEXT NOT NULL,
    daily_limit           INT  NOT NULL DEFAULT 30,
    warmup_enabled        BOOLEAN NOT NULL DEFAULT false,
    warmup_target_per_day INT  NOT NULL DEFAULT 20,
    status                TEXT NOT NULL DEFAULT 'active', -- active | error | paused
    last_error            TEXT NOT NULL DEFAULT '',
    created_at            TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, email)
);

-- Leads: prospects to contact.
CREATE TABLE leads (
    id            BIGSERIAL PRIMARY KEY,
    user_id       BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    email         TEXT NOT NULL,
    first_name    TEXT NOT NULL DEFAULT '',
    last_name     TEXT NOT NULL DEFAULT '',
    company       TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL DEFAULT '',
    custom_fields JSONB NOT NULL DEFAULT '{}'::jsonb,
    status        TEXT NOT NULL DEFAULT 'active',
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (user_id, email)
);

-- Campaigns: an outreach effort with a brief fed to Claude and a sequence of steps.
CREATE TABLE campaigns (
    id              BIGSERIAL PRIMARY KEY,
    user_id         BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name            TEXT NOT NULL,
    brief           TEXT NOT NULL DEFAULT '',   -- ICP + offer + tone, fed to Claude
    status          TEXT NOT NULL DEFAULT 'draft', -- draft | running | paused
    timezone        TEXT NOT NULL DEFAULT 'UTC',
    send_start_hour INT  NOT NULL DEFAULT 9,
    send_end_hour   INT  NOT NULL DEFAULT 17,
    send_weekdays   INT[] NOT NULL DEFAULT '{1,2,3,4,5}'::int[], -- 0=Sun .. 6=Sat
    daily_cap       INT  NOT NULL DEFAULT 100,
    track_opens     BOOLEAN NOT NULL DEFAULT false,
    track_clicks    BOOLEAN NOT NULL DEFAULT false,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Campaign steps: the sequence. Step 0 is the initial email (delay 0).
CREATE TABLE campaign_steps (
    id          BIGSERIAL PRIMARY KEY,
    campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    step_index  INT NOT NULL,
    delay_days  INT NOT NULL DEFAULT 0,
    angle       TEXT NOT NULL DEFAULT '', -- per-step instruction for Claude
    UNIQUE (campaign_id, step_index)
);

-- Which inboxes a campaign rotates through.
CREATE TABLE campaign_accounts (
    campaign_id BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    account_id  BIGINT NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    PRIMARY KEY (campaign_id, account_id)
);

-- Lead enrollment in a campaign.
CREATE TABLE campaign_leads (
    id           BIGSERIAL PRIMARY KEY,
    campaign_id  BIGINT NOT NULL REFERENCES campaigns(id) ON DELETE CASCADE,
    lead_id      BIGINT NOT NULL REFERENCES leads(id) ON DELETE CASCADE,
    current_step INT NOT NULL DEFAULT 0,
    status       TEXT NOT NULL DEFAULT 'active', -- active | replied | bounced | finished | paused
    next_send_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (campaign_id, lead_id)
);
CREATE INDEX idx_campaign_leads_due ON campaign_leads (status, next_send_at);

-- Generated + sent emails.
CREATE TABLE messages (
    id               BIGSERIAL PRIMARY KEY,
    campaign_lead_id BIGINT NOT NULL REFERENCES campaign_leads(id) ON DELETE CASCADE,
    step_index       INT NOT NULL,
    account_id       BIGINT REFERENCES email_accounts(id) ON DELETE SET NULL,
    subject          TEXT NOT NULL DEFAULT '',
    body             TEXT NOT NULL DEFAULT '',
    message_id       TEXT NOT NULL DEFAULT '',   -- RFC822 Message-ID we set on send
    in_reply_to      TEXT NOT NULL DEFAULT '',
    references_hdr   TEXT NOT NULL DEFAULT '',
    status           TEXT NOT NULL DEFAULT 'queued', -- queued | generating | generated | sent | failed | replied | bounced
    error            TEXT NOT NULL DEFAULT '',
    sent_at          TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_messages_message_id ON messages (message_id);
CREATE INDEX idx_messages_campaign_lead ON messages (campaign_lead_id);

-- Tracking + engagement events.
CREATE TABLE events (
    id         BIGSERIAL PRIMARY KEY,
    message_id BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    type       TEXT NOT NULL, -- open | click | reply | bounce
    metadata   JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_events_message ON events (message_id, type);

-- Warmup mail exchanged between the user's own inboxes.
CREATE TABLE warmup_messages (
    id              BIGSERIAL PRIMARY KEY,
    from_account_id BIGINT NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    to_account_id   BIGINT NOT NULL REFERENCES email_accounts(id) ON DELETE CASCADE,
    subject         TEXT NOT NULL DEFAULT '',
    message_id      TEXT NOT NULL DEFAULT '',
    status          TEXT NOT NULL DEFAULT 'sent', -- sent | received | replied
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_warmup_message_id ON warmup_messages (message_id);
