// Back and side views of the human characters, drawn from the same 32×48
// template every front frame uses, so they stay on-model.
//
//   node tools/art/human-views.mjs
//
// The rig and the folk view pick a view from the travel direction:
//   <char>            front (the pack's own art — not touched here)
//   <char>_back       walking away from the camera (up-screen)
//   <char>_side       walking left/right; drawn facing image RIGHT, flipped for left
// plus walk loops <char>_back_walk_NN / <char>_side_walk_NN for the characters
// that walk with clips (same frame count as their front walk).
//
// Side view: the character faces right, so the viewer sees their right side.
// Anything held in the left hand (staff, spear, stick, bundle) is on the far
// side, drawn behind the body. Back view: left and right swap, so props held
// in the front view's image-right hand move to image left.
//
// The keeper's frames carry <!--gear:under--> / <!--gear:over--> (under and
// over the arms) where keeperSkins.ts injects the armour and cloak drawn for
// that view; their shirt uses the art's own #456d89 and
// #688fa5 so the shirt recolour applies unchanged.

import { readFileSync, writeFileSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SPR = resolve(dirname(fileURLToPath(import.meta.url)), '../../assets/sprites');
const read = (n) => readFileSync(`${SPR}/${n}.svg`, 'utf8');
const write = (n, s) => writeFileSync(`${SPR}/${n}.svg`, s);
const r2 = (n) => +n.toFixed(2);
const pad = (i) => String(i + 1).padStart(2, '0');
const mix = (hex, to, t) => {
  const n = parseInt(hex.slice(1), 16);
  const ch = (s) => Math.round(((n >> s) & 255) * (1 - t) + to * t);
  return '#' + ((ch(16) << 16) | (ch(8) << 8) | ch(0)).toString(16).padStart(6, '0');
};
const ease = (t) => t * t * (3 - 2 * t);
const rad = (d) => (d * Math.PI) / 180;

// ── characters: template palette + what makes each one themselves ──────────
// hat: crown, outline, brim, band
const HAT = ['#8e7d59', '#514b3c', '#a29469', '#6e6146'];
const CHARS = {
  keeper:         { walk: 8, legs: '#394454', boots: '#382e29', shirt: '#456d89', sheen: '#a9c4c4', skin: '#d39a6e', skinO: '#8a5a42', skinH: '#f0c79c', hair: '#3b2620', hairHl: '#9a6a4a', hairStyle: 'swept', gear: true },
  villager:       { walk: 9, legs: '#534738', boots: '#3b3028', shirt: '#80634a', sheen: '#d0b27b', skin: '#bf8b63', skinO: '#896248', skinH: '#e1b18b', hair: '#4b382d', hairStyle: 'short', hat: HAT, prop: 'bundle' },
  villager2:      { walk: 9, legs: '#51404b', boots: '#3c3031', shirt: '#795668', sheen: '#d6a69a', skin: '#c89570', skinO: '#946b52', skinH: '#ecc7a2', hair: '#4c3229', hairStyle: 'long', lock: '#5d4031', apron: '#84586a', apronLine: '#a7747e' },
  woods_child:    { walk: 8, frontWalk: true, legs: '#625344', boots: '#44382c', shirt: '#50865d', sheen: '#b2ba79', skin: '#bb8960', skinO: '#825a44', skinH: '#e3b083', hair: '#6b462d', hairStyle: 'tousled', collar: '#b1bc7a', prop: 'stick' },
  ashmark_hunter: { walk: 8, frontWalk: true, legs: '#534637', boots: '#302b25', shirt: '#66553e', sheen: '#923c2d', skin: '#ab7452', skinO: '#75503c', skinH: '#d49e71', hair: '#292824', hairStyle: 'short', scarf: '#963d2f', hide: '#574b37', pouch: '#4b3c2b', prop: 'spear' },
  elder_yvenne:   { walk: 8, frontWalk: true, legs: '#48554d', boots: '#303933', shirt: '#356158', sheen: '#93a77d', skin: '#a47256', skinO: '#6d4a3d', skinH: '#c89470', hair: '#d2c8b5', hairStyle: 'long', lock: '#c3b9aa', robe: '#63856c', robeLine: '#a8b492', prop: 'staff' },
  // medics: the long coat's sleeves are the arms' sleeves, so `shirt` is the coat colour
  medic:          { walk: 0, legs: '#5b5747', boots: '#49392d', shirt: '#e3d9be', sheen: '#4f8b6f', skin: '#aa744e', skinO: '#76523d', skinH: '#d7a177', hair: '#414b3c', hairStyle: 'short', coat: '#e3d9be', coatLine: '#a99d85', strap: '#8b6c4c', bag: '#785b3f', bagMark: '#e9e4c8', trim: '#6e9b77' },
  medic_snow:     { walk: 0, legs: '#596976', boots: '#3c4950', shirt: '#b5d3dd', sheen: '#5d95ae', skin: '#b88764', skinO: '#835e48', skinH: '#e3b48d', hair: '#444f57', hairStyle: 'short', coat: '#b5d3dd', coatLine: '#7199a8', collarV: '#eaf1ed', strap: '#8b6c4c', bag: '#785b3f', bagMark: '#edf4ef', hood: '#dce8e8', hoodLine: '#779bac' },
  // village folk recoloured from the villagers' art (tools/art/folk-variants.mjs, same colours)
  elder_woods:    { walk: 9, legs: '#534738', boots: '#3b3028', shirt: '#5b6a4b', sheen: '#a7b48a', skin: '#bf8b63', skinO: '#896248', skinH: '#e1b18b', hair: '#a39d92', hairStyle: 'short', hat: ['#6a5f4b', '#3e382d', '#7f7359', '#554b3b'], prop: 'bundle' },
  villager_snow:  { walk: 9, legs: '#46525c', boots: '#3b3028', shirt: '#5d7787', sheen: '#c3d5db', skin: '#bf8b63', skinO: '#896248', skinH: '#e1b18b', hair: '#4b382d', hairStyle: 'short', hat: ['#d8d1c2', '#8d8576', '#ebe6da', '#b3ab9b'], prop: 'bundle' },
  elder_spire:    { walk: 9, legs: '#46525c', boots: '#3c3031', shirt: '#566f7d', sheen: '#b6cad2', skin: '#c89570', skinO: '#946b52', skinH: '#ecc7a2', hair: '#c9c3b7', hairStyle: 'long', lock: '#aea89c', apron: '#d9d1c1', apronLine: '#a59d8d' },
  // the pack's static Ashmark shaman (the Dunes elder) and the Marsh's tribal hunter: walks drawn from their front art
  shaman:         { walk: 8, frontWalk: true, legs: '#343c43', boots: '#262d31', shirt: '#505b65', sheen: '#7db7ae', skin: '#9c684b', skinO: '#674833', skinH: '#c99269', hair: '#2d2827', hairStyle: 'short', headdress: '#e4dcc5', collar: '#83bdb2', prop: 'staff', orb: '#74b7b0' },
  tribal:         { walk: 8, frontWalk: true, legs: '#695039', boots: '#3b3028', shirt: '#93673e', sheen: '#b8483b', skin: '#b37950', skinO: '#794e38', skinH: '#d99b68', hair: '#32251f', hairStyle: 'short', scarf: '#aa4333', pouch: '#815b3d', prop: 'spear', feathers: '#cb6048' },
};

/** Face colours and traits read from the character's A+ front art (tools/art/faces-aplus.mjs),
 *  so the side view's eye, brows, lips and marks match the front. */
function faceOf(name) {
  const s = read(name);
  const pick = (re) => s.match(re)?.[1] ?? null;
  const maleLip = pick(/<path d="M14\.4 21\.35Q16 21\.8 17\.6 21\.35" fill="none" stroke="(#[0-9a-f]{6})"/);
  const f = {
    iris: pick(/<circle cx="13\.1" cy="16\.35" r="1" fill="(#[0-9a-f]{6})"/),
    lid: pick(/<path d="M14\.9 16\.6Q13\.1 14\.4 11\.15 16\.1" fill="none" stroke="(#[0-9a-f]{6})"/),
    under: pick(/<path d="M14\.1 17\.25Q12\.9 17\.75 11\.7 16\.9" fill="none" stroke="(#[0-9a-f]{6})"/),
    brow: pick(/<path d="M14\.8 13\.3Q12\.8 12\.1 11\.1 13\.4[^"]*" fill="(#[0-9a-f]{6})"/),
    lip: maleLip ?? pick(/<path d="M14\.4 21\.3Q15\.3[^"]*" fill="(#[0-9a-f]{6})"/),
    lipLow: maleLip ? pick(/<path d="M15\.2 22\.05[^"]*" fill="none" stroke="(#[0-9a-f]{6})"/) : pick(/<path d="M14\.6 21\.4Q16 22\.5[^"]*" fill="(#[0-9a-f]{6})"/),
    lips: !maleLip,                                                       // drawn as filled lips
    lash: s.includes('<path d="M11.35 16.2L10.8 15.8"'),
    crease: s.includes('<path d="M14.4 15.3Q13.1 14.3 11.5 15.2"'),
    freckles: pick(/<circle cx="12\.4" cy="18\.3" r="\.17" fill="(#[0-9a-f]{6})"/),
    stubble: pick(/<path d="M10\.3 19Q10\.8 22[^"]*" fill="(#[0-9a-f]{6})"/),
    age: pick(/<path d="M10\.5 15\.5L9\.7 15\.1[^"]*" fill="none" stroke="(#[0-9a-f]{6})"/),
    paint: pick(/<path d="M10\.8 18\.6H12\.8[^"]*" stroke="(#[0-9a-f]{6})"/),
  };
  if (!f.iris || !f.lid || !f.under || !f.brow || !f.lip || !f.lipLow) throw new Error(`${name}: A+ face not found in front art`);
  return f;
}
for (const [name, c] of Object.entries(CHARS)) c.face = faceOf(name);

/** Root tag (and the child's scale wrapper) copied from the character's own art. */
function frameOf(name) {
  const src = read(name);
  const open = src.slice(0, src.indexOf('>') + 1);
  const wrap = /^<svg[^>]*>\s*(<g transform="[^"]*">)/.exec(src);
  return (body) => `${open}${wrap ? wrap[1] : ''}\n${body}\n${wrap ? '</g>' : ''}</svg>\n`;
}

const SHADOW = '<ellipse cx="16" cy="45.5" rx="10" ry="2" fill="#16151a" opacity=".25"/>';
const DARK = (d, w) => `<path d="${d}" fill="none" stroke="#000" stroke-opacity=".2" stroke-width="${w}" stroke-linecap="round" stroke-linejoin="round"/>`;

// ── limbs (stroke-drawn, the same construction keeper-frames.mjs uses) ──────
const SEG = 4.25;
/** Hip → knee → ankle, two-bone IK; the knee bends toward +x. Returns path d. */
const legPath = (hip, ankle) => {
  const [hx, hy] = hip, [ax, ay] = ankle;
  const vx = ax - hx, vy = ay - hy, d = Math.hypot(vx, vy) || 1;
  const reach = Math.min(d, 2 * SEG - 0.01);
  const h = Math.sqrt(Math.max(0, SEG * SEG - (reach / 2) ** 2));
  const kx = hx + vx / 2 + (vy / d) * h, ky = hy + vy / 2 - (vx / d) * h;
  return `M${r2(hx)} ${r2(hy)}L${r2(kx)} ${r2(ky)}L${r2(ax)} ${r2(ay)}`;
};
const trousers = (c, p, far) =>
  `<path d="${p}" fill="none" stroke="#302d2b" stroke-width="5.4" stroke-linecap="round" stroke-linejoin="round"/>` +
  `<path d="${p}" fill="none" stroke="${c.legs}" stroke-width="4.5" stroke-linecap="round" stroke-linejoin="round"/>` +
  (far ? DARK(p, 4.5) : '');
/** Arm from shoulder through elbow to hand; sleeve to the elbow, skin below. */
const arm = (c, s, e, h, far) => {
  const up = `M${r2(s[0])} ${r2(s[1])}L${r2(e[0])} ${r2(e[1])}`;
  const fo = `M${r2(e[0])} ${r2(e[1])}L${r2(h[0])} ${r2(h[1])}`;
  return `<path d="${up}" fill="none" stroke="#433d38" stroke-width="4.6" stroke-linecap="round"/>` +
    `<path d="${up}" fill="none" stroke="${c.shirt}" stroke-width="3.9" stroke-linecap="round"/>` +
    `<path d="${fo}" fill="none" stroke="${c.skinO}" stroke-width="3.5" stroke-linecap="round"/>` +
    `<path d="${fo}" fill="none" stroke="${c.skin}" stroke-width="2.8" stroke-linecap="round"/>` +
    `<ellipse cx="${r2(h[0])}" cy="${r2(h[1])}" rx="1.6" ry="2" fill="${c.skinH}" stroke="${c.skinO}" stroke-width=".4"/>` +
    (far ? DARK(`${up}L${r2(h[0])} ${r2(h[1])}`, 3.6) : '');
};
/** Arm hanging from `s`, swung `a`° (+ = toward +x), forearm bent a further `b`°. */
const swingArm = (c, s, a, b, far, len = 5.2) => {
  const e = [s[0] + Math.sin(rad(a)) * len, s[1] + Math.cos(rad(a)) * len];
  const h = [e[0] + Math.sin(rad(a + b)) * len, e[1] + Math.cos(rad(a + b)) * len];
  return { svg: arm(c, s, e, h, far), hand: h };
};

// ════════════════════════════════════════════════════════════════════════════
// BACK VIEW
// ════════════════════════════════════════════════════════════════════════════
const backTorso = (c) =>
  `<path d="m10 24q6-2.5 12 0l2 13q-8 4-16 0Z" fill="${c.shirt}" stroke="#3f3b37" stroke-width=".55"/>` +
  `<path d="m10.4 24.6 4.4 1.6.6 9-6.4.6Zm11.2 0-4.4 1.6-.6 9 6.4.6Z" fill="#000" opacity=".07"/>` +
  `<path d="M16 25.2V35" stroke="#000" stroke-opacity=".14" stroke-width=".5"/>` +
  `<path d="M9 34q7 2 14 0v2q-7 2-14 0Z" fill="${c.boots}"/>`;

/** Back-of-torso pieces worn over the shirt (before the arms). */
function backWear(c) {
  let s = '';
  if (c.apron) // apron strings tied in a bow at the small of the back
    s += `<path d="M14.2 34.6Q12.4 33.2 12 35.2Q12.6 36.6 14.6 35.6ZM17.8 34.6Q19.6 33.2 20 35.2Q19.4 36.6 17.4 35.6Z" fill="${c.apron}" stroke="${mix(c.apron, 0, .35)}" stroke-width=".4"/>` +
      `<path d="m15 35.6-1 3.6m3-3.6 1 3.6" stroke="${c.apron}" stroke-width=".8" stroke-linecap="round"/><circle cx="16" cy="35.2" r=".9" fill="${c.apron}"/>`;
  if (c.robe) // the robe hangs down the back too, without the front's trim
    s += `<path d="M10 25q6 2.4 12 0l3 15-17 0Z" fill="${c.robe}"/><path d="M9.4 34.6q6.6 1.6 13.2 0" fill="none" stroke="${c.robeLine}" stroke-width=".7"/>`;
  if (c.hide) // the hide over the shoulder, seen from behind
    s += `<path d="m25 23-4 3-1 10 4 2 3-13Z" fill="${c.hide}"/><path d="m24 26-1 9" stroke="${mix(c.hide, 255, .15)}" stroke-width=".5"/>`;
  if (c.pouch) s += `<path d="M8 35h4v5H8Z" fill="${c.pouch}"/>`;
  if (c.coat) // the long coat from behind, a vent up the back; strap over the right shoulder to the bag on the left hip
    s += `<path d="M8.6 23.4Q16 20.4 23.4 23.4L25.6 41.4H6.4Z" fill="${c.coat}" stroke="${c.coatLine}" stroke-width=".6"/>` +
      `<path d="M16 33.6V41.2M11 24.6 9.6 40.6M21 24.6l1.4 16" fill="none" stroke="${c.coatLine}" stroke-width=".45" opacity=".7"/>` +
      `<path d="m20.6 24.6-9.4 9.6" stroke="${c.strap}" stroke-width="1.5"/>` +
      `<path d="M4.6 33h6.6v7H4.6Z" fill="${c.bag}"/><path d="M4.8 34.6h6.2" stroke="${mix(c.bag, 0, .25)}" stroke-width=".6"/>`;
  if (c.gear && !c.hide) s += `<path d="M11.2 35h3v3.4h-3Z" fill="#ad9472"/>`;   // keeper's belt pouch, now on the left
  return s;
}

const backHead = (c) => {
  const hl = mix(c.hair, 255, .22), lo = mix(c.hair, 0, .25);
  let s = `<path d="M9 16Q7 15 7 17.3Q7.2 19.4 9.4 19.7M23 16Q25 15 25 17.3Q24.8 19.4 22.6 19.7" fill="${c.skin}" stroke="${c.skinO}" stroke-width=".55"/>` +
    `<path d="M9 14Q9 7 16 6Q23 6 24 14L23 19Q21 24 16 24Q11 24 9 19Z" fill="${c.skin}" stroke="${c.skinO}" stroke-width=".45"/>` +
    `<path d="M11 21.2Q16 23.4 21 21.2Q20 23.6 16 24Q12 23.6 11 21.2Z" fill="${c.skinO}" opacity=".3"/>`;
  if (c.hairStyle === 'long') {
    s += `<path d="M8.4 15Q8 6.6 16 5.6Q24 6.6 23.6 15L25 23Q23.6 28.6 16 29.2Q8.4 28.6 7 23Z" fill="${c.hair}" stroke="${lo}" stroke-width=".5"/>` +
      `<path d="M10 21Q10.6 26 13 28M22 21Q21.4 26 19 28M16 12V28.4" fill="none" stroke="${lo}" stroke-width=".4" opacity=".7"/>` +
      `<path d="M11.4 10.6Q16 6.8 20.6 10.6" fill="none" stroke="${hl}" stroke-width=".7" opacity=".55"/>`;
  } else if (c.hairStyle === 'tousled') {
    s += `<path d="M8.6 15Q8.4 6 16 5.4Q23.8 6 23.6 15L23 18.6 21.4 17.8 20 20.6 18 19.2 16 21 14 19.2 12 20.6 10.6 17.8 9 18.6Z" fill="${c.hair}" stroke="${lo}" stroke-width=".45"/>` +
      `<path d="M15.2 5.8Q15.6 4 17.4 4.4Q16.4 4.8 16.6 5.8Z" fill="${c.hair}" stroke="${lo}" stroke-width=".35"/>` +
      `<path d="M11.4 10.6Q16 7 20.6 10.6" fill="none" stroke="${hl}" stroke-width=".6" opacity=".5"/>`;
  } else {
    s += `<path d="M8.8 14.6Q8.6 6.4 16 5.8Q23.4 6.4 23.2 14.6L22.8 18.4Q21.6 20.8 19.6 20.4Q17.8 21.2 16 20.6Q14.2 21.2 12.4 20.4Q10.4 20.8 9.2 18.4Z" fill="${c.hair}" stroke="${lo}" stroke-width=".45"/>` +
      `<path d="M10.4 17.6Q16 19.6 21.6 17.6" fill="none" stroke="${lo}" stroke-width=".5" opacity=".6"/>` +
      `<path d="M11.4 10.6Q16 7 20.6 10.6" fill="none" stroke="${hl}" stroke-width=".6" opacity=".5"/>` +
      `<path d="M15.2 9.6Q16.4 8.8 17.2 10" fill="none" stroke="${lo}" stroke-width=".4" opacity=".6"/>`;
  }
  if (c.hat) {
    const [crown, line, brim, band] = c.hat;
    s += `<path d="M7 8q9-6 18 0l1 3-20 1Z" fill="${crown}" stroke="${line}" stroke-width=".7"/><path d="M5 11q11-2 22 0l-2 2H7Z" fill="${brim}"/>` +
      `<path d="M7.6 9.4q8.4-2 16.8 0" fill="none" stroke="${band}" stroke-width=".9"/>`;
  }
  if (c.headdress) // the shaman's feather crown, seen from behind
    s += `<path d="M8.2 13.2Q8 6.4 11 4.6L12.6 9 14.4 3.4 16 8.4 17.6 3.4 19.4 9 21 4.6Q24 6.4 23.8 13.2Q16 10.8 8.2 13.2Z" fill="${c.headdress}" stroke="${mix(c.headdress, 0, .3)}" stroke-width=".5"/>`;
  if (c.feathers) // three feathers stuck in the hair
    s += `<path d="M11 7.4 10 2M16 6.2V.6M21 7.4 22 2" stroke="${c.feathers}" stroke-width="1.8"/>`;
  if (c.scarf) // neck wrap, knotted at the back
    s += `<path d="M11 22.4q5 2.2 10 0l.6 2.4q-5.6 2-11.2 0Z" fill="${c.scarf}"/><path d="m15 24.4-1.4 4.4 1.8.4 1-4.2Zm2 0 1.6 3.6-1.6.6-.8-3.8Z" fill="${mix(c.scarf, 0, .15)}"/>`;
  if (c.collar) s += `<path d="m11 24.6 5 1 5-1" fill="none" stroke="${c.collar}" stroke-width=".8"/>`;
  if (c.collarV) s += `<path d="M10.2 23.4Q16 25.6 21.8 23.4" fill="none" stroke="${c.collarV}" stroke-width="1.4"/>`;
  if (c.hood) s += `<path d="M8 12q8-8 16 0l1 4q-9-3.4-18 0Z" fill="${c.hood}" stroke="${c.hoodLine}" stroke-width=".7"/><path d="M10 11.6q6-4.4 12 0" fill="none" stroke="${c.hoodLine}" stroke-width=".4" opacity=".6"/>`;
  return s;
};

/** Props held in the character's left hand: image left in the back view. */
function backProp(c, hand) {
  const [x, y] = hand;
  if (c.prop === 'staff') return `<path d="m${r2(x + 1)} ${r2(y - 17)}-4 19" stroke="#785639" stroke-width="1.7"/>` + (c.orb
    ? `<circle cx="${r2(x + 1)}" cy="${r2(y - 19)}" r="2.4" fill="${c.orb}"/><path d="M${r2(x + .6)} ${r2(y - 20.4)}q1.2.6.6 1.8" fill="none" stroke="#e5f3d5" stroke-width=".7"/>`
    : `<path d="m${r2(x + 1)} ${r2(y - 21)}-4 5 4 0Z" fill="#9fc1c8"/>`);
  if (c.prop === 'spear') return `<path d="M${r2(x + 1.5)} ${r2(y - 15)} ${r2(x - 2.5)} ${r2(y - 28)}" stroke="#8b663f" stroke-width="1.5"/><path d="m${r2(x - 1.5)} ${r2(y - 25)}-3-5m2 4-3 0" stroke="#d6c692" stroke-width="1"/>`;
  if (c.prop === 'stick') return `<path d="M${r2(x + 1)} ${r2(y)}q-5 1-7-3" fill="none" stroke="#d6a076" stroke-width="1.6"/>`;
  if (c.prop === 'bundle') return `<path d="m${r2(x - 1)} ${r2(y - 6)}-5 0-1 5 6 1Z" fill="#c3a16b" stroke="#765e3e" stroke-width=".7"/><path d="m${r2(x - 2)} ${r2(y - 5)}-1 3m-2-3-1 2" stroke="#e2ca91" stroke-width=".6"/>`;
  return '';
}

// back legs: feet stay under the hips; the stepping foot lifts and the forward
// foot sits a touch higher on screen (further from the camera).
const BACK_ANKLE = { L: [12.4, 42], R: [19.6, 42] };
const backBoot = (c, x, y, lift) =>
  `<path d="M${r2(x - 2.7)} ${r2(y)}h5.4l.4 2.6q-3.1 1.1-6.2 0Z" fill="${c.boots}" stroke="#292623" stroke-width=".55"/>` +
  (lift > .3 ? `<path d="M${r2(x - 2.4)} ${r2(y + 2.2)}h4.8" stroke="#292623" stroke-width=".6" opacity=".7"/>` : '');
const backLeg = (c, side, dy, fwd, lift) => {
  const [bx, by] = BACK_ANKLE[side];
  const hip = [side === 'L' ? 12.6 : 19.4, 34 + dy];
  const ankle = [bx, by - lift - fwd * 0.45];
  return trousers(c, legPath(hip, ankle).replace(/L([\d.]+) ([\d.]+)L/, (_, kx, ky) => `L${r2((+kx + hip[0] + ankle[0]) / 3)} ${ky}L`), false) +
    backBoot(c, ankle[0], ankle[1], lift);
};

function backFrame(c, { dy = 0, l = [0, 0], r = [0, 0], swing = 0 } = {}) {
  // arms swing a little against the legs; the left hand carries any prop
  const la = swingArm(c, [10.4, 25.2], -9 + swing * 2, 6, false);
  const ra = swingArm(c, [21.6, 25.2], 9 + swing * 2, -6, false);
  const arms = la.svg + ra.svg, lh = la.hand;
  return SHADOW + backLeg(c, 'L', dy, l[0], l[1]) + backLeg(c, 'R', dy, r[0], r[1]) +
    `<g transform="translate(0 ${r2(dy)})">` + backTorso(c) + backWear(c) + (c.gear ? '<!--gear:under-->' : '') + arms +
    (c.gear ? '<!--gear:over-->' : '') +
    `<path d="M14 22h4v3h-4Z" fill="${c.skin}"/>` + backHead(c) + backProp(c, lh) + '</g>';
}

// ════════════════════════════════════════════════════════════════════════════
// SIDE VIEW (facing image right)
// ════════════════════════════════════════════════════════════════════════════
const SIDE_HIP = [16, 34];
const sideBoot = (c, x, y, tilt, far) =>
  `<g transform="rotate(${r2(tilt)} ${r2(x)} ${r2(y)})"><path d="M${r2(x - 2.5)} ${r2(y - 1)}h3.6q.6 1.4 2.8 1.9 1.2.4.9 1.7H${r2(x - 2.7)}q-.4-1.6.2-3.6Z" fill="${c.boots}" stroke="#292623" stroke-width=".55"/>` +
  (far ? `<path d="M${r2(x - 2.5)} ${r2(y - 1)}h3.6q.6 1.4 2.8 1.9 1.2.4.9 1.7H${r2(x - 2.7)}q-.4-1.6.2-3.6Z" fill="#000" opacity=".2"/>` : '') + '</g>';
const sideLeg = (c, dy, [dx, lift], far) => {
  const hip = [SIDE_HIP[0] + (far ? -0.6 : 0.6), SIDE_HIP[1] + dy];
  const ankle = [16 + dx, 42 - lift];
  // toe drops as the foot leaves the ground and comes through
  const tilt = lift > 0.05 ? (dx < 0 ? 22 * Math.min(1, lift / 1.4) : -8 * Math.min(1, lift / 1.4)) : 0;
  return trousers(c, legPath(hip, ankle), far) + sideBoot(c, ankle[0], ankle[1], tilt, far);
};
const sideTorso = (c) =>
  `<path d="M12.4 24.6Q16.2 22.8 19.8 24.4L21 36.6Q16.2 38.6 11.2 36.8Z" fill="${c.shirt}" stroke="#3f3b37" stroke-width=".55"/>` +
  `<path d="M16.6 25 19.6 25.2 20.4 35.6 17 35.8Z" fill="${c.sheen}" opacity=".25"/>` +
  `<path d="M12.6 25.4 14 25.2 13 36.4 11.6 36.2Z" fill="#000" opacity=".1"/>` +
  `<path d="M11.1 34.2Q16.1 35.8 21 34.2V36.2Q16.1 37.8 11.1 36.2Z" fill="${c.boots}"/>` +
  `<path d="M19.4 34.6h1.5v1.9h-1.5Z" fill="#c4a879"/>`;

function sideWear(c) {
  let s = '';
  if (c.gear) // vest trim along the front edge and the collar
    s += `<path d="m19.2 25.4 1.2-.4.9 9-1.4.2Z" fill="#688fa5"/><path d="m17.4 24.6 2.4 2" fill="none" stroke="#d3cec0" stroke-width=".7"/>`;
  if (c.apron)
    s += `<path d="M19.6 27.4 21.4 27 24 40.2 18.6 40.4Z" fill="${c.apron}"/><path d="m19.4 32 2.6-.6m-2.2 4.4 2.8-.6" stroke="${c.apronLine}" stroke-width=".7"/>` +
      `<path d="M11.6 34.8Q16 36.4 20.6 34.8" fill="none" stroke="${c.apron}" stroke-width=".7"/>`;
  if (c.robe)
    s += `<path d="M12.2 25.6Q16.4 23.6 20.2 25.2L23 40H9.6Z" fill="${c.robe}"/><path d="M20 25.6 22.4 39.4M10.2 34.8q6.4 1.6 12.4 0" fill="none" stroke="${c.robeLine}" stroke-width=".7"/>`;
  if (c.hide) s += `<path d="M10 23.6 13 24.4 13.4 36.6 10.2 37.6 8.6 30Z" fill="${c.hide}"/><path d="m10.6 26.4.6 9" stroke="${mix(c.hide, 255, .15)}" stroke-width=".5"/>`;
  if (c.pouch) s += `<path d="M17 35.6h3.4v4.4H17Z" fill="${c.pouch}"/>`;
  if (c.collar) s += `<path d="m16.4 24.8 3.4 1.6" fill="none" stroke="${c.collar}" stroke-width=".8"/>`;
  if (c.coat)
    s += `<path d="M12 23.8Q16.4 22 20.2 24L22.8 41.4H9.4Z" fill="${c.coat}" stroke="${c.coatLine}" stroke-width=".6"/>` +
      `<path d="M19.8 25.2 22 40.8M10.6 33.4 9.8 41" fill="none" stroke="${c.coatLine}" stroke-width=".45" opacity=".7"/>` +
      `<path d="m16.8 24.4 3.6 7.4" stroke="${c.strap}" stroke-width="1.5"/>` +
      (c.collarV ? `<path d="M14.4 23.4Q17.6 24.6 19.8 26.8" fill="none" stroke="${c.collarV}" stroke-width="1.4"/>` : '') +
      (c.trim ? `<path d="m18.6 29 2.2.6" stroke="${c.trim}" stroke-width=".8"/>` : '');
  return s;
}

/** The A+ face in profile: almond eye with a coloured iris and catchlight, tapered brow,
 *  lips, and the character's own marks — all coloured from its front art (c.face). */
function sideFace(c, face) {
  const f = c.face;
  // upper lid height: lower = more closed (the iris is clipped by the eye white)
  const lid = { grit: 15.4, chatter: 15.75 }[face] ?? 14.95;
  const white = `M20.3 16.45Q21.3 ${lid} 22.6 15.95Q21.6 17.3 20.3 16.45Z`;
  let s = face === 'wince'
    ? `<path d="M20.5 15.75L22.35 16.4L20.6 17.05" fill="none" stroke="${f.lid}" stroke-width=".6" stroke-linecap="round" stroke-linejoin="round"/>`
    : `<clipPath id="se"><path d="${white}"/></clipPath><path d="${white}" fill="#f4eadb"/>` +
      `<g clip-path="url(#se)"><circle cx="21.75" cy="16.2" r=".9" fill="${f.iris}"/><circle cx="21.85" cy="16.2" r=".47" fill="#15181a"/><circle cx="21.45" cy="15.8" r=".3" fill="#fff"/></g>` +
      `<path d="M20.1 16.55Q21.3 ${r2(lid - .1)} 22.75 15.85" fill="none" stroke="${f.lid}" stroke-width=".65" stroke-linecap="round"/>` +
      `<path d="M20.7 16.95Q21.6 17.25 22.4 16.75" fill="none" stroke="${f.under}" stroke-width=".3"/>`;
  if (f.lash) s += `<path d="M20.2 16.5L19.65 16.1M20.55 16L20.15 15.55" fill="none" stroke="${f.lid}" stroke-width=".45" stroke-linecap="round"/>`;
  if (f.crease && face !== 'wince') s += `<path d="M20.5 15.35Q21.4 14.7 22.4 15.2" fill="none" stroke="${c.skinO}" stroke-width=".3" opacity=".7"/>`;
  // brow: thick at the front (over the nose), tapering back; women's a touch finer
  const [y0, qy, y1, by1, bqy, by0] = { grit: [13.9, 12.55, 13.4, 13.7, 13.25, 14.45], wince: [12.9, 12.25, 13.8, 14.1, 13, 13.5] }[face]
    ?? [13.4, 12.35, 13.5, 13.8, 13.05, 14];
  const t = f.lips ? .08 : 0;
  s += `<path d="M22.9 ${y0}Q21.4 ${qy} 19.9 ${y1}L20 ${r2(by1 - t)}Q21.4 ${r2(bqy - t)} 22.85 ${r2(by0 - t)}Z" fill="${f.brow}"/>`;
  // nose tip light and nostril
  s += `<ellipse cx="24.05" cy="17.3" rx=".45" ry=".3" fill="${c.skinH}" opacity=".65"/><circle cx="23.45" cy="18" r=".22" fill="${c.skinO}" opacity=".7"/>`;
  // mouth
  if (face === 'open')
    s += `<path d="M21.85 20.95Q22.75 20.7 23.1 21.25Q22.6 22.15 21.85 21.65Q21.6 21.2 21.85 20.95Z" fill="#4a2420" stroke="${f.lip}" stroke-width=".3"/>`;
  else if (face === 'chatter')
    s += `<path d="M21.6 20.9H23.15L23.05 21.85H21.75Z" fill="#f3e6d2" stroke="${f.lip}" stroke-width=".38" stroke-linejoin="round"/><path d="M22.1 20.95V21.8M22.6 20.95V21.8" stroke="${f.lip}" stroke-width=".2"/>`;
  else if (face === 'grit' || face === 'wince')
    s += `<path d="M21.6 21.2H23.15" stroke="${f.lip}" stroke-width=".6" stroke-linecap="round"/><path d="M21.6 21.2l-.25.22" stroke="${f.lip}" stroke-width=".32" stroke-linecap="round"/>`;
  else if (f.lips)
    s += `<path d="M21.7 21.05Q22.3 20.75 22.7 20.95Q23 20.85 23.25 21.05Q22.5 21.3 21.7 21.05Z" fill="${f.lip}"/><path d="M21.85 21.15Q22.6 21.95 23.15 21.15Q22.5 21.4 21.85 21.15Z" fill="${f.lipLow}"/>`;
  else
    s += `<path d="M21.75 21.1Q22.45 21.4 23.15 21.05" fill="none" stroke="${f.lip}" stroke-width=".55" stroke-linecap="round"/><path d="M21.75 21.1l-.22-.18" stroke="${f.lip}" stroke-width=".32" stroke-linecap="round"/>` +
      `<path d="M22.15 21.75Q22.6 21.9 23 21.72" fill="none" stroke="${f.lipLow}" stroke-width=".35" stroke-linecap="round" opacity=".75"/>`;
  // what each life has left on the face, as in the front art
  if (f.freckles) s += [[20.2, 18.5], [21, 18.9], [20.5, 19.35]].map(([x, y]) => `<circle cx="${x}" cy="${y}" r=".17" fill="${f.freckles}" opacity=".6"/>`).join('');
  if (f.stubble)
    s += `<path d="M16.6 20.2Q17.2 23.2 19.8 24Q22.4 24.2 23.4 21.6L23.4 20.6Q22.4 21.5 21.2 21.3Q19.6 21.2 18.6 20.4Q17.6 19.8 16.6 20.2Z" fill="${f.stubble}" opacity=".24"/>` +
      `<path d="M21.6 20.55Q22.6 20.25 23.45 20.6L23.4 20.9Q22.6 20.6 21.7 20.9Z" fill="${f.stubble}" opacity=".4"/>` +
      [[18.2, 21.6], [19.4, 22.6], [20.8, 23.2], [22.2, 22.8]].map(([x, y]) => `<circle cx="${x}" cy="${y}" r=".18" fill="${f.stubble}" opacity=".45"/>`).join('');
  if (f.age)
    s += `<path d="M19.6 15.4L18.9 15.1M19.6 16.4L18.9 16.7M19.6 11.2Q21 10.8 22.6 11.3M19.9 12.1Q21.1 11.8 22.4 12.2M22.4 19.6Q21.5 20.5 21.8 21.6" fill="none" stroke="${f.age}" stroke-width=".3" opacity=".6"/>`;
  if (f.paint) s += `<path d="M19.6 18.6H21.4M19.8 19.6H21.6" stroke="${f.paint}" stroke-width=".55" stroke-linecap="round"/>`;
  return s;
}

/** face: 'calm' | 'open' (mouth) | 'grit' (brow down, jaw set) | 'wince' (eye shut) | 'chatter' (teeth). */
const sideHead = (c, face = 'calm') => {
  const hl = mix(c.hair, 255, .22), lo = mix(c.hair, 0, .25);
  let s = '';
  // long hair falls behind the shoulders — drawn under the head
  if (c.hairStyle === 'long')
    s += `<path d="M9 14Q8.6 22 9.6 27.6Q11.6 29.6 14.6 28.4L15.4 21Z" fill="${c.hair}" stroke="${lo}" stroke-width=".5"/>` +
      `<path d="M10.6 18.6Q10.6 24 12 27.6" fill="none" stroke="${lo}" stroke-width=".4" opacity=".7"/>`;
  s += `<path d="M9.6 14.6Q9.4 6.6 16.6 6Q22.8 6 23.4 12.6L23.6 15.2 25 17.6 23.5 18.3 23.4 20.4Q22.8 23 19.6 23.8Q16.4 24.4 14.2 22.8Q10 20.4 9.6 14.6Z" fill="${c.skin}" stroke="${c.skinO}" stroke-width=".45"/>` +
    // cheek and jaw shading, brow light
    `<path d="M14.6 22.4Q17.4 24.2 20.6 23.2Q22.4 22.4 22.8 20.6Q20.6 22.4 17.6 22.2Q15.6 22.2 14.6 22.4Z" fill="${c.skinO}" opacity=".3"/>` +
    `<path d="M18.6 9.8Q21.6 10.2 22.6 13" fill="none" stroke="${c.skinH}" stroke-width=".7" opacity=".35"/>` +
    `<ellipse cx="20.8" cy="19.2" rx="1.25" ry=".65" fill="#e07a66" opacity=".32"/>` +
    sideFace(c, face) +
    // ear
    `<path d="M15.2 15.8Q13.8 15.4 13.9 17.6Q14.1 19.6 15.8 19.8Q16.6 18 16.2 16.2Z" fill="${c.skin}" stroke="${c.skinO}" stroke-width=".5"/><path d="M15 16.8Q14.6 17.8 15.4 18.8" fill="none" stroke="${c.skinO}" stroke-width=".3"/>`;
  // hair cap over the crown and the back of the head, sideburn in front of the ear
  if (c.hairStyle === 'long')
    s += `<path d="M9.2 15.4Q8.6 5.4 16.6 5.2Q23.6 5.4 23.8 12.4Q21.6 9.6 18.6 9.8Q17.6 12.8 16.8 15.4Q15.4 14.8 14.4 15.2L13.6 21Q10.4 20.6 9.2 15.4Z" fill="${c.hair}" stroke="${lo}" stroke-width=".45"/>` +
      `<path d="M11.4 9.8Q15 6.6 19.6 7.4" fill="none" stroke="${hl}" stroke-width=".7" opacity=".55"/>`;
  else if (c.hairStyle === 'tousled')
    s += `<path d="M9.2 15.6Q8.4 5.6 16.4 5.2Q23 5 24 11.6L21.8 10.6 20.6 12.4 18.6 10.8 17 14.4 15.6 15.2Q14 14.8 13.6 16.4L12.6 19.6Q9.6 18.8 9.2 15.6Z" fill="${c.hair}" stroke="${lo}" stroke-width=".45"/>` +
      `<path d="M14.6 5.6Q14.6 3.8 16.6 4Q15.6 4.6 15.8 5.4Z" fill="${c.hair}" stroke="${lo}" stroke-width=".35"/>` +
      `<path d="M11.6 9.6Q15 6.8 19.4 7.4" fill="none" stroke="${hl}" stroke-width=".6" opacity=".5"/>`;
  else
    s += `<path d="M9.3 15.2Q8.8 5.8 16.4 5.6Q22.6 5.6 23.6 11.8Q21.4 9.8 19 10.2Q18.4 12.6 17.2 14.8Q15.8 14.6 14.8 15.2L14 19Q11.6 19.4 10.4 18Q9.4 17 9.3 15.2Z" fill="${c.hair}" stroke="${lo}" stroke-width=".45"/>` +
      `<path d="M11.4 9.8Q15 6.8 19.4 7.6" fill="none" stroke="${hl}" stroke-width=".6" opacity=".5"/>` +
      `<path d="M10.2 15.8Q11.8 17.6 13.8 17.4" fill="none" stroke="${lo}" stroke-width=".5" opacity=".6"/>`;
  if (c.hairStyle === 'swept') // the keeper's fringe, swept forward over the brow
    s += `<path d="M17.8 6.4Q21.8 6 23.7 9.6Q24.1 11.2 23.5 12.6Q22.6 10.9 21 10.4Q20.4 11.8 19.2 12.4Q19.7 10.9 19.1 9.6Q18.4 10.8 17.2 11.2Q18.2 9 17.8 6.4Z" fill="${c.hair}"/>` +
      `<path d="M18.4 7Q21.2 6.8 22.9 9.2M19.4 9.4Q20.3 10.5 20.4 11.6" fill="none" stroke="${c.hairHl}" stroke-width=".45" stroke-linecap="round" opacity=".8"/>`;
  if (c.hat) {
    const [crown, line, brim, band] = c.hat;
    s += `<path d="M9.4 8.6Q15.6 3.4 22.4 7.4L23 10.8 9.2 11.2Z" fill="${crown}" stroke="${line}" stroke-width=".7"/><path d="M5.6 11.4Q15.6 9.6 27.6 10.6L26.4 12.2 7 12.6Z" fill="${brim}"/>` +
      `<path d="M9.6 9.6Q15.8 8.2 22.8 9" fill="none" stroke="${band}" stroke-width=".9"/>`;
  }
  if (c.headdress) // the shaman's feather crown in profile, swept back
    s += `<path d="M9 13.6Q8.6 7 11.4 5.2L13 9.6 14.6 3.8 16.4 8.8 18.4 4 19.8 9.2 22.4 6.2Q23.8 9 23.4 11.6Q16 10.4 9 13.6Z" fill="${c.headdress}" stroke="${mix(c.headdress, 0, .3)}" stroke-width=".5"/>`;
  if (c.feathers)
    s += `<path d="M12 7.6 10.4 2.2M14.8 6.4 14 .8M17.8 6.4 18.4 1" stroke="${c.feathers}" stroke-width="1.8"/>`;
  if (c.scarf) s += `<path d="M13.4 22.4Q17 24.2 20 22.6L20.4 25.2Q16.6 26.6 13 25Z" fill="${c.scarf}"/>`;
  if (c.hood) s += `<path d="M9 13.4Q9.6 4 17.2 4.6Q23.6 5.4 24.2 12L21.6 10.6Q16 9.4 11.6 12.8L10.4 16.4Z" fill="${c.hood}" stroke="${c.hoodLine}" stroke-width=".7"/>`;
  return s;
};

/** A scarf tail streaming behind the hunter (sways with the walk). */
const scarfTail = (c, sway) =>
  c.scarf ? `<path d="M13.6 23.4Q10.6 ${r2(25 + sway)} ${r2(8.4 - sway * .6)} ${r2(28.4 + sway)}L${r2(9.6 - sway * .6)} ${r2(29.6 + sway)}Q12 ${r2(27 + sway)} 14.2 25.2Z" fill="${mix(c.scarf, 0, .12)}"/>` : '';

/** Props in the far (left) hand, drawn behind the body. */
function sideProp(c, hand) {
  const [x, y] = hand;
  if (c.bag) return `<path d="M7.8 33.4h4.4v6.8H7.8Z" fill="${c.bag}"/><path d="M8 35h4" stroke="${mix(c.bag, 0, .25)}" stroke-width=".6"/>`;
  if (c.prop === 'staff') return `<path d="M${r2(x + 8)} ${r2(y - 15)} ${r2(x + 4)} ${r2(y + 8)}" stroke="#785639" stroke-width="1.7"/>` + (c.orb
    ? `<circle cx="${r2(x + 8.2)}" cy="${r2(y - 17)}" r="2.4" fill="${c.orb}"/><path d="M${r2(x + 7.8)} ${r2(y - 18.4)}q1.2.6.6 1.8" fill="none" stroke="#e5f3d5" stroke-width=".7"/>`
    : `<path d="m${r2(x + 8.2)} ${r2(y - 19.4)} 2.6 4.4-4 .6Z" fill="#9fc1c8"/>`);
  if (c.prop === 'spear') return `<path d="M${r2(x + 3)} ${r2(y + 6)} ${r2(x + 10.4)} ${r2(y - 24)}" stroke="#8b663f" stroke-width="1.5"/><path d="m${r2(x + 9.8)} ${r2(y - 21.6)} 1.6-5.6m-.4 4.2 2.8.8" stroke="#d6c692" stroke-width="1"/>`;
  if (c.prop === 'stick') return `<path d="M${r2(x)} ${r2(y)}q5.4 1 7.4-3" fill="none" stroke="#d6a076" stroke-width="1.6"/>`;
  if (c.prop === 'bundle') return `<path d="m${r2(x - 2)} ${r2(y - 2)} 5-.4.8 5-5.6.6Z" fill="#c3a16b" stroke="#765e3e" stroke-width=".7"/>`;
  return '';
}

function sideFrame(c, { dy = 0, far = [-1.2, 0], near = [1.4, 0], farArm = 8, nearArm = -8, lean = 1.5, sway = 0 } = {}) {
  const S_FAR = [15.2, 25.8], S_NEAR = [16.4, 25.8];
  const fa = swingArm(c, S_FAR, farArm, 10 + Math.max(0, farArm) * .5, true);
  const na = swingArm(c, S_NEAR, nearArm, 10 + Math.max(0, nearArm) * .5, false);
  const body =
    scarfTail(c, sway) + sideProp(c, fa.hand) + fa.svg +
    sideTorso(c) + sideWear(c) + (c.gear ? '<!--gear:under--><!--gear:over-->' : '') +
    `<path d="M14.6 21.6h3.2v3.6h-3.2Z" fill="${c.skin}"/>` + sideHead(c) + na.svg;
  return SHADOW + sideLeg(c, dy, far, true) + sideLeg(c, dy, near, false) +
    `<g transform="translate(0 ${r2(dy)}) rotate(${lean} 16 40)">${body}</g>`;
}

// ════════════════════════════════════════════════════════════════════════════
// WALK CYCLES
// ════════════════════════════════════════════════════════════════════════════
// Each foot: planted and sliding back for half the cycle, lifted and carried
// forward for the other half; feet half a cycle apart; the body is lowest at
// contact and highest as one leg passes under it. Arms swing against the legs.
const STRIDE = 2.9, LIFT = 1.8;
const foot = (ph) => {
  ph = ((ph % (2 * Math.PI)) + 2 * Math.PI) % (2 * Math.PI);
  if (ph < Math.PI) { const s = ph / Math.PI; return [STRIDE - 2 * STRIDE * s, 0]; }
  const s = (ph - Math.PI) / Math.PI;
  return [-STRIDE + 2 * STRIDE * ease(s), LIFT * Math.sin(Math.PI * s)];
};

// The pack drew no walk for the child, the hunter or the elder: their front walk is
// their own front art with its drawn legs swapped for stepping ones (the same legs
// keeper-frames.mjs gives the keeper), the body bobbing on top.
const FRONT_LEGS = /<path d="m(?:10 34 5 0|17 34 5 0|9\.7 42|18 42 4\.6)[^>]*\/>\s*/g;
function frontWalkFrame(name, c, l, r, dy) {
  const src = read(name);
  let inner = src.slice(src.indexOf('>') + 1, src.lastIndexOf('</svg>')).trim();
  const wrap = /^<g transform="[^"]*">/.exec(inner);
  if (wrap) inner = inner.slice(wrap[0].length, inner.lastIndexOf('</g>'));   // frameOf puts it back
  const shadow = inner.match(/<ellipse[^>]*\/>/)[0];
  const bootL = inner.match(/<path d="m9\.7 42[^>]*\/>/)[0], bootR = inner.match(/<path d="m18 42 4\.6[^>]*\/>/)[0];
  const body = inner.replace(shadow, '').replace(FRONT_LEGS, '');
  const leg = (boot, hip, ankle, [bx, by], tilt) =>
    trousers(c, legPath(hip, ankle), false) +
    `<g transform="translate(${r2(ankle[0] - bx)} ${r2(ankle[1] - by)}) rotate(${r2(tilt)} ${bx} ${by})">${boot}</g>`;
  const k = 2.4 / STRIDE;   // the keeper's front stride
  return shadow +
    leg(bootL, [12.5, 34 + dy], [12.4 + l[0] * k, 42 - l[1]], [12.4, 42], -l[1] * 6) +
    leg(bootR, [19.5, 34 + dy], [20 + r[0] * k, 42 - r[1]], [20, 42], r[1] * 6) +
    `<g transform="translate(0 ${r2(dy)})">${body}</g>`;
}

export {
  SPR, read, write, r2, pad, mix, ease, rad, CHARS, frameOf, SHADOW, DARK, SEG, legPath, trousers, arm, swingArm,
  backTorso, backWear, backHead, backBoot, sideBoot, sideTorso, sideWear, sideHead, foot, STRIDE,
  backProp, sideProp, scarfTail,
};

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
for (const [name, c] of Object.entries(CHARS)) {
  const frame = frameOf(name);
  write(`${name}_back`, frame(backFrame(c)));
  write(`${name}_side`, frame(sideFrame(c)));
  for (let k = 0; k < c.walk; k++) {
    const ph = (k / c.walk) * 2 * Math.PI;
    const l = foot(ph), r = foot(ph + Math.PI);
    const dy = 0.45 - 0.9 * Math.abs(Math.sin(ph));
    if (c.frontWalk) write(`${name}_walk_${pad(k)}`, frame(frontWalkFrame(name, c, l, r, dy)));
    // back: forward offset only shifts the foot on screen a little
    write(`${name}_back_walk_${pad(k)}`, frame(backFrame(c, {
      dy, l: [l[0] / STRIDE, l[1]], r: [r[0] / STRIDE, r[1]], swing: -l[0] / STRIDE,
    })));
    // side: far leg is the left one; the near arm swings with the far leg
    write(`${name}_side_walk_${pad(k)}`, frame(sideFrame(c, {
      dy, far: [l[0] * 1.35, l[1] * 1.2], near: [r[0] * 1.35, r[1] * 1.2], farArm: -r[0] / STRIDE * 22, nearArm: -l[0] / STRIDE * 22, sway: Math.sin(ph * 2) * .6,
    })));
  }
}
console.log('human views drawn');
}
