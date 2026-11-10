import { useState, type FormEvent } from "react";
import { useAuth } from "../lib/auth";
import { api } from "../lib/api";
import { AuthError, AuthLayout, PasswordField } from "./AuthLayout";
import { EnrollChooser } from "../components/EnrollChooser";

// FirstRun gates a freshly-created account: force a new password, then offer
// (but don't require) two-factor enrollment before entering the app.
export function FirstRun() {
  const { refresh } = useAuth();
  const [step, setStep] = useState<"password" | "twofa">("password");
  const [pw, setPw] = useState("");
  const [pw2, setPw2] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);

  async function changePw(e: FormEvent) {
    e.preventDefault();
    setErr("");
    if (pw.length < 8) {
      setErr("Use at least 8 characters.");
      return;
    }
    if (pw !== pw2) {
      setErr("The passwords don't match.");
      return;
    }
    setBusy(true);
    try {
      await api.post("/api/auth/password", { newPassword: pw });
      setStep("twofa");
    } catch (e) {
      setErr((e as Error).message || "Could not set your password.");
    } finally {
      setBusy(false);
    }
  }

  if (step === "password") {
    return (
      <AuthLayout title="Set your password" subtitle="Choose a new password to finish setting up your account">
        <form onSubmit={changePw} className="space-y-6">
          <PasswordField
            id="new-password"
            label="New password"
            autoFocus
            autoComplete="new-password"
            value={pw}
            onChange={(e) => setPw(e.target.value)}
          />
          <PasswordField
            id="repeat-password"
            label="Repeat password"
            autoComplete="new-password"
            value={pw2}
            onChange={(e) => setPw2(e.target.value)}
          />
          {err && <AuthError>{err}</AuthError>}
          <div className="pt-2">
            <button type="submit" className="btn-primary h-10 w-full" disabled={busy}>
              {busy ? "Saving…" : "Set password"}
            </button>
          </div>
        </form>
      </AuthLayout>
    );
  }

  return (
    <AuthLayout title="Welcome to Aperture" subtitle="A couple of optional steps to keep your account safe">
      <EnrollChooser onFinish={() => void refresh()} />
    </AuthLayout>
  );
}
