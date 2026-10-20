import Phaser from "phaser";
import { SIZE, T, MOUNTAINS } from "../shared/world.js";
import { UNLOADED, CHUNK, MAX_ELEV, type TileStore } from "./tiles.ts";

/* ────────────────────────────────────────────────────────────────────────────
 * Tile mountains on screen: terrain that hides what stands behind it.
 *
 * The terrain is baked into chunk RenderTextures at depth -1, under every
 * sprite (main.ts drawChunk), which is fine for hills of a level or three: a
 * keeper behind one shows over it, as they always have. A tile mountain
 * (gameserver/world/mountains.go, the tileMountains feature) stands up to 31
 * levels — 310 px — over the ground, and a keeper walking behind it must go
 * out of sight. So, for each mountain near the camera, every tall tile is also
 * baked into a strip: one RenderTexture per iso row (x + y) of the mountain, at
 * a depth just below the sprites standing on that row and above those on the
 * row behind. Painted in that order the mountain covers whatever is behind it
 * and nothing in front.
 *
 * That only works if the sprites around the mountain sort by where they stand
 * on the ground plane, not by their screen y: lifted by twenty levels, a keeper on
 * the summit has a smaller screen y than one on the ground behind the mountain,
 * and would be hidden by the very ground under their feet. Inside a mountain's
 * region depthAt() therefore gives ground-plane depth — row band first, then
 * position within the row — and everywhere else main.ts keeps the lifted
 * screen-y depth it has always used. So nothing changes away from the
 * mountains (folk.ts, which sorts its own sprites, included), and with
 * tileMountains off there are no regions at all.
 *
 * A strip is drawn over the chunk texture, which already holds the same ground
 * composited correctly. Its lower cliff bands could therefore paint over a
 * lower tile in front of it that the chunk drew later: so where the tile's
 * front neighbour is not itself in a strip, the levels that neighbour covers
 * are left out of the strip (each half of the tile against its own neighbour)
 * and the chunk texture shows through there. Those low levels never hide a
 * standing sprite anyway.
 *
 * A stack is one tile per level, 10 px apart, and all that shows of a level under
 * the top is the 10 px band under its diamond. Stacking the top tile there repeats
 * its skirt once per level, which reads as brickwork; so a mountain tile's levels
 * under its top are drawn as rock face instead (mountainFaces / faceAt below, the
 * art from tools/art/mountain-faces.mjs), here and in main.ts drawChunk and
 * repaintTileBox alike, so the strips and the chunk textures stay identical.
 *
 * Main-thread cost: regions are found by scanning one 69x69 window when a chunk
 * near a mountain site lands; strips are baked incrementally, a bounded number
 * of tile levels per frame, only for the mountain nearest the camera, and
 * dropped again when it is far. Per frame there is a box test per region in
 * depthAt and a handful of tile reads in coverAt.
 * ──────────────────────────────────────────────────────────────────────────── */

const TW = 64,
  TH = 32,
  LV = 10; // px per elevation level
const SCAN = 34; // tiles round a MOUNTAINS site the region scan looks
const PAD = 4; // tiles of foothill round the rock tiles that still go in strips
// tiles behind the strip box (on each axis) that also sort by ground plane; a sprite further
// back keeps its lifted depth, which is under every strip anyway
const BACK = 10;
const FRONT = 3;
// tile halves drawn into strips per frame (a level of a stack is one or two). It was 140
// stacks of at most 12 levels; stacks of 32 would near triple that, so the budget counts draws.
const BAKE_DRAWS_PER_FRAME = 2600;
const KEEP_PX = 2600; // a region's strips are dropped once the camera is this far off

/* ── rock faces ──────────────────────────────────────────────────────────────
 * Each mountain kind has four face bands and a strata overlay (src/assets.ts):
 * mtn_<isle>_<rock|snow>_face, _face2, _face3, _face4 are the four 10 px bands of
 * one cliff that wraps every four levels, so band j over band j + 1 is seamless;
 * a level shows band (phase - level) mod 4, the phase hashed from the tile, so
 * neighbouring tiles show different rock. _strata is a darker bedding plane on a
 * band's top edge, drawn on one level in each four (the same levels everywhere, so
 * a stratum runs right round the mountain); the phase re-hashes above each one, so
 * a tall face never shows the four-level repeat. Rock, ledge and trail tiles share
 * their island's rock face, snow and snow ledge its snow face. */

