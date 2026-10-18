// Front sit and collect clips for the folk the pack drew standing only — the
// child, the Ashmark hunter, Elder Yvenne, the shaman and the Marsh's tribal —
// cut from their own front art so they stay on-model.
//
//   node tools/art/folk-front-clips.mjs
//
//   <char>_sit_01..07      seated on a bench (the villagers' bench), breathing, a blink
//   <char>_collect_01..09  crouch, reach to the ground with the free hand, pluck, rise
//
// All five share the pack's 32×48 front template: legs and boots, the two
// sleeve / forearm / hand polygons, then torso, head and clothes. Sitting swaps
// the legs for bent ones; collecting swaps the legs for crouching ones and the
// free arm for a stroke-drawn reaching arm (the hand without the spear, staff
// or stick). The back and side views of the same clips come from
// npc-view-actions.mjs. Run after human-views.mjs.

import { CHARS, read, write, r2, pad, frameOf, legPath, trousers } from './human-views.mjs';
import { express } from './faces-aplus.mjs';

// which hand is free to reach with (the other holds the prop)
const CAST = { woods_child: 'L', ashmark_hunter: 'L', elder_yvenne: 'L', shaman: 'R', tribal: 'L' };

const FRONT_LEGS = /<path d="m(?:10 34 5 0|17 34 5 0|9\.7 42|18 42 4\.6)[^>]*\/>\s*/g;
const ARM = {
  L: [/<path d="m11 24-3 \.6[^>]*\/>/, /<path d="m7 33 3 \.3[^>]*\/>/],
  R: [/<path d="m21 24 3 \.6[^>]*\/>/, /<path d="m22 33\.3 3-\.3[^>]*\/>/],
};
// both hands are one path (and their knuckle lines another): keep the one that stays
const HANDS = /<path d="m6\.5 38\.7 2\.3\.6-\.2 2\.2-1\.6\.8-1\.2-\.8Zm16\.8\.6 2\.3-\.6\.7 2\.8-1\.2\.8-1\.6-\.8Z"/;
const KNUCKLES = /<path d="m6\.1 40 1\.6\.3m16\.8 0 1\.6-\.3"/;
const KEEP_HAND = { L: 'M23.3 39.3l2.3-.6.7 2.8-1.2.8-1.6-.8Z', R: 'm6.5 38.7 2.3.6-.2 2.2-1.6.8-1.2-.8Z' };
const KEEP_KNUCKLE = { L: 'M24.5 40.3l1.6-.3', R: 'm6.1 40 1.6.3' };

const BENCH = '<path d="M4 40H30V44H4Z" fill="#6a4e38"/><path d="M7 44V47M28 44V47" stroke="#473528" stroke-width="1.8"/>';
const SPROUT = (x, y) => `<path d="M${x} ${y} ${x + 3} ${y - 4} ${x + 5} ${y + 1}Z" fill="#6b8c56"/>`;
const SPRIG = (x, y) => `<g transform="translate(${r2(x)} ${r2(y)})">` +
  '<path d="M0 0Q-.6-2.6 .8-4.4" fill="none" stroke="#5a7a34" stroke-width=".7"/>' +
  '<path d="M.6-3.6Q3-5.6 4-3.2Q2-2.2.6-3.6Z" fill="#7fae4e" stroke="#3f5e27" stroke-width=".4"/>' +
  '<path d="M.2-2.2Q-2.6-3.8-3.4-1.4Q-1.4-.6.2-2.2Z" fill="#6c9a42" stroke="#3f5e27" stroke-width=".4"/></g>';

