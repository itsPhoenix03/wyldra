package world

import (
	"fmt"
	"math"
	"slices"
	"sort"
)

// Literal port of shared/world.js. Ordering, comparison thresholds and
// arithmetic are kept exactly as written in the JS; the only structural change
// is that JS Maps/Sets become Go maps whose iteration is never used to drive
// generation (only lookups), and every ordered output is produced by sorting.
//
// Every product that feeds an addition is wrapped in float64(...) so the
// compiler may not fuse it into an FMA (the JS rounds each step). The per-box
// scratch buffers are shared by every pass exactly as the JS shares them, so a
// value one island leaves behind is the value the next one sees.

const (
	WorldVersion = 5
	SIZE         = 1280
)

// Tile types (shared/world.js T).
const (
	TGrass  uint8 = 0
	TSand   uint8 = 1
	TSnow   uint8 = 2
	TMud    uint8 = 3
	TWater  uint8 = 4
	TBlight uint8 = 5
)

// Node kinds (shared/defs.js NODE).
const (
	NodeTree      uint8 = 0
	NodeBoulder   uint8 = 1
	NodeBush      uint8 = 2
	NodeStone     uint8 = 3
	NodeCrystal   uint8 = 4
	NodeStarmetal uint8 = 5
)

// TileKeys mirrors TILE_KEYS.
var TileKeys = []string{"grass", "sand", "snow", "mud", "water", "blight"}

// ISLES: Woods, Dunes, Spire, Marsh.
var ISLES = [4][2]int{{180, 180}, {1100, 180}, {180, 1100}, {1100, 1100}}

// ISLE_R is the base major-island radius.
const ISLE_R = 70

// MONOLITHS aliases ISLES in the JS.
var MONOLITHS = ISLES

// CORE is the world centre.
var CORE = [2]int{640, 640}

// MajorIsland mirrors an entry of MAJOR_ISLANDS.
type MajorIsland struct {
	ID     string
	Center [2]int
	Tile   uint8
}

// MajorIslands mirrors MAJOR_ISLANDS.
var MajorIslands = []MajorIsland{
	{"woods", ISLES[0], TGrass},
	{"dunes", ISLES[1], TSand},
	{"spire", ISLES[2], TSnow},
	{"marsh", ISLES[3], TMud},
}

// MedicIsland mirrors an entry of MEDIC_ISLANDS.
type MedicIsland struct {
	IslandID string
	Sprite   string
}

// MedicIslands mirrors MEDIC_ISLANDS.
var MedicIslands = []MedicIsland{
	{"woods", "medic"},
	{"spire", "medic_snow"},
}

var medicAnchorOffsets = map[string][2]int{
	"woods": {10, -8},
	"spire": {10, 8},
}

// ACTIVATION_I is CORE[1] * SIZE + CORE[0].
var ACTIVATION_I = CORE[1]*SIZE + CORE[0]

// MinorIsle mirrors an entry of MINOR_ISLES.
type MinorIsle struct {
	X, Y, R int
	Kind    string
}

// MinorIsles mirrors MINOR_ISLES.
var MinorIsles = []MinorIsle{
	{640, 180, 14, "rock"},
	{640, 1100, 13, "sandbar"},
	{180, 640, 13, "driftwood"},
	{1100, 640, 14, "ruin"},
	{420, 420, 11, "rock"},
	{860, 420, 11, "ruin"},
	{420, 860, 12, "icefloe"},
	{860, 860, 11, "blightshard"},
	{340, 180, 9, "sandbar"},
	{180, 940, 10, "icefloe"},
	{940, 1100, 10, "driftwood"},
	{1100, 340, 9, "rock"},
}

// Mountain mirrors an entry of MOUNTAINS.
type Mountain struct {
	X, Y int
	Key  string
}

// Mountains mirrors MOUNTAINS.
var Mountains = []Mountain{
	{150, 150, "mountain_woods"},
	{1130, 150, "mountain_dunes"},
	{150, 1130, "mountain_spire"},
	{1130, 1130, "mountain_marsh"},
	{640, 630, "mountain_core_obsidian"},
}

// TemplePiece mirrors an entry of TEMPLE_PIECES.
type TemplePiece struct {
	I   int
	Key string
}

// TemplePieces mirrors TEMPLE_PIECES.
var TemplePieces = []TemplePiece{
	{ACTIVATION_I, "core_activation_dais"},
	{(CORE[1]-5)*SIZE + CORE[0], "core_temple_ruins"},
	{(CORE[1]+1)*SIZE + CORE[0] - 5, "core_temple_arch_ruin"},
	{(CORE[1]+1)*SIZE + CORE[0] + 5, "core_temple_pillar_broken"},
	{(CORE[1]+5)*SIZE + CORE[0] - 3, "core_temple_pillar_broken"},
}

// LandmarkBlock holds tile indices blocked by mountains (3x3 footprint) and
// temple pieces other than the dais.
var LandmarkBlock = func() map[int]bool {
	s := map[int]bool{}
	for _, m := range Mountains {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				s[(m.Y+dy)*SIZE+(m.X+dx)] = true
			}
		}
	}
	for p := 1; p < len(TemplePieces); p++ {
		s[TemplePieces[p].I] = true
	}
	return s
}()

// Clearing mirrors an entry of CLEARINGS: flat, guaranteed-land, node- and
// decor-free ground for what the server and client later stand on the
// terrain. K is the island (ISLES index), Lvl the terrace level it is levelled
// to. Flat: radius levelled exactly; Blend: radius by which it has eased back
// into the natural terrain (0 = not levelled); Free: radius kept clear of nodes
// and decor; Land: radius that is always island ground and never lake.
type Clearing struct {
	ID                      string
	X, Y, K, Lvl            int
	Flat, Blend, Free, Land int
}

// villageFireOff (woods, dunes, spire, marsh) mirrors VILLAGE_FIRE_OFF: the
// fire seek points of folk.go folkVillageDefs (island centre + off) — move
// them together.
var villageFireOff = [4][2]int{{-8, 7}, {-8, -9}, {4, 12}, {9, -9}}

// plazaLvl is the level each island's plaza, medic and village are levelled to.
var plazaLvl = [4]int{2, 1, 3, 1}

// Clearings mirrors CLEARINGS, in the same order (the order the clearings are
// eased flat in): per island its monolith plaza, lore note and village, then
// the spawn search point (FindSpawn) and the medic anchors.
var Clearings = func() []Clearing {
	var out []Clearing
	for k, c := range ISLES {
		cx, cy := c[0], c[1]
		id, lvl, f := MajorIslands[k].ID, plazaLvl[k], villageFireOff[k]
		out = append(out,
			Clearing{id + "-monolith", cx, cy, k, lvl, 4, 10, 4, 11},
			Clearing{id + "-note", cx + 7, cy + 4, k, lvl, 0, 0, 1, 2},
			Clearing{id + "-village", cx + f[0], cy + f[1], k, lvl, 6, 12, 7, 10})
	}
	out = append(out, Clearing{"spawn", ISLES[0][0] - 12, ISLES[0][1] - 12, 0, plazaLvl[0], 3, 8, 3, 6})
	for _, mi := range MedicIslands {
		k := slices.IndexFunc(MajorIslands, func(m MajorIsland) bool { return m.ID == mi.IslandID })
		off := medicAnchorOffsets[mi.IslandID]
		out = append(out, Clearing{mi.IslandID + "-medic", ISLES[k][0] + off[0], ISLES[k][1] + off[1], k, plazaLvl[k], 3, 7, 3, 6})
	}
	return out
}()

var minorKindTile = map[string]uint8{
	"sandbar":     TSand,
	"ruin":        TSand,
	"driftwood":   TGrass,
	"rock":        TGrass,
	"icefloe":     TSnow,
	"blightshard": TBlight,
}

func abs(a int) int {
	if a < 0 {
		return -a
	}
	return a
}

func nearPOI(x, y int) bool {
	for _, m := range MONOLITHS {
		if abs(m[0]-x) < 3 && abs(m[1]-y) < 3 {
			return true
		}
	}
	if abs(x-CORE[0]) < 6 && abs(y-CORE[1]) < 6 {
		return true
	}
	for dy := -2; dy <= 2; dy++ {
		for dx := -2; dx <= 2; dx++ {
			if LandmarkBlock[(y+dy)*SIZE+(x+dx)] {
				return true
			}
		}
	}
	return false
}

// =============================================================================
// World generation: the passes of shared/world.js genWorld — the four major
// islands (islandPass), the Core (coreRegion), the minor isles (minorPass),
// the mountain ground stamp, then the derived layers and the node / decor maps.
// =============================================================================

const (
	majorHalf  = 88      // half-size of a major island's box; its edge is always open sea
	landCap    = 85      // no island land this far from its centre
	baseR      = 64      // nominal coast radius before shaping
	speck      = 12      // land pieces smaller than this are removed
	lagoonFill = 60      // enclosed water left by the coastline smaller than this is filled
	far        = 1 << 20 // "no source" chamfer distance
	lavaVents  = 6       // on the Core, outside the temple clearing
	gladeEdge  = 9       // spaceGladeTrees radius
)

var (
	no             = math.Inf(-1) // pickTop: not a candidate
	isleT          = [4]uint8{TGrass, TSand, TSnow, TMud}
	spawnX, spawnY = ISLES[0][0] - 12, ISLES[0][1] - 12 // findSpawn's search point
	decorKeys      = []string{"glow_mushroom", "sand_ruin_pillar", "ice_crystal_cluster", "bone_totem", "lava_vent"}
)

// dir16: unit vectors every 22.5 degrees: 0 = east, 4 = south (y grows down),
// 8 = west, 12 = north.
var dir16 = [16][2]float64{
	{1, 0}, {0.9238795325112867, 0.3826834323650898}, {0.7071067811865476, 0.7071067811865476},
	{0.3826834323650898, 0.9238795325112867}, {0, 1}, {-0.3826834323650898, 0.9238795325112867},
	{-0.7071067811865476, 0.7071067811865476}, {-0.9238795325112867, 0.3826834323650898}, {-1, 0},
	{-0.9238795325112867, -0.3826834323650898}, {-0.7071067811865476, -0.7071067811865476},
	{-0.3826834323650898, -0.9238795325112867}, {0, -1}, {0.3826834323650898, -0.9238795325112867},
	{0.7071067811865476, -0.7071067811865476}, {0.9238795325112867, -0.3826834323650898},
}

// featDef is one coastline feature of an island plan (ISLE_PLANS feats):
//
//	bay   p = dist, radius, depth
//	pen   p = from, to, halfWidth, lift
//	fjord p = from, halfWidth, depth
//	spit  p = from, to, halfWidth, lift, turn
type featDef struct {
	kind string
	dir  int
	p    [5]float64
}

// islePlan mirrors an entry of ISLE_PLANS. lakes: dir, dist, radius, keep.
type islePlan struct {
	area                  int
	lobe, warp            float64
	oct                   [3]float64
	jag, islet, isletBand float64
	feats                 []featDef
	lakes                 [][4]float64
	minPond               int
	t1, t2                float64
}

var islePlans = [4]islePlan{
	{ // Whispering Woods
		area: 12350, lobe: 0.2, warp: 10, oct: [3]float64{0.12, 0.06, 0.02}, jag: 0, islet: 0.6, isletBand: 0.24,
		feats: []featDef{{"bay", 0, [5]float64{56, 18, 1}}, {"bay", 6, [5]float64{58, 15, 1}}, {"pen", 11, [5]float64{46, 80, 8, 0.65}}},
		lakes: [][4]float64{{3, 36, 12.5, -1}, {8, 44, 4.5, -0.3}, {13, 30, 4, 0.25}}, minPond: 14,
		t1: 0.45, t2: 0.72,
	},
	{ // Sinking Dunes
		area: 12250, lobe: 0.14, warp: 7, oct: [3]float64{0.1, 0.035, 0.01}, jag: 0, islet: 0.66, isletBand: 0.2,
		feats: []featDef{{"spit", 2, [5]float64{56, 82, 2.6, 0.75, 1}}, {"spit", 9, [5]float64{56, 82, 2.6, 0.75, -1}},
			{"bay", 5, [5]float64{60, 17, 0.9}}, {"pen", 13, [5]float64{46, 78, 9, 0.5}}},
		lakes: [][4]float64{{11, 28, 9.5, -1}, {7, 42, 4.5, -0.2}}, minPond: 8,
		t1: 0.22, t2: 0.55,
	},
	{ // Frozen Spire
		area: 12300, lobe: 0.18, warp: 9, oct: [3]float64{0.1, 0.06, 0.04}, jag: 0.2, islet: 0.58, isletBand: 0.24,
		feats: []featDef{{"fjord", 0, [5]float64{34, 3, 1}}, {"fjord", 3, [5]float64{40, 2.8, 1}}, {"fjord", 6, [5]float64{36, 3.2, 1}},
			{"fjord", 10, [5]float64{42, 2.8, 1}}, {"fjord", 13, [5]float64{38, 3, 1}}},
		lakes: [][4]float64{{8, 30, 5, -1}, {1, 24, 4.5, -0.4}, {12, 50, 4, 0.1}}, minPond: 8,
		t1: 0.4, t2: 0.8,
	},
	{ // Blighted Marsh (inland water: marshWaters)
		area: 12750, lobe: 0.16, warp: 9, oct: [3]float64{0.1, 0.08, 0.06}, jag: 0.04, islet: 0.5, isletBand: 0.3,
		feats:   []featDef{{"bay", 3, [5]float64{58, 14, 0.9}}, {"bay", 10, [5]float64{60, 12, 0.9}}},
		minPond: 10,
		t1:      0.62, t2: 0.88,
	},
}

