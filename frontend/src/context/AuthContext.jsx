import { createContext, useContext, useState, useCallback, useEffect } from "react";

const AuthContext = createContext(null);

const TOKEN_KEY   = "ts_access_token";
const REFRESH_KEY = "ts_refresh_token";
const USER_KEY    = "ts_user";

// Relative base — empty string means same origin (works through gateway in Docker)
// Override with VITE_API_URL for local dev (e.g. http://localhost:3000)
const BASE = import.meta.env.VITE_API_URL ?? "";

export function AuthProvider({ children }) {
  const [user, setUser] = useState(() => {
    try { return JSON.parse(sessionStorage.getItem(USER_KEY)); } catch { return null; }
  });
  const [accessToken, setAccessToken] = useState(() => sessionStorage.getItem(TOKEN_KEY));
  const [loading, setLoading] = useState(false);
  const [error, setError]     = useState(null);

  // Silent refresh — fires 2 minutes before access token expires
  useEffect(() => {
    if (!accessToken) return;
    try {
      const payload    = JSON.parse(atob(accessToken.split(".")[1]));
      const msUntilExp = payload.exp * 1000 - Date.now() - 120_000;
      if (msUntilExp <= 0) { silentRefresh(); return; }
      const t = setTimeout(silentRefresh, msUntilExp);
      return () => clearTimeout(t);
    } catch { /* malformed token — ignore */ }
  }, [accessToken]);

  const silentRefresh = useCallback(async () => {
    const rt = sessionStorage.getItem(REFRESH_KEY);
    if (!rt) { logout(); return; }
    try {
      const res = await fetch(`${BASE}/api/v1/auth/refresh`, {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({ refresh_token: rt }),
      });
      if (!res.ok) { logout(); return; }
      storeTokens(await res.json());
    } catch { logout(); }
  }, []);

  const login = useCallback(async (username, password) => {
    setLoading(true);
    setError(null);
    try {
      const res  = await fetch(`${BASE}/api/v1/auth/login`, {
        method:  "POST",
        headers: { "Content-Type": "application/json" },
        body:    JSON.stringify({ username, password }),
      });
      const data = await res.json();
      if (!res.ok) throw new Error(data.error || "Login failed");
      storeTokens(data);
      return true;
    } catch (e) {
      setError(e.message);
      return false;
    } finally {
      setLoading(false);
    }
  }, []);

  const logout = useCallback(async () => {
    const rt = sessionStorage.getItem(REFRESH_KEY);
    if (rt) {
      try {
        await fetch(`${BASE}/api/v1/auth/logout`, {
          method:  "POST",
          headers: { "Content-Type": "application/json" },
          body:    JSON.stringify({ refresh_token: rt }),
        });
      } catch { /* ignore */ }
    }
    sessionStorage.removeItem(TOKEN_KEY);
    sessionStorage.removeItem(REFRESH_KEY);
    sessionStorage.removeItem(USER_KEY);
    setUser(null);
    setAccessToken(null);
  }, []);

  function storeTokens(data) {
    sessionStorage.setItem(TOKEN_KEY, data.access_token);
    sessionStorage.setItem(REFRESH_KEY, data.refresh_token);
    if (data.user) {
      sessionStorage.setItem(USER_KEY, JSON.stringify(data.user));
      setUser(data.user);
    }
    setAccessToken(data.access_token);
  }

  return (
    <AuthContext.Provider value={{
      user,
      accessToken,
      loading,
      error,
      login,
      logout,
      silentRefresh,
      isAuthenticated: !!user,
      canWrite: user?.role === "writer" || user?.role === "admin",
      isAdmin:  user?.role === "admin",
    }}>
      {children}
    </AuthContext.Provider>
  );
}

export function useAuth() {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
