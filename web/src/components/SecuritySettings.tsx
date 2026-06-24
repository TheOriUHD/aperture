import { useState, type FormEvent } from "react";
import { ShieldCheck, ShieldOff, KeyRound } from "lucide-react";
import { Modal } from "./ui/Modal";
import { TwoFactorSetup } from "./TwoFactorSetup";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";

export function SecuritySettings() {
  const { user, refresh } = useAuth();
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
      await api.post("/api/auth/password", { currentPassword: cur, newPassword: nw });
      setPwMsg("Password updated.");
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
    } catch (e) {
      setDisableErr((e as Error).message || "Verification failed.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="panel">
      <h2 className="text-sm font-medium text-fg mb-3">Security</h2>

      <div className="flex items-center justify-between rounded-lg bg-ink-850 p-3.5 mb-4">
        <div className="flex items-center gap-3">
          {user?.twoFactor ? (
            <ShieldCheck className="h-5 w-5 text-online" />
          ) : (
            <ShieldOff className="h-5 w-5 text-fg-muted" />
          )}
          <div>
            <p className="text-sm text-fg">Two-factor authentication</p>
            <p className="text-xs text-fg-faint">
              {user?.twoFactor
                ? "On — a one-time code is required at sign-in."
                : "Off — add an authenticator app for stronger security."}
            </p>
          </div>
        </div>
        {user?.twoFactor ? (
          <button
            className="btn-ghost !text-rec !border-rec/30 hover:!bg-rec/10"
            onClick={() => setDisabling(true)}
          >
            Turn off
          </button>
        ) : (
          <button className="btn-subtle" onClick={() => setSetup(true)}>
            Set up
          </button>
        )}
      </div>

      <form onSubmit={changePw} className="space-y-3">
        <p className="text-xs font-medium text-fg-muted flex items-center gap-1.5">
          <KeyRound className="h-3.5 w-3.5" /> Change password
        </p>
        <input
          className="input"
          type="password"
          autoComplete="current-password"
          placeholder="Current password"
          value={cur}
          onChange={(e) => setCur(e.target.value)}
        />
        <div className="grid grid-cols-2 gap-3">
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            placeholder="New password"
            value={nw}
            onChange={(e) => setNw(e.target.value)}
          />
          <input
            className="input"
            type="password"
            autoComplete="new-password"
            placeholder="Repeat new"
            value={nw2}
            onChange={(e) => setNw2(e.target.value)}
          />
        </div>
        {pwErr && <p className="text-xs text-rec">{pwErr}</p>}
        {pwMsg && <p className="text-xs text-online">{pwMsg}</p>}
        <button type="submit" className="btn-primary" disabled={busy || !cur || !nw}>
          Update password
        </button>
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
            <button
              className="btn-primary !bg-rec hover:!bg-rec/90"
              disabled={busy || !disableVal.trim()}
              onClick={disable2fa}
            >
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
          {disableErr && <p className="text-xs text-rec">{disableErr}</p>}
        </div>
      </Modal>
    </div>
  );
}
