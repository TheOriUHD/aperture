import { Radio, Home } from "lucide-react";

// ONVIF + HomeKit egress are advanced, off-by-default, env-gated features (they
// open extra listeners), so this panel documents rather than toggles them.
export function InteropEgress() {
  return (
    <div className="panel space-y-4">
      <h2 className="text-sm font-medium text-fg">Interoperability (advanced)</h2>

      <div className="space-y-1.5">
        <div className="flex items-center gap-2">
          <Radio className="h-4 w-4 text-fg-muted" />
          <span className="text-sm text-fg">ONVIF Profile S</span>
          <span className="ml-auto pill-offline">off by default</span>
        </div>
        <p className="text-xs text-fg-muted leading-relaxed">
          Expose your cameras to other NVRs/VMSes (Frigate, Synology, Blue Iris, Milestone). Enable with{" "}
          <code className="text-fg-faint">APERTURE_ONVIF_ENABLED=true</code>; the device service listens on{" "}
          <code className="text-fg-faint">:10101</code> (<code className="text-fg-faint">/onvif/device_service</code>).
          Streams resolve to go2rtc. <span className="text-amber">Unauthenticated on the LAN — trusted VLANs only.</span>
        </p>
      </div>

      <div className="space-y-1.5 border-t border-line pt-3">
        <div className="flex items-center gap-2">
          <Home className="h-4 w-4 text-fg-muted" />
          <span className="text-sm text-fg">Apple HomeKit</span>
          <span className="ml-auto pill-offline">off by default</span>
        </div>
        <p className="text-xs text-fg-muted leading-relaxed">
          Show cameras as HomeKit motion sensors + an arm/disarm security accessory. Enable with{" "}
          <code className="text-fg-faint">APERTURE_HOMEKIT_ENABLED=true</code>, then pair in the Home app with pin{" "}
          <code className="text-fg-faint">123-45-678</code>. <span className="text-amber">HKSV video recording is not implemented</span>{" "}
          — motion + arm only.
        </p>
      </div>
    </div>
  );
}
