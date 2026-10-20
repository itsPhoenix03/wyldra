// Rock faces for the tile mountains:
//   assets/tiles/mtn_<isle>_<rock|snow>_face{,2,3,4}.svg   four bands of one seamless cliff
//   assets/tiles/mtn_<isle>_<rock|snow>_strata.svg          a bedding-plane overlay
//
//   node tools/art/mountain-faces.mjs
//
// A tile stack draws one 64x40 tile per elevation level, each 10 px above the last,
// so all that shows of a level under the top one is a 10 px band: the strip under
// its diamond that the level above leaves bare (y 14 + x/2 .. 24 + x/2 on the left
// half, mirrored on the right). Stacking the island's top tile there repeats its
// skirt, rim lines and all, once per level, and a cliff reads as brickwork. These
// faces are drawn for every level under the top instead (src/main.ts drawChunk and
// repaintTileBox, src/peaks.ts strips):
//
//   - Each half is a cliff 40 px tall (four levels) drawn in face coordinates —
//     u 0..32 along the face, w 0..40 down it — that wraps top to bottom, and is
//     sheared onto the tile so a crack straight down in w is straight down on screen.
//     The four face textures are its four 10 px bands, so band j stacked on band
//     j + 1 continues it exactly: no seam anywhere, cracks and blocks run through.
//     A level's band is (phase - level) mod 4 for a phase hashed from the tile, so
//     neighbouring tiles show different parts of the cliff (src/peaks.ts faceAt).
//   - The rock is jointed ribs: tall slabs between wandering vertical joints (broken
//     here and there), fractured at staggered, steeply slanting heights. Each block
//     has its own shade, a lit left edge and a crevice down its right, so the face
//     reads as ridges, and a lit lip under the fracture over it.
//     Over that, per island: moss (Woods, Marsh), varnish streaks and cross-bedding
//     (Dunes), frost (Spire), glassy sheen and ember seams (Core); snow faces carry
//     snow on the block tops, in the joints and as spindrift.
//   - The strata overlay is a darker bedding plane along the top of a band (the seam
//     with the level above). The client draws it on a few absolute levels only, the
//     same ones on every tile, so a stratum is one layer round the whole mountain —
//     and changes the tile's phase there, so even a tall face never repeats.
//   - Base colours are the top tiles' own skirts: light left half, dark right.
// The silhouette is the tile's (diamond plus skirt) with the 2 px notch over the
// side corners filled, so a stacked corner has a straight edge. Seeded: every run
// writes the same files.

import { writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const OUT = resolve(dirname(fileURLToPath(import.meta.url)), '../../assets/tiles');
const r2 = (n) => +n.toFixed(2);
const BANDS = 4, PH = 10 * BANDS; // the cliff's period: four levels

// ── colour ──────────────────────────────────────────────────────────────────
const hex = (c) => [1, 3, 5].map((i) => parseInt(c.slice(i, i + 2), 16));
const css = (rgb) => '#' + rgb.map((v) => Math.round(Math.max(0, Math.min(255, v))).toString(16).padStart(2, '0')).join('');
const mix = (a, b, t) => css(hex(a).map((v, i) => v + (hex(b)[i] - v) * t));

// ── seeded randomness ───────────────────────────────────────────────────────
function rng(seed) {
  let s = 2166136261;
  for (const ch of seed) s = Math.imul(s ^ ch.charCodeAt(0), 16777619);
  return () => {
    s = (s + 0x6d2b79f5) | 0;
    let t = Math.imul(s ^ (s >>> 15), 1 | s);
    t = (t + Math.imul(t ^ (t >>> 7), 61 | t)) ^ t;
    return ((t ^ (t >>> 14)) >>> 0) / 4294967296;
  };
}
const span = (r, a, b) => a + (b - a) * r();

// ── the islands: skirt colours from mtn_<isle>_rock.svg, plus what grows or lodges on them ──
// [base, crack, highlight, deep] per half (left lit, right shaded)
const ISLES = {
  woods: {
    L: ['#747b72', '#4b524b', '#a5ab9c', '#5b6159'], R: ['#565e58', '#3a413d', '#7e877c', '#444b46'],
    moss: ['#62804c', '#8dab65'], snow: true,
  },
  dunes: {
    // sandstone: desert-varnish streaks down the face, cross-bedding, a pale bed over each stratum
    L: ['#b06f42', '#743f27', '#e2bb86', '#93573a'], R: ['#855034', '#58301e', '#b98557', '#6c3e28'],
    varnish: '#5e321e', bed: ['#cf9a62', '#a06a42'],
  },
  spire: {
    L: ['#7a8694', '#4a5462', '#b4bec8', '#626d7b'], R: ['#5b6676', '#3a4351', '#8794a2', '#495363'],
    frost: '#c6cfd8', snow: true,
  },
  marsh: {
    // dark wet rock: drip stains and moss
    L: ['#4e5246', '#2f3229', '#7c8068', '#3e4137'], R: ['#3a3d35', '#252821', '#5c614f', '#2e3028'],
    moss: ['#5f7c3f', '#7f9a55'], drip: '#20231c',
  },
  core: {
    // obsidian: glassy facets, glints, a rare ember seam
    L: ['#2a2231', '#130f18', '#6a5d7a', '#1f1926'], R: ['#1c1722', '#0e0b12', '#4a3f57', '#15111a'],
    glint: '#e3dcef', ember: '#b8492c', glass: true,
  },
};
const SNOW = { L: ['#eef3ef', '#c4d2d9'], R: ['#cfdde3', '#9fb4c0'] };

// ── face coordinates ────────────────────────────────────────────────────────
// left half: (u, v) -> (u, 14 + u/2 + v); right half: (u, v) -> (32 + u, 30 - u/2 + v); band v 0..10
const SHEAR = { L: 'matrix(1 .5 0 1 0 14)', R: 'matrix(1 -.5 0 1 32 30)' };
const pts = (p) => p.map(([u, w]) => `${r2(u)} ${r2(w)}`).join(' ');
const poly = (p, attrs) => `<path d="M${pts(p)}Z" ${attrs}/>`;
const path = (p, attrs) => `<path d="M${pts(p)}" fill="none" stroke-linecap="round" stroke-linejoin="round" ${attrs}/>`;

/** A wandering line down the cliff from u0, wrapping: the same u at w = 0 and w = PH. */
function joint(r, u0, amp, step) {
  const n = Math.round(PH / step), out = [];
  for (let k = 0; k <= n; k++) out.push([k === 0 || k === n ? u0 : u0 + span(r, -amp, amp), (k * PH) / n]);
  return out;
}
const uAt = (line, w) => {
  w = ((w % PH) + PH) % PH;
  for (let k = 1; k < line.length; k++)
    if (w <= line[k][1]) {
      const [u0, w0] = line[k - 1], [u1, w1] = line[k];
      return u0 + ((u1 - u0) * (w - w0)) / (w1 - w0);
    }
  return line[line.length - 1][0];
};
/** The joint between w0 and w1 (w1 may pass PH), as points. */
const along = (line, w0, w1) => {
  const p = [[uAt(line, w0), w0]];
  for (let w = Math.ceil(w0 / 2) * 2; w < w1; w += 2) if (w > w0) p.push([uAt(line, w), w]);
  p.push([uAt(line, w1), w1]);
  return p;
};

/** One half of the cliff, one period tall, in face coordinates. */
function cliff(isle, kind, side) {
  const I = ISLES[isle];
  const [base, crack, hi, deep] = I[side];
  const lit = side === 'L';
  const r = rng(`${isle}:${kind}:${side}`);
  const shade = (t) => (t >= 0 ? mix(base, hi, t * (lit ? 0.2 : 0.16)) : mix(base, deep, -t * 0.6));
  const out = [], over = [];
  // ribs: the tile's edges, and two or three wandering joints between, at least 6 apart
  let us;
  do {
    us = [0];
    for (let u = span(r, 6, 10); u < 27; u += span(r, 7, 12)) us.push(u);
    us.push(32);
  } while (us.length < 4 || us.length > 5);
  const lines = us.map((u, k) => (k === 0 || k === us.length - 1 ? joint(r, u, 0, PH) : joint(r, u, 2, span(r, 5, 8))));
  const blocks = [];
  for (let s = 0; s < us.length - 1; s++) {
    const A = lines[s], B = lines[s + 1];
    // fractures down this rib: staggered, steeply slanting, 12..26 apart, wrapping
    const w0 = span(r, 0, PH);
    const ws = [];
    for (let w = w0; w < w0 + PH - 9; w += span(r, 12, 26)) ws.push(w);
    const slant = ws.map(() => (r() < 0.5 ? -1 : 1) * span(r, 2.5, 5.5));
    for (let k = 0; k < ws.length; k++) {
      const ta = ws[k], tb = k + 1 < ws.length ? ws[k + 1] : w0 + PH;
      const sa = slant[k], sb = slant[(k + 1) % ws.length];
      // the block: down A, across the lower fracture, up B, across the upper one
      const left = along(A, ta - sa / 2, tb - sb / 2), right = along(B, ta + sa / 2, tb + sb / 2).reverse();
      const top = [[uAt(A, ta - sa / 2), ta - sa / 2], [uAt(B, ta + sa / 2), ta + sa / 2]];
      const t = s === 0 || s === us.length - 2 ? span(r, -0.35, 0.35) : span(r, -0.9, 1);
      blocks.push({ s, poly: [...left, ...right], top, t, line: r() < 0.5, part: span(r, 0.45, 1), A, B, ta: ta - sa / 2, tb: tb - sb / 2, ra: ta + sa / 2, rb: tb + sb / 2 });
    }
  }
  for (const b of blocks) out.push(poly(b.poly, `fill="${shade(b.t)}"`));
  for (const b of blocks) {
    // lit down the rib's left edge, a crevice down its right: the face reads as ridges
    if (b.s > 0) out.push(path(along(b.A, b.ta + 0.8, b.tb - 0.4).map(([u, w]) => [u + 0.7, w]), `stroke="${hi}" stroke-width=".7" opacity="${lit ? '.34' : '.2'}"`));
    if (b.s < us.length - 2) out.push(path(along(b.B, b.ra + 0.6, b.rb).map(([u, w]) => [u - 0.7, w]), `stroke="${deep}" stroke-width="1.3" opacity=".45"`));
    // the fracture over the block, part-way across, and the lit lip under it
    const [p, q] = b.top, m = [p[0] + (q[0] - p[0]) * b.part, p[1] + (q[1] - p[1]) * b.part];
    if (b.line) out.push(path([p, m], `stroke="${crack}" stroke-width=".8" opacity=".7"`));
    out.push(path([[p[0] + 0.7, p[1] + 1], [m[0] - 0.4, m[1] + 1]], `stroke="${hi}" stroke-width=".6" opacity="${lit ? '.36' : '.22'}"`));
  }
  // the joints, with gaps, and their lit lips
  lines.slice(1, -1).forEach((L) => {
    const w0 = span(r, 0, 10);
    for (let w = w0; w < w0 + PH - 1; ) {
      const len = Math.min(span(r, 10, 26), w0 + PH - w), gap = r() < 0.6 ? span(r, 3, 9) : 0;
      const seg = along(L, w, w + len);
      out.push(path(seg, `stroke="${crack}" stroke-width="${r2(span(r, 0.8, 1.1))}"`));
      w += len + gap;
    }
  });
  // diagonal hairline fractures, chips and specks inside the blocks
  for (let k = 0; k < 5; k++) {
    const u = span(r, 3, 29), w = span(r, 0, PH), dir = r() < 0.5 ? -1 : 1;
    out.push(path([[u, w], [u + dir * span(r, 1, 2), w + span(r, 1.4, 2.4)], [u + dir * span(r, 1.6, 3), w + span(r, 3, 5)]], `stroke="${crack}" stroke-width=".6" opacity=".7"`));
  }
  for (let k = 0; k < 4; k++) {
    const uc = span(r, 3, 29), wc = span(r, 0, PH), hw = span(r, 1, 2), hh = span(r, 1.2, 2.2);
    const p = [[uc - hw, wc + hh * 0.4], [uc - hw * 0.3, wc - hh], [uc + hw, wc - hh * 0.2], [uc + hw * 0.4, wc + hh]];
    out.push(poly(p, `fill="${hi}" opacity="${r2(lit ? span(r, 0.16, 0.26) : span(r, 0.1, 0.18))}"`));
    out.push(path([p[2], p[3], p[0]], `stroke="${crack}" stroke-width=".6" opacity=".45"`));
  }
  for (let k = 0; k < 10; k++)
    out.push(`<circle cx="${r2(span(r, 1.5, 30.5))}" cy="${r2(span(r, 0, PH))}" r="${r2(span(r, 0.4, 0.65))}" fill="${r() < 0.45 ? hi : crack}" opacity=".55"/>`);

  // ── what lives or lodges on each island's rock ──
  const ledges = blocks.filter((b) => b.s > 0 || r() < 0.5); // block tops something can sit on
  const onLedge = (b, f) => {
    const [p, q] = b.top, t = span(r, 0.15, 0.6), len = span(r, 2.4, 5);
    const u = p[0] + (q[0] - p[0]) * t, w = p[1] + (q[1] - p[1]) * t;
    f(u, w + 0.9, len, (q[1] - p[1]) / Math.max(1, q[0] - p[0]));
  };
  if (I.moss)
    for (const b of ledges)
      if (r() < (isle === 'marsh' ? 0.75 : 0.45))
        onLedge(b, (u, w, len) => {
          over.push(`<ellipse cx="${r2(u)}" cy="${r2(w)}" rx="${r2(len * 0.45)}" ry="${r2(span(r, 0.7, 1))}" fill="${I.moss[0]}" opacity=".85"/>`);
          over.push(`<circle cx="${r2(u - len * 0.15)}" cy="${r2(w - 0.3)}" r=".55" fill="${I.moss[1]}"/>`);
          if (r() < 0.5) over.push(path([[u + span(r, -1, 1), w + 0.6], [u + span(r, -1, 1), w + span(r, 1.6, 3)]], `stroke="${I.moss[0]}" stroke-width=".8" opacity=".7"`));
        });
  if (I.varnish)
    for (let k = 0; k < 3; k++) {
      const u = span(r, 3, 28), w = span(r, 0, PH), len = span(r, 14, 30), wd = span(r, 1, 2.6);
      out.push(poly([[u, w], [u + wd, w + 1], [u + wd * 0.7, w + len], [u + wd * 0.4, w + len + 2], [u + 0.2, w + len]], `fill="${I.varnish}" opacity="${r2(span(r, 0.16, 0.26))}"`));
    }
  if (I.bed)
    for (let k = 0; k < 6; k++) {
      const u = span(r, 4, 28), w = span(r, 0, PH);
      out.push(path([[u - 2.4, w + 0.7], [u + 2.4, w - 0.7]], `stroke="${hi}" stroke-width=".55" opacity=".35"`));
    }
  if (I.drip)
    for (const b of ledges)
      if (r() < 0.45)
        onLedge(b, (u, w) => out.push(poly([[u - 0.8, w], [u + 0.8, w], [u + 0.5, w + span(r, 5, 11)], [u - 0.2, w + span(r, 5, 11)]], `fill="${I.drip}" opacity=".4"`)));
  if (I.frost)
    for (let k = 0; k < 7; k++) {
      const u = span(r, 2, 30), w = span(r, 0, PH);
      over.push(path([[u, w], [u + 0.5, w]], `stroke="${I.frost}" stroke-width="1" opacity=".6"`));
    }
  if (I.glass) {
    for (let k = 0; k < 5; k++) {
      const u = span(r, 4, 26), w = span(r, 0, PH);
      over.push(`<path d="M${r2(u)} ${r2(w + 1)}q${r2(span(r, 1.6, 2.6))} ${r2(-span(r, 1.4, 2.4))} ${r2(span(r, 4, 5.6))} -.4" stroke="${hi}" stroke-width=".8" fill="none" opacity=".5" stroke-linecap="round"/>`);
    }
    for (let k = 0; k < 5; k++) {
      const u = span(r, 3, 28), w = span(r, 0, PH);
      over.push(path([[u, w], [u + 1.4, w - 0.7]], `stroke="${I.glint}" stroke-width=".7" opacity=".55"`));
    }
    const u = span(r, 8, 22), w = span(r, 0, PH);
    over.push(path([[u, w], [u + 1.6, w + 1], [u + 3, w + 0.6], [u + 4.2, w + 1.8], [u + 5, w + 3.4]], `stroke="${I.ember}" stroke-width=".9" opacity=".6"`));
  }
  if (kind === 'snow') {
    const [sn, sh] = SNOW[side];
    // snow on the block tops, and spindrift streaking down below it
    for (const b of ledges)
      if (r() < 0.7)
        onLedge(b, (u, w, len, k) => {
          const L = len * 1.25, dw = k * L;
          over.push(`<path d="M${r2(u - L)} ${r2(w - dw - 0.6)}L${r2(u + L)} ${r2(w + dw - 0.6)}q${r2(-L * 0.4)} ${r2(span(r, 1.4, 2.2))} ${r2(-L)} ${r2(span(r, 0.6, 1.2) - dw)}q${r2(-L * 0.5)} .2 ${r2(-L)} ${r2(-span(r, 0.4, 1) - dw)}Z" fill="${sn}" opacity=".95"/>`);
          over.push(path([[u - L * 0.7, w - dw * 0.7 + 0.6], [u + L * 0.7, w + dw * 0.7 + 0.6]], `stroke="${sh}" stroke-width=".7" opacity=".8"`));
          if (r() < 0.6) over.push(path([[u + span(r, -1, 1), w + 1.2], [u + span(r, -1.5, 1.5), w + span(r, 2.5, 5)]], `stroke="${sn}" stroke-width=".9" opacity=".45"`));
        });
    // snow packed into one joint, all the way down
    const J = lines[1 + Math.floor(r() * (lines.length - 2))];
    over.push(path(along(J, 0, PH).map(([u, w]) => [u - 0.7, w]), `stroke="${sn}" stroke-width="1" opacity=".55"`));
    for (let k = 0; k < 6; k++) over.push(`<circle cx="${r2(span(r, 2, 30))}" cy="${r2(span(r, 0, PH))}" r=".55" fill="${sn}" opacity=".75"/>`);
  }
  return out.join('') + over.join('');
}

/** The bedding plane on a band's top edge, one half: the same shape on every face. */
function stratum(isle, side) {
  const I = ISLES[isle];
  const [, crack, hi, deep] = I[side];
  const lit = side === 'L';
  const r = rng(`stratum:${side}`); // shared by every island, so strata meet across tiles of any kind
  const lo = [[-1, 2.2], [0, 2.2]];
  for (let u = span(r, 2, 4); u < 30; u += span(r, 2, 4)) lo.push([u, span(r, 1.3, 3.1)]);
  lo.push([32, 2.2], [33, 2.2]);
  const out = [];
  out.push(poly([[-1, -3], [33, -3], ...lo.slice().reverse()], `fill="${crack}" opacity=".55"`));
  out.push(poly([...lo, ...lo.slice().reverse().map(([u, v]) => [u, v + 1.4])], `fill="${deep}" opacity=".35"`));
  // sandstone: a pale bed under the plane
  if (I.bed) out.push(poly([...lo.map(([u, v]) => [u, v + 1.6]), ...lo.slice().reverse().map(([u, v]) => [u, v + 4.2])], `fill="${I.bed[lit ? 0 : 1]}" opacity=".3"`));
  out.push(path([[-1, 0.35], [33, 0.35]], `stroke="${hi}" stroke-width=".7" opacity="${lit ? '.45' : '.3'}"`));
  return out.join('');
}

// Element opacity becomes fill- / stroke-opacity: the same look for a one-paint element, no
// compositing layer, and resvg panics on an opacity layer that lands wholly off the canvas
// (which the wrapped copies of the cliff above and below the band often do).
const flat = (svg) =>
  svg.replace(/<(path|circle|ellipse|rect)\b([^>]*?) opacity="([^"]*)"([^>]*)\/>/g, (m, tag, a, o, b) =>
    `<${tag}${a}${/fill="none"/.test(a + b) ? ' stroke-opacity' : ' fill-opacity'}="${o}"${b}/>`);

