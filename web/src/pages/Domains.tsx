import { useEffect, useState } from "react";
import { api, Domain, DomainAvailability, MaildosoDomain, MaildosoSync } from "../api";

// Domains: the "done-for-you" sending-domain workflow. Configure Porkbun, search
// + buy (or import) a domain, then apply cold-email DNS. Mailbox provisioning on
// these domains (Google Workspace) lands in phase 2.
export default function Domains() {
  const [domains, setDomains] = useState<Domain[]>([]);
  const [msg, setMsg] = useState("");
  const [busy, setBusy] = useState(false);

  // Porkbun credentials.
  const [apiKey, setApiKey] = useState("");
  const [secretKey, setSecretKey] = useState("");
  const [keysSaved, setKeysSaved] = useState(false);

  // Search + buy.
  const [query, setQuery] = useState("");
  const [avail, setAvail] = useState<DomainAvailability | null>(null);
  const [years, setYears] = useState(1);
  const [whois, setWhois] = useState(true);
  const [autoRenew, setAutoRenew] = useState(true);

  // Maildoso provisioning (phase 2).
  const [mdKey, setMdKey] = useState("");
  const [mdBase, setMdBase] = useState("");
  const [mdSaved, setMdSaved] = useState(false);
  const [mdDomains, setMdDomains] = useState<MaildosoDomain[]>([]);
  const [mdDomainId, setMdDomainId] = useState("");
  const [mdNewDomain, setMdNewDomain] = useState("");
  const [localParts, setLocalParts] = useState("jane.doe\njohn.smith");
  const [mdMsg, setMdMsg] = useState("");

  const load = () => api.get<Domain[]>("/domains").then((d) => setDomains(d || []));
  useEffect(() => {
    load();
    api.get<{ key: string; is_secret: boolean }[]>("/settings")
      .then((s) => {
        const keys = (s || []).map((x) => x.key);
        setKeysSaved(keys.includes("porkbun_api_key"));
        setMdSaved(keys.includes("maildoso_api_key"));
      })
      .catch(() => {});
  }, []);

  const saveMaildoso = async () => {
    setMdMsg("");
    try {
      if (mdKey) await api.put("/settings", { key: "maildoso_api_key", value: mdKey, is_secret: true });
      if (mdBase) await api.put("/settings", { key: "maildoso_base_url", value: mdBase, is_secret: false });
      await api.get("/maildoso/ping");
      setMdMsg("✓ Maildoso connected"); setMdSaved(true); setMdKey("");
    } catch (e: any) { setMdMsg("✗ " + e.message); }
  };

  const loadMdDomains = async () => {
    setMdMsg("");
    try {
      const d = await api.get<MaildosoDomain[]>("/maildoso/domains");
      setMdDomains(d || []);
      if ((d || []).length && !mdDomainId) setMdDomainId(d[0].id);
      if (!(d || []).length) setMdMsg("No domains in Maildoso yet — add one below.");
    } catch (e: any) { setMdMsg("✗ " + e.message); }
  };

  const createMdDomain = async () => {
    const d = mdNewDomain.trim();
    if (!d) return;
    setMdMsg("");
    try {
      await api.post("/maildoso/domains", { domain: d });
      setMdMsg(`✓ Added ${d} to Maildoso`); setMdNewDomain(""); loadMdDomains(); load();
    } catch (e: any) { setMdMsg("✗ " + e.message); }
  };

  const orderMailboxes = async () => {
    const parts = localParts.split(/[\n,]/).map((s) => s.trim()).filter(Boolean);
    if (!mdDomainId || parts.length === 0) { setMdMsg("✗ Pick a domain and add at least one inbox name"); return; }
    if (!confirm(`Order ${parts.length} mailbox(es) on this domain via Maildoso? This charges your Maildoso account.`)) return;
    setMdMsg("Ordering mailboxes…");
    try {
      const r = await api.post<{ ordered: number }>("/maildoso/mailboxes", { domain_id: mdDomainId, local_parts: parts });
      setMdMsg(`✓ Ordered ${r.ordered} mailbox(es). When provisioning finishes, click "Sync" to connect them.`);
    } catch (e: any) { setMdMsg("✗ " + e.message); }
  };

  const syncMaildoso = async () => {
    setMdMsg("Syncing provisioned inboxes…");
    try {
      const r = await api.post<MaildosoSync>("/maildoso/sync", {});
      let m = `✓ Connected ${r.added}, ${r.pending} still provisioning, ${r.failed} failed`;
      if (r.errors?.length) m += ": " + r.errors.map((x) => `${x.email} (${x.error})`).join("; ");
      setMdMsg(m);
    } catch (e: any) { setMdMsg("✗ " + e.message); }
  };

  const saveKeys = async () => {
    setBusy(true); setMsg("");
    try {
      if (apiKey) await api.put("/settings", { key: "porkbun_api_key", value: apiKey, is_secret: true });
      if (secretKey) await api.put("/settings", { key: "porkbun_secret_key", value: secretKey, is_secret: true });
      const r = await api.get<{ ok: boolean; ip: string }>("/domains/ping");
      setMsg(`✓ Porkbun connected (API IP ${r.ip})`);
      setKeysSaved(true); setApiKey(""); setSecretKey("");
    } catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };

  const check = async () => {
    setBusy(true); setMsg(""); setAvail(null);
    try {
      const r = await api.get<DomainAvailability>(`/domains/check?domain=${encodeURIComponent(query)}`);
      setAvail(r);
    } catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };

  const buy = async () => {
    if (!avail) return;
    if (!confirm(`Buy ${avail.domain} for $${avail.price} (${years}yr) via Porkbun? This charges your Porkbun balance.`)) return;
    setBusy(true); setMsg("");
    try {
      await api.post("/domains/register", { domain: avail.domain, years, whois_privacy: whois, auto_renew: autoRenew });
      setMsg(`✓ Purchased ${avail.domain}`); setAvail(null); setQuery(""); load();
    } catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };

  const importDomain = async () => {
    const d = (avail?.domain || query).trim();
    if (!d) return;
    setBusy(true); setMsg("");
    try {
      await api.post("/domains/import", { domain: d });
      setMsg(`✓ Imported ${d}`); setAvail(null); setQuery(""); load();
    } catch (e: any) { setMsg("✗ " + e.message); }
    finally { setBusy(false); }
  };

  const applyDNS = async (dm: Domain) => {
    if (!confirm(`Apply cold-email DNS (MX, SPF, DMARC${dm.dkim_record ? ", DKIM" : ""}) to ${dm.domain} via Porkbun?`)) return;
    setMsg("Applying DNS records…");
    try {
      const r = await api.post<{ applied: string[]; dkim_set: boolean }>(`/domains/${dm.id}/dns`, {});
      setMsg(`✓ Applied ${r.applied.join(", ")} on ${dm.domain}` + (r.dkim_set ? "" : " (DKIM pending — added once Workspace provides the key)"));
      load();
    } catch (e: any) { setMsg("✗ " + e.message); }
  };

  const remove = async (dm: Domain) => {
    if (!confirm(`Remove ${dm.domain} from PipelineBuilder? (Does not cancel the registration at Porkbun.)`)) return;
    await api.del(`/domains/${dm.id}`); load();
  };

  return (
    <div>
      <h2>Domains</h2>
      <p className="muted">
        Buy a sending domain, auto-configure its DNS, and (soon) spin up Google Workspace
        inboxes on it — the done-for-you flow, in PipelineBuilder.
      </p>

      <div className="card">
        <h3>1. Connect Porkbun {keysSaved && <span className="badge active">connected</span>}</h3>
        <p className="muted">
          Create an API key at Porkbun → Account → API Access, and enable API access on each domain.
          Keys are stored encrypted.
        </p>
        <div className="grid2">
          <div><label>API key</label><input value={apiKey} onChange={(e) => setApiKey(e.target.value)} placeholder={keysSaved ? "•••••• (saved)" : "pk1_…"} /></div>
          <div><label>Secret key</label><input type="password" value={secretKey} onChange={(e) => setSecretKey(e.target.value)} placeholder={keysSaved ? "•••••• (saved)" : "sk1_…"} /></div>
        </div>
        <div className="row" style={{ marginTop: 12 }}>
          <button disabled={busy} onClick={saveKeys}>{keysSaved ? "Update & test" : "Save & test"}</button>
        </div>
      </div>

      <div className="card">
        <h3>2. Find a domain</h3>
        <div className="row">
          <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="yourbrandmail.com"
            onKeyDown={(e) => e.key === "Enter" && check()} />
          <button className="secondary" disabled={busy || !query} onClick={check}>Check</button>
        </div>

        {avail && (
          <div style={{ marginTop: 12 }}>
            {avail.available ? (
              <>
                <p className="ok">
                  ✓ <b>{avail.domain}</b> is available — ${avail.price}/yr{avail.premium ? " (premium)" : ""}
                </p>
                <div className="grid2">
                  <div><label>Years</label><input type="number" min={1} max={10} value={years} onChange={(e) => setYears(Number(e.target.value))} /></div>
                  <div style={{ display: "flex", gap: 16, alignItems: "flex-end", paddingBottom: 4 }}>
                    <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
                      <input type="checkbox" style={{ width: "auto" }} checked={whois} onChange={(e) => setWhois(e.target.checked)} /> WHOIS privacy
                    </label>
                    <label style={{ display: "inline-flex", gap: 6, alignItems: "center" }}>
                      <input type="checkbox" style={{ width: "auto" }} checked={autoRenew} onChange={(e) => setAutoRenew(e.target.checked)} /> Auto-renew
                    </label>
                  </div>
                </div>
                <div className="row" style={{ marginTop: 12 }}>
                  <button disabled={busy} onClick={buy}>Buy via Porkbun</button>
                  <button className="secondary" disabled={busy} onClick={importDomain}>I already own it — import</button>
                </div>
              </>
            ) : (
              <>
                <p className="err">✗ <b>{avail.domain}</b> is taken.</p>
                <button className="secondary" disabled={busy} onClick={importDomain}>I own this — import it</button>
              </>
            )}
          </div>
        )}
        {msg && <p className={msg.startsWith("✗") ? "err" : "ok"}>{msg}</p>}
      </div>

      <div className="card">
        <h3>Your domains</h3>
        <table>
          <thead><tr><th>Domain</th><th>Registrar</th><th>Status</th><th>DNS</th><th></th></tr></thead>
          <tbody>
            {domains.map((d) => (
              <tr key={d.id}>
                <td>{d.domain}{d.last_error && <div className="err" style={{ fontSize: 12 }}>{d.last_error}</div>}</td>
                <td>{d.registrar}</td>
                <td><span className={`badge ${d.status === "verified" || d.status === "dns_ready" ? "active" : ""}`}>{d.status}</span></td>
                <td>{d.dns_applied ? "✓ applied" : "—"}</td>
                <td className="row">
                  <button className="secondary" onClick={() => applyDNS(d)}>Apply DNS</button>
                  <button className="danger" onClick={() => remove(d)}>×</button>
                </td>
              </tr>
            ))}
            {domains.length === 0 && <tr><td colSpan={5} className="muted">No domains yet.</td></tr>}
          </tbody>
        </table>
      </div>

      <h2 style={{ marginTop: 32 }}>Done-for-you inboxes (Maildoso)</h2>
      <p className="muted">
        Provision pre-configured sending inboxes through Maildoso — it buys/connects the
        domain, sets SPF/DKIM/DMARC, and returns ready credentials. PipelineBuilder then
        connects them into Email Accounts automatically.
      </p>

      <div className="card">
        <h3>1. Connect Maildoso {mdSaved && <span className="badge active">connected</span>}</h3>
        <p className="muted">Generate an API key in Maildoso → Settings. Stored encrypted.</p>
        <div className="grid2">
          <div><label>API key</label><input type="password" value={mdKey} onChange={(e) => setMdKey(e.target.value)} placeholder={mdSaved ? "•••••• (saved)" : "md_…"} /></div>
          <div><label>API base URL <span className="muted">(optional)</span></label><input value={mdBase} onChange={(e) => setMdBase(e.target.value)} placeholder="https://api.maildoso.com/v1" /></div>
        </div>
        <div className="row" style={{ marginTop: 12 }}>
          <button onClick={saveMaildoso}>{mdSaved ? "Update & test" : "Save & test"}</button>
        </div>
      </div>

      <div className="card">
        <h3>2. Provision inboxes</h3>
        <div className="row">
          <button className="secondary" onClick={loadMdDomains}>Load Maildoso domains</button>
          {mdDomains.length > 0 && (
            <select value={mdDomainId} onChange={(e) => setMdDomainId(e.target.value)}>
              {mdDomains.map((d) => <option key={d.id} value={d.id}>{d.domain} ({d.status})</option>)}
            </select>
          )}
        </div>
        <div className="row" style={{ marginTop: 8 }}>
          <input value={mdNewDomain} onChange={(e) => setMdNewDomain(e.target.value)} placeholder="add a new domain to Maildoso…" />
          <button className="secondary" onClick={createMdDomain} disabled={!mdNewDomain.trim()}>Add domain</button>
        </div>
        <label style={{ marginTop: 12 }}>Inbox names (one per line — the part before @)</label>
        <textarea value={localParts} onChange={(e) => setLocalParts(e.target.value)} rows={4} style={{ width: "100%", fontFamily: "monospace" }} />
        <div className="row" style={{ marginTop: 12 }}>
          <button onClick={orderMailboxes} disabled={!mdDomainId}>Order mailboxes</button>
        </div>
      </div>

      <div className="card">
        <h3>3. Connect provisioned inboxes</h3>
        <p className="muted">
          Once Maildoso finishes provisioning (can take a few minutes), sync to pull the ready
          inboxes into Email Accounts. Safe to run repeatedly — already-connected inboxes are skipped.
        </p>
        <button onClick={syncMaildoso}>Sync provisioned inboxes</button>
        {mdMsg && <p className={mdMsg.startsWith("✗") ? "err" : "ok"} style={{ marginTop: 12 }}>{mdMsg}</p>}
      </div>
    </div>
  );
}
