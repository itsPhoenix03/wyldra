# Project state — Wyldra

**Last updated:** 2026-10-18 · branch `feat/advance-enemy-ai` (uncommitted) · session dump:
`session-context-dump/2026-10-18_2030__advanced-enemy-ai.md`

This is the status source of truth. Update it in the same pass as any session dump.
It describes the project **as it is now** — history lives in the session dumps.

---

## 1. Start here

The server has been split into three processes. The Go game server is authoritative.

```
Client ──HTTP──> Node control plane (:8090)   identity, profiles, world allocation,
   │                                          signs a 30s Ed25519 ticket
   └──WS────────> Go game server     (:8082)  the world, the tick, all gameplay
```

Go verifies tickets **offline** from a fetched public key and never calls Node on the
connection path, so a Node hiccup cannot stop players connecting.

The pre-migration Node server (`server/index.js`, :8081) still exists, still works, and is
still tested. It is reachable in the client with `?legacy=1`.

### Running it

```bash
npm start              # control + game + client, one Ctrl-C stops all three
npm run start:dev      # ...plus F9 kit, F10 tester panel and warp (loopback only)
npm run stack:servers  # servers only, for the wire suites
npm run start:legacy   # the pre-migration :8081 server (use ?legacy=1 in the client)
```

`tools/dev/stack.mjs` is a zero-dependency launcher (the repo has deliberately kept zero
server deps). It sets `HEARTH_EAGER_WORLDS=1`, and in `--no-client` mode also pins the ticket
keypair that `gameserver/test-go.mjs` signs with.

### Key documents

| Doc | What |
|---|---|
| `docs/10_GO_WIRE_PROTOCOL.md` | The client↔Go wire spec. Authoritative; both sides implement against it. |
| `control/PROTOCOL.md` | Ticket format, the `dev` claim, world registry. |
| `docs/09_GO_GAME_SERVER_MIGRATION_ASSESSMENT.md` | Why the split is shaped this way. §7 is the ownership table. |
| `AGENT_GUIDE.html` | Deep technical reference for the game itself. |

---

## 2. Verification gates

**Run these yourself. Do not trust a subagent's claim** — three reports in the last session
were wrong in ways only running the suite revealed.

```bash
npx tsc --noEmit                      # esbuild does NOT type-check; this is the only type gate
npx vite build
cd gameserver && go build ./... && go vet ./... && gofmt -l . && go test ./...
node tools/worldparity/compare.mjs    # MANDATORY after any worldgen edit
npm run test:control                  # control plane, HTTP black-box

# wire suites — need a FRESH server:
rm -f gameserver/world*.save.json
npm run stack:servers                 # in one terminal
npm run test:go                       # in another — must end ALL TESTS PASSED, zero skips
node gameserver/test-multiworld.mjs
```

**Both wire suites are stateful.** A second run against a live server fails at `gather`,
because nodes near spawn are on respawn timers. Delete the save and restart between runs.
They also need `HEARTH_ALLOW_WARP=1`, which `stack:servers` sets.

The legacy server has its own suite: `npm run test:legacy` (also needs `HEARTH_ALLOW_WARP=1`
and a fresh save). To run it without disturbing anything, copy `server/index.js` with `PORT`
and `SAVE_PATH` swapped and point a copy of `test.mjs` at the new port.

**Never bind :8081 or write `server/save.json`** without backing it up first — it is the
owner's real save and the legacy server owns that file. The Go server writes
`gameserver/world.<worldId>.save.json`.

Status at this update (2026-10-18): **all of the above green**, `test-go.mjs` included (twice in a
row). It had been red at 45adfeb on test flakiness, not game bugs: night creatures on the Dunes
read as weather damage (environmental `hp` frames carry no `ang`; creature hits do), and the
MOD stage's "open sand plain" could have a boulder next door. Run the suites against a throwaway
save dir (`HEARTH_SAVE_DIR`) and spare ports rather than deleting
`gameserver/world*.save.json`.

---

## 3. Invariants — do not break these

1. **One goroutine owns all game state.** Everything reachable from `*Room` is owned by
   `Room.Run` and guarded by nothing. No mutex on game state, ever. Socket readers only push to
   an inbox; writers only drain a buffered per-player channel; a client whose buffer fills is
   dropped rather than waited on. Measured realistic tick is 79µs against a 200ms budget, so
   there is no performance argument for changing this. (`room.Registry` has a mutex but stores
   only presence — never anything reachable from a `*Player` or `*Room`.)
2. **Worldgen is bit-exact with `shared/world.js`** (including hand-ported `alea` and
   `simplex-noise`). `tools/worldparity/compare.mjs` is the gate. The whole black-box test
   suite depends on this.
