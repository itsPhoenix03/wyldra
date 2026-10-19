#!/usr/bin/env node
// Bakes the SVG art into PNG texture atlases, so the browser never rasterises an SVG.
//
//   node tools/atlas/build-atlases.mjs          build if any input changed (fast no-op otherwise)
//   node tools/atlas/build-atlases.mjs --force  rebuild everything
//
// Inputs:  src/assets.ts (ASSET_MANIFEST + PACK_OF), the SVGs it names,
//          src/keeperSkinArt.ts for the keeper gear variants, and src/menuArt.ts
//          (BACKDROPS[BACKDROP]) for the title screen's backdrop.
// Outputs: assets/atlas/<pack>.<hash>.png          one atlas per pack (served from /atlas/)
//          assets/atlas/keeper~<gear>.<hash>.png   the keeper frame set wearing each gear variant
//          assets/atlas/keeper-mask~<gear>.<hash>.png  shirt coverage for recolouring (R body, G highlight)
//          assets/atlas/menu-<biome>.<hash>.png    the title screen's backdrop: an island of one biome's tiles
//          src/generated/atlas.json                frame rects + file names, bundled into the client
// Both output locations are gitignored: this script is the source of truth, and
// `npm start` / `npm run build` / `npm run dev` run it first.
//
// Every frame is rendered at exactly the size the old Phaser SVG loader used
// (the manifest's w×h, viewBox kept), so the art is pixel-for-pixel what it was.

