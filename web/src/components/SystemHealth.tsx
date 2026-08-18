import { useRef, useState } from "react";
import { Activity, Server, Cctv, HardDrive, Home, CloudSun, Download, Upload, FileDown } from "lucide-react";
import { useSystemStatus, useRestoreBackup, type SystemStatus } from "../hooks/useSystem";
import { downloadFile } from "../lib/api";
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
  // A full allocation is the normal rolling state of a hard reserve, not an issue.
  return n;
}

export function SystemHealth() {
  const { data: s } = useSystemStatus();
  const restore = useRestoreBackup();
  const fileRef = useRef<HTMLInputElement>(null);
  const [restoreMsg, setRestoreMsg] = useState("");
  const [restoreErr, setRestoreErr] = useState(false);

  async function onRestoreFile(e: React.ChangeEvent<HTMLInputElement>) {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    if (
      !window.confirm(
        "Restore configuration from this backup?\n\nThis REPLACES all current configuration (cameras, users, rules, settings) and may log you out.",
      )
    )
      return;
    setRestoreMsg("");
    setRestoreErr(false);
    try {
      const r = await restore.mutateAsync(file);
      setRestoreErr(false);
      setRestoreMsg(`Restored ${r.tables} tables — reloading…`);
      setTimeout(() => window.location.reload(), 1500);
    } catch (err) {
      setRestoreErr(true);
      setRestoreMsg((err as Error).message || "Restore failed.");
    }
  }

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
  // Total across every enabled destination (local + network shares); the plain
  // allocatedBytes is the local location only.
  const totalAlloc = s.storage.totalAllocatedBytes ?? s.storage.allocatedBytes;
  const hasAlloc = totalAlloc > 0;
  const usedPct = hasAlloc ? Math.min(100, Math.round((s.storage.usedBytes / totalAlloc) * 100)) : 0;
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

      <Row icon={Server} label="Live streaming (go2rtc)">
        <Dot level={go2rtcLevel} />
        <span className="text-xs text-fg-muted">
          {!s.go2rtc.reachable
            ? "Unreachable"
            : `${s.go2rtc.streams} ${s.go2rtc.streams === 1 ? "stream" : "streams"} registered`}
        </span>
      </Row>

      {/* Per-camera: independent live + recording signals + self-heal + coverage */}
      <div className="mt-2 border-t border-line pt-2 space-y-1.5">
        {s.cameras.map((c) => {
          const live: Level = !c.enabled ? "warn" : c.liveOk ? "ok" : "down";
          const rec: Level = !c.recordingExpected ? "warn" : c.recordingOk ? "ok" : "down";
          return (
            <div key={c.cameraId} className="flex items-center gap-3 text-xs">
              <Cctv className="h-3.5 w-3.5 text-fg-faint shrink-0" />
              <span className="flex-1 truncate text-fg">
                {c.name}
                {(c.restartsToday ?? 0) > 0 && (
                  <span className="ml-1.5 text-[#e0a23a]" title="Watchdog auto-restarts today">
                    ↻{c.restartsToday}
                  </span>
                )}
              </span>
              {c.coveragePercent != null && (
                <span
                  className={`w-10 text-right tabular-nums ${c.coveragePercent < 95 ? "text-[#e0a23a]" : "text-fg-faint"}`}
                  title="Today's recording coverage"
                >
                  {c.coveragePercent}%
                </span>
              )}
              <span className="flex items-center gap-1.5" title="Live view">
                <Dot level={live} />
                <span className="text-fg-faint w-7">live</span>
              </span>
              <span className="flex items-center gap-1.5" title="Recording">
                <Dot level={rec} />
                <span className="text-fg-faint w-8">{c.recordingExpected ? "rec" : "off"}</span>
              </span>
              <span className="text-fg-faint w-16 text-right tabular-nums">
                {c.lastSegmentTs ? fmtRelative(c.lastSegmentTs) : "—"}
              </span>
            </div>
          );
        })}
        {s.cameras.length === 0 && <p className="text-xs text-fg-faint">No cameras configured.</p>}
      </div>

      {/* Storage — scoped to Aperture's allocation, never the whole disk */}
      <div className="mt-3 border-t border-line pt-3">
        <Row icon={HardDrive} label="Storage">
          {hasAlloc ? (
            <>
              <Dot level="ok" />
              <span className="text-xs text-fg-muted">
                {formatBytes(s.storage.usedBytes)} of {formatBytes(totalAlloc)}
              </span>
            </>
          ) : (
            <>
              <Dot level="warn" />
              <span className="text-xs text-fg-muted">No allocation set</span>
            </>
          )}
        </Row>
        {hasAlloc && (
          <div className="mt-2 h-1.5 w-full rounded-full bg-ink-700 overflow-hidden">
            <div className="h-full rounded-full bg-brand" style={{ width: `${usedPct}%` }} />
          </div>
        )}
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

      {/* Maintenance: backup / restore / diagnostics */}
      <div className="mt-3 border-t border-line pt-3">
        <p className="text-xs font-medium text-fg-muted mb-2">Maintenance</p>
        <div className="flex flex-wrap gap-2">
          <button
            className="btn-subtle h-8"
            onClick={() => downloadFile("/api/system/backup", "aperture-backup.json")}
          >
            <Download className="h-3.5 w-3.5" />
            Download backup
          </button>
          <button
            className="btn-subtle h-8"
            onClick={() => downloadFile("/api/system/diagnostics", "aperture-diagnostics.tar.gz")}
          >
            <FileDown className="h-3.5 w-3.5" />
            Diagnostics
          </button>
          <button className="btn-subtle h-8" disabled={restore.isPending} onClick={() => fileRef.current?.click()}>
            <Upload className="h-3.5 w-3.5" />
            {restore.isPending ? "Restoring…" : "Restore…"}
          </button>
          <input
            ref={fileRef}
            type="file"
            accept=".json,application/json"
            className="sr-only"
            onChange={onRestoreFile}
          />
        </div>
        {restoreMsg && <p className={`text-xs mt-2 ${restoreErr ? "text-rec" : "text-online"}`}>{restoreMsg}</p>}
        <p className="text-[11px] text-fg-faint mt-1.5 leading-relaxed">
          Backup includes cameras, users, rules &amp; settings (not footage). It contains camera credentials and
          password hashes — store it securely. Restoring replaces all configuration and may sign you out.
        </p>
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
