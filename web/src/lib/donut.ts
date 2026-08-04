// Donut geometry helpers. We draw each segment as an explicit SVG arc path with
// exact start/end points rather than a dashed <circle>. Dashed circles wrap their
// dash pattern at the 12-o'clock start, and the two butt-cap ends meeting there
// (plus anti-aliasing) produce a visible notch/spike — the artifact this avoids.
// Arcs meet neighbours at exact shared points, so seams are clean at every angle.

function polar(cx: number, cy: number, r: number, frac: number) {
  const a = (frac * 360 - 90) * (Math.PI / 180); // 0 = 12 o'clock, clockwise
  return { x: cx + r * Math.cos(a), y: cy + r * Math.sin(a) };
}

// arcPath draws the centreline arc of a segment from startFrac..endFrac (fractions
// of the full circle). A near-full span is split into two half-arcs so start ≠ end.
export function arcPath(cx: number, cy: number, r: number, startFrac: number, endFrac: number): string {
  const span = endFrac - startFrac;
  if (span <= 0.00001) return "";
  if (span >= 0.99999) {
    const top = polar(cx, cy, r, 0);
    const bot = polar(cx, cy, r, 0.5);
    return `M ${top.x} ${top.y} A ${r} ${r} 0 1 1 ${bot.x} ${bot.y} A ${r} ${r} 0 1 1 ${top.x} ${top.y}`;
  }
  const s = polar(cx, cy, r, startFrac);
  const e = polar(cx, cy, r, endFrac);
  const large = span > 0.5 ? 1 : 0;
  return `M ${s.x} ${s.y} A ${r} ${r} 0 ${large} 1 ${e.x} ${e.y}`;
}

// donutSegments returns one arc-path string per value, laid out clockwise from
// 12 o'clock, sized to the values' total.
export function donutSegments(values: number[], cx: number, cy: number, r: number): string[] {
  const total = values.reduce((a, v) => a + Math.max(0, v), 0) || 1;
  const paths: string[] = [];
  let acc = 0;
  for (const v of values) {
    const start = acc / total;
    acc += Math.max(0, v);
    paths.push(arcPath(cx, cy, r, start, acc / total));
  }
  return paths;
}
