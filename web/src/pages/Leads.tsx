import { useEffect, useRef, useState } from "react";
import { api, uploadCSV, Lead, LeadImport, LeadStats, LeadsPage, ImportFileResult } from "../api";

const PER_PAGE = 100;

export default function Leads() {
  const [leads, setLeads] = useState<Lead[]>([]);
  const [counts, setCounts] = useState<LeadStats | null>(null);
  const [page, setPage] = useState(1);
  const [totalFiltered, setTotalFiltered] = useState(0);
  const [search, setSearch] = useState("");   // what's in the box
  const [query, setQuery] = useState("");     // debounced, drives the fetch
  const [loading, setLoading] = useState(false);
  const [form, setForm] = useState({ email: "", first_name: "", last_name: "", company: "", title: "" });
  const [msg, setMsg] = useState("");
  const [history, setHistory] = useState<LeadImport[] | null>(null); // null = panel closed
  const debounce = useRef<number | undefined>(undefined);

  const load = (p = page, q = query) => {
    setLoading(true);
    return api.get<LeadsPage>(`/leads?page=${p}&per_page=${PER_PAGE}&q=${encodeURIComponent(q)}`)
      .then((r) => {
        setLeads(r.leads || []);
        if (r.counts) setCounts(r.counts); // only sent on page 1 — keep the last known counts otherwise
        setTotalFiltered(r.total_filtered);
        setPage(r.page);
      })
      .finally(() => setLoading(false));
  };
  useEffect(() => { load(page, query); }, [page, query]);

  // Debounce typing so every keystroke doesn't hit the server.
  const onSearch = (v: string) => {
    setSearch(v);
    window.clearTimeout(debounce.current);
    debounce.current = window.setTimeout(() => { setPage(1); setQuery(v.trim()); }, 300);
  };

  const totalPages = Math.max(1, Math.ceil(totalFiltered / PER_PAGE));

  const loadHistory = () => api.get<LeadImport[]>("/leads/imports").then((h) => setHistory(h || []));
  const toggleHistory = () => (history === null ? loadHistory() : setHistory(null));

  const set = (k: string, v: string) => setForm({ ...form, [k]: v });
  const add = async () => {
    try { await api.post("/leads", form); setForm({ email: "", first_name: "", last_name: "", company: "", title: "" }); load(); }
    catch (e: any) { setMsg(e.message); }
  };
  const onFile = async (e: React.ChangeEvent<HTMLInputElement>) => {
    const files = Array.from(e.target.files || []);
    if (files.length === 0) return;
    e.target.value = ""; // allow re-selecting the same file(s)
    setMsg("Uploading…");
    try {
      const r = await uploadCSV(files);
      const parts = (r.files as ImportFileResult[] || []).map((f) =>
        f.error
          ? `✗ ${f.filename}: ${f.error}`
          : `${f.filename}: ${f.imported} imported, ${f.updated} updated, ${f.skipped} skipped${f.blacklisted ? `, ${f.blacklisted} blacklisted` : ""}`);
      let m = parts.join(" · ");
      if (r.blacklisted > 0) m += ` — ⚠️ blacklisted domains: ${(r.blacklisted_emails || []).join(", ")}`;
      setMsg(m); load();
      if (history !== null) loadHistory();
    } catch (e: any) { setMsg("✗ " + e.message); }
  };
  const remove = async (id: number) => { await api.del(`/leads/${id}`); load(); };
  const verify = async () => {
    setMsg("");
    try {
      const r = await api.post<{ queued: number }>("/leads/verify", {});
      setMsg(r.queued > 0
        ? `Verifying ${r.queued} lead(s) in the background — refresh in a bit to see results.`
        : "All leads already verified.");
    } catch (e: any) { setMsg("✗ " + e.message); }
  };

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
          <span className="muted">or import CSV — select multiple files at once (columns: email, first_name, last_name, company, title):</span>
          <input type="file" accept=".csv" multiple onChange={onFile} style={{ width: "auto" }} />
          <button className="secondary" onClick={toggleHistory}>{history === null ? "Upload history" : "Hide history"}</button>
        </div>
        {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
        {history !== null && (
          <div style={{ marginTop: 10 }}>
            <table>
              <thead><tr><th>File</th><th>Imported</th><th>Updated</th><th>Skipped</th><th>Blacklisted</th><th>When</th></tr></thead>
              <tbody>
                {history.map((h) => (
                  <tr key={h.id}>
                    <td>{h.filename}</td>
                    <td>{h.imported}</td>
                    <td>{h.updated}</td>
                    <td>{h.skipped}</td>
                    <td>{h.blacklisted}</td>
                    <td className="muted">{new Date(h.created_at).toLocaleString()}</td>
                  </tr>
                ))}
                {history.length === 0 && <tr><td colSpan={6} className="muted">No uploads yet.</td></tr>}
              </tbody>
            </table>
          </div>
        )}
      </div>

      <div className="card">
        <div className="flex-between">
          <h3>
            All leads ({counts ? counts.total.toLocaleString() : "…"})
            <span className="muted" style={{ fontWeight: 400, fontSize: 13 }}>
              {counts && <>
                {" "}· {counts.uncontacted.toLocaleString()} uncontacted
                {" "}· {counts.unverified.toLocaleString()} unverified
                {" "}· {counts.unenrolled.toLocaleString()} not in any campaign
              </>}
            </span>
          </h3>
          <button className="secondary" onClick={verify} title="Check deliverability via your verification provider (configure it in Settings)">Verify emails</button>
        </div>

        <div className="row" style={{ alignItems: "center", marginBottom: 10 }}>
          <input placeholder="Search email, name, company, title…" value={search}
            onChange={(e) => onSearch(e.target.value)} style={{ maxWidth: 320 }} />
          {query && <span className="muted">{totalFiltered.toLocaleString()} match{totalFiltered === 1 ? "" : "es"}</span>}
          <Pager page={page} totalPages={totalPages} loading={loading} onPage={setPage} />
        </div>

        <table>
          <thead><tr><th>Email</th><th>Verification</th><th>Status</th><th>Name</th><th>Company</th><th>Title</th><th>Source file</th><th></th></tr></thead>
          <tbody>
            {leads.map((l) => (
              <tr key={l.id}>
                <td>{l.email}</td>
                <td><VerifyBadge status={l.verification_status} /></td>
                <td>{l.contacted
                  ? <span className="badge bounced" title="Emailed before">contacted</span>
                  : <span className="badge active" title="Never emailed">uncontacted</span>}</td>
                <td>{l.first_name} {l.last_name}</td>
                <td>{l.company}</td>
                <td>{l.title}</td>
                <td className="muted" title="The upload this lead first arrived in">{l.source_file || "—"}</td>
                <td><button className="danger" onClick={() => remove(l.id)}>×</button></td>
              </tr>
            ))}
            {leads.length === 0 && !loading && <tr><td colSpan={8} className="muted">{query ? "No leads match your search." : "No leads yet."}</td></tr>}
            {leads.length === 0 && loading && <tr><td colSpan={8} className="muted">Loading…</td></tr>}
          </tbody>
        </table>

        <div className="row" style={{ justifyContent: "flex-end", marginTop: 10 }}>
          <Pager page={page} totalPages={totalPages} loading={loading} onPage={setPage} />
        </div>
      </div>
    </div>
  );
}

