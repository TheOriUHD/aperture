import { useEffect, useLayoutEffect, useMemo, useRef, useState } from "react";
import { useNavigate } from "react-router-dom";
import clsx from "clsx";
import {
  Check,
  Cctv,
  LayoutGrid,
  ListChecks,
  Maximize,
  Minimize,
  MoreVertical,
  PanelLeftClose,
  PanelLeftOpen,
  ScanEye,
  VideoOff,
} from "lucide-react";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { CameraPopout } from "../components/CameraPopout";
import { useObjectBoxes } from "../components/ObjectBox";
import { AddCamerasModal, PreviewTile } from "../components/AddCamerasModal";
import { EmptyState } from "../components/ui/EmptyState";
import { OverviewSidebar } from "../components/OverviewCards";
import { MobileDashboard } from "../components/MobileDashboard";
import { useCameras, visibleCameras } from "../hooks/useCameras";
import { useCameraGroups } from "../hooks/useGroups";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { useSystemStatus } from "../hooks/useSystem";
import { useClickOutside } from "../hooks/useClickOutside";
import { CUSTOM_VIEW, fitWall, parseCameraIds, resolveView } from "../lib/dashboardView";
import type { Camera } from "../lib/types";
import type { RecordingState } from "../lib/recordingState";

const SIDEBAR_KEY = "aperture.dashboard.sidebar";
// The view showing on the wall: "all", a camera group's id, or "custom".
const GROUP_KEY = "aperture.dashboard.group";
// The hand-picked cameras of the custom view: a JSON list of ids, in wall order.
const CUSTOM_KEY = "aperture.dashboard.custom";
// How close to the bottom of the wall (px) the pointer brings up the view bar.
const BAR_ZONE = 120;

function readLS(key: string, fallback: string): string {
  try {
    return localStorage.getItem(key) ?? fallback;
  } catch {
    return fallback;
  }
}
function writeLS(key: string, value: string) {
  try {
    localStorage.setItem(key, value);
  } catch {
    /* private mode: in-memory only */
  }
}
function removeLS(key: string) {
  try {
    localStorage.removeItem(key);
  } catch {
    /* private mode: nothing stored */
  }
}

export function Dashboard() {
  const navigate = useNavigate();
  const { data: cameras } = useCameras();
  const [popout, setPopout] = useState<Camera | null>(null);
  const all = cameras ?? [];
  // A disabled camera is left out rather than tiled as a dead placeholder: it is
  // neither streaming nor recording. The Devices page still lists it, which is
  // where it gets switched back on.
  const list = useMemo(() => visibleCameras(cameras) ?? [], [cameras]);
  const isDesktop = useMediaQuery("(min-width: 1024px)");

  // Cameras exist but every one of them is off: say that, rather than "No cameras
  // yet", which would send the operator off to add a camera they already have.
  const empty =
    all.length === 0 ? (
      <EmptyState
        icon={Cctv}
        title="No cameras yet"
        hint="Add a camera to see live feeds here."
        action={
          <button className="btn-primary" onClick={() => navigate("/cameras?add=1")}>
            Add camera
          </button>
        }
      />
    ) : (
      <EmptyState
        icon={VideoOff}
        title="All cameras are disabled"
        hint="Switch one on in Devices to see live feeds."
        action={
          <button className="btn-outline" onClick={() => navigate("/cameras")}>
            Open Devices
          </button>
        }
      />
    );

  return (
    <>
      <TopBar title="Dashboard" />
      {isDesktop ? (
        <DesktopDashboard list={list} all={all} empty={empty} onOpen={setPopout} />
      ) : list.length === 0 ? (
        <div className="flex-1 overflow-auto p-4">{empty}</div>
      ) : (
        <MobileDashboard cameras={list} onOpen={setPopout} />
      )}

      <CameraPopout
        camera={popout}
        onClose={() => setPopout(null)}
        onOpenPlayback={(id) => {
          setPopout(null);
          navigate(`/playback?camera=${id}`);
        }}
      />
    </>
  );
}

