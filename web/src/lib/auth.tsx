import { createContext, useContext, useEffect, useState, type ReactNode } from "react";
import { api, ApiError } from "./api";
import type { User } from "./types";

interface AuthState {
  user: User | null;
  loading: boolean;
  needsSetup: boolean;
  login: (email: string, password: string) => Promise<{ twoFactor: boolean; token?: string }>;
  login2fa: (token: string, code: string) => Promise<void>;
  setup: (email: string, name: string, password: string) => Promise<void>;
  logout: () => Promise<void>;
  refresh: () => Promise<void>;
}

const AuthContext = createContext<AuthState | null>(null);

export function AuthProvider({ children }: { children: ReactNode }) {
  const [user, setUser] = useState<User | null>(null);
  const [needsSetup, setNeedsSetup] = useState(false);
  const [loading, setLoading] = useState(true);

  async function refresh() {
    try {
      const status = await api.get<{ needsSetup: boolean }>("/api/setup/status");
      setNeedsSetup(status.needsSetup);
      if (status.needsSetup) {
        setUser(null);
        return;
      }
      const me = await api.get<User>("/api/auth/me");
      setUser(me);
    } catch (err) {
      if (err instanceof ApiError && err.status === 401) setUser(null);
      else setUser(null);
    } finally {
      setLoading(false);
    }
  }

  useEffect(() => {
    void refresh();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const value: AuthState = {
    user,
    loading,
    needsSetup,
    async login(email, password) {
      const r = await api.post<User & { twoFactorRequired?: boolean; token?: string }>(
        "/api/auth/login",
        { email, password },
      );
      if (r.twoFactorRequired) return { twoFactor: true, token: r.token };
      setUser(r);
      setNeedsSetup(false);
      return { twoFactor: false };
    },
    async login2fa(token, code) {
      const u = await api.post<User>("/api/auth/login/2fa", { token, code });
      setUser(u);
      setNeedsSetup(false);
    },
    async setup(email, name, password) {
      const u = await api.post<User>("/api/setup", { email, name, password });
      setUser(u);
      setNeedsSetup(false);
    },
    async logout() {
      await api.post("/api/auth/logout");
      setUser(null);
    },
    refresh,
  };

  return <AuthContext.Provider value={value}>{children}</AuthContext.Provider>;
}

export function useAuth(): AuthState {
  const ctx = useContext(AuthContext);
  if (!ctx) throw new Error("useAuth must be used within AuthProvider");
  return ctx;
}