// marshFishingPool mirrors MARSH_FISHING_POOL: the Marsh village fishes it.
var marshFishingPool = struct {
	x, y int
	r    float64
}{1118, 1082, 4}

// nodeQuota mirrors an entry of NODE_QUOTAS: the `count` best tiles by scorer.
type nodeQuota struct {
	kind   uint8
	count  int
	scorer string
}

var nodeQuotas = [4][]nodeQuota{
	{{NodeStarmetal, 5, "star"}, {NodeTree, 990, "forest"}, {NodeTree, 90, "lone"}, {NodeBush, 305, "edge"},
		{NodeStone, 60, "scarp"}, {NodeStone, 24, "spawnRock"}, {NodeStone, 110, "hill"}, {NodeStone, 25, "field"}},
	{{NodeStarmetal, 10, "star"}, {NodeBoulder, 310, "outcrop"}, {NodeStone, 269, "pebble"}},
	{{NodeCrystal, 266, "crevasse"}, {NodeBoulder, 439, "foot"}, {NodeStone, 253, "scree"}},
	{{NodeStarmetal, 13, "star"}, {NodeBush, 318, "reed"}, {NodeCrystal, 272, "bog"}},
}

var decorQuota = [4]int{69, 31, 27, 38} // per major island, decorKeys[k]

// minorStyle mirrors MINOR_STYLE: level thresholds of H and the shore band.
type minorStyle struct{ t1, t2, shore float64 }

var minorStyles = map[string]minorStyle{
	"rock":        {0.3, 0.55, 3},
	"sandbar":     {0.55, 0.9, 4},
	"driftwood":   {0.42, 0.75, 3},
	"ruin":        {0.38, 0.68, 3},
	"icefloe":     {0.4, 0.72, 3},
	"blightshard": {9, 9, 3}, // blight is always level 1
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
}

// ramp is 0 at a, 1 at b, linear between.
func ramp(v, a, b float64) float64 { return clamp01((v - a) / (b - a)) }

// grain is per-tile grain in about -1..1: field n sampled along a 3-4-5
// rotation, 1.6 noise units per tile.
func grain(n *Noise2D, x, y int, off float64) float64 {
	fx, fy := float64(x), float64(y)
	return n.Eval(float64(fx*1.28)+float64(fy*0.96)+off, float64(fy*1.28)-float64(fx*0.96)+off)
}

func b2u(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// box is a w*h window of the map; local index li = ly*w + lx.
type box struct{ cx, cy, x0, y0, w, h, n int }

func makeBox(cx, cy, half int) box {
	s := 2*half + 1
	return box{cx, cy, cx - half, cy - half, s, s, s * s}
}

// scratch mirrors the JS genWorld `C`: the noise fields, the output layers and
// the per-box scratch buffers, sized for a major island's box (the largest)
// and reused by every pass.
type scratch struct {
	elevN, veg, sct, coast, warp, shape *Noise2D

	tiles, elev, tileVis, nodeK, decorK []uint8

	land, carved, ocean, mask, noNode, el, el2, reach []uint8
	lab, queue, parent, dO, dW, dL, dT                []int32
	val, px, py, H, aux1, aux2, aux3, sortBuf         []float64
	corner                                            []int32 // levelStubs: tile corners
	cand, order                                       []int32
	score                                             []float64
}

// dirIndexOf is the dir16 index nearest the direction (dx, dy) (largest dot
// product, lowest index on a tie).
func dirIndexOf(dx, dy float64) int {
	best, bd := 0, math.Inf(-1)
	for j := 0; j < 16; j++ {
		d := float64(dir16[j][0]*dx) + float64(dir16[j][1]*dy)
		if d > bd {
			bd = d
			best = j
		}
	}
	return best
}

// segDist is the distance from (px, py) to the segment a-b, and how far along
// it the nearest point is (the JS SEG_T side channel: 0 at a, 1 at b).
func segDist(px, py, ax, ay, bx, by float64) (float64, float64) {
	ex, ey := bx-ax, by-ay
	len2 := float64(ex*ex) + float64(ey*ey)
	t := 0.0
	if len2 > 0 {
		t = clamp01((float64((px-ax)*ex) + float64((py-ay)*ey)) / len2)
	}
	return jsHypot2(px-(ax+float64(ex*t)), py-(ay+float64(ey*t))), t
}

// disc calls fn(li, x, y) for every tile of the box within radius r.
func disc(B box, x, y, r int, fn func(li, x, y int)) {
	for dy := -r; dy <= r; dy++ {
		for dx := -r; dx <= r; dx++ {
			if dx*dx+dy*dy > r*r {
				continue
			}
			lx, ly := x+dx-B.x0, y+dy-B.y0
			if lx < 0 || ly < 0 || lx >= B.w || ly >= B.h {
				continue
			}
			fn(ly*B.w+lx, x+dx, y+dy)
		}
	}
}

// components labels the 4-connected components of the tiles where
// mask[li] == val (1-based, 0 = not in the set) and returns their sizes by
// label. Seeds in tile order, neighbours W E N S.
func components(B box, mask []uint8, val uint8, lab, queue []int32) []int {
	w, h, n := B.w, B.h, B.n
	clear(lab[:n])
	sizes := []int{0}
	for s := 0; s < n; s++ {
		if mask[s] != val || lab[s] != 0 {
			continue
		}
		id := int32(len(sizes))
		qh, qt := 0, 0
		queue[qt] = int32(s)
		qt++
		lab[s] = id
		for qh < qt {
			li := int(queue[qh])
			qh++
			lx, ly := li%w, li/w
			if lx > 0 && mask[li-1] == val && lab[li-1] == 0 {
				lab[li-1] = id
				queue[qt] = int32(li - 1)
				qt++
			}
			if lx < w-1 && mask[li+1] == val && lab[li+1] == 0 {
				lab[li+1] = id
				queue[qt] = int32(li + 1)
				qt++
			}
			if ly > 0 && mask[li-w] == val && lab[li-w] == 0 {
				lab[li-w] = id
				queue[qt] = int32(li - w)
				qt++
			}
			if ly < h-1 && mask[li+w] == val && lab[li+w] == 0 {
				lab[li+w] = id
				queue[qt] = int32(li + w)
				qt++
			}
		}
		sizes = append(sizes, qt)
	}
	return sizes
}

// chamfer is the 3-4 chamfer distance from every tile to the nearest tile
// where src is set (far if none): forward then backward raster pass.
func chamfer(B box, src []uint8, out []int32) {
	w, h, n := B.w, B.h, B.n
	for li := 0; li < n; li++ {
		if src[li] != 0 {
			out[li] = 0
		} else {
			out[li] = far
		}
	}
	for ly := 0; ly < h; ly++ {
		for lx := 0; lx < w; lx++ {
			li := ly*w + lx
			v := out[li]
			if lx > 0 {
				v = min(v, out[li-1]+3)
			}
			if ly > 0 {
				v = min(v, out[li-w]+3)
				if lx > 0 {
					v = min(v, out[li-w-1]+4)
				}
				if lx < w-1 {
					v = min(v, out[li-w+1]+4)
				}
			}
			out[li] = v
		}
	}
	for ly := h - 1; ly >= 0; ly-- {
		for lx := w - 1; lx >= 0; lx-- {
			li := ly*w + lx
			v := out[li]
			if lx < w-1 {
				v = min(v, out[li+1]+3)
			}
			if ly < h-1 {
				v = min(v, out[li+w]+3)
				if lx < w-1 {
					v = min(v, out[li+w+1]+4)
				}
				if lx > 0 {
					v = min(v, out[li+w-1]+4)
				}
			}
			out[li] = v
		}
	}
}

// smoothCoast: water with 6+ of its 8 neighbours on land becomes land, land
// with 2 or fewer becomes water (double-buffered).
func smoothCoast(B box, C *scratch) {
	land, mask := C.land, C.mask
	w, h, n := B.w, B.h, B.n
	copy(mask[:n], land[:n])
	for ly := 1; ly < h-1; ly++ {
		for lx := 1; lx < w-1; lx++ {
			li := ly*w + lx
			c := land[li-w-1] + land[li-w] + land[li-w+1] + land[li-1] + land[li+1] + land[li+w-1] + land[li+w] + land[li+w+1]
			if land[li] == 0 && c >= 6 {
				mask[li] = 1
			} else if land[li] != 0 && c <= 2 {
				mask[li] = 0
			}
		}
	}
	copy(land[:n], mask[:n])
}

// smoothInland: carved water with 6+ land neighbours fills in, land with 6+
// carved-water neighbours floods (double-buffered).
func smoothInland(B box, C *scratch) {
	land, carved, mask := C.land, C.carved, C.mask
	w, h, n := B.w, B.h, B.n
	copy(mask[:n], land[:n])
	offs := [8]int{-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1}
	for ly := 1; ly < h-1; ly++ {
		for lx := 1; lx < w-1; lx++ {
			li := ly*w + lx
			cl, cw := 0, 0
			for _, o := range offs {
				if land[li+o] != 0 {
					cl++
				} else if carved[li+o] != 0 {
					cw++
				}
			}
			if land[li] == 0 && carved[li] != 0 && cl >= 6 {
				mask[li] = 1
			} else if land[li] != 0 && cw >= 6 {
				mask[li] = 0
			}
		}
	}
	for li := 0; li < n; li++ {
		if mask[li] != land[li] {
			land[li] = mask[li]
			carved[li] = 1 - mask[li]
		}
	}
}

// removeSpecks: land pieces smaller than speck become water (marked carved
// when asked).
func removeSpecks(B box, C *scratch, markCarved bool) {
	land, carved := C.land, C.carved
	sizes := components(B, land, 1, C.lab, C.queue)
	for li := 0; li < B.n; li++ {
		if land[li] != 0 && sizes[C.lab[li]] < speck {
			land[li] = 0
			if markCarved {
				carved[li] = 1
			}
		}
	}
}

// fillSmallWater: enclosed water bodies (not connected to the box corner)
// smaller than min become land.
func fillSmallWater(B box, C *scratch, min int) {
	land, carved, lab := C.land, C.carved, C.lab
	sizes := components(B, land, 0, lab, C.queue)
	sea := lab[0]
	for li := 0; li < B.n; li++ {
		if land[li] == 0 && lab[li] != sea && sizes[lab[li]] < min {
			land[li] = 1
			carved[li] = 0
		}
	}
}

// markOcean: ocean = water connected to the box corner (open sea).
func markOcean(B box, C *scratch) {
	land, ocean, lab := C.land, C.ocean, C.lab
	components(B, land, 0, lab, C.queue)
	sea := lab[0]
	for li := 0; li < B.n; li++ {
		ocean[li] = b2u(land[li] == 0 && lab[li] == sea)
	}
}

// reconnect joins every land piece inland water cut off from the island's
// main body back to it with a one-tile causeway (breadth-first search from the
// main body through carved water).
func reconnect(B box, C *scratch) {
	land, carved, lab, queue, parent := C.land, C.carved, C.lab, C.queue, C.parent
	w, n := B.w, B.n
	sizes := components(B, land, 1, lab, queue)
	main := 1
	for l := 2; l < len(sizes); l++ {
		if sizes[l] > sizes[main] {
			main = l
		}
	}
	need := make([]uint8, len(sizes)) // 1 = cut off by carved water, 2 = rejoined
	any := false
	for li := 0; li < n; li++ {
		if land[li] != 0 || carved[li] == 0 {
			continue
		}
		for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
			if land[nb] != 0 && int(lab[nb]) != main && need[lab[nb]] == 0 {
				need[lab[nb]] = 1
				any = true
			}
		}
	}
	if !any {
		return
	}
	for li := 0; li < n; li++ {
		parent[li] = -1 // -1 unvisited, -2 a land source, else the tile it was reached from
	}
	qh, qt := 0, 0
	for li := 0; li < n; li++ {
		if land[li] != 0 && int(lab[li]) == main {
			parent[li] = -2
			queue[qt] = int32(li)
			qt++
		}
	}
	for qh < qt {
		li := int(queue[qh])
		qh++
		for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
			if parent[nb] != -1 {
				continue
			}
			if land[nb] != 0 {
				l := lab[nb]
				if need[l] != 1 {
					continue
				}
				for t := li; parent[t] != -2; t = int(parent[t]) {
					land[t] = 1
					carved[t] = 0
				}
				need[l] = 2
				for s := 0; s < n; s++ {
					if lab[s] == l && land[s] != 0 {
						parent[s] = -2
						queue[qt] = int32(s)
						qt++
					}
				}
			} else if carved[nb] != 0 {
				parent[nb] = int32(li)
				queue[qt] = int32(nb)
				qt++
			}
		}
	}
}

