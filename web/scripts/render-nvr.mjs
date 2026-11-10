// Renders the sign-in page's product illustration (src/assets/nvr.svg): an
// Aperture NVR drawn as a small 3D model, in millimetres, so its parts keep
// real proportions. It is laid out like a desktop 4-bay NAS (Synology DS923+,
// UGREEN DXP4800): four upright 3.5" trays in one recessed cage, with a status
// display, a USB-C port and the power button in a strip below.
//
// The body is a rounded box seen from above and to the right. Every face is lit
// (Lambert + Blinn specular, in linear light, then gamma); flat faces carry
// their details through an affine map of their own plane, rounded corners are
// shaded along their curvature, and the drive trays sit in a real recess whose
// inner walls show. The SVG is inlined into the page (so its text uses the
// app's font); ids and classes are prefixed "nvr-", and #nvr-clock is the
// display's clock, which the page keeps current.
//
//   node scripts/render-nvr.mjs        (from web/)
import fs from "node:fs";
import path from "node:path";
import { fileURLToPath } from "node:url";

const OUT = path.join(path.dirname(fileURLToPath(import.meta.url)), "../src/assets/saved/nvr.svg");

// ---- camera ---------------------------------------------------------------
const deg = Math.PI / 180;
const YAW = 27 * deg; // turns the right side towards us
const PITCH = 17 * deg; // looks down onto the top
const cT = Math.cos(YAW), sT = Math.sin(YAW), cP = Math.cos(PITCH), sP = Math.sin(PITCH);
// World: x right, y up, z away from the viewer. Orthographic.
const proj = ([x, y, z]) => {
  const x1 = x * cT + z * sT;
  const z1 = -x * sT + z * cT;
  return [x1, -(y * cP + z1 * sP)];
};
const towardViewer = (n) => -n[0] * sT + n[2] * cT < 0;

// ---- light ----------------------------------------------------------------
const unit = (v) => {
  const l = Math.hypot(...v);
  return v.map((c) => c / l);
};
const dot = (a, b) => a[0] * b[0] + a[1] * b[1] + a[2] * b[2];
const LIGHT = unit([-0.5, 0.85, -0.6]);
const VIEW = [cP * sT, sP, -cP * cT];
const HALF = unit(LIGHT.map((c, i) => c + VIEW[i]));
const toSrgb = (c) => {
  c = Math.max(0, Math.min(1, c));
  return c <= 0.0031308 ? 12.92 * c : 1.055 * Math.pow(c, 1 / 2.4) - 0.055;
};
const hex = (rgb) => "#" + rgb.map((c) => Math.round(toSrgb(c) * 255).toString(16).padStart(2, "0")).join("");
function shade(n, albedo, { amb = 0.5, kd = 0.55, ks = 0.12, sh = 18 } = {}) {
  n = unit(n);
  const nl = Math.max(0, dot(n, LIGHT));
  const nh = Math.max(0, dot(n, HALF));
  const hemi = amb * (0.7 + 0.3 * n[1]);
  return hex(albedo.map((a) => a * (hemi + kd * nl) + ks * Math.pow(nh, sh)));
}
const SILVER = [0.8, 0.815, 0.84];
const PLINTH = [0.03, 0.033, 0.038];
const CAVITY = [0.05, 0.053, 0.06];

// ---- model (millimetres) --------------------------------------------------
const W = 196, D = 230, H = 204, R = 14; // body
const BASE_H = 5, BASE_IN = 10; // dark plinth under it
const Y0 = BASE_H, Y1 = BASE_H + H;
const FZ = -D / 2; // the front plane
const r2 = (n) => Math.round(n * 100) / 100;
const pt = (p) => `${r2(p[0])},${r2(p[1])}`;
const poly = (pts) => `M${pts.map(pt).join("L")}Z`;
const matrix = (o, eu, ev) => {
  const [e, f] = proj(o), [a, b] = proj(eu), [c, d] = proj(ev);
  return `matrix(${[a, b, c, d, e, f].map((n) => Math.round(n * 1e4) / 1e4).join(" ")})`;
};
// The front plane, pushed back by `depth`: u to the right, v down from the top.
const front = (depth = 0) => matrix([-W / 2, Y1, FZ + depth], [1, 0, 0], [0, -1, 0]);
const fw = (u, v, depth = 0) => proj([-W / 2 + u, Y1 - v, FZ + depth]);
// Right side: u from front to back, v down. Top: u along x, v from back to front.
const RIGHT = matrix([W / 2, Y1, FZ], [0, 0, 1], [0, -1, 0]);
const TOP = matrix([-W / 2, Y1, D / 2], [1, 0, 0], [0, 0, -1]);

