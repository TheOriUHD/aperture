// Renders the sign-in page's illustration (src/assets/scene.svg): a small
// isometric diorama of what Aperture does, in the style of UniFi's 3D
// pictograms. Three cameras feed a PoE switch, the switch feeds your own
// server, and Aperture (software, so a floating app window above that server,
// not a device) records to a NAS and streams to a phone. White devices stand
// on a dark glass platform; blue links run across its floor with light pulses
// travelling along them.
//
// Everything is modelled in 3D and drawn with an orthographic camera: rounded
// boxes (lit per face, corners shaded along their curve, details mapped onto
// each face's plane), cylinders and spheres. The SVG is inlined into the page;
// ids and classes are prefixed "sc-".
//
//   node scripts/render-scene.mjs        (from web/)
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const OUT = path.join(path.dirname(fileURLToPath(import.meta.url)), "../src/assets/scene.svg");

// ---- vectors ----------------------------------------------------------------
const add = (a, b) => a.map((c, i) => c + b[i]);
const sub = (a, b) => a.map((c, i) => c - b[i]);
const mul = (a, k) => a.map((c) => c * k);
const dot = (a, b) => a.reduce((s, c, i) => s + c * b[i], 0);
const cross = (a, b) => [a[1] * b[2] - a[2] * b[1], a[2] * b[0] - a[0] * b[2], a[0] * b[1] - a[1] * b[0]];
const unit = (v) => mul(v, 1 / Math.hypot(...v));

// ---- camera -----------------------------------------------------------------
const deg = Math.PI / 180;
const YAW = 36 * deg, PITCH = 30 * deg;
const cT = Math.cos(YAW), sT = Math.sin(YAW), cP = Math.cos(PITCH), sP = Math.sin(PITCH);
// World: x right, y up, z away from the viewer. Orthographic.
const proj = ([x, y, z]) => {
  const x1 = x * cT + z * sT;
  const z1 = -x * sT + z * cT;
  return [x1, -(y * cP + z1 * sP)];
};
const depth = ([x, y, z]) => -y * sP + (-x * sT + z * cT) * cP; // larger is farther
const towardViewer = (n) => depth(n) < 0;
const VIEW = unit([cP * sT, sP, -cP * cT]);

// ---- light ------------------------------------------------------------------
const LIGHT = unit([-0.5, 0.9, -0.55]);
const HALF = unit(add(LIGHT, VIEW));
const toSrgb = (c) => {
  c = Math.max(0, Math.min(1, c));
  return c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
};
const hex = (rgb) => "#" + rgb.map((c) => Math.round(toSrgb(c) * 255).toString(16).padStart(2, "0")).join("");
function shade(n, albedo, { amb = 0.55, kd = 0.5, ks = 0.1, sh = 16 } = {}) {
  n = unit(n);
  const nl = Math.max(0, dot(n, LIGHT));
  const nh = Math.max(0, dot(n, HALF));
  const hemi = amb * (0.7 + 0.3 * n[1]);
  return hex(albedo.map((a) => a * (hemi + kd * nl) + ks * Math.pow(nh, sh)));
}
const WHITE = [0.8, 0.81, 0.83];
const GLASS = [0.012, 0.014, 0.018];

// ---- svg helpers ------------------------------------------------------------
const r2 = (n) => Math.round(n * 100) / 100;
const pt = (p) => `${r2(p[0])},${r2(p[1])}`;
const poly = (pts) => `M${pts.map(pt).join("L")}Z`;
const line = (pts) => `M${pts.map(pt).join("L")}`;
const matrix = (o, eu, ev) => {
  const [e, f] = proj(o), [a, b] = proj(eu), [c, d] = proj(ev);
  return `matrix(${[a, b, c, d, e, f].map((n) => Math.round(n * 1e4) / 1e4).join(" ")})`;
};
const defs = [];
let gid = 0;
const nextId = (p) => `sc-${p}${gid++}`;
function gradient(a, b, stops) {
  const id = nextId("g");
  defs.push(
    `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${r2(a[0])}" y1="${r2(a[1])}" x2="${r2(b[0])}" y2="${r2(b[1])}">${stops
      .map(([o, c, op]) => `<stop offset="${Math.round(o * 1000) / 1000}" stop-color="${c}"${op !== undefined ? ` stop-opacity="${op}"` : ""}/>`)
      .join("")}</linearGradient>`,
  );
  return `url(#${id})`;
}

