import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import clsx from "clsx";
import { RefreshCw, ScrollText, ShieldAlert, ShieldCheck } from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { CheckRow, SeverityBars, SidebarPage, SidebarSearch, SidebarSection, Segmented } from "../components/ui/Layout";
import { api } from "../lib/api";
import { useCameras } from "../hooks/useCameras";
import { fmtDateTime } from "../lib/format";

interface AuditEntry {
  id: number;
  ts: number;
  actorEmail: string;
  action: string;
  target: string;
  detail: string;
  ip: string;
}

interface AuditResponse {
  entries: AuditEntry[];
  actions: string[];
}

type Level = 1 | 2 | 3 | 4;

// How much an entry deserves attention, for the four-step severity meter: a
// refused or failed action is the top, destroying something next, and reads,
// sign-ins, playback and exports are informational. Anything else changed
// configuration, which sits in between.
function levelOf(action: string): Level {
  const a = action.toLowerCase();
  if (/(fail|denied|tamper|locked)/.test(a)) return 4;
  if (/(delete|purge|revoke|remove|disable|reset|restore)/.test(a)) return 3;
  if (/(login|logout|signin|signout|view|export|download|access|playback|clip|verify|read|list|test)/.test(a)) return 1;
  return 2;
}

const LEVEL_LABEL: Record<Level, string> = { 1: "Info", 2: "Change", 3: "Removal", 4: "Failure" };

// "camera.update" -> type "Camera", event "Update"; "storage.destinations" ->
// "Storage", "Destinations".
function words(s: string): string {
  return s
    .split(/[._-]+/)
    .filter(Boolean)
    .map((w) => (/^(2fa|api|ha|ip|oidc|onvif|mqtt|sso|ptz|hls)$/i.test(w) ? w.toUpperCase() : w[0].toUpperCase() + w.slice(1)))
    .join(" ");
}
function typeOf(action: string): string {
  return words(action.split(".")[0] || action);
}
function eventOf(action: string): string {
  const rest = action.split(".").slice(1).join(".");
  return rest ? words(rest) : words(action);
}

const RANGES = [
  { value: "1d", label: "1D", ms: 864e5 },
  { value: "3d", label: "3D", ms: 3 * 864e5 },
  { value: "1w", label: "1W", ms: 7 * 864e5 },
  { value: "2w", label: "2W", ms: 14 * 864e5 },
  { value: "1m", label: "1M", ms: 30 * 864e5 },
  { value: "all", label: "All", ms: 0 },
] as const;
type Range = (typeof RANGES)[number]["value"];

