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
		if p.Top < p.Base+4 {
			t.Fatalf("%s rises only %d levels", p.Key, p.Top-p.Base)
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
		if worst > maxStep+1 {
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
	ramp := []color.RGBA{
		{70, 110, 60, 255}, {98, 138, 74, 255}, {128, 160, 86, 255}, {160, 170, 96, 255},
		{176, 160, 104, 255}, {170, 140, 100, 255}, {158, 120, 92, 255}, {146, 108, 90, 255},
		{140, 112, 104, 255}, {150, 136, 132, 255}, {176, 170, 168, 255}, {210, 208, 206, 255}, {245, 245, 248, 255},
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
		var txt []byte
		for y := y0; y <= y1; y++ {
			for x := x0; x <= x1; x++ {
				i := y*SIZE + x
				ch := byte("0123456789abcdef"[min(15, int(sw.Elev[i]))])
				switch {
				case sw.Tiles[i] == TWater:
					ch = '~'
				case trail[i]:
					ch = "ABCDEFGHIJKLMNOP"[min(15, int(sw.Elev[i]))]
				case !s.Mark[i]:
					ch = ".,:;"[min(3, int(sw.Elev[i]))]
				}
				txt = append(txt, ch)
			}
			txt = append(txt, '\n')
		}
		os.WriteFile(filepath.Join(dir, p.Key+".txt"), txt, 0o644)
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
		t.Logf("%s: base %d top %d summit %v trail %d tiles, bbox %d..%d x %d..%d, vis %v (%d foothill)", p.Key, p.Base, p.Top, p.Summit, len(p.Trail), p.X0, p.X1, p.Y0, p.Y1, count, keep)
	}
}
