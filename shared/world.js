// Shared deterministic worldgen — identical on client and server.
import { createNoise2D } from 'simplex-noise';
import alea from 'alea';
import { NODE } from './defs.js';

export const WORLD_VERSION = 5;   // v5: reshaped islands — real coastlines, landforms, forests, plazas
export const SIZE = 1280;
export const T = { GRASS: 0, SAND: 1, SNOW: 2, MUD: 3, WATER: 4, BLIGHT: 5 };
export const TILE_KEYS = ['grass', 'sand', 'snow', 'mud', 'water', 'blight'];
// Four far-flung islands (Woods/GRASS, Dunes/SAND, Spire/SNOW, Marsh/MUD) + Core, separated by open ocean.
export const ISLES = [[180, 180], [1100, 180], [180, 1100], [1100, 1100]];
export const ISLE_R = 70;
export const MONOLITHS = ISLES;
export const CORE = [640, 640];

export const MAJOR_ISLANDS = [
  { id: 'woods', center: ISLES[0], tile: T.GRASS },
  { id: 'dunes', center: ISLES[1], tile: T.SAND },
  { id: 'spire', center: ISLES[2], tile: T.SNOW },
  { id: 'marsh', center: ISLES[3], tile: T.MUD },
];

// Only two islands host a medic in this release — Dunes and Marsh keep no medic.
export const MEDIC_ISLANDS = [
  { islandId: 'woods', sprite: 'medic' },
  { islandId: 'spire', sprite: 'medic_snow' },
];
const MEDIC_ANCHOR_OFFSETS = { woods: [10, -8], spire: [10, 8] };

export const ACTIVATION_I = CORE[1] * SIZE + CORE[0];

export const MINOR_ISLES = [
  { x: 640, y: 180, r: 14, kind: 'rock' },
  { x: 640, y: 1100, r: 13, kind: 'sandbar' },
  { x: 180, y: 640, r: 13, kind: 'driftwood' },
  { x: 1100, y: 640, r: 14, kind: 'ruin' },
  { x: 420, y: 420, r: 11, kind: 'rock' },
  { x: 860, y: 420, r: 11, kind: 'ruin' },
  { x: 420, y: 860, r: 12, kind: 'icefloe' },
  { x: 860, y: 860, r: 11, kind: 'blightshard' },
  { x: 340, y: 180, r: 9, kind: 'sandbar' },
  { x: 180, y: 940, r: 10, kind: 'icefloe' },
  { x: 940, y: 1100, r: 10, kind: 'driftwood' },
  { x: 1100, y: 340, r: 9, kind: 'rock' },
];

export const MOUNTAINS = [
  { x: 150, y: 150, key: 'mountain_woods' },
  { x: 1130, y: 150, key: 'mountain_dunes' },
  { x: 150, y: 1130, key: 'mountain_spire' },
  { x: 1130, y: 1130, key: 'mountain_marsh' },
  { x: 640, y: 630, key: 'mountain_core_obsidian' },   // on the Core island (R~14), backdrop north of the temple
];

export const TEMPLE_PIECES = [
  { i: ACTIVATION_I, key: 'core_activation_dais' },
  { i: (CORE[1] - 5) * SIZE + CORE[0], key: 'core_temple_ruins' },
  { i: (CORE[1] + 1) * SIZE + CORE[0] - 5, key: 'core_temple_arch_ruin' },
  { i: (CORE[1] + 1) * SIZE + CORE[0] + 5, key: 'core_temple_pillar_broken' },
  { i: (CORE[1] + 5) * SIZE + CORE[0] - 3, key: 'core_temple_pillar_broken' },
];

// LANDMARK_BLOCK: Set of tile indices blocked by mountains (3x3 footprint) and temple pieces (except dais).
export const LANDMARK_BLOCK = (() => {
  const s = new Set();
  for (const m of MOUNTAINS) {
    for (let dy = -1; dy <= 1; dy++)
      for (let dx = -1; dx <= 1; dx++)
        s.add((m.y + dy) * SIZE + (m.x + dx));
  }
  for (let p = 1; p < TEMPLE_PIECES.length; p++) s.add(TEMPLE_PIECES[p].i);
  return s;
})();

// Clearings: flat, guaranteed-land, node- and decor-free ground for what the server and client
// later stand on the terrain. k: island (ISLES index). lvl: the terrace level it is levelled to.
// flat: radius levelled exactly; blend: radius by which it has eased back into the natural terrain
// (0 = not levelled); free: radius kept clear of nodes and decor; land: radius that is always
// island ground and never lake. One plaza per monolith (the lore note at +7,+4 included), the spawn
// search point (findSpawn), the medic anchors (MEDIC_ANCHOR_OFFSETS) and the village fires.
// The village fire points mirror the fire seek points of gameserver/world/folk.go folkVillageDefs
// (island centre + off) — move them together.
const VILLAGE_FIRE_OFF = [[-8, 7], [-8, -9], [4, 12], [9, -9]];   // woods, dunes, spire, marsh
const PLAZA_LVL = [2, 1, 3, 1];   // the level each island's plaza, medic and village are levelled to
export const CLEARINGS = (() => {
  const out = [];
  ISLES.forEach(([cx, cy], k) => {
    const id = MAJOR_ISLANDS[k].id, lvl = PLAZA_LVL[k], [fx, fy] = VILLAGE_FIRE_OFF[k];
    out.push({ id: `${id}-monolith`, x: cx, y: cy, k, lvl, flat: 4, blend: 10, free: 4, land: 11 });
    out.push({ id: `${id}-note`, x: cx + 7, y: cy + 4, k, lvl, flat: 0, blend: 0, free: 1, land: 2 });
    out.push({ id: `${id}-village`, x: cx + fx, y: cy + fy, k, lvl, flat: 6, blend: 12, free: 7, land: 10 });
  });
  out.push({ id: 'spawn', x: ISLES[0][0] - 12, y: ISLES[0][1] - 12, k: 0, lvl: PLAZA_LVL[0], flat: 3, blend: 8, free: 3, land: 6 });
  for (const { islandId } of MEDIC_ISLANDS) {
    const k = MAJOR_ISLANDS.findIndex((m) => m.id === islandId), [ox, oy] = MEDIC_ANCHOR_OFFSETS[islandId];
    out.push({ id: `${islandId}-medic`, x: ISLES[k][0] + ox, y: ISLES[k][1] + oy, k, lvl: PLAZA_LVL[k], flat: 3, blend: 7, free: 3, land: 6 });
  }
  return out;
})();

// Minor isle kind -> tile type mapping
const MINOR_KIND_TILE = {
  sandbar: T.SAND, ruin: T.SAND,
  driftwood: T.GRASS, rock: T.GRASS,
  icefloe: T.SNOW,
  blightshard: T.BLIGHT,
};

const nearPOI = (x, y) => {
  if (MONOLITHS.some(([mx, my]) => Math.abs(mx - x) < 3 && Math.abs(my - y) < 3)) return true;
  if (Math.abs(x - CORE[0]) < 6 && Math.abs(y - CORE[1]) < 6) return true;
  // within 2 tiles of any LANDMARK_BLOCK tile
  for (let dy = -2; dy <= 2; dy++)
    for (let dx = -2; dx <= 2; dx++)
      if (LANDMARK_BLOCK.has((y + dy) * SIZE + (x + dx))) return true;
  return false;
};

// =================================================================================================
// World generation. genWorld(seed) runs a fixed sequence of passes:
//
//   1. major islands — islandPass(k), each inside its own box of (2*MAJOR_HALF+1)^2 tiles
//      a. coastline    radial falloff + domain-warped noise + the island's designed bays,
//                      peninsulas, fjords and spits; land = the plan's `area` tiles with the highest
//                      coast value (a quantile, so every seed gets the same area); clearing and
//                      mountain discs forced to land; offshore islets
//      b. tidy         one-tile spikes and notches smoothed, land specks < SPECK removed, small
//                      enclosed lagoons filled
//      c. ocean        water connected to the box edge; chamfer distance to it (dO)
//      d. inland water the biome's lakes / pools / channels, never near the sea or a clearing;
//                      shores smoothed, tiny ponds filled, lake specks removed, cut-off pieces rejoined by causeways
//      e. distances    final ocean, dO, dW (to any water), dL (to inland water)
//      f. terrain      the biome's height field H, clearings eased flat, quantised to levels 1..3,
//                      bump/pit clean-up, ramps cut wherever +1 climbing cannot reach a tile,
//                      pillars, sea slabs and walled pockets lowered, straight cliff runs and
//                      straight ledges notched, lone ledge dashes levelled
//      g. dressing     tileVis, nodes, decor
//   2. the Core (coreRegion)        3. minor isles (minorPass)
//   4. mountain ground stamp        5. derived layers: veins, waterTemp, bergs (after all tiles)
//   6. the nodes / decor maps, built in tile-index order
//
// Portability (gameserver/world/world.go is a bit-exact port): only seeded simplex fields, + - * /,
// Math.abs/floor/min/max/hypot, fixed scan orders (row-major, neighbours W E N S),
// Int32Array BFS queues, and sorts of plain numbers or with a total-order comparator.
// =================================================================================================

const MAJOR_HALF = 88;    // half-size of a major island's box; its edge is always open sea
const LAND_CAP = 85;      // no island land this far from its centre (< ISLE_R + 16; server owns ISLE_R + 20)
const BASE_R = 64;        // nominal coast radius before shaping
const SPECK = 12;         // land pieces smaller than this are removed
const LAGOON_FILL = 60;   // enclosed water left by the coastline smaller than this is filled
const FAR = 1 << 20;      // "no source" chamfer distance
const NO = -Infinity;     // pickTop: not a candidate
const ISLE_T = [T.GRASS, T.SAND, T.SNOW, T.MUD];
const SPAWN_X = ISLES[0][0] - 12, SPAWN_Y = ISLES[0][1] - 12;   // findSpawn's search point
const DECOR_KEYS = ['glow_mushroom', 'sand_ruin_pillar', 'ice_crystal_cluster', 'bone_totem', 'lava_vent'];

// Unit vectors every 22.5 degrees: 0 = east, 4 = south (y grows down), 8 = west, 12 = north.
const DIR16 = [
  [1, 0], [0.9238795325112867, 0.3826834323650898], [0.7071067811865476, 0.7071067811865476],
  [0.3826834323650898, 0.9238795325112867], [0, 1], [-0.3826834323650898, 0.9238795325112867],
  [-0.7071067811865476, 0.7071067811865476], [-0.9238795325112867, 0.3826834323650898], [-1, 0],
  [-0.9238795325112867, -0.3826834323650898], [-0.7071067811865476, -0.7071067811865476],
  [-0.3826834323650898, -0.9238795325112867], [0, -1], [0.3826834323650898, -0.9238795325112867],
  [0.7071067811865476, -0.7071067811865476], [0.9238795325112867, -0.3826834323650898],
];

