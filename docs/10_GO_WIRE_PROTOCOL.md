# Go game server — wire protocol (Slices 1-4)

**Status:** authoritative spec for the Go runtime and the client. Both implement against
*this document*, not against each other.

Companion: `control/PROTOCOL.md` (the Node ticket format). This document covers only the
WebSocket between the client and the **Go** game server.

---

## 1. Connection

```
Client ──HTTP──> Node control plane (:8090)   POST /api/join -> { ticket, ws, worldId, name }
Client ──WS────> Go game server     (:8082)   first frame: { t:'auth', ticket }
```

The Go server verifies the ticket signature **offline** with the Ed25519 public key it
fetched at boot. It never calls Node on the connection path.

- Ticket invalid/expired → `{ t:'authfail', reason }`, then close. No world data is sent.
- Ticket valid → the server **routes it** (§11) and then replies `init` (§3); the session begins.

`authfail` reasons: `malformed`, `bad-signature`, `bad-json`, `expired` (ticket verification),
the routing refusals `wrong-instance`, `unknown-world` and `world-unavailable` (§11), and the
admission refusal `room-full` (§11.4). Every one of them is sent **before** any `init` or chunk
data, and the socket is closed immediately afterwards.

The client's identity (`userId`, `name`) comes from the **ticket**, never from a client-sent
field. A client that sends its own `name` in `auth` is ignored.

---

## 2. Framing

All messages are JSON text frames, `{ t: "<type>", ... }` — same shape as the legacy Node
server, so existing client handlers port over with minimal change.

**Exception:** chunk payloads are base64 strings inside JSON (§4). Binary frames are
deliberately NOT used in Slice 1 — debuggability over the last few percent of bandwidth.

---

## 3. `init` — sent once, immediately after `auth` succeeds

```jsonc
{
  "t": "init",
  "id": "<playerId>",          // session id, as today
  "name": "<from ticket>",
  "seed": "hearth-1",
  "worldVersion": 4,
  "size": 1280,
  "chunk": 64,                 // CHUNK_SIZE, so the client never hardcodes it
  "x": 168, "y": 168, "z": 0,  // authoritative spawn
  "hp": 10, "hunger": 10, "thirst": 10,
  "inv": { ... }, "tools": [...], "gear": [...],
  "players": [ { id, x, y, z, name, b, eq } ],
  "time": 0.31, "day": 1,
  "mono": [false,false,false,false],
  "won": false, "maxHp": 10, "wornGear": null,
  "dev": false,                // §10.6 — may this session use `dev` / `devcmd`?
  "weather": null,             // §9.4 — current front kind, or null
  "infected": [],              // §9.5 — corrupted tile indices
  "medics": [                  // §10.2 — exactly two, see below
    { "id": "medic-woods", "islandId": "woods", "sprite": "medic",
      "x": 190, "y": 172, "hutSprite": "medic_hut", "hutX": 190, "hutY": 170 },
    { "id": "medic-spire", "islandId": "spire", "sprite": "medic_snow",
      "x": 189, "y": 1107, "hutSprite": "medic_hut_snow", "hutX": 189, "hutY": 1105 }
  ],
  "camps": [                   // §13 — the island camps (folk huts + fires)
    { "id": "camp-woods", "islandId": "woods", "hutSprite": "folk_hut",
      "hutX": 172, "hutY": 187, "fireX": 174, "fireY": 190 }
  ]
}
```

**`hunger` and `thirst` are integers on the wire**, here and in every `stat` frame (§8.2).
The simulation carries them as fractions — the survival tick drains 0.055/0.083 per 5 s
block — but the wire value is `ceil()`, matching `Math.ceil(p.hunger)` in `server/index.js`.
Ceil, not floor: a bar reads 1 until the value has genuinely reached 0. Clients must not
round defensively; a fractional value here is a server bug.

**`dev`** is a plain boolean reporting whether this session holds the dev claim (§10.6) —
exactly the value `devAllowed` will use when a `dev` or `devcmd` frame arrives. It exists so the
client can render an honest tester panel instead of opening one whose buttons silently produce a
refusal toast. It is a *report*, never an input: the gate remains the signed ticket claim, and a
client that ignores this field and sends `devcmd` anyway is refused exactly as before. `false` is
sent explicitly rather than the key being omitted, so a client can distinguish "not a dev" from
"talking to a server too old to say".

**`medics`** is the whole medic roster. There are exactly two per world and both are pure
functions of the seed, so they ride in `init` rather than being streamed with the chunks that
contain them; the client needs them before the chunk containing a medic arrives, because the
hut tile is solid. The field names are exactly those of `findMedicSpawns()` in
`shared/world.js`, so the client can feed the array straight into its own
`medicBlockTiles(medics)` helper — the blocked hut tile is `hutY * SIZE + hutX`, and no
separate tile list is sent. `id` is the same identifier the `medic` frames (§10.2) are keyed
on. Coordinates above are for seed `hearth-1`.

**`players` shape changed** from the legacy array-of-arrays `[pid,x,y,equip,z,name,b]` to
objects. The client's `init` handler must be updated to match — flagged here because it is
the one place where "implement the spec" and "the old client keeps working" conflict.

`init` carries **no terrain.** Terrain arrives as chunks (§4). The client must be able to
render a "loading" state between `init` and the first chunk batch.

**This is the load-bearing change of Slice 1:** the client no longer calls `genWorld()`.
The Go server is the sole source of truth for what terrain *is*.

---

## 4. Chunks

The world is `1280 × 1280`, divided into `64 × 64` tile chunks → a `20 × 20` chunk grid,
400 chunks total. Chunk `(cx, cy)` covers tiles `x ∈ [cx*64, cx*64+63]`, same for `y`.

### 4.1 Push model

The server pushes; the client never requests. On connect, and whenever a player crosses a
chunk boundary, the server sends every chunk within **Chebyshev radius 2** of the player's
chunk (a 5×5 block, 25 chunks) that it has not already sent to that player. The server
tracks a per-player `sentChunks` set.

Rationale: pushing avoids a request storm on join and keeps the client dumb. The radius
covers well beyond the viewport at any supported zoom.

### 4.2 `chunk` message

```jsonc
{
  "t": "chunk",
  "cx": 2, "cy": 2,
  "tiles":     "<base64 RLE>",   // T.* values, 0..5
  "elev":      "<base64 RLE>",   // 0..3
  "veins":     "<base64 RLE>",   // 0 none, 1 iron, 2 diamond
  "waterTemp": "<base64 RLE>",   // 0 temperate, 1 freezing, 2 hot
  "tileVis":   "<base64 RLE>",   // 0/1 decorative variant flag
  "nodes": [[localIdx, kind], ...],     // resource nodes currently present
  "decor": [[localIdx, "key"], ...],    // static decor prop keys (the client hides one under anything built on its tile)
  "bergs": [localIdx, ...],             // iceberg water tiles
  "digs":  [localIdx, ...],             // carved mine tiles (z=1) in this chunk
  "structs": [[localIdx, "kind", hp, dir, lvl], ...]  // player structures
}
```

`localIdx` is `(y - cy*64) * 64 + (x - cx*64)`, range `0..4095`.

`structs` entries are `[localIdx, kind, hp, dir, lvl]` — matching the legacy `init` tuple.
`dir` drives fence and decor orientation on the client and must not be dropped. It is a
facing 0–3 (`src/rotation.ts`: front, mirrored, back, back mirrored — or the corner railing's
four corners); the server stores any other value as 0.

Omitted array fields mean empty. A chunk with no nodes may omit `"nodes"` entirely.

### 4.3 RLE encoding — normative

Each layer is 4096 bytes before encoding. Encode as a sequence of 2-byte runs:

```
byte 0: value
byte 1: run length, 1..255   (a run of 300 becomes 255 then 45)
```

Concatenate runs, then standard base64 (RFC 4648 §4, **with** padding).
Decoding MUST produce exactly 4096 bytes; a client that decodes a different length must
treat the chunk as corrupt and ignore it rather than render garbage.

Terrain of this kind is highly uniform, so a typical ocean chunk is a handful of runs.

**`veins` is masked to land on the wire.** Worldgen populates `veins` from noise on every
tile including open ocean, which makes it high-entropy and, measured, **1376 of the 1568
bytes of an all-water chunk**. Ore is only ever reachable underground beneath land, so the
server sends `0` for `veins` on water tiles. The server retains the true value; this is a
wire optimisation only, and it cuts a typical ocean chunk to roughly 200 bytes.

Radius-2 windows are clipped at world edges — a corner spawn receives 9 chunks, not 25.

### 4.4 Mutation after the fact

