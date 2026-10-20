package room

import (
	"math"
	"sort"

	"hearth/gameserver/world"
)

// Folk navigation (folk.go runs it): how the island folk get about their village.
//
// Each village keeps a grid of the ground round its fire (navN tiles a side)
// that says where a body can stand and how high it stands: open land, or water
// a keeper has bridged — not a hut, the fire, a landmark, a standing tree or
// rock, or a keeper's wall or blocking build. One tile to the next climbs or
// drops at most one level, so the folk take the ramp and never the cliff, and
// a diagonal never cuts the corner of anything solid. Routes are A* over that
// grid (capped, and cached by start and goal tile), pulled taut into straight
// legs a body a shoulder wide could walk, and followed a fraction of a tile a
// tick. On the way they give each other and the keepers room: they keep their
// distance, pass on the right, fall in behind someone going their way, wait a
// moment for someone coming through a gap, go round anyone standing in the
// road, and plan afresh when a keeper builds across it.
//
// The grid's fixed part (terrain, nodes, huts) is read once; the part keepers
// change (builds, walls, bridges, felled trees) is laid over it again whenever
// the count of any of them changes, or every navRefreshTk ticks in case two
// changes cancelled out. Regions (comp) are flood-filled at the same time, so
// "can I get there at all" is one comparison and A* never searches for a goal
// it cannot reach.

const (
	navR         = 28          // a village's grid reaches this far from its fire
	navN         = 2*navR + 1  // so many tiles a side
	navMaxExpand = 2600        // A* gives up after expanding this many tiles
	navBudget    = 4           // routes a village may plan in one tick; the rest wait a tick
	navCacheN    = 160         // tile paths remembered per village
	navRefreshTk = 150         // ticks between overlay rebuilds even when nothing seems to change
	navShoulder  = 0.22        // half a body's width, for the taut-leg check
	folkBodyR    = 0.3         // the room a body takes up among others
)

// cell kinds of the fixed layer
const (
	cellOpen  uint8 = iota // land
	cellBush               // land, slow going through the bush
	cellWater              // closed, unless bridged
	cellNode               // a standing tree or rock: closed until it is felled
	cellSolid              // a hut, the fire, a medic's hut, a landmark, off the world
)

type navPt struct{ x, y float64 }

// navGrid is one village's ground.
type navGrid struct {
	x0, y0 int     // world tile of cell 0
	base   []uint8 // the fixed layer (cell kinds above)
	elev   []uint8
	cost   []uint8 // extra cost of standing there (bush, behind a hut)
	open   []bool  // fixed layer + what keepers have changed
	comp   []int32 // region (-1: closed)
	gen    int     // bumped by every rebuild (routes planned on an older grid are re-checked)
	sig    [3]int  // the counts the overlay was last built from
	at     int64   // tick of the last rebuild
	built  map[int]bool // keepers' builds inside the grid at the last rebuild
	primed bool         // the first rebuild only notes what was already there

	// A* scratch, reused
	g     []int32
	from  []int32
	seen  []uint32 // == cur: reached this search
	shut  []uint32 // == cur: expanded this search
	cur   uint32
	heap  []navNode

	cache map[[2]int32][]int32
	ring  [][2]int32 // cache keys in the order they went in
}

type navNode struct{ f, c int32 }

func (g *navGrid) cellAt(x, y float64) int32 {
	if x < 0 || y < 0 {
		return -1
	}
	cx, cy := int(x)-g.x0, int(y)-g.y0
	if cx < 0 || cy < 0 || cx >= navN || cy >= navN {
		return -1
	}
	return int32(cy*navN + cx)
}

func (g *navGrid) cellOf(i int) int32 {
	cx, cy := i%world.SIZE-g.x0, i/world.SIZE-g.y0
	if cx < 0 || cy < 0 || cx >= navN || cy >= navN {
		return -1
	}
	return int32(cy*navN + cx)
}

