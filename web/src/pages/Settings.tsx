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
        {msg && <p className="ok">{msg}</p>}
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
        <h3>Email verification</h3>
        <p className="muted">
          Verify lead emails through a third-party provider so undeliverable addresses (even Apollo-sourced
          ones that bounce as "address not found") are flagged and skipped at send time. Pick your provider,
          paste its API key, then use <b>Verify emails</b> on the Leads page. New CSV imports auto-verify.
        </p>
        <div style={{ marginBottom: 10 }}>
          <label>Provider</label>
          <select
            value={vals["email_verify_provider"] || "millionverifier"}
            onChange={(e) => setVals({ ...vals, email_verify_provider: e.target.value })}
          >
            <option value="millionverifier">MillionVerifier</option>
            <option value="zerobounce">ZeroBounce</option>
            <option value="neverbounce">NeverBounce</option>
            <option value="bouncer">Bouncer</option>
          </select>
          <div className="row" style={{ marginTop: 6 }}>
            <button onClick={() => save("email_verify_provider", false)}>Save provider</button>
          </div>
        </div>
        <div>
          <label>API key {existing["email_verify_api_key"] && <span className="tag">set</span>}</label>
          <div className="row">
            <input
              type="password"
              value={vals["email_verify_api_key"] || ""}
              placeholder={existing["email_verify_api_key"] ? "•••••••• (leave blank to keep)" : ""}
              onChange={(e) => setVals({ ...vals, email_verify_api_key: e.target.value })}
            />
            <button onClick={() => save("email_verify_api_key", true)}>Save key</button>
          </div>
        </div>
        {msg && <p className="ok">{msg}</p>}
      </div>
    </div>
  );
}
