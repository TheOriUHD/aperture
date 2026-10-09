// A readable name for a signed-in browser, from its User-Agent: "Chrome on
// Windows", "Safari on iPhone". Only for recognising your own devices in a list,
// so it stays deliberately coarse.

export type DeviceKind = "phone" | "tablet" | "desktop";

export interface DeviceInfo {
  label: string;
  kind: DeviceKind;
}

export function describeDevice(ua: string): DeviceInfo {
  const os = /iPhone/.test(ua)
    ? "iPhone"
    : /iPad/.test(ua)
      ? "iPad"
      : /Android/.test(ua)
        ? "Android"
        : /Windows/.test(ua)
          ? "Windows"
          : /Macintosh|Mac OS X/.test(ua)
            ? "Mac"
            : /CrOS/.test(ua)
              ? "ChromeOS"
              : /Linux/.test(ua)
                ? "Linux"
                : "";
  const ios = os === "iPhone" || os === "iPad";
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /OPR\//.test(ua)
      ? "Opera"
      : /Firefox\/|FxiOS\//.test(ua)
        ? "Firefox"
        : /Chrome\/|CriOS\//.test(ua)
          ? "Chrome"
          : /Safari\//.test(ua)
            ? "Safari"
            : ios
              ? "Home Screen app" // an iOS web app launched from the home screen has no Safari token
              : "Browser";
  const kind: DeviceKind =
    os === "iPhone" || (os === "Android" && /Mobile/.test(ua)) ? "phone" : os === "iPad" || os === "Android" ? "tablet" : "desktop";
  return { label: os ? `${browser} on ${os}` : browser, kind };
}