// centre is the middle of cell c in world space.
func (g *navGrid) centre(c int32) navPt {
	return navPt{float64(g.x0+int(c)%navN) + 0.5, float64(g.y0+int(c)/navN) + 0.5}
}

// standable: open ground at (x, y).
func (g *navGrid) standable(x, y float64) bool {
	c := g.cellAt(x, y)
	return c >= 0 && g.open[c]
}

// reach: (x, y) is open ground in region home.
func (g *navGrid) reach(x, y float64, home int32) bool {
	c := g.cellAt(x, y)
	return c >= 0 && g.open[c] && g.comp[c] == home && home >= 0
}

// newNavGrid reads the fixed layer round (fx, fy) once the camps are in place.
func (r *Room) newNavGrid(fx, fy int) *navGrid {
	const n = navN * navN
	g := &navGrid{x0: fx - navR, y0: fy - navR,
		base: make([]uint8, n), elev: make([]uint8, n), cost: make([]uint8, n),
		open: make([]bool, n), comp: make([]int32, n),
		g: make([]int32, n), from: make([]int32, n), seen: make([]uint32, n), shut: make([]uint32, n),
		cache: map[[2]int32][]int32{},
	}
	for c := range n {
		x, y := g.x0+c%navN, g.y0+c/navN
		if x < 0 || y < 0 || x >= world.SIZE || y >= world.SIZE {
			g.base[c] = cellSolid
			continue
		}
		i := y*world.SIZE + x
		g.elev[c] = r.world.Elev[i]
		switch k, node := r.world.Nodes[i]; {
		case r.medicTiles[i] || world.LandmarkBlock[i]:
			g.base[c] = cellSolid
		case r.world.Tiles[i] == world.TWater:
			g.base[c] = cellWater
		case node && k == world.NodeBush:
			g.base[c], g.cost[c] = cellBush, 6
		case node:
			g.base[c] = cellNode
		}
	}
	return g
}

// navRefresh lays what the keepers have changed over the fixed layer when it
// looks different from last time, and returns the tiles of any new builds
// inside the grid (in tile order) for the folk to come and gawp at.
func (r *Room) navRefresh(g *navGrid, force bool) []int {
	sig := [3]int{len(r.structures), len(r.modOrder), len(r.removed)}
	if !force && sig == g.sig && r.tickN-g.at < navRefreshTk {
		return nil
	}
	g.sig, g.at = sig, r.tickN
	for c, b := range g.base {
		g.open[c] = b == cellOpen || b == cellBush
	}
	for i := range r.removed { // felled: the stump is walked over
		if c := g.cellOf(i); c >= 0 && g.base[c] == cellNode {
			g.open[c] = true
		}
	}
	built := make(map[int]bool, len(g.built))
	for _, k := range r.modOrder {
		m := r.modules[k]
		c := g.cellOf(m.I)
		if c < 0 {
			continue
		}
		def, known := r.defs.Modules[m.Kind]
		if m.Slot == "floor" && known && def.Water && g.base[c] == cellWater {
			g.open[c] = true
		}
	}
	for _, k := range r.modOrder { // second pass: a wall on a bridge still stops them
		m := r.modules[k]
		c := g.cellOf(m.I)
		if c < 0 {
			continue
		}
		built[m.I] = true
		if def, known := r.defs.Modules[m.Kind]; m.Slot == "wall" && known && def.Blocks {
			g.open[c] = false
		}
	}
	for i, s := range r.structures {
		c := g.cellOf(i)
		if c < 0 {
			continue
		}
		built[i] = true
		if !r.defs.DecorNonBlk[s.Kind] && s.Kind != "farmplot" {
			g.open[c] = false
		}
	}
	var fresh []int
	if g.primed {
		for i := range built {
			if !g.built[i] {
				fresh = append(fresh, i)
			}
		}
		sort.Ints(fresh)
	}
	g.built, g.primed = built, true
	g.label()
	g.gen++
	clear(g.cache)
	g.ring = g.ring[:0]
	return fresh
}

