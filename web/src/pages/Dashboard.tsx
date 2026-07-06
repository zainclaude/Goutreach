import { useEffect, useState } from "react";
import { api, Stats, Campaign } from "../api";

const RANGES = [
  { value: "today", label: "Today" },
  { value: "7d", label: "Last 7 days" },
  { value: "30d", label: "Last 30 days" },
  { value: "all", label: "All time" },
];

export default function Dashboard() {
  const [stats, setStats] = useState<Stats | null>(null);
  const [campaigns, setCampaigns] = useState<Campaign[]>([]);
  const [aiEnabled, setAiEnabled] = useState(true);
  const [range, setRange] = useState(() => {
    const stored = localStorage.getItem("dash_range");
    if (stored === "3d") return "30d"; // option replaced
    return RANGES.some((r) => r.value === stored) ? (stored as string) : "today";
  });

  useEffect(() => {
    api.get<Stats>(`/overview?range=${range}`).then(setStats).catch(() => {});
    localStorage.setItem("dash_range", range);
  }, [range]);

  useEffect(() => {
    api.get<Campaign[]>("/campaigns").then((c) => setCampaigns(c || [])).catch(() => {});
    api.get<{ ai_enabled: boolean }>("/me").then((m) => setAiEnabled(m.ai_enabled)).catch(() => {});
  }, []);

  const rate = (n: number) => (stats && stats.sent > 0 ? ((n / stats.sent) * 100).toFixed(1) + "%" : "—");

  return (
    <div>
      <div className="flex-between">
        <h2>Dashboard</h2>
        <select style={{ width: "auto" }} value={range} onChange={(e) => setRange(e.target.value)}>
          {RANGES.map((r) => <option key={r.value} value={r.value}>{r.label}</option>)}
        </select>
      </div>
      {!aiEnabled && (
        <div className="card" style={{ borderColor: "var(--danger)" }}>
          <b>AI generation is disabled.</b> Set <code>ANTHROPIC_API_KEY</code> on the server to enable
          research-driven email writing.
        </div>
      )}
      <div className="kpis">
        <div className="kpi"><div className="v">{stats?.sent ?? 0}</div><div className="l">Emails sent · {RANGES.find((r) => r.value === range)?.label}</div></div>
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
