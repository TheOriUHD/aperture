import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { Camera } from "../lib/types";

export function useCameras() {
  return useQuery({
    queryKey: ["cameras"],
    queryFn: () => api.get<Camera[]>("/api/cameras"),
  });
}

export interface CameraInput {
  name: string;
  rtspMainUrl: string;
  rtspSubUrl?: string;
  recordMode?: Camera["recordMode"];
  retentionDays?: number;
  enabled?: boolean;
}

export function useCreateCamera() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: CameraInput) => api.post<Camera>("/api/cameras", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cameras"] }),
  });
}

export function useUpdateCamera() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: CameraInput & { id: string }) =>
      api.put<Camera>(`/api/cameras/${id}`, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cameras"] }),
  });
}

export function useDeleteCamera() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/cameras/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["cameras"] }),
  });
}
