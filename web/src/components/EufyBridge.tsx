import { useEffect, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { AlertTriangle, Check, Copy, KeyRound, Loader2, Video } from "lucide-react";
import { api } from "../lib/api";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { Callout, IntegrationHeader, SectionTitle, StatusPill as Pill } from "./IntegrationParts";

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
    <div>
      <IntegrationHeader icon={Video} title="Eufy" status={<StatusPill st={st} />}>
        Eufy has no local API, so pan/tilt and device control go through a bridge that signs in to
        your Eufy account. Your password is stored encrypted here and passed only to that bridge.
      </IntegrationHeader>

      {state === "captcha" && (
        <>
          <SettingsHeading description="Eufy is asking for a captcha to finish signing in.">Captcha</SettingsHeading>
          <div className="divide-y divide-line">
            {st?.captchaImage && (
              <SettingsRow label="Challenge">
                <img
                  src={st.captchaImage}
                  alt="Captcha challenge"
                  className="rounded-md border border-line bg-white p-1"
                />
              </SettingsRow>
            )}
            <SettingsRow label="Answer">
              <div className="flex max-w-md gap-2">
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
            </SettingsRow>
          </div>
        </>
      )}

      {state === "verify" && (
        <>
          <SettingsHeading
            description={
              <>
                Eufy emailed a verification code to <span className="text-fg">{st?.username}</span>.
              </>
            }
          >
            Verification code
          </SettingsHeading>
          <div className="divide-y divide-line">
            <SettingsRow label="Code">
              <div className="flex max-w-md gap-2">
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
            </SettingsRow>
          </div>
        </>
      )}

      {state === "down" && (
        <Callout tone="danger" icon={AlertTriangle} className="mb-2 mt-2">
          The bridge is not responding. It may still be starting, or it may have rejected the
          account.
          {st?.lastError && <span className="mt-1 block font-mono text-fg-faint">{st.lastError}</span>}
        </Callout>
      )}

      {(state === "signin" || state === "down") && (
        <>
          <SettingsHeading>Account</SettingsHeading>
          <div className="divide-y divide-line">
            <SettingsRow label="Eufy account email">
              <input
                className="input max-w-md"
                value={username}
                onChange={(e) => setUsername(e.target.value)}
                placeholder="you@example.com"
                autoComplete="off"
              />
            </SettingsRow>
            <SettingsRow label="Password">
              <input
                className="input max-w-md"
                type="password"
                value={password}
                onChange={(e) => setPassword(e.target.value)}
                placeholder={st?.configured ? "unchanged" : ""}
                autoComplete="new-password"
              />
            </SettingsRow>
            <SettingsRow label="Country">
              <input
                className="input w-20 uppercase"
                value={country}
                onChange={(e) => setCountry(e.target.value.toUpperCase().slice(0, 2))}
                maxLength={2}
              />
            </SettingsRow>
            <SettingsRow label="">
              <div className="flex flex-wrap items-center gap-x-4 gap-y-2">
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
                <p className="max-w-sm text-xs leading-relaxed text-fg-faint">
                  Saving restarts the bridge — it only reads the account at startup. Expect a
                  verification code by email straight after.
                </p>
              </div>
            </SettingsRow>
          </div>
        </>
      )}

      {state === "connected" && (
        <>
          <SectionTitle
            description={
              st!.devices.length === 0
                ? undefined
                : "Use a serial number in the Eufy Control extension to attach its controls to a camera."
            }
            action={<span className="text-xs text-fg-faint">Signed in as {st?.username}</span>}
          >
            Devices
          </SectionTitle>
          {st!.devices.length === 0 ? (
            <p className="border-y border-line py-3 text-sm text-fg-faint">
              Signed in, but this account reports no devices yet. Eufy can take a minute after a
              fresh sign-in.
            </p>
          ) : (
            <div className="overflow-x-auto">
              <table className="tbl">
                <thead>
                  <tr>
                    <th>Name</th>
                    <th className="hidden sm:table-cell">Model</th>
                    <th className="hidden sm:table-cell">Pan &amp; tilt</th>
                    <th>Serial number</th>
                  </tr>
                </thead>
                <tbody>
                  {st!.devices.map((d) => (
                    <tr key={d.serialNumber}>
                      <td>
                        <span className="block text-fg">{d.name || d.serialNumber}</span>
                        <span className="block text-[11px] text-fg-faint sm:hidden">
                          {d.model ? `${d.model} · ` : ""}
                          {d.panTilt ? "pan & tilt" : "no pan/tilt"}
                        </span>
                      </td>
                      <td className="hidden sm:table-cell">{d.model || "–"}</td>
                      <td className="hidden sm:table-cell">{d.panTilt ? "Yes" : "No"}</td>
                      <td className="w-px whitespace-nowrap">
                        <button
                          className="btn-ghost h-7 gap-1.5 px-2.5 text-xs"
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
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
        </>
      )}

      {err && <p className="mt-3 text-sm text-danger">{err}</p>}
    </div>
  );
}

function StatusPill({ st }: { st?: EufyStatus }) {
  if (!st) return null;
  const [text, tone] = (
    !st.configured
      ? ["Not set up", "off"]
      : st.connected
        ? ["Connected", "ok"]
        : st.needsCaptcha || st.needsVerifyCode
          ? ["Needs you", "warn"]
          : st.bridgeRunning
            ? ["Signing in…", "off"]
            : ["Offline", "bad"]
  ) as [string, "off" | "ok" | "warn" | "bad"];
  return <Pill tone={tone}>{text}</Pill>;
}