// Island plans. Coastline features are placed in DIR16 steps from the island's seeded rotation:
//   ['bay',   dir, dist, radius, depth]            a round bite out of the coast, centred dist out
//   ['pen',   dir, from, to, halfWidth, lift]      a peninsula along the ray, tapering to its tip
//   ['fjord', dir, from, halfWidth, depth]         a narrow bent inlet from the open sea in to `from`
//   ['spit',  dir, from, to, halfWidth, lift, turn] a thin spit swinging `turn` DIR16 steps outward
// lakes: [dir, dist, radius, keep] — kept when the island's lake noise beats `keep` (-1 = always).
// area: land before inland water; lobe: radius swing with direction; warp: domain warp (tiles);
// oct: amplitudes of the 26 / 12 / 5.5-tile coast octaves; jag: ridged coast term (sharp headlands
// along noise creases); islet: islet noise level (higher =
// fewer), isletBand: how far offshore (in coast value) they rise; t1 / t2: level thresholds of H.
const ISLE_PLANS = [
  { // Whispering Woods — broad and lobed, two deep bays and a peninsula, one lake
    area: 12350, lobe: 0.2, warp: 10, oct: [0.12, 0.06, 0.02], jag: 0, islet: 0.6, isletBand: 0.24,
    feats: [['bay', 0, 56, 18, 1], ['bay', 6, 58, 15, 1], ['pen', 11, 46, 80, 8, 0.65]],
    lakes: [[3, 36, 12.5, -1], [8, 44, 4.5, -0.3], [13, 30, 4, 0.25]], minPond: 14,
    t1: 0.45, t2: 0.72,
  },
  { // Sinking Dunes — smooth beaches, sand spits, an oasis
    area: 12250, lobe: 0.14, warp: 7, oct: [0.1, 0.035, 0.01], jag: 0, islet: 0.66, isletBand: 0.2,
    feats: [['spit', 2, 56, 82, 2.6, 0.75, 1], ['spit', 9, 56, 82, 2.6, 0.75, -1], ['bay', 5, 60, 17, 0.9], ['pen', 13, 46, 78, 9, 0.5]],
    lakes: [[11, 28, 9.5, -1], [7, 42, 4.5, -0.2]], minPond: 8,
    t1: 0.22, t2: 0.55,
  },
  { // Frozen Spire — jagged, fjord-cut, a few frozen tarns
    area: 12300, lobe: 0.18, warp: 9, oct: [0.1, 0.06, 0.04], jag: 0.2, islet: 0.58, isletBand: 0.24,
    feats: [['fjord', 0, 34, 3, 1], ['fjord', 3, 40, 2.8, 1], ['fjord', 6, 36, 3.2, 1], ['fjord', 10, 42, 2.8, 1], ['fjord', 13, 38, 3, 1]],
    lakes: [[8, 30, 5, -1], [1, 24, 4.5, -0.4], [12, 50, 4, 0.1]], minPond: 8,
    t1: 0.4, t2: 0.8,
  },
  { // Blighted Marsh — ragged mudflat coast, islets, pools and channels (marshWaters)
    area: 12750, lobe: 0.16, warp: 9, oct: [0.1, 0.08, 0.06], jag: 0.04, islet: 0.5, isletBand: 0.3,
    feats: [['bay', 3, 58, 14, 0.9], ['bay', 10, 60, 12, 0.9]],
    lakes: [], minPond: 10,
    t1: 0.62, t2: 0.88,
  },
];
// The Marsh village (CLEARINGS) fishes this pool: 13 tiles from its fire, away from the monolith.
const MARSH_FISHING_POOL = { x: 1118, y: 1082, r: 4 };

// Node quotas per major island: [NODE kind, count, scorer name] — the `count` best-scoring tiles,
// placed in this order. Starmetal: the peaks of the old starmetal field, never on the Frozen Spire.
const NODE_QUOTAS = [
  [[NODE.STARMETAL, 5, 'star'], [NODE.TREE, 990, 'forest'], [NODE.TREE, 90, 'lone'], [NODE.BUSH, 305, 'edge'],
    [NODE.STONE, 60, 'scarp'], [NODE.STONE, 24, 'spawnRock'], [NODE.STONE, 110, 'hill'], [NODE.STONE, 25, 'field']],
  [[NODE.STARMETAL, 10, 'star'], [NODE.BOULDER, 310, 'outcrop'], [NODE.STONE, 269, 'pebble']],
  [[NODE.CRYSTAL, 266, 'crevasse'], [NODE.BOULDER, 439, 'foot'], [NODE.STONE, 253, 'scree']],
  [[NODE.STARMETAL, 13, 'star'], [NODE.BUSH, 318, 'reed'], [NODE.CRYSTAL, 272, 'bog']],
];
const DECOR_QUOTA = [69, 31, 27, 38];   // per major island, DECOR_KEYS[k]
const LAVA_VENTS = 6;                    // on the Core, outside the temple clearing

// Minor isle terrain per kind: level thresholds of H and the width of the shore band.
const MINOR_STYLE = {
  rock: { t1: 0.3, t2: 0.55, shore: 3 },
  sandbar: { t1: 0.55, t2: 0.9, shore: 4 },
  driftwood: { t1: 0.42, t2: 0.75, shore: 3 },
  ruin: { t1: 0.38, t2: 0.68, shore: 3 },
  icefloe: { t1: 0.4, t2: 0.72, shore: 3 },
  blightshard: { t1: 9, t2: 9, shore: 3 },   // blight is always level 1
};

const clamp01 = (v) => (v < 0 ? 0 : v > 1 ? 1 : v);
const ramp = (v, a, b) => clamp01((v - a) / (b - a));   // 0 at a, 1 at b, linear between
// Per-tile grain in about -1..1: field n sampled 1.6 noise units per tile along a 3-4-5 rotation, so
// neighbouring tiles are effectively independent and nothing lines up with the tile axes. (Jitter
// from simplex noise sampled finer than its cell peaks once per cell, so nodes ranked by it fall
// into orchard rows.)
const grain = (n, x, y, off) => n(x * 1.28 + y * 0.96 + off, y * 1.28 - x * 0.96 + off);

// A box: a w*h window of the map; local index li = ly*w + lx.
const makeBox = (cx, cy, half) => ({ cx, cy, x0: cx - half, y0: cy - half, w: 2 * half + 1, h: 2 * half + 1, n: (2 * half + 1) * (2 * half + 1) });

// The DIR16 index nearest the direction (dx, dy) (largest dot product, lowest index on a tie).
function dirIndexOf(dx, dy) {
  let best = 0, bd = -Infinity;
  for (let j = 0; j < 16; j++) {
    const d = DIR16[j][0] * dx + DIR16[j][1] * dy;
    if (d > bd) { bd = d; best = j; }
  }
  return best;
}

// Distance from (px, py) to the segment a-b; SEG_T is set to how far along it (0 at a, 1 at b).
let SEG_T = 0;
function segDist(px, py, ax, ay, bx, by) {
  const ex = bx - ax, ey = by - ay, len2 = ex * ex + ey * ey;
  const t = len2 > 0 ? clamp01(((px - ax) * ex + (py - ay) * ey) / len2) : 0;
  SEG_T = t;
  return Math.hypot(px - (ax + ex * t), py - (ay + ey * t));
}

// Calls fn(li, x, y) for every tile of the box within radius r (integer test dx*dx + dy*dy <= r*r).
function disc(B, x, y, r, fn) {
  const rr = Math.floor(r);
  for (let dy = -rr; dy <= rr; dy++)
    for (let dx = -rr; dx <= rr; dx++) {
      if (dx * dx + dy * dy > r * r) continue;
      const lx = x + dx - B.x0, ly = y + dy - B.y0;
      if (lx < 0 || ly < 0 || lx >= B.w || ly >= B.h) continue;
      fn(ly * B.w + lx, x + dx, y + dy);
    }
}

// 4-connected components of the tiles where mask[li] === val. lab[li] gets a 1-based label (0 =
// not in the set); returns sizes indexed by label. Seeds in tile order, neighbours W E N S.
function components(B, mask, val, lab, queue) {
  const { w, h, n } = B;
  lab.fill(0, 0, n);
  const sizes = [0];
  for (let s = 0; s < n; s++) {
    if (mask[s] !== val || lab[s]) continue;
    const id = sizes.length;
    let qh = 0, qt = 0;
    queue[qt++] = s; lab[s] = id;
    while (qh < qt) {
      const li = queue[qh++], lx = li % w, ly = (li - lx) / w;
      if (lx > 0 && mask[li - 1] === val && !lab[li - 1]) { lab[li - 1] = id; queue[qt++] = li - 1; }
      if (lx < w - 1 && mask[li + 1] === val && !lab[li + 1]) { lab[li + 1] = id; queue[qt++] = li + 1; }
      if (ly > 0 && mask[li - w] === val && !lab[li - w]) { lab[li - w] = id; queue[qt++] = li - w; }
      if (ly < h - 1 && mask[li + w] === val && !lab[li + w]) { lab[li + w] = id; queue[qt++] = li + w; }
    }
    sizes.push(qt);
  }
  return sizes;
}

// 3-4 chamfer distance: out[li] = 3 per straight step + 4 per diagonal step to the nearest tile
// where src[li] is set (FAR if none). Forward then backward raster pass; divide by 3 for tiles.
function chamfer(B, src, out) {
  const { w, h, n } = B;
  for (let li = 0; li < n; li++) out[li] = src[li] ? 0 : FAR;
  for (let ly = 0; ly < h; ly++)
    for (let lx = 0; lx < w; lx++) {
      const li = ly * w + lx;
      let v = out[li];
      if (lx > 0) v = Math.min(v, out[li - 1] + 3);
      if (ly > 0) {
        v = Math.min(v, out[li - w] + 3);
        if (lx > 0) v = Math.min(v, out[li - w - 1] + 4);
        if (lx < w - 1) v = Math.min(v, out[li - w + 1] + 4);
      }
      out[li] = v;
    }
  for (let ly = h - 1; ly >= 0; ly--)
    for (let lx = w - 1; lx >= 0; lx--) {
      const li = ly * w + lx;
      let v = out[li];
      if (lx < w - 1) v = Math.min(v, out[li + 1] + 3);
      if (ly < h - 1) {
        v = Math.min(v, out[li + w] + 3);
        if (lx < w - 1) v = Math.min(v, out[li + w + 1] + 4);
        if (lx > 0) v = Math.min(v, out[li + w - 1] + 4);
      }
      out[li] = v;
    }
}

// Coastline clean-up, double-buffered: water with 6+ of its 8 neighbours on land becomes land,
// land with 2 or fewer becomes water — no one-tile spikes or notches. Box-edge tiles stay water.
function smoothCoast(B, C) {
  const { land, mask } = C;
  const { w, h, n } = B;
  mask.set(land.subarray(0, n));
  for (let ly = 1; ly < h - 1; ly++)
    for (let lx = 1; lx < w - 1; lx++) {
      const li = ly * w + lx;
      const c = land[li - w - 1] + land[li - w] + land[li - w + 1] + land[li - 1] + land[li + 1] + land[li + w - 1] + land[li + w] + land[li + w + 1];
      if (!land[li] && c >= 6) mask[li] = 1;
      else if (land[li] && c <= 2) mask[li] = 0;
    }
  land.set(mask.subarray(0, n));
}

// Lakeshore clean-up, double-buffered: carved water with 6+ of its 8 neighbours on land fills in,
// land with 6+ carved-water neighbours floods — no one-tile spikes, notches or square corners.
function smoothInland(B, C) {
  const { land, carved, mask } = C;
  const { w, h, n } = B;
  mask.set(land.subarray(0, n));
  for (let ly = 1; ly < h - 1; ly++)
    for (let lx = 1; lx < w - 1; lx++) {
      const li = ly * w + lx;
      let cl = 0, cw = 0;
      for (const o of [-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1]) {
        if (land[li + o]) cl++;
        else if (carved[li + o]) cw++;
      }
      if (!land[li] && carved[li] && cl >= 6) mask[li] = 1;
      else if (land[li] && cw >= 6) mask[li] = 0;
    }
  for (let li = 0; li < n; li++)
    if (mask[li] !== land[li]) { land[li] = mask[li]; carved[li] = mask[li] ? 0 : 1; }
}

// Land pieces smaller than SPECK become water (marked carved when asked: they sat in a lake).
function removeSpecks(B, C, markCarved) {
  const { land, carved, lab, queue } = C;
  const sizes = components(B, land, 1, lab, queue);
  for (let li = 0; li < B.n; li++)
    if (land[li] && sizes[lab[li]] < SPECK) { land[li] = 0; if (markCarved) carved[li] = 1; }
}

// Enclosed water bodies (not connected to the box corner, which is always open sea) smaller than
// `min` become land.
function fillSmallWater(B, C, min) {
  const { land, carved, lab, queue } = C;
  const sizes = components(B, land, 0, lab, queue);
  const sea = lab[0];
  for (let li = 0; li < B.n; li++)
    if (!land[li] && lab[li] !== sea && sizes[lab[li]] < min) { land[li] = 1; carved[li] = 0; }
}

// ocean[li] = water connected to the box corner (open sea).
function markOcean(B, C) {
  const { land, ocean, lab, queue } = C;
  components(B, land, 0, lab, queue);
  const sea = lab[0];
  for (let li = 0; li < B.n; li++) ocean[li] = !land[li] && lab[li] === sea ? 1 : 0;
}

