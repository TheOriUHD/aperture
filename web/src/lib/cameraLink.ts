// Camera links reach the browser with their secret masked: the server replaces the
// username and password, or a UniFi Protect stream key, with "•••••" and never
// sends the real values (see internal/urlcred). Sending a link back with the dots still in place means "keep
// the saved login", which the server honours only for the address the login was
// saved for. These helpers let the editor explain that before a save is refused.

// hasLoginMask reports whether a link still carries the placeholder.
export function hasLoginMask(link: string): boolean {
  return link.includes("•");
}

// linkHost returns a link's host, lower-cased, without port or IPv6 brackets. It
// splits the link the way the server does: the address ends at the first "/", "?"
// or "#", and the login at its last "@".
export function linkHost(link: string): string {
  const i = link.indexOf("://");
  if (i <= 0) return "";
  const after = link.slice(i + 3);
  const end = after.search(/[/?#]/);
  const authority = end >= 0 ? after.slice(0, end) : after;
  let host = authority.slice(authority.lastIndexOf("@") + 1);
  if (host.startsWith("[")) {
    const k = host.indexOf("]");
    if (k > 0) return host.slice(1, k).toLowerCase();
  }
  const c = host.lastIndexOf(":");
  if (c >= 0) host = host.slice(0, c);
  return host.toLowerCase();
}

// isProtectLink reports a link UniFi Protect hands out: the console serves every
// camera on port 7441 (RTSPS) or 7447 (RTSP), at a path that is the stream key.
export function isProtectLink(link: string): boolean {
  return /^rtsp[sx]?:\/\/[^/?#]+:(7441|7447)\/[A-Za-z0-9]{16,}([?#].*)?$/i.test(link.trim());
}

// keyMasked reports a link whose hidden secret is a stream key, not a login.
export function keyMasked(link: string): boolean {
  const i = link.indexOf("://");
  const after = i >= 0 ? link.slice(i + 3) : link;
  const end = after.search(/[/?#]/);
  const authority = end >= 0 ? after.slice(0, end) : after;
  return hasLoginMask(link) && !hasLoginMask(authority);
}

// maskedLoginProblem explains why a link that still carries the placeholder
// cannot be saved, or returns "" when it can. `saved` are the camera's links as
// the server showed them: one of them carrying the placeholder means a login is
// saved for its host, and only that host may have it filled back in.
export function maskedLoginProblem(link: string, saved: string[]): string {
  if (!hasLoginMask(link)) return "";
  const host = linkHost(link);
  const withLogin = saved.filter(hasLoginMask);
  if (withLogin.some((s) => linkHost(s) === host)) return "";
  if (keyMasked(link)) {
    return withLogin.length > 0
      ? "New address: paste the full stream link again. The saved stream key is only used for the address it was saved for."
      : "Paste the full stream link.";
  }
  if (withLogin.length > 0) {
    return "New address: enter the camera's username and password again. The saved login is only sent to the address it was saved for.";
  }
  return "Replace the dots with the camera's username and password.";
}
