package room

import (
	"math"

	"hearth/gameserver/world"
)

// The attack layer of the enemy brain: how a creature commits to and lands a
// blow, melee or ranged. docs/10 §13.6.
//
// Every blow is telegraphed. A melee creature that finds a keeper in reach
// commits to a windup along a FIXED heading (`catk`, or `ctel` for the heavies),
// stands still for it, then strikes — a keeper who steps aside is missed — and
// catches its breath before it may commit again. A ranged creature aims at a
// fixed point (`caim`) and fires at it when the aim runs out.

// Attack machine states (Creature.AtkState). atkReady must stay the zero
// value: a fresh creature is ready, and creature_levels.go mayStrike reads it.
const (
	atkReady   uint8 = iota
	atkWindup        // committed to a melee blow: standing still, AtkAng fixed
	atkRecover       // the blow has landed; catching its breath
	atkAim           // aiming a ranged shot at (AimX, AimY)
)

const (
	strikeReach      = 1.1 // a lunge hits every keeper this close to where it lands
	heavyReach       = 2.5 // brute / bog shambler trigger reach, and "still engaged" radius
	heavyFirstWindup = 8   // the long telegraph that opens a heavy's engagement

	lanceAimTk = 3
	boltAimTk  = 2
	lanceReach = 9.0 // tiles the beam runs from the creature through its aim point
	lanceSteps = 18  // structure samples along it, half a tile apart
	lanceHit   = 0.7 // keepers this close to the beam take it
	boltHit    = 0.9 // keepers this close to the bolt's aim point take it
)

// meleeSpec is one type's melee tuning, in 200 ms ticks. A heavy has no lunge:
// slam is the radius around the creature its blow hits, and windup is the
// follow-up windup (the first of an engagement is heavyFirstWindup).
type meleeSpec struct {
	reach, lunge, slam        float64
	windup, recover, cooldown int
}

var meleeSpecs = map[string]meleeSpec{
	"crawler":       {reach: 1.5, lunge: 0.6, windup: 2, recover: 1, cooldown: 3},
	"drowned":       {reach: 1.6, lunge: 0.7, windup: 2, recover: 1, cooldown: 3},
	"stalker":       {reach: 1.8, lunge: 0.9, windup: 2, recover: 1, cooldown: 3},
	"husk_wolf":     {reach: 1.9, lunge: 1.0, windup: 2, recover: 1, cooldown: 3},
	"frost_wraith":  {reach: 1.4, lunge: 0.5, windup: 1, recover: 1, cooldown: 4},
	"blight_lancer": {reach: 1.3, lunge: 0.3, windup: 3, recover: 1, cooldown: 4},
	"brute":         {reach: heavyReach, slam: 1.4, windup: 5, recover: 3, cooldown: 2},
	"bog_shambler":  {reach: heavyReach, slam: 1.3, windup: 5, recover: 3, cooldown: 2},
}

// creAtk is the attack layer's per-creature state, embedded in Creature.
type creAtk struct {
	AtkState uint8
	AtkTk    int     // ticks left in the windup, recovery or aim
	AtkAng   float64 // melee heading, fixed when the windup begins
	AtkCd    int     // ticks before another melee blow may be committed

	// WindupTriggered: a heavy is engaged — a keeper has stayed within reach
	// since its first telegraph — so its next windup is the short one.
	WindupTriggered bool

	// the point a ranged shot was aimed at
	AimX, AimY float64

	// ranged cooldown (lancer beam, brute bolt), counted on fifth ticks
	ShotCd int
}

// attackTimers runs every tick, stunned or not, before anything else.
func (r *Room) attackTimers(c *Creature) {
	if c.AtkCd > 0 {
		c.AtkCd--
	}
	// a heavy whose keepers have all left opens its next engagement long again
	if c.WindupTriggered && c.AtkState == atkReady && r.meleeTarget(c, nil, heavyReach) == nil {
		c.WindupTriggered = false
	}
}

