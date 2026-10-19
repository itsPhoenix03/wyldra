package room

import (
	"math/rand"
	"sort"

	"hearth/gameserver/world"
)

// The land heals. Felling every ecoSourAt-th tree in a 16x16 sector sours some
// of its grass to mud (chopEcosystem, actions.go). Left alone, the sector
// recovers on its own: its felling pressure relaxes, each tree that grows back
// reclaims the soured ground nearest it, and the rest of the mud slowly turns
// back to grass. Nothing grows back through what is built on a tile, nor
// through blighted ground. Healed tiles go out as `regrow` (docs/10 §8.6).

const (
	ecoSourAt     = 8      // fellings in one sector that sour its ground
	ecoPassTicks  = 150    // the healing pass runs every 30 s
	ecoRestMs     = 120000 // a sector rests once nobody has felled a tree there for 2 min
	ecoHealChance = 0.12   // per pass, each soured tile in a rested sector heals at this chance (~4 min)
	ecoRootReach  = 3      // a tree that grows back reclaims soured ground this many tiles away
)

// sectorOf is the 16x16 sector key of a tile.
func sectorOf(x, y int) int { return ((x >> 4) << 8) | (y >> 4) }

// ecoRested: nobody has felled a tree in the sector for ecoRestMs — or since
// the server started, as fellings are not saved.
func (r *Room) ecoRested(sk int, now int64) bool {
	at, ok := r.sectorChopAt[sk]
	return !ok || now-at >= ecoRestMs
}

// ecoCanHeal: soured tile i may turn back to grass — nothing is built on it and
// the blight does not hold it.
func (r *Room) ecoCanHeal(i int) bool {
	return r.mudTiles[i] && !r.tileBuiltOn(i) && !r.isInfected(i)
}

// ecoHeal turns soured tiles back to grass and tells everyone.
func (r *Room) ecoHeal(tiles []int) {
	if len(tiles) == 0 {
		return
	}
	for _, i := range tiles {
		delete(r.mudTiles, i)
	}
	r.broadcast(map[string]any{"t": "regrow", "tiles": tiles})
}

// ecologyTick is the slow healing pass: rested sectors shed a felling's worth
// of pressure, and their soured tiles each have a chance to turn back to grass.
// Tiles are walked in index order so the dice fall the same way every run.
func (r *Room) ecologyTick(now int64) {
	for sk, n := range r.sectorChops {
		if !r.ecoRested(sk, now) {
			continue
		}
		if n <= 1 {
			delete(r.sectorChops, sk)
		} else {
			r.sectorChops[sk] = n - 1
		}
	}
	if len(r.mudTiles) == 0 {
		return
	}
	mud := make([]int, 0, len(r.mudTiles))
	for i := range r.mudTiles {
		mud = append(mud, i)
	}
	sort.Ints(mud)
	var healed []int
	for _, i := range mud {
		if !r.ecoRested(sectorOf(i%world.SIZE, i/world.SIZE), now) || !r.ecoCanHeal(i) {
			continue
		}
		if rand.Float64() < ecoHealChance {
			healed = append(healed, i)
		}
	}
	r.ecoHeal(healed)
}

// ecoTreeRegrew: a felled tree has grown back on tile i. Its sector's pressure
// eases by one felling, and its roots reclaim the nearest soured tile within
// ecoRootReach (ties go to the lower tile index). This happens even in a
// sector that is still being worked: regrowth is what heals the land.
func (r *Room) ecoTreeRegrew(i int) {
	x, y := i%world.SIZE, i/world.SIZE
	sk := sectorOf(x, y)
	if n := r.sectorChops[sk]; n <= 1 {
		delete(r.sectorChops, sk)
	} else {
		r.sectorChops[sk] = n - 1
	}
	if len(r.mudTiles) == 0 {
		return
	}
	best, bestD := -1, 1<<30
	for dy := -ecoRootReach; dy <= ecoRootReach; dy++ {
		for dx := -ecoRootReach; dx <= ecoRootReach; dx++ {
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= world.SIZE || ny >= world.SIZE {
				continue
			}
			j := ny*world.SIZE + nx
			if d := dx*dx + dy*dy; d < bestD && r.ecoCanHeal(j) {
				best, bestD = j, d
			}
		}
	}
	if best >= 0 {
		r.ecoHeal([]int{best})
	}
}