function DesktopDashboard({
  list,
  all,
  empty,
  onOpen,
}: {
  list: Camera[];
  all: Camera[];
  empty: React.ReactNode;
  onOpen: (c: Camera) => void;
}) {
  const [sidebar, setSidebar] = useState(() => readLS(SIDEBAR_KEY, "open") !== "closed");
  const { data: groups } = useCameraGroups();
  const [viewId, setViewId] = useState(() => readLS(GROUP_KEY, "all"));
  const [customIds, setCustomIds] = useState(() => parseCameraIds(readLS(CUSTOM_KEY, "")));
  const view = resolveView(list, groups ?? [], viewId, customIds);
  const shown = view.shown;

  const { data: status } = useSystemStatus();
  const recStates = useMemo(() => {
    const m: Record<string, RecordingState> = {};
    for (const c of status?.cameras ?? []) m[c.cameraId] = c.recordingState;
    return m;
  }, [status]);

  const wallRef = useRef<HTMLDivElement>(null);
  const [fullscreen, setFullscreen] = useState(false);
  useEffect(() => {
    const on = () => setFullscreen(document.fullscreenElement === wallRef.current);
    document.addEventListener("fullscreenchange", on);
    return () => document.removeEventListener("fullscreenchange", on);
  }, []);
  const toggleFullscreen = () => {
    if (document.fullscreenElement) void document.exitFullscreen();
    else void wallRef.current?.requestFullscreen?.();
  };

  // The view bar shows only while the pointer is down by it, at the bottom of
  // the wall, like the controls on a tile show only while it's hovered. A
  // pointer resting anywhere else on the wall leaves the cameras clear.
  const [nearBar, setNearBar] = useState(false);
  const trackBar = (e: React.PointerEvent<HTMLDivElement>) => {
    if (e.pointerType === "touch") return;
    const r = e.currentTarget.getBoundingClientRect();
    setNearBar(r.bottom - e.clientY <= BAR_ZONE);
  };

  // The camera picker previews the wall at its current size, so the layout it
  // shows is the one the wall will take on this screen.
  const tileBox = useRef({ w: 0, h: 0 });
  const [picker, setPicker] = useState<{ w: number; h: number } | null>(null);
  const openPicker = () => {
    const wall = wallRef.current;
    const b = tileBox.current;
    setPicker(b.w > 0 ? { ...b } : { w: wall?.clientWidth ?? 0, h: wall?.clientHeight ?? 0 });
  };

  const pickView = (id: string) => {
    setViewId(id);
    writeLS(GROUP_KEY, id);
  };
  const applyCustom = (ids: string[]) => {
    setPicker(null);
    setCustomIds(ids);
    writeLS(CUSTOM_KEY, JSON.stringify(ids));
    pickView(CUSTOM_VIEW);
  };
  const clearCustom = () => {
    setPicker(null);
    setCustomIds([]);
    removeLS(CUSTOM_KEY);
    if (viewId === CUSTOM_VIEW) pickView("all");
  };

  const views = [
    { id: "all", name: "All Cameras", n: list.length },
    ...(groups ?? []).map((g) => ({
      id: g.id,
      name: g.name,
      n: list.filter((c) => g.cameraIds.includes(c.id)).length,
    })),
    ...(view.custom.length > 0 ? [{ id: CUSTOM_VIEW, name: "Custom view", n: view.custom.length }] : []),
  ];

  const toggleSidebar = () =>
    setSidebar((o) => {
      writeLS(SIDEBAR_KEY, o ? "closed" : "open");
      return !o;
    });

  return (
    <div className="flex min-h-0 flex-1">
      {sidebar && (
        <aside className="relative w-[340px] shrink-0 overflow-y-auto py-4 pl-4 pr-3">
          <OverviewSidebar cameras={list} allCameras={all} />
        </aside>
      )}
      {/* The main area. */}
      <div className="relative flex min-w-0 flex-1 flex-col">
        <button
          onClick={toggleSidebar}
          title={sidebar ? "Hide overview" : "Show overview"}
          aria-label={sidebar ? "Hide overview" : "Show overview"}
          className="absolute left-0 top-4 z-10 grid h-7 w-5 place-items-center rounded-r-md border border-l-0 border-line bg-ink-900 text-fg-faint hover:text-fg"
        >
          {sidebar ? <PanelLeftClose className="h-3.5 w-3.5" /> : <PanelLeftOpen className="h-3.5 w-3.5" />}
        </button>
        <div
          ref={wallRef}
          onPointerMove={trackBar}
          onPointerLeave={() => setNearBar(false)}
          className={clsx("relative min-h-0 flex-1", fullscreen && "bg-black")}
        >
          {list.length === 0 ? (
            empty
          ) : shown.length === 0 ? (
            <EmptyState
              icon={LayoutGrid}
              title="No cameras in this view"
              hint="Pick another view with the button below."
            />
          ) : (
            <CameraWall
              cameras={shown}
              recStates={recStates}
              onOpen={onOpen}
              bare={fullscreen}
              boxRef={tileBox}
            />
          )}
          {/* The view bar floats over the bottom of the main area, however much
              of it the tiles fill, centred on the same axis as the tiles. It takes
              no space, and inside fullscreen it sits at the bottom of the screen. */}
          {list.length > 0 && (
            <div
              className={clsx(
                "pointer-events-none absolute bottom-6 z-20 flex justify-center",
                fullscreen ? "inset-x-0" : "left-6 right-4",
              )}
            >
              <ViewBar
                always={shown.length === 0}
                near={nearBar}
                views={views}
                current={view.id}
                onView={pickView}
                onChoose={openPicker}
                fullscreen={fullscreen}
                onFullscreen={toggleFullscreen}
              />
            </div>
          )}
        </div>
      </div>

      {/* Nothing outside the fullscreen element shows, so in fullscreen the picker
          renders into the wall itself. */}
      <AddCamerasModal
        open={!!picker}
        cameras={list}
        initial={shown.map((c) => c.id)}
        active={view.custom.length > 0}
        onApply={applyCustom}
        onEnd={clearCustom}
        onClose={() => setPicker(null)}
        title="Choose cameras"
        applyLabel="Apply"
        endLabel="Clear custom view"
        ordered
        preview={(cams) => <WallPreview cameras={cams} box={picker ?? { w: 0, h: 0 }} bare={fullscreen} />}
        container={fullscreen ? wallRef.current : document.body}
      />
    </div>
  );
}

