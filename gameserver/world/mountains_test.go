package world

import (
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

var mtnWorld *World
var mtnStamp *MountainStamp

func mountainFixture(t *testing.T) (*World, *MountainStamp) {
	t.Helper()
	if mtnWorld == nil {
		mtnWorld = GenWorld("hearth-1")
		mtnStamp = BuildMountains(mtnWorld, "hearth-1")
	}
	return mtnWorld, mtnStamp
}

// stamped returns a private copy of w with the stamp applied.
func stamped(w *World, s *MountainStamp) *World {
	c := copyLayers(w)
	s.Apply(c)
	return c
}

func copyLayers(w *World) *World {
	c := *w
	c.Elev = append([]uint8(nil), w.Elev...)
	c.TileVis = append([]uint8(nil), w.TileVis...)
	c.Nodes = make(map[int]uint8, len(w.Nodes))
	for k, v := range w.Nodes {
		c.Nodes[k] = v
	}
	c.Decor = make(map[int]string, len(w.Decor))
	for k, v := range w.Decor {
		c.Decor[k] = v
	}
	return &c
}

func TestMountainStampIsDeterministic(t *testing.T) {
	w, s := mountainFixture(t)
	again := BuildMountains(w, "hearth-1")
	if !reflect.DeepEqual(s, again) {
		t.Fatal("BuildMountains gave a different stamp for the same world and seed")
	}
	if len(s.Peaks) != len(Mountains) {
		t.Fatalf("built %d mountains, want one per MOUNTAINS site (%d)", len(s.Peaks), len(Mountains))
	}
	// and BuildMountains never writes the world it reads
	if got := sha(w.Elev); got != hearth1["elev"] {
		t.Fatal("BuildMountains wrote the base world's elevation")
	}
	if got := sha(w.TileVis); got != hearth1["tileVis"] {
		t.Fatal("BuildMountains wrote the base world's tileVis")
	}
	if len(w.Nodes) != hearth1Nodes || len(w.Decor) != hearth1Decor {
		t.Fatal("BuildMountains wrote the base world's nodes or decor")
	}
}

func TestMountainsAvoidReservedGroundAndWater(t *testing.T) {
	w, s := mountainFixture(t)
	reserved := mountainReserved(w)
	for _, c := range s.Changes {
		x, y := c.I%SIZE, c.I/SIZE
		if w.Tiles[c.I] == TWater {
			t.Fatalf("mountain tile (%d,%d) is water", x, y)
		}
		if reserved(x, y) {
			t.Fatalf("mountain tile (%d,%d) is reserved ground", x, y)
		}
		// every massif keeps at least a tile of ordinary ground between it and the water
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if w.Tiles[(y+d[1])*SIZE+x+d[0]] == TWater {
				t.Fatalf("mountain tile (%d,%d) touches water", x, y)
			}
		}
		if LandmarkBlock[c.I] && !MountainBlock[c.I] {
			t.Fatalf("mountain tile (%d,%d) covers a temple piece", x, y)
		}
		for _, cl := range Clearings {
			if math.Hypot(float64(x-cl.X), float64(y-cl.Y)) <= float64(max(cl.Free, cl.Land)) {
				t.Fatalf("mountain tile (%d,%d) is inside clearing %s", x, y, cl.ID)
			}
		}
		if c.Elev > MaxElev {
			t.Fatalf("mountain tile (%d,%d) is %d levels, over MaxElev", x, y, c.Elev)
		}
	}
	// the tiles the room's spawn, medics and camps are found on are unchanged
	sw := stamped(w, s)
	if FindSpawn(sw) != FindSpawn(w) {
		t.Fatal("the stamp moved the spawn")
	}
	m0, _ := FindMedicSpawns(w)
	m1, _ := FindMedicSpawns(sw)
	if !reflect.DeepEqual(m0, m1) {
		t.Fatal("the stamp moved a medic")
	}
	c0, c1 := FindFolkCamps(w, nil), FindFolkCamps(sw, nil)
	if !reflect.DeepEqual(c0, c1) {
		t.Fatal("the stamp moved a folk camp")
	}
	for _, c := range c1 {
		for _, i := range CampBlockTiles(c) {
			if s.Mark[i] {
				t.Fatalf("camp %s stands on a mountain tile", c.ID)
			}
		}
	}
}