const mix32 = (h: number) => {
  h ^= h >>> 16;
  h = Math.imul(h, 0x7feb352d);
  h ^= h >>> 15;
  h = Math.imul(h, 0x846ca68b);
  h ^= h >>> 16;
  return h >>> 0;
};
/** Which level (1..3) of the four 4k..4k+3 carries a stratum. */
const strataAt = (k: number) => 1 + (mix32(Math.imul(k, 0x9e3779b1) ^ 0x51ed27) % 3);
/** Does level l (0 = a stack's lowest) carry a stratum on its top edge? */
export const stratumAt = (l: number) => (l & 3) === strataAt(l >> 2);

/** The face band for level l of the stack at world tile (x, y). */
export function faceAt(faces: readonly string[], x: number, y: number, l: number): string {
  const k = l >> 2,
    seg = k + ((l & 3) > strataAt(k) ? 1 : 0); // which run between strata the level is in
  const phase = mix32(Math.imul(x, 0x27d4eb2d) ^ Math.imul(y, 0x165667b1) ^ Math.imul(seg, 0x9e3779b1));
  return faces[(phase - l) & 3];
}

const faceSets = new Map<string, readonly string[]>();
/**
 * The face textures under a tile whose top is `key` — [band 0..3, strata] — or null
 * for every tile that is not a tile mountain's, so ordinary ground (and everything
 * with tileMountains off) stacks exactly as it always has.
 */
export function mountainFaces(tm: Phaser.Textures.TextureManager, key: string): readonly string[] | null {
  if (!key.startsWith("mtn_")) return null;
  const known = faceSets.get(key);
  if (known) return known;
  const [, isle, part] = key.split("_");
  const b = `mtn_${isle}_${part === "snow" || part === "snowledge" ? "snow" : "rock"}_`;
  const set = [b + "face", b + "face2", b + "face3", b + "face4", b + "strata"];
  if (!set.every((k) => tm.exists(k))) return null; // not in the atlas: the old stacking
  faceSets.set(key, set);
  return set;
}

/** What the mountain view needs from the game scene. */
export interface PeakHost extends Phaser.Scene {
  world: TileStore;
  offX: number;
  z: number;
  tileKey(i: number): string | null;
  lipsAt(x: number, y: number): string[];
  /** Sprites inside this tile box must take a new depth (a region came, went or moved). */
  redepthBox(x0: number, y0: number, x1: number, y1: number): void;
}

interface Region {
  site: number; // MOUNTAINS index
  // the strip box: rock tiles plus PAD of foothill, and inside it which tiles are tall
  x0: number;
  y0: number;
  x1: number;
  y1: number;
  tall: Uint8Array;
  // the box that sorts by ground plane
  dx0: number;
  dy0: number;
  dx1: number;
  dy1: number;
  base: number; // the ground level it rises from
  peak: number; // its tallest stack, levels
  lift: number; // px, so the ground-plane depths meet the lifted ones at its edge
  strips: Phaser.GameObjects.RenderTexture[];
  bakeRow: number; // next row (x + y) to bake; > y1 + x1 when done; NaN when not started
  centreX: number; // screen px, for the near / far test
  centreY: number;
}

export class MountainView {
  private regions: Region[] = [];
  private pending = new Set<number>(); // MOUNTAINS indices to (re)scan
  private on = false;
  private halves = new Set<string>(); // texture keys that have L/R half frames

  constructor(private s: PeakHost) {}

  get enabled() {
    return this.on;
  }

  /** tileMountains switched. Off drops every region (and its strips). */
  setEnabled(on: boolean) {
    if (on === this.on) return;
    this.on = on;
    if (!on) {
      for (const r of this.regions) this.dropRegion(r);
      this.regions = [];
      this.pending.clear();
    } else for (let k = 0; k < MOUNTAINS.length; k++) this.pending.add(k);
  }

  /** A chunk landed: rescan the mountains whose window it touches. */
  chunkApplied(cx: number, cy: number) {
    if (!this.on) return;
    const x0 = cx * CHUNK,
      y0 = cy * CHUNK;
    MOUNTAINS.forEach((m: { x: number; y: number }, k: number) => {
      if (m.x + SCAN >= x0 && m.x - SCAN < x0 + CHUNK && m.y + SCAN >= y0 && m.y - SCAN < y0 + CHUNK) this.pending.add(k);
    });
  }

