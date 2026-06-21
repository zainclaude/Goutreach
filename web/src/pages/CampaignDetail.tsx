import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, Account, Campaign, Message, Stats, Step } from "../api";

export default function CampaignDetail() {
  const { id } = useParams();
  const cid = Number(id);
  const [campaign, setCampaign] = useState<Campaign | null>(null);
  const [steps, setSteps] = useState<Step[]>([]);
  const [accountIDs, setAccountIDs] = useState<number[]>([]);
  const [accounts, setAccounts] = useState<Account[]>([]);
  const [messages, setMessages] = useState<Message[]>([]);
  const [stats, setStats] = useState<Stats | null>(null);
  const [msg, setMsg] = useState("");

  const loadCampaign = () =>
    api.get<{ campaign: Campaign; steps: Step[]; account_ids: number[] }>(`/campaigns/${cid}`).then((d) => {
      setCampaign(d.campaign); setSteps(d.steps || []); setAccountIDs(d.account_ids || []);
    });
  const loadMessages = () => api.get<Message[]>(`/campaigns/${cid}/messages`).then((m) => setMessages(m || []));
  const loadStats = () => api.get<Stats>(`/campaigns/${cid}/stats`).then(setStats);

  useEffect(() => {
    loadCampaign(); loadMessages(); loadStats();
    api.get<Account[]>("/accounts").then((a) => setAccounts(a || []));
    const t = setInterval(loadMessages, 5000); // poll while previews generate
    return () => clearInterval(t);
  }, [cid]);

  const toggleAccount = async (aid: number) => {
    const next = accountIDs.includes(aid) ? accountIDs.filter((x) => x !== aid) : [...accountIDs, aid];
    setAccountIDs(next);
    await api.put(`/campaigns/${cid}/accounts`, { account_ids: next });
  };
  const enrollAll = async () => { const r = await api.post<{ enrolled: number }>(`/campaigns/${cid}/enroll`, { all: true }); setMsg(`Enrolled ${r.enrolled} leads`); };
  const preview = async () => {
    try { const r = await api.post<{ generating: number }>(`/campaigns/${cid}/preview`); setMsg(`Generating ${r.generating} previews… (refreshes automatically)`); }
    catch (e: any) { setMsg(e.message); }
  };
  const launch = async () => {
    if (!confirm("Launch campaign? Approved previews + remaining leads will start sending.")) return;
    await api.post(`/campaigns/${cid}/launch`); setMsg("Launched 🚀"); loadCampaign();
  };
  const setStatus = async (status: string) => { await api.patch(`/campaigns/${cid}`, { status }); loadCampaign(); };
  const approve = async (mid: number) => { await api.post(`/messages/${mid}/approve`); loadMessages(); };
  const reject = async (mid: number) => { await api.del(`/messages/${mid}`); loadMessages(); };
  const saveEdit = async (m: Message) => { await api.patch(`/messages/${m.id}`, { subject: m.subject, body: m.body }); setMsg("Saved edit"); };

  if (!campaign) return <div>Loading…</div>;

  return (
    <div>
      <div className="flex-between">
        <h2>{campaign.name} <span className={`badge ${campaign.status}`}>{campaign.status}</span></h2>
        <div className="row">
          {campaign.status !== "running" && <button onClick={() => setStatus("running")}>Set running</button>}
          {campaign.status === "running" && <button className="secondary" onClick={() => setStatus("paused")}>Pause</button>}
        </div>
      </div>
      {msg && <p className="ok">{msg}</p>}

      <div className="kpis">
        <div className="kpi"><div className="v">{stats?.sent ?? 0}</div><div className="l">Sent</div></div>
        <div className="kpi"><div className="v">{stats?.opens ?? 0}</div><div className="l">Opens</div></div>
        <div className="kpi"><div className="v">{stats?.replies ?? 0}</div><div className="l">Replies</div></div>
        <div className="kpi"><div className="v">{stats?.bounces ?? 0}</div><div className="l">Bounces</div></div>
      </div>

      <div className="card">
        <h3>Brief</h3>
        <p className="muted">{campaign.brief || "(no brief)"}</p>
        <h4>Sequence</h4>
        {steps.map((s) => <div key={s.step_index} className="tag" style={{ marginRight: 8 }}>Step {s.step_index + 1}: +{s.delay_days}d — {s.angle || "(no angle)"}</div>)}
      </div>

      <div className="card">
        <h3>Sending inboxes (round-robin, 1 per account)</h3>
        {accounts.length === 0 && <p className="muted">No email accounts connected yet.</p>}
        {accounts.map((a) => (
          <label key={a.id} style={{ display: "inline-flex", gap: 6, alignItems: "center", marginRight: 16 }}>
            <input type="checkbox" style={{ width: "auto" }} checked={accountIDs.includes(a.id)} onChange={() => toggleAccount(a.id)} /> {a.email}
          </label>
        ))}
      </div>

      <div className="card">
        <h3>Leads & launch</h3>
        <div className="row">
          <button className="secondary" onClick={enrollAll}>Enroll all leads</button>
          <button className="secondary" onClick={preview}>Generate {campaign.approval_count} previews</button>
          <button onClick={launch}>Approve & launch</button>
        </div>
        <p className="muted" style={{ marginTop: 8 }}>
          Each email is researched per the TikTok Shop → Amazon → Meta ads → retail decision tree, then written from the matching template.
        </p>
      </div>

      <div className="card">
        <h3>
          Emails ({messages.length})
          {messages.length > 0 && (() => {
            const cached = messages.filter((m) => (m.research_notes || "").toLowerCase().startsWith("cached")).length;
            const gen = messages.filter((m) => m.template_used && !(m.research_notes || "").toLowerCase().startsWith("cached")).length;
            return <span className="muted" style={{ fontWeight: 400, fontSize: 14, marginLeft: 8 }}>· ♻ {cached} cached · {gen} generated</span>;
          })()}
        </h3>
        <table>
          <thead><tr><th>Lead</th><th>Tmpl</th><th>Subject</th><th>Status</th><th>Approved</th><th></th></tr></thead>
          <tbody>
            {messages.map((m) => (
              <MessageRow key={m.id} m={m} onApprove={approve} onReject={reject} onSave={saveEdit} />
            ))}
            {messages.length === 0 && <tr><td colSpan={6} className="muted">No emails generated yet. Enroll leads, then generate previews.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function MessageRow({ m, onApprove, onReject, onSave }:
  { m: Message; onApprove: (id: number) => void; onReject: (id: number) => void; onSave: (m: Message) => void }) {
  const [open, setOpen] = useState(false);
  const [subject, setSubject] = useState(m.subject);
  const [body, setBody] = useState(m.body);
  const cached = (m.research_notes || "").toLowerCase().startsWith("cached");
  return (
    <>
      <tr>
        <td>{m.lead_email}<div className="muted">{m.lead_company}</div></td>
        <td>
          {m.template_used && <span className="tag">{m.template_used}</span>}
          {cached && <span className="tag" title="Reused from brand cache — no AI tokens used" style={{ marginLeft: 4 }}>♻ cached</span>}
        </td>
        <td>{m.subject || <span className="muted">{m.status === "generating" || m.status === "queued" ? "generating…" : "—"}</span>}</td>
        <td><span className={`badge ${m.status}`}>{m.status}</span></td>
        <td>{m.approved ? "✓" : "—"}</td>
        <td>
          <button className="secondary" onClick={() => setOpen(!open)}>{open ? "Hide" : "View"}</button>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6}>
            {m.research_notes && <p className="muted">🔎 {m.research_notes}</p>}
            <label>Subject</label>
            <input value={subject} onChange={(e) => setSubject(e.target.value)} />
            <label>Body</label>
            <textarea value={body} onChange={(e) => setBody(e.target.value)} style={{ minHeight: 160 }} />
            <div className="row" style={{ marginTop: 8 }}>
              <button onClick={() => onSave({ ...m, subject, body })}>Save edit</button>
              {!m.approved && <button className="secondary" onClick={() => onApprove(m.id)}>Approve</button>}
              <button className="danger" onClick={() => onReject(m.id)}>Reject</button>
            </div>
          </td>
        </tr>
      )}
    </>
  );
}
