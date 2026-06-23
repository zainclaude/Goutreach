import { useEffect, useState } from "react";
import { useParams } from "react-router-dom";
import { api, Account, Campaign, CampaignLeadDetail, Lead, Message, Stats, Step } from "../api";

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
  const [leads, setLeads] = useState<Lead[]>([]);
  const [enrolled, setEnrolled] = useState<CampaignLeadDetail[]>([]);
  const [showPicker, setShowPicker] = useState(false);
  const [selected, setSelected] = useState<number[]>([]);

  const loadCampaign = () =>
    api.get<{ campaign: Campaign; steps: Step[]; account_ids: number[] }>(`/campaigns/${cid}`).then((d) => {
      setCampaign(d.campaign); setSteps(d.steps || []); setAccountIDs(d.account_ids || []);
    });
  const loadMessages = () => api.get<Message[]>(`/campaigns/${cid}/messages`).then((m) => setMessages(m || []));
  const loadStats = () => api.get<Stats>(`/campaigns/${cid}/stats`).then(setStats);

  const loadLeads = () => api.get<Lead[]>("/leads").then((l) => setLeads(l || []));
  const loadEnrolled = () => api.get<CampaignLeadDetail[]>(`/campaigns/${cid}/leads`).then((l) => setEnrolled(l || []));

  useEffect(() => {
    loadCampaign(); loadMessages(); loadStats(); loadLeads(); loadEnrolled();
    api.get<Account[]>("/accounts").then((a) => setAccounts(a || []));
    const t = setInterval(loadMessages, 5000); // poll while previews generate
    return () => clearInterval(t);
  }, [cid]);

  const toggleAccount = async (aid: number) => {
    const next = accountIDs.includes(aid) ? accountIDs.filter((x) => x !== aid) : [...accountIDs, aid];
    setAccountIDs(next);
    await api.put(`/campaigns/${cid}/accounts`, { account_ids: next });
  };
  const setAllAccounts = async (ids: number[]) => {
    setAccountIDs(ids);
    await api.put(`/campaigns/${cid}/accounts`, { account_ids: ids });
  };
  const enrollAll = async () => { const r = await api.post<{ enrolled: number }>(`/campaigns/${cid}/enroll`, { all: true }); setMsg(`Enrolled ${r.enrolled} leads`); loadEnrolled(); };
  const enrollUnemailed = async () => { const r = await api.post<{ enrolled: number }>(`/campaigns/${cid}/enroll`, { unemailed: true }); setMsg(`Enrolled ${r.enrolled} previously-uncontacted leads`); loadLeads(); loadEnrolled(); };
  const removeAll = async () => {
    if (!confirm("Remove ALL leads from this campaign? This also clears their generated drafts.")) return;
    const r = await api.del<{ removed: number }>(`/campaigns/${cid}/enroll`);
    setMsg(`Removed ${r.removed} leads`); loadMessages(); loadEnrolled();
  };
  const removeLead = async (leadID: number, email: string) => {
    if (!confirm(`Remove ${email} from this campaign? Their generated draft is cleared too.`)) return;
    await api.del(`/campaigns/${cid}/leads/${leadID}`);
    setMsg(`Removed ${email}`); loadEnrolled(); loadMessages();
  };
  const toggleSelected = (lid: number) => setSelected((s) => s.includes(lid) ? s.filter((x) => x !== lid) : [...s, lid]);
  const enrollSelected = async () => {
    if (selected.length === 0) { setMsg("No leads selected"); return; }
    const r = await api.post<{ enrolled: number }>(`/campaigns/${cid}/enroll`, { lead_ids: selected });
    setMsg(`Enrolled ${r.enrolled} leads`); setSelected([]); setShowPicker(false); loadEnrolled();
  };
  const preview = async () => {
    try { const r = await api.post<{ generating: number }>(`/campaigns/${cid}/preview`); setMsg(`Generating ${r.generating} previews… (refreshes automatically)`); }
    catch (e: any) { setMsg(e.message); }
  };
  const launch = async () => {
    const previewed = new Set(
      messages.filter((m) => m.status === "generated" || m.status === "sent").map((m) => m.lead_email)
    );
    const unseen = enrolled.filter((e) => !previewed.has(e.email)).length;
    let prompt = "Launch campaign? Approved previews + remaining leads will start sending.";
    if (unseen > 0) {
      prompt = `⚠️ ${unseen} of ${enrolled.length} enrolled leads have NOT been previewed.\n\n` +
        `Their emails will be generated and sent automatically WITHOUT your review. ` +
        `Generate previews for them first if you want to see every email.\n\nLaunch anyway?`;
    }
    if (!confirm(prompt)) return;
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
        {accounts.length > 0 && (
          <div className="row" style={{ marginBottom: 10 }}>
            <button className="secondary" onClick={() => setAllAccounts(accounts.map((a) => a.id))} disabled={accountIDs.length === accounts.length}>Select all ({accounts.length})</button>
            <button className="secondary" onClick={() => setAllAccounts([])} disabled={accountIDs.length === 0}>Clear</button>
          </div>
        )}
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
          <button className="secondary" onClick={enrollUnemailed}>Enroll all unemailed leads</button>
          <button className="secondary" onClick={() => setShowPicker(!showPicker)}>{showPicker ? "Hide lead picker" : "Select leads…"}</button>
          <button className="danger" onClick={removeAll}>Remove all leads</button>
          <button className="secondary" onClick={preview}>Generate {campaign.approval_count} previews</button>
          <button onClick={launch}>Approve & launch</button>
        </div>
        {enrolled.length > 0 && (() => {
          const previewed = new Set(messages.filter((m) => m.status === "generated" || m.status === "sent").map((m) => m.lead_email));
          const unseen = enrolled.filter((e) => !previewed.has(e.email)).length;
          return unseen > 0
            ? <p className="err" style={{ marginTop: 8 }}>⚠️ {unseen} of {enrolled.length} enrolled leads have no preview — they'll send without review on launch.</p>
            : <p className="ok" style={{ marginTop: 8 }}>✓ All {enrolled.length} enrolled leads have a preview.</p>;
        })()}
        {showPicker && (() => {
          const enrolledIds = new Set(enrolled.map((e) => e.lead_id));
          return (
          <div style={{ marginTop: 12, maxHeight: 300, overflow: "auto", border: "1px solid #2a2a2a", borderRadius: 6, padding: 10 }}>
            <div className="row" style={{ marginBottom: 8 }}>
              <button className="secondary" onClick={() => setSelected(leads.filter((l) => !enrolledIds.has(l.id)).map((l) => l.id))}>Select all unenrolled</button>
              <button className="secondary" onClick={() => setSelected([])}>Clear</button>
              <button onClick={enrollSelected} disabled={selected.length === 0}>Enroll selected ({selected.length})</button>
            </div>
            {leads.length === 0 && <p className="muted">No leads yet — import leads first.</p>}
            {leads.map((l) => {
              const isEnrolled = enrolledIds.has(l.id);
              return (
              <label key={l.id} style={{ display: "flex", gap: 8, alignItems: "center", padding: "2px 0", opacity: isEnrolled ? 0.6 : 1 }}>
                <input type="checkbox" style={{ width: "auto" }} checked={selected.includes(l.id)} disabled={isEnrolled} onChange={() => toggleSelected(l.id)} />
                <span>{l.email}{l.company && <span className="muted"> · {l.company}</span>}</span>
                {isEnrolled && <span className="tag" style={{ marginLeft: "auto" }}>✓ in campaign</span>}
              </label>
              );
            })}
          </div>
          );
        })()}
        <p className="muted" style={{ marginTop: 8 }}>
          Each email is researched per the TikTok Shop → Amazon → Meta ads → retail decision tree, then written from the matching template.
        </p>
      </div>

      <div className="card">
        <h3>In this campaign ({enrolled.length})</h3>
        {enrolled.length === 0 && <p className="muted">No leads enrolled yet.</p>}
        {enrolled.length > 0 && (
          <table>
            <thead><tr><th>Lead</th><th>Company</th><th>Status</th><th>Step</th><th></th></tr></thead>
            <tbody>
              {enrolled.map((e) => (
                <tr key={e.lead_id}>
                  <td>{e.email}{(e.first_name || e.last_name) && <div className="muted">{[e.first_name, e.last_name].filter(Boolean).join(" ")}</div>}</td>
                  <td>{e.company || <span className="muted">—</span>}</td>
                  <td><span className={`badge ${e.status}`}>{e.status}</span></td>
                  <td>{e.current_step + 1}</td>
                  <td><button className="danger" title="Remove this lead from the campaign" onClick={() => removeLead(e.lead_id, e.email)}>×</button></td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
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
  // Keep the editable fields in sync when generation finishes: props update via
  // polling, but useState's initial value only applies on mount, so without this
  // the preview stays empty until a manual refresh.
  useEffect(() => {
    setSubject(m.subject);
    setBody(m.body);
  }, [m.subject, m.body]);
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
        <td><span className={`badge ${m.status}`} title={m.error || undefined}>{m.status}</span></td>
        <td>{m.approved ? "✓" : "—"}</td>
        <td>
          <button className="secondary" onClick={() => setOpen(!open)}>{open ? "Hide" : "View"}</button>
        </td>
      </tr>
      {open && (
        <tr>
          <td colSpan={6}>
            {m.status === "failed" && m.error && <p className="err">⚠️ {m.error}</p>}
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
