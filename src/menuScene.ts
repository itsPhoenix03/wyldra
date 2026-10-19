// The title screen's backdrop: one biome's tiles (and, for the Woods, its trees and
// plants) baked into one picture at build time (src/menuArt.ts, tools/atlas/
// build-atlases.mjs). The page only has to decode that one PNG, off the main thread; the
// light, clouds, motes or snow over it are CSS, picked by #menu[data-biome] / [data-frame].

import { MENU_BACKDROP } from "./atlasData.ts";

/** Hang the baked backdrop in #menu-backdrop once; it fades in when the picture is decoded. */
export function buildMenuScene(root: HTMLElement): void {
  const art = MENU_BACKDROP;
  if (!art || root.style.backgroundImage) return;
  const menu = root.closest<HTMLElement>("#menu");
  if (menu) {
    menu.dataset.biome = art.biome;
    menu.dataset.frame = art.frame;
  }
  const [w, h] = art.size;
  root.style.aspectRatio = `${w} / ${h}`;
  const img = new Image();
  img.src = art.file;
  img.decode().catch(() => {}).finally(() => {
    root.style.backgroundImage = `url(${art.file})`;
    root.classList.add("ready");
  });
}
