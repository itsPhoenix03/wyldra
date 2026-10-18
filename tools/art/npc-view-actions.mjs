// The NPCs' clips seen from the side and from behind — the camp folk's work and
// rest, the medics' healing — posed with the keeper's view kit
// (keeper-view-actions.mjs) so they match the back/side walks and stands.
//
//   node tools/art/npc-view-actions.mjs
//
// Writes <char>_side_<clip>_NN and <char>_back_<clip>_NN with the same frame
// count as the front clip <char>_<clip>_NN, which folk.ts / main.ts play the
// same way (Phaser anims at 11 fps).
//
//   villager    collect · build · sit          medic       heal · prepare
//   villager2   collect · craft · sit          medic_snow  heal · prepare · shiver

import { write, r2, pad, frameOf } from './human-views.mjs';
import {
  useChar, side, back, tool, lerp, SVG64, HAMMER, VIAL, FROST, BREATH,
} from './keeper-view-actions.mjs';

// ── props (the pack's own art) ──────────────────────────────────────────────
// a gathered handful of greens, stem along −y in prop space
const GREENS =
  '<path d="M0 2 0-9" stroke="#3d7653" stroke-width=".9"/>' +
  '<path d="M0-7Q-5-11-6-7Q-4-4 0-5ZM0-7Q5-11 6-7Q4-4 0-5Z" fill="#6c9a65"/>' +
  '<path d="M0-4Q-4-7-5-3Q-3 0 0-2ZM0-4Q4-7 5-3Q3 0 0-2Z" fill="#4f865d"/>';
const SPROUT = (x, y) => `<path d="M${x} ${y} ${x + 3} ${y - 4} ${x + 5} ${y + 1}Z" fill="#6b8c56"/>`;
const SPARKLE = (x, y, k) => k > 0
  ? `<g opacity="${r2(k)}"><circle cx="${x}" cy="${y}" r="${r2(3 + k)}" fill="#8bcbb0" opacity=".22"/><path d="M${x} ${r2(y - 3 * k)}V${r2(y + 3 * k)}M${r2(x - 3 * k)} ${y}H${r2(x + 3 * k)}" stroke="#e2f2da" stroke-width=".8"/></g>` : '';
const BENCH_WOOD = '#6a4e38', BENCH_LEG = '#473528';
const BOX = '<path d="m0 0 5 0 .6 3.2-5.8.4Z" fill="#c3a16b" stroke="#765e3e" stroke-width=".7"/><path d="m1 .8.6 2m1.8-2 .5 1.6" stroke="#e2ca91" stroke-width=".6"/>';
const PLANK_SIDE = '<path d="M20.6 42.2 37.4 42 37.6 44.2 20.8 44.4Z" fill="#9a7349" stroke="#60452f" stroke-width=".6"/><path d="M21.6 43.2 36.6 43" stroke="#d3a66a" stroke-width=".5"/>';
const PLANK_BACK = '<path d="M3.6 39 28.4 39.2 28.6 41.2 3.4 41Z" fill="#9a7349" stroke="#60452f" stroke-width=".6"/><path d="M4.6 40 27.4 40.2" stroke="#d3a66a" stroke-width=".5"/>';

// ── clips, as functions of the current character ────────────────────────────
const CLIPS = {};

// collect: bend to the sprout, pluck, straighten and settle the greens in the bundle
CLIPS.collect = {
  side: () => [
    [0,  0,   [21, 30],   'calm'],
    [8,  1.2, [22.5, 34], 'calm'],
    [17, 3,   [25, 39.5], 'calm'],
    [17, 3,   [25.4, 40], 'calm'],
    [9,  1.4, [23, 34.5], 'calm'],
    [2,  .3,  [21, 30],   'calm'],
    [3,  .2,  [21, 30.5], 'wince'],
    [1,  0,   [21, 30],   'calm'],
    [0,  0,   [21, 30.2], 'calm'],
  ].map(([lean, dy, h, face]) => side({
    lean, dy, face, near: [1.8, 0], na: { h, bow: 1.4 },
    tools: [{ art: GREENS, h, th: -90 + lean * .6, layer: 'front' }],
    layers: { ground: SPROUT(26, 43) },
  })),
  back: () => [
    [0,   1,   [23.5, 30],   false],
    [1.2, .95, [24, 33],     false],
    [3,   .88, [24.5, 37.5], false],
    [3,   .88, [24.6, 38],   false],
    [1.4, .94, [24, 34],     false],
    [.3,  .99, [23.5, 30],   false],
    [.2,  .99, [23.4, 30.4], false],
    [0,   1,   [23.5, 30],   false],
    [0,   1,   [23.5, 30.2], false],
  ].map(([dy, sy, h]) => back({
    dy, sy, l: [0, 0, -.3], r: [0, 0, .3], ra: { h, bow: 1.4 },
    tools: [{ art: GREENS, h, th: -90, layer: 'over' }],
    layers: { ground: SPROUT(23, 39) },
  })),
};

