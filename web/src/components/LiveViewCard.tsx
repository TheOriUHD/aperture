import { useState } from "react";
import { Cast, Eye, EyeOff, Copy, Check, RefreshCw, AlertTriangle } from "lucide-react";
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
    <div className="panel">
      <div className="flex items-center gap-2 mb-3">
        <Cast className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">Live View Card</h2>
      </div>
      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Instant WebRTC live view on a Home Assistant dashboard. Drop the key and a camera&apos;s handle
        into the card config — no token to build, no cameras to pick. Add the{" "}
        <code className="text-fg-muted">aperture-live-card.js</code> resource to HA, then use the config
        below.
      </p>

      {isLoading ? (
        <p className="text-xs text-fg-faint">Loading…</p>
      ) : (
        <div className="space-y-5">
          {/* Managed live key */}
          <div>
            <label className="label">Live key</label>
            <div className="flex items-center gap-2">
              <code className="flex-1 truncate rounded-md bg-ink-850 border border-line px-3 py-2 font-mono text-xs text-fg">
                {revealed ? data?.key : MASK}
              </code>
              <button
                className="btn-icon h-9 w-9"
                onClick={() => setRevealed((v) => !v)}
                aria-label={revealed ? "Hide key" : "Reveal key"}
                title={revealed ? "Hide" : "Reveal"}
              >
                {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
              </button>
              <button
                className="btn-icon h-9 w-9"
                onClick={() => data && copy(data.key, "key")}
                aria-label="Copy key"
                title="Copy"
              >
                {copied === "key" ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
              </button>
            </div>

            <div className="mt-2 flex items-start gap-2 rounded-md border border-[#e6b45a]/30 bg-[#e6b45a]/10 p-2.5">
              <AlertTriangle className="h-3.5 w-3.5 text-[#e6b45a] mt-0.5 shrink-0" />
              <p className="text-[11px] text-fg-muted leading-relaxed">
                This one key lets the card watch <strong className="text-fg">any</strong> camera live, and it
                lives in the dashboard&apos;s card config — so anyone who can view that dashboard can read it.
                For a household that&apos;s usually fine; reroll to invalidate it everywhere at once.
              </p>
            </div>

            <div className="mt-2">
              {confirming ? (
                <div className="flex items-center gap-2">
                  <span className="text-[11px] text-fg-muted">Reroll now? Existing cards stop working until updated.</span>
                  <button
                    className="btn-danger h-7 text-xs"
                    disabled={roll.isPending}
                    onClick={async () => {
                      await roll.mutateAsync();
                      setConfirming(false);
                      setRevealed(true);
                    }}
                  >
                    {roll.isPending ? "Rerolling…" : "Reroll"}
                  </button>
                  <button className="btn-ghost h-7 text-xs" onClick={() => setConfirming(false)}>
                    Cancel
                  </button>
                </div>
              ) : (
                <button className="inline-flex items-center gap-1.5 text-xs text-fg-muted hover:text-fg" onClick={() => setConfirming(true)}>
                  <RefreshCw className="h-3.5 w-3.5" /> Reroll key
                </button>
              )}
            </div>
          </div>

          {/* Camera slugs */}
          {cams.length > 0 && (
            <div>
              <label className="label">Cameras</label>
              <p className="text-[11px] text-fg-faint mb-2">
                Use a camera&apos;s handle as the <code className="text-fg-muted">camera:</code> value.
              </p>
              <div className="space-y-1">
                {cams.map((c) => (
                  <div key={c.id} className="flex items-center gap-2 text-xs">
                    <span className="w-32 truncate text-fg">{c.name}</span>
                    <code className="flex-1 truncate rounded bg-ink-850 px-2 py-1 font-mono text-fg-muted">{c.slug}</code>
                    <button
                      className="btn-icon h-7 w-7"
                      onClick={() => copy(c.slug, "slug-" + c.id)}
                      aria-label={`Copy ${c.name} handle`}
                    >
                      {copied === "slug-" + c.id ? (
                        <Check className="h-3.5 w-3.5 text-online" />
                      ) : (
                        <Copy className="h-3.5 w-3.5" />
                      )}
                    </button>
                  </div>
                ))}
              </div>
            </div>
          )}

          {/* Ready-to-paste config */}
          <div>
            <div className="flex items-center justify-between mb-1.5">
              <label className="label mb-0">Card config</label>
              <button
                className="inline-flex items-center gap-1.5 text-xs text-brand hover:underline"
                onClick={() => data && copy(configYaml(data.key), "yaml")}
              >
                {copied === "yaml" ? <Check className="h-3.5 w-3.5 text-online" /> : <Copy className="h-3.5 w-3.5" />}
                Copy config
              </button>
            </div>
            <pre className="overflow-x-auto rounded-md bg-ink-850 border border-line p-3 font-mono text-[11px] leading-relaxed text-fg-muted">
              {configYaml(revealed ? (data?.key ?? "") : MASK)}
            </pre>
            <p className="text-[11px] text-fg-faint mt-1.5">
              Copy config puts the real key on your clipboard even while it&apos;s hidden here.
            </p>
          </div>
        </div>
      )}
    </div>
  );
}
