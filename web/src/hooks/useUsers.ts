import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { Role } from "../lib/types";

export interface ManagedUser {
  id: string;
  email: string;
  name: string;
  role: Role;
  disabled: boolean;
  cameraIds: string[];
  etag?: string; // per-item concurrency tag from the list; sent as If-Match, never in a body
}

export interface UserInput {
  email?: string;
  name?: string;
  role?: Role;
  password?: string;
  disabled?: boolean;
  cameraIds?: string[];
}

export function useUsers() {
  return useQuery({ queryKey: ["users"], queryFn: () => api.get<ManagedUser[]>("/api/users") });
}

export function useCreateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (input: UserInput) => api.post<ManagedUser>("/api/users", input),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useUpdateUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: ({ id, etag, ...input }: UserInput & { id: string; etag?: string }) =>
      api.put<ManagedUser>(`/api/users/${id}`, input, { ifMatch: etag }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}

export function useDeleteUser() {
  const qc = useQueryClient();
  return useMutation({
    mutationFn: (id: string) => api.del(`/api/users/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });
}
