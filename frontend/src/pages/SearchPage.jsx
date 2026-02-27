import { useState, useCallback } from "react";
import { search } from "../utils/api";

const PAGE_SIZE = 20;

// ── Helpers ──────────────────────────────────────────────────────────────────

function highlight(text, query) {
  if (!query || !text) return text || "";
  const terms = query
    .toLowerCase()
    .split(/\s+/)
    .filter(Boolean)
    .map((t) => t.replace(/[.*+?^${}()|[\]\\]/g, "\\$&"));
  if (!terms.length) return text;
  const regex = new RegExp(`(${terms.join("|")})`, "gi");
  return text.split(regex).map((part, i) =>
    regex.test(part) ? <mark key={i} style={s.hl}>{part}</mark> : part
  );
}

function formatTs(nano) {
  if (!nano) return null;
  return new Date(Math.floor(nano / 1e6)).toISOString().replace("T", " ").slice(0, 23);
}

function detectLevel(doc) {
  // Use SeverityString field first, then fall back to scanning Message
  const sev = (doc.severity_string || "").toLowerCase();
  if (sev === "error" || sev === "fatal" || sev === "crit") return "ERROR";
  if (sev === "warn" || sev === "warning") return "WARN";
  if (sev === "debug") return "DEBUG";
  if (sev === "info") return "INFO";
  const msg = (doc.message || doc.message_raw || "").toLowerCase();
  if (msg.includes("error") || msg.includes("fatal")) return "ERROR";
  if (msg.includes("warn")) return "WARN";
  if (msg.includes("debug")) return "DEBUG";
  return "INFO";
}

const LEVEL_COLOR = { ERROR: "#f87171", WARN: "#fbbf24", DEBUG: "#94a3b8", INFO: "#34d399" };
const LEVEL_BG    = { ERROR: "#3f0f0f", WARN: "#3b1f00", DEBUG: "#1a2535", INFO: "#052e16" };

function tryParseJson(str) {
  try { return JSON.parse(str); } catch { return null; }
}

// ── Main page ─────────────────────────────────────────────────────────────────

