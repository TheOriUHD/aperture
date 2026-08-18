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
  // allocatedBytes/freeBytes are the LOCAL location only; total* spans every
  // enabled destination (see StorageInfo in useStorage.ts).
  storage: {
    allocatedBytes: number;
    usedBytes: number;
    freeBytes: number;
    totalAllocatedBytes?: number;
    totalFreeBytes?: number;
  };
  homeAssistant: { enabled: boolean; connected: boolean };
  weather: { configured: boolean; enabled: boolean; connected: boolean; lastUpdated: number; source: string };
  cameras: CameraHealth[];
}

export function useSystemStatus() {
  // The change feed is the primary driver: camera.state (watchdog on/offline
  // transitions) and alarm.state invalidate ["system-status"] the moment they
  // change. A slow 60s backstop stays alongside it (was 10s) so the free-running
  // fields — uptime, go2rtc stream count — keep advancing on the dashboard instead
  // of freezing between transitions, which can be far apart on a stable system.
  return useQuery({
    queryKey: ["system-status"],
    queryFn: () => api.get<SystemStatus>("/api/system/status"),
    refetchInterval: 60_000,
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

// The Home Assistant Live Card integration: a single server-managed live key plus each
// camera's human slug. See internal/api/integrations_live.go.
export interface LiveKeyCamera {
  id: string;
  name: string;
  slug: string;
}
export interface LiveKeyInfo {
  key: string;
  cameras: LiveKeyCamera[];
  apertureUrl: string;
}

export function useLiveKey() {
  return useQuery({
    queryKey: ["live-key"],
    queryFn: () => api.get<LiveKeyInfo>("/api/integrations/live-key"),
  });
}

export function useRollLiveKey() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: () => api.post<LiveKeyInfo>("/api/integrations/live-key/roll"),
    onSuccess: (data) => qc.setQueryData(["live-key"], data),
  });
}

// ONVIF / HomeKit interop toggles. `enabled` is what the operator asked for;
// `running` is what the subsystem actually reports, so a failed start shows as
// enabled-but-not-running rather than a false green light.
export interface InteropService {
  available: boolean;
  enabled: boolean;
  running: boolean;
  addr?: string; // ONVIF listen address
  pin?: string; // HomeKit pairing code
}
export interface InteropConfig {
  onvif: InteropService;
  homekit: InteropService;
}

export function useInterop() {
  return useQuery({
    queryKey: ["interop"],
    queryFn: () => api.get<InteropConfig>("/api/system/interop"),
    refetchInterval: 15_000,
  });
}

export function useSetInterop() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { onvif?: boolean; homekit?: boolean }) =>
      api.put<InteropConfig>("/api/system/interop", input),
    onSuccess: (data) => qc.setQueryData(["interop"], data),
  });
}
