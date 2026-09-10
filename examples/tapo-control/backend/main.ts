// Tapo Control — backend.
//
// Tapo cameras speak ONVIF on port 2020 once a "camera account" exists (the same
// account the RTSP stream uses). That is a plain HTTP+SOAP interface on the LAN, so
// unlike Eufy there is no cloud and no vendor bridge to run — this process talks to
// the cameras directly.
//
// It still exists as a backend rather than living in the panel, for two reasons:
//   1. the camera account password must never reach the browser, and a backend is
//      the only place an extension can hold a secret in the clear;
//   2. ONVIF authentication is a WS-Security digest over a per-request nonce, and a
//      panel under `connect-src 'none'` cannot make the calls anyway.
//
// The panels reach it over a loopback HTTP endpoint declared in the manifest like
// any other network host.

import { aperture } from "@aperture/sdk";

// Fixed, because both Deno's --allow-net and the host's egress check are decided
// before this code runs. 8788 rather than 8787 so it can coexist with Eufy Control.
const CONTROL_PORT = 8788;

const SOAP_TIMEOUT_MS = 8_000;

type Row = { camera: string; address: string };

type Cam = {
  camera: string;          // the Aperture camera id
  origin: string;          // http://ip:2020 — the ONLY origin we ever contact
  ptzPath: string;         // path of the PTZ service, per GetCapabilities
  profile: string;         // media profile token the PTZ config is attached to
  model: string;
  panTilt: boolean;        // ContinuousMove on the pan/tilt axes
  absolute: boolean;       // AbsoluteMove — what backs "centre"
  error: string;
};

const cams = new Map<string, Cam>();
let username = "";
let password = "";
let stepMs = 400;
let speed = 0.6;

// ---------------------------------------------------------------- SOAP plumbing

// ONVIF WS-Security UsernameToken: Base64(SHA1(nonce + created + password)). The
// nonce is per request, so the digest cannot be replayed.
async function wsse(): Promise<string> {
  const nonce = crypto.getRandomValues(new Uint8Array(16));
  const created = new Date().toISOString();
  const bytes = new Uint8Array([
    ...nonce,
    ...new TextEncoder().encode(created + password),
  ]);
  const hash = new Uint8Array(await crypto.subtle.digest("SHA-1", bytes));
  const b64 = (b: Uint8Array) => btoa(String.fromCharCode(...b));
  return `<s:Header><Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">` +
    `<UsernameToken><Username>${xml(username)}</Username>` +
    `<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">${b64(hash)}</Password>` +
    `<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">${b64(nonce)}</Nonce>` +
    `<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-utility-1.0.xsd">${created}</Created>` +
    `</UsernameToken></Security></s:Header>`;
}