Chunks are a *snapshot at send time*. Subsequent changes arrive as the existing incremental
messages (`build`, `node`, `dig`, `chit`, …) exactly as they do today. The client applies
them on top of its chunk cache. A chunk is never re-sent for a mutation.

---

## 4.5 The clock

The legacy server piggybacks `time`/`day` on the per-tick `cre` message. Slices 1 and 2 had no
creatures to carry it, so the clock was its own broadcast:

```jsonc
{ "t": "time", "time": 0.31, "day": 1 }
```

**Slice 3 removed that frame.** With creatures simulated, `cre` is sent once per tick again and
carries the clock exactly as the legacy server does (see §9.2). The client has always handled
both shapes, so nothing on the wire needs a compatibility window.

---

## 5. `pos` — movement

Unchanged in shape from the legacy server, and it keeps **all** the validation added in the
server-authoritative movement work:

- speed budget: `dt` (clamped to 1s) × `6.2` × `1.6` + `1.0` tiles
- collision mirroring the client's rules, z-aware; water passable at `z=0`
- layer changes gated by `zAnchor` — one mineshaft within 3.0 tiles of **both**
  endpoints — plus a 500 ms cooldown
- rejection → `{ t:'fix', x, y, z, b }`, throttled to one per 250 ms per player
- every server-side reposition opens a 1 s grace window suppressing the distance check
- a step that stays inside the tile the player already stands in is never collision-blocked
  (`withinTile`): it enters nothing, and refusing it walled in a keeper who surfaced on the
  mineshaft's own tile. The client also never surfaces there now (`surfaceExit`: the open tile
  nearest the shaft).
- **fall damage**: going over a **ledge** of 2+ elevation levels costs `drop − 1` hp. The drop is
  measured tile edge by tile edge along the step (`fallDrop`), taking the gentler of the two
  axis-by-axis routes, not between the step's two end tiles: a ~0.4-tile update can cross two
  edges at once, and a diagonal down a 1-level staircase (3 → 2 → 1) used to read as a fall.
  Landing in water never hurts. The client draws a worn, crumbling **ledge lip** (earth-toned shadow, broken brink, pebbles) on every tile edge that is
  such a drop (`makeLipTextures` / `lipsAt`) — including the back edges the iso view has no
  cliff face for.

Porting this faithfully is mandatory; it is the security boundary for all other handlers,
since every range check anchors on `p.x/p.y`.

---

## 6. Test hook

`{ t:'warp', x, y, z, b }` — unvalidated placement, gated behind the
`HEARTH_ALLOW_WARP` environment variable, exactly as in the Node server. It broadcasts the
same `pos` shape a normal move does. **Off unless the env var is set.**

`test.mjs` depends on this; without it the suite cannot position a player.

---

## 7. Ports

| Service | Port |
|---|---|
| Node control plane | 8090 |
| **Go game server** | **8082** |
| Legacy Node game server (untouched, still runs) | 8081 |
| Vite dev client | 5173 |

---

## 8. Slice 2 — resources, construction, farming, chests

Slice 2 adds the gameplay handlers. Every one of them keeps the legacy Node
message shape, so a client that already talks to `:8081` needs no change beyond
the `init`/chunk work of Slice 1.

### 8.1 Client → server

| type | payload | notes |
|---|---|---|
| `gather` | `i, seq?, dx?, dy?` | 250 ms shared cooldown, range 2.5 |
| `craft` | `r` | station must be within 4 tiles, and the player on the surface (`z=0`): stations stand there |
| `build` | `i, kind, dir?` | `z=0` only, range 6, dir is a facing 0–3 (else 0) |
| `dig` | `i, seq?, dx?, dy?` | `z=1` only, range 2, needs pick/spick |
| `plant` / `harvest` | `i, crop?` | `z=0` only, range 2.5, on a `farmplot` |
| `furn` | `i, kind` | `z=1` (carved tile) — furniture lives in the mines only |
| `torch` | — | `z=1`, on the player's own carved tile |
| `eq` / `wear` | `k` (or `null`) | must own the tool / gear |
| `use` | `k` | `medicine` throttled to one per 750 ms |
| `water` | — | 250 ms cooldown, adjacent water, cap 10 |
| `chest_open` / `chest_move` | `i, res?, n?` | same layer, range 2.5 |
| `atk` | `seq?, dx?, dy?` | 400 ms cooldown, `z=0` only |

Tile indices are shared across the three layers, so every surface-only rule above checks the
player's `z` as well as the distance — otherwise a keeper in a mine tunnel or a shelter right
below a workbench, a farm plot or a free tile could craft, plant, harvest or build through the
floor.

`seq` is echoed on the resulting `act` and on the `node` / `dig` / `chit`
outcome, and correlates a request to its outcome for reconciliation and dedup
**only** — it is never a security token. An action the server refuses answers
`{ t:'actReject', seq, reason }` and emits **no** `act`.

The server derives the animation clip and the tool from authoritative state, not
from anything the client claims: a bush is always `punch`/`null`, a tree is
`chop`/`axe` only if the player owns an axe, and a dig reports `spick` whenever
the player owns one.

### 8.2 Server → client

`inv`, `msg`, `stat`, `hp`, `act`, `actReject`, `node`, `dig`, `build`, `sd`,
`crop`, `furn`, `torch`, `chest`, `mud`, `berg`, `boat`, `wave`, `win` — all
identical in shape to the legacy server. `regrow` is Go-only (§8.6). `stat` is
`{ "t": "stat", "hunger": <int>, "thirst": <int> }`: both values are `ceil()` of the
fractional simulation state, exactly as `Math.ceil(p.hunger)` in `server/index.js`. See §3.

### 8.3 Chunk overlay fields (extends §4.2)

A chunk is the snapshot every player-made change is layered onto, so it carries
the mutable overlays as well as the generated terrain. All are optional; an
omitted array means empty.

```jsonc
{
  "digs":    [localIdx, ...],                  // carved mine tiles (z=1)
  "torches": [localIdx, ...],                  // lit mine tiles
  "mud":     [localIdx, ...],                  // grass turned to mud by logging
  "removed": [localIdx, ...],                  // nodes harvested, awaiting respawn
  "brokenBergs": [localIdx, ...],              // icebergs smashed by a reinforced hull
  "structs": [[localIdx, kind, hp, dir, lvl], ...],
  "furn":    [[localIdx, kind, z], ...],       // chests, beds, mine decor
  "farms":   [[localIdx, crop, stage], ...],   // stage 0..2
  "mods":    [[localIdx, slot, kind, hp, dir], ...]  // modular building (§12)
}
```

`removed` lists tiles that still appear in the chunk's `nodes` array but are
currently harvested; the client must not draw them until a `{t:'node', hp:-1}`
respawn arrives. A chunk is the whole truth for its nodes: a node it lists but
does not mark `removed` is standing.

A harvested node does not respawn while its tile is built on (a structure, a
farm, any module slot, or a reserved NPC tile). Its timer is pushed back 60 s at
a time until the tile is clear. On load, a node standing on a built tile is
felled the same way. Nothing may be placed over a standing node: `build`
refuses it, as it always has, and `buildmod` answers `modfail` with `why: "node"`.

### 8.4 Rules data

Recipes, node definitions, structure hit points, crops and the medic trade pools
live in **`shared/defs.json`**, which the Go server unmarshals at boot and the
JavaScript side consumes through the generated `shared/defs.js`. Neither side
hardcodes a cost or a hit-point total. Regenerate the wrapper with
`node tools/defs/gen-defs.mjs`; `--check` fails if it has drifted.

### 8.5 Persistence

The Go server writes one save file **per world** — `world.<worldId>.save.json` in
`HEARTH_SAVE_DIR` (default: the working directory), so the default setup writes
`gameserver/world.default.save.json`. Keying on `worldId` is what stops two worlds
in one process from clobbering each other; a single-world process may still pin one
path with `HEARTH_SAVE_PATH`. The server never touches `server/save.json`, which the
legacy server owns. The snapshot carries profiles, structures, digs, torches,
furniture, farms, chest contents, mud, felled-tree counters, broken icebergs and
node respawn timers. Two fields are stored as durations rather than absolute
clocks, because neither clock survives a restart: node respawns as milliseconds
remaining, and farms as ticks elapsed since planting.

**When the map changes, the players stay and the world resets.** Every save
carries the `WORLD_VERSION` and seed it was written under, and every tile index in
it is only meaningful on that terrain. On load (`room.loadSave`):

