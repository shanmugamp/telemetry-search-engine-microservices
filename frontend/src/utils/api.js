import axios from "axios";

// In production the frontend is served by the gateway on the same origin,
// so all API calls use relative URLs (no CORS, no hardcoded host).
// In local dev (npm run dev on :5173), set VITE_API_URL=http://localhost:3000
const BASE_URL = import.meta.env.VITE_API_URL ?? "";

const api = axios.create({ baseURL: BASE_URL, timeout: 30000 });

// ── Inject Bearer token on every request ─────────────────────────────────────
api.interceptors.request.use((config) => {
  const token = sessionStorage.getItem("ts_access_token");
  if (token) config.headers.Authorization = `Bearer ${token}`;
  return config;
});

// ── Auto-refresh on 401 ───────────────────────────────────────────────────────
let refreshPromise = null;

api.interceptors.response.use(
  (res) => res,
  async (error) => {
    const original = error.config;
    if (error.response?.status === 401 && !original._retry) {
      original._retry = true;

      if (!refreshPromise) {
        const refreshToken = sessionStorage.getItem("ts_refresh_token");
        if (!refreshToken) {
          window.dispatchEvent(new CustomEvent("auth:logout"));
          return Promise.reject(error);
        }
        refreshPromise = axios
          .post(`${BASE_URL}/api/v1/auth/refresh`, { refresh_token: refreshToken })
          .then((res) => {
            sessionStorage.setItem("ts_access_token", res.data.access_token);
            sessionStorage.setItem("ts_refresh_token", res.data.refresh_token);
            refreshPromise = null;
            return res.data.access_token;
          })
          .catch((e) => {
            refreshPromise = null;
            sessionStorage.clear();
            window.dispatchEvent(new CustomEvent("auth:logout"));
            return Promise.reject(e);
          });
      }

      try {
        const newToken = await refreshPromise;
        original.headers.Authorization = `Bearer ${newToken}`;
        return api(original);
      } catch {
        return Promise.reject(error);
      }
    }
    return Promise.reject(error);
  }
);

// ── Auth ──────────────────────────────────────────────────────────────────────
export const login  = (username, password) =>
  api.post("/api/v1/auth/login",  { username, password });
export const logout = (refreshToken) =>
  api.post("/api/v1/auth/logout", { refresh_token: refreshToken });

// ── Search service ────────────────────────────────────────────────────────────
// `filters` is an object with any combination of:
//   q, sender, hostname, app_name, proc_id, msg_id,
//   groupings, facility, raw_message, structured_data
// Only non-empty values are forwarded to the backend.
export const search = (filters = {}, page = 1, pageSize = 20) => {
  const FIELD_KEYS = [
    "q", "sender", "hostname", "app_name", "proc_id",
    "msg_id", "groupings", "facility", "raw_message", "structured_data",
  ];
  const params = { page, page_size: pageSize };
  for (const key of FIELD_KEYS) {
    const val = filters[key];
    if (val !== undefined && val !== null && String(val).trim() !== "") {
      params[key] = String(val).trim();
    }
  }
  return api.get("/api/v1/search", { params });
};

export const getStats = () => api.get("/api/v1/stats");

// ── Ingest service ────────────────────────────────────────────────────────────
export const uploadFile = (file, onProgress) => {
  const form = new FormData();
  form.append("file", file);
  return api.post("/api/v1/upload", form, {
    headers: { "Content-Type": "multipart/form-data" },
    onUploadProgress: (e) => {
      if (onProgress && e.total) onProgress(Math.round((e.loaded * 100) / e.total));
    },
  });
};

export const getFiles   = ()     => api.get("/api/v1/files");
export const deleteFile = (name) => api.delete(`/api/v1/files/${encodeURIComponent(name)}`);
export const getJob     = (id)   => api.get(`/api/v1/jobs/${id}`);
export const getJobs    = ()     => api.get("/api/v1/jobs");

export default api;