const defs = [];
let gid = 0;

// A rounded rectangle in the x-z plane as dense samples with outward normals.
function outline(w, d, r, steps = 28) {
  const pts = [];
  const corners = [
    [w / 2 - r, -d / 2 + r, -90],
    [w / 2 - r, d / 2 - r, 0],
    [-w / 2 + r, d / 2 - r, 90],
    [-w / 2 + r, -d / 2 + r, 180],
  ];
  for (const [cx, cz, a0] of corners) {
    for (let i = 0; i <= steps; i++) {
      const a = (a0 + (90 * i) / steps) * deg;
      pts.push({ x: cx + r * Math.cos(a), z: cz + r * Math.sin(a), n: [Math.cos(a), 0, Math.sin(a)] });
    }
  }
  return pts;
}
function visibleSamples(w, d, r) {
  const all = outline(w, d, r);
  const vis = all.map((p) => towardViewer(p.n));
  const s = vis.findIndex((v, i) => v && !vis[(i - 1 + all.length) % all.length]);
  const run = [];
  for (let k = 0; k < all.length; k++) {
    const i = (s + k) % all.length;
    if (!vis[i]) break;
    run.push(all[i]);
  }
  return run;
}

// The visible wall of an extruded outline, shaded along its curve with a
// screen-space gradient (the flat faces get their details drawn on top).
function wall(w, d, r, y0, y1, albedo, opts) {
  const seg = visibleSamples(w, d, r);
  const top = seg.map((p) => proj([p.x, y1, p.z]));
  const bot = seg.map((p) => proj([p.x, y0, p.z]));
  const id = `nvr-w${gid++}`;
  const xs = top.map((p) => p[0]);
  const x0 = Math.min(...xs), x1 = Math.max(...xs);
  const stops = seg
    .map((p, i) => ({ o: (top[i][0] - x0) / (x1 - x0 || 1), c: shade(p.n, albedo, opts) }))
    .filter((s, i, a) => i === 0 || s.o - a[i - 1].o > 0.002);
  defs.push(
    `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${r2(x0)}" y1="0" x2="${r2(x1)}" y2="0">${stops
      .map((s) => `<stop offset="${Math.round(s.o * 1000) / 1000}" stop-color="${s.c}"/>`)
      .join("")}</linearGradient>`,
  );
  return `<path d="M${top.map(pt).join("L")}L${bot.reverse().map(pt).join("L")}Z" fill="url(#${id})"/>`;
}

// ---- front: drive cage and trays ------------------------------------------
// Like a desktop 4-bay NAS: one recessed cage holding four upright 3.5" trays
// (a drive stands on its side, so a tray front is about 31 x 114 mm), each a
// plain panel with its bay number, an activity light, a seam above the latch
// section, and a lock.
const CAGE = { u: 20, v: 15, w: 156, h: 127, r: 5 };
const TRAY = { w: 31, h: 114, pitch: 37 };
const RECESS = 3; // tray faces sit this far behind the front
const PLATE = 3.5; // thickness of a tray's front plate
const CAVITY_DEPTH = 70;
const trayAt = (i) => ({ x: CAGE.u + (CAGE.w - (4 * TRAY.pitch - (TRAY.pitch - TRAY.w))) / 2 + i * TRAY.pitch, y: CAGE.v + 6.5 });

// Linear gradient between two screen points (for surfaces that recede).
function depthGradient(a, b, stops) {
  const id = `nvr-g${gid++}`;
  defs.push(
    `<linearGradient id="${id}" gradientUnits="userSpaceOnUse" x1="${r2(a[0])}" y1="${r2(a[1])}" x2="${r2(b[0])}" y2="${r2(b[1])}">${stops
      .map(([o, c]) => `<stop offset="${o}" stop-color="${c}"/>`)
      .join("")}</linearGradient>`,
  );
  return `url(#${id})`;
}

