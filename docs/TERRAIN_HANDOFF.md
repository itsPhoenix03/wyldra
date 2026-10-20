# Terrain handoff: tile mountains, feature flags, peek camera

> **Status: complete.** `tileMountains` now ships **on** by default (`gameserver/room/features.go`), and the `init()` override in `room/features_test.go` is gone. What §4 asked for is done:
> - **Shape:** `MaxElev` 32, Spire peak 28, faces of at most 4, cairns and full-turn trails (`gameserver/world/mountains.go`).
> - **Rock faces:** a mountain tile's levels under its top are drawn with `mtn_<isle>_<rock|snow>_face{,2,3,4}`, with `_strata` on a few levels (`tools/art/mountain-faces.mjs`, picked by `src/peaks.ts` `faceAt`). The same faces are used in `drawChunk`, `repaintTileBox` and the strips.
> - **32-level elevation audit (`src/tiles.ts` `MAX_ELEV`):** chunk bake pad 310 + 80 px, `repaintTileBox` reach ±12, strip bake budgeted in draws, `coverAt` looks as far ahead as the region's peak.
> - **Silhouette:** fixed. It was drawn off its texture.
> - **Verified in game** on a throwaway stack:
>   - Woods, Dunes and Spire with the flag on, and off (flat ground plus sprite mountains), toggled live;
>   - the Woods trail walked by key input from foot to summit (elev 25), with no snapbacks and no damage;
>   - the keeper hidden behind the Spire, with the silhouette showing.
> - **Suites:** `go vet` and `go test ./...`, `test-go.mjs` (its fall-damage cliff now avoids the mountains), `smoke.mjs` (now mountain-aware: terrain may differ only inside the stamps), `PARITY OK`, tsc, vite build.
>
> Not checked: the camera peek offsets, depth sort at the foot in detail, `test-multiworld`. Faint chunk-edge lines at zoom 0.55 are there with the flag off as well. The sections below are the history.

The status of the terrain work on branch `feat/tile-mountains`, and what is left to do. It is written so any agent (Claude, Codex, …) can pick it up cold. Read it top to bottom before touching code.

## 0. What the owner asked for (verbatim)

> "make mountains using tiles and when the player goes behind a mountain made of tiles or a place where there is a height difference then the camera should move towards that side if the player is moving only then, both the mountains and this camera moving should be feature flagged and can be turned off."
> "give an option in the tester menu for feature flag switching"
> "do not update in the legacy server, only go server"

## 1. Branches and state

- **`feat/world-map-layout`**: world map v5 (the generator rewrite). It is the base of everything below.
- **`feat/npc-routines`**: the folk/NPC overhaul, commit `e924b76`. Done and verified.
- **`feat/tile-mountains`** (this branch, off `feat/npc-routines`): the terrain work. About **60% done**.
  - The server side is complete and tested.
  - The client side compiles and builds but has **never been run in a browser**.
  - **`tileMountains` ships OFF by default** (`gameserver/room/features.go` `featureDefs`), because the mountains don't look right yet (see §4.1). The room tests force it on in `room/features_test.go` `init()`, so the stamp stays covered.

Checks that pass on this branch:
- `cd gameserver && go vet ./... && go test -count=1 ./...`
- `npx tsc --noEmit`
- `npx vite build`
- `node tools/worldparity/compare.mjs`, which must print `PARITY OK`. `shared/world.js` is untouched.

## 2. Hard rules (do not break)

- **Do not modify the legacy Node server** (`server/`), and **do not change the output of `shared/world.js`**. It is the bit-exact JS reference of the Go generator `gameserver/world/world.go`, and the parity gate above must stay OK. Tile mountains are a **Go-only pass applied by the room on top of `world.GenWorld`**, like the village camps (`gameserver/world/folk.go`).
- Never bind ports 8081 / 8090 / 8082 / 5173: that is the owner's live stack. Never touch `gameserver/world*.save.json` or `server/save.json`. Use throwaway ports and scratch save dirs, via `HEARTH_SAVE_DIR`.
- No `Co-Authored-By` or AI attribution in commits or PRs. Commit or push only when the owner asks.
- **Browser main-thread budget:** bake terrain to RenderTextures once, never per frame. The per-frame camera logic must stay O(a few tile lookups).
- With both flags OFF, the game must look and behave exactly as before, sprite mountains included.

