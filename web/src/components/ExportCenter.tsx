import { useEffect, useState } from "react";
import { Scissors, Download, Trash2, Share2, Loader2, AlertCircle, Link2, Lock, Check } from "lucide-react";
import { Modal } from "./ui/Modal";
import { Switch } from "./ui/Switch";
import { ShareModal } from "./ShareModal";
import {
  useExports,
  useCreateExport,
  useDeleteExport,
  useShares,
  useDeleteShare,
  type ExportJob,
} from "../hooks/useExports";
import { fmtTime, fmtDuration, fmtRelative, formatBytes } from "../lib/format";
import { useDownloads } from "../lib/downloads";
import type { Camera } from "../lib/types";

const PRESETS = [
  { label: "30 sec", ms: 30_000 },
  { label: "1 min", ms: 60_000 },
  { label: "5 min", ms: 300_000 },
  { label: "15 min", ms: 900_000 },
];

function toLocalInput(ms: number): string {
  const d = new Date(ms);
  const p = (n: number) => String(n).padStart(2, "0");
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())}T${p(d.getHours())}:${p(
    d.getMinutes(),
  )}:${p(d.getSeconds())}`;
}

export function ExportCenter({
  open,
  onClose,
  camera,
  cameras,
  defaultTo,
}: {
  open: boolean;
  onClose: () => void;
  camera: Camera | null;
  cameras: Camera[];
  defaultTo: number;
}) {
  const [selectedCams, setSelectedCams] = useState<string[]>(camera ? [camera.id] : []);
  const [composite, setComposite] = useState(false);
  const [from, setFrom] = useState(defaultTo - 60_000);
  const [to, setTo] = useState(defaultTo);
  const [burnIn, setBurnIn] = useState(false);
  const [audio, setAudio] = useState(true);
  const [speed, setSpeed] = useState(1); // 1 = realtime; N = timelapse N×
  const [shareJob, setShareJob] = useState<ExportJob | null>(null);

  const { data: jobs } = useExports();
  const { data: shares } = useShares();
  const create = useCreateExport();
  const del = useDeleteExport();
  const delShare = useDeleteShare();
  const { start: startDownload } = useDownloads();

  // Seed the range only when the modal opens — not on every live tick of
  // defaultTo, which would otherwise reset the inputs the user is editing.
  useEffect(() => {
    if (!open) return;
    setSelectedCams(camera ? [camera.id] : cameras[0] ? [cameras[0].id] : []);
    setComposite(false);
    setSpeed(1);
    setTo(defaultTo);
    setFrom(defaultTo - 60_000);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [open]);

  const SPEEDS = [
    { label: "Realtime", v: 1 },
    { label: "8×", v: 8 },
    { label: "30×", v: 30 },
    { label: "60×", v: 60 },
    { label: "120×", v: 120 },
  ];

  const durMs = to - from;
  const multi = selectedCams.length > 1;
  const mode: "single" | "zip" | "composite" = multi ? (composite ? "composite" : "zip") : "single";
  const invalid =
    selectedCams.length === 0 ||
    durMs <= 0 ||
    durMs > 4 * 3600_000 ||
    (composite && selectedCams.length > 4);

  function toggleCam(id: string) {
    setSelectedCams((s) => (s.includes(id) ? s.filter((x) => x !== id) : [...s, id]));
  }

  return (
    <>
      <Modal open={open} onClose={onClose} title="Export & download clips">
        <div className="space-y-5">
          {/* create form */}
          <div className="space-y-4">
            <div>
              <label className="label">Cameras</label>
              <div className="flex flex-wrap gap-1.5">
                {cameras.map((c) => {
                  const on = selectedCams.includes(c.id);
                  return (
                    <button
                      key={c.id}
                      onClick={() => toggleCam(c.id)}
                      aria-pressed={on}
                      className={`inline-flex h-7 items-center gap-1.5 rounded-md border px-2.5 text-xs transition-colors ${
                        on
                          ? "border-brand/60 bg-brand/10 font-medium text-brand"
                          : "border-line-strong text-fg-muted hover:border-fg-faint hover:text-fg"
                      }`}
                    >
                      {on && <Check className="h-3 w-3" strokeWidth={3} />}
                      {c.name}
                    </button>
                  );
                })}
              </div>
            </div>

            <div className="grid grid-cols-1 sm:grid-cols-2 gap-3">
              <div className="min-w-0">
                <label className="label">From</label>
                <input
                  className="input text-xs min-w-0"
                  type="datetime-local"
                  step={1}
                  value={toLocalInput(from)}
                  onChange={(e) => setFrom(new Date(e.target.value).getTime())}
                />
              </div>
              <div className="min-w-0">
                <label className="label">To</label>
                <input
                  className="input text-xs min-w-0"
                  type="datetime-local"
                  step={1}
                  value={toLocalInput(to)}
                  onChange={(e) => setTo(new Date(e.target.value).getTime())}
                />
              </div>
            </div>

            <div className="flex flex-wrap items-center gap-2">
              <span className="text-xs text-fg-muted">Last</span>
              <div className="seg h-7 text-xs">
                {PRESETS.map((p) => (
                  <button
                    key={p.ms}
                    onClick={() => setFrom(to - p.ms)}
                    className={`seg-item px-2.5 ${durMs === p.ms ? "seg-item-active font-medium" : ""}`}
                  >
                    {p.label}
                  </button>
                ))}
              </div>
            </div>

            {multi && (
              <div className="flex items-center justify-between gap-4">
                <div>
                  <span className="text-sm text-fg">Combine into one multi-view</span>
                  <p className="hint">
                    {composite
                      ? "Grid montage — transcoded, no audio, up to 4 cameras"
                      : "Off = a .zip with one clip per camera"}
                  </p>
                </div>
                <Switch checked={composite} onChange={setComposite} label="Combine into one multi-view" />
              </div>
            )}

            {!composite && (
              <>
                <div className="flex items-center justify-between gap-4">
                  <div>
                    <span className="text-sm text-fg">Burn in timestamp</span>
                    <p className="hint">Overlays camera name + time onto each clip.</p>
                  </div>
                  <Switch checked={burnIn} onChange={setBurnIn} label="Burn in timestamp" />
                </div>
                <div className="flex items-center justify-between gap-4">
                  <span className="text-sm text-fg">Include audio</span>
                  <Switch checked={audio} onChange={setAudio} label="Include audio" disabled={speed > 1} />
                </div>
                <div>
                  <div className="mb-2 flex items-baseline justify-between">
                    <span className="text-sm text-fg">Timelapse</span>
                    <span className="text-xs tabular-nums text-fg-faint">
                      {speed > 1 ? `${fmtDuration(durMs)} → ~${fmtDuration(Math.round(durMs / speed))}` : "off"}
                    </span>
                  </div>
                  <div className="seg flex h-7 w-full text-xs">
                    {SPEEDS.map((s) => (
                      <button
                        key={s.v}
                        onClick={() => setSpeed(s.v)}
                        className={`seg-item flex-1 px-2 ${speed === s.v ? "seg-item-active font-medium" : ""}`}
                      >
                        {s.label}
                      </button>
                    ))}
                  </div>
                </div>
              </>
            )}

            <button
              className="btn-primary w-full"
              disabled={invalid || create.isPending}
              onClick={() => create.mutate({ cameraIds: selectedCams, mode, from, to, burnIn, audio, speed })}
            >
              <Scissors className="h-4 w-4" />
              {durMs > 0 && !invalid
                ? `Export ${fmtDuration(durMs)} ${multi && !composite ? "clips" : "clip"}`
                : "Export"}
              {!invalid && mode === "single" && !burnIn && speed === 1 && (
                <span className="text-[11px] font-normal text-white/70"> · instant copy</span>
              )}
              {!invalid && speed > 1 && (
                <span className="text-[11px] font-normal text-white/70"> · {speed}× timelapse</span>
              )}
              {!invalid && mode === "zip" && (
                <span className="text-[11px] font-normal text-white/70"> · {selectedCams.length} clips · .zip</span>
              )}
              {!invalid && mode === "composite" && (
                <span className="text-[11px] font-normal text-white/70"> · multi-view</span>
              )}
            </button>
            {durMs > 4 * 3600_000 && (
              <p className="text-xs text-danger">Range too long — 4 hours max per export.</p>
            )}
            {composite && selectedCams.length > 4 && (
              <p className="text-xs text-danger">Multi-view supports up to 4 cameras.</p>
            )}
            {create.isPending && <p className="text-xs text-fg-faint">Starting export…</p>}
            {create.isError && (
              <p className="text-xs text-danger">Couldn’t start the export — please try again.</p>
            )}
            {create.isSuccess && !create.isPending && (
              <p className="text-xs text-online">Export started — see “Recent clips” below.</p>
            )}
          </div>

          {/* jobs list — always shown so there's a stable place clips appear */}
          <div className="border-t border-line pt-4">
            <p className="mb-2 text-[13px] font-semibold text-fg">Recent clips</p>
            {!jobs || jobs.length === 0 ? (
              <p className="hint">Your exported clips will appear here.</p>
            ) : (
              <div className="max-h-64 divide-y divide-line overflow-auto rounded-md border border-line">
                {jobs.map((j) => (
                  <div
                    key={j.id}
                    className="flex items-center gap-2 px-3 py-2 text-xs"
                  >
                    <div className="flex-1 min-w-0">
                      <div className="truncate text-fg">
                        {j.cameraName}
                        <span className="text-fg-faint">
                          {" "}
                          · {fmtTime(j.fromTs)}–{fmtTime(j.toTs)}
                        </span>
                      </div>
                      <JobStatus job={j} />
                    </div>
                    {j.status === "done" && (
                      <>
                        <button
                          className="btn-icon h-7 w-7"
                          onClick={() => {
                            onClose();
                            startDownload(
                              `/api/export/${j.id}/download`,
                              `${j.cameraName}_clip.mp4`,
                              j.cameraName,
                            );
                          }}
                          aria-label="Download"
                        >
                          <Download className="h-3.5 w-3.5" />
                        </button>
                        <button
                          className="btn-icon h-7 w-7"
                          onClick={() => setShareJob(j)}
                          aria-label="Share"
                        >
                          <Share2 className="h-3.5 w-3.5" />
                        </button>
                      </>
                    )}
                    <button
                      className="btn-icon h-7 w-7"
                      onClick={() => del.mutate(j.id)}
                      aria-label="Delete"
                    >
                      <Trash2 className="h-3.5 w-3.5" />
                    </button>
                  </div>
                ))}
              </div>
            )}
          </div>

          {/* shared links */}
          {shares && shares.filter((sh) => !sh.revoked).length > 0 && (
            <div className="border-t border-line pt-4">
              <p className="mb-2 flex items-center gap-1.5 text-[13px] font-semibold text-fg">
                <Link2 className="h-3.5 w-3.5 text-fg-muted" /> Shared links
              </p>
              <div className="max-h-40 divide-y divide-line overflow-auto rounded-md border border-line">
                {shares
                  .filter((sh) => !sh.revoked)
                  .map((sh) => (
                    <div
                      key={sh.id}
                      className="flex items-center gap-2 px-3 py-2 text-xs"
                    >
                      <div className="flex-1 min-w-0">
                        <div className="truncate text-fg flex items-center gap-1.5">
                          {sh.hasPassword && <Lock className="h-3 w-3 text-fg-faint" />}
                          {sh.label}
                        </div>
                        <div className="text-fg-faint">
                          {sh.expiresAt ? `expires ${fmtRelative(sh.expiresAt * 1000)}` : "never expires"} ·{" "}
                          {sh.views} view{sh.views === 1 ? "" : "s"}
                          {sh.maxViews ? ` / ${sh.maxViews}` : ""}
                        </div>
                      </div>
                      <button
                        className="btn-icon h-7 w-7"
                        onClick={() => delShare.mutate(sh.id)}
                        aria-label="Revoke link"
                      >
                        <Trash2 className="h-3.5 w-3.5" />
                      </button>
                    </div>
                  ))}
              </div>
            </div>
          )}
        </div>
      </Modal>

      {shareJob && <ShareModal job={shareJob} onClose={() => setShareJob(null)} />}
    </>
  );
}

function JobStatus({ job }: { job: ExportJob }) {
  if (job.status === "done") {
    return <span className="text-online">Ready · {formatBytes(job.sizeBytes)}</span>;
  }
  if (job.status === "error") {
    return (
      <span className="flex items-center gap-1 text-danger">
        <AlertCircle className="h-3 w-3" /> {job.error || "Failed"}
      </span>
    );
  }
  if (job.status === "queued") {
    return <span className="text-fg-faint">Queued…</span>;
  }
  // running
  if (job.burnIn) {
    return (
      <div className="flex items-center gap-2">
        <div className="h-1 flex-1 rounded-full bg-ink-700 overflow-hidden max-w-32">
          <div className="h-full bg-brand" style={{ width: `${Math.round(job.progress * 100)}%` }} />
        </div>
        <span className="text-fg-faint">{Math.round(job.progress * 100)}%</span>
      </div>
    );
  }
  return (
    <span className="flex items-center gap-1 text-fg-faint">
      <Loader2 className="h-3 w-3 animate-spin" /> Processing…
    </span>
  );
}
