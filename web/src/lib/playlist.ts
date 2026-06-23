// Maps between wall-clock time and HLS-playlist offset. The generated playlist
// concatenates recorded segments (excluding gaps), so playlist time is the sum
// of segment durations — not linear wall-clock. These helpers convert both ways
// using the segment index.

export interface Seg {
  startTs: number; // unix ms
  endTs: number; // unix ms
  durationMs: number;
}

/** Playlist offset (seconds) for a wall-clock time; snaps gaps to next footage. */
export function wallToOffset(segs: Seg[], wallMs: number): number {
  let acc = 0;
  for (const s of segs) {
    if (wallMs < s.startTs) return acc; // inside a gap -> start of next segment
    if (wallMs < s.endTs) return acc + (wallMs - s.startTs) / 1000;
    acc += s.durationMs / 1000;
  }
  return acc; // past the end
}

/** Wall-clock time (ms) for a playlist offset (seconds). */
export function offsetToWall(segs: Seg[], offsetSec: number): number | null {
  let acc = 0;
  for (const s of segs) {
    const d = s.durationMs / 1000;
    if (offsetSec < acc + d) return s.startTs + (offsetSec - acc) * 1000;
    acc += d;
  }
  return segs.length ? segs[segs.length - 1].endTs : null;
}