// climbable is the movement rule the trail is built for: a 4-connected step
// up of at most one level, any drop, never into water.
func reachable(w *World, from int, inside func(int) bool) map[int]bool {
	seen := map[int]bool{from: true}
	q := []int{from}
	for len(q) > 0 {
		i := q[0]
		q = q[1:]
		x, y := i%SIZE, i/SIZE
		for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			nx, ny := x+d[0], y+d[1]
			j := ny*SIZE + nx
			if nx < 0 || ny < 0 || nx >= SIZE || ny >= SIZE || seen[j] || !inside(j) || w.Tiles[j] == TWater {
				continue
			}
			if int(w.Elev[j])-int(w.Elev[i]) > 1 {
				continue
			}
			seen[j] = true
			q = append(q, j)
		}
	}
	return seen
}

func TestMountainTrailReachesTheSummit(t *testing.T) {
	w, s := mountainFixture(t)
	sw := stamped(w, s)
	for _, p := range s.Peaks {
		if len(p.Trail) < 2 {
			t.Fatalf("%s has no trail", p.Key)
		}
		foot, top := p.Trail[0], p.Trail[len(p.Trail)-1]
		if s.Mark[foot] && sw.Elev[foot] > p.Base+1 {
			t.Fatalf("%s: the trail starts %d levels up", p.Key, sw.Elev[foot])
		}
		if top != p.Summit[1]*SIZE+p.Summit[0] && sw.Elev[top] != p.Top {
			t.Fatalf("%s: the trail ends at level %d, the summit is %d", p.Key, sw.Elev[top], p.Top)
		}
		for k := 1; k < len(p.Trail); k++ {
			a, b := p.Trail[k-1], p.Trail[k]
			if d := abs(a%SIZE-b%SIZE) + abs(a/SIZE-b/SIZE); d != 1 {
				t.Fatalf("%s: trail step %d is not 4-connected", p.Key, k)
			}
			if rise := int(sw.Elev[b]) - int(sw.Elev[a]); rise > 1 || rise < 0 {
				t.Fatalf("%s: trail step %d changes %d levels", p.Key, k, rise)
			}
		}
		// and the movement rule itself, from the foot over the whole map round the
		// massif, reaches the summit — the trail is not just a list, it is walkable
		box := func(i int) bool {
			x, y := i%SIZE, i/SIZE
			return x >= p.X0-6 && x <= p.X1+6 && y >= p.Y0-6 && y <= p.Y1+6
		}
		if !reachable(sw, foot, box)[top] {
			t.Fatalf("%s: a +1 climb from the foot does not reach the summit", p.Key)
		}
		if len(p.Trail) < int(p.Top-p.Base) {
			t.Fatalf("%s: a %d-tile trail cannot climb %d levels at one a tile", p.Key, len(p.Trail), p.Top-p.Base)
		}
		// it winds right round the summit on the way up: the bearing from the
		// summit, unwrapped along the trail, turns through a full circle
		turn, last := 0.0, math.NaN()
		for _, i := range p.Trail[:len(p.Trail)-1] {
			a := math.Atan2(float64(i/SIZE-p.Summit[1]), float64(i%SIZE-p.Summit[0]))
			if !math.IsNaN(last) {
				turn += math.Remainder(a-last, 2*math.Pi)
			}
			last = a
		}
		if math.Abs(turn) < 2*math.Pi-1e-9 {
			t.Fatalf("%s: the trail winds only %.2f turns round the summit", p.Key, math.Abs(turn)/(2*math.Pi))
		}
	}
}

// mountainForms: each island's mountain rises the height its form is built
// for over its local ground, and its rock stays within a compact core round
// the summit.
var mountainForms = [5]struct {
	lo, hi int     // rise of the summit over the base, levels
	core   float64 // the farthest rock from the summit, tiles
}{
	{20, 24, 11}, // the Woods: a snow-capped massif, ~+22
	{14, 16, 10}, // the Dunes: a sheer butte
	{26, 28, 11}, // the Spire, the tallest
	{12, 14, 9},  // the Marsh: a craggy tor
	{8, 10, 6},   // the Core: an obsidian spike
}

