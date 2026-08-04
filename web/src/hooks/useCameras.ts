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
  motionThreshold?: number;
  locationId?: string;
  prerollMs?: number;
  postrollMs?: number;
  cooldownMs?: number;
  minDurationMs?: number;
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

export function useReorderCameras() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (ids: string[]) => api.post("/api/cameras/reorder", { cameraIds: ids }),
    onMutate: async (ids) => {
      await qc.cancelQueries({ queryKey: ["cameras"] });
      const prev = qc.getQueryData<Camera[]>(["cameras"]);
      if (prev) {
        const byId = new Map(prev.map((c) => [c.id, c]));
        const reordered = ids.map((id) => byId.get(id)).filter(Boolean) as Camera[];
        // keep any cameras not in the id list (e.g. ACL edge) at the end
        const rest = prev.filter((c) => !ids.includes(c.id));
        qc.setQueryData(["cameras"], [...reordered, ...rest]);
      }
      return { prev };
    },
    onError: (_e, _ids, ctx) => {
      if (ctx?.prev) qc.setQueryData(["cameras"], ctx.prev);
    },
    onSettled: () => qc.invalidateQueries({ queryKey: ["cameras"] }),
  });
}

export interface CameraTestResult {
  ok: boolean;
  snapshot?: string; // base64 JPEG
  error?: string;
}

export function useTestCamera() {
  return useMutation({
    mutationFn: (rtspMainUrl: string) =>
      api.post<CameraTestResult>("/api/cameras/test", { rtspMainUrl }),
  });
}
