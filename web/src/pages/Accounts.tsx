import { useEffect, useState } from "react";
import { api, uploadAccountsCSV, Account, AccountStat, WarmupStat } from "../api";

const blank = {
  provider: "gmail", email: "", from_name: "", password: "",
  smtp_host: "", smtp_port: 587, smtp_username: "", smtp_password: "",
  imap_host: "", imap_port: 993, imap_username: "", imap_password: "",
  daily_limit: 30, warmup_enabled: true, warmup_target_per_day: 20,
  skip_verify: false,
};

export default function Accounts() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [stats, setStats] = useState<Record<number, AccountStat>>({});
  const [warmup, setWarmup] = useState<WarmupStat[]>([]);
  const [form, setForm] = useState({ ...blank });
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);
  const [googleEnabled, setGoogleEnabled] = useState(false);
  const [importSkipVerify, setImportSkipVerify] = useState(false);

  const load = () => {
    api.get<Account[]>("/accounts").then((a) => setAccounts(a || []));
    api.get<AccountStat[]>("/accounts/stats").then((s) => {
      const m: Record<number, AccountStat> = {};
      (s || []).forEach((x) => (m[x.account_id] = x));
      setStats(m);
    });
    api.get<WarmupStat[]>("/accounts/warmup-stats").then((w) => setWarmup(w || [])).catch(() => {});
  };
  useEffect(() => {
    load();
    api.get<{ google_enabled: boolean }>("/me").then((m) => setGoogleEnabled(m.google_enabled)).catch(() => {});
    const p = new URLSearchParams(window.location.search);
    if (p.get("connected")) setMsg("✓ Google account connected");
    if (p.get("error")) setMsg("✗ " + p.get("error"));
  }, []);

  const connectGoogle = async () => {
    setMsg("");
    try {
      const r = await api.get<{ url: string }>("/oauth/google/start");
      window.location.href = r.url;
    } catch (e: any) { setMsg("✗ " + e.message); }
  };

  const importAccounts = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (!f) return;
    setMsg(importSkipVerify ? "Importing accounts (no verification)…" : "Importing & verifying accounts…");
    try {
      const r = await uploadAccountsCSV(f, importSkipVerify);
      let m = `✓ Added ${r.added}`;
      if (r.unverified) m += `, ${r.unverified} saved unverified`;
      if (r.failed) m += `, ${r.failed} failed`;
      const notes = [...(r.warnings || []), ...(r.errors || [])];
      if (notes.length) m += ": " + notes.map((x: any) => `${x.email} (${x.error})`).join("; ");
      setMsg(m);
      load();
    } catch (e: any) { setMsg("✗ " + e.message); }
    finally { e.target.value = ""; }
  };

  const set = (k: string, v: any) => setForm({ ...form, [k]: v });

  const verify = async () => {
    setBusy(true); setMsg("");
    try { await api.post("/accounts/verify", form); setMsg("✓ Credentials verified"); }
    catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };
  const create = async () => {
    setBusy(true); setMsg("");
    try { await api.post("/accounts", form); setForm({ ...blank }); setMsg("✓ Account added"); load(); }
    catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };
  const remove = async (id: number) => {
    if (!confirm("Delete this account?")) return;
    await api.del(`/accounts/${id}`); load();
  };
  const saveSettings = async (a: Account, daily: number, warm: boolean, target: number) => {
    await api.patch(`/accounts/${a.id}`, { from_name: a.from_name, daily_limit: daily, warmup_enabled: warm, warmup_target_per_day: target });
    load();
  };

  return (
    <div>
      <h2>Email Accounts</h2>

      <div className="card">
        <table>
          <thead><tr>
            <th>Email</th><th>Daily cap</th><th>Sent today</th><th>Sent total</th>
            <th>Replies today</th><th>Replies total</th><th>Reply rate</th><th>Warmup</th><th>Reply polling</th><th>Status</th><th></th>
          </tr></thead>
          <tbody>
            {accounts.map((a) => {
              const s = stats[a.id];
              return (
                <tr key={a.id}>
                  <td>{a.email}<div className="muted">{a.from_name}</div></td>
                  <td>
                    <input style={{ width: 70 }} type="number" defaultValue={a.daily_limit}
                      onBlur={(e) => saveSettings(a, Number(e.target.value), a.warmup_enabled, a.warmup_target_per_day)} />
                  </td>
                  <td>{s?.sent_today ?? 0}</td>
                  <td>{s?.sent_lifetime ?? 0}</td>
                  <td>{s?.replies_today ?? 0}</td>
                  <td>{s?.replies_lifetime ?? 0}</td>
                  <td>{s ? (s.reply_rate * 100).toFixed(1) + "%" : "—"}</td>
                  <td>
                    <span className={a.warmup_enabled ? "badge active" : "badge"}>{a.warmup_enabled ? "on" : "off"}</span>{" "}
                    <button className="secondary" style={{ padding: "2px 8px", fontSize: 12 }}
                      onClick={() => saveSettings(a, a.daily_limit, !a.warmup_enabled, a.warmup_target_per_day)}>
                      {a.warmup_enabled ? "Turn off" : "Turn on"}
                    </button>
                  </td>
                  <td><PollHealth account={a} /></td>
                  <td>
                    <span className={`badge ${a.status}`} title={a.last_error || undefined}>{a.status}</span>
                    {a.status !== "active" && a.last_error && (
                      <div className="muted" style={{ fontSize: 11, maxWidth: 260, marginTop: 2 }}>{a.last_error}</div>
                    )}
                  </td>
                  <td><button className="danger" onClick={() => remove(a.id)}>×</button></td>
                </tr>
              );
            })}
            {accounts.length === 0 && <tr><td colSpan={11} className="muted">No accounts yet.</td></tr>}
          </tbody>
        </table>
      </div>

      {warmup.length > 0 && (
        <div className="card">
          <h3>Warmup results <span className="muted" style={{ fontWeight: 400, fontSize: 13 }}>(last 30 days)</span></h3>
          <table>
            <thead><tr>
              <th>Email</th><th>Warmup</th><th>Sent today</th><th>Sent (30d)</th><th>Inbox</th><th>Spam</th><th>Inbox rate</th>
            </tr></thead>
            <tbody>
              {warmup.map((w) => (
                <tr key={w.account_id}>
                  <td>{w.email}</td>
                  <td>{w.warmup_enabled ? <span className="badge active">on</span> : <span className="muted">off</span>}</td>
                  <td>{w.sent_today}</td>
                  <td>{w.sent}</td>
                  <td>{w.inbox}</td>
                  <td>{w.spam > 0 ? <span className="err">{w.spam}</span> : 0}{w.pending > 0 && <span className="muted"> (+{w.pending} pending)</span>}</td>
                  <td>{(w.inbox + w.spam) > 0 ? (w.inbox_rate * 100).toFixed(0) + "%" : "—"}</td>
                </tr>
              ))}
            </tbody>
          </table>
          <p className="muted" style={{ marginTop: 8 }}>
            Placement is measured across your own connected inboxes: warmup mail found in a recipient's spam folder is counted as spam (and auto-rescued to the inbox). "Pending" = sent but landing not yet detected.
          </p>
        </div>
      )}

      <div className="card">
        <h3>Bulk import accounts (CSV)</h3>
        <p className="muted">
          Upload a CSV to connect many mailboxes at once. Each row is verified before saving.
          Columns: <code>email, password, provider, from_name, smtp_host, smtp_port, imap_host, imap_port, daily_limit, warmup, warmup_target</code>.
          For Google/Outlook, just set <code>provider</code> + <code>email</code> + <code>password</code> (app password) — servers auto-fill.
          <br />
          <b>Vendor exports work as-is</b> (e.g. Maildoso): separate <code>IMAP/SMTP Username + Password</code>, <code>IMAP/SMTP Host + Port</code>, <code>First/Last Name</code>, and <code>Warmup Enabled/Limit</code> columns are all recognized.
        </p>
        <label style={{ display: "inline-flex", gap: 8, alignItems: "center", marginBottom: 10 }}>
          <input type="checkbox" style={{ width: "auto" }} checked={importSkipVerify} onChange={(e) => setImportSkipVerify(e.target.checked)} />
          Skip verification (save without dialing SMTP/IMAP)
        </label>
        <p className="muted" style={{ marginTop: 0 }}>
          Use this if import fails with <code>context deadline exceeded</code> or TLS errors — that means the server can't reach the mailbox host right now. Accounts are saved as <b>unverified</b>; the sender/warmup workers will surface real connection errors later.
        </p>
        <input type="file" accept=".csv" onChange={importAccounts} style={{ width: "auto" }} />
      </div>

      <div className="card">
        <h3>Connect a mailbox</h3>
        <label>Provider</label>
        <select value={form.provider} onChange={(e) => set("provider", e.target.value)}>
          {googleEnabled && <option value="google_oauth">Google — Sign in with Google (OAuth, no password)</option>}
          <option value="gmail">Google (Gmail / Workspace) — email + app password</option>
          <option value="outlook">Outlook / Microsoft 365 — email + password</option>
          <option value="custom">Custom (manual SMTP / IMAP)</option>
        </select>

        {form.provider === "google_oauth" ? (
          <div style={{ marginTop: 12 }}>
            <p className="muted">
              Authorize PipelineBuilder to send and read mail for a Google account — no app password needed.
              You'll be redirected to Google to grant access, then bounced back here.
            </p>
            {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
            <button onClick={connectGoogle}>Connect with Google</button>
          </div>
        ) : (
        <>
        <div className="grid2" style={{ marginTop: 8 }}>
          <div><label>Email</label><input value={form.email} onChange={(e) => set("email", e.target.value)} /></div>
          <div><label>From name</label><input value={form.from_name} onChange={(e) => set("from_name", e.target.value)} /></div>
        </div>

        {form.provider !== "custom" ? (
          <>
            <label>Password</label>
            <input type="password" value={form.password} onChange={(e) => set("password", e.target.value)} />
            {form.provider === "gmail" && (
              <p className="muted">
                Use a Google <b>App Password</b> (Google Account → Security → 2-Step Verification → App passwords),
                not your normal password. For Workspace, make sure IMAP is enabled in Gmail settings. SMTP/IMAP
                servers are filled in automatically.
              </p>
            )}
            {form.provider === "outlook" && (
              <p className="muted">
                Enter your account password (or app password if MFA is on). Servers are filled in automatically.
                Note: some Microsoft 365 tenants disable basic auth — if verification fails, your admin must allow it.
              </p>
            )}
          </>
        ) : (
          <div className="grid2" style={{ marginTop: 8 }}>
            <div><label>SMTP host</label><input value={form.smtp_host} onChange={(e) => set("smtp_host", e.target.value)} placeholder="smtp.example.com" /></div>
            <div><label>SMTP port</label><input type="number" value={form.smtp_port} onChange={(e) => set("smtp_port", Number(e.target.value))} /></div>
            <div><label>SMTP username</label><input value={form.smtp_username} onChange={(e) => set("smtp_username", e.target.value)} placeholder="(defaults to email)" /></div>
            <div><label>SMTP password</label><input type="password" value={form.smtp_password} onChange={(e) => set("smtp_password", e.target.value)} /></div>
            <div><label>IMAP host</label><input value={form.imap_host} onChange={(e) => set("imap_host", e.target.value)} placeholder="imap.example.com" /></div>
            <div><label>IMAP port</label><input type="number" value={form.imap_port} onChange={(e) => set("imap_port", Number(e.target.value))} /></div>
            <div><label>IMAP username</label><input value={form.imap_username} onChange={(e) => set("imap_username", e.target.value)} placeholder="(defaults to email)" /></div>
            <div><label>IMAP password</label><input type="password" value={form.imap_password} onChange={(e) => set("imap_password", e.target.value)} /></div>
          </div>
        )}

        <div className="grid2" style={{ marginTop: 8 }}>
          <div><label>Daily send limit</label><input type="number" value={form.daily_limit} onChange={(e) => set("daily_limit", Number(e.target.value))} /></div>
          <div><label>Warmup target / day</label><input type="number" value={form.warmup_target_per_day} onChange={(e) => set("warmup_target_per_day", Number(e.target.value))} /></div>
        </div>
        <label style={{ display: "inline-flex", gap: 8, alignItems: "center", marginTop: 10 }}>
          <input type="checkbox" style={{ width: "auto" }} checked={form.warmup_enabled} onChange={(e) => set("warmup_enabled", e.target.checked)} /> Enable warmup
        </label>
        <br />
        <label style={{ display: "inline-flex", gap: 8, alignItems: "center", marginTop: 8 }}>
          <input type="checkbox" style={{ width: "auto" }} checked={form.skip_verify} onChange={(e) => set("skip_verify", e.target.checked)} /> Skip verification (save without dialing SMTP/IMAP)
        </label>
        {msg && <p className={msg.startsWith("✓") ? "ok" : "err"}>{msg}</p>}
        <div className="row" style={{ marginTop: 12 }}>
          <button className="secondary" disabled={busy} onClick={verify}>Verify</button>
          <button disabled={busy} onClick={create}>Add account</button>
        </div>
        </>
        )}
      </div>
    </div>
  );
}

// PollHealth shows whether the reply poller can read this mailbox's inbox.
// A failing IMAP poll means replies to this inbox won't be detected.
function PollHealth({ account }: { account: Account }) {
  const when = account.imap_last_polled_at ? new Date(account.imap_last_polled_at).toLocaleString() : null;
  if (!when) return <span className="muted" title="Not polled yet">—</span>;
  if (account.imap_last_error) {
    return (
      <span>
        <span className="badge bounced" title={account.imap_last_error}>failing</span>
        <div className="muted" style={{ fontSize: 11, maxWidth: 240, marginTop: 2 }}>{account.imap_last_error}</div>
        <div className="muted" style={{ fontSize: 11 }}>last tried {when}</div>
      </span>
    );
  }
  return (
    <span>
      <span className="badge active" title="Inbox read successfully — replies will be detected">ok</span>
      <div className="muted" style={{ fontSize: 11 }}>{when}</div>
    </span>
  );
}