// Joins every land piece that inland water cut off from the island's main body back to it with a
// one-tile causeway: a breadth-first search from the main body through carved water; the first
// time it touches a cut-off piece, the path it came by becomes land and the piece joins the search.
function reconnect(B, C) {
  const { land, carved, lab, queue, parent } = C;
  const { w, n } = B;
  const sizes = components(B, land, 1, lab, queue);
  let main = 1;
  for (let l = 2; l < sizes.length; l++) if (sizes[l] > sizes[main]) main = l;
  const need = new Uint8Array(sizes.length);   // 1 = cut off by carved water, 2 = rejoined
  let any = false;
  for (let li = 0; li < n; li++) {
    if (land[li] || !carved[li]) continue;
    for (const nb of [li - 1, li + 1, li - w, li + w])
      if (land[nb] && lab[nb] !== main && !need[lab[nb]]) { need[lab[nb]] = 1; any = true; }
  }
  if (!any) return;
  parent.fill(-1, 0, n);   // -1 unvisited, -2 a land source, else the tile it was reached from
  let qh = 0, qt = 0;
  for (let li = 0; li < n; li++) if (land[li] && lab[li] === main) { parent[li] = -2; queue[qt++] = li; }
  while (qh < qt) {
    const li = queue[qh++];
    for (const nb of [li - 1, li + 1, li - w, li + w]) {
      if (parent[nb] !== -1) continue;
      if (land[nb]) {
        const l = lab[nb];
        if (need[l] !== 1) continue;
        for (let t = li; parent[t] !== -2; t = parent[t]) { land[t] = 1; carved[t] = 0; }
        need[l] = 2;
        for (let s = 0; s < n; s++) if (lab[s] === l && land[s]) { parent[s] = -2; queue[qt++] = s; }
      } else if (carved[nb]) {
        parent[nb] = li; queue[qt++] = nb;
      }
    }
  }
}

// Most common level among counts c1..c3; ties go to the level nearer `own`, then the lower.
function pickLevel(c1, c2, c3, own) {
  let best = 1, bc = -1, bd = 9;
  for (let L = 1; L <= 3; L++) {
    const c = L === 1 ? c1 : L === 2 ? c2 : c3, d = Math.abs(L - own);
    if (c > bc || (c === bc && d < bd)) { best = L; bc = c; bd = d; }
  }
  return best;
}

// Terrace clean-up, double-buffered (scan order does not matter). mode 8: a land tile sharing its
// level with at most two of its eight land neighbours takes their commonest level. mode 4: a land
// tile differing from at least three of its four land neighbours takes their commonest level.
// The box edge is always water, so land tiles never index outside it.
function smoothLevels(B, C, mode) {
  const { land, el, el2 } = C;
  const w = B.w;
  const offs = mode === 8 ? [-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1] : [-1, 1, -w, w];
  el2.set(el.subarray(0, B.n));
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    let c1 = 0, c2 = 0, c3 = 0;
    for (const o of offs) {
      const nb = li + o;
      if (!land[nb]) continue;
      const e = el[nb];
      if (e === 1) c1++; else if (e === 2) c2++; else c3++;
    }
    const own = el[li], same = own === 1 ? c1 : own === 2 ? c2 : c3, tot = c1 + c2 + c3;
    if (mode === 8 ? tot >= 3 && same <= 2 : tot - same >= 3) el2[li] = pickLevel(c1, c2, c3, own);
  }
  el.set(el2.subarray(0, B.n));
}

// Every land tile must be walkable from the sea shore with the normal one-level climb. A
// breadth-first walk from the shore; while some tile stays unreached, the first one in tile order
// that has a reached neighbour (so sits 2+ above it) is set one above the highest such neighbour —
// a ramp — and the walk goes on from it. Returns whether it cut any ramp.
function ensureClimbable(B, C) {
  const { land, ocean, el, reach, queue } = C;
  const { w, n } = B;
  reach.fill(0, 0, n);
  let qh = 0, qt = 0, cut = false;
  for (let li = 0; li < n; li++)
    if (land[li] && (ocean[li - 1] || ocean[li + 1] || ocean[li - w] || ocean[li + w])) { reach[li] = 1; queue[qt++] = li; }
  for (;;) {
    while (qh < qt) {
      const li = queue[qh++], lim = el[li] + 1;
      for (const nb of [li - 1, li + 1, li - w, li + w])
        if (land[nb] && !reach[nb] && el[nb] <= lim) { reach[nb] = 1; queue[qt++] = nb; }
    }
    let fix = -1, to = 0;
    for (let li = 0; li < n && fix < 0; li++) {
      if (!land[li] || reach[li]) continue;
      let best = 0;
      for (const nb of [li - 1, li + 1, li - w, li + w]) if (reach[nb] && el[nb] > best) best = el[nb];
      if (best > 0) { fix = li; to = best + 1; }
    }
    if (fix < 0) return cut;
    el[fix] = to; reach[fix] = 1; queue[qt++] = fix; cut = true;
  }
}

// No walled pockets: a same-level land region of at most 40 tiles with 60%+ of its land border 2+
// levels higher has those walls cut down to one above it (they become ledges). Regions are labelled
// per level; walls collected over the whole box first, then lowered. Lowering never strands a tile.
function unwallPockets(B, C) {
  const { land, el, lab, queue } = C;
  const { w, n } = B;
  const cuts = [];
  for (let L = 1; L <= 2; L++) {
    const sizes = components(B, el, L, lab, queue);
    const per = new Int32Array(sizes.length), walled = new Int32Array(sizes.length);
    for (let li = 0; li < n; li++) {
      if (el[li] !== L) continue;
      for (const nb of [li - 1, li + 1, li - w, li + w]) {
        if (!land[nb] || el[nb] === L) continue;
        per[lab[li]]++;
        if (el[nb] >= L + 2) walled[lab[li]]++;
      }
    }
    for (let li = 0; li < n; li++) {
      const l = lab[li];
      if (el[li] !== L || sizes[l] > 40 || !per[l] || walled[l] * 10 < per[l] * 6) continue;
      for (const nb of [li - 1, li + 1, li - w, li + w]) if (land[nb] && el[nb] >= L + 2) cuts.push(nb, L + 1);
    }
  }
  let changed = false;
  for (let j = 0; j < cuts.length; j += 2) if (el[cuts[j]] > cuts[j + 1]) { el[cuts[j]] = cuts[j + 1]; changed = true; }
  return changed;
}

// No ruler-straight cliffs: in every run of more than 6 consecutive 2-level edges along a tile axis
// (edges between rows y and y+1 at consecutive x, then between columns x and x+1 at consecutive y)
// the higher tile of every 7th edge drops to the middle level — a notch, and a one-step way up.
// Dropping a level-3 tile to 2 next to a level-1 tile never makes a new 2-level edge.
function breakStraightCliffs(B, C) {
  const { land, el } = C;
  const { w, h } = B;
  let changed = false;
  const notch = (a, b) => { if (el[a] > el[b]) el[a] = el[b] + 1; else el[b] = el[a] + 1; changed = true; };
  const cliff = (a, b) => land[a] && land[b] && Math.abs(el[a] - el[b]) >= 2;
  for (let ly = 0; ly < h - 1; ly++) {
    let run = 0;
    for (let lx = 0; lx < w; lx++) {
      const a = ly * w + lx;
      if (!cliff(a, a + w)) { run = 0; continue; }
      if (++run > 6) { notch(a, a + w); run = 0; }
    }
  }
  for (let lx = 0; lx < w - 1; lx++) {
    let run = 0;
    for (let ly = 0; ly < h; ly++) {
      const a = ly * w + lx;
      if (!cliff(a, a + 1)) { run = 0; continue; }
      if (++run > 6) { notch(a, a + 1); run = 0; }
    }
  }
  return changed;
}

// No pillars or slabs, double-buffered: a land tile higher than 3+ of its land neighbours, or than
// all of its 2+ land neighbours, drops to its highest lower neighbour (no one-tile towers); an
// elevated tile in a run of 1-2 elevated land tiles with open sea beyond both ends, along either
// axis, drops to level 1 (no thin raised slabs poking into the sea). Returns whether it changed.
function trimSpikes(B, C) {
  const { land, ocean, el, el2 } = C;
  const { w, n } = B;
  el2.set(el.subarray(0, n));
  let changed = false;
  for (let li = 0; li < n; li++) {
    if (!land[li]) continue;
    const e = el[li];
    let lower = 0, nland = 0, top = 0;
    for (const nb of [li - 1, li + 1, li - w, li + w]) {
      if (!land[nb]) continue;
      nland++;
      if (el[nb] < e) { lower++; if (el[nb] > top) top = el[nb]; }
    }
    if (lower >= 3 || (lower >= 2 && lower === nland)) { el2[li] = top; changed = true; continue; }
    if (e < 2) continue;
    for (const step of [1, w]) {
      let a = li, b = li, wd = 1;
      while (wd <= 3 && land[a - step] && el[a - step] >= 2) { a -= step; wd++; }
      while (wd <= 3 && land[b + step] && el[b + step] >= 2) { b += step; wd++; }
      if (wd <= 2 && ocean[a - step] && ocean[b + step]) { el2[li] = 1; changed = true; break; }
    }
  }
  el.set(el2.subarray(0, n));
  return changed;
}

// Lone ledge dashes. In the iso view a level step shows only as the higher tile's side face toward
// +x or +y ("drawn" edges), so a short jog in an otherwise hidden (north- or west-facing) terrace
// edge shows as a dash floating on flat ground. A stub is a set of at most 3 drawn edges joined at
// tile corners with no other drawn edge within a corner-step. For each 1-level edge of a stub (in
// tile order), the jog is levelled: lowered — the strip of up to STUB_STRIP higher tiles running
// back from it whose north/west side is low, ending on low ground — or raised — the strip of up to
// STUB_STRIP lower tiles running on from it whose south/east side is high, ending on high ground,
// never touching the sea — whichever is shorter (lowering on a tie). Neither exposes a new drawn face. Returns whether
// it changed anything.
const STUB_STRIP = 8;
function levelStubs(B, C) {
  const { land, ocean, el, corner: par } = C;
  const { w, h, n } = B;
  const W1 = w + 1, NC = W1 * (h + 1);
  par.fill(-1, 0, NC);
  const find = (a) => { while (par[a] !== a) { par[a] = par[par[a]]; a = par[a]; } return a; };
  const join = (a, b) => {
    if (par[a] < 0) par[a] = a;
    if (par[b] < 0) par[b] = b;
    const ra = find(a), rb = find(b);
    if (ra !== rb) par[ra] = rb;
  };
  const edges = [];   // [higher tile, step to the lower tile (w: south, 1: east), first corner, second corner]
  for (let li = 0; li < n; li++) {
    if (!land[li]) continue;
    const lx = li % w, ly = (li - lx) / w;
    if (land[li + w] && el[li] > el[li + w]) { const c = (ly + 1) * W1 + lx; join(c, c + 1); edges.push(li, w, c, c + 1); }
    if (land[li + 1] && el[li] > el[li + 1]) { const c = ly * W1 + lx + 1; join(c, c + W1); edges.push(li, 1, c, c + W1); }
  }
  const size = new Int32Array(NC), state = new Uint8Array(NC);   // state: 1 stub, 2 not
  for (let e = 0; e < edges.length; e += 4) size[find(edges[e + 2])]++;
  const isStub = (r) => {
    if (state[r]) return state[r] === 1;
    let alone = size[r] <= 3;
    for (let e = 0; e < edges.length && alone; e += 4) {
      if (find(edges[e + 2]) !== r) continue;
      for (const c of [edges[e + 2], edges[e + 3]]) {
        const cx = c % W1, cy = (c - cx) / W1;
        for (let dy = -1; dy <= 1 && alone; dy++)
          for (let dx = -1; dx <= 1; dx++) {
            const nx = cx + dx, ny = cy + dy;
            if (nx < 0 || ny < 0 || nx > w || ny > h) continue;
            const o = ny * W1 + nx;
            if (par[o] >= 0 && find(o) !== r) { alone = false; break; }
          }
      }
    }
    state[r] = alone ? 1 : 2;
    return alone;
  };
  const nearSea = (t) => ocean[t - 1] || ocean[t + 1] || ocean[t - w] || ocean[t + w];
  let changed = false;
  for (let e = 0; e < edges.length; e += 4) {
    const hh = edges[e], d = edges[e + 1], ll = hh + d, r = find(edges[e + 2]);
    if (size[r] > 3 || !isStub(r)) continue;
    const eH = el[hh], eL = el[ll];
    if (eH - eL !== 1) continue;   // already levelled, or a cliff (those keep their lips)
    const perp = d === 1 ? w : 1;
    let kA = 0;
    for (let t = hh; ; t -= d) {
      if (kA > 0 && (!land[t] || el[t] <= eL)) break;
      if (kA === STUB_STRIP || !land[t] || el[t] !== eH || (land[t - perp] && el[t - perp] > eL)) { kA = 0; break; }
      kA++;
    }
    let kB = 0;
    for (let t = ll; ; t += d) {
      if (kB > 0 && land[t] && el[t] >= eH) break;
      if (kB === STUB_STRIP || !land[t] || el[t] !== eL || !land[t + perp] || el[t + perp] < eH || nearSea(t)) { kB = 0; break; }
      kB++;
    }
    if (kA && (!kB || kA <= kB)) { for (let k = 0; k < kA; k++) el[hh - k * d] = eL; changed = true; }
    else if (kB) { for (let k = 0; k < kB; k++) el[ll + k * d] = eH; changed = true; }
  }
  return changed;
}