const head = `<svg xmlns="http://www.w3.org/2000/svg" width="64" height="40" viewBox="0 0 64 40">
  <!-- generated by tools/art/mountain-faces.mjs: re-cut it there, not in this file -->`;
const clip = `<clipPath id="band"><rect x="0" y="-3" width="32" height="13"/></clipPath>`;

function face(isle, kind, j) {
  const [bl] = ISLES[isle].L, [br] = ISLES[isle].R;
  const sub = (side) =>
    `<g transform="${SHEAR[side]}"><g clip-path="url(#band)">` +
    [-PH, 0, PH].map((o) => `<use href="#${side}" y="${o - 10 * j}"/>`).join('') + `</g></g>`;
  return `${head}
  <defs>${clip}<g id="L">${cliff(isle, kind, 'L')}</g><g id="R">${cliff(isle, kind, 'R')}</g></defs>
  <path d="M32 0 2 15 0 14 0 24 32 40Z" fill="${bl}"/><path d="M32 0 62 15 64 14 64 24 32 40Z" fill="${br}"/>
  ${sub('L')}${sub('R')}
</svg>
`;
}

function strata(isle) {
  return `${head}
  <defs>${clip}</defs>
  <g transform="${SHEAR.L}"><g clip-path="url(#band)">${stratum(isle, 'L')}</g></g>
  <g transform="${SHEAR.R}"><g clip-path="url(#band)">${stratum(isle, 'R')}</g></g>
</svg>
`;
}

let n = 0;
for (const isle of Object.keys(ISLES))
  for (const kind of ISLES[isle].snow ? ['rock', 'snow'] : ['rock']) {
    const key = `mtn_${isle}_${kind}`;
    for (let j = 0; j < BANDS; j++, n++) writeFileSync(resolve(OUT, `${key}_face${j ? j + 1 : ''}.svg`), flat(face(isle, kind, j)));
    writeFileSync(resolve(OUT, `${key}_strata.svg`), flat(strata(isle)));
    n++;
  }
console.log(`[mountain-faces] wrote ${n} files to assets/tiles`);