// CameraWall fits every tile on screen at 16:9, edge to edge, centring a short
// last row the way a video wall does. Only when tiles would get too small to be
// useful does it fall back to scrolling. It reports the box it lays tiles into
// through `boxRef`, for the camera picker's preview.
function CameraWall({
  cameras,
  recStates,
  onOpen,
  bare,
  boxRef,
}: {
  cameras: Camera[];
  recStates: Record<string, RecordingState>;
  onOpen: (c: Camera) => void;
  bare?: boolean;
  boxRef?: React.MutableRefObject<{ w: number; h: number }>;
}) {
  const ref = useRef<HTMLDivElement>(null);
  const [box, setBox] = useState({ w: 0, h: 0 });
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => {
      const b = { w: el.clientWidth, h: el.clientHeight };
      if (boxRef) boxRef.current = b;
      setBox(b);
    };
    const ro = new ResizeObserver(measure);
    ro.observe(el);
    measure();
    return () => ro.disconnect();
  }, [boxRef]);

  const { cols, tileW, tileH, scroll } = fitWall(cameras.length, box.w, box.h);

  return (
    <div
      className={clsx(
        "absolute inset-0",
        bare ? "p-0" : "pb-4 pl-6 pr-4 pt-4",
        scroll ? "overflow-y-auto" : "overflow-hidden",
      )}
    >
      <div ref={ref} className="h-full w-full">
        {box.w > 0 && (
          <div
            className="relative mx-auto flex flex-wrap justify-center"
            style={{ width: scroll ? undefined : tileW * cols }}
          >
            {cameras.map((cam) => (
              <div key={cam.id} style={{ width: tileW, height: tileH }}>
                <CameraTile camera={cam} recState={recStates[cam.id]} onOpen={onOpen} className="h-full w-full" />
              </div>
            ))}
          </div>
        )}
      </div>
    </div>
  );
}

// WallPreview is the picker's layout preview: the wall as it will look with this
// selection, scaled down, including the space the tiles leave empty on this
// screen. `box` is the wall's tile area in pixels.
function WallPreview({ cameras, box, bare }: { cameras: Camera[]; box: { w: number; h: number }; bare?: boolean }) {
  const ref = useRef<HTMLDivElement>(null);
  const [avail, setAvail] = useState(0);
  useLayoutEffect(() => {
    const el = ref.current;
    if (!el) return;
    const ro = new ResizeObserver(() => setAvail(el.clientWidth));
    ro.observe(el);
    setAvail(el.clientWidth);
    return () => ro.disconnect();
  }, []);

  const w = box.w > 0 ? box.w : 1600;
  const h = box.h > 0 ? box.h : 900;
  const n = cameras.length;
  const { cols, tileW, tileH, scroll } = fitWall(n, w, h);
  const rows = Math.max(1, Math.ceil(n / cols));
  // A scrolling wall is previewed whole, every row of it.
  const fullH = scroll ? Math.max(h, rows * tileH) : h;
  const s = Math.min(avail / w, (window.innerHeight * 0.34) / fullH);
  const short = n % cols; // tiles in a short last row, centred like on the wall

  return (
    <div ref={ref} className="w-full">
      {avail > 0 && (
        <div
          className={clsx("mx-auto overflow-hidden rounded-lg border border-line", bare ? "bg-black" : "bg-ink-900")}
          style={{ width: w * s, height: fullH * s }}
        >
          {n === 0 ? (
            <p className="grid h-full place-items-center text-xs text-fg-faint">No cameras selected</p>
          ) : (
            <div
              className="mx-auto grid"
              style={{
                width: cols * tileW * s,
                gridTemplateColumns: `repeat(${cols * 2}, minmax(0, 1fr))`,
                gridAutoRows: tileH * s,
              }}
            >
              {cameras.map((c, i) => (
                <div
                  key={c.id}
                  className="flex overflow-hidden p-px"
                  style={{ gridColumn: short && i === n - short ? `${cols - short + 1} / span 2` : "span 2" }}
                >
                  <PreviewTile camera={c} className="flex-1" />
                </div>
              ))}
            </div>
          )}
        </div>
      )}
    </div>
  );
}

