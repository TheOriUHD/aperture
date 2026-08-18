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

// Camera-side ONVIF: the picture settings the camera itself exposes. Optional —
// a camera that doesn't answer simply reports why, and nothing else is affected.
export interface OnvifRange {
  min: number;
  max: number;
}
export interface OnvifImaging {
  brightness?: number;
  contrast?: number;
  saturation?: number;
  sharpness?: number;
  irCutFilter?: string;
  ranges: Record<string, OnvifRange>;
  irCutOptions?: string[];
}
export interface OnvifProbe {
  ok: boolean;
  port: number;
  device: { manufacturer: string; model: string; firmware: string; serial: string };
  hasImaging: boolean;
  hasPtz: boolean;
  videoSource: string;
  error?: string;
}
export interface CameraOnvif {
  enabled: boolean;
  port: number;
  user: string;
  host: string;
  hasPassword: boolean;
  probe?: OnvifProbe;
  imaging?: OnvifImaging;
  imagingError?: string;
}

export function useCameraOnvif(cameraId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["camera-onvif", cameraId],
    queryFn: () => api.get<CameraOnvif>(`/api/cameras/${cameraId}/onvif`),
    enabled: !!cameraId && enabled,
    staleTime: 10_000,
  });
}

export function useSetCameraOnvif() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: { id: string; enabled?: boolean; port?: number; user?: string; password?: string }) =>
      api.put<CameraOnvif>(`/api/cameras/${id}/onvif`, body),
    onSuccess: (d, v) => qc.setQueryData(["camera-onvif", v.id], d),
  });
}

// Applies picture settings to the camera itself.
export function useSetCameraImaging() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: {
      id: string;
      brightness?: number;
      contrast?: number;
      saturation?: number;
      sharpness?: number;
      irCutFilter?: string;
    }) => api.put<CameraOnvif>(`/api/cameras/${id}/onvif/imaging`, body),
    onSuccess: (d, v) => qc.setQueryData(["camera-onvif", v.id], d),
  });
}

// Vendor-specific camera extras (Reolink LED spotlight / IR illuminators) —
// controls the ONVIF standard doesn't model. Only present for cameras the ONVIF
// probe identified as that vendor.
export interface CameraVendor {
  vendor: string;
  supported: boolean;
  spotlight?: { state: number; mode: number; bright: number };
  ir?: { state: string };
  error?: string;
}

export function useCameraVendor(cameraId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["camera-vendor", cameraId],
    queryFn: () => api.get<CameraVendor>(`/api/cameras/${cameraId}/vendor`),
    enabled: !!cameraId && enabled,
    staleTime: 10_000,
  });
}

export function useSetCameraVendor() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...body }: {
      id: string;
      spotlight?: { state?: number; mode?: number; bright?: number };
      ir?: string;
    }) => api.put<CameraVendor>(`/api/cameras/${id}/vendor`, body),
    onSuccess: (d, v) => qc.setQueryData(["camera-vendor", v.id], d),
  });
}
