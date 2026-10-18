// A+ faces on every front frame: the face redesign of 2026-10-15 (keeper-face-
// redesign "A+": A's almond eyes with a coloured iris and catchlight, ~12% larger;
// stubble and creased lids as character traits; lash flicks on the women).
//
//   node tools/art/faces-aplus.mjs
//
// The redesign ships one neutral front sprite per character (<char>.svg, already
// in assets/sprites). Every front animation frame still carries the grounded face
// from tools/art/faces.mjs; this pass cuts that head out and puts the character's
// A+ head in its place, posed with the expression the old frame was drawing:
//
//   old frame                               A+ expression
//   grit brows + bared teeth (attack, chop)  determined lids/brows, gritted teeth
//   grit brows + set mouth (build, heal)     determined lids/brows, set mouth
//   one eye swollen shut (injured)           squeezed eyes, worried brows, gasp
//   squint + chattering teeth (shiver)       narrowed lids, chattering teeth
//   heavy lids + panting (sweat)             tired lids/brows, open mouth
//   open mouth (jump, swim, meds)            open mouth
//   closed eyes (blink frames)               closed eyes
//   wide smile (sit)                         wide smile
//
// Marks drawn on the face for the moment (the injured bruise, the shiver flush)
// are kept. The keeper's A+ head also brings new hair and a warmer skin tone, so
// the keeper's arms and hands are recoloured to match.
//
// Idempotent: a converted file carries MARK and is skipped. Run it after
// tools/art/faces.mjs; tools/art/keeper-frames.mjs and human-views.mjs already
// read the A+ art, so they can be rerun afterwards.

import { readFileSync, writeFileSync, readdirSync } from 'node:fs';
import { dirname, resolve } from 'node:path';
import { fileURLToPath } from 'node:url';

const SPR = resolve(dirname(fileURLToPath(import.meta.url)), '../../assets/sprites');
const MARK = '<!--face:aplus-->';
const OLD_MARK = '<!--face:grounded-1-->';
// longest names first so `medic_snow_walk_01` is not read as `medic`
const CHARS = ['ashmark_hunter', 'elder_yvenne', 'woods_child', 'medic_snow', 'villager2', 'villager', 'medic', 'keeper', 'tribal', 'shaman'];

const OLD_HEAD = '<path d="M9 14Q9 7 16 6Q23 6 24 14L23 19Q21 24 16 24Q11 24 9 19Z"';
const HAIR_CAP = '<path d="M9 14q0-7 6-9 7-2 9 7l-4-3-5-2-4 5Z"';
// the keeper's old neck/hair, and the A+ keeper's vest (first element after its hair)
const OLD_NECK = /<path d="M ?14 +22h ?4v ?3h ?-4Z" fill="#bf865f"\/>/;
const OLD_HAIRLIGHT = /<path d="m ?11 +10 +4 ?-3 +5 +0?\.6"[^>]*\/>/;
const NEW_NECK = '<path d="M14 22h4v3.2h-4Z"';
const VEST = '<path d="m11 25 2 1-1 9-2-1';
const KEEPER_SKIN = { '#bf865f': '#d39a6e', '#885d47': '#8a5a42', '#e5af80': '#f0c79c' };

const r2 = (n) => +n.toFixed(2);

// ── the A+ head of each character, cut from its base sprite ─────────────────
function aplusHead(char) {
  const src = readFileSync(`${SPR}/${char}.svg`, 'utf8').replace(MARK, '');
  let a, b;
  if (char === 'keeper') { a = src.indexOf(NEW_NECK); b = src.indexOf(VEST); }
  else { a = src.indexOf('>', src.search(/<path d="M14 22h4v3h-4Z"/)) + 1; b = src.indexOf(HAIR_CAP); }
  if (a <= 0 || b < 0 || b < a) throw new Error(`${char}: A+ head not found in base art`);
  return src.slice(a, b).trim();
}