/** The character's front art split into parts. */
function parts(name) {
  const src = read(name);
  let inner = src.slice(src.indexOf('>') + 1, src.lastIndexOf('</svg>')).trim();
  const wrap = /^<g transform="[^"]*">/.exec(inner);
  if (wrap) inner = inner.slice(wrap[0].length, inner.lastIndexOf('</g>'));   // frameOf puts it back
  const shadow = inner.match(/<ellipse[^>]*\/>/)[0];
  const legsFill = inner.match(/<path d="m10 34 5 0[^>]*fill="(#[0-9a-f]{6})"/)[1];
  const bootFill = inner.match(/<path d="m9\.7 42[^>]*fill="(#[0-9a-f]{6})"/)[1];
  const body = inner.replace(shadow, '').replace(FRONT_LEGS, '');
  return { shadow, body, legsFill, bootFill };
}

/** Legs bent at the knee, hips at y 34 + dy, feet planted; knees spread outward. */
function crouchLegs(c, dy) {
  const leg = (hip, ankle, out) => {
    // legPath bends the knee toward +x; mirror the left leg so both knees go out
    const p = out > 0 ? legPath(hip, ankle) : legPath([32 - hip[0], hip[1]], [32 - ankle[0], ankle[1]])
      .replace(/([ML])([\d.]+) /g, (_, k, x) => `${k}${r2(32 - +x)} `);
    return trousers(c, p, false);
  };
  return leg([12.5, 34 + dy], [11.6, 42], -1) + leg([19.5, 34 + dy], [20.4, 42], 1);
}

function boots(bootFill) {
  return `<path d="m9.7 42 5.3-.2.1 2.2-2.2 1.2-5.1-.2q-.7-1.5 1.9-3Z" fill="${bootFill}" stroke="#292623" stroke-width=".55"/>` +
    `<path d="m18 42 4.6-.1 2.2 2.3-1.1 1-5.6 0Z" fill="${bootFill}" stroke="#292623" stroke-width=".55"/>`;
}

/** A stroke-drawn arm in the art's own sleeve and skin colours, shoulder → elbow → hand. */
function reachArm(body, side, s, e, h) {
  const sleeve = body.match(ARM[side][0])[0], fore = body.match(ARM[side][1])[0];
  const [, sFill] = sleeve.match(/fill="(#[0-9a-f]{6})"/), [, sLine] = sleeve.match(/stroke="(#[0-9a-f]{6})"/);
  const [, skin] = fore.match(/fill="(#[0-9a-f]{6})"/), [, skinLine] = fore.match(/stroke="(#[0-9a-f]{6})"/);
  const hand = body.match(/<path d="m6\.5 38\.7[^>]*fill="(#[0-9a-f]{6})"/)[1];
  const up = `M${r2(s[0])} ${r2(s[1])}L${r2(e[0])} ${r2(e[1])}`, fo = `M${r2(e[0])} ${r2(e[1])}L${r2(h[0])} ${r2(h[1])}`;
  return `<path d="${up}" fill="none" stroke="${sLine}" stroke-width="4.4" stroke-linecap="round"/>` +
    `<path d="${up}" fill="none" stroke="${sFill}" stroke-width="3.7" stroke-linecap="round"/>` +
    `<path d="${fo}" fill="none" stroke="${skinLine}" stroke-width="3.3" stroke-linecap="round"/>` +
    `<path d="${fo}" fill="none" stroke="${skin}" stroke-width="2.6" stroke-linecap="round"/>` +
    `<ellipse cx="${r2(h[0])}" cy="${r2(h[1])}" rx="1.5" ry="1.8" fill="${hand}" stroke="${skinLine}" stroke-width=".4"/>`;
}

/** The body with one arm taken off (its hand and knuckle line too), and the arm's place marked. */
function oneArmed(body, side) {
  const [sl, fo] = ARM[side];
  return body
    .replace(sl, '<!--reach-->').replace(fo, '')
    .replace(HANDS, `<path d="${KEEP_HAND[side]}"`).replace(KNUCKLES, `<path d="${KEEP_KNUCKLE[side]}"`);
}

for (const [name, side] of Object.entries(CAST)) {
  const c = CHARS[name], frame = frameOf(name);
  const { shadow, body, legsFill, bootFill } = parts(name);
  const blink = express(body, { eyes: 'closed' });

  // sit: on the bench, hands on the knees, breathing, a blink on the fourth beat
  const seated =
    `<path d="m9 35 7 1 4 6-2 2-8-4-4-1Z" fill="${legsFill}" stroke="#302d2b" stroke-width=".6"/>` +
    `<path d="m17 36 6 0 5 5-2 2-9-3Z" fill="${legsFill}" stroke="#302d2b" stroke-width=".6"/>` +
    `<path d="m17 42 4-2 4 1-1 3-6 1Z" fill="${bootFill}"/><path d="m25 41 3-1 2 2-1 2-5 0Z" fill="${bootFill}"/>`;
  for (let k = 0; k < 7; k++) {
    const b = Math.sin((k / 7) * Math.PI * 2) * 0.3;
    write(`${name}_sit_${pad(k)}`, frame(
      `<g transform="translate(0 ${r2(2 + b)})">${shadow}${seated}${k === 3 ? blink : body}</g>${BENCH}`));
  }

  // collect: crouch, reach the sprout with the free hand, pluck it, rise holding it
  const mx = (x) => (side === 'L' ? x : 32 - x);   // poses are written for the left hand
  const shoulder = [mx(10.4), 25];
  const ROWS = [
    // hip drop, hand [x, y] in body space, holding?
    [0,   [8.4, 34],   false],
    [1.2, [9.6, 37],   false],
    [2.6, [11.6, 39.4], false],
    [3.2, [12.4, 38.8], false],
    [3.2, [12.2, 38.4], true],
    [2.4, [11, 36.4],  true],
    [1.2, [9.6, 33.6], true],
    [0.4, [9, 31.6],   true],
    [0,   [8.6, 32.4], true],
  ];
  const armless = oneArmed(body, side);
  ROWS.forEach(([dy, hb, held], i) => {
    const h = [mx(hb[0]), hb[1]];
    // elbow bowed outward, away from the body
    const e = [(shoulder[0] + h[0]) / 2 + (side === 'L' ? -1.5 : 1.5), (shoulder[1] + h[1]) / 2];
    const arm = reachArm(body, side, shoulder, e, h) + (held ? SPRIG(h[0], h[1] - 0.6) : '');
    const sprout = held ? '' : SPROUT(mx(10.6) - (side === 'L' ? 0 : 5), 42.6);
    write(`${name}_collect_${pad(i)}`, frame(
      shadow + sprout + crouchLegs(c, dy) + boots(bootFill) +
      `<g transform="translate(0 ${r2(dy)})">${armless.replace('<!--reach-->', arm)}</g>`));
  });
}
console.log('folk front clips drawn');
