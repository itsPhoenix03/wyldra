package room

import (
	"fmt"
	"math"
	"math/rand"

	"hearth/gameserver/world"
)

// Slice 4: the F9 dev kit (`dev`) and the F10 tester panel (`devcmd`).
//
// # Gating
//
// The legacy server gates both on a process-wide `DEV` env var, which cannot
// work once one Go process hosts rooms for many players: the switch is either
// on for everybody in every room or off for everybody. Per the architecture
// decision in docs/09 §7 ("gate on a claim in the ticket Node issues, rather
// than the current server-wide DEV env var"), the authority here is a per-player
// claim carried in the signed ticket.
//
// The control plane mints that claim: /api/join signs an optional boolean `dev`
// into the ticket payload, set from the account's own permission list and never
// from anything the client sends (control/PROTOCOL.md §2, §3.3). Node only ever
// emits it as `true` — an account without the permission gets a payload with no
// `dev` key at all — so in practice the claim is "true or silent".
//
// The rule is now a single one: the claim is present and true, or dev tools are
// off. There is no server-side fallback — the earlier `HEARTH_DEV` env var is
// gone, because an operator flag that grants world-mutating commands to every
// connected player has no place in production, and with the control plane
// minting the claim there is nothing left for it to cover. An absent claim
// means "this account is not a dev" (control/PROTOCOL.md §2); an explicit
// false, which Node never mints, is denied for the same reason.
//
// devAllowed is the single choke point. Every command below goes through it.
func (r *Room) devAllowed(p *Player) bool {
	// Dereferencing is safe under the nil check, and the pointer is never
	// rewritten after the handshake.
	return p.S.DevClaim != nil && *p.S.DevClaim
}

// The refusal strings. The client renders them as a toast, so they are short,
// and the two differ from each other exactly as they do in the reference
// server.
//
// They are deliberately NOT the legacy text any more. The legacy strings said
// "start the server with: npm run server:dev", which on this path is wrong and
// actively misleading: `npm run server:dev` starts the *legacy* Node server on
// :8081 with its process-wide DEV flag, and the Go server never reads it. A
// developer who followed that advice would see nothing change and conclude the
// tester panel was never ported.
//
// The accurate advice is the control plane's: dev is a per-account claim signed
// into the ticket (control/PROTOCOL.md §3.3). `npm run start:dev` sets
// HEARTH_DEV_ALL=1, which grants the claim to any loopback client — the local
// developer — while named accounts are granted through the
// HEARTH_DEV_TOKS / HEARTH_DEV_USERS allowlist. Reconnect after either, since
// the claim is baked into the ticket at join time.
const (
	devOffMsg    = "Dev tools off — your ticket carries no dev claim. Locally: npm run start:dev, then reconnect."
	devCmdOffMsg = "Dev tools off — no dev claim. Locally: npm run start:dev, then reconnect."
)

// devKitInv is the `dev` grant, verbatim from server/index.js. Every key is a
// real INV_KEYS slot, so this only ever overwrites existing counters.
var devKitInv = map[string]int{
	"wood": 500, "stone": 500, "fiber": 200, "crystal": 100, "iron": 100,
	"diamond": 50, "starmetal": 50, "essence": 100, "water": 10, "meat": 5,
	"cookedmeat": 10, "wall": 50, "campfire": 5, "workbench": 3, "forge": 2,
	"mineshaft": 3, "engine": 1, "core": 4, "boat": 2,
	"sboat": 1, "torch": 30,
}

var devKitTools = []string{"axe", "pick", "spick", "sword", "isword"}
var devKitGear = []string{"heatcloak", "furcloak", ArmorKey}

// handleDev is the F9 kit: every tool, both cloaks, and enough of everything to
// reach the endgame in one sitting.
func (r *Room) handleDev(p *Player) {
	if !r.devAllowed(p) {
		r.send(p, map[string]any{"t": "msg", "s": devOffMsg})
		return
	}
	for k, v := range devKitInv {
		p.Inv[k] = v
	}
	// building materials come from defs rather than the literal above, so a new
	// material never needs a dev-kit edit to be testable
	for _, k := range r.defs.Materials {
		p.Inv[k] = 100
	}
	for _, t := range devKitTools {
		p.Tools[t] = true
	}
	for _, g := range devKitGear {
		p.Gear[g] = true
	}
	p.HP, p.Hunger, p.Thirst = r.defs.MaxHP, 10, 10
	r.sendInv(p)
	r.send(p, map[string]any{"t": "stat", "hunger": 10, "thirst": 10})
	r.send(p, map[string]any{"t": "msg", "s": "\U0001F6E0 DEV KIT granted: all tools, gear and materials."})
}

