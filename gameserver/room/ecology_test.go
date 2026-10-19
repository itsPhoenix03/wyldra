package room

import (
	"testing"

	"hearth/gameserver/world"
)

// The land heals (ecology.go): soured ground turns back to grass once its
// sector rests, regrowing trees reclaim it, and nothing grows back through a
// building or the blight. Also the farm loop end to end.

// grassSector finds a 16x16 sector that is grass throughout with no medic or
// landmark tile, returning its top-left corner. Trees may stand in it.
func grassSector(t *testing.T, r *Room) (int, int) {
	t.Helper()
	for sy := 16; sy < world.SIZE-32; sy += 16 {
		for sx := 16; sx < world.SIZE-32; sx += 16 {
			ok := true
			for y := sy; y < sy+16 && ok; y++ {
				for x := sx; x < sx+16; x++ {
					i := y*world.SIZE + x
					if r.world.Tiles[i] != world.TGrass || r.medicTiles[i] || world.LandmarkBlock[i] {
						ok = false
						break
					}
				}
			}
			if ok {
				return sx, sy
			}
		}
	}
	t.Fatal("no open 16x16 grass sector")
	return 0, 0
}

// bareTiles returns n tiles of the sector with no node on them.
func bareTiles(r *Room, sx, sy, n int) []int {
	var out []int
	for y := sy + 1; y < sy+15 && len(out) < n; y += 3 {
		for x := sx + 1; x < sx+15 && len(out) < n; x += 3 {
			i := y*world.SIZE + x
			if _, node := r.world.Nodes[i]; !node {
				out = append(out, i)
			}
		}
	}
	return out
}

// ecoPass runs the sim tick on the next healing-pass boundary.
func (f *fixture) ecoPass() {
	f.r.tickN = (f.r.tickN/ecoPassTicks + 1) * ecoPassTicks
	f.r.ecologyTick(f.now)
}

// The 8th felling sours the ground and starts the pressure over; a building
// is never soured under.
func TestEcoEighthFellingSours(t *testing.T) {
	f := newFixture(t)
	sx, sy := grassSector(t, f.r)
	built := bareTiles(f.r, sx, sy, 1)[0]
	f.r.structures[built] = &Structure{Kind: "wall", HP: 40, Lvl: 1}
	for k := 0; k < ecoSourAt-1; k++ {
		if f.r.chopEcosystem(sx+3, sy+3, f.now) {
			t.Fatalf("felling %d soured the ground", k+1)
		}
	}
	// every one of the 256 tiles is a candidate; 40 tries keeps one off the wall
	// only by luck, so check the wall over many sourings instead
	for round := 0; round < 30; round++ {
		if round > 0 {
			f.r.mudTiles = map[int]bool{} // fresh grass, or the sector soon runs out of it
			for k := 0; k < ecoSourAt-1; k++ {
				f.r.chopEcosystem(sx+3, sy+3, f.now)
			}
		}
		if !f.r.chopEcosystem(sx+3, sy+3, f.now) {
			t.Fatalf("round %d: the %dth felling did not sour the ground", round, ecoSourAt)
		}
		if f.r.sectorChops[sectorOf(sx, sy)] != 0 {
			t.Fatalf("pressure did not start over after souring: %d", f.r.sectorChops[sectorOf(sx, sy)])
		}
		if f.r.mudTiles[built] {
			t.Fatal("ground under a wall soured — it could never heal")
		}
	}
}

// A rested sector's mud heals over a few passes; an active one's does not.
func TestEcoRestedSectorHeals(t *testing.T) {
	f := newFixture(t)
	sx, sy := grassSector(t, f.r)
	for k := 0; k < ecoSourAt; k++ {
		f.r.chopEcosystem(sx+3, sy+3, f.now)
	}
	n := len(f.r.mudTiles)
	if n == 0 {
		t.Fatal("setup: nothing soured")
	}
	// still being worked: a pass a minute after the last felling heals nothing
	f.now += ecoRestMs / 2
	f.reset()
	for k := 0; k < 20; k++ {
		f.ecoPass()
	}
	if len(f.r.mudTiles) != n {
		t.Fatalf("mud healed in a sector felled %d s ago", ecoRestMs/2000)
	}
	// rested: the ground comes back, a few tiles a pass, every tile in the end
	f.now += ecoRestMs
	f.reset()
	f.ecoPass()
	passes := 1
	for len(f.r.mudTiles) > 0 && passes < 300 {
		f.ecoPass()
		passes++
	}
	if len(f.r.mudTiles) != 0 {
		t.Fatalf("%d soured tiles never healed", len(f.r.mudTiles))
	}
	if passes < 3 {
		t.Fatalf("all %d tiles healed in %d passes — that is not slow", n, passes)
	}
	healed := map[int]bool{}
	for _, m := range f.drain() {
		if m["t"] != "regrow" {
			continue
		}
		prev := -1
		for _, v := range m["tiles"].([]any) {
			i := int(v.(float64))
			if i <= prev {
				t.Fatal("regrow tiles are not in tile order")
			}
			prev = i
			healed[i] = true
		}
	}
	if len(healed) != n {
		t.Fatalf("regrow frames named %d tiles, %d healed", len(healed), n)
	}
}

