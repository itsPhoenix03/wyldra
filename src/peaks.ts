import Phaser from "phaser";
import { SIZE, T, MOUNTAINS } from "../shared/world.js";
import { UNLOADED, CHUNK, type TileStore } from "./tiles.ts";

/* ────────────────────────────────────────────────────────────────────────────
 * Tile mountains on screen: terrain that hides what stands behind it.
 *
 * The terrain is baked into chunk RenderTextures at depth -1, under every
 * sprite (main.ts drawChunk), which is fine for hills of a level or three: a
 * keeper behind one shows over it, as they always have. A tile mountain
 * (gameserver/world/mountains.go, the tileMountains feature) stands up to ten
 * levels — 100 px — over the ground, and a keeper walking behind it must go
 * out of sight. So, for each mountain near the camera, every tall tile is also
 * baked into a strip: one RenderTexture per iso row (x + y) of the mountain, at
 * a depth just below the sprites standing on that row and above those on the
 * row behind. Painted in that order the mountain covers whatever is behind it
 * and nothing in front.
 *
 * That only works if the sprites around the mountain sort by where they stand
 * on the ground plane, not by their screen y: lifted by ten levels, a keeper on
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
 * Main-thread cost: regions are found by scanning one 69x69 window when a chunk
 * near a mountain site lands; strips are baked incrementally, a bounded number
 * of tile stacks per frame, only for the mountain nearest the camera, and
 * dropped again when it is far. Per frame there is a box test per region in
 * depthAt and a handful of tile reads in coverAt.
 * ──────────────────────────────────────────────────────────────────────────── */

const TW = 64,
  TH = 32,
  LV = 10; // px per elevation level
const SCAN = 34; // tiles round a MOUNTAINS site the region scan looks
const PAD = 4; // tiles of foothill round the rock tiles that still go in strips
const BACK = 10; // rows behind the strip box that sort by ground plane (a stack hides ~6 rows, a sprite is ~4 tall)
const FRONT = 3;
const BAKE_PER_FRAME = 140; // tile stacks baked per frame
const KEEP_PX = 2600; // a region's strips are dropped once the camera is this far off

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
   * rows in front, in the body's column and the two either side.
   */
  coverAt(x: number, y: number, footY: number): number {
    const r = this.regionAt(x, y);
    if (!r || this.s.z !== 0) return 0;
    const row = Math.floor(x) + Math.floor(y),
      col = x - y;
    const w = r.x1 - r.x0 + 1;
    let cover = 0;
    for (let rr = row + 1; rr <= row + 9; rr++) {
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
    if (near) this.bake(near, BAKE_PER_FRAME);
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
    for (let y = y0; y <= y1; y++)
      for (let x = x0; x <= x1; x++) {
        const i = y * SIZE + x;
        const t = W.tiles[i];
        if (t !== UNLOADED && t !== T.WATER && W.elev[i] >= base + 2) tall[(y - y0) * w + x - x0] = 1;
      }
    const c = this.iso((x0 + x1) / 2, (y0 + y1) / 2);
    const r: Region = {
      site: k, x0, y0, x1, y1, tall,
      dx0: Math.max(0, x0 - BACK), dy0: Math.max(0, y0 - BACK), dx1: Math.min(SIZE - 1, x1 + FRONT), dy1: Math.min(SIZE - 1, y1 + FRONT),
      base, lift: (base - 1) * LV, strips: [], bakeRow: NaN, centreX: c.x, centreY: c.y,
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
    this.halves.add(key);
    return true;
  }

  /** Bake up to `budget` tile stacks of r's strips, back row first. */
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
      budget -= tiles.length;
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
        const p = this.iso(x, y),
          e = Math.max(1, W.elev[i]);
        const diag = cover(x + 1, y + 1);
        const fromL = Math.max(cover(x, y + 1), diag),
          fromR = Math.max(cover(x + 1, y), diag);
        const lx = p.x - TW / 2 - minX;
        for (let l = Math.min(fromL, fromR); l < e; l++) {
          const ly = p.y - l * LV - minY;
          if (l >= fromL) rt.batchDrawFrame(key, "L", lx, ly);
          if (l >= fromR) rt.batchDrawFrame(key, "R", lx + TW / 2, ly);
        }
        for (const lip of this.s.lipsAt(x, y)) rt.batchDraw(lip, lx, p.y - (e - 1) * LV - minY);
      }
      rt.endDraw();
      r.strips.push(rt);
    }
  }
}