// pickLevel is the most common level among counts c1..c3; ties go to the
// level nearer own, then the lower.
func pickLevel(c1, c2, c3, own int) uint8 {
	best, bc, bd := 1, -1, 9
	for L := 1; L <= 3; L++ {
		c := c3
		if L == 1 {
			c = c1
		} else if L == 2 {
			c = c2
		}
		d := abs(L - own)
		if c > bc || (c == bc && d < bd) {
			best, bc, bd = L, c, d
		}
	}
	return uint8(best)
}

// smoothLevels is the terrace clean-up (double-buffered). mode 8: a land tile
// sharing its level with at most two of its eight land neighbours takes their
// commonest level. mode 4: a land tile differing from at least three of its
// four land neighbours takes their commonest level.
func smoothLevels(B box, C *scratch, mode int) {
	land, el, el2 := C.land, C.el, C.el2
	w := B.w
	offs := []int{-1, 1, -w, w}
	if mode == 8 {
		offs = []int{-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1}
	}
	copy(el2[:B.n], el[:B.n])
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		c1, c2, c3 := 0, 0, 0
		for _, o := range offs {
			nb := li + o
			if land[nb] == 0 {
				continue
			}
			switch el[nb] {
			case 1:
				c1++
			case 2:
				c2++
			default:
				c3++
			}
		}
		own := int(el[li])
		same := c3
		if own == 1 {
			same = c1
		} else if own == 2 {
			same = c2
		}
		tot := c1 + c2 + c3
		if (mode == 8 && tot >= 3 && same <= 2) || (mode != 8 && tot-same >= 3) {
			el2[li] = pickLevel(c1, c2, c3, own)
		}
	}
	copy(el[:B.n], el2[:B.n])
}

// ensureClimbable: every land tile must be walkable from the sea shore with
// the one-level climb. While some tile stays unreached, the first one in tile
// order with a reached neighbour is set one above the highest such neighbour.
// Returns whether it cut any ramp.
func ensureClimbable(B box, C *scratch) bool {
	land, ocean, el, reach, queue := C.land, C.ocean, C.el, C.reach, C.queue
	w, n := B.w, B.n
	clear(reach[:n])
	qh, qt, cut := 0, 0, false
	for li := 0; li < n; li++ {
		if land[li] != 0 && (ocean[li-1] != 0 || ocean[li+1] != 0 || ocean[li-w] != 0 || ocean[li+w] != 0) {
			reach[li] = 1
			queue[qt] = int32(li)
			qt++
		}
	}
	for {
		for qh < qt {
			li := int(queue[qh])
			qh++
			lim := int(el[li]) + 1
			for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
				if land[nb] != 0 && reach[nb] == 0 && int(el[nb]) <= lim {
					reach[nb] = 1
					queue[qt] = int32(nb)
					qt++
				}
			}
		}
		fix, to := -1, 0
		for li := 0; li < n && fix < 0; li++ {
			if land[li] == 0 || reach[li] != 0 {
				continue
			}
			best := 0
			for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
				if reach[nb] != 0 && int(el[nb]) > best {
					best = int(el[nb])
				}
			}
			if best > 0 {
				fix, to = li, best+1
			}
		}
		if fix < 0 {
			return cut
		}
		el[fix] = uint8(to)
		reach[fix] = 1
		queue[qt] = int32(fix)
		qt++
		cut = true
	}
}

// unwallPockets: a same-level land region of at most 40 tiles with 60%+ of its
// land border 2+ levels higher has those walls cut down to one above it.
// Walls are collected over the whole box first, then lowered. Returns whether
// it lowered anything.
func unwallPockets(B box, C *scratch) bool {
	land, el, lab := C.land, C.el, C.lab
	w, n := B.w, B.n
	var cuts []int
	for L := uint8(1); L <= 2; L++ {
		sizes := components(B, el, L, lab, C.queue)
		per, walled := make([]int32, len(sizes)), make([]int32, len(sizes))
		for li := 0; li < n; li++ {
			if el[li] != L {
				continue
			}
			for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
				if land[nb] == 0 || el[nb] == L {
					continue
				}
				per[lab[li]]++
				if el[nb] >= L+2 {
					walled[lab[li]]++
				}
			}
		}
		for li := 0; li < n; li++ {
			if el[li] != L {
				continue
			}
			l := lab[li]
			if sizes[l] > 40 || per[l] == 0 || walled[l]*10 < per[l]*6 {
				continue
			}
			for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
				if land[nb] != 0 && el[nb] >= L+2 {
					cuts = append(cuts, nb, int(L)+1)
				}
			}
		}
	}
	changed := false
	for j := 0; j < len(cuts); j += 2 {
		if int(el[cuts[j]]) > cuts[j+1] {
			el[cuts[j]] = uint8(cuts[j+1])
			changed = true
		}
	}
	return changed
}

// breakStraightCliffs: in every run of more than 6 consecutive 2-level edges
// along a tile axis the higher tile of every 7th edge drops to the middle
// level (rows first, then columns; in place). Returns whether it notched.
func breakStraightCliffs(B box, C *scratch) bool {
	land, el := C.land, C.el
	w, h := B.w, B.h
	changed := false
	notch := func(a, b int) {
		if el[a] > el[b] {
			el[a] = el[b] + 1
		} else {
			el[b] = el[a] + 1
		}
		changed = true
	}
	cliff := func(a, b int) bool {
		return land[a] != 0 && land[b] != 0 && abs(int(el[a])-int(el[b])) >= 2
	}
	for ly := 0; ly < h-1; ly++ {
		run := 0
		for lx := 0; lx < w; lx++ {
			a := ly*w + lx
			if !cliff(a, a+w) {
				run = 0
				continue
			}
			if run++; run > 6 {
				notch(a, a+w)
				run = 0
			}
		}
	}
	for lx := 0; lx < w-1; lx++ {
		run := 0
		for ly := 0; ly < h; ly++ {
			a := ly*w + lx
			if !cliff(a, a+1) {
				run = 0
				continue
			}
			if run++; run > 6 {
				notch(a, a+1)
				run = 0
			}
		}
	}
	return changed
}

// trimSpikes: no pillars or slabs (double-buffered via el2). A land tile higher
// than 3+ of its land neighbours, or than all of its 2+ land neighbours, drops
// to its highest lower neighbour; an elevated tile in a run of 1-2 elevated
// land tiles with open sea beyond both ends, along either axis, drops to
// level 1. Returns whether it changed anything.
func trimSpikes(B box, C *scratch) bool {
	land, ocean, el, el2 := C.land, C.ocean, C.el, C.el2
	w, n := B.w, B.n
	copy(el2[:n], el[:n])
	changed := false
	for li := 0; li < n; li++ {
		if land[li] == 0 {
			continue
		}
		e := el[li]
		lower, nland, top := 0, 0, uint8(0)
		for _, nb := range [4]int{li - 1, li + 1, li - w, li + w} {
			if land[nb] == 0 {
				continue
			}
			nland++
			if el[nb] < e {
				lower++
				if el[nb] > top {
					top = el[nb]
				}
			}
		}
		if lower >= 3 || (lower >= 2 && lower == nland) {
			el2[li] = top
			changed = true
			continue
		}
		if e < 2 {
			continue
		}
		for _, step := range [2]int{1, w} {
			a, b, wd := li, li, 1
			for wd <= 3 && land[a-step] != 0 && el[a-step] >= 2 {
				a -= step
				wd++
			}
			for wd <= 3 && land[b+step] != 0 && el[b+step] >= 2 {
				b += step
				wd++
			}
			if wd <= 2 && ocean[a-step] != 0 && ocean[b+step] != 0 {
				el2[li] = 1
				changed = true
				break
			}
		}
	}
	copy(el[:n], el2[:n])
	return changed
}

// settleTerraces repeats the lowering-only terrace clean-ups (pillars and
// slabs, walled pockets, straight cliffs, straight ledges) until none changes
// anything, then re-runs the climb check; if that had to cut a ramp it all
// settles again (at most 4 rounds; the climb check always runs last).
func settleTerraces(B box, C *scratch) {
	for round := 0; round < 4; round++ {
		for pass := 0; pass < 8; pass++ {
			a := trimSpikes(B, C)
			b := unwallPockets(B, C)
			c := breakStraightCliffs(B, C)
			d := breakStraightLedges(B, C)
			if !a && !b && !c && !d {
				break
			}
		}
		if !ensureClimbable(B, C) {
			return
		}
	}
}

// stubStrip mirrors STUB_STRIP: the longest strip levelStubs lowers or raises.
const stubStrip = 8

// levelStubs levels lone ledge dashes. Only a step's higher tile's side face
// toward +x or +y is drawn; a stub is a set of at most 3 drawn edges joined at
// tile corners (union-find over C.corner, path halving) with no other drawn
// edge within a corner-step. For each 1-level edge of a stub, in tile order,
// the jog is lowered (the strip of up to stubStrip higher tiles running back
// from it) or raised (the strip of lower tiles running on from it, never by the
// sea), whichever is shorter, lowering on a tie. Returns whether it changed.
func levelStubs(B box, C *scratch) bool {
	land, ocean, el, par := C.land, C.ocean, C.el, C.corner
	w, h, n := B.w, B.h, B.n
	W1 := w + 1
	NC := W1 * (h + 1)
	for i := 0; i < NC; i++ {
		par[i] = -1
	}
	find := func(a int) int {
		for int(par[a]) != a {
			par[a] = par[par[a]]
			a = int(par[a])
		}
		return a
	}
	join := func(a, b int) {
		if par[a] < 0 {
			par[a] = int32(a)
		}
		if par[b] < 0 {
			par[b] = int32(b)
		}
		ra, rb := find(a), find(b)
		if ra != rb {
			par[ra] = int32(rb)
		}
	}
	var edges []int // higher tile, step to the lower tile (w: south, 1: east), first corner, second corner
	for li := 0; li < n; li++ {
		if land[li] == 0 {
			continue
		}
		lx, ly := li%w, li/w
		if land[li+w] != 0 && el[li] > el[li+w] {
			c := (ly+1)*W1 + lx
			join(c, c+1)
			edges = append(edges, li, w, c, c+1)
		}
		if land[li+1] != 0 && el[li] > el[li+1] {
			c := ly*W1 + lx + 1
			join(c, c+W1)
			edges = append(edges, li, 1, c, c+W1)
		}
	}
	size := make([]int32, NC)
	state := make([]uint8, NC) // 1 stub, 2 not
	for e := 0; e < len(edges); e += 4 {
		size[find(edges[e+2])]++
	}
	isStub := func(r int) bool {
		if state[r] != 0 {
			return state[r] == 1
		}
		alone := size[r] <= 3
		for e := 0; e < len(edges) && alone; e += 4 {
			if find(edges[e+2]) != r {
				continue
			}
			for _, c := range [2]int{edges[e+2], edges[e+3]} {
				cx, cy := c%W1, c/W1
				for dy := -1; dy <= 1 && alone; dy++ {
					for dx := -1; dx <= 1; dx++ {
						nx, ny := cx+dx, cy+dy
						if nx < 0 || ny < 0 || nx > w || ny > h {
							continue
						}
						if o := ny*W1 + nx; par[o] >= 0 && find(o) != r {
							alone = false
							break
						}
					}
				}
			}
		}
		state[r] = 2
		if alone {
			state[r] = 1
		}
		return alone
	}
	nearSea := func(t int) bool {
		return ocean[t-1] != 0 || ocean[t+1] != 0 || ocean[t-w] != 0 || ocean[t+w] != 0
	}
	changed := false
	for e := 0; e < len(edges); e += 4 {
		hh, d := edges[e], edges[e+1]
		ll, r := hh+d, find(edges[e+2])
		if size[r] > 3 || !isStub(r) {
			continue
		}
		eH, eL := el[hh], el[ll]
		if int(eH)-int(eL) != 1 {
			continue // already levelled, or a cliff (those keep their lips)
		}
		perp := 1
		if d == 1 {
			perp = w
		}
		kA := 0
		for t := hh; ; t -= d {
			if kA > 0 && (land[t] == 0 || el[t] <= eL) {
				break
			}
			if kA == stubStrip || land[t] == 0 || el[t] != eH || (land[t-perp] != 0 && el[t-perp] > eL) {
				kA = 0
				break
			}
			kA++
		}
		kB := 0
		for t := ll; ; t += d {
			if kB > 0 && land[t] != 0 && el[t] >= eH {
				break
			}
			if kB == stubStrip || land[t] == 0 || el[t] != eL || land[t+perp] == 0 || el[t+perp] < eH || nearSea(t) {
				kB = 0
				break
			}
			kB++
		}
		if kA != 0 && (kB == 0 || kA <= kB) {
			for k := 0; k < kA; k++ {
				el[hh-k*d] = eL
			}
			changed = true
		} else if kB != 0 {
			for k := 0; k < kB; k++ {
				el[ll+k*d] = eH
			}
			changed = true
		}
	}
	return changed
}