3. **Ordered mirrors.** Go randomises map iteration where the JS reference walks Maps in
   insertion order. `creOrder`, `aniOrder`, `playerOrder`, `infOrder` and `structIndices()`
   each decide a real outcome. Add state whose iteration order affects an outcome → add a
   mirror, maintained in exactly one add and one remove site.
4. **`shared/defs.json` is the authored source**; `shared/defs.js` is generated from it
   (`npm run defs`, `npm run defs:check` for drift). Edit the JSON, then regenerate.
5. **Water is passable at z=0.** `posBlocked` mirrors the *client's* per-layer rules, not the
   server's `blocked()` — that treats water as solid and would forbid swimming and boats.
6. **Takeover resolves before the cap check** (§5). Reversed, a crashed player can never
   re-enter their own full room.
7. **Vitals are integers on the wire**, via `ceil` at every boundary. Ceil not floor:
   starvation fires at `<= 0`, so a bar must read 1 until the value truly reaches zero.
8. **The client must never auto-reconnect on `kick: replaced`** — two tabs would evict each
   other forever.
9. **Dev commands are gated only on the signed ticket claim.** No server-wide env fallback:
   that hands world-mutating commands to every connected player.

---

## 4. Shipped and working

### The game
Deterministic 1280×1280 worldgen (4 major islands + Core + minor isles, `WORLD_VERSION = 4`);
three z-layers; survival loop with hunger, thirst, thermal damage, weather and day/night;
gathering, crafting, building, digging, farming, chests, boats, swimming; 9 creature types and
7 animal species; wisp infection; two medic NPCs with a full bargain state machine; progression
through 4 Monoliths → Aether Forge → Monolith Cores → World Engine → a 4-minute final assault.

### The Go port — feature-complete
**23/23 inbound** message types (`hello` replaced by ticket-based `auth`) and **38/38 outbound**.
Every handler, the sim tick, creature and animal AI, weather, infection, the medic bargain,
progression, waves and `dev`/`devcmd`.

### Terrain streaming
Go streams 64×64 chunks (20×20 grid, Chebyshev radius 2, 2-byte-run RLE + base64); `veins` is
masked to land on the wire. `src/tiles.ts` is the client store; unloaded tiles use sentinel
`255` and render as **void, not water** (water is walkable and would mislead), and movement
input is gated until the player's own chunk arrives.

### Server-authoritative movement
Speed budget (dt clamped to 1s), z-aware collision, `zAnchor` layer gating requiring one
mineshaft/shelter within 3.0 tiles of **both** endpoints, throttled `{t:'fix'}` snapback, and a
1s grace window on all 12 server-side reposition sites.

### Multi-world hosting
`gameserver/hosting` routes on the verified ticket (`wrong-instance` / `unknown-world` /
`world-unavailable`). Rooms are lazy by default, `HEARTH_EAGER_WORLDS=1` flips it. Per-world
save files. Cross-world isolation asserted directly; `-race` clean.

### Room admission
**4 players per room** (`room.DefaultMaxPlayers`, configurable per world via `maxPlayers` or
`HEARTH_WORLD_MAX_PLAYERS`). A 5th gets `authfail: room-full`, closed before any world data.
**One live session per identity**: a second ticket for a live `userId` evicts the first, which
receives `{t:'kick',reason:'replaced'}`. This fixes the duplicate-browser-tab clone.

### Modular building
A second placement system beside legacy `structures`, keyed **`tile:slot`** so one tile can
carry a floor, two wall edges, a roof, a fixture and a decor piece at once. 19 module kinds
(every `mod_*` asset) paid for out of a new **materials tier** — 10 crafted intermediates
(`wood_planks`, `stone_blocks`, `glass_pane`, …) that are ordinary recipes; modules themselves
are never inventory items. `buildmod` in, `mod`/`modhp`/`modd`/`modfail` out, modules streamed
with their chunk. **Walls fill their tile** (the art is a block, not an edge panel) in the pos
validator, creature steering and the client's `blockedAt`; doors do not, and a blocking wall
cannot be placed on a tile a player stands on. Support is one rule (roof→pillar here or wall
next door, fixture→floor, decor→either) and removal cascades with half refunds.
Roofs were retired; **modular walls and fixtures are sandstorm shelter** (`Room.sheltered`, with
blocking structures and village / medic huts), and a sheltered keeper also takes no desert heat
while the storm blows (docs/10 §9.4). Lantern hooks light at night. Bridge segments are the one piece allowed
over water and must stay moored to land; cutting a span drops the rest and stops counting as
swimming. Wire spec §12; `gameserver/room/modules.go`, `modules_test.go`, MOD stage of
`test-go.mjs`.

