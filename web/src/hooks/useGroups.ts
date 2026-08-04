import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export interface CameraGroup {
  id: string;
  name: string;
  cameraIds: string[];
  sortOrder: number;
  createdAt: number;
  updatedAt: number;
}

export function useCameraGroups() {
  return useQuery({
    queryKey: ["camera-groups"],
    queryFn: () => api.get<CameraGroup[]>("/api/camera-groups"),
    staleTime: 30_000,
  });
}

export function useCreateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { name: string; cameraIds: string[] }) =>
      api.post<CameraGroup>("/api/camera-groups", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["camera-groups"] }),
  });
}

export function useUpdateGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: { id: string; name?: string; cameraIds?: string[]; sortOrder?: number }) =>
      api.put<CameraGroup>(`/api/camera-groups/${id}`, input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["camera-groups"] }),
  });
}

export function useDeleteGroup() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/camera-groups/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["camera-groups"] }),
  });
}

// Set exactly which groups a camera belongs to (the camera-side assignment path).
export function useSetCameraGroups() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ cameraId, groupIds }: { cameraId: string; groupIds: string[] }) =>
      api.put(`/api/cameras/${cameraId}/groups`, { groupIds }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["camera-groups"] }),
  });
}