// The lowering-only terrace clean-ups (pillars and slabs, walled pockets, straight cliffs, straight
// 1-level ledges), repeated until none changes anything — each only lowers tiles, so that ends.
// Lowering can, rarely, take away the one step up to higher ground, so the climb check runs after
// and the whole settles again if it had to cut a ramp (at most 4 rounds; the climb check always
// runs last, so every tile stays walkable from the shore).
function settleTerraces(B, C) {
  for (let round = 0; round < 4; round++) {
    for (let pass = 0; pass < 8; pass++) {
      const a = trimSpikes(B, C), b = unwallPockets(B, C), c = breakStraightCliffs(B, C), d = breakStraightLedges(B, C);
      if (!a && !b && !c && !d) break;
    }
    if (!ensureClimbable(B, C)) return;
  }
}

// No ruled ledges. In the iso view a level step is drawn only as the higher tile's side face toward
// +x or +y, so only edges whose higher tile is to the north or west show; along a tile axis such a
// run is a ruler-straight screen diagonal. In every run of more than 8 consecutive drawn 1-level
// edges along an axis, the higher tiles of the 9th edge and the next drop to the lower level — a
// two-tile ramp gap (still part of the outline, so no dash is left). Hidden runs are left alone.
function breakStraightLedges(B, C) {
  const { land, el } = C;
  const { w, h } = B;
  let changed = false;
  const scan = (outer, inner, at, step) => {
    for (let o = 0; o < outer; o++) {
      let run = 0;
      for (let k = 0; k < inner; k++) {
        const a = at(o, k), b = a + step;
        if (!(land[a] && land[b] && el[a] - el[b] === 1)) { run = 0; continue; }
        if (++run <= 8) continue;
        el[a] = el[b];
        if (k + 1 < inner) {
          const a2 = at(o, k + 1), b2 = a2 + step;
          if (land[a2] && land[b2] && el[a2] - el[b2] === 1) el[a2] = el[b2];
        }
        changed = true;
        run = 0;
      }
    }
  };
  scan(h - 1, w, (ly, lx) => ly * w + lx, w);    // edges between rows, along x
  scan(w - 1, h, (lx, ly) => ly * w + lx, 1);    // edges between columns, along y
  return changed;
}

// Rocks (boulders, stones, crystals, starmetal) never pack: taken in pickTop's ranking, a tile is
// skipped if it would touch more than one rock side-on, touch a rock that already touches another,
// or have more than two rocks among its eight neighbours — so no 2x2 blocks, no rows of 3, at most
// about one rock per two or three tiles, and the quota spills outward onto the rims and aprons.
// With `spread` (boulders) a tile is skipped if any rock is among its eight neighbours at all.
const isRock = (k) => k === NODE.BOULDER + 1 || k === NODE.STONE + 1 || k === NODE.CRYSTAL + 1 || k === NODE.STARMETAL + 1;
const ROCK_KIND = new Set([NODE.BOULDER, NODE.STONE, NODE.CRYSTAL, NODE.STARMETAL]);
function placeRocks(B, C, count, score, kind, spread) {
  const { nodeK } = C;
  const ranked = pickTop(B, C, B.n, score);
  let placed = 0;
  for (let r = 0; r < ranked.length && placed < count; r++) {
    const li = ranked[r], lx = li % B.w, i = (B.y0 + (li - lx) / B.w) * SIZE + B.x0 + lx;
    let orth = 0, ring = 0, side = -1;
    for (const o of [-1, 1, -SIZE, SIZE]) if (isRock(nodeK[i + o])) { orth++; side = i + o; }
    for (const o of [-SIZE - 1, -SIZE + 1, SIZE - 1, SIZE + 1]) if (isRock(nodeK[i + o])) ring++;
    if (orth > 1 || orth + ring > 2 || (spread && orth + ring > 0)) continue;
    if (orth === 1) {
      let o2 = 0;
      for (const o of [-1, 1, -SIZE, SIZE]) if (isRock(nodeK[side + o])) o2++;
      if (o2 > 0) continue;
    }
    nodeK[i] = kind + 1;
    placed++;
  }
}

// The `count` box tiles with the highest score(li, x, y) (NO = not a candidate), ranked by score
// descending then tile index ascending — a total order, so every platform picks the same tiles.
function pickTop(B, C, count, score) {
  const { cand, score: sc, order } = C;
  let m = 0;
  for (let li = 0; li < B.n; li++) {
    const lx = li % B.w;
    const s = score(li, B.x0 + lx, B.y0 + (li - lx) / B.w);
    if (s === NO) continue;
    cand[m] = li; sc[m] = s; m++;
  }
  const ord = order.subarray(0, m);
  for (let j = 0; j < m; j++) ord[j] = j;
  ord.sort((a, b) => (sc[b] - sc[a]) || (a - b));
  const out = new Int32Array(Math.min(count, m));
  for (let j = 0; j < out.length; j++) out[j] = cand[ord[j]];
  return out;
}

// --- 1. a major island ---------------------------------------------------------------------------

// Coastline features of plan P laid out round the island: DIR16 steps from the seeded rotation;
// bays and fjords never cut towards the island's mountain.
function layoutFeatures(P, k, rot, mdir, shape) {
  return P.feats.map((f, j) => {
    let di = (rot + f[1]) & 15;
    const off = Math.min(Math.abs(di - mdir), 16 - Math.abs(di - mdir));
    if ((f[0] === 'bay' || f[0] === 'fjord') && off <= 1) di = (di + 3) & 15;
    const [ux, uy] = DIR16[di];
    if (f[0] === 'bay') return { kind: 0, x: ux * f[2], y: uy * f[2], r: f[3], amt: f[4] };
    if (f[0] === 'pen') return { kind: 1, ax: ux * f[2], ay: uy * f[2], bx: ux * f[3], by: uy * f[3], hw: f[4], amt: f[5] };
    if (f[0] === 'spit') {
      const [vx, vy] = DIR16[(di + f[6]) & 15];
      return { kind: 1, ax: ux * f[2], ay: uy * f[2], bx: vx * f[3], by: vy * f[3], hw: f[4], amt: f[5] };
    }
    // fjord: from the open sea (92 out) to `from`, bent sideways at its middle
    const bend = 7 * shape(k * 3.7 + j * 1.9, 13.3), mid = (f[2] + 92) / 2;
    return { kind: 2, ax: ux * f[2], ay: uy * f[2], mx: ux * mid - uy * bend, my: uy * mid + ux * bend, bx: ux * 92, by: uy * 92, hw: f[3], amt: f[4] };
  });
}

// Sum of the features' pull on the coast value at warped island-relative (wx, wy).
function featureValue(feats, wx, wy) {
  let v = 0;
  for (const f of feats) {
    if (f.kind === 0) {
      v -= f.amt * clamp01(1 - Math.hypot(wx - f.x, wy - f.y) / f.r);
    } else if (f.kind === 1) {
      const sd = segDist(wx, wy, f.ax, f.ay, f.bx, f.by);
      v += f.amt * clamp01(1 - sd / (f.hw * (1 - 0.55 * SEG_T)));
    } else {
      const sd = Math.min(segDist(wx, wy, f.ax, f.ay, f.mx, f.my), segDist(wx, wy, f.mx, f.my, f.bx, f.by));
      v -= f.amt * clamp01(1.5 - sd / f.hw);
    }
  }
  return v;
}

// Woods rock outcrops: each a raised table with one rock face. Across the face (b, towards the high
// side) the ground is pulled up to level 3 for `depth` tiles, then eases back down over `slope`
// tiles — a one-step back slope; in front of the face an apron eases from level 1 back into the
// hills over `footW` tiles. Along the face (a) it fades out over 6 tiles at ragged ends. The face
// is bowed and wiggled and never runs along a tile axis (tangents are odd DIR16 steps). Outcrop 0's
// face drops to the east-north-east (test.mjs scans the Woods for an eastward 2-level step to
// exercise fall damage).
function woodsScarps(B, C, rot) {
  const out = [];
  const add = (di, ti) => {
    const [tx, ty] = DIR16[ti];
    const [x, y] = scarpSite(B, C, di, tx, ty), j = out.length;
    out.push({ x, y, tx, ty, nx: -ty, ny: tx, len: 12, depth: 6, slope: 7, footW: 7,
      bow: 3 * C.N.shape(rot * 0.37 + j * 5.1, 61.7), off: 31.7 * (j + 1) });
  };
  add((rot + 9) & 15, 3);                          // tangent 22.5 degrees off south, high side west
  add((rot + 14) & 15, ((rot * 3 + 4) & 15) | 1);
  return out;
}
// Where an outcrop goes: of the sites in every DIR16 direction 28, 34 and 40 out, one whose centre
// is 10+ tiles from any water and whose two ends (12 along the tangent) are 6+, beyond every Woods
// clearing's blend radius (so it is not levelled away), nearest the planned direction `di`, then
// with dry ground behind the table (13 back, 8+ tiles from water) and in front of the face (7 out,
// 6+), then the driest. Returns the site in warped island-relative coordinates.
function scarpSite(B, C, di, tx, ty) {
  let best = -Infinity, bx = 0, by = 0;
  const at = (x, y) => (y - B.y0) * B.w + x - B.x0;
  for (let s = 0; s < 16; s++)
    for (const dd of [28, 34, 40]) {
      const x = Math.floor(B.cx + DIR16[s][0] * dd + 0.5), y = Math.floor(B.cy + DIR16[s][1] * dd + 0.5);
      const ex = Math.floor(tx * 12 + 0.5), ey = Math.floor(ty * 12 + 0.5);
      const li = at(x, y);
      if (!C.land[li] || C.dW[li] < 30 || C.dW[at(x + ex, y + ey)] < 18 || C.dW[at(x - ex, y - ey)] < 18) continue;
      if (CLEARINGS.some((c) => c.k === 0 && Math.hypot(x - c.x, y - c.y) <= c.blend + 13)) continue;
      const off = Math.min(Math.abs(s - di), 16 - Math.abs(s - di));
      const back = C.dW[at(x - Math.floor(ty * 13 + 0.5), y + Math.floor(tx * 13 + 0.5))];
      const front = C.dW[at(x + Math.floor(ty * 7 + 0.5), y - Math.floor(tx * 7 + 0.5))];
      const score = Math.min(C.dW[li] / 3, 14) - 1.5 * off - (back < 24 ? 3 : 0) - (front < 18 ? 3 : 0);
      if (score > best) { best = score; bx = C.px[li]; by = C.py[li]; }
    }
  return [bx, by];
}
// Applies the outcrops to height h at warped (wx, wy); `near` (0..1) fades them out by the water.
// The face is an arc (bow) wiggled by noise n along its length; length and table depth wander too.
// SCARP_ROCK is set to how strongly the tile is part of an outcrop (for stones).
let SCARP_ROCK = 0;
function applyScarps(scarps, wx, wy, h, near, top, foot, n) {
  SCARP_ROCK = 0;
  for (const c of scarps) {
    const ox = wx - c.x, oy = wy - c.y;
    const a = ox * c.tx + oy * c.ty;
    const b = ox * c.nx + oy * c.ny + c.bow * ((a * a) / (c.len * c.len) - 0.3) + 1.5 * n(a / 5 + c.off, c.off);
    const along = ramp(c.len + 2 * n(b / 10 + c.off + 11, c.off + 5) - Math.abs(a), 0, 7) * near;
    if (along <= 0) continue;
    const depth = c.depth + 2 * n(a / 6 + c.off + 23, c.off + 9);
    if (b >= 0) {
      const s = along * (b < depth ? 1 : 1 - (b - depth) / c.slope);
      if (s > 0) { h += s * (top - h); if (s > SCARP_ROCK) SCARP_ROCK = s; }
    } else if (b > -c.footW) {
      h += along * (1 + b / c.footW) * (foot - h);
    }
  }
  return h;
}

