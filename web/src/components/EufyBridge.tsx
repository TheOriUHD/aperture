import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Check, Copy, KeyRound, Loader2, Video } from "lucide-react";
import { api } from "../lib/api";

// The Eufy bridge's setup surface.
//
// Eufy publishes no local device API, so control goes through a bridge that signs in
// to a Eufy cloud account — and that sign-in can raise a two-factor code or a captcha
// at any point. Both used to be answerable only from a shell. This card is the whole
// flow: account, challenge, and what the account actually found.
//
// It is deliberately one card that changes state rather than a multi-step wizard: the
// states are mutually exclusive and mostly transient, and a stepper would imply the
// user can navigate between them when in reality the bridge decides which one they
// are in.

interface EufyDevice {
  serialNumber: string;
  name: string;
  model?: string;
  panTilt: boolean;
}

interface EufyStatus {
  configured: boolean;
  bridgeRunning: boolean;
  connected: boolean;
  needsVerifyCode: boolean;
  needsCaptcha: boolean;
  captchaId?: string;
  captchaImage?: string;
  username?: string;
  lastError?: string;
  devices: EufyDevice[];
}

const ENDPOINT = "/api/system/integrations/eufy";

export function EufyBridge() {
  const qc = useQueryClient();
  const { data: st } = useQuery({
    queryKey: ["eufy"],
    queryFn: () => api.get<EufyStatus>(ENDPOINT),
    // Sign-in challenges arrive asynchronously from the Eufy cloud, so this polls
    // while anything is in flight. Once connected there is nothing to watch for.
    refetchInterval: (q) => {
      const d = q.state.data as EufyStatus | undefined;
      return d?.connected && !d?.needsCaptcha && !d?.needsVerifyCode ? false : 4000;
    },
  });

  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [country, setCountry] = useState("DE");
  const [code, setCode] = useState("");
  const [busy, setBusy] = useState("");
  const [err, setErr] = useState("");
  const [copied, setCopied] = useState("");

  useEffect(() => {
    if (st?.username) setUsername(st.username);
  }, [st?.username]);

  const refresh = () => qc.invalidateQueries({ queryKey: ["eufy"] });

  async function run(what: string, fn: () => Promise<unknown>) {
    setBusy(what);
    setErr("");
    try {
      await fn();
      await refresh();
      setPassword("");
      setCode("");
    } catch (e) {
      setErr((e as Error).message || "that did not work");
    } finally {
      setBusy("");
    }
  }

  async function copy(text: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(text);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* clipboard unavailable; the value is selectable anyway */
    }
  }

  const state: "signin" | "verify" | "captcha" | "connected" | "down" = !st
    ? "signin"
    : st.needsCaptcha
      ? "captcha"
      : st.needsVerifyCode
        ? "verify"
        : st.connected
          ? "connected"
          : !st.bridgeRunning && st.configured
            ? "down"
            : "signin";

  return (
    <div className="panel">
      <h2 className="flex items-center gap-2 text-sm font-medium text-fg mb-1">
        <Video className="h-4 w-4" />
        Eufy
        <StatusPill st={st} />
      </h2>
      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Eufy has no local API, so pan/tilt and device control go through a bridge that signs in to
        your Eufy account. Your password is stored encrypted here and passed only to that bridge.
      </p>

      {state === "captcha" && (
        <div className="space-y-3">
          <p className="text-xs text-fg-muted">Eufy is asking for a captcha to finish signing in.</p>
          {st?.captchaImage && (
            <img
              src={st.captchaImage}
              alt="Captcha challenge"
              className="rounded-md border border-line bg-white p-1"
            />
          )}
          <div className="flex gap-2">
            <input
              className="input flex-1"
              value={code}
              onChange={(e) => setCode(e.target.value)}
              placeholder="What the image says"
              autoFocus
            />
            <button
              className="btn-primary"
              disabled={!code.trim() || busy !== ""}
              onClick={() =>
                run("captcha", () =>
                  api.post(`${ENDPOINT}/captcha`, { captchaId: st?.captchaId ?? "", code: code.trim() }),
                )
              }
            >
              {busy === "captcha" ? <Loader2 className="h-4 w-4 animate-spin" /> : "Submit"}
            </button>
          </div>
        </div>
      )}

      {state === "verify" && (
        <div className="space-y-3">
          <p className="text-xs text-fg-muted">
            Eufy emailed a verification code to <span className="text-fg">{st?.username}</span>.
          </p>
          <div className="flex gap-2">
            <input
              className="input flex-1 font-mono tracking-widest"
              value={code}
              onChange={(e) => setCode(e.target.value.replace(/\D/g, "").slice(0, 8))}
              placeholder="123456"
              inputMode="numeric"
              autoFocus
            />
            <button
              className="btn-primary"
              disabled={!code.trim() || busy !== ""}
              onClick={() => run("verify", () => api.post(`${ENDPOINT}/verify-code`, { code: code.trim() }))}
            >
              {busy === "verify" ? <Loader2 className="h-4 w-4 animate-spin" /> : "Verify"}
            </button>
          </div>
        </div>
      )}

      {state === "down" && (
        <div className="rounded-md border border-rec/30 bg-rec/10 px-3 py-2 text-xs text-fg-muted">
          The bridge is not responding. It may still be starting, or it may have rejected the
          account.
          {st?.lastError && <span className="mt-1 block font-mono text-fg-faint">{st.lastError}</span>}
        </div>
      )}

      {(state === "signin" || state === "down") && (
        <div className="space-y-3">
          <div>
            <label className="label">Eufy account email</label>
            <input
              className="input"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="you@example.com"
              autoComplete="off"
            />
          </div>
          <div className="grid grid-cols-3 gap-3">
            <div className="col-span-2">
              <label className="label">Password</label>
              <input
                className="input"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={st?.configured ? "unchanged" : ""}
                autoComplete="new-password"
              />
            </div>
            <div>
              <label className="label">Country</label>
              <input
                className="input uppercase"
                value={country}
                onChange={(e) => setCountry(e.target.value.toUpperCase().slice(0, 2))}
                maxLength={2}
              />
            </div>
          </div>
          <p className="text-xs text-fg-faint">
            Saving restarts the bridge — it only reads the account at startup. Expect a
            verification code by email straight after.
          </p>
          <div className="flex justify-end">
            <button
              className="btn-primary"
              disabled={!username.trim() || (!password && !st?.configured) || busy !== ""}
              onClick={() =>
                run("save", () =>
                  api.put(ENDPOINT, { username: username.trim(), password, country: country.trim() }),
                )
              }
            >
              {busy === "save" ? (
                <>
                  <Loader2 className="h-4 w-4 animate-spin" /> Restarting bridge…
                </>
              ) : (
                <>
                  <KeyRound className="h-4 w-4" /> {st?.configured ? "Update account" : "Sign in"}
                </>
              )}
            </button>
          </div>
        </div>
      )}

      {state === "connected" && (
        <div className="space-y-3">
          {st!.devices.length === 0 ? (
            <p className="text-xs text-fg-faint">
              Signed in, but this account reports no devices yet. Eufy can take a minute after a
              fresh sign-in.
            </p>
          ) : (
            <>
              <p className="text-xs text-fg-faint">
                Use a serial number in the Eufy Control extension to attach its controls to a camera.
              </p>
              <div className="divide-y divide-line rounded-md border border-line">
                {st!.devices.map((d) => (
                  <div key={d.serialNumber} className="flex items-center gap-3 px-3 py-2">
                    <div className="min-w-0 flex-1">
                      <div className="truncate text-sm text-fg">{d.name || d.serialNumber}</div>
                      <div className="text-[11px] text-fg-faint">
                        {d.model ? `${d.model} · ` : ""}
                        {d.panTilt ? "pan & tilt" : "no pan/tilt"}
                      </div>
                    </div>
                    <button
                      className="btn-ghost h-8 gap-1.5 text-[11px]"
                      onClick={() => copy(d.serialNumber)}
                      title="Copy serial number"
                    >
                      {copied === d.serialNumber ? (
                        <Check className="h-3.5 w-3.5 text-online" />
                      ) : (
                        <Copy className="h-3.5 w-3.5" />
                      )}
                      <span className="font-mono">{d.serialNumber}</span>
                    </button>
                  </div>
                ))}
              </div>
            </>
          )}
          <p className="text-right text-[11px] text-fg-faint">Signed in as {st?.username}</p>
        </div>
      )}

      {err && <p className="mt-3 text-xs text-rec">{err}</p>}
    </div>
  );
}

function StatusPill({ st }: { st?: EufyStatus }) {
  if (!st) return null;
  const [text, tone] = !st.configured
    ? ["not set up", "text-fg-faint"]
    : st.connected
      ? ["connected", "text-online"]
      : st.needsCaptcha || st.needsVerifyCode
        ? ["needs you", "text-[#e6b45a]"]
        : st.bridgeRunning
          ? ["signing in…", "text-fg-muted"]
          : ["offline", "text-rec"];
  return <span className={`ml-auto text-[11px] font-normal ${tone}`}>{text}</span>;
}