function cage() {
  const { u, v, w, h } = CAGE;
  const u1 = u + w, v1 = v + h;
  const opening = [fw(u, v), fw(u1, v), fw(u1, v1), fw(u, v1)];
  defs.push(`<clipPath id="nvr-cage"><path d="${poly(opening)}"/></clipPath>`);
  const deep = CAVITY_DEPTH;
  // The cage's inner walls: the left one and the floor face the camera; both
  // fall into darkness towards the back.
  const leftWall = [fw(u, v), fw(u, v1), fw(u, v1, deep), fw(u, v, deep)];
  const floor = [fw(u, v1), fw(u1, v1), fw(u1, v1, deep), fw(u, v1, deep)];
  const lw = depthGradient(fw(u, v + h / 2), fw(u, v + h / 2, 24), [[0, shade([1, 0, 0], [0.16, 0.165, 0.175])], [1, "#060708"]]);
  const fl = depthGradient(fw(u + w / 2, v1), fw(u + w / 2, v1, 24), [[0, shade([0, 1, 0], [0.11, 0.115, 0.125])], [1, "#060708"]]);
  // The ceiling's shadow on the back of the cavity.
  const s = [`<g clip-path="url(#nvr-cage)">`, `<path d="${poly(opening)}" fill="url(#nvr-cavity)"/>`];
  s.push(`<path d="${poly(leftWall)}" fill="${lw}"/>`, `<path d="${poly(floor)}" fill="${fl}"/>`);
  for (let i = 0; i < 4; i++) s.push(tray(i));
  s.push(`</g>`);
  return s.join("");
}

// The chamfer round the cage opening, drawn on the front plane: it faces up
// (lit) along the bottom and down (shaded) along the top.
function chamfer() {
  const { u, v, w, h, r } = CAGE;
  const c = 2.6;
  const rr = (x, y, ww, hh, rad) =>
    `M${x + rad} ${y}H${x + ww - rad}A${rad} ${rad} 0 0 1 ${x + ww} ${y + rad}V${y + hh - rad}A${rad} ${rad} 0 0 1 ${x + ww - rad} ${y + hh}H${x + rad}A${rad} ${rad} 0 0 1 ${x} ${y + hh - rad}V${y + rad}A${rad} ${rad} 0 0 1 ${x + rad} ${y}Z`;
  return `<path d="${rr(u - c, v - c, w + 2 * c, h + 2 * c, r + c)}${rr(u, v, w, h, r)}" fill-rule="evenodd" fill="url(#nvr-chamfer)"/>
    <path d="${rr(u - c, v - c, w + 2 * c, h + 2 * c, r + c)}" fill="none" stroke="#fff" stroke-opacity=".35" stroke-width=".35"/>`;
}