// ViewBar floats over the bottom of the wall while the pointer is down by it:
// which cameras are shown (all, a group or a hand-picked set), the object-box
// toggle and fullscreen. It stays up while its menu is open, while a key moves
// focus into it, and always on a touch screen, which cannot hover.
function ViewBar({
  always,
  near,
  views,
  current,
  onView,
  onChoose,
  fullscreen,
  onFullscreen,
}: {
  always?: boolean;
  near?: boolean;
  views: { id: string; name: string; n: number }[];
  current: string;
  onView: (id: string) => void;
  onChoose: () => void;
  fullscreen: boolean;
  onFullscreen: () => void;
}) {
  const [open, setOpen] = useState(false);
  const ref = useRef<HTMLDivElement>(null);
  useClickOutside(ref, () => setOpen(false), open);
  const active = views.find((v) => v.id === current) ?? views[0];

  return (
    <div
      ref={ref}
      className={clsx(
        "pointer-events-auto relative flex items-center gap-1.5 transition-opacity duration-150",
        always || open || near
          ? "opacity-100"
          : "opacity-0 has-[:focus-visible]:opacity-100 [@media(hover:none)]:opacity-100",
      )}
    >
      <button
        onClick={() => setOpen((o) => !o)}
        aria-haspopup="menu"
        aria-expanded={open}
        className="flex h-8 items-center rounded-md bg-[rgba(24,26,29,0.88)] text-[13px] text-brand shadow-pop backdrop-blur-sm hover:bg-[rgba(36,39,43,0.95)]"
      >
        <span className="max-w-[260px] truncate px-3 font-medium">
          {active.name} ({active.n})
        </span>
        <span className="h-4 w-px bg-white/15" />
        <span className="grid h-8 w-7 place-items-center text-fg-muted">
          <MoreVertical className="h-4 w-4" />
        </span>
      </button>
      <BoxesButton />
      <button
        onClick={onFullscreen}
        title={fullscreen ? "Exit fullscreen" : "Fullscreen"}
        aria-label={fullscreen ? "Exit fullscreen" : "Fullscreen"}
        className="grid h-8 w-8 place-items-center rounded-md bg-[rgba(24,26,29,0.88)] text-fg/85 shadow-pop backdrop-blur-sm hover:bg-[rgba(36,39,43,0.95)] hover:text-fg"
      >
        {fullscreen ? <Minimize className="h-4 w-4" /> : <Maximize className="h-4 w-4" />}
      </button>
      {open && (
        <div
          role="menu"
          className="absolute bottom-11 left-0 z-20 max-h-[60vh] w-60 animate-pop-in overflow-y-auto card py-1.5 shadow-pop"
        >
          {views.map((v) => (
            <button
              key={v.id}
              role="menuitemradio"
              aria-checked={v.id === active.id}
              onClick={() => {
                onView(v.id);
                setOpen(false);
              }}
              className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-fg/90 hover:bg-white/[0.04]"
            >
              <span className="min-w-0 flex-1 truncate">{v.name}</span>
              <span className="text-xs tabular-nums text-fg-faint">{v.n}</span>
              <Check className={clsx("h-4 w-4 text-brand", v.id === active.id ? "opacity-100" : "opacity-0")} />
            </button>
          ))}
          <div className="mx-3 my-1.5 h-px bg-line" />
          <button
            role="menuitem"
            onClick={() => {
              setOpen(false);
              onChoose();
            }}
            className="flex w-full items-center gap-2.5 px-3 py-2 text-left text-sm text-fg/90 hover:bg-white/[0.04]"
          >
            <span className="min-w-0 flex-1 truncate">Choose cameras…</span>
            <ListChecks className="h-4 w-4 text-fg-muted" />
          </button>
        </div>
      )}
    </div>
  );
}

// BoxesButton shows or hides the boxes round recognised objects (one switch
// shared with Playback and the expanded view).
function BoxesButton() {
  const [on, setOn] = useObjectBoxes();
  const label = on ? "Hide object boxes" : "Show object boxes";
  return (
    <button
      onClick={() => setOn(!on)}
      title={label}
      aria-label={label}
      aria-pressed={on}
      className={clsx(
        "grid h-8 w-8 place-items-center rounded-md bg-[rgba(24,26,29,0.88)] shadow-pop backdrop-blur-sm hover:bg-[rgba(36,39,43,0.95)]",
        on ? "text-brand" : "text-fg/85 hover:text-fg",
      )}
    >
      <ScanEye className="h-4 w-4" />
    </button>
  );
}
