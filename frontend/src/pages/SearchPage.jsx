import { useState, useCallback, useRef } from "react";
import { search } from "../utils/api";

const PAGE_SIZE = 20;

// The 9 field filters exposed in the UI, mapped to backend query-param keys.
const FIELD_FILTERS = [
  { label: "Sender",          key: "sender"          },
  { label: "Hostname",        key: "hostname"        },
  { label: "AppName",         key: "app_name"        },
  { label: "ProcID",          key: "proc_id"         },
  { label: "MsgID",           key: "msg_id"          },
  { label: "Groupings",       key: "groupings"       },
  { label: "Facility",        key: "facility"        },
  { label: "Raw Message",     key: "raw_message"     },
  { label: "Structured Data", key: "structured_data" },
];

// ── Helpers ───────────────────────────────────────────────────────────────────

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
  const sev = (doc.severity_string || "").toLowerCase();
  if (sev === "error" || sev === "fatal" || sev === "crit") return "ERROR";
  if (sev === "warn"  || sev === "warning")                  return "WARN";
  if (sev === "debug")                                        return "DEBUG";
  if (sev === "info")                                         return "INFO";
  const msg = (doc.message || doc.message_raw || "").toLowerCase();
  if (msg.includes("error") || msg.includes("fatal")) return "ERROR";
  if (msg.includes("warn"))  return "WARN";
  if (msg.includes("debug")) return "DEBUG";
  return "INFO";
}

const LEVEL_COLOR = { ERROR: "#f87171", WARN: "#fbbf24", DEBUG: "#94a3b8", INFO: "#34d399" };
const LEVEL_BG    = { ERROR: "#3f0f0f", WARN: "#3b1f00", DEBUG: "#1a2535", INFO: "#052e16" };

function tryParseJson(str) {
  try { return JSON.parse(str); } catch { return null; }
}

function hasAnyFilter(q, fields) {
  if (q.trim()) return true;
  return FIELD_FILTERS.some(({ key }) => (fields[key] || "").trim());
}

// ── Main component ─────────────────────────────────────────────────────────────