function tray(i) {
  const { x, y } = trayAt(i);
  const { w, h } = TRAY;
  const s = [];
  // The plate's top and right edges show in the gaps (the camera is above and
  // to the right), with the plate's thickness.
  const P = (uu, vv, dd) => fw(uu, vv, dd);
  s.push(`<path d="${poly([P(x + 1, y, RECESS), P(x + w - 1, y, RECESS), P(x + w - 1, y, RECESS + PLATE), P(x + 1, y, RECESS + PLATE)])}" fill="${shade([0, 1, 0], [0.13, 0.135, 0.145])}"/>`);
  s.push(`<path d="${poly([P(x + w, y + 1, RECESS), P(x + w, y + h - 1, RECESS), P(x + w, y + h - 1, RECESS + PLATE), P(x + w, y + 1, RECESS + PLATE)])}" fill="${shade([1, 0, 0], [0.09, 0.095, 0.1])}"/>`);
  const split = y + h * 0.79;
  const lockY = split + (y + h - split) / 2;
  const cx = x + w / 2;
  const busy = i === 2 ? ` class="nvr-busy"` : "";
  const f = [];
  // Plate.
  f.push(`<rect x="${x}" y="${y}" width="${w}" height="${h}" rx="2.2" fill="url(#nvr-tray)"/>`);
  f.push(`<rect x="${x + 1}" y="${y + 0.3}" width="${w - 2}" height=".5" rx=".25" fill="#fff" opacity=".2"/>`);
  f.push(`<rect x="${x + 0.3}" y="${y + 1.2}" width=".45" height="${h - 2.4}" rx=".22" fill="#fff" opacity=".08"/>`);
  f.push(`<rect x="${x + 1}" y="${y + h - 0.75}" width="${w - 2}" height=".5" rx=".25" fill="#000" opacity=".5"/>`);
  // Activity light: a light pipe across the top.
  f.push(`<g${busy}><rect x="${cx - 5.5}" y="${y + 5.2}" width="11" height="3" rx="1.5" fill="#39c66d" opacity=".22" filter="url(#nvr-glow)"/>`);
  f.push(`<rect x="${cx - 4.5}" y="${y + 6.2}" width="9" height="1" rx=".5" fill="#6ff0a0"/></g>`);
  // Bay number, recessed into the plate.
  f.push(`<text x="${cx}" y="${y + 27.4}" text-anchor="middle" class="nvr-num" style="fill:#fff;opacity:.13">0${i + 1}</text>`);
  f.push(`<text x="${cx}" y="${y + 27}" text-anchor="middle" class="nvr-num">0${i + 1}</text>`);
  // Seam above the latch section, which is a shade darker.
  f.push(`<rect x="${x + 0.6}" y="${split}" width="${w - 1.2}" height="${y + h - split - 0.6}" rx="1.6" fill="#000" opacity=".16"/>`);
  f.push(`<rect x="${x + 0.6}" y="${split - 0.25}" width="${w - 1.2}" height=".5" fill="#0b0c0e" opacity=".85"/>`);
  f.push(`<rect x="${x + 0.6}" y="${split + 0.25}" width="${w - 1.2}" height=".35" fill="#fff" opacity=".12"/>`);
  // Lock: a metal cylinder with a key slot, and the turn arc beside it.
  f.push(`<circle cx="${cx}" cy="${lockY + 0.35}" r="4.6" fill="#fff" opacity=".12"/>`);
  f.push(`<circle cx="${cx}" cy="${lockY}" r="4.6" fill="#0d0e10"/>`);
  f.push(`<circle cx="${cx}" cy="${lockY}" r="3.8" fill="url(#nvr-lock)"/>`);
  f.push(`<circle cx="${cx}" cy="${lockY}" r="3.8" fill="none" stroke="#fff" stroke-opacity=".22" stroke-width=".25"/>`);
  // Keyway: a slot with the pin channel, and its lit lower lip.
  f.push(`<path d="M${cx - 2} ${lockY + 0.25}h4" stroke="#fff" stroke-opacity=".25" stroke-width=".95" stroke-linecap="round"/>`);
  f.push(`<path d="M${cx - 2} ${lockY}h4" stroke="#050506" stroke-width=".95" stroke-linecap="round"/>`);
  f.push(`<circle cx="${cx}" cy="${lockY}" r=".75" fill="#050506"/>`);
  // Padlock marks at either end of the turn arc.
  f.push(`<g fill="none" stroke="#6b7178" stroke-width=".3"><rect x="${cx + 2.9}" y="${lockY - 7.3}" width="1.5" height="1.15" rx=".2" fill="#6b7178" stroke="none"/><path d="M${cx + 3.15} ${lockY - 7.3}v-.45a.5.5 0 0 1 1 0v.45"/><rect x="${cx + 6.7}" y="${lockY + 0.7}" width="1.5" height="1.15" rx=".2" fill="#6b7178" stroke="none"/><path d="M${cx + 6.95} ${lockY + 0.7}v-.45a.5.5 0 0 1 1 0"/></g>`);
  f.push(`<path d="M${cx + 4.4} ${lockY - 5.4}A6.6 6.6 0 0 1 ${cx + 6.6} ${lockY - 0.6}" fill="none" stroke="#6b7178" stroke-width=".4" stroke-linecap="round"/>`);
  f.push(`<path d="M${cx + 6.6} ${lockY - 0.6}l-.9 -.9M${cx + 6.6} ${lockY - 0.6}l.6 -1.1" fill="none" stroke="#6b7178" stroke-width=".4" stroke-linecap="round"/>`);
  s.push(`<g transform="${front(RECESS)}">${f.join("")}</g>`);
  return s.join("");
}

