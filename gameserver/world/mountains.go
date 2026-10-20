package world

import (
	"container/heap"
	"math"
	"sort"
)

// =============================================================================
// Tile mountains: a Go-only pass the room stamps on top of GenWorld.
//
// shared/world.js only marks each MOUNTAINS site with a 3x3 landmark footprint
// and the client draws an SVG mountain on it. This pass builds a real mountain
// out of terrain instead: stacked tiles rising 26-28 levels above the local
// ground on the Spire, ~22 on the Woods, 14-16 on the Dunes, 12-14 on the
// Marsh and 8-10 on the Core, each in its own island's form — a horn, a
// snow-capped massif, a sheer butte, a craggy tor and an obsidian spike.
//
// Seen in the iso view a level is 10 px and a tile 64x32, so a heap of tiles
// only reads as a mountain where it is steep: nine levels spread over fourteen
// tiles look like a low mound, and terraces in rings look like a ziggurat. So
// every mountain is one compact, steep core — radius 6-9 tiles, 3-4 levels per
// tile under the summit — standing on lower slopes that ease off into the
// island's own ground. Its outline is a star, not a ring: 3-5 spurs run down
// from one dominant summit, with gullies between them, the whole outline
// warped by noise and the high ground broken by crags, so no contour closes
// round the mountain at one level.
//
// Like the village camps (folk.go) it is not part of the bit-exact port: the
// JS generator and the parity digests never see it. It is a pure function of
// the generated world and the seed — BuildMountains reads the world and never
// writes it — and its result is a list of tile changes that carries the base
// value of everything it touches, so a room can stamp it onto its own copy of
// the world and lift it off again at runtime (the tileMountains feature flag,
// gameserver/room/features.go) without regenerating anything.
//
// Every changed tile keeps its tile type (the island's ground), so biome rules
// — exposure, footstep sounds, digging, what grows — are untouched. The
// mountain is told apart by tileVis (VisRock..VisTrail below), which the client
// maps to the per-island rock art (src/main.ts tileKey). The lower slopes —
// everything up to rockLine levels over the base, and the gentle shoulders
// below the core — stay the island's own ground: grass, sand, snow, mud, with
// their ordinary ledges and whatever grows there. So the rock starts where the
// mountain steepens, and never as a lone block in the fields.
//
// No face between two neighbouring tiles is taller than maxStep (4) levels, so
// a fall costs at most 3 hp (room/movement.go). The one way up is the trail: a
// ledge cut into the mountainside, rising at most a level a tile, that winds
// right round the big mountains on its way from the foot to a small summit.
//
// The massif never covers water (it keeps massifSpec.shore tiles of ordinary
// ground off every shore and lake), the clearings (monolith plazas, lore
// notes, the spawn, the medics, the village yards — with their free and land
// radii plus reserveMargin), the monoliths or the temple pieces, and it eases
// down before reaching any of them (capSlope) rather than ending in a cut cliff.
// =============================================================================

// MaxElev is the tallest stack the mountains build. Ordinary terrain is 0..3;
// every consumer of Elev (client stacking, lips, fall damage, repaint boxes,
// the chunk bake's pad) is sized for this.
const MaxElev = 32

// tileVis values on mountain tiles. 0 and 1 are the ordinary ground variants
// (shared/world.js); the mountain values are Go-only and start above them.
const (
	VisRock      uint8 = 2 // bare rock: a cliff edge (a neighbour 2+ levels below)
	VisLedge     uint8 = 3 // a walkable terrace on the high ground: rock with scree and ground cover
	VisSnow      uint8 = 4 // snow-capped rock, on the upper levels of the Woods and the Spire
	VisSnowLedge uint8 = 5 // a snow-covered terrace
	VisTrail     uint8 = 6 // the trail to the summit
)

// IsMountainVis reports whether a tileVis value marks a mountain tile.
func IsMountainVis(v uint8) bool { return v >= VisRock && v <= VisTrail }

// MountainBlock is the part of LandmarkBlock that is the mountains' 3x3
// footprints (the rest is the temple pieces). With tile mountains on there is
// no sprite there to walk into, so the room stops blocking these tiles.
var MountainBlock = func() map[int]bool {
	s := map[int]bool{}
	for _, m := range Mountains {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				s[(m.Y+dy)*SIZE+(m.X+dx)] = true
			}
		}
	}
	return s
}()

// TileChange is one stamped tile, with the base values it replaced. Keep is a
// lower-slope tile: it is lifted but keeps the island's ground, and the trees,
// bushes and decor on it.
type TileChange struct {
	I                 int
	Elev, Vis         uint8
	BaseElev, BaseVis uint8
	Keep              bool
}

// MountainPeak describes one built massif.
type MountainPeak struct {
	Key  string // the MOUNTAINS entry (its sprite key)
	Site [2]int // the MOUNTAINS site
	// Island is the MajorIslands index, or 4 for the Core.
	Island int
	// Base is the local ground level the massif rises from; Top the summit's.
	Base, Top uint8
	Summit    [2]int
	// Trail runs from a foot tile (ordinary ground) to the summit, 4-connected;
	// each tile is at most one level above the one before it.
	Trail []int
	// Turns is how far the trail winds round the summit on its way up, in
	// turns (1 = once right round).
	Turns float64
	// X0..Y1 is the bounding box of the stamped tiles (inclusive).
	X0, Y0, X1, Y1 int
}

// MountainStamp is the whole pass: what to write and what it replaced.
type MountainStamp struct {
	Peaks []MountainPeak
	// Changes is every mountain tile, ascending by index.
	Changes []TileChange
	// Nodes are the boulders, stones, crystals and stunted trees the stamp
	// places on its ledges and lower slopes, and the summit cairns. The nodes
	// and decor of the rock tiles are cleared; the lower slopes (TileChange.Keep)
	// keep theirs.
	Nodes map[int]uint8
	// BaseNodes / BaseDecor are the base world's nodes and decor on the cleared
	// tiles, restored when the stamp is lifted.
	BaseNodes map[int]uint8
	BaseDecor map[int]string
	// Mark is every mountain tile; Face the steep ones (rock, not a ledge or
	// the trail), where nothing spawns.
	Mark map[int]bool
	Face map[int]bool
}

// Apply writes the stamp into w (a world the caller owns, never a shared one).
func (s *MountainStamp) Apply(w *World) {
	for _, c := range s.Changes {
		w.Elev[c.I], w.TileVis[c.I] = c.Elev, c.Vis
		if !c.Keep {
			delete(w.Nodes, c.I)
			delete(w.Decor, c.I)
		}
	}
	for i, k := range s.Nodes {
		w.Nodes[i] = k
	}
}

// Revert lifts the stamp off w again, restoring every base value it replaced.
func (s *MountainStamp) Revert(w *World) {
	for _, c := range s.Changes {
		w.Elev[c.I], w.TileVis[c.I] = c.BaseElev, c.BaseVis
		if c.Keep {
			// a lower slope keeps its base cover: only the scree the stamp added goes
			if _, placed := s.Nodes[c.I]; placed {
				delete(w.Nodes, c.I)
			}
			continue
		}
		if k, ok := s.BaseNodes[c.I]; ok {
			w.Nodes[c.I] = k
		} else {
			delete(w.Nodes, c.I)
		}
		if d, ok := s.BaseDecor[c.I]; ok {
			w.Decor[c.I] = d
		} else {
			delete(w.Decor, c.I)
		}
	}
}

// The forms a massif takes.
const (
	// formPeak: a summit crest with spurs and gullies; a horn (horn levels at
	// hornSteep under the summit), a steep core (steep levels a tile) down to
	// the knee, then a break of slope to lower slopes easing off over run tiles
	// (the Woods, the Spire, the Core's spike).
	formPeak = iota
	// formButte: a flat-topped sandstone tower with sheer walls on a talus
	// apron, a caprock knob for its summit (the Dunes).
	formButte
	// formTor: a rounded hill, its granite crown broken into blocky outcrops
	// split by clefts, the tallest of them the summit (the Marsh).
	formTor
)

