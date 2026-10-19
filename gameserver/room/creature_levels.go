package room

import "math/rand"

// Enemy levels: every creature carries a level from 1 to 5. Level 1 is the
// base combat brain every enemy has (creature_base.go: perception, target
// choice, attack slots, steering; creature_attack.go: telegraphed melee and
// ranged attacks). Higher levels unlock extra abilities (creature_elite.go),
// and which abilities a type can ever learn is part of its character: a wisp
// will never charge and a brute will never dodge. docs/10 §13.6.
//
// A natural spawn rolls its level from the type's tier, the world's progress
// (lit monoliths) and a veteran roll; the final assault adds one. Dev spawns
// and test fixtures get level 1 unless asked otherwise, so they stay
// deterministic.

// creAbility is one learnable ability, used as a bit in Creature.Abil.
type creAbility uint16

const (
	abFlank    creAbility = 1 << iota // approach from the prey's side or back
	abPackCall                        // on first sighting, rouse allies toward the prey
	abDodge                           // sidestep a keeper's swing
	abHitRun                          // fall back after landing a blow
	abCharge                          // telegraphed long dash attack
	abLeadShot                        // ranged: aim where the prey is going
	abAlpha                           // leads a pack: focus, aura, rally, panic on death
)

const (
	maxCreLevel = 5
	maxAlphas   = 2 // alive at once, room-wide

	// Attack slots (creature_base.go): at most aiSlotMax light melee hunters
	// close in on one keeper at a time; the rest hold a ring this far out
	// until a slot frees up.
	aiSlotMax    = 3
	aiRingRadius = 3.2
)

// creTier is each type's base level before progress and the veteran roll.
var creTier = map[string]int{
	"crawler": 1, "drowned": 1, "wisp": 1,
	"stalker": 2, "husk_wolf": 2, "frost_wraith": 2,
	"bog_shambler": 3, "brute": 3, "blight_lancer": 3,
}

// creKit is the level at which each type learns each ability. An ability a
// type is not listed for is never learned.
var creKit = map[string]map[creAbility]int{
	"crawler":       {abFlank: 2, abPackCall: 2, abDodge: 3, abHitRun: 3, abAlpha: 5},
	"drowned":       {abFlank: 2, abPackCall: 2, abDodge: 3, abHitRun: 3, abCharge: 4},
	"stalker":       {abFlank: 2, abDodge: 3, abHitRun: 3, abCharge: 4, abAlpha: 5},
	"husk_wolf":     {abFlank: 2, abPackCall: 2, abDodge: 3, abHitRun: 3, abCharge: 4, abAlpha: 5},
	"frost_wraith":  {abDodge: 3, abLeadShot: 4},
	"wisp":          {abDodge: 3},
	"bog_shambler":  {abCharge: 4},
	"brute":         {abCharge: 4, abLeadShot: 4, abAlpha: 5},
	"blight_lancer": {abFlank: 3, abLeadShot: 4},
}

// abilitiesFor is the ability set a creature of this type has at this level.
func abilitiesFor(typ string, lvl int) creAbility {
	var a creAbility
	for ab, at := range creKit[typ] {
		if lvl >= at {
			a |= ab
		}
	}
	return a
}

// has reports whether the creature has learned an ability.
func (c *Creature) has(a creAbility) bool { return c.Abil&a != 0 }

// setLevel fixes a creature's level and the abilities that come with it. It
// does not touch hp: rollLevel's caller adds the veteran bonus at spawn.
func (c *Creature) setLevel(lvl int) {
	lvl = max(1, min(maxCreLevel, lvl))
	c.Level = lvl
	c.Abil = abilitiesFor(c.Type, lvl)
}

// alphaCount is how many alphas are alive.
func (r *Room) alphaCount() int {
	n := 0
	for _, c := range r.creOrder {
		if c.has(abAlpha) {
			n++
		}
	}
	return n
}

// rollLevel picks a natural spawn's level: tier, +1 per two lit monoliths,
// a veteran roll (+1 at 16%, +2 at 4%), +1 during the final assault. A type
// that would become an alpha while maxAlphas are already alive stays at 4.
func (r *Room) rollLevel(typ string, strength int) int {
	lvl := creTier[typ]
	if lvl == 0 {
		lvl = 1
	}
	lvl += (strength - 1) / 2
	switch v := rand.Float64(); {
	case v < 0.04:
		lvl += 2
	case v < 0.20:
		lvl++
	}
	if r.wave != nil {
		lvl++
	}
	lvl = max(1, min(maxCreLevel, lvl))
	if at, ok := creKit[typ][abAlpha]; ok && lvl >= at && r.alphaCount() >= maxAlphas {
		lvl = at - 1
	}
	return lvl
}

// levelUp applies a rolled level to a freshly spawned creature: the abilities,
// +1 hp per level above the type's tier, and half as much again for an alpha.
func (r *Room) levelUp(c *Creature, lvl int) {
	c.setLevel(lvl)
	if bonus := c.Level - creTier[c.Type]; bonus > 0 {
		c.HP += bonus
	}
	if c.has(abAlpha) {
		c.HP += c.HP / 2
	}
	c.MaxHP = c.HP
}

// mayStrike: the creature is free to begin a new attack or charge this tick —
// not mid-attack, mid-charge or panicking. Both creature_attack.go and
// creature_elite.go must check it before committing to anything.
func (c *Creature) mayStrike() bool {
	return c.AtkState == atkReady && c.ChargeTk == 0 && c.PanicTk == 0
}
