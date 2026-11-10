import { useEffect, useState } from "react";
import { Home, Thermometer } from "lucide-react";
import { Switch } from "./ui/Switch";
import { SettingsHeading, SettingsRow } from "./ui/Layout";
import { CopyIconButton, IntegrationHeader, SectionTitle, StatusPill, ToggleRow } from "./IntegrationParts";
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
    <div>
      <IntegrationHeader
        icon={Home}
        title="Home Assistant"
        status={
          ha?.enabled && (
            <StatusPill tone={ha.connected ? "ok" : "off"}>{ha.connected ? "Connected" : "Disconnected"}</StatusPill>
          )
        }
      >
        Aperture publishes cameras, motion + recording sensors, and storage to your MQTT broker via
        auto-discovery. Point it at your Home Assistant&apos;s MQTT broker (or use Aperture&apos;s built-in
        one and add it to HA).
      </IntegrationHeader>

      <SettingsHeading>MQTT broker</SettingsHeading>
      <div className="divide-y divide-line">
        <ToggleRow label="Enable MQTT publishing" hint="Auto-discovery + live state to the broker">
          <Switch checked={enabled} onChange={setEnabled} label="Enable Home Assistant" />
        </ToggleRow>
        <SettingsRow label="MQTT broker URL">
          <input
            className="input max-w-md font-mono text-xs"
            placeholder="tcp://192.168.1.10:1883"
            value={broker}
            onChange={(e) => setBroker(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="Username">
          <input className="input max-w-md" value={username} onChange={(e) => setUsername(e.target.value)} />
        </SettingsRow>
        <SettingsRow label="Password">
          <input
            className="input max-w-md"
            type="password"
            placeholder={ha?.hasPassword ? "Leave blank to keep" : ""}
            value={password}
            onChange={(e) => setPassword(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow
          label="Discovery prefix"
          hint={
            <>
              Matches HA&apos;s MQTT discovery prefix — almost always{" "}
              <code className="text-fg-muted">homeassistant</code>.
            </>
          }
        >
          <input className="input max-w-md" value={prefix} onChange={(e) => setPrefix(e.target.value)} />
        </SettingsRow>
        <SettingsRow
          label="Sensor state topic"
          hint={
            <>
              Where HA publishes sensor states — enable <em>MQTT Statestream</em> in HA. Cameras can
              then show one of these readings on their overlay.
            </>
          }
        >
          <input
            className="input max-w-md font-mono text-xs"
            value={sensorTopic}
            onChange={(e) => setSensorTopic(e.target.value)}
          />
        </SettingsRow>
        <SettingsRow label="">
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
        </SettingsRow>
      </div>

      {ha?.enabled && (
        <>
          <SectionTitle
            description="Thermometer readings on the broker. Assign one to a camera in its settings to show it on the overlay."
          >
            <span className="inline-flex items-center gap-1.5">
              <Thermometer className="h-4 w-4 text-fg-muted" />
              Temperature sensors
            </span>
          </SectionTitle>
          {sensors && sensors.length > 0 ? (
            <div className="overflow-x-auto">
              <table className="tbl table-fixed">
                <thead>
                  <tr>
                    <th>Sensor</th>
                    <th className="w-28">Reading</th>
                    <th className="hidden w-40 sm:table-cell">Updated</th>
                  </tr>
                </thead>
                <tbody>
                  {sensors.map((s) => (
                    <tr key={s.entityId}>
                      <td className="truncate" title={s.entityId}>
                        <span className="text-fg">{s.name || s.entityId}</span>
                      </td>
                      <td className="whitespace-nowrap tabular-nums">
                        <span className="text-fg">
                          {s.value}
                          {s.unit ? ` ${s.unit}` : ""}
                        </span>
                      </td>
                      <td className="hidden whitespace-nowrap sm:table-cell">
                        {s.updatedAt ? fmtRelative(s.updatedAt * 1000) : ""}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          ) : (
            <p className="border-y border-line py-3 text-sm text-fg-faint">
              None received yet. Enable MQTT Statestream in Home Assistant and confirm the topic above.
            </p>
          )}
        </>
      )}

      {ha && ha.streams.length > 0 && (
        <>
          <SectionTitle
            description={
              <>
                For live streams in HA, add each as a go2rtc / generic camera using its RTSP URL. These
                point at the host where the RTSP port is reachable; if a URL below isn&apos;t reachable (e.g.
                you open Aperture through a reverse proxy), set <code className="text-fg-muted">APERTURE_STREAM_HOST</code> to
                your LAN IP.
              </>
            }
          >
            Live video URLs
          </SectionTitle>
          <div className="overflow-x-auto">
            <table className="tbl table-fixed">
              <thead>
                <tr>
                  <th className="w-32 sm:w-44">Camera</th>
                  <th>RTSP URL</th>
                  <th className="w-12" aria-label="Copy" />
                </tr>
              </thead>
              <tbody>
                {ha.streams.map((s) => (
                  <tr key={s.cameraId}>
                    <td className="truncate">
                      <span className="text-fg">{s.name}</span>
                    </td>
                    <td className="truncate font-mono text-xs" title={s.rtsp}>
                      {s.rtsp}
                    </td>
                    <td className="!px-1 text-right">
                      <CopyIconButton copied={copied === s.cameraId} onClick={() => copy(s.rtsp, s.cameraId)} label="Copy RTSP URL" />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </div>
  );
}
