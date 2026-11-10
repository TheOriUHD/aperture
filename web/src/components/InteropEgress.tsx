import { Cable, Radio, Home } from "lucide-react";
import { IntegrationHeader } from "./IntegrationParts";

// ONVIF + HomeKit egress are advanced, off-by-default, env-gated features (they
// open extra listeners), so this panel documents rather than toggles them.
export function InteropEgress() {
  return (
    <div>
      <IntegrationHeader icon={Cable} title="Interoperability (advanced)">
        Extra listeners that stay off unless enabled with an environment variable.
      </IntegrationHeader>

      <div className="mt-3 divide-y divide-line border-y border-line">
        <div className="flex items-start gap-4 py-4">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-line bg-ink-800">
            <Radio className="h-[18px] w-[18px] text-fg-muted" strokeWidth={1.75} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-semibold text-fg">ONVIF Profile S</span>
              <span className="pill-gray">off by default</span>
            </div>
            <p className="mt-1 max-w-[680px] text-sm leading-relaxed text-fg-muted">
              Expose your cameras to other NVRs/VMSes (Frigate, Synology, Blue Iris, Milestone). Enable with{" "}
              <code className="text-fg">APERTURE_ONVIF_ENABLED=true</code>; the device service listens on{" "}
              <code className="text-fg">:10101</code> (<code className="text-fg">/onvif/device_service</code>).
              Streams resolve to go2rtc. <span className="text-warn">Unauthenticated on the LAN — trusted VLANs only.</span>
            </p>
          </div>
        </div>

        <div className="flex items-start gap-4 py-4">
          <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-line bg-ink-800">
            <Home className="h-[18px] w-[18px] text-fg-muted" strokeWidth={1.75} />
          </div>
          <div className="min-w-0 flex-1">
            <div className="flex flex-wrap items-center gap-2">
              <span className="text-sm font-semibold text-fg">Apple HomeKit</span>
              <span className="pill-gray">off by default</span>
            </div>
            <p className="mt-1 max-w-[680px] text-sm leading-relaxed text-fg-muted">
              Show cameras as HomeKit motion sensors + an arm/disarm security accessory. Enable with{" "}
              <code className="text-fg">APERTURE_HOMEKIT_ENABLED=true</code>, then pair in the Home app with pin{" "}
              <code className="text-fg">123-45-678</code>. <span className="text-warn">HKSV video recording is not implemented</span>{" "}
              — motion + arm only.
            </p>
          </div>
        </div>
      </div>
    </div>
  );
}