  /** A tile's look changed (mud, blight): a tall one in a strip needs its region rebaked. */
  tileChanged(i: number) {
    const x = i % SIZE,
      y = (i / SIZE) | 0;
    for (const r of this.regions)
      if (x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1 && r.tall[(y - r.y0) * (r.x1 - r.x0 + 1) + x - r.x0]) {
        this.clearStrips(r);
        r.bakeRow = NaN;
      }
  }

  /** Underground the mountains are not drawn over anything (the faint surface chunks are enough). */
  setZ(z: number) {
    for (const r of this.regions) for (const st of r.strips) st.setVisible(z === 0);
  }

  destroy() {
    for (const r of this.regions) this.dropRegion(r);
    this.regions = [];
  }

  /** The region whose ground-plane box holds (x, y), or null. */
  private regionAt(x: number, y: number): Region | null {
    for (const r of this.regions) if (x >= r.dx0 && x < r.dx1 + 1 && y >= r.dy0 && y < r.dy1 + 1) return r;
    return null;
  }

  /** Is (x, y) inside a mountain's ground-plane box? */
  inRegion(x: number, y: number) {
    return this.regionAt(x, y) !== null;
  }

  /**
   * Ground-plane sort depth for something standing at world (x, y) and drawn `off`
   * px below the lifted top vertex of its tile (the offsets main.ts has always added
   * to a sprite's depth), or null outside every mountain region. A sprite on row
   * r = floor(x) + floor(y) gets a depth inside (stripDepth(r), stripDepth(r + 1)):
   * after the row's own strip, before the next row's.
   */
  depthAt(x: number, y: number, off: number): number | null {
    const r = this.regionAt(x, y);
    if (!r) return null;
    const row = Math.floor(x) + Math.floor(y);
    const v = 16 * (x + y - row) + off;
    return row * 16 - r.lift + 0.5 + 15 * Math.min(1, Math.max(0, (v + 8) / 56));
  }

  /**
   * How much of a body standing at (x, y) — feet at screen y footY, about 46 px
   * tall — the strips in front of it hide, 0..1. A handful of tile reads: the
   * rows in front, in the body's column and the two either side, only as far
   * ahead as the region's tallest stack could still rise over the feet (about 20
   * rows for a 32-level peak over a keeper at its foot; it was a fixed 9).
   */
  coverAt(x: number, y: number, footY: number): number {
    const r = this.regionAt(x, y);
    if (!r || this.s.z !== 0) return 0;
    const row = Math.floor(x) + Math.floor(y),
      col = x - y;
    const w = r.x1 - r.x0 + 1;
    let cover = 0;
    const last = Math.min(r.x1 + r.y1, Math.floor((footY - 8 + (r.peak - 1) * LV) / (TH / 2)));
    for (let rr = row + 1; rr <= last; rr++) {
      for (const dc of [-1, 0, 1]) {
        let c = Math.round(col) + dc;
        if ((c + rr) & 1) c += dc >= 0 ? 1 : -1; // tiles of row rr have columns of its parity
        const tx = (rr + c) / 2,
          ty = (rr - c) / 2;
        if (tx < r.x0 || tx > r.x1 || ty < r.y0 || ty > r.y1) continue;
        if (!r.tall[(ty - r.y0) * w + tx - r.x0]) continue;
        const ov = 1 - Math.abs(c - col); // horizontal overlap with the body's centre line, tiles
        if (ov <= 0.05) continue;
        const e = Math.max(1, this.s.world.elev[ty * SIZE + tx]);
        const top = rr * (TH / 2) - (e - 1) * LV + 8; // just below the top vertex
        cover = Math.max(cover, Math.min(1, Math.max(0, (footY - top) / 46)) * Math.min(1, ov * 1.6));
      }
    }
    return cover;
  }

  /** Per frame: one pending scan, and a bounded slice of baking for the region nearest the camera. */
  update(cam: Phaser.Geom.Rectangle) {
    if (!this.on) return;
    if (this.pending.size) {
      const [k] = this.pending;
      this.pending.delete(k);
      this.scan(k);
    }
    const cx = cam.centerX,
      cy = cam.centerY;
    let near: Region | null = null,
      nd = Infinity;
    for (const r of this.regions) {
      const d = Math.hypot(r.centreX - cx, r.centreY - cy);
      if (d > KEEP_PX) {
        if (r.strips.length || !Number.isNaN(r.bakeRow)) {
          this.clearStrips(r);
          r.bakeRow = NaN;
        }
      } else if (d < nd) {
        nd = d;
        near = r;
      }
    }
    if (near) this.bake(near, BAKE_DRAWS_PER_FRAME);
  }