// step: a body can go from cell a to its side-neighbour b (a itself may be
// closed: someone standing where a keeper just built steps off it).
func (g *navGrid) step(a, b int32) bool {
	if !g.open[b] {
		return false
	}
	d := int(g.elev[a]) - int(g.elev[b])
	return d <= 1 && d >= -1
}

// adj: a body can go from cell a to cell b, which touches it side or corner on.
func (g *navGrid) adj(a, b int32) bool {
	ax, ay, bx, by := a%navN, a/navN, b%navN, b/navN
	dx, dy := bx-ax, by-ay
	switch {
	case dx == 0 && dy == 0:
		return true
	case dx < -1 || dx > 1 || dy < -1 || dy > 1:
		return false
	case dx == 0 || dy == 0:
		return g.step(a, b)
	}
	o1, o2 := ay*navN+bx, by*navN+ax // the two cells the corner passes between
	return g.step(a, o1) && g.step(o1, b) && g.step(a, o2) && g.step(o2, b)
}

// label flood-fills the regions: side-on neighbours a step apart in height.
func (g *navGrid) label() {
	for c := range g.comp {
		g.comp[c] = -1
	}
	var q []int32
	id := int32(0)
	for s := range g.comp {
		if !g.open[s] || g.comp[s] >= 0 {
			continue
		}
		g.comp[s] = id
		q = append(q[:0], int32(s))
		for len(q) > 0 {
			c := q[len(q)-1]
			q = q[:len(q)-1]
			x, y := c%navN, c/navN
			for _, d := range [4][2]int32{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				nx, ny := x+d[0], y+d[1]
				if nx < 0 || ny < 0 || nx >= navN || ny >= navN {
					continue
				}
				nc := ny*navN + nx
				if g.comp[nc] < 0 && g.step(c, nc) {
					g.comp[nc] = id
					q = append(q, nc)
				}
			}
		}
		id++
	}
}

// nearestOpen is the open cell of region home nearest c within rad tiles (c if it is one), or -1.
func (g *navGrid) nearestOpen(c int32, home int32, rad int) int32 {
	if c < 0 {
		return -1
	}
	if g.open[c] && (home < 0 || g.comp[c] == home) {
		return c
	}
	x, y := c%navN, c/navN
	best, bd := int32(-1), int32(1<<30)
	for dy := -int32(rad); dy <= int32(rad); dy++ {
		for dx := -int32(rad); dx <= int32(rad); dx++ {
			nx, ny := x+dx, y+dy
			if nx < 0 || ny < 0 || nx >= navN || ny >= navN {
				continue
			}
			nc := ny*navN + nx
			if d := dx*dx + dy*dy; d < bd && g.open[nc] && (home < 0 || g.comp[nc] == home) {
				best, bd = nc, d
			}
		}
	}
	return best
}

func octile(a, b int32) int32 {
	dx, dy := a%navN-b%navN, a/navN-b/navN
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	if dx < dy {
		dx, dy = dy, dx
	}
	return 10*dx + 4*dy
}

var navDirs = [8][3]int32{{1, 0, 10}, {-1, 0, 10}, {0, 1, 10}, {0, -1, 10}, {1, 1, 14}, {1, -1, 14}, {-1, 1, 14}, {-1, -1, 14}}