// breakStraightLedges: only edges whose higher tile is to the north or west
// are drawn in the iso view. In every run of more than 8 consecutive drawn
// 1-level edges along an axis, the higher tiles of the 9th edge and the next
// drop to the lower level — a two-tile ramp gap (edges between rows first,
// then between columns; in place). Returns whether it changed anything.
func breakStraightLedges(B box, C *scratch) bool {
	land, el := C.land, C.el
	w, h := B.w, B.h
	changed := false
	drawn := func(a, b int) bool { return land[a] != 0 && land[b] != 0 && int(el[a])-int(el[b]) == 1 }
	scan := func(outer, inner int, at func(o, k int) int, step int) {
		for o := 0; o < outer; o++ {
			run := 0
			for k := 0; k < inner; k++ {
				a := at(o, k)
				b := a + step
				if !drawn(a, b) {
					run = 0
					continue
				}
				if run++; run <= 8 {
					continue
				}
				el[a] = el[b]
				if k+1 < inner {
					if a2 := at(o, k+1); drawn(a2, a2+step) {
						el[a2] = el[a2+step]
					}
				}
				changed = true
				run = 0
			}
		}
	}
	scan(h-1, w, func(ly, lx int) int { return ly*w + lx }, w) // edges between rows, along x
	scan(w-1, h, func(lx, ly int) int { return ly*w + lx }, 1) // edges between columns, along y
	return changed
}

// isRock / rockKind mirror isRock and ROCK_KIND: the node kinds placeRocks
// keeps apart (nodeK values are kind + 1).
func isRock(k uint8) bool {
	return k == NodeBoulder+1 || k == NodeStone+1 || k == NodeCrystal+1 || k == NodeStarmetal+1
}

func rockKind(kind uint8) bool {
	return kind == NodeBoulder || kind == NodeStone || kind == NodeCrystal || kind == NodeStarmetal
}

// placeRocks: rocks never pack. Taken in pickTop's ranking, a tile is skipped
// if it would touch more than one rock side-on, touch a rock that already
// touches another, or have more than two rocks among its eight neighbours.
// With spread (boulders) it is skipped if any rock is among its eight
// neighbours at all.
func placeRocks(B box, C *scratch, count int, score func(li, x, y int) float64, kind uint8, spread bool) {
	nodeK := C.nodeK
	ranked := pickTop(B, C, B.n, score)
	placed := 0
	for r := 0; r < len(ranked) && placed < count; r++ {
		li := int(ranked[r])
		i := (B.y0+li/B.w)*SIZE + B.x0 + li%B.w
		orth, ring, side := 0, 0, -1
		for _, o := range [4]int{-1, 1, -SIZE, SIZE} {
			if isRock(nodeK[i+o]) {
				orth++
				side = i + o
			}
		}
		for _, o := range [4]int{-SIZE - 1, -SIZE + 1, SIZE - 1, SIZE + 1} {
			if isRock(nodeK[i+o]) {
				ring++
			}
		}
		if orth > 1 || orth+ring > 2 || (spread && orth+ring > 0) {
			continue
		}
		if orth == 1 {
			o2 := 0
			for _, o := range [4]int{-1, 1, -SIZE, SIZE} {
				if isRock(nodeK[side+o]) {
					o2++
				}
			}
			if o2 > 0 {
				continue
			}
		}
		nodeK[i] = kind + 1
		placed++
	}
}

// pickTop returns the count box tiles with the highest score(li, x, y) (no =
// not a candidate), ranked by score descending then tile index ascending — a
// total order, so the sort algorithm cannot change the pick.
func pickTop(B box, C *scratch, count int, score func(li, x, y int) float64) []int32 {
	cand, sc, order := C.cand, C.score, C.order
	m := 0
	for li := 0; li < B.n; li++ {
		lx := li % B.w
		s := score(li, B.x0+lx, B.y0+li/B.w)
		if s == no {
			continue
		}
		cand[m] = int32(li)
		sc[m] = s
		m++
	}
	ord := order[:m]
	for j := range ord {
		ord[j] = int32(j)
	}
	slices.SortFunc(ord, func(a, b int32) int {
		if sc[a] > sc[b] {
			return -1
		}
		if sc[a] < sc[b] {
			return 1
		}
		return int(a - b)
	})
	out := make([]int32, min(count, m))
	for j := range out {
		out[j] = cand[ord[j]]
	}
	return out
}

// --- 1. a major island -------------------------------------------------------

// feature is a laid-out coastline feature: kind 0 bay (x, y, r), 1 peninsula
// or spit (a-b, hw), 2 fjord (a-m-b, hw); amt its pull.
type feature struct {
	kind                       int
	x, y, r, amt               float64
	ax, ay, bx, by, mx, my, hw float64
}

// layoutFeatures lays plan P's coastline features round the island in dir16
// steps from the seeded rotation; bays and fjords never cut towards the
// island's mountain.
func layoutFeatures(P *islePlan, k, rot, mdir int, shape *Noise2D) []feature {
	out := make([]feature, 0, len(P.feats))
	for j, f := range P.feats {
		di := (rot + f.dir) & 15
		off := min(abs(di-mdir), 16-abs(di-mdir))
		if (f.kind == "bay" || f.kind == "fjord") && off <= 1 {
			di = (di + 3) & 15
		}
		ux, uy := dir16[di][0], dir16[di][1]
		p := f.p
		ax, ay := float64(ux*p[0]), float64(uy*p[0])
		switch f.kind {
		case "bay":
			out = append(out, feature{kind: 0, x: ax, y: ay, r: p[1], amt: p[2]})
		case "pen":
			out = append(out, feature{kind: 1, ax: ax, ay: ay, bx: float64(ux * p[1]), by: float64(uy * p[1]), hw: p[2], amt: p[3]})
		case "spit":
			v := dir16[(di+int(p[4]))&15]
			out = append(out, feature{kind: 1, ax: ax, ay: ay, bx: float64(v[0] * p[1]), by: float64(v[1] * p[1]), hw: p[2], amt: p[3]})
		default: // fjord: from the open sea (92 out) to `from`, bent sideways at its middle
			bend := float64(7 * shape.Eval(float64(float64(k)*3.7)+float64(float64(j)*1.9), 13.3))
			mid := (p[0] + 92) / 2
			out = append(out, feature{kind: 2, ax: ax, ay: ay,
				mx: float64(ux*mid) - float64(uy*bend), my: float64(uy*mid) + float64(ux*bend),
				bx: float64(ux * 92), by: float64(uy * 92), hw: p[1], amt: p[2]})
		}
	}
	return out
}

// featureValue is the features' pull on the coast value at warped
// island-relative (wx, wy).
func featureValue(feats []feature, wx, wy float64) float64 {
	v := 0.0
	for i := range feats {
		f := &feats[i]
		switch f.kind {
		case 0:
			v -= float64(f.amt * clamp01(1-jsHypot2(wx-f.x, wy-f.y)/f.r))
		case 1:
			sd, t := segDist(wx, wy, f.ax, f.ay, f.bx, f.by)
			v += float64(f.amt * clamp01(1-sd/(f.hw*(1-float64(0.55*t)))))
		default:
			s1, _ := segDist(wx, wy, f.ax, f.ay, f.mx, f.my)
			s2, _ := segDist(wx, wy, f.mx, f.my, f.bx, f.by)
			v -= float64(f.amt * clamp01(1.5-math.Min(s1, s2)/f.hw))
		}
	}
	return v
}

// scarp is one Woods rock outcrop (woodsScarps).
type scarp struct {
	x, y, tx, ty, nx, ny, len, depth, slope, footW, bow, off float64
}

// woodsScarps lays out the Woods rock outcrops: raised tables with one face.
// Outcrop 0's face drops to the east-north-east (test.mjs relies on it).
func woodsScarps(B box, C *scratch, rot int) []scarp {
	var out []scarp
	add := func(di, ti int) {
		tx, ty := dir16[ti][0], dir16[ti][1]
		x, y := scarpSite(B, C, di, tx, ty)
		j := float64(len(out))
		out = append(out, scarp{x: x, y: y, tx: tx, ty: ty, nx: -ty, ny: tx, len: 12, depth: 6, slope: 7, footW: 7,
			bow: 3 * C.shape.Eval(float64(float64(rot)*0.37)+float64(j*5.1), 61.7), off: 31.7 * (j + 1)})
	}
	add((rot+9)&15, 3)
	add((rot+14)&15, ((rot*3+4)&15)|1)
	return out
}

// scarpSite picks where an outcrop goes (see shared/world.js scarpSite) and
// returns it in warped island-relative coordinates.
func scarpSite(B box, C *scratch, di int, tx, ty float64) (float64, float64) {
	best, bx, by := math.Inf(-1), 0.0, 0.0
	at := func(x, y int) int { return (y-B.y0)*B.w + x - B.x0 }
	fcx, fcy := float64(B.cx), float64(B.cy)
	r := func(v float64) int { return int(math.Floor(v + 0.5)) } // callers round v first: no FMA
	for s := 0; s < 16; s++ {
		for _, dd := range [3]float64{28, 34, 40} {
			x, y := r(fcx+float64(dir16[s][0]*dd)), r(fcy+float64(dir16[s][1]*dd))
			ex, ey := r(float64(tx*12)), r(float64(ty*12))
			li := at(x, y)
			if C.land[li] == 0 || C.dW[li] < 30 || C.dW[at(x+ex, y+ey)] < 18 || C.dW[at(x-ex, y-ey)] < 18 {
				continue
			}
			inClearing := false
			for _, c := range Clearings {
				if c.K == 0 && jsHypot2(float64(x-c.X), float64(y-c.Y)) <= float64(c.Blend+13) {
					inClearing = true
					break
				}
			}
			if inClearing {
				continue
			}
			off := min(abs(s-di), 16-abs(s-di))
			back := C.dW[at(x-r(float64(ty*13)), y+r(float64(tx*13)))]
			front := C.dW[at(x+r(float64(ty*7)), y-r(float64(tx*7)))]
			backPen, frontPen := 0.0, 0.0
			if back < 24 {
				backPen = 3
			}
			if front < 18 {
				frontPen = 3
			}
			score := math.Min(float64(C.dW[li])/3, 14) - float64(1.5*float64(off)) - backPen - frontPen
			if score > best {
				best, bx, by = score, C.px[li], C.py[li]
			}
		}
	}
	return bx, by
}

// applyScarps applies the outcrops to height h at warped (wx, wy); near (0..1)
// fades them out by the water. The second result is the JS SCARP_ROCK side
// channel: how strongly the tile is part of an outcrop (for stones).
func applyScarps(scarps []scarp, wx, wy, h, near, top, foot float64, n *Noise2D) (float64, float64) {
	rock := 0.0
	for i := range scarps {
		c := &scarps[i]
		ox, oy := wx-c.x, wy-c.y
		a := float64(ox*c.tx) + float64(oy*c.ty)
		b := float64(ox*c.nx) + float64(oy*c.ny) + float64(c.bow*(float64(a*a)/float64(c.len*c.len)-0.3)) + float64(1.5*n.Eval(a/5+c.off, c.off))
		along := float64(ramp(c.len+float64(2*n.Eval(b/10+c.off+11, c.off+5))-math.Abs(a), 0, 7) * near)
		if along <= 0 {
			continue
		}
		depth := c.depth + float64(2*n.Eval(a/6+c.off+23, c.off+9))
		if b >= 0 {
			f := 1.0
			if !(b < depth) {
				f = 1 - (b-depth)/c.slope
			}
			s := float64(along * f)
			if s > 0 {
				h += float64(s * (top - h))
				if s > rock {
					rock = s
				}
			}
		} else if b > -c.footW {
			h += float64(float64(along*(1+b/c.footW)) * (foot - h))
		}
	}
	return h, rock
}

// dryClear: is (x, y) far enough from island k's clearings, mountain and
// monolith for inland water? margin widens every clearing's land radius.
func dryClear(k, x, y int, margin float64) bool {
	for _, c := range Clearings {
		if c.K != k {
			continue
		}
		r := float64(c.Land) + margin
		dx, dy := x-c.X, y-c.Y
		if float64(dx*dx+dy*dy) <= r*r {
			return false
		}
	}
	m := Mountains[k]
	mx, my := x-m.X, y-m.Y
	if mx*mx+my*my <= 25 {
		return false
	}
	ox, oy := x-ISLES[k][0], y-ISLES[k][1]
	return ox*ox+oy*oy > 81 // never within 9 of the monolith
}

