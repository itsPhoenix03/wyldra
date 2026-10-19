# Advanced enemy AI — levels, base brain, telegraphed attacks, level abilities — 2026-10-18 20:30

## Session summary
The owner asked for an "advance enemy AI" on a new branch `feat/advance-enemy-ai`: better
movement, attacks and tactics as a base for every enemy, plus features only some enemies get
depending on their level, distributed across types, built by Sonnet agents under guidance. The
orchestrator built the skeleton (levels, ability kits, hook points, wire contract); four Sonnet
agents filled disjoint files in parallel; the orchestrator integrated and verified. Nothing is
committed — the owner has not asked for a commit.

## What shipped
- **Levels** (`gameserver/room/creature_levels.go`): level 1–5 per creature, rolled at natural
  spawn (`spawnLeveled` → `rollLevel` + `levelUp`): type tier + (strength−1)/2 + veteran roll
  (+1 at 16 %, +2 at 4 %) + 1 in the final assault, capped 5, ≤ 2 alphas alive. +1 hp per level
  above tier, alpha +50 %. `creKit` lists which ability each type learns at which level. Dev
  spawns and test fixtures are level 1 (`devcmd spawn` takes `lvl`).
- **Base brain** (`creature_base.go`, every enemy): sight lines (`creLOS`), hearing 6, swing
  noise 14 / 1.5 s, the scent radius actually working (legacy bug: it never exceeded sight),
  grudge / stickiness / wounded scoring, 3 attack slots per keeper with the rest circling a
  3.2-tile ring, momentum, side-consistent ±35° steering, stuck recovery.
- **Attack layer** (`creature_attack.go`, every enemy): melee state machine ready → windup
  (fixed heading, `catk`/`ctel`) → lunge/slam → recover → cooldown; aimed ranged shots (`caim`,
  then `shot` at the stored aim). Tables in docs/10 §13.6.
- **Level abilities** (`creature_elite.go`): flank, pack call, dodge (`cdodge`, swing whiffs),
  hit and run, charge (`ccharge`), lead shot, alpha (focus fire, `crally`, `cpanic` on death).
- **Wire**: `cre` rows carry level as 5th element; new frames `catk caim cdodge ccharge crally
  cpanic`; `ctel` gains `ang`/`tk`. docs/10 §13.6 is the spec, with tuning.
- **Client** (`src/main.ts`, `src/audio.ts`): baked level pips / alpha crown + glow, every
  telegraph drawn (windup crouch + lunge, aim line, charge strip, dodge hop, rally ring, panic
  "?"), five synthesized sounds, dev-panel level select. Legacy server (4-element rows) degrades
  to level 1 with no badges.
- **Tester panel** (follow-up request): `devcmd aitest {kind}` — five scripted squads
  (`devAIScenarios` in dev.go) that clear the arena and spawn homeless at fixed levels — and
  `devcmd monoreset`, which darkens all four monoliths and broadcasts `{t:"monoreset"}`
  (Engine / wave / `won` untouched). Buttons in the F10 panel; both covered by the no-claim
  refusal tests (Go and wire) and by `TestDevMonoReset` / `TestDevAITestScenarios` and the
  DEVOK stage. Dev placement is now one helper, `devSpot`, which also avoids structures.
- **Verified by the orchestrator**: `tsc`, `vite build`, Go build/vet/gofmt, `go test ./...`
  (room package 8× in a row), `test-go.mjs` ALL TESTS PASSED with zero skips,
  `test-multiworld.mjs`, `test:control`. Benchmarks equal to HEAD within noise
  (realistic ~150–190 µs, CreatureAIOnly ~32 µs, overload ~1.5 ms per tick).

