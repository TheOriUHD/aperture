import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { ExtPanel } from "./panels";

// useExtPanels lists the UI panels of enabled extensions (any authenticated user).
// Empty when the extension subsystem gate is off, so every ExtensionSlot renders
// nothing and the first-party UI is unchanged.
export function useExtPanels() {
  return useQuery({
    queryKey: ["ext-panels"],
    queryFn: () => api.get<ExtPanel[]>("/api/ext/panels"),
    staleTime: 30_000,
  });
}
