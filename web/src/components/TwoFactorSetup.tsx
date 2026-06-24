import { useEffect, useState } from "react";
import QRCode from "qrcode";
import { ShieldCheck } from "lucide-react";
import { api } from "../lib/api";
import { Spinner } from "./ui/Spinner";

// TwoFactorSetup runs the enrollment flow: fetch a secret, render its QR,
// verify the first code, then reveal one-time recovery codes.
export function TwoFactorSetup({ onDone }: { onDone: () => void }) {
  const [qr, setQr] = useState("");
  const [secret, setSecret] = useState("");
  const [code, setCode] = useState("");
  const [err, setErr] = useState("");
  const [busy, setBusy] = useState(false);
  const [recovery, setRecovery] = useState<string[] | null>(null);

  useEffect(() => {
    void api
      .post<{ otpauthUrl: string; secret: string }>("/api/auth/2fa/setup")
      .then(async (r) => {
        setSecret(r.secret);
        setQr(await QRCode.toDataURL(r.otpauthUrl, { margin: 1, width: 200 }));
      })
      .catch(() => setErr("Could not start setup."));
  }, []);

  async function verify() {
    setErr("");
    setBusy(true);
    try {
      const r = await api.post<{ recoveryCodes: string[] }>("/api/auth/2fa/enable", { code: code.trim() });
      setRecovery(r.recoveryCodes);
    } catch (e) {
      setErr((e as Error).message || "Incorrect code.");
    } finally {
      setBusy(false);
    }
  }

  if (recovery) {
    return (
      <div className="space-y-3">
        <p className="flex items-center gap-2 text-sm text-online">
          <ShieldCheck className="h-4 w-4" /> Two-factor authentication is on.
        </p>
        <p className="text-xs text-fg-faint">
          Save these recovery codes somewhere safe — each works once if you lose your authenticator.
        </p>
        <div className="grid grid-cols-2 gap-1.5 font-mono text-xs bg-ink-850 rounded-lg p-3">
          {recovery.map((c) => (
            <span key={c} className="text-fg-muted">
              {c}
            </span>
          ))}
        </div>
        <button className="btn-primary w-full" onClick={onDone}>
          Done
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-3">
      <p className="text-xs text-fg-faint leading-relaxed">
        Scan this with an authenticator app (Google Authenticator, Authy, 1Password…), then enter the
        6-digit code to confirm.
      </p>
      <div className="grid place-items-center min-h-[160px]">
        {qr ? <img src={qr} alt="2FA QR code" className="rounded-lg bg-white p-2" /> : <Spinner />}
      </div>
      {secret && (
        <p className="text-center text-xs text-fg-faint">
          Or enter manually: <code className="text-fg-muted break-all">{secret}</code>
        </p>
      )}
      <input
        className="input text-center tracking-[0.3em] font-mono"
        placeholder="000000"
        inputMode="numeric"
        autoComplete="one-time-code"
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {err && <p className="text-xs text-rec">{err}</p>}
      <button className="btn-primary w-full" disabled={busy || code.trim().length < 6} onClick={verify}>
        {busy ? "Verifying…" : "Enable two-factor"}
      </button>
    </div>
  );
}
