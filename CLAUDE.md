# PipelineBuilder (Goutreach) — project notes

AI cold-email tool for Zainith Agency (Zain, zain@zainith.agency). Go backend
(`cmd/server`, `internal/`) + React frontend (`web/`), Postgres, deployed on
Render.

## Infrastructure facts

- **Sending inboxes were purchased from PrimeForge** (not Maildoso — the
  Maildoso integration exists in-app but wasn't used for these). They are
  Google-hosted mailboxes connected via Google app passwords: ~20 inboxes
  (zain@/zaina@/zainali@/zain.ali@) across startsocialcommerce.com,
  joinsocialcommerce.com, growsocialcommerce.com, freshsocialcommerce.com,
  topsocialcommerce.com, easysocialcommerce.com.
- Domains are registered/DNS-managed via the in-app **Porkbun** integration.
- Production: Render service `goutreach` (srv-d8tc18jaml3c73a9qr40, team
  tea-d8rpg3po3t8c73ecmks0), https://goutreach.onrender.com, Postgres
  `goutreach-db` (basic-256mb).

## Deploying

Render auto-deploys the branch `claude/ai-cold-email-tool-yk07ob` (see
render.yaml). Development happens on a session work branch; deploying = fast-
forward pushing the work branch to the deploy branch. Migrations run
automatically on boot. Deploys restart the process; interrupted preview
generations self-heal via the sender's recovery sweep.

## AI generation

- Two providers: Claude (`claude-sonnet-5`, default, server-side web search)
  and Kimi via Moonshot's Anthropic-compatible endpoint (per-user setting
  `ai_provider`; needs `kimi_api_key` + `serper_api_key` for web search).
  Kimi-generated emails are tagged `[kimi]` in research notes for A/B.
- Brand-level email cache is per provider (domain + step + provider).
- TikTok Shop research: FastMoss primary (an authoritative "no" when its
  search finds no matching shop), kalodata scrape fallback. Zero metrics are
  never treated as verified.
