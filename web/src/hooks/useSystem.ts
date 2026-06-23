import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export function useSiteName() {
  return useQuery({
    queryKey: ["site-name"],
    queryFn: () => api.get<{ siteName: string }>("/api/system/settings"),
    staleTime: 60_000,
  });
}

export function useSetSiteName() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (siteName: string) => api.put<{ siteName: string }>("/api/system/settings", { siteName }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site-name"] }),
  });
}

export interface PurgeResult {
  deletedSegments: number;
  deletedEvents: number;
  freedBytes: number;
}

export interface HAStream {
  cameraId: string;
  name: string;
  rtsp: string;
}
export interface HAConfig {
  enabled: boolean;
  broker: string;
  username: string;
  prefix: string;
  hasPassword: boolean;
  connected: boolean;
  streams: HAStream[];
}

export function useHA() {
  return useQuery({
    queryKey: ["ha"],
    queryFn: () => api.get<HAConfig>("/api/system/ha"),
    refetchInterval: 8000,
  });
}

export function useSetHA() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      enabled: boolean;
      broker: string;
      username: string;
      password?: string;
      prefix: string;
    }) => api.put<HAConfig>("/api/system/ha", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["ha"] }),
  });
}

export interface OIDCConfig {
  enabled: boolean;
  issuer: string;
  clientId: string;
  domain: string;
  autoCreate: boolean;
  name: string;
  hasSecret: boolean;
  redirectUrl: string;
}

export function useOIDC() {
  return useQuery({
    queryKey: ["oidc"],
    queryFn: () => api.get<OIDCConfig>("/api/system/oidc"),
    staleTime: 30_000,
  });
}

export function useSetOIDC() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: {
      enabled: boolean;
      issuer: string;
      clientId: string;
      clientSecret?: string;
      domain: string;
      autoCreate: boolean;
      name: string;
    }) => api.put<OIDCConfig>("/api/system/oidc", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["oidc"] }),
  });
}

export function usePurge() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: { cameraId?: string; from: number; to: number }) =>
      api.post<PurgeResult>("/api/system/purge", input),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["storage"] });
      qc.invalidateQueries({ queryKey: ["segments"] });
      qc.invalidateQueries({ queryKey: ["events"] });
      qc.invalidateQueries({ queryKey: ["events-all"] });
      qc.invalidateQueries({ queryKey: ["events-today"] });
    },
  });
}
