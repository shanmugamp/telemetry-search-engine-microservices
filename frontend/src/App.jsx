import { useState, useEffect } from "react";
import { useAuth } from "./context/AuthContext";
import LoginPage from "./pages/LoginPage";
import SearchPage from "./pages/SearchPage";
import UploadPage from "./pages/UploadPage";
import { getStats, getFiles } from "./utils/api";

export default function App() {
  const { isAuthenticated, user, logout, canWrite } = useAuth();
  if (!isAuthenticated) return <LoginPage />;
  return <AuthenticatedShell user={user} logout={logout} canWrite={canWrite} />;
}

function AuthenticatedShell({ user, logout, canWrite }) {
  const [page, setPage] = useState("search");
  const [stats, setStats] = useState(null);
  const [files, setFiles] = useState([]);

  const fetchStats = async () => {
    try {
      const res = await getStats();
      setStats(res.data);
    } catch (_) {}
  };

  const fetchFiles = async () => {
    try {
      const res = await getFiles();
      setFiles(res.data?.files ?? []);
    } catch (_) {}
  };

  const refreshAll = () => {
    fetchStats();
    fetchFiles();
  };

  useEffect(() => {
    refreshAll();
    const interval = setInterval(refreshAll, 10000);
    return () => clearInterval(interval);
  }, []);

  // Handle forced logout from API 401
  useEffect(() => {
    const handler = () => logout();
    window.addEventListener("auth:logout", handler);
    return () => window.removeEventListener("auth:logout", handler);
  }, [logout]);

  const roleBadgeColor = {
    admin:  { bg: "#3b0764", color: "#c084fc" },
    writer: { bg: "#1e3a5f", color: "#60a5fa" },
    reader: { bg: "#052e16", color: "#34d399" },
  }[user?.role] || { bg: "#1e293b", color: "#94a3b8" };

  return (
    <div style={styles.shell}>
      <aside style={styles.sidebar}>
        {/* Logo */}
        <div style={styles.logo}>
          <svg width="28" height="28" viewBox="0 0 28 28" fill="none">
            <rect width="28" height="28" rx="6" fill="#6366f1"/>
            <path d="M7 14h4l3-7 4 14 3-7h4" stroke="#fff" strokeWidth="2"
              strokeLinecap="round" strokeLinejoin="round"/>
          </svg>
          <span style={styles.logoText}>TelemetrySearch</span>
        </div>

        {/* User badge */}
        <div style={styles.userBox}>
          <div style={styles.userAvatar}>{user?.username?.[0]?.toUpperCase()}</div>
          <div style={{ minWidth: 0 }}>
            <div style={styles.userName}>{user?.username}</div>
            <span style={{ ...styles.roleBadge, background: roleBadgeColor.bg, color: roleBadgeColor.color }}>
              {user?.role}
            </span>
          </div>
        </div>

        {/* Nav */}
        <nav style={styles.nav}>
          <NavItem icon="🔍" label="Search" active={page === "search"} onClick={() => setPage("search")} />
          {canWrite && (
            <NavItem icon="📂" label="Upload" active={page === "upload"} onClick={() => setPage("upload")} />
          )}
        </nav>

        {/* Index stats */}
        <div style={styles.statsBox}>
          <div style={styles.statsTitle}>Index Status</div>

          {stats ? (
            <>
              <StatRow label="Documents" value={stats.total_documents?.toLocaleString() ?? "—"} />
              <StatRow label="Terms"     value={stats.total_terms?.toLocaleString()     ?? "—"} />
              <div style={styles.readyRow}>
                <span style={{ color: stats.index_ready ? "#34d399" : "#f59e0b", fontSize: 11, fontWeight: 600 }}>
                  {stats.index_ready ? "● Ready" : "⟳ Loading…"}
                </span>
              </div>
            </>
          ) : (
            <div style={{ fontSize: 11, color: "#475569" }}>Loading…</div>
          )}

          {/* Files from ingest-service */}
          <div style={{ ...styles.statsTitle, marginTop: 14 }}>
            Uploaded Files
            <span style={styles.fileBadge}>{files.length}</span>
          </div>
          {files.length === 0 ? (
            <div style={{ fontSize: 11, color: "#475569" }}>No files uploaded yet</div>
          ) : (
            <div style={styles.fileList}>
              {files.map((f) => (
                <div key={f.name} style={styles.fileItem} title={`${(f.size_bytes / 1024).toFixed(1)} KB · ${f.mod_time}`}>
                  📄 {f.name}
                  <span style={styles.fileSize}>{formatBytes(f.size_bytes)}</span>
                </div>
              ))}
            </div>
          )}
        </div>

        <button style={styles.logoutBtn} onClick={logout}>Sign out →</button>
      </aside>

      <main style={styles.main}>
        {page === "search" ? (
          <SearchPage onStatsRefresh={refreshAll} />
        ) : (
          <UploadPage onUploadComplete={() => { refreshAll(); setPage("search"); }} />
        )}
      </main>
    </div>
  );
}

