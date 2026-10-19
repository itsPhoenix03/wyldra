// The title screen's backdrop: a scene built from one biome's own tiles (and, for the
// Woods, its trees and plants), raised into terraces from a height map. Pure data, no DOM:
// tools/atlas/build-atlases.mjs bakes the active backdrop into one PNG
// (assets/atlas/menu-<id>.<hash>.png), so the menu decodes a single image off the main
// thread instead of laying out hundreds of tiles. Switch biome with BACKDROP; index.html
// styles the sky for it (#menu[data-biome]).
//
// Everything is deterministic (a fixed hash, no Math.random): the same scene every build.

export type BackdropId = "woods" | "spire";
export const BACKDROP: BackdropId = "woods";

export const TILE_W = 64; // the game's tile: a 64 x 32 diamond on an 8 px side
export const TILE_H = 40;
export const LAYER = 8; // one tile's thickness: a level is two layers, stacked
export const LAYERS_PER_LEVEL = 2;

export interface Cell {
  x: number;
  y: number;
  h: number; // level: 0 is the sea
  top: string; // the tile on top
  side: string; // the tile the column under it is stacked from
  prop?: [string, number, number]; // a plant standing on it: sprite, x / y nudge in px
}

/**
 * How the scene is framed: an island in a round sea (its edge faded out by CSS), or land
 * running off every edge — the picture is a `width` x `height` window into the grid,
 * `top` px below the grid's back corner, so no edge of the grid shows.
 */
export type Frame = { kind: "island"; cx: number; cy: number; r: number } | { kind: "land"; width: number; height: number; top: number };

export interface Backdrop {
  grid: number; // cells along each side of the iso grid
  tiles: string[]; // every tile it uses (assets/tiles/<name>.svg)
  props: Record<string, [number, number]>; // every plant it uses (assets/sprites/<name>.svg), drawn w x h
  sea: string; // the sea tile; an island's is drawn see-through over `deep`
  deep: string;
  frame: Frame;
  cells(): Cell[]; // in painter's order, back to front
}

function hash(x: number, y: number): number {
  let h = (Math.imul(x, 374761393) + Math.imul(y, 668265263)) >>> 0;
  h = Math.imul(h ^ (h >>> 13), 1274126177) >>> 0;
  return ((h ^ (h >>> 16)) >>> 0) / 4294967296;
}

/** Smooth value noise in [0, 1) at a feature size of `s` cells. */
function noise(x: number, y: number, s: number): number {
  const fx = x / s, fy = y / s, x0 = Math.floor(fx), y0 = Math.floor(fy);
  const sm = (t: number) => t * t * (3 - 2 * t);
  const u = sm(fx - x0), v = sm(fy - y0);
  const a = hash(x0, y0), b = hash(x0 + 1, y0), c = hash(x0, y0 + 1), d = hash(x0 + 1, y0 + 1);
  return a + (b - a) * u + (c - a) * v + (a - b - c + d) * u * v;
}

/** A rounded rise centred on (px, py): `top` levels high, falling to nothing at radius r. */
const rise = (x: number, y: number, px: number, py: number, r: number, top: number, shape = 1.4) =>
  Math.max(0, 1 - Math.hypot(x - px, y - py) / r) ** shape * top;

/** Every cell of an n x n grid, back to front, through `cell` (null skips it). */
function sweep(n: number, cell: (x: number, y: number) => Cell | null): Cell[] {
  const cells: Cell[] = [];
  for (let s = 0; s <= 2 * (n - 1); s++) {
    for (let x = Math.max(0, s - n + 1); x <= Math.min(n - 1, s); x++) {
      const c = cell(x, s - x);
      if (c) cells.push(c);
    }
  }
  return cells;
}