export default function SearchPage() {
  const [inputVal, setInputVal]   = useState("");
  const [query, setQuery]         = useState("");
  const [results, setResults]     = useState(null);
  const [loading, setLoading]     = useState(false);
  const [error, setError]         = useState(null);
  const [page, setPage]           = useState(1);
  const [expanded, setExpanded]   = useState({});

  const doSearch = useCallback(async (q, p) => {
    if (!q.trim()) return;
    setLoading(true);
    setError(null);
    try {
      const res = await search(q, p, PAGE_SIZE);
      setResults(res.data);
      setPage(p);
      setExpanded({});
    } catch (e) {
      setError(e.message || "Search failed");
    } finally {
      setLoading(false);
    }
  }, []);

  const handleSubmit = (e) => {
    e.preventDefault();
    setQuery(inputVal);
    doSearch(inputVal, 1);
  };

  const totalPages = results ? Math.ceil(results.total_count / PAGE_SIZE) : 0;
  const toggle = (id) => setExpanded((p) => ({ ...p, [id]: !p[id] }));

  return (
    <div style={s.page}>
      {/* Header */}
      <div style={s.topBar}>
        <div>
          <h1 style={s.title}>Log Search</h1>
          <p style={s.subtitle}>Full-text BM25 search across all indexed telemetry</p>
        </div>
      </div>

      {/* Search bar */}
      <form onSubmit={handleSubmit} style={s.searchRow}>
        <div style={s.inputWrap}>
          <svg style={s.searchIcon} viewBox="0 0 20 20" fill="currentColor" width="18" height="18">
            <path fillRule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clipRule="evenodd"/>
          </svg>
          <input
            style={s.input}
            value={inputVal}
            onChange={(e) => setInputVal(e.target.value)}
            placeholder='Search logs… e.g. "error timeout kafka"'
            autoFocus
          />
          <button type="submit" style={s.btn} disabled={loading}>
            {loading ? "…" : "Search"}
          </button>
        </div>
      </form>

      {/* Meta bar */}
      {results && !loading && (
        <div style={s.meta}>
          <span style={s.metaCount}>{results.total_count.toLocaleString()} results</span>
          <span style={s.metaMs}>⚡ {results.took_ms.toFixed(2)} ms</span>
          <span style={s.metaPage}>Page {page} of {totalPages.toLocaleString()}</span>
        </div>
      )}

      {error && <div style={s.errBanner}>⚠ {error}</div>}

      {/* Results list */}
      <div style={s.list}>
        {loading && (
          <div style={s.center}>
            <div style={s.spinner} />
            <span style={{ color: "#64748b", marginLeft: 12 }}>Searching…</span>
          </div>
        )}

        {!loading && results?.documents?.length === 0 && (
          <div style={s.emptyState}>
            <div style={{ fontSize: 40 }}>🔭</div>
            <div style={{ color: "#94a3b8", fontSize: 16, marginTop: 12 }}>No results for "<strong>{query}</strong>"</div>
            <div style={{ color: "#475569", fontSize: 13, marginTop: 6 }}>Try different keywords or upload more files</div>
          </div>
        )}

        {!loading && results?.documents?.map((doc) => (
          <LogCard
            key={doc.id}
            doc={doc}
            query={query}
            expanded={!!expanded[doc.id]}
            onToggle={() => toggle(doc.id)}
          />
        ))}
      </div>

      {/* Pagination */}
      {totalPages > 1 && !loading && (
        <div style={s.pager}>
          <PageBtn disabled={page <= 1} onClick={() => doSearch(query, page - 1)}>← Prev</PageBtn>
          {buildPageNums(page, totalPages).map((p, i) =>
            p === "…" ? (
              <span key={`dot${i}`} style={s.pagDot}>…</span>
            ) : (
              <PageBtn key={p} active={p === page} onClick={() => doSearch(query, p)}>{p}</PageBtn>
            )
          )}
          <PageBtn disabled={page >= totalPages} onClick={() => doSearch(query, page + 1)}>Next →</PageBtn>
        </div>
      )}

      {/* Empty landing */}
      {!results && !loading && (
        <div style={s.landing}>
          <div style={{ fontSize: 52 }}>📊</div>
          <div style={s.landTitle}>Search your telemetry</div>
          <div style={s.landSub}>
            Type keywords to search across Message, Namespace, AppName, Hostname, Severity, Sender, and Structured Data fields.
          </div>
        </div>
      )}
    </div>
  );
}

// ── Log Card ──────────────────────────────────────────────────────────────────

function LogCard({ doc, query, expanded, onToggle }) {
  const level   = detectLevel(doc);
  const color   = LEVEL_COLOR[level];
  const ts      = formatTs(doc.nano_timestamp);
  const message = doc.message || doc.message_raw || "";

  // Parse structured_data JSON for pretty display
  const structured = tryParseJson(doc.structured_data);
  const rawJson    = tryParseJson(doc.message_raw);

  return (
    <div style={{ ...s.card, borderLeft: `3px solid ${color}` }}>
      {/* ── Row 1: badges + timestamp ── */}
      <div style={s.cardHead} onClick={onToggle}>
        <div style={s.badges}>
          <span style={{ ...s.lvlBadge, background: LEVEL_BG[level], color }}>
            {level}
          </span>
          {doc.severity_string && doc.severity_string !== level && (
            <span style={s.chip}>{doc.severity_string}</span>
          )}
          {doc.app_name && <span style={{ ...s.chip, color: "#a5b4fc" }}>{doc.app_name}</span>}
          {doc.namespace && <span style={{ ...s.chip, color: "#67e8f9" }}>{doc.namespace}</span>}
          {doc.hostname && <span style={s.chip}>{doc.hostname}</span>}
        </div>
        <div style={s.cardMeta}>
          {ts && <span style={s.tsText}>{ts}</span>}
          <span style={{ color: "#475569", fontSize: 11 }}>{expanded ? "▲" : "▼"}</span>
        </div>
      </div>

      {/* ── Row 2: main message ── */}
      {message && (
        <div style={s.msgRow}>
          <span style={s.msgText}>{highlight(message, query)}</span>
        </div>
      )}

      {/* ── Expanded detail ── */}
      {expanded && (
        <div style={s.detail}>
          {/* Quick fields grid */}
          <div style={s.fieldGrid}>
            <Field label="Sender"     value={doc.sender} />
            <Field label="Tag"        value={doc.tag} />
            <Field label="Hostname"   value={doc.hostname} />
            <Field label="AppName"    value={doc.app_name} />
            <Field label="ProcID"     value={doc.proc_id} />
            <Field label="MsgID"      value={doc.msg_id} />
            <Field label="Event"      value={doc.event} />
            <Field label="EventID"    value={doc.event_id} />
            <Field label="Groupings"  value={doc.groupings} />
            <Field label="Facility"   value={doc.facility_string || doc.facility} />
            <Field label="Priority"   value={doc.priority != null ? String(doc.priority) : null} />
            <Field label="Timestamp"  value={doc.timestamp} />
          </div>

          {/* Raw message if different */}
          {doc.message_raw && doc.message_raw !== message && (
            <Section label="Raw Message">
              {rawJson ? (
                <JsonBlock data={rawJson} query={query} />
              ) : (
                <pre style={s.pre}>{highlight(doc.message_raw, query)}</pre>
              )}
            </Section>
          )}

          {/* Structured data */}
          {doc.structured_data && (
            <Section label="Structured Data">
              {structured ? (
                <JsonBlock data={structured} query={query} />
              ) : (
                <pre style={s.pre}>{highlight(doc.structured_data, query)}</pre>
              )}
            </Section>
          )}
        </div>
      )}
    </div>
  );
}

