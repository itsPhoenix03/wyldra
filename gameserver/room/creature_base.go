package room

import (
	"math"
	"sort"
)

// The base layer of the enemy brain, given to every creature at every level:
// how it perceives keepers and picks one to hunt, how hunters share a keeper
// (attack slots), and how it moves. docs/10 §13.6.

const (
	baseHearRadius  = 6.0  // a keeper this close is heard through any wall
	baseNoiseRadius = 14.0 // a keeper who just swung is heard this far
	baseNoiseMs     = 1500 // how long a swing stays audible
	baseLOSStep     = 0.5  // sight-line sampling interval, tiles
	baseGrudgeTicks = 50   // 10 s
	baseSlotRange   = 8.0  // light melee hunters this close bid for a slot
	baseRingStep    = 0.35 // rad a ring holder's aim leads round the prey
	baseRingSpeed   = 0.55 // ring holders mill about at this fraction of speed
	baseStuckTicks  = 10   // sampled ticks without progress before it is "stuck"
	baseStuckGain   = 0.25 // tiles closer to the goal that count as progress
	baseStuckMove   = 1.2  // tiles walked from the anchor that also count
	baseUnstickTk   = 4    // sidestep length (the first tick is spent setting it)
	baseTurnLimit   = 100 * math.Pi / 180
)

// creBase is the base layer's per-creature state, embedded in Creature.
type creBase struct {
	// AggroID is the session id of the keeper currently hunted ("" = none).
	AggroID string
	// NoSlot: denied an attack slot on its prey this tick, so it holds a ring
	// aiRingRadius out (creature_levels.go) instead of closing in. The zero
	// value means "may close in", so a lone creature, or one stepped by a test
	// without assignSlots, always attacks.
	NoSlot bool

	// the keeper who last struck it, held against them for GrudgeTk ticks
	GrudgeID string
	GrudgeTk int

	// momentum: speed carried into the next step, the heading of the last step
	// (steer sets it), and the tick rampSpeed last ran
	CarrySp, CarryAng float64
	CarryAt           int64
	HeadOK            bool

	// stuck recovery: best goal distance in the window, sampled ticks since it
	// last improved, the window's anchor, and the tick last sampled
	StuckBest      float64
	StuckN         int
	StuckX, StuckY float64
	StuckAt        int64
	// the sidestep under way: ticks left, its heading, and which side it took
	// last so the next one tries the other
	UnstickTk   int
	UnstickAng  float64
	UnstickFlip bool
}

// baseSlotFamily: the light melee types that share a keeper's attack slots.
func baseSlotFamily(typ string) bool {
	switch typ {
	case "crawler", "drowned", "stalker", "husk_wolf":
		return true
	}
	return false
}

// baseAngDiff is the absolute angle between two headings, 0..π.
func baseAngDiff(a, b float64) float64 { return math.Abs(math.Remainder(a-b, 2*math.Pi)) }

// baseTimers runs every tick, stunned or not, before anything else.
func (r *Room) baseTimers(c *Creature) {
	if c.GrudgeTk > 0 {
		c.GrudgeTk--
	}
	if c.UnstickTk > 0 {
		c.UnstickTk--
	}
	// a failed detour search older than its retry window means nothing now;
	// clearing it keeps stuck recovery from reading it as a siege forever
	if c.PathFailAt != 0 && c.Path == nil && r.tickN-int64(c.PathFailAt) > aiPathRetry+5 {
		c.PathFailAt = 0
	}
}

// assignSlots runs once per tick, before any creature steps. Per keeper, the
// aiSlotMax nearest light melee hunters within baseSlotRange keep a slot; the
// rest are NoSlot and hold a ring (baseTactics).
func (r *Room) assignSlots() {
	type bid struct {
		c *Creature
		q *Player
		d float64
	}
	var bids []bid
	for _, c := range r.creOrder {
		c.NoSlot = false
		if r.wave != nil || c.AggroID == "" || !c.Hunting || !baseSlotFamily(c.Type) {
			continue
		}
		q := r.players[c.AggroID]
		if q == nil || q.Z != 0 {
			continue
		}
		if d := math.Hypot(q.X-c.X, q.Y-c.Y); d <= baseSlotRange {
			bids = append(bids, bid{c, q, d})
		}
	}
	if len(bids) == 0 {
		return
	}
	// stable: equal distances keep creOrder
	sort.SliceStable(bids, func(i, j int) bool { return bids[i].d < bids[j].d })
	type seat struct {
		q *Player
		n int
	}
	var seats []seat
	for _, b := range bids {
		k := 0
		for k < len(seats) && seats[k].q != b.q {
			k++
		}
		if k == len(seats) {
			seats = append(seats, seat{q: b.q})
		}
		if seats[k].n >= aiSlotMax {
			b.c.NoSlot = true
		} else {
			seats[k].n++
		}
	}
}

