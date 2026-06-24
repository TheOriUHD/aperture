import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";

export interface Channel {
  id: string;
  type: string;
  name: string;
  enabled: boolean;
  createdAt: number;
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
  createdAt: number;
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
    mutationFn: (b: { id: string; name: string; enabled: boolean }) =>
      api.put<Channel>(`/api/notify/channels/${b.id}`, { name: b.name, enabled: b.enabled }),
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
    mutationFn: (b: Partial<Rule> & { id?: string }) =>
      b.id ? api.put<Rule>(`/api/notify/rules/${b.id}`, b) : api.post<Rule>("/api/notify/rules", b),
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