// ---- rounded boxes ----------------------------------------------------------
function outline(cx, cz, w, d, r, steps = 16) {
  const pts = [];
  const corners = [
    [cx + w / 2 - r, cz - d / 2 + r, -90],
    [cx + w / 2 - r, cz + d / 2 - r, 0],
    [cx - w / 2 + r, cz + d / 2 - r, 90],
    [cx - w / 2 + r, cz - d / 2 + r, 180],
  ];
  for (const [ox, oz, a0] of corners) {
    for (let i = 0; i <= steps; i++) {
      const a = (a0 + (90 * i) / steps) * deg;
      pts.push({ x: ox + r * Math.cos(a), z: oz + r * Math.sin(a), n: [Math.cos(a), 0, Math.sin(a)] });
    }
  }
  return pts;
}
function visibleRun(pts) {
  const vis = pts.map((p) => towardViewer(p.n));
  const s = vis.findIndex((v, i) => v && !vis[(i - 1 + pts.length) % pts.length]);
  const run = [];
  for (let k = 0; k < pts.length; k++) {
    const i = (s + k) % pts.length;
    if (!vis[i]) break;
    run.push(pts[i]);
  }
  return run;
}
// The visible side wall, shaded along its curve with a screen-space gradient.
function wall(cx, cz, w, d, r, y0, y1, albedo, opts) {
  const seg = visibleRun(outline(cx, cz, w, d, r));
  const top = seg.map((p) => proj([p.x, y1, p.z]));
  const bot = seg.map((p) => proj([p.x, y0, p.z]));
  const xs = top.map((p) => p[0]);
  const x0 = Math.min(...xs), x1 = Math.max(...xs);
  const stops = seg
    .map((p, i) => [(top[i][0] - x0) / (x1 - x0 || 1), shade(p.n, albedo, opts)])
    .filter((s, i, a) => i === 0 || s[0] - a[i - 1][0] > 0.002);
  const fill = gradient([x0, 0], [x1, 0], stops);
  return `<path d="M${top.map(pt).join("L")}L${bot.reverse().map(pt).join("L")}Z" fill="${fill}"/>`;
}
// A highlight along the top's visible edge: the rounded edge catches the light.
function topEdge(cx, cz, w, d, r, y, strength = 0.9) {
  const run = visibleRun(outline(cx, cz, w - 1, d - 1, Math.max(0.5, r - 0.5))).map((p) => proj([p.x, y - 0.3, p.z]));
  const xs = run.map((p) => p[0]);
  const g = gradient([Math.min(...xs), 0], [Math.max(...xs), 0], [
    [0, "#fff", 0.15 * strength],
    [0.2, "#fff", strength],
    [0.62, "#fff", 0.85 * strength],
    [0.78, "#fff", 0.45 * strength],
    [1, "#fff", 0.1 * strength],
  ]);
  return `<path d="${line(run)}" fill="none" stroke="${g}" stroke-width="1.3" stroke-linecap="round" stroke-linejoin="round"/>`;
}
function box({ x, z, y0 = 0, w, d, h, r, albedo = WHITE, opts, topFill, front, right, top, glint = 0.9 }) {
  const y1 = y0 + h;
  let s = wall(x, z, w, d, r, y0, y1, albedo, opts);
  if (front) s += `<g transform="${matrix([x - w / 2, y1, z - d / 2], [1, 0, 0], [0, -1, 0])}">${front(w, h)}</g>`;
  if (right) s += `<g transform="${matrix([x + w / 2, y1, z - d / 2], [0, 0, 1], [0, -1, 0])}">${right(d, h)}</g>`;
  s += `<g transform="${matrix([x - w / 2, y1, z + d / 2], [1, 0, 0], [0, 0, -1])}"><rect width="${w}" height="${d}" rx="${r}" fill="${topFill ?? shade([0, 1, 0], albedo, opts)}"/>${top ? top(w, d) : ""}</g>`;
  if (glint) s += topEdge(x, z, w, d, r, y1, glint);
  return s;
}
// Soft contact shadow of a footprint on the floor.
function footShadow(cx, cz, w, d, r, opacity = 0.6, blur = "sc-shadow") {
  const pts = outline(cx + 4, cz + 6, w + 6, d + 6, r + 3).map((p) => proj([p.x, 0, p.z]));
  return `<path d="${poly(pts)}" fill="#000" opacity="${opacity}" filter="url(#${blur})"/>`;
}

