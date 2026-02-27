import { useState } from "react";
import { useAuth } from "../context/AuthContext";

export default function LoginPage() {
  const { login, loading, error } = useAuth();
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [showPass, setShowPass] = useState(false);

  const handleSubmit = async (e) => {
    e.preventDefault();
    await login(username, password);
  };

  return (
    <div style={s.shell}>
      <div style={s.card}>
        {/* Logo */}
        <div style={s.logoRow}>
          <svg width="36" height="36" viewBox="0 0 28 28" fill="none">
            <rect width="28" height="28" rx="6" fill="#6366f1"/>
            <path d="M7 14h4l3-7 4 14 3-7h4" stroke="#fff" strokeWidth="2"
              strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
          <span style={s.logoText}>TelemetrySearch</span>
        </div>

        <h1 style={s.title}>Sign in</h1>
        <p style={s.subtitle}>Use your credentials to access the platform</p>

        <form onSubmit={handleSubmit} style={s.form}>
          <div style={s.fieldGroup}>
            <label style={s.label}>Username</label>
            <input
              style={s.input}
              type="text"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="admin"
              autoComplete="username"
              autoFocus
              required
            />
          </div>

          <div style={s.fieldGroup}>
            <label style={s.label}>Password</label>
            <div style={s.passWrap}>
              <input
                style={{ ...s.input, paddingRight: 44 }}
                type={showPass ? "text" : "password"}
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder="••••••••"
                autoComplete="current-password"
                required
              />
              <button
                type="button"
                style={s.eyeBtn}
                onClick={() => setShowPass((v) => !v)}
                aria-label={showPass ? "Hide password" : "Show password"}
              >
                {showPass ? "🙈" : "👁"}
              </button>
            </div>
          </div>

          {error && (
            <div style={s.errorBox} role="alert">
              ⚠ {error}
            </div>
          )}

          <button type="submit" style={s.submitBtn} disabled={loading}>
            {loading ? "Signing in…" : "Sign in →"}
          </button>
        </form>

        {/* Role hints for dev */}
        <div style={s.hints}>
          <div style={s.hintTitle}>Default accounts (dev only)</div>
          {[
            { user: "admin", pass: "admin123", role: "Admin — full access" },
            { user: "writer", pass: "writer123", role: "Writer — upload & search" },
            { user: "reader", pass: "reader123", role: "Reader — search only" },
          ].map(({ user, pass, role }) => (
            <button
              key={user}
              style={s.hintBtn}
              type="button"
              onClick={() => { setUsername(user); setPassword(pass); }}
            >
              <span style={s.hintUser}>{user}</span>
              <span style={s.hintRole}>{role}</span>
            </button>
          ))}
        </div>
      </div>
    </div>
  );
}

const s = {
  shell: {
    minHeight: "100vh", display: "flex", alignItems: "center", justifyContent: "center",
    background: "#0f172a", fontFamily: "'Inter', 'Segoe UI', sans-serif",
  },
  card: {
    width: 400, background: "#1e293b", border: "1px solid #334155",
    borderRadius: 16, padding: "36px 40px",
  },
  logoRow: { display: "flex", alignItems: "center", gap: 10, marginBottom: 24 },
  logoText: { fontSize: 16, fontWeight: 700, color: "#f1f5f9" },
  title: { margin: "0 0 4px", fontSize: 22, fontWeight: 700, color: "#f1f5f9" },
  subtitle: { margin: "0 0 28px", fontSize: 13, color: "#64748b" },
  form: { display: "flex", flexDirection: "column", gap: 18 },
  fieldGroup: { display: "flex", flexDirection: "column", gap: 6 },
  label: { fontSize: 13, fontWeight: 600, color: "#94a3b8" },
  input: {
    background: "#0f172a", border: "1px solid #334155", borderRadius: 8,
    padding: "10px 14px", color: "#e2e8f0", fontSize: 14, outline: "none",
    width: "100%", boxSizing: "border-box",
  },
  passWrap: { position: "relative" },
  eyeBtn: {
    position: "absolute", right: 10, top: "50%", transform: "translateY(-50%)",
    background: "none", border: "none", cursor: "pointer", fontSize: 16, padding: 4,
  },
  errorBox: {
    background: "#450a0a", color: "#f87171", border: "1px solid #7f1d1d",
    borderRadius: 8, padding: "10px 14px", fontSize: 13,
  },
  submitBtn: {
    background: "#6366f1", color: "#fff", border: "none", borderRadius: 8,
    padding: "12px", fontSize: 15, fontWeight: 600, cursor: "pointer",
    marginTop: 4, transition: "opacity 0.15s",
  },
  hints: {
    marginTop: 28, borderTop: "1px solid #334155", paddingTop: 20,
  },
  hintTitle: { fontSize: 11, color: "#475569", fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 10 },
  hintBtn: {
    display: "flex", justifyContent: "space-between", alignItems: "center",
    width: "100%", background: "#0f172a", border: "1px solid #334155", borderRadius: 8,
    padding: "8px 12px", marginBottom: 6, cursor: "pointer",
  },
  hintUser: { fontSize: 13, color: "#a5b4fc", fontWeight: 600 },
  hintRole: { fontSize: 11, color: "#64748b" },
};
