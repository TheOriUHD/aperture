import { useQuery } from "@tanstack/react-query";
import { api } from "../lib/api";
import type { ExtNavItem } from "./types";

// The pinned extensions the nav rail renders. Separate from useExtPanels so the
// always-mounted rail pulls in nothing but this tiny query.
export function useExtNav() {
  return useQuery({
    queryKey: ["ext-nav"],
    queryFn: () => api.get<ExtNavItem[]>("/api/ext/nav"),
    staleTime: 30_000,
  });
}