// build / craft: kneel at a plank and hammer it twice (the keeper's build, posed for them)
const HAMMERING = {
  side: () => [
    [4,  [23.5, 27.5], -55], [2,  [14.5, 21.5], -125], [8,  [24, 27], -60], [12, [24.5, 36.4], -4], [10, [24.5, 34], -24],
    [4,  [23.5, 27.5], -55], [2,  [14.5, 21.5], -125], [12, [24.5, 36.4], -4], [8,  [23.5, 31], -55],
  ].map(([lean, h, th], i) => side({
    lean, dy: 3, far: [2.8, 0], near: [-3, 0, 32], face: i === 3 || i === 7 ? 'grit' : 'calm', prop: false,
    na: { h, bow: th < -90 ? -1.6 : 1.4 }, fa: { h: [24.5, 40.8], bow: 1.6 },
    tools: [{ art: HAMMER, h, th, layer: th < -90 ? 'prehead' : 'front' }], nearBehindHead: th < -90,
    layers: { ground: PLANK_SIDE },
  })),
  back: () => [
    [[23.5, 24], -100, 'over', 1, false, false], [[20.5, 18], -112, 'over', 1, false, true],
    [[22.5, 26], -50, 'behind', .8, false, false], [[21, 33], 60, 'behind', 1, true, false],
    [[22, 31], 20, 'behind', 1, true, false], [[23.5, 24], -100, 'over', 1, false, false],
    [[20.5, 18], -112, 'over', 1, false, true], [[21, 33], 60, 'behind', 1, true, false],
    [[23, 30], -30, 'behind', .9, false, false],
  ].map(([h, th, layer, sy, hid, over]) => back({
    dy: 3, l: [1.6, 0], r: [-1.2, .6], prop: false,
    la: { h: [12, 38.5], behind: true }, ra: { h, behind: hid, over, bow: 1.6 },
    tools: [{ art: HAMMER, h, th, layer, sy }], layers: { ground: PLANK_BACK },
  })),
};
CLIPS.build = HAMMERING;
CLIPS.craft = HAMMERING;

// sit: on a bench, hands on the knees, breathing, a blink
CLIPS.sit = {
  side: () => Array.from({ length: 7 }, (_, k) => {
    const b = Math.sin((k / 7) * Math.PI * 2) * .3;
    return side({
      dy: 2.5 + b, face: k === 3 ? 'wince' : 'calm',
      far: [5, 0], near: [6, 0], na: { h: [21.6, 37.4 + b], bow: 1.4 }, fa: { h: [20.4, 37.4 + b], bow: 1.4 },
      layers: {
        ground: `<path d="M8.6 38.4H23.6V40.2H8.6Z" fill="${BENCH_WOOD}"/><path d="M10.4 40.2V45.4M21.8 40.2V45.4" stroke="${BENCH_LEG}" stroke-width="1.8"/>`,
      },
    });
  }),
  back: (name) => Array.from({ length: 7 }, (_, k) => {
    const b = Math.sin((k / 7) * Math.PI * 2) * .3;
    const box = name === 'villager' ? tool(BOX, [4.6, 35.4], -90) : '';
    return back({
      dy: 2.5 + b, seated: true, prop: false,
      la: { h: [12.5, 37], behind: true }, ra: { h: [19.5, 37], behind: true },
      layers: {
        top: `<path d="M4 38.6H28V41.4H4Z" fill="${BENCH_WOOD}"/><path d="M6.6 41.4V46M25.4 41.4V46" stroke="${BENCH_LEG}" stroke-width="1.8"/>${box}`,
      },
    });
  }),
};

// heal: lift a vial and tip it toward the patient ahead, a healing glow where it lands
CLIPS.heal = {
  side: () => [
    [[19.5, 33],   -90, 0],
    [[21.5, 28],   -80, 0],
    [[23, 22],     -60, 0],
    [[25.5, 24],   -20, .5],
    [[26.5, 26],   10,  1],
    [[26.5, 26.5], 15,  1],
    [[25.5, 27],   0,   .7],
    [[22.5, 30],   -60, .3],
    [[19.5, 33],   -90, 0],
  ].map(([h, th, k]) => side({
    na: { h, bow: 1.5 }, tools: [{ art: VIAL, h, th, layer: 'front' }],
    layers: { over: SPARKLE(31, 32, k) },
  })),
  back: () => [
    [[23, 34],   -90, 'front',  false, 0],
    [[23.5, 28], -80, 'front',  false, 0],
    [[24, 21.5], -70, 'front',  false, 0],
    [[22.5, 23], -40, 'behind', true,  .4],
    [[22, 24],   -20, 'behind', true,  1],
    [[22, 24.5], -15, 'behind', true,  1],
    [[22.5, 24], -30, 'behind', true,  .7],
    [[23.5, 28], -70, 'front',  false, .3],
    [[23, 34],   -90, 'front',  false, 0],
  ].map(([h, th, layer, hid, k]) => back({
    ra: { h, behind: hid, bow: hid ? 2.4 : 1.4 }, tools: [{ art: VIAL, h, th, layer, sy: hid ? .7 : 1 }],
    layers: { behind: SPARKLE(26, 27, k) },
  })),
};