function Field({ label, value }) {
  if (!value && value !== 0) return null;
  return (
    <div style={s.field}>
      <span style={s.fieldLabel}>{label}</span>
      <span style={s.fieldVal}>{value}</span>
    </div>
  );
}

function Section({ label, children }) {
  return (
    <div style={s.section}>
      <div style={s.sectionLabel}>{label}</div>
      {children}
    </div>
  );
}

function JsonBlock({ data, query }) {
  return (
    <div style={s.jsonGrid}>
      {Object.entries(data).map(([k, v]) => (
        <div key={k} style={s.jsonRow}>
          <span style={s.jsonKey}>{k}</span>
          <span style={s.jsonVal}>
            {highlight(Array.isArray(v) ? v.join(", ") : String(v), query)}
          </span>
        </div>
      ))}
    </div>
  );
}

function PageBtn({ children, disabled, active, onClick }) {
  return (
    <button
      onClick={onClick}
      disabled={disabled}
      style={{
        ...s.pagBtn,
        background: active ? "#6366f1" : "#1e293b",
        color: active ? "#fff" : "#94a3b8",
        borderColor: active ? "#6366f1" : "#334155",
        opacity: disabled ? 0.35 : 1,
        cursor: disabled ? "default" : "pointer",
      }}
    >
      {children}
    </button>
  );
}

function buildPageNums(cur, total) {
  if (total <= 9) return Array.from({ length: total }, (_, i) => i + 1);
  const pages = new Set([1, total, cur, cur - 1, cur + 1, cur - 2, cur + 2].filter(p => p >= 1 && p <= total));
  const sorted = [...pages].sort((a, b) => a - b);
  const out = [];
  let prev = 0;
  for (const p of sorted) {
    if (p - prev > 1) out.push("…");
    out.push(p);
    prev = p;
  }
  return out;
}

// ── Styles ────────────────────────────────────────────────────────────────────