function xml(s: string): string {
  return s.replace(/[<>&"']/g, (c) =>
    ({ "<": "&lt;", ">": "&gt;", "&": "&amp;", '"': "&quot;", "'": "&apos;" }[c]!));
}

async function soap(url: string, body: string): Promise<string> {
  const env = `<?xml version="1.0" encoding="UTF-8"?>` +
    `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope">${await wsse()}<s:Body>${body}</s:Body></s:Envelope>`;
  const res = await fetch(url, {
    method: "POST",
    headers: { "content-type": "application/soap+xml; charset=utf-8" },
    body: env,
    signal: AbortSignal.timeout(SOAP_TIMEOUT_MS),
  });
  const text = await res.text();
  if (!res.ok) {
    // A SOAP Fault carries the real reason (bad credentials, most often) in Text.
    const reason = pick(text, "Text") ?? `HTTP ${res.status}`;
    throw new Error(reason);
  }
  return text;
}

// ONVIF replies are small and their shape is fixed, so a tag scrape is enough and
// avoids pulling an XML parser into the sandbox. Namespace prefixes vary by vendor,
// hence the optional prefix in each pattern.
function pick(xmlText: string, tag: string): string | null {
  const m = xmlText.match(new RegExp(`<(?:\\w+:)?${tag}[^>]*>([^<]*)</(?:\\w+:)?${tag}>`));
  return m ? m[1] : null;
}
function pickAttr(xmlText: string, tag: string, attr: string): string | null {
  const m = xmlText.match(new RegExp(`<(?:\\w+:)?${tag}[^>]*\\b${attr}="([^"]+)"`));
  return m ? m[1] : null;
}
function serviceIn(xmlText: string, service: string): string | null {
  const m = xmlText.match(
    new RegExp(`<(?:\\w+:)?${service}>[\\s\\S]*?<(?:\\w+:)?XAddr>([^<]*)</(?:\\w+:)?XAddr>`),
  );
  return m ? m[1] : null;
}

// A camera reports its own service URLs, and a camera that is lying (or simply
// misconfigured behind NAT) would point us at some other host. Keep only the PATH it
// reports and always dial the origin the ADMIN configured, so egress can never leave
// the declared allow-list. Deno would refuse the connection anyway; this turns a
// confusing permission error into correct behaviour.
function pathOf(xaddr: string | null, fallback: string): string {
  if (!xaddr) return fallback;
  try {
    return new URL(xaddr).pathname || fallback;
  } catch {
    return fallback;
  }
}

// ---------------------------------------------------------------- capability probe

async function probe(row: Row): Promise<Cam> {
  const origin = new URL(row.address).origin;
  const cam: Cam = {
    camera: row.camera, origin, ptzPath: "/onvif/service", profile: "",
    model: "", panTilt: false, absolute: false, error: "",
  };
  const dev = origin + "/onvif/device_service";
  try {
    const info = await soap(dev, `<GetDeviceInformation xmlns="http://www.onvif.org/ver10/device/wsdl"/>`);
    cam.model = [pick(info, "Manufacturer"), pick(info, "Model")].filter(Boolean).join(" ").trim();

    const caps = await soap(dev, `<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>All</Category></GetCapabilities>`);
    const ptzX = serviceIn(caps, "PTZ");
    const mediaX = serviceIn(caps, "Media");
    if (!ptzX || !mediaX) {
      // A fixed camera (a Tapo C100, say) advertises no PTZ service at all. That is
      // a normal answer, not a failure: the panel simply renders nothing for it.
      return cam;
    }
    cam.ptzPath = pathOf(ptzX, "/onvif/service");
    const mediaURL = origin + pathOf(mediaX, "/onvif/service");

    const profs = await soap(mediaURL, `<GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/>`);
    cam.profile = pickAttr(profs, "Profiles", "token") ?? "";
    if (!cam.profile) return cam;

    const nodes = await soap(origin + cam.ptzPath, `<GetNodes xmlns="http://www.onvif.org/ver20/ptz/wsdl"/>`);
    cam.panTilt = /ContinuousPanTiltVelocitySpace/.test(nodes);
    cam.absolute = /AbsolutePanTiltPositionSpace/.test(nodes);
  } catch (err) {
    cam.error = String((err as Error).message ?? err);
  }
  return cam;
}

async function refresh(): Promise<void> {
  const rows = ((await aperture.storage.get("cameras")) ?? "") as unknown;
  let list: Row[] = [];
  try {
    // Group settings reach a backend as the stored JSON text.
    list = typeof rows === "string" ? JSON.parse(rows || "[]") : (rows as Row[]) ?? [];
  } catch {
    list = [];
  }
  cams.clear();
  for (const row of list) {
    if (!row?.camera || !row?.address) continue;
    const cam = await probe(row);
    cams.set(row.camera, cam);
    console.log(
      `tapo-control: ${row.camera} -> ${cam.origin} ${cam.model || "(unknown model)"}` +
        ` panTilt=${cam.panTilt}${cam.error ? " error=" + cam.error : ""}`,
    );
  }
}

// ---------------------------------------------------------------- PTZ commands

const VECTOR: Record<string, [number, number]> = {
  // ONVIF pan is +x right, tilt is +y up.
  left: [-1, 0], right: [1, 0], up: [0, 1], down: [0, -1],
};

// speedPct and stepMs are per-request overrides from the jog widget, so the operator
// can change how fast and how far a nudge moves without opening settings. Both are
// clamped here rather than trusted: the panel is sandboxed, but it is still input.
async function move(cam: Cam, dir: string, speedPct?: number, stepOverride?: number): Promise<void> {
  const v = VECTOR[dir];
  if (!v) throw new Error("bad direction");
  const url = cam.origin + cam.ptzPath;
  const vel = Number.isFinite(speedPct!) && speedPct! > 0
    ? Math.max(0.05, Math.min(speedPct! / 100, 1))
    : speed;
  const dwell = Number.isFinite(stepOverride!) && stepOverride! > 0
    ? Math.max(50, Math.min(stepOverride!, 5_000))
    : stepMs;
  const [x, y] = [v[0] * vel, v[1] * vel];
  await soap(url,
    `<ContinuousMove xmlns="http://www.onvif.org/ver20/ptz/wsdl">` +
      `<ProfileToken>${xml(cam.profile)}</ProfileToken>` +
      `<Velocity><PanTilt x="${x}" y="${y}" xmlns="http://www.onvif.org/ver10/schema"/></Velocity>` +
      `</ContinuousMove>`);
  // ONVIF moves until stopped, so the "step" is how long we wait before stopping.
  // Stop even if the wait is interrupted, or the camera keeps panning to its limit.
  try {
    await new Promise((r) => setTimeout(r, dwell));
  } finally {
    await stop(cam);
  }
}


async function stop(cam: Cam): Promise<void> {
  await soap(cam.origin + cam.ptzPath,
    `<Stop xmlns="http://www.onvif.org/ver20/ptz/wsdl">` +
      `<ProfileToken>${xml(cam.profile)}</ProfileToken><PanTilt>true</PanTilt><Zoom>true</Zoom></Stop>`);
}

// Tapo reports HomeSupported=false, so "centre" is an absolute move to the middle of
// the normalised position space rather than an ONVIF GotoHomePosition.
async function centre(cam: Cam): Promise<void> {
  await soap(cam.origin + cam.ptzPath,
    `<AbsoluteMove xmlns="http://www.onvif.org/ver20/ptz/wsdl">` +
      `<ProfileToken>${xml(cam.profile)}</ProfileToken>` +
      `<Position><PanTilt x="0" y="0" xmlns="http://www.onvif.org/ver10/schema"/></Position>` +
      `</AbsoluteMove>`);
}

// ---------------------------------------------------------------- control API

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), { status, headers: { "content-type": "application/json" } });
}

