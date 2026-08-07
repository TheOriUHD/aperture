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
export function useCameraChoices(requestsCameras: boolean, requestedCameras: string[] | null | undefined) {
  const { data: cams = [], isLoading } = useCameras();
  const requested = requestedCameras ?? []; // the API returns [] (never null) for an all request
  const requestAll = requestsCameras && requested.length === 0;
  const key = requested.join(",");
  const choices = useMemo<CamChoice[]>(() => {
    if (!requestsCameras) return [];
    const list = cams.map((c) => ({ id: c.id, name: c.name || c.id }));
    if (requestAll) return list;
    const req = new Set(requested);
    return list.filter((c) => req.has(c.id));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [cams, requestsCameras, requestAll, key]);
  return { choices, requestAll, loading: isLoading };
}

// The grant value model, shared by the consent modal and the row editor:
//   null      → "all cameras, including any added later" (future-inclusive/unrestricted)
//   string[]  → EXACTLY these cameras (a camera added later is NOT granted)
// The two are distinct decisions, chosen explicitly — ticking every current box is
// not the same as choosing "all + future".
export function initialGrantValue(
  requestAll: boolean,
  requested: string[],
  current?: string[] | null,
): string[] | null {
  if (current !== undefined) {
    const cur = current ?? [];
    if (cur.length === 0) return requestAll ? null : requested; // stored "all" → the all option
    return cur;
  }
  return requestAll ? null : requested; // install default = the manifest request
}

export function CameraGrantControl({
  requestsCameras,
  requestedCameras,
  value,
  onChange,
}: {
  requestsCameras: boolean;
  requestedCameras: string[] | null | undefined;
  value: string[] | null;
  onChange: (v: string[] | null) => void;
}) {
  const { choices, requestAll } = useCameraChoices(requestsCameras, requestedCameras);
  const isAll = value === null;
  const selected = new Set(value ?? []);
  const toggle = (id: string) => {
    const next = new Set(selected);
    if (next.has(id)) next.delete(id);
    else next.add(id);
    onChange([...next]);
  };

  return (
    <div className="space-y-2">
      {/* Only an all-requesting extension offers the future-inclusive "all"; a
          specific request has a fixed ceiling and is always an explicit list. */}
      {requestAll && (
        <div className="grid gap-1.5">
          <Mode
            checked={isAll}
            onSelect={() => onChange(null)}
            title="All cameras"
            sub="Including any camera added later"
          />
          <Mode
            checked={!isAll}
            onSelect={() => onChange(value ?? choices.map((c) => c.id))}
            title="Only the cameras I choose"
            sub="Exactly these — cameras added later are not granted"
          />
        </div>
      )}

      {(!requestAll || !isAll) && (
        <div className={clsx(requestAll && "mt-1")}>
          {choices.length === 0 ? (
            <p className="text-xs text-fg-faint">No cameras are available to you to grant.</p>
          ) : (
            <>
              <div className="mb-2 flex items-center justify-between">
                <span className="text-[11px] text-fg-faint">
                  {selected.size} of {choices.length} selected
                </span>
                <div className="flex gap-2 text-[11px]">
                  <button type="button" className="text-brand hover:underline" onClick={() => onChange(choices.map((c) => c.id))}>
                    Select all
                  </button>
                  <button type="button" className="text-fg-muted hover:underline" onClick={() => onChange([])}>
                    Clear
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
            </>
          )}
        </div>
      )}
    </div>
  );
}

function Mode({ checked, onSelect, title, sub }: { checked: boolean; onSelect: () => void; title: string; sub: string }) {
  return (
    <button
      type="button"
      onClick={onSelect}
      className={clsx(
        "flex items-start gap-2.5 rounded-lg border px-3 py-2 text-left transition-colors",
        checked ? "border-brand/40 bg-brand/10" : "border-line bg-ink-850 hover:border-line-strong",
      )}
    >
      <span
        className={clsx(
          "mt-0.5 grid h-4 w-4 shrink-0 place-items-center rounded-full border",
          checked ? "border-brand" : "border-line-strong",
        )}
      >
        {checked && <span className="h-2 w-2 rounded-full bg-brand" />}
      </span>
      <span className="min-w-0">
        <span className="block text-sm text-fg">{title}</span>
        <span className="block text-[11px] text-fg-faint">{sub}</span>
      </span>
    </button>
  );
}
