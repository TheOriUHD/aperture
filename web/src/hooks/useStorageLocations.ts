import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { StorageLocation } from "./useStorage";

export interface LocationsResponse {
  locations: StorageLocation[];
  dataDir: string; // fixed install location, shown read-only
}

export function useStorageLocations() {
  return useQuery({
    queryKey: ["storage-locations"],
    queryFn: () => api.get<LocationsResponse>("/api/system/storage/locations"),
    refetchInterval: 30000,
  });
}

export interface CreateLocationInput {
  name: string;
  type: "local" | "smb" | "sftp";
  config: Record<string, string | number>;
  secret?: string; // plaintext credential; encrypted server-side before storage
  allocationBytes?: number;
}

function invalidate(qc: ReturnType<typeof useQueryClient>) {
  qc.invalidateQueries({ queryKey: ["storage-locations"] });
  qc.invalidateQueries({ queryKey: ["storage"] });
  qc.invalidateQueries({ queryKey: ["cameras"] });
}

export function useCreateLocation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CreateLocationInput) =>
      api.post<StorageLocation>("/api/system/storage/locations", input),
    onSuccess: () => invalidate(qc),
  });
}

export function useUpdateLocation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({
      id,
      ...patch
    }: {
      id: string;
      name?: string;
      allocationBytes?: number;
      enabled?: boolean;
      config?: Record<string, string | number>;
      secret?: string;
    }) => api.put<StorageLocation>(`/api/system/storage/locations/${id}`, patch),
    onSuccess: () => invalidate(qc),
  });
}

export function useDeleteLocation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, force }: { id: string; force?: boolean }) =>
      api.del<{ status: string }>(`/api/system/storage/locations/${id}${force ? "?force=true" : ""}`),
    onSuccess: () => invalidate(qc),
  });
}

export interface TestStep {
  id: string;
  label: string;
  status: "pass" | "fail" | "skip" | "warn";
  detail?: string;
}
export interface TestResult {
  ok: boolean;
  steps: TestStep[];
  diskTotalBytes?: number;
  diskFreeBytes?: number;
  hostKey?: string; // SFTP: observed host-key fingerprint, pinned on save
}

export function useTestLocation() {
  return useMutation({
    mutationFn: (input: {
      name?: string;
      type: string;
      config: Record<string, unknown>;
      secret?: string;
    }) => api.post<TestResult>("/api/system/storage/locations/test", input),
  });
}

export function useAssignCameras() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { locationId: string; cameraIds: string[] }) =>
      api.post<{ status: string; moved: number }>("/api/system/storage/locations/assign", input),
    onSuccess: () => invalidate(qc),
  });
}

// Sets a destination's allocation (default hard-reserve or network soft cap) and
// applies it immediately — no restart.
export function useAllocateLocation() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { locationId: string; allocationBytes: number }) =>
      api.post<LocationsResponse>("/api/system/storage/locations/allocate", input),
    onSuccess: () => invalidate(qc),
  });
}
