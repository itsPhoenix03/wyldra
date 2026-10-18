// Village folk drawn from the villagers' own art: the Woods and Spire elders and
// a Spire villager in winter wool. Recoloured, not redrawn, so they keep every
// clip the pack gave the villagers (walk, collect, build / craft, sit).
//
//   node tools/art/folk-variants.mjs
//
//   elder_woods    ← villager    grey hair and stubble, moss-dyed wool, an old darkened hat
//   villager_snow  ← villager    slate-blue wool and a fur hat for the Spire's cold
//   elder_spire    ← villager2   grey hair, slate wool, a fur wrap over the apron
//
// This writes the FRONT art (<name>.svg and <name>_<clip>_NN.svg). The back and
// side views come from tools/art/human-views.mjs and npc-view-actions.mjs, whose
// CHARS entries use the same colours. The elders also get the age lines the A+
// redesign gives Elder Yvenne. Run after tools/art/faces-aplus.mjs, before
// human-views.mjs (which reads these base sprites).

import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SPR = resolve(dirname(fileURLToPath(import.meta.url)), '../../assets/sprites');

const VARIANTS = {
  elder_woods: {
    from: 'villager', aged: true, map: {
      '#4b382d': '#a39d92', // hair and brows
      '#3a2e28': '#8e887e', // stubble, gone grey
      '#80634a': '#5b6a4b', // shirt: moss-dyed wool
      '#d0b27b': '#a7b48a', // shirt sheen
      '#8e7d59': '#6a5f4b', '#514b3c': '#3e382d', '#a29469': '#7f7359', // hat: old, darkened felt
    },
  },
  villager_snow: {
    from: 'villager', map: {
      '#80634a': '#5d7787', '#d0b27b': '#c3d5db', // shirt: slate-blue wool
      '#534738': '#46525c', // trousers
      '#8e7d59': '#d8d1c2', '#514b3c': '#8d8576', '#a29469': '#ebe6da', // a fur hat
    },
  },
  elder_spire: {
    from: 'villager2', aged: true, map: {
      '#52362d': '#c9c3b7', '#4c3229': '#bdb7ab', '#5d4031': '#aea89c', // grey hair
      '#795668': '#566f7d', '#d6a69a': '#b6cad2', // slate wool
      '#84586a': '#d9d1c1', '#a7747e': '#a59d8d', // a fur wrap over the apron
      '#51404b': '#46525c', // trousers
    },
  },
};

// Elder Yvenne's age lines (crow's feet, forehead, cheeks), drawn in the skin's line colour
const AGE = (line) =>
  `<path d="M10.5 15.5L9.7 15.1M10.5 16.5L9.7 16.8M21.5 15.5L22.3 15.1M21.5 16.5L22.3 16.8M12.4 10.7Q16 10.1 19.6 10.7M13.2 11.6Q16 11.1 18.8 11.6M14.6 19.9Q13.7 21 14.1 22.1M17.4 19.9Q18.3 21 17.9 22.1" fill="none" stroke="${line}" stroke-width=".3" opacity=".6"/>`;
const NOSE = '<path d="M16.4 16.5Q17.1 18.5';
// the A+ head's skin line colour: the stroke on its ears
const LINE = /<path d="M9\.3 15\.1Q7 14\.5[^"]*" fill="#[0-9a-f]{6}" stroke="(#[0-9a-f]{6})"/;

let n = 0;
for (const [name, v] of Object.entries(VARIANTS)) {
  const files = readdirSync(SPR).filter((f) =>
    f === `${v.from}.svg` || new RegExp(`^${v.from}_(?!back_|side_)[a-z]+_[0-9][0-9]\\.svg$`).test(f));
  for (const f of files) {
    let s = readFileSync(`${SPR}/${f}`, 'utf8');
    if (v.aged) {
      const line = s.match(LINE)?.[1];
      if (!line || !s.includes(NOSE)) throw new Error(`${f}: A+ face not found`);
      s = s.replace(NOSE, AGE(line) + NOSE);
    }
    s = s.replace(/#[0-9a-f]{6}/gi, (c) => v.map[c.toLowerCase()] ?? c);
    writeFileSync(`${SPR}/${name}${f.slice(v.from.length)}`, s);
    n++;
  }
}
console.log(`folk variants: ${n} front sprites written`);
