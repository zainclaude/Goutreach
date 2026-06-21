import { useEffect, useState } from "react";
import { api, Account, AccountStat } from "../api";

const blank = {
  email: "", from_name: "", smtp_host: "", smtp_port: 587, smtp_username: "", smtp_password: "",
  imap_host: "", imap_port: 993, imap_username: "", imap_password: "",
  daily_limit: 30, warmup_enabled: false, warmup_target_per_day: 20,
};

export default function Accounts() {
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [stats, setStats] = useState<Record<number, AccountStat>>({});
  const [form, setForm] = useState({ ...blank });
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  const load = () => {
    api.get<Account[]>("/accounts").then((a) => setAccounts(a || []));
    api.get<AccountStat[]>("/accounts/stats").then((s) => {
      const m: Record<number, AccountStat> = {};
      (s || []).forEach((x) => (m[x.account_id] = x));
      setStats(m);
    });
  };
  useEffect(load, []);

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
            <th>Replies today</th><th>Replies total</th><th>Reply rate</th><th>Warmup</th><th>Status</th><th></th>
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
                    <input type="checkbox" style={{ width: "auto" }} defaultChecked={a.warmup_enabled}
                      onChange={(e) => saveSettings(a, a.daily_limit, e.target.checked, a.warmup_target_per_day)} />
                  </td>
                  <td><span className={`badge ${a.status}`}>{a.status}</span></td>
                  <td><button className="danger" onClick={() => remove(a.id)}>×</button></td>
                </tr>
              );
            })}
            {accounts.length === 0 && <tr><td colSpan={10} className="muted">No accounts yet.</td></tr>}
          </tbody>
        </table>
      </div>

      <div className="card">
        <h3>Connect a mailbox (SMTP + IMAP)</h3>
        <div className="grid2">
          <div><label>Email</label><input value={form.email} onChange={(e) => set("email", e.target.value)} /></div>
          <div><label>From name</label><input value={form.from_name} onChange={(e) => set("from_name", e.target.value)} /></div>
          <div><label>SMTP host</label><input value={form.smtp_host} onChange={(e) => set("smtp_host", e.target.value)} placeholder="smtp.gmail.com" /></div>
          <div><label>SMTP port</label><input type="number" value={form.smtp_port} onChange={(e) => set("smtp_port", Number(e.target.value))} /></div>
          <div><label>SMTP username</label><input value={form.smtp_username} onChange={(e) => set("smtp_username", e.target.value)} placeholder="(defaults to email)" /></div>
          <div><label>SMTP password</label><input type="password" value={form.smtp_password} onChange={(e) => set("smtp_password", e.target.value)} /></div>
          <div><label>IMAP host</label><input value={form.imap_host} onChange={(e) => set("imap_host", e.target.value)} placeholder="imap.gmail.com" /></div>
          <div><label>IMAP port</label><input type="number" value={form.imap_port} onChange={(e) => set("imap_port", Number(e.target.value))} /></div>
          <div><label>IMAP username</label><input value={form.imap_username} onChange={(e) => set("imap_username", e.target.value)} placeholder="(defaults to email)" /></div>
          <div><label>IMAP password</label><input type="password" value={form.imap_password} onChange={(e) => set("imap_password", e.target.value)} /></div>
          <div><label>Daily send limit</label><input type="number" value={form.daily_limit} onChange={(e) => set("daily_limit", Number(e.target.value))} /></div>
          <div><label>Warmup target / day</label><input type="number" value={form.warmup_target_per_day} onChange={(e) => set("warmup_target_per_day", Number(e.target.value))} /></div>
        </div>
        <label style={{ display: "inline-flex", gap: 8, alignItems: "center", marginTop: 10 }}>
          <input type="checkbox" style={{ width: "auto" }} checked={form.warmup_enabled} onChange={(e) => set("warmup_enabled", e.target.checked)} /> Enable warmup
        </label>
        {msg && <p className={msg.startsWith("✓") ? "ok" : "err"}>{msg}</p>}
        <div className="row" style={{ marginTop: 12 }}>
          <button className="secondary" disabled={busy} onClick={verify}>Verify</button>
          <button disabled={busy} onClick={create}>Add account</button>
        </div>
      </div>
    </div>
  );
}
