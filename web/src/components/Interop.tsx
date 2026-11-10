import { useState } from "react";
import { Share2, Home } from "lucide-react";
import { Switch } from "./ui/Switch";
import { CopyIconButton, IntegrationHeader } from "./IntegrationParts";
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
    <div>
      <IntegrationHeader icon={Share2} title="Other systems">
        Let other software on your network use your cameras. Both are off by default and start
        the moment you switch them on.
      </IntegrationHeader>

      <div className="mt-3 divide-y divide-line border-y border-line">
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
            <p className="text-xs text-fg-faint">
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
            <div className="space-y-2">
              <p className="text-xs text-fg-faint">
                In the Home app choose <em>Add Accessory</em> → <em>More options</em> → Aperture, then
                enter this code:
              </p>
              <div className="flex items-center gap-1">
                <code className="rounded-md border border-line bg-ink-850 px-3 py-1.5 font-mono text-sm tracking-wider text-fg">
                  {formatPin(data.homekit.pin)}
                </code>
                <CopyIconButton
                  copied={copied}
                  label="Copy pairing code"
                  onClick={() => {
                    void navigator.clipboard?.writeText(formatPin(data.homekit.pin!));
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  }}
                />
              </div>
            </div>
          )}
        </Row>
      </div>

      {err && <p className="mt-3 text-sm text-danger">{err}</p>}
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
    <div className="flex items-start gap-4 py-4">
      <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-line bg-ink-800">
        <Icon className="h-[18px] w-[18px] text-fg-muted" strokeWidth={1.75} />
      </div>
      <div className="min-w-0 flex-1">
        <div className="flex flex-wrap items-center gap-2">
          <span className="text-sm font-semibold text-fg">{title}</span>
          {svc?.running && <span className="pill-green">Running</span>}
          {stalled && <span className="pill-red">Failed to start</span>}
          {svc && !svc.available && <span className="pill-gray">Unavailable</span>}
        </div>
        <p className="mt-1 max-w-[640px] text-sm leading-relaxed text-fg-muted">{desc}</p>
        {children && <div className="mt-3">{children}</div>}
      </div>
      <div className="pt-1">
        <Switch checked={on} onChange={onChange} disabled={busy || !svc?.available} label={title} />
      </div>
    </div>
  );
}
