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
      <div className="space-y-4">
        <div className="flex items-start gap-3 rounded-md bg-online/10 px-3 py-2.5">
          <ShieldCheck className="mt-0.5 h-4 w-4 shrink-0 text-online" />
          <div className="min-w-0">
            <p className="text-sm font-medium text-fg">Two-factor authentication is on.</p>
            <p className="mt-0.5 text-xs leading-relaxed text-fg-muted">
              Save these recovery codes somewhere safe — each works once if you lose your authenticator.
            </p>
          </div>
        </div>
        <div className="grid grid-cols-2 gap-x-4 gap-y-1.5 rounded-md border border-line bg-ink-900 px-4 py-3 font-mono text-[13px] tabular-nums">
          {recovery.map((c) => (
            <span key={c} className="select-all text-fg/85">
              {c}
            </span>
          ))}
        </div>
        <button className="btn-primary h-10 w-full" onClick={onDone}>
          Done
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <p className="text-sm leading-relaxed text-fg-muted">
        Scan this with an authenticator app (Google Authenticator, Authy, 1Password…), then enter the 6-digit code to
        confirm.
      </p>
      <div className="flex justify-center">
        {qr ? (
          <img src={qr} alt="2FA QR code" className="h-[176px] w-[176px] rounded-lg bg-white p-2" />
        ) : (
          <div className="grid h-[176px] w-[176px] place-items-center rounded-lg border border-line bg-ink-900">
            <Spinner />
          </div>
        )}
      </div>
      {secret && (
        <div>
          <p className="mb-1.5 text-center text-xs text-fg-faint">Or enter this key manually</p>
          <code className="block select-all break-all rounded-md border border-line bg-ink-900 px-3 py-2 text-center font-mono text-xs text-fg-muted">
            {secret}
          </code>
        </div>
      )}
      <input
        className="input h-10 text-center font-mono tracking-[0.3em]"
        placeholder="000000"
        inputMode="numeric"
        autoComplete="one-time-code"
        aria-label="6-digit code"
        value={code}
        onChange={(e) => setCode(e.target.value)}
      />
      {err && <p className="text-xs text-danger">{err}</p>}
      <button className="btn-primary h-10 w-full" disabled={busy || code.trim().length < 6} onClick={verify}>
        {busy ? "Verifying…" : "Enable two-factor"}
      </button>
    </div>
  );
}
