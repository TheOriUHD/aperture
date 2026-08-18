import { useState } from "react";
import { Share2, Home, Copy, Check } from "lucide-react";
import { Switch } from "./ui/Switch";
import { useInterop, useSetInterop, type InteropService } from "../hooks/useSystem";

// ONVIF + HomeKit: two optional ways other systems consume Aperture. Both are
// started and stopped in place from here — no config file, no restart.
export function Interop() {
  const { data } = useInterop();
  const set = useSetInterop();
  const [err, setErr] = useState("");
  const [copied, setCopied] = useState(false);

  async function toggle(patch: { onvif?: boolean; homekit?: boolean }) {
    setErr("");
    try {
      await set.mutateAsync(patch);
    } catch (e) {
      setErr(e instanceof Error ? e.message : "Could not apply the change.");
    }
  }

  return (
    <div className="panel">
      <div className="flex items-center gap-2 mb-3">
        <Share2 className="h-4 w-4 text-fg-muted" />
        <h2 className="text-sm font-medium text-fg">Other systems</h2>
      </div>
      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Let other software on your network use your cameras. Both are off by default and start
        the moment you switch them on.
      </p>

      <div className="space-y-3">
        {/* ONVIF */}
        <Row
          icon={Share2}
          title="ONVIF"
          desc="Publishes your cameras to any recorder or viewer that speaks ONVIF (Profile S), so another system on your network can find and watch them."
          svc={data?.onvif}
          busy={set.isPending}
          onChange={(v) => toggle({ onvif: v })}
        >
          {data?.onvif.running && data.onvif.addr && (
            <p className="text-[11px] text-fg-faint">
              Listening on <code className="text-fg-muted">{data.onvif.addr}</code> — other systems
              discover it automatically on the LAN.
            </p>
          )}
        </Row>

        {/* HomeKit */}
        <Row
          icon={Home}
          title="Apple Home (HomeKit)"
          desc="Adds your cameras and an alarm switch to the Apple Home app, so you can view them on iPhone, iPad and Mac and arm or disarm from there."
          svc={data?.homekit}
          busy={set.isPending}
          onChange={(v) => toggle({ homekit: v })}
        >
          {data?.homekit.running && data.homekit.pin && (
            <div className="flex items-center gap-2">
              <p className="text-[11px] text-fg-faint">
                In the Home app choose <em>Add Accessory</em> → <em>More options</em> → Aperture, then
                enter this code:
              </p>
              <code className="rounded bg-ink-850 px-2 py-1 font-mono text-xs text-fg">
                {formatPin(data.homekit.pin)}
              </code>
              <button
                className="btn-icon h-7 w-7"
                aria-label="Copy pairing code"
                onClick={() => {
                  void navigator.clipboard?.writeText(formatPin(data.homekit.pin!));
                  setCopied(true);
                  setTimeout(() => setCopied(false), 1500);
                }}
              >
                {copied ? <Check className="h-3.5 w-3.5 text-online" /> : <Copy className="h-3.5 w-3.5" />}
              </button>
            </div>
          )}
        </Row>
      </div>

      {err && <p className="mt-3 text-xs text-rec">{err}</p>}
    </div>
  );
}

// HomeKit shows its 8-digit code as 123-45-678.
function formatPin(pin: string) {
  return pin.length === 8 ? `${pin.slice(0, 3)}-${pin.slice(3, 5)}-${pin.slice(5)}` : pin;
}

function Row({
  icon: Icon,
  title,
  desc,
  svc,
  busy,
  onChange,
  children,
}: {
  icon: typeof Share2;
  title: string;
  desc: string;
  svc?: InteropService;
  busy: boolean;
  onChange: (v: boolean) => void;
  children?: React.ReactNode;
}) {
  const on = !!svc?.enabled;
  // Requested-on but not actually up: say so plainly instead of showing green.
  const stalled = on && svc?.available && !svc.running;
  return (
    <div className="rounded-lg border border-line bg-ink-850 p-3">
      <div className="flex items-start gap-3">
        <div className="grid h-8 w-8 shrink-0 place-items-center rounded-lg bg-brand/12">
          <Icon className="h-4 w-4 text-brand" />
        </div>
        <div className="min-w-0 flex-1">
          <div className="flex items-center gap-2">
            <span className="text-sm font-medium text-fg">{title}</span>
            {svc?.running && <span className="pill pill-online">Running</span>}
            {stalled && <span className="pill bg-rec/15 text-rec">Failed to start</span>}
            {svc && !svc.available && <span className="pill bg-ink-700 text-fg-faint">Unavailable</span>}
          </div>
          <p className="mt-0.5 text-xs text-fg-faint leading-relaxed">{desc}</p>
        </div>
        <Switch checked={on} onChange={onChange} disabled={busy || !svc?.available} label={title} />
      </div>
      {children && <div className="mt-2.5 pl-11">{children}</div>}
    </div>
  );
}