// ---- cylinders and spheres --------------------------------------------------
function hull(points) {
  const p = points.slice().sort((a, b) => a[0] - b[0] || a[1] - b[1]);
  const crossZ = (o, a, b) => (a[0] - o[0]) * (b[1] - o[1]) - (a[1] - o[1]) * (b[0] - o[0]);
  const lower = [], upper = [];
  for (const q of p) {
    while (lower.length >= 2 && crossZ(lower[lower.length - 2], lower[lower.length - 1], q) <= 0) lower.pop();
    lower.push(q);
  }
  for (const q of p.reverse()) {
    while (upper.length >= 2 && crossZ(upper[upper.length - 2], upper[upper.length - 1], q) <= 0) upper.pop();
    upper.push(q);
  }
  return lower.slice(0, -1).concat(upper.slice(0, -1));
}
// A cylinder from p0 to p1; returns its body and the frame of its p1 cap (for
// drawing on the cap in local coordinates, where the cap is a circle of `r`).
function cylinder(p0, p1, r, albedo, opts, { capFill } = {}) {
  const a = unit(sub(p1, p0));
  const u = unit(cross(a, Math.abs(a[1]) < 0.95 ? [0, 1, 0] : [1, 0, 0]));
  const v = cross(a, u);
  const n = 64;
  const ring = (c) =>
    Array.from({ length: n }, (_, i) => {
      const t = (i / n) * 2 * Math.PI;
      const nn = add(mul(u, Math.cos(t)), mul(v, Math.sin(t)));
      return { p: add(c, mul(nn, r)), n: nn };
    });
  const r0 = ring(p0), r1 = ring(p1);
  const body = hull([...r0, ...r1].map((q) => proj(q.p)));
  const s0 = proj(p0), s1 = proj(p1);
  let A = sub(s1, s0);
  if (Math.hypot(...A) < 1e-6) A = [0, 1];
  A = mul(A, 1 / Math.hypot(...A));
  const N = [-A[1], A[0]];
  const stops = r0
    .filter((q) => towardViewer(q.n))
    .map((q) => [dot(sub(proj(q.p), s0), N), shade(q.n, albedo, opts)])
    .sort((x, y) => x[0] - y[0]);
  const t0 = stops[0][0], t1 = stops[stops.length - 1][0];
  const fill = gradient(add(s0, mul(N, t0)), add(s0, mul(N, t1)), stops.map(([t, c]) => [(t - t0) / (t1 - t0 || 1), c]));
  let s = `<path d="${poly(body)}" fill="${fill}"/>`;
  const capVisible = towardViewer(a);
  const cap = capVisible ? p1 : p0;
  const capN = capVisible ? a : mul(a, -1);
  s += `<path d="${poly((capVisible ? r1 : r0).map((q) => proj(q.p)))}" fill="${capFill ?? shade(capN, albedo, opts)}"/>`;
  return { svg: s, frame: matrix(cap, u, v), cap };
}
// A sphere: a disc lit from the light's direction on screen.
function sphere(c, r, colors, id) {
  const s = proj(c);
  const l = proj(LIGHT);
  const ll = Math.hypot(...l);
  const hl = [0.5 + (l[0] / ll) * 0.22, 0.5 + (l[1] / ll) * 0.22];
  defs.push(
    `<radialGradient id="${id}" cx="${r2(hl[0])}" cy="${r2(hl[1])}" r=".72">${colors.map(([o, c2]) => `<stop offset="${o}" stop-color="${c2}"/>`).join("")}</radialGradient>`,
  );
  return { svg: `<circle cx="${r2(s[0])}" cy="${r2(s[1])}" r="${r}" fill="url(#${id})"/>`, at: s };
}

// ---- the platform -----------------------------------------------------------
const SLAB = { w: 780, d: 560, h: 16, r: 48 };
// Floor coordinates (the slab's top face frame): u along +x, v towards the front.
const fl = (x, z) => [x + SLAB.w / 2, SLAB.d / 2 - z];
const FLOOR = matrix([-SLAB.w / 2, 0, SLAB.d / 2], [1, 0, 0], [0, 0, -1]);

// ---- devices ----------------------------------------------------------------
const HOST = { x: 85, z: 30, w: 140, d: 140, h: 62, r: 14 };
const APP = { y: 150, w: 236, h: 150 }; // the floating Aperture window, above HOST
const NAS = { x: 285, z: 165, w: 96, d: 118, h: 102, r: 12 };
const SW = { x: -170, z: 30, w: 184, d: 88, h: 28, r: 9 };
const CAMS = [
  { kind: "dome", x: -295, z: -165 },
  { kind: "bullet", x: -170, z: -175 },
  { kind: "turret", x: -45, z: -165 },
];
const PHONE = { x: 335, z: -120 };

// The server Aperture runs on: an unbranded mini PC.
function host() {
  const { x, z, w, d, h, r } = HOST;
  return box({
    x, z, w, d, h, r,
    front: (W, H) => {
      let s = "";
      // Power button with its light, two USB ports, an air intake.
      s += `<circle cx="24" cy="${H / 2}" r="6.5" fill="#9aa0a7"/><circle cx="24" cy="${H / 2}" r="5.2" fill="url(#sc-btn)"/>`;
      s += `<circle cx="24" cy="${H / 2}" r="6.4" fill="none" stroke="#4797ff" stroke-width="1" opacity=".9"/>`;
      for (let i = 0; i < 2; i++) s += `<rect x="${40 + i * 16}" y="${H / 2 - 3}" width="11" height="6" rx="1" fill="#0d0f11"/><rect x="${41.5 + i * 16}" y="${H / 2 - 2}" width="8" height="1.6" fill="#3d72d6"/>`;
      s += `<rect x="${W - 52}" y="${H / 2 - 9}" width="38" height="18" rx="3" fill="url(#sc-vent)"/>`;
      return s;
    },
    right: (D, H) => `<rect x="${D * 0.2}" y="${H * 0.25}" width="${D * 0.6}" height="${H * 0.5}" rx="4" fill="url(#sc-vent)"/>`,
    top: (W, D) => `<rect x="${W * 0.2}" y="${D * 0.2}" width="${W * 0.6}" height="${D * 0.6}" rx="10" fill="url(#sc-vent)" opacity=".7"/>`,
  });
}

