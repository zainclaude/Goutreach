import { useEffect, useState } from "react";
import { api, Domain, DomainAvailability } from "../api";

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

  const load = () => api.get<Domain[]>("/domains").then((d) => setDomains(d || []));
  useEffect(() => {
    load();
    api.get<{ key: string; is_secret: boolean }[]>("/settings")
      .then((s) => setKeysSaved((s || []).some((x) => x.key === "porkbun_api_key")))
      .catch(() => {});
  }, []);

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
    </div>
  );
}