// ---- front: status display ------------------------------------------------
// A 77 x 36 mm display behind black glass, showing what a console's front
// display does: name and clock; cameras and recording; storage; throughput and
// the health of each drive. Its content is laid out on a 64 x 30 grid, scaled.
const LCD = { u: 21, v: 154, w: 77, h: 36, scale: 1.2 };
function display() {
  const { u, v } = LCD;
  const w = LCD.w / LCD.scale, h = LCD.h / LCD.scale;
  const storage = 0.46;
  const ring = { cx: 54, cy: 15.6, r: 5.7 };
  const circ = 2 * Math.PI * ring.r;
  const dim = "#8d96a1";
  const s = [];
  s.push(`<rect x="${u - 2.6}" y="${v - 2.6}" width="${LCD.w + 5.2}" height="${LCD.h + 5.2}" rx="2.6" fill="#000" opacity=".45" transform="translate(0 .35)"/>`);
  s.push(`<rect x="${u - 2.4}" y="${v - 2.4}" width="${LCD.w + 4.8}" height="${LCD.h + 4.8}" rx="2.4" fill="url(#nvr-glass)"/>`);
  s.push(`<g transform="translate(${u} ${v}) scale(${LCD.scale})">`);
  // Header: product mark, name, clock.
  s.push(`<circle cx="3.7" cy="3.85" r="1.75" fill="none" stroke="#4797ff" stroke-width=".75"/><circle cx="3.7" cy="3.85" r=".8" fill="#4797ff"/>`);
  s.push(`<text x="6.9" y="4.9" font-size="2.8" font-weight="600" fill="#eef2f6">Aperture NVR</text>`);
  s.push(`<text id="nvr-clock" x="${w - 3}" y="4.9" font-size="2.8" font-weight="500" fill="#eef2f6" text-anchor="end">08:36</text>`);
  s.push(`<rect x="2.6" y="7.3" width="${w - 5.2}" height=".2" fill="#fff" opacity=".1"/>`);
  // Cameras and recording.
  s.push(`<text x="2.8" y="20.2" font-size="11.5" font-weight="300" fill="#fff" letter-spacing="-.3">5</text>`);
  s.push(`<text x="10.8" y="12.3" font-size="2.2" fill="${dim}">Cameras online</text>`);
  s.push(`<circle cx="11.7" cy="15.45" r=".8" fill="#f0494e" class="nvr-rec"/>`);
  s.push(`<text x="13.2" y="16.3" font-size="2.4" font-weight="600" fill="#ff6b70">Recording</text>`);
  s.push(`<text x="10.8" y="20.2" font-size="2.2" fill="${dim}">24/7 · 2 events</text>`);
  // Storage gauge.
  s.push(`<text x="${ring.cx - ring.r - 2.4}" y="14.2" font-size="2.1" fill="${dim}" text-anchor="end">Storage</text>`);
  s.push(`<text x="${ring.cx - ring.r - 2.4}" y="17.9" font-size="2.3" font-weight="600" fill="#eef2f6" text-anchor="end">3.7 / 8 TB</text>`);
  s.push(`<circle cx="${ring.cx}" cy="${ring.cy}" r="${ring.r}" fill="none" stroke="#1d2530" stroke-width="1.7"/>`);
  s.push(
    `<circle cx="${ring.cx}" cy="${ring.cy}" r="${ring.r}" fill="none" stroke="#4797ff" stroke-width="1.7" stroke-linecap="round" stroke-dasharray="${r2(circ * storage)} ${r2(circ)}" transform="rotate(-90 ${ring.cx} ${ring.cy})"/>`,
  );
  s.push(`<text x="${ring.cx}" y="${ring.cy + 1}" font-size="2.7" font-weight="600" fill="#fff" text-anchor="middle">46%</text>`);
  // Footer: throughput and per-drive health.
  s.push(`<rect x="2.6" y="${h - 6}" width="${w - 5.2}" height=".2" fill="#fff" opacity=".1"/>`);
  s.push(`<text x="2.8" y="${h - 1.9}" font-size="2.2" fill="#cfd6dd"><tspan fill="#39c66d">↓</tspan> 48.2 Mbps  <tspan fill="#4797ff">↑</tspan> 6.1 Mbps</text>`);
  s.push(`<text x="${w - 16.2}" y="${h - 1.9}" font-size="2" fill="${dim}" text-anchor="end">HDD</text>`);
  for (let i = 0; i < 4; i++) {
    s.push(`<rect x="${w - 15.2 + i * 3.1}" y="${h - 4.5}" width="2.2" height="2.9" rx=".45" fill="#39c66d" opacity=".9"/>`);
  }
  s.push(`</g>`);
  s.push(`<path d="M${u - 2.2} ${v - 2.2}H${u + LCD.w * 0.5}L${u + LCD.w * 0.3} ${v + LCD.h + 2.2}H${u - 2.2}Z" fill="#fff" opacity=".035"/>`);
  return s.join("");
}

