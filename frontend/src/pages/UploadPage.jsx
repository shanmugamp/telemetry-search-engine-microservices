import { useState, useRef, useEffect } from "react";
import { uploadFile, getJob } from "../utils/api";
import { useAuth } from "../context/AuthContext";

export default function UploadPage({ onUploadComplete }) {
  const { canWrite } = useAuth();
  const [dragging, setDragging] = useState(false);
  const [uploads, setUploads] = useState([]);
  const inputRef = useRef();

  const pollJob = (uploadId, jobId) => {
    const interval = setInterval(async () => {
      try {
        const res = await getJob(jobId);
        const job = res.data;
        setUploads((prev) =>
          prev.map((u) =>
            u.id === uploadId
              ? {
                  ...u,
                  jobStatus: job.status,
                  docsIndexed: job.docs_indexed,
                  status: job.status === "done" ? "done"
                        : job.status === "failed" ? "error"
                        : "uploading",
                  message: job.status === "done"
                    ? `Indexed ${job.docs_indexed.toLocaleString()} documents`
                    : job.status === "failed"
                    ? job.error || "Processing failed"
                    : `Processing… ${job.docs_indexed || 0} docs indexed`,
                }
              : u
          )
        );
        if (job.status === "done" || job.status === "failed") {
          clearInterval(interval);
          if (job.status === "done" && onUploadComplete) onUploadComplete();
        }
      } catch {
        clearInterval(interval);
      }
    }, 2000);
  };

  const addUpload = (file) => {
    if (!canWrite) return;
    const id = Date.now() + Math.random();
    const entry = {
      id, name: file.name, size: file.size,
      status: "uploading", progress: 0, message: "Uploading…", jobId: null,
    };
    setUploads((prev) => [entry, ...prev]);

    uploadFile(file, (pct) => {
      setUploads((prev) => prev.map((u) => u.id === id ? { ...u, progress: pct } : u));
    })
      .then((res) => {
        const jobId = res.data.job_id;
        setUploads((prev) =>
          prev.map((u) => u.id === id ? { ...u, progress: 100, jobId, message: "Queued for processing…" } : u)
        );
        if (jobId) pollJob(id, jobId);
      })
      .catch((err) => {
        const msg = err.response?.data?.error || err.message || "Upload failed";
        setUploads((prev) =>
          prev.map((u) => u.id === id ? { ...u, status: "error", message: msg } : u)
        );
      });
  };

  const handleFiles = (files) => Array.from(files).forEach(addUpload);

  const onDrop = (e) => {
    e.preventDefault();
    setDragging(false);
    handleFiles(e.dataTransfer.files);
  };

  const formatBytes = (b) => {
    if (b < 1024) return b + " B";
    if (b < 1048576) return (b / 1024).toFixed(1) + " KB";
    return (b / 1048576).toFixed(1) + " MB";
  };

  return (
    <div style={s.page}>
      <div style={s.header}>
        <h1 style={s.title}>Upload Parquet Files</h1>
        <p style={s.subtitle}>
          Files are validated, saved, and indexed asynchronously. Track progress below.
          {!canWrite && <span style={s.readonlyBadge}> 🔒 Read-only — contact an admin to upload</span>}
        </p>
      </div>

      {canWrite && (
        <div
          style={{
            ...s.dropZone,
            borderColor: dragging ? "#6366f1" : "#334155",
            background: dragging ? "rgba(99,102,241,0.08)" : "#1e293b",
          }}
          onDragOver={(e) => { e.preventDefault(); setDragging(true); }}
          onDragLeave={() => setDragging(false)}
          onDrop={onDrop}
          onClick={() => inputRef.current?.click()}
        >
          <input ref={inputRef} type="file" accept="*" multiple style={{ display: "none" }}
            onChange={(e) => handleFiles(e.target.files)} />
          <div style={{ fontSize: 48, marginBottom: 12 }}>📦</div>
          <div style={s.dropText}>{dragging ? "Drop to upload" : "Drop Parquet files or click to browse"}</div>
          <div style={s.dropSub}>Backend validates file format automatically</div>
          <button style={s.browseBtn} type="button" onClick={(e) => { e.stopPropagation(); inputRef.current?.click(); }}>
            Browse Files
          </button>
        </div>
      )}

      {uploads.length > 0 && (
        <div style={s.list}>
          <div style={s.listHeader}>Upload History</div>
          {uploads.map((u) => (
            <div key={u.id} style={s.uploadItem}>
              <div style={s.uploadTop}>
                <div style={s.uploadName}>
                  <span style={{ marginRight: 8 }}>
                    {u.status === "done" ? "✅" : u.status === "error" ? "❌" : "⏳"}
                  </span>
                  {u.name}
                  {u.jobId && (
                    <span style={s.jobBadge}>{u.jobId}</span>
                  )}
                </div>
                <span style={s.uploadSize}>{formatBytes(u.size)}</span>
              </div>

              {u.status === "uploading" && (
                <div style={s.progressBar}>
                  <div style={{ ...s.progressFill, width: `${u.progress}%` }} />
                </div>
              )}

              {u.message && (
                <div style={{ ...s.uploadMsg, color: u.status === "error" ? "#f87171" : "#34d399" }}>
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

      <div style={s.infoBox}>
        <div style={s.infoTitle}>Async Ingest Pipeline</div>
        <div style={s.infoGrid}>
          <InfoCard icon="✅" title="Validate" desc="Magic byte check (PAR1) before saving to disk." />
          <InfoCard icon="📬" title="Queue" desc="File queued in background worker pool (4 goroutines)." />
          <InfoCard icon="🔍" title="Index" desc="Parquet rows parsed and indexed into BM25 inverted index." />
          <InfoCard icon="⚡" title="Live" desc="Track progress via job_id polling until status = done." />
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
  page: { padding: "24px 32px", overflow: "auto", height: "100%", boxSizing: "border-box" },
  header: { marginBottom: 24 },
  title: { margin: 0, fontSize: 22, fontWeight: 700, color: "#f1f5f9" },
  subtitle: { margin: "6px 0 0", fontSize: 13, color: "#64748b", lineHeight: 1.6 },
  readonlyBadge: { color: "#f59e0b", fontWeight: 600 },
  dropZone: {
    border: "2px dashed", borderRadius: 12, padding: "48px 32px",
    textAlign: "center", cursor: "pointer", transition: "all 0.2s", marginBottom: 24, userSelect: "none",
  },
  dropText: { fontSize: 16, fontWeight: 600, color: "#e2e8f0", marginBottom: 6 },
  dropSub: { fontSize: 13, color: "#64748b", marginBottom: 20 },
  browseBtn: {
    background: "#6366f1", color: "#fff", border: "none", borderRadius: 8,
    padding: "10px 24px", fontSize: 14, fontWeight: 600, cursor: "pointer",
  },
  list: { marginBottom: 24 },
  listHeader: { fontSize: 12, color: "#64748b", fontWeight: 600, textTransform: "uppercase", letterSpacing: "0.06em", marginBottom: 10 },
  uploadItem: { background: "#1e293b", border: "1px solid #334155", borderRadius: 8, padding: "12px 14px", marginBottom: 8 },
  uploadTop: { display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 8 },
  uploadName: { fontSize: 13, color: "#e2e8f0", fontWeight: 500, display: "flex", alignItems: "center", gap: 6 },
  uploadSize: { fontSize: 12, color: "#64748b" },
  jobBadge: { fontSize: 10, background: "#1e3a5f", color: "#60a5fa", padding: "2px 6px", borderRadius: 4, fontFamily: "monospace" },
  progressBar: { height: 4, background: "#334155", borderRadius: 2, overflow: "hidden", marginBottom: 4 },
  progressFill: { height: "100%", background: "#6366f1", borderRadius: 2, transition: "width 0.3s" },
  uploadMsg: { fontSize: 12, marginTop: 4 },
  docCount: { fontSize: 11, color: "#6366f1", marginTop: 4, fontWeight: 600 },
  infoBox: { background: "#1e293b", border: "1px solid #334155", borderRadius: 12, padding: "20px 24px" },
  infoTitle: { fontSize: 13, fontWeight: 600, color: "#94a3b8", marginBottom: 16 },
  infoGrid: { display: "grid", gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))", gap: 16 },
  infoCard: { background: "#0f172a", border: "1px solid #334155", borderRadius: 8, padding: "14px 16px" },
  infoCardTitle: { fontSize: 14, fontWeight: 600, color: "#e2e8f0", marginBottom: 6 },
  infoCardDesc: { fontSize: 12, color: "#64748b", lineHeight: 1.5 },
};