const s = {
  page:      { display: "flex", flexDirection: "column", height: "100%", background: "#0f172a" },
  topBar:    { padding: "24px 32px 0" },
  title:     { margin: 0, fontSize: 22, fontWeight: 700, color: "#f1f5f9" },
  subtitle:  { margin: "4px 0 0", fontSize: 13, color: "#64748b" },
  searchRow: { padding: "16px 32px" },
  inputWrap: { display: "flex", alignItems: "center", background: "#1e293b", border: "1px solid #334155", borderRadius: 10, overflow: "hidden" },
  searchIcon:{ margin: "0 12px", color: "#64748b", flexShrink: 0 },
  input:     { flex: 1, background: "transparent", border: "none", outline: "none", color: "#e2e8f0", fontSize: 15, padding: "14px 4px" },
  btn:       { background: "#6366f1", color: "#fff", border: "none", padding: "14px 28px", cursor: "pointer", fontWeight: 600, fontSize: 14 },
  meta:      { display: "flex", alignItems: "center", gap: 16, padding: "0 32px 10px" },
  metaCount: { fontSize: 13, color: "#e2e8f0", fontWeight: 600 },
  metaMs:    { fontSize: 13, color: "#6366f1", fontWeight: 600 },
  metaPage:  { fontSize: 13, color: "#64748b", marginLeft: "auto" },
  errBanner: { margin: "0 32px 12px", padding: "10px 14px", background: "#450a0a", color: "#f87171", borderRadius: 8, fontSize: 13 },
  list:      { flex: "1 1 0", overflowY: "auto", minHeight: 0, padding: "0 32px 8px", display: "flex", flexDirection: "column", gap: 6 },
  center:    { display: "flex", alignItems: "center", justifyContent: "center", padding: 60 },
  spinner:   { width: 22, height: 22, border: "3px solid #334155", borderTop: "3px solid #6366f1", borderRadius: "50%", animation: "spin 0.8s linear infinite" },
  emptyState:{ textAlign: "center", padding: "60px 0" },

  // Card
  card:      { background: "#1e293b", borderRadius: 8, border: "1px solid #1e3a5f", flexShrink: 0 },
  cardHead:  { display: "flex", justifyContent: "space-between", alignItems: "center", padding: "9px 14px", cursor: "pointer", userSelect: "none", gap: 12 },
  badges:    { display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", minWidth: 0 },
  lvlBadge:  { fontSize: 10, fontWeight: 700, padding: "2px 7px", borderRadius: 4, letterSpacing: "0.07em", flexShrink: 0 },
  chip:      { fontSize: 11, color: "#94a3b8", background: "#0f172a", border: "1px solid #334155", borderRadius: 4, padding: "1px 7px", maxWidth: 220, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
  cardMeta:  { display: "flex", alignItems: "center", gap: 10, flexShrink: 0 },
  tsText:    { fontSize: 11, color: "#475569", fontFamily: "monospace", whiteSpace: "nowrap" },
  msgRow:    { padding: "2px 14px 10px 14px" },
  msgText:   { fontSize: 13, color: "#cbd5e1", lineHeight: 1.55, wordBreak: "break-word" },
  hl:        { background: "rgba(99,102,241,0.3)", color: "#a5b4fc", borderRadius: 2 },

  // Expanded detail
  detail:    { borderTop: "1px solid #334155", padding: "12px 14px", display: "flex", flexDirection: "column", gap: 12 },
  fieldGrid: { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))", gap: "8px 16px" },
  field:     { display: "flex", flexDirection: "column", gap: 2 },
  fieldLabel:{ fontSize: 10, fontWeight: 600, color: "#475569", textTransform: "uppercase", letterSpacing: "0.07em" },
  fieldVal:  { fontSize: 12, color: "#94a3b8", wordBreak: "break-word" },
  section:   { display: "flex", flexDirection: "column", gap: 6 },
  sectionLabel: { fontSize: 10, fontWeight: 600, color: "#475569", textTransform: "uppercase", letterSpacing: "0.07em" },
  pre:       { margin: 0, padding: "8px 10px", background: "#0f172a", borderRadius: 6, fontSize: 12, color: "#94a3b8", overflowX: "auto", lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-all" },
  jsonGrid:  { display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 12px", background: "#0f172a", borderRadius: 6, padding: "8px 10px" },
  jsonRow:   { display: "contents" },
  jsonKey:   { fontSize: 12, color: "#6366f1", fontFamily: "monospace", whiteSpace: "nowrap", alignSelf: "start", paddingTop: 1 },
  jsonVal:   { fontSize: 12, color: "#94a3b8", fontFamily: "monospace", wordBreak: "break-all" },

  // Pagination
  pager:     { display: "flex", gap: 5, justifyContent: "center", padding: "12px 32px 16px", flexWrap: "wrap" },
  pagBtn:    { border: "1px solid", borderRadius: 6, padding: "6px 12px", fontSize: 13, fontWeight: 500, minWidth: 36, transition: "all 0.15s" },
  pagDot:    { display: "flex", alignItems: "center", color: "#475569", padding: "0 4px" },

  // Landing
  landing:   { flex: 1, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 12, paddingBottom: 80 },
  landTitle: { fontSize: 20, fontWeight: 600, color: "#e2e8f0" },
  landSub:   { fontSize: 13, color: "#64748b", maxWidth: 440, textAlign: "center", lineHeight: 1.6 },
};