// Aperture itself: a glass app window floating above the server, showing live
// cameras (one with a person boxed), with the light it is projected in.
function app() {
  const { x, z, h: hh } = HOST;
  const { y, w, h } = APP;
  const zf = z - 2;
  // Light rising from the server's top into the window.
  const a0 = proj([x - HOST.w / 2 + 18, hh + 1, z - 20]), a1 = proj([x + HOST.w / 2 - 18, hh + 1, z - 20]);
  const b0 = proj([x - w / 2, y, zf]), b1 = proj([x + w / 2, y, zf]);
  const lg = gradient([0, a0[1]], [0, b0[1]], [[0, "#4797ff", 0.32], [1, "#4797ff", 0.04]]);
  let s = `<path d="${poly([a0, a1, b1, b0])}" fill="${lg}"/>`;
  // The window floats gently.
  s += `<g class="sc-float">`;
  const frame = matrix([x - w / 2, y + h, zf], [1, 0, 0], [0, -1, 0]);
  s += `<g transform="${frame}">
    <rect x="-6" y="-6" width="${w + 12}" height="${h + 12}" rx="16" fill="#4797ff" opacity=".35" filter="url(#sc-glowWide)"/>
    <rect width="${w}" height="${h}" rx="10" fill="#0b0e13" opacity=".94"/>
    <rect width="${w}" height="${h}" rx="10" fill="none" stroke="#6fb0ff" stroke-width="1.4" opacity=".85"/>
    <rect x="1" y="1" width="${w - 2}" height="22" rx="9" fill="#fff" opacity=".04"/>
    <circle cx="16" cy="13" r="5" fill="none" stroke="#4797ff" stroke-width="2"/><circle cx="16" cy="13" r="2" fill="#4797ff"/>
    <text x="27" y="17" class="sc-app">Aperture</text>
    <circle cx="${w - 34}" cy="13" r="2.4" fill="#f0494e" class="sc-rec"/><text x="${w - 28}" y="16.4" class="sc-app-dim">REC</text>`;
  const tw = (w - 30) / 2, th = (h - 44) / 2;
  const tiles = [[10, 30], [20 + tw, 30], [10, 36 + th], [20 + tw, 36 + th]];
  tiles.forEach(([tx, ty], i) => {
    s += `<rect x="${tx}" y="${ty}" width="${tw}" height="${th}" rx="4" fill="url(#sc-feed${i % 3})"/>`;
    s += `<path d="M${tx} ${ty + th * 0.6}L${tx + tw * 0.4} ${ty + th * 0.48}L${tx + tw} ${ty + th * 0.64}V${ty + th - 4}a4 4 0 0 1 -4 4H${tx + 4}a4 4 0 0 1 -4 -4Z" fill="#0c1118" opacity=".55"/>`;
  });
  // Motion on the first camera: a white outline round the tile, a person boxed and labelled.
  const [mx, my] = tiles[0];
  s += `<rect x="${mx}" y="${my}" width="${tw}" height="${th}" rx="4" fill="none" stroke="#fff" stroke-width="1.6"/>
    <g transform="translate(${mx + tw * 0.56} ${my + th * 0.22})">
      <circle cx="6" cy="4" r="3.2" fill="#e3eaf3"/><rect x="2.6" y="8" width="6.8" height="13" rx="3" fill="#e3eaf3"/>
      <rect x="-2" y="-1" width="16" height="25" rx="1.5" fill="none" stroke="#fff" stroke-width="1"/>
      <rect x="-2" y="-8" width="25" height="7" rx="1.5" fill="#fff"/><text x="0" y="-2.6" class="sc-tag">Person</text>
    </g>
  </g></g>`;
  return s;
}

function nas() {
  const { x, z, w, d, h, r } = NAS;
  return box({
    x, z, w, d, h, r,
    front: (W, H) => {
      let s = "";
      for (let i = 0; i < 2; i++) {
        const u = 22 + i * 28;
        s += `<rect x="${u}" y="14" width="22" height="62" rx="3" fill="#121417"/>`;
        s += `<rect x="${u + 1.5}" y="15.5" width="19" height="59" rx="2.2" fill="url(#sc-tray)"/>`;
        s += `<rect x="${u + 6.5}" y="20" width="9" height="1.8" rx=".9" fill="#5ee08b" class="${i === 0 ? "sc-busy2" : ""}"/>`;
      }
      s += `<circle cx="${W - 20}" cy="${H - 14}" r="1.7" fill="#7db6ff"/>`;
      return s;
    },
    right: (D, H) => `<rect x="${D * 0.2}" y="${H * 0.2}" width="${D * 0.6}" height="${H * 0.55}" rx="4" fill="url(#sc-vent)"/>`,
  });
}

function sw() {
  const { x, z, w, d, h, r } = SW;
  return box({
    x, z, w, d, h, r,
    front: (W, H) => {
      let s = "";
      // Eight PoE ports, each lit, and two SFP cages.
      for (let i = 0; i < 8; i++) {
        const u = 18 + i * 15.5;
        s += `<rect x="${u}" y="8" width="12.5" height="10" rx="1.2" fill="#0d0f11"/>`;
        s += `<rect x="${u + 1}" y="8.6" width="10.5" height="1.4" rx=".7" fill="#3a3f45"/>`;
        s += `<circle cx="${u + 6.25}" cy="22.4" r="1.15" fill="${i < 3 ? "#5ee08b" : i === 3 ? "#f2b84b" : "#5ee08b"}" opacity="${i < 6 ? 1 : 0.25}" class="${i === 1 || i === 4 ? "sc-busy" : ""}"/>`;
      }
      for (let i = 0; i < 2; i++) s += `<rect x="${W - 44 + i * 16}" y="9" width="13" height="8" rx="1" fill="#0d0f11" stroke="#8c9198" stroke-width=".6"/>`;
      return s;
    },
    top: (W, D) => `<rect x="${W * 0.12}" y="${D * 0.22}" width="${W * 0.76}" height="${D * 0.56}" rx="4" fill="url(#sc-ribs)"/>`,
  });
}