// handleDevCmd is the F10 tester panel. Unknown commands are silently ignored,
// as in the legacy server.
func (r *Room) handleDevCmd(p *Player, m map[string]any) {
	if !r.devAllowed(p) {
		r.send(p, map[string]any{"t": "msg", "s": devCmdOffMsg})
		return
	}
	switch getString(m, "cmd") {
	case "tp":
		// `typeof m.x === 'number'`: a numeric JSON value and nothing else.
		fx, okX := m["x"].(float64)
		fy, okY := m["y"].(float64)
		if !okX || !okY || math.IsNaN(fx) || math.IsInf(fx, 0) || math.IsNaN(fy) || math.IsInf(fy, 0) {
			return
		}
		// The legacy handler does not clamp; it survives because JS returns
		// undefined for an out-of-range tile index where Go would panic.
		p.X = math.Min(world.SIZE-1, math.Max(0, fx))
		p.Y = math.Min(world.SIZE-1, math.Max(0, fy))
		p.Z, p.B = 0, 0
		if r.world.Tiles[ti(p.X, p.Y)] != world.TWater {
			p.LastLandX, p.LastLandY = p.X, p.Y
		}
		r.warped(p)
		r.broadcast(map[string]any{"t": "pos", "id": p.S.ID, "x": p.X, "y": p.Y, "z": 0})
		r.send(p, map[string]any{"t": "hp", "hp": p.HP, "x": p.X, "y": p.Y})
		// Not in the legacy server, which shipped the whole map inside `init`:
		// the Go server streams terrain, so a teleport has to push the chunks
		// around the destination or the tester lands in an empty world.
		r.pushChunks(p)

	case "mono":
		// `m.i >= 0 && m.i < 4`, restricted to an actual JSON integer for the
		// same array-indexing reason as handleUseCore.
		f, isNum := m["i"].(float64)
		if !isNum || math.IsNaN(f) || f != math.Trunc(f) || f < 0 || f >= 4 {
			return
		}
		i := int(f)
		if r.mono[i] {
			return
		}
		r.mono[i] = true
		r.broadcast(map[string]any{"t": "mono", "i": i})
		if allTrue(r.mono) && !r.won && r.wave == nil {
			r.send(p, map[string]any{"t": "msg", "s": "All Monoliths lit — build the Engine at the Core."})
		}

	case "god":
		p.God = !p.God
		if p.God {
			p.HP = r.defs.MaxHP
		}
		r.send(p, map[string]any{"t": "hp", "hp": p.HP, "x": p.X, "y": p.Y})
		state := "OFF"
		if p.God {
			state = "ON — you cannot die"
		}
		r.send(p, map[string]any{"t": "msg", "s": "\U0001F6E1 God mode " + state})

	case "wx":
		kind := getString(m, "kind")
		if kind != "rain" && kind != "sandstorm" && kind != "snowstorm" {
			kind = ""
		}
		r.weather.kind = kind
		if kind == "" {
			r.weather.until = 0
		} else {
			r.weather.until = r.now() + 180000 // 3 min for testing
		}
		r.broadcast(map[string]any{"t": "wx", "kind": nullable(kind)})

	case "time":
		v, ok := m["v"].(float64)
		if !ok || math.IsNaN(v) {
			return
		}
		r.time = math.Max(0, math.Min(0.999, v)) // the sim tick broadcasts it
		label := "day"
		if r.isNight() {
			label = "night"
		}
		r.send(p, map[string]any{"t": "msg", "s": "\U0001F550 Time set to " + label})

	case "spawn":
		typ := getString(m, "type")
		ct, known := creTypes[typ]
		if !known {
			return
		}
		// place it a few tiles away on terrain it can actually occupy
		sx, sy, found := r.devSpot(p.X, p.Y, typ == "drowned", 4, 8)
		hp := ct.hpBase + ct.hpStr*r.strength()
		// no home tile -> no leash, so the test subject always commits to the
		// chase instead of wandering back
		c := r.newCreature(sx, sy, hp, typ, 0, false)
		// level 1 unless the tester asks for one (2-5, docs/10 §13.6)
		if lv := getNum(m, "lvl"); !math.IsNaN(lv) && lv >= 2 {
			r.levelUp(c, int(lv))
		}
		tail := ""
		if !found {
			tail = " — no valid tile, placed on you"
		}
		r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf("\U0001F9EA Spawned %s Lv%d (%d hp)%s", typ, c.Level, c.HP, tail)})

	case "clearcre":
		n := len(r.creatures)
		r.creatures = map[string]*Creature{}
		r.creOrder = nil
		r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf("\U0001F9F9 Cleared %d monsters", n)})

	case "aitest":
		// an enemy-AI set piece: clear the arena, then drop the squad together
		// 7-9 tiles away so it comes at the tester as a group (docs/10 §13.6)
		sc, known := devAIScenarios[getString(m, "kind")]
		if !known {
			return
		}
		r.creatures, r.creOrder = map[string]*Creature{}, nil
		ax, ay, _ := r.devSpot(p.X, p.Y, false, 7, 9)
		for _, sq := range sc.squad {
			x, y, ok := r.devSpot(ax, ay, false, 1, 2.5)
			if !ok {
				x, y, _ = r.devSpot(p.X, p.Y, false, 4, 8)
			}
			ct := creTypes[sq.typ]
			c := r.newCreature(x, y, ct.hpBase+ct.hpStr*r.strength(), sq.typ, 0, false)
			if sq.lvl >= 2 {
				r.levelUp(c, sq.lvl)
			}
		}
		r.send(p, map[string]any{"t": "msg", "s": "\U0001F9EA AI test — " + sc.name + ": " + sc.hint})

	case "monoreset":
		// every monolith back to dormant, as in a new world. Strength (and so
		// spawn gates and enemy levels) drops with it; the Engine, a running
		// wave and a won world are left as they are.
		n := 0
		for _, lit := range r.mono {
			if lit {
				n++
			}
		}
		r.mono = [4]bool{}
		r.broadcast(map[string]any{"t": "monoreset"})
		r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf("\U0001F5FF Monoliths reset — all four dormant (%d were lit)", n)})

	case "kill":
		p.God = false
		p.HP, p.Z = r.defs.MaxHP, 0
		p.Hunger, p.Thirst = 10, 10
		p.X, p.Y = r.respawnPoint(p.S.ID)
		r.warped(p)
		r.send(p, map[string]any{"t": "hp", "hp": p.HP, "x": p.X, "y": p.Y})
		r.send(p, map[string]any{"t": "msg", "s": "\U0001F480 Killed — respawned."})
		r.pushChunks(p) // see the note under `tp`
	}
}