// ---- front: buttons and ports beside the display --------------------------
function controls() {
  const cy = LCD.v + LCD.h / 2;
  const px = W - 26;
  return `
    <circle cx="${px}" cy="${cy}" r="6.1" fill="#8a9097"/>
    <circle cx="${px}" cy="${cy}" r="5.5" fill="none" stroke="#4797ff" stroke-width=".8" filter="url(#nvr-glow)" opacity=".9"/>
    <circle cx="${px}" cy="${cy}" r="5.5" fill="none" stroke="#8cc0ff" stroke-width=".45"/>
    <circle cx="${px + 0.35}" cy="${cy + 0.5}" r="4.6" fill="#000" opacity=".35" filter="url(#nvr-soft)"/>
    <circle cx="${px}" cy="${cy}" r="4.6" fill="url(#nvr-button)"/>
    <path d="M${px - 1.35} ${cy - 1.2}A1.9 1.9 0 1 0 ${px + 1.35} ${cy - 1.2}" fill="none" stroke="#7a8087" stroke-width=".5" stroke-linecap="round"/>
    <path d="M${px} ${cy - 2.6}V${cy - 0.6}" stroke="#7a8087" stroke-width=".5" stroke-linecap="round"/>
    <rect x="${px - 25}" y="${cy - 1.55}" width="9" height="3.1" rx="1.55" fill="none" stroke="#fff" stroke-opacity=".45" stroke-width=".25" transform="translate(0 .25)"/>
    <rect x="${px - 25}" y="${cy - 1.55}" width="9" height="3.1" rx="1.55" fill="#0a0b0d"/>
    <rect x="${px - 23.2}" y="${cy - 0.3}" width="5.4" height=".6" rx=".3" fill="#3d4248"/>
    <text x="${px - 20.5}" y="${cy + 6.6}" text-anchor="middle" class="nvr-label">USB-C</text>
    <text x="${LCD.u + LCD.w + 9}" y="${cy + 1.15}" class="nvr-mark" style="fill:#fff;opacity:.6">APERTURE</text>
    <text x="${LCD.u + LCD.w + 9}" y="${cy + 0.95}" class="nvr-mark">APERTURE</text>`;
}

function frontFace() {
  const l = R - 0.6, r = W - R + 0.6;
  return `<g transform="${front()}"><rect x="${l}" y="0" width="${r - l}" height="${H}" fill="${shade([0, 0, -1], SILVER)}"/>${chamfer()}${display()}${controls()}</g>${cage()}`;
}

function rightFace() {
  const l = R - 0.6, r = D - R + 0.6;
  return `<g transform="${RIGHT}">
    <rect x="${l}" y="0" width="${r - l}" height="${H}" fill="url(#nvr-sideShade)"/>
    <rect x="${D * 0.3}" y="${H * 0.18}" width="${D * 0.55}" height="${H * 0.5}" rx="4" fill="url(#nvr-sideHoles)"/>
  </g>`;
}

function topFace() {
  return `<g transform="${TOP}">
    <rect x="0" y="0" width="${W}" height="${D}" rx="${R}" fill="url(#nvr-topShade)"/>
    <rect x="0" y="0" width="${W}" height="${D}" rx="${R}" fill="#fff" filter="url(#nvr-brushed)" opacity=".5"/>
    <rect x="0" y="0" width="${W}" height="${D}" rx="${R}" fill="url(#nvr-topSheen)"/>
    <rect x="${W * 0.16}" y="14" width="${W * 0.68}" height="40" rx="3" fill="url(#nvr-topHoles)"/>
    <g transform="translate(${W / 2} ${D * 0.62})">
      <circle r="19" fill="none" stroke="#fff" stroke-width="4.6" opacity=".6" transform="translate(-.4 .9)"/>
      <circle r="19" fill="none" stroke="#b6bbc1" stroke-width="4.6"/>
      <circle r="7.2" fill="#fff" opacity=".6" transform="translate(-.4 .9)"/>
      <circle r="7.2" fill="#b6bbc1"/>
    </g>
  </g>`;
}

function bottomEdge() {
  const run = visibleSamples(W - 0.8, D - 0.8, R - 0.4).map((p) => proj([p.x, Y0 + 0.6, p.z]));
  return `<path d="M${run.map(pt).join("L")}" fill="none" stroke="#000" stroke-opacity=".22" stroke-width="1.1" stroke-linejoin="round"/>`;
}