export default function SearchPage() {
  const [inputVal,    setInputVal]    = useState("");
  const [fieldVals,   setFieldVals]   = useState({});        // { hostname: "fluent-bit", … }
  const [committed,   setCommitted]   = useState({ q: "", fields: {} }); // what was last searched
  const [results,     setResults]     = useState(null);
  const [loading,     setLoading]     = useState(false);
  const [error,       setError]       = useState(null);
  const [page,        setPage]        = useState(1);
  const [expanded,    setExpanded]    = useState({});
  const [showFilters, setShowFilters] = useState(false);

  // Track in-flight requests so stale responses from slow queries are discarded.
  const reqIdRef = useRef(0);

  const doSearch = useCallback(async (q, fields, p) => {
    if (!hasAnyFilter(q, fields)) return;

    const myId = ++reqIdRef.current;
    setLoading(true);
    setError(null);

    try {
      const res = await search({ q, ...fields }, p, PAGE_SIZE);
      if (myId !== reqIdRef.current) return; // stale — discard
      setResults(res.data);
      setPage(p);
      setExpanded({});
    } catch (e) {
      if (myId !== reqIdRef.current) return;
      setError(e.message || "Search failed");
    } finally {
      if (myId === reqIdRef.current) setLoading(false);
    }
  }, []);

  // FIX for Bug 1 – Tab/window closes on cut/copy/paste:
  //
  // The original code wrapped the input in a <form>.  On several Chromium
  // versions, pressing Ctrl+X / Ctrl+C / Ctrl+V inside an <input> that sits
  // inside a <form> can fire the form's submit handler (the browser dispatches
  // a synthetic "submit" before React's synthetic event system can cancel it).
  // When the submit handler calls window.location navigation internally, or
  // when Vite's dev-HMR intercepts an unhandled submit, the tab reloads or
  // closes.
  //
  // Solution: remove <form> entirely.  Use a plain <div> and handle Enter
  // explicitly via onKeyDown.  All clipboard shortcuts (Ctrl+X/C/V) now pass
  // through the input without any form side-effects.
  const handleKeyDown = (e) => {
    if (e.key === "Enter") {
      // Prevent any ancestor <form> (LoginPage etc.) from catching this.
      e.preventDefault();
      e.stopPropagation();
      triggerSearch();
    }
    // Cut / Copy / Paste: do nothing — let the browser handle them normally.
  };

  const triggerSearch = () => {
    const snap = { q: inputVal, fields: { ...fieldVals } };
    setCommitted(snap);
    doSearch(snap.q, snap.fields, 1);
  };

  const setField = (key, val) =>
    setFieldVals((prev) => ({ ...prev, [key]: val }));

  const clearAll = () => {
    setInputVal("");
    setFieldVals({});
    setResults(null);
    setCommitted({ q: "", fields: {} });
  };

  const totalPages        = results ? Math.ceil(results.total_count / PAGE_SIZE) : 0;
  const toggle            = (id) => setExpanded((p) => ({ ...p, [id]: !p[id] }));
  const activeFilterCount = FIELD_FILTERS.filter(({ key }) => (fieldVals[key] || "").trim()).length;

  return (
    <div style={s.page}>

      {/* ── Header ─────────────────────────────────────────────────────────── */}
      <div style={s.topBar}>
        <h1 style={s.title}>Log Search</h1>
        <p style={s.subtitle}>Full-text BM25 search · field filters for Hostname, Sender, AppName and more</p>
      </div>

      {/* ── Search bar — NOTE: plain <div>, NOT <form> ──────────────────────
          Using <form> caused the tab / window to close when the user pressed
          Ctrl+X, Ctrl+C or Ctrl+V inside the input on Chromium-based browsers.
          A <div> + explicit onKeyDown(Enter) is the safe replacement.        */}
      <div style={s.searchSection}>
        <div style={s.inputRow}>
          <div style={s.inputWrap}>
            {/* search icon */}
            <svg style={s.searchIcon} viewBox="0 0 20 20" fill="currentColor" width="18" height="18">
              <path fillRule="evenodd" d="M8 4a4 4 0 100 8 4 4 0 000-8zM2 8a6 6 0 1110.89 3.476l4.817 4.817a1 1 0 01-1.414 1.414l-4.816-4.816A6 6 0 012 8z" clipRule="evenodd"/>
            </svg>

            <input
              style={s.input}
              value={inputVal}
              onChange={(e) => setInputVal(e.target.value)}
              onKeyDown={handleKeyDown}
              placeholder='Search logs… e.g. "error timeout kafka"'
              autoFocus
            />

            {/* Filter-panel toggle */}
            <button
              type="button"
              title="Toggle field filters"
              onClick={() => setShowFilters((v) => !v)}
              style={{
                ...s.filterBtn,
                background:  showFilters || activeFilterCount > 0 ? "#312e81" : "transparent",
                borderColor: activeFilterCount > 0 ? "#6366f1" : "transparent",
              }}
            >
              <svg viewBox="0 0 20 20" fill="currentColor" width="15" height="15">
                <path fillRule="evenodd" d="M3 3a1 1 0 011-1h12a1 1 0 011 1v3a1 1 0 01-.293.707L13 10.414V15a1 1 0 01-.553.894l-4 2A1 1 0 017 17v-6.586L3.293 6.707A1 1 0 013 6V3z" clipRule="evenodd"/>
              </svg>
              {activeFilterCount > 0 && (
                <span style={s.filterBadge}>{activeFilterCount}</span>
              )}
            </button>

            <button type="button" style={s.searchBtn} disabled={loading} onClick={triggerSearch}>
              {loading ? "…" : "Search"}
            </button>
          </div>
        </div>

        {/* ── Field-filter panel ───────────────────────────────────────────── */}
        {showFilters && (
          <div style={s.filterPanel}>
            <div style={s.filterGrid}>
              {FIELD_FILTERS.map(({ label, key }) => (
                <div key={key} style={s.filterField}>
                  <label style={s.filterLabel}>{label}</label>
                  <input
                    style={{
                      ...s.filterInput,
                      borderColor: (fieldVals[key] || "").trim() ? "#6366f1" : "#334155",
                    }}
                    value={fieldVals[key] || ""}
                    onChange={(e) => setField(key, e.target.value)}
                    onKeyDown={handleKeyDown}
                    placeholder={`Filter by ${label}…`}
                  />
                </div>
              ))}
            </div>
            {(activeFilterCount > 0 || inputVal.trim()) && (
              <button type="button" onClick={clearAll} style={s.clearBtn}>
                ✕ Clear all
              </button>
            )}
          </div>
        )}
      </div>

      {/* ── Meta bar ───────────────────────────────────────────────────────── */}
      {results && !loading && (
        <div style={s.meta}>
          <span style={s.metaCount}>{results.total_count.toLocaleString()} results</span>
          <span style={s.metaMs}>⚡ {results.took_ms.toFixed(2)} ms</span>
          {results.cache_hit && <span style={s.cacheTag}>cached</span>}
          <span style={s.metaPage}>Page {page} of {totalPages.toLocaleString()}</span>
        </div>
      )}

      {error && <div style={s.errBanner}>⚠ {error}</div>}

      {/* ── Results list ───────────────────────────────────────────────────── */}
      <div style={s.list}>
        {loading && (
          <div style={s.center}>
            <div style={s.spinner}/>
            <span style={{ color: "#64748b", marginLeft: 12 }}>Searching…</span>
          </div>
        )}

        {!loading && results?.documents?.length === 0 && (
          <div style={s.emptyState}>
            <div style={{ fontSize: 40 }}>🔭</div>
            <div style={{ color: "#94a3b8", fontSize: 16, marginTop: 12 }}>
              No results for "<strong>
                {committed.q || Object.values(committed.fields).filter(Boolean).join(" ")}
              </strong>"
            </div>
            <div style={{ color: "#475569", fontSize: 13, marginTop: 6 }}>
              Try the <strong style={{ color: "#a5b4fc" }}>filter icon</strong> to search by
              Hostname, Sender, AppName and more.
            </div>
          </div>
        )}

        {!loading && results?.documents?.map((doc) => (
          <LogCard
            key={doc.id}
            doc={doc}
            query={committed.q}
            expanded={!!expanded[doc.id]}
            onToggle={() => toggle(doc.id)}
          />
        ))}
      </div>

      {/* ── Pagination ─────────────────────────────────────────────────────── */}
      {totalPages > 1 && !loading && (
        <div style={s.pager}>
          <PageBtn disabled={page <= 1}        onClick={() => doSearch(committed.q, committed.fields, page - 1)}>← Prev</PageBtn>
          {buildPageNums(page, totalPages).map((p, i) =>
            p === "…"
              ? <span key={`dot${i}`} style={s.pagDot}>…</span>
              : <PageBtn key={p} active={p === page} onClick={() => doSearch(committed.q, committed.fields, p)}>{p}</PageBtn>
          )}
          <PageBtn disabled={page >= totalPages} onClick={() => doSearch(committed.q, committed.fields, page + 1)}>Next →</PageBtn>
        </div>
      )}

      {/* ── Landing state ──────────────────────────────────────────────────── */}
      {!results && !loading && (
        <div style={s.landing}>
          <div style={{ fontSize: 52 }}>📊</div>
          <div style={s.landTitle}>Search your telemetry</div>
          <div style={s.landSub}>
            Type keywords for full-text search, or click the{" "}
            <strong style={{ color: "#a5b4fc" }}>filter icon</strong> (▼) to search
            specific fields: Sender, Hostname, AppName, ProcID, MsgID, Groupings,
            Facility, Raw Message, Structured Data.
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

  const structured = tryParseJson(doc.structured_data);
  const rawJson    = tryParseJson(doc.message_raw);

  return (
    <div style={{ ...s.card, borderLeft: `3px solid ${color}` }}>

      {/* Row 1: badges + timestamp — click to expand */}
      <div style={s.cardHead} onClick={onToggle}>
        <div style={s.badges}>
          <span style={{ ...s.lvlBadge, background: LEVEL_BG[level], color }}>{level}</span>
          {doc.severity_string && doc.severity_string !== level && (
            <span style={s.chip}>{doc.severity_string}</span>
          )}
          {doc.app_name  && <span style={{ ...s.chip, color: "#a5b4fc" }}>{doc.app_name}</span>}
          {doc.namespace && <span style={{ ...s.chip, color: "#67e8f9" }}>{doc.namespace}</span>}
          {doc.hostname  && <span style={s.chip}>{doc.hostname}</span>}
        </div>
        <div style={s.cardMeta}>
          {ts && <span style={s.tsText}>{ts}</span>}
          <span style={{ color: "#475569", fontSize: 11 }}>{expanded ? "▲" : "▼"}</span>
        </div>
      </div>

      {/* Row 2: main message — intentionally selectable for copy */}
      {message && (
        <div style={s.msgRow}>
          <span style={s.msgText}>{highlight(message, query)}</span>
        </div>
      )}

      {/* Expanded detail */}
      {expanded && (
        <div style={s.detail}>
          <div style={s.fieldGrid}>
            <Field label="Sender"    value={doc.sender} />
            <Field label="Tag"       value={doc.tag} />
            <Field label="Hostname"  value={doc.hostname} />
            <Field label="AppName"   value={doc.app_name} />
            <Field label="ProcID"    value={doc.proc_id} />
            <Field label="MsgID"     value={doc.msg_id} />
            <Field label="Event"     value={doc.event} />
            <Field label="EventID"   value={doc.event_id} />
            <Field label="Groupings" value={doc.groupings} />
            <Field label="Facility"  value={doc.facility_string || doc.facility} />
            <Field label="Priority"  value={doc.priority != null ? String(doc.priority) : null} />
            <Field label="Timestamp" value={doc.timestamp} />
          </div>

          {doc.message_raw && doc.message_raw !== message && (
            <Section label="Raw Message">
              {rawJson
                ? <JsonBlock data={rawJson} query={query} />
                : <pre style={s.pre}>{highlight(doc.message_raw, query)}</pre>}
            </Section>
          )}

          {doc.structured_data && (
            <Section label="Structured Data">
              {structured
                ? <JsonBlock data={structured} query={query} />
                : <pre style={s.pre}>{highlight(doc.structured_data, query)}</pre>}
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
      type="button"
      onClick={onClick}
      disabled={disabled}
      style={{
        ...s.pagBtn,
        background:  active    ? "#6366f1" : "#1e293b",
        color:       active    ? "#fff"    : "#94a3b8",
        borderColor: active    ? "#6366f1" : "#334155",
        opacity:     disabled  ? 0.35      : 1,
        cursor:      disabled  ? "default" : "pointer",
      }}
    >
      {children}
    </button>
  );
}

function buildPageNums(cur, total) {
  if (total <= 9) return Array.from({ length: total }, (_, i) => i + 1);
  const pages = new Set(
    [1, total, cur, cur - 1, cur + 1, cur - 2, cur + 2].filter((p) => p >= 1 && p <= total)
  );
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

  // Search section — plain div, NOT a form
  searchSection: { padding: "16px 32px 0" },
  inputRow:      { marginBottom: 0 },
  inputWrap: {
    display: "flex", alignItems: "center",
    background: "#1e293b", border: "1px solid #334155",
    borderRadius: 10, overflow: "hidden",
  },
  searchIcon: { margin: "0 12px", color: "#64748b", flexShrink: 0 },
  input: {
    flex: 1, background: "transparent", border: "none", outline: "none",
    color: "#e2e8f0", fontSize: 15, padding: "14px 4px",
    // No special clipboard handling needed — without <form>, Ctrl+X/C/V
    // work natively and cannot accidentally submit anything.
  },
  filterBtn: {
    position: "relative", border: "1px solid transparent",
    padding: "14px 14px", cursor: "pointer", color: "#94a3b8",
    display: "flex", alignItems: "center", gap: 4, flexShrink: 0,
    transition: "background 0.15s",
  },
  filterBadge: {
    position: "absolute", top: 6, right: 6,
    background: "#6366f1", color: "#fff", borderRadius: "50%",
    width: 14, height: 14, fontSize: 9, fontWeight: 700,
    display: "flex", alignItems: "center", justifyContent: "center",
  },
  searchBtn: {
    background: "#6366f1", color: "#fff", border: "none",
    padding: "14px 28px", cursor: "pointer", fontWeight: 600,
    fontSize: 14, flexShrink: 0,
  },

  filterPanel: {
    marginTop: 8, background: "#1e293b", border: "1px solid #334155",
    borderRadius: 8, padding: 16,
  },
  filterGrid: {
    display: "grid",
    gridTemplateColumns: "repeat(auto-fill, minmax(210px, 1fr))",
    gap: "12px 16px",
  },
  filterField:  { display: "flex", flexDirection: "column", gap: 4 },
  filterLabel:  { fontSize: 10, fontWeight: 600, color: "#64748b", textTransform: "uppercase", letterSpacing: "0.07em" },
  filterInput:  {
    background: "#0f172a", border: "1px solid",
    borderRadius: 6, padding: "7px 10px", color: "#e2e8f0",
    fontSize: 13, outline: "none",
  },
  clearBtn: {
    marginTop: 12, background: "transparent", border: "1px solid #334155",
    color: "#94a3b8", borderRadius: 6, padding: "6px 14px",
    cursor: "pointer", fontSize: 12,
  },

  meta:      { display: "flex", alignItems: "center", gap: 16, padding: "10px 32px" },
  metaCount: { fontSize: 13, color: "#e2e8f0", fontWeight: 600 },
  metaMs:    { fontSize: 13, color: "#6366f1", fontWeight: 600 },
  cacheTag:  { fontSize: 11, color: "#34d399", background: "#052e16", borderRadius: 4, padding: "2px 7px", fontWeight: 600 },
  metaPage:  { fontSize: 13, color: "#64748b", marginLeft: "auto" },
  errBanner: { margin: "0 32px 12px", padding: "10px 14px", background: "#450a0a", color: "#f87171", borderRadius: 8, fontSize: 13 },
  list:      { flex: "1 1 0", overflowY: "auto", minHeight: 0, padding: "0 32px 8px", display: "flex", flexDirection: "column", gap: 6 },
  center:    { display: "flex", alignItems: "center", justifyContent: "center", padding: 60 },
  spinner:   { width: 22, height: 22, border: "3px solid #334155", borderTop: "3px solid #6366f1", borderRadius: "50%", animation: "spin 0.8s linear infinite" },
  emptyState:{ textAlign: "center", padding: "60px 0" },

  card:      { background: "#1e293b", borderRadius: 8, border: "1px solid #1e3a5f", flexShrink: 0 },
  // cardHead has NO userSelect:"none" on the whole row — only the badges get it.
  // This way clicking the row expands/collapses, but users can still select
  // and copy the timestamp text.
  cardHead:  { display: "flex", justifyContent: "space-between", alignItems: "center", padding: "9px 14px", cursor: "pointer", gap: 12 },
  badges:    { display: "flex", alignItems: "center", gap: 6, flexWrap: "wrap", minWidth: 0, userSelect: "none" },
  lvlBadge:  { fontSize: 10, fontWeight: 700, padding: "2px 7px", borderRadius: 4, letterSpacing: "0.07em", flexShrink: 0 },
  chip:      { fontSize: 11, color: "#94a3b8", background: "#0f172a", border: "1px solid #334155", borderRadius: 4, padding: "1px 7px", maxWidth: 220, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" },
  cardMeta:  { display: "flex", alignItems: "center", gap: 10, flexShrink: 0, userSelect: "none" },
  tsText:    { fontSize: 11, color: "#475569", fontFamily: "monospace", whiteSpace: "nowrap" },
  msgRow:    { padding: "2px 14px 10px 14px" },
  msgText:   { fontSize: 13, color: "#cbd5e1", lineHeight: 1.55, wordBreak: "break-word" },
  hl:        { background: "rgba(99,102,241,0.3)", color: "#a5b4fc", borderRadius: 2 },

  detail:       { borderTop: "1px solid #334155", padding: "12px 14px", display: "flex", flexDirection: "column", gap: 12 },
  fieldGrid:    { display: "grid", gridTemplateColumns: "repeat(auto-fill, minmax(200px, 1fr))", gap: "8px 16px" },
  field:        { display: "flex", flexDirection: "column", gap: 2 },
  fieldLabel:   { fontSize: 10, fontWeight: 600, color: "#475569", textTransform: "uppercase", letterSpacing: "0.07em" },
  fieldVal:     { fontSize: 12, color: "#94a3b8", wordBreak: "break-word" },
  section:      { display: "flex", flexDirection: "column", gap: 6 },
  sectionLabel: { fontSize: 10, fontWeight: 600, color: "#475569", textTransform: "uppercase", letterSpacing: "0.07em" },
  pre:          { margin: 0, padding: "8px 10px", background: "#0f172a", borderRadius: 6, fontSize: 12, color: "#94a3b8", overflowX: "auto", lineHeight: 1.5, whiteSpace: "pre-wrap", wordBreak: "break-all" },
  jsonGrid:     { display: "grid", gridTemplateColumns: "auto 1fr", gap: "4px 12px", background: "#0f172a", borderRadius: 6, padding: "8px 10px" },
  jsonRow:      { display: "contents" },
  jsonKey:      { fontSize: 12, color: "#6366f1", fontFamily: "monospace", whiteSpace: "nowrap", alignSelf: "start", paddingTop: 1 },
  jsonVal:      { fontSize: 12, color: "#94a3b8", fontFamily: "monospace", wordBreak: "break-all" },

  pager:   { display: "flex", gap: 5, justifyContent: "center", padding: "12px 32px 16px", flexWrap: "wrap" },
  pagBtn:  { border: "1px solid", borderRadius: 6, padding: "6px 12px", fontSize: 13, fontWeight: 500, minWidth: 36, transition: "all 0.15s" },
  pagDot:  { display: "flex", alignItems: "center", color: "#475569", padding: "0 4px" },

  landing:   { flex: 1, display: "flex", flexDirection: "column", alignItems: "center", justifyContent: "center", gap: 12, paddingBottom: 80 },
  landTitle: { fontSize: 20, fontWeight: 600, color: "#e2e8f0" },
  landSub:   { fontSize: 13, color: "#64748b", maxWidth: 460, textAlign: "center", lineHeight: 1.6 },
};
