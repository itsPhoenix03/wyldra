package room

import (
	"math"
	"math/rand"
)

// The level features of the enemy brain: abilities a creature only has from
// a certain level, and only if its type can learn them (creature_levels.go
// creKit). docs/10 §13.6.

// eliteRoll is the dice behind a dodge; tests swap it to force or forbid one.
var eliteRoll = rand.Float64

const (
	eliteRadius  = 12.0 // a pack call carries, and followers keep to their alpha, this far
	elitePanicTk = 15   // a fallen alpha's followers flee this long
	eliteRallyTk = 25   // an alpha rallies at most this often when struck
	eliteDeg     = math.Pi / 180
)

// creElite is the level layer's per-creature state, embedded in Creature.
type creElite struct {
	// ChargeTk > 0: a charge is under way (its windup, then the dash);
	// eliteOverride owns the creature's tick until it ends.
	ChargeTk int
	// PanicTk > 0: panicking (its alpha fell); it must not attack.
	PanicTk int

	Dashing   bool // the windup is over: ChargeLen tiles of dash left along ChargeAng
	ChargeLen float64
	ChargeAng float64
	// ticks until the creature may charge / dodge / rally again
	ChargeCd, DodgeCd, RallyCd int
	// PanicX, PanicY is where the killer stood: the panicked run from it
	PanicX, PanicY float64
}

// eliteTick runs every tick, stunned or not, before anything else.
func (r *Room) eliteTick(c *Creature, typ string) {
	if c.ChargeCd > 0 {
		c.ChargeCd--
	}
	if c.DodgeCd > 0 {
		c.DodgeCd--
	}
	if c.RallyCd > 0 {
		c.RallyCd--
	}
}

// eliteOverride: a level feature under way (charge, panic) owns this tick.
// Called after the stranded and leash checks, before target selection.
func (r *Room) eliteOverride(c *Creature, typ string, baseSp float64, nowMs int64) bool {
	switch {
	case c.PanicTk > 0:
		r.panicStep(c, typ, baseSp)
	case c.ChargeTk > 0:
		r.chargeStep(c, typ, nowMs)
	default:
		return false
	}
	return true
}

// panicStep runs from the killer at 1.1x speed, only onto open ground.
func (r *Room) panicStep(c *Creature, typ string, baseSp float64) {
	c.PanicTk--
	ang := math.Atan2(c.Y-c.PanicY, c.X-c.PanicX)
	sp := baseSp * 1.1
	for _, rot := range [5]float64{0, 35 * eliteDeg, -35 * eliteDeg, 70 * eliteDeg, -70 * eliteDeg} {
		nx, ny := c.X+math.Cos(ang+rot)*sp, c.Y+math.Sin(ang+rot)*sp
		if !r.creStepBlocked(ti(nx, ny), canSwim[typ]) {
			c.X, c.Y = nx, ny
			return
		}
	}
}

// answersTo: c is the kind of creature that follows alpha a.
func (c *Creature) answersTo(a *Creature) bool {
	if c == a || c.has(abAlpha) {
		return false
	}
	switch c.Type {
	case "crawler": // the brute is a warlord
		return a.Type == "crawler" || a.Type == "brute"
	case "stalker", "husk_wolf":
		return a.Type == c.Type
	}
	return false
}

// alphaOf is the nearest alpha within reach that c follows, or nil.
func (r *Room) alphaOf(c *Creature) *Creature {
	switch c.Type {
	case "crawler", "stalker", "husk_wolf":
	default:
		return nil
	}
	var best *Creature
	bd := eliteRadius * eliteRadius
	for _, a := range r.creOrder {
		if !a.has(abAlpha) || !c.answersTo(a) {
			continue
		}
		if dx, dy := a.X-c.X, a.Y-c.Y; dx*dx+dy*dy <= bd {
			bd, best = dx*dx+dy*dy, a
		}
	}
	return best
}

// elitePrey may swap the base brain's choice of prey (nil = none): a follower
// hunts what its alpha hunts.
func (r *Room) elitePrey(c *Creature, typ string, prey *Player) *Player {
	a := r.alphaOf(c)
	if a == nil || a.AggroID == "" {
		return prey
	}
	q := r.players[a.AggroID]
	if q == nil || q.Z != 0 || math.Hypot(q.X-c.X, q.Y-c.Y) > 26 {
		return prey
	}
	return q
}

