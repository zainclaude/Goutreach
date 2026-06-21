import { useEffect, useState } from "react";
import { api, Stats, Campaign } from "../api";

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [aiEnabled, setAiEnabled] = useState(true);

  useEffect(() => {
    api.get<Stats>("/overview").then(setStats).catch(() => {});
    api.get<Campaign[]>("/campaigns").then((c) => setCampaigns(c || [])).catch(() => {});
    api.get<{ ai_enabled: boolean }>("/me").then((m) => setAiEnabled(m.ai_enabled)).catch(() => {});
  }, []);

  const rate = (n: number) => (stats && stats.sent > 0 ? ((n / stats.sent) * 100).toFixed(1) + "%" : "—");

  return (
    <div>
      <h2>Dashboard</h2>
      {!aiEnabled && (
        <div className="card" style={{ borderColor: "var(--danger)" }}>
          <b>AI generation is disabled.</b> Set <code>ANTHROPIC_API_KEY</code> on the server to enable
          research-driven email writing.
        </div>
      )}
      <div className="kpis">
        <div className="kpi"><div className="v">{stats?.sent ?? 0}</div><div className="l">Emails sent</div></div>
        <div className="kpi"><div className="v">{stats?.opens ?? 0}</div><div className="l">Opens ({rate(stats?.opens ?? 0)})</div></div>
        <div className="kpi"><div className="v">{stats?.replies ?? 0}</div><div className="l">Replies ({rate(stats?.replies ?? 0)})</div></div>
        <div className="kpi"><div className="v">{stats?.clicks ?? 0}</div><div className="l">Clicks</div></div>
        <div className="kpi"><div className="v">{stats?.bounces ?? 0}</div><div className="l">Bounces</div></div>
      </div>

      <div className="card" style={{ marginTop: 18 }}>
        <h3>Campaigns</h3>
        <table>
          <thead><tr><th>Name</th><th>Status</th></tr></thead>
          <tbody>
            {campaigns.map((c) => (
              <tr key={c.id}>
                <td><a href={`/campaigns/${c.id}`}>{c.name}</a></td>
                <td><span className={`badge ${c.status}`}>{c.status}</span></td>
              </tr>
            ))}
            {campaigns.length === 0 && <tr><td colSpan={2} className="muted">No campaigns yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}
