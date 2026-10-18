/* ────────────────────────────────────────────────────────────────────────────
 * Keeper skin art — the pure string side of keeper skins, shared by the client
 * (src/keeperSkins.ts) and the atlas build (tools/atlas/build-atlases.mjs).
 * No Phaser here: the build bundles this file for Node.
 *
 * The build bakes one atlas per gear variant (GEAR_VARIANTS) in the art's own
 * shirt, plus a mask atlas saying how much of each pixel is shirt (R) and shirt
 * highlight (G). Any other shirt is a per-pixel blend of those two, done in a
 * worker (src/skinWorker.ts) — no SVG is rasterised in the browser.
 * ──────────────────────────────────────────────────────────────────────────── */

export const SHIRTS = [
  '#456d89', '#a24a3c', '#4f7f3a', '#6d4c8f', '#b8742c', '#2f8a82', '#a8902e', '#a24f7f',
];
export const SHIRT_HEX = SHIRTS.map((s) => parseInt(s.slice(1), 16));

/** Stable palette slot from the server-broadcast name — every client agrees. */
export function shirtFor(id: string): number {
  let h = 0;
  for (const c of id) h = (h * 31 + c.charCodeAt(0)) & 0xffffff;
  return h % SHIRTS.length;
}

export type Cloak = 'furcloak' | 'heatcloak' | null;

// keeper-equipment pack, fur/heat_cloak_idle_01 drape (32×48, keeper torso space)
const CLOAK_ART: Record<'furcloak' | 'heatcloak', string> = {
  furcloak:
    '<path d="M9 22q7-3 14 0l3 8 3 13-6 1-2-7-5 0-2 7-6-1 3-13Z" fill="#775b45" stroke="#4f3e33" stroke-width=".6"/>' +
    '<path d="m9 24-2 6-2 13 4 1 3-13 1-5Zm14 0 2 6 2 13-4 1-3-13-1-5Z" fill="#a17e60"/>' +
    '<path d="M9 22q7 5 14 0l2 4-6 4-3-4-3 4-6-4Z" fill="#d7c6a9" stroke="#4f3e33" stroke-width=".5"/>' +
    '<path d="m12 26 4 3 4-3" fill="none" stroke="#4f3e33" stroke-width=".6"/>' +
    '<circle cx="16" cy="29" r=".8" fill="#cfb784"/>' +
    '<path d="m7 26 2 2m14-2 2 2m-16 7 2 2m12-2-2 2" stroke="#eadcc3" stroke-width=".8"/>',
  heatcloak:
    '<path d="M9 22q7-3 14 0l3 8 3 13-6 1-2-7-5 0-2 7-6-1 3-13Z" fill="#9f4933" stroke="#6f3a30" stroke-width=".6"/>' +
    '<path d="m9 24-2 6-2 13 4 1 3-13 1-5Zm14 0 2 6 2 13-4 1-3-13-1-5Z" fill="#c87540"/>' +
    '<path d="M9 22q7 5 14 0l2 4-6 4-3-4-3 4-6-4Z" fill="#d89c5d" stroke="#6f3a30" stroke-width=".5"/>' +
    '<path d="m12 26 4 3 4-3" fill="none" stroke="#6f3a30" stroke-width=".6"/>' +
    '<circle cx="16" cy="29" r=".8" fill="#cfb784"/>' +
    '<path d="m10 33 2 2m10-2-2 2" stroke="#e6b369" stroke-width=".8"/>',
};

const CLOAK_TAG: Record<'furcloak' | 'heatcloak', string> = { furcloak: 'f', heatcloak: 'h' };

