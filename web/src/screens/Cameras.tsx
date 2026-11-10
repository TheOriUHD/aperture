import { useEffect, useMemo, useState } from "react";
import { useNavigate, useSearchParams } from "react-router-dom";
import { useQueryClient } from "@tanstack/react-query";
import clsx from "clsx";
import {
  Binoculars,
  Camera as CameraIcon,
  Cctv,
  LayoutGrid,
  List,
  Play,
  Plus,
  Settings,
} from "lucide-react";
import { TopBar } from "../components/TopBar";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { CameraModal } from "../components/CameraModal";
import { LiveVideo } from "../components/LiveVideo";
import {
  CheckRow,
  DetailSection,
  InfoList,
  SidePanel,
  SidebarPage,
  SidebarSearch,
  SidebarSection,
  Segmented,
  StatusDot,
} from "../components/ui/Layout";
import { useAuth } from "../lib/auth";
import { useCameras } from "../hooks/useCameras";
import { useCameraGroups } from "../hooks/useGroups";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { useSystemStatus, type CameraHealth } from "../hooks/useSystem";
import { useStorageLocations } from "../hooks/useStorageLocations";
import { isProtectLink } from "../lib/cameraLink";
import { fmtRelative } from "../lib/format";
import type { Camera, RecordMode } from "../lib/types";

const MODE_LABEL: Record<RecordMode, string> = {
  never: "Off",
  continuous: "Continuous",
  motion: "Motion",
  scheduled: "Scheduled",
};

type Health = "online" | "offline" | "disabled";
type Rec = "recording" | "idle" | "live" | "failed";