// A round pad each camera stands on, ringed with light.
function pad(x, z, r = 46) {
  const top = outline(x, z, 2 * r, 2 * r, r, 24).map((p) => proj([p.x, 5, p.z]));
  const ring = outline(x, z, 2 * r - 6, 2 * r - 6, r - 3, 24).map((p) => proj([p.x, 5.2, p.z]));
  return `${wall(x, z, 2 * r, 2 * r, r, 0, 5, [0.03, 0.035, 0.045], { amb: 0.6, ks: 0.05 })}
    <path d="${poly(top)}" fill="#16191e"/>
    <path d="${poly(ring)}" fill="none" stroke="#4797ff" stroke-width="1.4" opacity=".55" filter="url(#sc-glow)"/>
    <path d="${poly(ring)}" fill="none" stroke="#6fb0ff" stroke-width=".7" opacity=".8"/>`;
}

function camera(c) {
  const { x, z, kind } = c;
  let s = pad(x, z);
  if (kind === "dome") {
    // White base, smoked dome with the lens inside.
    s += cylinder([x, 5, z], [x, 16, z], 34, WHITE).svg;
    const top = outline(x, z, 68, 68, 34, 24).map((p) => proj([p.x, 16, p.z]));
    s += `<path d="${poly(top)}" fill="${shade([0, 1, 0], WHITE)}"/>`;
    const sp = sphere([x, 16, z], 26, [[0, "#5b6470"], [0.45, "#22272e"], [1, "#0b0d10"]], nextId("dome"));
    // Clip the dome to above the base's rim (its front half).
    const id = nextId("clip");
    const rim = outline(x, z, 52, 52, 26, 24)
      .map((p) => ({ q: proj([p.x, 16, p.z]), front: towardViewer(p.n) }))
      .filter((p) => p.front)
      .map((p) => p.q)
      .sort((a, b) => a[0] - b[0]);
    defs.push(`<clipPath id="${id}"><path d="M${pt([rim[0][0], rim[0][1] - 80])}L${rim.map(pt).join("L")}L${pt([rim[rim.length - 1][0], rim[rim.length - 1][1] - 80])}Z"/></clipPath>`);
    s += `<g clip-path="url(#${id})">${sp.svg}</g>`;
    // Lens glint and the status light on the base.
    const lens = proj([x - 8, 26, z - 14]);
    s += `<circle cx="${r2(lens[0])}" cy="${r2(lens[1])}" r="5" fill="#0a0b0d"/><circle cx="${r2(lens[0] - 1.4)}" cy="${r2(lens[1] - 1.4)}" r="1.6" fill="#8fc2ff" opacity=".8"/>`;
    const led = proj([x - 10, 10, z - 32]);
    s += `<circle cx="${r2(led[0])}" cy="${r2(led[1])}" r="1.6" fill="#7db6ff"/>`;
    s += topEdge(x, z, 68, 68, 34, 16, 0.6);
  } else if (kind === "bullet") {
    // Wall-style mount: a foot, an arm, and the bullet body looking forward.
    s += cylinder([x, 5, z + 14], [x, 12, z + 14], 22, WHITE).svg;
    s += cylinder([x, 12, z + 14], [x, 44, z + 10], 7, WHITE).svg;
    const back = [x + 8, 50, z + 36], front = [x - 14, 42, z - 46];
    const body = cylinder(back, front, 19, WHITE, undefined, { capFill: "#e9ecef" });
    s += body.svg;
    // The front: a dark bezel, the lens, a sun-shield lip above.
    s += `<g transform="${body.frame}">
      <circle r="16.5" fill="#15181c"/>
      <circle r="9.5" fill="url(#sc-lens)"/>
      <circle r="9.5" fill="none" stroke="#3a4048" stroke-width="1"/>
      <circle cx="-3" cy="3" r="2.6" fill="#9cc8ff" opacity=".55"/>
      <circle cx="11.5" cy="-8" r="1.5" fill="#7db6ff"/>
    </g>`;
  } else {
    // Turret: a white base ring holding a ball, its lens looking forward.
    s += cylinder([x, 5, z], [x, 18, z], 30, WHITE).svg;
    const top = outline(x, z, 60, 60, 30, 24).map((p) => proj([p.x, 18, p.z]));
    s += `<path d="${poly(top)}" fill="${shade([0, 1, 0], WHITE)}"/>`;
    s += `<path d="${poly(outline(x, z, 52, 52, 26, 24).map((p) => proj([p.x, 18.2, p.z])))}" fill="#9aa0a7"/>`;
    const c3 = [x, 34, z];
    s += sphere(c3, 24, [[0, "#ffffff"], [0.5, "#dfe2e6"], [1, "#9ba1a8"]], nextId("ball")).svg;
    const f = unit([-0.35, -0.2, -1]);
    const u = unit(cross(f, [0, 1, 0]));
    const v = cross(f, u);
    s += `<g transform="${matrix(add(c3, mul(f, 21.5)), u, v)}">
      <circle r="12" fill="#15181c"/>
      <circle r="7" fill="url(#sc-lens)"/>
      <circle cx="-2.2" cy="2.2" r="1.9" fill="#9cc8ff" opacity=".55"/>
    </g>`;
    s += topEdge(x, z, 60, 60, 30, 18, 0.6);
  }
  return s;
}

