// Dev tool: render the generated world to PNGs and measure it, so terrain
// redesigns can be compared before and after. No dependencies beyond the repo's
// own worldgen (PNG encoder is in png.mjs).
//
//   node tools/worldmap/render.mjs [seed=hearth-1] [outDir]
//        [--scale=4] [--crop=x,y,size[,name]]... [--metrics-only]
//
// Writes into outDir (default: <os tmpdir>/worldmap-<seed>):
//   overview.png   whole map, 1 px per 2x2 tiles
//   woods|dunes|spire|marsh.png   200x200-tile crops centred on each monolith
//   core.png       60x60-tile crop centred on the Core
//   metrics.json   per-region numbers (see metrics.mjs); summary also on stdout
//
// Crop legend: land shaded per elevation (L1 dark .. L3 light); dark edge line =
// cliff (neighbour 2+ lower), light edge line = ledge (1 lower); dots: tree dark
// green, bush light green, stone grey, boulder brown-grey, crystal cyan,
// starmetal magenta, decor yellow, iceberg white. Red ring = monolith, yellow
// square = spawn, pink = medic (hollow = hut), black 3x3 box = mountain, orange =
// temple piece. Faint white circle = ISLE_R+20 (island ownership radius), faint
// yellow circle = nominal ISLE_R. Ticks on the top/left border: every 10 tiles,
// long every 50.
import os from 'node:os';
import path from 'node:path';
import { mkdirSync, writeFileSync } from 'node:fs';
import {
  SIZE, T, ISLES, ISLE_R, CORE, MOUNTAINS, TEMPLE_PIECES, MAJOR_ISLANDS,
  genWorld, findSpawn, findMedicSpawns,
} from '../../shared/world.js';
import { NODE } from '../../shared/defs.js';
import { writePng } from './png.mjs';
import { computeMetrics, formatSummary } from './metrics.mjs';

const N = SIZE * SIZE;

// --- CLI ---------------------------------------------------------------------
const positional = [], flags = { crop: [] };
for (const a of process.argv.slice(2)) {
  const m = /^--([\w-]+)(?:=(.*))?$/.exec(a);
  if (!m) positional.push(a);
  else if (m[1] === 'crop') flags.crop.push(m[2]);
  else flags[m[1]] = m[2] === undefined ? true : m[2];
}
const SEED = positional[0] || 'hearth-1';
const OUT = path.resolve(positional[1] || path.join(os.tmpdir(), `worldmap-${SEED}`));
const SCALE = Math.max(1, Number(flags.scale) || 4);
if (positional.length > 2 || flags.help) {
  console.error('usage: render.mjs [seed=hearth-1] [outDir] [--scale=4] [--crop=x,y,size[,name]] [--metrics-only]');
  process.exit(2);
}

// --- colours -----------------------------------------------------------------
const BASE = {
  [T.GRASS]: [92, 160, 68], [T.SAND]: [222, 196, 130], [T.SNOW]: [238, 245, 252],
  [T.MUD]: [122, 100, 56], [T.BLIGHT]: [70, 30, 90],
};
const ELEV_SHADE = [1, 0.68, 0.84, 1.0];                // by elev level; 0 unused
const VIS_TINT = [255, 238, 140];                       // tileVis variant tint
const WATER = [                                         // [shallow, deep] by waterTemp
  [[72, 158, 218], [16, 44, 112]],                     // temperate
  [[150, 208, 236], [64, 120, 182]],                   // freezing
  [[218, 104, 84], [118, 34, 46]],                     // hot
];
const NODE_RGB = {
  [NODE.TREE]: [2, 38, 12], [NODE.BUSH]: [170, 240, 90], [NODE.STONE]: [150, 150, 158],
  [NODE.BOULDER]: [128, 98, 82], [NODE.CRYSTAL]: [40, 245, 245], [NODE.STARMETAL]: [255, 0, 255],
};
const DECOR_RGB = [255, 226, 30], BERG_RGB = [255, 255, 255];

