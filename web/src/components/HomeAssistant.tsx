import { useEffect, useState } from "react";
import { Copy, Check, Home, Thermometer } from "lucide-react";
import { Switch } from "./ui/Switch";
import { useHA, useSetHA, useHASensors } from "../hooks/useSystem";
import { fmtRelative } from "../lib/format";

export function HomeAssistant() {
  const { data: ha } = useHA();
  const setHA = useSetHA();
  const { data: sensors } = useHASensors(!!ha?.enabled);

  const [enabled, setEnabled] = useState(false);
  const [broker, setBroker] = useState("");
  const [username, setUsername] = useState("");
  const [password, setPassword] = useState("");
  const [prefix, setPrefix] = useState("homeassistant");
  const [sensorTopic, setSensorTopic] = useState("homeassistant/sensor");
  const [saved, setSaved] = useState(false);
  const [copied, setCopied] = useState("");

  useEffect(() => {
    if (!ha) return;
    setEnabled(ha.enabled);
    setBroker(ha.broker);
    setUsername(ha.username);
    setPrefix(ha.prefix || "homeassistant");
    setSensorTopic(ha.sensorTopic || "homeassistant/sensor");
  }, [ha]);

  async function copy(text: string, key: string) {
    try {
      await navigator.clipboard.writeText(text);
      setCopied(key);
      setTimeout(() => setCopied(""), 1500);
    } catch {
      /* ignore */
    }
  }

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <Home className="h-4 w-4" />
          Home Assistant
        </h2>
        {ha?.enabled && (
          <span className={`pill ${ha.connected ? "pill-online" : "bg-ink-700 text-fg-faint"}`}>
            <span className={`h-1.5 w-1.5 rounded-full ${ha.connected ? "bg-online" : "bg-fg-faint"}`} />
            {ha.connected ? "Connected" : "Disconnected"}
          </span>
        )}
      </div>

      <p className="text-xs text-fg-faint mb-4 leading-relaxed">
        Aperture publishes cameras, motion + recording sensors, and storage to your MQTT broker via
        auto-discovery. Point it at your Home Assistant&apos;s MQTT broker (or use Aperture&apos;s built-in
        one and add it to HA).
      </p>

      <div className="space-y-4">
        <div className="flex items-center justify-between">
          <div>
            <span className="text-sm text-fg">Enable MQTT publishing</span>
            <p className="text-xs text-fg-faint">Auto-discovery + live state to the broker</p>
          </div>
          <Switch checked={enabled} onChange={setEnabled} label="Enable Home Assistant" />
        </div>

        <div>
          <label className="label">MQTT broker URL</label>
          <input
            className="input font-mono text-xs"
            placeholder="tcp://192.168.1.10:1883"
            value={broker}
            onChange={(e) => setBroker(e.target.value)}
          />
        </div>
        <div className="grid grid-cols-2 gap-3">
          <div>
            <label className="label">Username</label>
            <input className="input" value={username} onChange={(e) => setUsername(e.target.value)} />
          </div>
          <div>
            <label className="label">Password</label>
            <input
              className="input"
              type="password"
              placeholder={ha?.hasPassword ? "Leave blank to keep" : ""}
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          </div>
        </div>
        <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
          <div>
            <label className="label">Discovery prefix</label>
            <input className="input" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
            <p className="text-xs text-fg-faint mt-1.5">
              Matches HA&apos;s MQTT discovery prefix — almost always{" "}
              <code className="text-fg-muted">homeassistant</code>.
            </p>
          </div>
          <div>
            <label className="label">Sensor state topic</label>
            <input
              className="input font-mono text-xs"
              value={sensorTopic}
              onChange={(e) => setSensorTopic(e.target.value)}
            />
            <p className="text-xs text-fg-faint mt-1.5">
              Where HA publishes sensor states — enable <em>MQTT Statestream</em> in HA. Cameras can
              then show one of these readings on their overlay.
            </p>
          </div>
        </div>

        <div className="flex items-center gap-3">
          <button
            className="btn-primary"
            disabled={setHA.isPending}
            onClick={async () => {
              await setHA.mutateAsync({
                enabled,
                broker,
                username,
                password: password || undefined,
                prefix,
                sensorTopic,
              });
              setPassword("");
              setSaved(true);
              setTimeout(() => setSaved(false), 1500);
            }}
          >
            {setHA.isPending ? "Saving…" : "Save"}
          </button>
          {saved && <span className="text-xs text-online">Saved.</span>}
        </div>
      </div>

      {ha?.enabled && (
        <div className="border-t border-line mt-5 pt-4">
          <p className="flex items-center gap-1.5 text-xs font-medium text-fg-muted mb-1">
            <Thermometer className="h-3.5 w-3.5" />
            Temperature sensors
          </p>
          <p className="text-xs text-fg-faint mb-2.5">
            Thermometer readings on the broker. Assign one to a camera in its settings to show it on the
            overlay.
          </p>
          {sensors && sensors.length > 0 ? (
            <div className="space-y-1.5 max-h-48 overflow-auto">
              {sensors.map((s) => (
                <div key={s.entityId} className="flex items-center gap-2 text-xs">
                  <span className="flex-1 truncate text-fg">{s.name || s.entityId}</span>
                  <span className="tabular-nums text-fg-muted">
                    {s.value}
                    {s.unit ? ` ${s.unit}` : ""}
                  </span>
                  <span className="w-16 text-right text-fg-faint">
                    {s.updatedAt ? fmtRelative(s.updatedAt * 1000) : ""}
                  </span>
                </div>
              ))}
            </div>
          ) : (
            <p className="text-xs text-fg-faint">
              None received yet. Enable MQTT Statestream in Home Assistant and confirm the topic above.
            </p>
          )}
        </div>
      )}

      {ha && ha.streams.length > 0 && (
        <div className="border-t border-line mt-5 pt-4">
          <p className="text-xs font-medium text-fg-muted mb-1">Live video URLs</p>
          <p className="text-xs text-fg-faint mb-2.5">
            For live streams in HA, add each as a go2rtc / generic camera using its RTSP URL. These
            point at the host where the RTSP port is reachable; if a URL below isn't reachable (e.g.
            you open Aperture through a reverse proxy), set <code className="text-fg-muted">APERTURE_STREAM_HOST</code> to
            your LAN IP.
          </p>
          <div className="space-y-1.5">
            {ha.streams.map((s) => (
              <div key={s.cameraId} className="flex items-center gap-2 text-xs">
                <span className="w-28 truncate text-fg-muted">{s.name}</span>
                <code className="flex-1 truncate rounded bg-ink-850 px-2 py-1 font-mono text-fg-muted">
                  {s.rtsp}
                </code>
                <button
                  className="btn-icon h-7 w-7"
                  onClick={() => copy(s.rtsp, s.cameraId)}
                  aria-label="Copy RTSP URL"
                >
                  {copied === s.cameraId ? (
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
    </div>
  );
}
