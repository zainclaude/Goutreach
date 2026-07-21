import { useEffect, useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, Campaign } from "../api";

export default function Campaigns() {
  const [list, setList] = useState<Campaign[]>([]);
  const [form, setForm] = useState({
    name: "", brief: "", daily_cap: 100, require_approval: true, approval_count: 5,
    track_opens: false, track_clicks: false,
    steps: [{ delay_days: 0, angle: "Intro email" }],
  });
  const [err, setErr] = useState("");
  const nav = useNavigate();

  const load = () => api.get<Campaign[]>("/campaigns").then((c) => setList(c || []));
  useEffect(() => { load(); }, []);

  const set = (k: string, v: any) => setForm({ ...form, [k]: v });
  const setStep = (i: number, k: string, v: any) => {
    const steps = form.steps.slice(); (steps[i] as any)[k] = v; setForm({ ...form, steps });
  };
  const addStep = () => setForm({ ...form, steps: [...form.steps, { delay_days: 3, angle: "Follow up" }] });
  const rmStep = (i: number) => setForm({ ...form, steps: form.steps.filter((_, j) => j !== i) });

  const create = async () => {
    setErr("");
    try { const c = await api.post<Campaign>("/campaigns", form); nav(`/campaigns/${c.id}`); }
    catch (e: any) { setErr(e.message); }
  };
  const remove = async (c: Campaign) => {
    if (!confirm(`Delete campaign "${c.name}"? Its enrollments, drafts, and stats are removed. Leads stay in your lead list.`)) return;
    try { await api.del(`/campaigns/${c.id}`); load(); }
    catch (e: any) { setErr(e.message); }
  };

  return (
    <div>
      <h2>Campaigns</h2>

      <div className="card">
        <h3>New campaign</h3>
        <label>Name</label>
        <input value={form.name} onChange={(e) => set("name", e.target.value)} />
        <label>Brief (offer, ICP, tone — fed to Claude)</label>
        <textarea value={form.brief} onChange={(e) => set("brief", e.target.value)}
          placeholder="We help DTC beauty brands scale on TikTok Shop with managed creator campaigns. Friendly, concise tone." />
        <div className="grid2" style={{ marginTop: 8 }}>
          <div><label>Daily cap (campaign)</label><input type="number" value={form.daily_cap} onChange={(e) => set("daily_cap", Number(e.target.value))} /></div>
          <div><label>Preview count before launch</label><input type="number" value={form.approval_count} onChange={(e) => set("approval_count", Number(e.target.value))} /></div>
        </div>
        <div className="row" style={{ marginTop: 10 }}>
          <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
            <input type="checkbox" style={{ width: "auto" }} checked={form.require_approval} onChange={(e) => set("require_approval", e.target.checked)} /> Require preview approval before sending
          </label>
          <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
            <input type="checkbox" style={{ width: "auto" }} checked={form.track_opens} onChange={(e) => set("track_opens", e.target.checked)} /> Track opens
          </label>
          <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
            <input type="checkbox" style={{ width: "auto" }} checked={form.track_clicks} onChange={(e) => set("track_clicks", e.target.checked)} /> Track clicks
          </label>
        </div>

        <h4>Sequence steps</h4>
        {form.steps.map((s, i) => (
          <div className="row" key={i} style={{ alignItems: "center", marginBottom: 8 }}>
            <span className="tag">Step {i + 1}</span>
            <div style={{ width: 130 }}>
              <input type="number" value={s.delay_days} onChange={(e) => setStep(i, "delay_days", Number(e.target.value))} placeholder="delay days" />
            </div>
            <div style={{ flex: 1 }}>
              <input value={s.angle} onChange={(e) => setStep(i, "angle", e.target.value)} placeholder="angle / instruction for this step" />
            </div>
            {form.steps.length > 1 && <button className="danger" onClick={() => rmStep(i)}>×</button>}
          </div>
        ))}
        <button className="secondary" onClick={addStep}>+ Add step</button>

        {err && <p className="err">{err}</p>}
        <div style={{ marginTop: 12 }}><button onClick={create}>Create campaign</button></div>
      </div>

      <div className="card">
        <h3>All campaigns</h3>
        <table>
          <thead><tr><th>Name</th><th>Status</th><th>Approval</th><th></th></tr></thead>
          <tbody>
            {list.map((c) => (
              <tr key={c.id}>
                <td><a href={`/campaigns/${c.id}`}>{c.name}</a></td>
                <td><span className={`badge ${c.status}`}>{c.status}</span></td>
                <td>{c.require_approval ? `preview ${c.approval_count}` : "off"}</td>
                <td><button className="danger" title="Delete campaign" onClick={() => remove(c)}>×</button></td>
              </tr>
            ))}
            {list.length === 0 && <tr><td colSpan={4} className="muted">No campaigns yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}