import { createHash } from 'node:crypto';
import { existsSync, mkdirSync, readFileSync, readdirSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { dirname, join, resolve } from 'node:path';
import { fileURLToPath, pathToFileURL } from 'node:url';
import { deflateSync } from 'node:zlib';
import { build } from 'esbuild';
import { Resvg } from '@resvg/resvg-js';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const OUT_DIR = join(ROOT, 'assets/atlas');
const INDEX = join(ROOT, 'src/generated/atlas.json');
const FORCE = process.argv.includes('--force');
const PAD = 2; // extruded border around every frame: linear filtering at camera zoom never samples a neighbour

// ── load the manifest and the skin art straight from the client sources ──────────
async function loadModule(entry) {
  const out = join(tmpdir(), `wyldra-atlas-${process.pid}-${entry.replace(/\W/g, '_')}.mjs`);
  await build({ entryPoints: [join(ROOT, entry)], bundle: true, format: 'esm', platform: 'node', outfile: out, logLevel: 'error' });
  try { return await import(pathToFileURL(out).href); } finally { rmSync(out, { force: true }); }
}
const { ASSET_MANIFEST, PACK_OF, KEEPER_PACK } = await loadModule('src/assets.ts');
const skin = await loadModule('src/keeperSkinArt.ts');
const menuArt = await loadModule('src/menuArt.ts');

// ── skip when nothing changed ────────────────────────────────────────────────────
const svgCache = new Map();
const svgOf = (url) => {
  let s = svgCache.get(url);
  if (s === undefined) svgCache.set(url, (s = readFileSync(join(ROOT, 'assets', url), 'utf8')));
  return s;
};
const inputHash = (() => {
  const h = createHash('sha1');
  h.update(readFileSync(fileURLToPath(import.meta.url)));
  h.update(readFileSync(join(ROOT, 'src/keeperSkinArt.ts')));
  h.update(JSON.stringify(ASSET_MANIFEST));
  h.update(JSON.stringify([...PACK_OF]));
  for (const [, url] of ASSET_MANIFEST) h.update(url).update(svgOf(url));
  const bd = menuArt.BACKDROPS[menuArt.BACKDROP];
  h.update(menuArt.BACKDROP).update(JSON.stringify(bd.cells()));
  for (const t of bd.tiles) h.update(svgOf(`/tiles/${t}.svg`));
  return h.digest('hex').slice(0, 16);
})();
if (!FORCE && existsSync(INDEX)) {
  try {
    const prev = JSON.parse(readFileSync(INDEX, 'utf8'));
    const files = [...Object.values(prev.packs).map((p) => p.file), ...Object.values(prev.keeperSkins.variants), ...Object.values(prev.keeperSkins.masks),
      ...(prev.menuBackdrop ? [prev.menuBackdrop.file] : [])];
    if (prev.inputs === inputHash && files.every((f) => existsSync(join(ROOT, 'assets', f)))) {
      console.log(`[atlas] up to date (${Object.keys(prev.packs).length} packs)`);
      process.exit(0);
    }
  } catch { /* unreadable index: rebuild */ }
}
const t0 = Date.now();

// ── rasterise one SVG at w×h, the way Phaser's SVGFile did ───────────────────────
function sized(svg, w, h) {
  return svg.replace(/<svg\b[^>]*>/, (tag) => {
    const num = (a) => parseFloat((tag.match(new RegExp(`\\s${a}="([^"]*)"`)) || [])[1]);
    let t = tag;
    const sw = num('width'), sh = num('height');
    if (!/\sviewBox=/.test(t) && sw && sh) t = t.replace(/^<svg/, `<svg viewBox="0 0 ${sw} ${sh}"`);
    for (const [a, v] of [['width', w], ['height', h]])
      t = new RegExp(`\\s${a}="`).test(t) ? t.replace(new RegExp(`(\\s${a}=")[^"]*"`), `$1${v}px"`) : t.replace(/^<svg/, `<svg ${a}="${v}px"`);
    return t;
  });
}
/** Premultiplied RGBA, exactly w×h. */
function render(svg, w, h, what) {
  const img = new Resvg(sized(svg, w, h), { font: { loadSystemFonts: false } }).render();
  if (img.width !== w || img.height !== h) throw new Error(`[atlas] ${what}: rendered ${img.width}x${img.height}, want ${w}x${h}`);
  return img.pixels;
}

// ── shelf packing ────────────────────────────────────────────────────────────────
function pack(items) {
  const cells = items.map((it) => ({ ...it, cw: it.w + PAD * 2, ch: it.h + PAD * 2 }));
  const area = cells.reduce((s, c) => s + c.cw * c.ch, 0);
  let W = 256;
  while (W * W < area * 1.2 && W < 4096) W *= 2;
  cells.sort((a, b) => b.ch - a.ch || b.cw - a.cw || (a.key < b.key ? -1 : 1));
  let x = 0, y = 0, shelf = 0;
  for (const c of cells) {
    if (x + c.cw > W) { x = 0; y += shelf; shelf = 0; }
    c.x = x + PAD; c.y = y + PAD;
    x += c.cw; shelf = Math.max(shelf, c.ch);
  }
  const H = Math.ceil((y + shelf) / 4) * 4;
  if (H > 4096) throw new Error(`[atlas] pack too large: ${W}x${H}`);
  return { W, H, cells };
}

/** Copy a premultiplied frame into the atlas and extrude its edge PAD pixels outward. */
function blit(dst, W, src, x0, y0, w, h) {
  for (let y = -PAD; y < h + PAD; y++) {
    const sy = Math.min(h - 1, Math.max(0, y));
    for (let x = -PAD; x < w + PAD; x++) {
      const sx = Math.min(w - 1, Math.max(0, x));
      const s = (sy * w + sx) * 4, d = ((y0 + y) * W + (x0 + x)) * 4;
      dst[d] = src[s]; dst[d + 1] = src[s + 1]; dst[d + 2] = src[s + 2]; dst[d + 3] = src[s + 3];
    }
  }
}

// ── PNG encoding (straight alpha, per-row adaptive filter) ───────────────────────
const CRC = new Int32Array(256).map((_, n) => { let c = n; for (let k = 0; k < 8; k++) c = c & 1 ? 0xedb88320 ^ (c >>> 1) : c >>> 1; return c; });
const crc32 = (buf) => { let c = -1; for (const b of buf) c = CRC[(c ^ b) & 255] ^ (c >>> 8); return (c ^ -1) >>> 0; };
const chunk = (type, data) => {
  const len = Buffer.alloc(4); len.writeUInt32BE(data.length);
  const td = Buffer.concat([Buffer.from(type, 'ascii'), data]);
  const crc = Buffer.alloc(4); crc.writeUInt32BE(crc32(td));
  return Buffer.concat([len, td, crc]);
};
function png(premul, W, H, opaque = false) {
  const stride = W * 4, raw = Buffer.alloc((stride + 1) * H);
  const row = Buffer.alloc(stride), prev = Buffer.alloc(stride), cand = [Buffer.alloc(stride), Buffer.alloc(stride), Buffer.alloc(stride)];
  for (let y = 0; y < H; y++) {
    for (let i = 0; i < stride; i += 4) {
      const p = y * stride + i, a = premul[p + 3];
      if (opaque) { row[i] = premul[p]; row[i + 1] = premul[p + 1]; row[i + 2] = premul[p + 2]; row[i + 3] = 255; continue; }
      row[i + 3] = a;
      for (let c = 0; c < 3; c++) row[i + c] = a ? Math.min(255, Math.round((premul[p + c] * 255) / a)) : 0;
    }
    // filters 0 (none), 1 (sub), 2 (up): keep whichever has the smallest residual
    let best = 0, bestSum = Infinity;
    for (let f = 0; f < 3; f++) {
      const o = cand[f]; let sum = 0;
      for (let i = 0; i < stride; i++) {
        const v = f === 0 ? row[i] : f === 1 ? (row[i] - (i >= 4 ? row[i - 4] : 0)) & 255 : (row[i] - prev[i]) & 255;
        o[i] = v; sum += v < 128 ? v : 256 - v;
      }
      if (sum < bestSum) { bestSum = sum; best = f; }
    }
    raw[y * (stride + 1)] = best;
    cand[best].copy(raw, y * (stride + 1) + 1);
    row.copy(prev);
  }
  const ihdr = Buffer.alloc(13);
  ihdr.writeUInt32BE(W, 0); ihdr.writeUInt32BE(H, 4); ihdr[8] = 8; ihdr[9] = 6;
  return Buffer.concat([Buffer.from([137, 80, 78, 71, 13, 10, 26, 10]), chunk('IHDR', ihdr), chunk('IDAT', deflateSync(raw, { level: 9 })), chunk('IEND', Buffer.alloc(0))]);
}

const written = new Set();
function writeAtlas(name, bytes) {
  const file = `/atlas/${name}.${createHash('sha1').update(bytes).digest('hex').slice(0, 10)}.png`;
  writeFileSync(join(ROOT, 'assets', file), bytes);
  written.add(file.slice('/atlas/'.length));
  return file;
}

// ── the packs ────────────────────────────────────────────────────────────────────
mkdirSync(OUT_DIR, { recursive: true });
mkdirSync(dirname(INDEX), { recursive: true });
const byPack = new Map();
for (const [key, url, w, h] of ASSET_MANIFEST) {
  const p = PACK_OF.get(key);
  if (!byPack.has(p)) byPack.set(p, []);
  byPack.get(p).push({ key, url, w, h });
}
const index = { inputs: inputHash, packs: {}, keeperSkins: { variants: {}, masks: {} } };
let keeperLayout = null;
let kb = 0;
for (const [name, items] of [...byPack].sort(([a], [b]) => (a < b ? -1 : 1))) {
  const { W, H, cells } = pack(items);
  const atlas = new Uint8Array(W * H * 4);
  for (const c of cells) blit(atlas, W, render(svgOf(c.url), c.w, c.h, c.key), c.x, c.y, c.w, c.h);
  const bytes = png(atlas, W, H);
  kb += bytes.length / 1024;
  index.packs[name] = {
    file: writeAtlas(name, bytes),
    frames: Object.fromEntries(cells.sort((a, b) => (a.key < b.key ? -1 : 1)).map((c) => [c.key, [c.x, c.y, c.w, c.h]])),
  };
  if (name === KEEPER_PACK) keeperLayout = { W, H, cells };
  console.log(`[atlas] ${name.padEnd(15)} ${String(items.length).padStart(4)} frames  ${W}x${H}  ${(bytes.length / 1024).toFixed(0)} KB`);
}

// ── keeper skins: one atlas per gear variant + its shirt mask, same layout as `keeper` ──
// A mask texel is how much of that pixel is shirt body (R) and shirt highlight (G):
// rendering with the shirt black, then each fill white in turn, isolates each fill's
// premultiplied contribution — compositing is linear in a fill's colour.
{
  const { W, H, cells } = keeperLayout;
  for (const v of skin.GEAR_VARIANTS) {
    const { cloak, armor } = skin.gearOf(v);
    const dress = (c) => skin.skinSvg(svgOf(c.url), c.key, 0, cloak, armor);
    if (v) {
      const atlas = new Uint8Array(W * H * 4);
      for (const c of cells) blit(atlas, W, render(dress(c), c.w, c.h, `${c.key}~${v}`), c.x, c.y, c.w, c.h);
      const bytes = png(atlas, W, H);
      kb += bytes.length / 1024;
      index.keeperSkins.variants[v] = writeAtlas(`keeper~${v}`, bytes);
    }
    const mask = new Uint8Array(W * H * 4);
    for (const c of cells) {
      const svg = dress(c);
      const r0 = render(skin.withShirtColors(svg, '#000000', '#000000'), c.w, c.h, c.key);
      const r1 = render(skin.withShirtColors(svg, '#ffffff', '#000000'), c.w, c.h, c.key);
      const r2 = render(skin.withShirtColors(svg, '#000000', '#ffffff'), c.w, c.h, c.key);
      const m = new Uint8Array(c.w * c.h * 4);
      for (let i = 0; i < m.length; i += 4) {
        m[i] = Math.max(r1[i] - r0[i], r1[i + 1] - r0[i + 1], r1[i + 2] - r0[i + 2], 0);
        m[i + 1] = Math.max(r2[i] - r0[i], r2[i + 1] - r0[i + 1], r2[i + 2] - r0[i + 2], 0);
        m[i + 3] = 255;
      }
      blit(mask, W, m, c.x, c.y, c.w, c.h);
    }
    const bytes = png(mask, W, H, true);
    kb += bytes.length / 1024;
    index.keeperSkins.masks[v] = writeAtlas(`keeper-mask~${v || 'plain'}`, bytes);
    console.log(`[atlas] keeper skin ${v || 'plain'}: atlas${v ? '' : ' (= keeper pack)'} + mask`);
  }
}

// ── the title screen's backdrop: one picture made only of one biome's tiles (and, for
// the Woods, its trees and plants), from src/menuArt.ts. Each SVG becomes a symbol (its
// ids namespaced so they cannot collide); every cell is a column of tiles stacked one
// tile-thickness apart with its top tile last, then the plant standing on it, back to
// front; resvg renders the lot into a single PNG. An island is cropped to itself; land is
// a window cut from inside the grid, so no edge of the grid shows.
{
  const { BACKDROP, BACKDROPS, TILE_W, TILE_H, LAYER, LAYERS_PER_LEVEL } = menuArt;
  const bd = BACKDROPS[BACKDROP];
  const symbol = (id, src, w, h) => {
    const vb = (src.match(/viewBox="([^"]+)"/) || [])[1] || `0 0 ${w} ${h}`;
    const inner = src.replace(/^[\s\S]*?<svg\b[^>]*>/, '').replace(/<\/svg>\s*$/, '')
      .replace(/id="([^"]+)"/g, `id="${id}-$1"`).replace(/url\(#([^)]+)\)/g, `url(#${id}-$1)`);
    return `<symbol id="${id}" viewBox="${vb}">${inner}</symbol>`;
  };
  const symbols = [
    ...bd.tiles.map((t) => symbol(`t-${t}`, svgOf(`/tiles/${t}.svg`), TILE_W, TILE_H)),
    ...Object.entries(bd.props).map(([k, [w, h]]) => symbol(`p-${k}`, svgOf(`/sprites/${k}.svg`), w, h)),
  ];
  const island = bd.frame.kind === 'island';
  const win = island ? null
    : { x0: -bd.frame.width / 2, y0: bd.frame.top, x1: bd.frame.width / 2, y1: bd.frame.top + bd.frame.height };
  const TALL = 90; // how far a plant may stand above its tile
  const parts = [];
  let x0 = Infinity, y0 = Infinity, x1 = -Infinity, y1 = -Infinity;
  let tiles = 0, plants = 0;
  for (const c of bd.cells()) {
    const cx = ((c.x - c.y) * TILE_W) / 2, cy = ((c.x + c.y) * TILE_W) / 4;
    const layers = c.h === 0 ? 1 : c.h * LAYERS_PER_LEVEL + 1;
    const lift = (layers - 1) * LAYER;
    // a land window skips every column that cannot reach into it
    if (win && (cx + TILE_W / 2 < win.x0 || cx - TILE_W / 2 > win.x1 || cy - TILE_W / 4 - lift - TALL > win.y1 || cy + TILE_H > win.y0 + (win.y1 - win.y0) + TILE_H * 4 || cy + TILE_H < win.y0)) continue;
    for (let k = 0; k < layers; k++) {
      const x = cx - TILE_W / 2, y = cy - TILE_W / 4 - k * LAYER;
      const tile = k === layers - 1 ? c.top : c.side;
      // an island's open sea is drawn see-through over a deep base, so it reads calmer than the land
      const fade = island && c.h === 0 && tile === bd.sea ? ' opacity=".58"' : '';
      parts.push(`<use href="#t-${tile}" x="${x}" y="${y}" width="${TILE_W}" height="${TILE_H}"${fade}/>`);
      tiles++;
      x0 = Math.min(x0, x); y0 = Math.min(y0, y); x1 = Math.max(x1, x + TILE_W); y1 = Math.max(y1, y + TILE_H);
    }
    if (c.prop) {
      const [k, dx, dy] = c.prop, [w, h] = bd.props[k];
      const fx = cx + dx, fy = cy - lift + dy; // its feet on the middle of its tile
      parts.push(`<use href="#p-${k}" x="${fx - w / 2}" y="${fy - h + 3}" width="${w}" height="${h}"/>`);
      plants++;
    }
  }
  let base = '', vx, vy, W, H, focus;
  if (island) {
    W = Math.ceil(x1 - x0); H = Math.ceil(y1 - y0); vx = x0; vy = y0;
    // the deep water under the round sea (a circle on the grid is a 2:1 ellipse on screen)
    const rx = bd.frame.r * (TILE_W / 2) * Math.SQRT2;
    const ox = ((bd.frame.cx - bd.frame.cy) * TILE_W) / 2, oy = ((bd.frame.cx + bd.frame.cy) * TILE_W) / 4;
    base = `<ellipse cx="${ox}" cy="${oy}" rx="${rx}" ry="${rx / 2}" fill="${bd.deep}"/>`;
    focus = [(ox - x0) / W, (oy - y0) / H].map((v) => +v.toFixed(4)); // where the island's centre lands
  } else {
    W = bd.frame.width; H = bd.frame.height; vx = win.x0; vy = win.y0;
    focus = [0.5, 0.5];
  }
  const svg = `<svg xmlns="http://www.w3.org/2000/svg" width="${W}" height="${H}" viewBox="${vx} ${vy} ${W} ${H}">` +
    `<defs>${symbols.join('')}</defs>${base}${parts.join('')}</svg>`;
  const name = `menu-${BACKDROP}`;
  const bytes = png(render(svg, W, H, name), W, H);
  kb += bytes.length / 1024;
  index.menuBackdrop = { biome: BACKDROP, frame: bd.frame.kind, file: writeAtlas(name, bytes), size: [W, H], focus };
  console.log(`[atlas] ${name.padEnd(15)} ${String(tiles).padStart(4)} tiles, ${plants} plants  ${W}x${H}  ${(bytes.length / 1024).toFixed(0)} KB`);
}

// ── publish ──────────────────────────────────────────────────────────────────────
for (const f of readdirSync(OUT_DIR)) if (f.endsWith('.png') && !written.has(f)) rmSync(join(OUT_DIR, f));
writeFileSync(INDEX, JSON.stringify(index) + '\n');
console.log(`[atlas] ${ASSET_MANIFEST.length} frames -> ${written.size} PNGs, ${(kb / 1024).toFixed(1)} MB, in ${((Date.now() - t0) / 1000).toFixed(1)}s`);
