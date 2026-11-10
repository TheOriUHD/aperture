import { useState, type FormEvent } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { ShieldCheck, ShieldOff, KeyRound, Fingerprint, Trash2, Plus } from "lucide-react";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { Modal } from "./ui/Modal";
import { TwoFactorSetup } from "./TwoFactorSetup";
import { SignedInDevices } from "./SignedInDevices";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { registerPasskey, passkeysSupported, passkeyError } from "../lib/passkey";

interface PasskeyRow {
  id: string;
  name: string;
  createdAt: number;
  lastUsed: number;
}

export function SecuritySettings() {
  const { user, refresh } = useAuth();
  const qc = useQueryClient();
  const passkeys = useQuery({
    queryKey: ["passkeys"],
    queryFn: () => api.get<PasskeyRow[]>("/api/auth/passkeys"),
  });
  const [pkBusy, setPkBusy] = useState(false);
  const [pkErr, setPkErr] = useState("");
  const canPasskey = passkeysSupported();

  async function addPasskey() {
    setPkErr("");
    setPkBusy(true);
    try {
      await registerPasskey("Passkey");
      await passkeys.refetch();
      await refresh();
    } catch (e) {
      setPkErr(passkeyError(e));
    } finally {
      setPkBusy(false);
    }
  }

  async function removePasskey(id: string) {
    await api.del(`/api/auth/passkeys/${id}`);
    await passkeys.refetch();
    await refresh();
  }
  const [setup, setSetup] = useState(false);
  const [disabling, setDisabling] = useState(false);
  const [disableVal, setDisableVal] = useState("");
  const [disableErr, setDisableErr] = useState("");
  const [busy, setBusy] = useState(false);

  const [cur, setCur] = useState("");
  const [nw, setNw] = useState("");
  const [nw2, setNw2] = useState("");
  const [pwMsg, setPwMsg] = useState("");
  const [pwErr, setPwErr] = useState("");

  async function changePw(e: FormEvent) {
    e.preventDefault();
    setPwErr("");
    setPwMsg("");
    if (nw.length < 8) {
      setPwErr("Use at least 8 characters.");
      return;
    }
    if (nw !== nw2) {
      setPwErr("The passwords don't match.");
      return;
    }
    setBusy(true);
    try {
      const r = await api.post<{ signedOut?: number }>("/api/auth/password", { currentPassword: cur, newPassword: nw });
      const n = r?.signedOut ?? 0;
      setPwMsg(
        n > 0 ? `Password updated. ${n} other device${n === 1 ? " was" : "s were"} signed out.` : "Password updated.",
      );
      await qc.invalidateQueries({ queryKey: ["sessions"] });
      setCur("");
      setNw("");
      setNw2("");
    } catch (e) {
      setPwErr((e as Error).message || "Could not change password.");
    } finally {
      setBusy(false);
    }
  }

  async function disable2fa() {
    setDisableErr("");
    setBusy(true);
    try {
      // The same field is sent as both code and password; the server accepts
      // whichever verifies.
      await api.post("/api/auth/2fa/disable", { code: disableVal.trim(), password: disableVal.trim() });
      setDisabling(false);
      setDisableVal("");
      await refresh();
      await qc.invalidateQueries({ queryKey: ["sessions"] }); // trusted marks are gone
    } catch (e) {
      setDisableErr((e as Error).message || "Verification failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <>
      <SettingsHeading>Sign-in</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow
          label="Two-factor authentication"
          hint={
            user?.twoFactor
              ? "On — a one-time code is required when you sign in on a new device."
              : "Off — add an authenticator app for stronger security."
          }
        >
          <div className="flex flex-wrap items-center gap-3">
            {user?.twoFactor ? (
              <span className="pill-green">
                <ShieldCheck className="h-3 w-3" /> On
              </span>
            ) : (
              <span className="pill-gray">
                <ShieldOff className="h-3 w-3" /> Off
              </span>
            )}
            {user?.twoFactor ? (
              <button className="btn-ghost text-danger hover:border-danger/50 hover:bg-danger/10" onClick={() => setDisabling(true)}>
                Turn off
              </button>
            ) : (
              <button className="btn-outline" onClick={() => setSetup(true)}>
                Set up
              </button>
            )}
          </div>
        </SettingsRow>

        <SettingsRow
          label="Passkeys"
          hint="Sign in with your fingerprint, face, or device PIN — no password."
          align="start"
        >
          <div className="max-w-xl">
            {(passkeys.data?.length ?? 0) > 0 && (
              <ul className="mb-3 divide-y divide-line border-y border-line">
                {passkeys.data!.map((p) => (
                  <li key={p.id} className="flex items-center gap-3 py-2 transition-colors hover:bg-white/[0.02]">
                    <Fingerprint className="h-4 w-4 shrink-0 text-fg-muted" />
                    <div className="min-w-0 flex-1">
                      <p className="truncate text-sm text-fg">{p.name || "Passkey"}</p>
                      <p className="text-xs text-fg-faint">
                        Added {new Date(p.createdAt * 1000).toLocaleDateString()}
                        {p.lastUsed > 0 && ` · last used ${new Date(p.lastUsed * 1000).toLocaleDateString()}`}
                      </p>
                    </div>
                    <button
                      className="btn-icon hover:text-danger"
                      title="Remove passkey"
                      aria-label="Remove passkey"
                      onClick={() => removePasskey(p.id)}
                    >
                      <Trash2 className="h-4 w-4" />
                    </button>
                  </li>
                ))}
              </ul>
            )}
            <button className="btn-outline" disabled={!canPasskey || pkBusy} onClick={addPasskey}>
              <Plus className="h-4 w-4" />
              {pkBusy ? "Waiting…" : "Add passkey"}
            </button>
            {!canPasskey && (
              <p className="mt-2 text-xs text-warn">
                Passkeys need HTTPS or localhost — not available in this browser context.
              </p>
            )}
            {pkErr && <p className="mt-2 text-xs text-danger">{pkErr}</p>}
          </div>
        </SettingsRow>
      </div>

      <SignedInDevices />

      <SettingsHeading>Change password</SettingsHeading>
      <form onSubmit={changePw} className="divide-y divide-line">
        <SettingsRow label="Current password">
          <input
            className="input max-w-sm"
            type="password"
            autoComplete="current-password"
            aria-label="Current password"
            value={cur}
            onChange={(e) => setCur(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="New password" hint="At least 8 characters.">
          <input
            className="input max-w-sm"
            type="password"
            autoComplete="new-password"
            aria-label="New password"
            value={nw}
            onChange={(e) => setNw(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Repeat new password">
          <input
            className="input max-w-sm"
            type="password"
            autoComplete="new-password"
            aria-label="Repeat new password"
            value={nw2}
            onChange={(e) => setNw2(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="">
          <div className="flex flex-wrap items-center gap-3">
            <button type="submit" className="btn-primary" disabled={busy || !cur || !nw}>
              <KeyRound className="h-4 w-4" />
              Update password
            </button>
            {pwErr && <p className="text-xs text-danger">{pwErr}</p>}
            {pwMsg && <p className="text-xs text-online">{pwMsg}</p>}
          </div>
        </SettingsRow>
      </form>

      <Modal open={setup} onClose={() => setSetup(false)} title="Set up two-factor">
        <TwoFactorSetup
          onDone={async () => {
            setSetup(false);
            await refresh();
          }}
        />
      </Modal>

      <Modal
        open={disabling}
        onClose={() => {
          setDisabling(false);
          setDisableVal("");
          setDisableErr("");
        }}
        title="Turn off two-factor"
        footer={
          <div className="flex justify-end gap-2 w-full">
            <button
              className="btn-ghost"
              onClick={() => {
                setDisabling(false);
                setDisableVal("");
              }}
            >
              Cancel
            </button>
            <button className="btn-danger" disabled={busy || !disableVal.trim()} onClick={disable2fa}>
              Turn off
            </button>
          </div>
        }
      >
        <div className="space-y-3">
          <p className="text-sm text-fg-muted">
            Confirm with a current authenticator code or your password.
          </p>
          <input
            className="input"
            placeholder="Code or password"
            value={disableVal}
            onChange={(e) => setDisableVal(e.target.value)}
          />
          {disableErr && <p className="text-xs text-danger">{disableErr}</p>}
        </div>
      </Modal>
    </>
  );
}