const mix = (a, b, t) => [a[0] + (b[0] - a[0]) * t, a[1] + (b[1] - a[1]) * t, a[2] + (b[2] - a[2]) * t];
const shade = (c, f) => [Math.min(255, c[0] * f), Math.min(255, c[1] * f), Math.min(255, c[2] * f)];

// Distance (tiles) from each tile to the nearest land — 3-4 chamfer transform.
function landDistance(tiles) {
  const d = new Uint16Array(N);
  const BIG = 60000;
  for (let i = 0; i < N; i++) d[i] = tiles[i] === T.WATER ? BIG : 0;
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const i = y * SIZE + x;
      let v = d[i];
      if (x > 0) v = Math.min(v, d[i - 1] + 3);
      if (y > 0) {
        v = Math.min(v, d[i - SIZE] + 3);
        if (x > 0) v = Math.min(v, d[i - SIZE - 1] + 4);
        if (x < SIZE - 1) v = Math.min(v, d[i - SIZE + 1] + 4);
      }
      d[i] = v;
    }
  }
  for (let y = SIZE - 1; y >= 0; y--) {
    for (let x = SIZE - 1; x >= 0; x--) {
      const i = y * SIZE + x;
      let v = d[i];
      if (x < SIZE - 1) v = Math.min(v, d[i + 1] + 3);
      if (y < SIZE - 1) {
        v = Math.min(v, d[i + SIZE] + 3);
        if (x < SIZE - 1) v = Math.min(v, d[i + SIZE + 1] + 4);
        if (x > 0) v = Math.min(v, d[i + SIZE - 1] + 4);
      }
      d[i] = v;
    }
  }
  return d;
}

function tileColour(world, dist, i) {
  const t = world.tiles[i];
  if (t === T.WATER) {
    const [shallow, deep] = WATER[world.waterTemp[i]];
    return mix(shallow, deep, Math.pow(Math.min(dist[i] / 3, 40) / 40, 0.7));
  }
  let c = shade(BASE[t], ELEV_SHADE[world.elev[i]]);
  if (world.tileVis[i]) c = mix(c, VIS_TINT, 0.16);
  return c;
}

// --- raster ------------------------------------------------------------------
class Raster {
  constructor(w, h) { this.w = w; this.h = h; this.rgb = new Uint8Array(w * h * 3); }
  set(x, y, c) {
    if (x < 0 || y < 0 || x >= this.w || y >= this.h) return;
    const o = (y * this.w + x) * 3;
    this.rgb[o] = c[0]; this.rgb[o + 1] = c[1]; this.rgb[o + 2] = c[2];
  }
  blend(x, y, c, a) {
    if (x < 0 || y < 0 || x >= this.w || y >= this.h) return;
    const o = (y * this.w + x) * 3;
    for (let k = 0; k < 3; k++) this.rgb[o + k] = this.rgb[o + k] * (1 - a) + c[k] * a;
  }
  rect(x, y, w, h, c) { for (let j = 0; j < h; j++) for (let i = 0; i < w; i++) this.set(x + i, y + j, c); }
  outline(x, y, w, h, c, t = 1) {
    this.rect(x, y, w, t, c); this.rect(x, y + h - t, w, t, c);
    this.rect(x, y, t, h, c); this.rect(x + w - t, y, t, h, c);
  }
  // Ring of tile-space radius r around tile-space centre (cx, cy) -> pixels.
  ring(win, cx, cy, r, c, a, thick) {
    const s = win.scale;
    const px0 = Math.max(0, Math.floor((cx - r - win.x0) * s - 2)), px1 = Math.min(this.w - 1, Math.ceil((cx + r - win.x0 + 1) * s + 2));
    const py0 = Math.max(0, Math.floor((cy - r - win.y0) * s - 2)), py1 = Math.min(this.h - 1, Math.ceil((cy + r - win.y0 + 1) * s + 2));
    for (let py = py0; py <= py1; py++) {
      for (let px = px0; px <= px1; px++) {
        const fx = win.x0 + (px + 0.5) / s - 0.5, fy = win.y0 + (py + 0.5) / s - 0.5;   // tile centre = integer
        if (Math.abs(Math.hypot(fx - cx, fy - cy) - r) * s <= thick / 2) this.blend(px, py, c, a);
      }
    }
  }
}

