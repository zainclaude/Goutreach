import { useEffect, useState } from "react";
import { api, Setting } from "../api";

// Integration credential fields. Passwords are stored encrypted (is_secret).
const FIELDS: { key: string; label: string; secret: boolean }[] = [
  { key: "fastmoss_client_secret", label: "FastMoss API client_secret (primary)", secret: true },
  { key: "kalodata_email", label: "Kalodata email (fallback scrape)", secret: false },
  { key: "kalodata_password", label: "Kalodata password (fallback scrape)", secret: true },
];

export default function Settings() {
  const [existing, setExisting] = useState<Record<string, Setting>>({});
  const [vals, setVals] = useState<Record<string, string>>({});
  const [msg, setMsg] = useState("");

  const load = () => api.get<Setting[]>("/settings").then((s) => {
    const m: Record<string, Setting> = {};
    (s || []).forEach((x) => (m[x.key] = x));
    setExisting(m);
    const v: Record<string, string> = {};
    (s || []).forEach((x) => { if (!x.is_secret) v[x.key] = x.value; });
    setVals((prev) => ({ ...v, ...prev }));
  });
  useEffect(() => { load(); }, []);

  const save = async (key: string, secret: boolean) => {
    await api.put("/settings", { key, value: vals[key] || "", is_secret: secret });
    setMsg(`Saved ${key}`); load();
  };

  return (
    <div>
      <h2>Settings</h2>
      <div className="card">
        <h3>TikTok Shop data providers</h3>
        <p className="muted">
          Step 1 of email research pulls the brand's TikTok Shop metrics (GMV, # creators, # videos)
          from the <b>FastMoss OpenAPI</b> (primary), falling back to scraping <b>Kalodata</b> with the
          login below. Secrets are encrypted at rest and never shown back. Get your FastMoss
          client_secret from its Console → API keys.
        </p>
        {FIELDS.map((f) => (
          <div key={f.key} style={{ marginBottom: 10 }}>
            <label>{f.label} {existing[f.key] && f.secret && <span className="tag">set</span>}</label>
            <div className="row">
              <input
                type={f.secret ? "password" : "text"}
                value={vals[f.key] || ""}
                placeholder={f.secret && existing[f.key] ? "•••••••• (leave blank to keep)" : ""}
                onChange={(e) => setVals({ ...vals, [f.key]: e.target.value })}
              />
              <button onClick={() => save(f.key, f.secret)}>Save</button>
            </div>
          </div>
        ))}
        <div className="row" style={{ marginTop: 6 }}>
          <button
            className="secondary"
            onClick={async () => {
              setMsg("Testing FastMoss key…");
              try { await api.get("/research/fastmoss/ping"); setMsg("✓ FastMoss key works — live TikTok Shop data is available."); }
              catch (e: any) { setMsg("✗ " + e.message); }
            }}
          >
            Test FastMoss key
          </button>
          <input
            style={{ width: 220 }}
            placeholder="Brand name (e.g. HealthForce)"
            value={vals["__fm_test_brand"] || ""}
            onChange={(e) => setVals({ ...vals, __fm_test_brand: e.target.value })}
          />
          <button
            className="secondary"
            onClick={async () => {
              const b = (vals["__fm_test_brand"] || "").trim();
              if (!b) { setMsg("✗ Enter a brand name to look up."); return; }
              setMsg(`Looking up "${b}" on FastMoss…`);
              try {
                const r = await api.get<{ found: boolean; shop_name: string; revenue_usd: number; creators: number; videos: number }>(
                  `/research/fastmoss/ping?brand=${encodeURIComponent(b)}`);
                setMsg(r.found
                  ? `✓ Found "${r.shop_name}": $${Math.round(r.revenue_usd).toLocaleString()} GMV · ${r.creators} creators · ${r.videos} videos (30d)`
                  : `✗ FastMoss has no US shop matching "${b}" — emails for this brand will use fallback wording.`);
              } catch (e: any) { setMsg("✗ " + e.message); }
            }}
          >
            Test brand lookup
          </button>
        </div>
        {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
      </div>

      <div className="card">
        <h3>AI generation model</h3>
        <p className="muted">
          Which model researches brands and writes your emails. <b>Claude Sonnet 5</b> is the default
          (uses the server's Anthropic key, built-in web search). <b>Kimi K2.6</b> (Moonshot) is the
          cheaper open-source alternative — it needs a Moonshot API key (platform.moonshot.ai → API keys)
          plus a <b>Serper.dev</b> key for web research, since Kimi has no built-in search.
          Reply classification always uses Claude. Kimi-generated emails are tagged <code>[kimi]</code> in
          their research notes so you can compare quality when A/B testing.
        </p>
        <label>Provider</label>
        <select
          value={vals["ai_provider"] || "claude"}
          onChange={async (e) => {
            const v = e.target.value;
            setVals({ ...vals, ai_provider: v });
            await api.put("/settings", { key: "ai_provider", value: v, is_secret: false });
            setMsg(v === "kimi"
              ? "Saved — new emails will be generated with Kimi (make sure both keys below are set)"
              : "Saved — new emails will be generated with Claude");
          }}
        >
          <option value="claude">Claude Sonnet 5 (default)</option>
          <option value="kimi">Kimi K2.6 (Moonshot)</option>
        </select>
        <label style={{ marginTop: 10, display: "block" }}>
          Kimi (Moonshot) API key {existing["kimi_api_key"] && <span className="tag">set</span>}
        </label>
        <div className="row">
          <input
            type="password"
            value={vals["kimi_api_key"] || ""}
            placeholder={existing["kimi_api_key"] ? "•••••••• (leave blank to keep)" : "sk-..."}
            onChange={(e) => setVals({ ...vals, kimi_api_key: e.target.value })}
          />
          <button onClick={() => save("kimi_api_key", true)}>Save key</button>
        </div>
        <label style={{ marginTop: 10, display: "block" }}>Kimi model</label>
        <div className="row">
          <input
            value={vals["kimi_model"] || ""}
            placeholder="kimi-k2.6 (default)"
            onChange={(e) => setVals({ ...vals, kimi_model: e.target.value })}
          />
          <button onClick={() => save("kimi_model", false)}>Save</button>
        </div>
        <label style={{ marginTop: 10, display: "block" }}>
          Serper.dev API key (web search for Kimi) {existing["serper_api_key"] && <span className="tag">set</span>}
        </label>
        <div className="row">
          <input
            type="password"
            value={vals["serper_api_key"] || ""}
            placeholder={existing["serper_api_key"] ? "•••••••• (leave blank to keep)" : ""}
            onChange={(e) => setVals({ ...vals, serper_api_key: e.target.value })}
          />
          <button onClick={() => save("serper_api_key", true)}>Save key</button>
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button
            className="secondary"
            onClick={async () => {
              setMsg("Testing Moonshot key (sends a tiny generation request)…");
              try {
                const r = await api.get<{ ok: boolean; model: string; reply: string }>("/ai/kimi/ping");
                setMsg(`✓ Moonshot key works — ${r.model} replied "${r.reply}"`);
              } catch (e: any) { setMsg("✗ " + e.message); }
            }}
          >
            Test Kimi key
          </button>
          <button
            className="secondary"
            onClick={async () => {
              setMsg("Fetching models your Moonshot key can use…");
              try {
                const r = await api.get<{ ok: boolean; models: string[] }>("/ai/kimi/models");
                setMsg(`✓ Models available to your key: ${r.models.join(", ")} — paste one into the Kimi model field.`);
              } catch (e: any) { setMsg("✗ " + e.message); }
            }}
          >
            List Kimi models
          </button>
          <button
            className="secondary"
            onClick={async () => {
              setMsg("Testing Serper key (runs one real search)…");
              try {
                const r = await api.get<{ ok: boolean; sample: string }>("/ai/serper/ping");
                setMsg(`✓ Serper key works — web search is available. ${r.sample}`);
              } catch (e: any) { setMsg("✗ " + e.message); }
            }}
          >
            Test Serper key
          </button>
        </div>
        {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
      </div>

      <div className="card">
        <h3>Blacklist (never email)</h3>
        <p className="muted">
          One domain per line (or comma-separated). Leads on these domains are blocked when importing/adding,
          and skipped at send time if added later. Email addresses work too — only the domain is used
          (e.g. <code>competitor.com</code> or <code>joe@competitor.com</code>).
        </p>
        <textarea
          style={{ minHeight: 120, fontFamily: "monospace" }}
          placeholder={"competitor.com\nexample.net"}
          value={vals["blacklist_domains"] || ""}
          onChange={(e) => setVals({ ...vals, blacklist_domains: e.target.value })}
        />
        <div className="row" style={{ marginTop: 8 }}>
          <button onClick={() => save("blacklist_domains", false)}>Save blacklist</button>
        </div>
      </div>

      <div className="card">
        <h3>Reply notifications</h3>
        <p className="muted">
          When a lead replies in your inbox, PipelineBuilder emails these addresses so you and your
          salesperson know there's a response to reply to. One per line, or comma-separated.
        </p>
        <textarea
          style={{ minHeight: 80, fontFamily: "monospace" }}
          placeholder={"you@youragency.com\nsalesperson@youragency.com"}
          value={vals["notification_emails"] || ""}
          onChange={(e) => setVals({ ...vals, notification_emails: e.target.value })}
        />
        <div className="row" style={{ marginTop: 8 }}>
          <button onClick={() => save("notification_emails", false)}>Save notification emails</button>
        </div>
      </div>

      <div className="card">
        <h3>Email verification (ZeroBounce)</h3>
        <p className="muted">
          Verify lead emails through <b>ZeroBounce</b> so undeliverable addresses (even Apollo-sourced ones
          that bounce as "address not found") are flagged and skipped at send time. Paste your ZeroBounce API
          key below, then use <b>Verify emails</b> on the Leads page. New CSV imports auto-verify.
          Get your key at zerobounce.net → Settings → API.
        </p>
        <label>ZeroBounce API key {existing["email_verify_api_key"] && <span className="tag">set</span>}</label>
        <div className="row">
          <input
            type="password"
            value={vals["email_verify_api_key"] || ""}
            placeholder={existing["email_verify_api_key"] ? "•••••••• (leave blank to keep)" : ""}
            onChange={(e) => setVals({ ...vals, email_verify_api_key: e.target.value })}
          />
          <button onClick={() => save("email_verify_api_key", true)}>Save key</button>
        </div>
        <label style={{ display: "flex", alignItems: "center", gap: 8, marginTop: 14 }}>
          <input
            type="checkbox"
            style={{ width: "auto" }}
            checked={(vals["require_verified_send"] ?? "1") !== "0"}
            onChange={async (e) => {
              const val = e.target.checked ? "1" : "0";
              setVals({ ...vals, require_verified_send: val });
              await api.put("/settings", { key: "require_verified_send", value: val, is_secret: false });
              setMsg("Saved");
            }}
          />
          Only email verified leads — hold any lead until it's been verified (unverified leads wait; invalid ones are skipped)
        </label>
        {msg && <p className="ok">{msg}</p>}
      </div>

      <div className="card">
        <h3>Newsletter (beehiiv)</h3>
        <p className="muted">
          When a reply is AI-classified as <b>interested</b>, the lead is automatically added to your
          beehiiv newsletter. Grab both values from beehiiv → Settings → API (the publication ID starts
          with <code>pub_</code>).
        </p>
        <label>beehiiv API key {existing["beehiiv_api_key"] && <span className="tag">set</span>}</label>
        <div className="row">
          <input
            type="password"
            value={vals["beehiiv_api_key"] || ""}
            placeholder={existing["beehiiv_api_key"] ? "•••••••• (leave blank to keep)" : ""}
            onChange={(e) => setVals({ ...vals, beehiiv_api_key: e.target.value })}
          />
          <button onClick={() => save("beehiiv_api_key", true)}>Save key</button>
        </div>
        <label style={{ marginTop: 10, display: "block" }}>Publication ID</label>
        <div className="row">
          <input
            value={vals["beehiiv_publication_id"] || ""}
            placeholder="pub_00000000-0000-0000-0000-000000000000"
            onChange={(e) => setVals({ ...vals, beehiiv_publication_id: e.target.value })}
          />
          <button onClick={() => save("beehiiv_publication_id", false)}>Save</button>
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <button
            className="secondary"
            onClick={async () => {
              setMsg("");
              try {
                const r = await api.post<{ synced: number; total: number; errors: string[] }>("/replies/beehiiv-sync");
                setMsg(r.errors.length > 0
                  ? `✗ Synced ${r.synced}/${r.total} — ${r.errors.join("; ")}`
                  : `✓ Synced ${r.synced} interested lead(s) to beehiiv`);
              } catch (e: any) { setMsg("✗ " + e.message); }
            }}
            title="Adds every interested replier (past and present) to the newsletter — safe to run repeatedly"
          >
            Sync interested replies now
          </button>
        </div>
        {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
      </div>
    </div>
  );
}
