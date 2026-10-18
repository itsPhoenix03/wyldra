import Phaser from 'phaser';
import { KEEPER_SKINS, PACKS, atlasBitmap } from './atlasData.ts';
import { installAtlas } from './atlas.ts';
import { KEEPER_PACK } from './assets.ts';
import { SHIRTS, SHIRT_BODY, SHIRT_LIGHT, shirtLight, skinId, type Cloak } from './keeperSkinArt.ts';

export { SHIRTS, SHIRT_HEX, shirtFor, skinId, skinKey, type Cloak } from './keeperSkinArt.ts';

/* ────────────────────────────────────────────────────────────────────────────
 * Keeper skins — per-player shirt colour and worn gear.
 *
 * The art (src/keeperSkinArt.ts) is baked at build time, never in the browser:
 * tools/atlas/build-atlases.mjs renders the keeper frame set once per gear
 * variant (armor and/or cloak) in the art's own shirt, plus a shirt mask for
 * each. A skin is then:
 *   - shirt 0: that gear variant's atlas, as is;
 *   - any other shirt: the variant recoloured through its mask in a Web Worker
 *     (src/skinWorker.ts).
 * Either way the main thread does one texture upload per skin — the old path
 * rasterised all ~420 keeper SVGs on the main thread, per skin, per player.
 *
 * Skin 0 without gear is the keeper pack itself, so nothing is built for the
 * common case. Any other skin is built once per texture manager (async); until
 * it is ready the rig keeps showing the plain frames.
 * ──────────────────────────────────────────────────────────────────────────── */

let worker: Worker | null = null;
let nextReq = 1;
const replies = new Map<number, { ok: (b: ImageBitmap) => void; fail: (e: Error) => void }>();

function recolor(gear: string, shirt: number): Promise<ImageBitmap> {
  if (!worker) {
    worker = new Worker(new URL('./skinWorker.ts', import.meta.url), { type: 'module' });
    worker.onmessage = (e: MessageEvent) => {
      const r = replies.get(e.data.id);
      if (!r) return;
      replies.delete(e.data.id);
      if (e.data.bmp) r.ok(e.data.bmp);
      else r.fail(new Error(e.data.error));
    };
  }
  const id = nextReq++;
  return new Promise((ok, fail) => {
    replies.set(id, { ok, fail });
    worker!.postMessage({
      id,
      atlas: gear ? KEEPER_SKINS.variants[gear] : PACKS[KEEPER_PACK].file,
      mask: KEEPER_SKINS.masks[gear],
      from: [SHIRT_BODY, SHIRT_LIGHT],
      to: [SHIRTS[shirt], shirtLight(shirt)],
    });
  });
}

// recoloured atlases outlive a game (like atlasData's bitmaps), so a re-join reuses them
const recolored = new Map<string, Promise<ImageBitmap>>();

// per texture manager: a game restart gets a fresh manager and reinstalls
const built = new WeakMap<Phaser.Textures.TextureManager, Map<string, boolean>>();

/** True once every frame of the skin exists; starts building it on first ask. */
export function ensureSkin(scene: Phaser.Scene, _frames: string[], shirt: number, cloak: Cloak, armor = false): boolean {
  const id = skinId(shirt, cloak, armor);
  if (id === '0') return true;
  const tm = scene.textures;
  let m = built.get(tm);
  if (!m) built.set(tm, (m = new Map()));
  const state = m.get(id);
  if (state !== undefined) return state;
  m.set(id, false);
  const gear = id.slice(1); // skinId is the shirt digit, then the gear suffix
  let bmp = shirt === 0 ? atlasBitmap(KEEPER_SKINS.variants[gear]) : recolored.get(id);
  if (!bmp) {
    recolored.set(id, (bmp = recolor(gear, shirt)));
    bmp.catch(() => recolored.delete(id));
  }
  bmp.then(
    (b) => {
      if (!(tm as unknown as { game: unknown }).game) return; // the game was torn down meanwhile
      installAtlas(tm, `atlas:keeper@${id}`, b, PACKS[KEEPER_PACK].frames, `@${id}`);
      m!.set(id, true);
    },
    (e) => console.warn('keeper skin failed', id, e), // stays false: rig keeps the plain art
  );
  return false;
}
