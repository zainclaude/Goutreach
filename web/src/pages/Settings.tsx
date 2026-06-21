import { useEffect, useState } from "react";
import { api, Setting } from "../api";

// Integration credential fields. Passwords are stored encrypted (is_secret).
const FIELDS: { key: string; label: string; secret: boolean }[] = [
  { key: "kalodata_email", label: "Kalodata email", secret: false },
  { key: "kalodata_password", label: "Kalodata password", secret: true },
  { key: "fastmoss_email", label: "Fastmoss email (fallback)", secret: false },
  { key: "fastmoss_password", label: "Fastmoss password (fallback)", secret: true },
];

export default function Settings() {
  const [existing, setExisting] = useState<Record<string, Setting>>({});
  const [vals, setVals] = useState<Record<string, string>>({});
  const [msg, setMsg] = useState("");

  const load = () => api.get<Setting[]>("/settings").then((s) => {
    const m: Record<string, Setting> = {};
    (s || []).forEach((x) => (m[x.key] = x));
    setExisting(m);
    const v: Record<string, string> = {};
    (s || []).forEach((x) => { if (!x.is_secret) v[x.key] = x.value; });
    setVals((prev) => ({ ...v, ...prev }));
  });
  useEffect(() => { load(); }, []);

  const save = async (key: string, secret: boolean) => {
    await api.put("/settings", { key, value: vals[key] || "", is_secret: secret });
    setMsg(`Saved ${key}`); load();
  };

  return (
    <div>
      <h2>Settings</h2>
      <div className="card">
        <h3>TikTok Shop data providers</h3>
        <p className="muted">
          Step 1 of email research checks whether a brand is on TikTok Shop via Kalodata, falling back
          to Fastmoss. Passwords are encrypted at rest and never shown back.
        </p>
        {FIELDS.map((f) => (
          <div key={f.key} style={{ marginBottom: 10 }}>
            <label>{f.label} {existing[f.key] && f.secret && <span className="tag">set</span>}</label>
            <div className="row">
              <input
                type={f.secret ? "password" : "text"}
                value={vals[f.key] || ""}
                placeholder={f.secret && existing[f.key] ? "•••••••• (leave blank to keep)" : ""}
                onChange={(e) => setVals({ ...vals, [f.key]: e.target.value })}
              />
              <button onClick={() => save(f.key, f.secret)}>Save</button>
            </div>
          </div>
        ))}
        {msg && <p className="ok">{msg}</p>}
      </div>
    </div>
  );
}