// massifSpec is the shape of one island's mountain.
type massifSpec struct {
	form int
	h    int // rise of the summit above the local ground, levels (wear and the summit take a level or two off)
	// steep is the core's slope, levels a tile, and knee the height over the
	// base where it breaks to the lower slopes; a tor's knee is the top of its
	// dome. A peak a trail must wind round keeps steep well under maxStep: one
	// turn of the trail passes the turn below it across slope as steep as this,
	// and the trail's cuts need the slack.
	steep, knee float64
	// top is a butte's flat top's radius, tiles; foot how far the slopes run out
	// from the summit before they meet the ground down a gully (a peak's follows
	// from h, horn, steep, knee and run). A spur runs out reach times as far as
	// a gully, and the whole massif is drawn out elong times along its long axis.
	top, foot, reach, elong float64
	// run is how far a peak's lower slopes run out below the knee, tiles; horn
	// how many levels under its summit are a horn, hornSteep levels a tile.
	run, horn float64
	// crest is the summit's length along the long axis, tiles: 1 makes a summit
	// of two tiles; summit is how many tiles the summit may have (2-4).
	crest  float64
	summit int
	spurs  int     // ridges running down from the summit
	crag   float64 // the crags' amplitude on the high ground, levels
	warp   float64 // how far the outline wanders, tiles
	snowAt int     // rise from which the high ground is snow (0: never)
	trees  float64 // chance of a stunted tree on a low ledge
	wrap   bool    // the trail winds once right round the summit
	shore  float64 // tiles of ordinary ground kept between the massif and any water
	near   float64 // how far from the site the summit may move to find the room it needs
}

// massifSpecs by island: Woods (a snow-capped massif, ~+22), Dunes (a sheer
// sandstone butte, +15), Spire (a horned peak, the tallest, +26..28), Marsh (a
// craggy tor, +13), then the Core — a small obsidian spike (+9) between the
// north shore and the temple.
var massifSpecs = [5]massifSpec{
	{form: formPeak, h: 25, steep: 3.4, knee: 8, run: 4.5, reach: 1.25, elong: 1.05, crest: 1, summit: 3, spurs: 4, crag: 1.2, warp: 1.1, snowAt: 16, trees: 0.12, wrap: true, shore: 2, near: 5},
	{form: formButte, h: 15, steep: 4, top: 3, foot: 8, reach: 1.15, elong: 1.35, summit: 3, spurs: 4, crag: 0.4, warp: 0.9, wrap: true, shore: 2, near: 5},
	{form: formPeak, h: 28, steep: 3.3, knee: 9, run: 5, horn: 9, reach: 1.25, elong: 1, crest: 1, summit: 3, spurs: 5, crag: 1.2, warp: 1.1, snowAt: 14, wrap: true, shore: 2, near: 5},
	{form: formTor, h: 13, knee: 10, foot: 7.5, reach: 1.2, elong: 1.15, summit: 3, spurs: 3, crag: 0.9, warp: 1.2, wrap: true, shore: 1.5, near: 4},
	{form: formPeak, h: 11, steep: 4, knee: 4, run: 2.6, reach: 1.35, elong: 1, crest: 1, summit: 2, spurs: 3, crag: 0.4, warp: 0.6, wrap: true, shore: 1, near: 9},
}

const (
	reserveMargin = 2 // tiles kept clear round every clearing, monolith and temple piece
	// maxStep is the tallest face between two neighbouring tiles: a fall off it
	// costs maxStep-1 hp.
	maxStep = 4
	// rockLine: the lower slopes, up to this many levels over the base, stay
	// the island's own ground; rock starts above it.
	rockLine = 3
	// capSlope: within this many levels a tile of room the massif eases down
	// toward water and reserved ground.
	capSlope = 2.5
	// trailGap: how far apart (Manhattan) two stretches of the trail are checked
	// for a slope between them steeper than maxStep a tile
	trailGap = 3
	// hornSteep: a horn's slope, levels a tile
	hornSteep = 3.9
	// minCrag: the smallest patch of rock left standing apart from the core
	minCrag = 6
)

// mountainReserved is every tile the mountains must never touch, apart from
// water: the clearings with their free and land radii (monolith plazas, lore
// notes, the spawn, the medics, the village yards), the monoliths, the temple
// pieces and the medics' own tiles, each with reserveMargin. The folk camps
// (FindFolkCamps, which the room runs on the stamped world) stand in their
// village's clearing.
func mountainReserved(w *World) func(x, y int) bool {
	type disc struct{ x, y, r float64 }
	var discs []disc
	for _, c := range Clearings {
		discs = append(discs, disc{float64(c.X), float64(c.Y), float64(max(c.Free, c.Land)) + reserveMargin})
	}
	for _, m := range MONOLITHS {
		discs = append(discs, disc{float64(m[0]), float64(m[1]), 3 + reserveMargin})
	}
	for _, p := range TemplePieces {
		discs = append(discs, disc{float64(p.I % SIZE), float64(p.I / SIZE), 1 + reserveMargin})
	}
	if medics, err := FindMedicSpawns(w); err == nil {
		for _, md := range medics {
			discs = append(discs, disc{float64(md.X), float64(md.Y), 2 + reserveMargin},
				disc{float64(md.HutX), float64(md.HutY), 2 + reserveMargin})
		}
	}
	return func(x, y int) bool {
		for _, d := range discs {
			if math.Hypot(float64(x)-d.x, float64(y)-d.y) <= d.r {
				return true
			}
		}
		return false
	}
}

// BuildMountains builds the tile mountains for a generated world. It is
// deterministic in (w, seed) and reads w without writing it.
func BuildMountains(w *World, seed string) *MountainStamp {
	s := &MountainStamp{
		Nodes: map[int]uint8{}, BaseNodes: map[int]uint8{}, BaseDecor: map[int]string{},
		Mark: map[int]bool{}, Face: map[int]bool{},
	}
	reserved := mountainReserved(w)
	for _, m := range Mountains {
		buildMassif(w, s, m, seed, reserved)
	}
	sort.Slice(s.Changes, func(a, b int) bool { return s.Changes[a].I < s.Changes[b].I })
	return s
}

// spur is one ridge running down from the summit: its bearing at the summit,
// how far it bends by the time it reaches the foot, its half-width (radians)
// and how much farther than a gully it runs out.
type spur struct{ a, bend, w, reach float64 }

// block is one rock stack of a tor: a rounded box (centre, half-sizes,
// bearing, window coordinates) with its top this many levels over the base.
type block struct{ x, y, a, b, ang, top float64 }

// massif is the working state for one mountain: a square window of the map.
type massif struct {
	B      box
	base   []uint8   // base elevation
	land   []bool    // dry land the massif may stand on
	room   []float64 // tiles from the nearest ground it may not stand on
	rise   []int     // levels above b0 the massif lifts each tile (0: not mountain)
	el     []int     // final elevation
	pinned []bool    // the trail, the summit and benches beside them: wear may not move them
	corner []bool    // tiles widening a diagonal stretch of the trail
	b0     int
	spec   massifSpec
	isle   int
	// the summit point (window coordinates, a tile centre), the bearing of the
	// massif's long axis and the summit crest's run from the summit point
	px, py, axis float64
	cdx, cdy     float64
	lean         float64 // the bearing of the massif's longer side
	spurs        []spur
	blocks       []block
	// summit is the summit tile (window index) once settled, top its level
	summit, top int
	noise       *Noise2D
	rng         *Alea
	// toCentre is the unit vector from the site toward its island's centre:
	// the trail starts on that side, the side the folk and the keepers live on.
	toCentre [2]float64
}

func (M *massif) in(lx, ly int) bool { return lx >= 0 && ly >= 0 && lx < M.B.w && ly < M.B.h }

var dirs4 = [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}

