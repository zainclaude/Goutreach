import { NavLink, Route, Routes, useNavigate, Navigate } from "react-router-dom";
import { getToken, clearToken } from "./api";
import Login from "./pages/Login";
import Dashboard from "./pages/Dashboard";
import Accounts from "./pages/Accounts";
import Leads from "./pages/Leads";
import Campaigns from "./pages/Campaigns";
import CampaignDetail from "./pages/CampaignDetail";
import Inbox from "./pages/Inbox";
import Templates from "./pages/Templates";
import Settings from "./pages/Settings";

function Shell({ children }: { children: React.ReactNode }) {
  const nav = useNavigate();
  const logout = () => { clearToken(); nav("/login"); };
  const link = ({ isActive }: { isActive: boolean }) => (isActive ? "active" : "");
  return (
    <div className="layout">
      <div className="sidebar">
        <h1>Goutreach</h1>
        <nav>
          <NavLink to="/" end className={link}>Dashboard</NavLink>
          <NavLink to="/campaigns" className={link}>Campaigns</NavLink>
          <NavLink to="/leads" className={link}>Leads</NavLink>
          <NavLink to="/accounts" className={link}>Email Accounts</NavLink>
          <NavLink to="/inbox" className={link}>Inbox</NavLink>
          <NavLink to="/templates" className={link}>Templates</NavLink>
          <NavLink to="/settings" className={link}>Settings</NavLink>
        </nav>
        <div style={{ marginTop: 24 }}>
          <button className="secondary" onClick={logout}>Log out</button>
        </div>
      </div>
      <div className="main">{children}</div>
    </div>
  );
}

function Protected({ children }: { children: React.ReactNode }) {
  if (!getToken()) return <Navigate to="/login" replace />;
  return <Shell>{children}</Shell>;
}

export default function App() {
  return (
    <Routes>
      <Route path="/login" element={<Login />} />
      <Route path="/" element={<Protected><Dashboard /></Protected>} />
      <Route path="/campaigns" element={<Protected><Campaigns /></Protected>} />
      <Route path="/campaigns/:id" element={<Protected><CampaignDetail /></Protected>} />
      <Route path="/leads" element={<Protected><Leads /></Protected>} />
      <Route path="/accounts" element={<Protected><Accounts /></Protected>} />
      <Route path="/inbox" element={<Protected><Inbox /></Protected>} />
      <Route path="/templates" element={<Protected><Templates /></Protected>} />
      <Route path="/settings" element={<Protected><Settings /></Protected>} />
    </Routes>
  );
}
