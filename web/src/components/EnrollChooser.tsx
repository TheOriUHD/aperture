import { useState, type ReactNode } from "react";
import { KeyRound, Smartphone, Check, ChevronRight } from "lucide-react";
import { useAuth } from "../lib/auth";
import { registerPasskey, passkeysSupported, passkeyError } from "../lib/passkey";
import { TwoFactorSetup } from "./TwoFactorSetup";

// EnrollChooser offers a passkey, an authenticator app, or nothing. After one
// method is added it re-asks — the added one shows a green check, the wording
// shifts, and the opt-out is renamed to suit "you're already part-secured".
export function EnrollChooser({ onFinish }: { onFinish: () => void }) {
  const { user } = useAuth();
  const [added, setAdded] = useState({
    passkey: (user?.passkeys ?? 0) > 0,
    totp: !!user?.twoFactor,
  });
  const [view, setView] = useState<"choose" | "totp">("choose");
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState("");
  const supported = passkeysSupported();

  const count = (added.passkey ? 1 : 0) + (added.totp ? 1 : 0);

  async function addPasskey() {
    setErr("");
    setBusy(true);
    try {
      await registerPasskey("Passkey");
      setAdded((a) => ({ ...a, passkey: true }));
    } catch (e) {
      setErr(passkeyError(e));
    } finally {
      setBusy(false);
    }
  }

  if (view === "totp") {
    return (
      <TwoFactorSetup
        onDone={() => {
          setAdded((a) => ({ ...a, totp: true }));
          setView("choose");
        }}
      />
    );
  }

  const title = count === 0 ? "Secure your account" : "Add another way to sign in?";
  const subtitle =
    count === 0
      ? "Add a passkey or an authenticator app — or skip for now."
      : count === 2
        ? "You're all set — both methods are active."
        : "Nice. Add the other method too, or you're all set.";

  return (
    <div className="space-y-3">
      <div>
        <h3 className="text-base font-medium text-fg">{title}</h3>
        <p className="text-sm text-fg-muted mt-0.5">{subtitle}</p>
      </div>

      <OptionRow
        icon={<KeyRound className="h-5 w-5" />}
        title="Passkey"
        subtitle={
          added.passkey
            ? "Added — sign in with your fingerprint, face, or device PIN."
            : supported
              ? "Use your fingerprint, face, or device PIN. Nothing to type or lose."
              : "Needs HTTPS or localhost in this browser."
        }
        added={added.passkey}
        disabled={!supported || busy}
        onClick={addPasskey}
      />

      <OptionRow
        icon={<Smartphone className="h-5 w-5" />}
        title="Authenticator app"
        subtitle={
          added.totp
            ? "Added — a one-time code is required at sign-in."
            : "A 6-digit code from Google Authenticator, Authy, 1Password…"
        }
        added={added.totp}
        disabled={busy}
        onClick={() => setView("totp")}
      />

      {err && <p className="text-xs text-rec">{err}</p>}

      <button
        className={`${count === 0 ? "btn-ghost" : "btn-primary"} w-full`}
        disabled={busy}
        onClick={onFinish}
      >
        {count === 0 ? "Skip for now" : count === 2 ? "Done" : "No thanks, I'm all set"}
      </button>
    </div>
  );
}

function OptionRow({
  icon,
  title,
  subtitle,
  added,
  disabled,
  onClick,
}: {
  icon: ReactNode;
  title: string;
  subtitle: string;
  added: boolean;
  disabled: boolean;
  onClick: () => void;
}) {
  const actionable = !added && !disabled;
  return (
    <button
      type="button"
      disabled={added || disabled}
      onClick={onClick}
      className={`w-full flex items-center gap-3 rounded-lg border p-3.5 text-left transition ${
        added
          ? "border-online/40 bg-online/5"
          : disabled
            ? "border-line bg-ink-850 opacity-60 cursor-not-allowed"
            : "border-line-strong bg-ink-850 hover:border-brand hover:bg-ink-800"
      }`}
    >
      <div
        className={`grid place-items-center h-9 w-9 rounded-lg shrink-0 ${
          added ? "bg-online/15 text-online" : "bg-ink-700 text-brand"
        }`}
      >
        {added ? <Check className="h-5 w-5" /> : icon}
      </div>
      <div className="min-w-0 flex-1">
        <p className="text-sm font-medium text-fg">{title}</p>
        <p className="text-xs text-fg-faint leading-snug">{subtitle}</p>
      </div>
      {added ? (
        <span className="pill bg-online/15 text-online shrink-0">Added</span>
      ) : actionable ? (
        <ChevronRight className="h-4 w-4 text-fg-faint shrink-0" />
      ) : null}
    </button>
  );
}