func TestMountainHeightsAndForms(t *testing.T) {
	w, s := mountainFixture(t)
	sw := stamped(w, s)
	for _, p := range s.Peaks {
		f := mountainForms[p.Island]
		if rise := int(p.Top) - int(p.Base); rise < f.lo || rise > f.hi {
			t.Fatalf("%s rises %d levels, want %d..%d", p.Key, rise, f.lo, f.hi)
		}
		if p.Top > MaxElev {
			t.Fatalf("%s: summit at %d, over MaxElev", p.Key, p.Top)
		}
		// one summit of 2-4 tiles: the tiles at the top level, all joined to the
		// summit tile, and nothing else of the massif up there
		si := p.Summit[1]*SIZE + p.Summit[0]
		at := 0
		for y := p.Y0; y <= p.Y1; y++ {
			for x := p.X0; x <= p.X1; x++ {
				if i := y*SIZE + x; s.Mark[i] && sw.Elev[i] >= p.Top {
					at++
				}
			}
		}
		top := reachable(sw, si, func(i int) bool { return s.Mark[i] && sw.Elev[i] == p.Top })
		if at < 2 || at > 4 || len(top) != at {
			t.Fatalf("%s: %d tiles at the summit's level (%d joined to it), want one summit of 2-4", p.Key, at, len(top))
		}
		for i := range s.Face {
			if x, y := i%SIZE, i/SIZE; x >= p.X0 && x <= p.X1 && y >= p.Y0 && y <= p.Y1 {
				if d := math.Hypot(float64(x-p.Summit[0]), float64(y-p.Summit[1])); d > f.core {
					t.Fatalf("%s: rock at (%d,%d), %.1f tiles from the summit (the core is %.0f)", p.Key, x, y, d, f.core)
				}
			}
		}
	}
}

// TestMountainRockStartsAboveTheFoot: the lower slopes are the island's own
// ground — no rock, ledge or snow cap within rockLine levels of the base — and
// no rock stands alone: every rock tile has rock or the trail round it.
func TestMountainRockStartsAboveTheFoot(t *testing.T) {
	w, s := mountainFixture(t)
	sw := stamped(w, s)
	base := map[int]uint8{}
	for _, p := range s.Peaks {
		for y := p.Y0; y <= p.Y1; y++ {
			for x := p.X0; x <= p.X1; x++ {
				base[y*SIZE+x] = p.Base
			}
		}
	}
	rocky := func(i int) bool { v := sw.TileVis[i]; return s.Mark[i] && v >= VisRock && v <= VisSnowLedge }
	for _, c := range s.Changes {
		if !rocky(c.I) {
			continue
		}
		x, y := c.I%SIZE, c.I/SIZE
		if int(c.Elev)-int(base[c.I]) <= rockLine {
			t.Fatalf("rock at (%d,%d) only %d levels over the base", x, y, int(c.Elev)-int(base[c.I]))
		}
		k := 0
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if j := (y+dy)*SIZE + x + dx; (dx != 0 || dy != 0) && s.Mark[j] && (rocky(j) || sw.TileVis[j] == VisTrail) {
					k++
				}
			}
		}
		if k < 2 {
			t.Fatalf("a lone rock tile at (%d,%d)", x, y)
		}
	}
}

func TestMountainsHaveSteepFaces(t *testing.T) {
	w, s := mountainFixture(t)
	sw := stamped(w, s)
	for _, p := range s.Peaks {
		two, three, worst := 0, 0, 0
		for y := p.Y0; y <= p.Y1; y++ {
			for x := p.X0; x <= p.X1; x++ {
				i := y*SIZE + x
				if !s.Mark[i] {
					continue
				}
				for _, j := range []int{i + 1, i + SIZE} {
					if sw.Tiles[j] == TWater {
						continue
					}
					d := abs(int(sw.Elev[i]) - int(sw.Elev[j]))
					if d > maxStep {
						t.Logf("%s: %d-level face between (%d,%d) %d and (%d,%d) %d", p.Key, d, x, y, sw.Elev[i], j%SIZE, j/SIZE, sw.Elev[j])
					}
					worst = max(worst, d)
					if d == 2 {
						two++
					} else if d >= 3 {
						three++
					}
				}
			}
		}
		need := 20 // the Core's crag is small: a handful of faces is all it has room for
		if p.Island == 4 {
			need = 8
		}
		if two+three < need || three < 5 {
			t.Fatalf("%s: only %d two-level and %d three-level faces", p.Key, two, three)
		}
		if worst > maxStep {
			t.Fatalf("%s: a %d-level drop between neighbours (faces should be %d at most)", p.Key, worst, maxStep)
		}
		// so it cannot be walked up just anywhere: plain +1 walking from the foot,
		// away from the trail, does not reach the summit
		trail := map[int]bool{}
		for _, i := range p.Trail {
			trail[i] = true
		}
		inside := func(i int) bool {
			x, y := i%SIZE, i/SIZE
			return !trail[i] && x >= p.X0-6 && x <= p.X1+6 && y >= p.Y0-6 && y <= p.Y1+6
		}
		foot := (p.Y1+3)*SIZE + (p.X0+p.X1)/2
		for sw.Tiles[foot] == TWater || s.Mark[foot] {
			foot += SIZE
		}
		if p.Island != 4 && reachable(sw, foot, inside)[p.Summit[1]*SIZE+p.Summit[0]] {
			t.Logf("%s: the summit is also reachable off the trail (a gentle flank)", p.Key)
		}
	}
}

