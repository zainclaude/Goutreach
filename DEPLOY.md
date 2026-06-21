# Deploying PipelineBuilder

The app is a single Docker image (Go backend that also serves the built React
dashboard) plus a Postgres database. Pick one platform below.

After deploy, open the app URL, **sign up** (the first account becomes the owner
and signup then locks), then go to **Settings** to add your kalodata/fastmoss
logins and **Templates** to paste A–E.

Required environment variables on any platform:

| Var | Required | Notes |
|---|---|---|
| `DATABASE_URL` | yes | Postgres connection string (the platform's managed DB) |
| `ENCRYPTION_KEY` | yes | Any non-empty secret; hashed to a 32-byte key. Keep it stable — changing it makes stored mailbox passwords undecryptable |
| `JWT_SECRET` | yes | Any non-empty secret for session signing |
| `ANTHROPIC_API_KEY` | for AI | Enables email generation |
| `APP_URL` | recommended | Public URL, used in open/click tracking links (auto-detected on Render) |
| `PORT` / `HTTP_ADDR` | auto | The server binds `$PORT` if set, else `:8080` |

---

## Option A — Render (easiest, uses `render.yaml`)

1. Push this branch to GitHub.
2. In Render: **New + → Blueprint**, select the repo. Render reads `render.yaml`
   and creates the web service + free Postgres, and generates `ENCRYPTION_KEY`
   and `JWT_SECRET` for you.
3. After the first deploy, open the service → **Environment** and set
   `ANTHROPIC_API_KEY`. (Render injects the public URL automatically.)
4. Open the service URL.

Free Postgres on Render expires after ~90 days; upgrade the DB plan for anything
beyond a trial.

---

## Option B — Fly.io (uses `fly.toml`)

```bash
fly launch --no-deploy --copy-config
fly postgres create --name goutreach-db
fly postgres attach goutreach-db        # sets DATABASE_URL
fly secrets set \
  ANTHROPIC_API_KEY=sk-ant-... \
  ENCRYPTION_KEY=$(openssl rand -hex 24) \
  JWT_SECRET=$(openssl rand -hex 24)
fly secrets set APP_URL=https://<your-app>.fly.dev
fly deploy
```

---

## Option C — Any VPS / Docker host

```bash
git checkout claude/ai-cold-email-tool-yk07ob
cp .env.example .env   # fill in ANTHROPIC_API_KEY, ENCRYPTION_KEY, JWT_SECRET, APP_URL
docker compose up -d --build
```
Put a reverse proxy (Caddy/Nginx) with TLS in front, pointed at port 8080, and
set `APP_URL` to your https domain.

---

### What I need from you to deploy it for you
Tell me which platform and grant access (e.g. invite to the Render team / a Render
API key, or a Fly.io deploy token, or SSH to a VPS). With that I can run the
deploy and hand you the live URL. Without credentials I can only prepare the
config (done above) — you run the final step.
