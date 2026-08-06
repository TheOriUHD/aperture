import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, uploadFile } from "../lib/api";

export interface AppSettings {
  siteName: string;
  overviewTempSensor: string; // HA entity id; "" = geo weather
  spotlightsEnabled: boolean; // show the dashboard Spotlights card
  recordQuality: "high" | "low"; // preferred recording stream (main vs sub)
}

export function useSiteName() {
  return useQuery({
    queryKey: ["site-name"],
    queryFn: () => api.get<AppSettings>("/api/system/settings"),
    staleTime: 60_000,
  });
}

export function useSetSiteName() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (siteName: string) => api.put<AppSettings>("/api/system/settings", { siteName }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site-name"] }),
  });
}

// Update any subset of the app settings (site name, overview temp source).
export function useSetSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      siteName?: string;
      overviewTempSensor?: string;
      spotlightsEnabled?: boolean;
      recordQuality?: "high" | "low";
    }) => api.put<AppSettings>("/api/system/settings", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site-name"] }),
  });
}

export interface BitratePoint {
  ts: number;
  bps: number;
}
export interface BitrateData {
  buckets: BitratePoint[];
  currentBps: number;
}

// Recording-throughput series for the overview chart (one point per minute).
// Poll kept: the change feed has no bitrate.updated message (the series advances
// one bucket per minute regardless of any discrete event), so this stays a poll.
export function useBitrate() {
  return useQuery({
    queryKey: ["bitrate"],
    queryFn: () => api.get<BitrateData>("/api/system/bitrate?minutes=30"),
    refetchInterval: 30_000,
  });
}

export interface CameraHealth {
  cameraId: string;
  name: string;
  enabled: boolean;
  recordMode: string;
  recordingExpected: boolean;
  recordingOk: boolean;
  lastSegmentTs: number;
  liveOk: boolean;
  watchdogStale?: boolean;
  restartsToday?: number;
  watchdogLastSeen?: number;
  coveragePercent?: number;
}

export interface SystemStatus {
  version: string;
  uptimeSec: number;
  go2rtc: { reachable: boolean; streams: number };
  liveDownCount: number;
  recDownCount: number;
  storage: { allocatedBytes: number; usedBytes: number; freeBytes: number };
  homeAssistant: { enabled: boolean; connected: boolean };
  weather: { configured: boolean; enabled: boolean; connected: boolean; lastUpdated: number; source: string };
  cameras: CameraHealth[];
}

export function useSystemStatus() {
  // Poll dropped: the change feed drives this. camera.state (watchdog on/offline
  // transitions) and alarm.state both invalidate ["system-status"], so per-camera
  // health and the armed state refresh on the transitions that matter rather than
  // on a 10s timer (uptime/go2rtc counters simply stop free-running between them).
  return useQuery({
    queryKey: ["system-status"],
    queryFn: () => api.get<SystemStatus>("/api/system/status"),
  });
}

export function useRestoreBackup() {
  return useMutation({
    mutationFn: (file: File) => uploadFile<{ ok: boolean; tables: number }>("/api/system/restore", "backup", file),
  });
}

export function useReindex() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<{ recovered: number }>("/api/system/reindex"),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["storage"] });
      qc.invalidateQueries({ queryKey: ["system-status"] });
    },
  });
}

export interface PurgeResult {
  deletedSegments: number;
  deletedEvents: number;
  freedBytes: number;
}

export interface HAStream {
  cameraId: string;
  name: string;
  rtsp: string;
}
export interface HAConfig {
  enabled: boolean;
  broker: string;
  username: string;
  prefix: string;
  sensorTopic: string;
  hasPassword: boolean;
  connected: boolean;
  streams: HAStream[];
}

// Poll kept: no ha.* feed message exists (broker connection state and the MQTT
// stream list have no server-side emitter), so this stays a poll.
export function useHA() {
  return useQuery({
    queryKey: ["ha"],
    queryFn: () => api.get<HAConfig>("/api/system/ha"),
    refetchInterval: 8000,
  });
}

export function useSetHA() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      enabled: boolean;
      broker: string;
      username: string;
      password?: string;
      prefix: string;
      sensorTopic: string;
    }) => api.put<HAConfig>("/api/system/ha", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ha"] }),
  });
}

export interface HASensor {
  entityId: string;
  name: string;
  value: string;
  unit: string;
  deviceClass: string;
  updatedAt: number;
}

// Poll kept: sensor values arrive over inbound MQTT continuously and have no
// discrete ha.* feed message, so this stays a poll.
export function useHASensors(enabled = true) {
  return useQuery({
    queryKey: ["ha-sensors"],
    queryFn: () => api.get<HASensor[]>("/api/system/ha/sensors"),
    refetchInterval: 10000,
    enabled,
  });
}

export interface OIDCConfig {
  enabled: boolean;
  issuer: string;
  clientId: string;
  domain: string;
  autoCreate: boolean;
  name: string;
  hasSecret: boolean;
  redirectUrl: string;
}

export function useOIDC() {
  return useQuery({
    queryKey: ["oidc"],
    queryFn: () => api.get<OIDCConfig>("/api/system/oidc"),
    staleTime: 30_000,
  });
}

export function useSetOIDC() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      enabled: boolean;
      issuer: string;
      clientId: string;
      clientSecret?: string;
      domain: string;
      autoCreate: boolean;
      name: string;
    }) => api.put<OIDCConfig>("/api/system/oidc", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["oidc"] }),
  });
}

export function usePurge() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { cameraId?: string; from: number; to: number }) =>
      api.post<PurgeResult>("/api/system/purge", input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["storage"] });
      qc.invalidateQueries({ queryKey: ["segments"] });
      qc.invalidateQueries({ queryKey: ["events"] });
      qc.invalidateQueries({ queryKey: ["events-all"] });
      qc.invalidateQueries({ queryKey: ["events-today"] });
    },
  });
}