// A phone on a small dock, showing four cameras; one has a motion box.
function phone() {
  const { x, z } = PHONE;
  let s = pad(x, z, 50);
  s += box({ x, z: z + 6, y0: 5, w: 74, d: 30, h: 9, r: 8 });
  s += box({
    x, z, y0: 14, w: 64, d: 9, h: 124, r: 4.5,
    albedo: [0.12, 0.125, 0.135],
    opts: { amb: 0.6, ks: 0.25, sh: 30 },
    glint: 0.5,
    front: (W, H) => {
      let f = `<rect x="2" y="2" width="${W - 4}" height="${H - 4}" rx="4" fill="#07090c"/>`;
      f += `<rect x="${W / 2 - 7}" y="5" width="14" height="3.4" rx="1.7" fill="#000"/>`;
      f += `<circle cx="9" cy="15.5" r="2" fill="none" stroke="#4797ff" stroke-width=".9"/><circle cx="9" cy="15.5" r=".8" fill="#4797ff"/>`;
      f += `<rect x="13" y="14.5" width="16" height="2" rx="1" fill="#e6ebf0" opacity=".8"/>`;
      const tiles = [[5, 22], [W / 2 + 1, 22], [5, 49], [W / 2 + 1, 49], [5, 76], [W / 2 + 1, 76]];
      tiles.forEach(([tx, ty], i) => {
        const tw = W / 2 - 6, th = 25;
        f += `<rect x="${tx}" y="${ty}" width="${tw}" height="${th}" rx="2" fill="url(#sc-feed${i % 3})"/>`;
        f += `<path d="M${tx} ${ty + th * 0.62}L${tx + tw * 0.45} ${ty + th * 0.5}L${tx + tw} ${ty + th * 0.66}V${ty + th}H${tx}Z" fill="#0c1118" opacity=".55"/>`;
      });
      // Motion: a white box round a figure in the first tile, and a red REC dot.
      f += `<rect x="11" y="27" width="7" height="13" rx=".8" fill="none" stroke="#fff" stroke-width=".8"/><circle cx="14.5" cy="30" r="1.4" fill="#dfe7f1"/><rect x="13" y="31.8" width="3" height="6" rx="1.2" fill="#dfe7f1"/>`;
      f += `<rect x="5" y="22" width="${W / 2 - 6}" height="25" rx="2" fill="none" stroke="#fff" stroke-width=".9" opacity=".9"/>`;
      f += `<circle cx="${W - 9}" cy="15.5" r="1.5" fill="#f0494e" class="sc-rec"/>`;
      f += `<rect x="${W / 2 - 10}" y="${H - 9}" width="20" height="1.6" rx=".8" fill="#e6ebf0" opacity=".5"/>`;
      return f;
    },
  });
  return s;
}

// ---- links ------------------------------------------------------------------
// Routes across the floor (world x, z), from source to destination.
const LINKS = [
  { pts: [[-295, -119], [-295, -84], [-225, -84], [-225, -14]], delay: 0 },
  { pts: [[-170, -129], [-170, -14]], delay: 0.8 },
  { pts: [[-45, -119], [-45, -84], [-115, -84], [-115, -14]], delay: 1.6 },
  { pts: [[-78, 30], [10, 30]], delay: 0.4 },
  { pts: [[155, 50], [285, 50], [285, 106]], delay: 1.2 },
  { pts: [[85, -40], [85, -120], [285, -120]], delay: 0.9, wireless: true },
];
function routePath(pts, r = 14) {
  // Rounded corners: shorten each segment into its corner and arc through it.
  const p = pts.map(([x, z]) => fl(x, z));
  let d = `M${pt(p[0])}`;
  for (let i = 1; i < p.length - 1; i++) {
    const a = p[i - 1], b = p[i], c = p[i + 1];
    const ab = unit(sub(b, a)), bc = unit(sub(c, b));
    const s1 = sub(b, mul(ab, r)), s2 = add(b, mul(bc, r));
    d += `L${pt(s1)}Q${pt(b)} ${pt(s2)}`;
  }
  return d + `L${pt(p[p.length - 1])}`;
}
function links() {
  let s = "";
  for (const l of LINKS) {
    const d = routePath(l.pts);
    s += `<path d="${d}" fill="none" stroke="#4797ff" stroke-width="7" opacity=".28" filter="url(#sc-glow)"/>`;
    s += l.wireless
      ? `<path d="${d}" fill="none" stroke="#3b82f0" stroke-width="2.4" stroke-linecap="round" stroke-dasharray="1 7"/>`
      : `<path d="${d}" fill="none" stroke="#3b82f0" stroke-width="2.4" stroke-linecap="round"/>`;
    s += `<path d="${d}" fill="none" stroke="#b9d9ff" stroke-width="2.6" stroke-linecap="round" stroke-dasharray="14 520" class="sc-pulse" style="animation-delay:-${l.delay}s"/>`;
  }
  // Each link ends in a port glow where it meets a device.
  for (const l of LINKS) {
    const e = fl(...l.pts[l.pts.length - 1]);
    s += `<circle cx="${r2(e[0])}" cy="${r2(e[1])}" r="5" fill="#6fb0ff" opacity=".5" filter="url(#sc-glow)"/>`;
  }
  return s;
}
// ---- floor labels -----------------------------------------------------------
function labels() {
  const t = (x, z, text, anchor = "start") => {
    const [u, v] = fl(x, z);
    return `<text x="${r2(u)}" y="${r2(v)}" class="sc-label" text-anchor="${anchor}">${text}</text>`;
  };
  return (
    t(-352, -232, "CAMERAS") +
    t(-307, 0, "NETWORK") +
    t(-36, -64, "APERTURE") +
    t(214, 100, "STORAGE")
  );
}