// buildMassif builds one mountain into the stamp: it lays the massif out
// (place, shape), settles it into faces of maxStep or less round a small
// summit, finds and carves the trail, and writes the tiles.
func buildMassif(w *World, s *MountainStamp, m Mountain, seed string, reserved func(x, y int) bool) {
	M := newMassif(w, m, seed, reserved)
	M.place()
	M.shape()
	M.settle()
	path, lv := M.trail()
	if path != nil {
		M.carve(path, lv)
	}
	M.classify(w, s, m, path)
}

// newMassif sets up the window round a site: the base ground, the ground the
// massif may stand on and how much room it has there, and b0.
func newMassif(w *World, m Mountain, seed string, reserved func(x, y int) bool) *massif {
	isle, bd := 0, math.Inf(1)
	for q := 0; q < 4; q++ {
		if d := math.Hypot(float64(m.X-ISLES[q][0]), float64(m.Y-ISLES[q][1])); d < bd {
			isle, bd = q, d
		}
	}
	centre := ISLES[isle]
	if math.Hypot(float64(m.X-CORE[0]), float64(m.Y-CORE[1])) < 20 {
		isle, centre = 4, CORE
	}
	spec := massifSpecs[isle]
	if spec.form == formPeak {
		spec.foot = spec.horn/hornSteep + (float64(spec.h)-spec.horn-spec.knee)/spec.steep + spec.run
	}
	half := int(math.Ceil(spec.foot*spec.reach*spec.elong+spec.near)) + 4
	M := &massif{
		B: makeBox(m.X, m.Y, half), spec: spec, isle: isle,
		noise: NewNoise2D(NewAlea(seed, "mountain-shape", m.Key)),
		rng:   NewAlea(seed, "mountain", m.Key),
	}
	dx, dy := float64(centre[0]-m.X), float64(centre[1]-m.Y)
	if l := math.Hypot(dx, dy); l > 0 {
		M.toCentre = [2]float64{dx / l, dy / l}
	}
	n := M.B.n
	M.base, M.land, M.room = make([]uint8, n), make([]bool, n), make([]float64, n)
	M.rise, M.el, M.pinned, M.corner = make([]int, n), make([]int, n), make([]bool, n), make([]bool, n)
	water, blocked := make([]uint8, n), make([]uint8, n)
	dW, dB := make([]int32, n), make([]int32, n)
	var levels []int
	for li := 0; li < n; li++ {
		x, y := M.B.x0+li%M.B.w, M.B.y0+li/M.B.w
		if x < 0 || y < 0 || x >= SIZE || y >= SIZE {
			water[li] = 1
			continue
		}
		i := y*SIZE + x
		M.base[li] = w.Elev[i]
		if w.Tiles[i] == TWater {
			water[li] = 1
		} else if reserved(x, y) || (LandmarkBlock[i] && !MountainBlock[i]) {
			blocked[li] = 1
		}
		if water[li] == 0 && math.Hypot(float64(x-m.X), float64(y-m.Y)) <= spec.foot*spec.reach {
			levels = append(levels, int(w.Elev[i]))
		}
	}
	// b0, the ground it rises from: the median level round the site
	sort.Ints(levels)
	M.b0 = 1
	if len(levels) > 0 {
		M.b0 = max(1, levels[len(levels)/2])
	}
	M.spec.h = min(spec.h, MaxElev-M.b0)
	chamfer(M.B, water, dW)
	for li := 0; li < n; li++ {
		blocked[li] |= water[li]
	}
	chamfer(M.B, blocked, dB)
	for li := 0; li < n; li++ {
		room := math.Min(float64(dW[li])/3-spec.shore, float64(dB[li])/3-1)
		M.room[li] = room
		M.land[li] = water[li] == 0 && blocked[li] == 0 && room > 0
	}
	return M
}

