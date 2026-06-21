import { useEffect, useState } from "react";
import { api, Reply } from "../api";

export default function Inbox() {
  const [replies, setReplies] = useState<Reply[]>([]);
  const [active, setActive] = useState<Reply | null>(null);
  const [body, setBody] = useState("");
  const [msg, setMsg] = useState("");

  const load = () => api.get<Reply[]>("/replies").then((r) => setReplies(r || []));
  useEffect(() => { load(); }, []);

  const send = async () => {
    if (!active) return;
    setMsg("");
    try { await api.post(`/replies/${active.message_id}/reply`, { body }); setMsg("Reply sent ✓"); setBody(""); }
    catch (e: any) { setMsg("✗ " + e.message); }
  };

  return (
    <div>
      <h2>Inbox — lead replies</h2>
      <p className="muted">Warmup emails are excluded. Click a reply to respond in-thread.</p>
      <div className="row" style={{ alignItems: "flex-start" }}>
        <div className="card" style={{ flex: 1, minWidth: 320 }}>
          <table>
            <thead><tr><th>From</th><th>Campaign</th><th>When</th></tr></thead>
            <tbody>
              {replies.map((r) => (
                <tr key={r.message_id} style={{ cursor: "pointer" }} onClick={() => { setActive(r); setMsg(""); }}>
                  <td>{r.lead_name || r.lead_email}<div className="muted">{r.lead_email}</div></td>
                  <td>{r.campaign_name}</td>
                  <td className="muted">{r.replied_at ? new Date(r.replied_at).toLocaleString() : ""}</td>
                </tr>
              ))}
              {replies.length === 0 && <tr><td colSpan={3} className="muted">No replies yet.</td></tr>}
            </tbody>
          </table>
        </div>

        {active && (
          <div className="card" style={{ flex: 1, minWidth: 360 }}>
            <h3>Re: {active.subject}</h3>
            <p className="muted">To: {active.lead_email} ({active.campaign_name})</p>
            <div className="card" style={{ background: "var(--panel2)" }}>
              <div className="muted">Your original email:</div>
              <div style={{ whiteSpace: "pre-wrap" }}>{active.body}</div>
            </div>
            <label>Your reply</label>
            <textarea value={body} onChange={(e) => setBody(e.target.value)} style={{ minHeight: 140 }} />
            {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
            <div style={{ marginTop: 8 }}><button onClick={send}>Send reply</button></div>
          </div>
        )}
      </div>
    </div>
  );
}
