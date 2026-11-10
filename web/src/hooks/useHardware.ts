import { useMemo } from "react";
import { useMutation, useQueries, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import { useAuth } from "../lib/auth";
import { useCameras } from "./useCameras";
import { accelById, cpuById } from "../lib/hardware/search";
import { computeCapacity, type CameraWork, type Capacity, type HardwareProfile } from "../lib/capacity";
import type { CpuSpec } from "../lib/hardware/cpus";
import type { AccelSpec } from "../lib/hardware/accelerators";
import type { Camera } from "../lib/types";
import type { LocationsResponse } from "./useStorageLocations";

export interface DetectedHardware {
  cpuModel: string;
  board?: string;
  arch: string;
  logicalCpus: number;
  cpuQuota: number; // cores; 0 = unlimited
  memTotalBytes: number;
  memLimitBytes: number; // 0 = unlimited
  virtual?: string;
  accelerators: { kind: string; name: string; path: string }[];
}

export interface HardwareInfo {
  profile: HardwareProfile | null;
  detected: DetectedHardware;
}

export function useHardware() {
  return useQuery({
    queryKey: ["hardware"],
    queryFn: () => api.get<HardwareInfo>("/api/system/hardware"),
    staleTime: 5 * 60_000,
  });
}

export function useSetHardware() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (p: Omit<HardwareProfile, "updatedAt">) => api.put<{ profile: HardwareProfile }>("/api/system/hardware", p),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["hardware"] }),
  });
}

// Each camera's main-stream size, read off a snapshot. Cached per camera, so the
// dashboard, the Devices page and Settings share one load.
function loadSize(url: string): Promise<{ w: number; h: number }> {
  return new Promise((resolve, reject) => {
    const img = new Image();
    img.onload = () => (img.naturalWidth ? resolve({ w: img.naturalWidth, h: img.naturalHeight }) : reject(new Error("empty")));
    img.onerror = () => reject(new Error("no snapshot"));
    img.src = url;
  });
}

export function useCameraResolutions(cameras: Camera[]): Record<string, { w: number; h: number }> {
  const list = cameras.filter((c) => c.enabled);
  const results = useQueries({
    queries: list.map((c) => ({
      queryKey: ["cam-res", c.id],
      queryFn: () => loadSize(`/api/v1/cameras/${c.id}/snapshot.jpg`),
      staleTime: 30 * 60_000,
      gcTime: 60 * 60_000,
      retry: 1,
    })),
  });
  const key = results.map((r) => (r.data ? `${r.data.w}x${r.data.h}` : "-")).join(",");
  return useMemo(() => {
    const out: Record<string, { w: number; h: number }> = {};
    list.forEach((c, i) => {
      const d = results[i]?.data;
      if (d) out[c.id] = d;
    });
    return out;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key, list.map((c) => c.id).join(",")]);
}

interface DetectionConfig {
  mode: "off" | "bridge" | "local" | "onnx";
  bridgeUrl: string;
}

// A detector reached through localhost runs on this machine, so its inference
// is this machine's work; one elsewhere on the network is not.
function detectorIsLocal(url: string): boolean {
  try {
    const h = new URL(url).hostname.replace(/^\[|\]$/g, "");
    return h === "localhost" || h === "::1" || h.startsWith("127.") || h === "host.docker.internal";
  } catch {
    return false;
  }
}

export function reencodes(c: Pick<Camera, "recordHeight" | "recordFps" | "recordKbps">): boolean {
  return (c.recordHeight ?? 0) > 0 || (c.recordFps ?? 0) > 0 || (c.recordKbps ?? 0) > 0;
}