// --- overview (1 px per 2x2 tiles) --------------------------------------------
function renderOverview(world, dist) {
  const W = SIZE / 2, img = new Raster(W, W);
  for (let py = 0; py < W; py++) {
    for (let px = 0; px < W; px++) {
      // dominant tile type of the 2x2 block; land wins ties so thin spits stay visible
      const cells = [0, 1, SIZE, SIZE + 1].map((o) => (py * 2) * SIZE + px * 2 + o);
      const landCells = cells.filter((i) => world.tiles[i] !== T.WATER);
      let rep;
      if (landCells.length >= 2) {
        const count = {};
        for (const i of landCells) count[world.tiles[i]] = (count[world.tiles[i]] || 0) + 1;
        const top = Math.max(...Object.values(count));
        rep = landCells.filter((i) => count[world.tiles[i]] === top).reduce((a, b) => (world.elev[b] > world.elev[a] ? b : a));
      } else {
        rep = cells[0];
      }
      img.set(px, py, tileColour(world, dist, rep).map(Math.round));
    }
  }
  return img;
}

// --- crops ---------------------------------------------------------------------
function renderCrop(world, dist, spawn, medics, win) {
  const { x0, y0, w, h, scale: s } = win;
  const img = new Raster(w * s, h * s);
  const lw = Math.max(1, Math.round(s / 4));            // edge-line width
  const dot = s >= 4 ? Math.round(s / 2) : 1, dotOff = Math.floor((s - dot) / 2);
  const { tiles, elev } = world;
  const lower = (i, j) => tiles[j] !== T.WATER && elev[i] - elev[j];

  for (let ty = 0; ty < h; ty++) {
    for (let tx = 0; tx < w; tx++) {
      const wx = x0 + tx, wy = y0 + ty;
      const px = tx * s, py = ty * s;
      if (wx < 0 || wy < 0 || wx >= SIZE || wy >= SIZE) { img.rect(px, py, s, s, [0, 0, 0]); continue; }
      const i = wy * SIZE + wx;
      const col = tileColour(world, dist, i).map(Math.round);
      img.rect(px, py, s, s, col);

      if (tiles[i] !== T.WATER) {
        // terrace edges on the higher tile, facing a lower land neighbour
        const edge = (j, draw) => {
          const d = lower(i, j);
          if (d >= 1) draw(d >= 2 ? mix(col, [8, 6, 4], 0.85) : mix(col, [255, 255, 235], 0.5));
        };
        if (wy > 0) edge(i - SIZE, (c) => img.rect(px, py, s, lw, c.map(Math.round)));
        if (wy < SIZE - 1) edge(i + SIZE, (c) => img.rect(px, py + s - lw, s, lw, c.map(Math.round)));
        if (wx > 0) edge(i - 1, (c) => img.rect(px, py, lw, s, c.map(Math.round)));
        if (wx < SIZE - 1) edge(i + 1, (c) => img.rect(px + s - lw, py, lw, s, c.map(Math.round)));
        if (world.tileVis[i]) img.set(px + s - 1 - lw, py + s - 1 - lw, mix(col, [255, 250, 200], 0.55).map(Math.round));
      }

      const dotAt = (c) => img.rect(px + dotOff, py + dotOff, dot, dot, c);
      if (world.nodes.has(i)) dotAt(NODE_RGB[world.nodes.get(i)]);
      else if (world.decor.has(i)) dotAt(DECOR_RGB);
      else if (world.bergs.has(i)) dotAt(BERG_RGB);
    }
  }

  const tpx = (wx) => (wx - x0) * s, tpy = (wy) => (wy - y0) * s;
  const centres = ISLES.map((c) => [c[0], c[1]]);

  // island radii: nominal coast and the server's ownership radius
  for (const [cx, cy] of centres) {
    img.ring(win, cx, cy, ISLE_R, [255, 235, 120], 0.3, 1);
    img.ring(win, cx, cy, ISLE_R + 20, [255, 255, 255], 0.4, 1);
  }
  // temple pieces
  for (const p of TEMPLE_PIECES) {
    const wx = p.i % SIZE, wy = (p.i / SIZE) | 0;
    img.rect(tpx(wx), tpy(wy), s, s, [255, 140, 0]);
  }
  // mountains: 3x3 footprint outlined in black
  for (const m of MOUNTAINS) img.outline(tpx(m.x - 1), tpy(m.y - 1), 3 * s, 3 * s, [0, 0, 0], lw);
  // medics (filled) and huts (hollow)
  for (const m of medics) {
    img.rect(tpx(m.x), tpy(m.y), s, s, [255, 105, 180]);
    img.outline(tpx(m.hutX), tpy(m.hutY), s, s, [255, 105, 180], lw);
  }
  // spawn: bright yellow square with a dark rim so it stands out on any tile
  img.outline(tpx(spawn[0]) - 1, tpy(spawn[1]) - 1, s + 2, s + 2, [0, 0, 0], 1);
  img.rect(tpx(spawn[0]), tpy(spawn[1]), s, s, [255, 255, 0]);
  // monolith centres
  for (const [cx, cy] of centres) img.ring(win, cx, cy, 2.5, [255, 24, 24], 1, Math.max(1.5, s / 3));

  // coordinate ticks on the top and left borders
  for (let t = 0; t < Math.max(w, h); t++) {
    const wx = x0 + t, wy = y0 + t;
    if (t < w && wx % 10 === 0) img.rect(t * s, 0, 1, wx % 50 === 0 ? 7 : 3, [255, 255, 255]);
    if (t < h && wy % 10 === 0) img.rect(0, t * s, wy % 50 === 0 ? 7 : 3, 1, [255, 255, 255]);
  }
  return img;
}