// attackBusy: an attack under way owns this tick (the creature does nothing
// else). Called after the stun check and before every other behaviour,
// including the wisp / frost wraith drift.
func (r *Room) attackBusy(c *Creature, typ string, cdmg int, nowMs int64) bool {
	if c.AtkState == atkReady {
		return false
	}
	if c.PanicTk > 0 { // a panicking creature must not attack: it drops the blow
		c.AtkState = atkReady
		return false
	}
	if c.AtkTk--; c.AtkTk > 0 {
		return true
	}
	switch c.AtkState {
	case atkWindup:
		r.strike(c, typ, cdmg, nowMs)
	case atkRecover:
		c.AtkState, c.AtkCd = atkReady, meleeSpecs[typ].cooldown
	case atkAim:
		c.AtkState = atkReady
		if typ == "blight_lancer" {
			r.fireLance(c, nowMs)
		} else {
			r.fireBolt(c, nowMs)
		}
	}
	return true
}

// attackInterrupted: the creature was just struck and stunned (combat.go
// creatureSurvived). A light creature loses its windup or aim, which rewards
// the keeper who hit first; a heavy has super armor and carries on.
func (r *Room) attackInterrupted(c *Creature, typ string) {
	if (c.AtkState == atkWindup || c.AtkState == atkAim) && meleeSpecs[typ].slam == 0 {
		c.AtkState, c.AtkCd = atkReady, 2
	}
}

// heavyWindup: the brute / bog shambler telegraph now lives in the attack
// machine (meleeTrigger commits it, attackBusy runs it), so it never freezes
// the creature on its own.
func (r *Room) heavyWindup(c *Creature, typ string) bool { return false }

// meleeTarget is the surface keeper a creature would commit a blow to: its prey
// if that keeper is within reach, else the nearest one that is (nil for none).
func (r *Room) meleeTarget(c *Creature, prey *Player, reach float64) *Player {
	if prey != nil && prey.Z == 0 && math.Hypot(prey.X-c.X, prey.Y-c.Y) < reach {
		return prey
	}
	var best *Player
	bd := reach
	for _, q := range r.playerOrder {
		if q.Z != 0 {
			continue
		}
		if d := math.Hypot(q.X-c.X, q.Y-c.Y); d < bd {
			bd, best = d, q
		}
	}
	return best
}

// meleeTrigger runs at the end of a hunter's tick, after it has moved. prey is
// the keeper it is hunting (nil when searching, chasing folk or ambling). It
// only commits to a windup; attackBusy lands the blow.
func (r *Room) meleeTrigger(c *Creature, typ string, prey *Player, cdmg int, nowMs int64) {
	sp, ok := meleeSpecs[typ]
	if !ok || cdmg <= 0 || c.AtkCd > 0 || !c.mayStrike() {
		return
	}
	q := r.meleeTarget(c, prey, sp.reach)
	if q == nil {
		return
	}
	heavy := sp.slam > 0
	tk := sp.windup
	if heavy && !c.WindupTriggered {
		tk = heavyFirstWindup
	}
	tx, ty := q.X, q.Y
	if !heavy && c.has(abLeadShot) { // aim where the keeper is going
		tx, ty = tx+q.VX*float64(tk+1), ty+q.VY*float64(tk+1)
	}
	c.AtkState, c.AtkTk, c.AtkAng = atkWindup, tk, math.Atan2(ty-c.Y, tx-c.X)
	msg := "catk"
	if heavy {
		c.WindupTriggered, msg = true, "ctel"
	}
	r.broadcast(map[string]any{"t": msg, "id": c.ID, "ang": c.AtkAng, "tk": tk})
}

// creatureContact is the frost wraith's melee trigger: its drift branch never
// reaches meleeTrigger, so both its dart and its drift call this instead (a
// wisp has no damage, which makes it a no-op). Its strike carries the slow.
func (r *Room) creatureContact(c *Creature, cdmg int, sendSlow bool, nowMs int64) {
	if cdmg > 0 {
		r.meleeTrigger(c, c.Type, nil, cdmg, nowMs)
	}
}