// The camera's network address, read from its (masked) stream link.
function hostOf(link: string): string {
  const m = /^[a-z][a-z0-9+.-]*:\/\/(?:[^@/]*@)?(\[[^\]]+\]|[^/:?#]+)(?::(\d+))?/i.exec(link || "");
  return m ? m[1] : "";
}
function sourceOf(c: Camera): string {
  // Saved links arrive with the stream key masked, which isProtectLink (written for
  // freshly pasted links) does not accept; the console port identifies them too.
  if (isProtectLink(c.rtspMainUrl) || /^rtsp[sx]?:\/\/[^/?#]+:(7441|7447)\/\u2022+/i.test(c.rtspMainUrl)) return "UniFi Protect";
  if ((c.vendor ?? "") === "reolink") return "Reolink";
  return "RTSP";
}
function healthOf(c: Camera, h?: CameraHealth): Health {
  if (!c.enabled) return "disabled";
  if (h && !h.liveOk) return "offline";
  return "online";
}
function recOf(c: Camera, h?: CameraHealth): Rec {
  const s = h?.recordingState;
  if (s === "failed") return "failed";
  if (s === "recording" || (!s && c.recordingNow)) return "recording";
  if (s === "notScheduled") return "idle";
  return "live";
}
const REC_LABEL: Record<Rec, string> = {
  recording: "Recording",
  idle: "Idle (outside schedule)",
  live: "Live view only",
  failed: "Not recording",
};
function qualityOf(c: Camera): string {
  const parts: string[] = [];
  if (c.recordHeight) parts.push(`${c.recordHeight}p`);
  if (c.recordFps) parts.push(`${c.recordFps} fps`);
  if (c.recordKbps) parts.push(c.recordKbps >= 1000 ? `${c.recordKbps / 1000} Mbps` : `${c.recordKbps} kbps`);
  return parts.length ? parts.join(" · ") : "Original";
}
function tierOf(r?: { w: number; h: number }): string {
  if (!r) return "—";
  const h = Math.max(r.h, Math.round((r.w * 9) / 16));
  const tier = h <= 720 ? "HD" : h <= 1080 ? "Full HD" : h <= 1520 ? "2K" : "4K";
  return `${tier} (${r.w}×${r.h})`;
}

// Picture size of each enabled camera, read off a snapshot.
function useSnapshotSizes(cameras: Camera[]) {
  const [res, setRes] = useState<Record<string, { w: number; h: number }>>({});
  const key = cameras.map((c) => `${c.id}:${c.enabled}`).join(",");
  useEffect(() => {
    const imgs: HTMLImageElement[] = [];
    for (const c of cameras) {
      if (!c.enabled) continue;
      const img = new Image();
      img.onload = () => setRes((r) => ({ ...r, [c.id]: { w: img.naturalWidth, h: img.naturalHeight } }));
      img.src = `/api/v1/cameras/${c.id}/snapshot.jpg`;
      imgs.push(img);
    }
    return () => imgs.forEach((i) => (i.onload = null));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [key]);
  return res;
}

// CamerasManager is the device list: filters on the left, every camera as a table
// row (or a snapshot card), and a side panel with the selected camera's overview
// and settings.
export function CamerasManager() {
  const navigate = useNavigate();
  const { user } = useAuth();
  const isAdmin = user?.role === "admin";
  const { data: cameras, isLoading } = useCameras();
  const { data: status } = useSystemStatus();
  const { data: groups } = useCameraGroups();
  const { data: locData } = useStorageLocations();
  const [params, setParams] = useSearchParams();
  const qc = useQueryClient();

  const all = cameras ?? [];
  const sizes = useSnapshotSizes(all);
  const healthBy = useMemo(() => {
    const m: Record<string, CameraHealth> = {};
    for (const h of status?.cameras ?? []) m[h.cameraId] = h;
    return m;
  }, [status]);

  const [search, setSearch] = useState("");
  const [view, setView] = useState<"list" | "grid">(() => {
    try {
      return localStorage.getItem("aperture.devices.view") === "grid" ? "grid" : "list";
    } catch {
      return "list";
    }
  });
  const wide = useMediaQuery("(min-width: 768px)");
  const layout = wide ? view : "grid";
  const [fHealth, setFHealth] = useState<Set<Health>>(new Set());
  const [fRec, setFRec] = useState<Set<Rec>>(new Set());
  const [fSource, setFSource] = useState<Set<string>>(new Set());
  const [fGroup, setFGroup] = useState<Set<string>>(new Set());

  // Selection lives in the URL so a camera can be linked to directly.
  const selectedId = params.get("camera");
  const selected = all.find((c) => c.id === selectedId) ?? null;
  const [tab, setTab] = useState<"overview" | "settings">("overview");
  // The editor works on a copy taken when it opens, so the 60-second camera
  // refresh can't wipe edits in progress; saving or reverting takes a new copy.
  const [editKey, setEditKey] = useState(0);
  const [editing, setEditing] = useState<Camera | null>(null);
  useEffect(() => {
    if (selected && (!editing || editing.id !== selected.id)) setEditing(selected);
    if (!selected) setEditing(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selected?.id]);

  // A camera deleted (here or elsewhere) closes its panel.
  useEffect(() => {
    if (selectedId && cameras && !cameras.some((c) => c.id === selectedId)) select(null);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [selectedId, cameras]);

  const select = (id: string | null) => {
    const p = new URLSearchParams(params);
    if (id) p.set("camera", id);
    else p.delete("camera");
    setParams(p, { replace: true });
    if (!id) setTab("overview");
  };

  const [addOpen, setAddOpen] = useState(params.get("add") === "1" && isAdmin);
  useEffect(() => {
    if (params.get("add") === "1") {
      const p = new URLSearchParams(params);
      p.delete("add");
      setParams(p, { replace: true });
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, []);

  const flip = <T,>(set: Set<T>, v: T, on: boolean) => {
    const n = new Set(set);
    if (on) n.add(v);
    else n.delete(v);
    return n;
  };

  const rows = useMemo(
    () =>
      all.map((c) => ({
        cam: c,
        health: healthOf(c, healthBy[c.id]),
        rec: recOf(c, healthBy[c.id]),
        source: sourceOf(c),
        host: hostOf(c.rtspMainUrl),
      })),
    [all, healthBy],
  );
  const shown = useMemo(() => {
    const q = search.trim().toLowerCase();
    return rows.filter((r) => {
      if (fHealth.size && !fHealth.has(r.health)) return false;
      if (fRec.size && !fRec.has(r.rec)) return false;
      if (fSource.size && !fSource.has(r.source)) return false;
      if (fGroup.size && !(groups ?? []).some((g) => fGroup.has(g.id) && g.cameraIds.includes(r.cam.id))) return false;
      if (q && !`${r.cam.name} ${r.host} ${r.source}`.toLowerCase().includes(q)) return false;
      return true;
    });
  }, [rows, search, fHealth, fRec, fSource, fGroup, groups]);
  const n = <K extends "health" | "rec" | "source">(k: K, v: string) => rows.filter((r) => r[k] === v).length;
  const sources = [...new Set(rows.map((r) => r.source))].sort();
  const filtered = !!(search || fHealth.size || fRec.size || fSource.size || fGroup.size);

  const sidebar = (
    <div>
      <div className="flex items-center gap-2">
        <div className="min-w-0 flex-1">
          <SidebarSearch value={search} onChange={setSearch} />
        </div>
        <Segmented
          className="hidden md:inline-flex [&_.seg-item]:px-2"
          value={view}
          onChange={(v) => {
            setView(v);
            try {
              localStorage.setItem("aperture.devices.view", v);
            } catch {
              /* private mode */
            }
          }}
          options={[
            { value: "list", label: <List className="h-4 w-4" />, title: "List" },
            { value: "grid", label: <LayoutGrid className="h-4 w-4" />, title: "Grid" },
          ]}
        />
      </div>
      {isAdmin && (
        <button className="btn-outline mt-3 w-full" onClick={() => setAddOpen(true)}>
          <Plus className="h-4 w-4" />
          Add camera
        </button>
      )}
      <div className="mt-2">
        <SidebarSection title="Status">
          {(["online", "offline", "disabled"] as Health[]).map((h) => (
            <CheckRow
              key={h}
              label={h === "online" ? "Online" : h === "offline" ? "Offline" : "Disabled"}
              count={n("health", h)}
              checked={fHealth.has(h)}
              onChange={(v) => setFHealth((s) => flip(s, h, v))}
            />
          ))}
        </SidebarSection>
        <SidebarSection title="Recording">
          {(["recording", "idle", "live", "failed"] as Rec[]).map((r) => (
            <CheckRow
              key={r}
              label={r === "recording" ? "Recording" : r === "idle" ? "Idle" : r === "live" ? "Live only" : "Not recording"}
              count={n("rec", r)}
              checked={fRec.has(r)}
              onChange={(v) => setFRec((s) => flip(s, r, v))}
            />
          ))}
        </SidebarSection>
        {sources.length > 1 && (
          <SidebarSection title="Source">
            {sources.map((s) => (
              <CheckRow key={s} label={s} count={n("source", s)} checked={fSource.has(s)} onChange={(v) => setFSource((x) => flip(x, s, v))} />
            ))}
          </SidebarSection>
        )}
        {(groups?.length ?? 0) > 0 && (
          <SidebarSection title="Groups">
            {groups!.map((g) => (
              <CheckRow
                key={g.id}
                label={g.name}
                count={g.cameraIds.filter((id) => all.some((c) => c.id === id)).length}
                checked={fGroup.has(g.id)}
                onChange={(v) => setFGroup((s) => flip(s, g.id, v))}
              />
            ))}
          </SidebarSection>
        )}
      </div>
      {filtered && (
        <button
          className="btn-link mt-3 px-1"
          onClick={() => {
            setSearch("");
            setFHealth(new Set());
            setFRec(new Set());
            setFSource(new Set());
            setFGroup(new Set());
          }}
        >
          Clear filters
        </button>
      )}
    </div>
  );

  const selHealth = selected ? healthOf(selected, healthBy[selected.id]) : "online";
  const selRec = selected ? recOf(selected, healthBy[selected.id]) : "live";
  const selH = selected ? healthBy[selected.id] : undefined;
  const locName = (id?: string) =>
    (locData?.locations ?? []).find((l) => l.id === (id ?? ""))?.name ?? (id ? "—" : "Default");

  return (
    <SidebarPage sidebar={sidebar} storageKey="devices">
      <div className="min-h-0 flex-1 overflow-auto px-4 pb-6 lg:px-5">
        {isLoading ? (
          <div className="grid place-items-center py-20">
            <Spinner label="Loading cameras" />
          </div>
        ) : all.length === 0 ? (
          <EmptyState
            icon={Cctv}
            title="No cameras configured"
            hint={isAdmin ? "Add an RTSP camera, or a UniFi Protect stream, to begin." : "Ask an administrator to add cameras."}
            action={
              isAdmin ? (
                <button className="btn-primary" onClick={() => setAddOpen(true)}>
                  <Plus className="h-4 w-4" />
                  Add camera
                </button>
              ) : undefined
            }
          />
        ) : shown.length === 0 ? (
          <EmptyState icon={Cctv} title="No cameras match these filters" hint="Clear a filter to see more." />
        ) : layout === "list" ? (
          <table className="tbl tbl-hover min-w-[760px]">
            <thead className="sticky top-0 z-10 bg-ink-900">
              <tr>
                <th className="w-12">Type</th>
                <th>Name</th>
                <th>Status</th>
                <th className="hidden xl:table-cell">Source</th>
                <th className="hidden md:table-cell">IP Address</th>
                <th className="hidden 2xl:table-cell">Resolution</th>
                <th>Recording</th>
                <th>Recording Mode</th>
              </tr>
            </thead>
            <tbody>
              {shown.map((r) => (
                <tr
                  key={r.cam.id}
                  onClick={() => select(r.cam.id === selectedId ? null : r.cam.id)}
                  className={clsx(r.cam.id === selectedId && "is-selected", !r.cam.enabled && "text-fg/50")}
                >
                  <td>
                    <span className="flex items-center gap-2.5">
                      <StatusDot tone={r.health === "online" ? "ok" : r.health === "offline" ? "bad" : "off"} />
                      <Cctv className="h-4 w-4 text-fg-muted" />
                    </span>
                  </td>
                  <td className="font-medium text-fg">{r.cam.name}</td>
                  <td>{r.health === "online" ? "Online" : r.health === "offline" ? "Offline" : "Disabled"}</td>
                  <td className="hidden xl:table-cell">{r.source}</td>
                  <td className="hidden font-mono text-[13px] md:table-cell">{r.host || "—"}</td>
                  <td className="hidden 2xl:table-cell">{tierOf(sizes[r.cam.id])}</td>
                  <td>
                    <RecLabel rec={r.rec} />
                  </td>
                  <td>
                    {MODE_LABEL[r.cam.recordMode]}
                    {(r.cam.recordHeight || r.cam.recordFps || r.cam.recordKbps) ? (
                      <span className="ml-1.5 text-xs text-fg-faint">{qualityOf(r.cam)}</span>
                    ) : null}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        ) : (
          <div className="grid gap-3 pt-4 [grid-template-columns:repeat(auto-fill,minmax(260px,1fr))]">
            {shown.map((r) => (
              <button
                key={r.cam.id}
                onClick={() => select(r.cam.id === selectedId ? null : r.cam.id)}
                className={clsx(
                  "group overflow-hidden rounded-lg border bg-ink-800 text-left transition-colors",
                  r.cam.id === selectedId ? "border-brand" : "border-transparent hover:border-line-strong",
                )}
              >
                <div className="relative aspect-video bg-ink-950">
                  {r.cam.enabled ? (
                    <img src={`/api/v1/cameras/${r.cam.id}/snapshot.jpg`} alt="" className="h-full w-full object-cover" loading="lazy" />
                  ) : (
                    <div className="grid h-full place-items-center text-xs text-fg-faint">Disabled</div>
                  )}
                </div>
                <div className="flex items-center gap-2 px-3 py-2.5">
                  <StatusDot tone={r.health === "online" ? "ok" : r.health === "offline" ? "bad" : "off"} />
                  <span className="min-w-0 flex-1 truncate text-sm font-medium text-fg">{r.cam.name}</span>
                  <span className="text-xs text-fg-faint">{MODE_LABEL[r.cam.recordMode]}</span>
                </div>
              </button>
            ))}
          </div>
        )}
      </div>

      {selected && (
        <SidePanel
          open
          onClose={() => select(null)}
          title={selected.name}
          subtitle={sourceOf(selected)}
          tabs={
            isAdmin
              ? [
                  { id: "overview", icon: Binoculars, label: "Overview" },
                  { id: "settings", icon: Settings, label: "Settings" },
                ]
              : undefined
          }
          tab={tab}
          onTab={(t) => setTab(t as "overview" | "settings")}
        >
          {tab === "overview" || !isAdmin ? (
            <>
              <div className="relative aspect-video overflow-hidden rounded-lg bg-ink-950">
                {selected.enabled ? (
                  <LiveVideo key={selected.id} camera={selected} fit="contain" />
                ) : (
                  <div className="grid h-full place-items-center text-sm text-fg-faint">Camera is disabled</div>
                )}
              </div>
              <DetailSection collapsible={false}>
                <div className="flex items-center gap-3">
                  <span className="grid h-10 w-10 shrink-0 place-items-center rounded-md bg-ink-700">
                    <Cctv className="h-5 w-5 text-fg-muted" />
                  </span>
                  <div className="min-w-0 flex-1">
                    <div className="flex items-center gap-2">
                      <span className="truncate text-sm font-semibold text-fg">{sourceOf(selected)} camera</span>
                      <span className={selHealth === "online" ? "pill-green" : selHealth === "offline" ? "pill-red" : "pill-gray"}>
                        {selHealth === "online" ? "Online" : selHealth === "offline" ? "Offline" : "Disabled"}
                      </span>
                    </div>
                    <p className="truncate text-xs text-fg-muted">
                      {hostOf(selected.rtspMainUrl) ? `Streaming from ${hostOf(selected.rtspMainUrl)}` : "Stream link hidden"}
                    </p>
                  </div>
                </div>
              </DetailSection>
              <DetailSection collapsible={false}>
                <InfoList
                  rows={[
                    {
                      label: "Recording",
                      value: REC_LABEL[selRec],
                      tone: selRec === "failed" ? "bad" : selRec === "recording" ? "ok" : undefined,
                    },
                    { label: "Recording mode", value: MODE_LABEL[selected.recordMode] },
                    { label: "Resolution", value: tierOf(sizes[selected.id]) },
                    { label: "Recording quality", value: qualityOf(selected) },
                    { label: "Retention", value: `${selected.retentionDays} days` },
                    { label: "Storage", value: locName(selected.locationId) },
                    {
                      label: "Last recorded",
                      value: selH?.lastSegmentTs ? fmtRelative(selH.lastSegmentTs) : "—",
                    },
                    ...(selH?.restartsToday ? [{ label: "Restarts today", value: String(selH.restartsToday), tone: "warn" as const }] : []),
                  ]}
                />
              </DetailSection>
              <DetailSection collapsible={false}>
                <InfoList
                  rows={[
                    { label: "Motion detection", value: selected.motionDetection === false ? "Off" : "On" },
                    { label: "Audio detection", value: selected.audioDetection ? "On" : "Off" },
                    { label: "Sub stream", value: selected.rtspSubUrl ? "Yes" : selected.rtspMainUrl ? "No" : "—" },
                    { label: "Home Assistant & HomeKit", value: selected.haPublish === false ? "Hidden" : "Shown" },
                  ]}
                />
              </DetailSection>
              <div className="grid grid-cols-2 gap-2">
                <button className="btn-ghost" onClick={() => navigate(`/playback?camera=${selected.id}`)}>
                  <Play className="h-4 w-4" />
                  Playback
                </button>
                <a
                  className="btn-ghost"
                  href={`/api/v1/cameras/${selected.id}/snapshot.jpg`}
                  download={`${selected.name}.jpg`}
                >
                  <CameraIcon className="h-4 w-4" />
                  Snapshot
                </a>
              </div>
            </>
          ) : (
            editing && (
              <CameraModal
                key={`${editing.id}:${editKey}`}
                variant="panel"
                open
                camera={editing}
                onClose={() => {
                  // Saved, reverted or deleted: start again from the current data. The
                  // save has already refetched the list, so read the cache rather than
                  // this render's copy, which can be older.
                  const latest = qc.getQueryData<Camera[]>(["cameras"]) ?? cameras ?? [];
                  const fresh = latest.find((c) => c.id === editing.id) ?? null;
                  setEditing(fresh);
                  setEditKey((k) => k + 1);
                }}
              />
            )
          )}
        </SidePanel>
      )}

      <CameraModal open={addOpen} camera={null} onClose={() => setAddOpen(false)} />
    </SidebarPage>
  );
}

function RecLabel({ rec }: { rec: Rec }) {
  if (rec === "failed") return <span className="pill-red">Not recording</span>;
  return (
    <span className="inline-flex items-center gap-2">
      {rec === "recording" ? (
        <span className="h-2 w-2 rounded-full bg-rec" />
      ) : (
        <span className="h-2 w-2 rounded-full bg-fg-faint/60" />
      )}
      {rec === "recording" ? "Recording" : rec === "idle" ? "Idle" : "Live only"}
    </span>
  );
}

// Cameras is the Devices page.
export function Cameras() {
  return (
    <>
      <TopBar title="Devices" />
      <CamerasManager />
    </>
  );
}