// eliteSpeed is a speed multiplier for this tick: a pack runs harder near its alpha.
func (r *Room) eliteSpeed(c *Creature, typ string) float64 {
	if r.alphaOf(c) != nil {
		return 1.1
	}
	return 1
}

// eliteFace is the keeper's unit heading, or ok=false when it has none yet.
func eliteFace(q *Player) (fx, fy float64, ok bool) {
	h := math.Hypot(q.FaceX, q.FaceY)
	if h < 0.01 {
		return 0, 0, false
	}
	return q.FaceX / h, q.FaceY / h, true
}

// eliteTactics bends the aim point last, after creatureTactics and
// baseTactics.
func (r *Room) eliteTactics(c *Creature, typ string, prey *Player, tx, ty float64) (float64, float64) {
	if c.Abil == 0 {
		return tx, ty
	}
	d := math.Hypot(prey.X-c.X, prey.Y-c.Y)
	if c.has(abCharge) && r.startCharge(c, typ, prey, d) {
		return c.X, c.Y // hold still through the windup
	}
	if c.has(abFlank) {
		return r.flankAim(c, typ, prey, d, tx, ty)
	}
	return tx, ty
}

// flankAim sends a creature that is in front of its keeper round to the side
// and back instead of straight in. d is its distance to the keeper.
func (r *Room) flankAim(c *Creature, typ string, q *Player, d, tx, ty float64) (float64, float64) {
	fx, fy, ok := eliteFace(q)
	if !ok || d < 2 || d > 9 || c.Retreat > 0 {
		return tx, ty
	}
	ux, uy := (c.X-q.X)/d, (c.Y-q.Y)/d // from the keeper out to the creature
	front := ux*fx+uy*fy > 0.3
	cross := fx*uy - fy*ux // > 0: on the keeper's left
	side := c.Orbit
	if cross > 0 {
		side = 1
	} else if cross < 0 {
		side = -1
	}
	if typ == "blight_lancer" {
		// strafe toward the keeper's back (hysteresis: not while nearly in line)
		if cross > 0.1 {
			c.Orbit = -1
		} else if cross < -0.1 {
			c.Orbit = 1
		}
		return tx, ty
	}
	var ax, ay float64
	switch {
	case c.NoSlot && front: // a ring waiter goes round, never across the keeper's front
		ax, ay = q.X-fy*side*aiRingRadius, q.Y+fx*side*aiRingRadius
	case c.NoSlot:
		a := 0.35 * c.Orbit
		ax = q.X - (fx*math.Cos(a)-fy*math.Sin(a))*aiRingRadius
		ay = q.Y - (fx*math.Sin(a)+fy*math.Cos(a))*aiRingRadius
	case front && r.plainChase(c, typ, q, d, tx, ty):
		ax, ay = q.X-fy*side*2.2-fx, q.Y+fx*side*2.2-fy
	default:
		return tx, ty
	}
	if r.creStepBlocked(ti(ax, ay), canSwim[typ]) {
		return tx, ty
	}
	return ax, ay
}

// plainChase: the aim (tx, ty) still points at the prey (a straight run or a
// lead), so bending it spoils nothing: not a retreat, a stalker's orbit or a
// wolf's pack spot.
func (r *Room) plainChase(c *Creature, typ string, q *Player, d, tx, ty float64) bool {
	if math.Hypot(tx-q.X, ty-q.Y) > 1.5+4.2*math.Hypot(q.VX, q.VY) || // 4.2: the longest lead
		(tx-c.X)*(q.X-c.X)+(ty-c.Y)*(q.Y-c.Y) <= 0 {
		return false
	}
	if typ == "husk_wolf" {
		_, _, spot := r.wolfFlank(c, q, d)
		return !spot
	}
	return true
}

