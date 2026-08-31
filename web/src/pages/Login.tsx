import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { api, setToken } from "../api";

export default function Login() {
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [mode, setMode] = useState<"login" | "signup">("login");
  const [err, setErr] = useState("");
  const nav = useNavigate();
  const expired = new URLSearchParams(window.location.search).get("expired") === "1";

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setErr("");
    try {
      const r = await api.post<{ token: string }>(`/auth/${mode}`, { email, password });
      setToken(r.token);
      nav("/");
    } catch (e: any) {
      setErr(e.message);
    }
  };

  return (
    <div className="login-wrap">
      <form className="card login-card" onSubmit={submit}>
        <h2>PipelineBuilder</h2>
        <p className="muted">{mode === "login" ? "Sign in to your dashboard" : "Create the first account"}</p>
        {expired && <p className="err">Your session expired — log in again to continue.</p>}
        <label>Email</label>
        <input value={email} onChange={(e) => setEmail(e.target.value)} type="email" required />
        <label>Password</label>
        <input value={password} onChange={(e) => setPassword(e.target.value)} type="password" required />
        {err && <p className="err">{err}</p>}
        <div style={{ marginTop: 14 }}>
          <button type="submit">{mode === "login" ? "Log in" : "Sign up"}</button>
        </div>
        <p className="muted" style={{ marginTop: 12, cursor: "pointer" }}
           onClick={() => setMode(mode === "login" ? "signup" : "login")}>
          {mode === "login" ? "Need to create the first account? Sign up" : "Already have an account? Log in"}
        </p>
      </form>
    </div>
  );
}