// Starmetal Armor, keeper torso space: slate cuirass with a pale steel plate,
// the starmetal star on the chest, and pauldrons the arms are drawn over.
const ARMOR_ART =
  '<ellipse cx="9.6" cy="25.6" rx="3" ry="2.1" fill="#6e8393" stroke="#253547" stroke-width=".5"/>' +
  '<ellipse cx="22.4" cy="25.6" rx="3" ry="2.1" fill="#6e8393" stroke="#253547" stroke-width=".5"/>' +
  '<path d="M10.3 24.4Q16 22.6 21.7 24.4L22.9 34.6Q16 37.2 9.1 34.6Z" fill="#4b5b70" stroke="#253547" stroke-width=".6"/>' +
  '<path d="M11.8 25.4Q16 24.1 20.2 25.4L21 32.9Q16 34.9 11 32.9Z" fill="#6e8393" stroke="#9eb8bc" stroke-width=".45"/>' +
  '<path d="M16 24.6V34.4" stroke="#41576b" stroke-width=".6"/>' +
  '<path d="M16 27.2 16.8 29 18.7 29.3 17.3 30.5 17.7 32.3 16 31.4 14.3 32.3 14.7 30.5 13.3 29.3 15.2 29Z" fill="#a4dbe1"/>' +
  '<path d="M16 28.3 16.4 29.4 17.5 29.6 16.7 30.3 16.9 31.3 16 30.8 15.1 31.3 15.3 30.3 14.5 29.6 15.6 29.4Z" fill="#d7f2e9"/>';

// Back and side views (tools/art/human-views.mjs) mark where gear goes with
// <!--gear:under--> (armour, under the arms) and <!--gear:over--> (cloak; over
// the arms from behind, behind the near arm in profile). Same colours as above.
type GearView = { cloak: Record<'furcloak' | 'heatcloak', string>; armor: string };
const CLOAK_TONES = {
  furcloak: { base: '#775b45', line: '#4f3e33', fold: '#a17e60', collar: '#d7c6a9', fleck: '#eadcc3' },
  heatcloak: { base: '#9f4933', line: '#6f3a30', fold: '#c87540', collar: '#d89c5d', fleck: '#e6b369' },
};
const cloakBy = (art: (t: typeof CLOAK_TONES.furcloak) => string) =>
  ({ furcloak: art(CLOAK_TONES.furcloak), heatcloak: art(CLOAK_TONES.heatcloak) });
const GEAR_VIEW: Record<'back' | 'side', GearView> = {
  back: {
    cloak: cloakBy((t) =>
      `<path d="M10.4 22.6Q16 21 21.6 22.6L24.6 30 26 42.4Q16 45 6 42.4L7.4 30Z" fill="${t.base}" stroke="${t.line}" stroke-width=".6"/>` +
      `<path d="M12.6 26.4 11.2 42.6M16 26V43.4M19.4 26.4l1.4 16.2" stroke="${t.fold}" stroke-width=".9" opacity=".8"/>` +
      `<path d="M10 22.4Q16 26.4 22 22.4L23 25Q16 28.8 9 25Z" fill="${t.collar}" stroke="${t.line}" stroke-width=".5"/>` +
      `<path d="m8.6 31 1.6 1.4m13.2-1.4-1.6 1.4m-9.8 6.6 1.4 1.4m6.8-1.4-1.4 1.4" stroke="${t.fleck}" stroke-width=".8"/>`),
    armor:
      '<ellipse cx="9.6" cy="25.6" rx="3" ry="2.1" fill="#6e8393" stroke="#253547" stroke-width=".5"/>' +
      '<ellipse cx="22.4" cy="25.6" rx="3" ry="2.1" fill="#6e8393" stroke="#253547" stroke-width=".5"/>' +
      '<path d="M10.3 24.4Q16 22.6 21.7 24.4L22.9 34.6Q16 37.2 9.1 34.6Z" fill="#4b5b70" stroke="#253547" stroke-width=".6"/>' +
      '<path d="M11.6 27.2q4.4-1.4 8.8 0M11.2 30.8q4.8-1.4 9.6 0" fill="none" stroke="#9eb8bc" stroke-width=".45"/>' +
      '<path d="M16 24V35.6" stroke="#2e3d50" stroke-width=".8"/>',
  },
  side: {
    cloak: cloakBy((t) =>
      `<path d="M13 22.8Q17.4 22 19.2 24.4L17.2 27.6 15.6 42.8Q11 43.6 7.4 42.4L9.8 30Q10.6 24.6 13 22.8Z" fill="${t.base}" stroke="${t.line}" stroke-width=".6"/>` +
      `<path d="M12 27.2 9.6 42.2M14.8 27.6l-.6 15" stroke="${t.fold}" stroke-width=".9" opacity=".8"/>` +
      `<path d="M12.6 22.6Q16.6 21.4 19.8 23.8L19.4 25.8Q16 24.6 12.4 25.8Z" fill="${t.collar}" stroke="${t.line}" stroke-width=".5"/>` +
      `<path d="m9.4 33 1.6 1.2m-1.2 5.2 1.4 1" stroke="${t.fleck}" stroke-width=".8"/>`),
    armor:
      '<path d="M12.6 24.8Q16.4 23.2 20 24.8L21.4 34.8Q16.4 37 11.6 35Z" fill="#4b5b70" stroke="#253547" stroke-width=".6"/>' +
      '<path d="M16.4 25.6 20 25.8 20.8 33.6 17 34.4Z" fill="#6e8393" stroke="#9eb8bc" stroke-width=".45"/>' +
      '<path d="M19 27.8 19.5 28.9 20.6 29.1 19.8 29.8 20 30.9 19 30.4 18 30.9 18.2 29.8 17.4 29.1 18.5 28.9Z" fill="#a4dbe1"/>',
  },
};

