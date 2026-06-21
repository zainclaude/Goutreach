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
    try { const r = await uploadCSV(f); setMsg(`Imported ${r.imported}, updated ${r.updated}, skipped ${r.skipped}`); load(); }
    catch (e: any) { setMsg(e.message); }
  };
  const remove = async (id: number) => { await api.del(`/leads/${id}`); load(); };

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
        <div className="flex-between"><h3>All leads ({leads.length})</h3></div>
        <table>
          <thead><tr><th>Email</th><th>Name</th><th>Company</th><th>Title</th><th></th></tr></thead>
          <tbody>
            {leads.map((l) => (
              <tr key={l.id}>
                <td>{l.email}</td>
                <td>{l.first_name} {l.last_name}</td>
                <td>{l.company}</td>
                <td>{l.title}</td>
                <td><button className="danger" onClick={() => remove(l.id)}>×</button></td>
              </tr>
            ))}
            {leads.length === 0 && <tr><td colSpan={5} className="muted">No leads yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}
