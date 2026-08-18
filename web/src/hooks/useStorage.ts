import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export interface CameraStorage {
  cameraId: string;
  name: string;
  bytes: number;
  count: number;
  oldestTs: number;
  newestTs: number;
}

export interface Storage {
  recordedBytes: number;
  appBytes: number;
  usedBytes: number;
  allocatedBytes: number; // 0 = no allocation set
  freeInAllocationBytes: number;
  diskTotalBytes: number;
  diskFreeBytes: number;
  recordingRateBps: number; // combined encode rate if all cameras record continuously
  recordingCameras: number;
  estimatedFullSeconds: number; // how long the allocation holds at that rate
  cameras: CameraStorage[];
  path: string;
  defaultPath: string;
  capBytes: number;
  dataDir?: string; // fixed install location (app + database), read-only
  locations?: StorageLocation[];
  // Totals across EVERY enabled destination (local + SMB/SFTP). The fields above
  // describe the default local location only, so any global "used of total"
  // display must use these — otherwise all-locations usage is compared against
  // local-only capacity and reads as >100%. 0 = at least one destination is
  // unlimited.
  totalAllocatedBytes?: number;
  totalUsedBytes?: number;
  totalFreeInAllocationBytes?: number;
}

// A recording destination. The built-in default (id "") is virtual — it maps to
// the recordings dir + storage cap and is flagged isDefault.
export interface StorageLocation {
  id: string;
  name: string;
  type: "local" | "smb" | "sftp";
  config: Record<string, unknown>;
  allocationBytes: number; // local = hard reserve, network = soft cap (0 = unset)
  enabled: boolean;
  isDefault: boolean;
  hasCredential: boolean;
  usedBytes: number;
  cameraCount: number;
  diskTotalBytes?: number;
  diskFreeBytes?: number;
  sortOrder?: number;
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, never in a body
}

export function useStorage() {
  // storage.updated (fired on storage mutations — config change, location
  // create/edit/allocate/assign/delete) is the primary driver, but the disk on
  // this VM also fills from OUTSIDE Aperture, which produces no mutation and no
  // feed message. A bar driven only by the feed would never show that, so a slow
  // 60s backstop stays alongside it (was 30s) to catch external/passive disk-fill.
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.get<Storage>("/api/system/storage"),
    refetchInterval: 60_000,
  });
}

export function useSetStorageConfig() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { capBytes: number; path: string }) =>
      api.put<Storage>("/api/system/storage", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["storage"] }),
  });
}

// Whole-disk breakdown — used ONLY by the allocation wizard.
export interface DiskStats {
  totalBytes: number;
  freeBytes: number;
  otherBytes: number; // used by non-Aperture data (not allocatable)
  apertureUsedBytes: number; // current footage + app data (excludes balloon)
  balloonBytes: number; // current hard reservation
  allocatedBytes: number; // current allocation (0 = none)
  maxAllocatableBytes: number; // ceiling the slider may reach (leaves margin)
  marginBytes: number; // safety margin always kept free
}

export function useDiskStats(enabled: boolean) {
  return useQuery({
    queryKey: ["disk-stats"],
    queryFn: () => api.get<DiskStats>("/api/system/storage/disk"),
    enabled,
    refetchOnWindowFocus: false,
  });
}

// Save the hard allocation and restart the service to apply it.
export function useReserveStorage() {
  return useMutation({
    mutationFn: (allocatedBytes: number) =>
      api.post<{ allocatedBytes: number; restarting: boolean }>("/api/system/storage/reserve", {
        allocatedBytes,
      }),
  });
}