// Barchan dunes, 0..~1 at tile (x, y). The ground is cut into cells of BARCHAN_CELL tiles in a frame
// turned to the wind (u downwind, v across, a 30-degree rotation, then warped a little so crests
// meander); a cell holds a dune unless its presence sample is low. A dune is an elongated mound of
// radius 6..11 round a jittered centre, with a hollow bitten out of its downwind side so its horns
// point downwind; bigger dunes stand taller. The tallest dune of the tile's 3x3 cells wins. Every
// per-cell sample is the shape field at the cell's integer coordinates times fixed factors.
const BARCHAN_CELL = [18, 14];
function barchans(shape, warp, x, y) {
  const u = x * 0.8660254037844387 + y * 0.5 + 3 * warp(x / 9 + 2000, y / 9 + 2000);
  const v = y * 0.8660254037844387 - x * 0.5 + 3 * warp(x / 9 + 2040, y / 9 + 2040);
  const [cw, ch] = BARCHAN_CELL;
  const iu = Math.floor(u / cw), iv = Math.floor(v / ch);
  let best = 0;
  for (let cu = iu - 1; cu <= iu + 1; cu++)
    for (let cv = iv - 1; cv <= iv + 1; cv++) {
      const sx = cu * 1.37, sy = cv * 1.91;
      if (shape(sx + 300, sy + 300) < -0.45) continue;   // an empty cell
      const size = 0.5 + 0.5 * shape(sx + 420, sy + 420);
      const r = 6 + 5 * size;
      const du = u - (cu + 0.5 + 0.3 * shape(sx + 340, sy + 340)) * cw;
      const dv = v - (cv + 0.5 + 0.35 * shape(sx + 380, sy + 380)) * ch;
      const body = 1 - Math.hypot(du * 0.8, dv) / r;
      if (body <= 0) continue;
      const bite = 1 - Math.hypot(du - 0.6 * r, dv * 1.1) / (0.8 * r);
      const hgt = clamp01(body * 1.8) * (1 - clamp01(bite * 2.5)) * (0.6 + 0.4 * size);
      if (hgt > best) best = hgt;
    }
  return best;
}

// Dunes rocks: four table rocks or buttes of different sizes and four small outcrops, each placed by
// duneRockSites. [dir offset (DIR16 steps from the island rotation), distance, radius, tall]: a tall
// rock is a butte (level-3 top inside a level-2 rim), a low one a level-2 table.
const DUNE_ROCKS = [
  [1, 34, 11, 1], [6, 44, 8, 1], [10, 30, 9, 0], [13, 48, 7, 1],
  [3, 50, 3.5, 1], [8, 52, 3, 1], [12, 26, 4, 0], [15, 40, 3, 1],
];
// Where each Dunes rock goes: of the sites in every DIR16 direction at dist - 8, dist and dist + 8,
// one inland (radius + 5 tiles from the sea, radius + 2 from any water), clear of the clearings by
// radius + 6 and of the rocks already placed by both radii + 5, nearest the planned direction and
// distance. A rock with no such site is left out. Each gets a seeded long axis and elongation.
function duneRockSites(B, C, k, rot, shape) {
  const out = [];
  DUNE_ROCKS.forEach(([dOff, dist, r, tall], j) => {
    const di = (rot + dOff) & 15;
    let best = -Infinity, bx = 0, by = 0;
    for (let s = 0; s < 16; s++)
      for (const dd of [dist - 8, dist, dist + 8]) {
        const x = Math.floor(B.cx + DIR16[s][0] * dd + 0.5), y = Math.floor(B.cy + DIR16[s][1] * dd + 0.5);
        const li = (y - B.y0) * B.w + x - B.x0;
        if (!C.land[li] || C.dO[li] < (r + 5) * 3 || C.dW[li] < (r + 2) * 3 || !dryClear(k, x, y, r + 6)) continue;
        if (out.some((o) => Math.hypot(x - o.gx, y - o.gy) < o.r + r + 5)) continue;
        const off = Math.min(Math.abs(s - di), 16 - Math.abs(s - di));
        const score = -off - Math.abs(dd - dist) / 8;
        if (score > best) { best = score; bx = x; by = y; }
      }
    if (best === -Infinity) return;
    const li = (by - B.y0) * B.w + bx - B.x0;
    const [ax, ay] = DIR16[(rot * 7 + j * 5) & 15];
    out.push({ gx: bx, gy: by, x: C.px[li], y: C.py[li], r, tall, ax, ay, stretch: 1 + 0.35 * (0.5 + 0.5 * shape(j * 4.3 + 77, k * 2.1)) });
  });
  return out;
}

// Is (x, y) far enough from island k's clearings, mountain and monolith for inland water?
// margin widens every clearing's guaranteed-land radius.
function dryClear(k, x, y, margin) {
  for (const c of CLEARINGS) {
    if (c.k !== k) continue;
    const r = c.land + margin, dx = x - c.x, dy = y - c.y;
    if (dx * dx + dy * dy <= r * r) return false;
  }
  const m = MOUNTAINS[k], mx = x - m.x, my = y - m.y;
  if (mx * mx + my * my <= 25) return false;
  const ox = x - ISLES[k][0], oy = y - ISLES[k][1];
  return ox * ox + oy * oy > 81;   // never within 9 of the monolith
}