// ── expression pieces (all drawn on the shared 32×48 face template) ─────────
const EYE = (x) => new RegExp(`<g transform="translate\\(${x} 16\\.35\\) scale\\(1\\.12\\)[^"]*">[\\s\\S]*?</g>[\\s\\S]*?</g>`);
const EYE_L = EYE('13\\.1'), EYE_R = EYE('18\\.9');
const BROWS = /<path d="M14\.8 13\.3Q12\.8 12\.1 11\.1 13\.4L11\.2 13\.(?:7|65)Q[^"]*" fill="(#[0-9a-f]{6})"\/><path d="M17\.2 13\.3[^>]*\/>/;
const MOUTH_M = /<path d="M14\.4 21\.35Q16 21\.8 17\.6 21\.35"[^>]*\/>\s*<path d="M14\.4 21\.35l[^>]*\/>\s*<path d="M15\.2 22\.05[^>]*\/>/;
const MOUTH_F = /<path d="M14\.4 21\.3Q15\.3[^>]*\/>\s*<path d="M14\.6 21\.4Q16 22\.5[^>]*\/>\s*<ellipse cx="15\.6" cy="21\.85"[^>]*\/>/;
const TEETH = '#f3e6d2', INSIDE = '#4a2420';

/** Brow pair: top edge inner→outer, lower edge back; `thin` for the women's finer brows. */
const BROW_SHAPES = {
  calm:       [13.3, 12.1, 13.4, 13.7, 12.85, 13.9],
  determined: [13.8, 12.3, 13.3, 13.6, 13.05, 14.4],
  worried:    [12.8, 12, 13.7, 14, 12.75, 13.4],
  tired:      [13.75, 12.9, 13.85, 14.15, 13.65, 14.3],
};
function brows(kind, fill, thin) {
  const [y0, qy, y1, by1, bqy, by0] = BROW_SHAPES[kind];
  const t = thin ? [-.05, -.1, -.1] : [0, 0, 0];
  const one = (m) => {
    const x = (v) => r2(m ? 32 - v : v);
    return `<path d="M${x(14.8)} ${y0}Q${x(12.8)} ${qy} ${x(11.1)} ${y1}L${x(11.2)} ${r2(by1 + t[0])}Q${x(12.8)} ${r2(bqy + t[1])} ${x(14.75)} ${r2(by0 + t[2])}Z" fill="${fill}"/>`;
  };
  return one(false) + one(true);
}

/** Lower the upper lid to y (14.5 = open); the iris is clipped by the eye white. */
const lids = (eye, y) => eye
  .replace(/Q(13\.1|18\.9) 14\.5 /g, `Q$1 ${y} `)
  .replace(/Q(13\.1|18\.9) 14\.4 /g, `Q$1 ${r2(y - .1)} `);

/** Replace an eye's contents, keeping its scale group, lash flicks and lid crease. */
function eyeWith(eye, art) {
  const open = eye.slice(0, eye.indexOf('>') + 1);
  const keep = (eye.match(/<path d="M(?:11\.35 16\.2|11\.8 15\.7|20\.65 16\.2|20\.2 15\.7|14\.4 15\.3|17\.6 15\.3)[^>]*\/>/g) || []).join('');
  return `${open}${art}${keep}</g>`;
}
const closedEye = (m, lid) => `<path d="${m ? 'M17.3 16.4Q18.9 17.3 20.7 16.2' : 'M14.7 16.4Q13.1 17.3 11.3 16.2'}" fill="none" stroke="${lid}" stroke-width="0.65" stroke-linecap="round"/>`;
const squeezedEye = (m, lid) => `<path d="${m ? 'M20.6 15.7L17.7 16.5L20.4 17.2' : 'M11.4 15.7L14.3 16.5L11.6 17.2'}" fill="none" stroke="${lid}" stroke-width="0.65" stroke-linecap="round" stroke-linejoin="round"/>`;

function mouth(kind, lip, low) {
  switch (kind) {
    case 'set':
      return `<path d="M14.3 21.45H17.7" stroke="${lip}" stroke-width=".6" stroke-linecap="round"/>` +
        `<path d="M14.3 21.45l-.3.25M17.7 21.45l.3.25" stroke="${lip}" stroke-width=".32" stroke-linecap="round"/>`;
    case 'teeth':
      return `<path d="M14.2 20.9H17.8L17.4 21.95H14.6Z" fill="${TEETH}" stroke="${lip}" stroke-width=".45" stroke-linejoin="round"/>` +
        `<path d="M14.45 21.42H17.55" stroke="${lip}" stroke-width=".2" opacity=".6"/>`;
    case 'chatter':
      return `<path d="M14.5 20.95H17.5L17.2 21.95H14.8Z" fill="${TEETH}" stroke="${lip}" stroke-width=".42" stroke-linejoin="round"/>` +
        `<path d="M15.25 21V21.9M16 21V21.9M16.75 21V21.9" stroke="${lip}" stroke-width=".22"/>`;
    case 'open':
      return `<path d="M15.1 21.15Q16 20.65 16.9 21.15Q17.15 22.35 16 22.55Q14.85 22.35 15.1 21.15Z" fill="${INSIDE}" stroke="${lip}" stroke-width=".35"/>`;
    case 'hurt':
      return `<path d="M14.6 21.9Q16 20.6 17.4 21.9Q16 22.9 14.6 21.9Z" fill="${INSIDE}" stroke="${lip}" stroke-width=".4"/>` +
        `<path d="M15.1 21.5Q16 21.1 16.9 21.5" fill="none" stroke="${TEETH}" stroke-width=".35"/>`;
    case 'smile':
      return `<path d="M14.2 21.2Q16 22.35 17.8 21.2" fill="none" stroke="${lip}" stroke-width=".55" stroke-linecap="round"/>` +
        `<path d="M14.2 21.2l-.3-.25M17.8 21.2l.3-.25" stroke="${lip}" stroke-width=".32" stroke-linecap="round"/>` +
        `<path d="M15.2 22.3Q16 22.55 16.8 22.3" fill="none" stroke="${low}" stroke-width=".35" stroke-linecap="round" opacity=".75"/>`;
  }
  throw new Error(`mouth ${kind}`);
}

/**
 * Pose an A+ head: { eyes: 'open' | 'determined' | 'narrow' | 'tired' | 'closed' | 'squeezed',
 * brows: calm | determined | worried | tired, mouth: calm | set | teeth | chatter | open | hurt | smile,
 * marks: extra face marks drawn under the eyes }.
 */
function express(head, { eyes = 'open', brows: bk = 'calm', mouth: mk = 'calm', marks = '' } = {}) {
  let s = head;
  const eL = s.match(EYE_L)?.[0], eR = s.match(EYE_R)?.[0];
  if (!eL || !eR) throw new Error('A+ eyes not found');
  const lid = eL.match(/<path d="M14\.9 16\.6Q13\.1 14\.4 11\.15 16\.1" fill="none" stroke="(#[0-9a-f]{6})"/)[1];
  const LIDS = { open: 14.5, determined: 14.95, tired: 15.25, narrow: 15.35 };
  let nL = eL, nR = eR;
  if (eyes in LIDS) { nL = lids(eL, LIDS[eyes]); nR = lids(eR, LIDS[eyes]); }
  else if (eyes === 'closed') { nL = eyeWith(eL, closedEye(false, lid)); nR = eyeWith(eR, closedEye(true, lid)); }
  else if (eyes === 'squeezed') { nL = eyeWith(eL, squeezedEye(false, lid)); nR = eyeWith(eR, squeezedEye(true, lid)); }
  // eyeWith rebuilds the group without the clipPath, so the right eye's id stays unique
  s = s.replace(eL, marks + nL).replace(eR, nR);

  const bm = s.match(BROWS);
  if (!bm) throw new Error('A+ brows not found');
  if (bk !== 'calm') s = s.replace(bm[0], brows(bk, bm[1], /L11\.2 13\.65/.test(bm[0])));

  if (mk !== 'calm') {
    const male = s.match(MOUTH_M), female = s.match(MOUTH_F);
    const m = male || female;
    if (!m) throw new Error('A+ mouth not found');
    const lip = male ? m[0].match(/stroke="(#[0-9a-f]{6})"/)[1] : m[0].match(/fill="(#[0-9a-f]{6})"/)[1];
    const low = male ? m[0].match(/M15\.2 22\.05[^>]*stroke="(#[0-9a-f]{6})"/)[1] : lip;
    // the women's lips already smile; keep them for the sitting smile
    if (!(female && mk === 'smile')) s = s.replace(m[0], mouth(mk, lip, low));
  }
  return s;
}

// ── read the expression an old grounded face was drawing ────────────────────
const BRUISE = /<path d="M19\.5 18\.1Q21\.4 18\.3 21\.2 20\.2[^>]*\/>/;
const FLUSH = /<path d="M10\.8 18\.6Q12 18\.1 13\.3 19M18\.7 19[^>]*\/>/;
function expressionOf(old) {
  const has = (t) => old.includes(t);
  const e = {};
  if (has('M11.7 16.6Q13.2 17 14.8 16.6')) e.eyes = 'closed';
  else if (has('M17.5 16.6 20.1 16.6')) e.eyes = 'squeezed';                       // injured: one eye swollen shut
  else if (has('M11.8 16.1 14.8 16.1')) e.eyes = 'tired';                           // sweat: heavy lids
  else if (/stroke-width="\.85"/.test(old)) e.eyes = 'narrow';                      // shiver: squint
  if (has('M11.2 13.5 14.7 14.4')) { e.brows = 'determined'; e.eyes ??= 'determined'; }
  else if (has('M11.1 14.7 14.8 13.1')) e.brows = 'worried';
  else if (has('M11.2 14.5Q13 14')) e.brows = 'tired';
  if (has('M14 20.6 18 20.6')) e.mouth = 'teeth';
  else if (has('M14.4 20.8 17.6 20.8')) e.mouth = 'chatter';
  else if (has('M14.3 21Q16 20.4 17.7 21')) e.mouth = 'set';
  else if (has('M15 20.7Q16 20.1 17 20.7')) e.mouth = 'open';
  else if (has('M14 20.8Q16 22.2 18 20.8')) e.mouth = 'smile';
  else if (has('M14.1 21.7Q15.1 20.5')) e.mouth = 'hurt';
  e.marks = (old.match(BRUISE)?.[0] ?? '') + (old.match(FLUSH)?.[0] ?? '');
  return e;
}

/** Swap a front frame's grounded head for the character's A+ head. */
function convert(src, char, head) {
  if (char === 'keeper') {
    const a = src.search(OLD_NECK), hl = src.match(OLD_HAIRLIGHT);
    const b = hl ? src.indexOf(hl[0]) + hl[0].length : -1;
    if (a < 0 || b < a || !src.includes(OLD_MARK)) throw new Error('keeper head anchors missing');
    const old = src.slice(a, b);
    const out = src.slice(0, a).replace(/#[0-9a-f]{6}/gi, (c) => KEEPER_SKIN[c.toLowerCase()] ?? c) +
      express(head, expressionOf(old)) + MARK +
      src.slice(b).replace(/#[0-9a-f]{6}/gi, (c) => KEEPER_SKIN[c.toLowerCase()] ?? c);
    return out;
  }
  const a = src.indexOf(OLD_HEAD), b = src.indexOf(OLD_MARK);
  if (a < 0 || b < a) throw new Error('head anchors missing');
  return src.slice(0, a) + express(head, expressionOf(src.slice(a, b))) + MARK + src.slice(b + OLD_MARK.length);
}

export { express, aplusHead, MARK };

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  const heads = Object.fromEntries(CHARS.map((c) => [c, aplusHead(c)]));
  let done = 0, skipped = 0;
  for (const f of readdirSync(SPR).sort()) {
    if (!f.endsWith('.svg')) continue;
    const name = f.slice(0, -4);
    const char = CHARS.find((c) => name === c || new RegExp(`^${c}_[a-z]+_[0-9][0-9]$`).test(name));
    if (!char || /_(back|side)_/.test(name)) continue;
    let src = readFileSync(`${SPR}/${f}`, 'utf8');
    if (src.includes(MARK)) { skipped++; continue; }
    if (name === char || (!src.includes(OLD_MARK) && EYE_L.test(src))) {
      // the base sprites are the redesign's own art, and the child's, hunter's and
      // elder's walks are drawn from them (human-views.mjs): just mark them, right
      // after the face so the mark is carried into everything drawn from them
      const at = src.indexOf(char === 'keeper' ? VEST : HAIR_CAP);
      if (at < 0) throw new Error(`${name}: no place for the A+ mark`);
      src = src.slice(0, at) + MARK + src.slice(at);
    } else {
      src = convert(src, char, heads[char]);
    }
    writeFileSync(`${SPR}/${f}`, src);
    done++;
  }
  console.log(`faces: ${done} given the A+ face, ${skipped} already A+`);
}
