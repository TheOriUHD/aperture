import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../lib/auth";
import { api } from "../lib/api";
import { AuthLayout } from "./AuthLayout";

export function Login() {
  const { t } = useTranslation();
  const { login } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState(
    new URLSearchParams(window.location.search).get("error") === "sso"
      ? t("auth.ssoFailed", "Single sign-on failed. Try again or use your password.")
      : "",
  );
  const [busy, setBusy] = useState(false);
  const [sso, setSso] = useState<{ enabled: boolean; name: string }>({ enabled: false, name: "" });

  useEffect(() => {
    api
      .get<{ enabled: boolean; name: string }>("/api/auth/oidc/status")
      .then(setSso)
      .catch(() => {});
  }, []);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await login(email, password);
    } catch {
      setError(t("auth.invalid", "Invalid email or password."));
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthLayout title={t("auth.welcome", "Welcome back")} subtitle={t("auth.sub", "Sign in to your NVR")}>
      <form onSubmit={onSubmit} className="space-y-4">
        <div>
          <label className="label" htmlFor="email">
            {t("auth.email", "Email")}
          </label>
          <input
            id="email"
            type="email"
            autoFocus
            autoComplete="username"
            className="input"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
          />
        </div>
        <div>
          <label className="label" htmlFor="password">
            {t("auth.password", "Password")}
          </label>
          <input
            id="password"
            type="password"
            autoComplete="current-password"
            className="input"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </div>
        {error && <p className="text-sm text-rec">{error}</p>}
        <button type="submit" className="btn-primary w-full" disabled={busy}>
          {busy ? t("auth.signingIn", "Signing in…") : t("auth.signIn", "Sign in")}
        </button>
      </form>

      {sso.enabled && (
        <>
          <div className="my-5 flex items-center gap-3 text-fg-faint">
            <span className="h-px flex-1 bg-line" />
            <span className="text-xs uppercase tracking-wide">{t("auth.or", "or")}</span>
            <span className="h-px flex-1 bg-line" />
          </div>
          <a href="/api/auth/oidc/login" className="btn-secondary w-full justify-center">
            {t("auth.signInWith", "Sign in with {{name}}", { name: sso.name })}
          </a>
        </>
      )}
    </AuthLayout>
  );
}