// barchanCell mirrors BARCHAN_CELL: the dune cell size along / across the wind.
var barchanCell = [2]float64{18, 14}

// barchans is the crescent-dune height (0..~1) at tile (x, y): cells in a
// frame turned 30 degrees to the wind (u downwind, v across, a little warped),
// each holding an elongated mound with a hollow bitten out of its downwind
// side; the tallest dune of the tile's 3x3 cells wins (cu outer, cv inner).
func barchans(shape, warp *Noise2D, x, y int) float64 {
	fx, fy := float64(x), float64(y)
	u := float64(fx*0.8660254037844387) + float64(fy*0.5) + float64(3*warp.Eval(fx/9+2000, fy/9+2000))
	v := float64(fy*0.8660254037844387) - float64(fx*0.5) + float64(3*warp.Eval(fx/9+2040, fy/9+2040))
	cw, ch := barchanCell[0], barchanCell[1]
	iu, iv := math.Floor(u/cw), math.Floor(v/ch)
	best := 0.0
	for cu := iu - 1; cu <= iu+1; cu++ {
		for cv := iv - 1; cv <= iv+1; cv++ {
			sx, sy := float64(cu*1.37), float64(cv*1.91)
			if shape.Eval(sx+300, sy+300) < -0.45 {
				continue // an empty cell
			}
			size := 0.5 + float64(0.5*shape.Eval(sx+420, sy+420))
			r := 6 + float64(5*size)
			du := u - float64((cu+0.5+float64(0.3*shape.Eval(sx+340, sy+340)))*cw)
			dv := v - float64((cv+0.5+float64(0.35*shape.Eval(sx+380, sy+380)))*ch)
			body := 1 - jsHypot2(float64(du*0.8), dv)/r
			if body <= 0 {
				continue
			}
			bite := 1 - jsHypot2(du-float64(0.6*r), float64(dv*1.1))/float64(0.8*r)
			hgt := float64(float64(clamp01(float64(body*1.8))*(1-clamp01(float64(bite*2.5)))) * (0.6 + float64(0.4*size)))
			if hgt > best {
				best = hgt
			}
		}
	}
	return best
}

// duneRocks mirrors DUNE_ROCKS: four table rocks or buttes and four small
// outcrops — dir offset (dir16 steps from the island rotation), distance,
// radius, tall (a butte: level-3 top inside a level-2 rim; else a level-2 table).
var duneRocks = [8][4]float64{
	{1, 34, 11, 1}, {6, 44, 8, 1}, {10, 30, 9, 0}, {13, 48, 7, 1},
	{3, 50, 3.5, 1}, {8, 52, 3, 1}, {12, 26, 4, 0}, {15, 40, 3, 1},
}

// duneRock is one placed Dunes rock: its tile (gx, gy), its centre in warped
// island-relative coordinates (x, y), and its long axis and elongation.
type duneRock struct {
	gx, gy                   int
	x, y, r, ax, ay, stretch float64
	tall                     bool
}

// duneRockSites places each Dunes rock (see shared/world.js duneRockSites);
// a rock with no inland, clear site is left out. First best site wins.
func duneRockSites(B box, C *scratch, k, rot int, shape *Noise2D) []duneRock {
	var out []duneRock
	fcx, fcy := float64(B.cx), float64(B.cy)
	for j, dr := range duneRocks {
		dOff, dist, r, tall := int(dr[0]), dr[1], dr[2], dr[3] != 0
		di := (rot + dOff) & 15
		best, bx, by := math.Inf(-1), 0, 0
		for s := 0; s < 16; s++ {
			for _, dd := range [3]float64{dist - 8, dist, dist + 8} {
				x := int(math.Floor(fcx + float64(dir16[s][0]*dd) + 0.5))
				y := int(math.Floor(fcy + float64(dir16[s][1]*dd) + 0.5))
				li := (y-B.y0)*B.w + x - B.x0
				if C.land[li] == 0 || float64(C.dO[li]) < (r+5)*3 || float64(C.dW[li]) < (r+2)*3 || !dryClear(k, x, y, r+6) {
					continue
				}
				crowded := false
				for _, o := range out {
					if jsHypot2(float64(x-o.gx), float64(y-o.gy)) < o.r+r+5 {
						crowded = true
						break
					}
				}
				if crowded {
					continue
				}
				off := min(abs(s-di), 16-abs(s-di))
				score := -float64(off) - math.Abs(dd-dist)/8
				if score > best {
					best, bx, by = score, x, y
				}
			}
		}
		if best == math.Inf(-1) {
			continue
		}
		li := (by-B.y0)*B.w + bx - B.x0
		a := dir16[(rot*7+j*5)&15]
		out = append(out, duneRock{gx: bx, gy: by, x: C.px[li], y: C.py[li], r: r, tall: tall, ax: a[0], ay: a[1],
			stretch: 1 + float64(0.35*(0.5+float64(0.5*shape.Eval(float64(float64(j)*4.3)+77, float64(k)*2.1))))})
	}
	return out
}

func islandPass(k int, C *scratch) {
	P := &islePlans[k]
	cx, cy := ISLES[k][0], ISLES[k][1]
	fk, fcx, fcy := float64(k), float64(cx), float64(cy)
	tile := isleT[k]
	B := makeBox(cx, cy, majorHalf)
	w, n := B.w, B.n
	land, carved, ocean, val, px, py := C.land, C.carved, C.ocean, C.val, C.px, C.py
	dO, dW, dL, H, el, aux1, aux2 := C.dO, C.dW, C.dL, C.H, C.el, C.aux1, C.aux2
	elevN, coast, warp, shape := C.elevN, C.coast, C.warp, C.shape
	rot := int(math.Min(15, math.Floor((shape.Eval(float64(fk*13.7)+2.3, 5.9)+1)*8)))
	mdir := dirIndexOf(float64(Mountains[k].X-cx), float64(Mountains[k].Y-cy))

	// ---- a. coastline: value per tile, the top `area` become land ----
	feats := layoutFeatures(P, k, rot, mdir, shape)
	cnt := 0
	for ly := 0; ly < B.h; ly++ {
		for lx := 0; lx < w; lx++ {
			li := ly*w + lx
			x, y := B.x0+lx, B.y0+ly
			fx, fy := float64(x), float64(y)
			ox, oy := float64(x-cx), float64(y-cy)
			wx := ox + float64(P.warp*warp.Eval(fx/38, fy/38))
			wy := oy + float64(P.warp*warp.Eval(fx/38+71.3, fy/38+37.9))
			px[li], py[li] = wx, wy
			land[li], carved[li] = 0, 0
			d := jsHypot2(ox, oy)
			if d >= landCap {
				val[li] = -9
				continue
			}
			q := jsHypot2(wx, wy)
			ux, uy := 1.0, 0.0
			if q > 1e-9 {
				ux, uy = wx/q, wy/q
			}
			lobe := (shape.Eval(float64(ux*0.55)+float64(fk*23.1), float64(uy*0.55)+7.7) +
				float64(0.5*shape.Eval(float64(ux*1.3)+float64(fk*23.1)+40.2, float64(uy*1.3)-12.4))) / 1.5
			ax, ay := fcx+wx, fcy+wy
			v := 1 - q/(baseR*(1+float64(P.lobe*lobe))) +
				float64(P.oct[0]*coast.Eval(ax/26, ay/26)) + float64(P.oct[1]*coast.Eval(ax/12+50, ay/12+50)) +
				float64(P.oct[2]*coast.Eval(ax/5.5+100, ay/5.5+100))
			jag := 0.0
			if P.jag != 0 {
				jag = float64(P.jag * (0.6 - math.Abs(coast.Eval(ax/7+150, ay/7+150))))
			}
			v = v + jag
			v = v + featureValue(feats, wx, wy)
			if d > 70 {
				v -= float64((d - 70) * 0.05) // soft fence well inside landCap
			}
			val[li] = v
			C.sortBuf[cnt] = v
			cnt++
		}
	}
	sorted := C.sortBuf[:cnt]
	sort.Float64s(sorted)
	th := sorted[cnt-P.area]
	for li := 0; li < n; li++ {
		if val[li] >= th {
			land[li] = 1
		}
	}
	setLand := func(li, _, _ int) { land[li] = 1 }
	for _, c := range Clearings {
		if c.K == k {
			disc(B, c.X, c.Y, c.Land, setLand)
		}
	}
	disc(B, Mountains[k].X, Mountains[k].Y, 3, setLand)
	// offshore islets and skerries in the band just off the coast
	for li := 0; li < n; li++ {
		if land[li] != 0 || !(val[li] > th-P.isletBand) {
			continue
		}
		lx := li % w
		x, y := B.x0+lx, B.y0+li/w
		if jsHypot2(float64(x-cx), float64(y-cy)) < 83 && warp.Eval(float64(x)/8+300+float64(40*k), float64(y)/8+300) > P.islet {
			land[li] = 1
		}
	}

	// ---- b. tidy ----
	smoothCoast(B, C)
	for _, c := range Clearings {
		if c.K == k {
			disc(B, c.X, c.Y, c.Land, setLand)
		}
	}
	removeSpecks(B, C, false)
	fillSmallWater(B, C, lagoonFill)

	// ---- c. ocean and the distance to it ----
	markOcean(B, C)
	chamfer(B, ocean, dO)

	// ---- d. inland water ----
	if k == 3 {
		marshWaters(B, C, k)
	} else {
		for j, lk := range P.lakes {
			dOff, dist, rad, keep := int(lk[0]), lk[1], lk[2], lk[3]
			fj := float64(j)
			if j > 0 && !(shape.Eval(float64(fk*7.7)+float64(fj*3.1), 21.9) > keep) {
				continue
			}
			lxc, lyc := lakeSite(B, C, k, (rot+dOff)&15, dist+float64(5*shape.Eval(float64(fk*5.1)+float64(fj*2.7), 33.3)), rad)
			sx, sy := dir16[(rot*5+j*7)&15][0], dir16[(rot*5+j*7)&15][1] // the lake's long axis
			for li := 0; li < n; li++ {
				if land[li] == 0 || dO[li] < 18 {
					continue // at least 6 tiles of shore between lake and sea
				}
				lx := li % w
				x, y := B.x0+lx, B.y0+li/w
				fx, fy := float64(x), float64(y)
				ax, ay := fcx+px[li], fcy+py[li]
				// an elongated blob in its own small warp, edged by two octaves of coast noise
				ex := px[li] - lxc + float64(3*warp.Eval(fx/9+500, fy/9+500))
				ey := py[li] - lyc + float64(3*warp.Eval(fx/9+540, fy/9+540))
				along, across := float64(ex*sx)+float64(ey*sy), float64(ey*sx)-float64(ex*sy)
				lv := 1 - jsHypot2(along/1.3, float64(across*1.3))/rad +
					float64(0.28*coast.Eval(ax/8+200, ay/8+200)) + float64(0.12*coast.Eval(ax/3.5+220, ay/3.5+220))
				if !(lv > 0) || !dryClear(k, x, y, 2) {
					continue
				}
				land[li], carved[li] = 0, 1
			}
		}
	}
	smoothInland(B, C)
	fillSmallWater(B, C, P.minPond)
	removeSpecks(B, C, true)
	reconnect(B, C)
	fillSmallWater(B, C, P.minPond)

	// ---- e. final distances ----
	markOcean(B, C)
	chamfer(B, ocean, dO)
	for li := 0; li < n; li++ {
		C.mask[li] = b2u(land[li] == 0)
	}
	chamfer(B, C.mask, dW)
	for li := 0; li < n; li++ {
		C.mask[li] = b2u(land[li] == 0 && ocean[li] == 0)
	}
	chamfer(B, C.mask, dL)

	// ---- f. terrain ----
	var scarps []scarp
	if k == 0 {
		scarps = woodsScarps(B, C, rot)
	}
	var duneRockList []duneRock
	if k == 1 {
		duneRockList = duneRockSites(B, C, k, rot, shape)
	}
	for li := 0; li < n; li++ {
		aux1[li], aux2[li] = 0, 0
		if land[li] == 0 {
			continue
		}
		lx := li % w
		x, y := B.x0+lx, B.y0+li/w
		fx, fy := float64(x), float64(y)
		wx, wy := px[li], py[li]
		dOt, dWt, dLt := float64(dO[li])/3, float64(dW[li])/3, float64(dL[li])/3
		var h float64
		switch k {
		case 0:
			// Woods: rolling lowland rising to a few hilltops, rock scarps
			hills := 0.5 + float64(0.5*(float64(0.7*elevN.Eval(fx/46, fy/46))+float64(0.3*elevN.Eval(fx/19+40, fy/19+40))))
			shore := ramp(dWt+float64(1.8*elevN.Eval(fx/8+60, fy/8+60)), 1, 8)
			var rock float64
			h, rock = applyScarps(scarps, wx, wy, float64(shore*(0.2+float64(0.8*hills))), ramp(dWt, 2, 8), P.t2+0.12, P.t1-0.15, elevN)
			aux2[li] = rock // rockiness, for stones
		case 1:
			// Dunes: a field of crescent (barchan) dunes blown along a 30-degree wind, flat pans, a few rocks
			erg := ramp(elevN.Eval(fx/52+300, fy/52+300), -0.25, 0.05) // 0 in the pans, 1 in the dune sea
			dune := float64(barchans(shape, warp, x, y) * erg)
			// the rocks: the strongest of the warped, lobed, elongated blobs; rim where mv > 0, top past 0.25
			rx := float64(2.5 * warp.Eval(fx/7+2300, fy/7+2300))
			ry := float64(2.5 * warp.Eval(fx/7+2340, fy/7+2340))
			edge := float64(0.22*coast.Eval((fcx+wx)/6+260, (fcy+wy)/6+260)) + float64(0.1*coast.Eval((fcx+wx)/3+280, (fcy+wy)/3+280))
			mv, tall := -9.0, false
			for ri := range duneRockList {
				r := &duneRockList[ri]
				ox, oy := wx-r.x+rx, wy-r.y+ry
				along, across := float64(ox*r.ax)+float64(oy*r.ay), float64(oy*r.ax)-float64(ox*r.ay)
				if v := 1 - jsHypot2(along/r.stretch, float64(across*r.stretch))/r.r + edge; v > mv {
					mv, tall = v, r.tall
				}
			}
			top := 0.0
			if tall {
				top = float64(0.45 * ramp(mv, 0.25, 0.33))
			}
			h = float64(ramp(dWt+float64(1.3*elevN.Eval(fx/7+660, fy/7+660)), 1.3, 5.5)*(0.06+float64(0.8*dune))) +
				float64(0.45*ramp(mv, 0, 0.08)) + top
			aux1[li], aux2[li] = erg, mv
		case 2:
			// Spire: coastal lowland, a plateau, a high massif round the monolith; crags and valleys
			q := jsHypot2(wx, wy)
			ux, uy := 1.0, 0.0
			if q > 1e-9 {
				ux, uy = wx/q, wy/q
			}
			massR := 27 + float64(7*shape.Eval(float64(ux*0.6)+900, float64(uy*0.6)+900)) + float64(3*elevN.Eval(fx/9+1000, fy/9+1000))
			lowW := float64((6 + float64(3*elevN.Eval(fx/17+1100, fy/17+1100))) * (1 - ramp(elevN.Eval(fx/24+1200, fy/24+1200), 0.18, 0.33)))
			jit := float64(1.5*elevN.Eval(fx/7+1150, fy/7+1150)) + float64(0.8*elevN.Eval(fx/3+1170, fy/3+1170))
			tierP := ramp(dOt+jit-lowW, 0, 1.2)
			tierM := ramp(massR-q+float64(0.8*elevN.Eval(fx/3+1190, fy/3+1190)), 0, 1.5)
			crag := ramp(elevN.Eval(fx/20+1300, fy/20+1300), 0.3, 0.42)
			valley := float64(0.42 * ramp(1-math.Abs(elevN.Eval((fcx+wx)/40+1400, (fcy+wy)/40+1400)), 0.9, 0.96))
			h = 0.2 + float64(0.4*tierP) + float64(0.4*math.Max(tierM, float64(crag*tierP))) - valley - float64(0.4*(1-ramp(dLt, 1, 3)))
		default:
			// Marsh: low and flat, scattered hummocks
			hx := fx + float64(2*warp.Eval(fx/5+2050, fy/5+2050))
			hy := fy + float64(2*warp.Eval(fx/5+2090, fy/5+2090))
			hum := float64(0.65*elevN.Eval(hx/13+1500, hy/13+1500)) + float64(0.35*elevN.Eval(hx/6+1600, hy/6+1600))
			h = float64(ramp(dWt+elevN.Eval(fx/7+1650, fy/7+1650), 1, 4) * (0.5 + float64(0.5*hum)))
		}
		H[li] = h
	}
	// clearings (and the mountain footprint) eased flat
	mid := [4]float64{0, float64(P.t1 * 0.5), (P.t1 + P.t2) / 2, P.t2 + 0.15}
	flatten := func(x, y, flat, blend int, target float64) {
		disc(B, x, y, blend, func(li, tx, ty int) {
			if land[li] == 0 {
				return
			}
			r := jsHypot2(float64(tx-x), float64(ty-y))
			a := 1.0
			if !(r <= float64(flat)) {
				a = (float64(blend) - r) / float64(blend-flat)
			}
			s := float64(float64(a*a) * (3 - float64(2*a)))
			H[li] += float64(s * (target - H[li]))
		})
	}
	for _, c := range Clearings {
		if c.K == k && c.Blend > 0 {
			flatten(c.X, c.Y, c.Flat, c.Blend, mid[c.Lvl])
		}
	}
	{
		m := Mountains[k]
		flatten(m.X, m.Y, 2, 5, H[(m.Y-B.y0)*w+m.X-B.x0])
	}
	for li := 0; li < n; li++ {
		switch {
		case land[li] == 0:
			el[li] = 0
		case H[li] > P.t2:
			el[li] = 3
		case H[li] > P.t1:
			el[li] = 2
		default:
			el[li] = 1
		}
	}
	smoothLevels(B, C, 8)
	smoothLevels(B, C, 8)
	smoothLevels(B, C, 4)
	ensureClimbable(B, C)
	settleTerraces(B, C)
	for round := 0; round < 3 && levelStubs(B, C); round++ {
		settleTerraces(B, C)
	}
	for li := 0; li < n; li++ {
		if land[li] == 0 {
			continue
		}
		lx := li % w
		i := (B.y0+li/w)*SIZE + B.x0 + lx
		C.tiles[i] = tile
		C.elev[i] = el[li]
	}

	// ---- g. dressing ----
	dressIsland(B, C, k)
}

