import { useEffect, useState } from "react";
import { api, uploadCSV, Lead } from "../api";

export default function Leads() {
  const [leads, setLeads] = useState<Lead[]>([]);
  const [form, setForm] = useState({ email: "", first_name: "", last_name: "", company: "", title: "" });
  const [msg, setMsg] = useState("");

  const load = () => api.get<Lead[]>("/leads").then((l) => setLeads(l || []));
  useEffect(() => { load(); }, []);

  const set = (k: string, v: string) => setForm({ ...form, [k]: v });
  const add = async () => {
    try { await api.post("/leads", form); setForm({ email: "", first_name: "", last_name: "", company: "", title: "" }); load(); }
    catch (e: any) { setMsg(e.message); }
  };
  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const f = e.target.files?.[0];
    if (!f) return;
    try {
      const r = await uploadCSV(f);
      let m = `Imported ${r.imported}, updated ${r.updated}, skipped ${r.skipped}`;
      if (r.blacklisted > 0) m += ` — ⚠️ ${r.blacklisted} not added (blacklisted domain): ${(r.blacklisted_emails || []).join(", ")}`;
      setMsg(m); load();
    } catch (e: any) { setMsg(e.message); }
  };
  const remove = async (id: number) => { await api.del(`/leads/${id}`); load(); };
  const verify = async () => {
    setMsg("");
    try {
      const r = await api.post<{ queued: number }>("/leads/verify", {});
      setMsg(r.queued > 0
        ? `Verifying ${r.queued} lead(s) in the background — refresh in a bit to see results.`
        : "All leads already verified.");
    } catch (e: any) { setMsg("✗ " + e.message); }
  };

  return (
    <div>
      <h2>Leads</h2>
      <div className="card">
        <h3>Add lead</h3>
        <div className="grid2">
          <div><label>Email</label><input value={form.email} onChange={(e) => set("email", e.target.value)} /></div>
          <div><label>Company / Brand</label><input value={form.company} onChange={(e) => set("company", e.target.value)} /></div>
          <div><label>First name</label><input value={form.first_name} onChange={(e) => set("first_name", e.target.value)} /></div>
          <div><label>Last name</label><input value={form.last_name} onChange={(e) => set("last_name", e.target.value)} /></div>
          <div><label>Title</label><input value={form.title} onChange={(e) => set("title", e.target.value)} /></div>
        </div>
        <div className="row" style={{ marginTop: 12, alignItems: "center" }}>
          <button onClick={add}>Add</button>
          <span className="muted">or import CSV (columns: email, first_name, last_name, company, title):</span>
          <input type="file" accept=".csv" onChange={onFile} style={{ width: "auto" }} />
        </div>
        {msg && <p className="ok">{msg}</p>}
      </div>

      <div className="card">
        <div className="flex-between">
          <h3>All leads ({leads.length})</h3>
          <button className="secondary" onClick={verify} title="Check deliverability via your verification provider (configure it in Settings)">Verify emails</button>
        </div>
        <table>
          <thead><tr><th>Email</th><th>Verification</th><th>Status</th><th>Name</th><th>Company</th><th>Title</th><th></th></tr></thead>
          <tbody>
            {leads.map((l) => (
              <tr key={l.id}>
                <td>{l.email}</td>
                <td><VerifyBadge status={l.verification_status} /></td>
                <td>{l.contacted
                  ? <span className="badge bounced" title="Emailed before">contacted</span>
                  : <span className="badge active" title="Never emailed">uncontacted</span>}</td>
                <td>{l.first_name} {l.last_name}</td>
                <td>{l.company}</td>
                <td>{l.title}</td>
                <td><button className="danger" onClick={() => remove(l.id)}>×</button></td>
              </tr>
            ))}
            {leads.length === 0 && <tr><td colSpan={7} className="muted">No leads yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// VerifyBadge shows the email-verification result for a lead.
function VerifyBadge({ status }: { status: string }) {
  switch (status) {
    case "valid":
      return <span className="badge active" title="Deliverable">valid</span>;
    case "invalid":
      return <span className="badge bounced" title="Undeliverable — will be skipped when sending">invalid</span>;
    case "risky":
      return <span className="badge" title="Risky (spam-trap/abuse/role)" style={{ background: "#7a5b00", color: "#ffd778" }}>risky</span>;
    case "catch_all":
      return <span className="badge" title="Catch-all domain — accepts all addresses, can't confirm" style={{ background: "#33405e", color: "#9db4e8" }}>catch-all</span>;
    default:
      return <span className="muted" title="Not verified yet">—</span>;
  }
}
