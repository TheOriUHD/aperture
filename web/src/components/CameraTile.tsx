import { useRef } from "react";
import { useNavigate } from "react-router-dom";
import { Camera as CameraIcon, Maximize2, Play } from "lucide-react";
import { LiveVideo } from "./LiveVideo";
import { LiveConditions } from "./ConditionsOverlay";
import { LiveObjectsOverlay } from "./LiveObjectsOverlay";
import { ParkedBadge } from "./ObjectBox";
import type { Camera } from "../lib/types";
import type { RecordingState } from "../lib/recordingState";

// One live camera on the dashboard wall. The picture is edge to edge; its name
// and actions appear on hover (always on a touch screen, which has no hover).
// Two things stay visible without hovering: the conditions readout, and a
// recording mark — a quiet red dot while recording, a labelled warning if
// recording should be running and is not.
export function CameraTile({
  camera,
  onOpen,
  recState,
  className,
}: {
  camera: Camera;
  onOpen?: (c: Camera) => void;
  recState?: RecordingState;
  className?: string;
}) {
  const navigate = useNavigate();
  const videoRef = useRef<HTMLVideoElement>(null);
  const failed = recState === "failed";
  const recording = recState ? recState === "recording" : camera.recordingNow;

  return (
    <div
      className={`group relative overflow-hidden bg-ink-950 ${onOpen ? "cursor-pointer" : ""} ${className ?? ""}`}
      onClick={() => onOpen?.(camera)}
    >
      <LiveVideo camera={camera} videoRef={videoRef} />
      {/* White outline while it moves; labelled boxes round what it recognises. */}
      <LiveObjectsOverlay cameraId={camera.id} videoRef={videoRef} fit="cover" />

      {/* Hover chrome: name + actions, top left. */}
      <div className="absolute left-2.5 top-2.5 flex items-center gap-1.5 opacity-0 transition-opacity duration-150 group-hover:opacity-100 [@media(hover:none)]:opacity-100">
        <span className="osd-chip max-w-[220px] truncate">{camera.name}</span>
        <span className="osd-chip hidden gap-0 px-1 lg:inline-flex" onClick={(e) => e.stopPropagation()}>
          <TileAction label="Playback" onClick={() => navigate(`/playback?camera=${camera.id}`)}>
            <Play className="h-4 w-4" />
          </TileAction>
          {onOpen && (
            <TileAction label="Expand" onClick={() => onOpen(camera)}>
              <Maximize2 className="h-4 w-4" />
            </TileAction>
          )}
          <a
            href={`/api/v1/cameras/${camera.id}/snapshot.jpg`}
            download={`${camera.name}-${new Date().toISOString().slice(0, 19).replace(/[:T]/g, "-")}.jpg`}
            title="Snapshot"
            aria-label="Snapshot"
            className="grid h-7 w-7 place-items-center rounded text-white/85 hover:bg-white/10 hover:text-white"
          >
            <CameraIcon className="h-4 w-4" />
          </a>
        </span>
      </div>

      {/* Parked vehicle and recording marks, top right. */}
      <div className="absolute right-2.5 top-2.5 flex items-center gap-2">
        <ParkedBadge cameraId={camera.id} />
        {failed ? (
          <span className="pill-red h-6 px-2" title="Recording should be running but is not">
            <span className="h-1.5 w-1.5 rounded-full bg-danger" />
            Not recording
          </span>
        ) : recording ? (
          <span
            className="block h-2.5 w-2.5 rounded-full bg-rec shadow-[0_0_0_3px_rgba(0,0,0,0.35)] animate-rec-pulse"
            title="Recording"
          />
        ) : null}
      </div>

      {/* Conditions readout (bottom-right). */}
      <LiveConditions variant="compact" className="bottom-2.5 right-3" cameraId={camera.id} />
    </div>
  );
}

function TileAction({ label, onClick, children }: { label: string; onClick: () => void; children: React.ReactNode }) {
  return (
    <button
      type="button"
      title={label}
      aria-label={label}
      onClick={onClick}
      className="grid h-7 w-7 place-items-center rounded text-white/85 hover:bg-white/10 hover:text-white"
    >
      {children}
    </button>
  );
}