// strike lands the blow a windup committed to: a light creature lunges along its
// fixed heading, stopping short of anything in the way; a heavy slams where it
// stands. Every surface keeper in reach of that spot is hit.
func (r *Room) strike(c *Creature, typ string, cdmg int, nowMs int64) {
	sp := meleeSpecs[typ]
	rad := sp.slam
	if rad == 0 {
		r.lunge(c, typ, sp.lunge)
		rad = strikeReach
	}
	hit := false
	for _, q := range r.playerOrder {
		if q.Z != 0 || math.Hypot(q.X-c.X, q.Y-c.Y) >= rad {
			continue
		}
		r.hitPlayer(q, c, cdmg, nowMs, typ == "frost_wraith")
		hit = true
	}
	if hit {
		struckRetreat(c, typ)
	}
	c.AtkState, c.AtkTk = atkRecover, sp.recover
}

// lunge moves the creature dist tiles along its attack heading in sub-steps of
// about a quarter tile, stopping before the first tile it cannot enter (or, for
// a fire-shy creature at night, the first step deeper into a campfire's ring).
func (r *Room) lunge(c *Creature, typ string, dist float64) {
	n := int(math.Ceil(dist / 0.25))
	dx, dy := math.Cos(c.AtkAng)*dist/float64(n), math.Sin(c.AtkAng)*dist/float64(n)
	fear := r.fearsFire(c, typ)
	for k := 0; k < n; k++ {
		nx, ny := c.X+dx, c.Y+dy
		if r.creStepBlocked(ti(nx, ny), canSwim[typ]) || (fear && r.fireBlocks(c, nx, ny)) ||
			r.mtnClimbBlocked(ti(c.X, c.Y), ti(nx, ny)) {
			return
		}
		c.X, c.Y = nx, ny
	}
}

// hitPlayer applies creature contact damage with the knockback angle, respawns
// on death, and optionally sends the frost-wraith slow.
func (r *Room) hitPlayer(q *Player, c *Creature, dmg int, nowMs int64, slow bool) {
	q.LastDamageAt = nowMs
	taken := r.creatureDamage(q, dmg)
	q.HP -= taken
	cAng := math.Atan2(q.Y-c.Y, q.X-c.X) // away from the creature = push direction
	if q.HP <= 0 {
		r.respawn(q, false)
	}
	out := map[string]any{"t": "hp", "hp": q.HP, "x": q.X, "y": q.Y, "ang": cAng}
	if taken < dmg {
		out["blocked"] = dmg - taken // the armor turned some or all of it
	}
	r.send(q, out)
	if slow {
		r.send(q, map[string]any{"t": "slow", "ticks": 30})
	}
}

// startAim begins aiming a ranged shot at keeper q: where it stands, or with
// lead shot where it will be when the aim runs out. The creature then stands
// still (attackBusy) and fires at that point, so a keeper who moves is missed.
func (r *Room) startAim(c *Creature, q *Player, kind string, tk int) {
	ax, ay := q.X, q.Y
	if c.has(abLeadShot) {
		ax, ay = ax+q.VX*float64(tk+1), ay+q.VY*float64(tk+1)
	}
	c.AtkState, c.AtkTk, c.AimX, c.AimY = atkAim, tk, ax, ay
	r.broadcast(map[string]any{
		"t": "caim", "id": c.ID, "kind": kind,
		"tx": toFixed(ax, 1), "ty": toFixed(ay, 1), "tk": tk,
	})
}

// lancerBeam is the blight lancer's siege shot: when ready it starts aiming at
// the first keeper in beam range (fireLance lets it off).
func (r *Room) lancerBeam(c *Creature, nowMs int64) {
	if c.ShotCd > 0 {
		c.ShotCd--
	}
	if c.ShotCd != 0 || !c.mayStrike() {
		return
	}
	for _, q := range r.playerOrder {
		d := math.Hypot(q.X-c.X, q.Y-c.Y)
		if q.Z != 0 || d < 1.5 || d > 9 {
			continue
		}
		c.ShotCd = 4 // ~4s between beams
		r.startAim(c, q, "lance", lanceAimTk)
		return
	}
}