### Asset pipeline and streaming
The browser never rasterises an SVG. `tools/atlas/build-atlases.mjs` (`npm run atlas`; run
automatically by `npm start`, `npm run dev`, `npm run build`, cached by an input hash) bakes
`src/assets.ts`'s manifest into PNG atlases in `assets/atlas/` plus `src/generated/atlas.json`
(both gitignored). Packs (`PACK_OF`): `base` + `keeper` load before the scene starts
(decoded off-thread with `createImageBitmap` while the menu is open); each NPC's clips are its
own pack, streamed per island (`src/zones.ts`, ~110 tiles off the shore) or on demand when one
walks into view. `src/atlas.ts` registers every frame as an alias texture of its atlas, so all
the old string keys still work. Keeper skins: the build bakes one atlas per gear variant plus a
shirt mask; other shirts are blended in `src/skinWorker.ts`. `assets/sw.js` caches `/atlas/`
cache-first (names are content-hashed) and falls back to the network if Cache Storage fails.
Join runs in parallel with loading (`net.ts prefetchJoin/takeJoin`).

Server side, the per-tick `cre` frame only carries entities within 40 tiles of each player
and is skipped for a client whose queue is half full (docs/10 §9.2.1).

### Island folk: alarm and regard
A creature within 14 tiles of a village fire sends the whole village to its huts (not only folk
within 7 of it). Each island also tallies the harm each keeper does there (trees felled, blighting
fellings, game killed; fades 1 per 20s, not saved): wary folk `scold`, hostile ones `shun` (children
and villagers hide, watchers scold). `gameserver/room/folk_regard.go`, docs/10 §13.4. The keeper's
swing reach is now 1.5 tiles (`AtkReach`), down from the legacy 2.4.
Weather: a sandstorm sends the whole Dunes village in, a snowstorm the Spire, rain only the Woods /
Marsh children (`shelter` then `hide`; `folk_weather.go`, docs/10 §13.5).

### Movement: ledges and the mine exit
Fall damage is per ledge crossed (`fallDrop`), and the client draws a worn, crumbling lip on every 2+ drop edge.
The keeper has a voice (`src/keeperVoice.ts`, client-only): it explains a ledge the first time you
reach one, reacts to a fall, and answers the folk — greetings, scolding, shunning, weather — taking
turns with them (one villager greets at a time).
Surfacing from a mine lands on open ground by the shaft (`surfaceExit`), and a step within your
own tile is never blocked (client + server), so nobody can be walled into a blocked tile. docs/10 §5.

### Enemy AI: levels and the combat brain
Every creature has a level 1–5 (5th element of its `cre` row; rolled from type tier, lit
monoliths, a veteran roll and the final assault; ≤ 2 alphas alive). Three layers on top of
the legacy port, each in its own file and struct embedded in `Creature`:
`creature_base.go` (every enemy — sight lines, hearing, working scent radius, grudge and
sticky targeting, 3 attack slots per keeper with the rest circling a ring, momentum, stuck
recovery), `creature_attack.go` (every enemy — every melee blow and ranged shot is
telegraphed (`catk` / `ctel` / `caim`) and lands where it was committed, so keepers can step
out of it), and `creature_elite.go` (by level and type kit, `creature_levels.go creKit` —
flank, pack call, dodge (`cdodge`), hit and run, charge (`ccharge`), lead shot, alpha with
focus fire / `crally` / `cpanic` on death). `stepCreature` in creatures.go owns the call
order. Client renders level pips / alpha crown and every telegraph (`src/main.ts`
`cue*`, sounds in `src/audio.ts`); dev panel spawns take a level, and its "Enemy AI test"
button (`devcmd aitest`: swarm / pack / duel / siege / warlord) clears the monsters and drops
a scripted squad; "↺ Reset" (`devcmd monoreset`) puts every monolith back to dormant. Wire spec and tuning:
docs/10 §13.6. Balance note: a crawler deals ~0.83 hp/s to a keeper who stands still
(was 1.0); a hugged brute slams every ~2 s for 2 to everyone within 1.4 (was 2 per second).

### Ecology: the land heals
Over-felling still sours a sector's grass to mud (every 8th tree in a 16×16 sector), but it now
recovers (`gameserver/room/ecology.go`, docs/10 §8.6): resting sectors (2 min without a felling)
shed pressure and their mud turns back to grass (12% per 30 s pass per tile), and a regrown tree
reclaims the nearest soured tile. Nothing heals or regrows through a building, farm, module or
blight; healed tiles go out as `regrow`. Creatures walk over farm plots instead of gnawing them
(that bug destroyed crops). The client hides world decor under built tiles.

