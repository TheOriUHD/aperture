import { useState } from "react";
import { Cast, Eye, EyeOff, Copy, Check, RefreshCw, AlertTriangle } from "lucide-react";
import { SettingsRow } from "./ui/Layout";
import { Spinner } from "./ui/Spinner";
import { Callout, CodeValue, CopyIconButton, IntegrationHeader, SectionTitle } from "./IntegrationParts";
import { useLiveKey, useRollLiveKey, type LiveKeyCamera } from "../hooks/useSystem";

const MASK = "aplv_" + "•".repeat(24);

// LiveViewCard is the easy path for the Home Assistant Live Card: a single managed key
// (reveal / copy / reroll — no scoped-token building, no camera picking) and every
// camera's copy-paste slug, plus a ready-to-paste card config. The key authorizes live
// view of any camera; the card names which camera by its slug.
export function LiveViewCard() {
  const { data, isLoading } = useLiveKey();
  const roll = useRollLiveKey();
  const [revealed, setRevealed] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [copied, setCopied] = useState("");

  async function copy(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* ignore */
    }
  }

  const cams: LiveKeyCamera[] = data?.cameras ?? [];
  const example = cams[0];
  const configYaml = (key: string) =>
    [
      "type: custom:aperture-live-card",
      `url: ${data?.apertureUrl || "https://aperture.example.com"}`,
      `token: ${key}`,
      `camera: ${example ? example.slug : "zimmer_ph"}`,
      `name: ${example ? example.name : "Zimmer"}`,
    ].join("\n");

  return (
    <div>
      <IntegrationHeader icon={Cast} title="Live View Card">
        Instant WebRTC live view on a Home Assistant dashboard. Drop the key and a camera&apos;s handle
        into the card config — no token to build, no cameras to pick. Add the{" "}
        <code className="text-fg">aperture-live-card.js</code> resource to HA, then use the config
        below.
      </IntegrationHeader>

      {isLoading ? (
        <div className="py-6">
          <Spinner label="Loading…" />
        </div>
      ) : (
        <>
          {/* Managed live key */}
          <div className="divide-y divide-line">
            <SettingsRow label="Live key" align="start">
              <div className="max-w-xl space-y-2.5">
                <div className="flex items-center gap-1">
                  <CodeValue className="flex-1 text-fg">{revealed ? data?.key : MASK}</CodeValue>
                  <button
                    className="btn-icon"
                    onClick={() => setRevealed((v) => !v)}
                    aria-label={revealed ? "Hide key" : "Reveal key"}
                    title={revealed ? "Hide" : "Reveal"}
                  >
                    {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
                  </button>
                  <button
                    className="btn-icon"
                    onClick={() => data && copy(data.key, "key")}
                    aria-label="Copy key"
                    title="Copy"
                  >
                    {copied === "key" ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
                  </button>
                </div>

                <Callout tone="warn" icon={AlertTriangle}>
                  This one key lets the card watch <strong className="text-fg">any</strong> camera live, and it
                  lives in the dashboard&apos;s card config — so anyone who can view that dashboard can read it.
                  For a household that&apos;s usually fine; reroll to invalidate it everywhere at once.
                </Callout>

                {confirming ? (
                  <div className="flex flex-wrap items-center gap-2">
                    <span className="text-xs text-fg-muted">Reroll now? Existing cards stop working until updated.</span>
                    <button
                      className="btn-danger h-7 px-3 text-xs"
                      disabled={roll.isPending}
                      onClick={async () => {
                        await roll.mutateAsync();
                        setConfirming(false);
                        setRevealed(true);
                      }}
                    >
                      {roll.isPending ? "Rerolling…" : "Reroll"}
                    </button>
                    <button className="btn-ghost h-7 px-3 text-xs" onClick={() => setConfirming(false)}>
                      Cancel
                    </button>
                  </div>
                ) : (
                  <button className="btn-link text-xs" onClick={() => setConfirming(true)}>
                    <RefreshCw className="h-3.5 w-3.5" /> Reroll key
                  </button>
                )}
              </div>
            </SettingsRow>
          </div>

          {/* Camera slugs */}
          {cams.length > 0 && (
            <>
              <SectionTitle
                description={
                  <>
                    Use a camera&apos;s handle as the <code className="text-fg-muted">camera:</code> value.
                  </>
                }
              >
                Cameras
              </SectionTitle>
              <div className="overflow-x-auto">
                <table className="tbl table-fixed">
                  <thead>
                    <tr>
                      <th className="w-40 sm:w-56">Camera</th>
                      <th>Handle</th>
                      <th className="w-12" aria-label="Copy" />
                    </tr>
                  </thead>
                  <tbody>
                    {cams.map((c) => (
                      <tr key={c.id}>
                        <td className="truncate">
                          <span className="text-fg">{c.name}</span>
                        </td>
                        <td className="truncate font-mono text-xs">{c.slug}</td>
                        <td className="!px-1 text-right">
                          <CopyIconButton
                            copied={copied === "slug-" + c.id}
                            onClick={() => copy(c.slug, "slug-" + c.id)}
                            label={`Copy ${c.name} handle`}
                          />
                        </td>
                      </tr>
                    ))}
                  </tbody>
                </table>
              </div>
            </>
          )}

          {/* Ready-to-paste config */}
          <SectionTitle
            description="Copy config puts the real key on your clipboard even while it's hidden here."
            action={
              <button className="btn-outline" onClick={() => data && copy(configYaml(data.key), "yaml")}>
                {copied === "yaml" ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
                Copy config
              </button>
            }
          >
            Card config
          </SectionTitle>
          <pre className="overflow-x-auto rounded-lg border border-line bg-ink-850 p-4 font-mono text-xs leading-relaxed text-fg/85">
            {configYaml(revealed ? (data?.key ?? "") : MASK)}
          </pre>
        </>
      )}
    </div>
  );
}
