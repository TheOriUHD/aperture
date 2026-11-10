import { useState, type FormEvent } from "react";
import { useAuth } from "../lib/auth";
import { AuthError, AuthField, AuthLayout, PasswordField } from "./AuthLayout";

export function Setup() {
  const { setup } = useAuth();
  const [name, setName] = useState("");
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [confirm, setConfirm] = useState("");
  const [error, setError] = useState("");
  const [busy, setBusy] = useState(false);

  async function onSubmit(e: FormEvent) {
    e.preventDefault();
    setError("");
    if (password.length < 8) return setError("Password must be at least 8 characters.");
    if (password !== confirm) return setError("Passwords do not match.");
    setBusy(true);
    try {
      await setup(email, name, password);
    } catch {
      setError("Could not create the administrator account.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <AuthLayout title="Welcome to Aperture" subtitle="Create the administrator account">
      <form onSubmit={onSubmit} className="space-y-6">
        <AuthField id="name" label="Name" autoFocus autoComplete="name" value={name} onChange={(e) => setName(e.target.value)} />
        <AuthField
          id="email"
          type="email"
          label="Email"
          autoComplete="email"
          autoCapitalize="none"
          spellCheck={false}
          value={email}
          onChange={(e) => setEmail(e.target.value)}
        />
        <PasswordField
          id="password"
          label="Password"
          autoComplete="new-password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
        />
        <PasswordField
          id="confirm"
          label="Confirm password"
          autoComplete="new-password"
          value={confirm}
          onChange={(e) => setConfirm(e.target.value)}
        />
        {error && <AuthError>{error}</AuthError>}
        <div className="pt-2">
          <button type="submit" className="btn-primary h-10 w-full" disabled={busy}>
            {busy ? "Creating…" : "Create account"}
          </button>
        </div>
      </form>
    </AuthLayout>
  );
}