function topEdge() {
  const run = visibleSamples(W - 0.9, D - 0.9, R - 0.45).map((p) => proj([p.x, Y1 - 0.25, p.z]));
  const xs = run.map((p) => p[0]);
  defs.push(`<linearGradient id="nvr-edgeGlint" gradientUnits="userSpaceOnUse" x1="${r2(Math.min(...xs))}" x2="${r2(Math.max(...xs))}" y1="0" y2="0">
      <stop offset="0" stop-color="#fff" stop-opacity=".2"/>
      <stop offset=".16" stop-color="#fff" stop-opacity=".95"/>
      <stop offset=".6" stop-color="#fff" stop-opacity=".85"/>
      <stop offset=".72" stop-color="#fff" stop-opacity=".5"/>
      <stop offset="1" stop-color="#fff" stop-opacity=".12"/>
    </linearGradient>`);
  return `<path d="M${run.map(pt).join("L")}" fill="none" stroke="url(#nvr-edgeGlint)" stroke-width="1.1" stroke-linecap="round" stroke-linejoin="round"/>`;
}

function shadow() {
  const pts = outline(W - BASE_IN * 2 + 6, D - BASE_IN * 2 + 6, R).map((p) => proj([p.x, 0, p.z]));
  const wide = outline(W + 30, D + 24, R + 24).map((p) => proj([p.x + 14, 0, p.z + 18]));
  return `<path d="${poly(wide)}" fill="#000" opacity=".55" filter="url(#nvr-softShadow)"/>
<path d="${poly(pts)}" fill="#000" opacity=".8" filter="url(#nvr-contactShadow)"/>`;
}

// ---- assemble -------------------------------------------------------------
const plinth = wall(W - BASE_IN * 2, D - BASE_IN * 2, R - 4, 0, Y0 + 1, PLINTH, { amb: 0.6, kd: 0.5, ks: 0.05 });
const body = wall(W, D, R, Y0, Y1, SILVER);
const faces = frontFace() + rightFace() + bottomEdge() + topFace() + topEdge();

const corners = [];
for (const x of [-W / 2 - 30, W / 2 + 46]) for (const y of [0, Y1]) for (const z of [-D / 2 - 22, D / 2 + 40]) corners.push(proj([x, y, z]));
const minX = Math.min(...corners.map((c) => c[0])), maxX = Math.max(...corners.map((c) => c[0]));
const minY = Math.min(...corners.map((c) => c[1])) - 4, maxY = Math.max(...corners.map((c) => c[1])) + 6;
const vb = [minX, minY, maxX - minX, maxY - minY].map((n) => Math.round(n));

