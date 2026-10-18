// Keeper shirt recolouring, off the main thread.
//
// Request:  { id, atlas, mask, from: [body, light], to: [body, light] }  (urls, '#rrggbb')
// Reply:    { id, bmp: ImageBitmap }  (premultiplied, transferred) or { id, error }
//
// The mask atlas (tools/atlas/build-atlases.mjs) says, per pixel, how much
// premultiplied colour the shirt body (R) and highlight (G) contribute. Swapping
// a shirt is therefore exact per pixel: take the art's own fill colours out and
// put the new ones in, weighted by that coverage — antialiased edges included.

type Px = { data: Uint8ClampedArray; w: number; h: number };

const ctx = self as unknown as {
  onmessage: ((e: MessageEvent) => void) | null;
  postMessage(msg: unknown, transfer?: Transferable[]): void;
};

const pixels = new Map<string, Promise<Px>>();
function load(url: string): Promise<Px> {
  let p = pixels.get(url);
  if (!p) {
    p = fetch(url)
      .then((r) => {
        if (!r.ok) throw new Error(`${url}: HTTP ${r.status}`);
        return r.blob();
      })
      .then((b) => createImageBitmap(b, { premultiplyAlpha: 'none' }))
      .then((bmp) => {
        const c = new OffscreenCanvas(bmp.width, bmp.height);
        const g = c.getContext('2d')!;
        g.drawImage(bmp, 0, 0);
        bmp.close();
        return { data: g.getImageData(0, 0, c.width, c.height).data, w: c.width, h: c.height };
      });
    p.catch(() => pixels.delete(url));
    pixels.set(url, p);
  }
  return p;
}

const rgb = (hex: string) => [1, 3, 5].map((i) => parseInt(hex.slice(i, i + 2), 16));

ctx.onmessage = async (e: MessageEvent) => {
  const { id, atlas, mask, from, to } = e.data;
  try {
    const [base, m] = await Promise.all([load(atlas), load(mask)]);
    const [fb, fl] = from.map(rgb), [tb, tl] = to.map(rgb);
    const db = [0, 1, 2].map((c) => tb[c] - fb[c]), dl = [0, 1, 2].map((c) => tl[c] - fl[c]);
    const src = base.data, mk = m.data, out = new Uint8ClampedArray(src.length);
    for (let i = 0; i < src.length; i += 4) {
      const a = src[i + 3];
      if (!a) continue;
      const m1 = mk[i] / 255, m2 = mk[i + 1] / 255;
      out[i + 3] = a;
      if (!m1 && !m2) {
        out[i] = src[i]; out[i + 1] = src[i + 1]; out[i + 2] = src[i + 2];
        continue;
      }
      for (let c = 0; c < 3; c++) {
        const p = (src[i + c] * a) / 255 + m1 * db[c] + m2 * dl[c]; // premultiplied
        out[i + c] = (Math.min(a, Math.max(0, p)) * 255) / a;
      }
    }
    const bmp = await createImageBitmap(new ImageData(out, base.w, base.h), { premultiplyAlpha: 'premultiply' });
    ctx.postMessage({ id, bmp }, [bmp]);
  } catch (err) {
    ctx.postMessage({ id, error: String(err) });
  }
};