## 3. What is built

### 3.1 Feature flags (server-authoritative, per room). Done and tested.

- **Registry:** `gameserver/room/features.go` (`featureDefs`: name, default, description, terrain?). Adding a flag is one line.
  - Flags: `tileMountains` (default **false** for now) and `cameraPeek` (default true).
- **Precedence:** the save, then the per-world `features` in the worlds config, then `HEARTH_FEATURES="tileMountains=1,cameraPeek=0"`, then the default. The env and config are parsed in `gameserver/hosting/config.go` and `manager.go`.
- **Persistence:** saved as an optional `features` field (`gameserver/persist/persist.go` Snapshot). A save without it loads with the defaults, and `WORLD_VERSION` is unchanged.
- **Wire** (documented in `docs/10_GO_WIRE_PROTOCOL.md` §14):
  - `init.features = {tileMountains, cameraPeek}`;
  - a broadcast `{t:"feat", features}` on every change;
  - `devcmd {cmd:"feat", name, on}`: dev-claim gated, validated, with a toast to the tester. It lives in `gameserver/room/dev.go`.
- **A runtime toggle of a terrain flag** (`applyTerrainFeatures`) does this:
  1. broadcasts `feat`;
  2. stamps or lifts the mountains;
  3. drops the touched `chunkCache` entries;
  4. re-pushes those chunks to players whose push window holds them, and marks them unsent for everyone else.
- **Client:**
  - the "Feature flags" checkboxes are in the F10 tester panel (`buildDevPanel` in `src/main.ts`) and update on `feat`;
  - the Settings menu has a "Camera peek" toggle (`src/settings.ts` key `hearth-campeek`, `src/menu.ts`, `index.html`). The effective peek is the server flag AND this setting;
  - a client with no `features` (the legacy path) treats `tileMountains` as false and `cameraPeek` as true.

### 3.2 Tile mountains (Go). Built and tested, but the shape needs rework (§4.1).

- **`gameserver/world/mountains.go`.** A deterministic stamp at each `MOUNTAINS` site (Woods (150,150), Dunes (1130,150), Spire (150,1130), Marsh (1130,1130), Core (640,630)).
  - Per-island numbers live in `massifSpecs`, the landform in `shape()`, the rock / ledge / trail choice in `classify()`, and the trail in `trailSearch()` / `trailAt()`.
  - It records every changed tile's base values (elev, tileVis, nodes, decor), so lifting the stamp restores the world byte for byte. This is tested.
  - It never covers water (keeping a shore margin), CLEARINGS with their free/land radius + 2 (monolith plazas, notes, spawn, medics, villages), MONOLITHS, TEMPLE_PIECES, medics or camps.
  - `MaxElev` is 12 for now.
  - tileVis values on mountain tiles: 2 rock (a cliff edge), 3 ledge, 4 snow rock, 5 snow ledge, 6 trail. `world.IsMountainVis` tests for them.
- **`gameserver/room/mountains.go`.** Applies the stamp to the room's **own copy** of elev, tileVis, nodes and decor. `cfg.World` can be shared between rooms or tests, so never write it.
  - `landmarkBlocks()`: the mountains' 3×3 `LandmarkBlock` footprints stop blocking while the flag is on (used in `movement.go` and `modules.go`).
    - `folk.go` still reads the raw `world.LandmarkBlock`. That is harmless, since villages are far away, but it could switch to `r.landmarkBlocks`.
  - `mtnClimbBlocked()`: creatures and animals cannot climb more than +1 onto or off mountain tiles. It is wired into steering, path search, lunge, charge, panic, dodge, knockback and animal wandering (`creature_*.go`, `combat.go`, `animals.go`).
  - `onMountainFace()`: nothing spawns on faces (creatures, animals, dev spawns).
