import { useEffect, useState, type FormEvent } from "react";
import { useTranslation } from "react-i18next";
import { KeyRound, LogIn } from "lucide-react";
import { useAuth } from "../lib/auth";
import { api } from "../lib/api";
import { passkeysSupported, passkeyError } from "../lib/passkey";
import { AuthCheck, AuthDivider, AuthError, AuthField, AuthLayout, AuthNote, PasswordField } from "./AuthLayout";

export function Login() {
  const { t } = useTranslation();
  const { login, login2fa, loginPasskey } = useAuth();
  const canPasskey = passkeysSupported();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [tfToken, setTfToken] = useState<string | null>(null);
  const [code, setCode] = useState("");
  // On by default: the point is not being asked again on your own devices.
  const [trustDevice, setTrustDevice] = useState(true);
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

  async function onSubmit(e: FormEvent<HTMLFormElement>) {
    e.preventDefault();
    // Read the form itself: a password manager can fill the fields without the
    // change events React state relies on.
    const form = new FormData(e.currentTarget);
    const em = String(form.get("email") ?? email).trim();
    const pw = String(form.get("password") ?? password);
    if (!em || !pw) {
      setError(t("auth.missing", "Enter your email and password."));
      return;
    }
    setError("");
    setBusy(true);
    try {
      const r = await login(em, pw);
      if (r.twoFactor) setTfToken(r.token ?? "");
    } catch {
      setError(t("auth.invalid", "Invalid email or password."));
    } finally {
      setBusy(false);
    }
  }

  async function onPasskey() {
    setError("");
    setBusy(true);
    try {
      await loginPasskey();
    } catch (e) {
      setError(passkeyError(e));
    } finally {
      setBusy(false);
    }
  }

  async function onSubmit2fa(e: FormEvent) {
    e.preventDefault();
    setError("");
    setBusy(true);
    try {
      await login2fa(tfToken ?? "", code.trim(), trustDevice);
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
        <form onSubmit={onSubmit2fa} className="space-y-6">
          <AuthField
            id="code"
            label={t("auth.code", "Authentication code")}
            autoFocus
            inputMode="numeric"
            autoComplete="one-time-code"
            inputClassName="font-mono text-lg tracking-[0.35em]"
            placeholder="000000"
            value={code}
            onChange={(e) => setCode(e.target.value)}
          />
          <AuthCheck
            checked={trustDevice}
            onChange={setTrustDevice}
            label={t("auth.trustDevice", "Trust this device")}
            hint={t("auth.trustDeviceHint", "Don't ask for a code when I sign in on this browser again.")}
          />
          {error && <AuthError>{error}</AuthError>}
          <div className="space-y-2.5 pt-2">
            <button type="submit" className="btn-primary h-10 w-full" disabled={busy || code.trim().length < 6}>
              {busy ? t("auth.verifying", "Verifying…") : t("auth.verify", "Verify")}
            </button>
            <button
              type="button"
              className="btn-ghost h-10 w-full"
              onClick={() => {
                setTfToken(null);
                setCode("");
                setError("");
              }}
            >
              {t("common.cancel", "Cancel")}
            </button>
          </div>
        </form>
        <AuthNote className="mt-8">{t("auth.recoveryHint", "Lost your device? Enter one of your recovery codes.")}</AuthNote>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout>
      <form onSubmit={onSubmit} className="space-y-6" noValidate>
        <AuthField
          id="email"
          name="email"
          type="email"
          label={t("auth.email", "Email")}
          autoFocus
          autoComplete="username"
          autoCapitalize="none"
          spellCheck={false}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <PasswordField
          id="password"
          name="password"
          label={t("auth.password", "Password")}
          autoComplete="current-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        {error && <AuthError>{error}</AuthError>}
        <div className="pt-2">
          <button type="submit" className="btn-primary h-10 w-full" disabled={busy}>
            {busy ? t("auth.signingIn", "Signing in…") : t("auth.signIn", "Sign in")}
          </button>
        </div>
      </form>

      {(canPasskey || sso.enabled) && (
        <>
          <AuthDivider>{t("auth.or", "or")}</AuthDivider>
          <div className="space-y-2.5">
            {canPasskey && (
              <button type="button" className="btn-ghost h-10 w-full" disabled={busy} onClick={onPasskey}>
                <KeyRound className="h-4 w-4" />
                {t("auth.signInPasskey", "Sign in with a passkey")}
              </button>
            )}
            {sso.enabled && (
              <a href="/api/auth/oidc/login" className="btn-ghost h-10 w-full">
                <LogIn className="h-4 w-4" />
                {t("auth.signInWith", "Sign in with {{name}}", { name: sso.name })}
              </a>
            )}
          </div>
        </>
      )}

      <AuthNote className="mt-8">
        {t("auth.forgot", "Forgot your password? An administrator can set a new one for you under Users.")}
      </AuthNote>
    </AuthLayout>
  );
}