- **Same seed, same version** — a normal load.
- **Same seed, older version** — a migration (`room.migrateSave`). Kept: every
  profile (name, HP, hunger, thirst, inventory, tools, gear, worn gear, armor),
  the monolith states (`mono`), `won`, `day` and `time`. Dropped: everything
  tile-indexed — node respawns, mud, felled-tree counters, structures, digs,
  torches, furniture, broken icebergs, farms, chest contents and modules (so beds
  and campfires, and with them respawn points, go too). Each kept profile's `x`/`y`
  becomes `persist.NoPos` (-1) and `z` becomes 0: the room never restores a position
  outside the world, so the keeper joins at the normal spawn on the surface instead
  of at coordinates that now mean different ground or inside a mine that is gone.
  One line is logged (`world v4 -> v5: map changed — kept N player profiles, reset
  the world`, plus the backup path below). The next save is written in the current
  version, so a migration runs once; a profile nobody has rejoined keeps `NoPos`
  until they do. Before anything can overwrite the old world, the file is copied
  as-is to `<save file>.v<oldVersion>.bak` beside it (`world.default.save.json.v4.bak`,
  `save.json.v4.bak`; through the optional `persist.Archiver` store capability, so
  in-memory stores skip it). The copy is made once: if that `.bak` already exists a
  rerun leaves it alone, so a crash mid-migration cannot replace the pristine copy.
  A failed backup is logged and does not stop the server.
- **Different seed, a newer version than this build, or no version** — the save is
  ignored and the world starts fresh, profiles included (the next save overwrites it).

The legacy server (`server/index.js`) follows the same rule, with `x`/`y` of -1 in
its profiles.

---

### 8.6 The land heals

`gameserver/room/ecology.go`. Felling trees puts pressure on a 16×16 sector; the 8th felling
sours up to 14 of its grass tiles to mud (`mud`, as before) and the pressure starts over. Ground
under anything built never sours. Then the land recovers on its own:

- **Rest.** A sector where no tree has been felled for 2 minutes is resting (fellings are not
  saved, so after a restart every sector rests). Every 30 s a resting sector sheds one felling of
  pressure, and each of its soured tiles turns back to grass with a 12% chance — about four
  minutes for a tile on average.
- **Regrowth.** A felled tree that grows back eases its sector by one felling and reclaims the
  nearest soured tile within 3 tiles (ties to the lower tile index), resting or not.
- **Never through a building or the blight.** A soured tile under a structure, a farm or any
  module slot, or under corruption, stays mud until it is clear.

```jsonc
{ "t": "regrow", "tiles": [152487, 152490] }   // ascending tile indices, now grass again
```

The chunk overlay's `mud` list (§8.3) only ever carries tiles that are still soured. Creatures
walk over farm plots like any open decor; only a blocking structure is gnawed (a hunter crossing
a field used to chew the plot and its crop away). The legacy server sours ground but never heals
it, and sends no `regrow`.

## 9. Slice 3 — creatures, wildlife, weather, creature combat

All shapes are identical to the legacy server; this section exists so the Slice 3 surface is
enumerated in one place.

### 9.1 Client → server