// ---- assemble ---------------------------------------------------------------
const slab = box({
  x: 0,
  z: 0,
  y0: -SLAB.h,
  w: SLAB.w,
  d: SLAB.d,
  h: SLAB.h,
  r: SLAB.r,
  albedo: [0.025, 0.03, 0.04],
  opts: { amb: 0.6, ks: 0.08 },
  topFill: "url(#sc-floor)",
  top: (W, D) => `<rect width="${W}" height="${D}" rx="${SLAB.r}" fill="url(#sc-grid)" mask="url(#sc-fade)"/>
    <ellipse cx="${fl(HOST.x, HOST.z)[0]}" cy="${fl(HOST.x, HOST.z)[1]}" rx="260" ry="200" fill="url(#sc-pool)"/>
    ${links()}`,
  glint: 0,
});
// The slab's rim catches a blue light along its front edges.
const rim = (() => {
  const run = visibleRun(outline(0, 0, SLAB.w - 1, SLAB.d - 1, SLAB.r - 0.5)).map((p) => proj([p.x, -0.4, p.z]));
  const xs = run.map((p) => p[0]);
  const g = gradient([Math.min(...xs), 0], [Math.max(...xs), 0], [
    [0, "#4797ff", 0.1],
    [0.3, "#6fb0ff", 0.8],
    [0.7, "#6fb0ff", 0.7],
    [1, "#4797ff", 0.1],
  ]);
  return `<path d="${line(run)}" fill="none" stroke="${g}" stroke-width="1.4"/>`;
})();

// Devices, back to front.
const items = [
  { d: depth([NAS.x, 0, NAS.z]), svg: footShadow(NAS.x, NAS.z, NAS.w, NAS.d, NAS.r) + nas() },
  { d: depth([HOST.x, 0, HOST.z]), svg: footShadow(HOST.x, HOST.z, HOST.w, HOST.d, HOST.r) + host() },
  { d: depth([SW.x, 0, SW.z]), svg: footShadow(SW.x, SW.z, SW.w, SW.d, SW.r, 0.5) + sw() },
  ...CAMS.map((c) => ({ d: depth([c.x, 0, c.z]), svg: camera(c) })),
  { d: depth([PHONE.x, 0, PHONE.z]), svg: phone() },
].sort((a, b) => b.d - a.d);

// The app window floats in front of everything behind the server; it is drawn
// after the devices behind it and before those in front.
const hostDepth = depth([HOST.x, 0, HOST.z]);
const behind = items.filter((i) => i.d >= hostDepth), before = items.filter((i) => i.d < hostDepth);
const body = [slab, rim, ...behind.map((i) => i.svg), app(), ...before.map((i) => i.svg)].join("\n");

