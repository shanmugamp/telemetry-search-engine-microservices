import { useState, useRef } from "react";
import { uploadFile, getJob } from "../utils/api";
import { useAuth } from "../context/AuthContext";

// Maximum number of files uploaded simultaneously.
// Keeping this at 5 ensures we stay well within the nginx burst=20 limit
// even when the user drops 50+ files at once.
const MAX_CONCURRENT_UPLOADS = 5;

export default function UploadPage({ onUploadComplete }) {
  const { canWrite } = useAuth();
  const [dragging,  setDragging]  = useState(false);
  const [uploads,   setUploads]   = useState([]);
  const inputRef = useRef();

  // ── Per-upload helpers ──────────────────────────────────────────────────────

  const setUpload = (id, patch) =>
    setUploads((prev) => prev.map((u) => u.id === id ? { ...u, ...patch } : u));

  const pollJob = (uploadId, jobId) => {
    const iv = setInterval(async () => {
      try {
        const res = await getJob(jobId);
        const job = res.data;
        const isDone   = job.status === "done";
        const isFailed = job.status === "failed";
        setUpload(uploadId, {
          jobStatus:   job.status,
          docsIndexed: job.docs_indexed,
          status:      isDone ? "done" : isFailed ? "error" : "processing",
          message:     isDone   ? `Indexed ${job.docs_indexed.toLocaleString()} documents`
                     : isFailed ? job.error || "Processing failed"
                     :            `Processing… ${job.docs_indexed || 0} docs so far`,
        });
        if (isDone || isFailed) {
          clearInterval(iv);
          if (isDone && onUploadComplete) onUploadComplete();
        }
      } catch {
        clearInterval(iv);
      }
    }, 2000);
  };

  // Upload a single file and return when the HTTP request completes.
  const uploadOne = async (file, id) => {
    try {
      const res = await uploadFile(file, (pct) =>
        setUpload(id, { progress: pct })
      );
      const jobId = res.data.job_id;
      setUpload(id, { progress: 100, jobId, status: "processing", message: "Queued — waiting for worker…" });
      if (jobId) pollJob(id, jobId);
    } catch (err) {
      const msg = err.response?.data?.error || err.message || "Upload failed";
      setUpload(id, { status: "error", message: msg });
    }
  };

  // ── Batch-upload with bounded concurrency ───────────────────────────────────
  //
  // WHY THIS IS NEEDED:
  // The old code called addUpload() for every file inside a forEach loop,
  // which fired all HTTP requests simultaneously. With nginx burst=5, any
  // batch larger than 5 files would see 503 for all requests beyond the burst.
  //
  // This function instead runs uploads in a pool: at most MAX_CONCURRENT_UPLOADS
  // (5) are in-flight at any moment. As each one finishes, the next queued file
  // is started. This works regardless of how many files the user drops.
  const handleFiles = (files) => {
    if (!canWrite) return;
    const fileList = Array.from(files);
    if (!fileList.length) return;

    // Create all upload entries upfront so the UI shows them all immediately.
    const entries = fileList.map((file) => ({
      id:       Date.now() + Math.random(),
      file,
      name:     file.name,
      size:     file.size,
      status:   "queued",        // queued → uploading → processing → done/error
      progress: 0,
      message:  "Waiting in queue…",
      jobId:    null,
    }));

    setUploads((prev) => [...entries, ...prev]);

    // Run with a semaphore: maintain a pool of at most MAX_CONCURRENT_UPLOADS
    // active uploads. Each finished upload starts the next queued one.
    let nextIndex = 0;

    const startNext = () => {
      if (nextIndex >= entries.length) return;
      const entry = entries[nextIndex++];
      setUpload(entry.id, { status: "uploading", message: "Uploading…" });
      uploadOne(entry.file, entry.id).finally(startNext);
    };

    // Kick off the first MAX_CONCURRENT_UPLOADS slots.
    const slots = Math.min(MAX_CONCURRENT_UPLOADS, entries.length);
    for (let i = 0; i < slots; i++) startNext();
  };

  // ── Drag-and-drop + click handlers ─────────────────────────────────────────

  const onDrop = (e) => {
    e.preventDefault();
    setDragging(false);
    handleFiles(e.dataTransfer.files);
  };

  const formatBytes = (b) => {
    if (b < 1024)    return b + " B";
    if (b < 1048576) return (b / 1024).toFixed(1) + " KB";
    return (b / 1048576).toFixed(1) + " MB";
  };

  const statusIcon = (status) =>
    status === "done"       ? "✅"
  : status === "error"      ? "❌"
  : status === "processing" ? "⚙️"
  : status === "uploading"  ? "⬆️"
  : "🕐"; // queued

  // ── Render ──────────────────────────────────────────────────────────────────

  return (
    <div style={s.page}>
      <div style={s.header}>
        <h1 style={s.title}>Upload Parquet Files</h1>
        <p style={s.subtitle}>
          Select or drop up to any number of files. They are uploaded in batches
          of {MAX_CONCURRENT_UPLOADS} and indexed asynchronously.
          {!canWrite && <span style={s.readonlyBadge}> 🔒 Read-only — contact an admin to upload</span>}
        </p>
      </div>

      {canWrite && (
        <div
          style={{
            ...s.dropZone,
            borderColor: dragging ? "#6366f1" : "#334155",
            background:  dragging ? "rgba(99,102,241,0.08)" : "#1e293b",
          }}
          onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          onClick={() => inputRef.current?.click()}
        >
          <input
            ref={inputRef}
            type="file"
            accept="*"
            multiple
            style={{ display: "none" }}
            onChange={(e) => handleFiles(e.target.files)}
          />
          <div style={{ fontSize: 48, marginBottom: 12 }}>📦</div>
          <div style={s.dropText}>{dragging ? "Drop to upload" : "Drop Parquet files or click to browse"}</div>
          <div style={s.dropSub}>
            Upload any number of files — batched automatically ({MAX_CONCURRENT_UPLOADS} at a time)
          </div>
          <button
            style={s.browseBtn}
            type="button"
            onClick={(e) => { e.stopPropagation(); inputRef.current?.click(); }}
          >
            Browse Files
          </button>
        </div>
      )}

      {uploads.length > 0 && (
        <div style={s.list}>
          <div style={s.listHeader}>
            Upload Queue
            <span style={s.queueStats}>
              {uploads.filter(u => u.status === "done").length} done
              {" · "}
              {uploads.filter(u => u.status === "error").length > 0 &&
                <span style={{ color: "#f87171" }}>
                  {uploads.filter(u => u.status === "error").length} failed ·{" "}
                </span>
              }
              {uploads.filter(u => u.status === "uploading" || u.status === "processing").length} active
              {" · "}
              {uploads.filter(u => u.status === "queued").length} queued
            </span>
          </div>

          {uploads.map((u) => (
            <div key={u.id} style={{
              ...s.uploadItem,
              borderColor: u.status === "error"      ? "#7f1d1d"
                         : u.status === "done"       ? "#14532d"
                         : u.status === "uploading"  ? "#1e3a5f"
                         : "#334155",
            }}>
              <div style={s.uploadTop}>
                <div style={s.uploadName}>
                  <span style={{ marginRight: 8, fontSize: 14 }}>{statusIcon(u.status)}</span>
                  <span style={{ flex: 1, minWidth: 0, overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                    {u.name}
                  </span>
                  {u.jobId && <span style={s.jobBadge}>{u.jobId}</span>}
                </div>
                <span style={s.uploadSize}>{formatBytes(u.size)}</span>
              </div>

              {/* Progress bar — visible while uploading */}
              {u.status === "uploading" && (
                <div style={s.progressBar}>
                  <div style={{ ...s.progressFill, width: `${u.progress}%` }} />
                </div>
              )}

              {/* Status message */}
              {u.message && (
                <div style={{
                  ...s.uploadMsg,
                  color: u.status === "error"      ? "#f87171"
                       : u.status === "done"       ? "#34d399"
                       : u.status === "queued"     ? "#64748b"
                       : "#60a5fa",
                }}>
                  {u.message}
                </div>
              )}

              {u.docsIndexed > 0 && (
                <div style={s.docCount}>{u.docsIndexed.toLocaleString()} documents indexed</div>
              )}
            </div>
          ))}
        </div>
      )}

      {/* How it works */}
      <div style={s.infoBox}>
        <div style={s.infoTitle}>Async Ingest Pipeline</div>
        <div style={s.infoGrid}>
          <InfoCard icon="🗂️" title="Batch Queue"  desc={`Browser uploads ${MAX_CONCURRENT_UPLOADS} files at a time. Remaining files wait in the local queue so nginx is never overwhelmed.`} />
          <InfoCard icon="✅" title="Validate"    desc="Gateway checks PAR1 magic bytes before saving to disk. Non-parquet files are rejected immediately." />
          <InfoCard icon="📬" title="Worker Pool" desc="Ingest service processes files via a 4-goroutine worker pool. Each file gets a job_id you can poll." />
          <InfoCard icon="🔍" title="Index"       desc="After a worker finishes, search-service is notified and adds the rows to the BM25 inverted index." />
          <InfoCard icon="⚡" title="Live Status" desc="The UI polls /api/v1/jobs/:id every 2 s and updates each card until status = done or failed." />
        </div>
      </div>
    </div>
  );
}

function InfoCard({ icon, title, desc }) {
  return (
    <div style={s.infoCard}>
      <div style={{ fontSize: 22, marginBottom: 8 }}>{icon}</div>
      <div style={s.infoCardTitle}>{title}</div>
      <div style={s.infoCardDesc}>{desc}</div>
    </div>
  );
}

const s = {
  page:          { padding: "24px 32px", overflow: "auto", height: "100%", boxSizing: "border-box" },
  header:        { marginBottom: 24 },
  title:         { margin: 0, fontSize: 22, fontWeight: 700, color: "#f1f5f9" },
  subtitle:      { margin: "6px 0 0", fontSize: 13, color: "#64748b", lineHeight: 1.6 },
  readonlyBadge: { color: "#f59e0b", fontWeight: 600 },

  dropZone: {
    border: "2px dashed", borderRadius: 12, padding: "48px 32px",
    textAlign: "center", cursor: "pointer", transition: "all 0.2s",
    marginBottom: 24, userSelect: "none",
  },
  dropText:   { fontSize: 16, fontWeight: 600, color: "#e2e8f0", marginBottom: 6 },
  dropSub:    { fontSize: 13, color: "#64748b", marginBottom: 20 },
  browseBtn:  {
    background: "#6366f1", color: "#fff", border: "none", borderRadius: 8,
    padding: "10px 24px", fontSize: 14, fontWeight: 600, cursor: "pointer",
  },

  list:       { marginBottom: 24 },
  listHeader: {
    fontSize: 12, color: "#64748b", fontWeight: 600,
    textTransform: "uppercase", letterSpacing: "0.06em",
    marginBottom: 10, display: "flex", justifyContent: "space-between", alignItems: "center",
  },
  queueStats: { fontSize: 11, fontWeight: 400, color: "#475569", textTransform: "none", letterSpacing: 0 },

  uploadItem: {
    background: "#1e293b", border: "1px solid",
    borderRadius: 8, padding: "12px 14px", marginBottom: 8,
    transition: "border-color 0.3s",
  },
  uploadTop:  { display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 6, gap: 8 },
  uploadName: { fontSize: 13, color: "#e2e8f0", fontWeight: 500, display: "flex", alignItems: "center", gap: 6, flex: 1, minWidth: 0 },
  uploadSize: { fontSize: 12, color: "#64748b", flexShrink: 0 },
  jobBadge:   { fontSize: 10, background: "#1e3a5f", color: "#60a5fa", padding: "2px 6px", borderRadius: 4, fontFamily: "monospace", flexShrink: 0 },

  progressBar:  { height: 4, background: "#334155", borderRadius: 2, overflow: "hidden", marginBottom: 6 },
  progressFill: { height: "100%", background: "#6366f1", borderRadius: 2, transition: "width 0.3s" },

  uploadMsg:  { fontSize: 12, marginTop: 2 },
  docCount:   { fontSize: 11, color: "#6366f1", marginTop: 4, fontWeight: 600 },

  infoBox:       { background: "#1e293b", border: "1px solid #334155", borderRadius: 12, padding: "20px 24px" },
  infoTitle:     { fontSize: 13, fontWeight: 600, color: "#94a3b8", marginBottom: 16 },
  infoGrid:      { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 16 },
  infoCard:      { background: "#0f172a", border: "1px solid #334155", borderRadius: 8, padding: "14px 16px" },
  infoCardTitle: { fontSize: 14, fontWeight: 600, color: "#e2e8f0", marginBottom: 6 },
  infoCardDesc:  { fontSize: 12, color: "#64748b", lineHeight: 1.5 },
};
