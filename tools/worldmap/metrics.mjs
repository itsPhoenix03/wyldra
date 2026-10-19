// Terrain metrics for a generated world: per-region land / lake / fragmentation /
// terrace / reachability numbers, so island-shape changes can be compared
// before and after. Pure function of the world — no I/O.
//
//   import { computeMetrics, formatSummary } from './metrics.mjs';
//
// Regions: each major island = land within ISLE_R+30 of its centre, Core = land
// within 30 of CORE, minors = all remaining land.
import {
  SIZE, T, TILE_KEYS, ISLES, ISLE_R, CORE, MINOR_ISLES, MOUNTAINS, TEMPLE_PIECES,
  MAJOR_ISLANDS, findSpawn, findMedicSpawns,
} from '../../shared/world.js';
import { NODE } from '../../shared/defs.js';

const N = SIZE * SIZE;
const NONE = 255;                                   // region id of a water tile
export const REGIONS = [...MAJOR_ISLANDS.map((m) => ({ id: m.id, centre: m.center, limit: ISLE_R + 30 })),
  { id: 'core', centre: CORE, limit: 30 },
  { id: 'minors', centre: null, limit: 0 }];
const MINOR = REGIONS.length - 1;
const NODE_NAMES = Object.fromEntries(Object.entries(NODE).map(([k, v]) => [v, k.toLowerCase()]));
const TOP = 5;
const SPECK = 10;                                    // land components this small are "specks"

const idx = (x, y) => y * SIZE + x;
const inMap = (x, y) => x >= 0 && y >= 0 && x < SIZE && y < SIZE;

// 4-connected component labelling over `member` (Uint8Array 0/1). With `group`,
// neighbours must also share the group id. lab is 1-based, 0 = not a member.
function label(member, group) {
  const lab = new Int32Array(N);
  const queue = new Int32Array(N);
  const comps = [];
  for (let s = 0; s < N; s++) {
    if (!member[s] || lab[s]) continue;
    const id = comps.length + 1;
    let qh = 0, qt = 0, sx = 0, sy = 0;
    queue[qt++] = s; lab[s] = id;
    const visit = (j, g) => {
      if (member[j] && !lab[j] && (!group || group[j] === g)) { lab[j] = id; queue[qt++] = j; }
    };
    while (qh < qt) {
      const i = queue[qh++];
      const x = i % SIZE, y = (i / SIZE) | 0;
      sx += x; sy += y;
      const g = group ? group[i] : 0;
      if (x > 0) visit(i - 1, g);
      if (x < SIZE - 1) visit(i + 1, g);
      if (y > 0) visit(i - SIZE, g);
      if (y < SIZE - 1) visit(i + SIZE, g);
    }
    comps.push({ size: qt, cx: sx / qt, cy: sy / qt, first: s });
  }
  return { lab, comps };
}

// Tiles reachable over land from `starts`; a step to a 4-neighbour is allowed
// when elev[dst] <= elev[src] + climb (descending is always free).
function reach(world, starts, climb) {
  const { tiles, elev } = world;
  const seen = new Uint8Array(N);
  const queue = new Int32Array(N);
  let qh = 0, qt = 0;
  for (const s of starts) if (!seen[s]) { seen[s] = 1; queue[qt++] = s; }
  while (qh < qt) {
    const i = queue[qh++];
    const lim = elev[i] + climb;
    const x = i % SIZE, y = (i / SIZE) | 0;
    const step = (j) => {
      if (!seen[j] && tiles[j] !== T.WATER && elev[j] <= lim) { seen[j] = 1; queue[qt++] = j; }
    };
    if (x > 0) step(i - 1);
    if (x < SIZE - 1) step(i + 1);
    if (y > 0) step(i - SIZE);
    if (y < SIZE - 1) step(i + SIZE);
  }
  return seen;
}

const topSizes = (comps, n = TOP) => comps.map((c) => c.size).sort((a, b) => b - a).slice(0, n);
const topClusters = (comps, n = TOP) => [...comps].sort((a, b) => b.size - a.size).slice(0, n)
  .map((c) => ({ size: c.size, cx: Math.round(c.cx), cy: Math.round(c.cy) }));
const zeroed = (keys) => Object.fromEntries(keys.map((k) => [k, 0]));