// startCharge begins a charge at prey q, d tiles off, if one is possible now.
func (r *Room) startCharge(c *Creature, typ string, q *Player, d float64) bool {
	lo, hi := 4.0, 8.0
	if typ == "brute" || typ == "bog_shambler" {
		lo, hi = 3, 7
	}
	if c.ChargeCd > 0 || !c.mayStrike() || c.NoSlot || c.Retreat > 0 || d < lo || d > hi {
		return false
	}
	swims, fear := canSwim[typ], r.fearsFire(c, typ)
	for s := 0.5; s < d; s += 0.5 { // the straight line must be clear
		x, y := c.X+(q.X-c.X)*s/d, c.Y+(q.Y-c.Y)*s/d
		if r.creStepBlocked(ti(x, y), swims) || (fear && r.inFireRing(x, y)) {
			return false
		}
	}
	c.ChargeAng = math.Atan2(q.Y-c.Y, q.X-c.X)
	c.ChargeLen = math.Min(d+1.5, 8)
	c.ChargeTk, c.Dashing = 3, false
	r.broadcast(map[string]any{
		"t": "ccharge", "id": c.ID, "ang": c.ChargeAng, "tk": 3, "len": toFixed(c.ChargeLen, 1),
	})
	return true
}

// chargeStep is one tick of a charge: stand through the windup, then dash.
func (r *Room) chargeStep(c *Creature, typ string, nowMs int64) {
	if !c.Dashing {
		if c.ChargeTk--; c.ChargeTk > 0 {
			return
		}
		c.Dashing, c.ChargeTk = true, 1
	}
	step, wall := 1.4, 2
	if typ == "brute" || typ == "bog_shambler" {
		step, wall = 1.0, 6
	}
	n := int(math.Ceil(step/0.35 - 1e-9))
	sub := step / float64(n)
	swims, cdmg := canSwim[typ], creTypes[typ].dmg+1
	cos, sin := math.Cos(c.ChargeAng), math.Sin(c.ChargeAng)
	for k := 0; k < n && c.ChargeLen > 1e-9; k++ {
		l := math.Min(sub, c.ChargeLen)
		nx, ny := c.X+cos*l, c.Y+sin*l
		ni := ti(nx, ny)
		if r.creStepBlocked(ni, swims) {
			s := r.structures[ni]
			if s != nil && r.creBlocked(ni) {
				r.chargeSmash(ni, s, wall)
			}
			if s != nil || (len(r.modules) > 0 && r.wallBlocks(ni)) {
				c.Stun = 5 // slammed into it: dazed
			}
			r.endCharge(c)
			return
		}
		c.X, c.Y = nx, ny
		c.ChargeLen -= l
		hit := false
		for _, q := range r.playerOrder {
			if q.Z == 0 && math.Hypot(q.X-c.X, q.Y-c.Y) < 1.0 {
				r.hitPlayer(q, c, cdmg, nowMs, false)
				hit = true
			}
		}
		if hit {
			struckRetreat(c, typ)
			r.endCharge(c)
			return
		}
	}
	if c.ChargeLen <= 1e-9 {
		r.endCharge(c)
	}
}

// chargeSmash batters the structure on tile i, as the gnaw in stepCreature does.
func (r *Room) chargeSmash(i int, s *Structure, dmg int) {
	s.HP -= dmg
	if s.HP > 0 {
		r.broadcast(map[string]any{"t": "sd", "i": i, "hp": s.HP})
		return
	}
	r.destroyStructure(i)
	r.broadcast(map[string]any{"t": "sd", "i": i, "hp": 0})
	if r.wave != nil && i == r.wave.engineI {
		r.wave = nil
		r.broadcast(map[string]any{"t": "wave", "secs": 0})
		r.broadcast(map[string]any{"t": "msg", "s": "THE WORLD ENGINE WAS DESTROYED! Rebuild it to try again."})
	}
}

// endCharge ends a charge, however it ended, and starts the cooldown.
func (r *Room) endCharge(c *Creature) {
	c.ChargeTk, c.Dashing, c.ChargeLen = 0, false, 0
	c.ChargeCd = 40
	if c.Type == "brute" || c.Type == "bog_shambler" {
		c.ChargeCd = 50
	}
}