### Title screen: the Whispering Woods
`index.html` (`#menu`) is laid out like a game title screen: the logo and a vertical menu on the
left — **Play** (straight into the shared world), Create room, Join room, Settings; one choice is
always selected, hover / focus / arrow keys / W-S move it, its hint shows below, Enter picks it
(`menu.ts wireMainMenu`). Behind it, a Woods landscape running off every edge (the owner asked for
no island ringed by water): terraced meadows of the game's grass / flower-grass tiles with soil
cliffs, a river winding down the valley to a coast in the near-right corner, and the game's own
pine, oak and autumn trees, bushes and glowcaps — no characters or animals (owner's request).
Golden-hour light, drifting cloud shadows and rising motes are CSS over it; the layers lean with
the pointer (`wireParallax`). The scene is a deterministic height map (`src/menuArt.ts`,
`BACKDROPS[BACKDROP]`; a Frozen Spire island is the other choice) that
`tools/atlas/build-atlases.mjs` renders with resvg into one PNG (`assets/atlas/menu-<biome>.<hash>.png`,
`atlas.json menuBackdrop` with its biome and frame, ~0.9 MB for the Woods): tile columns stacked
one tile-thickness apart, each plant drawn right after its own column, a 1700 x 1000 window cut
from inside a 64-cell grid. `src/menuScene.ts` hangs the picture once decoded and sets
`#menu[data-biome][data-frame]` for the CSS. Measured: ~0.6% main thread steady, zero
main-thread paints, ~1.1 s at load (the original menu ~1.2 s); 80 DOM nodes. Inspect a moment by
freezing animations with `#menu * { animation-delay: -Ns !important; animation-play-state: paused !important }`.

### Dev tooling
F9 kit and F10 panel, gated on the ticket's `dev` claim. `HEARTH_DEV_ALL=1` (set by
`npm run start:dev`) grants it to **loopback requests only** — checked per-request, not on the
bind address, because control binds `0.0.0.0` for LAN play. `HEARTH_DEV_TOKS` /
`HEARTH_DEV_USERS` allowlist named accounts. **The claim is baked into the ticket at join, so
you must reconnect after enabling it.** A dev session also shows an FPS / frame-time / network
meter (bottom-left), and the F10 panel's "Combat ranges + motion lines" toggle draws every
player's and enemy's attack radius and heading (`src/devOverlay.ts`; radii mirror the Go server).

---

## 5. Open items, ranked

1. ~~**`userId` is random and in-memory — players lose their character.**~~ **FIXED.**
   `userId` is now DERIVED, not minted: `control/identity.js` returns
   `'u_' + HMAC-SHA256(secret, "hearth-user-id:v1
" + tok)[:12]`, same `u_` + 24-hex shape as
   the old `randomBytes(12)`, so no consumer changed. It needs no storage and survives a
   restart — or a second control-plane process — which the `Map` never could. The secret comes
   from `HEARTH_ID_SECRET`, else a gitignored `control/.id-secret` generated on first boot
   (`wx`, so a racing process cannot clobber it), else a built-in constant with a warning:
   deterministic-and-public beats private-and-volatile, because a volatile secret *is* this bug.
   **Rotating the secret renames every account and orphans every save** — pin
   `HEARTH_ID_SECRET` before running more than one control plane. `control/test.mjs` proves it
   by spawning a genuinely restarted instance (`:8096`) and comparing the two ticket `userId`s,
   plus distinctness across toks and that the id really is keyed (`:8095`, different secret).
2. **Room codes are still cosmetic.** `requestJoin()` sends only `{name, tok}` — no `worldId` —
   and `src/menu.ts` collects a room code into localStorage and sends it nowhere. The multi-world
   backend is complete and proven; this is now pure wiring on the menu and join call.
3. **No durable storage in the control plane.** Profiles and the world registry are in-memory,
   behind the `store.js` seam designed for a DB swap. No longer fatal — item 1 moved the save
   key out of that Map — but a restart still drops `name` and `createdAt` (the client re-sends
   its name on the next join, so the visible loss is small).
4. **Decide when to retire the legacy server.** `server/index.js` and `?legacy=1` both work and
   are tested, but they are a second implementation of the game rules — the exact thing the
   migration existed to remove. `npm run server:dev` still points at it.
5. **Multi-instance identity.** Two `hearthd` processes can each hold a live session for one
   `userId`. Needs presence state the control plane lacks (shared `userId -> {instanceId,
   sessionId}` plus an evict RPC or pub/sub, with leases so a crashed instance expires). The
   room-side logic would not change — only who tells it to evict.