// baseOnStruck: a keeper's swing just landed on this creature (it survived).
func (r *Room) baseOnStruck(c *Creature, p *Player) {
	c.GrudgeID, c.GrudgeTk = p.S.ID, baseGrudgeTicks
	c.CarrySp = 0 // knocked back: the momentum is gone
}

// creLOS: nothing that blocks sight stands between two points. The segment is
// sampled every baseLOSStep tiles, skipping the end tiles; walls, blocking
// structures and medic huts block, water and terrain do not.
func (r *Room) creLOS(x0, y0, x1, y1 float64) bool {
	dx, dy := x1-x0, y1-y0
	d := math.Hypot(dx, dy)
	last, end := ti(x0, y0), ti(x1, y1)
	walls := len(r.modules) > 0
	for s := baseLOSStep; s < d; s += baseLOSStep {
		i := ti(x0+dx*s/d, y0+dy*s/d)
		if i == last || i == end {
			continue
		}
		last = i
		if _, ok := r.structures[i]; ok && r.creBlocked(i) {
			return false
		}
		if r.medicTiles[i] || (walls && r.wallBlocks(i)) {
			return false
		}
	}
	return true
}

// creDetects: creature c notices keeper q at distance d. A keeper is noticed in
// plain sight, heard close by or swinging, or smelled carrying essence or meat.
func (r *Room) creDetects(c *Creature, q *Player, d, sight, scent float64, now int64) bool {
	switch {
	case d <= baseHearRadius:
		return true
	case q.LastAtk != 0 && now-q.LastAtk <= baseNoiseMs && d <= baseNoiseRadius:
		return true
	// the legacy scent radius was dead (the search started at `sight`, so it
	// never exceeded it); the scent now genuinely reaches `scent`
	case d < scent && (q.Inv["essence"] > 0 || q.Inv["meat"] > 0):
		return true
	}
	return d < sight && r.creLOS(c.X, c.Y, q.X, q.Y)
}

// selectPrey picks the keeper to hunt and its distance. sight is the normal
// detection radius, scent the radius for a keeper carrying essence or meat.
// Among the keepers it notices it takes the lowest score: distance, less a
// bonus for the one who struck it, the one it already hunts, and the wounded.
// Returns nil (and sight) when nobody is noticed.
func (r *Room) selectPrey(c *Creature, typ string, sight, scent float64) (*Player, float64) {
	var best *Player
	bestD, bestScore := sight, 0.0
	now := r.now()
	for _, q := range r.playerOrder {
		if q.Z != 0 {
			continue
		}
		d := math.Hypot(q.X-c.X, q.Y-c.Y)
		score := d
		if c.GrudgeTk > 0 && q.S.ID == c.GrudgeID {
			score -= 5
		}
		if c.AggroID != "" && q.S.ID == c.AggroID {
			score -= 3 // stickiness: keep the current prey
		}
		if q.HP <= 3 {
			score -= 2
		}
		// a keeper that cannot win the pick is not worth a sight-line test
		if best != nil && score >= bestScore {
			continue
		}
		if r.creDetects(c, q, d, sight, scent, now) {
			best, bestD, bestScore = q, d, score
		}
	}
	return best, bestD
}

// baseTactics bends the aim point (after creature_ai.go creatureTactics) and
// returns a speed multiplier. A light melee hunter denied a slot holds a ring
// round its prey; a creature that was stuck sidesteps first.
func (r *Room) baseTactics(c *Creature, typ string, prey *Player, tx, ty float64) (float64, float64, float64) {
	if c.UnstickTk > 0 {
		return c.X + math.Cos(c.UnstickAng)*3, c.Y + math.Sin(c.UnstickAng)*3, 1
	}
	if !c.NoSlot || c.Retreat > 0 || !baseSlotFamily(typ) {
		return tx, ty, 1
	}
	dx, dy := c.X-prey.X, c.Y-prey.Y
	d := math.Hypot(dx, dy)
	if d > aiRingRadius+3 {
		return tx, ty, 1
	}
	if d < 1e-6 {
		dx, dy, d = 1, 0, 1
	}
	if d < aiRingRadius-0.6 {
		return c.X + dx/d*3, c.Y + dy/d*3, 1 // crowding the keeper: back straight off
	}
	// aim at the ring a little ahead of where it stands, so waiters circle
	a := math.Atan2(dy, dx) + c.Orbit*baseRingStep
	return prey.X + math.Cos(a)*aiRingRadius, prey.Y + math.Sin(a)*aiRingRadius, baseRingSpeed
}

