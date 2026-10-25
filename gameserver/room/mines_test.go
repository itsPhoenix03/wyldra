package room

import (
	"testing"

	"hearth/gameserver/world"
)

// mineSite finds a tile whose 7x7 surroundings are all diggable and whose
// centre takes a building, so every openShaft case fits around it.
func mineSite(t *testing.T, f *fixture) (int, int) {
	t.Helper()
	x, y, ok := findTile(f.r.world, func(i int) bool {
		x, y := i%world.SIZE, i/world.SIZE
		if x < 3 || y < 3 || x >= world.SIZE-3 || y >= world.SIZE-3 {
			return false
		}
		for dy := -3; dy <= 3; dy++ {
			for dx := -3; dx <= 3; dx++ {
				if !f.r.diggable((y+dy)*world.SIZE + x + dx) {
					return false
				}
			}
		}
		_, isNode := f.r.world.Nodes[i]
		return !isNode && !f.r.blockedTile(float64(x), float64(y))
	})
	if !ok {
		t.Fatal("hearth-1 has no open diggable ground, which cannot be right")
	}
	return x, y
}

func at(x, y int) int { return y*world.SIZE + x }

// sameMine reports whether two carved tiles belong to one connected mine.
func (r *Room) sameMine(a, b int) bool {
	return r.digs[a] && r.digs[b] && r.mineRoot(a) == r.mineRoot(b)
}

func TestOpenShaftOnFreshRockCarvesTheChamber(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	opened, mines := f.r.openShaft(x, y)
	if len(opened) != 9 || mines != 0 {
		t.Fatalf("fresh entrance: opened %d tiles into %d mines, want 9 into 0", len(opened), mines)
	}
}

func TestOpenShaftOverATunnelCarvesNothing(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	for dx := -3; dx <= 3; dx++ {
		f.r.carve(at(x+dx, y))
	}
	before := len(f.r.digs)
	opened, mines := f.r.openShaft(x, y)
	if len(opened) != 0 || len(f.r.digs) != before {
		t.Fatalf("an entrance over a tunnel scraped %v out of it", opened)
	}
	if mines != 1 {
		t.Fatalf("an entrance over a tunnel leads into %d mines, want 1", mines)
	}
}

func TestOpenShaftBesideATunnelOpensOnlyTheShaft(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	// a tunnel running past the entrance's east side
	for dy := -3; dy <= 3; dy++ {
		f.r.carve(at(x+1, y+dy))
	}
	opened, mines := f.r.openShaft(x, y)
	if len(opened) != 1 || opened[0] != at(x, y) {
		t.Fatalf("an entrance beside a tunnel opened %v, want only the shaft", opened)
	}
	if mines != 1 || !f.r.sameMine(at(x, y), at(x+1, y+3)) {
		t.Fatal("the shaft is not part of the tunnel beside it")
	}
}

func TestOpenShaftAtACornerBridgesIn(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	f.r.carve(at(x+1, y+1))
	f.r.carve(at(x+2, y+1))
	opened, mines := f.r.openShaft(x, y)
	if len(opened) != 2 {
		t.Fatalf("a corner entrance opened %v, want the shaft and one bridge", opened)
	}
	if mines != 1 || !f.r.sameMine(at(x, y), at(x+2, y+1)) {
		t.Fatal("the corner entrance is not walkable into the mine")
	}
}

func TestOpenShaftBetweenTwoMinesJoinsThem(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	f.r.carve(at(x-1, y))
	f.r.carve(at(x-2, y))
	f.r.carve(at(x+1, y))
	f.r.carve(at(x+2, y))
	if f.r.sameMine(at(x-2, y), at(x+2, y)) {
		t.Fatal("two separate tunnels already count as one mine")
	}
	opened, mines := f.r.openShaft(x, y)
	if len(opened) != 1 || mines != 2 {
		t.Fatalf("opened %v into %d mines, want only the shaft into 2", opened, mines)
	}
	if !f.r.sameMine(at(x-2, y), at(x+2, y)) {
		t.Fatal("the entrance did not join the two mines")
	}
}

// Digging the last rock between two mines makes them one, and only that dig
// reports a break-through.
func TestCarveReportsABreakThrough(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	for dx := -3; dx <= -1; dx++ {
		f.r.carve(at(x+dx, y))
	}
	if n := f.r.carve(at(x+1, y)); n != 0 {
		t.Fatalf("a lone pocket touched %d mines", n)
	}
	if n := f.r.carve(at(x+2, y)); n != 1 {
		t.Fatalf("extending a mine touched %d mines, want 1", n)
	}
	if n := f.r.carve(at(x, y)); n != 2 {
		t.Fatalf("breaking through touched %d mines, want 2", n)
	}
	if !f.r.sameMine(at(x-3, y), at(x+2, y)) {
		t.Fatal("the break-through did not join the mines")
	}
}

// End to end: the digger is told, and a reloaded room still knows the mines.
func TestDigBreakThroughTellsTheDigger(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	f.r.carve(at(x-1, y))
	f.r.carve(at(x+1, y))
	f.p.Tools["pick"] = true
	f.place(float64(x-1)+0.5, float64(y)+0.5, 1)
	f.drain()
	f.r.handleDig(f.p, map[string]any{"t": "dig", "i": float64(at(x, y))})
	told := false
	for _, m := range f.drain() {
		if m["t"] == "msg" && m["s"] == "⛏ You broke through into another mine!" {
			told = true
		}
	}
	if !told {
		t.Fatal("the digger was not told they broke through")
	}
}

func TestBuildOverAMineDropsIntoIt(t *testing.T) {
	f := newFixture(t)
	x, y := mineSite(t, f)
	for dx := -3; dx <= 3; dx++ {
		f.r.carve(at(x+dx, y))
	}
	before := len(f.r.digs)
	f.p.Inv["mineshaft"] = 1
	f.place(float64(x)+0.5, float64(y+1)+0.5, 0)
	f.drain()
	f.r.handleBuild(f.p, map[string]any{"t": "build", "kind": "mineshaft", "i": float64(at(x, y))})
	if s := f.r.structures[at(x, y)]; s == nil || s.Kind != "mineshaft" {
		t.Fatal("the entrance was not built")
	}
	if len(f.r.digs) != before {
		t.Fatal("building over a mine scraped a chamber out of it")
	}
	told := false
	for _, m := range f.drain() {
		if m["t"] == "dig" {
			t.Fatalf("building over a mine broadcast a dig: %v", m)
		}
		if m["t"] == "msg" && m["s"] == "Your entrance opens into an existing mine." {
			told = true
		}
	}
	if !told {
		t.Fatal("the builder was not told the entrance opens into a mine")
	}
}
