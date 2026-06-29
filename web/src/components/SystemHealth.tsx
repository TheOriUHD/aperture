import { Activity, Server, Cctv, HardDrive, Home, CloudSun } from "lucide-react";
import { useSystemStatus, type SystemStatus } from "../hooks/useSystem";
import { formatBytes, fmtRelative, fmtDuration } from "../lib/format";

type Level = "ok" | "warn" | "down";

function Dot({ level }: { level: Level }) {
  const color = level === "ok" ? "bg-online" : level === "warn" ? "bg-[#e0a23a]" : "bg-rec";
  return <span className={`h-2 w-2 rounded-full ${color} shrink-0`} />;
}

function issueCount(s: SystemStatus): number {
  let n = s.liveDownCount + s.recDownCount;
  if (!s.go2rtc.reachable) n++;
  if (s.homeAssistant.enabled && !s.homeAssistant.connected) n++;
  if (s.weather.configured && s.weather.enabled && !s.weather.connected) n++;
  const freePct = s.storage.totalBytes ? s.storage.freeBytes / s.storage.totalBytes : 1;
  if (freePct < 0.05) n++;
  return n;
}

export function SystemHealth() {
  const { data: s } = useSystemStatus();
  if (!s) {
    return (
      <div className="panel">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg mb-3">
          <Activity className="h-4 w-4" />
          System health
        </h2>
        <p className="text-xs text-fg-faint">Loading…</p>
      </div>
    );
  }

  const issues = issueCount(s);
  const freePct = s.storage.totalBytes ? s.storage.freeBytes / s.storage.totalBytes : 1;
  const go2rtcLevel: Level = !s.go2rtc.reachable ? "down" : s.go2rtc.streams === 0 ? "warn" : "ok";

  return (
    <div className="panel">
      <div className="flex items-center justify-between mb-3">
        <h2 className="flex items-center gap-2 text-sm font-medium text-fg">
          <Activity className="h-4 w-4" />
          System health
        </h2>
        <span className={`pill ${issues === 0 ? "pill-online" : "bg-rec/15 text-rec"}`}>
          <Dot level={issues === 0 ? "ok" : "down"} />
          {issues === 0 ? "All systems normal" : `${issues} ${issues === 1 ? "issue" : "issues"}`}
        </span>
      </div>

      {/* go2rtc — the live-streaming ingress */}
      <Row icon={Server} label="Live streaming (go2rtc)">
        <Dot level={go2rtcLevel} />
        <span className="text-xs text-fg-muted">
          {!s.go2rtc.reachable
            ? "Unreachable"
            : `${s.go2rtc.streams} ${s.go2rtc.streams === 1 ? "stream" : "streams"} registered`}
        </span>
      </Row>

      {/* Per-camera: independent live + recording signals */}
      <div className="mt-2 border-t border-line pt-2 space-y-1.5">
        {s.cameras.map((c) => {
          const live: Level = !c.enabled ? "warn" : c.liveOk ? "ok" : "down";
          const rec: Level = !c.recordingExpected ? "warn" : c.recordingOk ? "ok" : "down";
          return (
            <div key={c.cameraId} className="flex items-center gap-3 text-xs">
              <Cctv className="h-3.5 w-3.5 text-fg-faint shrink-0" />
              <span className="flex-1 truncate text-fg">{c.name}</span>
              <span className="flex items-center gap-1.5" title="Live view">
                <Dot level={live} />
                <span className="text-fg-faint w-8">live</span>
              </span>
              <span className="flex items-center gap-1.5" title="Recording">
                <Dot level={rec} />
                <span className="text-fg-faint w-9">
                  {c.recordingExpected ? "rec" : "off"}
                </span>
              </span>
              <span className="text-fg-faint w-20 text-right tabular-nums">
                {c.lastSegmentTs ? fmtRelative(c.lastSegmentTs) : "—"}
              </span>
            </div>
          );
        })}
        {s.cameras.length === 0 && <p className="text-xs text-fg-faint">No cameras configured.</p>}
      </div>

      {/* Storage */}
      <div className="mt-3 border-t border-line pt-3">
        <Row icon={HardDrive} label="Storage">
          <Dot level={freePct < 0.05 ? "down" : freePct < 0.12 ? "warn" : "ok"} />
          <span className="text-xs text-fg-muted">
            {formatBytes(s.storage.freeBytes)} free of {formatBytes(s.storage.totalBytes)}
          </span>
        </Row>
        <div className="mt-2 h-1.5 w-full rounded-full bg-ink-700 overflow-hidden">
          <div
            className="h-full rounded-full bg-brand"
            style={{ width: `${Math.min(100, Math.round((1 - freePct) * 100))}%` }}
          />
        </div>
      </div>

      {/* Integrations */}
      <div className="mt-3 border-t border-line pt-2 space-y-1.5">
        {s.homeAssistant.enabled && (
          <Row icon={Home} label="Home Assistant">
            <Dot level={s.homeAssistant.connected ? "ok" : "down"} />
            <span className="text-xs text-fg-muted">{s.homeAssistant.connected ? "Connected" : "Disconnected"}</span>
          </Row>
        )}
        {s.weather.configured && (
          <Row icon={CloudSun} label="Weather">
            <Dot level={!s.weather.enabled ? "warn" : s.weather.connected ? "ok" : "down"} />
            <span className="text-xs text-fg-muted">
              {!s.weather.enabled ? "Disabled" : s.weather.connected ? `${s.weather.source}` : "No data"}
            </span>
          </Row>
        )}
      </div>

      <p className="mt-3 border-t border-line pt-2 text-[11px] text-fg-faint">
        Uptime {fmtDuration(s.uptimeSec * 1000)} · version {s.version}
      </p>
    </div>
  );
}

function Row({
  icon: Icon,
  label,
  children,
}: {
  icon: typeof Activity;
  label: string;
  children: React.ReactNode;
}) {
  return (
    <div className="flex items-center gap-3 text-sm">
      <Icon className="h-4 w-4 text-fg-muted shrink-0" />
      <span className="flex-1 text-fg-muted">{label}</span>
      <span className="flex items-center gap-2">{children}</span>
    </div>
  );
}