// Nothing grows back through a building or the blight.
func TestEcoNoHealUnderBuildOrBlight(t *testing.T) {
	f := newFixture(t)
	sx, sy := grassSector(t, f.r)
	bare := bareTiles(f.r, sx, sy, 3)
	a, b, c := bare[0], bare[1], bare[2]
	f.r.mudTiles[a], f.r.mudTiles[b], f.r.mudTiles[c] = true, true, true
	f.r.structures[a] = &Structure{Kind: "campfire", HP: 10, Lvl: 1}
	f.r.addModule(&Module{I: b, Slot: "floor", Kind: "mod_floor_wood", HP: 10})
	f.r.setInfected(c, f.now+10*ecoRestMs)
	for k := 0; k < 200; k++ {
		f.ecoPass()
	}
	if !f.r.mudTiles[a] || !f.r.mudTiles[b] || !f.r.mudTiles[c] {
		t.Fatalf("soured ground healed under a building or the blight: campfire=%v floor=%v blight=%v",
			!f.r.mudTiles[a], !f.r.mudTiles[b], !f.r.mudTiles[c])
	}
	// clear them and they heal like any other
	delete(f.r.structures, a)
	f.r.removeModule(b, "floor")
	for k := 0; k < 300 && (f.r.mudTiles[a] || f.r.mudTiles[b]); k++ {
		f.ecoPass()
	}
	if f.r.mudTiles[a] || f.r.mudTiles[b] {
		t.Fatal("ground did not heal once the building came down")
	}
}

// Rested sectors shed felling pressure; worked ones keep it.
func TestEcoPressureRelaxes(t *testing.T) {
	f := newFixture(t)
	sx, sy := grassSector(t, f.r)
	sk := sectorOf(sx, sy)
	for k := 0; k < 5; k++ {
		f.r.chopEcosystem(sx+3, sy+3, f.now)
	}
	f.ecoPass()
	if f.r.sectorChops[sk] != 5 {
		t.Fatalf("a sector felled this instant lost pressure: %d", f.r.sectorChops[sk])
	}
	f.now += ecoRestMs
	for k := 0; k < 5; k++ {
		f.ecoPass()
	}
	if _, left := f.r.sectorChops[sk]; left {
		t.Fatalf("pressure did not relax away in a rested sector: %d", f.r.sectorChops[sk])
	}
}

// A tree growing back eases its sector and reclaims the nearest soured tile —
// never one under a building.
func TestEcoRegrownTreeReclaimsSoil(t *testing.T) {
	f := newFixture(t)
	i := firstOf(t, f.r.world, world.NodeTree)
	x, y := i%world.SIZE, i/world.SIZE
	var cand []int
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			j := (y+dy)*world.SIZE + (x + dx)
			if j != i && f.r.world.Tiles[j] != world.TWater {
				cand = append(cand, j)
			}
		}
	}
	if len(cand) < 2 {
		t.Skip("tree with too little land round it")
	}
	near, built := cand[0], cand[1]
	far := ti(float64(x+3), float64(y))
	f.r.mudTiles[near], f.r.mudTiles[built], f.r.mudTiles[far] = true, true, true
	f.r.structures[built] = &Structure{Kind: "wall", HP: 40, Lvl: 1}
	sk := sectorOf(x, y)
	f.r.sectorChops[sk] = 3
	f.r.sectorChopAt[sk] = f.now // still being worked: only regrowth can heal
	f.r.removed[i] = f.now - 1
	f.reset()
	f.respawnPass()
	if f.r.removedAt(i) {
		t.Fatal("setup: the tree did not grow back")
	}
	if f.r.mudTiles[near] {
		t.Fatal("the regrown tree reclaimed nothing next to it")
	}
	if !f.r.mudTiles[built] || !f.r.mudTiles[far] {
		t.Fatal("one regrown tree reclaimed more than its nearest open tile")
	}
	if f.r.sectorChops[sk] != 2 {
		t.Fatalf("pressure after a regrowth = %d, want 2", f.r.sectorChops[sk])
	}
	if m := f.lastOfType("regrow"); m == nil {
		t.Fatal("no regrow frame for the reclaimed tile")
	}
}