// lakeSite picks where a lake goes (see shared/world.js lakeSite) and returns
// it in warped island-relative coordinates.
func lakeSite(B box, C *scratch, k, di int, dist, rad float64) (float64, float64) {
	best, bx, by := math.Inf(-1), 0.0, 0.0
	fcx, fcy := float64(B.cx), float64(B.cy)
	for s := 0; s < 16; s++ {
		for _, dd := range [3]float64{dist - 6, dist, dist + 6} {
			x := int(math.Floor(fcx + float64(dir16[s][0]*dd) + 0.5))
			y := int(math.Floor(fcy + float64(dir16[s][1]*dd) + 0.5))
			li := (y-B.y0)*B.w + x - B.x0
			if C.land[li] == 0 || !dryClear(k, x, y, rad+2) {
				continue
			}
			off := min(abs(s-di), 16-abs(s-di))
			score := math.Min(float64(C.dO[li])/3, rad+10) - float64(0.5*float64(off))
			if score > best {
				best, bx, by = score, C.px[li], C.py[li]
			}
		}
	}
	return bx, by
}

// marshWaters is the Marsh's inland water: pools, meandering channels, and
// the village's fishing pool.
func marshWaters(B box, C *scratch, k int) {
	land, carved, dO := C.land, C.carved, C.dO
	elevN, warp := C.elevN, C.warp
	fp := marshFishingPool
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		lx := li % B.w
		x, y := B.x0+lx, B.y0+li/B.w
		fx, fy := float64(x), float64(y)
		// pools: low-frequency blobs in a small warp
		qx := fx + float64(4*warp.Eval(fx/8+1600, fy/8+1600))
		qy := fy + float64(4*warp.Eval(fx/8+1650, fy/8+1650))
		pool := float64(0.85*elevN.Eval(qx/11+1700, qy/11+1700)) + float64(0.15*elevN.Eval(qx/5.5+1750, qy/5.5+1750))
		// channels: the zero line of a field over a strongly warped, rotated domain
		mx := fx + float64(8*warp.Eval(fx/13+1900, fy/13+1900))
		my := fy + float64(8*warp.Eval(fx/13+1950, fy/13+1950))
		u, v := float64(mx*0.8)+float64(my*0.6), float64(my*0.8)-float64(mx*0.6)
		gate := ramp(elevN.Eval(fx/45+1850, fy/45+1850), -0.05, 0.25)
		half := float64((0.06 + float64(0.05*(0.5+float64(0.5*elevN.Eval(fx/12+1880, fy/12+1880))))) * gate)
		chan_ := math.Abs(elevN.Eval(u/30+1800, v/30+1800)) < half
		ffx := float64(x-fp.x) + float64(1.5*warp.Eval(fx/4+1980, fy/4+1980))
		ffy := float64(y-fp.y) + float64(1.5*warp.Eval(fx/4+1990, fy/4+1990))
		wet := false
		if jsHypot2(ffx, ffy) < fp.r {
			wet = dryClear(k, x, y, 0)
		} else if pool > 0.48 && dO[li] >= 9 {
			wet = dryClear(k, x, y, 2)
		} else if chan_ && dO[li] >= 6 {
			wet = dryClear(k, x, y, 2)
		}
		if wet {
			land[li], carved[li] = 0, 1
		}
	}
}

// spaceGladeTrees: within gladeEdge tiles of the spawn no two trees touch.
// Nearest first (squared distance, then tile index): a tree is felled if a
// 4-neighbour tree is nearer, or as near and earlier in tile order.
func spaceGladeTrees(nodeK []uint8) {
	const tree, R = NodeTree + 1, gladeEdge
	for d2 := 0; d2 <= R*R; d2++ {
		for dy := -R; dy <= R; dy++ {
			for dx := -R; dx <= R; dx++ {
				if dx*dx+dy*dy != d2 {
					continue
				}
				i := (spawnY+dy)*SIZE + spawnX + dx
				if nodeK[i] != tree {
					continue
				}
				for _, o := range [4][2]int{{-1, 0}, {1, 0}, {0, -1}, {0, 1}} {
					ex, ey := dx+o[0], dy+o[1]
					j, e2 := i+o[1]*SIZE+o[0], ex*ex+ey*ey
					if nodeK[j] == tree && (e2 < d2 || (e2 == d2 && j < i)) {
						nodeK[i] = 0
						break
					}
				}
			}
		}
	}
}