- **Tests:**
  - `world/mountains_test.go`: determinism, reserved spots, a trail BFS at +1 per step reaching the summit, faces present, the restore. `HEARTH_MOUNTAIN_PNG=<dir> go test ./world -run TestMountainRenders` writes a top-down PNG, a text dump and a JSON region per mountain.
  - `room/features_test.go`: gating, the init payload, toggle / restore / restream, persistence, and the creature climb rule.
  - `hosting/features_test.go`.
- **Players** climb mountains with the normal rules: +1 per step, +2 while jumping. A drop of 2+ costs drop − 1 hp (`movement.go`).

### 3.3 Client. It compiles and builds, but has never been seen running.

- **`src/peaks.ts`:** depth-sorted **occlusion strips**. Around each mountain it bakes one RenderTexture per iso row (x + y), so tall terrain hides sprites behind it.
  - A row leaves out the cliff levels that a non-strip neighbour in front already covers.
  - The bake is incremental (≤ 140 stacks per frame), runs only for the nearest mountain, and is dropped when the camera is far away.
- **Depth model** (in `src/main.ts`): inside mountain regions, sprites sort by ground-plane (unlifted) depth through `depthAt`, `standAt` and `restand`. Elsewhere, and with the flag off, they use the old lifted-y depth unchanged.
  - The reason for the scoping: `src/folk.ts` sorts folk by lifted y, so a global switch would mis-sort the villages.
- **Silhouette:** while the keeper is hidden by terrain, a soft silhouette of them is drawn above it, via a small RenderTexture.
- **Camera peek:**
  - **Triggers:** only while the keeper moves, and only when they are occluded (cover > 0.2) or there is a rise or drop of 2+ levels within 2 tiles ahead (`heightAhead`, which samples 0.6 / 1 / 1.5 / 2 tiles along the move).
  - **Offset:** the follow offset eases toward the move direction, plus a bias toward the hidden side. It is capped at 128 px ÷ zoom.
  - **Easing:** after stopping it holds for 0.4 s, then eases back over about 0.8 s.
  - **Off when:** in mines (z = 1), when dead, in menus, or when the flag or setting is off.
- **Other client changes:**
  - per-island mountain tile keys in `tileKey` (`src/tiles.ts`, `src/assets.ts`), with 19 SVGs `assets/tiles/mtn_<isle>_<rock|ledge|trail|snow|snowledge>.svg`;
  - handling for chunks the server re-sends (a re-sent chunk replaces nodes and decor);
  - sprite mountains hidden while the flag is on;
  - an elev audit for 12 levels: a 120 px bottom pad on the chunk bake, and a ±7 `repaintTileBox` radius.

## 4. What is left (about 40%)

### 4.1 Reshape the mountains. Required; this is why the flag ships off.

**Problem.** The iso previews read as wide stepped plateaus, a ziggurat or quarry, or scattered stone ruins, not as mountains. The causes:
- the aspect ratio: about 15 tiles across (~650 px on screen) for only +10 levels (100 px at 10 px per level), roughly 10:1;
- terraces are concentric rings;
- isolated 1-level rock cells and rock-edged low steps throughout the foothills, which read as walls and ruins.

**Targets** (in `gameserver/world/mountains.go`):
- Raise `MaxElev` to about **32**.
- Peak heights over the local ground:

  | Mountain | Height | Form |
  |---|---|---|
  | Spire | +26 to +28 | the tallest: it is "the Spire" |
  | Woods | ~+22 | |
  | Dunes | +14 to +16 | a sheer-sided butte or mesa is right for sand, but taller and cleaner |
  | Marsh | +12 to +14 | a craggy tor |
  | Core | +8 to +10 | an obsidian spike; the Core island is small (R ~14) and has temple pieces at CORE ± 5 |

