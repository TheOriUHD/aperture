// Eufy Control — backend.
//
// Eufy publishes no local device API. Control goes through eufy-security-ws, which
// signs in to the Eufy cloud and reaches the HomeBase over P2P. That server speaks
// WebSocket, and a sandboxed UI panel cannot open one: panels run under
// `connect-src 'none'` and their only egress is host-mediated HTTP. So the socket
// lives here, in the backend, and the panels reach it over a loopback HTTP endpoint
// that is declared in the manifest like any other network host.
//
// This process therefore does two things:
//   1. keeps one WebSocket to eufy-security-ws, negotiating the schema and tracking
//      which devices exist and what they can do;
//   2. serves a tiny control API on 127.0.0.1 for this extension's own panels.
//
// It deliberately holds no Eufy credentials. Those live in eufy-security-ws.

import { aperture } from "@aperture/sdk";

// The loopback port the panels talk to. Declared statically in the manifest's
// network permissions, so both Deno's --allow-net and the host's egress check
// accept it. Fixed rather than random because both of those are decided before
// this code runs.
const CONTROL_PORT = 8787;

// eufy-security-ws speaks a versioned schema. Pan/tilt arrived in schema 3 and
// device.calibrate in 10; asking for a schema the server predates makes it reply
// with its own maximum, which we then adopt.
const WANT_SCHEMA = 13;

// PanTiltDirection, from eufy-security-client's p2p types. These are wire values —
// do not renumber them.
const DIRECTION = { rotate360: 0, left: 1, right: 2, up: 3, down: 4 } as const;
type DirectionName = keyof typeof DIRECTION;

// device.get_commands reports BARE command names ("pan_and_tilt"), while invoking
// one uses the prefixed form ("device.pan_and_tilt"). Accept either so a capability
// check cannot silently report every device as incapable.
function hasCmd(dev: DeviceInfo, name: string): boolean {
  return dev.commands.includes(name) || dev.commands.includes("device." + name);
}

interface DeviceInfo {
  serialNumber: string;
  name?: string;
  commands: string[];
}

let socket: WebSocket | null = null;
let schema = WANT_SCHEMA;
let connected = false;
let lastError = "";
let nextMessageId = 1;
const devices = new Map<string, DeviceInfo>();
const pending = new Map<string, { resolve: (v: unknown) => void; reject: (e: Error) => void }>();

function send(command: string, extra: Record<string, unknown> = {}): Promise<unknown> {
  const ws = socket;
  if (!ws || ws.readyState !== WebSocket.OPEN) {
    return Promise.reject(new Error("not connected to eufy-security-ws"));
  }
  const messageId = `ap-${nextMessageId++}`;
  ws.send(JSON.stringify({ messageId, command, ...extra }));
  return new Promise((resolve, reject) => {
    pending.set(messageId, { resolve, reject });
    // A command that never comes back must not leak its entry, or a long-running
    // process slowly fills this map with dead promises.
    setTimeout(() => {
      if (pending.delete(messageId)) reject(new Error(`timed out: ${command}`));
    }, 10_000);
  });
}

function onMessage(raw: string) {
  let msg: Record<string, unknown>;
  try {
    msg = JSON.parse(raw);
  } catch {
    return;
  }

  if (msg.type === "version") {
    // The server states the schema range it supports; take the highest we both know.
    const max = Number(msg.maxSchemaVersion ?? 0);
    if (max > 0 && max < schema) schema = max;
    return;
  }

  if (msg.type === "result") {
    const id = String(msg.messageId ?? "");
    const p = pending.get(id);
    if (!p) return;
    pending.delete(id);
    if (msg.success) p.resolve(msg.result);
    else p.reject(new Error(String((msg.errorCode as string) ?? "command failed")));
    return;
  }

  if (msg.type === "event") {
    const ev = (msg.event ?? {}) as Record<string, unknown>;
    // Device list changes as the HomeBase reports in; keep our capability map fresh.
    if (ev.source === "driver" && ev.event === "connected") connected = true;
    if (ev.source === "driver" && ev.event === "disconnected") connected = false;
  }
}