function islandPass(k, C) {
  const P = ISLE_PLANS[k], [cx, cy] = ISLES[k], tile = ISLE_T[k];
  const B = makeBox(cx, cy, MAJOR_HALF);
  const { w, n } = B;
  const { land, carved, ocean, val, px, py, dO, dW, dL, H, el, aux1, aux2 } = C;
  const { elev: elevN, coast, warp, shape } = C.N;
  const rot = Math.min(15, Math.floor((shape(k * 13.7 + 2.3, 5.9) + 1) * 8));
  const mdir = dirIndexOf(MOUNTAINS[k].x - cx, MOUNTAINS[k].y - cy);

  // ---- a. coastline: value per tile, the top `area` become land ----
  const feats = layoutFeatures(P, k, rot, mdir, shape);
  let cnt = 0;
  for (let ly = 0; ly < B.h; ly++)
    for (let lx = 0; lx < w; lx++) {
      const li = ly * w + lx, x = B.x0 + lx, y = B.y0 + ly, ox = x - cx, oy = y - cy;
      const wx = ox + P.warp * warp(x / 38, y / 38), wy = oy + P.warp * warp(x / 38 + 71.3, y / 38 + 37.9);
      px[li] = wx; py[li] = wy; land[li] = 0; carved[li] = 0;
      const d = Math.hypot(ox, oy);
      if (d >= LAND_CAP) { val[li] = -9; continue; }
      const q = Math.hypot(wx, wy), ux = q > 1e-9 ? wx / q : 1, uy = q > 1e-9 ? wy / q : 0;
      const lobe = (shape(ux * 0.55 + k * 23.1, uy * 0.55 + 7.7) + 0.5 * shape(ux * 1.3 + k * 23.1 + 40.2, uy * 1.3 - 12.4)) / 1.5;
      const ax = cx + wx, ay = cy + wy;
      let v = 1 - q / (BASE_R * (1 + P.lobe * lobe))
        + P.oct[0] * coast(ax / 26, ay / 26) + P.oct[1] * coast(ax / 12 + 50, ay / 12 + 50) + P.oct[2] * coast(ax / 5.5 + 100, ay / 5.5 + 100)
        + (P.jag ? P.jag * (0.6 - Math.abs(coast(ax / 7 + 150, ay / 7 + 150))) : 0)
        + featureValue(feats, wx, wy);
      if (d > 70) v -= (d - 70) * 0.05;   // soft fence well inside LAND_CAP
      val[li] = v;
      C.sortBuf[cnt++] = v;
    }
  const th = C.sortBuf.subarray(0, cnt).sort()[cnt - P.area];
  for (let li = 0; li < n; li++) if (val[li] >= th) land[li] = 1;
  for (const c of CLEARINGS) if (c.k === k) disc(B, c.x, c.y, c.land, (li) => { land[li] = 1; });
  disc(B, MOUNTAINS[k].x, MOUNTAINS[k].y, 3, (li) => { land[li] = 1; });
  // offshore islets and skerries in the band just off the coast
  for (let li = 0; li < n; li++) {
    if (land[li] || !(val[li] > th - P.isletBand)) continue;
    const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w;
    if (Math.hypot(x - cx, y - cy) < 83 && warp(x / 8 + 300 + 40 * k, y / 8 + 300) > P.islet) land[li] = 1;
  }

  // ---- b. tidy ----
  smoothCoast(B, C);
  for (const c of CLEARINGS) if (c.k === k) disc(B, c.x, c.y, c.land, (li) => { land[li] = 1; });
  removeSpecks(B, C, false);
  fillSmallWater(B, C, LAGOON_FILL);

  // ---- c. ocean and the distance to it ----
  markOcean(B, C);
  chamfer(B, ocean, dO);

  // ---- d. inland water ----
  if (k === 3) marshWaters(B, C, k);
  else {
    P.lakes.forEach(([dOff, dist, rad, keep], j) => {
      if (j > 0 && !(shape(k * 7.7 + j * 3.1, 21.9) > keep)) return;
      const [lxc, lyc] = lakeSite(B, C, k, (rot + dOff) & 15, dist + 5 * shape(k * 5.1 + j * 2.7, 33.3), rad);
      const [sx, sy] = DIR16[(rot * 5 + j * 7) & 15];   // the lake's long axis
      for (let li = 0; li < n; li++) {
        if (!land[li] || dO[li] < 18) continue;   // at least 6 tiles of shore between lake and sea
        const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w, ax = cx + px[li], ay = cy + py[li];
        // an elongated blob in its own small warp, edged by two octaves of coast noise
        const ex = px[li] - lxc + 3 * warp(x / 9 + 500, y / 9 + 500), ey = py[li] - lyc + 3 * warp(x / 9 + 540, y / 9 + 540);
        const along = ex * sx + ey * sy, across = ey * sx - ex * sy;
        const lv = 1 - Math.hypot(along / 1.3, across * 1.3) / rad
          + 0.28 * coast(ax / 8 + 200, ay / 8 + 200) + 0.12 * coast(ax / 3.5 + 220, ay / 3.5 + 220);
        if (!(lv > 0) || !dryClear(k, x, y, 2)) continue;
        land[li] = 0; carved[li] = 1;
      }
    });
  }
  smoothInland(B, C);
  fillSmallWater(B, C, P.minPond);
  removeSpecks(B, C, true);
  reconnect(B, C);
  fillSmallWater(B, C, P.minPond);

  // ---- e. final distances ----
  markOcean(B, C);
  chamfer(B, ocean, dO);
  for (let li = 0; li < n; li++) C.mask[li] = land[li] ? 0 : 1;
  chamfer(B, C.mask, dW);
  for (let li = 0; li < n; li++) C.mask[li] = !land[li] && !ocean[li] ? 1 : 0;
  chamfer(B, C.mask, dL);

  // ---- f. terrain ----
  const scarps = k === 0 ? woodsScarps(B, C, rot) : null;
  const duneRocks = k === 1 ? duneRockSites(B, C, k, rot, shape) : null;
  for (let li = 0; li < n; li++) {
    aux1[li] = 0; aux2[li] = 0;
    if (!land[li]) continue;
    const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w;
    const wx = px[li], wy = py[li], dOt = dO[li] / 3, dWt = dW[li] / 3, dLt = dL[li] / 3;
    let h;
    if (k === 0) {
      // Woods: rolling lowland rising to a few hilltops, rock scarps
      const hills = 0.5 + 0.5 * (0.7 * elevN(x / 46, y / 46) + 0.3 * elevN(x / 19 + 40, y / 19 + 40));
      const shore = ramp(dWt + 1.8 * elevN(x / 8 + 60, y / 8 + 60), 1, 8);   // jittered: chamfer bands are octagons
      h = applyScarps(scarps, wx, wy, shore * (0.2 + 0.8 * hills), ramp(dWt, 2, 8), P.t2 + 0.12, P.t1 - 0.15, elevN);
      aux2[li] = SCARP_ROCK;   // rockiness, for stones
    } else if (k === 1) {
      // Dunes: a field of crescent (barchan) dunes blown along a 30-degree wind, flat pans, a few rocks
      const erg = ramp(elevN(x / 52 + 300, y / 52 + 300), -0.25, 0.05);   // 0 in the pans, 1 in the dune sea
      const dune = barchans(shape, warp, x, y) * erg;
      // the rocks: the strongest of the warped, lobed, elongated blobs; rim where mv > 0, top past 0.25
      const rx = 2.5 * warp(x / 7 + 2300, y / 7 + 2300), ry = 2.5 * warp(x / 7 + 2340, y / 7 + 2340);
      const edge = 0.22 * coast((cx + wx) / 6 + 260, (cy + wy) / 6 + 260) + 0.1 * coast((cx + wx) / 3 + 280, (cy + wy) / 3 + 280);
      let mv = -9, tall = 0;
      for (const r of duneRocks) {
        const ox = wx - r.x + rx, oy = wy - r.y + ry;
        const along = ox * r.ax + oy * r.ay, across = oy * r.ax - ox * r.ay;
        const v = 1 - Math.hypot(along / r.stretch, across * r.stretch) / r.r + edge;
        if (v > mv) { mv = v; tall = r.tall; }
      }
      h = ramp(dWt + 1.3 * elevN(x / 7 + 660, y / 7 + 660), 1.3, 5.5) * (0.06 + 0.8 * dune)
        + 0.45 * ramp(mv, 0, 0.08) + (tall ? 0.45 * ramp(mv, 0.25, 0.33) : 0);
      aux1[li] = erg; aux2[li] = mv;
    } else if (k === 2) {
      // Spire: coastal lowland, a plateau, a high massif round the monolith; crags and valleys
      const q = Math.hypot(wx, wy), ux = q > 1e-9 ? wx / q : 1, uy = q > 1e-9 ? wy / q : 0;
      const massR = 27 + 7 * shape(ux * 0.6 + 900, uy * 0.6 + 900) + 3 * elevN(x / 9 + 1000, y / 9 + 1000);
      const lowW = (6 + 3 * elevN(x / 17 + 1100, y / 17 + 1100)) * (1 - ramp(elevN(x / 24 + 1200, y / 24 + 1200), 0.18, 0.33));
      const jit = 1.5 * elevN(x / 7 + 1150, y / 7 + 1150) + 0.8 * elevN(x / 3 + 1170, y / 3 + 1170);   // ragged tier edges
      const tierP = ramp(dOt + jit - lowW, 0, 1.2), tierM = ramp(massR - q + 0.8 * elevN(x / 3 + 1190, y / 3 + 1190), 0, 1.5);
      const crag = ramp(elevN(x / 20 + 1300, y / 20 + 1300), 0.3, 0.42);
      const valley = 0.42 * ramp(1 - Math.abs(elevN((cx + wx) / 40 + 1400, (cy + wy) / 40 + 1400)), 0.9, 0.96);
      h = 0.2 + 0.4 * tierP + 0.4 * Math.max(tierM, crag * tierP) - valley - 0.4 * (1 - ramp(dLt, 1, 3));
    } else {
      // Marsh: low and flat, scattered hummocks
      const hx = x + 2 * warp(x / 5 + 2050, y / 5 + 2050), hy = y + 2 * warp(x / 5 + 2090, y / 5 + 2090);   // lumpy, not boxy
      const hum = 0.65 * elevN(hx / 13 + 1500, hy / 13 + 1500) + 0.35 * elevN(hx / 6 + 1600, hy / 6 + 1600);
      h = ramp(dWt + elevN(x / 7 + 1650, y / 7 + 1650), 1, 4) * (0.5 + 0.5 * hum);
    }
    H[li] = h;
  }
  // clearings (and the mountain footprint) eased flat
  const mid = [0, P.t1 * 0.5, (P.t1 + P.t2) / 2, P.t2 + 0.15];
  const flatten = (x, y, flat, blend, target) => disc(B, x, y, blend, (li, tx, ty) => {
    if (!land[li]) return;
    const r = Math.hypot(tx - x, ty - y);
    const a = r <= flat ? 1 : (blend - r) / (blend - flat), s = a * a * (3 - 2 * a);
    H[li] += s * (target - H[li]);
  });
  for (const c of CLEARINGS) if (c.k === k && c.blend > 0) flatten(c.x, c.y, c.flat, c.blend, mid[c.lvl]);
  { const m = MOUNTAINS[k]; flatten(m.x, m.y, 2, 5, H[(m.y - B.y0) * w + m.x - B.x0]); }
  for (let li = 0; li < n; li++) el[li] = land[li] ? (H[li] > P.t2 ? 3 : H[li] > P.t1 ? 2 : 1) : 0;
  smoothLevels(B, C, 8);
  smoothLevels(B, C, 8);
  smoothLevels(B, C, 4);
  ensureClimbable(B, C);
  settleTerraces(B, C);
  for (let round = 0; round < 3 && levelStubs(B, C); round++) settleTerraces(B, C);
  for (let li = 0; li < n; li++) {
    if (!land[li]) continue;
    const lx = li % w, i = (B.y0 + (li - lx) / w) * SIZE + B.x0 + lx;
    C.tiles[i] = tile; C.elev[i] = el[li];
  }

  // ---- g. dressing ----
  dressIsland(B, C, k);
}

// Where a lake goes: of the sites in every DIR16 direction at dist - 6, dist and dist + 6 from the
// centre, the one that is deepest inland (sea distance, counted up to rad + 10 tiles) with its whole
// body clear of the clearings, preferring the planned direction `di` (half a tile of depth per
// DIR16 step away). Returns the site in warped island-relative coordinates (lakes are drawn there).
function lakeSite(B, C, k, di, dist, rad) {
  let best = -Infinity, bx = 0, by = 0;
  for (let s = 0; s < 16; s++)
    for (const dd of [dist - 6, dist, dist + 6]) {
      const x = Math.floor(B.cx + DIR16[s][0] * dd + 0.5), y = Math.floor(B.cy + DIR16[s][1] * dd + 0.5);
      const li = (y - B.y0) * B.w + x - B.x0;
      if (!C.land[li] || !dryClear(k, x, y, rad + 2)) continue;
      const off = Math.min(Math.abs(s - di), 16 - Math.abs(s - di));
      const score = Math.min(C.dO[li] / 3, rad + 10) - 0.5 * off;
      if (score > best) { best = score; bx = C.px[li]; by = C.py[li]; }
    }
  return [bx, by];
}

// Marsh inland water: pools of mixed size, meandering channels that widen and narrow and taper
// out at their ends, and the village's fishing pool.
function marshWaters(B, C, k) {
  const { land, carved, dO } = C;
  const { elev: elevN, warp } = C.N;
  const fp = MARSH_FISHING_POOL;
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w;
    // pools: low-frequency blobs in a small warp, so they come out round and lobed, not boxy
    const qx = x + 4 * warp(x / 8 + 1600, y / 8 + 1600), qy = y + 4 * warp(x / 8 + 1650, y / 8 + 1650);
    const pool = 0.85 * elevN(qx / 11 + 1700, qy / 11 + 1700) + 0.15 * elevN(qx / 5.5 + 1750, qy / 5.5 + 1750);
    // channels: the zero line of a field over a strongly warped domain turned off the tile axes
    // (3-4-5 rotation); its half-width swings along the channel and closes where the gate does
    const mx = x + 8 * warp(x / 13 + 1900, y / 13 + 1900), my = y + 8 * warp(x / 13 + 1950, y / 13 + 1950);
    const u = mx * 0.8 + my * 0.6, v = my * 0.8 - mx * 0.6;
    const gate = ramp(elevN(x / 45 + 1850, y / 45 + 1850), -0.05, 0.25);
    const half = (0.06 + 0.05 * (0.5 + 0.5 * elevN(x / 12 + 1880, y / 12 + 1880))) * gate;
    const chan = Math.abs(elevN(u / 30 + 1800, v / 30 + 1800)) < half;
    const fx = x - fp.x + 1.5 * warp(x / 4 + 1980, y / 4 + 1980), fy = y - fp.y + 1.5 * warp(x / 4 + 1990, y / 4 + 1990);
    let wet = false;
    if (Math.hypot(fx, fy) < fp.r) wet = dryClear(k, x, y, 0);
    else if (pool > 0.48 && dO[li] >= 9) wet = dryClear(k, x, y, 2);
    else if (chan && dO[li] >= 6) wet = dryClear(k, x, y, 2);
    if (wet) { land[li] = 0; carved[li] = 1; }
  }
}

// The spawn glade's edge: within GLADE_EDGE tiles of the spawn no two trees touch. Nearest first
// (squared distance, then tile index): a tree is felled if a 4-neighbour tree is nearer, or as near
// and earlier in tile order. The first trees a keeper reaches stand apart — test.mjs builds a
// workbench, a mineshaft and a farmplot on and beside the nearest ones.
const GLADE_EDGE = 9;
function spaceGladeTrees(nodeK) {
  const TREE = NODE.TREE + 1, R = GLADE_EDGE;
  for (let d2 = 0; d2 <= R * R; d2++)
    for (let dy = -R; dy <= R; dy++)
      for (let dx = -R; dx <= R; dx++) {
        if (dx * dx + dy * dy !== d2) continue;
        const i = (SPAWN_Y + dy) * SIZE + SPAWN_X + dx;
        if (nodeK[i] !== TREE) continue;
        for (const [ox, oy] of [[-1, 0], [1, 0], [0, -1], [0, 1]]) {
          const ex = dx + ox, ey = dy + oy, j = i + oy * SIZE + ox, e2 = ex * ex + ey * ey;
          if (nodeK[j] === TREE && (e2 < d2 || (e2 === d2 && j < i))) { nodeK[i] = 0; break; }
        }
      }
}