// prepare: a vial out of the bag on the hip, held up and shaken, put back
CLIPS.prepare = {
  side: () => [
    [[13.6, 34],   false, 0,  0],
    [[13.6, 34],   true,  0,  0],
    [[17.5, 31],   true,  0,  0],
    [[21, 28.5],   true,  0,  0],
    [[21.6, 28],   true,  12, 2],
    [[20.6, 28.8], true,  -12, 2],
    [[21, 28.5],   true,  0,  4],
    [[17, 31],     true,  0,  0],
    [[13.6, 34],   false, 0,  0],
  ].map(([h, held, tilt, headRot]) => side({
    headRot, na: { h, bow: h[0] < 16 ? -1.2 : 1.5 },
    tools: held ? [{ art: VIAL, h, th: -90 + tilt, layer: 'front' }] : [],
  })),
  back: () => [
    [[8, 35.5],  false, 0],
    [[8, 35.5],  true,  0],
    [[7.6, 32],  true,  0],
    [[8, 28.6],  true,  0],
    [[7.4, 28],  true,  12],
    [[8.6, 28.8], true, -12],
    [[8, 28.6],  true,  0],
    [[7.6, 32],  true,  0],
    [[8, 35.5],  false, 0],
  ].map(([h, held, tilt]) => back({
    la: { h, bow: -1.4 }, tools: held ? [{ art: VIAL, h, th: -90 + tilt, layer: 'over' }] : [],
  })),
};

// shiver: hunched in the coat, arms wrapped, trembling, frost and breath
CLIPS.shiver = {
  side: () => Array.from({ length: 9 }, (_, k) => {
    const t = k % 2 ? .35 : -.35, f = k % 3;
    const frost = `<path d="M${6 + f} ${10 - f}l-2-3M${27 - f} ${9 + f}l2-3M${4 + f} ${17 + f}l-2-2M${28 - f} ${16 - f}l2-2" stroke="${FROST}" stroke-width=".8"/>`;
    const breath = k % 3 === 1 ? `<path d="M24.5 20.4 28.4 19.4 30.4 21.2 27.4 22.2" fill="${BREATH}" opacity=".5"/>` : '';
    return side({
      bx: t, lean: 4, face: 'chatter', na: { h: [19.6, 28.6], bow: 2.2 }, fa: { h: [19, 29.4], bow: 2 },
      layers: { over: frost + breath },
    });
  }),
  back: () => Array.from({ length: 9 }, (_, k) => {
    const t = k % 2 ? .35 : -.35, f = k % 3;
    const frost = `<path d="M${5 + f} ${10 - f}l-2-3M${27 - f} ${9 + f}l2-3M${3 + f} ${18 + f}l-2-2M${29 - f} ${17 - f}l2-2" stroke="${FROST}" stroke-width=".8"/>`;
    return back({
      bx: t, headDy: .6, la: { h: [14.5, 30], e: [8.2, 30], behind: true }, ra: { h: [17.5, 30], e: [23.8, 30], behind: true },
      layers: { top: frost },
    });
  }),
};

// ── write ───────────────────────────────────────────────────────────────────
const CAST = {
  villager: ['collect', 'build', 'sit'],
  villager2: ['collect', 'craft', 'sit'],
  medic: ['heal', 'prepare'],
  medic_snow: ['heal', 'prepare', 'shiver'],
  // village folk recoloured from the villagers (tools/art/folk-variants.mjs) do the same work
  elder_woods: ['collect', 'build', 'sit'],
  villager_snow: ['collect', 'build', 'sit'],
  elder_spire: ['collect', 'craft', 'sit'],
  // the folk drawn standing only: their front sit / collect are tools/art/folk-front-clips.mjs
  woods_child: ['collect', 'sit'],
  ashmark_hunter: ['collect', 'sit'],
  elder_yvenne: ['collect', 'sit'],
  shaman: ['collect', 'sit'],
  tribal: ['collect', 'sit'],
};
// clips that reach past the 32px body canvas (a 64×48 canvas, body still centred)
// (src/assets.ts lists the same keys)
const WIDE = new Set([
  'villager_side_build', 'villager_back_build', 'villager2_side_craft', 'villager2_back_craft',
  'medic_side_heal', 'medic_snow_side_heal',
  'elder_woods_side_build', 'elder_woods_back_build', 'villager_snow_side_build', 'villager_snow_back_build',
  'elder_spire_side_craft', 'elder_spire_back_craft',
]);
const only = process.argv[2];
for (const [name, clips] of Object.entries(CAST)) {
  if (only && only !== name) continue;
  useChar(name);
  const narrow = frameOf(name);
  for (const clip of clips)
    for (const view of ['side', 'back']) {
      const frames = CLIPS[clip][view](name);
      const wide = WIDE.has(`${name}_${view}_${clip}`);
      frames.forEach((body, i) =>
        write(`${name}_${view}_${clip}_${pad(i)}`, wide ? `${SVG64}\n${body}\n</svg>\n` : narrow(body)));
    }
}
console.log('npc view actions drawn');