  /** Find (or lose, or move) the mountain round MOUNTAINS[k] from the streamed tiles. */
  private scan(k: number) {
    const m = MOUNTAINS[k] as { x: number; y: number };
    const W = this.s.world;
    let x0 = SIZE,
      y0 = SIZE,
      x1 = -1,
      y1 = -1;
    for (let y = Math.max(0, m.y - SCAN); y <= Math.min(SIZE - 1, m.y + SCAN); y++)
      for (let x = Math.max(0, m.x - SCAN); x <= Math.min(SIZE - 1, m.x + SCAN); x++) {
        const i = y * SIZE + x;
        if (W.tiles[i] === UNLOADED || W.tileVis[i] < 2) continue;
        if (x < x0) x0 = x;
        if (x > x1) x1 = x;
        if (y < y0) y0 = y;
        if (y > y1) y1 = y;
      }
    const old = this.regions.find((r) => r.site === k);
    if (old) {
      this.dropRegion(old);
      this.regions = this.regions.filter((r) => r !== old);
      this.s.redepthBox(old.dx0, old.dy0, old.dx1, old.dy1);
    }
    if (x1 < 0) return;
    x0 = Math.max(0, x0 - PAD);
    y0 = Math.max(0, y0 - PAD);
    x1 = Math.min(SIZE - 1, x1 + PAD);
    y1 = Math.min(SIZE - 1, y1 + PAD);
    // the ground it rises from: the median level round the box's edge
    const edge: number[] = [];
    for (let x = x0; x <= x1; x++)
      for (const y of [y0, y1]) {
        const i = y * SIZE + x;
        if (W.tiles[i] !== UNLOADED && W.tiles[i] !== T.WATER) edge.push(W.elev[i]);
      }
    for (let y = y0; y <= y1; y++)
      for (const x of [x0, x1]) {
        const i = y * SIZE + x;
        if (W.tiles[i] !== UNLOADED && W.tiles[i] !== T.WATER) edge.push(W.elev[i]);
      }
    edge.sort((a, b) => a - b);
    const base = Math.max(1, edge.length ? edge[edge.length >> 1] : 1);
    const w = x1 - x0 + 1;
    const tall = new Uint8Array(w * (y1 - y0 + 1));
    let peak = base;
    for (let y = y0; y <= y1; y++)
      for (let x = x0; x <= x1; x++) {
        const i = y * SIZE + x;
        const t = W.tiles[i];
        if (t !== UNLOADED && t !== T.WATER && W.elev[i] >= base + 2) {
          tall[(y - y0) * w + x - x0] = 1;
          peak = Math.max(peak, W.elev[i]);
        }
      }
    const c = this.iso((x0 + x1) / 2, (y0 + y1) / 2);
    const r: Region = {
      site: k, x0, y0, x1, y1, tall,
      dx0: Math.max(0, x0 - BACK), dy0: Math.max(0, y0 - BACK), dx1: Math.min(SIZE - 1, x1 + FRONT), dy1: Math.min(SIZE - 1, y1 + FRONT),
      base, peak: Math.min(peak, MAX_ELEV), lift: (base - 1) * LV, strips: [], bakeRow: NaN, centreX: c.x, centreY: c.y,
    };
    this.regions.push(r);
    this.s.redepthBox(r.dx0, r.dy0, r.dx1, r.dy1);
  }

  private iso(x: number, y: number) {
    return { x: ((x - y) * TW) / 2 + this.s.offX, y: ((x + y) * TH) / 2 };
  }

  private clearStrips(r: Region) {
    for (const st of r.strips) st.destroy();
    r.strips = [];
  }

  private dropRegion(r: Region) {
    this.clearStrips(r);
    r.bakeRow = NaN;
  }

