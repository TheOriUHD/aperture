import { useEffect, useRef, useState } from "react";
import { useSearchParams } from "react-router-dom";
import { useQuery, useQueryClient, keepPreviousData } from "@tanstack/react-query";
import Hls from "hls.js";
import {
  Play,
  Pause,
  Rewind,
  RotateCcw,
  RotateCw,
  Keyboard,
  Maximize2,
  Settings,
  Camera as CameraIcon,
  ArrowUp,
  LayoutGrid,
  Calendar,
  Columns2,
  PanelLeft,
  PanelBottom,
  Download,
  Cctv,
  Scissors,
  Gauge,
  Link2,
  ChevronFirst,
  ChevronLast,
  Volume2,
  VolumeX,
  ScanEye,
  Grid2x2,
  Search,
  Trash2,
  Film,
} from "lucide-react";
import { CameraRail } from "../components/CameraRail";
import { CameraImagingOverlay } from "../components/CameraImagingOverlay";
import { CameraSpotlightButton } from "../components/CameraSpotlightButton";
import { VerticalTimeline, type TR, type TLEvent } from "../components/VerticalTimeline";
import { EventStrip } from "../components/EventStrip";
import { LiveVideo } from "../components/LiveVideo";
import { ZoomControl } from "../components/ZoomControl";
import { ExportCenter } from "../components/ExportCenter";
import { CameraModal } from "../components/CameraModal";
import { DetectionOverlay } from "../components/DetectionOverlay";
import { GridSearchOverlay, filterByRegion, type Region } from "../components/GridSearch";
import { MultiCamGrid } from "../components/MultiCamGrid";
import { AddCamerasModal } from "../components/AddCamerasModal";
import { ViewerZoom } from "../components/ViewerZoom";
import { TopBar } from "../components/TopBar";
import { ConfirmDialog } from "../components/ui/ConfirmDialog";
import { DateTimePicker } from "../components/DateTimePicker";
import { PlaybackConditions } from "../components/ConditionsOverlay";
import { useAuth } from "../lib/auth";
import { fsVideoStyle, FullscreenChrome } from "../components/fullscreen";
import { useCameras } from "../hooks/useCameras";
import { useMediaQuery } from "../hooks/useMediaQuery";
import { useSite, useWeatherRange } from "../hooks/useEnvironment";
import { usePrefs } from "../lib/prefs";
import { api } from "../lib/api";
import { warmLive, stopWarmLive } from "../lib/liveWarm";
import { fmtTimeSec, fmtDate } from "../lib/format";
import { wallToOffset, offsetToWall, type Seg } from "../lib/playlist";
import type { MotionEvent } from "../lib/types";

interface Segment extends Seg {
  id: string;
  hasMotion: boolean;
}

const DAY_MS = 24 * 60 * 60 * 1000;
const MONTH_MS = 30 * DAY_MS;
// Largest window first so the zoom slider reads left = out, right (+) = in.
const ZOOMS = [720, 360, 240, 180, 120, 90, 60, 45, 30, 20, 15, 10, 5].map((m) => m * 60_000);
const ZOOM_DEFAULT = 8; // 30 min

const startOfDay = (ms: number) => {
  const d = new Date(ms);
  d.setHours(0, 0, 0, 0);
  return d.getTime();
};
const clamp = (x: number, lo: number, hi: number) => Math.min(hi, Math.max(lo, x));
const fmtClock = fmtTimeSec;