// Extent.
const ext = [];
for (const [x, z] of [[-SLAB.w / 2, -SLAB.d / 2], [SLAB.w / 2, -SLAB.d / 2], [SLAB.w / 2, SLAB.d / 2], [-SLAB.w / 2, SLAB.d / 2]]) {
  ext.push(proj([x, 0, z]), proj([x, -SLAB.h, z]));
}
for (const dx of [-APP.w / 2 - 20, APP.w / 2 + 20]) ext.push(proj([HOST.x + dx, APP.y + APP.h + 24, HOST.z]));
const minX = Math.min(...ext.map((p) => p[0])) - 12, maxX = Math.max(...ext.map((p) => p[0])) + 12;
const minY = Math.min(...ext.map((p) => p[1])) - 8, maxY = Math.max(...ext.map((p) => p[1])) + 24;
const vb = [minX, minY, maxX - minX, maxY - minY].map((n) => Math.round(n));

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vb.join(" ")}" class="sc-art" aria-hidden="true" focusable="false">
<!-- Generated by web/scripts/render-scene.mjs; edit that, not this. -->
<style>
  .sc-art text { font-family: "Inter Variable", Inter, system-ui, sans-serif; }
  .sc-app { font-size: 10px; font-weight: 600; fill: #eef2f6; }
  .sc-app-dim { font-size: 8px; font-weight: 600; fill: #ff6b70; letter-spacing: .5px; }
  .sc-tag { font-size: 5.4px; font-weight: 700; fill: #0b0e13; }
  @keyframes sc-float { 0%, 100% { transform: translateY(0) } 50% { transform: translateY(-5px) } }
  .sc-float { animation: sc-float 5s ease-in-out infinite }
  .sc-label { font-size: 13px; font-weight: 600; letter-spacing: 2.6px; fill: #fff; opacity: .32; }
  @keyframes sc-pulse { from { stroke-dashoffset: 534 } to { stroke-dashoffset: 0 } }
  @keyframes sc-rise { from { stroke-dashoffset: 250 } to { stroke-dashoffset: 0 } }
  @keyframes sc-busy { 0%, 100% { opacity: 1 } 12% { opacity: .25 } 24% { opacity: 1 } 41% { opacity: .35 } 55% { opacity: 1 } }
  .sc-pulse { animation: sc-pulse 2.8s linear infinite }
  .sc-rise { animation: sc-rise 2.2s linear infinite }
  .sc-busy { animation: sc-busy 2.4s steps(1) infinite }
  .sc-busy2 { animation: sc-busy 3.1s steps(1) infinite }
  @keyframes sc-rec { 0%, 55% { opacity: 1 } 70%, 90% { opacity: .2 } 100% { opacity: 1 } }
  .sc-rec { animation: sc-rec 2.4s ease-in-out infinite }
  @media (prefers-reduced-motion: reduce) { .sc-pulse, .sc-rise, .sc-busy, .sc-busy2, .sc-rec, .sc-float { animation: none } .sc-pulse, .sc-rise { opacity: 0 } }
</style>
<defs>
  <filter id="sc-shadow" x="-30%" y="-30%" width="160%" height="160%"><feGaussianBlur stdDeviation="6"/></filter>
  <filter id="sc-shadowWide" x="-50%" y="-80%" width="200%" height="260%"><feGaussianBlur stdDeviation="18"/></filter>
  <filter id="sc-glow" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="3"/></filter>
  <filter id="sc-glowWide" x="-50%" y="-50%" width="200%" height="200%"><feGaussianBlur stdDeviation="10"/></filter>
  <linearGradient id="sc-floor" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="#1c2026"/>
    <stop offset="1" stop-color="#121418"/>
  </linearGradient>
  <pattern id="sc-grid" width="40" height="40" patternUnits="userSpaceOnUse">
    <path d="M40 0H0V40" fill="none" stroke="#fff" stroke-opacity=".06" stroke-width="1"/>
  </pattern>
  <radialGradient id="sc-fadeG" cx=".5" cy=".5" r=".6">
    <stop offset=".35" stop-color="#fff"/>
    <stop offset="1" stop-color="#000"/>
  </radialGradient>
  <mask id="sc-fade" maskContentUnits="objectBoundingBox"><rect width="1" height="1" fill="url(#sc-fadeG)"/></mask>
  <radialGradient id="sc-pool">
    <stop offset="0" stop-color="#4797ff" stop-opacity=".16"/>
    <stop offset="1" stop-color="#4797ff" stop-opacity="0"/>
  </radialGradient>
  <linearGradient id="sc-tray" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="#3d4148"/>
    <stop offset="1" stop-color="#2a2d32"/>
  </linearGradient>
  <pattern id="sc-vent" width="5" height="4.33" patternUnits="userSpaceOnUse">
    <circle cx="1.25" cy="1.08" r="1.05" fill="#7b8188"/>
    <circle cx="3.75" cy="3.25" r="1.05" fill="#7b8188"/>
  </pattern>
  <pattern id="sc-ribs" width="7" height="7" patternUnits="userSpaceOnUse">
    <rect width="4" height="7" fill="#c3c7cc"/>
  </pattern>
  <radialGradient id="sc-lens" cx=".4" cy=".35" r=".8">
    <stop offset="0" stop-color="#3b5a86"/>
    <stop offset=".5" stop-color="#141c28"/>
    <stop offset="1" stop-color="#05070a"/>
  </radialGradient>
  <radialGradient id="sc-btn" cx=".4" cy=".35" r=".8"><stop offset="0" stop-color="#ffffff"/><stop offset="1" stop-color="#b9bec4"/></radialGradient>
  <linearGradient id="sc-cloud" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="#ffffff"/>
    <stop offset=".6" stop-color="#e7ebf0"/>
    <stop offset="1" stop-color="#b9c3cf"/>
  </linearGradient>
  <linearGradient id="sc-feed0" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#4c6487"/><stop offset="1" stop-color="#1f2a3a"/></linearGradient>
  <linearGradient id="sc-feed1" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#5a6b5a"/><stop offset="1" stop-color="#232b24"/></linearGradient>
  <linearGradient id="sc-feed2" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="#6b6274"/><stop offset="1" stop-color="#28232e"/></linearGradient>
  <radialGradient id="sc-lobe">
    <stop offset="0" stop-color="#fff" stop-opacity=".9"/>
    <stop offset="1" stop-color="#fff" stop-opacity="0"/>
  </radialGradient>
  ${defs.join("\n  ")}
</defs>
${body}
</svg>
`;

fs.mkdirSync(path.dirname(OUT), { recursive: true });
fs.writeFileSync(OUT, svg);
console.log(`wrote ${OUT} (${(svg.length / 1024).toFixed(1)} KB, viewBox ${vb.join(" ")})`);
