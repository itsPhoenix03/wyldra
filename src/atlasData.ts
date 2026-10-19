// Atlas index + off-main-thread decoding. No Phaser here: boot.ts imports this to
// start fetching the boot atlases while the player is still on the menu.
//
// The atlases are baked by tools/atlas/build-atlases.mjs; src/generated/atlas.json
// is its index (frame rects + content-hashed file names). Decoding goes through
// createImageBitmap, which decodes off the main thread — the main thread's only
// job per atlas is the one texture upload when it is installed (src/atlas.ts).

import ATLAS from './generated/atlas.json';

export type Rect = [number, number, number, number];
export interface PackDef { file: string; frames: Record<string, Rect> }
export const PACKS = ATLAS.packs as unknown as Record<string, PackDef>;
/** Keeper gear-variant atlases and shirt masks, keyed by gear suffix ('' = no gear). */
export const KEEPER_SKINS = ATLAS.keeperSkins as { variants: Record<string, string>; masks: Record<string, string> };
/** The title screen's backdrop, baked from src/menuArt.ts; focus is where the island's centre lands (0..1). */
export const MENU_BACKDROP = (ATLAS as unknown as {
  menuBackdrop?: { biome: string; frame: "island" | "land"; file: string; size: [number, number]; focus: [number, number] };
}).menuBackdrop;

/** Every atlas file this build serves (the service worker keeps these and drops the rest). */
export const ATLAS_FILES: string[] = [
  ...Object.values(PACKS).map((p) => p.file),
  ...Object.values(KEEPER_SKINS.variants),
  ...Object.values(KEEPER_SKINS.masks),
  ...(MENU_BACKDROP ? [MENU_BACKDROP.file] : []),
];

// A decoded bitmap outlives a Phaser.Game (TextureSource.destroy never closes its
// image), so re-joining after a quit reuses these without fetching or decoding again.
const bitmaps = new Map<string, Promise<ImageBitmap>>();

/** Fetch and decode one atlas file, premultiplied the way Phaser's WebGL pipeline expects. */
export function atlasBitmap(url: string): Promise<ImageBitmap> {
  let p = bitmaps.get(url);
  if (!p) {
    p = fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`atlas ${url}: HTTP ${r.status}`);
        return r.blob();
      })
      .then((b) => createImageBitmap(b, { premultiplyAlpha: 'premultiply' }));
    p.catch(() => bitmaps.delete(url)); // a failed fetch may be retried later
    bitmaps.set(url, p);
  }
  return p;
}

/** The decoded atlas of a pack. */
export const packBitmap = (name: string) => atlasBitmap(PACKS[name].file);

/** Packs a new world cannot render without (src/assets.ts PACK_OF): decoded before the scene starts. */
export const BOOT_PACKS = ['base', 'keeper'];
