package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// Folk navigation (folk_nav.go): routes round cliffs, ramps, huts and walls.

// moorWorld is a copy of the shared world with a patch east of the Woods fire
// flattened to open grass at height 2 and shaped by shape, which may raise
// ground or plant nothing at all. The shared world is never touched.
func moorWorld(t *testing.T, shape func(w *world.World, fx, fy int)) (*fixture, *village) {
	t.Helper()
	if sharedWorld == nil {
		sharedWorld = world.GenWorld("hearth-1")
	}
	w := *sharedWorld
	w.Tiles = append([]uint8(nil), sharedWorld.Tiles...)
	w.Elev = append([]uint8(nil), sharedWorld.Elev...)
	w.Nodes = make(map[int]uint8, len(sharedWorld.Nodes))
	for i, k := range sharedWorld.Nodes {
		w.Nodes[i] = k
	}
	// the Woods fire is where FindFolkCamps puts it on the shared world
	camps := world.FindFolkCamps(sharedWorld, map[int]bool{})
	fx, fy := -1, -1
	for _, c := range camps {
		if c.IslandID == "woods" {
			fx, fy = c.FireX, c.FireY
		}
	}
	if fx < 0 {
		t.Fatal("fixture: no Woods fire")
	}
	for y := fy - 7; y <= fy+7; y++ {
		for x := fx + 8; x <= fx+21; x++ {
			i := y*world.SIZE + x
			w.Tiles[i], w.Elev[i] = world.TGrass, 2
			delete(w.Nodes, i)
		}
	}
	shape(&w, fx, fy)
	r, err := New(Config{Seed: "hearth-1", AllowWarp: true, World: &w})
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{r: r, t: t, now: 1_700_000_000_000}
	r.nowFn = func() int64 { return f.now }
	f.sess = NewSession("test01", "u_test", "Tester", "test")
	f.p = newPlayer(f.sess, r.spawn, f.now, r.defs)
	f.p.X, f.p.Y = 640, 600 // out of everyone's way
	r.players[f.sess.ID] = f.p
	r.playerOrder = append(r.playerOrder, f.p)
	f.p.chunkInit = true
	v := r.villageOf("woods")
	if v == nil || v.nav.x0 != fx-navR || v.nav.y0 != fy-navR {
		t.Fatal("fixture: the Woods village moved in the copied world")
	}
	return f, v
}

// walker is a folk member of v standing at (x, y) with nothing else to do.
func walker(v *village, x, y float64) *Folk {
	fk := v.members[len(v.members)-1]
	fk.X, fk.Y = x, y
	fk.dropPlans()
	fk.state = fsTask
	return fk
}

// walkTo walks fk to (x, y), checking every tick's step for height and footing;
// it returns the heights of the tiles it crossed, or fails the test.
func walkTo(t *testing.T, f *fixture, fk *Folk, x, y float64, ticks int, each func(k int)) []uint8 {
	t.Helper()
	g := fk.vil.nav
	var seen []uint8
	last := ti(fk.X, fk.Y)
	for k := range ticks {
		if each != nil {
			each(k)
		}
		fk.vil.navBudget = navBudget
		r := f.r
		r.navRefresh(g, false)
		if r.folkGo(fk, x, y, folkWalkSp) {
			return seen
		}
		i := ti(fk.X, fk.Y)
		if c := g.cellAt(fk.X, fk.Y); c < 0 || !g.open[c] {
			t.Fatalf("tick %d: stepped onto a closed tile at %.2f,%.2f", k, fk.X, fk.Y)
		}
		if i != last {
			if d := int(r.world.Elev[i]) - int(r.world.Elev[last]); d > 1 || d < -1 {
				t.Fatalf("tick %d: a step of %d levels at %.2f,%.2f", k, d, fk.X, fk.Y)
			}
			seen = append(seen, r.world.Elev[i])
			last = i
		}
	}
	t.Fatalf("never got to %.1f,%.1f: at %.2f,%.2f (none %v)", x, y, fk.X, fk.Y, fk.nav.none)
	return nil
}

// A plateau two levels up with a ramp at one end: the folk walk round to the
// ramp and up it, never up the cliff; take the ramp away and there is no way up.
func TestFolkPathTakesTheRampNotTheCliff(t *testing.T) {
	plateau := func(ramp bool) func(w *world.World, fx, fy int) {
		return func(w *world.World, fx, fy int) {
			for y := fy - 7; y <= fy+7; y++ {
				for x := fx + 15; x <= fx+21; x++ {
					w.Elev[y*world.SIZE+x] = 4 // a cliff of +2 along its west face
				}
				if ramp && y >= fy+5 {
					for x := fx + 13; x <= fx+14; x++ {
						w.Elev[y*world.SIZE+x] = 3 // the ramp: 2 → 3 → 4
					}
				}
			}
		}
	}
	f, v := moorWorld(t, plateau(true))
	fx, fy := float64(f.r.camps[0].FireX), float64(f.r.camps[0].FireY)
	fk := walker(v, fx+11.5, fy-4.5)
	seen := walkTo(t, f, fk, fx+18.5, fy-4.5, 600, nil)
	ramp := false
	for _, e := range seen {
		ramp = ramp || e == 3
	}
	if !ramp {
		t.Fatalf("got up the plateau without the ramp: heights crossed %v", seen)
	}
	if math.Hypot(fk.X-(fx+18.5), fk.Y-(fy-4.5)) > 0.1 {
		t.Fatalf("stopped short at %.2f,%.2f", fk.X, fk.Y)
	}

	// no ramp: no way up, and they know it rather than walking into the cliff
	f, v = moorWorld(t, plateau(false))
	fk = walker(v, fx+11.5, fy-4.5)
	for range 20 {
		v.navBudget = navBudget
		if f.r.folkGo(fk, fx+18.5, fy-4.5, folkWalkSp) {
			t.Fatal("walked up a two-level cliff")
		}
	}
	if !fk.nav.none {
		t.Fatal("found a way up a plateau with no ramp")
	}
	if fk.X > fx+15 {
		t.Fatalf("climbed onto the plateau anyway: %.2f,%.2f", fk.X, fk.Y)
	}
}

