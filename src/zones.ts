// Island zones for asset streaming.
//
// The world is four far-flung major islands plus the Core, with open ocean
// between (shared/world.js). Each island's folk clips are their own atlas packs
// (src/assets.ts ZONE_PACKS), loaded once the player is committed to that island
// and kept for the rest of the session (a revisit costs nothing).
//
// "Committed" means within COMMIT_R of the island's centre: about 110 tiles of
// open water off its shore. Heading out and turning back short of that loads
// nothing; past it, a boat (6.2 tiles/s) still has ~20s of sea before landfall,
// far longer than a few hundred KB of atlas takes. If a folk member of an
// unloaded kind does turn up first (a warp, a respawn), FolkView asks for its
// pack on the spot and it stands in its base pose until the clips arrive.

import { CORE, ISLES, ISLE_R } from '../shared/world.js';

const ZONES: [string, number, number][] = [
  ['woods', ISLES[0][0], ISLES[0][1]],
  ['dunes', ISLES[1][0], ISLES[1][1]],
  ['spire', ISLES[2][0], ISLES[2][1]],
  ['marsh', ISLES[3][0], ISLES[3][1]],
  ['core', CORE[0], CORE[1]],
];

/** How far from an island's centre its packs start loading, in tiles. */
export const COMMIT_R = ISLE_R + 110;

/** The zones whose packs should be loaded for a player standing at (x, y). */
export function zonesNear(x: number, y: number): string[] {
  const out: string[] = [];
  for (const [id, cx, cy] of ZONES) if (Math.hypot(x - cx, y - cy) < COMMIT_R) out.push(id);
  return out;
}