// find is the tile path from s to t (both ends included), or nil. avoid are
// cells to keep off this once (a keeper standing in the road).
func (g *navGrid) find(s, t int32, avoid []int32) []int32 {
	if g.comp[t] < 0 || (g.comp[s] >= 0 && g.comp[s] != g.comp[t]) {
		return nil
	}
	if s == t {
		return []int32{s}
	}
	key := [2]int32{s, t}
	if len(avoid) == 0 {
		if p, ok := g.cache[key]; ok {
			return p
		}
	}
	g.cur++
	if g.cur == 0 { // wrapped: start the stamps afresh
		clear(g.seen)
		clear(g.shut)
		g.cur = 1
	}
	cur := g.cur
	g.heap = g.heap[:0]
	g.g[s], g.from[s], g.seen[s] = 0, -1, cur
	g.push(navNode{octile(s, t), s})
	expanded := 0
	var found bool
	for len(g.heap) > 0 {
		n := g.pop()
		c := n.c
		if g.shut[c] == cur {
			continue
		}
		g.shut[c] = cur
		if c == t {
			found = true
			break
		}
		if expanded++; expanded > navMaxExpand {
			break
		}
		x, y := c%navN, c/navN
		for _, d := range navDirs {
			nx, ny := x+d[0], y+d[1]
			if nx < 0 || ny < 0 || nx >= navN || ny >= navN {
				continue
			}
			nc := ny*navN + nx
			if g.shut[nc] == cur || !g.adj(c, nc) || inCells(avoid, nc) {
				continue
			}
			cost := g.g[c] + d[2] + int32(g.cost[nc])
			if g.elev[nc] > g.elev[c] {
				cost += 3 // the climb
			}
			if g.seen[nc] != cur || cost < g.g[nc] {
				g.g[nc], g.from[nc], g.seen[nc] = cost, c, cur
				g.push(navNode{cost + octile(nc, t), nc})
			}
		}
	}
	if !found {
		return nil
	}
	var path []int32
	for c := t; c != -1; c = g.from[c] {
		path = append(path, c)
	}
	for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
		path[a], path[b] = path[b], path[a]
	}
	if len(avoid) == 0 {
		if len(g.ring) >= navCacheN {
			delete(g.cache, g.ring[0])
			g.ring = g.ring[1:]
		}
		g.cache[key] = path
		g.ring = append(g.ring, key)
	}
	return path
}

func inCells(s []int32, c int32) bool {
	for _, x := range s {
		if x == c {
			return true
		}
	}
	return false
}

func (g *navGrid) push(n navNode) {
	g.heap = append(g.heap, n)
	h := g.heap
	for i := len(h) - 1; i > 0; {
		p := (i - 1) / 2
		if h[p].f <= h[i].f {
			break
		}
		h[p], h[i] = h[i], h[p]
		i = p
	}
}

func (g *navGrid) pop() navNode {
	h := g.heap
	top := h[0]
	last := len(h) - 1
	h[0] = h[last]
	h = h[:last]
	for i := 0; ; {
		l, m := 2*i+1, i
		if l < len(h) && h[l].f < h[m].f {
			m = l
		}
		if l+1 < len(h) && h[l+1].f < h[m].f {
			m = l + 1
		}
		if m == i {
			break
		}
		h[i], h[m] = h[m], h[i]
		i = m
	}
	g.heap = h
	return top
}

// clear: a body a shoulder wide walks the straight leg from a to b on open
// ground, never a step of more than one level, never cutting a solid corner.
func (g *navGrid) clear(a, b navPt) bool {
	dx, dy := b.x-a.x, b.y-a.y
	l := math.Hypot(dx, dy)
	if l < 1e-6 {
		return true
	}
	ux, uy := dx/l, dy/l
	n := int(l/0.2) + 1
	for _, lane := range [3]float64{0, navShoulder, -navShoulder} {
		prev := g.cellAt(a.x, a.y)
		if prev < 0 {
			return false
		}
		for k := 1; k <= n; k++ {
			s := l * float64(k) / float64(n)
			w := lane * math.Min(1, math.Min(s, l-s)/0.5) // a body narrows to a point at either end
			c := g.cellAt(a.x+ux*s-uy*w, a.y+uy*s+ux*w)
			if c == prev {
				continue
			}
			if c < 0 || !g.adj(prev, c) {
				return false
			}
			prev = c
		}
	}
	return true
}

