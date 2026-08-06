import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export interface Channel {
  id: string;
  type: string;
  name: string;
  enabled: boolean;
  createdAt: number;
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, never in a body
}

export interface RuleConditions {
  timeOfDay?: "" | "day" | "night";
  onlyPrecip?: boolean;
  objects?: string[]; // require one of these detected object labels (e.g. "person")
}

export interface Rule {
  id: string;
  name: string;
  enabled: boolean;
  cameras: string[];
  eventTypes: string[];
  channels: string[];
  quietStart: number;
  quietEnd: number;
  cooldownSec: number;
  conditions?: RuleConditions;
  createdAt: number;
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, never in a body
}

export function useChannels() {
  return useQuery({ queryKey: ["channels"], queryFn: () => api.get<Channel[]>("/api/notify/channels") });
}
export function useCreateChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { type: string; name: string; config: Record<string, string> }) =>
      api.post<Channel>("/api/notify/channels", b),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
}
export function useUpdateChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (b: { id: string; name: string; enabled: boolean; etag?: string }) =>
      api.put<Channel>(`/api/notify/channels/${b.id}`, { name: b.name, enabled: b.enabled }, { ifMatch: b.etag }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
}
export function useDeleteChannel() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/notify/channels/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["channels"] }),
  });
}
export function useTestChannel() {
  return useMutation({ mutationFn: (id: string) => api.post(`/api/notify/channels/${id}/test`) });
}

export function useRules() {
  return useQuery({ queryKey: ["rules"], queryFn: () => api.get<Rule[]>("/api/notify/rules") });
}
export function useSaveRule() {
  const qc = useQueryClient();
  return useMutation({
    // id travels in the URL and etag in the If-Match header — both trip the
    // server's DisallowUnknownFields if sent in the body. On create there is no
    // etag yet, so it posts without If-Match.
    mutationFn: ({ id, etag, ...body }: Partial<Rule> & { id?: string }) =>
      id ? api.put<Rule>(`/api/notify/rules/${id}`, body, { ifMatch: etag }) : api.post<Rule>("/api/notify/rules", body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rules"] }),
  });
}
export function useDeleteRule() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/notify/rules/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["rules"] }),
  });
}
export function useTestRule() {
  return useMutation({ mutationFn: (id: string) => api.post(`/api/notify/rules/${id}/test`) });
}