// tileVis, nodes and decor of a major island.
function dressIsland(B, C, k) {
  const { land, el, dO, dW, dL, dT, H, aux1, aux2, aux3, noNode, nodeK, decorK, tileVis } = C;
  const { elev: elevN, veg, sct } = C.N;
  const P = ISLE_PLANS[k], w = B.w;
  const gi = (li) => { const lx = li % w; return (B.y0 + (li - lx) / w) * SIZE + B.x0 + lx; };
  // tiles no node or decor may take: off the island, near a POI, inside a clearing's free radius
  for (let li = 0; li < B.n; li++) noNode[li] = 1;
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w;
    noNode[li] = nearPOI(x, y) ? 1 : 0;
  }
  for (const c of CLEARINGS) if (c.k === k) disc(B, c.x, c.y, c.free, (li) => { noNode[li] = 1; });
  const open = (li) => !noNode[li] && !nodeK[gi(li)];

  // Woods forest density (aux1, 0..1) and its raw field (aux3, for ranking): low-frequency stands
  // favouring the hills, a ring of woodland round the spawn glade, glades punched out, kept off the
  // water's edge
  if (k === 0) {
    for (let li = 0; li < B.n; li++) {
      if (!land[li]) continue;
      const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w, e = el[li];
      let f = 0.75 * veg(x / 34, y / 34) + 0.25 * veg(x / 13 + 31, y / 13 + 31) + (e === 3 ? 0.22 : e === 2 ? 0.06 : 0);
      const sp = Math.hypot(x - SPAWN_X, y - SPAWN_Y);
      if (sp < 20) f += 0.15 * ramp(sp, 3, 7) * ramp(20 - sp, 0, 6);
      let F = ramp(f, -0.3, 0.6);
      const g = veg(x / 6.5 + 90, y / 6.5 + 90);
      if (g > 0.5) F *= ramp(0.7 - g, 0, 0.2);
      const keep = ramp(dW[li] / 3 + 2.5 * sct(x / 5 + 770, y / 5 + 770), 1.5, 4);   // a ragged, gappy setback from the water
      aux1[li] = F * keep;
      aux3[li] = f - 0.6 * (1 - keep);
    }
  }

  // node scorers: score(li, x, y) -> number, or NO when the tile is not a candidate
  const S = {
    star: (li, x, y) => (open(li) ? sct(x / 2 + 700, y / 2 + 700) : NO),
    // the woods' best stands, nearly solid with a few gaps, edges ragged by a little jitter
    // the densest stands first, thinning toward their edges; groves and small glades inside, and
    // a random third of the tiles left open so spacing is irregular, with clumps and gaps
    forest: (li, x, y) => {
      if (!open(li) || aux1[li] <= 0.3) return NO;
      const grove = sct(x / 4.5 + 510, y / 4.5 + 510);
      if (grove < -0.5 || grain(sct, x, y, 505) < -0.35) return NO;
      return aux3[li] + 0.12 * grove + 0.18 * grain(sct, x, y, 500);
    },
    lone: (li, x, y) => (open(li) && aux1[li] <= 0.3 && dW[li] >= 9 && dT[li] > 12 ? grain(sct, x, y, 520) : NO),
    // bushes 1-4 tiles out from the trees, and round the lake
    edge: (li, x, y) => {
      if (!open(li)) return NO;
      let e = dT[li] > 0 && dT[li] <= 12 ? 1.2 - dT[li] / 12 : 0;
      if (dL[li] <= 6) e += 0.5 * ramp(sct(x / 6 + 775, y / 6 + 775), 0, 0.4);   // patches along the lake
      return e > 0 ? e + 0.4 * grain(sct, x, y, 540) : NO;
    },
    // stones: on the rock scarps, round the spawn, on the hilltops, and a few in the fields
    scarp: (li, x, y) => (open(li) && aux2[li] > 0.2 ? aux2[li] + 0.3 * grain(sct, x, y, 560) : NO),
    spawnRock: (li, x, y) => {
      const sp = Math.hypot(x - SPAWN_X, y - SPAWN_Y);
      return open(li) && sp >= 3 && sp <= 15 ? grain(sct, x, y, 560) : NO;
    },
    hill: (li, x, y) => (open(li) && el[li] === 3 ? grain(sct, x, y, 560) : NO),
    field: (li, x, y) => (open(li) ? grain(sct, x, y, 560) : NO),
    // Dunes boulders: scree aprons round the rocks first, then rocky reg flats and the rock tops
    outcrop: (li, x, y) => {
      if (!open(li)) return NO;
      const mv = aux2[li], reg = ramp(elevN(x / 14 + 2380, y / 14 + 2380), 0.25, 0.55) * (el[li] === 1 ? 1 : 0.3);
      return (mv > 0.08 ? 0.75 : ramp(mv, -0.45, -0.05)) + 0.7 * reg + 0.4 * grain(sct, x, y, 600);
    },
    pebble: (li, x, y) => (open(li)
      ? 0.6 * ramp(aux2[li], -0.6, -0.1) + 0.4 * ramp(elevN(x / 14 + 2380, y / 14 + 2380), 0.15, 0.55) + 0.45 * grain(sct, x, y, 620) + 0.25 * (1 - aux1[li]) : NO),
    crevasse: (li, x, y) => (open(li) && el[li] >= 2
      ? 0.8 * ramp(elevN(x / 11 + 2100, y / 11 + 2100), 0.05, 0.5) + (el[li] === 3 ? 0.3 : 0) + 0.15 * sct(x / 4 + 660, y / 4 + 660) + 0.3 * grain(sct, x, y, 660) : NO),
    foot: (li, x, y) => {
      if (!open(li)) return NO;
      let foot = 0;
      for (const o of [-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1, -2 * w, 2 * w, -2, 2])
        if (land[li + o] && el[li + o] - el[li] > foot) foot = el[li + o] - el[li];
      return 0.5 * foot + 0.4 * grain(sct, x, y, 680);
    },
    scree: (li, x, y) => (open(li) ? grain(sct, x, y, 700) + (el[li] === 1 ? 0.2 : 0) : NO),
    reed: (li, x, y) => {
      if (!open(li) || el[li] !== 1) return NO;
      const dLt = dL[li] / 3;
      // reed beds: patches along the banks (patch noise), a few on the mudflats, irregular within
      return (dLt <= 2.5 ? 1.2 * ramp(sct(x / 6 + 730, y / 6 + 730), -0.15, 0.3) : 0)
        + (dO[li] <= 6 ? 0.45 * ramp(sct(x / 7 + 735, y / 7 + 735), 0, 0.4) : 0) + 0.3 * grain(sct, x, y, 720);
    },
    bog: (li, x, y) => (open(li)
      ? 0.9 * ramp(elevN(x / 10 + 2400, y / 10 + 2400), 0.15, 0.55) + 0.15 * sct(x / 4 + 740, y / 4 + 740) + 0.3 * grain(sct, x, y, 740) - (el[li] >= 2 ? 0.3 : 0) : NO),
  };
  for (const [kind, count, scorer] of NODE_QUOTAS[k]) {
    if (scorer === 'edge') spaceGladeTrees(nodeK);   // all trees are down: thin the spawn glade's edge
    if (scorer === 'lone' || scorer === 'edge') {   // distance to the trees placed so far
      for (let li = 0; li < B.n; li++) C.mask[li] = land[li] && nodeK[gi(li)] === NODE.TREE + 1 ? 1 : 0;
      chamfer(B, C.mask, dT);
    }
    if (ROCK_KIND.has(kind)) placeRocks(B, C, count, S[scorer], kind, kind === NODE.BOULDER);
    else for (const li of pickTop(B, C, count, S[scorer])) nodeK[gi(li)] = kind + 1;
  }

  // decor: the island's prop on the best spots of its own kind of ground; never on nodes, near a
  // POI, in a clearing, or within 20 of the spawn
  const decorScore = (li, x, y) => {
    if (!open(li) || Math.hypot(x - SPAWN_X, y - SPAWN_Y) < 20) return NO;
    const ds = 0.6 * sct(x / 3 + 400, y / 3 + 400) + 0.4 * grain(sct, x, y, 400), e = el[li];
    if (k === 0) return aux1[li] > 0.45 ? ds : NO;                 // glow mushrooms in the woods' shade
    if (k === 1) return e <= 2 ? ds + 0.4 * (1 - aux1[li]) : NO;   // ruined pillars out on the pans
    if (k === 2) return e >= 2 ? ds : NO;                          // ice crystals on the heights
    return e <= 2 ? ds + (dL[li] <= 12 ? 0.3 : 0) : NO;            // bone totems by the pools
  };
  for (const li of pickTop(B, C, DECOR_QUOTA[k], decorScore)) decorK[gi(li)] = k + 1;

  // tileVis: each biome's variant tile in coherent patches where it belongs
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    const lx = li % w, x = B.x0 + lx, y = B.y0 + (li - lx) / w, e = el[li];
    let vis = false;
    if (k === 0) vis = aux1[li] < 0.25 && dW[li] >= 6 && 0.7 * sct(x / 11 + 580, y / 11 + 580) + 0.3 * sct(x / 3 + 590, y / 3 + 590) > 0.22;   // flower meadows
    else if (k === 1) vis = e === 1 && aux1[li] < 0.15 && dW[li] >= 6 && sct(x / 6 + 640, y / 6 + 640) > -0.45;   // cracked pans
    else if (k === 2) vis = (e === 3 && elevN(x / 16 + 2200, y / 16 + 2200) > -0.15)
      || (e === 1 && dO[li] <= 9 && elevN(x / 22 + 2300, y / 22 + 2300) > 0.3);   // glaciers, ice shelves
    else vis = e >= 2 ? sct(x / 5 + 760, y / 5 + 760) > -0.5 : H[li] > P.t1 - 0.05 && sct(x / 5 + 760, y / 5 + 760) > 0;   // moss on hummocks
    tileVis[gi(li)] = vis ? 1 : 0;
  }
}

// --- 2. the Core ----------------------------------------------------------------------------------
// An irregular blighted crater rim (radius 12.6..17.8), always level 1; ash near its edge; a few
// lava vents outside the temple clearing.
function coreRegion(C) {
  const B = makeBox(CORE[0], CORE[1], 20);
  const { land, carved, val, lab, queue } = C;
  const { sct, coast, shape } = C.N;
  for (let li = 0; li < B.n; li++) {
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w, dx = x - CORE[0], dy = y - CORE[1];
    const d = Math.hypot(dx, dy), ux = d > 1e-9 ? dx / d : 1, uy = d > 1e-9 ? dy / d : 0;
    const R = Math.min(17.8, Math.max(12.6, 15 + 2.2 * shape(ux * 0.7 + 500, uy * 0.7 + 500) + 1.1 * coast(x / 4.5 + 600, y / 4.5 + 600)));
    val[li] = R - d;   // how far inside the rim
    land[li] = d < R ? 1 : 0; carved[li] = 0;
  }
  // one landmass, no holes
  const sizes = components(B, land, 1, lab, queue);
  let main = 1;
  for (let l = 2; l < sizes.length; l++) if (sizes[l] > sizes[main]) main = l;
  for (let li = 0; li < B.n; li++) if (land[li] && lab[li] !== main) land[li] = 0;
  fillSmallWater(B, C, B.n);
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w, i = y * SIZE + x;
    C.tiles[i] = T.BLIGHT; C.elev[i] = 1;
    const a = sct(x / 3 + 800, y / 3 + 800);
    C.tileVis[i] = (val[li] < 2.6 ? a > -0.35 : a > 0.55) ? 1 : 0;
  }
  // lava vents: best-scoring open tiles at least 4 apart
  const ranked = pickTop(B, C, B.n, (li, x, y) =>
    (land[li] && val[li] > 1.5 && Math.hypot(x - CORE[0], y - CORE[1]) >= 7 && !nearPOI(x, y) ? sct(x / 2.5 + 850, y / 2.5 + 850) : NO));
  const vents = [];
  for (const li of ranked) {
    if (vents.length >= LAVA_VENTS) break;
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w;
    if (vents.some(([vx, vy]) => Math.hypot(vx - x, vy - y) < 4)) continue;
    vents.push([x, y]);
    C.decorK[y * SIZE + x] = DECOR_KEYS.indexOf('lava_vent') + 1;
  }
}

