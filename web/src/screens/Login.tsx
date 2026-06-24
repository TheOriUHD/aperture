import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { useAuth } from "../lib/auth";
import { api } from "../lib/api";
import { AuthLayout } from "./AuthLayout";

export function Login() {
  const { t } = useTranslation();
  const { login, login2fa } = useAuth();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tfToken, setTfToken] = useState<string | null>(null);
  const [code, setCode] = useState("");
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
      const r = await login(email, password);
      if (r.twoFactor) setTfToken(r.token ?? "");
    } catch {
      setError(t("auth.invalid", "Invalid email or password."));
    } finally {
      setBusy(false);
    }
  }

  async function onSubmit2fa(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await login2fa(tfToken ?? "", code.trim());
    } catch {
      setError(t("auth.badCode", "Incorrect code. Try again, or use a recovery code."));
    } finally {
      setBusy(false);
    }
  }

  if (tfToken !== null) {
    return (
      <AuthLayout
        title={t("auth.twoFactor", "Two-factor authentication")}
        subtitle={t("auth.enterCode", "Enter the code from your authenticator app")}
      >
        <form onSubmit={onSubmit2fa} className="space-y-4">
          <input
            autoFocus
            inputMode="numeric"
            autoComplete="one-time-code"
            className="input text-center tracking-[0.4em] text-lg font-mono"
            placeholder="000000"
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
          {error && <p className="text-sm text-rec">{error}</p>}
          <button type="submit" className="btn-primary w-full" disabled={busy || code.trim().length < 6}>
            {busy ? t("auth.verifying", "Verifying…") : t("auth.verify", "Verify")}
          </button>
          <button
            type="button"
            className="btn-ghost w-full"
            onClick={() => {
              setTfToken(null);
              setCode("");
              setError("");
            }}
          >
            {t("common.cancel", "Cancel")}
          </button>
          <p className="text-xs text-fg-faint text-center">
            {t("auth.recoveryHint", "Lost your device? Enter one of your recovery codes.")}
          </p>
        </form>
      </AuthLayout>
    );
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
