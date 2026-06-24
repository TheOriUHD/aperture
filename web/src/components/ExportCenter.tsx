import { useEffect, useState } from "react";
import { Scissors, Download, Trash2, Share2, Loader2, AlertCircle, Link2, Lock } from "lucide-react";
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
  const [cameraId, setCameraId] = useState(camera?.id ?? "");
  const [from, setFrom] = useState(defaultTo - 60_000);
  const [to, setTo] = useState(defaultTo);
  const [burnIn, setBurnIn] = useState(false);
  const [audio, setAudio] = useState(true);
  const [shareJob, setShareJob] = useState<ExportJob | null>(null);

  const { data: jobs } = useExports();
  const { data: shares } = useShares();
  const create = useCreateExport();
  const del = useDeleteExport();
  const delShare = useDeleteShare();

  useEffect(() => {
    if (open) {
      setCameraId(camera?.id ?? cameras[0]?.id ?? "");
      setTo(defaultTo);
      setFrom(defaultTo - 60_000);
    }
  }, [open, camera?.id, defaultTo, cameras]);

  const durMs = to - from;
  const invalid = !cameraId || durMs <= 0 || durMs > 4 * 3600_000;

  return (
    <>
      <Modal open={open} onClose={onClose} title="Export & download clips">
        <div className="space-y-4">
          {/* create form */}
          <div className="space-y-3">
            <div>
              <label className="label">Camera</label>
              <select className="input" value={cameraId} onChange={(e) => setCameraId(e.target.value)}>
                {cameras.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
            </div>

            <div className="grid grid-cols-2 gap-3">
              <div>
                <label className="label">From</label>
                <input
                  className="input text-xs"
                  type="datetime-local"
                  step={1}
                  value={toLocalInput(from)}
                  onChange={(e) => setFrom(new Date(e.target.value).getTime())}
                />
              </div>
              <div>
                <label className="label">To</label>
                <input
                  className="input text-xs"
                  type="datetime-local"
                  step={1}
                  value={toLocalInput(to)}
                  onChange={(e) => setTo(new Date(e.target.value).getTime())}
                />
              </div>
            </div>

            <div className="flex flex-wrap gap-2">
              <span className="text-xs text-fg-faint self-center mr-1">Ending here, grab:</span>
              {PRESETS.map((p) => (
                <button
                  key={p.ms}
                  onClick={() => setFrom(to - p.ms)}
                  className={`btn h-7 px-2.5 text-xs ${
                    durMs === p.ms
                      ? "bg-brand/15 text-brand border border-brand/40"
                      : "bg-ink-800 text-fg-muted border border-line-strong hover:bg-ink-700"
                  }`}
                >
                  {p.label}
                </button>
              ))}
            </div>

            <div className="flex items-center justify-between">
              <div>
                <span className="text-sm text-fg">Burn in timestamp</span>
                <p className="text-xs text-fg-faint">Overlays camera + time (re-encodes; slower)</p>
              </div>
              <Switch checked={burnIn} onChange={setBurnIn} label="Burn in timestamp" />
            </div>
            <div className="flex items-center justify-between">
              <span className="text-sm text-fg">Include audio</span>
              <Switch checked={audio} onChange={setAudio} label="Include audio" />
            </div>

            <button
              className="btn-primary w-full"
              disabled={invalid || create.isPending}
              onClick={() => create.mutate({ cameraId, from, to, burnIn, audio })}
            >
              <Scissors className="h-4 w-4" />
              {durMs > 0 && !invalid ? `Export ${fmtDuration(durMs)} clip` : "Export clip"}
              {!burnIn && !invalid && (
                <span className="text-[11px] text-ink-900/70"> · instant copy</span>
              )}
            </button>
            {durMs > 4 * 3600_000 && (
              <p className="text-xs text-rec">Range too long — 4 hours max per export.</p>
            )}
          </div>

          {/* jobs list */}
          {jobs && jobs.length > 0 && (
            <div className="border-t border-line pt-3">
              <p className="text-xs font-medium text-fg-muted mb-2">Recent clips</p>
              <div className="space-y-1.5 max-h-64 overflow-auto">
                {jobs.map((j) => (
                  <div
                    key={j.id}
                    className="flex items-center gap-2 rounded-lg bg-ink-850 px-2.5 py-2 text-xs"
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
                        <a
                          className="btn-icon h-7 w-7"
                          href={`/api/export/${j.id}/download`}
                          aria-label="Download"
                        >
                          <Download className="h-3.5 w-3.5" />
                        </a>
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
            </div>
          )}

          {/* shared links */}
          {shares && shares.filter((sh) => !sh.revoked).length > 0 && (
            <div className="border-t border-line pt-3">
              <p className="text-xs font-medium text-fg-muted mb-2 flex items-center gap-1.5">
                <Link2 className="h-3.5 w-3.5" /> Shared links
              </p>
              <div className="space-y-1.5 max-h-40 overflow-auto">
                {shares
                  .filter((sh) => !sh.revoked)
                  .map((sh) => (
                    <div
                      key={sh.id}
                      className="flex items-center gap-2 rounded-lg bg-ink-850 px-2.5 py-2 text-xs"
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
      <span className="flex items-center gap-1 text-rec">
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