async function refreshDevices() {
  const res = (await send("start_listening")) as { state?: Record<string, unknown> } | undefined;
  const state = (res?.state ?? {}) as Record<string, unknown>;
  // state.devices is a list of SERIAL NUMBER STRINGS, not device objects. Decoding it
  // as objects yields a list of empty devices, which reads as "signed in but nothing
  // found" — the details have to be fetched per serial.
  const serials = (state.devices ?? []) as string[];
  devices.clear();
  for (const serial of serials) {
    if (!serial) continue;
    let name = serial;
    try {
      const props = (await send("device.get_properties", { serialNumber: serial })) as
        | { properties?: { name?: string } }
        | undefined;
      if (props?.properties?.name) name = props.properties.name;
    } catch { /* fall back to the serial */ }
    let commands: string[] = [];
    try {
      // Capability discovery, rather than guessing from the model name: the jog
      // wheel should appear only where the device actually pans.
      const r = (await send("device.get_commands", { serialNumber: serial })) as
        | { commands?: string[] }
        | undefined;
      commands = r?.commands ?? [];
    } catch {
      commands = [];
    }
    devices.set(serial, { serialNumber: serial, name, commands });
  }
}

function connect(url: string) {
  try {
    socket = new WebSocket(url);
  } catch (err) {
    lastError = String(err);
    return;
  }
  socket.onopen = async () => {
    lastError = "";
    try {
      await send("set_api_schema", { schemaVersion: schema });
      await refreshDevices();
      connected = true;
      console.log(`eufy-control: connected, ${devices.size} device(s)`);
    } catch (err) {
      lastError = String(err);
    }
  };
  socket.onmessage = (e) => onMessage(String(e.data));
  socket.onerror = () => {
    lastError = "websocket error";
  };
  socket.onclose = () => {
    connected = false;
    socket = null;
    // Reconnect steadily. The supervisor restarts this process only if it exits,
    // and exiting on a transient network blip would be the wrong response.
    setTimeout(() => connect(url), 5_000);
  };
}

function json(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "content-type": "application/json" },
  });
}

// The control API the panels call. Loopback only — Deno's --allow-net is scoped to
// this exact host:port, and the host's egress check independently requires the same
// address to be a declared network permission.
async function handle(req: Request): Promise<Response> {
  const url = new URL(req.url);
  if (url.pathname === "/status") {
    const serial = url.searchParams.get("serial") ?? "";
    const dev = serial ? devices.get(serial) : undefined;
    return json({
      connected,
      lastError,
      schema,
      devices: [...devices.values()].map((d) => ({ serial: d.serialNumber, name: d.name })),
      // What this specific device can do, so a panel renders only real controls.
      capabilities: dev
        ? {
            panTilt: hasCmd(dev, "pan_and_tilt"),
            calibrate: hasCmd(dev, "calibrate"),
            alarm: hasCmd(dev, "trigger_alarm_sound"),
          }
        : null,
    });
  }

  if (req.method !== "POST") return json({ error: "method not allowed" }, 405);
  const body = (await req.json().catch(() => ({}))) as Record<string, unknown>;
  const serial = String(body.serial ?? "");
  if (!serial || !devices.has(serial)) return json({ error: "unknown device" }, 404);

  try {
    switch (url.pathname) {
      case "/ptz": {
        const dir = String(body.direction ?? "") as DirectionName;
        if (!(dir in DIRECTION)) return json({ error: "bad direction" }, 400);
        // Eufy pans in fixed timed bursts with no velocity parameter, so the only
        // way to travel further in one press is to repeat the burst. That is what
        // the widget's "Step" control adjusts. Clamped: the panel is sandboxed but
        // is still input, and a large repeat would hold the socket for seconds.
        const repeat = Math.max(1, Math.min(Number(body.repeat) || 1, 5));
        for (let i = 0; i < repeat; i++) {
          await send("device.pan_and_tilt", { serialNumber: serial, direction: DIRECTION[dir] });
          if (i + 1 < repeat) await new Promise((r) => setTimeout(r, 350));
        }
        return json({ ok: true });
      }
      case "/calibrate":
        await send("device.calibrate", { serialNumber: serial });
        return json({ ok: true });
      case "/alarm":
        if (body.off) await send("device.reset_alarm", { serialNumber: serial });
        else await send("device.trigger_alarm", { serialNumber: serial, seconds: Number(body.seconds ?? 10) });
        return json({ ok: true });
      default:
        return json({ error: "not found" }, 404);
    }
  } catch (err) {
    return json({ error: String((err as Error).message ?? err) }, 502);
  }
}

const wsUrl = (await aperture.storage.get("ws_url")) ?? "";
if (!wsUrl) {
  // Not an error worth crashing over: the admin has installed the extension but not
  // configured it yet. Serve status so the panels can say so, and pick the socket up
  // when the setting is saved (which restarts this process).
  console.log("eufy-control: no ws_url configured yet");
} else {
  connect(String(wsUrl).replace(/^http/, "ws"));
}

Deno.serve({ port: CONTROL_PORT, hostname: "127.0.0.1" }, handle);
console.log(`eufy-control: control API on 127.0.0.1:${CONTROL_PORT}`);
