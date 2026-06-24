import { useState } from "react";
import { Link2, Copy, Check } from "lucide-react";
import { Modal } from "./ui/Modal";
import { Switch } from "./ui/Switch";
import { useCreateShare, type ExportJob, type CreatedShare } from "../hooks/useExports";

const EXPIRY = [
  { label: "1 hour", hours: 1 },
  { label: "24 hours", hours: 24 },
  { label: "7 days", hours: 168 },
  { label: "Never", hours: 0 },
];

export function ShareModal({ job, onClose }: { job: ExportJob; onClose: () => void }) {
  const create = useCreateShare();
  const [expiresHours, setExpiresHours] = useState(24);
  const [usePassword, setUsePassword] = useState(false);
  const [password, setPassword] = useState("");
  const [limitViews, setLimitViews] = useState(false);
  const [maxViews, setMaxViews] = useState(5);
  const [result, setResult] = useState<CreatedShare | null>(null);
  const [copied, setCopied] = useState(false);

  async function copy() {
    if (!result) return;
    try {
      await navigator.clipboard.writeText(result.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* ignore */
    }
  }

  return (
    <Modal
      open
      onClose={onClose}
      title="Share clip"
      footer={
        result ? (
          <button className="btn-primary" onClick={onClose}>
            Done
          </button>
        ) : (
          <>
            <button className="btn-subtle" onClick={onClose}>
              Cancel
            </button>
            <button
              className="btn-primary"
              disabled={create.isPending || (usePassword && password.length < 1)}
              onClick={async () => {
                const r = await create.mutateAsync({
                  exportId: job.id,
                  expiresHours,
                  password: usePassword ? password : undefined,
                  maxViews: limitViews ? maxViews : 0,
                });
                setResult(r);
              }}
            >
              {create.isPending ? "Creating…" : "Create link"}
            </button>
          </>
        )
      }
    >
      {result ? (
        <div className="space-y-3">
          <p className="text-sm text-fg-muted">
            Anyone with this link can view the clip
            {result.expiresAt ? "" : ""} — no account needed.
          </p>
          <div className="flex items-center gap-2">
            <input className="input font-mono text-xs flex-1" readOnly value={result.url} />
            <button className="btn-icon h-9 w-9" onClick={copy} aria-label="Copy link">
              {copied ? <Check className="h-4 w-4 text-online" /> : <Copy className="h-4 w-4" />}
            </button>
          </div>
          <p className="text-xs text-fg-faint">
            Manage or revoke shared links from your account at any time.
          </p>
        </div>
      ) : (
        <div className="space-y-4">
          <div className="flex items-center gap-2 text-sm text-fg-muted">
            <Link2 className="h-4 w-4 text-brand" />
            {job.cameraName}
          </div>

          <div>
            <label className="label">Link expires</label>
            <div className="grid grid-cols-4 gap-2">
              {EXPIRY.map((e) => (
                <button
                  key={e.hours}
                  onClick={() => setExpiresHours(e.hours)}
                  className={`btn h-9 text-xs ${
                    expiresHours === e.hours
                      ? "bg-brand/15 text-brand border border-brand/40"
                      : "bg-ink-800 text-fg-muted border border-line-strong hover:bg-ink-700"
                  }`}
                >
                  {e.label}
                </button>
              ))}
            </div>
          </div>

          <div className="flex items-center justify-between">
            <span className="text-sm text-fg">Require a password</span>
            <Switch checked={usePassword} onChange={setUsePassword} label="Require password" />
          </div>
          {usePassword && (
            <input
              className="input"
              type="text"
              placeholder="Password to view"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
            />
          )}

          <div className="flex items-center justify-between">
            <span className="text-sm text-fg">Limit number of views</span>
            <Switch checked={limitViews} onChange={setLimitViews} label="Limit views" />
          </div>
          {limitViews && (
            <input
              className="input w-28"
              type="number"
              min={1}
              value={maxViews}
              onChange={(e) => setMaxViews(Math.max(1, Number(e.target.value)))}
            />
          )}
        </div>
      )}
    </Modal>
  );
}