// dressIsland places a major island's nodes, decor and tileVis.
func dressIsland(B box, C *scratch, k int) {
	land, el, dO, dW, dL, dT, H := C.land, C.el, C.dO, C.dW, C.dL, C.dT, C.H
	aux1, aux2, aux3, noNode, nodeK, decorK, tileVis := C.aux1, C.aux2, C.aux3, C.noNode, C.nodeK, C.decorK, C.tileVis
	elevN, veg, sct := C.elevN, C.veg, C.sct
	P := &islePlans[k]
	w := B.w
	gi := func(li int) int { return (B.y0+li/w)*SIZE + B.x0 + li%w }
	// tiles no node or decor may take: off the island, near a POI, inside a clearing's free radius
	for li := 0; li < B.n; li++ {
		noNode[li] = 1
	}
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		noNode[li] = b2u(nearPOI(B.x0+li%w, B.y0+li/w))
	}
	for _, c := range Clearings {
		if c.K == k {
			disc(B, c.X, c.Y, c.Free, func(li, _, _ int) { noNode[li] = 1 })
		}
	}
	open := func(li int) bool { return noNode[li] == 0 && nodeK[gi(li)] == 0 }

	// Woods forest density (aux1, 0..1) and its raw field (aux3, for ranking)
	if k == 0 {
		for li := 0; li < B.n; li++ {
			if land[li] == 0 {
				continue
			}
			x, y := B.x0+li%w, B.y0+li/w
			fx, fy := float64(x), float64(y)
			e := el[li]
			lvl := 0.0
			if e == 3 {
				lvl = 0.22
			} else if e == 2 {
				lvl = 0.06
			}
			f := float64(0.75*veg.Eval(fx/34, fy/34)) + float64(0.25*veg.Eval(fx/13+31, fy/13+31)) + lvl
			sp := jsHypot2(float64(x-spawnX), float64(y-spawnY))
			if sp < 20 {
				f += float64(float64(0.15*ramp(sp, 3, 7)) * ramp(20-sp, 0, 6))
			}
			F := ramp(f, -0.3, 0.6)
			g := veg.Eval(fx/6.5+90, fy/6.5+90)
			if g > 0.5 {
				F *= ramp(0.7-g, 0, 0.2)
			}
			keep := ramp(float64(dW[li])/3+float64(2.5*sct.Eval(fx/5+770, fy/5+770)), 1.5, 4) // a ragged setback from the water
			aux1[li] = float64(F * keep)
			aux3[li] = f - float64(0.6*(1-keep))
		}
	}

	// node scorers: score(li, x, y), or no when the tile is not a candidate
	S := map[string]func(li, x, y int) float64{
		"star": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			return sct.Eval(float64(x)/2+700, float64(y)/2+700)
		},
		"forest": func(li, x, y int) float64 {
			if !open(li) || aux1[li] <= 0.3 {
				return no
			}
			grove := sct.Eval(float64(x)/4.5+510, float64(y)/4.5+510)
			if grove < -0.5 || grain(sct, x, y, 505) < -0.35 {
				return no
			}
			return aux3[li] + float64(0.12*grove) + float64(0.18*grain(sct, x, y, 500))
		},
		"lone": func(li, x, y int) float64 {
			if open(li) && aux1[li] <= 0.3 && dW[li] >= 9 && dT[li] > 12 {
				return grain(sct, x, y, 520)
			}
			return no
		},
		"edge": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			e := 0.0
			if dT[li] > 0 && dT[li] <= 12 {
				e = 1.2 - float64(dT[li])/12
			}
			if dL[li] <= 6 {
				e += float64(0.5 * ramp(sct.Eval(float64(x)/6+775, float64(y)/6+775), 0, 0.4)) // patches along the lake
			}
			if e > 0 {
				return e + float64(0.4*grain(sct, x, y, 540))
			}
			return no
		},
		"scarp": func(li, x, y int) float64 {
			if open(li) && aux2[li] > 0.2 {
				return aux2[li] + float64(0.3*grain(sct, x, y, 560))
			}
			return no
		},
		"spawnRock": func(li, x, y int) float64 {
			sp := jsHypot2(float64(x-spawnX), float64(y-spawnY))
			if open(li) && sp >= 3 && sp <= 15 {
				return grain(sct, x, y, 560)
			}
			return no
		},
		"hill": func(li, x, y int) float64 {
			if open(li) && el[li] == 3 {
				return grain(sct, x, y, 560)
			}
			return no
		},
		"field": func(li, x, y int) float64 {
			if open(li) {
				return grain(sct, x, y, 560)
			}
			return no
		},
		// Dunes boulders: scree aprons round the rocks first, then rocky reg flats and the rock tops
		"outcrop": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			mv := aux2[li]
			flat := 0.3
			if el[li] == 1 {
				flat = 1
			}
			reg := float64(ramp(elevN.Eval(float64(x)/14+2380, float64(y)/14+2380), 0.25, 0.55) * flat)
			apron := 0.75
			if !(mv > 0.08) {
				apron = ramp(mv, -0.45, -0.05)
			}
			return apron + float64(0.7*reg) + float64(0.4*grain(sct, x, y, 600))
		},
		"pebble": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			return float64(0.6*ramp(aux2[li], -0.6, -0.1)) + float64(0.4*ramp(elevN.Eval(float64(x)/14+2380, float64(y)/14+2380), 0.15, 0.55)) +
				float64(0.45*grain(sct, x, y, 620)) + float64(0.25*(1-aux1[li]))
		},
		"crevasse": func(li, x, y int) float64 {
			if !open(li) || el[li] < 2 {
				return no
			}
			top := 0.0
			if el[li] == 3 {
				top = 0.3
			}
			return float64(0.8*ramp(elevN.Eval(float64(x)/11+2100, float64(y)/11+2100), 0.05, 0.5)) + top +
				float64(0.15*sct.Eval(float64(x)/4+660, float64(y)/4+660)) + float64(0.3*grain(sct, x, y, 660))
		},
		"foot": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			foot := 0
			for _, o := range [12]int{-w - 1, -w, -w + 1, -1, 1, w - 1, w, w + 1, -2 * w, 2 * w, -2, 2} {
				if d := int(el[li+o]) - int(el[li]); land[li+o] != 0 && d > foot {
					foot = d
				}
			}
			return float64(0.5*float64(foot)) + float64(0.4*grain(sct, x, y, 680))
		},
		"scree": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			low := 0.0
			if el[li] == 1 {
				low = 0.2
			}
			return grain(sct, x, y, 700) + low
		},
		"reed": func(li, x, y int) float64 {
			if !open(li) || el[li] != 1 {
				return no
			}
			// reed beds: patches along the banks, a few on the mudflats, irregular within
			bank, flat := 0.0, 0.0
			if float64(dL[li])/3 <= 2.5 {
				bank = float64(1.2 * ramp(sct.Eval(float64(x)/6+730, float64(y)/6+730), -0.15, 0.3))
			}
			if dO[li] <= 6 {
				flat = float64(0.45 * ramp(sct.Eval(float64(x)/7+735, float64(y)/7+735), 0, 0.4))
			}
			return bank + flat + float64(0.3*grain(sct, x, y, 720))
		},
		"bog": func(li, x, y int) float64 {
			if !open(li) {
				return no
			}
			high := 0.0
			if el[li] >= 2 {
				high = 0.3
			}
			return float64(0.9*ramp(elevN.Eval(float64(x)/10+2400, float64(y)/10+2400), 0.15, 0.55)) +
				float64(0.15*sct.Eval(float64(x)/4+740, float64(y)/4+740)) + float64(0.3*grain(sct, x, y, 740)) - high
		},
	}
	for _, q := range nodeQuotas[k] {
		if q.scorer == "edge" {
			spaceGladeTrees(nodeK) // all trees are down: thin the spawn glade's edge
		}
		if q.scorer == "lone" || q.scorer == "edge" { // distance to the trees placed so far
			for li := 0; li < B.n; li++ {
				C.mask[li] = b2u(land[li] != 0 && nodeK[gi(li)] == NodeTree+1)
			}
			chamfer(B, C.mask, dT)
		}
		if rockKind(q.kind) {
			placeRocks(B, C, q.count, S[q.scorer], q.kind, q.kind == NodeBoulder)
			continue
		}
		for _, li := range pickTop(B, C, q.count, S[q.scorer]) {
			nodeK[gi(int(li))] = q.kind + 1
		}
	}

	// decor: the island's prop on the best spots of its own kind of ground; never
	// on nodes, near a POI, in a clearing, or within 20 of the spawn
	decorScore := func(li, x, y int) float64 {
		if !open(li) || jsHypot2(float64(x-spawnX), float64(y-spawnY)) < 20 {
			return no
		}
		ds := float64(0.6*sct.Eval(float64(x)/3+400, float64(y)/3+400)) + float64(0.4*grain(sct, x, y, 400))
		e := el[li]
		switch k {
		case 0: // glow mushrooms in the woods' shade
			if aux1[li] > 0.45 {
				return ds
			}
		case 1: // ruined pillars out on the pans
			if e <= 2 {
				return ds + float64(0.4*(1-aux1[li]))
			}
		case 2: // ice crystals on the heights
			if e >= 2 {
				return ds
			}
		default: // bone totems by the pools
			if e <= 2 {
				pool := 0.0
				if dL[li] <= 12 {
					pool = 0.3
				}
				return ds + pool
			}
		}
		return no
	}
	for _, li := range pickTop(B, C, decorQuota[k], decorScore) {
		decorK[gi(int(li))] = uint8(k + 1)
	}

	// tileVis: each biome's variant tile in coherent patches where it belongs
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		fx, fy := float64(B.x0+li%w), float64(B.y0+li/w)
		e := el[li]
		var vis bool
		switch k {
		case 0: // flower meadows
			vis = aux1[li] < 0.25 && dW[li] >= 6 &&
				float64(0.7*sct.Eval(fx/11+580, fy/11+580))+float64(0.3*sct.Eval(fx/3+590, fy/3+590)) > 0.22
		case 1: // cracked pans
			vis = e == 1 && aux1[li] < 0.15 && dW[li] >= 6 && sct.Eval(fx/6+640, fy/6+640) > -0.45
		case 2: // glaciers, ice shelves
			vis = (e == 3 && elevN.Eval(fx/16+2200, fy/16+2200) > -0.15) ||
				(e == 1 && dO[li] <= 9 && elevN.Eval(fx/22+2300, fy/22+2300) > 0.3)
		default: // moss on hummocks
			if e >= 2 {
				vis = sct.Eval(fx/5+760, fy/5+760) > -0.5
			} else {
				vis = H[li] > P.t1-0.05 && sct.Eval(fx/5+760, fy/5+760) > 0
			}
		}
		tileVis[gi(li)] = b2u(vis)
	}
}

// --- 2. the Core --------------------------------------------------------------
// An irregular blighted crater rim, always level 1; ash near its edge; a few
// lava vents outside the temple clearing.
func coreRegion(C *scratch) {
	B := makeBox(CORE[0], CORE[1], 20)
	land, carved, val, lab := C.land, C.carved, C.val, C.lab
	sct, coast, shape := C.sct, C.coast, C.shape
	for li := 0; li < B.n; li++ {
		x, y := B.x0+li%B.w, B.y0+li/B.w
		fx, fy := float64(x), float64(y)
		dx, dy := float64(x-CORE[0]), float64(y-CORE[1])
		d := jsHypot2(dx, dy)
		ux, uy := 1.0, 0.0
		if d > 1e-9 {
			ux, uy = dx/d, dy/d
		}
		R := math.Min(17.8, math.Max(12.6, 15+float64(2.2*shape.Eval(float64(ux*0.7)+500, float64(uy*0.7)+500))+
			float64(1.1*coast.Eval(fx/4.5+600, fy/4.5+600))))
		val[li] = R - d // how far inside the rim
		land[li], carved[li] = b2u(d < R), 0
	}
	// one landmass, no holes
	sizes := components(B, land, 1, lab, C.queue)
	main := 1
	for l := 2; l < len(sizes); l++ {
		if sizes[l] > sizes[main] {
			main = l
		}
	}
	for li := 0; li < B.n; li++ {
		if land[li] != 0 && int(lab[li]) != main {
			land[li] = 0
		}
	}
	fillSmallWater(B, C, B.n)
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		x, y := B.x0+li%B.w, B.y0+li/B.w
		i := y*SIZE + x
		C.tiles[i], C.elev[i] = TBlight, 1
		a := sct.Eval(float64(x)/3+800, float64(y)/3+800)
		if val[li] < 2.6 {
			C.tileVis[i] = b2u(a > -0.35)
		} else {
			C.tileVis[i] = b2u(a > 0.55)
		}
	}
	// lava vents: best-scoring open tiles at least 4 apart
	ranked := pickTop(B, C, B.n, func(li, x, y int) float64 {
		if land[li] != 0 && val[li] > 1.5 && jsHypot2(float64(x-CORE[0]), float64(y-CORE[1])) >= 7 && !nearPOI(x, y) {
			return sct.Eval(float64(x)/2.5+850, float64(y)/2.5+850)
		}
		return no
	})
	var vents [][2]int
	for _, li := range ranked {
		if len(vents) >= lavaVents {
			break
		}
		x, y := B.x0+int(li)%B.w, B.y0+int(li)/B.w
		crowded := false
		for _, v := range vents {
			if jsHypot2(float64(v[0]-x), float64(v[1]-y)) < 4 {
				crowded = true
				break
			}
		}
		if crowded {
			continue
		}
		vents = append(vents, [2]int{x, y})
		C.decorK[y*SIZE+x] = uint8(slices.Index(decorKeys, "lava_vent") + 1)
	}
}

