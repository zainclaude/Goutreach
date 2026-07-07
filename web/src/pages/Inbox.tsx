import { useEffect, useState } from "react";
import { api, Reply } from "../api";

export default function Inbox() {
  const [replies, setReplies] = useState<Reply[]>([]);
  const [active, setActive] = useState<Reply | null>(null);
  const [filter, setFilter] = useState(() => localStorage.getItem("inbox_filter") || "interested");
  const [body, setBody] = useState("");
  const [cc, setCc] = useState("");
  const [msg, setMsg] = useState("");

  const load = () => api.get<Reply[]>("/replies").then((r) => setReplies(r || []));
  useEffect(() => { load(); }, []);

  const send = async () => {
    if (!active) return;
    setMsg("");
    try { await api.post(`/replies/${active.message_id}/reply`, { body, cc }); setMsg("Reply sent ✓"); setBody(""); setCc(""); }
    catch (e: any) { setMsg("✗ " + e.message); }
  };

  const setAndSaveFilter = (f: string) => { setFilter(f); localStorage.setItem("inbox_filter", f); };
  // Unclassified ("") replies are treated as potentially interested — never hidden by default.
  const visible = replies.filter((r) => {
    if (filter === "all") return true;
    if (filter === "no-noise") return !["ooo", "unsubscribe"].includes(r.reply_category);
    return r.reply_category === "interested" || r.reply_category === "";
  });
  const hidden = replies.length - visible.length;

  return (
    <div>
      <div className="flex-between">
        <h2>Inbox — lead replies</h2>
        <select style={{ width: "auto" }} value={filter} onChange={(e) => setAndSaveFilter(e.target.value)}>
          <option value="interested">Interested only</option>
          <option value="no-noise">All except OOO & unsubscribe</option>
          <option value="all">All replies</option>
        </select>
      </div>
      <p className="muted">
        Warmup emails are excluded. Replies are AI-classified; this view shows{" "}
        {filter === "all" ? "every reply" : filter === "no-noise" ? "everything except out-of-office and unsubscribe replies" : "interested (and not-yet-classified) replies"}
        {hidden > 0 ? ` — ${hidden} hidden by the filter` : ""}. Only interested replies trigger
        email notifications. Click a reply to respond in-thread.
      </p>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="card" style={{ flex: 1, minWidth: 320 }}>
          <table>
            <thead><tr><th>From</th><th>Type</th><th>Campaign</th><th>When</th></tr></thead>
            <tbody>
              {visible.map((r) => (
                <tr key={r.message_id} style={{ cursor: "pointer" }} onClick={() => { setActive(r); setMsg(""); setCc(""); setBody(""); }}>
                  <td>{r.lead_name || r.lead_email}<div className="muted">{r.lead_email}</div></td>
                  <td><CategoryBadge category={r.reply_category} /></td>
                  <td>{r.campaign_name}</td>
                  <td className="muted">{r.replied_at ? new Date(r.replied_at).toLocaleString() : ""}</td>
                </tr>
              ))}
              {visible.length === 0 && <tr><td colSpan={4} className="muted">{replies.length > 0 ? "No replies match this filter — switch to \"All replies\" above." : "No replies yet."}</td></tr>}
            </tbody>
          </table>
        </div>

        {active && (
          <div className="card" style={{ flex: 1, minWidth: 360 }}>
            <h3>Re: {active.subject}</h3>
            <p className="muted">To: {active.lead_email} ({active.campaign_name})</p>
            <div className="card" style={{ background: "var(--panel2)", borderLeft: "3px solid #6c8cff" }}>
              <div className="muted">{active.lead_name || active.lead_email} replied{active.replied_at ? ` · ${new Date(active.replied_at).toLocaleString()}` : ""}:</div>
              <div style={{ whiteSpace: "pre-wrap" }}>
                {active.reply_body
                  ? active.reply_body
                  : <span className="muted">(reply text not captured yet — it'll appear after the next inbox poll)</span>}
              </div>
            </div>
            <details style={{ marginTop: 8 }}>
              <summary className="muted" style={{ cursor: "pointer" }}>Your original email</summary>
              <div style={{ whiteSpace: "pre-wrap", marginTop: 6 }}>{active.body}</div>
            </details>
            <label style={{ marginTop: 8, display: "block" }}>Cc <span className="muted">(optional, comma-separated)</span></label>
            <input value={cc} onChange={(e) => setCc(e.target.value)} placeholder="teammate@agency.com, client@brand.com" />
            <label style={{ marginTop: 8, display: "block" }}>Your reply</label>
            <textarea value={body} onChange={(e) => setBody(e.target.value)} style={{ minHeight: 140 }} />
            {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
            <div style={{ marginTop: 8 }}><button onClick={send}>Send reply</button></div>
          </div>
        )}
      </div>
    </div>
  );
}

// CategoryBadge shows the AI classification of a reply.
function CategoryBadge({ category }: { category: string }) {
  switch (category) {
    case "interested":
      return <span className="badge replied" title="AI classified this as an interested reply">interested</span>;
    case "not_interested":
      return <span className="badge bounced" title="AI classified this as not interested">not interested</span>;
    case "ooo":
      return <span className="badge" title="Automated out-of-office reply">out of office</span>;
    case "unsubscribe":
      return <span className="badge bounced" title="Asked to be removed — consider blacklisting this domain">unsubscribe</span>;
    case "other":
      return <span className="badge" title="Neutral or unclear reply">other</span>;
    default:
      return <span className="muted" title="Not classified (received before classification existed)">—</span>;
  }
}
