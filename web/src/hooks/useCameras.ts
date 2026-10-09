import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { Camera } from "../lib/types";

export function useCameras() {
  return useQuery({
    queryKey: ["cameras"],
    queryFn: () => api.get<Camera[]>("/api/cameras"),
  });
}

// visibleCameras is the set a viewer should ever SEE. A disabled camera is
// absent from the dashboard, playback, multi-view and the export picker rather
// than drawn as a dead tile labelled "Disabled": it streams nothing and records
// nothing, so a placeholder only takes up space.
//
// Camera MANAGEMENT deliberately keeps the full list from useCameras(): Settings
// -> Cameras is where a disabled camera is found and switched back on, and the
// ACL, token-grant, group, storage and automation pickers must be able to
// configure one before it is turned on.
export function visibleCameras(cameras: Camera[] | undefined): Camera[] | undefined {
  return cameras?.filter((c) => c.enabled);
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

// An edit sends only what changed. In particular a link is sent only when it was
// edited: one left out keeps exactly what is saved, login included.
export type CameraUpdate = Partial<CameraInput> & { id: string };

export function useUpdateCamera() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, ...input }: CameraUpdate) =>
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

// cameraId lets the server fill a masked link's saved login back in for the test.
export function useTestCamera() {
  return useMutation({
    mutationFn: (req: { rtspMainUrl: string; cameraId?: string }) =>
      api.post<CameraTestResult>("/api/cameras/test", req),
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
  host: string;
  // Whether an account is saved; the values themselves never leave the server.
  hasUser: boolean;
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

// Polled, because the spotlight also changes without us: motion can trigger it,
// the camera's own schedule can, and so can the vendor app. Server-side probe and
// login caching make this read cheap enough to poll at this rate.
export function useCameraVendor(cameraId: string | undefined, enabled = true) {
  return useQuery({
    queryKey: ["camera-vendor", cameraId],
    queryFn: () => api.get<CameraVendor>(`/api/cameras/${cameraId}/vendor`),
    enabled: !!cameraId && enabled,
    // DELIBERATELY NOT POLLED. Unlike every other query here, this one does not read
    // Aperture's database — it reaches out to the CAMERA over the vendor's own HTTP
    // API. A Reolink serves very few concurrent sessions, and polling it every few
    // seconds alongside the RTSP pull that recording depends on is enough to wedge
    // the camera: it keeps answering pings while its RTSP stops delivering, and
    // recording stops. Nothing here changes on its own — the mutation below writes
    // the applied state straight back into this cache — so there is nothing to poll
    // for. A change made in the vendor's own app is picked up on the next mount.
    staleTime: 60_000,
    refetchOnWindowFocus: false,
    retry: false, // never retry against a camera that is already struggling
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
    // Reflect the change immediately: the request takes about a second against a
    // camera, and a light switch that visibly lags feels broken. onSuccess below
    // replaces the optimistic value with what the camera actually applied, and
    // onError rolls it back — that is what keeps it honest (this query is not
    // polled, on purpose; see above).
    onMutate: async (v) => {
      const key = ["camera-vendor", v.id];
      await qc.cancelQueries({ queryKey: key });
      const prev = qc.getQueryData<CameraVendor>(key);
      if (prev?.spotlight && v.spotlight) {
        qc.setQueryData<CameraVendor>(key, {
          ...prev,
          spotlight: { ...prev.spotlight, ...v.spotlight },
        });
      }
      if (prev?.ir && v.ir) {
        qc.setQueryData<CameraVendor>(key, { ...prev, ir: { state: v.ir } });
      }
      return { prev };
    },
    onError: (_e, v, ctx) => {
      if (ctx?.prev) qc.setQueryData(["camera-vendor", v.id], ctx.prev);
    },
    onSuccess: (d, v) => qc.setQueryData(["camera-vendor", v.id], d),
  });
}
