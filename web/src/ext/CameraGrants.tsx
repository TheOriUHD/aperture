import { useMemo } from "react";
import clsx from "clsx";
import { Check } from "lucide-react";
import { useCameras } from "../hooks/useCameras";

export interface CamChoice {
  id: string;
  name: string;
}

// The cameras the user may pick from: their own cameras, bounded by the manifest
// request. A specific request is a ceiling; an empty request means "all cameras".
export function useCameraChoices(requestsCameras: boolean, requestedCameras: string[]) {
  const { data: cams = [], isLoading } = useCameras();
  const requestAll = requestsCameras && requestedCameras.length === 0;
  const key = requestedCameras.join(",");
  const choices = useMemo<CamChoice[]>(() => {
    if (!requestsCameras) return [];
    const list = cams.map((c) => ({ id: c.id, name: c.name || c.id }));
    if (requestAll) return list;
    const req = new Set(requestedCameras);
    return list.filter((c) => req.has(c.id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cams, requestsCameras, requestAll, key]);
  return { choices, requestAll, loading: isLoading };
}

// grantsToSend converts a selection into the value the API expects: null when every
// choice is selected for an all-requesting extension (unrestricted, future-inclusive),
// otherwise the explicit id list.
export function grantsToSend(selected: Set<string>, choices: CamChoice[], requestAll: boolean): string[] | null {
  const ids = choices.filter((c) => selected.has(c.id)).map((c) => c.id);
  if (requestAll && ids.length === choices.length) return null;
  return ids;
}

// initialSelection derives the default checked set: from an existing grant ([] = all
// → everything), else all choices (the manifest's requested default at install).
export function initialSelection(choices: CamChoice[], grants: string[] | null): Set<string> {
  if (!grants || grants.length === 0) return new Set(choices.map((c) => c.id));
  const g = new Set(grants);
  return new Set(choices.filter((c) => g.has(c.id)).map((c) => c.id));
}

export function CameraGrantList({
  choices,
  selected,
  onChange,
}: {
  choices: CamChoice[];
  selected: Set<string>;
  onChange: (next: Set<string>) => void;
}) {
  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange(next);
  };
  return (
    <div>
      <div className="mb-2 flex items-center justify-between">
        <span className="text-[11px] text-fg-faint">
          {selected.size} of {choices.length} selected
        </span>
        <div className="flex gap-2 text-[11px]">
          <button type="button" className="text-brand hover:underline" onClick={() => onChange(new Set(choices.map((c) => c.id)))}>
            All
          </button>
          <button type="button" className="text-fg-muted hover:underline" onClick={() => onChange(new Set())}>
            None
          </button>
        </div>
      </div>
      <div className="grid max-h-48 gap-1.5 overflow-auto">
        {choices.map((c) => {
          const on = selected.has(c.id);
          return (
            <button
              key={c.id}
              type="button"
              onClick={() => toggle(c.id)}
              className={clsx(
                "flex items-center gap-2.5 rounded-lg border px-3 py-2 text-left text-sm transition-colors",
                on ? "border-brand/40 bg-brand/10 text-fg" : "border-line bg-ink-850 text-fg-muted hover:text-fg",
              )}
            >
              <span
                className={clsx(
                  "grid h-4 w-4 shrink-0 place-items-center rounded border",
                  on ? "border-brand bg-brand text-white" : "border-line-strong",
                )}
              >
                {on && <Check className="h-3 w-3" />}
              </span>
              <span className="truncate">{c.name}</span>
            </button>
          );
        })}
      </div>
    </div>
  );
}
