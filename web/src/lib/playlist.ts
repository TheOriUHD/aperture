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

export interface Range {
  start: number;
  end: number;
}

/** Merge contiguous/overlapping segments into solid spans (gaps <= gapMs join). */
export function mergeRanges(segs: { startTs: number; endTs: number }[], gapMs = 2000): Range[] {
  if (segs.length === 0) return [];
  const sorted = [...segs].sort((a, b) => a.startTs - b.startTs);
  const out: Range[] = [{ start: sorted[0].startTs, end: sorted[0].endTs }];
  for (let i = 1; i < sorted.length; i++) {
    const last = out[out.length - 1];
    if (sorted[i].startTs - last.end <= gapMs) {
      last.end = Math.max(last.end, sorted[i].endTs);
    } else {
      out.push({ start: sorted[i].startTs, end: sorted[i].endTs });
    }
  }
  return out;
}

/**
 * computeGaps returns the uncovered intervals (the inverse of mergeRanges)
 * within [windowStart, windowEnd]. Used to render the "no recording" band on a
 * continuous camera's timeline. Gaps below minGapMs are ignored as noise.
 */
export function computeGaps(
  ranges: Range[],
  windowStart: number,
  windowEnd: number,
  minGapMs = 5000,
): Range[] {
  const gaps: Range[] = [];
  let cursor = windowStart;
  for (const r of ranges) {
    if (r.start > cursor && r.start - cursor >= minGapMs) {
      gaps.push({ start: cursor, end: r.start });
    }
    cursor = Math.max(cursor, r.end);
  }
  if (windowEnd - cursor >= minGapMs) {
    gaps.push({ start: cursor, end: windowEnd });
  }
  return gaps;
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