// smooth pulls a tile path taut: from (sx, sy) through the path to (tx, ty),
// keeping only the corners a straight leg cannot cut.
func (g *navGrid) smooth(sx, sy float64, cells []int32, tx, ty float64) []navPt {
	if len(cells) < 2 { // already in the goal's tile
		return []navPt{{tx, ty}}
	}
	pts := make([]navPt, 0, len(cells)+1)
	pts = append(pts, navPt{sx, sy})
	for _, c := range cells[1 : len(cells)-1] {
		pts = append(pts, g.centre(c))
	}
	pts = append(pts, navPt{tx, ty})
	var out []navPt
	if len(pts) > 1 && !g.clear(pts[0], pts[1]) { // off-centre in the first tile: to its middle first
		out = append(out, g.centre(cells[0]))
		pts[0] = out[0]
	}
	anchor := pts[0]
	for k := 1; k < len(pts); {
		j := k
		for j+1 < len(pts) && g.clear(anchor, pts[j+1]) {
			j++
		}
		out = append(out, pts[j])
		anchor = pts[j]
		k = j + 1
	}
	return out
}

// moveOK: a body can take the small step from (ax, ay) to (bx, by).
func (g *navGrid) moveOK(ax, ay, bx, by float64) bool {
	a, b := g.cellAt(ax, ay), g.cellAt(bx, by)
	if b < 0 || a < 0 {
		return false
	}
	return a == b || g.adj(a, b)
}

// ── following a route ───────────────────────────────────────────────────────

// folkNav is where a folk member is headed and how they mean to get there.
type folkNav struct {
	gx, gy  float64 // the goal
	has     bool    // a goal is set (route may still be waiting on the budget)
	route   []navPt
	ri      int
	gen     int // the grid's gen the route was planned on
	none    bool // no way there
	blocked int  // ticks the way ahead has been shut
	yield   int  // ticks spent letting someone by
	squeeze int  // ticks left of edging past someone there is no going round
	avoid   []int32
	hx, hy  float64 // the way they last moved
}

func (n *folkNav) reset() {
	n.has, n.route, n.ri, n.none, n.blocked, n.yield, n.squeeze, n.avoid = false, nil, 0, false, 0, 0, 0, n.avoid[:0]
}

// folkGo moves f a tick's walk at sp toward (x, y) along a route, planning one
// when the goal is new or the way was shut; true once f stands there (or as
// near as it can get, if someone is standing on the spot). f.stuck counts the
// ticks it could not move at all.
func (r *Room) folkGo(f *Folk, x, y, sp float64) bool {
	v, n := f.vil, &f.nav
	g := v.nav
	if math.Hypot(x-f.X, y-f.Y) < 0.06 {
		f.stuck = 0
		n.has = false
		return true
	}
	if !n.has || math.Abs(n.gx-x) > 0.05 || math.Abs(n.gy-y) > 0.05 {
		n.reset()
		n.gx, n.gy, n.has = x, y, true
	}
	if n.none && n.gen != g.gen {
		n.none = false // the ground changed: there may be a way now
	}
	if n.route != nil && n.gen != g.gen && !r.routeStillClear(f) {
		n.route = nil // a keeper built across it: think again
	}
	if n.route == nil {
		if n.none {
			f.stuck++
			return false
		}
		if v.navBudget <= 0 {
			return false // the village has planned enough this tick: next tick
		}
		v.navBudget--
		if !r.planRoute(f) {
			n.none, n.gen = true, g.gen
			f.stuck++
			return false
		}
	}
	for n.ri < len(n.route)-1 && math.Hypot(n.route[n.ri].x-f.X, n.route[n.ri].y-f.Y) < 0.08 {
		n.ri++
	}
	wp := n.route[n.ri]
	last := n.ri == len(n.route)-1
	d := math.Hypot(wp.x-f.X, wp.y-f.Y)
	if last && d <= sp && g.moveOK(f.X, f.Y, wp.x, wp.y) {
		if r.personAt(f, wp.x, wp.y, 0.4) == nil {
			f.X, f.Y = wp.x, wp.y
			f.stuck, f.moved, n.has = 0, true, false
			return true
		}
	}
	if last && d < 0.9 && r.personAt(f, wp.x, wp.y, 0.4) != nil {
		f.stuck, n.has = 0, false
		return true // someone is standing on the spot: do it from here
	}
	dx, dy := (wp.x-f.X)/d, (wp.y-f.Y)/d
	step := math.Min(sp, d)
	ax, ay, slow, wait := r.folkAvoid(f, dx, dy)
	if wait && n.yield < 10 && n.squeeze == 0 {
		n.yield++
		f.stuck = 0
		return false // letting someone through first
	}
	if n.squeeze > 0 {
		n.squeeze--
	}
	var bump *navPt // someone standing right in the way
	for k, c := range [3][3]float64{{ax, ay, step * slow}, {dx, dy, step * slow}, {dx, dy, step * 0.4}} {
		nx, ny := f.X+c[0]*c[2], f.Y+c[1]*c[2]
		if c[2] < 1e-4 || !g.moveOK(f.X, f.Y, nx, ny) {
			continue
		}
		if k > 0 && n.squeeze == 0 { // the straight way, unbent: not if it walks into someone
			if p := r.personAt(f, nx, ny, 0.42); p != nil {
				bump = p
				continue
			}
		}
		f.X, f.Y = nx, ny
		n.hx, n.hy = c[0], c[1]
		f.stuck, n.blocked, f.moved = 0, 0, true
		if !wait && bump == nil {
			n.yield = 0
		}
		if !last && math.Hypot(wp.x-f.X, wp.y-f.Y) < math.Max(sp, 0.12) {
			n.ri++ // reached the corner: on to the next leg
		}
		return false
	}
	if bump != nil {
		// someone stands in a gap there is no going round: wait a moment, then
		// look for a way round them, and if there is none, edge past
		f.stuck = 0
		switch n.yield++; {
		case n.yield == 8:
			if c := g.cellAt(bump.x, bump.y); c >= 0 && len(n.avoid) < 4 && c != g.cellAt(n.gx, n.gy) {
				n.avoid = append(n.avoid, c)
				n.route = nil
			}
		case n.yield >= 14:
			n.squeeze, n.yield = 10, 0
		}
		return false
	}
	// the way ahead is shut (a build, or pushed against a wall): plan again
	n.blocked++
	f.stuck++
	if n.blocked >= 3 {
		n.route, n.blocked = nil, 0
	}
	return false
}