// --- 3. minor isles -------------------------------------------------------------
// Each one a single small landmass of its kind's tile: warped, lobed coast
// within r + 5.5 of its centre, a domed interior, the old node rule.
func minorPass(m MinorIsle, j int, C *scratch) {
	B := makeBox(m.X, m.Y, m.R+8)
	land, carved, ocean, val, dO, H, el, lab := C.land, C.carved, C.ocean, C.val, C.dO, C.H, C.el, C.lab
	elevN, veg, sct, coast, warp, shape := C.elevN, C.veg, C.sct, C.coast, C.warp, C.shape
	tile, st := minorKindTile[m.Kind], minorStyles[m.Kind]
	fr := float64(m.R)
	cnt := 0
	for li := 0; li < B.n; li++ {
		x, y := B.x0+li%B.w, B.y0+li/B.w
		fx, fy := float64(x), float64(y)
		ox, oy := float64(x-m.X), float64(y-m.Y)
		land[li], carved[li] = 0, 0
		d := jsHypot2(ox, oy)
		if d > fr+5.5 {
			val[li] = -9
			continue
		}
		wx := ox + float64(2.5*warp.Eval(fx/11+900, fy/11+900))
		wy := oy + float64(2.5*warp.Eval(fx/11+950, fy/11+950))
		q := jsHypot2(wx, wy)
		ux, uy := 1.0, 0.0
		if q > 1e-9 {
			ux, uy = wx/q, wy/q
		}
		v := 1 - q/(fr*(1+float64(0.18*shape.Eval(float64(ux*0.6)+300+float64(j*17), float64(uy*0.6)+300)))) +
			float64(0.12*coast.Eval(fx/6+700, fy/6+700)) + float64(0.05*coast.Eval(fx/3+750, fy/3+750))
		if d > fr+2 {
			v -= float64((d - fr - 2) * 0.08)
		}
		val[li] = v
		C.sortBuf[cnt] = v
		cnt++
	}
	area := int(math.Floor(float64(float64(3.14159*(fr+0.45))*(fr+0.45)) + 0.5))
	sorted := C.sortBuf[:cnt]
	sort.Float64s(sorted)
	th := sorted[cnt-area]
	for li := 0; li < B.n; li++ {
		if val[li] >= th {
			land[li] = 1
		}
	}
	// a single landmass with no lakes
	sizes := components(B, land, 1, lab, C.queue)
	main := 1
	for l := 2; l < len(sizes); l++ {
		if sizes[l] > sizes[main] {
			main = l
		}
	}
	for li := 0; li < B.n; li++ {
		if land[li] != 0 && int(lab[li]) != main {
			land[li] = 0
		}
	}
	fillSmallWater(B, C, B.n)
	markOcean(B, C)
	chamfer(B, ocean, dO)
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			el[li] = 0
			continue
		}
		fx, fy := float64(B.x0+li%B.w), float64(B.y0+li/B.w)
		H[li] = float64(ramp(float64(dO[li])/3+float64(1.2*elevN.Eval(fx/5+2650, fy/5+2650)), 1.6, st.shore+2) *
			(0.35 + float64(0.65*(0.5+float64(0.5*elevN.Eval(fx/9+2600, fy/9+2600))))))
		switch {
		case H[li] > st.t2:
			el[li] = 3
		case H[li] > st.t1:
			el[li] = 2
		default:
			el[li] = 1
		}
	}
	smoothLevels(B, C, 8)
	smoothLevels(B, C, 8)
	smoothLevels(B, C, 4)
	ensureClimbable(B, C)
	settleTerraces(B, C)
	for round := 0; round < 3 && levelStubs(B, C); round++ {
		settleTerraces(B, C)
	}
	for li := 0; li < B.n; li++ {
		if land[li] == 0 {
			continue
		}
		x, y := B.x0+li%B.w, B.y0+li/B.w
		fx, fy := float64(x), float64(y)
		i := y*SIZE + x
		C.tiles[i] = tile
		if tile == TBlight {
			C.elev[i] = 1
		} else {
			C.elev[i] = el[li]
		}
		C.tileVis[i] = b2u(sct.Eval(fx/7+55, fy/7+55) > 0.45)
		if tile == TBlight || nearPOI(x, y) {
			continue
		}
		// minor island nodes: TREE (driftwood only), BUSH, STONE — no crystal/boulder/starmetal
		v, s := veg.Eval(fx/6, fy/6), sct.Eval(fx/3, fy/3)
		if m.Kind == "driftwood" && v > 0.62 {
			C.nodeK[i] = NodeTree + 1
		} else if s > 0.80 {
			C.nodeK[i] = NodeBush + 1
		} else if s < -0.85 {
			C.nodeK[i] = NodeStone + 1
		}
	}
}

// World mirrors the object returned by genWorld().
type World struct {
	Tiles     []uint8
	Elev      []uint8
	Veins     []uint8
	WaterTemp []uint8
	TileVis   []uint8
	Nodes     map[int]uint8
	Bergs     map[int]bool
	Decor     map[int]string
}

// GenWorld is genWorld(seed).
func GenWorld(seed string) *World {
	const NN = SIZE * SIZE
	const BN = (2*majorHalf + 1) * (2*majorHalf + 1)
	u8 := func(n int) []uint8 { return make([]uint8, n) }
	i32 := func(n int) []int32 { return make([]int32, n) }
	f64 := func(n int) []float64 { return make([]float64, n) }
	C := &scratch{
		elevN: NewNoise2D(NewAlea(seed + "e")), // landforms: hills, dunes, tiers, hummocks, pools
		veg:   NewNoise2D(NewAlea(seed + "t")), // forests, minor-isle trees
		sct:   NewNoise2D(NewAlea(seed + "s")), // scatter: veins, bergs, starmetal, node jitter, tileVis, decor
		coast: NewNoise2D(NewAlea(seed + "c")), // coastline and lake-edge detail
		warp:  NewNoise2D(NewAlea(seed + "w")), // domain warp, offshore islets
		shape: NewNoise2D(NewAlea(seed + "o")), // island layout: lobes, feature rotation, lake jitter

		tiles: u8(NN), elev: u8(NN), tileVis: u8(NN), nodeK: u8(NN), decorK: u8(NN),

		land: u8(BN), carved: u8(BN), ocean: u8(BN), mask: u8(BN), noNode: u8(BN), el: u8(BN), el2: u8(BN), reach: u8(BN),
		lab: i32(BN), queue: i32(BN), parent: i32(BN), dO: i32(BN), dW: i32(BN), dL: i32(BN), dT: i32(BN),
		val: f64(BN), px: f64(BN), py: f64(BN), H: f64(BN), aux1: f64(BN), aux2: f64(BN), aux3: f64(BN), sortBuf: f64(BN),
		cand: i32(BN), score: f64(BN), order: i32(BN),
		corner: i32((2*majorHalf + 2) * (2*majorHalf + 2)),
	}
	tiles, elev, nodeK := C.tiles, C.elev, C.nodeK
	for i := range tiles {
		tiles[i] = TWater
	}
	veins, waterTemp := u8(NN), u8(NN)

	// ---- 1-3. islands ----
	for k := 0; k < 4; k++ {
		islandPass(k, C)
	}
	coreRegion(C)
	for j, m := range MinorIsles {
		minorPass(m, j, C)
	}

	// ---- 4. stamp solid ground under landmark footprints (seed-independent guarantee) ----
	for _, m := range Mountains {
		isCore := jsHypot2(float64(m.X-CORE[0]), float64(m.Y-CORE[1])) < 20
		k, bd := 0, math.Inf(1)
		for q := 0; q < 4; q++ {
			d := jsHypot2(float64(m.X-ISLES[q][0]), float64(m.Y-ISLES[q][1]))
			if d < bd {
				bd = d
				k = q
			}
		}
		ground := isleT[k]
		if isCore {
			ground = TBlight
		}
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				i := (m.Y+dy)*SIZE + (m.X + dx)
				if tiles[i] == TWater {
					tiles[i], elev[i], nodeK[i] = ground, 1, 0
				}
			}
		}
	}

	// ---- 5. derived layers, now every tile is final ----
	sct := C.sct
	for y := 0; y < SIZE; y++ {
		fy := float64(y)
		for x := 0; x < SIZE; x++ {
			fx := float64(x)
			i := y*SIZE + x
			vm := sct.Eval(fx/5+250, fy/5+250)
			switch {
			case vm > 0.68:
				veins[i] = 1
			case vm < -0.74:
				veins[i] = 2
			}
			if tiles[i] != TWater {
				continue
			}
			if jsHypot2(float64(x-ISLES[2][0]), float64(y-ISLES[2][1])) < ISLE_R+180 {
				waterTemp[i] = 1 // freezing
			} else if jsHypot2(float64(x-CORE[0]), float64(y-CORE[1])) < 125 {
				waterTemp[i] = 2 // hot
			}
		}
	}
	w := &World{
		Tiles: tiles, Elev: elev, Veins: veins, WaterTemp: waterTemp, TileVis: C.tileVis,
		Nodes: map[int]uint8{}, Bergs: map[int]bool{}, Decor: map[int]string{},
	}
	// iceberg ring around the Frozen Spire — lethal to wooden hulls
	const BR = ISLE_R + 135
	for y := max(0, ISLES[2][1]-BR); y <= min(SIZE-1, ISLES[2][1]+BR); y++ {
		for x := max(0, ISLES[2][0]-BR); x <= min(SIZE-1, ISLES[2][0]+BR); x++ {
			i := y*SIZE + x
			dSpire := jsHypot2(float64(x-ISLES[2][0]), float64(y-ISLES[2][1]))
			if tiles[i] == TWater && dSpire > ISLE_R+8 && dSpire < ISLE_R+135 && sct.Eval(float64(x)/2+900, float64(y)/2+900) > 0.88 {
				w.Bergs[i] = true
			}
		}
	}

	// ---- 6. nodes and decor (the JS Maps, in tile-index order) ----
	for i := 0; i < NN; i++ {
		if nodeK[i] != 0 {
			w.Nodes[i] = nodeK[i] - 1
		}
		if C.decorK[i] != 0 {
			w.Decor[i] = decorKeys[C.decorK[i]-1]
		}
	}
	return w
}

func sortedKeys[V any](m map[int]V) []int {
	out := make([]int, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Ints(out)
	return out
}

// SortedNodeKeys returns the node tile indices in ascending order.
func SortedNodeKeys(m map[int]uint8) []int { return sortedKeys(m) }

// SortedBergKeys returns the iceberg tile indices in ascending order.
func SortedBergKeys(m map[int]bool) []int { return sortedKeys(m) }

// SortedDecorKeys returns the decor tile indices in ascending order.
func SortedDecorKeys(m map[int]string) []int { return sortedKeys(m) }

// NearestLand is nearestLand(world, x, y).
func NearestLand(w *World, x, y float64) [2]float64 {
	for r := 1; r < 130; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				nx := jsRound(x + float64(dx))
				ny := jsRound(y + float64(dy))
				if nx < 0 || ny < 0 || nx >= SIZE || ny >= SIZE {
					continue
				}
				if w.Tiles[int(ny)*SIZE+int(nx)] != TWater {
					return [2]float64{nx, ny}
				}
			}
		}
	}
	return [2]float64{x, y}
}

// Diggable is DIGGABLE(world, i): mining under Woods, Dunes and Spire only.
func Diggable(w *World, i int) bool {
	return w.Tiles[i] == TGrass || w.Tiles[i] == TSand || w.Tiles[i] == TSnow
}

// FindSpawn is findSpawn(world).
func FindSpawn(w *World) [2]int {
	sx, sy := ISLES[0][0]-12, ISLES[0][1]-12 // (168, 168)
	for r := 0; r < 50; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				x, y := sx+dx, sy+dy
				if x < 0 || y < 0 || x >= SIZE || y >= SIZE {
					continue
				}
				i := y*SIZE + x
				if w.Tiles[i] == TGrass {
					if _, has := w.Nodes[i]; !has {
						return [2]int{x, y}
					}
				}
			}
		}
	}
	return [2]int{sx, sy}
}

// FindNearestValidTile is findNearestValidTile(cx, cy, pred, maxR).
func FindNearestValidTile(cx, cy int, pred func(x, y, i int) bool, maxR int) (int, int, bool) {
	for r := 0; r <= maxR; r++ {
		for dy := -r; dy <= r; dy++ {
			for dx := -r; dx <= r; dx++ {
				if max(abs(dx), abs(dy)) != r {
					continue
				}
				x, y := cx+dx, cy+dy
				if x < 0 || y < 0 || x >= SIZE || y >= SIZE {
					continue
				}
				if pred(x, y, y*SIZE+x) {
					return x, y, true
				}
			}
		}
	}
	return 0, 0, false
}

// Medic is one entry of findMedicSpawns().
type Medic struct {
	ID         string
	IslandID   string
	Sprite     string
	X, Y       int
	HutSprite  string
	HutX, HutY int
}

// FindMedicSpawns is findMedicSpawns(world).
func FindMedicSpawns(w *World) ([]Medic, error) {
	out := make([]Medic, 0, len(MedicIslands))
	for _, mi := range MedicIslands {
		var island MajorIsland
		for _, cand := range MajorIslands {
			if cand.ID == mi.IslandID {
				island = cand
				break
			}
		}
		off := medicAnchorOffsets[mi.IslandID]
		cx, cy := island.Center[0], island.Center[1]
		x, y, ok := FindNearestValidTile(cx+off[0], cy+off[1], func(tx, ty, i int) bool {
			_, hasNode := w.Nodes[i]
			return w.Tiles[i] == island.Tile && !hasNode && !LandmarkBlock[i] &&
				jsHypot(float64(tx-cx), float64(ty-cy)) > 5
		}, 40)
		if !ok {
			return nil, fmt.Errorf("findMedicSpawns: no valid tile for medic-%s", mi.IslandID)
		}
		hx, hy, ok := FindNearestValidTile(x, y-2, func(tx, ty, i int) bool {
			_, hasNode := w.Nodes[i]
			return w.Tiles[i] == island.Tile && !hasNode && !LandmarkBlock[i] &&
				jsHypot(float64(tx-x), float64(ty-y)) >= 2
		}, 6)
		if !ok {
			return nil, fmt.Errorf("findMedicSpawns: no valid hut tile for medic-%s", mi.IslandID)
		}
		hutSprite := "medic_hut"
		if mi.IslandID == "spire" {
			hutSprite = "medic_hut_snow"
		}
		out = append(out, Medic{
			ID: "medic-" + mi.IslandID, IslandID: mi.IslandID, Sprite: mi.Sprite,
			X: x, Y: y, HutSprite: hutSprite, HutX: hx, HutY: hy,
		})
	}
	return out, nil
}

// MedicBlockTiles is medicBlockTiles(medics).
func MedicBlockTiles(medics []Medic) map[int]bool {
	s := map[int]bool{}
	for _, m := range medics {
		s[m.HutY*SIZE+m.HutX] = true
	}
	return s
}
