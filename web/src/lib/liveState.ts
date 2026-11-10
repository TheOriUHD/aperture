import { useSyncExternalStore } from "react";

// Live per-camera state pushed over the change feed: whether a camera is moving
// right now (motion.state) and the objects recognised in its latest frame
// (objects.live). Kept outside React Query because it changes several times a
// second and only the tiles of the camera concerned should re-render.

export type LiveObject = { id?: number; label: string; score: number; x: number; y: number; w: number; h: number };

export type LiveCamera = {
  moving: boolean;
  objects: LiveObject[];
  objectsAt: number; // when the objects arrived (ms); they go stale after OBJECT_TTL
  parked: number; // vehicles standing still in view (the "P" badge)
  parkedAt: number; // when that was last said; stale after PARKED_TTL
};

// A camera's boxes disappear this long after the last update, so a lost
// "nothing here any more" message can't leave a box on screen.
export const OBJECT_TTL = 1500;

// The parked mark is told with every recognised frame of a watched camera (at
// least every 0.7 s); it goes after this long without word.
export const PARKED_TTL = 2500;

const EMPTY: LiveCamera = { moving: false, objects: [], objectsAt: 0, parked: 0, parkedAt: 0 };
const state = new Map<string, LiveCamera>();
const listeners = new Map<string, Set<() => void>>();

function emit(id: string) {
  listeners.get(id)?.forEach((fn) => fn());
}

function update(id: string, patch: Partial<LiveCamera>) {
  state.set(id, { ...(state.get(id) ?? EMPTY), ...patch });
  emit(id);
}

type FeedMsg = { type?: string; camera?: string; ts?: number; data?: unknown };

// applyFeed takes a change-feed message; returns true if it was live state.
export function applyFeed(msg: FeedMsg): boolean {
  const id = msg.camera;
  if (!id) return false;
  if (msg.type === "motion.state") {
    const active = !!(msg.data as { active?: boolean } | undefined)?.active;
    update(id, active ? { moving: true } : { moving: false, objects: [], objectsAt: 0 });
    return true;
  }
  if (msg.type === "objects.live") {
    const d = msg.data as { ts?: number; objects?: LiveObject[]; parked?: number } | undefined;
    // A snapshot replayed on reconnect may be old; ignore what's already stale.
    const ts = d?.ts ?? msg.ts ?? Date.now();
    if (Date.now() - ts > OBJECT_TTL) return true;
    const now = Date.now();
    update(id, {
      objects: Array.isArray(d?.objects) ? d!.objects! : [],
      objectsAt: now,
      parked: typeof d?.parked === "number" ? d.parked : 0,
      parkedAt: now,
    });
    return true;
  }
  return false;
}

// resetLive forgets everything (the feed reconnected and will resend state).
export function resetLive() {
  const ids = [...state.keys()];
  state.clear();
  ids.forEach(emit);
}

function subscribe(id: string, fn: () => void) {
  let set = listeners.get(id);
  if (!set) listeners.set(id, (set = new Set()));
  set.add(fn);
  return () => {
    set!.delete(fn);
  };
}

export function useLiveCamera(id: string): LiveCamera {
  return useSyncExternalStore(
    (fn) => subscribe(id, fn),
    () => state.get(id) ?? EMPTY,
    () => EMPTY,
  );
}

// Friendly names for the labels the model reports (COCO classes).
const NAMES: Record<string, string> = {
  person: "Person",
  car: "Car",
  truck: "Truck",
  bus: "Bus",
  motorcycle: "Motorcycle",
  bicycle: "Bicycle",
  dog: "Dog",
  cat: "Cat",
  bird: "Bird",
  horse: "Horse",
};

export function labelName(label: string): string {
  return NAMES[label] ?? label.charAt(0).toUpperCase() + label.slice(1);
}
