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
}

export function useStorage() {
  // Poll dropped: storage.updated invalidates ["storage"]. It fires on storage
  // mutations (config change, location create/edit/allocate/assign/delete), so the
  // panel refreshes on the changes a user makes; slow passive disk-fill now
  // refreshes on the next mutation or navigation rather than every 30s.
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.get<Storage>("/api/system/storage"),
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