// Pager is the ‹ Prev / page N of M / Next › control (with a jump box so 600+
// pages stay navigable).
function Pager({ page, totalPages, loading, onPage }:
  { page: number; totalPages: number; loading: boolean; onPage: (p: number) => void }) {
  const [jump, setJump] = useState("");
  const go = (p: number) => onPage(Math.min(totalPages, Math.max(1, p)));
  return (
    <div className="row" style={{ alignItems: "center", marginLeft: "auto", gap: 6 }}>
      <button className="secondary" disabled={page <= 1 || loading} onClick={() => go(page - 1)}>‹ Prev</button>
      <span className="muted" style={{ whiteSpace: "nowrap" }}>page {page.toLocaleString()} of {totalPages.toLocaleString()}</span>
      <button className="secondary" disabled={page >= totalPages || loading} onClick={() => go(page + 1)}>Next ›</button>
      <input value={jump} onChange={(e) => setJump(e.target.value.replace(/\D/g, ""))}
        onKeyDown={(e) => { if (e.key === "Enter" && jump) { go(Number(jump)); setJump(""); } }}
        placeholder="go to…" style={{ width: 64 }} title="Type a page number and press Enter" />
    </div>
  );
}

// VerifyBadge shows the email-verification result for a lead.
function VerifyBadge({ status }: { status: string }) {
  switch (status) {
    case "valid":
      return <span className="badge active" title="Deliverable">valid</span>;
    case "invalid":
      return <span className="badge bounced" title="Undeliverable — will be skipped when sending">invalid</span>;
    case "risky":
      return <span className="badge" title="Risky (spam-trap/abuse/role)" style={{ background: "#7a5b00", color: "#ffd778" }}>risky</span>;
    case "catch_all":
      return <span className="badge" title="Catch-all domain — accepts all addresses, can't confirm" style={{ background: "#33405e", color: "#9db4e8" }}>catch-all</span>;
    default:
      return <span className="muted" title="Not verified yet">—</span>;
  }
}
