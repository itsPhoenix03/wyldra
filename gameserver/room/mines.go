package room

import (
	"sort"

	"hearth/gameserver/world"
)

// Mines are not owned. The underground is one shared grid of carved tiles
// (r.digs), and a mine is simply a connected run of them: tunnels that touch
// edge-on are one mine, whoever dug them. mineParent groups the carved tiles
// with a union-find so the room can tell when a new entrance opens into a mine
// that is already there, and when a dig breaks through and joins two mines.
//
// Carved tiles are never filled back in, so mines only ever merge and never
// have to split.

// orthoNeighbours are the four edge-on steps. Diagonal tiles do not connect:
// a keeper cannot squeeze between two corners of rock.
var orthoNeighbours = [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}}

// mineRoot returns the representative tile of i's mine. A carved tile the
// union-find has never seen (tests write r.digs directly) is its own root.
func (r *Room) mineRoot(i int) int {
	for {
		p, ok := r.mineParent[i]
		if !ok || p == i {
			return i
		}
		// path halving keeps the chains short without recursion
		if gp, ok := r.mineParent[p]; ok {
			r.mineParent[i] = gp
		}
		i = p
	}
}

// carve digs tile i out and links it into the mine of every carved tile it
// touches edge-on. It returns how many separate mines the tile touched: 0
// starts a new pocket, 1 extends a mine, 2 or more means it just joined them
// into one. Every write to r.digs goes through here.
func (r *Room) carve(i int) int {
	if r.digs[i] {
		return 0
	}
	r.digs[i] = true
	r.mineParent[i] = i
	x, y := i%world.SIZE, i/world.SIZE
	touched := 0
	for _, d := range orthoNeighbours {
		nx, ny := x+d[0], y+d[1]
		if nx < 0 || ny < 0 || nx >= world.SIZE || ny >= world.SIZE {
			continue
		}
		ni := ny*world.SIZE + nx
		if !r.digs[ni] {
			continue
		}
		a, b := r.mineRoot(i), r.mineRoot(ni)
		if a == b {
			continue
		}
		r.mineParent[b] = a
		touched++
	}
	return touched
}

// openShaft carves what a new mine entrance at (x, y) needs, and nothing more:
//
//   - Over an existing tunnel: nothing. The keeper climbs straight down into
//     the mine that is already there.
//   - Beside one (anything carved in the 3x3 around it): only the shaft tile,
//     plus one bridging tile when the mine touches only at a corner, so the
//     entrance opens into that mine without scraping a chamber out of it.
//   - On fresh rock: the 3x3 starting chamber.
//
// It returns the tiles it opened, sorted, and how many mines that already
// existed the entrance now leads into: 0 for a fresh mine, 1 for an existing
// one, 2 or more when opening it joined them into one.
func (r *Room) openShaft(x, y int) (opened []int, mines int) {
	at := func(dx, dy int) (int, bool) {
		nx, ny := x+dx, y+dy
		if nx < 0 || ny < 0 || nx >= world.SIZE || ny >= world.SIZE {
			return 0, false
		}
		return ny*world.SIZE + nx, true
	}
	shaft := y*world.SIZE + x
	if r.digs[shaft] {
		return nil, 1
	}
	// Every mine the carving could reach: the 3x3 plus the ring touching it.
	var before []int
	near := false
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			if di, ok := at(dx, dy); ok && r.digs[di] {
				before = append(before, r.mineRoot(di))
				if dx >= -1 && dx <= 1 && dy >= -1 && dy <= 1 {
					near = true
				}
			}
		}
	}
	dig := func(di int) {
		r.carve(di)
		opened = append(opened, di)
	}
	switch {
	case !near:
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if di, ok := at(dx, dy); ok && r.diggable(di) && !r.digs[di] {
					dig(di)
				}
			}
		}
	case r.carve(shaft) > 0:
		opened = append(opened, shaft) // the mine touches the shaft edge-on
	default:
		opened = append(opened, shaft)
		// Only a corner touches. Carve the edge-on tile between the shaft and
		// that corner so the way in is walkable.
	corners:
		for _, d := range [4][2]int{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
			if ci, ok := at(d[0], d[1]); !ok || !r.digs[ci] {
				continue
			}
			for _, b := range [2][2]int{{d[0], 0}, {0, d[1]}} {
				if bi, ok := at(b[0], b[1]); ok && r.diggable(bi) {
					dig(bi)
					break corners
				}
			}
		}
	}
	root := r.mineRoot(shaft)
	seen := map[int]bool{}
	for _, b := range before {
		if !seen[b] {
			seen[b] = true
			if r.mineRoot(b) == root {
				mines++
			}
		}
	}
	sort.Ints(opened)
	return opened, mines
}

// shaftTorchOrder is where a new entrance's torch goes, first carved tile wins:
// the tiles behind the ladder on screen first, so the torch never hides it.
var shaftTorchOrder = [8][2]int{{0, -1}, {-1, 0}, {-1, -1}, {1, 0}, {0, 1}, {1, -1}, {-1, 1}, {1, 1}}

// lightShaft sets a torch beside the ladder of a new entrance at (x, y), so the
// light down there has a source. A mine that is already lit around the shaft
// gets none. It returns the torch's tile, or -1.
func (r *Room) lightShaft(x, y int) int {
	at := func(dx, dy int) (int, bool) {
		nx, ny := x+dx, y+dy
		if nx < 0 || ny < 0 || nx >= world.SIZE || ny >= world.SIZE {
			return 0, false
		}
		return ny*world.SIZE + nx, true
	}
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			if t, ok := at(dx, dy); ok && r.torches[t] {
				return -1
			}
		}
	}
	spot := y*world.SIZE + x // the shaft itself if nothing beside it is carved
	for _, d := range shaftTorchOrder {
		if t, ok := at(d[0], d[1]); ok && r.digs[t] {
			spot = t
			break
		}
	}
	if !r.digs[spot] {
		return -1
	}
	r.torches[spot] = true
	return spot
}

// lightShafts gives every standing entrance its torch: entrances built before
// they came with one, or whose torch spot was dug out later, get it on load.
func (r *Room) lightShafts() {
	for i, s := range r.structures {
		if s.Kind == "mineshaft" {
			r.lightShaft(i%world.SIZE, i/world.SIZE)
		}
	}
}
