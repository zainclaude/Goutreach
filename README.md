# Goutreach

An AI-personalized cold email platform (an Instantly.ai-style tool) that connects email
accounts, warms them up, imports leads, sends **fully AI-generated** cold emails to your ICP,
and tracks analytics (sent, opens, replies, bounces).

Built with a **Go** backend + workers, **Postgres**, and a **React/TypeScript** dashboard.

## What it does

- **Connect mailboxes** via SMTP (sending) + IMAP (reply/bounce tracking) — any provider.
- **Warm up** inboxes with a peer-network ramp across your own connected accounts (auto
  open + occasional auto-reply) to build sending reputation.
- **Import leads** (CSV or manual) with custom fields.
- **AI email generation** that follows a research decision tree per lead:
  1. Is the brand on **TikTok Shop**? (kalodata, falling back to fastmoss) → Template **A**
  2. On **Amazon**? → Template **B**
  3. Running **Meta ads**? → Template **C**
  4. Big **retail presence**? → Template **D**, else generic Template **E**

  Claude (`claude-opus-4-8`) does the research with web search/fetch + a TikTok Shop tool,
  selects the matching template, and personalizes it for the contact.
- **Preview before sending** — generate the first N emails of a campaign, review/edit/approve,
  then launch.
- **Round-robin sending** — 1 email per account, cycling through the pool, respecting each
  account's configurable **daily cap** and the campaign send window.
- **Unified inbox** of lead replies (warmup excluded) with in-thread reply.
- **Per-account stats**: sent today/lifetime, replies today/lifetime, reply rate.

## Architecture

```
cmd/server            entrypoint: config, db, router, background workers
internal/config       env configuration
internal/db           pgx pool + embedded migration runner
internal/crypto       AES-256-GCM for secrets at rest (SMTP/IMAP/integration passwords)
internal/auth         JWT auth + bcrypt
internal/store        all Postgres models + queries
internal/mailer       SMTP send (go-mail) + IMAP poll (go-imap)
internal/ai           Claude tool-use loop: research decision tree → template → email
internal/research     TikTok Shop checker (kalodata → fastmoss) reading encrypted settings
internal/sender       scheduler: generate + send, round-robin, caps, windows, threading
internal/tracking     open pixel, click redirect, IMAP reply/bounce poller, warmup replies
internal/warmup       peer-network warmup ramp
internal/api          HTTP handlers (chi)
web/                  React + Vite + TypeScript dashboard
```

## Running locally

Requirements: Go 1.24+, Node 22+, Postgres 16 (or Docker).

```bash
cp .env.example .env        # then edit values
# Start Postgres (Docker):
docker compose up -d db
# Build the frontend:
cd web && npm install && npm run build && cd ..
# Run the server (serves API + built dashboard on :8080):
export $(grep -v '^#' .env | xargs)
go run ./cmd/server
```

Or run everything with Docker: `docker compose up --build`.

Open http://localhost:8080, create the first account, and connect a mailbox.

### Frontend dev mode

```bash
cd web && npm run dev    # Vite on :5173, proxies /api + /t to :8080
```

## Configuration

| Env var | Purpose |
|---|---|
| `DATABASE_URL` | Postgres connection string |
| `APP_URL` | Public base URL used in tracking links |
| `ANTHROPIC_API_KEY` | Claude API key (required for AI generation) |
| `ENCRYPTION_KEY` | 32+ byte key for at-rest encryption |
| `JWT_SECRET` | Dashboard session signing secret |

Kalodata / Fastmoss credentials are entered in the **Settings** page and stored encrypted —
they are never committed to source.

## Tests

```bash
go test ./...
```

## Notes & status

- Open-rate tracking is lossy and can hurt deliverability — it's an off-by-default per-campaign toggle.
- The kalodata/fastmoss TikTok Shop lookups are login-gated, JS-heavy dashboards; the integration
  framework (credential storage + fallback + tool wiring) is complete, and the live lookup
  (`internal/research`) is the single place to finalize the scraping/automation. Until then the
  step returns "unknown" and the decision tree falls through to the Amazon/Meta/retail checks
  (which run via Claude's web tools).
- Email **templates A–E** are edited in the Templates page; leave any blank to let Claude write
  freely for that branch.