// --- 3. minor isles ---------------------------------------------------------------------------------
// Each one a single small landmass of its kind's tile: warped, lobed coast within r + 5.5 of its
// centre, a domed interior, the old node rule.
function minorPass(m, j, C) {
  const B = makeBox(m.x, m.y, m.r + 8);
  const { land, carved, ocean, val, dO, H, el, lab, queue } = C;
  const { elev: elevN, veg, sct, coast, warp, shape } = C.N;
  const tile = MINOR_KIND_TILE[m.kind], st = MINOR_STYLE[m.kind];
  let cnt = 0;
  for (let li = 0; li < B.n; li++) {
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w, ox = x - m.x, oy = y - m.y;
    land[li] = 0; carved[li] = 0;
    const d = Math.hypot(ox, oy);
    if (d > m.r + 5.5) { val[li] = -9; continue; }
    const wx = ox + 2.5 * warp(x / 11 + 900, y / 11 + 900), wy = oy + 2.5 * warp(x / 11 + 950, y / 11 + 950);
    const q = Math.hypot(wx, wy), ux = q > 1e-9 ? wx / q : 1, uy = q > 1e-9 ? wy / q : 0;
    let v = 1 - q / (m.r * (1 + 0.18 * shape(ux * 0.6 + 300 + j * 17, uy * 0.6 + 300)))
      + 0.12 * coast(x / 6 + 700, y / 6 + 700) + 0.05 * coast(x / 3 + 750, y / 3 + 750);
    if (d > m.r + 2) v -= (d - m.r - 2) * 0.08;
    val[li] = v;
    C.sortBuf[cnt++] = v;
  }
  const area = Math.floor(3.14159 * (m.r + 0.45) * (m.r + 0.45) + 0.5);
  const th = C.sortBuf.subarray(0, cnt).sort()[cnt - area];
  for (let li = 0; li < B.n; li++) if (val[li] >= th) land[li] = 1;
  // a single landmass with no lakes
  const sizes = components(B, land, 1, lab, queue);
  let main = 1;
  for (let l = 2; l < sizes.length; l++) if (sizes[l] > sizes[main]) main = l;
  for (let li = 0; li < B.n; li++) if (land[li] && lab[li] !== main) land[li] = 0;
  fillSmallWater(B, C, B.n);
  markOcean(B, C);
  chamfer(B, ocean, dO);
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) { el[li] = 0; continue; }
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w;
    H[li] = ramp(dO[li] / 3 + 1.2 * elevN(x / 5 + 2650, y / 5 + 2650), 1.6, st.shore + 2) * (0.35 + 0.65 * (0.5 + 0.5 * elevN(x / 9 + 2600, y / 9 + 2600)));
    el[li] = H[li] > st.t2 ? 3 : H[li] > st.t1 ? 2 : 1;
  }
  smoothLevels(B, C, 8);
  smoothLevels(B, C, 8);
  smoothLevels(B, C, 4);
  ensureClimbable(B, C);
  settleTerraces(B, C);
  for (let round = 0; round < 3 && levelStubs(B, C); round++) settleTerraces(B, C);
  for (let li = 0; li < B.n; li++) {
    if (!land[li]) continue;
    const lx = li % B.w, x = B.x0 + lx, y = B.y0 + (li - lx) / B.w, i = y * SIZE + x;
    C.tiles[i] = tile; C.elev[i] = tile === T.BLIGHT ? 1 : el[li];
    C.tileVis[i] = sct(x / 7 + 55, y / 7 + 55) > 0.45 ? 1 : 0;
    if (tile === T.BLIGHT || nearPOI(x, y)) continue;
    // minor island nodes: TREE (driftwood only), BUSH, STONE — no crystal/boulder/starmetal
    const v = veg(x / 6, y / 6), s = sct(x / 3, y / 3);
    if (m.kind === 'driftwood' && v > 0.62) C.nodeK[i] = NODE.TREE + 1;
    else if (s > 0.80) C.nodeK[i] = NODE.BUSH + 1;
    else if (s < -0.85) C.nodeK[i] = NODE.STONE + 1;
  }
}

export function genWorld(seed) {
  const N = {
    elev: createNoise2D(alea(seed + 'e')),    // landforms: hills, dunes, tiers, hummocks, pools
    veg: createNoise2D(alea(seed + 't')),     // forests, minor-isle trees
    sct: createNoise2D(alea(seed + 's')),     // scatter: veins, bergs, starmetal, node jitter, tileVis, decor
    coast: createNoise2D(alea(seed + 'c')),   // coastline and lake-edge detail
    warp: createNoise2D(alea(seed + 'w')),    // domain warp, offshore islets
    shape: createNoise2D(alea(seed + 'o')),   // island layout: lobes, feature rotation, lake jitter
  };
  const NN = SIZE * SIZE;
  const tiles = new Uint8Array(NN).fill(T.WATER);
  const elev = new Uint8Array(NN);       // 0 water, 1 plain, 2 hill, 3 peak
  const veins = new Uint8Array(NN);      // underground: 0 none, 1 iron, 2 diamond
  const waterTemp = new Uint8Array(NN);  // 0 temperate, 1 freezing, 2 hot
  const tileVis = new Uint8Array(NN);    // 1 = the tile type's variant (flower_grass, cracked_sand, ...)
  const nodeK = new Uint8Array(NN);      // 0 none, else NODE kind + 1
  const decorK = new Uint8Array(NN);     // 0 none, else DECOR_KEYS index + 1

  // per-box scratch, sized for a major island's box (the largest)
  const BN = (2 * MAJOR_HALF + 1) * (2 * MAJOR_HALF + 1);
  const C = {
    N, tiles, elev, tileVis, nodeK, decorK,
    land: new Uint8Array(BN), carved: new Uint8Array(BN), ocean: new Uint8Array(BN), mask: new Uint8Array(BN),
    noNode: new Uint8Array(BN), el: new Uint8Array(BN), el2: new Uint8Array(BN), reach: new Uint8Array(BN),
    lab: new Int32Array(BN), queue: new Int32Array(BN), parent: new Int32Array(BN),
    dO: new Int32Array(BN), dW: new Int32Array(BN), dL: new Int32Array(BN), dT: new Int32Array(BN),
    val: new Float64Array(BN), px: new Float64Array(BN), py: new Float64Array(BN), H: new Float64Array(BN),
    aux1: new Float64Array(BN), aux2: new Float64Array(BN), aux3: new Float64Array(BN), sortBuf: new Float64Array(BN),
    cand: new Int32Array(BN), score: new Float64Array(BN), order: new Int32Array(BN),
    corner: new Int32Array((2 * MAJOR_HALF + 2) * (2 * MAJOR_HALF + 2)),   // levelStubs: tile corners
  };

  // ---- 1-3. islands ----
  for (let k = 0; k < 4; k++) islandPass(k, C);
  coreRegion(C);
  MINOR_ISLES.forEach((m, j) => minorPass(m, j, C));

  // ---- 4. stamp solid ground under landmark footprints (seed-independent guarantee) ----
  for (const m of MOUNTAINS) {
    const isCore = Math.hypot(m.x - CORE[0], m.y - CORE[1]) < 20;
    let k = 0, bd = Infinity;
    for (let q = 0; q < 4; q++) {
      const d = Math.hypot(m.x - ISLES[q][0], m.y - ISLES[q][1]);
      if (d < bd) { bd = d; k = q; }
    }
    const ground = isCore ? T.BLIGHT : ISLE_T[k];
    for (let dy = -1; dy <= 1; dy++)
      for (let dx = -1; dx <= 1; dx++) {
        const i = (m.y + dy) * SIZE + (m.x + dx);
        if (tiles[i] === T.WATER) { tiles[i] = ground; elev[i] = 1; nodeK[i] = 0; }
      }
  }

  // ---- 5. derived layers, now every tile is final ----
  const sct = N.sct;
  for (let y = 0; y < SIZE; y++)
    for (let x = 0; x < SIZE; x++) {
      const i = y * SIZE + x;
      const vm = sct(x / 5 + 250, y / 5 + 250);
      veins[i] = vm > 0.68 ? 1 : vm < -0.74 ? 2 : 0;
      if (tiles[i] !== T.WATER) continue;
      if (Math.hypot(x - ISLES[2][0], y - ISLES[2][1]) < ISLE_R + 180) waterTemp[i] = 1;   // freezing
      else if (Math.hypot(x - CORE[0], y - CORE[1]) < 125) waterTemp[i] = 2;               // hot
    }
  // iceberg ring around the Frozen Spire — lethal to wooden hulls (scanned in tile order)
  const bergs = new Set();
  const BR = ISLE_R + 135;
  for (let y = Math.max(0, ISLES[2][1] - BR); y <= Math.min(SIZE - 1, ISLES[2][1] + BR); y++)
    for (let x = Math.max(0, ISLES[2][0] - BR); x <= Math.min(SIZE - 1, ISLES[2][0] + BR); x++) {
      const i = y * SIZE + x, dSpire = Math.hypot(x - ISLES[2][0], y - ISLES[2][1]);
      if (tiles[i] === T.WATER && dSpire > ISLE_R + 8 && dSpire < ISLE_R + 135 && sct(x / 2 + 900, y / 2 + 900) > 0.88) bergs.add(i);
    }

  // ---- 6. nodes and decor, in tile-index order ----
  const nodes = new Map(); // tile index -> NODE kind
  const decor = new Map(); // tile index -> string prop key
  for (let i = 0; i < NN; i++) {
    if (nodeK[i]) nodes.set(i, nodeK[i] - 1);
    if (decorK[i]) decor.set(i, DECOR_KEYS[decorK[i] - 1]);
  }

  return { tiles, elev, veins, nodes, bergs, waterTemp, tileVis, decor };
}

export function nearestLand(world, x, y) {
  for (let r = 1; r < 130; r++)
    for (let dy = -r; dy <= r; dy++)
      for (let dx = -r; dx <= r; dx++) {
        const nx = Math.round(x + dx), ny = Math.round(y + dy);
        if (nx < 0 || ny < 0 || nx >= SIZE || ny >= SIZE) continue;
        if (world.tiles[ny * SIZE + nx] !== T.WATER) return [nx, ny];
      }
  return [x, y];
}

// mining is possible under the Woods, Dunes and Spire — never the Marsh, water or the Core
export const DIGGABLE = (world, i) =>
  world.tiles[i] === T.GRASS || world.tiles[i] === T.SAND || world.tiles[i] === T.SNOW;

export function findSpawn(world) {
  const [sx, sy] = [ISLES[0][0] - 12, ISLES[0][1] - 12]; // (168, 168)
  for (let r = 0; r < 50; r++)
    for (let dy = -r; dy <= r; dy++)
      for (let dx = -r; dx <= r; dx++) {
        const x = sx + dx, y = sy + dy;
        if (x < 0 || y < 0 || x >= SIZE || y >= SIZE) continue;
        const i = y * SIZE + x;
        if (world.tiles[i] === T.GRASS && !world.nodes.has(i)) return [x, y];
      }
  return [sx, sy];
}

// Deterministic fixed-order ring search: increasing radius, fixed dx/dy iteration order,
// bounded max radius. Never Math.random or Map/Set iteration order — client and server
// must both derive the same answer independently from the same seed.
export function findNearestValidTile(cx, cy, pred, maxR = 40) {
  for (let r = 0; r <= maxR; r++)
    for (let dy = -r; dy <= r; dy++)
      for (let dx = -r; dx <= r; dx++) {
        if (Math.max(Math.abs(dx), Math.abs(dy)) !== r) continue;
        const x = cx + dx, y = cy + dy;
        if (x < 0 || y < 0 || x >= SIZE || y >= SIZE) continue;
        if (pred(x, y, y * SIZE + x)) return [x, y];
      }
  return null;
}

// Deterministic medic placement — same seed/world always yields the same two spots.
export function findMedicSpawns(world) {
  return MEDIC_ISLANDS.map(({ islandId, sprite }) => {
    const island = MAJOR_ISLANDS.find((mi) => mi.id === islandId);
    const [ox, oy] = MEDIC_ANCHOR_OFFSETS[islandId];
    const [cx, cy] = island.center;
    const found = findNearestValidTile(cx + ox, cy + oy, (tx, ty, i) =>
      world.tiles[i] === island.tile &&
      !world.nodes.has(i) &&
      !LANDMARK_BLOCK.has(i) &&
      Math.hypot(tx - cx, ty - cy) > 5
    );
    if (!found) throw new Error(`findMedicSpawns: no valid tile for medic-${islandId}`);
    const [x, y] = found;
    // decorative hut two tiles behind the medic, so the medic always stands outside its door
    const hut = findNearestValidTile(x, y - 2, (tx, ty, i) =>
      world.tiles[i] === island.tile &&
      !world.nodes.has(i) &&
      !LANDMARK_BLOCK.has(i) &&
      Math.hypot(tx - x, ty - y) >= 2
    , 6);
    if (!hut) throw new Error(`findMedicSpawns: no valid hut tile for medic-${islandId}`);
    return {
      id: `medic-${islandId}`, islandId, sprite, x, y,
      hutSprite: islandId === 'spire' ? 'medic_hut_snow' : 'medic_hut',
      hutX: hut[0], hutY: hut[1],
    };
  });
}

// Tiles the medic hut occupies — blocked for movement and building on both sides.
export const medicBlockTiles = (medics) => new Set(medics.map((m) => m.hutY * SIZE + m.hutX));