- The steep part stays compact, radius about 6–9. Foothills out to about 12–14 stay ordinary island ground.
- Silhouette: **one dominant summit, with 3–5 spurs or ridges and gullies between them.** Not rings.
  - Upper slopes are steep: 3–4 levels per tile.
  - **No face is taller than 4 levels**, so a fall costs at most 3 hp.
  - Lower slopes ease off; ledges are irregular.
- **No rock tileVis within about +3 of the base:** those tiles stay grass, sand, snow or mud with the normal ledges, and there are no isolated rock cells. Rock starts where the slope steepens. Scree (boulders, stones) goes near the foot.
- **Trail:** a spiral or switchback ledge at +1 per tile, with at least one full wrap on the big mountains, drawn with trail tiles, ending on a 2–4 tile summit. Add an optional small cairn if an existing decor sprite fits; it must not block.
- Keep every guarantee in §3.2 (reserved spots, byte-exact restore, determinism) and the tests.
- **Iterate visually:**
  1. `HEARTH_MOUNTAIN_PNG=<dir> go test ./world -run TestMountainRenders` writes `<key>.png`, `.txt` and `.json`.
  2. Draw an iso preview with the real tile art from the JSON. A ready script existed at the previous agent's scratch path `…/scratchpad/terrain/iso.mjs`, which is not in the repo. Recreate it: it read the JSON region, rendered each tile's SVG with `@resvg/resvg-js` (already in node_modules), stacked `e` copies at 10 px per level in painter's order by diagonal row, and wrote a PNG with `tools/worldmap/png.mjs`. Or simply judge it in-game.

### 4.2 Rock-face art. Required.

- **Problem:** stacking the whole 64×40 tile once per level leaves a hard horizontal band at every level, so faces look like brickwork or masonry.
- **Fix:** add face textures.
  - In `drawChunk` and `repaintTileBox` (`src/main.ts`), and in the strips in `src/peaks.ts`, draw a mountain tile's levels **below its top** with `<key>_face`, and only the top level with the top texture.
  - Design the face's visible 10 px band so it tiles seamlessly in the vertical: rock with vertical cracks, and a darker stratum line only every few levels, from 2–3 variants chosen by a hash of (x, y, level).
  - Keep the existing light/dark split between the two skirt faces. Snow-cap faces are snow-streaked rock.
  - Add the new keys to `src/assets.ts` beside the existing `mtn_*`.
- **Elev audit for 32 levels:** the chunk bake pad (now 120 px) must cover the tallest lift plus the 40 px tile, about 360 px. Also check the `repaintTileBox` radius (now ±7; it needs about ±12), strip RT heights, camera bounds, and any map or minimap view.

### 4.3 In-game verification. Required; never done.

Run a throwaway stack.
- The repo's own capture tool is `tools/media/capture.mjs`: headless Chrome over CDP, a dev player via `HEARTH_DEV_ALL=1`, and `window.__hearth` for teleport and time. Copy its approach.
- Start these, on free ports such as 8390 / 8382 / 5499 / CDP 9555:
  - control: `CONTROL_PORT=<c> HEARTH_DEV_ALL=1 HEARTH_GAME_WS=ws://localhost:<g> HEARTH_TICKET_PUBKEY=… HEARTH_TICKET_PRIVKEY=… node control/index.js`. The test key pair is the `TEST_PUBKEY` / `TEST_PRIVKEY` constants in `gameserver/test-go.mjs`.
  - game server: `HEARTH_GAME_PORT=<g> HEARTH_TICKET_PUBKEY=… HEARTH_EAGER_WORLDS=1 HEARTH_ALLOW_WARP=1 HEARTH_SAVE_DIR=<scratch> HEARTH_FEATURES=tileMountains=1 ./hearthd`, built with `go build ./cmd/hearthd`.
  - client: `VITE_CONTROL_URL=http://localhost:<c> npx vite --port <v> --strictPort`.