// personAt is where someone already stands within rad of (x, y) that f would
// be walking into — a keeper, or a folk member standing still — or nil.
func (r *Room) personAt(f *Folk, x, y, rad float64) *navPt {
	closer := func(ox, oy float64) bool {
		return math.Hypot(ox-x, oy-y) < rad && math.Hypot(ox-x, oy-y) < math.Hypot(ox-f.X, oy-f.Y)
	}
	for _, q := range r.playerOrder {
		if q.Z == 0 && math.Abs(q.X-x) < 1 && math.Abs(q.Y-y) < 1 && closer(q.X, q.Y) {
			return &navPt{q.X, q.Y}
		}
	}
	for _, o := range f.vil.members {
		if o != f && o.state != fsHidden && !o.moved && (f.party == nil || o.party != f.party) &&
			math.Abs(o.X-x) < 1 && math.Abs(o.Y-y) < 1 && closer(o.X, o.Y) {
			return &navPt{o.X, o.Y}
		}
	}
	return nil
}

// planRoute finds f's way to its goal, keeping off n.avoid; false if there is none.
func (r *Room) planRoute(f *Folk) bool {
	n, g := &f.nav, f.vil.nav
	s := g.cellAt(f.X, f.Y)
	t := g.cellAt(n.gx, n.gy)
	if s < 0 || t < 0 {
		return false
	}
	home := g.comp[s]
	if home < 0 { // standing where something was just built: whichever open tile is nearest
		s2 := g.nearestOpen(s, -1, 2)
		if s2 < 0 {
			return false
		}
		home = g.comp[s2]
	}
	if t2 := g.nearestOpen(t, home, 2); t2 != t {
		if t2 < 0 {
			return false
		}
		t = t2
		c := g.centre(t)
		n.gx, n.gy = c.x, c.y
	}
	cells := g.find(s, t, n.avoid)
	if cells == nil {
		return false
	}
	n.route, n.ri, n.gen = g.smooth(f.X, f.Y, cells, n.gx, n.gy), 0, g.gen
	return true
}