const svg = `<svg xmlns="http://www.w3.org/2000/svg" viewBox="${vb.join(" ")}" class="nvr-art" aria-hidden="true" focusable="false">
<!-- Generated by web/scripts/render-nvr.mjs; edit that, not this. -->
<style>
  .nvr-art text { font-family: "Inter Variable", Inter, system-ui, sans-serif; font-variant-numeric: tabular-nums; }
  .nvr-num { font-size: 8.4px; font-weight: 700; letter-spacing: -.2px; fill: #1b1e22; }
  .nvr-mark { font-size: 3.2px; font-weight: 600; letter-spacing: .9px; fill: #8f959c; }
  .nvr-label { font-size: 1.9px; font-weight: 500; letter-spacing: .15px; fill: #80868d; }
  @keyframes nvr-rec { 0%, 55% { opacity: 1 } 70%, 90% { opacity: .2 } 100% { opacity: 1 } }
  @keyframes nvr-busy { 0%, 100% { opacity: 1 } 10% { opacity: .2 } 22% { opacity: 1 } 37% { opacity: .3 } 49% { opacity: 1 } 63% { opacity: .25 } 70% { opacity: 1 } }
  .nvr-rec { animation: nvr-rec 2.4s ease-in-out infinite }
  .nvr-busy { animation: nvr-busy 2.6s steps(1) infinite }
  @media (prefers-reduced-motion: reduce) { .nvr-rec, .nvr-busy { animation: none } }
</style>
<defs>
  <filter id="nvr-softShadow" x="-30%" y="-60%" width="160%" height="220%"><feGaussianBlur stdDeviation="16"/></filter>
  <filter id="nvr-contactShadow" x="-20%" y="-50%" width="140%" height="200%"><feGaussianBlur stdDeviation="3.5"/></filter>
  <filter id="nvr-soft" x="-30%" y="-30%" width="160%" height="160%"><feGaussianBlur stdDeviation=".5"/></filter>
  <filter id="nvr-glow" x="-60%" y="-60%" width="220%" height="220%"><feGaussianBlur stdDeviation=".9"/></filter>
  <filter id="nvr-brushed" x="0" y="0" width="100%" height="100%">
    <feTurbulence type="fractalNoise" baseFrequency=".9 .006" numOctaves="2" seed="7" result="n"/>
    <feColorMatrix in="n" type="matrix" values="0 0 0 0 1  0 0 0 0 1  0 0 0 0 1  0 0 0 -.9 .62" result="a"/>
    <feComposite in="a" in2="SourceGraphic" operator="in"/>
  </filter>
  <linearGradient id="nvr-sideShade" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="${shade([1, 0, 0], SILVER)}"/>
    <stop offset="1" stop-color="${shade([1, -0.2, 0.15], SILVER)}"/>
  </linearGradient>
  <linearGradient id="nvr-topShade" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="${shade([-0.12, 1, -0.1], SILVER)}"/>
    <stop offset=".55" stop-color="${shade([0, 1, 0], SILVER)}"/>
    <stop offset="1" stop-color="${shade([0.15, 1, 0.12], SILVER)}"/>
  </linearGradient>
  <linearGradient id="nvr-topSheen" x1="0" y1="0" x2="1" y2=".35">
    <stop offset="0" stop-color="#fff" stop-opacity="0"/>
    <stop offset=".38" stop-color="#fff" stop-opacity=".16"/>
    <stop offset=".5" stop-color="#fff" stop-opacity="0"/>
  </linearGradient>
  <linearGradient id="nvr-glass" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="#101317"/>
    <stop offset="1" stop-color="#050607"/>
  </linearGradient>
  <linearGradient id="nvr-tray" x1="0" y1="0" x2="1" y2="1">
    <stop offset="0" stop-color="#43474e"/>
    <stop offset=".5" stop-color="#363a40"/>
    <stop offset="1" stop-color="#2b2e33"/>
  </linearGradient>
  <linearGradient id="nvr-cavity" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="#030304"/>
    <stop offset="1" stop-color="#0b0c0e"/>
  </linearGradient>
  <linearGradient id="nvr-chamfer" x1="0" y1="0" x2="0" y2="1">
    <stop offset="0" stop-color="#8a9097"/>
    <stop offset=".12" stop-color="#9da3aa"/>
    <stop offset=".88" stop-color="#c3c8cd"/>
    <stop offset="1" stop-color="#e9ecef"/>
  </linearGradient>
  <radialGradient id="nvr-lock" cx=".4" cy=".32" r=".85">
    <stop offset="0" stop-color="#6e747b"/>
    <stop offset=".6" stop-color="#3f444a"/>
    <stop offset="1" stop-color="#25282c"/>
  </radialGradient>
  <radialGradient id="nvr-button" cx=".4" cy=".35" r=".75">
    <stop offset="0" stop-color="#f6f7f8"/>
    <stop offset=".6" stop-color="#d4d8dc"/>
    <stop offset="1" stop-color="#a7adb3"/>
  </radialGradient>
  <pattern id="nvr-sideHoles" width="3.4" height="2.944" patternUnits="userSpaceOnUse">
    <circle cx=".85" cy=".736" r=".75" fill="#41464c"/>
    <circle cx="2.55" cy="2.208" r=".75" fill="#41464c"/>
  </pattern>
  <pattern id="nvr-topHoles" width="5" height="4.33" patternUnits="userSpaceOnUse">
    <circle cx="1.25" cy="1.2" r="1.15" fill="#fff" opacity=".7"/>
    <circle cx="1.25" cy="1.08" r="1.15" fill="#80868d"/>
    <circle cx="3.75" cy="3.37" r="1.15" fill="#fff" opacity=".7"/>
    <circle cx="3.75" cy="3.25" r="1.15" fill="#80868d"/>
  </pattern>
  ${defs.join("\n  ")}
</defs>
${shadow()}
${plinth}
${body}
${faces}
</svg>
`;

fs.mkdirSync(path.dirname(OUT), { recursive: true });
fs.writeFileSync(OUT, svg);
console.log(`wrote ${OUT} (${(svg.length / 1024).toFixed(1)} KB, viewBox ${vb.join(" ")})`);
