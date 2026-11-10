import { useRef, useState } from "react";
import { Activity, Server, Cctv, HardDrive, Home, CloudSun, Download, Upload, FileDown } from "lucide-react";
import { useSystemStatus, useRestoreBackup, type SystemStatus } from "../hooks/useSystem";
import { downloadFile } from "../lib/api";
import { formatBytes, fmtRelative, fmtDuration } from "../lib/format";
import { recordingDisplay, type Level } from "../lib/recordingState";
import { SettingsHeading, SettingsRow, StatusDot } from "./ui/Layout";
import { Skeleton } from "./ui/Skeleton";

function Dot({ level }: { level: Level }) {
  return <StatusDot tone={level === "ok" ? "ok" : level === "idle" ? "off" : level === "warn" ? "warn" : "bad"} />;
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
      <>
        <SettingsHeading>System health</SettingsHeading>
        <div className="space-y-2">
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
          <Skeleton className="h-10" />
        </div>
      </>
    );
  }

  const issues = issueCount(s);
  // Total across every enabled destination (local + network shares); the plain
  // allocatedBytes is the local location only.
  const totalAlloc = s.storage.totalAllocatedBytes ?? s.storage.allocatedBytes;
  const hasAlloc = totalAlloc > 0;
  const usedPct = hasAlloc ? Math.min(100, Math.round((s.storage.usedBytes / totalAlloc) * 100)) : 0;
  const go2rtcLevel: Level = !s.go2rtc.reachable ? "down" : s.go2rtc.streams === 0 ? "warn" : "ok";
  const flush = "[&_td:first-child]:pl-0 [&_td:last-child]:pr-0 [&_th:first-child]:pl-0 [&_th:last-child]:pr-0";

  return (
    <>
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-2">
        <h3 className="text-sm font-semibold text-fg">System health</h3>
        <span className={issues === 0 ? "pill-green" : "pill-red"}>
          <Dot level={issues === 0 ? "ok" : "down"} />
          {issues === 0 ? "All systems normal" : `${issues} ${issues === 1 ? "issue" : "issues"}`}
        </span>
      </div>

      <div className="divide-y divide-line">
        <SettingsRow label={<RowLabel icon={Server}>Live streaming (go2rtc)</RowLabel>}>
          <Status level={go2rtcLevel}>
            {!s.go2rtc.reachable
              ? "Unreachable"
              : `${s.go2rtc.streams} ${s.go2rtc.streams === 1 ? "stream" : "streams"} registered`}
          </Status>
        </SettingsRow>

        {/* Storage — scoped to Aperture's allocation, never the whole disk */}
        <SettingsRow label={<RowLabel icon={HardDrive}>Storage</RowLabel>}>
          {hasAlloc ? (
            <div className="flex max-w-md items-center gap-3">
              <Status level="ok">
                {formatBytes(s.storage.usedBytes)} of {formatBytes(totalAlloc)}
              </Status>
              <div className="h-1.5 min-w-[80px] flex-1 overflow-hidden rounded-full bg-ink-700">
                <div className="h-full rounded-full bg-brand" style={{ width: `${usedPct}%` }} />
              </div>
              <span className="w-9 text-right text-xs tabular-nums text-fg-faint">{usedPct}%</span>
            </div>
          ) : (
            <Status level="warn">No allocation set</Status>
          )}
        </SettingsRow>

        {/* Integrations */}
        {s.homeAssistant.enabled && (
          <SettingsRow label={<RowLabel icon={Home}>Home Assistant</RowLabel>}>
            <Status level={s.homeAssistant.connected ? "ok" : "down"}>
              {s.homeAssistant.connected ? "Connected" : "Disconnected"}
            </Status>
          </SettingsRow>
        )}
        {s.weather.configured && (
          <SettingsRow label={<RowLabel icon={CloudSun}>Weather</RowLabel>}>
            <Status level={!s.weather.enabled ? "warn" : s.weather.connected ? "ok" : "down"}>
              {!s.weather.enabled ? "Disabled" : s.weather.connected ? `${s.weather.source}` : "No data"}
            </Status>
          </SettingsRow>
        )}

        <SettingsRow label={<RowLabel icon={Activity}>Uptime</RowLabel>}>
          <span className="text-sm tabular-nums text-fg">
            {fmtDuration(s.uptimeSec * 1000)} <span className="text-fg-faint">· version {s.version}</span>
          </span>
        </SettingsRow>
      </div>

      {/* Per-camera: independent live + recording signals + self-heal + coverage */}
      <SettingsHeading>Cameras</SettingsHeading>
      {s.cameras.length === 0 ? (
        <p className="flex items-center gap-3 border-y border-line py-4 text-sm text-fg-faint">
          <Cctv className="h-4 w-4 shrink-0" />
          No cameras configured.
        </p>
      ) : (
        <div className="overflow-x-auto">
          <table className={`tbl table-fixed ${flush}`}>
            <thead>
              <tr>
                <th>Camera</th>
                <th className="w-20 sm:w-28">Live</th>
                <th className="w-20 sm:w-28">Recording</th>
                <th className="hidden w-32 sm:table-cell" title="Today's recording coverage">
                  Coverage today
                </th>
                <th className="hidden w-36 !text-right sm:table-cell">Last segment</th>
              </tr>
            </thead>
            <tbody>
              {s.cameras.map((c) => {
                const live: Level = !c.enabled ? "warn" : c.liveOk ? "ok" : "down";
                const rec = recordingDisplay(c.recordingState);
                return (
                  <tr key={c.cameraId} className="hover:bg-white/[0.02]">
                    <td className="py-1.5">
                      <div className="flex min-w-0 items-center gap-2.5">
                        <Cctv className="h-4 w-4 shrink-0 text-fg-faint" />
                        <div className="min-w-0">
                          <div className="flex min-w-0 items-center gap-2">
                            <span className="truncate text-fg">{c.name}</span>
                            {(c.restartsToday ?? 0) > 0 && (
                              <span className="pill-orange shrink-0" title="Watchdog auto-restarts today">
                                ↻{c.restartsToday}
                              </span>
                            )}
                          </div>
                          {/* On a phone, coverage and the last segment fold under the name. */}
                          {(c.coveragePercent != null || !!c.lastSegmentTs) && (
                            <p className="truncate text-xs tabular-nums text-fg-faint sm:hidden">
                              {c.coveragePercent != null && (
                                <span className={c.coveragePercent < 95 ? "text-warn" : ""}>{c.coveragePercent}%</span>
                              )}
                              {c.coveragePercent != null && c.lastSegmentTs ? " · " : ""}
                              {c.lastSegmentTs ? fmtRelative(c.lastSegmentTs) : ""}
                            </p>
                          )}
                        </div>
                      </div>
                    </td>
                    <td title="Live view">
                      <span className="inline-flex items-center gap-2">
                        <Dot level={live} />
                        <span className="text-fg-muted">live</span>
                      </span>
                    </td>
                    <td title={rec.title}>
                      <span className="inline-flex items-center gap-2">
                        <Dot level={rec.level} />
                        <span className="text-fg-muted">{rec.label}</span>
                      </span>
                    </td>
                    <td className="hidden sm:table-cell" title="Today's recording coverage">
                      {c.coveragePercent != null ? (
                        <span className={`tabular-nums ${c.coveragePercent < 95 ? "text-warn" : ""}`}>
                          {c.coveragePercent}%
                        </span>
                      ) : (
                        <span className="text-fg-faint">—</span>
                      )}
                    </td>
                    <td className="hidden text-right tabular-nums text-fg-muted sm:table-cell">
                      {c.lastSegmentTs ? fmtRelative(c.lastSegmentTs) : "—"}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      )}

      {/* Maintenance: backup / restore / diagnostics */}
      <SettingsHeading>Maintenance</SettingsHeading>
      <div className="divide-y divide-line">
        <SettingsRow
          label="Backup"
          hint="Cameras, users, rules & settings (not footage). It contains camera credentials and password hashes — store it securely."
        >
          <button className="btn-ghost" onClick={() => downloadFile("/api/system/backup", "aperture-backup.json")}>
            <Download className="h-4 w-4" />
            Download backup
          </button>
        </SettingsRow>
        <SettingsRow label="Restore" hint="Restoring replaces all configuration and may sign you out.">
          <div className="flex flex-wrap items-center gap-3">
            <button className="btn-ghost" disabled={restore.isPending} onClick={() => fileRef.current?.click()}>
              <Upload className="h-4 w-4" />
              {restore.isPending ? "Restoring…" : "Restore…"}
            </button>
            <input
              ref={fileRef}
              type="file"
              accept=".json,application/json"
              className="sr-only"
              onChange={onRestoreFile}
            />
            {restoreMsg && <p className={`text-xs ${restoreErr ? "text-danger" : "text-online"}`}>{restoreMsg}</p>}
          </div>
        </SettingsRow>
        <SettingsRow label="Diagnostics">
          <button
            className="btn-ghost"
            onClick={() => downloadFile("/api/system/diagnostics", "aperture-diagnostics.tar.gz")}
          >
            <FileDown className="h-4 w-4" />
            Download diagnostics
          </button>
        </SettingsRow>
      </div>
    </>
  );
}

function RowLabel({ icon: Icon, children }: { icon: typeof Activity; children: React.ReactNode }) {
  return (
    <span className="flex items-center gap-2.5">
      <Icon className="h-4 w-4 shrink-0 text-fg-faint" />
      {children}
    </span>
  );
}

function Status({ level, children }: { level: Level; children: React.ReactNode }) {
  return (
    <span className="inline-flex items-center gap-2 whitespace-nowrap text-sm text-fg">
      <Dot level={level} />
      {children}
    </span>
  );
}
