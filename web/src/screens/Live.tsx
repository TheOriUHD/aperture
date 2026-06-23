import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { LayoutGrid, Grid2x2, Square, Cctv, Plus } from "lucide-react";
import clsx from "clsx";
import { TopBar } from "../components/TopBar";
import { CameraTile } from "../components/CameraTile";
import { EmptyState } from "../components/ui/EmptyState";
import { Spinner } from "../components/ui/Spinner";
import { useCameras } from "../hooks/useCameras";

type GridSize = 1 | 2 | 3;

const gridClass: Record<GridSize, string> = {
  1: "grid-cols-1",
  2: "grid-cols-1 sm:grid-cols-2",
  3: "grid-cols-1 sm:grid-cols-2 lg:grid-cols-3",
};

export function Live() {
  const { data: cameras, isLoading } = useCameras();
  const [size, setSize] = useState<GridSize>(2);

  const enabled = useMemo(() => cameras ?? [], [cameras]);

  return (
    <>
      <TopBar
        title="Live view"
        right={
          <div className="flex items-center gap-1 bg-ink-800 rounded-md p-0.5 border border-line">
            {([1, 2, 3] as GridSize[]).map((s) => (
              <button
                key={s}
                onClick={() => setSize(s)}
                className={clsx(
                  "h-7 w-7 grid place-items-center rounded",
                  size === s ? "bg-ink-700 text-fg" : "text-fg-faint hover:text-fg",
                )}
                aria-label={`${s} column grid`}
              >
                {s === 1 ? (
                  <Square className="h-4 w-4" />
                ) : s === 2 ? (
                  <Grid2x2 className="h-4 w-4" />
                ) : (
                  <LayoutGrid className="h-4 w-4" />
                )}
              </button>
            ))}
          </div>
        }
      />
      <div className="flex-1 overflow-auto p-4">
        {isLoading ? (
          <div className="h-full grid place-items-center">
            <Spinner label="Loading cameras" />
          </div>
        ) : enabled.length === 0 ? (
          <EmptyState
            icon={Cctv}
            title="No cameras yet"
            hint="Add your first RTSP camera to start watching and recording."
            action={
              <Link to="/cameras" className="btn-primary">
                <Plus className="h-4 w-4" />
                Add camera
              </Link>
            }
          />
        ) : (
          <div className={clsx("grid gap-3", gridClass[size])}>
            {enabled.map((cam) => (
              <CameraTile key={cam.id} camera={cam} />
            ))}
          </div>
        )}
      </div>
    </>
  );
}
