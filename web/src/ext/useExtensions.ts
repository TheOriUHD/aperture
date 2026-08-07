import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api, uploadFile } from "../lib/api";
import type { ExtItem, SettingsView, StageView, StoreView, UpdateInfo } from "./types";

// The extension manager's data layer. Every mutation that can change what renders
// elsewhere (panels, the rail) invalidates those queries too, so the Dashboard
// card, settings panels, and nav rail stay in sync with the manager without a
// reload.

export function useExtensions() {
  return useQuery({ queryKey: ["ext"], queryFn: () => api.get<ExtItem[]>("/api/ext") });
}

export function useExtStore(enabled: boolean) {
  return useQuery({
    queryKey: ["ext-store"],
    queryFn: () => api.get<StoreView>("/api/ext/store"),
    enabled,
    staleTime: 60_000,
    retry: 0, // a store misconfig shouldn't retry-storm; surface it once
  });
}

function useInvalidateExt() {
  const qc = useQueryClient();
  return () => {
    qc.invalidateQueries({ queryKey: ["ext"] });
    qc.invalidateQueries({ queryKey: ["ext-panels"] });
    qc.invalidateQueries({ queryKey: ["ext-nav"] });
    qc.invalidateQueries({ queryKey: ["ext-store"] });
  };
}

/** All the row actions as one object of mutations, so a row can stay compact. */
export function useExtActions() {
  const invalidate = useInvalidateExt();
  const opts = { onSuccess: invalidate };

  return {
    enable: useMutation({ mutationFn: (id: string) => api.post(`/api/ext/${id}/enable`), ...opts }),
    disable: useMutation({ mutationFn: (id: string) => api.post(`/api/ext/${id}/disable`), ...opts }),
    uninstall: useMutation({ mutationFn: (id: string) => api.del(`/api/ext/${id}`), ...opts }),
    pin: useMutation({ mutationFn: (id: string) => api.post(`/api/ext/${id}/pin`), ...opts }),
    unpin: useMutation({ mutationFn: (id: string) => api.del(`/api/ext/${id}/pin`), ...opts }),
    revert: useMutation({ mutationFn: (id: string) => api.post(`/api/ext/${id}/revert`), ...opts }),
    setCameras: useMutation({
      mutationFn: (v: { id: string; cameras: string[] | null }) =>
        api.put(`/api/ext/${v.id}/cameras`, { cameras: v.cameras }),
      ...opts,
    }),
    applyUpdate: useMutation({
      mutationFn: (v: { id: string; stagingId: string; consented: boolean }) =>
        api.post(`/api/ext/${v.id}/update`, { stagingId: v.stagingId, consented: v.consented }),
      ...opts,
    }),
  };
}

/** An extension's settings: the schema + current non-secret values + which secrets
 * are set. Loaded lazily (when a row is expanded). */
export function useExtSettings(id: string, enabled: boolean) {
  return useQuery({
    queryKey: ["ext-settings", id],
    queryFn: () => api.get<SettingsView>(`/api/ext/${id}/settings`),
    enabled,
  });
}

/** Save an extension's settings. The server validates against the schema and
 * restarts the backend so it re-reads them, so this also refreshes the row (state)
 * and the settings query. */
export function useSaveExtSettings() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (v: { id: string; values: Record<string, unknown> }) =>
      api.put<SettingsView>(`/api/ext/${v.id}/settings`, { values: v.values }),
    onSuccess: (_data, v) => {
      qc.invalidateQueries({ queryKey: ["ext-settings", v.id] });
      qc.invalidateQueries({ queryKey: ["ext"] });
    },
  });
}

/** Staging (never touches the cache until confirm): upload a file, fetch a URL,
 * or pick a store item — each returns the consent view the modal shows. */
export function stageUpload(file: File) {
  return uploadFile<StageView>("/api/ext/install", "bundle", file);
}
export function stageUrl(url: string) {
  return api.post<StageView>("/api/ext/install/url", { url });
}
export function stageStore(id: string) {
  return api.post<StageView>("/api/ext/store/install", { id });
}
export function confirmInstall(stagingId: string, cameras?: string[] | null, allowDowngrade?: boolean) {
  // cameras: an explicit id list = grant exactly those; null/undefined = as the
  // manifest requested (all, or its list). The server validates against the ceiling.
  // If the id is already installed the server treats this as an UPDATE (preserving
  // ext_kv + camera grant); allowDowngrade authorizes a lower version.
  return api.post<ExtItem>("/api/ext/install/confirm", { stagingId, cameras, allowDowngrade });
}

export function checkUpdate(id: string) {
  return api.get<UpdateInfo>(`/api/ext/${id}/update`);
}
