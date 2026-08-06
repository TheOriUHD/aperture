import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export interface ExportJob {
  id: string;
  cameraId: string;
  cameraName: string;
  fromTs: number;
  toTs: number;
  burnIn: boolean;
  audio: boolean;
  status: "queued" | "running" | "done" | "error";
  progress: number;
  sizeBytes: number;
  error: string;
  createdAt: number;
  finishedAt: number;
}

export function useExports() {
  // Poll dropped: export.progress invalidates ["exports"] on every progress tick
  // of a running job (export/manager.go → OnExportProgress), which is exactly what
  // the old 1.2s while-active poll was for; create/delete still invalidate too.
  return useQuery({
    queryKey: ["exports"],
    queryFn: () => api.get<ExportJob[]>("/api/export"),
  });
}

export function useCreateExport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: {
      cameraIds: string[];
      mode: "single" | "zip" | "composite";
      from: number;
      to: number;
      burnIn: boolean;
      audio: boolean;
      speed?: number; // 0/1 realtime, N = timelapse N×
    }) => api.post<ExportJob>("/api/export", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["exports"] }),
  });
}

export function useDeleteExport() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/export/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["exports"] }),
  });
}

export interface CreatedShare {
  id: string;
  url: string;
  label: string;
  expiresAt: number;
  maxViews: number;
}

export function useCreateShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: {
      exportId: string;
      label?: string;
      expiresHours?: number;
      password?: string;
      maxViews?: number;
    }) => api.post<CreatedShare>("/api/shares", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["shares"] }),
  });
}

export interface ShareInfo {
  id: string;
  label: string;
  expiresAt: number;
  maxViews: number;
  views: number;
  revoked: boolean;
  hasPassword: boolean;
  createdAt: number;
}

export function useShares() {
  return useQuery({ queryKey: ["shares"], queryFn: () => api.get<ShareInfo[]>("/api/shares") });
}

export function useDeleteShare() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/shares/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["shares"] }),
  });
}