// cameraWork turns a camera's settings into what it makes Aperture do.
export function cameraWork(
  c: Pick<Camera, "id" | "name" | "recordMode" | "motionDetection" | "rtspSubUrl" | "recordHeight" | "recordFps" | "recordKbps" | "detectMethod" | "locationId">,
  size: { w: number; h: number } | undefined,
  ctx: { isAdmin: boolean; detection?: DetectionConfig; remoteLocations: Set<string>; mirrorsRemote: (id: string) => boolean },
): CameraWork {
  const motion = c.recordMode === "motion" || c.motionDetection !== false;
  const det = ctx.detection;
  const method = c.detectMethod || (det?.mode ?? "off");
  return {
    id: c.id,
    name: c.name,
    width: size?.w,
    height: size?.h,
    recording: c.recordMode !== "never",
    motion,
    // Viewers aren't sent stream links; count their cameras as having a sub stream.
    hasSub: ctx.isAdmin ? !!c.rtspSubUrl : true,
    reencode: c.recordMode !== "never" && reencodes(c),
    reencodeHeight: c.recordHeight || undefined,
    // Aperture's own recognition always runs here; an external detector only
    // when it is on this machine.
    ai: motion && (method === "local" || method === "onnx" || (method === "bridge" && !!det && detectorIsLocal(det.bridgeUrl))),
    remoteStorage: c.recordMode !== "never" && (ctx.remoteLocations.has(c.locationId ?? "") || ctx.mirrorsRemote(c.id)),
  };
}

export interface CapacityState {
  loading: boolean;
  configured: boolean;
  profile: HardwareProfile | null;
  cpu?: CpuSpec;
  accel?: AccelSpec;
  capacity?: Capacity;
  works: CameraWork[];
  // With a camera added (or, given replaceId, changed), for the soft limit.
  project: (work: CameraWork, replaceId?: string) => Capacity | undefined;
  // What a camera with these settings would make Aperture do.
  workFor: (c: Parameters<typeof cameraWork>[0], size?: { w: number; h: number }) => CameraWork;
  sizeOf: (id: string) => { w: number; h: number } | undefined;
}

export function useCapacity(): CapacityState {
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: cameras, isLoading: camsLoading } = useCameras();
  const { data: hw, isLoading: hwLoading } = useHardware();
  const enabled = useMemo(() => (cameras ?? []).filter((c) => c.enabled), [cameras]);
  const sizes = useCameraResolutions(enabled);
  // Same query keys as the Detection and Storage settings, so the cache is shared.
  const { data: detection } = useQuery({
    queryKey: ["detection"],
    queryFn: () => api.get<DetectionConfig>("/api/system/detection"),
    enabled: isAdmin,
  });
  const { data: locs } = useQuery({
    queryKey: ["storage-locations"],
    queryFn: () => api.get<LocationsResponse>("/api/system/storage/locations"),
    enabled: isAdmin,
  });

  return useMemo(() => {
    const profile = hw?.profile ?? null;
    const cpu = profile ? cpuById(profile.cpuId) : undefined;
    const accel = profile?.accelerator ? accelById(profile.accelerator.id) : undefined;
    const remote = (locs?.locations ?? []).filter((l) => l.type !== "local");
    const remoteLocations = new Set(remote.map((l) => l.id));
    const mirrorsRemote = (id: string) => remote.some((l) => l.cameras?.some((m) => m.cameraId === id));
    const ctx = { isAdmin, detection, remoteLocations, mirrorsRemote };
    const works = enabled.map((c) => cameraWork(c, sizes[c.id], ctx));
    const capacity = profile && cpu ? computeCapacity(cpu, profile, works, accel) : undefined;
    return {
      loading: camsLoading || hwLoading,
      configured: !!capacity,
      profile,
      cpu,
      accel,
      capacity,
      works,
      project: (work: CameraWork, replaceId?: string) =>
        profile && cpu ? computeCapacity(cpu, profile, [...works.filter((w) => w.id !== replaceId), work], accel) : undefined,
      workFor: (c: Parameters<typeof cameraWork>[0], size?: { w: number; h: number }) => cameraWork(c, size, ctx),
      sizeOf: (id: string) => sizes[id],
    };
  }, [hw, enabled, sizes, detection, locs, isAdmin, camsLoading, hwLoading]);
}