export function skinId(shirt: number, cloak: Cloak, armor = false): string {
  return `${shirt}${armor ? 'a' : ''}${cloak ? CLOAK_TAG[cloak] : ''}`;
}

/** Texture key of `frame` in a skin ('0' is the shipped art itself). */
export function skinKey(frame: string, id: string): string {
  return id === '0' ? frame : `${frame}@${id}`;
}

export const mix = (hex: string, to: number, t: number) => {
  const n = parseInt(hex.slice(1), 16);
  const ch = (s: number) => Math.round(((n >> s) & 255) * (1 - t) + to * t);
  return '#' + ((ch(16) << 16) | (ch(8) << 8) | ch(0)).toString(16).padStart(6, '0');
};

export function skinSvg(src: string, frame: string, shirt: number, cloak: Cloak, armor = false): string {
  let s = src;
  if (cloak || armor) {
    // the vest trim and collar sit on the shirt front — the gear now covers it
    s = s.replace(/<path[^>]*fill="#688fa5"[^>]*\/>/i, '').replace(/<path[^>]*stroke="#d3cec0"[^>]*\/>/i, '');
  }
  if (s.includes('<!--gear:')) {
    // back / side view: the frame says where its gear goes
    const g = GEAR_VIEW[frame.includes('_back') ? 'back' : 'side'];
    s = s.replace('<!--gear:under-->', armor ? g.armor : '').replace('<!--gear:over-->', cloak ? g.cloak[cloak] : '');
  } else {
    // Gear goes right after the belt buckle: over the torso, under arms/head/prop.
    // Cloak first, then armor at the same point, so the cloak ends up outermost.
    const BUCKLE = /(<path[^>]*fill="#c4a879"[^>]*\/>)/i;
    if (cloak) s = s.replace(BUCKLE, `$1${CLOAK_ART[cloak]}`);
    if (armor) s = s.replace(BUCKLE, `$1${ARMOR_ART}`);
  }
  if (shirt) {
    const c = SHIRTS[shirt];
    s = s.replace(/#456d89/gi, c).replace(/#688fa5/gi, mix(c, 255, 0.25));
  }
  return s;
}


/** The art's own shirt fills: the body colour and its highlight. */
export const SHIRT_BODY = '#456d89', SHIRT_LIGHT = '#688fa5';
/** Highlight of a recoloured shirt (shirt 0 keeps the art's own SHIRT_LIGHT). */
export const shirtLight = (shirt: number) => (shirt ? mix(SHIRTS[shirt], 255, 0.25) : SHIRT_LIGHT);

/** Every gear combination a keeper can wear, as the skin-id suffix after the shirt digit. */
export const GEAR_VARIANTS = ['', 'a', 'f', 'h', 'af', 'ah'] as const;
export const gearOf = (v: string): { cloak: Cloak; armor: boolean } => ({
  armor: v.includes('a'),
  cloak: v.includes('f') ? 'furcloak' : v.includes('h') ? 'heatcloak' : null,
});

/** `svg` with the shirt body and highlight fills swapped for the given colours (mask renders). */
export const withShirtColors = (svg: string, body: string, light: string) =>
  svg.replace(/#456d89/gi, body).replace(/#688fa5/gi, light);