export function computeMetrics(world, seed, genMs = null) {
  const t0 = performance.now();
  const { tiles, elev, nodes, decor, tileVis, bergs } = world;

  // --- regions ---
  const region = new Uint8Array(N).fill(NONE);
  const landMask = new Uint8Array(N);
  const waterMask = new Uint8Array(N);
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const i = idx(x, y);
      if (tiles[i] === T.WATER) { waterMask[i] = 1; continue; }
      landMask[i] = 1;
      let r = MINOR;
      for (let k = 0; k < MINOR; k++) {
        const c = REGIONS[k].centre;
        if (Math.hypot(x - c[0], y - c[1]) <= REGIONS[k].limit) { r = k; break; }
      }
      region[i] = r;
    }
  }

  // --- water: ocean = component containing (0,0); everything else is a lake ---
  const water = label(waterMask, null);
  const ocean = water.lab[0];
  if (tiles[0] !== T.WATER) throw new Error('tile (0,0) is not water — cannot identify the ocean');
  const votes = new Uint32Array((water.comps.length + 1) * REGIONS.length);   // lake -> neighbouring land regions
  const coast = [];
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const i = idx(x, y);
      if (!landMask[i]) continue;
      let touchesOcean = false;
      for (const [dx, dy] of [[1, 0], [-1, 0], [0, 1], [0, -1]]) {
        const nx = x + dx, ny = y + dy;
        if (!inMap(nx, ny)) continue;
        const j = idx(nx, ny);
        if (!waterMask[j]) continue;
        const l = water.lab[j];
        if (l === ocean) touchesOcean = true; else votes[l * REGIONS.length + region[i]]++;
      }
      if (touchesOcean) coast.push(i);
    }
  }
  const lakesOf = REGIONS.map(() => []);
  water.comps.forEach((c, k) => {
    const l = k + 1;
    if (l === ocean) return;
    let best = -1, bv = 0;
    for (let r = 0; r < REGIONS.length; r++) {
      const v = votes[l * REGIONS.length + r];
      if (v > bv) { bv = v; best = r; }
    }
    if (best >= 0) lakesOf[best].push(c);   // a water body with no land neighbour cannot occur
  });

  // --- land fragmentation (components never cross a region boundary) ---
  const land = label(landMask, region);
  const landCompsOf = REGIONS.map(() => []);
  for (const c of land.comps) landCompsOf[region[c.first]].push(c);

  // --- reachability from the coast, and from the spawn ---
  const reach1 = reach(world, coast, 1);
  const reach2 = reach(world, coast, 2);
  const stuck1 = new Uint8Array(N), stuck2 = new Uint8Array(N);
  for (let i = 0; i < N; i++) {
    if (landMask[i] && !reach1[i]) stuck1[i] = 1;
    if (landMask[i] && !reach2[i]) stuck2[i] = 1;
  }
  const stuckComps1 = label(stuck1, region);
  const stuckComps2 = label(stuck2, region);
  const stuckOf = (comps) => REGIONS.map((_, r) => comps.comps.filter((c) => region[c.first] === r));

  const spawn = findSpawn(world);
  const spawnI = idx(spawn[0], spawn[1]);
  const fromSpawn1 = reach(world, [spawnI], 1);
  const fromSpawn2 = reach(world, [spawnI], 2);

  // --- per-region tallies ---
  const R = REGIONS.map((reg) => ({
    id: reg.id,
    centre: reg.centre,
    land: 0,
    byTile: zeroed(TILE_KEYS.filter((_, t) => t !== T.WATER)),
    byElev: { 1: 0, 2: 0, 3: 0 },
    maxDist: reg.centre ? 0 : null,
    nodes: {},
    decor: 0, tileVis: 0,
    cliffEdges: 0, ledgeEdges: 0, bumps: 0,
    coastTiles: 0, coastLandable: 0,
  }));
  // ocean-coast tiles, and how many a swimmer or boat can step onto (level 1: a +1 climb from water)
  for (const i of coast) {
    R[region[i]].coastTiles++;
    if (elev[i] === 1) R[region[i]].coastLandable++;
  }
  // tree clumps (4-connected tree tiles): forests read as few large clumps, scatter as many tiny ones
  const treeMask = new Uint8Array(N);
  for (const [i, k] of nodes) if (k === NODE.TREE) treeMask[i] = 1;
  const treeClumps = label(treeMask, region);
  const clumpsOf = REGIONS.map(() => []);
  for (const c of treeClumps.comps) clumpsOf[region[c.first]].push(c);
  const spawnReach = REGIONS.map(() => ({ plus1: 0, plus2: 0 }));
  for (let y = 0; y < SIZE; y++) {
    for (let x = 0; x < SIZE; x++) {
      const i = idx(x, y);
      if (!landMask[i]) continue;
      const r = region[i], s = R[r];
      s.land++;
      s.byTile[TILE_KEYS[tiles[i]]]++;
      s.byElev[elev[i]]++;
      if (s.centre) s.maxDist = Math.max(s.maxDist, Math.hypot(x - s.centre[0], y - s.centre[1]));
      if (tileVis[i]) s.tileVis++;
      if (decor.has(i)) s.decor++;
      if (nodes.has(i)) {
        const k = NODE_NAMES[nodes.get(i)];
        s.nodes[k] = (s.nodes[k] || 0) + 1;
      }
      if (fromSpawn1[i]) spawnReach[r].plus1++;
      if (fromSpawn2[i]) spawnReach[r].plus2++;
      // terraces: land-land edges to the right / below, plus isolated bumps and pits
      let differing = 0;
      for (const [dx, dy] of [[1, 0], [0, 1], [-1, 0], [0, -1]]) {
        const nx = x + dx, ny = y + dy;
        if (!inMap(nx, ny)) continue;
        const j = idx(nx, ny);
        if (!landMask[j]) continue;
        const d = Math.abs(elev[i] - elev[j]);
        if (d > 0) differing++;
        if (dx >= 0 && dy >= 0) {
          if (d >= 2) s.cliffEdges++;
          else if (d === 1) s.ledgeEdges++;
        }
      }
      if (differing >= 3) s.bumps++;
    }
  }
  // per-minor-island breakdown (land assigned to the nearest minor centre)
  const minorIsles = MINOR_ISLES.map((m) => ({
    x: m.x, y: m.y, r: m.r, kind: m.kind, land: 0, byElev: { 1: 0, 2: 0, 3: 0 }, maxDist: 0,
  }));
  for (let i = 0; i < N; i++) {
    if (region[i] !== MINOR) continue;
    const x = i % SIZE, y = (i / SIZE) | 0;
    let b = 0, bd = Infinity;
    MINOR_ISLES.forEach((m, k) => {
      const d = Math.hypot(x - m.x, y - m.y);
      if (d < bd) { bd = d; b = k; }
    });
    minorIsles[b].land++;
    minorIsles[b].byElev[elev[i]]++;
    minorIsles[b].maxDist = Math.max(minorIsles[b].maxDist, bd);
  }

  // --- assemble ---
  R.forEach((s, r) => {
    const lakes = lakesOf[r];
    s.lakes = {
      count: lakes.length,
      top: topSizes(lakes),
      inlandWater: lakes.reduce((a, c) => a + c.size, 0),
      tiny: lakes.filter((c) => c.size <= SPECK).length,
    };
    s.landComponents = {
      count: landCompsOf[r].length,
      top: topSizes(landCompsOf[r]),
      specks: landCompsOf[r].filter((c) => c.size <= SPECK).length,
      under12: landCompsOf[r].filter((c) => c.size < 12).length,
      mainShare: s.land ? Math.round(1000 * topSizes(landCompsOf[r], 1)[0] / s.land) / 10 : 0,
    };
    s.coast = { tiles: s.coastTiles, landable: s.coastLandable, pct: s.coastTiles ? Math.round(1000 * s.coastLandable / s.coastTiles) / 10 : 0 };
    delete s.coastTiles; delete s.coastLandable;
    const clumps = clumpsOf[r];
    s.treeClumps = { count: clumps.length, top: topSizes(clumps), inClumps10: clumps.filter((c) => c.size >= 10).reduce((a, c) => a + c.size, 0) };
    s.nodeTotal = Object.values(s.nodes).reduce((a, b) => a + b, 0);
    const u1 = stuckOf(stuckComps1)[r], u2 = stuckOf(stuckComps2)[r];
    s.reach = {
      unreachablePlus1: u1.reduce((a, c) => a + c.size, 0),
      unreachablePlus1Clusters: u1.length,
      unreachablePlus1Top: topClusters(u1),
      unreachablePlus2: u2.reduce((a, c) => a + c.size, 0),
      unreachablePlus2Top: topClusters(u2),
    };
    if (r === 0) s.spawnReach = spawnReach[r];   // the spawn only lives on the Woods
    s.maxDist = s.maxDist === null ? null : Math.round(s.maxDist * 10) / 10;
  });

  // --- marker checks ---
  const tileInfo = (x, y) => {
    const i = idx(x, y);
    return { x, y, tile: TILE_KEYS[tiles[i]], elev: elev[i], land: !!landMask[i] };
  };
  MAJOR_ISLANDS.forEach((m, k) => {
    const [cx, cy] = m.center, mi = idx(cx, cy);
    const note = tileInfo(cx + 7, cy + 4);
    R[k].monolith = {
      ...tileInfo(cx, cy),
      reachPlus1: !!reach1[mi], reachPlus2: !!reach2[mi],
      ...(k === 0 ? { reachFromSpawnPlus1: !!fromSpawn1[mi], reachFromSpawnPlus2: !!fromSpawn2[mi] } : {}),
    };
    R[k].note = note;
  });
  // resources the wire tests gather round the spawn
  const nearSpawn = { r15: {}, r30: {} };
  for (const [i, k] of nodes) {
    const d = Math.hypot((i % SIZE) - spawn[0], ((i / SIZE) | 0) - spawn[1]);
    const name = NODE_NAMES[k];
    if (d <= 15) nearSpawn.r15[name] = (nearSpawn.r15[name] || 0) + 1;
    if (d <= 30) nearSpawn.r30[name] = (nearSpawn.r30[name] || 0) + 1;
  }
  const markers = {
    spawn: tileInfo(spawn[0], spawn[1]),
    nearSpawn,
    medics: findMedicSpawns(world).map((m) => ({
      id: m.id, ...tileInfo(m.x, m.y), hut: tileInfo(m.hutX, m.hutY),
    })),
    mountains: MOUNTAINS.map((m) => {
      const es = [];
      for (let dy = -1; dy <= 1; dy++) for (let dx = -1; dx <= 1; dx++) es.push(elev[idx(m.x + dx, m.y + dy)]);
      return { key: m.key, x: m.x, y: m.y, centreLand: !!landMask[idx(m.x, m.y)],
        footprintLand: es.filter((e) => e > 0).length, footprintElevMin: Math.min(...es), footprintElevMax: Math.max(...es) };
    }),
    temple: TEMPLE_PIECES.map((p) => ({ key: p.key, ...tileInfo(p.i % SIZE, (p.i / SIZE) | 0) })),
  };

  return {
    seed,
    genMs: genMs === null ? null : Math.round(genMs * 10) / 10,
    analysisMs: Math.round((performance.now() - t0) * 10) / 10,
    size: SIZE,
    totals: {
      land: R.reduce((a, s) => a + s.land, 0),
      oceanTiles: water.comps[ocean - 1].size,
      lakes: R.reduce((a, s) => a + s.lakes.count, 0),
      nodes: nodes.size, decor: decor.size, bergs: bergs.size,
    },
    regions: Object.fromEntries(R.map((s) => [s.id, s])),
    minorIsles,
    markers,
  };
}