// The farm loop end to end: craft and build a plot, plant wheat, watch it grow
// through the sim tick, harvest it, and bake the grain at a campfire.
func TestFarmLoopEndToEnd(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 4)
	f.stand(gx, gy, 0)
	f.p.HP = 1 << 20 // the world's creatures roam while it grows
	f.p.Inv["wood"], f.p.Inv["fiber"] = 20, 20

	f.r.handleCraft(f.p, map[string]any{"t": "craft", "r": "farmplot"})
	if f.p.Inv["farmplot"] != 1 {
		t.Fatalf("crafting a farm plot gave %d", f.p.Inv["farmplot"])
	}
	plot := ti(gx+1, gy)
	f.r.handleBuild(f.p, map[string]any{"t": "build", "kind": "farmplot", "i": float64(plot)})
	if s := f.r.structures[plot]; s == nil || s.Kind != "farmplot" {
		t.Fatal("the farm plot was not built")
	}
	f.r.handlePlant(f.p, map[string]any{"t": "plant", "i": float64(plot), "crop": "wheat"})
	if f.r.farms[plot] == nil {
		t.Fatal("wheat was not planted")
	}
	f.drain()

	// grow it through the real sim tick: stages 1 and 2 are announced as reached
	gt := int64(f.r.growTicks(f.r.defs.Crops["wheat"]))
	stages := map[float64]bool{}
	for f.r.tickN < f.r.farms[plot].PlantedTick+gt+100 {
		f.r.tickN++
		f.r.onSimTick()
		for _, m := range f.drain() {
			if m["t"] == "crop" && m["i"] == float64(plot) {
				stages[m["stage"].(float64)] = true
			}
		}
	}
	if !stages[1] || !stages[2] {
		t.Fatalf("growth stages announced: %v, want 1 and 2", stages)
	}

	f.stand(gx, gy, 0)
	f.r.handleHarvest(f.p, map[string]any{"t": "harvest", "i": float64(plot)})
	want := f.r.defs.Crops["wheat"].Yield["grain"]
	if f.p.Inv["grain"] != want || f.r.farms[plot] != nil {
		t.Fatalf("harvest: grain=%d (want %d), farm left=%v", f.p.Inv["grain"], want, f.r.farms[plot] != nil)
	}
	// the plot stays and takes a new crop
	f.r.handlePlant(f.p, map[string]any{"t": "plant", "i": float64(plot), "crop": "wheat"})
	if f.r.farms[plot] == nil {
		t.Fatal("the plot would not take a second crop")
	}

	// grain bakes into bread at a campfire
	f.r.structures[ti(gx-1, gy)] = &Structure{Kind: "campfire", HP: 10, Lvl: 1}
	f.r.handleCraft(f.p, map[string]any{"t": "craft", "r": "bread"})
	if f.p.Inv["bread"] != 1 || f.p.Inv["grain"] != want-f.r.defs.Recipes["bread"].Cost["grain"] {
		t.Fatalf("bread craft: bread=%d grain=%d", f.p.Inv["bread"], f.p.Inv["grain"])
	}
}

// A hunter whose path crosses a field walks over the plots; it used to gnaw
// each one it stepped on down to nothing, taking the crop with it.
func TestCreatureWalksOverFarmPlot(t *testing.T) {
	f := newFixture(t)
	f.p.HP = 1 << 20
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.p.X, f.p.Y = gx+4, gy
	var plots []int
	for dy := -1; dy <= 1; dy++ {
		i := ti(gx, gy+float64(dy))
		f.r.structures[i] = &Structure{Kind: "farmplot", HP: 10, Lvl: 1}
		f.r.farms[i] = &Farm{Crop: "wheat", lastStage: -1}
		plots = append(plots, i)
	}
	c := f.spawnAt("crawler", gx-4, gy, true)
	f.run(c, 60)
	for _, i := range plots {
		if s := f.r.structures[i]; s == nil || s.HP != 10 || f.r.farms[i] == nil {
			t.Fatalf("a crawler crossing the field damaged a plot: %+v crop=%v", s, f.r.farms[i] != nil)
		}
	}
	if c.X < gx+1 {
		t.Fatalf("the crawler never crossed the field (x %.1f, plots at %.1f)", c.X, gx)
	}
}
