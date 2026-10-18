import Phaser from 'phaser';
import { PACKS, packBitmap, type Rect } from './atlasData.ts';

/* ────────────────────────────────────────────────────────────────────────────
 * Atlas textures with the old per-sprite keys.
 *
 * Each atlas is ONE GPU texture. Every frame in it is also registered as its own
 * texture key (an alias sharing the atlas's TextureSource, so the same WebGL
 * texture), which keeps the whole client's `add.image(x, y, 'rock')`,
 * `setTexture(key)`, `textures.exists(key)` and anim `{ key }` call sites working
 * unchanged — and since every alias is the same GL texture, sprites from one atlas
 * batch into one draw call instead of one per sprite.
 *
 * A key that has not streamed in yet simply does not exist, which is what the
 * callers' existing `textures.exists(...) ? key : fallback` paths already handle.
 * ──────────────────────────────────────────────────────────────────────────── */

const atlasKey = (name: string) => `atlas:${name}`;

/**
 * Upload `bmp` as texture `key` and alias every frame as `<frame><suffix>`.
 * The one texture upload is the only main-thread cost; aliases are plain objects.
 */
export function installAtlas(
  tm: Phaser.Textures.TextureManager, key: string, bmp: ImageBitmap,
  frames: Record<string, Rect>, suffix = '',
) {
  if (tm.exists(key)) return;
  const atlas = tm.addImage(key, bmp as unknown as HTMLImageElement);
  if (!atlas) return;
  const src = atlas.source[0];
  const list = tm.list as Record<string, Phaser.Textures.Texture>;
  for (const k in frames) {
    const name = k + suffix;
    if (list[name]) continue;
    const [x, y, w, h] = frames[k];
    const t = new Phaser.Textures.Texture(tm, name, []);
    t.source.push(src);
    t.add('__BASE', 0, x, y, w, h);
    list[name] = t;
  }
}

/**
 * Per-scene pack streaming. `ensure` is idempotent and cheap to call every frame;
 * `onInstalled` runs once per pack, after its textures exist (register anims there).
 */
export class PackLoader {
  private pending = new Map<string, Promise<void>>();

  constructor(
    private tm: Phaser.Textures.TextureManager,
    private live: () => boolean,
    private onInstalled: (pack: string) => void,
  ) {}

  has(name: string) {
    return this.tm.exists(atlasKey(name));
  }

  /** Install an already-decoded pack right now (the boot packs, awaited before the scene starts). */
  installNow(name: string, bmp: ImageBitmap) {
    if (this.has(name)) return;
    installAtlas(this.tm, atlasKey(name), bmp, PACKS[name].frames);
    this.onInstalled(name);
  }

  ensure(name: string): Promise<void> {
    if (!PACKS[name] || this.has(name)) return Promise.resolve();
    let p = this.pending.get(name);
    if (!p) {
      p = packBitmap(name).then((bmp) => {
        if (this.live()) this.installNow(name, bmp);
      });
      p.catch((e) => {
        console.warn('[atlas] pack failed', name, e);
        this.pending.delete(name); // retried on the next ensure()
      });
      this.pending.set(name, p);
    }
    return p;
  }
}