// --- human-readable summary -------------------------------------------------
const fmt = (n) => n.toLocaleString('en-US');
const kv = (o) => Object.entries(o).filter(([, v]) => v).map(([k, v]) => `${k} ${fmt(v)}`).join(', ') || 'none';
const at = (c) => `(${c.cx},${c.cy})`;
const yn = (b) => (b ? 'yes' : 'NO');

export function formatSummary(m) {
  const out = [];
  out.push(`seed ${m.seed}   genWorld ${m.genMs} ms   analysis ${m.analysisMs} ms   `
    + `land ${fmt(m.totals.land)}, lakes ${m.totals.lakes}, nodes ${fmt(m.totals.nodes)}, decor ${fmt(m.totals.decor)}, bergs ${fmt(m.totals.bergs)}`);
  for (const s of Object.values(m.regions)) {
    out.push('');
    out.push(`== ${s.id.toUpperCase()}${s.centre ? `  centre ${s.centre[0]},${s.centre[1]}` : ''}   land ${fmt(s.land)}`
      + (s.maxDist === null ? '' : `   max dist ${s.maxDist}`));
    out.push(`  tiles  ${kv(s.byTile)}`);
    out.push(`  elev   L1 ${fmt(s.byElev[1])}  L2 ${fmt(s.byElev[2])}  L3 ${fmt(s.byElev[3])}`);
    out.push(`  lakes  ${s.lakes.count} (${s.lakes.tiny} tiny <=${SPECK})  largest [${s.lakes.top.join(', ')}]  inland water ${fmt(s.lakes.inlandWater)}`);
    out.push(`  land components  ${s.landComponents.count} (${s.landComponents.specks} specks <=${SPECK}, ${s.landComponents.under12} under 12)  largest [${s.landComponents.top.map(fmt).join(', ')}]  main ${s.landComponents.mainShare}%`);
    out.push(`  coast  ${fmt(s.coast.tiles)} ocean-coast tiles, ${fmt(s.coast.landable)} landable at L1 (${s.coast.pct}%)`);
    out.push(`  nodes  ${fmt(s.nodeTotal)}: ${kv(s.nodes)}   decor ${fmt(s.decor)}   tileVis ${fmt(s.tileVis)}`);
    if (s.treeClumps.count) out.push(`  tree clumps  ${fmt(s.treeClumps.count)}  largest [${s.treeClumps.top.join(', ')}]  trees in clumps >=10: ${fmt(s.treeClumps.inClumps10)}`);
    out.push(`  terraces  cliff edges ${fmt(s.cliffEdges)}  ledge edges ${fmt(s.ledgeEdges)}  bumps/pits ${fmt(s.bumps)} (${(100 * s.bumps / Math.max(1, s.land)).toFixed(2)}% of land)`);
    const r = s.reach;
    out.push(`  reach  not walkable with +1 climb: ${fmt(r.unreachablePlus1)} (${(100 * r.unreachablePlus1 / Math.max(1, s.land)).toFixed(1)}%) in ${r.unreachablePlus1Clusters} clusters`
      + (r.unreachablePlus1Top.length ? ` largest ${r.unreachablePlus1Top.map((c) => `${c.size}@${at(c)}`).join(' ')}` : '')
      + `;  with +2: ${fmt(r.unreachablePlus2)}`
      + (r.unreachablePlus2Top.length ? ` largest ${r.unreachablePlus2Top.map((c) => `${c.size}@${at(c)}`).join(' ')}` : ''));
    if (s.spawnReach) out.push(`  walkable from spawn  +1: ${fmt(s.spawnReach.plus1)} (${(100 * s.spawnReach.plus1 / s.land).toFixed(1)}%)   +2: ${fmt(s.spawnReach.plus2)} (${(100 * s.spawnReach.plus2 / s.land).toFixed(1)}%)`);
    if (s.monolith) {
      const mo = s.monolith;
      out.push(`  monolith (${mo.x},${mo.y}) land ${yn(mo.land)} ${mo.tile} elev ${mo.elev}  reachable from coast +1 ${yn(mo.reachPlus1)} +2 ${yn(mo.reachPlus2)}`
        + ('reachFromSpawnPlus1' in mo ? `  from spawn +1 ${yn(mo.reachFromSpawnPlus1)} +2 ${yn(mo.reachFromSpawnPlus2)}` : ''));
      out.push(`  note tile (${s.note.x},${s.note.y}) land ${yn(s.note.land)} ${s.note.tile} elev ${s.note.elev}`);
    }
  }
  out.push('');
  out.push('== MINOR ISLES (nearest-centre split)');
  for (const mi of m.minorIsles) {
    out.push(`  ${String(mi.x).padStart(4)},${String(mi.y).padStart(4)} r${String(mi.r).padStart(2)} ${mi.kind.padEnd(11)} land ${String(mi.land).padStart(4)}  L1 ${mi.byElev[1]} L2 ${mi.byElev[2]} L3 ${mi.byElev[3]}  max dist ${mi.maxDist.toFixed(1)}`);
  }
  out.push('');
  out.push('== MARKERS');
  const mk = m.markers;
  out.push(`  spawn (${mk.spawn.x},${mk.spawn.y}) ${mk.spawn.tile} elev ${mk.spawn.elev}`);
  out.push(`  near spawn  <=15: ${kv(mk.nearSpawn.r15)}   <=30: ${kv(mk.nearSpawn.r30)}`);
  for (const me of mk.medics) out.push(`  ${me.id} (${me.x},${me.y}) ${me.tile} elev ${me.elev}   hut (${me.hut.x},${me.hut.y}) ${me.hut.tile} elev ${me.hut.elev}`);
  for (const mo of mk.mountains) out.push(`  ${mo.key} (${mo.x},${mo.y}) footprint land ${mo.footprintLand}/9 elev ${mo.footprintElevMin}..${mo.footprintElevMax}`);
  return out.join('\n');
}
