// Minimal typed API client. Token is stored in localStorage.

const TOKEN_KEY = "goutreach_token";

export function getToken(): string {
  return localStorage.getItem(TOKEN_KEY) || "";
}
export function setToken(t: string) {
  localStorage.setItem(TOKEN_KEY, t);
}
export function clearToken() {
  localStorage.removeItem(TOKEN_KEY);
}

async function req<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const tok = getToken();
  if (tok) headers["Authorization"] = `Bearer ${tok}`;
  let payload: BodyInit | undefined;
  if (body !== undefined) {
    headers["Content-Type"] = "application/json";
    payload = JSON.stringify(body);
  }
  const res = await fetch(`/api${path}`, { method, headers, body: payload });
  if (!res.ok) {
    let msg = `HTTP ${res.status}`;
    try {
      const j = await res.json();
      if (j.error) msg = j.error;
    } catch {}
    throw new Error(msg);
  }
  if (res.status === 204) return undefined as T;
  const text = await res.text();
  return (text ? JSON.parse(text) : undefined) as T;
}

export const api = {
  get: <T>(p: string) => req<T>("GET", p),
  post: <T>(p: string, b?: unknown) => req<T>("POST", p, b),
  put: <T>(p: string, b?: unknown) => req<T>("PUT", p, b),
  patch: <T>(p: string, b?: unknown) => req<T>("PATCH", p, b),
  del: <T>(p: string) => req<T>("DELETE", p),
};

export async function uploadCSV(file: File) {
  return uploadTo("/api/leads/import", file);
}

export async function uploadAccountsCSV(file: File) {
  return uploadTo("/api/accounts/import", file);
}

async function uploadTo(path: string, file: File) {
  const fd = new FormData();
  fd.append("file", file);
  const res = await fetch(path, {
    method: "POST",
    headers: { Authorization: `Bearer ${getToken()}` },
    body: fd,
  });
  if (!res.ok) throw new Error(`upload failed: ${res.status}`);
  return res.json();
}

// --- types ---
export interface Account {
  id: number; email: string; from_name: string;
  smtp_host: string; imap_host: string;
  daily_limit: number; warmup_enabled: boolean; warmup_target_per_day: number;
  status: string; last_error: string;
}
export interface AccountStat {
  account_id: number; email: string;
  sent_today: number; sent_lifetime: number;
  replies_today: number; replies_lifetime: number; reply_rate: number;
}
export interface Lead {
  id: number; email: string; first_name: string; last_name: string;
  company: string; title: string; status: string;
}
export interface Campaign {
  id: number; name: string; brief: string; status: string;
  timezone: string; send_start_hour: number; send_end_hour: number;
  daily_cap: number; track_opens: boolean; track_clicks: boolean;
  require_approval: boolean; approval_count: number;
}
export interface Step { step_index: number; delay_days: number; angle: string; }
export interface Message {
  id: number; step_index: number; subject: string; body: string;
  status: string; template_used: string; research_notes: string;
  approved: boolean; lead_email: string; lead_company: string;
}
export interface Stats {
  sent: number; opens: number; clicks: number; replies: number; bounces: number;
}
export interface Template { key: string; name: string; subject: string; body: string; }
export interface Setting { key: string; value: string; is_secret: boolean; }
export interface Reply {
  message_id: number; subject: string; body: string;
  lead_email: string; lead_name: string; campaign_name: string;
  replied_at: string | null; reply_snippet: string;
}