// devSpot finds a tile rmin..rmax tiles from (x, y) that a creature can stand
// on: open water for one that wants it, otherwise land a creature may step onto
// (no structure, wall or medic hut). found=false leaves it at (x, y).
func (r *Room) devSpot(x, y float64, wantWater bool, rmin, rmax float64) (float64, float64, bool) {
	for att := 0; att < 40; att++ {
		a := rand.Float64() * math.Pi * 2
		rr := rmin + rand.Float64()*(rmax-rmin)
		cx := math.Round(x + math.Cos(a)*rr)
		cy := math.Round(y + math.Sin(a)*rr)
		if cx <= 0 || cy <= 0 || cx >= world.SIZE || cy >= world.SIZE {
			continue
		}
		i := ti(cx, cy)
		if wantWater == (r.world.Tiles[i] == world.TWater) && (wantWater || !r.creStepBlocked(i, false)) {
			return cx, cy, true
		}
	}
	return x, y, false
}

// devSquad is one member of an AI test squad: a type at a level.
type devSquad struct {
	typ string
	lvl int
}

// devAIScenarios are the tester panel's enemy-AI set pieces, keyed by the
// `aitest` kind. Each shows off one slice of the brain (docs/10 §13.6); the
// hint is the toast telling the tester what to watch for.
var devAIScenarios = map[string]struct {
	name, hint string
	squad      []devSquad
}{
	"swarm": {"Swarm", "six Lv1 crawlers. Three close in at once while the rest circle; " +
		"every lunge is telegraphed, so step aside.",
		[]devSquad{{"crawler", 1}, {"crawler", 1}, {"crawler", 1}, {"crawler", 1}, {"crawler", 1}, {"crawler", 1}}},
	"pack": {"Wolf pack", "a crowned Lv5 alpha and three wolves. They flank and follow the alpha's lead; " +
		"strike the alpha to enrage them, kill it to make them panic.",
		[]devSquad{{"husk_wolf", 5}, {"husk_wolf", 3}, {"husk_wolf", 2}, {"husk_wolf", 2}}},
	"duel": {"Duelists", "two Lv3 crawlers that dodge and hit and run, and a Lv4 stalker that charges. " +
		"Swing while they wind up; bait the charge into a wall.",
		[]devSquad{{"crawler", 3}, {"crawler", 3}, {"stalker", 4}}},
	"siege": {"Siege", "a Lv4 brute and a Lv4 blight lancer. Shots are aimed first and lead you; " +
		"step off the aim line. The brute charges.",
		[]devSquad{{"brute", 4}, {"blight_lancer", 4}}},
	"warlord": {"Warlord", "a Lv5 brute leading four Lv2 crawlers. They hunt whoever it hunts; " +
		"kill the warlord and they scatter.",
		[]devSquad{{"brute", 5}, {"crawler", 2}, {"crawler", 2}, {"crawler", 2}, {"crawler", 2}}},
}

// godTick is the legacy dev god mode: heal any protected player back to full
// every tick. One choke point is enough because no single hit exceeds MAX_HP, so
// hp never reaches the respawn threshold between two ticks.
func (r *Room) godTick() {
	for _, gp := range r.playerOrder {
		if gp.God && gp.HP < r.defs.MaxHP {
			gp.HP = r.defs.MaxHP
			r.send(gp, map[string]any{"t": "hp", "hp": gp.HP, "x": gp.X, "y": gp.Y})
		}
	}
}