- Teleport with the devcmd `tp` (or the F10 panel's fast-travel buttons). Mountains sit about 42 tiles NW, NE, SW and SE of each island centre (ISLES in `shared/world.js`).

Check, with screenshots, and fix what looks wrong:
1. Each of the five mountains with the flags on; the same views with them off (flat ground plus sprite mountains); a wide shot of each island.
2. The keeper halfway up the trail and on the summit: climbing works, nothing is blocked wrongly, and fall damage is sane.
3. The keeper behind a mountain: occluded, with the silhouette showing.
4. **Peek camera:** moving behind a mountain compared with standing still. Log the camera offset values while walking. Also check that ordinary 1-level terraces do NOT make it lean.
5. The depth sort of trees, rocks, creatures and folk at the mountain's foot and on its ledges, and strip edges (seams, double-drawn faces). Transient effects (damage pops, dust, wakes) inside a region still sort by lifted y and can tuck under a strip; convert them to `depthAt` if so.
6. The F10 panel's "Feature flags" section: a live toggle in both directions. Off brings back flat ground and the sprite mountains with no stale chunks, and on stamps them again.
7. Wire suites against your own ports:
   - `gameserver/test-go.mjs`;
   - `gameserver/smoke.mjs`: it compares streamed terrain near spawn with `shared/world.js`, so with mountains on it will flag the Woods mountain chunk. Make it mountain-aware, or run it with `tileMountains=0` plus a separate check that streamed terrain differs only inside the mountain footprints;
   - `test-multiworld`: reads `HEARTH_MW_CONTROL_PORT` / `HEARTH_MW_GAME_PORT`, which default to 8390 / 8382, so don't clash with your stack.

### 4.4 Then

- Flip `tileMountains` back to default **true** in `featureDefs`, delete the `init()` override at the bottom of `room/features_test.go`, and re-run everything in §1.
- Update `docs/MAP_EXPANSION_GUIDE.md` and `docs/ELEVATION_AND_WORLD_EDGE_GUIDE.md` §9 with the final heights and elev range, and `.agents/PROJECT_STATE.md`.

## 5. Map of the code

| Area | Files |
|---|---|
| Flags | `gameserver/room/features.go`, `dev.go` (`feat` devcmd), `room.go` (init payload, Config.Features, save), `persist/persist.go`, `hosting/config.go`, `manager.go` |
| Mountain stamp | `gameserver/world/mountains.go`, `gameserver/room/mountains.go` |
| Climb and spawn rules | `gameserver/room/creature_ai.go`, `creature_attack.go`, `creature_elite.go`, `creatures.go`, `combat.go`, `animals.go`, `movement.go`, `modules.go` |
| Client terrain and depth | `src/main.ts` (`drawChunk`, `repaintTileBox`, `tileKey`, `depthAt`/`standAt`/`restand`, `applyChunkMsg`, silhouette, camera peek, `buildDevPanel`), `src/peaks.ts`, `src/tiles.ts`, `src/assets.ts`, `assets/tiles/mtn_*.svg` |
| Settings | `src/settings.ts`, `src/menu.ts`, `index.html` |
| Docs | `docs/10_GO_WIRE_PROTOCOL.md` §14, `docs/ELEVATION_AND_WORLD_EDGE_GUIDE.md` §9, `docs/MAP_EXPANSION_GUIDE.md` |

Facts worth knowing:
- SIZE is 1280 and a tile index is `i = y*SIZE + x`.
- An iso tile is 64×32 (art 64×40), and each elevation level lifts 10 px (`isoE` in main.ts).
- Terrain is baked into 1024×512 chunk RenderTextures at depth −1, under every sprite.
- The server streams 64×64-tile chunks (`gameserver/room/chunks.go`: `chunkCache`, `chunkMessage`, `pushChunks`, `p.sentChunks`), carrying tiles / elev / veins / wtemp / tvis.