// wallAcross builds a keeper's wall across x = wx from y0 to y1 (inclusive).
func wallAcross(r *Room, wx, y0, y1 int) {
	for y := y0; y <= y1; y++ {
		r.structures[y*world.SIZE+wx] = &Structure{Kind: "wall", HP: 100, Lvl: 1}
	}
}

// A keeper's wall across the way: the folk go round its end. Built while they
// are on their way, they plan afresh and still never walk into it.
func TestFolkPathGoesRoundAPlayerWall(t *testing.T) {
	f, v := moorWorld(t, func(*world.World, int, int) {})
	if f.r.defs.DecorNonBlk["wall"] {
		t.Fatal("fixture: walls do not block")
	}
	fx, fy := f.r.camps[0].FireX, f.r.camps[0].FireY
	wallAcross(f.r, fx+15, fy-7, fy+4) // a gap at the south end
	fk := walker(v, float64(fx)+10.5, float64(fy)-2.5)
	walkTo(t, f, fk, float64(fx)+19.5, float64(fy)-2.5, 800, nil)

	// again, the wall going up once they have set off
	f, v = moorWorld(t, func(*world.World, int, int) {})
	fk = walker(v, float64(fx)+10.5, float64(fy)-2.5)
	built := false
	walkTo(t, f, fk, float64(fx)+19.5, float64(fy)-2.5, 800, func(k int) {
		if k == 12 {
			if fk.X > float64(fx)+14 {
				t.Fatal("fixture: already past the wall's line")
			}
			wallAcross(f.r, fx+15, fy-7, fy+4)
			built = true
		}
	})
	if !built {
		t.Fatal("arrived before the wall went up")
	}
}

// Two folk walking at each other down the same line pass without walking
// through each other, and one walking at a keeper standing in the road goes round.
func TestFolkPassEachOtherAndTheKeeper(t *testing.T) {
	f, v := moorWorld(t, func(*world.World, int, int) {})
	fx, fy := float64(f.r.camps[0].FireX), float64(f.r.camps[0].FireY)
	a, b := v.members[len(v.members)-1], v.members[len(v.members)-2]
	for _, fk := range []*Folk{a, b} {
		fk.dropPlans()
		fk.state = fsTask
	}
	a.X, a.Y = fx+9.5, fy+0.5
	b.X, b.Y = fx+19.5, fy+0.5
	for _, m := range v.members {
		if m != a && m != b {
			m.state = fsHidden // out of the way
		}
	}
	doneA, doneB, closest := false, false, 9.0
	for k := 0; k < 400 && !(doneA && doneB); k++ {
		v.navBudget = navBudget
		a.moved, b.moved = false, false
		doneA = doneA || f.r.folkGo(a, fx+19.5, fy+0.5, folkWalkSp)
		doneB = doneB || f.r.folkGo(b, fx+9.5, fy+0.5, folkWalkSp)
		f.r.folkSeparate(v)
		closest = math.Min(closest, math.Hypot(a.X-b.X, a.Y-b.Y))
	}
	if !doneA || !doneB {
		t.Fatalf("head-on pair never got by each other: %.2f,%.2f and %.2f,%.2f", a.X, a.Y, b.X, b.Y)
	}
	if closest < 0.35 {
		t.Fatalf("walked through each other: %.2f apart at the closest", closest)
	}

	// a keeper standing square in the road
	f.p.X, f.p.Y = fx+14.5, fy+0.5
	a.X, a.Y = fx+9.5, fy+0.5
	a.nav.reset()
	near, done := 9.0, false
	for k := 0; k < 400 && !done; k++ {
		v.navBudget = navBudget
		done = f.r.folkGo(a, fx+19.5, fy+0.5, folkWalkSp)
		near = math.Min(near, math.Hypot(a.X-f.p.X, a.Y-f.p.Y))
	}
	if !done {
		t.Fatalf("never got round the keeper: at %.2f,%.2f", a.X, a.Y)
	}
	if near < 0.45 {
		t.Fatalf("walked into the keeper: %.2f apart at the closest", near)
	}
}

// A route is cached by tile, so the same walk planned twice costs one search.
func TestFolkPathCache(t *testing.T) {
	f := newFixture(t)
	v := f.r.villageOf("woods")
	g := v.nav
	s, tc := g.cellAt(v.fx-2.5, v.fy+0.5), g.cellAt(v.fx+5.5, v.fy-4.5)
	p1 := g.find(s, tc, nil)
	if p1 == nil {
		t.Fatal("fixture: no way across the Woods yard")
	}
	if p2 := g.find(s, tc, nil); &p2[0] != &p1[0] {
		t.Fatal("the same route was searched for twice")
	}
	f.r.navRefresh(g, true) // a rebuild forgets them
	if p3 := g.find(s, tc, nil); &p3[0] == &p1[0] {
		t.Fatal("a route outlived the ground it was planned on")
	}
}
