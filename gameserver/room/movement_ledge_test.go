package room

import (
	"testing"

	"hearth/gameserver/world"
)

// ledgePatch finds a 3x3 patch of plain grass with nothing on it and lets the test
// shape its elevation (row-major, top-left first). The world is shared between
// tests, so the original elevations are put back afterwards.
func ledgePatch(t *testing.T, f *fixture, elev [9]uint8) (int, int) {
	t.Helper()
	x0, y0, ok := findTile(f.r.world, func(i int) bool {
		x, y := i%world.SIZE, i/world.SIZE
		if x < 2 || y < 2 || x > world.SIZE-4 || y > world.SIZE-4 {
			return false
		}
		for dy := 0; dy < 3; dy++ {
			for dx := 0; dx < 3; dx++ {
				j := (y+dy)*world.SIZE + x + dx
				_, node := f.r.world.Nodes[j]
				_, st := f.r.structures[j]
				if f.r.world.Tiles[j] != world.TGrass || node || st || f.r.medicTiles[j] || world.LandmarkBlock[j] {
					return false
				}
			}
		}
		return true
	})
	if !ok {
		t.Fatal("fixture: no open 3x3 grass patch")
	}
	var saved [9]uint8
	for k := 0; k < 9; k++ {
		j := (y0+k/3)*world.SIZE + x0 + k%3
		saved[k], f.r.world.Elev[j] = f.r.world.Elev[j], elev[k]
	}
	t.Cleanup(func() {
		for k := 0; k < 9; k++ {
			f.r.world.Elev[(y0+k/3)*world.SIZE+x0+k%3] = saved[k]
		}
	})
	return x0, y0
}

// stepFrom moves the player from (fx, fy) to (tx, ty) through handlePos.
func (f *fixture) stepFrom(fx, fy, tx, ty float64) {
	f.p.HP = MaxHP
	f.place(fx, fy, 0)
	f.now += 200
	f.pos(map[string]any{"t": "pos", "x": tx, "y": ty})
}

// Cutting diagonally across the corner of a staircase (3 -> 2 -> 1, every step a
// single level) is walking down a slope, not a fall — though the two end tiles
// differ by 2. This is the "fall damage on ground that looks flat" report.
func TestStaircaseCornerIsNotAFall(t *testing.T) {
	f := newFixture(t)
	x0, y0 := ledgePatch(t, f, [9]uint8{
		3, 2, 2,
		2, 1, 1,
		2, 1, 1,
	})
	f.stepFrom(float64(x0)+0.8, float64(y0)+0.8, float64(x0)+1.2, float64(y0)+1.2)
	if f.p.HP != MaxHP {
		t.Fatalf("a diagonal step down a 1-level staircase cost hp: %d", f.p.HP)
	}
	if f.p.X != float64(x0)+1.2 {
		t.Fatal("the step was refused")
	}
}

// A real ledge still hurts: straight over it, and diagonally when both
// staircase routes go over it.
func TestLedgeStillHurts(t *testing.T) {
	f := newFixture(t)
	x0, y0 := ledgePatch(t, f, [9]uint8{
		4, 1, 1,
		1, 1, 1,
		1, 1, 1,
	})
	f.stepFrom(float64(x0)+0.8, float64(y0)+0.5, float64(x0)+1.2, float64(y0)+0.5)
	if f.p.HP != MaxHP-2 {
		t.Fatalf("a 3-level ledge cost %d hp, want 2", MaxHP-f.p.HP)
	}
	f.stepFrom(float64(x0)+0.8, float64(y0)+0.8, float64(x0)+1.2, float64(y0)+1.2)
	if f.p.HP != MaxHP-2 {
		t.Fatalf("diagonally off a ledge on both sides cost %d hp, want 2", MaxHP-f.p.HP)
	}
}

// A keeper standing inside a blocked tile (surfacing on the mineshaft's own tile)
// can step within it and out of it, but still cannot step into another blocked tile.
func TestStepOutOfABlockedTile(t *testing.T) {
	f := newFixture(t)
	x0, y0 := ledgePatch(t, f, [9]uint8{1, 1, 1, 1, 1, 1, 1, 1, 1})
	shaft := (y0+1)*world.SIZE + x0 + 1
	f.r.structures[shaft] = &Structure{Kind: "mineshaft", HP: 40, Lvl: 1}
	wall := (y0+1)*world.SIZE + x0 + 2
	f.r.structures[wall] = &Structure{Kind: "wall", HP: 20, Lvl: 1}
	t.Cleanup(func() { delete(f.r.structures, shaft); delete(f.r.structures, wall) })
	cx, cy := float64(x0)+1.5, float64(y0)+1.5

	f.place(cx, cy, 0)
	f.now += 200
	f.pos(map[string]any{"t": "pos", "x": cx - 0.2, "y": cy})
	if f.p.X != cx-0.2 {
		t.Fatal("a step inside the tile the keeper already stands in was refused")
	}
	f.now += 200
	f.pos(map[string]any{"t": "pos", "x": cx - 0.7, "y": cy})
	if f.p.X != cx-0.7 {
		t.Fatal("stepping out of the blocked tile onto open ground was refused")
	}
	f.place(cx, cy, 0)
	f.now += 200
	f.pos(map[string]any{"t": "pos", "x": cx + 0.7, "y": cy})
	if f.p.X != cx {
		t.Fatal("stepping from the shaft tile into a wall was allowed")
	}
}