// routeStillClear: nothing new stands across what is left of f's route.
func (r *Room) routeStillClear(f *Folk) bool {
	n, g := &f.nav, f.vil.nav
	n.gen = g.gen
	at := navPt{f.X, f.Y}
	for _, p := range n.route[n.ri:] {
		if !g.clear(at, p) {
			return false
		}
		at = p
	}
	return true
}

// folkAvoid bends f's way (dx, dy) round the people near it: room to the
// shoulder, round anyone standing in the road, right to pass someone coming
// the other way, and in behind someone going the same way. It returns the
// bent way, how much to slow, and whether to stop and let someone through.
func (r *Room) folkAvoid(f *Folk, dx, dy float64) (float64, float64, float64, bool) {
	sx, sy, slow := 0.0, 0.0, 1.0
	wait := false
	lx, ly := -dy, dx // to the left of the way
	body := func(ox, oy, rad float64, moving bool, hx, hy float64, yieldTo bool) {
		rx, ry := ox-f.X, oy-f.Y
		if math.Abs(rx) > 1.6 || math.Abs(ry) > 1.6 {
			return
		}
		d := math.Hypot(rx, ry)
		if d < 1e-6 {
			return
		}
		if d < folkBodyR+rad { // too close: shoulder room
			k := (folkBodyR + rad - d) / (folkBodyR + rad)
			sx -= rx / d * k
			sy -= ry / d * k
		}
		along, lat := rx*dx+ry*dy, rx*lx+ry*ly
		if along <= 0 || along > 1.4 || math.Abs(lat) > folkBodyR+rad+0.15 {
			return
		}
		w := (1.4 - along) / 1.4
		switch {
		case moving && hx*dx+hy*dy < -0.3: // coming the other way: keep right
			sx -= lx * w * 0.9
			sy -= ly * w * 0.9
			if along < 0.7 && yieldTo {
				wait = true
			}
		case moving && hx*dx+hy*dy > 0.5: // going our way: fall in behind
			if along < 0.9 {
				slow = math.Min(slow, 0.45)
			}
		default: // standing in the road: round them, on the side away from them
			side := 1.0
			if lat > 0 {
				side = -1
			}
			sx += lx * side * w * 1.3
			sy += ly * side * w * 1.3
		}
	}
	for k, o := range f.vil.members {
		if o == f || o.state == fsHidden || (f.party != nil && o.party == f.party) {
			continue
		}
		body(o.X, o.Y, folkBodyR, o.moved, o.nav.hx, o.nav.hy, k < f.idx)
	}
	for _, q := range r.playerOrder {
		if q.Z == 0 {
			body(q.X, q.Y, 0.4, false, 0, 0, true)
		}
	}
	bx, by := dx+sx, dy+sy
	if l := math.Hypot(bx, by); l > 1e-6 {
		bx, by = bx/l, by/l
	} else {
		bx, by = dx, dy
	}
	return bx, by, slow, wait
}

// folkMakeRoom: f is standing still with someone right on top of it — a keeper
// walked up close, or another folk member stopped there — so it edges aside.
func (r *Room) folkMakeRoom(f *Folk) {
	g := f.vil.nav
	push := func(ox, oy, rad float64) {
		rx, ry := f.X-ox, f.Y-oy
		d := math.Hypot(rx, ry)
		if d >= rad || d < 1e-6 {
			return
		}
		nx, ny := f.X+rx/d*0.05, f.Y+ry/d*0.05
		if g.moveOK(f.X, f.Y, nx, ny) {
			f.X, f.Y = nx, ny
		}
	}
	for _, q := range r.playerOrder {
		if q.Z == 0 {
			push(q.X, q.Y, 0.42)
		}
	}
	for _, o := range f.vil.members {
		if o.idx < f.idx && o.state != fsHidden && !o.moved {
			push(o.X, o.Y, 0.36)
		}
	}
}