// --- main ----------------------------------------------------------------------
const t0 = performance.now();
const world = genWorld(SEED);
const genMs = performance.now() - t0;

const metrics = computeMetrics(world, SEED, genMs);
mkdirSync(OUT, { recursive: true });
const written = [];

if (!flags['metrics-only']) {
  const dist = landDistance(world.tiles);
  const spawn = findSpawn(world), medics = findMedicSpawns(world);

  const ov = renderOverview(world, dist);
  writePng(path.join(OUT, 'overview.png'), ov.w, ov.h, ov.rgb);
  written.push('overview.png');

  const windows = [
    ...MAJOR_ISLANDS.map((m) => ({ name: m.id, cx: m.center[0], cy: m.center[1], size: 200 })),
    { name: 'core', cx: CORE[0], cy: CORE[1], size: 60 },
    ...flags.crop.map((spec, n) => {
      const [x, y, size, name] = String(spec).split(',');
      if (![x, y, size].every((v) => Number.isFinite(Number(v)))) throw new Error(`bad --crop=${spec} (want x,y,size[,name])`);
      return { name: name || `crop${n + 1}`, cx: Number(x), cy: Number(y), size: Number(size) };
    }),
  ];
  metrics.crops = {};
  for (const wdef of windows) {
    const win = { x0: wdef.cx - Math.floor(wdef.size / 2), y0: wdef.cy - Math.floor(wdef.size / 2), w: wdef.size, h: wdef.size, scale: SCALE };
    const img = renderCrop(world, dist, spawn, medics, win);
    writePng(path.join(OUT, `${wdef.name}.png`), img.w, img.h, img.rgb);
    written.push(`${wdef.name}.png`);
    metrics.crops[wdef.name] = { x0: win.x0, y0: win.y0, tiles: wdef.size, scale: SCALE };
  }
}
writeFileSync(path.join(OUT, 'metrics.json'), JSON.stringify(metrics, null, 2) + '\n');
written.push('metrics.json');

console.log(formatSummary(metrics));
console.log(`\nwrote ${written.join(', ')} -> ${OUT}`);