// The Whispering Woods: rolling terraced meadows running off every edge, soil showing where
// the land steps up, a river winding down the valley to a coast in the near-left corner,
// and the game's own trees and plants: forests on the hills, bushes and glowcaps in the grass.
const WOODS: Backdrop = {
  grid: 64,
  tiles: ["grass", "flower_grass", "water"],
  props: { pine_tree: [56, 72], tree: [48, 64], autumn_tree: [58, 68], bush: [36, 28], glow_mushroom: [26, 25] },
  sea: "water",
  deep: "#0b2236",
  frame: { kind: "land", width: 1700, height: 1000, top: 430 },
  cells: () =>
    sweep(64, (x, y) => {
      const s = x + y; // depth: the back of the scene is small s
      const u = x - y; // across: left is negative
      // the coast in the near-right corner, clear of the menu
      if (x - y - 17 + (noise(x, y, 6) - 0.5) * 9 + (s - 72) * 0.16 > 0) return { x, y, h: 0, top: "water", side: "water" };
      // the river, meandering from the back of the valley down to that coast; the land
      // falls toward it in steps, so it runs in a valley rather than a canyon
      const bed = 9 * Math.sin(s / 11) + (s - 34) * 0.42;
      const off = Math.abs(u - bed);
      const level = Math.max(0, Math.floor((64 - s) * 0.045));
      // the land: rolling hills, higher toward the back, a broad hill behind the valley
      let land = 2 + (noise(x, y, 8) - 0.5) * 5 + (noise(x + 33, y + 11, 3.5) - 0.5) * 2 +
        Math.max(0, 60 - s) * 0.07 + rise(x, y, 38, 20, 10, 5) + rise(x, y, 22, 32, 7, 3);
      if (s > 14) land = Math.min(land, level + 1 + Math.max(0, off - 1.6) * 0.75);
      if (s > 14 && off < 1.6) return { x, y, h: level, top: "water", side: "grass" };
      const h = Math.max(1, Math.floor(land));
      const top = noise(x + 90, y + 17, 2.6) > 0.62 ? "flower_grass" : "grass";
      const c: Cell = { x, y, h, top, side: "grass" };
      // plants: forests where the forest noise is high, a scatter of bushes and glowcaps elsewhere
      const r = hash(x * 7 + 3, y * 3 + 1);
      const nudge = (k: string): [string, number, number] => [k, (hash(x + 11, y * 13) - 0.5) * 14, (hash(x * 5, y + 9) - 0.5) * 6];
      const v = hash(x * 11, y * 17 + 5);
      const kind = v < 0.45 ? "pine_tree" : v < 0.8 ? "tree" : "autumn_tree";
      if (noise(x + 300, y + 50, 6) > 0.53 && r < 0.64) c.prop = nudge(kind);
      else if (r < 0.025) c.prop = nudge(kind); // a lone tree out in the meadow
      else if (r < 0.07) c.prop = nudge("bush");
      else if (r > 0.985) c.prop = nudge("glow_mushroom");
      return c;
    }),
};

// The Frozen Spire: snow terraces and packed ice round a tall ice spire, floes in a
// freezing sea that rings it.
const SPIRE: Backdrop = {
  grid: 36,
  tiles: ["snow", "packed_ice", "water_freezing"],
  props: {},
  sea: "water_freezing",
  deep: "#0b1b34",
  frame: { kind: "island", cx: 18, cy: 17, r: 17.5 },
  cells: () =>
    sweep(36, (x, y) => {
      const d = Math.hypot(x - 18, y - 17);
      if (d > 17.5) return null;
      const coast = 4.4 - d * 0.4 + (noise(x, y, 6) - 0.5) * 2.8 + (noise(x + 40, y, 2.5) - 0.5) * 0.9;
      const spire = Math.max(0, 1 - d / 6.5) ** 1.5 * 22;
      const lift = coast + spire + rise(x, y, 12, 20, 3, 7) + rise(x, y, 23, 12, 3.2, 8);
      if (lift < 0.6) {
        const floe = d > 9 && d < 15 && hash(x * 3 + 1, y * 5 + 2) < 0.09;
        const t = floe ? "packed_ice" : "water_freezing";
        return { x, y, h: 0, top: t, side: t };
      }
      const h = Math.max(1, Math.floor(lift));
      const t = h >= 12 || noise(x + 90, y + 17, 3) > 0.72 ? "packed_ice" : "snow";
      return { x, y, h, top: t, side: t };
    }),
};

export const BACKDROPS: Record<BackdropId, Backdrop> = { woods: WOODS, spire: SPIRE };