// Logs is the tamper-evident activity log as a full page: filters on the left,
// the entries as a table.
export function Logs() {
  const [action, setAction] = useState("");
  const [actor, setActor] = useState("");
  const [search, setSearch] = useState("");
  const [range, setRange] = useState<Range>("all");
  const [levels, setLevels] = useState<Set<Level>>(new Set());
  const [types, setTypes] = useState<Set<string>>(new Set());
  const [verify, setVerify] = useState<{ intact: boolean } | null>(null);
  const [verifying, setVerifying] = useState(false);

  const { data, isLoading, isFetching, refetch } = useQuery({
    queryKey: ["audit", action, actor],
    queryFn: () => {
      const p = new URLSearchParams();
      if (action) p.set("action", action);
      if (actor) p.set("actor", actor);
      p.set("limit", "500");
      return api.get<AuditResponse>(`/api/system/audit?${p.toString()}`);
    },
  });

  const entries = data?.entries ?? [];
  // Many entries name a camera by its ID; show its name where it still exists.
  const { data: cameras } = useCameras();
  const nameOf = (target: string) => cameras?.find((c) => c.id === target)?.name ?? target;
  const typeCounts = useMemo(() => {
    const m = new Map<string, number>();
    for (const e of entries) m.set(typeOf(e.action), (m.get(typeOf(e.action)) ?? 0) + 1);
    return [...m.entries()].sort((a, b) => a[0].localeCompare(b[0]));
  }, [entries]);

  const shown = useMemo(() => {
    const since = RANGES.find((r) => r.value === range)?.ms ?? 0;
    const cutoff = since ? Date.now() - since : 0;
    const q = search.trim().toLowerCase();
    return entries.filter((e) => {
      if (cutoff && e.ts * 1000 < cutoff) return false;
      if (levels.size && !levels.has(levelOf(e.action))) return false;
      if (types.size && !types.has(typeOf(e.action))) return false;
      if (q && !`${e.action} ${e.target} ${nameOf(e.target)} ${e.detail} ${e.actorEmail} ${e.ip}`.toLowerCase().includes(q)) return false;
      return true;
    });
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [entries, range, levels, types, search, cameras]);

  const toggle = <T,>(set: Set<T>, v: T, on: boolean) => {
    const n = new Set(set);
    if (on) n.add(v);
    else n.delete(v);
    return n;
  };
  const filtered = !!(search || range !== "all" || levels.size || types.size || action || actor);

  const sidebar = (
    <div className="space-y-1">
      <div className="flex items-center gap-2">
        <div className="min-w-0 flex-1">
          <SidebarSearch value={search} onChange={setSearch} />
        </div>
        <button className="btn-icon shrink-0" onClick={() => refetch()} aria-label="Refresh" title="Refresh">
          <RefreshCw className={clsx("h-4 w-4", isFetching && "animate-spin")} />
        </button>
      </div>
      <div className="pt-2">
        <Segmented value={range} onChange={setRange} full options={RANGES.map((r) => ({ value: r.value, label: r.label }))} />
      </div>
      <div className="grid grid-cols-4 gap-1.5 pt-2">
        {([1, 2, 3, 4] as Level[]).map((l) => (
          <button
            key={l}
            title={LEVEL_LABEL[l]}
            onClick={() => setLevels((s) => toggle(s, l, !s.has(l)))}
            className={clsx(
              "grid h-8 place-items-center rounded-md border transition-colors",
              levels.has(l) ? "border-brand bg-brand/10" : "border-line-strong hover:bg-white/[0.04]",
            )}
          >
            <SeverityBars level={l} />
          </button>
        ))}
      </div>

      <SidebarSection title="Types">
        {typeCounts.length === 0 ? (
          <p className="px-1 text-xs text-fg-faint">Nothing logged yet.</p>
        ) : (
          typeCounts.map(([t, n]) => (
            <CheckRow key={t} label={t} count={n} checked={types.has(t)} onChange={(v) => setTypes((s) => toggle(s, t, v))} />
          ))
        )}
      </SidebarSection>

      <SidebarSection title="Action" defaultOpen={!!action}>
        <select className="input" value={action} onChange={(e) => setAction(e.target.value)}>
          <option value="">All actions</option>
          {(data?.actions ?? []).map((a) => (
            <option key={a} value={a}>
              {a}
            </option>
          ))}
        </select>
      </SidebarSection>

      <SidebarSection title="User" defaultOpen={!!actor}>
        <input className="input" placeholder="Filter by user email…" value={actor} onChange={(e) => setActor(e.target.value)} />
      </SidebarSection>

      <SidebarSection title="Integrity">
        <p className="mb-2.5 text-xs leading-relaxed text-fg-faint">
          Every entry is hash-chained to the one before it, so an edited or deleted record breaks the chain.
        </p>
        <button
          className="btn-ghost w-full"
          disabled={verifying}
          onClick={async () => {
            setVerifying(true);
            try {
              setVerify(await api.get<{ intact: boolean }>("/api/system/audit/verify"));
            } finally {
              setVerifying(false);
            }
          }}
        >
          {verify ? (
            verify.intact ? (
              <>
                <ShieldCheck className="h-4 w-4 text-online" /> Chain intact
              </>
            ) : (
              <>
                <ShieldAlert className="h-4 w-4 text-danger" /> Chain broken
              </>
            )
          ) : (
            <>
              <ShieldCheck className="h-4 w-4" /> {verifying ? "Verifying…" : "Verify chain"}
            </>
          )}
        </button>
      </SidebarSection>

      {filtered && (
        <button
          className="btn-link mt-2 px-1"
          onClick={() => {
            setSearch("");
            setRange("all");
            setLevels(new Set());
            setTypes(new Set());
            setAction("");
            setActor("");
          }}
        >
          Clear filters
        </button>
      )}
    </div>
  );

  return (
    <>
      <TopBar title="Activity Log" />
      <SidebarPage sidebar={sidebar} storageKey="logs">
        <div className="min-h-0 flex-1 overflow-auto px-4 pb-6 lg:px-5">
          {isLoading ? (
            <div className="grid place-items-center py-20">
              <Spinner label="Loading activity" />
            </div>
          ) : shown.length === 0 ? (
            <EmptyState
              icon={ScrollText}
              title={filtered ? "Nothing matches these filters" : "No activity yet"}
              hint={
                filtered
                  ? "Try a longer time range or clear the filters."
                  : "Sign-ins, configuration changes, deletions, exports and footage access will be recorded here."
              }
            />
          ) : (
            <>
              <ul className="divide-y divide-line md:hidden">
                {shown.map((e) => (
                  <li key={e.id} className="py-3">
                    <div className="flex items-center gap-2">
                      <span className="text-sm font-medium text-fg">
                        {typeOf(e.action)} · {eventOf(e.action)}
                      </span>
                      <span className="ml-auto">
                        <SeverityBars level={levelOf(e.action)} />
                      </span>
                    </div>
                    {(e.target || e.detail) && (
                      <p className="mt-0.5 truncate text-sm text-fg/80">
                        {e.target && <span className="text-brand">{nameOf(e.target)}</span>}
                        {e.target && e.detail ? " · " : ""}
                        {e.detail}
                      </p>
                    )}
                    <p className="mt-0.5 text-xs text-fg-faint">
                      {fmtDateTime(e.ts * 1000)}
                      {e.actorEmail ? ` · ${e.actorEmail}` : ""}
                    </p>
                  </li>
                ))}
              </ul>
              <table className="tbl hidden min-w-[860px] md:table">
                <thead className="sticky top-0 z-10 bg-ink-900">
                  <tr>
                    <th>Type</th>
                    <th>Event</th>
                    <th>Description</th>
                    <th className="hidden xl:table-cell">User</th>
                    <th>Severity</th>
                    <th className="text-right">Date / Time</th>
                  </tr>
                </thead>
                <tbody>
                  {shown.map((e) => (
                    <tr key={e.id} className="hover:bg-white/[0.02]">
                      <td className="whitespace-nowrap">{typeOf(e.action)}</td>
                      <td className="whitespace-nowrap" title={e.action}>
                        {eventOf(e.action)}
                      </td>
                      <td className="max-w-[520px]">
                        <div className="truncate">
                          {e.target && (
                            <span className="text-brand" title={e.target}>
                              {nameOf(e.target)}
                            </span>
                          )}
                          {e.target && e.detail ? <span className="text-fg-faint"> · </span> : null}
                          {e.detail && <span>{e.detail}</span>}
                          {!e.target && !e.detail && <span className="text-fg-faint">—</span>}
                        </div>
                      </td>
                      <td className="hidden whitespace-nowrap xl:table-cell">
                        <span className="text-fg/80">{e.actorEmail || "—"}</span>
                        {e.ip && <span className="ml-2 text-xs text-fg-faint">{e.ip}</span>}
                      </td>
                      <td>
                        <span title={LEVEL_LABEL[levelOf(e.action)]}>
                          <SeverityBars level={levelOf(e.action)} />
                        </span>
                      </td>
                      <td className="whitespace-nowrap text-right tabular-nums">{fmtDateTime(e.ts * 1000)}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
              <p className="py-5 text-center text-sm text-fg-faint">
                {shown.length === entries.length
                  ? `Showing all ${entries.length} results`
                  : `Showing ${shown.length} of ${entries.length} results`}
              </p>
            </>
          )}
        </div>
      </SidebarPage>
    </>
  );
}
