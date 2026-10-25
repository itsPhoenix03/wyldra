// Bakes where the keeper's mine lantern hangs in every keeper frame.
//
//   node tools/art/lantern-anchors.mjs      → src/lanternAnchors.ts
//
// Underground the keeper carries a hand lantern (rig.ts setLantern). It is an
// overlay, not part of the frame art, so it needs to know, per frame, where the
// free hand is (standing / walking) and where the hip is (any other clip: the
// lantern is hooked on the belt so both hands are free).
//
//   hand  the free hand's centre: image left in the front and back views (tools
//         are held in the facing-side arm, image right), the near arm in the side
//         view (drawn after the torso: the far hand is hidden behind the body).
//         Base and walk frames only. The lantern is always drawn over the frame.
//   belt  a fixed point on the belt, image-left hip (front/back) or the near
//         hip just behind the buckle line (side), carried through the transform
//         of the group the belt is drawn in, so it follows bends, kneels and
//         crouches in every clip.
//
// Offsets are in sprite px relative to the rig sprite origin (canvas centre,
// FEET_OY = 45.5): every keeper frame keeps the body centred at x 16, including
// the 64-wide swing frames (viewBox -16..48).

import { readFileSync, readdirSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const ROOT = resolve(dirname(fileURLToPath(import.meta.url)), '../..');
const SPR = `${ROOT}/assets/sprites`;
const OUT = `${ROOT}/src/lanternAnchors.ts`;
const OX = 16, OY = 45.5;

// clips the rig plays on foot (row/swim never carry the lantern)
const CLIPS = ['walk', 'chop', 'mine', 'spick', 'slash', 'islash', 'punch', 'pickup', 'build', 'torch', 'meds',
  'jump', 'leap', 'shiver', 'sweat', 'injured'];
// belt hook point in each view's un-posed body (belt band is y 34..36)
const HIP = { front: [10.3, 36.1], back: [10.3, 36.1], side: [13.2, 36.4] };
// the base front pose has its own long-armed art with a path fist, not the template ellipse
const BASE_FRONT_HAND = [7.3, 40.3];

const I = [1, 0, 0, 1, 0, 0];
const mul = (a, b) => [a[0] * b[0] + a[2] * b[1], a[1] * b[0] + a[3] * b[1], a[0] * b[2] + a[2] * b[3], a[1] * b[2] + a[3] * b[3],
  a[0] * b[4] + a[2] * b[5] + a[4], a[1] * b[4] + a[3] * b[5] + a[5]];
function parseT(s) {
  let m = I;
  for (const [, fn, args] of s.matchAll(/(\w+)\(([^)]*)\)/g)) {
    const a = args.trim().split(/[ ,]+/).map(Number);
    if (fn === 'translate') m = mul(m, [1, 0, 0, 1, a[0], a[1] || 0]);
    else if (fn === 'scale') m = mul(m, [a[0], 0, 0, a[1] ?? a[0], 0, 0]);
    else if (fn === 'rotate') {
      const r = (a[0] * Math.PI) / 180, c = Math.cos(r), n = Math.sin(r), cx = a[1] || 0, cy = a[2] || 0;
      m = mul(mul(mul(m, [1, 0, 0, 1, cx, cy]), [c, n, -n, c, 0, 0]), [1, 0, 0, 1, -cx, -cy]);
    } else throw new Error(`unsupported transform ${fn}`);
  }
  return m;
}
const apply = (m, [x, y]) => [m[0] * x + m[2] * y + m[4], m[1] * x + m[3] * y + m[5]];
const off = ([x, y]) => [+(x - OX).toFixed(2), +(y - OY).toFixed(2)];

/** Walks the SVG once: the hand ellipses (absolute centres) and the belt group's transform. */
function scan(svg) {
  const stack = [I], hands = [];
  let belt = null;
  for (const [, close, tag, attrs] of svg.matchAll(/<(\/?)(g|ellipse|path)\b([^>]*?)\/?>/g)) {
    if (tag === 'g') {
      if (close) stack.pop();
      else { const t = attrs.match(/transform="([^"]+)"/); stack.push(mul(stack.at(-1), t ? parseT(t[1]) : I)); }
      continue;
    }
    const a = attrs.replace(/=" +/g, '="');
    if (tag === 'ellipse' && /rx="1.6" ry="2" fill="#f0c79c"/.test(a)) {
      const cx = +a.match(/cx="([^"]+)"/)[1], cy = +a.match(/cy="([^"]+)"/)[1];
      hands.push(apply(stack.at(-1), [cx, cy]));
    } else if (tag === 'path' && belt === null && /fill="#382e29"/.test(a) && /d="M ?(9 +34q|11\.1 34\.2Q)/.test(a)) {
      belt = stack.at(-1);
    }
  }
  return { hands, belt };
}

const views = [['front', 'keeper'], ['back', 'keeper_back'], ['side', 'keeper_side']];
const files = new Set(readdirSync(SPR));
const hand = {}, belt = {}, missing = [];
for (const [view, base] of views) {
  const frames = [base];
  for (const c of CLIPS)
    for (let i = 1; files.has(`${base}_${c}_${String(i).padStart(2, '0')}.svg`); i++) frames.push(`${base}_${c}_${String(i).padStart(2, '0')}`);
  for (const f of frames) {
    const { hands, belt: m } = scan(readFileSync(`${SPR}/${f}.svg`, 'utf8'));
    if (m) belt[f] = off(apply(m, HIP[view])); else missing.push(f);
    if (f === base || f.includes('_walk_')) {
      // front/back: image-left hand (smaller x); side: the near arm, drawn second
      const h = f === 'keeper' ? BASE_FRONT_HAND
        : view === 'side' ? hands[1] : hands.reduce((p, q) => (q[0] < p[0] ? q : p));
      if (!h) throw new Error(`${f}: no hand found`);
      hand[f] = off(h);
    }
  }
}
if (missing.length) throw new Error(`belt not found in: ${missing.join(' ')}`);

const rows = (o) => Object.entries(o).map(([k, [x, y]]) => `  ${k}: [${x}, ${y}],`).join('\n');
writeFileSync(OUT, `// GENERATED by tools/art/lantern-anchors.mjs from the keeper frame art. Do not edit.
// Where the mine lantern hangs in each keeper frame, in sprite px from the rig's
// sprite origin (rig.ts bodyRoot space, image space: the rig's flip mirrors it).

/** The free hand (standing and walk frames only). */
export const LANTERN_HAND: Record<string, readonly [number, number]> = {
${rows(hand)}
};

/** The belt hook at the hip (every on-foot frame). */
export const LANTERN_BELT: Record<string, readonly [number, number]> = {
${rows(belt)}
};
`);
console.log(`[lantern] ${Object.keys(hand).length} hand + ${Object.keys(belt).length} belt anchors → src/lanternAnchors.ts`);