func smooth01(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// wrapPi brings an angle into (-pi, pi].
func wrapPi(a float64) float64 {
	for a > math.Pi {
		a -= 2 * math.Pi
	}
	for a <= -math.Pi {
		a += 2 * math.Pi
	}
	return a
}

// roomAt samples how much room the massif has at a window point.
func (M *massif) roomAt(x, y float64) float64 {
	lx, ly := int(math.Floor(x)), int(math.Floor(y))
	if !M.in(lx, ly) {
		return -1
	}
	return M.room[ly*M.B.w+lx]
}

// place picks the summit, the long axis and the spurs (or, for a tor, its
// stacks).
//
// The summit is the roomiest tile within spec.near of the site, up to the room
// it needs — enough that the capSlope easing toward the shore and the
// clearings leaves the summit its full height, and most of its foot radius —
// a little nearer the site winning a tie. The long axis is the bearing along which the
// island leaves the most room either side, leaning toward the line across the
// view (map bearing -45 degrees, screen left to right): the camera looks from
// the south-east, so a main ridge across the view stands the summit between
// two shoulders against the sky, the outline that reads as a mountain, where
// one running toward the camera would show it end-on as a lump.
//
// Two spurs run down the long axis, one from each end of the summit crest: the
// main ridge. The rest, shorter, fan out between them on either side, at
// irregular bearings, each turned a little toward open ground.
func (M *massif) place() {
	S, rng := M.spec, M.rng
	w := M.B.w
	jit := func(a float64) float64 { return (rng.Next() - 0.5) * 2 * a }
	cx, cy := M.B.cx-M.B.x0, M.B.cy-M.B.y0
	need := math.Max(S.foot*0.8, float64(S.h)/capSlope+0.5)
	best := math.Inf(-1)
	M.px, M.py = float64(cx)+0.5, float64(cy)+0.5
	nr := int(S.near)
	for dy := -nr; dy <= nr; dy++ {
		for dx := -nr; dx <= nr; dx++ {
			d := math.Hypot(float64(dx), float64(dy))
			if d > S.near || !M.in(cx+dx, cy+dy) || !M.land[(cy+dy)*w+cx+dx] {
				continue
			}
			if v := math.Min(need, M.room[(cy+dy)*w+cx+dx]) - 0.12*d; v > best {
				best, M.px, M.py = v, float64(cx+dx)+0.5, float64(cy+dy)+0.5
			}
		}
	}
	// the open length of a ray from the summit, up to L tiles
	open := func(a, L float64) float64 {
		ux, uy := math.Cos(a), math.Sin(a)
		for t := 1.0; t <= L; t++ {
			if M.roomAt(M.px+ux*t, M.py+uy*t) < 1 {
				return t - 1
			}
		}
		return L
	}
	L := S.foot * S.reach * S.elong
	bestScore := math.Inf(-1)
	for k := 0; k < 32; k++ {
		a := float64(k) * math.Pi / 16
		score := open(a, L) + 0.7*open(a+math.Pi, L) + 0.35*L*math.Abs(math.Sin(a-math.Pi/4))
		if score > bestScore {
			M.axis, bestScore = a, score
		}
	}
	M.axis += jit(0.15)
	M.lean = rng.Next() * 2 * math.Pi
	// the summit crest: crest tiles along whichever map axis is nearer the long
	// axis, so its tiles are 4-neighbours
	if ux, uy := math.Cos(M.axis), math.Sin(M.axis); math.Abs(ux) >= math.Abs(uy) {
		M.cdx = S.crest * math.Copysign(1, ux)
	} else {
		M.cdy = S.crest * math.Copysign(1, uy)
	}
	if S.form == formTor {
		M.placeBlocks()
		return
	}
	add := func(a, re float64) {
		// turn toward whichever side runs out farther
		if l, r := open(a-0.25, L), open(a+0.25, L); l > r+1 {
			a -= 0.15
		} else if r > l+1 {
			a += 0.15
		}
		M.spurs = append(M.spurs, spur{a: a, bend: jit(0.3), w: 0.42 + 0.14*rng.Next(), reach: re})
	}
	add(M.axis, S.reach)
	add(M.axis+math.Pi+jit(0.35), 1+(S.reach-1)*(0.8+0.2*rng.Next()))
	rest := S.spurs - 2
	flip := 1.0
	if rng.Next() < 0.5 {
		flip = -1
	}
	for k := 0; k < rest; k++ {
		side := flip * (1 - 2*float64(k%2))
		m := (rest + 1 - k%2) / 2 // spurs on this side of the main ridge
		a := M.axis + side*math.Pi*float64(k/2+1)/float64(m+1) + jit(0.3)
		add(a, 1+(S.reach-1)*(0.35+0.4*rng.Next()))
	}
}

// placeBlocks lays out a tor: the summit stack, the tallest, on the summit
// point, and smaller, lower stacks round it at irregular bearings, a cleft of
// a tile or so between each and the next.
func (M *massif) placeBlocks() {
	S, rng := M.spec, M.rng
	H := float64(S.h)
	M.blocks = append(M.blocks, block{M.px, M.py, 1.1 + 0.3*rng.Next(), 0.85 + 0.25*rng.Next(), M.axis + 0.4*(rng.Next()-0.5), H})
	k := 4 + int(rng.Next()*2)
	a0 := rng.Next() * 2 * math.Pi
	for j := 0; j < k; j++ {
		a := a0 + 2*math.Pi*float64(j)/float64(k) + (rng.Next()-0.5)*0.7
		d := 2.2 + 1.6*rng.Next()
		// an outcrop a few levels proud of the dome where it stands, never up to
		// the summit stack
		top := math.Min(H-2, M.profile(d)+2+1.5*rng.Next())
		M.blocks = append(M.blocks, block{M.px + math.Cos(a)*d, M.py + math.Sin(a)*d,
			0.8 + 0.5*rng.Next(), 0.6 + 0.4*rng.Next(), rng.Next() * math.Pi, top})
	}
}

// star is how far the slopes run out along bearing th at radius r, as a
// multiple of a gully's run: 1 down a gully, up to the spur's reach along its
// crest. A spur's lobe narrows to a sharp crest line ((1 - |d|/w)^1.5), so
// between two spurs the contours pinch in to a gully. Over that the run wobbles
// with the bearing — noise sampled round a circle, so it closes on itself —
// and leans to one side, so one flank is a steep face and another a longer
// slope, and no contour is a regular polygon.
func (M *massif) star(th, r float64) float64 {
	s := 1.0
	f := math.Min(1, r/M.spec.foot)
	for _, sp := range M.spurs {
		d := math.Abs(wrapPi(th - sp.a - sp.bend*f))
		if d < sp.w {
			s = math.Max(s, 1+(sp.reach-1)*math.Pow(1-d/sp.w, 1.5))
		}
	}
	return s * (1 + 0.13*M.noise.Eval(1.7*math.Cos(th)+31, 1.7*math.Sin(th)-17) + 0.08*math.Cos(th-M.lean))
}

// profile is a form's height in levels over the base at r tiles from the
// summit crest, as measured down a gully.
func (M *massif) profile(r float64) float64 {
	S := M.spec
	H := float64(S.h)
	switch S.form {
	case formButte:
		// the flat top a level under the caprock, the sheer wall, the talus
		talus := rockLine + 0.5
		rw := S.top + (H-1-talus)/S.steep
		switch {
		case r < S.top:
			return H - 1
		case r < rw:
			return H - 1 - S.steep*(r-S.top)
		case r < S.foot:
			return talus * math.Pow(1-(r-rw)/(S.foot-rw), 1.4)
		}
		return 0
	case formTor:
		// the hill under the stacks: a rounded dome, broad-shouldered, steepest
		// low down
		if u := r / S.foot; u < 1 {
			return S.knee * math.Pow(1-u*u, 1.3)
		}
		return 0
	}
	// a peak: the steep core straight down from the summit to the knee, then a
	// break of slope to the lower slopes — talus and the island's own ground —
	// easing off over run tiles
	// (a horn: the top horn levels steeper still, maxStep-ish, a needle on the core)
	rh := S.horn / hornSteep
	if r <= rh {
		return H - hornSteep*r
	}
	r1 := rh + (H-S.horn-S.knee)/S.steep
	if r <= r1 {
		return H - S.horn - S.steep*(r-rh)
	}
	if t := (r - r1) / S.run; t < 1 {
		return S.knee * math.Pow(1-t, 1.6)
	}
	return 0
}

// shape lays out the massif: rise[] is the level each tile is lifted to.
//
// Each tile's distance from the summit crest — domain-warped (less so near the
// summit), so the outline wanders and the crests do not run ruler-straight,
// and squeezed along the long axis — is divided by how far the slopes run out
// on its bearing (star), and the form's profile turns that into a height. On
// the high ground, short of the summit, ridged noise adds crags: notches and
// buttresses rather than a smooth cone. Toward water and reserved ground the
// height is capped at capSlope levels a tile of room, so the massif eases down
// before it gets there.
func (M *massif) shape() {
	S, no := M.spec, M.noise
	w := M.B.w
	H := float64(S.h)
	ax, ay := math.Cos(M.axis), math.Sin(M.axis)
	cl2 := M.cdx*M.cdx + M.cdy*M.cdy
	for li := 0; li < M.B.n; li++ {
		if !M.land[li] {
			continue
		}
		lx, ly := float64(li%w)+0.5, float64(li/w)+0.5
		dx, dy := lx-M.px, ly-M.py
		f := S.warp * smooth01(math.Hypot(dx, dy)/3)
		wx := dx + f*(no.Eval(lx/9+11.3, ly/9-4.1)+0.7*no.Eval(lx/4+3.3, ly/4+8.8))
		wy := dy + f*(no.Eval(lx/9-7.7, ly/9+21.9)+0.7*no.Eval(lx/4-5.1, ly/4-2.2))
		if cl2 > 0 {
			t := math.Max(0, math.Min(1, (wx*M.cdx+wy*M.cdy)/cl2))
			wx, wy = wx-t*M.cdx, wy-t*M.cdy
		}
		along, across := wx*ax+wy*ay, -wx*ay+wy*ax
		r := math.Hypot(along/S.elong, across)
		h := M.profile(r / M.star(math.Atan2(wy, wx), r))
		if S.form == formTor {
			h = math.Max(h, M.stacks(lx, ly, h))
		}
		// crags on the high ground: none on the lower slopes or the summit
		if g := smooth01((h-rockLine-1)/4) * (1 - smooth01((h-H+5)/3)); g > 0 {
			c := 0.5 - math.Abs(no.Eval(lx*0.31+71, ly*0.31-19)) + 0.6*(0.5-math.Abs(no.Eval(lx*0.73-29, ly*0.73+43)))
			h += 1.6 * S.crag * c * g
		}
		h = math.Min(h, capSlope*M.room[li])
		if lv := int(math.Round(h)); lv >= 1 {
			M.rise[li] = min(lv, S.h)
		}
	}
}

// stacks is a tor's rock stacks at a window point standing on a hill h levels
// high there: each a rounded box, flat-topped and sheer-sided, its edges broken
// by noise so no two stacks are the same.
func (M *massif) stacks(lx, ly, h float64) float64 {
	best := 0.0
	for k, b := range M.blocks {
		dx, dy := lx-b.x, ly-b.y
		ca, sa := math.Cos(b.ang), math.Sin(b.ang)
		p, q := (dx*ca+dy*sa)/b.a, (-dx*sa+dy*ca)/b.b
		s := math.Pow(math.Pow(math.Abs(p), 4)+math.Pow(math.Abs(q), 4), 0.25)
		s += 0.25 * M.noise.Eval(lx*0.5+float64(k)*13.1, ly*0.5-float64(k)*7.3)
		if s >= 1.6 {
			continue
		}
		top := b.top
		if s > 1 {
			// a short sheer drop to the hill, a level or two of talus round it
			top = h + (b.top-h)*math.Max(0, 1-(s-1)*2.2)
		}
		best = math.Max(best, top)
	}
	return best
}

// settle turns rise into final elevations: it trims any high ground that is
// not part of the mountain and any lone pinnacle, wears every face down to at
// most maxStep levels between neighbours, and levels a small summit on the
// highest ground left.
func (M *massif) settle() {
	for li := 0; li < M.B.n; li++ {
		M.el[li] = int(M.base[li])
		if M.rise[li] > 0 {
			M.el[li] = max(M.el[li], M.b0+M.rise[li])
		}
	}
	M.wear()
	M.trim()
	M.unspike()
	M.wear()
	M.levelSummit()
	M.consolidate()
}

// unspike takes every lone pinnacle — a tile above all four of its neighbours,
// short of the summit — down to its highest neighbour. On a mountainside each
// tile has ground at least as high on its uphill side; a stack standing clear
// of the slope reads as a pillar, or a ruin, not as rock.
func (M *massif) unspike() {
	w := M.B.w
	top := 0
	for li := 0; li < M.B.n; li++ {
		if M.rise[li] > 0 {
			top = max(top, M.el[li])
		}
	}
	for pass := 0; pass < 8; pass++ {
		changed := false
		for li := 0; li < M.B.n; li++ {
			if M.rise[li] <= 0 || M.pinned[li] || M.el[li] >= top {
				continue
			}
			hi := math.MinInt
			for _, d := range dirs4 {
				if nx, ny := li%w+d[0], li/w+d[1]; M.in(nx, ny) {
					hi = max(hi, M.el[ny*w+nx])
				}
			}
			if M.el[li] > hi {
				M.el[li] = max(int(M.base[li]), hi)
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// highRock is a tile above the rock line: rock (or a ledge), not lower slope.
func (M *massif) highRock(li int) bool { return M.rise[li] > 0 && M.el[li]-M.b0 > rockLine }

// trim keeps the rock to the mountain itself. Ground above the rock line that
// is not joined to the main mass — a crag thrown up on a spur's tail — is
// lowered to the rock line, and so is any rock tile with no more than one
// rocky neighbour (a lone stack, or the very tip of a spur), so no rock stands
// alone among the fields.
func (M *massif) trim() {
	w, n := M.B.w, M.B.n
	seed, bv := -1, math.Inf(-1)
	for li := 0; li < n; li++ {
		if !M.highRock(li) {
			continue
		}
		v := float64(M.el[li]) - 0.05*math.Hypot(float64(li%w)+0.5-M.px, float64(li/w)+0.5-M.py)
		if v > bv {
			seed, bv = li, v
		}
	}
	if seed < 0 {
		return
	}
	main := make([]bool, n)
	main[seed] = true
	q := []int{seed}
	for len(q) > 0 {
		li := q[0]
		q = q[1:]
		for _, d := range dirs4 {
			nx, ny := li%w+d[0], li/w+d[1]
			if nj := ny*w + nx; M.in(nx, ny) && !main[nj] && M.highRock(nj) {
				main[nj] = true
				q = append(q, nj)
			}
		}
	}
	lower := func(li int) { M.el[li] = max(int(M.base[li]), M.b0+rockLine) }
	for li := 0; li < n; li++ {
		if M.highRock(li) && !main[li] {
			lower(li)
		}
	}
	for pass := 0; pass < 2; pass++ {
		var lone []int
		for li := 0; li < n; li++ {
			if !M.highRock(li) {
				continue
			}
			k := 0
			for _, d := range dirs4 {
				if nx, ny := li%w+d[0], li/w+d[1]; M.in(nx, ny) && M.highRock(ny*w+nx) {
					k++
				}
			}
			if k <= 1 {
				lone = append(lone, li)
			}
		}
		for _, li := range lone {
			lower(li)
		}
	}
}

// levelSummit makes the summit: the highest tile and one or two of its
// highest neighbours, levelled to one top no other tile reaches — a summit of
// 2-4 tiles, never a broad plateau. It levels by taking the higher tiles down
// to the lowest of them, never by building up, so no face round the summit
// grows: wear has already left every neighbour within maxStep of them. A
// butte's summit is the exception, a caprock knob one level over its flat top.
func (M *massif) levelSummit() {
	w := M.B.w
	best, bv := -1, math.Inf(-1)
	for li := 0; li < M.B.n; li++ {
		if M.rise[li] <= 0 {
			continue
		}
		v := float64(M.el[li]) - 0.05*math.Hypot(float64(li%w)+0.5-M.px, float64(li/w)+0.5-M.py)
		if v > bv {
			best, bv = li, v
		}
	}
	if best < 0 {
		return
	}
	plateau := []int{best}
	// the highest neighbours join it, up to three tiles in all
	type cand struct{ li, el int }
	var cs []cand
	bx, by := best%w, best/w
	for _, d := range dirs4 {
		if nx, ny := bx+d[0], by+d[1]; M.in(nx, ny) && M.rise[ny*w+nx] > 0 {
			cs = append(cs, cand{ny*w + nx, M.el[ny*w+nx]})
		}
	}
	sort.Slice(cs, func(a, b int) bool {
		if cs[a].el != cs[b].el {
			return cs[a].el > cs[b].el
		}
		return cs[a].li < cs[b].li
	})
	top := M.el[best]
	for _, c := range cs {
		if len(plateau) < M.spec.summit && c.el >= M.el[best]-2 {
			plateau = append(plateau, c.li)
			top = min(top, c.el)
		}
	}
	if M.spec.form == formButte {
		top++
	}
	on := map[int]bool{}
	for _, li := range plateau {
		M.el[li], M.pinned[li], on[li] = top, true, true
	}
	// nothing else reaches the summit's level
	for li := 0; li < M.B.n; li++ {
		if !on[li] && M.rise[li] > 0 && M.el[li] >= top {
			M.el[li] = top - 1
		}
	}
	M.summit, M.top = best, top
}

// wear lowers every unpinned mountain tile to at most maxStep above its lowest
// neighbour, never below its base ground, until nothing moves.
func (M *massif) wear() {
	w := M.B.w
	for pass := 0; pass < 96; pass++ {
		changed := false
		for li := 0; li < M.B.n; li++ {
			if M.rise[li] <= 0 || M.pinned[li] {
				continue
			}
			lx, ly := li%w, li/w
			lo := math.MaxInt
			for _, d := range dirs4 {
				if M.in(lx+d[0], ly+d[1]) {
					lo = min(lo, M.el[(ly+d[1])*w+lx+d[0]])
				}
			}
			if lim := max(int(M.base[li]), lo+maxStep); M.el[li] > lim {
				M.el[li] = lim
				changed = true
			}
		}
		if !changed {
			return
		}
	}
}

// trail finds the trail: a cheapest path in (tile, trail level, heading,
// winding) space from ordinary ground at the foot, on the side facing the
// island centre, to the summit. Each step keeps the trail's level or rises
// one. The trail is cut into the slope — up to maxStep levels below the ground
// it crosses — or built up two levels at most, the cost growing with the
// square of the cut, so it keeps close to the mountainside; turning costs a
// little, so it runs in legs rather than a staircase. It never passes more
// than maxStep a tile above or below anything that cannot move (the summit,
// the ground off the mountain), nor, once found, beside another stretch of
// itself across a slope steeper than that (trailRuns), so carving it never
// leaves a face taller than maxStep.
//
// On the steep core a trail rising a level a tile cannot go straight up a
// slope of 3-4 levels a tile: it has to traverse, and so it spirals. Where the
// spec asks for it (wrap), the trail must wind once right round the summit: a
// ray runs out from the summit beside the start, the trail starts just past it
// and must cross it once, the long way round, and step onto the summit from a
// bearing past where it started — tracked as the winding coordinate of the
// state. Both ways round, with and without the core gate (trailSearch), are
// tried and the cheapest kept; a massif with no room to wind round still gets a
// trail. Returns the trail as window indices, foot first, with the trail level
// of each; it ends on whichever summit tile it reaches first.
func (M *massif) trail() ([]int, []int) {
	if M.top <= 0 {
		return nil, nil
	}
	var best, bestLv []int
	bestCost := math.Inf(1)
	ways := []int{0}
	if M.spec.wrap {
		ways = []int{1, -1}
	}
	for _, way := range ways {
		if path, lv, cost := M.trailAt(way); path != nil && cost < bestCost {
			best, bestLv, bestCost = path, lv, cost
		}
	}
	if best == nil && M.spec.wrap {
		// a massif too cramped to wind round still gets a trail
		best, bestLv, _ = M.trailAt(0)
	}
	return best, bestLv
}

// trailAt runs the search until its path is a simple one. A path in (tile,
// level) space may pass a tile twice at two levels, which no real trail can:
// when it does, the tile is held at its first level and the search runs again.
func (M *massif) trailAt(way int) ([]int, []int, float64) {
	path, lv, cost := M.trailRuns(way, false)
	if way != 0 {
		if p2, l2, c2 := M.trailRuns(way, true); p2 != nil && (path == nil || c2 < cost) {
			path, lv, cost = p2, l2, c2
		}
	}
	return path, lv, cost
}

func (M *massif) trailRuns(way int, gated bool) ([]int, []int, float64) {
	hold := map[int]int{}
	w := M.B.w
	for try := 0; try < 24; try++ {
		path, lv, cost := M.trailSearch(way, gated, hold)
		if path == nil {
			return nil, nil, 0
		}
		// Two stretches of the trail D tiles apart (D <= trailGap) and more than
		// D*maxStep levels apart would need a face over maxStep somewhere between
		// them — two turns of the spiral too close for the slope between — so
		// the later tile is barred from the trail (hold -1).
		at, crossed := map[int]int{}, false
		for k, li := range path {
			if j, dup := at[li]; dup && lv[j] != lv[k] {
				hold[li] = lv[j]
				crossed = true
				break
			}
			for dy := -trailGap; dy <= trailGap && !crossed; dy++ {
				for dx := -trailGap; dx <= trailGap; dx++ {
					D := abs(dx) + abs(dy)
					if D == 0 || D > trailGap || !M.in(li%w+dx, li/w+dy) {
						continue
					}
					if j, ok := at[li+dy*w+dx]; ok && abs(lv[j]-lv[k]) > D*maxStep {
						hold[li] = -1
						crossed = true
						break
					}
				}
			}
			if crossed {
				break
			}
			at[li] = k
		}
		if !crossed {
			return path, lv, cost
		}
	}
	return nil, nil, 0
}

// coreTurn is the core a winding trail spirals up last: above level gate, how
// many radians of winding it needs. A trail rising a level a tile up a slope
// of s levels a tile closes in on the summit by r/sqrt(s*s-1) tiles a radian,
// so from the core's edge r1 to the summit's it turns sqrt(s*s-1)*ln(r1/1.5)
// radians — and has to: the gate opens when a little less than that is left
// to wind, so the core takes no more than its own share and never closes a
// turn round itself.
func (M *massif) coreTurn() (int, float64) {
	S := M.spec
	H := float64(S.h)
	s, r1, gate := S.steep, S.horn/hornSteep+(H-S.horn-S.knee)/S.steep, M.b0+int(math.Round(S.knee))
	switch S.form {
	case formButte:
		r1, gate = S.top+(H-1-rockLine-0.5)/S.steep, M.b0+rockLine+1
	case formTor:
		s, r1, gate = 2.5, 0.6*S.foot, M.b0+int(math.Round(S.knee/2))
	}
	turn := 0.85 * math.Sqrt(s*s-1) * math.Log(math.Max(2, r1)/1.5)
	return gate, math.Max(1.5, math.Min(1.5*math.Pi, turn))
}

// trailSearch is one run of the trail's cheapest-path search; hold pins tiles
// a previous run passed twice to the level it first took them at. A gated
// winding search keeps the trail below the core (coreTurn) until it has wound
// round all but the core's share of its turn: left to itself the cheapest trail
// climbs straight into the core and makes up the turn it owes in tight circles
// round the summit, one turn on top of the last, which no slope of maxStep
// levels a tile can hold apart.
func (M *massif) trailSearch(way int, gated bool, hold map[int]int) ([]int, []int, float64) {
	w, n := M.B.w, M.B.n
	summit, top := M.summit, M.top
	levels := top + 1
	// winding: sheet 1 is the start; crossing the ray the long way round moves
	// to sheet 2, the goal; crossing it back the short way, to sheet 0
	sheets, goalSheet := 1, 0
	if way != 0 {
		sheets, goalSheet = 3, 2
	}
	idx := func(li, t, dir, sh int) int { return ((li*levels+t)*4+dir)*sheets + sh }
	N := n * levels * 4 * sheets
	dist := make([]float64, N)
	prev := make([]int32, N)
	for k := range dist {
		dist[k] = math.Inf(1)
		prev[k] = -1
	}
	// each tile's bearing from the summit, measured from the ray
	ray := math.Atan2(M.toCentre[1], M.toCentre[0]) - float64(way)*0.3
	sx, sy := float64(summit%w)+0.5, float64(summit/w)+0.5
	phi, rad := make([]float64, n), make([]float64, n)
	// pinLo..pinHi: the trail levels a tile allows beside what cannot move —
	// what is already pinned (the summit and its benches) and the ground off
	// the mountain — no more than maxStep a tile under or over it, out to
	// trailGap tiles
	pinLo, pinHi := make([]int, n), make([]int, n)
	for li := 0; li < n; li++ {
		dx, dy := float64(li%w)+0.5-sx, float64(li/w)+0.5-sy
		phi[li], rad[li] = wrapPi(math.Atan2(dy, dx)-ray), math.Hypot(dx, dy)
		pinLo[li], pinHi[li] = math.MinInt/2, math.MaxInt/2
		for oy := -trailGap; oy <= trailGap; oy++ {
			for ox := -trailGap; ox <= trailGap; ox++ {
				D := abs(ox) + abs(oy)
				if nx, ny := li%w+ox, li/w+oy; D > 0 && D <= trailGap && M.in(nx, ny) && (M.pinned[ny*w+nx] || M.rise[ny*w+nx] <= 0) {
					pinLo[li] = max(pinLo[li], M.el[ny*w+nx]-D*maxStep)
					pinHi[li] = min(pinHi[li], M.el[ny*w+nx]+D*maxStep)
				}
			}
		}
	}
	// wound is how far round the summit a trail on sheet sh at tile li has come,
	// radians: its bearing past the ray, the way it winds (0..2pi), and a turn
	// for each crossing of the ray
	wound := func(li, sh int) float64 {
		return math.Mod(phi[li]*float64(way)+2*math.Pi, 2*math.Pi) + 2*math.Pi*float64(sh-sheets/2)
	}
	// the summit's tiles (pinned before the trail is: the summit and its benches)
	onTop := func(li int) bool { return M.pinned[li] && M.el[li] == top }
	cross := func(a, b int) int {
		if way == 0 || math.Abs(phi[a]) > math.Pi/2 || math.Abs(phi[b]) > math.Pi/2 {
			return 0
		}
		switch {
		case phi[a] < 0 && phi[b] >= 0:
			return way
		case phi[a] >= 0 && phi[b] < 0:
			return -way
		}
		return 0
	}
	pq := &stateHeap{}
	// the foot: ordinary ground beside the mountain, on the island-centre side
	// — for a winding trail, in a narrow sector just past the ray
	start := func(lo, hi float64) int {
		count := 0
		for li := 0; li < n; li++ {
			if M.rise[li] > 0 || !M.land[li] || M.el[li] > M.b0+1 {
				continue
			}
			lx, ly := li%w, li/w
			next := false
			for _, d := range dirs4 {
				if M.in(lx+d[0], ly+d[1]) && M.rise[(ly+d[1])*w+lx+d[0]] > 0 {
					next = true
				}
			}
			if !next {
				continue
			}
			if way == 0 {
				dx, dy := float64(lx)+0.5-M.px, float64(ly)+0.5-M.py
				if l := math.Hypot(dx, dy); l == 0 || (dx*M.toCentre[0]+dy*M.toCentre[1])/l < lo {
					continue
				}
			} else if f := phi[li] * float64(way); f <= lo || f > hi {
				continue
			}
			for dir := 0; dir < 4; dir++ {
				k := idx(li, M.el[li], dir, sheets/2)
				dist[k] = 0
				heap.Push(pq, stateItem{k, 0})
			}
			count++
		}
		return count
	}
	gate, coreTurn := M.coreTurn()
	// past: the bearing (from the ray) the trail must be past as it steps onto
	// the summit, the far edge of the start sector — so it has wound at least
	// once right round
	past := 0.6
	if way == 0 {
		if start(0.1, 0) == 0 {
			start(-2, 0)
		}
	} else if start(0.04, past) == 0 {
		if past = 1.2; start(0.04, past) == 0 {
			return nil, nil, 0
		}
	}
	goal := -1
	for pq.Len() > 0 {
		it := heap.Pop(pq).(stateItem)
		if it.d > dist[it.k] {
			continue
		}
		sh := it.k % sheets
		dir := (it.k / sheets) % 4
		t := (it.k / (sheets * 4)) % levels
		li := it.k / (sheets * 4 * levels)
		if onTop(li) {
			if t == top && sh == goalSheet {
				goal = it.k
				break
			}
			continue // the summit is where it ends, on whichever of its tiles it reaches first
		}
		lx, ly := li%w, li/w
		for nd, d := range dirs4 {
			if nd == (dir+2)%4 {
				continue // no doubling back onto the tile just left
			}
			nx, ny := lx+d[0], ly+d[1]
			if !M.in(nx, ny) {
				continue
			}
			nj := ny*w + nx
			if M.rise[nj] <= 0 || !M.land[nj] {
				continue
			}
			ns := sh + cross(li, nj)
			if ns < 0 || ns >= sheets || onTop(nj) && way != 0 && (ns != goalSheet || wound(li, sh) < 2*math.Pi+past) {
				continue
			}
			// how far round it has come: the bearing past the ray the way it winds,
			// and a turn for each crossing
			turned := wound(nj, ns)
			for up := 0; up <= 1; up++ {
				nt := t + up
				if gated && nt > gate && turned < 2*math.Pi+past-coreTurn {
					continue
				}
				cut := M.el[nj] - nt
				if nt > top || cut > maxStep || cut < -2 || (M.pinned[nj] && cut != 0) {
					continue
				}
				if nt == top && M.el[nj] != top {
					continue // the summit is its own few tiles: the trail reaches its level only there
				}
				if h, held := hold[nj]; held && h != nt {
					continue
				}
				if nt < pinLo[nj] || nt > pinHi[nj] {
					continue
				}
				cost := 1.0
				if cut > 0 {
					cost += 0.6 * float64(cut*cut)
				} else if cut < 0 {
					cost += 2 * float64(cut*cut)
				}
				if nd != dir {
					cost += 0.8
				}
				k := idx(nj, nt, nd, ns)
				if d2 := it.d + cost; d2 < dist[k] {
					dist[k] = d2
					prev[k] = int32(it.k)
					heap.Push(pq, stateItem{k, d2})
				}
			}
		}
	}
	if goal < 0 {
		return nil, nil, 0
	}
	var path, lv []int
	for k := goal; k >= 0; k = int(prev[k]) {
		path = append(path, k/(sheets*4*levels))
		lv = append(lv, (k/(sheets*4))%levels)
	}
	for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
		path[a], path[b] = path[b], path[a]
		lv[a], lv[b] = lv[b], lv[a]
	}
	return path, lv, dist[goal]
}

// carve cuts the trail into the massif: its tiles take the trail's levels and
// are pinned, a diagonal staircase gets its other corner too (so a keeper
// walking it on the diagonal never steps into the face beside it), the faces
// round it are worn down again, and a notch or built-up step that leaves a
// lower neighbour more than maxStep under the trail gets a bench filled in.
func (M *massif) carve(path, lv []int) {
	w := M.B.w
	for k, li := range path {
		M.el[li], M.pinned[li] = lv[k], true
	}
	for k := 1; k+1 < len(path); k++ {
		a, b, c := path[k-1], path[k], path[k+1]
		if abs(a%w-c%w) != 1 || abs(a/w-c/w) != 1 || lv[k-1] != lv[k+1] {
			continue
		}
		ox, oy := a%w+c%w-b%w, a/w+c/w-b/w
		o := oy*w + ox
		if !M.in(ox, oy) || M.pinned[o] || !M.land[o] || M.rise[o] <= 0 {
			continue
		}
		ok := true // and it may not stand more than maxStep off anything pinned beside it
		for _, d := range dirs4 {
			if nx, ny := ox+d[0], oy+d[1]; M.in(nx, ny) && M.pinned[ny*w+nx] && abs(M.el[ny*w+nx]-lv[k]) > maxStep {
				ok = false
			}
		}
		if ok {
			M.el[o], M.pinned[o], M.corner[o] = lv[k], true, true
		}
	}
	M.consolidate()
}

// consolidate makes every face maxStep levels or less once the summit and the
// trail are pinned: unpinned ground is worn down, and where wearing has left
// something under a pinned tile by more than maxStep, that neighbour is filled
// up to the step and pinned as a bench, so the next wear cannot take it away
// again. Each bench is lower than what it holds up, so this settles.
func (M *massif) consolidate() {
	w, n := M.B.w, M.B.n
	for pass := 0; pass < 48; pass++ {
		M.wear()
		changed := false
		for li := 0; li < n; li++ {
			if !M.pinned[li] {
				continue
			}
			lx, ly := li%w, li/w
			for _, d := range dirs4 {
				nx, ny := lx+d[0], ly+d[1]
				if !M.in(nx, ny) {
					continue
				}
				if nj := ny*w + nx; !M.pinned[nj] && M.rise[nj] > 0 && M.el[nj] < M.el[li]-maxStep {
					M.el[nj], M.pinned[nj] = M.el[li]-maxStep, true
					changed = true
				}
			}
		}
		if !changed {
			return
		}
	}
}

type stateItem struct {
	k int
	d float64
}
type stateHeap []stateItem

func (h stateHeap) Len() int { return len(h) }
func (h stateHeap) Less(a, b int) bool {
	if h[a].d != h[b].d {
		return h[a].d < h[b].d
	}
	return h[a].k < h[b].k // a total order, so the trail is deterministic
}
func (h stateHeap) Swap(a, b int) { h[a], h[b] = h[b], h[a] }
func (h *stateHeap) Push(x any)   { *h = append(*h, x.(stateItem)) }
func (h *stateHeap) Pop() any {
	old := *h
	it := old[len(old)-1]
	*h = old[:len(old)-1]
	return it
}

// classify writes the massif into the stamp: the tile changes with their
// tileVis (the island's own ground on the lower slopes, rock on the faces,
// ledges on the terraces, snow up high, the trail), the cleared nodes and
// decor, and the few nodes it places — scree at the foot, boulders and
// stunted trees on the ledges, a cairn on the summit.
func (M *massif) classify(w *World, s *MountainStamp, m Mountain, trail []int) {
	bw := M.B.w
	pk := MountainPeak{Key: m.Key, Site: [2]int{m.X, m.Y}, Island: M.isle, Base: uint8(M.b0),
		X0: SIZE, Y0: SIZE, X1: -1, Y1: -1}
	onTrail := map[int]bool{}
	for _, li := range trail {
		onTrail[li] = true
	}
	// beside: a 4-neighbour is in set (nothing is placed where it would crowd
	// the trail)
	beside := func(li int, set map[int]bool) bool {
		lx, ly := li%bw, li/bw
		for _, d := range dirs4 {
			if M.in(lx+d[0], ly+d[1]) && set[(ly+d[1])*bw+lx+d[0]] {
				return true
			}
		}
		return false
	}
	// the rock: the steep core — everything above the knee (a butte's walls and
	// top, above its talus; a tor's granite crown, above its lower slopes) — and below it only
	// real cliff edges, a drop of three levels or more: the gentle shoulders in
	// between stay the island's own ground. A patch of rock smaller than minCrag
	// tiles not joined to the core goes back to ground too, and so does a cell
	// with fewer than two rocky neighbours, so no rock stands about the fields
	// like a ruin.
	coreAt := M.b0 + int(math.Ceil(M.spec.knee))
	switch M.spec.form {
	case formButte:
		coreAt = M.b0 + rockLine + 1
	case formTor:
		coreAt = M.b0 + rockLine + 3
	}
	rock := make([]bool, M.B.n)
	for li := 0; li < M.B.n; li++ {
		if !M.highRock(li) || onTrail[li] || M.corner[li] {
			continue
		}
		drop := 0
		for _, d := range dirs4 {
			if nx, ny := li%bw+d[0], li/bw+d[1]; M.in(nx, ny) {
				drop = max(drop, M.el[li]-M.el[ny*bw+nx])
			}
		}
		rock[li] = M.el[li] >= coreAt || drop >= 3
	}
	comp := make([]int, M.B.n)
	for li := 0; li < M.B.n; li++ {
		if !rock[li] || comp[li] != 0 {
			continue
		}
		// one patch, flood-filled
		patch, q, core := []int{li}, []int{li}, false
		comp[li] = 1
		for len(q) > 0 {
			a := q[0]
			q = q[1:]
			if M.el[a] >= coreAt || M.pinned[a] && !onTrail[a] {
				core = true
			}
			for _, d := range dirs4 {
				if nx, ny := a%bw+d[0], a/bw+d[1]; M.in(nx, ny) {
					if nj := ny*bw + nx; rock[nj] && comp[nj] == 0 {
						comp[nj] = 1
						patch, q = append(patch, nj), append(q, nj)
					}
				}
			}
		}
		if !core && len(patch) < minCrag {
			for _, a := range patch {
				rock[a] = false
			}
		}
	}
	for lone := true; lone; {
		lone = false
		for li := 0; li < M.B.n; li++ {
			if !rock[li] {
				continue
			}
			k := 0
			for dy := -1; dy <= 1; dy++ {
				for dx := -1; dx <= 1; dx++ {
					if nx, ny := li%bw+dx, li/bw+dy; (dx != 0 || dy != 0) && M.in(nx, ny) {
						if nj := ny*bw + nx; rock[nj] || onTrail[nj] {
							k++
						}
					}
				}
			}
			if k < 2 {
				rock[li], lone = false, true
			}
		}
	}
	// near: within a few tiles of the rock, where scree comes to rest
	near := make([]int, M.B.n)
	for li := range near {
		near[li] = 99
		if rock[li] {
			near[li] = 0
		}
	}
	for pass := 0; pass < 3; pass++ {
		for li := 0; li < M.B.n; li++ {
			for _, d := range dirs4 {
				if nx, ny := li%bw+d[0], li/bw+d[1]; M.in(nx, ny) {
					near[li] = min(near[li], near[ny*bw+nx]+1)
				}
			}
		}
	}
	summitTile := -1
	if len(trail) > 0 {
		summitTile = trail[len(trail)-1]
	}
	cairn := false
	for li := 0; li < M.B.n; li++ {
		if M.rise[li] <= 0 && !M.pinned[li] {
			continue
		}
		lx, ly := li%bw, li/bw
		x, y := M.B.x0+lx, M.B.y0+ly
		i := y*SIZE + x
		e := M.el[li]
		if e == int(w.Elev[i]) && !onTrail[li] && !rock[li] {
			continue // nothing to change
		}
		pk.X0, pk.Y0, pk.X1, pk.Y1 = min(pk.X0, x), min(pk.Y0, y), max(pk.X1, x), max(pk.Y1, y)
		s.Mark[i] = true
		trailTile := (onTrail[li] && li != summitTile) || M.corner[li]
		// the lower slopes keep the island's own ground and whatever grows on it
		if !rock[li] && !trailTile && li != summitTile {
			s.Changes = append(s.Changes, TileChange{I: i, Elev: uint8(e), Vis: w.TileVis[i], BaseElev: w.Elev[i], BaseVis: w.TileVis[i], Keep: true})
			// scree: boulders and stones come to rest at the foot of the rock
			_, hasNode := w.Nodes[i]
			_, hasDecor := w.Decor[i]
			if r := M.rng.Next(); !hasNode && !hasDecor && near[li] <= 3 && !beside(li, onTrail) {
				f := 1 - 0.22*float64(near[li]-1)
				if r < 0.05*f {
					s.Nodes[i] = NodeBoulder
				} else if r < 0.16*f {
					s.Nodes[i] = NodeStone
				}
			}
			continue
		}
		// a cliff edge: a neighbour 2+ levels below, so this stack is a face the
		// camera sees (or a brink to fall from); the foot of a wall is a ledge
		steep := false
		for _, d := range dirs4 {
			if M.in(lx+d[0], ly+d[1]) && e-M.el[(ly+d[1])*bw+lx+d[0]] >= 2 {
				steep = true
			}
		}
		vis := VisLedge
		if steep && e < M.top { // the summit is a walkable top, a ledge however it falls away
			vis = VisRock
		}
		if M.spec.snowAt > 0 && e-M.b0 >= M.spec.snowAt {
			vis += VisSnow - VisRock
		}
		// the trail and the corners widening it; its last tile, on the summit,
		// is a ledge like the rest of the summit
		if trailTile {
			vis = VisTrail
		} else if li == summitTile {
			vis = VisLedge
			if M.spec.snowAt > 0 && e-M.b0 >= M.spec.snowAt {
				vis = VisSnowLedge
			}
		}
		s.Changes = append(s.Changes, TileChange{I: i, Elev: uint8(e), Vis: vis, BaseElev: w.Elev[i], BaseVis: w.TileVis[i]})
		if vis == VisRock || vis == VisSnow {
			s.Face[i] = true
		}
		if k, ok := w.Nodes[i]; ok {
			s.BaseNodes[i] = k
		}
		if d, ok := w.Decor[i]; ok {
			s.BaseDecor[i] = d
		}
		if vis != VisLedge && vis != VisSnowLedge || onTrail[li] || M.corner[li] {
			continue
		}
		// a cairn: a stone on the summit beside the trail's end (nodes block
		// building, never walking)
		if e == M.top && !cairn {
			s.Nodes[i], cairn = NodeStone, true
			continue
		}
		// a few boulders and stones on the ledges (crystals on the Spire's, which
		// grow no trees), stunted trees on the Woods' lowest
		if beside(li, onTrail) || e >= M.top {
			continue
		}
		r := M.rng.Next()
		switch {
		case M.spec.trees > 0 && e-M.b0 <= rockLine+5 && r < M.spec.trees:
			s.Nodes[i] = NodeTree
		case M.isle == 2 && r > 0.94:
			s.Nodes[i] = NodeCrystal
		case r > 0.95:
			s.Nodes[i] = NodeBoulder
		case r > 0.88:
			s.Nodes[i] = NodeStone
		}
	}
	for _, li := range trail {
		pk.Trail = append(pk.Trail, (M.B.y0+li/bw)*SIZE+M.B.x0+li%bw)
	}
	if len(trail) > 0 {
		pk.Summit, pk.Top = [2]int{M.B.x0 + M.summit%bw, M.B.y0 + M.summit/bw}, uint8(M.el[summitTile])
		// how far it winds round the summit
		turn, last := 0.0, math.NaN()
		for _, li := range trail {
			dx, dy := float64(li%bw-M.summit%bw), float64(li/bw-M.summit/bw)
			if dx == 0 && dy == 0 {
				break
			}
			a := math.Atan2(dy, dx)
			if !math.IsNaN(last) {
				turn += wrapPi(a - last)
			}
			last = a
		}
		pk.Turns = math.Abs(turn) / (2 * math.Pi)
	}
	if pk.X1 >= 0 {
		s.Peaks = append(s.Peaks, pk)
	}
}
