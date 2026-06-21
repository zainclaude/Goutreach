import { useEffect, useState } from "react";
import { api, Template } from "../api";

const HINTS: Record<string, string> = {
  A: "Used when the brand IS on TikTok Shop (kalodata/fastmoss check).",
  B: "Used when the brand sells on Amazon.",
  C: "Used when the brand is running Meta ads.",
  D: "Used when the brand has a big retail presence.",
  E: "Generic fallback when no signals are found.",
};

export default function Templates() {
  const [templates, setTemplates] = useState<Template[]>([]);
  const [msg, setMsg] = useState("");

  const load = () => api.get<Template[]>("/templates").then((t) => setTemplates(t || []));
  useEffect(() => { load(); }, []);

  const set = (i: number, k: string, v: string) => {
    const next = templates.slice(); (next[i] as any)[k] = v; setTemplates(next);
  };
  const save = async (t: Template) => {
    await api.put(`/templates/${t.key}`, { name: t.name, subject: t.subject, body: t.body });
    setMsg(`Saved template ${t.key}`);
  };

  return (
    <div>
      <h2>Templates</h2>
      <p className="muted">
        Claude picks one of these per lead via the research decision tree, then personalizes it.
        Leave a template blank to let Claude write freely for that branch.
      </p>
      {msg && <p className="ok">{msg}</p>}
      {templates.map((t, i) => (
        <div className="card" key={t.key}>
          <div className="flex-between">
            <h3>Template {t.key}</h3>
            <span className="muted">{HINTS[t.key]}</span>
          </div>
          <label>Internal name</label>
          <input value={t.name} onChange={(e) => set(i, "name", e.target.value)} />
          <label>Subject</label>
          <input value={t.subject} onChange={(e) => set(i, "subject", e.target.value)} />
          <label>Body</label>
          <textarea value={t.body} onChange={(e) => set(i, "body", e.target.value)} style={{ minHeight: 160 }} />
          <div style={{ marginTop: 8 }}><button onClick={() => save(t)}>Save</button></div>
        </div>
      ))}
    </div>
  );
}