func TestMountainRevertRestoresTheBaseWorld(t *testing.T) {
	w, s := mountainFixture(t)
	c := copyLayers(w)
	s.Apply(c)
	if sha(c.Elev) == hearth1["elev"] {
		t.Fatal("applying the stamp changed no elevation")
	}
	s.Revert(c)
	if sha(c.Elev) != hearth1["elev"] || sha(c.TileVis) != hearth1["tileVis"] {
		t.Fatal("reverting the stamp did not restore elev / tileVis byte for byte")
	}
	if !reflect.DeepEqual(c.Nodes, w.Nodes) || !reflect.DeepEqual(c.Decor, w.Decor) {
		t.Fatal("reverting the stamp did not restore the nodes and decor")
	}
	// and again: stamp, lift, stamp is the same as one stamp
	s.Apply(c)
	s.Revert(c)
	s.Apply(c)
	if !reflect.DeepEqual(c.Elev, stamped(w, s).Elev) {
		t.Fatal("re-stamping does not give the same world")
	}
}

func TestMountainNodesStayOffFacesAndTrail(t *testing.T) {
	w, s := mountainFixture(t)
	trail := map[int]bool{}
	for _, p := range s.Peaks {
		for _, i := range p.Trail {
			trail[i] = true
		}
	}
	sw := stamped(w, s)
	for i := range s.Mark {
		if _, ok := sw.Nodes[i]; ok && (s.Face[i] || trail[i]) {
			t.Fatalf("a node stands on a mountain face or the trail at (%d,%d)", i%SIZE, i/SIZE)
		}
		if _, ok := sw.Decor[i]; ok && (s.Face[i] || trail[i]) {
			t.Fatalf("decor left on a mountain face or the trail at (%d,%d)", i%SIZE, i/SIZE)
		}
	}
	if len(s.Nodes) == 0 {
		t.Fatal("the mountains carry no boulders, stones or trees at all")
	}
}