No new message types. `atk` gains its creature/animal half: the handler now scans creatures
and then animals for the nearest target within **1.5** tiles (`AtkReach`; the legacy server's 2.4 reached too far) and only falls through to
structure demolition when it finds none.

- `act.targetI` carries the struck creature or animal id (e.g. `"c12"`, `"a3"`), or `null` on
  a miss. It was always `null` in Slices 1-2.
- A landed, non-lethal hit emits `chit`; a killing blow does not. `act` fires either way.

### 9.2 `cre` — the per-tick world frame

Sent once per tick to each player whenever at least one player is connected:

```jsonc
{
  "t": "cre",
  "c": [ ["c12", 640.25, 641.00, "crawler"] ],   // id, x, y, type
  "a": [ ["a3", 182.50, 179.75, "deer"] ],       // id, x, y, species
  "f": [ ["camp-woods-0-1", 174.12, 188.40, "villager", "carry", null, "wood"] ], // §13 — id, x, y, kind, act, face, item
  "time": 0.3141, "day": 2,
  "r": 40                                        // §9.2.1 interest radius, tiles
}
```

Positions are rounded to 2 decimal places and the clock to 4, matching the legacy
`+v.toFixed(n)`. Both arrays are emitted in **spawn order**, not map order (see §9.6).

#### 9.2.1 Interest radius and backlog

Each player's frame carries only the creatures, animals and folk within `r` tiles
(Euclidean, `room.InterestRadius` = 40) of that player's position; the view is about 22
tiles across. Something that leaves the frame while near the edge of `r` walked out of
range — the client removes it silently and plays a death only for things that vanish
well inside it (`src/main.ts`, `outOfRange`). The legacy server sends everything and no `r`.

`cre` is a snapshot the next tick supersedes, so a player whose outbound queue is at
least half full (`Session.Backlogged`) is **skipped** for that tick rather than queued
another frame. A browser that stalls briefly (a long frame, a tab switch) therefore
catches up instead of overflowing its queue and being dropped (`dropSlow`). Every other
frame type is still always queued.

Creature types: `crawler`, `stalker`, `brute`, `wisp`, `husk_wolf`, `bog_shambler`,
`frost_wraith`, `drowned`, `blight_lancer`.
Wildlife species: `deer`, `boar` (Woods), `lizard`, `crab` (Dunes), `fox`, `hare` (Spire),
`toad` (Marsh).

### 9.3 Combat and creature-driven frames

| Message | Direction | Meaning |
| --- | --- | --- |
| `chit` | broadcast | `{ id, ang, by, seq }` — a creature or animal took a non-lethal hit. `ang` points away from the attacker (the knockback direction); `by` + `seq` correlate it to the attacker's `act`. |
| `hp` | to one player | Gains an `ang` field when the damage came from a creature: the direction to shove the player. |
| `slow` | to one player | `{ ticks: 30 }` — the frost wraith's chilling touch. |
| `ctel` | broadcast | `{ id, ang, tk }` — a brute or bog shambler has begun its slam windup: 8 ticks to open an engagement, 5 for each follow-up (§13.6). |
| `calert` | broadcast | `{ id }` — a creature has just locked on to a player (§13.3). Once per hunt, not per tick. |
| `shot` | broadcast | `{ fx, fy, tx, ty }` for the brute's blight bolt, plus `kind: "lance"` for the blight lancer's beam. Coordinates rounded to 1 decimal. |
| `sd` | broadcast | Already in Slice 2; creatures now also drive it by gnawing structures. |

### 9.4 Weather

```jsonc
{ "t": "wx", "kind": "sandstorm" }   // or "rain", "snowstorm", or null to clear
```

Weather is a single global state, not per-region. A front starts with probability
`(TICK_MS/1000)/180` per tick — roughly one every 180 s — and lasts 45-90 s. What differs per
biome is who it hurts:

- `rain` (Woods / Marsh) — cosmetic on the server; the client draws it.
- `sandstorm` (Dunes) — 1 hp per 5 s on `SAND` unless **shelter** is within 2 tiles
  (`Room.sheltered`): a blocking structure, a modular `wall` or `fixture` (pillar, door), a
  village hut or a medic's hut. Open decor (`DECOR_NONBLOCKING`: rugs, lanterns, banners…),
  farmplots and modular floors are not shelter.
- `snowstorm` (Spire) — 1 hp per 5 s on `SNOW` unless a **campfire** is within 6 tiles. A fur
  cloak does not help; that is what separates a blizzard from ordinary cold.

**Shelter covers the biome's exposure too.** While a storm blows, a keeper sheltered from it
takes no desert heat (shade beside the wall) or glacial cold (the fire's warmth) either. The
survival cases are first-match, and a sheltered keeper used to fall through the storm case into
plain heat / cold — losing the same 1 hp per 5 s as one standing in the open. With no storm,
heat and cold are back to needing the cloaks.

Being underground (`z != 0`) shelters from all of it. "Blizzard" is the client's
name for `snowstorm`, and ambient snowfall is a permanent client-side particle layer in the
Spire biome — neither is a distinct server state.

### 9.5 Corruption

```jsonc
{ "t": "infect", "tiles": [820481] }
{ "t": "cure",   "tiles": [820481] }
```

Wisps corrupt the ground they drift over (every 25 ticks), and both wisps and frost wraiths
corrupt on being hit and on death; a bog shambler corrupts its own tile plus the four
orthogonals when it dies. Every corrupted tile cures itself after **120 s**. Corrupted tiles
also breed crawlers: a low spawn roll places a crawler on a random infected tile instead of at
the Core.

`init` continues to carry the current `weather` kind and the full `infected` tile list.

### 9.6 Iteration order is part of the protocol

Go randomises map iteration where the JS reference walks a `Map`/`Set` in insertion order, and
several rules resolve ties by "whichever came first". Every such site iterates an explicitly
ordered slice instead of the map, so behaviour is reproducible across runs:

| Ordered mirror | Decides |
| --- | --- |
| `creOrder` | the `cre` array order, the `atk` target scan, wisp/wolf population counts, husk-wolf pack-link, pack enrage, dawn despawn |
| `aniOrder` | the `cre` array order, the `atk` target scan, the wildlife movement pass |
| `playerOrder` | which player a creature targets or damages first, and which player a spawn is biased toward |
| `infOrder` | which infected tile breeds a crawler, and the `cure` payload order |
| `structIndices()` | which structure a brute walks to (ascending tile index; memoised per tick) |

## 10. Slice 4 — the medic NPC, endgame progression, waves, dev tooling

All shapes are identical to the legacy server. This section enumerates the last of the
gameplay surface, and it closes the protocol: after Slice 4 there is no message type in
`server/index.js` that the Go server does not answer.

### 10.1 Client → server

| Type | Fields | Meaning |
| --- | --- | --- |
| `medic` | `medicId`, `action`, `offerId?` | interact with a medic; `action` is `inspect`, `accept` or `decline` |
| `usecore` | `i` (0-3) | spend one Monolith Core to awaken monolith `i` |
| `dev` | — | grant the F9 dev kit (gated, §10.6) |
| `devcmd` | `cmd`, plus per-command fields (§10.6) | run one F10 tester-panel command (gated) |

`medicId` is the stable, seed-derived id `medic-woods` or `medic-spire`. `offerId` is opaque
and server-minted; a client must echo back exactly the `offer.id` it was given.

`usecore.i` must be a JSON integer in 0..3. The legacy handler indexes its arrays with the raw
value — harmless in JS, where a bad index merely misses — so the Go port validates the type
instead of indexing a real array with it. Same for `devcmd cmd:"mono"`.

### 10.2 The medic bargain

Two medics exist per world, both deterministic functions of the seed
(`shared/world.js findMedicSpawns`): one on the Woods, one on the Frozen Spire. **Both are sent
to the client in `init.medics` (§3)** — the client no longer derives them, because on the Go
path it never generates the world. A medic's own
tile and its hut tile are permanently blocked — they cannot be walked through, built on,
harvested, or pathed over by creatures — so a medic can never be attacked or buried.

The state machine holds **at most one live offer per player**:

```
                 inspect (injured, in range, out of combat)
   no offer  ─────────────────────────────────────────────►  live offer
      ▲                                                       │  │  │
      │  decline / accept / 60 s expiry                       │  │  │
      └───────────────────────────────────────────────────────┘  │  │
                                                                 │  │
              inspect again ── same offer id, smaller expiresInMs ┘  │
              accept, paid in full ── medicResult ok, healed to MAX_HP┘
```

Server → client:

```jsonc
// inspect succeeded, or decline cleared the offer (offer: null)
{ "t": "medicOffer", "medicId": "medic-woods", "hp": 3, "maxHp": 10,
  "offer": { "id": "lkyj7a:1", "medicId": "medic-woods",
             "resource": "fiber", "amount": 4, "expiresInMs": 57300 } }

// accept succeeded
{ "t": "medicResult", "ok": true, "medicId": "medic-woods",
  "paid": { "resource": "fiber", "amount": 4 }, "hp": 10, "healed": 7 }

// anything refused
{ "t": "medicResult", "ok": false, "medicId": "medic-woods", "reason": "in-combat" }
```

The offer's own `createdAt` and owning player id are never sent. `expiresInMs` is relative and
is recomputed on every send, so a repeat `inspect` returns the same `id` with a **smaller**
`expiresInMs` — a client that sees it grow has hit a reroll bug.

Timings and radii, all from `server/index.js` and docs/05 §3:

| Constant | Value | Effect |
| --- | --- | --- |
| offer TTL | 60 s | after which the offer is dead and `accept` answers `offer-expired` |
| reroll delay | 20 s | applied after an expiry **or** an explicit `decline` |
| combat lockout | 5 s since `lastDamageAt` | any medic frame answers `in-combat` |
| spam floor | 200 ms per player | extra frames are dropped with no reply at all |
| interaction range | 2.5 tiles | Euclidean, from the medic's tile |

Rejection reasons, in the order the server checks them — the earlier ones therefore win even
when a later one also applies:

| `reason` | Cause |
| --- | --- |
| `unknown-medic` | `medicId` is not one of the world's medics |
| `wrong-level` | the player is underground (`z != 0`) |
| `too-far` | further than 2.5 tiles |
| `in-combat` | took combat damage in the last 5 s |
| `offer-mismatch` | `accept` with no offer, the wrong `offerId`, or the other medic's offer |
| `offer-expired` | `accept` on an offer past its 60 s TTL |
| `dead` | `hp <= 0` |
| `full-health` | `hp >= MAX_HP` — a healthy player is never handed an offer, or charged |
| `insufficient-resource` | the player holds less than `offer.amount` of `offer.resource` |
| `rate-limited` | `inspect` inside the 20 s reroll delay |

**Environmental damage does not arm the combat lockout.** Snow, desert heat, thermal water,
starvation and weather all chip 1 hp without stamping `lastDamageAt`. If they did, the Spire
medic would be permanently unreachable without the very Fur Cloak a player would be visiting
them to survive without.

**Payment is atomic.** The three conditions under which healing could be a no-op — dead,
already full, cannot afford — are all checked *before* the resource is deducted, and nothing
between the check and the deduction can change player state (the room is single-goroutine). So
a player is never charged without being healed, and never healed without being charged.
Treatment always restores to `MAX_HP`, whatever the missing amount.

Offers are drawn from `MEDIC_TRADE_POOLS[islandId]` in `shared/defs.json` — a weighted list of
`{resource, min, max, weight}` — by cumulative weight, with the amount uniform over
`[min, max]`. A player's offer and reroll timer are discarded when they disconnect.

### 10.3 Progression

| Rung | Gate | Enforced by |
| --- | --- | --- |
| Aether Forge | `RECIPES.forge.station == "workbench"` | the ordinary `craft` station check |
| Monolith Core | `RECIPES.core.station == "forge"` | same |
| Awaken a monolith | `usecore` within **3** tiles of `MONOLITHS[i]`, holding a Core, monolith not already lit | `usecore` |
| World Engine | `RECIPES.engine.station == "forge"`, built **only** on `ACTIVATION_I` (the Core dais), **only** with all four monoliths lit | `build` |

A successful `usecore` broadcasts `{"t":"mono","i":i}` and charges one `core`. `init` carries
the full `mono` array and `won`.

Lighting a monolith raises `strength = 1 + (monoliths lit)`, which the Slice 3 spawn gates
read: brutes need `strength >= 3`, blight lancers `>= 2`, and the population cap is
`2 + strength` by day / `6 + 3*strength` at night. Progress makes the world harder immediately.

Refused Engine builds answer with `msg`, not a rejection type:

- off the dais — `The World Engine must be built on the activation dais at the temple heart.`
- monoliths unlit — `All 4 Monoliths must be awakened first.`

### 10.4 The final assault

Placing the Engine arms a four-minute wave and broadcasts `{"t":"wave","secs":240}`. The client
counts down locally; the server sends no further ticks.

While a wave is live:

- the creature cap jumps to **20** (from `2 + strength` / `6 + 3*strength`),
- every creature targets the Engine tile instead of a player,
- a creature within **1.6** tiles gnaws the Engine every 5th tick for 1 damage, or 3 for a
  brute or bog shambler,
- a blight lancer's beam does **20** to the Engine rather than vaporising it outright, which is
  the only structure that survives a beam at all.

Three things end a wave, and all three broadcast `{"t":"wave","secs":0}` so the client's
countdown stops:

| Ending | Extra broadcast |
| --- | --- |
| the timer runs out | `{"t":"win"}`, and `won` latches true |
| a creature gnaws the Engine down | `msg` `THE WORLD ENGINE WAS DESTROYED! Rebuild it to try again.` |
| a lancer beam finishes the Engine | the same `msg` |

A keeper cannot end it: `atk` never picks the Engine as a structure to demolish, during the wave
or after it, so defenders can swing beside it freely (a swing next to it falls to the nearest
other structure, if any). Earlier builds of both servers let a player demolish it, which
cancelled the assault and refunded half its cost.

`won` zeroes the creature cap for good and stops the nightly Blight Storm erosion of wooden
structures. It is persisted, so a won world stays won across a restart. A destroyed Engine
leaves `won` false: rebuild it and the four minutes start again.

### 10.5 Ordered mirrors (extends §9.6)

Slice 4 adds no ordered mirror, and that is deliberate rather than an omission. `medicOffers`
and `medicRerollAt` are keyed by session id, hold at most one entry per player, and are never
iterated — no outcome can depend on their order, so mirroring them would be dead weight. `mono`
is a fixed four-element array and `wave` is a single struct.

### 10.6 `dev` / `devcmd` — gating

**The legacy server gates both on a process-wide `DEV` env var. The Go server does not.** One
Go process hosts rooms for many players, so a process-wide switch is either on for everyone in
every room or off for everyone. Per docs/09 §7, the authority is a **per-player claim in the
signed ticket** the control plane issues:

```jsonc
// ticket payload — `dev` is optional
{ "userId": "...", "worldId": "...", "instanceId": "...", "name": "...",
  "iat": 1757000000000, "exp": 1757000030000, "jti": "...", "dev": true }
```

The claim is verified offline with the rest of the ticket, so a client cannot grant itself the
tester panel. **There is exactly one rule:**

> `dev` present and `true` → dev tools on. Anything else — the key absent, `null`, or `false` →
> off.

The `HEARTH_DEV` env fallback that used to cover an absent claim is **gone**, along with
`room.Config.DevTools`. An operator flag that hands world-mutating commands to every connected
player has no place in production, and with `control/` minting the claim there is nothing left
for it to cover. `control/PROTOCOL.md` §2 is the contract: Node emits `dev` **only ever as
`true`** and omits the key entirely otherwise, so a ticket for an ordinary account is
byte-identical to the pre-`dev` format, and Go reads absent as false.

The permission itself is per-account and lives on the control plane: the `HEARTH_DEV_TOKS` /
`HEARTH_DEV_USERS` operator allowlist (`control/PROTOCOL.md` §3.3), never client input.

The claim is still decoded as a `*bool`, purely so a re-marshalled payload cannot manufacture a
claim that was never made. A `dev` field of any non-boolean type rejects the whole ticket as
`bad-json` rather than being coerced — there is no path by which a malformed claim becomes a
spurious `true`. Covered by `auth.TestVerifyDevClaim` / `TestVerifyDevClaimWrongType` and
`room.TestDevGateIsTicketClaimOnly`, and end to end by the DEV stage of
`gameserver/test-go.mjs`, which asserts both commands are refused and mutate nothing.

**Both sides of the gate are under test.** `gameserver/room/dev_test.go` drives every command
on a fixture with the claim granted and asserts the resulting room state field by field (the kit
contents, the teleport's chunk push, god mode surviving creature contact and the environmental
tick, one spawn per `creTypes` key, the respawn and its movement grace window, the strength gate
the monoliths move, the survival-tick branch each weather kind selects, the clamped clock) and
the same table with the claim absent, asserting nothing at all changed. The `DEVOK` stage of
`gameserver/test-go.mjs` repeats it over a real socket. That stage needs one dev-claimed and one
unclaimed ticket in the same run, which no single control-plane configuration can produce, so
the suite signs both itself against a pinned throwaway keypair — see its header for the env vars
the control plane must be started with.

A refused command answers with a `msg` toast. **These are no longer the legacy strings.** The
legacy text (`Dev mode is off — start the server with: npm run server:dev`) is wrong on this
path: `npm run server:dev` starts the *legacy* Node server on :8081 and sets the `DEV` env var
the Go server deliberately does not read, so a developer who followed it would see nothing change
and reasonably conclude the feature was never ported. The current strings point at the mechanism
that actually grants the claim:

- `dev` → `Dev tools off — your ticket carries no dev claim. Locally: npm run start:dev, then reconnect.`
- `devcmd` → `Dev tools off — no dev claim. Locally: npm run start:dev, then reconnect.`

`npm run start:dev` sets `HEARTH_DEV_ALL=1` on the control plane, which grants the claim to
loopback callers only (`control/PROTOCOL.md` §3.3); named accounts go through the
`HEARTH_DEV_TOKS` / `HEARTH_DEV_USERS` allowlist. Either way the claim is baked into the ticket
at join time, so a reconnect is required — hence "then reconnect". A client that reads
`init.dev` (§3) never has to see these strings at all.

### 10.7 `devcmd` commands

| `cmd` | Fields | Effect |
| --- | --- | --- |
| `tp` | `x`, `y` (both JSON numbers) | teleport to `(x, y)` on the surface, boat cleared; broadcasts `pos`, sends `hp`, and **pushes chunks** |
| `mono` | `i` (0-3) | light monolith `i` if unlit; broadcasts `mono`, and announces the Engine when the fourth closes the set |
| `god` | — | toggle invulnerability: `godTick` heals the player to full at the top of every tick |
| `wx` | `kind` (`rain` \| `sandstorm` \| `snowstorm`, anything else clears) | force weather for 180 s; broadcasts `wx` |
| `time` | `v` (0..0.999) | set the world clock; the sim tick broadcasts it as usual |
| `spawn` | `type` (a `CRE_TYPES` key), optional `lvl` (2-5) | spawn one creature 4-8 tiles away with `strength`-scaled hp and **no home tile**, so it never disengages; level 1 unless `lvl` is given (§13.6) |
| `clearcre` | — | remove every creature |
| `aitest` | `kind`: `swarm` \| `pack` \| `duel` \| `siege` \| `warlord` | enemy-AI set piece (§13.6): remove every creature, then spawn the scenario's squad at its levels 7-9 tiles away, homeless like `spawn`; the toast says what to watch for |
| `monoreset` | — | every monolith back to dormant; broadcasts `{ t: "monoreset" }` (clients clear their lit monoliths). Strength drops with it; the Engine, a running wave and `won` are untouched |
| `kill` | — | clear god mode, respawn at bed/campfire/spawn with full vitals; **pushes chunks** |

Unknown `cmd` values are ignored silently, as in the legacy server.

The two chunk pushes are the only deviation from the legacy behaviour. The legacy server shipped
the whole map inside `init`, so a teleport needed no terrain; the Go server streams chunks, so a
tester who teleports without one lands in an empty world.

---

## 11. Hosting many worlds — routing and the allocation boundary

One `hearthd` process is one **instance**, and an instance hosts one or more **worlds**. Each
world is a `Room` with its own goroutine, its own state, its own overlays and its own save file.
Nothing mutable is shared between two rooms, which is what makes cross-world leakage impossible
rather than merely unlikely.

### 11.1 Routing

The control plane allocates and the ticket carries the result (`control/PROTOCOL.md` §1, §3.1):

```jsonc
{ "userId": "...", "worldId": "frontier", "instanceId": "inst-2", ... }
```

On a verified ticket the game server:

1. compares `instanceId` with its own. **A mismatch is refused** with
   `{ t:'authfail', reason:'wrong-instance' }`. The ticket is valid and correctly signed; it
   simply names a world some other process hosts, and serving it anyway would make allocation
   advisory. An empty `instanceId` never matches.
2. looks `worldId` up among the worlds it hosts; unknown → `authfail: unknown-world`.
3. hands the connection to that world's room. The socket holds a reference to exactly one room
   for its lifetime — there is no path by which a frame crosses worlds.

If the room cannot be produced (world build failed, process shutting down) the reason is
`world-unavailable`.

A client never picks its own binding: `POST /api/join` resolves the world server-side and signs
the result, and `GET /api/worlds` deliberately omits `instanceId`.

### 11.2 Which worlds does a process host?

Config, read once at boot, in the same shapes and the same precedence `control/store.js` uses —
so **one registry can configure both processes**, each taking the slice bound to its own
instance:

| Variable | Default | Meaning |
|---|---|---|
| `HEARTH_INSTANCE_ID` | `local` | which instance this process is |
| `HEARTH_WORLDS` | — | JSON array of `{worldId, seed, instanceId, ws, name}` records (`ws` ignored here) |
| `HEARTH_WORLDS_FILE` | `control/worlds.json`, found by walking up | the same JSON in a file |
| `HEARTH_WORLD_ID` / `HEARTH_WORLD_SEED` / `HEARTH_SEED` | `default` / `hearth-1` | the single-world fallback |
| `HEARTH_EAGER_WORLDS` | unset | build every world at boot instead of on first join |
| `HEARTH_SAVE_DIR` | working directory | where `world.<worldId>.save.json` files go |

First source yielding at least one entry wins. Entries whose `instanceId` is not ours are
skipped — they are someone else's worlds. A registry that names worlds but none of ours **fails
at boot**: that is a misconfigured process, not an idle one. A `worldId` must be
`[A-Za-z0-9._-]{1,64}`, because it names a save file.

With no configuration at all a process is instance `local` hosting one world `default` on seed
`hearth-1`, which is exactly the pre-multi-world behaviour the client and `test-go.mjs` expect.

### 11.3 Lazy by default

Worldgen is ~2-6s of CPU and hundreds of MB per world, so rooms are built **on first join**, not
at boot: a process configured with eight worlds would otherwise stall for a minute at startup
and hold every world resident even if nobody joins. The first player into a world pays the build
inside their handshake window (bounded at 45s); everyone after finds the room warm.
`HEARTH_EAGER_WORLDS=1` builds everything at boot instead, for operators who prefer a warm
process and a slower start.

Two simultaneous first-joins cannot build the same world twice: the room-manager entry is
published under a lock before the build starts, and the second caller waits on its completion
channel. That lock guards the *registry of rooms* only — never game state, which stays owned by
each room's own goroutine (see the contract at the top of `gameserver/room/room.go`).

Covered by `gameserver/hosting/*_test.go` (run them with `-race`) and end to end by
`node gameserver/test-multiworld.mjs`, which starts a control plane and a two-world game server
on private ports and proves routing, isolation and the `wrong-instance` refusal over the wire.

### 11.4 Admission — the player cap and one live session per identity

Routing (§11.1) decides *which* room; admission decides *whether*. Both rules below are resolved
on the room goroutine, in the `join` case of `Room.Run` — not in the socket layer and not in the
control plane, because only the room knows who is actually connected. Allocation is not
admission.

**The player cap.** A world admits at most `maxPlayers` concurrent players; the default is **4**,
the co-op target in `PLAN.md`. A refused client gets `{ t:'authfail', reason:'room-full' }` and
is closed — before `init`, before any chunk. It is a per-world setting carried in the same world
registry §11.2 describes, so one file still configures both processes (the control plane ignores
the field):

```jsonc
[{ "worldId": "frontier", "seed": "s2", "instanceId": "inst-2", "maxPlayers": 8 }]
```

| Variable | Default | Meaning |
|---|---|---|
| `maxPlayers` (per entry in `HEARTH_WORLDS` / `control/worlds.json`) | `4` | concurrent players in that world |
| `HEARTH_WORLD_MAX_PLAYERS` / `HEARTH_MAX_PLAYERS` | `4` | the same cap for the single-world fallback |

Absent, zero, negative or non-numeric all mean the default. A cap is never a promise about
*which* four: see takeover below.

**One live session per identity.** A duplicated browser tab shares `localStorage`, presents the
same `hearth-tok`, and is issued a ticket for the same `userId` — which used to put a second
copy of one player in the room. A second live session for an identity now **takes over**: the
old session is evicted and the new one is admitted.

```jsonc
{ "t": "kick", "reason": "replaced" }   // server -> the session being evicted
```

`kick` is sent to the evicted session and the socket is closed immediately after, so a client
must handle it wherever it handles a disconnect: show the reason ("you opened this world in
another tab"), and do **not** auto-reconnect on `replaced` — an auto-reconnect loop between two
tabs would evict each other forever. `reason` is an open string; unknown reasons should be
treated as a plain disconnect.

Takeover, not rejection, on purpose: rejecting the newcomer would lock a player out of their own
character after a browser crash or a dropped connection until the stale socket timed out, which
is a worse failure than the duplication it prevents.

Eviction runs the **same teardown a disconnect does** — profile snapshotted, player removed from
`players` *and* `playerOrder`, `pl` broadcast to everyone else — so nothing is lost and no ghost
is left in an ordered mirror (§9.6, §10.5).

**Ordering: takeover is resolved BEFORE the cap.** If a full room already holds one of your
sessions, your new connection is replacing a seat, not claiming another one, so it is admitted.
Checking the cap first would mean a player who crashed could never get back into the world their
own stale session is still sitting in.

**Scope.** The rule spans every room a process hosts: joining world B while still live in world A
evicts the world-A session (the identity registry is process-wide, beside the registry of rooms —
`gameserver/room/registry.go`). Across *instances* it is not enforced: that needs shared state
the control plane does not have yet, so two processes can still each hold one session for the
same identity.

Covered by `gameserver/room/admission_test.go`, `gameserver/hosting/hosting_test.go` and, over
the wire, by the ADMIT stage of `gameserver/test-go.mjs`.

---

## 12. Modular building — `buildmod`

Legacy `structures` is one entry per tile, which cannot hold a floor, a wall,
a fixture and a decor piece at once. Modules therefore have their
own store keyed **`tile:slot`**, and the two systems do not mix: a tile carrying
a legacy structure refuses modules, and vice versa.

Slots are `floor`, `wall`, `fixture`, `decor` (`MODULE_SLOTS` in
`shared/defs.json`), and each module kind names its slot directly in
`MODULES[kind].slot`.

**Modules are not inventory items.** `MODULES[kind].cost` is spent straight from
the player's bag in building materials — the `MATERIALS` tier (`wood_planks`,
`stone_blocks`, `glass_pane`, ...), which are themselves ordinary crafted
recipes. Nothing about a module ever enters `INV_KEYS`.

### 12.1 Client → server

| type | payload | notes |
|---|---|---|
| `buildmod` | `i, kind, slot, dir?, seq?` | range 6, `z=0`, dir is a facing 0–3 (else 0) |

Placement is refused — with nothing charged — when the kind is unknown, the slot
does not fit the kind, the tile is water, a landmark, a medic hut or already
carries a legacy structure, the `tile:slot` is taken, the player is out of reach
or indoors, or the materials are not in the bag.

Modules are taken back down with the ordinary `atk` swing: an attack that finds
no creature and no legacy structure in range hits the nearest module within 2.4
tiles and refunds half its materials, mirroring structure demolition.

### 12.2 Server → client

```jsonc
{ "t": "mod",     "i": 1234, "slot": "wall", "kind": "mod_wall_stone", "hp": 40, "dir": 0 }
{ "t": "modhp",   "i": 1234, "slot": "wall", "hp": 22 }     // damaged, still standing
{ "t": "modd",    "i": 1234, "slot": "wall" }               // destroyed or demolished
{ "t": "modfail", "seq": 17, "why": "slot-occupied" }       // unicast to the sender only
```

`modfail` echoes the request's `seq` so the client clears that exact preview
instead of guessing. `why` is one of `unknown-module`, `bad-slot`, `bad-tile`,
`outdoors-only`, `too-far`, `water`, `blocked`, `node`, `tile-occupied`,
`slot-occupied`, `unsupported`, `occupied`, `no-anchor`, `cost`. It is advisory text for the UI — the authoritative fact
is simply that no `mod` broadcast followed.

Existing modules arrive with their chunk (§8.3 `mods`), never in `init`.

### 12.3 Walls fill their tile

A wall owns its whole tile, exactly as the legacy palisade does: `posBlocked`
refuses it, creature steering refuses it, and the client's `blockedAt` mirrors
both. A room is a ring of wall tiles around floor tiles.

An earlier pass modelled walls as *edges* (`wallNE`/`wallNW`) and it was wrong on
both counts. The art is drawn as a tile-filling block — a diamond top, two side
faces and a ground shadow — so an edge-mounted sprite rendered as a post floating
between tiles with no meaningful facing to rotate; and a player could wall
themselves in on all four edges of their own tile with no way out. Saves written
under the old model load through `legacySlot()`, which folds both edges onto the
one `wall` slot.

`mod_door` is a wall that does not block. Floors, fixtures and decor never
block anything, and a blocking wall may not be placed on a tile a player is
standing on (`occupied`).

### 12.4 Support and cascade

One rule, enforced server-side on both placement and removal:

| slot | needs |
|---|---|
| `floor` | nothing — free-standing |
| `wall` | nothing — a fence or a screen is a legitimate build |
| `fixture` | a floor on its own tile |
| `decor` | a floor or a wall on its own tile |

Placement of an unsupported piece is refused with `unsupported`. Removal
**cascades**: destroying a piece takes down whatever it was holding up, repeating
until the tile is stable (pulling a floor strands the fixture and the decor on
it), and every piece that falls refunds half its materials to whoever knocked
it down. The client mirrors the table to colour its ghost; the server decides.

### 12.5 Bridges over water

`mod_bridge_segment` (`water: true` in `MODULES`) is the only piece that may be
built over open water, and only when the span reaches dry land: a segment is
anchored if a 4-neighbour is land, or another segment that is itself anchored —
checked breadth-first across the whole span, so a hundred-tile causeway is
validated in one traversal. Anything else over water is refused with `water`; an
unmoored segment with `no-anchor`.

Standing on a deck is **not** being in the water: the survival tick skips thermal
water damage for a player on a bridge tile, and the client neither starts
swimming nor launches a boat there. Water stays walkable at z=0 either way
(invariant §5), so no collision rule changes.

Cutting a span cascades outward from the gap: every segment that can no longer
reach land falls in and refunds half its materials to whoever cut it.

### 12.6 Persistence

The snapshot carries `modules` as an object keyed `"tile:slot"` with
`{kind, hp, dir, owner}`. A load skips any entry whose key is malformed, whose
tile is out of bounds, or whose kind or slot no longer exists in the defs, so
rolling `shared/defs.json` back can never crash the server. This is also how the
retired pieces (the `roof` slot and its three roofs, `mod_window`, `mod_arch`,
`mod_stairs`, `mod_lantern_hook`) leave older saves: they are dropped on load.

### 12.7 What a build is for

Modules shape space: a ring of walls keeps creatures out (their steering tests
the same tiles), a door lets you in and out, pillars and decor dress a floor, and
bridges cross water. Roofs, and the shelter they gave, were retired with the
roof slot — weather is escaped underground or with the right cloak.

Keepers have no houses either: the `shelter` structure and its z=2 interior
were retired. The layers are now the surface (z=0) and the mines (z=1) — the
mines are the safe place (no weather, no creatures) and the only place furniture
can stand. Old saves drop shelter structures and z=2 furniture on load, and a
`pos` asking for z=2 is snapped back.

### 12.8 Starmetal Armor — `wear` with `starmetal_armor`

`starmetal_armor` is `gear` (crafted once, at the forge, from a deliberately
late-game bill — see `RECIPES`). It is worn in its **own slot**, independent of
the cloak in `wornGear`: `{t:"wear", k:"starmetal_armor"}` toggles it, and
`{t:"wear", k:null}` still only removes the cloak.

Armored, every creature hit (contact, lancer beam, brute bolt) costs half its
damage. HP is an integer, so the remainder is carried per player and a 1-damage
hit lands every other time. Weather, water, hunger and fire ignore armor. A hit
the armor reduced carries `blocked` (the damage it turned) on the `hp` frame.

The worn state rides `armor: bool` on `init`, `inv`, each `players` entry,
`pj`, and the `worn` broadcast (`{t:"worn", id, k, armor}`), and is saved in the
profile (gated on owning the gear when restored).

## 13. Island folk and the hunting brain

### 13.1 Camps

Three camps — Woods (`folk_hut`), Dunes (`folk_hut_dunes`), Marsh
(`folk_hut_marsh`) — are placed by `world.FindFolkCamps` near each island's
centre. Placement is deterministic for a world, except that no camp tile may
overlap the spawn, a medic, or a structure already in the save. A camp's **hut
and fire tiles** join the medic block set: nothing may build on, walk through or
path through them, on the server or in client prediction. The door tile in front
of the hut stays open. `camps` rides `init` (§3) whole, like `medics`.

### 13.2 Folk

Each camp has a fixed roster (Woods: `villager`, `villager2`, `woods_child`;
Dunes: `ashmark_hunter`, `villager2`; Marsh: `elder_yvenne`, `villager`). Folk are
server-simulated, cannot be attacked, and ride every `cre` frame as
`f: [id, x, y, kind, act, face, item]` (positions are tile-centre floats, 2 dp; ids are
`<campId>-<k>`). `act` is one of `idle`, `walk`, `run`, `play`, `carry`, `talk`, `chat`, `speak`,
`listen`, `watch`, `wave`, `call`, `sit`, `collect`, `build`, `craft`, `cook`, `tend`, `guard`,
`cheer`, `dance`, `hide`, `shun`, `shelter`, `scold` (`shun`/`scold` §13.4, `shelter` §13.5).
`face` is whole world degrees while at something, or null. `item` (7th, optional, appended) is
what they carry — `wood`, `stone`, `fish`, `game`, `herbs`, `berries`, `reeds`, `water`, `torch`
(the night watch's light) — or null; always null while `hide`. Clients that read only six fields
keep working. The routine (gameserver/room/folk*.go): A* routes over a per-village grid (±1 level
a step, round huts, nodes, water and keepers' builds), utility-chosen work by role, needs and
personality, daily duties (stoking, the cook's `call`, the elder's `speak` with the rest
`listen`ing), hunters out in pairs, a rotating night watch, a feast (`cheer`/`dance`) the evening
after a monolith is lit, and `guard`s (hunters, the watch, builders) standing between a creature
and the huts facing it while the rest hide:

- By day they wander the yard, work, sit at the fire, and stop and `talk` to a
  surface player within 2.4 tiles. What they say is client-side flavour.
- A creature within 7 tiles of them, **or within 14 tiles of their village's fire**,
  sends them running (`run`) to the hut door; inside they are `hide` and invisible
  to creatures. They come back out once nothing is within 10 tiles of them and
  nothing within 18 of the fire, for 15 ticks. The fire rule is the village alarm:
  without it, folk on the far side of the village sat on while their neighbours fled.
- At night everyone but the watch on duty turns in (`hide`); the watch (a roster per village,
  three shifts a night, turning nightly) keeps the edge and the fire with a `torch`.

### 13.3 Creature AI on top of the legacy port

The Slice 3 rules (§9) still pick targets, ranges and damage. Layered on top:

- **Memory.** Prey that leaves aggro range is searched for at its last known
  position for 30 ticks. A struck creature rouses every non-wisp creature within
  10 tiles toward the attacker.
- **`calert`.** Sent once when a creature first locks on to a player.
- **Detours.** When the direct step and the ±35° turns are all blocked, a bounded
  BFS (22-tile window) finds a way round. If the target is beyond the window or
  unreachable inside it, the creature heads for the reachable tile nearest the
  target, as long as that tile is at least one tile closer. Only a hunt that
  can't get any closer gnaws through structures, as before, so a fully enclosed
  base is still besieged.
- **Bridges.** Bridged water (`mod_bridge_segment` floor modules) is solid
  ground for every creature, and for folk. When water blocks a non-swimmer and
  no short path exists, it picks a bridge within 45 tiles. It weighs the walk
  to the bridge against how close the bridge gets it to the target, so it
  prefers the far end. It heads there for up to 150 ticks, or until it arrives
  or the bridge is destroyed. Brutes no longer fire their water bolt at a
  player standing on a bridge, since they can walk out to them.
- **Cut bridges.** Removing a bridge segment drops every creature's cached route
  and bridge plan. A non-swimmer left in open water flounders toward the nearest
  land at half speed and cannot deal damage until it's ashore. With no land
  within 12 tiles, it drowns and is simply dropped from the `cre` frame.
- **Tactics.** Chasers lead a moving player. Husk wolves spread out around their
  prey. Stalkers, drowned and wolves fall back after landing a hit. Crawlers,
  stalkers, drowned and wolves break off once when under a third of their HP.
  Blight lancers back off inside 4.5 tiles and strafe between 4.5 and 8 tiles.
- **Fire.** At night, crawlers and husk wolves (unless enraged) will not step
  deeper into a 2.6-tile ring around a campfire.
- **Spacing.** Creatures push apart inside 0.75 tiles.
- **Idle.** With no prey, crawlers, stalkers, wolves and drowned chase folk within
  12 tiles (folk are never damaged). Every other idle creature ambles near home
  at 40% speed. Searching, folk-chasing and ambling creatures never gnaw.

### 13.4 Village regard — the folk remember harm

`gameserver/room/folk_regard.go`. Each island's folk keep a per-keeper (by `userId`)
tally of harm done to their island: **1** per tree felled, **+4** when that felling
blights the ground (`chopEcosystem` turned grass to mud), **2** per animal killed.
It is capped at 30, forgiven at 1 per 20 s, and not saved.

| Tally | Standing | What the folk do |
|---|---|---|
| < 6 | welcome | `talk` as before |
| ≥ 6 | wary | within 6 tiles they `scold` instead of talking: `face` points at the keeper |
| ≥ 14 | hostile | villagers and children `shun` (run for the hut, then `hide`) within 6 tiles and stay in while the keeper is within 9; watchers stand and `scold` |

Folk who see the harm done (within 12 tiles) `scold` that keeper for 12 ticks
whatever the tally. A change of standing reaches the keeper as a `msg`. Lines are
client-side flavour (`src/folk.ts` `SCOLDS`, `SHUN_SHOUTS`).

### 13.5 Folk and the weather

`gameserver/room/folk_weather.go`. The folk of the island the weather bites react the moment it
starts: a `sandstorm` sends the whole Dunes village in and a `snowstorm` the whole Spire, watchers
included; `rain` calls the Woods and Marsh children in while the adults carry on. While heading
in they are `shelter` (the same run as `run`; the client shouts about the storm rather than the
blight), then `hide` until it passes — then out again after the usual all-clear. Folk on other
islands carry on. On every `wx`, up to two folk in earshot remark on it client-side
(`src/folk.ts` `onWeather`).

### 13.6 Enemy levels and the combat brain

`gameserver/room/creature_levels.go` (levels, abilities), `creature_base.go` (base brain),
`creature_attack.go` (attacks), `creature_elite.go` (level features). The legacy server has none
of this: it sends 4-element `cre` rows and none of the frames below, and the client must keep
working against it.

**Levels.** Every creature has a level 1–5, sent as the **5th element of its `cre` row**:
`["c12", 640.25, 641.00, "crawler", 2]`. A natural spawn rolls it: the type's tier (crawler,
drowned, wisp 1; stalker, husk wolf, frost wraith 2; bog shambler, brute, blight lancer 3),
+1 per two lit monoliths, a veteran roll (+1 at 16%, +2 at 4%), +1 during the final assault,
capped at 5. Each level above the tier adds 1 hp. A level-5 creature that can lead is an
**alpha** (+50% hp); at most two are alive at once. `devcmd spawn` takes an optional `lvl`
(2–5); without it a dev spawn is level 1.

**Base brain — every creature, every level.** Perception, target choice, attack slots and
steering (`creature_base.go`); every melee blow is telegraphed and every ranged shot is aimed
before it fires (`creature_attack.go`).

**Abilities by level.** A type only ever learns what its kit lists (`creKit`):

| Ability | crawler | drowned | stalker | husk wolf | frost wraith | wisp | bog shambler | brute | blight lancer |
|---|---|---|---|---|---|---|---|---|---|
| flank — come at the keeper's side or back | 2 | 2 | 2 | 2 | | | | | 3 |
| pack call — rouse allies on first sighting | 2 | 2 | | 2 | | | | | |
| dodge — sidestep a swing | 3 | 3 | 3 | 3 | 3 | 3 | | | |
| hit and run — fall back after a blow | 3 | 3 | 3 | 3 | | | | | |
| charge — telegraphed dash | | 4 | 4 | 4 | | | 4 | 4 | |
| lead shot — aim where the keeper is going | | | | | 4 | | | 4 | 4 |
| alpha — leads a pack | 5 | | 5 | 5 | | | | 5 | |

**Frames.** All broadcast. `ang` is radians in tile space (`atan2(dy, dx)`); `tk` is a count of
200 ms ticks; coordinates are tiles rounded to 1 decimal.

| Message | Meaning |
| --- | --- |
| `catk` | `{ id, ang, tk }` — a melee windup has begun. In `tk` ticks the creature lunges along `ang`; whoever is in reach of where it lands is hit. Not sent for brutes and bog shamblers, which send `ctel`. |
| `ctel` | `{ id, ang, tk }` — a brute or bog shambler is winding up its slam (`ang`, `tk` added to the §9.3 shape). |
| `caim` | `{ id, kind, tx, ty, tk }` — a ranged attack is being aimed at `(tx, ty)`; `kind` is `"bolt"` (brute) or `"lance"` (blight lancer). It fires as the usual `shot` after `tk` ticks, at that aim, so a keeper who moves off the line is missed. |
| `cdodge` | `{ id, ang, by, seq }` — the creature sidestepped keeper `by`'s swing `seq`, hopping along `ang`. That swing's `act` carries `targetI: null` and nothing else is struck. |
| `ccharge` | `{ id, ang, tk, len }` — a charge: after `tk` ticks of windup the creature dashes up to `len` tiles along `ang`. |
| `crally` | `{ id }` — an alpha has rallied its pack. |
| `cpanic` | `{ ids }` — an alpha fell; these creatures flee for a few seconds. |

Every `hp` frame a creature causes — melee, charge, bolt or beam — carries `ang`; environmental
damage never does (§9.3, and the wire suite tells the two apart by it).

**Tuning** (ticks of 200 ms; tiles). Perception (`creature_base.go`): a keeper is noticed in
sight with a clear line (blocking structures, modular walls and medic huts block it; water does
not), heard within 6 tiles through anything, heard within 14 for 1.5 s after swinging, or smelled
out to the scent radius (40) when carrying essence or meat — the legacy scent radius never
worked. Among those noticed it takes the lowest of distance − 5 (struck it in the last 10 s)
− 3 (already its prey) − 2 (keeper at ≤ 3 hp). At most 3 light melee hunters (crawler, drowned,
stalker, husk wolf) within 8 tiles close in on one keeper; the rest circle a 3.2-tile ring at
55% speed. Speed builds to full over two ticks and halves on a turn sharper than 100°; a hunter
that neither closes 0.25 tiles nor walks 1.2 tiles in 10 ticks re-plans and sidesteps.

Melee (`creature_attack.go`): a creature commits when a keeper is within trigger reach; the
heading is fixed at the start, it stands still through the windup, then lunges and hits every
keeper within 1.1 tiles of where it lands. Being struck cancels a light creature's windup or aim;
heavies have super armor.

| type | trigger | windup | lunge | recover | cooldown |
|---|---|---|---|---|---|
| crawler | 1.5 | 2 | 0.6 | 1 | 3 |
| drowned | 1.6 | 2 | 0.7 | 1 | 3 |
| stalker | 1.8 | 2 | 0.9 | 1 | 3 |
| husk wolf | 1.9 | 2 | 1.0 | 1 | 3 |
| frost wraith | 1.4 | 1 | 0.5 | 1 | 4 |
| blight lancer | 1.3 | 3 | 0.3 | 1 | 4 |
| brute | 2.5 | 8, then 5 | slam r 1.4 | 3 | 2 |
| bog shambler | 2.5 | 8, then 5 | slam r 1.3 | 3 | 2 |

Ranged: the lance aims 3 ticks and runs 9 tiles through its aim point, hitting keepers within 0.7
of the beam up to the first blocking structure (which it still annihilates); the bolt aims 2 and
hits keepers within 0.9 of its aim point. Both go through the armor like any creature hit.

Abilities (`creature_elite.go`): dodge 30 / 35 / 45 % at levels 3 / 4 / 5, a 1-tile hop, 12-tick
cooldown, never while winding up or charging. Charge from 4–8 tiles (heavies 3–7) on a clear
line: 3-tick windup, dash 1.4 tiles/tick (heavies 1.0) for up to 8 tiles, damage + 1, a blocking
structure in the way takes 2 (brute 6) and dazes the charger for 5 ticks; 40-tick cooldown
(heavies 50). Hit and run: a level-3 crawler falls back 3 ticks after a blow (stalkers 7, wolves
and drowned 4 at every level). Pack call and alpha reach 12 tiles. Followers — wolves of a wolf
alpha, stalkers of a stalker alpha, crawlers of a crawler or brute alpha — hunt the alpha's prey
within 26 tiles and run 10% faster near it. A struck alpha enrages them for 60 ticks (at most
every 25); a slain one panics them for 15.

## 14. Feature flags and the tile mountains

Per-room switches held by the server (`gameserver/room/features.go`). Precedence: the room's
save (`features`) > the world's registry entry (`"features": {"tileMountains": false}`) >
`HEARTH_FEATURES` (`tileMountains=0,cameraPeek=1`) > the registry default. A save with no
`features` loads with the defaults.

| flag | default | effect |
|---|---|---|
| `tileMountains` | on | terrain mountains at the five `MOUNTAINS` sites (Go-only, `world/mountains.go`); the sprite mountains hide and their 3x3 `LANDMARK_BLOCK` footprint stops blocking |
| `cameraPeek` | on | the client camera leans ahead behind mountains and over height changes (also needs the player's Settings toggle) |

- `init.features`: `{tileMountains: bool, cameraPeek: bool}`. A client that gets none (legacy) uses tileMountains=false, cameraPeek=true.
- `{t:"feat", features:{...}}`: broadcast whenever a flag changes. For a terrain flag the touched chunks follow, re-sent to every player whose push window holds them; everyone else has them marked unsent. A re-sent chunk replaces the earlier copy whole (its `nodes`/`decor` are the full truth).
- `devcmd {cmd:"feat", name, on}`: dev-claim gated. Unknown `name` gets a `msg`; a non-boolean `on` or a no-op is ignored; a change broadcasts `feat` and toasts the tester.
- tileVis on mountain tiles (Go-only): 2 rock (cliff edge), 3 ledge, 4 snow rock, 5 snow ledge, 6 trail. Foothill tiles keep their base 0/1. `elev` reaches 12 (`world.MaxElev`); the RLE is byte-valued, so the encoding is unchanged.
- Creatures and animals may not climb more than +1 onto or off a mountain tile, and nothing spawns on a mountain face. Players climb by the usual rules (+1, +2 jumping).