- **Title screen redesign** — five rounds with the owner. (1) "more crazy and impressive": a
  floating-archipelago diorama; (2) "remove the mid part ... uses npcs and tiles": a panning
  shore of four villages; (3) "like a game home screen should be": game layout — logo plus a
  vertical, keyboard-driven menu with a one-click **Play** — over a firelit camp; (4) "use only
  tiles and no npcs or animals, just tiles of spire": a Frozen Spire island baked from Spire
  tiles; (5) "make use of woods now" + "add tree and vegetation also and do not make it
  surrounded by water all sides": the current Woods landscape (PROJECT_STATE §4). Lessons: game
  conventions (menu list) over a backdrop of the game's own tiles and plants, no characters or
  animals, land running off the edges; bake it at build time (an SVG-<img> version cost 2.4 s
  main-thread at load). Adding PNG filters 3/4 to the atlas encoder made files bigger with its
  min-residual heuristic — reverted. Verified with frozen-time headless screenshots (narrow widths
  via iframes — headless Chrome won't size a window under ~500 px), a CDP key test and CDP traces.

## Decisions made
- **Layer = file = embedded struct.** `Creature` embeds `creBase`, `creAtk`, `creElite`, each
  declared in its own file, so a layer adds fields without touching creatures.go. Cross-layer
  reads are only: `AggroID`, `NoSlot` (base), `AtkState` (attack), `ChargeTk`, `PanicTk`
  (elite), and `mayStrike()`. Keep it that way.
- **`stepCreature` owns the hook order**; layers never call each other's step logic.
  `attackBusy` runs before the wisp/wraith drift; `eliteOverride` after stranded/leash.
- `NoSlot` is a denial flag (zero value = may attack) so tests that step one creature without
  `assignSlots` still see it attack.
- Ring holders may still strike a keeper who walks into their reach (slots govern approach,
  not opportunity) — otherwise hugging a waiter would be safe.
- `choosePrey` leaves `AggroID` = final pick, so `selectPrey` sees last tick's prey (stickiness)
  and `assignSlots` sees this tick's.
- Dodge resolves in `handleAtk` before `act` is broadcast: a dodged swing's `act` has
  `targetI: null` and nothing else is struck (no fall-through to demolition).
- The lance hits keepers in front of the structure that stops it (segment ends at the
  structure); keepers behind it are shielded.
- Brutes no longer chase underground keepers (the legacy fallback loop wasn't z-filtered).
- Go-only, like the rest of the hunting brain: `server/index.js` was not touched.

## Bugs found and root causes
- Legacy scent radius was dead code: the nearest-player search started `bd` at the sight
  radius, so `min(bd, rad)` could never reach the 40-tile essence radius. Fixed in `selectPrey`.
- Without a "walked ≥ 1.2 tiles" escape, stuck detection fires on tactics whose aim moves with
  the creature (stalker orbit, lancer strafe, retreat).
- Lunges must respect campfire fear or `TestCrawlerFearsFireAtNight` fails (fire-shy creatures
  lunged into the ring).
- `TestWolfPackFlanks` sampled the spread on the last tick, which after telegraphed strikes and
  hit-and-run measures the retreat; it now takes the widest spread of the approach.
- Parallel Go agents in one package see each other's half-written files fail to compile; one
  agent worked around it with a `go build -overlay` in the scratchpad.

- **Keepers could demolish the World Engine** (owner report): `handleAtk`'s structure-demolition
  fallback picked the nearest structure within 2.4 with no kind filter, so one iron-sword swing
  ended the final assault and refunded half the Engine. The Engine is now never a demolition
  candidate (Go + legacy); `TestKeeperCannotDemolishEngine` replaced the test that pinned the old
  behaviour. Only creatures can bring it down.
- **Crafting through a mine's ceiling** (owner report): `nearStruct` (Go), `nearStruct`
  (legacy) and the client's `nearKind` compared x/y only — the "tile indices are shared across
  z-layers" pitfall. A keeper at z=1 under a workbench could craft. All three now require
  `z == 0`; the same leak was closed on `build`, `plant` and `harvest` (Go + legacy). Chests
  were already layer-checked. `TestNoReachThroughTheFloor` fails without the guard. Legacy suite
  run on a port-8181 copy with a scratch save: ALL TESTS PASSED.

- **Vegetation on built tiles / farms / healing** (owner report). Go regrowth already refused
  built tiles; the gaps were (a) the legacy server regrew nodes through structures (fixed),
  (b) worldgen decor (glow mushrooms…) drew through anything built (client now hides it),
  (c) the client's `cure` repainted soured ground as grass (now restores mud), and (d) a real
  farm bug, found by the new end-to-end farm test: the creature step switch gnawed ANY
  non-decor structure on its step tile, and steering allows stepping onto farm plots, so a
  hunter crossing a field chewed the plot and its crop away (Go + legacy). Mud never healed;
  `ecology.go` adds the healing (rest + regrowth), `regrow` frame, docs/10 §8.6. Chopping
  pressure now resets when the ground sours, so decay can't re-trigger it on one felling.

## What did NOT get done
- No playtest in a browser — the client was verified by `tsc`/`vite build` and code review only.
- Legacy Node server has none of this (by design).
- `devOverlay.ts` does not label levels (skipped to avoid per-frame cost).
- Balance is first-pass: crawler ~0.83 hp/s vs a still keeper (was 1.0); a hugged brute slams
  ~every 2 s for 2 in a 1.4 radius (was 2/s). Wave balance was not retuned.

## Notes for the next session
- Commit is pending the owner's go-ahead (branch `feat/advance-enemy-ai`).
- Run the wire suites against a throwaway stack on spare ports (control 8190 / game 8182 /
  `HEARTH_SAVE_DIR` in the scratchpad, `HEARTH_TICKET_PUBKEY/PRIVKEY` from test-go.mjs) — never
  delete `gameserver/world*.save.json`.
- `git worktree add` fails on this repo (Windows path length under `art-packs/`); for a HEAD
  comparison use `git archive HEAD gameserver shared | tar -x`.
- Every creature-caused `hp` frame must carry `ang` — the wire suite tells creature hits from
  weather by it.