// TestMountainRenders writes a top-down heightmap of every massif when
// HEARTH_MOUNTAIN_PNG names a directory: colour by level, hillshaded, faces of
// 2 levels outlined grey and 3+ black, the trail in orange, water blue.
//
//	HEARTH_MOUNTAIN_PNG=/tmp/mtn go test ./world -run TestMountainRenders
func TestMountainRenders(t *testing.T) {
	dir := os.Getenv("HEARTH_MOUNTAIN_PNG")
	if dir == "" {
		t.Skip("set HEARTH_MOUNTAIN_PNG to a directory to render the mountains")
	}
	w, s := mountainFixture(t)
	sw := stamped(w, s)
	const px = 10
	// a hypsometric ramp over 0..MaxElev: greens, ochres, browns, greys, white
	stops := []struct {
		at float64
		c  color.RGBA
	}{{0, color.RGBA{70, 110, 60, 255}}, {3, color.RGBA{128, 160, 86, 255}}, {8, color.RGBA{176, 160, 104, 255}},
		{15, color.RGBA{150, 112, 92, 255}}, {22, color.RGBA{140, 128, 124, 255}}, {28, color.RGBA{200, 198, 196, 255}},
		{MaxElev, color.RGBA{248, 248, 250, 255}}}
	ramp := make([]color.RGBA, MaxElev+1)
	for e := range ramp {
		k := 1
		for k < len(stops)-1 && float64(e) > stops[k].at {
			k++
		}
		a, b := stops[k-1], stops[k]
		f := math.Max(0, math.Min(1, (float64(e)-a.at)/(b.at-a.at)))
		mix := func(p, q uint8) uint8 { return uint8(float64(p) + (float64(q)-float64(p))*f) }
		ramp[e] = color.RGBA{mix(a.c.R, b.c.R), mix(a.c.G, b.c.G), mix(a.c.B, b.c.B), 255}
	}
	for _, p := range s.Peaks {
		x0, y0, x1, y1 := p.X0-8, p.Y0-8, p.X1+8, p.Y1+8
		W, H := (x1-x0+1)*px, (y1-y0+1)*px
		img := image.NewRGBA(image.Rect(0, 0, W, H))
		trail := map[int]bool{}
		for _, i := range p.Trail {
			trail[i] = true
		}
		el := func(x, y int) float64 { return float64(sw.Elev[y*SIZE+x]) }
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				i := y*SIZE + x
				var c color.RGBA
				if sw.Tiles[i] == TWater {
					c = color.RGBA{50, 90, 160, 255}
				} else {
					c = ramp[min(len(ramp)-1, int(sw.Elev[i]))]
					// light from the north-west
					sh := 1 + 0.09*((el(x-1, y)-el(x+1, y))+(el(x, y-1)-el(x, y+1)))
					sh = math.Max(0.55, math.Min(1.35, sh))
					c = color.RGBA{uint8(math.Min(255, float64(c.R)*sh)), uint8(math.Min(255, float64(c.G)*sh)), uint8(math.Min(255, float64(c.B)*sh)), 255}
					if s.Face[i] {
						c = color.RGBA{uint8(int(c.R) * 3 / 4), uint8(int(c.G) * 3 / 4), uint8(int(c.B) * 3 / 4), 255}
					}
					if trail[i] {
						c = color.RGBA{235, 120, 30, 255}
					}
				}
				for py := 0; py < px; py++ {
					for pxx := 0; pxx < px; pxx++ {
						img.Set((x-x0)*px+pxx, (y-y0)*px+py, c)
					}
				}
				if _, ok := sw.Nodes[i]; ok && s.Mark[i] {
					for k := 3; k < 7; k++ {
						img.Set((x-x0)*px+k, (y-y0)*px+5, color.RGBA{30, 30, 30, 255})
						img.Set((x-x0)*px+5, (y-y0)*px+k, color.RGBA{30, 30, 30, 255})
					}
				}
			}
		}
		// faces: the edge between two tiles, by how far apart they are
		edge := func(d int) (color.RGBA, bool) {
			switch {
			case d >= 3:
				return color.RGBA{0, 0, 0, 255}, true
			case d == 2:
				return color.RGBA{90, 90, 90, 255}, true
			}
			return color.RGBA{}, false
		}
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				i := y*SIZE + x
				if sw.Tiles[i] == TWater {
					continue
				}
				if sw.Tiles[i+1] != TWater {
					if c, ok := edge(abs(int(sw.Elev[i]) - int(sw.Elev[i+1]))); ok {
						for k := 0; k < px; k++ {
							img.Set((x-x0+1)*px-1, (y-y0)*px+k, c)
							img.Set((x-x0+1)*px, (y-y0)*px+k, c)
						}
					}
				}
				if sw.Tiles[i+SIZE] != TWater {
					if c, ok := edge(abs(int(sw.Elev[i]) - int(sw.Elev[i+SIZE]))); ok {
						for k := 0; k < px; k++ {
							img.Set((x-x0)*px+k, (y-y0+1)*px-1, c)
							img.Set((x-x0)*px+k, (y-y0+1)*px, c)
						}
					}
				}
			}
		}
		// the levels in base 36 (ordinary ground off the mountain as .,:;), then
		// the same window again with the trail as its step number mod 10, the
		// summit's level tiles as ^, rock as #, ledges as =, the lower slopes as -
		const digits = "0123456789abcdefghijklmnopqrstuvwxyz"
		step := map[int]int{}
		for k, i := range p.Trail {
			step[i] = k
		}
		var txt, ovl []byte
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				i := y*SIZE + x
				ch, o := digits[min(35, int(sw.Elev[i]))], byte(' ')
				switch {
				case sw.Tiles[i] == TWater:
					ch, o = '~', '~'
				case !s.Mark[i]:
					ch = ".,:;"[min(3, int(sw.Elev[i]))]
				}
				if s.Mark[i] {
					switch sw.TileVis[i] {
					case VisRock, VisSnow:
						o = '#'
					case VisLedge, VisSnowLedge:
						o = '='
					default:
						o = '-'
					}
				}
				if sw.Elev[i] == p.Top && s.Mark[i] {
					o = '^'
				}
				if k, ok := step[i]; ok {
					o = byte('0' + k%10)
				}
				txt, ovl = append(txt, ch), append(ovl, o)
			}
			txt, ovl = append(txt, '\n'), append(ovl, '\n')
		}
		txt = append(append(txt, '\n'), ovl...)
		os.WriteFile(filepath.Join(dir, p.Key+".txt"), txt, 0o644)
		// the ground the massif had to work with: water ~, reserved ground R (the
		// clearings, monoliths, temple pieces and medics with their margins), the
		// base levels elsewhere, and the MOUNTAINS site as @
		reserved := mountainReserved(w)
		var ctx []byte
		for y := p.Site[1] - 26; y <= p.Site[1]+26; y++ {
			for x := p.Site[0] - 26; x <= p.Site[0]+26; x++ {
				i := y*SIZE + x
				ch := byte("0123456789"[min(9, int(w.Elev[i]))])
				switch {
				case x == p.Site[0] && y == p.Site[1]:
					ch = '@'
				case w.Tiles[i] == TWater:
					ch = '~'
				case reserved(x, y) || (LandmarkBlock[i] && !MountainBlock[i]):
					ch = 'R'
				}
				ctx = append(ctx, ch)
			}
			ctx = append(ctx, '\n')
		}
		os.WriteFile(filepath.Join(dir, p.Key+"-ctx.txt"), ctx, 0o644)
		// and the region as data, for an iso preview drawn with the real tile art
		region := map[string]any{"x0": x0, "y0": y0, "w": x1 - x0 + 1, "h": y1 - y0 + 1, "island": p.Island}
		var tl, els, vis []int
		var nodes [][2]int
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				i := y*SIZE + x
				tl, els, vis = append(tl, int(sw.Tiles[i])), append(els, int(sw.Elev[i])), append(vis, int(sw.TileVis[i]))
				if k, ok := sw.Nodes[i]; ok {
					nodes = append(nodes, [2]int{(y-y0)*(x1-x0+1) + x - x0, int(k)})
				}
			}
		}
		region["tiles"], region["elev"], region["vis"], region["nodes"] = tl, els, vis, nodes
		if b, err := json.Marshal(region); err == nil {
			os.WriteFile(filepath.Join(dir, p.Key+".json"), b, 0o644)
		}
		f, err := os.Create(filepath.Join(dir, fmt.Sprintf("%s.png", p.Key)))
		if err != nil {
			t.Fatal(err)
		}
		if err := png.Encode(f, img); err != nil {
			t.Fatal(err)
		}
		f.Close()
		count := map[uint8]int{}
		keep := 0
		for _, c := range s.Changes {
			if x, y := c.I%SIZE, c.I/SIZE; x >= p.X0 && x <= p.X1 && y >= p.Y0 && y <= p.Y1 {
				count[c.Vis]++
				if c.Keep {
					keep++
				}
			}
		}
		// the steep core's radius: the farthest rock from the summit
		core := 0.0
		for _, c := range s.Changes {
			if x, y := c.I%SIZE, c.I/SIZE; !c.Keep && c.Vis != VisTrail && x >= p.X0 && x <= p.X1 && y >= p.Y0 && y <= p.Y1 {
				core = math.Max(core, math.Hypot(float64(x-p.Summit[0]), float64(y-p.Summit[1])))
			}
		}
		t.Logf("%s: base %d top %d (+%d) summit %v trail %d tiles %.2f turns, rock radius %.1f, bbox %d..%d x %d..%d, vis %v (%d lower slope)",
			p.Key, p.Base, p.Top, p.Top-p.Base, p.Summit, len(p.Trail), p.Turns, core, p.X0, p.X1, p.Y0, p.Y1, count, keep)
	}
}