6. **Cold multi-world first join** waits ~6s for worldgen with no progress shown.
7. **z=2 shelter interiors are unvalidated for x/y** — the server has no `shelterAnchor`, so
   inside a shelter a client can walk through walls. It must still *walk* there.
9. **Demolition is ownerless.** `handleAtk` picks the nearest structure, then the nearest
   module, within 2.4 tiles and refunds to whoever swung — `owner` is recorded on both and
   never checked. Free griefing in co-op; the guide's §2 rule 5 asks for owner-only demolish.
8. Minor: `buildDevPanel()` closes over the scene that built it (stale after quit-and-rejoin);
   `pj` is broadcast to the joining player itself (cosmetic — the client's own-id guard means no
   ghost renders); `control/worlds.json` is not checked in (env fallback covers the default);
   medic visibility for a *second* client and medic placement across a reconnect are unverified.

---

## 6. Deliberately deferred (owner's decision — do not start without asking)

- **Elevation rework** — `docs/ELEVATION_AND_WORLD_EDGE_GUIDE.md`
- **Postgres/Redis room architecture** — `docs/MULTIPLAYER_ROOM_ARCHITECTURE_GUIDE.md`
- **`docs/07_REMAINING_IMPLEMENTATION_ROADMAP.md`** — owner said to ignore new doc additions

---

## 7. Next feature work

1. ~~**Modular building system**~~ **SHIPPED** (§4). What the guide still lists as open and
   this pass did not do: no rotation at all (the block art has no meaningful facing; railings and
   banners would need edge-drawn art), modules are surface-only (`z=0`), and demolition is still
   ownerless — anyone may knock anything down (see §5.9).
   Smaller leftovers from `docs/PLAYER_BUILDING_CUSTOMIZATION_GUIDE.md`: crop weather coupling,
   the `frostroot` crop and per-crop tile gating, a crop picker (the client hard-codes
   "wheat if fiber≥2 else glowcap"), and per-stage crop art.
2. **`PLAN.md` systems with zero code** — fire spread automata, water flow/trenches, blight
   evolution, convergence events, transport networks, Blighted Heart mini-dungeons,
   roles/classes. Fire spread and blight evolution were judged highest value. These are exactly
   the full-map CPU work that motivated one goroutine per room.
3. **Interest management on `cre` broadcasts** — every player currently receives every creature
   update for the whole world; there is no interest management. This is what limits room size,
   and it can reuse the existing chunk-radius logic.

---

## 8. Feature → file map

| Area | Files |
|------|-------|
| **Go game server** (authoritative) | `gameserver/` |
| — worldgen, bit-exact port | `gameserver/world/` **(verified — do not edit without re-running parity)** |
| — room loop, handlers, AI, medic, dev | `gameserver/room/` |
| — enemy levels / base brain / attacks / abilities | `gameserver/room/creature_{levels,base,attack,elite}.go` |
| — multi-world routing | `gameserver/hosting/` |
| — ticket verification | `gameserver/auth/` |
| — chunk RLE codec | `gameserver/proto/` |
| — WebSocket edge | `gameserver/net/` |
| **Node control plane** | `control/` (`index.js`, `store.js`, `ticket.js`, `PROTOCOL.md`) |
| **Legacy Node game server** | `server/index.js` (still live on :8081) |
| Rules data (authored) | `shared/defs.json` → generated `shared/defs.js` |
| Worldgen (JS, legacy + parity reference) | `shared/world.js` |
| Day cycle | `shared/time.js` |
| Phaser scene, rendering, input, protocol client | `src/main.ts` |
| Streamed tile store | `src/tiles.ts` |
| Control-plane join / URLs / LEGACY flag | `src/net.ts` |
| Player rig / animation | `src/rig.ts` |
| In-game DOM UI | `src/ui.ts` |
| Menu / lobby / profile / settings | `src/menu.ts`, `src/boot.ts`, `src/settings.ts` |
| Asset manifest / audio | `src/assets.ts`, `src/audio.ts` |
| All art | `assets/sprites/**` (SVG only) |
| Dev stack launcher | `tools/dev/stack.mjs` |
| Worldgen parity harness | `tools/worldparity/` |
| Defs generator | `tools/defs/gen-defs.mjs` |
| Test suites | `gameserver/test-go.mjs`, `gameserver/test-multiworld.mjs`, `control/test.mjs`, `test.mjs` (legacy) |
| Deep technical reference | `AGENT_GUIDE.html` |