  /** Left / right half frames of a tile texture (its two cliff faces), added once. */
  private half(key: string) {
    if (this.halves.has(key)) return true;
    const tm = this.s.textures;
    if (!tm.exists(key)) return false;
    const tex = tm.get(key);
    const f = tex.get();
    tex.add("L", f.sourceIndex, f.cutX, f.cutY, TW / 2, f.cutHeight);
    tex.add("R", f.sourceIndex, f.cutX + TW / 2, f.cutY, TW / 2, f.cutHeight);
    // Texture.add makes the first frame added the texture's default, so every
    // batchDraw(key) without a frame (drawChunk, repaintTileBox) would draw only
    // the left half and leave the right half of each tile black: keep __BASE.
    tex.firstFrame = "__BASE";
    this.halves.add(key);
    return true;
  }

  /** Bake about `budget` tile halves of r's strips, back row first (a row is never split). */
  private bake(r: Region, budget: number) {
    const last = r.x1 + r.y1;
    if (Number.isNaN(r.bakeRow)) r.bakeRow = r.x0 + r.y0;
    const W = this.s.world,
      w = r.x1 - r.x0 + 1;
    const tallAt = (x: number, y: number) =>
      x >= r.x0 && x <= r.x1 && y >= r.y0 && y <= r.y1 && r.tall[(y - r.y0) * w + x - r.x0] === 1;
    // levels of this tile a non-strip neighbour already covers in the chunk texture
    const cover = (x: number, y: number) => {
      if (tallAt(x, y)) return 0;
      if (x < 0 || y < 0 || x >= SIZE || y >= SIZE) return 1;
      const i = y * SIZE + x;
      return W.tiles[i] === UNLOADED ? 0 : Math.max(1, W.elev[i]);
    };
    while (r.bakeRow <= last && budget > 0) {
      const row = r.bakeRow++;
      const tiles: [number, number][] = [];
      for (let x = Math.max(r.x0, row - r.y1); x <= Math.min(r.x1, row - r.y0); x++) if (tallAt(x, row - x)) tiles.push([x, row - x]);
      if (!tiles.length) continue;
      let minX = Infinity,
        minY = Infinity,
        maxX = -Infinity,
        maxY = -Infinity;
      for (const [x, y] of tiles) {
        const p = this.iso(x, y),
          e = Math.max(1, W.elev[y * SIZE + x]);
        minX = Math.min(minX, p.x - TW / 2);
        maxX = Math.max(maxX, p.x + TW / 2);
        minY = Math.min(minY, p.y - (e - 1) * LV);
        maxY = Math.max(maxY, p.y + 40);
      }
      minX = Math.floor(minX);
      minY = Math.floor(minY);
      const rt = this.s.add
        .renderTexture(minX, minY, Math.ceil(maxX - minX), Math.ceil(maxY - minY))
        .setOrigin(0)
        .setDepth(row * (TH / 2) - r.lift)
        .setVisible(this.s.z === 0);
      rt.beginDraw();
      for (const [x, y] of tiles) {
        const i = y * SIZE + x;
        const key = this.s.tileKey(i);
        if (!key || !this.half(key)) continue;
        let faces = mountainFaces(this.s.textures, key);
        if (faces && !faces.every((f) => this.half(f))) faces = null;
        const p = this.iso(x, y),
          e = Math.max(1, W.elev[i]);
        const diag = cover(x + 1, y + 1);
        const fromL = Math.max(cover(x, y + 1), diag),
          fromR = Math.max(cover(x + 1, y), diag);
        const lx = p.x - TW / 2 - minX;
        for (let l = Math.min(fromL, fromR); l < e; l++) {
          const ly = p.y - l * LV - minY;
          // the levels under the top are rock face, exactly as drawChunk draws them
          const face = faces && l < e - 1 ? faceAt(faces, x, y, l) : key,
            strat = faces && face !== key && stratumAt(l) ? faces[4] : null;
          if (l >= fromL) {
            rt.batchDrawFrame(face, "L", lx, ly);
            if (strat) rt.batchDrawFrame(strat, "L", lx, ly);
            budget--;
          }
          if (l >= fromR) {
            rt.batchDrawFrame(face, "R", lx + TW / 2, ly);
            if (strat) rt.batchDrawFrame(strat, "R", lx + TW / 2, ly);
            budget--;
          }
        }
        for (const lip of this.s.lipsAt(x, y)) rt.batchDraw(lip, lx, p.y - (e - 1) * LV - minY);
      }
      rt.endDraw();
      r.strips.push(rt);
    }
  }
}