// packCall runs once when a creature first locks on to a keeper
// (creature_ai.go creatureSawPrey): it rouses the idle toward the keeper, and
// an alpha also rallies its followers.
func (r *Room) packCall(c *Creature, prey *Player) {
	call, alpha := c.has(abPackCall), c.has(abAlpha)
	if !call && !alpha {
		return
	}
	for _, o := range r.creOrder {
		if o == c || o.Hunting || o.Type == "wisp" || o.Type == "frost_wraith" {
			continue
		}
		if !call && !o.answersTo(c) { // an alpha without the call rouses only its own
			continue
		}
		if math.Hypot(o.X-c.X, o.Y-c.Y) > eliteRadius {
			continue
		}
		o.LastX, o.LastY = prey.X, prey.Y
		if o.Memory < aiMemoryTicks {
			o.Memory = aiMemoryTicks
		}
	}
	if alpha {
		r.broadcast(map[string]any{"t": "crally", "id": c.ID})
	}
}

// tryDodge runs when keeper p's swing has picked c as its target, before the
// hit resolves. ok=true means c sidestepped (and has already moved): the swing
// misses, and actions.go broadcasts `cdodge` with ang, the hop heading.
func (r *Room) tryDodge(p *Player, c *Creature) (ang float64, ok bool) {
	// a creature committed to a windup or a charge cannot dodge: bait it, then swing
	if !c.has(abDodge) || c.Stun > 0 || c.AtkState != atkReady || c.ChargeTk > 0 || c.DodgeCd > 0 {
		return 0, false
	}
	chance := 0.30
	switch c.Level {
	case 4:
		chance = 0.35
	case 5:
		chance = 0.45
	}
	if eliteRoll() >= chance {
		return 0, false
	}
	ux, uy := 1.0, 0.0 // from the keeper out to the creature
	if d := math.Hypot(c.X-p.X, c.Y-p.Y); d > 1e-6 {
		ux, uy = (c.X-p.X)/d, (c.Y-p.Y)/d
	}
	swims := canSwim[c.Type]
	open := func(dx, dy float64) bool {
		i := ti(c.X+dx, c.Y+dy)
		return !r.creStepBlocked(i, swims) && r.structures[i] == nil
	}
	hx, hy := -uy, ux
	left, right := open(hx, hy), open(-hx, -hy)
	switch {
	case left && right:
		if rand.Intn(2) == 0 {
			hx, hy = -hx, -hy
		}
	case right:
		hx, hy = -hx, -hy
	case left:
	case open(ux, uy): // boxed in on both sides: hop straight back
		hx, hy = ux, uy
	default:
		return 0, false
	}
	c.X, c.Y = c.X+hx, c.Y+hy
	c.DodgeCd = 12
	return math.Atan2(hy, hx), true
}

// eliteOnStruck: keeper p's swing just landed on c and c survived.
func (r *Room) eliteOnStruck(c *Creature, p *Player) {
	if c.ChargeTk > 0 {
		r.endCharge(c) // a blow breaks a charge
	}
	if !c.has(abAlpha) || c.RallyCd > 0 {
		return
	}
	c.RallyCd = eliteRallyTk
	r.broadcast(map[string]any{"t": "crally", "id": c.ID})
	for _, o := range r.creOrder {
		if o.answersTo(c) && math.Hypot(o.X-c.X, o.Y-c.Y) <= eliteRadius {
			o.Enraged = max(o.Enraged, 60)
		}
	}
}

// eliteOnKilled: keeper p just killed c (already removed from the room). An
// alpha's followers panic and run from the killer.
func (r *Room) eliteOnKilled(c *Creature, p *Player) {
	if !c.has(abAlpha) {
		return
	}
	var ids []string
	for _, o := range r.creOrder {
		if !o.answersTo(c) || math.Hypot(o.X-c.X, o.Y-c.Y) > eliteRadius {
			continue
		}
		if o.ChargeTk > 0 {
			r.endCharge(o)
		}
		o.PanicTk, o.PanicX, o.PanicY = elitePanicTk, p.X, p.Y
		ids = append(ids, o.ID)
	}
	if len(ids) > 0 {
		r.broadcast(map[string]any{"t": "cpanic", "ids": ids})
	}
}

// struckRetreat: the skirmishers fall back after landing a blow.
func struckRetreat(c *Creature, typ string) {
	switch typ {
	case "stalker":
		c.Retreat = 7
	case "husk_wolf", "drowned":
		c.Retreat = 4
	case "crawler":
		if c.has(abHitRun) {
			c.Retreat = 3
		}
	}
}