// fireLance looses the beam from the creature through its aim point out to
// lanceReach. The first blocking structure on the line eats it and is
// annihilated (the Engine only takes 20); otherwise it hits every surface
// keeper on the line. A keeper beyond a structure is shielded by it.
func (r *Room) fireLance(c *Creature, nowMs int64) {
	ang := math.Atan2(c.AimY-c.Y, c.AimX-c.X)
	ux, uy := math.Cos(ang), math.Sin(ang)
	hitI, length := -1, lanceReach // the beam's length up to what stopped it
	for st := 1; st <= lanceSteps; st++ {
		s := lanceReach * float64(st) / lanceSteps
		px, py := c.X+ux*s, c.Y+uy*s
		if px < 0 || py < 0 || px >= world.SIZE || py >= world.SIZE {
			break
		}
		li := ti(px, py)
		ls, ok := r.structures[li]
		if ok && !r.defs.DecorNonBlk[ls.Kind] && ls.Kind != "farmplot" {
			hitI, length = li, s-lanceReach/lanceSteps
			break
		}
	}
	bx, by := c.AimX, c.AimY
	if hitI >= 0 {
		bx, by = float64(hitI%world.SIZE), float64(hitI/world.SIZE)
	}
	r.broadcast(map[string]any{
		"t": "shot", "kind": "lance",
		"fx": toFixed(c.X, 1), "fy": toFixed(c.Y, 1),
		"tx": toFixed(bx, 1), "ty": toFixed(by, 1),
	})
	if hitI >= 0 {
		ls := r.structures[hitI]
		if ls.Kind == "engine" { // never one-shot the finale objective
			ls.HP -= 20
			if ls.HP <= 0 {
				r.destroyStructure(hitI)
				r.broadcast(map[string]any{"t": "sd", "i": hitI, "hp": 0})
				if r.wave != nil && hitI == r.wave.engineI {
					r.wave = nil
					r.broadcast(map[string]any{"t": "wave", "secs": 0})
					r.broadcast(map[string]any{"t": "msg", "s": "THE WORLD ENGINE WAS DESTROYED! Rebuild it to try again."})
				}
			} else {
				r.broadcast(map[string]any{"t": "sd", "i": hitI, "hp": ls.HP})
			}
		} else {
			r.destroyStructure(hitI)
			r.broadcast(map[string]any{"t": "sd", "i": hitI, "hp": 0})
			r.broadcast(map[string]any{"t": "msg", "s": "⚡ A Blight Lancer's beam vaporised a structure!"})
		}
	}
	for _, q := range r.playerOrder {
		if q.Z != 0 {
			continue
		}
		along := math.Max(0, math.Min(length, (q.X-c.X)*ux+(q.Y-c.Y)*uy))
		if math.Hypot(q.X-(c.X+ux*along), q.Y-(c.Y+uy*along)) <= lanceHit {
			r.hitPlayer(q, c, 2, nowMs, false)
		}
	}
}

// bruteBolt is the brute's ranged answer to players hiding in water it cannot
// enter: when ready it starts aiming at the first such keeper (fireBolt lets it
// off).
func (r *Room) bruteBolt(c *Creature, nowMs int64) {
	if c.ShotCd > 0 {
		c.ShotCd--
	}
	if c.ShotCd != 0 || !c.mayStrike() {
		return
	}
	for _, q := range r.playerOrder {
		d := math.Hypot(q.X-c.X, q.Y-c.Y)
		// a player on a bridge is within reach on foot: no bolt
		if q.Z == 0 && d > 1.5 && d < 7 && !r.creFooting(ti(q.X, q.Y)) {
			c.ShotCd = 3 // one bolt every ~3s
			r.startAim(c, q, "bolt", boltAimTk)
			return
		}
	}
}

// fireBolt looses the bolt at the aim point; any surface keeper still there is
// hit.
func (r *Room) fireBolt(c *Creature, nowMs int64) {
	r.broadcast(map[string]any{
		"t":  "shot",
		"fx": toFixed(c.X, 1), "fy": toFixed(c.Y, 1),
		"tx": toFixed(c.AimX, 1), "ty": toFixed(c.AimY, 1),
	})
	for _, q := range r.playerOrder {
		if q.Z == 0 && math.Hypot(q.X-c.AimX, q.Y-c.AimY) < boltHit {
			r.hitPlayer(q, c, 1, nowMs, false)
		}
	}
}
