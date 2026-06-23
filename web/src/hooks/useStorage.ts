import { useQuery } from "@tanstack/react-query";
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
}

export function useStorage() {
  return useQuery({
    queryKey: ["storage"],
    queryFn: () => api.get<Storage>("/api/system/storage"),
    refetchInterval: 30000,
  });
}
