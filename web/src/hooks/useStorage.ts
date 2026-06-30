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
  diskTotalBytes: number;
  diskFreeBytes: number;
  cameras: CameraStorage[];
  path: string;
  defaultPath: string;
  capBytes: number;
}

export function useStorage() {
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.get<Storage>("/api/system/storage"),
    refetchInterval: 30000,
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
