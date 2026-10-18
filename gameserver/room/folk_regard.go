package room

import (
	"fmt"
	"math"

	"hearth/gameserver/world"
)

// Village alarm and village regard (docs/10_GO_WIRE_PROTOCOL.md §13.4).
//
// Alarm: folk used to react only to a creature within folkThreatR of
// themselves, so with one at the edge of the village half of it ran and the
// other half sat on by the fire. Now a creature within folkAlarmR of a
// village's fire sends the whole village to its huts, and they stay in until
// nothing is within folkAlarmClearR of the fire.
//
// Regard: each island's folk keep a tally of the harm each keeper has done to
// their land — trees felled (more when the felling blights the ground, see
// chopEcosystem), and game killed. It fades with time. At harmWary the folk
// stop greeting that keeper and scold instead; at harmHostile the villagers
// and children run for their huts at the sight of them, while the watchers
// (elders, hunters) stand their ground and scold. The keeper is told whenever
// their standing with an island changes.

const (
	folkAlarmR      = 14.0 // a creature this close to the fire alarms the whole village
	folkAlarmClearR = 18.0 // and the village stays in until none is within this
	folkScoldR      = 6.0  // a keeper the folk resent, this close, is scolded (or fled)
	folkWitnessR    = 12.0 // folk who see the harm done turn on the keeper there and then
	folkWitnessTk   = 12   // ticks they spend telling them off

	harmTree    = 1.0 // a tree felled on their island
	harmBlight  = 4.0 // ...that turned their ground to mud
	harmAnimal  = 2.0 // an animal of their island killed
	harmWary    = 6.0
	harmHostile = 14.0
	harmMax     = 30.0
	harmFadeTk  = 100 // every 20s...
	harmFade    = 1.0 // ...this much is forgiven
)

type standing int

const (
	standWelcome standing = iota
	standWary
	standHostile
)

func standingOf(h float64) standing {
	switch {
	case h >= harmHostile:
		return standHostile
	case h >= harmWary:
		return standWary
	}
	return standWelcome
}

// islandAt is the major island (x, y) lies on or just off, or "".
func islandAt(x, y float64) string {
	for _, mi := range world.MajorIslands {
		if math.Hypot(x-float64(mi.Center[0]), y-float64(mi.Center[1])) < world.ISLE_R+20 {
			return mi.ID
		}
	}
	return ""
}

// villageOf is the village on island id, or nil (Core and the minor isles have none).
func (r *Room) villageOf(id string) *village {
	for _, v := range r.villages {
		if v.isle == id {
			return v
		}
	}
	return nil
}

// standingWith is how the folk of island id regard keeper p.
func (r *Room) standingWith(p *Player, id string) standing {
	return standingOf(r.harm[p.S.UserID][id])
}

var standingNews = map[standing]string{
	standWelcome: "The folk of the %s have stopped muttering about you. Their fire is open to you again.",
	standWary:    "The folk of the %s watch you warily now — felled trees and slain game do not go unnoticed.",
	standHostile: "The folk of the %s want you gone. Their children run from you; their elders will not.",
}

var islandNames = map[string]string{"woods": "Woods", "dunes": "Dunes", "spire": "Spire", "marsh": "Marsh"}

// addHarm records harm keeper p did to the land at (x, y): the island's tally
// rises, folk in sight of it turn on them, and a change of standing is told.
func (r *Room) addHarm(p *Player, x, y, amt float64) {
	id := islandAt(x, y)
	v := r.villageOf(id)
	if v == nil {
		return
	}
	m := r.harm[p.S.UserID]
	if m == nil {
		m = map[string]float64{}
		r.harm[p.S.UserID] = m
	}
	before := standingOf(m[id])
	m[id] = math.Min(harmMax, m[id]+amt)
	if after := standingOf(m[id]); after != before {
		r.send(p, map[string]any{"t": "msg", "s": fmt.Sprintf(standingNews[after], islandNames[id])})
	}
	for _, f := range v.members {
		if f.state == fsHidden || f.state == fsFlee || math.Hypot(f.X-x, f.Y-y) > folkWitnessR {
			continue
		}
		f.scoldTk, f.scoldAt = folkWitnessTk, p
	}
}

// harmFadeTick forgives a little of every keeper's harm, and tells a keeper
// who is online when their standing improves.
func (r *Room) harmFadeTick() {
	if r.tickN%harmFadeTk != 0 {
		return
	}
	for uid, m := range r.harm {
		for id, h := range m {
			before := standingOf(h)
			h -= harmFade
			if h <= 0 {
				delete(m, id)
				h = 0
			} else {
				m[id] = h
			}
			if after := standingOf(h); after != before {
				for _, q := range r.playerOrder {
					if q.S.UserID == uid {
						r.send(q, map[string]any{"t": "msg", "s": fmt.Sprintf(standingNews[after], islandNames[id])})
					}
				}
			}
		}
		if len(m) == 0 {
			delete(r.harm, uid)
		}
	}
}

// creatureNear: any creature within rad of (x, y).
func (r *Room) creatureNear(x, y, rad float64) bool {
	for _, c := range r.creOrder {
		if math.Hypot(c.X-x, c.Y-y) < rad {
			return true
		}
	}
	return false
}

// villageAlarmed: a creature is close enough to the fire of f's village to send
// everyone in (rad folkAlarmR), or to keep them in (folkAlarmClearR).
func (r *Room) villageAlarmed(f *Folk, rad float64) bool {
	return f.vil != nil && r.creatureNear(f.vil.fx, f.vil.fy, rad)
}

// resentedNear is the nearest surface keeper within rad of f whose standing
// with f's island is at least min, or nil.
func (r *Room) resentedNear(f *Folk, rad float64, min standing) *Player {
	if f.vil == nil {
		return nil
	}
	var best *Player
	for _, q := range r.playerOrder {
		if q.Z != 0 || r.standingWith(q, f.vil.isle) < min {
			continue
		}
		if d := math.Hypot(q.X-f.X, q.Y-f.Y); d < rad {
			rad, best = d, q
		}
	}
	return best
}

// shunning: f is one who hides from a hostile keeper (watchers face them instead).
func (r *Room) shunning(f *Folk, rad float64) bool {
	return !f.watcher && r.resentedNear(f, rad, standHostile) != nil
}

// scold turns f to face keeper q and tell them off.
func (f *Folk) scold(q *Player) {
	f.Act, f.Face = "scold", math.Atan2(q.Y-f.Y, q.X-f.X)
}