export function Playback() {
  const { data: cameras } = useCameras();
  const [params] = useSearchParams();
  const [selectedId, setSelectedId] = useState<string | null>(null);

  // Deep link: ?t=<unix ms> (e.g. clicking a detection on the dashboard) must open
  // in recorded mode AT that moment. Seed state synchronously so the very first
  // render is already not-following at `t` — otherwise the "pin to live" effect,
  // running with the default following=true, clobbers the position back to live.
  const deepLinkT = Number(params.get("t"));
  const hasDeepLink = deepLinkT > 0;
  const [nowMs, setNowMs] = useState(() => Date.now());
  const [following, setFollowing] = useState(!hasDeepLink);
  const [selectedState, setSelectedState] = useState(() => (hasDeepLink ? deepLinkT : Date.now()));
  const { prefs } = usePrefs();
  // Collapsible chrome: hide the camera list and/or the events strip so the
  // player fills the screen (only the app's nav rail stays). Persisted so the
  // layout you chose survives a reload.
  const [showRail, setShowRail] = useState(() => localStorage.getItem("pb.rail") !== "0");
  const [showEvents, setShowEvents] = useState(() => localStorage.getItem("pb.events") !== "0");
  useEffect(() => {
    localStorage.setItem("pb.rail", showRail ? "1" : "0");
  }, [showRail]);
  useEffect(() => {
    localStorage.setItem("pb.events", showEvents ? "1" : "0");
  }, [showEvents]);
  const { user } = useAuth();
  const qc = useQueryClient();
  const isAdmin = user?.role === "admin";
  const [zoomIdx, setZoomIdx] = useState(
    Math.min(ZOOMS.length - 1, Math.max(0, prefs.timelineZoom ?? ZOOM_DEFAULT)),
  );
  const [paused, setPaused] = useState(false);

  const isDesktop = useMediaQuery("(min-width: 1024px)");
  const [camerasOpen, setCamerasOpen] = useState(false);
  const [exportOpen, setExportOpen] = useState(false);
  const [settingsOpen, setSettingsOpen] = useState(false);
  const [railRight, setRailRight] = useState(false); // flip timeline/rail to the other side
  const [playbackRate, setPlaybackRate] = useState(1);
  const [speedOpen, setSpeedOpen] = useState(false);
  const [copied, setCopied] = useState(false);
  const [muted, setMuted] = useState(true); // both live + playback start muted; tap to hear
  const [dateOpen, setDateOpen] = useState(false); // custom date/time picker popover
  const [fs, setFs] = useState(false); // mobile pseudo-fullscreen (rotate + fill)
  const [boxesOn, setBoxesOn] = useState(false); // object-detection box overlay
  const [gridOpen, setGridOpen] = useState(false); // grid-search draw overlay
  const [region, setRegion] = useState<Region | null>(null); // grid-search motion area
  const [quality, setQuality] = useState<"auto" | "low" | "high">("auto");
  const [manageOpen, setManageOpen] = useState(false); // Manage dropdown menu
  const [confirmRemove, setConfirmRemove] = useState(false);
  const [removeBusy, setRemoveBusy] = useState(false);
  const [multiCams, setMultiCams] = useState<string[]>([]); // all cameras in the multi-view grid
  const [addCamsOpen, setAddCamsOpen] = useState(false); // "Add Cameras to Playback" modal
  const [multiStartId, setMultiStartId] = useState<string | null>(null); // camera to return to on End
  const [qualityOpen, setQualityOpen] = useState(false); // global-quality popover
  const [zoom, setZoom] = useState(1); // single-player digital zoom (1..8)
  const [pan, setPan] = useState({ x: 0.5, y: 0.5 }); // zoom viewport centre (fraction)

  const RATES = [0.5, 0.75, 1, 1.25, 1.5, 2, 4];

  const playerVideoRef = useRef<HTMLVideoElement>(null);
  const containerRef = useRef<HTMLDivElement>(null);
  const hlsRef = useRef<Hls | null>(null);
  const selectedRef = useRef<number>(selectedState);
  const segmentsRef = useRef<Segment[]>([]);
  const seekTimer = useRef<number | undefined>(undefined);
  const swipeY = useRef(0); // vertical swipe start on the mobile video (camera strip)

  const windowMs = ZOOMS[zoomIdx];
  const isLive = following;
  // The timeline position is DECOUPLED from the player: it is always
  // `selectedState`. While live-following (and not mid-scrub) an effect pins it
  // to the live edge; a scrub owns it otherwise. Crucially, `following` only
  // flips on settle — never per scroll tick — so scrubbing never remounts the
  // video (that per-tick remount was the flicker).
  const selectedMs = selectedState;
  selectedRef.current = selectedMs;
  // Whether the playhead itself sits at the live edge — drives the LIVE pill,
  // separator and jump-to-live arrow, independent of the player's follow state.
  const atLiveEdge = selectedMs >= nowMs - 1000;
  const dayStart = startOfDay(selectedMs);
  const dayEnd = dayStart + DAY_MS;

  useEffect(() => {
    const i = setInterval(() => setNowMs(Date.now()), 1000);
    return () => clearInterval(i);
  }, []);

  useEffect(() => {
    if (!cameras?.length) return;
    const q = params.get("camera");
    if (q && cameras.some((c) => c.id === q)) setSelectedId(q);
    else setSelectedId((prev) => prev ?? cameras[0].id);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cameras]);

  // Deep link: ?t=<unix ms> opens playback at that moment (e.g. from Alarms).
  const didInitT = useRef(false);
  useEffect(() => {
    if (didInitT.current) return;
    const t = Number(params.get("t"));
    if (t > 0) {
      didInitT.current = true;
      setFollowing(false);
      setSelectedState(t);
      selectedRef.current = t;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [params]);

  const selected = cameras?.find((c) => c.id === selectedId) ?? null;
  // Multi-view: `multiCams` is the full set of grid cameras. >1 = synced grid;
  // otherwise single-player of `selected`. In multi-view the timeline still keys
  // off `selected` (the camera the view was started on).
  const multiActive = multiCams.length > 1;
  const multiView = multiActive
    ? (cameras ?? []).filter((c) => multiCams.includes(c.id))
    : selected
      ? [selected]
      : [];

  // Keep multi-view coherent: a grid of one collapses to single view, and the
  // timeline's reference camera must stay one of the grid cameras.
  useEffect(() => {
    if (multiCams.length === 0) return;
    if (multiCams.length === 1) {
      setSelectedId(multiCams[0]);
      setMultiCams([]);
    } else if (selectedId && !multiCams.includes(selectedId)) {
      setSelectedId(multiCams[0]);
    }
  }, [multiCams, selectedId]);

  // Reset digital zoom when the camera changes or multi-view toggles.
  useEffect(() => {
    setZoom(1);
    setPan({ x: 0.5, y: 0.5 });
  }, [selectedId, multiActive]);

  function applyMulti(ids: string[]) {
    setAddCamsOpen(false);
    setPaused(false); // start the synced grid playing
    if (ids.length <= 1) {
      if (ids[0]) setSelectedId(ids[0]);
      setMultiCams([]);
      return;
    }
    setMultiStartId((prev) =>
      prev && ids.includes(prev) ? prev : selectedId && ids.includes(selectedId) ? selectedId : ids[0],
    );
    if (selectedId && !ids.includes(selectedId)) setSelectedId(ids[0]);
    setMultiCams(ids);
  }
  function endMulti() {
    setAddCamsOpen(false);
    setMultiCams([]);
    if (multiStartId) setSelectedId(multiStartId);
    setMultiStartId(null);
  }

  // Keep a live connection warm for the selected camera the whole time we're on
  // this screen, so scrubbing back to the live edge is instant (the visible
  // player borrows the already-connected stream) instead of renegotiating.
  useEffect(() => {
    warmLive(selected?.id, selected?.enabled ?? false);
    return () => stopWarmLive();
  }, [selected?.id, selected?.enabled]);

  const followingToday = dayStart === startOfDay(nowMs);

  const { data: segments = [] } = useQuery({
    queryKey: ["segments", selectedId, dayStart],
    queryFn: () => api.get<Segment[]>(`/api/cameras/${selectedId}/segments?from=${dayStart}&to=${dayEnd}`),
    enabled: !!selectedId,
    refetchInterval: followingToday ? 15000 : false,
  });
  segmentsRef.current = segments;

  // The rail is drawn from a WIDER window (the day plus its neighbours) so the
  // recording stripes run continuously across midnight instead of stopping dead
  // at 00:00 and re-loading when you scroll over the boundary. keepPreviousData
  // keeps the old rail on screen while the shifted window loads, so crossing
  // midnight never blanks it. HLS playback stays per-day, so `segments` above is
  // kept separate as the source of truth for the offset mapping.
  const railFrom = dayStart - DAY_MS;
  const railTo = dayEnd + DAY_MS;
  // Rail coverage: the server merges segments into ranges (T2.1), replacing the
  // wide ~12k-segment fetch + client mergeRanges. Same {start,end} shape, so the
  // rail draws identically. The per-day `segments` query (offset mapping) is
  // deliberately left on raw segments — do not point wallToOffset at coverage.
  const { data: railRanges = [] } = useQuery({
    queryKey: ["rail-coverage", selectedId, dayStart],
    queryFn: () =>
      api.get<TR[]>(`/api/cameras/${selectedId}/coverage?from=${railFrom}&to=${railTo}&tolerance_ms=15000`),
    enabled: !!selectedId,
    refetchInterval: followingToday ? 15000 : false,
    placeholderData: keepPreviousData,
  });

  const { data: events = [] } = useQuery({
    queryKey: ["events", selectedId, dayStart],
    queryFn: () => api.get<MotionEvent[]>(`/api/cameras/${selectedId}/events?from=${dayStart}&to=${dayEnd}`),
    enabled: !!selectedId,
    refetchInterval: followingToday ? 15000 : false,
  });

  const recorded: TR[] = railRanges;
  // No gap layer: the rail's grey track IS "nothing recorded", so absence of blue
  // says it without a separate stripe. The old hatched-red gaps were computed from
  // a coverage fetch that is briefly stale after a camera/day switch (keepPrevious)
  // and fragments on every recorder restart, so they regularly painted "no footage"
  // straight over footage that existed.
  // Grid Search: when a motion area is drawn, restrict events to those whose
  // motion overlapped it. Feeds both the timeline markers and the events panel.
  const filteredEvents = filterByRegion(events, region);
  const tlEvents: TLEvent[] = filteredEvents.map((e) => ({
    id: e.id,
    start: e.startTs,
    end: e.endTsMs ?? e.endTs,
    thumbUrl: e.thumbUrl,
  }));

  // Site profile drives the weather ribbon's timezone/enablement. (The day/night
  // background shading it used to feed is gone — it tinted the whole timeline and
  // read as an unexplained colour change mid-scroll.)
  const { data: site } = useSite();

  // The day's weather samples feed the timeline's environmental ribbon (opt-in).
  const { data: dayWeather } = useWeatherRange(dayStart, dayEnd, !!site?.configured && !!prefs.timelineRibbon);
  const ribbon = prefs.timelineRibbon ? dayWeather : undefined;

  // HLS playback for the active day.
  useEffect(() => {
    if (isLive || !selected || segments.length === 0) return;
    const video = playerVideoRef.current;
    if (!video) return;
    const url = `/api/v1/cameras/${selected.id}/hls?from=${dayStart}&to=${dayEnd}`;
    if (Hls.isSupported()) {
      const hls = new Hls({ maxBufferLength: 30, enableWorker: true });
      hlsRef.current = hls;
      hls.loadSource(url);
      hls.attachMedia(video);
      hls.on(Hls.Events.MANIFEST_PARSED, () => {
        video.currentTime = wallToOffset(segmentsRef.current, selectedRef.current);
        void video.play().catch(() => undefined);
      });
      return () => {
        hls.destroy();
        hlsRef.current = null;
      };
    } else if (video.canPlayType("application/vnd.apple.mpegurl")) {
      video.src = url;
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, selected?.id, dayStart, segments.length > 0]);

  const scrubbing = useRef(false);
  const snapAnim = useRef(0);
  const allowSnap = useRef(true);
  const nowRef = useRef(nowMs);
  nowRef.current = nowMs;
  const followingRef = useRef(following);
  followingRef.current = following;

  // While live-following (and not mid-scrub), pin the playhead to the live edge.
  // A scrub sets scrubbing.current, so this yields the position to the gesture
  // instead of yanking it back — no fighting between the clock tick and the drag.
  useEffect(() => {
    if (following && !scrubbing.current) {
      setSelectedState(nowMs);
      selectedRef.current = nowMs;
    }
  }, [following, nowMs]);

  // Spring the view to the live edge, then switch to live. A real critically-
  // damped spring (fixed-timestep, so it's frame-rate independent) on the
  // *displacement from live* — recomputed against Date.now() every frame, so it
  // tracks the advancing edge with zero lag, decelerates smoothly instead of
  // "itching" in, and lands exactly on the present (no dip, no teleport). ζ = 1
  // means it never overshoots past live.
  function animateToLive() {
    cancelAnimationFrame(snapAnim.current);
    scrubbing.current = true;
    let disp = selectedRef.current - Date.now(); // ms ahead of (−behind) live
    let vel = 0; // ms per second
    const omega = 20; // natural frequency (rad/s)
    const k = omega * omega; // stiffness
    const c = 2 * omega; // damping at ζ = 1
    const fixed = 1 / 240; // fixed physics step (s)
    let prev = performance.now();
    let acc = 0;
    const step = (t: number) => {
      let frame = (t - prev) / 1000;
      prev = t;
      if (frame > 0.05) frame = 0.05; // clamp tab stalls
      acc += frame;
      while (acc >= fixed) {
        const a = -k * disp - c * vel; // spring toward disp = 0
        vel += a * fixed;
        disp += vel * fixed; // semi-implicit Euler (stable)
        acc -= fixed;
      }
      const x = Date.now() + disp;
      setSelectedState(x);
      selectedRef.current = x;
      if (Math.abs(disp) < 25 && Math.abs(vel) < 800) {
        const now = Date.now();
        setSelectedState(now);
        selectedRef.current = now;
        scrubbing.current = false;
        setNowMs(now); // hand off continuously to the live clock
        setFollowing(true);
        setPaused(false);
        return;
      }
      snapAnim.current = requestAnimationFrame(step);
    };
    snapAnim.current = requestAnimationFrame(step);
  }

  // Settle a scrub: bounce (smoothly) to live if we're near/over the edge,
  // otherwise seek the recorded player to the landing time.
  function settleNow() {
    window.clearTimeout(seekTimer.current);
    const snapMs = clamp(windowMs * 0.05, 5000, 90_000);
    if (allowSnap.current && selectedRef.current >= nowRef.current - snapMs) {
      animateToLive();
      return;
    }
    // Settling in the past: commit the player to recorded playback at the
    // landing time — ONCE. If we were live-following, flipping `following`
    // remounts the recorded <video> and the HLS effect seeks to selectedRef on
    // manifest; otherwise it's already mounted, so seek it directly.
    scrubbing.current = false;
    if (followingRef.current) {
      setFollowing(false);
    } else {
      const v = playerVideoRef.current;
      if (v && hlsRef.current) {
        v.currentTime = wallToOffset(segmentsRef.current, selectedRef.current);
        void v.play().catch(() => undefined);
      }
    }
  }
  // Wheel/continuous scrubs have no discrete end, so settle after an idle gap.
  // Long enough that rapid ("spam") scrolling never settles mid-gesture — the
  // player stays put and the settle spring can't fight the incoming scroll.
  function scheduleSettle() {
    window.clearTimeout(seekTimer.current);
    seekTimer.current = window.setTimeout(settleNow, 160);
  }
  // A pointer drag DID end (finger lifted): settle immediately so the bounce to
  // live happens right away rather than after the idle debounce.
  function endScrub() {
    settleNow();
  }

  function applyScrub(ms: number, snap: boolean) {
    cancelAnimationFrame(snapAnim.current);
    allowSnap.current = snap;
    const now = nowRef.current;
    let c: number;
    if (ms > now) {
      // Rubber-band past the live edge into the clear zone above. No ceiling to
      // "hit" — resistance grows the further you push (≈1:1 at the edge, ever
      // stiffer beyond), so you can stretch it far while holding and it springs
      // back into live on release.
      const over = ms - now;
      const a = windowMs * 0.17; // resistance scale: bigger = looser stretch
      c = now + a * Math.log1p(over / a);
    } else {
      c = Math.max(ms, now - MONTH_MS);
    }
    scrubbing.current = true;
    // NOTE: deliberately does NOT touch `following` — the player is left alone
    // during the gesture and only commits on settle, so scrubbing can't thrash
    // the video element.
    setSelectedState(c);
    selectedRef.current = c;
    scheduleSettle();
  }

  // Drag / wheel scrubbing: snaps to live when settling near the edge.
  function scrubToTime(ms: number) {
    applyScrub(ms, true);
  }
  // Explicit seeks (skip buttons, timeline click): never snap; only go live if
  // the target is essentially the present.
  function seekToTime(ms: number) {
    if (ms >= nowMs - 1500) {
      goLive();
      return;
    }
    // Explicit jump (skip button, event/calendar, timeline click): commit to
    // recorded playback at once rather than waiting for a scrub to settle.
    cancelAnimationFrame(snapAnim.current);
    window.clearTimeout(seekTimer.current);
    scrubbing.current = false;
    allowSnap.current = false;
    const c = Math.max(ms, nowRef.current - MONTH_MS);
    const wasFollowing = followingRef.current;
    setFollowing(false);
    setSelectedState(c);
    selectedRef.current = c;
    if (!wasFollowing) {
      const v = playerVideoRef.current;
      if (v && hlsRef.current) {
        v.currentTime = wallToOffset(segmentsRef.current, c);
        void v.play().catch(() => undefined);
      }
    }
  }

  function goLive() {
    setPlaybackRate(1);
    // Spring to live whenever the playhead isn't already sitting at the edge —
    // `following` may still be true mid-scrub, so key off position too.
    if (!followingRef.current || selectedRef.current < nowRef.current - 1000) animateToLive();
  }

  // Apply the chosen playback speed to the recorded-video element.
  useEffect(() => {
    if (playerVideoRef.current && !isLive) playerVideoRef.current.playbackRate = playbackRate;
  }, [playbackRate, isLive, selectedId]);

  function frameStep(dir: number) {
    const v = playerVideoRef.current;
    if (!v || !selected || isLive) return;
    v.pause();
    const wall = offsetToWall(segmentsRef.current, v.currentTime + dir / (selected.fps || 15));
    if (wall) seekToTime(wall);
  }

  function findNextEvent(ms: number): number | null {
    const next = events.find((e) => e.startTs > ms);
    return next ? next.startTs : null;
  }
  function findPrevEvent(ms: number): number | null {
    let hit: number | null = null;
    for (const e of events) {
      const end = e.endTsMs ?? (e.endTs || nowMs);
      if (end < ms) hit = e.startTs;
    }
    return hit;
  }

  async function copyLink() {
    if (!selected) return;
    const url = `${window.location.origin}/playback?camera=${selected.id}&t=${Math.round(selectedMs)}`;
    try {
      await navigator.clipboard.writeText(url);
    } catch {
      window.prompt("Copy this link:", url);
      return;
    }
    setCopied(true);
    setTimeout(() => setCopied(false), 1500);
  }

  // Keyboard shortcuts (ignored while typing in a field).
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      const el = e.target as HTMLElement | null;
      if (el && (["INPUT", "TEXTAREA", "SELECT"].includes(el.tagName) || el.isContentEditable)) return;
      // Mirrors UniFi Protect's shortcut set (see the keyboard-help popover).
      switch (e.key) {
        case " ":
          e.preventDefault();
          if (!isLive) togglePlay();
          break;
        case "ArrowUp":
          if (!isLive) seekToTime(selectedMs + 1000); // skip +1s
          break;
        case "ArrowDown":
          if (!isLive) seekToTime(selectedMs - 1000); // skip -1s
          break;
        case "ArrowLeft":
          frameStep(-1); // step back one frame
          break;
        case "ArrowRight":
          frameStep(1); // step forward one frame
          break;
        case "j":
        case "J":
          setPlaybackRate((r) => RATES[Math.max(0, RATES.indexOf(r) - 1)] ?? r);
          break;
        case "l":
        case "L":
          setPlaybackRate((r) => RATES[Math.min(RATES.length - 1, RATES.indexOf(r) + 1)] ?? r);
          break;
        case "f":
        case "F":
          fullscreen();
          break;
      }
    };
    window.addEventListener("keydown", onKey);
    return () => window.removeEventListener("keydown", onKey);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [isLive, selectedMs, selected, nowMs]);

  function togglePlay() {
    // Multi-view has no single element — drive all recorded tiles via `paused`.
    if (multiActive) {
      setPaused((p) => !p);
      return;
    }
    const v = playerVideoRef.current;
    if (!v) return;
    if (v.paused) void v.play().then(() => setPaused(false)).catch(() => undefined);
    else {
      v.pause();
      setPaused(true);
    }
  }
  function snapshot() {
    const v = playerVideoRef.current;
    if (!v || !v.videoWidth) return;
    const canvas = document.createElement("canvas");
    canvas.width = v.videoWidth;
    canvas.height = v.videoHeight;
    canvas.getContext("2d")?.drawImage(v, 0, 0);
    canvas.toBlob(
      (b) => {
        if (!b) return;
        const url = URL.createObjectURL(b);
        const a = document.createElement("a");
        a.href = url;
        a.download = `${selected?.name ?? "camera"}-${new Date().toISOString().replace(/[:.]/g, "-")}.jpg`;
        a.click();
        URL.revokeObjectURL(url);
      },
      "image/jpeg",
      0.95,
    );
  }
  function fullscreen() {
    // Mobile: rotate + fill (iPhone blocks element fullscreen). Desktop: native.
    if (!isDesktop) {
      setFs((f) => !f);
      return;
    }
    const el = containerRef.current;
    if (!document.fullscreenElement) void el?.requestFullscreen?.();
    else void document.exitFullscreen();
  }

  const mobileFs = fs && !isDesktop;

  // Digital-zoom transform for the single player (origin top-left). pan is the
  // viewport centre; derive the clamped top-left corner and translate to it.
  const zx = Math.max(0, Math.min(1 - 1 / zoom, pan.x - 0.5 / zoom));
  const zy = Math.max(0, Math.min(1 - 1 / zoom, pan.y - 0.5 / zoom));
  const zoomTransform =
    zoom > 1 ? `translate(${(-zx * zoom * 100).toFixed(3)}%, ${(-zy * zoom * 100).toFixed(3)}%) scale(${zoom})` : undefined;

  // The visible timeline window — the range the "Remove" action deletes.
  const removeFrom = Math.round(selectedMs - windowMs * 0.8);
  const removeTo = Math.round(Math.min(nowMs, selectedMs + windowMs * 0.2));
  async function removeRecordings() {
    if (!selected) return;
    setRemoveBusy(true);
    try {
      await api.del(`/api/cameras/${selected.id}/recordings?from=${removeFrom}&to=${removeTo}`);
      qc.invalidateQueries({ queryKey: ["segments"] });
      qc.invalidateQueries({ queryKey: ["rail-segments"] });
      qc.invalidateQueries({ queryKey: ["events"] });
    } catch {
      /* surfaced by the request layer */
    } finally {
      setRemoveBusy(false);
      setConfirmRemove(false);
    }
  }

  function openDatePicker() {
    setDateOpen((o) => !o);
  }

  // One video element, reused by both layouts (only one renders at a time).
  function renderPlayer() {
    if (!selected) {
      return <div className="absolute inset-0 grid place-items-center text-sm text-fg-faint">No cameras</div>;
    }
    return isLive ? (
      <LiveVideo
        key={selected.id}
        camera={selected}
        fit="contain"
        videoRef={playerVideoRef}
        muted={muted}
        fsStyle={mobileFs ? fsVideoStyle : undefined}
        quality={quality}
      />
    ) : (
      <video
        ref={playerVideoRef}
        playsInline
        muted={muted}
        className={mobileFs ? "bg-black" : "absolute inset-0 h-full w-full object-contain bg-black"}
        style={mobileFs ? fsVideoStyle : undefined}
        onTimeUpdate={(e) => {
          if (scrubbing.current) return;
          const wall = offsetToWall(segmentsRef.current, e.currentTarget.currentTime);
          if (wall) {
            setSelectedState(wall);
            selectedRef.current = wall;
          }
        }}
        onPlay={() => setPaused(false)}
        onPause={() => setPaused(true)}
      />
    );
  }

  const overlayTime = selectedMs;
  const fmtDay = (ms: number) => new Date(ms).toLocaleDateString(undefined, { month: "short", day: "numeric" });

  const datePickerPopover = dateOpen && (
    <>
      <div className="fixed inset-0 z-40" onClick={() => setDateOpen(false)} />
      <div className="absolute bottom-16 left-3 z-50">
        <DateTimePicker
          value={selectedMs}
          max={nowMs}
          onApply={(ms) => {
            seekToTime(ms);
            setDateOpen(false);
          }}
          onCancel={() => setDateOpen(false)}
        />
      </div>
    </>
  );

  // ---- Mobile layout (UniFi Protect-style) --------------------------------
  const exportModal = (
    <ExportCenter
      open={exportOpen}
      onClose={() => setExportOpen(false)}
      camera={selected}
      cameras={cameras ?? []}
      defaultTo={selectedMs}
    />
  );

  if (!isDesktop) {
    return (
      <div
        className="flex h-full min-h-0 flex-col bg-ink-900"
        style={{ paddingTop: "env(safe-area-inset-top)" }}
      >
        {exportModal}
        {/* camera switcher (slides in/out) */}
        <div
          className={`shrink-0 overflow-hidden border-b border-line transition-[max-height] duration-300 ${
            camerasOpen ? "max-h-[120px]" : "max-h-0"
          }`}
        >
          <div className="flex gap-2 overflow-x-auto p-2">
            {(cameras ?? []).map((c) => (
              <button
                key={c.id}
                onClick={() => setSelectedId(c.id)}
                className="shrink-0 w-[120px]"
              >
                <div
                  className={`relative aspect-video rounded-lg overflow-hidden border ${
                    c.id === selectedId ? "border-brand ring-1 ring-brand" : "border-line"
                  }`}
                >
                  <MobileThumb cameraId={c.id} enabled={c.enabled} />
                </div>
                <p
                  className={`mt-1 text-[11px] uppercase tracking-wide truncate ${
                    c.id === selectedId ? "text-fg font-medium" : "text-fg-muted"
                  }`}
                >
                  {c.name}
                </p>
              </button>
            ))}
          </div>
        </div>

        {/* video — swipe down opens the camera strip, up hides it (UniFi-style) */}
        <div
          ref={containerRef}
          className="relative shrink-0 aspect-video bg-black"
          onTouchStart={(e) => {
            swipeY.current = e.touches[0]?.clientY ?? 0;
          }}
          onTouchEnd={(e) => {
            const dy = (e.changedTouches[0]?.clientY ?? 0) - swipeY.current;
            if (dy > 45) setCamerasOpen(true);
            else if (dy < -45) setCamerasOpen(false);
          }}
        >
          {renderPlayer()}
          {selected && !mobileFs && (
            <div className="absolute top-2 left-2 flex max-w-[62%] items-center gap-1 rounded-md bg-black/40 px-2 py-1 text-[11px] font-mono text-white/90 backdrop-blur-sm">
              <span className="shrink-0 tabular-nums">
                {fmtDate(overlayTime)} {fmtClock(overlayTime)}
              </span>
              <span className="truncate text-white/60">· {selected.name}</span>
            </div>
          )}
          {selected && !mobileFs && (
            <PlaybackConditions tsMs={selectedMs} className="top-2 right-2" variant="compact" cameraId={selected.id} live={isLive} />
          )}
        </div>

        {mobileFs && <FullscreenChrome onExit={() => setFs(false)} />}

        {/* control header */}
        {selected && (
          <div className="flex items-center gap-2 px-3 h-12 border-b border-line shrink-0">
            <span className="flex items-center gap-2 text-sm font-medium text-fg">
              <span className={`h-2.5 w-2.5 rounded-full ${isLive ? "bg-rec" : "bg-fg-faint"}`} />
              {selected.name}
            </span>
            <div className="ml-auto flex items-center gap-0.5">
              {!isLive && (
                <>
                  <BarBtn onClick={togglePlay} label={paused ? "Play" : "Pause"}>
                    {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
                  </BarBtn>
                  <BarBtn onClick={() => seekToTime(selectedMs - 30_000)} label="Back 30 seconds">
                    <Rewind className="h-4 w-4" />
                  </BarBtn>
                </>
              )}
              <BarBtn onClick={() => setMuted((m) => !m)} label={muted ? "Unmute" : "Mute"}>
                {muted ? <VolumeX className="h-4 w-4" /> : <Volume2 className="h-4 w-4" />}
              </BarBtn>
              <BarBtn onClick={() => setCamerasOpen((o) => !o)} label="Cameras">
                <LayoutGrid className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={() => setExportOpen(true)} label="Export clip">
                <Scissors className="h-4 w-4" />
              </BarBtn>
              <BarBtn onClick={fullscreen} label="Fullscreen">
                <Maximize2 className="h-4 w-4" />
              </BarBtn>
            </div>
          </div>
        )}

        {/* timeline */}
        <div className="relative flex-1 min-h-0">
          {selected && (
            <VerticalTimeline
              selectedMs={selectedMs}
              windowMs={windowMs}
              nowMs={nowMs}
              segments={recorded}
              events={tlEvents}
              live={atLiveEdge}
              following={following}
              ribbon={ribbon}
              onScrubTo={scrubToTime}
              onScrubEnd={endScrub}
              onSeekAbs={seekToTime}
              onEventClick={seekToTime}
            />
          )}
          <ZoomControl
            value={zoomIdx}
            max={ZOOMS.length - 1}
            onChange={setZoomIdx}
            className="absolute top-3 right-3 z-20"
          />
          {!atLiveEdge && (
            <button
              onClick={goLive}
              className="absolute bottom-16 right-3 h-10 w-10 rounded-full bg-brand text-white grid place-items-center shadow-pop"
              aria-label="Jump to live"
            >
              <ArrowUp className="h-4 w-4" />
            </button>
          )}
          <button
            onClick={openDatePicker}
            className="absolute bottom-3 left-3 flex items-center gap-2 rounded-full bg-ink-700/90 px-3.5 py-2.5 text-xs text-fg backdrop-blur-sm"
          >
            <Calendar className="h-3.5 w-3.5" />
            {fmtDay(selectedMs)}
          </button>
          <button
            onClick={snapshot}
            className="absolute bottom-3 right-3 h-10 w-10 rounded-full bg-ink-700/90 text-fg-muted grid place-items-center backdrop-blur-sm"
            aria-label="Snapshot"
          >
            <CameraIcon className="h-4 w-4" />
          </button>
          {datePickerPopover}
        </div>
      </div>
    );
  }

  // ---- Desktop layout -----------------------------------------------------
  return (
    <div className="flex h-full min-h-0 flex-col">
      {exportModal}
      <CameraModal open={settingsOpen} onClose={() => setSettingsOpen(false)} camera={selected} />
      <AddCamerasModal
        open={addCamsOpen}
        cameras={cameras ?? []}
        initial={multiActive ? multiCams : selectedId ? [selectedId] : []}
        active={multiActive}
        onApply={applyMulti}
        onEnd={endMulti}
        onClose={() => setAddCamsOpen(false)}
      />
      <ConfirmDialog
        open={confirmRemove}
        danger
        busy={removeBusy}
        title="Remove footage?"
        message={`Permanently delete ${selected?.name ?? "this camera"}'s recordings from ${fmtClock(
          removeFrom,
        )} to ${fmtClock(removeTo)}. Saved (locked) clips are kept. This can't be undone.`}
        confirmLabel="Remove"
        onConfirm={removeRecordings}
        onCancel={() => setConfirmRemove(false)}
      />

      <TopBar title="Playback" />

      {/* Top half: rail + timeline + player. Events panel is a full-width section below. */}
      <div className={`flex min-h-0 flex-1 ${railRight ? "flex-row-reverse" : ""}`}>
        {showRail && (
          <CameraRail
            cameras={cameras ?? []}
            selectedId={selectedId}
            onSelect={setSelectedId}
            multiIds={multiActive ? multiCams : undefined}
          />
        )}

      {/* timeline column */}
      <div className="relative w-[220px] shrink-0 border-x border-line flex flex-col bg-ink-900">
        <div className="px-3 pt-3 pb-2.5 space-y-2.5">
          <div className="flex items-center justify-between">
            <button
              onClick={() => setDateOpen((o) => !o)}
              className="text-sm font-medium text-fg hover:text-brand"
            >
              {fmtDate(selectedMs)}
            </button>
            <div className="flex items-center gap-0.5">
              <button
                onClick={() => setShowRail((v) => !v)}
                className={`btn-icon h-6 w-6 ${showRail ? "" : "text-brand"}`}
                aria-label={showRail ? "Hide the camera list" : "Show the camera list"}
                title={showRail ? "Hide the camera list" : "Show the camera list"}
              >
                <PanelLeft className="h-4 w-4" />
              </button>
              <button
                onClick={() => setShowEvents((v) => !v)}
                className={`btn-icon h-6 w-6 ${showEvents ? "" : "text-brand"}`}
                aria-label={showEvents ? "Hide the events panel" : "Show the events panel"}
                title={showEvents ? "Hide the events panel" : "Show the events panel"}
              >
                <PanelBottom className="h-4 w-4" />
              </button>
              <button
                onClick={() => setRailRight((r) => !r)}
                className="btn-icon h-6 w-6"
                aria-label="Flip timeline to the other side"
                title="Flip timeline to the other side"
              >
                <Columns2 className="h-4 w-4" />
              </button>
            </div>
          </div>
          {/* zoom pill */}
          <div className="flex items-center gap-2 rounded-full bg-ink-800 px-3 py-1">
            <button
              onClick={() => setZoomIdx((i) => Math.max(0, i - 1))}
              className="text-fg-faint hover:text-fg leading-none"
              aria-label="Zoom out timeline"
            >
              −
            </button>
            <input
              type="range"
              min={0}
              max={ZOOMS.length - 1}
              step={1}
              value={zoomIdx}
              onChange={(e) => setZoomIdx(Number(e.target.value))}
              className="tl-zoom flex-1 min-w-0"
              style={{
                background: `linear-gradient(to right, #1b9df0 ${(zoomIdx / (ZOOMS.length - 1)) * 100}%, #2a3038 ${
                  (zoomIdx / (ZOOMS.length - 1)) * 100
                }%)`,
              }}
            />
            <button
              onClick={() => setZoomIdx((i) => Math.min(ZOOMS.length - 1, i + 1))}
              className="text-fg-faint hover:text-fg leading-none"
              aria-label="Zoom in timeline"
            >
              +
            </button>
          </div>
        </div>

        <div className="relative flex-1 min-h-0 py-2">
          <VerticalTimeline
            selectedMs={selectedMs}
            windowMs={windowMs}
            nowMs={nowMs}
            segments={recorded}
            events={tlEvents}
            live={atLiveEdge}
            following={following}
            ribbon={ribbon}
            onScrubTo={scrubToTime}
            onScrubEnd={endScrub}
            onSeekAbs={seekToTime}
            onEventClick={seekToTime}
          />
          {/* Jump-to-live pill: only while scrubbing the past; bounces back to live. */}
          <button
            onClick={goLive}
            className={`absolute bottom-4 right-3 z-10 grid h-9 w-9 place-items-center rounded-full bg-brand text-white shadow-pop transition-all duration-200 hover:bg-[#2aa6f5] ${
              atLiveEdge ? "pointer-events-none scale-90 opacity-0" : "scale-100 opacity-100"
            }`}
            aria-label="Jump to live"
          >
            <ArrowUp className="h-4 w-4" />
          </button>
        </div>

        {/* Manage menu: download / timelapse / remove */}
        <div className="relative border-t border-line">
          <button
            onClick={() => setManageOpen((o) => !o)}
            className="flex w-full items-center justify-center gap-2 py-2.5 text-sm text-brand hover:bg-ink-850"
          >
            <Download className="h-4 w-4" />
            Manage
          </button>
          {manageOpen && (
            <>
              <div className="fixed inset-0 z-40" onClick={() => setManageOpen(false)} />
              <div className="absolute bottom-12 left-2 right-2 z-50 rounded-lg border border-line bg-ink-850 p-1.5 shadow-pop">
                <ManageItem
                  icon={<Download className="h-4 w-4" />}
                  label="Download clip"
                  onClick={() => {
                    setManageOpen(false);
                    setExportOpen(true);
                  }}
                />
                <ManageItem
                  icon={<Film className="h-4 w-4" />}
                  label="Timelapse export"
                  onClick={() => {
                    setManageOpen(false);
                    setExportOpen(true);
                  }}
                />
                <ManageItem
                  icon={<Link2 className={`h-4 w-4 ${copied ? "text-online" : ""}`} />}
                  label={copied ? "Link copied" : "Copy link to this moment"}
                  onClick={() => {
                    setManageOpen(false);
                    void copyLink();
                  }}
                />
                {isAdmin && (
                  <ManageItem
                    danger
                    icon={<Trash2 className="h-4 w-4" />}
                    label="Remove footage…"
                    onClick={() => {
                      setManageOpen(false);
                      setConfirmRemove(true);
                    }}
                  />
                )}
              </div>
            </>
          )}
        </div>

        {dateOpen && (
          <>
            <div className="fixed inset-0 z-40" onClick={() => setDateOpen(false)} />
            <div className="absolute top-12 left-3 z-50">
              <DateTimePicker
                value={selectedMs}
                max={nowMs}
                onApply={(ms) => {
                  seekToTime(ms);
                  setDateOpen(false);
                }}
                onCancel={() => setDateOpen(false)}
              />
            </div>
          </>
        )}
      </div>

      {/* player + control bar + events */}
      <div className="flex-1 min-w-0 flex flex-col">
        <div ref={containerRef} className="relative flex-1 overflow-hidden bg-black min-h-0">
          {multiActive ? (
            <MultiCamGrid
              cameras={multiView}
              atMs={selectedMs}
              following={following}
              muted={muted}
              paused={paused}
              quality={quality}
              primaryId={selectedId}
              onTime={(wall) => {
                if (scrubbing.current) return; // don't fight an active scrub
                setSelectedState(wall);
                selectedRef.current = wall;
              }}
              onRemove={(id) => setMultiCams((m) => m.filter((x) => x !== id))}
            />
          ) : (
            // Digital zoom wraps the player (and its object boxes, so they track
            // the zoom); the name/controls below stay put outside it.
            <div className="absolute inset-0" style={{ transform: zoomTransform, transformOrigin: "0 0" }}>
              {renderPlayer()}
              {boxesOn && selected && (
                <DetectionOverlay events={events} atMs={selectedMs} videoRef={playerVideoRef} fit="contain" />
              )}
            </div>
          )}
          {/* Grid Search draw overlay. */}
          {gridOpen && selected && (
            <GridSearchOverlay region={region} onApply={setRegion} onClose={() => setGridOpen(false)} />
          )}

          {selected && multiView.length <= 1 && (
            <div className="absolute top-3 left-3 flex items-center gap-2 rounded-md bg-black/35 px-2.5 py-1 backdrop-blur-sm">
              <span className="text-xs font-mono tabular-nums text-white/90">{fmtClock(overlayTime)}</span>
              <span className="text-white/30">|</span>
              <span className="text-xs font-medium text-white">{selected.name}</span>
            </div>
          )}
          {selected && !gridOpen && !multiActive && (
            <div className="absolute top-3 right-3 flex items-center gap-1">
              {/* Picture controls — only rendered when this camera's ONVIF is on
                  and actually reporting adjustable settings. */}
              <div className="relative">
                <CameraImagingOverlay cameraId={selected.id} VidBtn={VidBtn} />
              </div>
              <VidBtn
                onClick={() => setBoxesOn((v) => !v)}
                label={boxesOn ? "Hide object boxes" : "Show object boxes"}
                active={boxesOn}
              >
                <ScanEye className="h-4 w-4" />
              </VidBtn>
              <VidBtn onClick={snapshot} label="Take snapshot">
                <CameraIcon className="h-4 w-4" />
              </VidBtn>
              <CameraSpotlightButton cameraId={selected.id} VidBtn={VidBtn} />
              {isAdmin ? (
                <VidBtn onClick={() => setSettingsOpen(true)} label="Camera settings">
                  <Settings className="h-4 w-4" />
                </VidBtn>
              ) : (
                <div className="group relative">
                  <span className="grid h-8 w-8 cursor-default place-items-center rounded-md bg-black/45 text-white/50 backdrop-blur-sm">
                    <Settings className="h-4 w-4" />
                  </span>
                  <div className="pointer-events-none absolute right-0 top-9 hidden w-max max-w-[220px] rounded-md bg-black/85 px-2.5 py-1.5 text-[11px] leading-snug text-white/90 group-hover:block">
                    You don&apos;t have permission to edit this camera&apos;s settings.
                  </div>
                </div>
              )}
            </div>
          )}
          {region && !gridOpen && (
            <button
              onClick={() => setRegion(null)}
              className="absolute bottom-3 left-3 z-10 flex items-center gap-1.5 rounded-full bg-brand/90 px-3 py-1.5 text-xs font-medium text-white shadow-pop hover:bg-brand"
            >
              <Search className="h-3.5 w-3.5" />
              Motion area · {filteredEvents.length} {filteredEvents.length === 1 ? "event" : "events"}
              <span className="ml-0.5 opacity-80">✕</span>
            </button>
          )}
          {selected && multiView.length <= 1 && <KeyboardHelp />}
          {selected && !multiActive && !gridOpen && (
            <ViewerZoom zoom={zoom} pan={pan} onZoom={setZoom} onPan={setPan} />
          )}
        </div>

        {/* control bar (below the video) */}
        {selected && (
          <div className="flex items-center gap-1 h-12 px-3 border-t border-line bg-ink-900 shrink-0">
            {isLive ? (
              <span className="pill bg-online/20 text-online mr-1">
                <span className="h-1.5 w-1.5 rounded-full bg-online" />
                LIVE
              </span>
            ) : (
              <BarBtn onClick={togglePlay} label={paused ? "Play" : "Pause"}>
                {paused ? <Play className="h-4 w-4" /> : <Pause className="h-4 w-4" />}
              </BarBtn>
            )}
            <BarBtn onClick={() => setMuted((m) => !m)} label={muted ? "Unmute" : "Mute"}>
              {muted ? <VolumeX className="h-4 w-4" /> : <Volume2 className="h-4 w-4" />}
            </BarBtn>
            <SkipBtn dir={-1} onClick={() => seekToTime((isLive ? nowMs : selectedMs) - 10_000)} />
            {!isLive && <SkipBtn dir={1} onClick={() => seekToTime(selectedMs + 10_000)} />}
            {!isLive && (
              <>
                <BarBtn
                  onClick={() => {
                    const p = findPrevEvent(selectedMs);
                    if (p) seekToTime(p);
                  }}
                  label="Previous motion event"
                >
                  <ChevronFirst className="h-4 w-4" />
                </BarBtn>
                <BarBtn
                  onClick={() => {
                    const n = findNextEvent(selectedMs);
                    if (n) seekToTime(n);
                  }}
                  label="Next motion event"
                >
                  <ChevronLast className="h-4 w-4" />
                </BarBtn>
              </>
            )}
            {!isLive && (
              <button onClick={goLive} className="ml-1 h-8 rounded-md px-2.5 text-xs text-fg-muted hover:text-fg hover:bg-ink-700">
                Go live
              </button>
            )}
            <div className="ml-2 flex items-center gap-2">
              <span className="text-xs font-mono text-fg-muted tabular-nums">{fmtClock(overlayTime)}</span>
              {selected && <PlaybackConditions tsMs={selectedMs} variant="bar" cameraId={selected.id} live={isLive} />}
            </div>
            <div className="ml-auto flex items-center gap-0.5">
              {/* Playback speed — greyed out while live. Hidden in multi-view. */}
              {!multiActive && (
                <div className="relative">
                  <BarBtn
                    onClick={() => !isLive && setSpeedOpen((o) => !o)}
                    label={isLive ? "Playback speed (live)" : "Playback speed"}
                    disabled={isLive}
                    active={!isLive && playbackRate !== 1}
                  >
                    <Gauge className="h-4 w-4" />
                  </BarBtn>
                  {speedOpen && !isLive && (
                    <div className="absolute bottom-10 right-0 z-20 rounded-md border border-line bg-ink-850 p-1 shadow-pop">
                      {RATES.map((rate) => (
                        <button
                          key={rate}
                          onClick={() => {
                            setPlaybackRate(rate);
                            setSpeedOpen(false);
                          }}
                          className={`block w-full rounded px-3 py-1 text-left text-xs tabular-nums ${
                            playbackRate === rate ? "bg-brand text-white" : "text-fg-muted hover:bg-ink-700"
                          }`}
                        >
                          {rate}×
                        </button>
                      ))}
                    </div>
                  )}
                </div>
              )}
              {/* Multi-camera playback — opens the picker. */}
              <BarBtn onClick={() => setAddCamsOpen(true)} label="Multi-camera playback" active={multiActive}>
                <Grid2x2 className="h-4 w-4" />
              </BarBtn>
              {/* Grid search — single-view only. */}
              {!multiActive && (
                <BarBtn onClick={() => setGridOpen(true)} label="Grid search (motion area)" active={!!region}>
                  <Search className="h-4 w-4" />
                </BarBtn>
              )}
              {/* Change global viewer quality. */}
              <div className="relative">
                <BarBtn onClick={() => setQualityOpen((o) => !o)} label="Change global viewer quality">
                  <span className="text-[11px] font-semibold">
                    {quality === "low" ? "SD" : quality === "high" ? "HD" : "Auto"}
                  </span>
                </BarBtn>
                {qualityOpen && (
                  <>
                    <div className="fixed inset-0 z-10" onClick={() => setQualityOpen(false)} />
                    <div className="absolute bottom-10 right-0 z-20 w-32 rounded-md border border-line bg-ink-850 p-1 shadow-pop">
                      {(["high", "auto", "low"] as const).map((opt) => (
                        <button
                          key={opt}
                          onClick={() => {
                            setQuality(opt);
                            setQualityOpen(false);
                          }}
                          className={`block w-full rounded px-3 py-1.5 text-left text-xs ${
                            quality === opt ? "bg-brand text-white" : "text-fg-muted hover:bg-ink-700"
                          }`}
                        >
                          {opt === "high" ? "High (HD)" : opt === "low" ? "Low (SD)" : "Auto"}
                        </button>
                      ))}
                    </div>
                  </>
                )}
              </div>
              <BarBtn onClick={fullscreen} label="Fullscreen">
                <Maximize2 className="h-4 w-4" />
              </BarBtn>
            </div>
          </div>
        )}
        </div>
      </div>

      {/* Bottom half: events panel spans the full width, under everything. Drag its
          top edge to grow it into a multi-row grid. */}
      {showEvents && (
        <EventStrip dateLabel={fmtDate(selectedMs)} events={filteredEvents} onSelect={(ms) => seekToTime(ms)} />
      )}
    </div>
  );
}

function BarBtn({
  onClick,
  label,
  children,
  active,
  disabled,
}: {
  onClick: () => void;
  label: string;
  children: React.ReactNode;
  active?: boolean;
  disabled?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      title={label}
      disabled={disabled}
      className={`grid h-8 min-w-8 place-items-center rounded-md px-1.5 transition-colors ${
        disabled
          ? "cursor-not-allowed text-fg-faint/40"
          : active
            ? "bg-brand/15 text-brand hover:bg-brand/25"
            : "text-fg-muted hover:bg-ink-700 hover:text-fg"
      }`}
    >
      {children}
    </button>
  );
}

// A row in the Manage dropdown menu.
function ManageItem({
  icon,
  label,
  onClick,
  danger,
}: {
  icon: React.ReactNode;
  label: string;
  onClick: () => void;
  danger?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      className={`flex w-full items-center gap-2.5 rounded-md px-2.5 py-2 text-sm ${
        danger ? "text-rec hover:bg-rec/10" : "text-fg-muted hover:bg-ink-700 hover:text-fg"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}

// Overlay button for the video's top-right cluster (dark pill, light icon).
function VidBtn({
  onClick,
  label,
  children,
  active,
}: {
  onClick: () => void;
  label: string;
  children: React.ReactNode;
  active?: boolean;
}) {
  return (
    <button
      onClick={onClick}
      aria-label={label}
      title={label}
      className={`grid h-8 w-8 place-items-center rounded-md backdrop-blur-sm transition-colors ${
        active ? "bg-brand text-white hover:bg-[#2aa6f5]" : "bg-black/45 text-white/85 hover:bg-black/70"
      }`}
    >
      {children}
    </button>
  );
}

// UniFi-style ±10s skip: a circular arrow with a "10" tucked inside.
function SkipBtn({ dir, onClick }: { dir: 1 | -1; onClick: () => void }) {
  const Icon = dir < 0 ? RotateCcw : RotateCw;
  return (
    <button
      onClick={onClick}
      aria-label={dir < 0 ? "Back 10 seconds" : "Forward 10 seconds"}
      className="relative h-8 w-8 grid place-items-center rounded-md text-fg-muted hover:text-fg hover:bg-ink-700"
    >
      <Icon className="h-[18px] w-[18px]" />
      <span className="absolute text-[7px] font-bold leading-none">10</span>
    </button>
  );
}

// Keyboard-shortcuts helper (UniFi's bottom-left video icon): hover to reveal the
// same shortcuts the keydown handler wires.
function KeyboardHelp() {
  return (
    <div className="group absolute bottom-3 left-3 z-10">
      <button
        className="h-8 w-8 grid place-items-center rounded-md bg-black/45 text-white/80 backdrop-blur-sm hover:bg-black/70"
        aria-label="Keyboard shortcuts"
      >
        <Keyboard className="h-4 w-4" />
      </button>
      <div className="pointer-events-none absolute bottom-10 left-0 hidden w-max rounded-lg border border-line bg-ink-850/95 p-3 shadow-pop backdrop-blur group-hover:block">
        <div className="grid grid-cols-[1fr_auto] items-center gap-x-5 gap-y-2 text-xs">
          <span className="text-fg-muted">Skip ±1s</span>
          <span className="flex gap-1"><Kbd>↑</Kbd><Kbd>↓</Kbd></span>
          <span className="text-fg-muted">Frame ±1</span>
          <span className="flex gap-1"><Kbd>←</Kbd><Kbd>→</Kbd></span>
          <span className="text-fg-muted">Slow / Speed up</span>
          <span className="flex gap-1"><Kbd>J</Kbd><Kbd>L</Kbd></span>
          <span className="text-fg-muted">Play / Pause</span>
          <Kbd>Space</Kbd>
        </div>
      </div>
    </div>
  );
}

function Kbd({ children }: { children: React.ReactNode }) {
  return (
    <span className="inline-grid h-6 min-w-[22px] place-items-center rounded border border-line-strong bg-ink-800 px-1.5 text-[11px] font-medium text-fg">
      {children}
    </span>
  );
}

function MobileThumb({ cameraId, enabled }: { cameraId: string; enabled: boolean }) {
  const [tick, setTick] = useState(0);
  const [ok, setOk] = useState(true);
  useEffect(() => {
    if (!enabled) return;
    const i = setInterval(() => setTick((t) => t + 1), 5000);
    return () => clearInterval(i);
  }, [enabled]);
  if (!enabled || !ok) {
    return (
      <div className="absolute inset-0 grid place-items-center bg-ink-800">
        <Cctv className="h-5 w-5 text-fg-faint" />
      </div>
    );
  }
  return (
    <img
      src={`/api/v1/cameras/${cameraId}/snapshot.jpg?t=${tick}`}
      alt=""
      className="h-full w-full object-cover"
      onError={() => setOk(false)}
    />
  );
}