// rampSpeed shapes this tick's step length (sp) from the speed it carries:
// from rest it builds to full in two ticks, slowing is immediate, and a sharp
// turn costs half the carried speed. It never exceeds sp.
func (r *Room) rampSpeed(c *Creature, sp, moveAng float64, mode aiMode) float64 {
	carried := c.CarrySp
	if gap := r.tickN - c.CarryAt; gap > 1 || gap < 0 {
		carried, c.HeadOK = 0, false // it paused (stun, attack): start from rest
	}
	c.CarryAt = r.tickN
	out := sp
	switch {
	case c.HeadOK && carried > 0 && baseAngDiff(moveAng, c.CarryAng) > baseTurnLimit:
		out = math.Min(sp, math.Max(carried*0.5, 0.08))
	case carried < sp:
		out = math.Min(sp, carried+math.Max(sp*0.5, 0.08))
	}
	c.CarrySp = out
	return out
}

// steer takes the step toward moveAng, turning ±35° when the straight step is
// blocked, first to the side it was already sliding. When every candidate is
// blocked it returns moved=false with ni set to the STRAIGHT step's tile: the
// caller gnaws whatever structure stands there, so that must stay the straight
// tile. The fan stays at ±35°: wider slides would never leave a besieged
// creature boxed in for the BFS and gnaw fallback.
func (r *Room) steer(c *Creature, moveAng, sp float64, blocked func(int, float64, float64) bool) (nx, ny float64, ni int, moved bool) {
	nx = c.X + math.Cos(moveAng)*sp
	ny = c.Y + math.Sin(moveAng)*sp
	ni = ti(nx, ny)
	if !blocked(ni, nx, ny) {
		c.CarryAng, c.HeadOK = moveAng, true
		return nx, ny, ni, true
	}
	rot := 35 * math.Pi / 180
	if c.HeadOK && math.Remainder(c.CarryAng-moveAng, 2*math.Pi) < 0 {
		rot = -rot
	}
	for k := 0; k < 2; k++ {
		tryAng := moveAng + rot
		tnx := c.X + math.Cos(tryAng)*sp
		tny := c.Y + math.Sin(tryAng)*sp
		tni := ti(tnx, tny)
		if !blocked(tni, tnx, tny) {
			c.CarryAng, c.HeadOK = tryAng, true
			return tnx, tny, tni, true
		}
		rot = -rot
	}
	return nx, ny, ni, false
}

// trackProgress runs after the creature has moved, with the goal its feet
// were heading for. A hunter or searcher that neither closes on its goal nor
// walks anywhere for baseStuckTicks sampled ticks drops its detour (forcing a
// fresh search) and sidesteps for a few ticks. Ring holders and creatures
// gnawing through a wall are idle on purpose, and the aim points of tactics
// that move with the creature are covered by the walked-distance test.
func (r *Room) trackProgress(c *Creature, gx, gy float64, mode aiMode) {
	if (mode != aiHunt && mode != aiSearch) || c.NoSlot || (c.PathFailAt != 0 && c.Path == nil) {
		return
	}
	d := math.Hypot(gx-c.X, gy-c.Y)
	if d < 1.5 {
		return
	}
	gap := r.tickN - c.StuckAt
	switch {
	case gap > 1 || gap < 0 || c.StuckBest == 0 || c.UnstickTk > 0 ||
		d < c.StuckBest-baseStuckGain || math.Hypot(c.X-c.StuckX, c.Y-c.StuckY) >= baseStuckMove:
		// a fresh window: first sample, after a gap or a sidestep, or progress
		c.StuckBest, c.StuckN, c.StuckX, c.StuckY = d, 0, c.X, c.Y
	default:
		c.StuckN++
		if c.StuckN >= baseStuckTicks {
			c.Path, c.PathFailAt = nil, 0
			ang := math.Atan2(gy-c.Y, gx-c.X) + math.Pi/2*c.Orbit
			if c.UnstickFlip {
				ang += math.Pi
			}
			c.UnstickFlip = !c.UnstickFlip
			c.UnstickAng, c.UnstickTk = ang, baseUnstickTk
			c.StuckBest = 0
		}
	}
	c.StuckAt = r.tickN
}