async function handle(req: Request): Promise<Response> {
  const url = new URL(req.url);

  if (url.pathname === "/status") {
    const id = url.searchParams.get("camera") ?? "";
    const cam = id ? cams.get(id) : undefined;
    return json({
      configured: cams.size > 0,
      cameras: [...cams.values()].map((c) => ({ camera: c.camera, model: c.model, error: c.error })),
      mapped: !!cam,
      model: cam?.model ?? "",
      lastError: cam?.error ?? "",
      // What THIS camera can do, so a panel renders only controls that exist.
      capabilities: cam ? { panTilt: cam.panTilt, centre: cam.absolute } : null,
    });
  }

  if (req.method !== "POST") return json({ error: "method not allowed" }, 405);
  const body = (await req.json().catch(() => ({}))) as Record<string, unknown>;

  if (url.pathname === "/rescan") {
    await refresh();
    return json({ ok: true, cameras: cams.size });
  }

  const cam = cams.get(String(body.camera ?? ""));
  if (!cam) return json({ error: "camera not mapped" }, 404);
  if (cam.error) return json({ error: cam.error }, 502);

  try {
    switch (url.pathname) {
      case "/ptz":
        if (!cam.panTilt) return json({ error: "this camera has no pan/tilt" }, 400);
        await move(cam, String(body.direction ?? ""), Number(body.speed), Number(body.step));
        return json({ ok: true });
      case "/centre":
        if (!cam.absolute) return json({ error: "this camera cannot recentre" }, 400);
        await centre(cam);
        return json({ ok: true });
      case "/stop":
        await stop(cam);
        return json({ ok: true });
      default:
        return json({ error: "not found" }, 404);
    }
  } catch (err) {
    return json({ error: String((err as Error).message ?? err) }, 502);
  }
}

// ---------------------------------------------------------------- start

username = String((await aperture.storage.get("username")) ?? "");
password = String((await aperture.storage.get("password")) ?? "");
stepMs = Math.max(50, Math.min(Number(await aperture.storage.get("step_ms")) || 400, 5_000));
const pct = Number(await aperture.storage.get("speed"));
speed = Math.max(0.05, Math.min((Number.isFinite(pct) && pct > 0 ? pct : 60) / 100, 1));

if (!username || !password) {
  // Installed but not configured yet. Serve status so the panels can say exactly
  // that, and pick the credentials up when they are saved (which restarts this).
  console.log("tapo-control: no camera account configured yet");
} else {
  await refresh();
}

Deno.serve({ port: CONTROL_PORT, hostname: "127.0.0.1" }, handle);
console.log(`tapo-control: control API on 127.0.0.1:${CONTROL_PORT}`);