function NavItem({ icon, label, active, onClick }) {
  return (
    <button onClick={onClick} style={{
      ...styles.navItem,
      background:  active ? "rgba(99,102,241,0.15)" : "transparent",
      color:       active ? "#a5b4fc" : "#94a3b8",
      borderLeft:  active ? "3px solid #6366f1" : "3px solid transparent",
    }}>
      <span style={{ marginRight: 10 }}>{icon}</span>{label}
    </button>
  );
}

function StatRow({ label, value }) {
  return (
    <div style={styles.statRow}>
      <span style={styles.statLabel}>{label}</span>
      <span style={styles.statValue}>{value}</span>
    </div>
  );
}

function formatBytes(b) {
  if (b < 1024)        return b + " B";
  if (b < 1048576)     return (b / 1024).toFixed(1) + " KB";
  return (b / 1048576).toFixed(1) + " MB";
}

const styles = {
  shell: { display: "flex", height: "100vh", background: "#0f172a", color: "#e2e8f0", fontFamily: "'Inter','Segoe UI',sans-serif", overflow: "hidden" },
  sidebar: { width: 240, minWidth: 240, background: "#1e293b", borderRight: "1px solid #334155", display: "flex", flexDirection: "column", overflow: "hidden" },
  logo: { display: "flex", alignItems: "center", gap: 10, padding: "20px 16px", borderBottom: "1px solid #334155" },
  logoText: { fontWeight: 700, fontSize: 15, color: "#f1f5f9" },
  userBox: { display: "flex", alignItems: "center", gap: 10, padding: "12px 16px", borderBottom: "1px solid #334155" },
  userAvatar: { width: 32, height: 32, borderRadius: "50%", background: "#6366f1", color: "#fff", display: "flex", alignItems: "center", justifyContent: "center", fontWeight: 700, fontSize: 14, flexShrink: 0 },
  userName: { fontSize: 13, fontWeight: 600, color: "#e2e8f0", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
  roleBadge: { fontSize: 10, fontWeight: 700, padding: "1px 6px", borderRadius: 4, textTransform: "uppercase", letterSpacing: "0.06em" },
  nav: { padding: "12px 0", display: "flex", flexDirection: "column", gap: 2 },
  navItem: { display: "flex", alignItems: "center", padding: "10px 16px", cursor: "pointer", border: "none", width: "100%", textAlign: "left", fontSize: 14, fontWeight: 500, transition: "all 0.15s", borderRadius: "0 6px 6px 0" },
  statsBox: { margin: "12px", background: "#0f172a", borderRadius: 8, padding: "12px", border: "1px solid #334155", flex: 1, overflow: "auto" },
  statsTitle: { fontSize: 11, fontWeight: 600, color: "#64748b", textTransform: "uppercase", letterSpacing: "0.08em", marginBottom: 8, display: "flex", alignItems: "center", gap: 6 },
  statRow: { display: "flex", justifyContent: "space-between", marginBottom: 6 },
  statLabel: { fontSize: 12, color: "#94a3b8" },
  statValue: { fontSize: 12, fontWeight: 600, color: "#a5b4fc" },
  readyRow: { marginBottom: 4 },
  fileBadge: { background: "#1e293b", color: "#60a5fa", borderRadius: 9, padding: "1px 6px", fontSize: 10, fontWeight: 700 },
  fileList: { marginTop: 4 },
  fileItem: { fontSize: 11, color: "#64748b", marginBottom: 5, display: "flex", justifyContent: "space-between", alignItems: "center", overflow: "hidden" },
  fileSize: { fontSize: 10, color: "#475569", flexShrink: 0, marginLeft: 4 },
  logoutBtn: { margin: "0 12px 12px", background: "transparent", border: "1px solid #334155", borderRadius: 8, padding: "8px 12px", color: "#64748b", cursor: "pointer", fontSize: 13, textAlign: "left" },
  main: { flex: "1 1 0", minHeight: 0, overflow: "hidden", display: "flex", flexDirection: "column" },
};
