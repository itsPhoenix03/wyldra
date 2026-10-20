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
// and the client draws an SVG mountain on it. This pass builds a real massif
// out of terrain instead: stacked tiles rising 8-10 levels above the local
// ground on the four major islands (4-5 on the Core). Grassy (sandy, snowy,
// muddy) foothills carry it out 12-15 tiles along a ridge and its spurs; on
// them stands compact, steep high ground — a summit and lesser peaks on a
// crest, climbing in cliff bands of 2 and 3 levels with ledges between — and
// one switchback trail runs from the foot to a small summit plateau, every
// step of it at most one level up.
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
// maps to the per-island rock art (src/main.ts tileKey).
//
// The massif never covers water (it keeps massifSpec.shore tiles of ordinary
// ground off every shore and lake), the clearings (monolith plazas, lore
// notes, the spawn, the medics, the village yards — with their free and land
// radii plus reserveMargin), the monoliths or the temple pieces, and it eases
// down before reaching any of them rather than ending in a cut cliff.
// =============================================================================

// MaxElev is the tallest stack the mountains build. Ordinary terrain is 0..3;
// every consumer of Elev (client stacking, lips, fall damage, repaint boxes)
// is sized for this.
const MaxElev = 12

// tileVis values on mountain tiles. 0 and 1 are the ordinary ground variants
// (shared/world.js); the mountain values are Go-only and start above them.
const (
	VisRock      uint8 = 2 // bare rock: a cliff edge (a neighbour 2+ levels below)
	VisLedge     uint8 = 3 // a walkable terrace on the high ground: rock with scree and ground cover
	VisSnow      uint8 = 4 // snow-capped rock, on the upper levels of the Woods and the Spire
	VisSnowLedge uint8 = 5 // a snow-covered terrace
	VisTrail     uint8 = 6 // the switchback trail to the summit
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
// foothill tile: it is lifted but keeps the island's ground, and the trees,
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
	// X0..Y1 is the bounding box of the stamped tiles (inclusive).
	X0, Y0, X1, Y1 int
}

// MountainStamp is the whole pass: what to write and what it replaced.
type MountainStamp struct {
	Peaks []MountainPeak
	// Changes is every mountain tile, ascending by index.
	Changes []TileChange
	// Nodes are the boulders, stones, crystals and stunted trees the stamp
	// places on its ledges and upper foothills. The nodes and decor of the rock
	// tiles are cleared; the foothills (TileChange.Keep) keep theirs.
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
			// a foothill keeps its base cover: only the scree the stamp added goes
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

// massifSpec is the shape of one island's mountain.
type massifSpec struct {
	r       float64 // footprint radius, tiles: how far the foothills reach
	h       int     // rise of the summit above the local ground, levels
	foot    float64 // how high the foothills rise, as a fraction of h
	peakR   float64 // the summit's radius, tiles
	peaks   int     // lesser peaks beside it
	spurs   int     // foothill spurs off the main ridge
	width   float64 // foothill ridge half-width as a fraction of r
	ridgeLn float64 // main ridge half-length as a fraction of r
	snowAt  int     // rise from which the tops are snow (0: never)
	trees   float64 // chance of a stunted tree on a low ledge
	mesa    bool    // flat-topped sandstone: tables and buttes, sheer sides
	grade   int     // the trail rises at most a level per this many tiles
	shore   float64 // tiles of ordinary ground kept between the massif and any water
	ease    float64 // over which it eases down toward anything it must avoid
}

// massifSpecs by island: Woods, Dunes, Spire, Marsh, then the Core — a small
// obsidian crag along the north shore, behind the temple.
var massifSpecs = [5]massifSpec{
	{r: 14, h: 10, foot: 0.25, peakR: 6, peaks: 2, spurs: 5, width: 0.6, ridgeLn: 0.8, snowAt: 8, trees: 0.12, shore: 2, ease: 3, grade: 3},
	{r: 14, h: 10, foot: 0.45, peakR: 5, peaks: 1, spurs: 3, width: 0.5, ridgeLn: 0.7, mesa: true, shore: 2, ease: 2.5, grade: 3},
	{r: 15, h: 10, foot: 0.25, peakR: 6, peaks: 2, spurs: 6, width: 0.55, ridgeLn: 0.85, snowAt: 5, shore: 2, ease: 3, grade: 3},
	{r: 12, h: 10, foot: 0.25, peakR: 5.5, peaks: 1, spurs: 4, width: 0.6, ridgeLn: 0.75, shore: 1.5, ease: 2.5, grade: 3},
	{r: 7, h: 5, foot: 0.3, peakR: 3, peaks: 1, spurs: 0, width: 0.45, ridgeLn: 1.1, shore: 1, ease: 1.5, grade: 2},
}

const (
	reserveMargin = 2 // tiles kept clear round every clearing, monolith and temple piece
	maxStep       = 3 // the steepest face between two neighbouring tiles
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

// massif is the working state for one mountain: a square window of the map.
type massif struct {
	B      box
	base   []uint8   // base elevation
	land   []bool    // dry land the massif may stand on
	room   []float64 // tiles from the nearest ground it may not stand on
	env    []float64 // 0..1: how much of the massif's height a tile may take
	rise   []int     // levels above b0 the massif lifts each tile (0: not mountain)
	el     []int     // final elevation
	pinned []bool    // the trail, the summit and benches beside them: wear may not move them
	corner []bool    // tiles widening a diagonal stretch of the trail
	b0     int
	spec   massifSpec
	isle   int
	peakX  float64 // the main peak, window coordinates
	peakY  float64
	noise  *Noise2D
	rng    *Alea
	// toCentre is the unit vector from the site toward its island's centre:
	// the trail starts on that side, the side the folk and the keepers live on.
	toCentre [2]float64
}

func (M *massif) in(lx, ly int) bool { return lx >= 0 && ly >= 0 && lx < M.B.w && ly < M.B.h }

var dirs4 = [4][2]int{{1, 0}, {0, 1}, {-1, 0}, {0, -1}}

func buildMassif(w *World, s *MountainStamp, m Mountain, seed string, reserved func(x, y int) bool) {
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
	M := &massif{
		B: makeBox(m.X, m.Y, int(spec.r)+10), spec: spec, isle: isle,
		noise: NewNoise2D(NewAlea(seed, "mountain-shape", m.Key)),
		rng:   NewAlea(seed, "mountain", m.Key),
	}
	dx, dy := float64(centre[0]-m.X), float64(centre[1]-m.Y)
	if l := math.Hypot(dx, dy); l > 0 {
		M.toCentre = [2]float64{dx / l, dy / l}
	}
	n := M.B.n
	M.base, M.land, M.room, M.env = make([]uint8, n), make([]bool, n), make([]float64, n), make([]float64, n)
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
		if water[li] == 0 && math.Hypot(float64(x-m.X), float64(y-m.Y)) <= spec.r {
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
		M.env[li] = smooth01(room / spec.ease)
	}
	M.shape()
	M.settle()
	trail := M.trail()
	M.classify(w, s, m, trail)
}

func smooth01(t float64) float64 {
	t = math.Max(0, math.Min(1, t))
	return t * t * (3 - 2*t)
}

// ridge is one crest segment: from a to b, its crest easing from height ha to
// hb, its flanks reaching wa..wb tiles either side, with the cross-section of
// its kind.
type ridge struct {
	ax, ay, bx, by, ha, hb, wa, wb float64
	kind                           int
}

// ridge kinds: the broad foothills, the steep high ground, a flat-topped table
const (
	rFoot = iota
	rPeak
	rMesa
)

func (rd ridge) at(px, py float64) float64 {
	vx, vy := rd.bx-rd.ax, rd.by-rd.ay
	l2 := vx*vx + vy*vy
	t := 0.0
	if l2 > 0 {
		t = math.Max(0, math.Min(1, ((px-rd.ax)*vx+(py-rd.ay)*vy)/l2))
	}
	d := math.Hypot(px-(rd.ax+vx*t), py-(rd.ay+vy*t))
	st := t * t * (3 - 2*t)
	hc := rd.ha + (rd.hb-rd.ha)*st
	u := d / (rd.wa + (rd.wb-rd.wa)*t)
	if u >= 1 {
		return 0
	}
	switch rd.kind {
	case rMesa:
		return hc * (1 - math.Pow(u, 4)) // level across most of its width, then sheer
	case rPeak:
		return hc * math.Pow(1-u, 0.85) // a steep, nearly straight flank
	}
	return hc * (0.5*(1-u*u) + 0.5*(1-u)) // foothills: broad, rounded shoulders
}

// roomAt samples how much room the massif has at a window point.
func (M *massif) roomAt(x, y float64) float64 {
	lx, ly := int(math.Floor(x)), int(math.Floor(y))
	if !M.in(lx, ly) {
		return -1
	}
	return M.room[ly*M.B.w+lx]
}

// shape lays out the massif and quantises it: rise[] is the level each tile
// is lifted to.
//
// Seen in the iso view a level is 10 px and a tile row 16, so a mountain only
// reads as one where it is steep: 9 levels spread over a 14-tile slope look
// like a low mound. The massif is therefore two things. Broad foothills — the
// main ridge and its spurs, a few levels high, walkable, with gullies between
// the spurs — carry it out to its full radius; the high ground on them is
// compact and steep — the summit and one or two lesser peaks, joined by a
// narrow crest, rising in cliff bands of 2 and 3 levels.
func (M *massif) shape() {
	S, rng, no := M.spec, M.rng, M.noise
	R := S.r
	w := M.B.w
	sx, sy := float64(M.B.cx-M.B.x0)+0.5, float64(M.B.cy-M.B.y0)+0.5
	jit := func(a float64) float64 { return (rng.Next() - 0.5) * 2 * a }
	// the summit: the roomiest tile within a few tiles of the site, so the peak
	// has the width its cliff bands need
	M.peakX, M.peakY = sx, sy
	best := math.Inf(-1)
	for dy := -4; dy <= 4; dy++ {
		for dx := -4; dx <= 4; dx++ {
			if r := math.Min(S.peakR+3, M.roomAt(sx+float64(dx), sy+float64(dy))) - 0.3*math.Hypot(float64(dx), float64(dy)); r > best {
				best, M.peakX, M.peakY = r, sx+float64(dx), sy+float64(dy)
			}
		}
	}
	// the main ridge runs the way the island leaves most room, preferring
	// across the line to the island centre so the climb side is a long flank
	L := R * S.ridgeLn
	theta, bestScore := 0.0, math.Inf(-1)
	for k := 0; k < 32; k++ {
		a := float64(k) * math.Pi / 16
		ux, uy := math.Cos(a), math.Sin(a)
		score := 0.0
		for t := -L; t <= L; t += 1 {
			score += math.Min(4, math.Max(-4, M.roomAt(M.peakX+ux*t, M.peakY+uy*t)))
		}
		score += 6 * math.Abs(ux*M.toCentre[1]-uy*M.toCentre[0])
		if M.isle == 4 {
			score += 20 * math.Abs(ux) // the Core's crag lies along its shore
		}
		if score > bestScore {
			theta, bestScore = a, score
		}
	}
	theta += jit(0.12)
	ux, uy := math.Cos(theta), math.Sin(theta)
	px, py := -uy, ux
	W := R * S.width
	F := S.foot
	footKind, peakKind, shoulderKind := rFoot, rPeak, rFoot
	if S.mesa {
		footKind, peakKind, shoulderKind = rMesa, rMesa, rMesa
	}
	// the foothills: the main ridge end to end, and spurs off it
	ax, ay := M.peakX-ux*L+px*jit(2), M.peakY-uy*L+py*jit(2)
	bx, by := M.peakX+ux*L*0.9+px*jit(2), M.peakY+uy*L*0.9+py*jit(2)
	ridges := []ridge{
		{ax, ay, M.peakX, M.peakY, F * 0.7, F, W * 0.7, W, footKind},
		{M.peakX, M.peakY, bx, by, F, F * 0.65, W, W * 0.7, footKind},
	}
	main := ridges
	for k := 0; k < S.spurs; k++ {
		side := 1.0 - 2*float64(k%2)
		rd := main[k%2]
		f := 0.15 + 0.7*rng.Next()
		ox, oy := rd.ax+(rd.bx-rd.ax)*f, rd.ay+(rd.by-rd.ay)*f
		h0 := (rd.ha + (rd.hb-rd.ha)*f) * 0.92
		a := theta + side*(math.Pi/2+jit(0.6))
		dx, dy := math.Cos(a), math.Sin(a)
		ln := R * (0.45 + 0.3*rng.Next())
		ridges = append(ridges, ridge{ox, oy, ox + dx*ln, oy + dy*ln, h0, h0 * (0.35 + 0.2*rng.Next()), W * 0.55, W * 0.38, footKind})
	}
	// the high ground: the summit, lesser peaks out along the ridge, and the
	// narrow crest between them
	rp := S.peakR * (0.9 + 0.2*rng.Next())
	ridges = append(ridges, ridge{M.peakX, M.peakY, M.peakX, M.peakY, 1, 1, rp, rp, peakKind})
	for k := 0; k < S.peaks; k++ {
		side := 1.0 - 2*float64(k%2)
		d := rp * (1.1 + 0.4*rng.Next())
		qx := M.peakX + ux*d*side + px*jit(1.5)
		qy := M.peakY + uy*d*side + py*jit(1.5)
		hq := 0.72 + 0.14*rng.Next()
		rq := rp * (0.65 + 0.15*rng.Next())
		ridges = append(ridges,
			ridge{qx, qy, qx, qy, hq, hq, rq, rq, peakKind},
			ridge{M.peakX, M.peakY, qx, qy, 0.74, hq * 0.82, rp * 1.05, rq * 1.1, shoulderKind})
	}
	// the cliff bands of the high ground: steps of 2 or 3 from the foothills'
	// top to the summit
	foot := max(1, int(math.Round(F*float64(S.h))))
	set := []int{}
	for v := 1; v <= foot; v++ {
		set = append(set, v)
	}
	for top := foot; top < S.h; {
		step := 3
		if rng.Next() < 0.45 {
			step = 2
		}
		top = min(S.h, top+step)
		set = append(set, top)
	}
	hf := make([]float64, M.B.n)
	hmax := 0.0
	for li := 0; li < M.B.n; li++ {
		if !M.land[li] {
			continue
		}
		lx, ly := float64(li%w)+0.5, float64(li/w)+0.5
		// domain warp: an irregular outline and wandering crests
		qx := lx + 2.4*no.Eval(lx/10+11.3, ly/10-4.1)
		qy := ly + 2.4*no.Eval(lx/10-7.7, ly/10+21.9)
		h := 0.0
		for _, rd := range ridges {
			h = math.Max(h, rd.at(qx, qy))
		}
		if h <= 0 {
			continue
		}
		// ribs and gullies down the flanks (ridged noise)
		h *= 0.86 + 0.14*(1-math.Abs(no.Eval(qx*0.15+40, qy*0.15-13)))
		h *= M.env[li]
		hf[li] = h
		hmax = math.Max(hmax, h)
	}
	if hmax <= 0 {
		return
	}
	for li := 0; li < M.B.n; li++ {
		if hf[li] <= 0 {
			continue
		}
		t := hf[li] / hmax * float64(S.h)
		if t < 0.6 {
			continue
		}
		lx, ly := float64(li%w), float64(li/w)
		// crags on the high ground: pinnacles and notches rather than a smooth cone
		if f := t / float64(S.h); f > F && !S.mesa {
			t += 2.4 * (f - F) * (0.5 - math.Abs(no.Eval(lx*0.33+71, ly*0.33-19)))
		}
		lv := int(math.Round(t))
		// above the foothills the ground snaps to the cliff bands, their contours
		// wandering a level up and down so a cliff line climbs and falls along the
		// mountainside instead of ringing it
		if lv > foot {
			tj := int(math.Round(t + 1.1*no.Eval(lx*0.14-120, ly*0.14+77)))
			snap := foot
			for _, v := range set {
				if v <= tj {
					snap = v
				}
			}
			lv = max(foot, min(snap, lv+1))
		}
		M.rise[li] = min(lv, S.h)
	}
}

// settle turns rise into final elevations, wears every face down to at most
// maxStep levels between neighbours, and levels a small summit plateau on the
// highest ground left.
func (M *massif) settle() {
	w := M.B.w
	for li := 0; li < M.B.n; li++ {
		M.el[li] = int(M.base[li])
		if M.rise[li] > 0 {
			M.el[li] = max(M.el[li], M.b0+M.rise[li])
		}
	}
	M.wear()
	best, bv := -1, math.Inf(-1)
	for li := 0; li < M.B.n; li++ {
		if M.rise[li] <= 0 {
			continue
		}
		v := float64(M.el[li]) - 0.05*math.Hypot(float64(li%w)+0.5-M.peakX, float64(li/w)+0.5-M.peakY)
		if v > bv {
			best, bv = li, v
		}
	}
	if best < 0 {
		return
	}
	top := M.el[best]
	cx, cy := best%w, best/w
	for dy := -1; dy <= 1; dy++ {
		for dx := -1; dx <= 1; dx++ {
			li := (cy+dy)*w + cx + dx
			if !M.in(cx+dx, cy+dy) || M.rise[li] <= 0 || M.el[li] < top-maxStep {
				continue
			}
			ok := true // raising it must not open a face steeper than maxStep
			for _, d := range dirs4 {
				nx, ny := cx+dx+d[0], cy+dy+d[1]
				if M.in(nx, ny) && M.el[ny*w+nx] < top-maxStep {
					ok = false
				}
			}
			if ok {
				M.el[li] = top
				M.pinned[li] = true
			}
		}
	}
	M.consolidate()
}

// wear lowers every unpinned mountain tile to at most maxStep above its lowest
// neighbour, never below its base ground, until nothing moves.
func (M *massif) wear() {
	w := M.B.w
	for pass := 0; pass < 64; pass++ {
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

// trail finds and carves the switchback trail: a cheapest path in (tile,
// trail level, flat steps since the last rise, heading) space from ordinary
// ground at the foot, on the side facing the island centre, to the summit. Each
// step keeps the trail's level or rises one, and it rises at most once every
// spec.grade tiles — so gaining 9 levels takes ~27 tiles on a slope 14 across,
// and the trail has to traverse and switch back. Turning costs extra, so it runs
// straight between bends. The trail is cut into the slope — up to four levels
// below the ground it crosses, cutting deeper costing more — or built up one
// level at most. Returns the trail as window indices, foot first.
func (M *massif) trail() []int {
	summit, top := -1, 0
	for li := 0; li < M.B.n; li++ {
		if M.pinned[li] && M.el[li] > top {
			summit, top = li, M.el[li]
		}
	}
	if summit < 0 {
		return nil
	}
	// A path in (tile, level) space may pass a tile twice at two levels, which
	// no real trail can. When it does, the tile is held at its first level and
	// the search runs again, until the path is a simple one.
	// A cramped massif may leave no room for the gentlest grade: it then accepts
	// one level per fewer tiles rather than having no trail at all.
	for g := M.spec.grade; g >= 1; g-- {
		if path := M.trailAt(summit, top, g); path != nil {
			return path
		}
	}
	return nil
}

func (M *massif) trailAt(summit, top, grade int) []int {
	hold := map[int]int{}
	for try := 0; try < 24; try++ {
		path, lv := M.trailSearch(summit, top, grade, hold)
		if path == nil {
			return nil
		}
		// Two legs of a switchback side by side more than maxStep apart would be
		// a cliff between them; such a tile is barred from the trail (hold -1).
		w := M.B.w
		at, crossed := map[int]int{}, false
		for k, li := range path {
			if j, dup := at[li]; dup && lv[j] != lv[k] {
				hold[li] = lv[j]
				crossed = true
				break
			}
			for _, d := range dirs4 {
				nb := li + d[1]*w + d[0]
				if j, ok := at[nb]; ok && j < k-1 && abs(lv[j]-lv[k]) > maxStep {
					hold[li] = -1
					crossed = true
				}
				// nor may it pass under the summit plateau's edge
				if _, ok := at[nb]; !ok && M.in(li%w+d[0], li/w+d[1]) && M.pinned[nb] && abs(M.el[nb]-lv[k]) > maxStep {
					hold[li] = -1
					crossed = true
				}
			}
			if crossed {
				break
			}
			at[li] = k
		}
		if !crossed {
			return M.carve(path, lv)
		}
	}
	return nil
}

// trailSearch is one run of the trail's cheapest-path search; hold pins tiles
// a previous run passed twice to the level it first took them at.
func (M *massif) trailSearch(summit, top, grade int, hold map[int]int) ([]int, []int) {
	w, n := M.B.w, M.B.n
	const levels = MaxElev + 1
	// state: (tile, trail level, flat steps since the last rise (capped), heading)
	G := grade
	idx := func(li, t, s, dir int) int { return ((li*levels+t)*G+s)*4 + dir }
	N := n * levels * G * 4
	dist := make([]float64, N)
	prev := make([]int32, N)
	for k := range dist {
		dist[k] = math.Inf(1)
		prev[k] = -1
	}
	pq := &stateHeap{}
	sx, sy := float64(M.B.cx-M.B.x0), float64(M.B.cy-M.B.y0)
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
		dx, dy := float64(lx)-sx, float64(ly)-sy
		if l := math.Hypot(dx, dy); !next || l == 0 || (dx*M.toCentre[0]+dy*M.toCentre[1])/l < 0.1 {
			continue
		}
		for dir := 0; dir < 4; dir++ {
			k := idx(li, M.el[li], G-1, dir)
			dist[k] = 0
			heap.Push(pq, stateItem{k, 0})
		}
	}
	goal := -1
	for pq.Len() > 0 {
		it := heap.Pop(pq).(stateItem)
		if it.d > dist[it.k] {
			continue
		}
		dir, since := it.k%4, (it.k/4)%G
		t, li := (it.k/(4*G))%levels, it.k/(4*G*levels)
		if li == summit && t == top {
			goal = it.k
			break
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
			for up := 0; up <= 1; up++ {
				nt := t + up
				cut := M.el[nj] - nt
				if nt > top || cut > 4 || cut < -2 || (M.pinned[nj] && cut != 0) {
					continue
				}
				if h, held := hold[nj]; held && h != nt {
					continue
				}
				cost := 1.0
				if cut > 0 {
					cost += 0.75 * float64(cut)
				} else if cut < 0 {
					cost += 2.5 * float64(-cut)
				}
				ns := min(G-1, since+1)
				if up == 1 {
					if since < G-1 {
						continue // at most a level per G tiles: it has to traverse
					}
					ns = 0
				}
				if nd != dir {
					cost += 1.3
				}
				k := idx(nj, nt, ns, nd)
				if d2 := it.d + cost; d2 < dist[k] {
					dist[k] = d2
					prev[k] = int32(it.k)
					heap.Push(pq, stateItem{k, d2})
				}
			}
		}
	}
	if goal < 0 {
		return nil, nil
	}
	var path, lv []int
	for k := goal; k >= 0; k = int(prev[k]) {
		path = append(path, k/(4*G*levels))
		lv = append(lv, (k/(4*G))%levels)
	}
	for a, b := 0, len(path)-1; a < b; a, b = a+1, b-1 {
		path[a], path[b] = path[b], path[a]
		lv[a], lv[b] = lv[b], lv[a]
	}
	return path, lv
}

// carve cuts the trail into the massif: its tiles take the trail's levels and
// are pinned, a diagonal staircase gets its other corner too (so a keeper
// walking it on the diagonal never steps into the face beside it), the faces
// round it are worn down again, and a notch or built-up step that leaves a
// lower neighbour more than maxStep under the trail gets a bench filled in.
func (M *massif) carve(path, lv []int) []int {
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
		if M.in(ox, oy) && !M.pinned[o] && M.land[o] && M.rise[o] > 0 {
			M.el[o], M.pinned[o], M.corner[o] = lv[k], true, true
		}
	}
	M.consolidate()
	return path
}

// consolidate makes every face maxStep levels or less once the summit and the
// trail are pinned: unpinned ground is worn down, and where wearing has left
// something under a pinned tile by more than maxStep, that neighbour is filled
// up to the step and pinned as a bench, so the next wear cannot take it away
// again. Each bench is lower than what it holds up, so this settles.
func (M *massif) consolidate() {
	w, n := M.B.w, M.B.n
	for pass := 0; pass < 32; pass++ {
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
// tileVis (rock on the faces, ledges on the terraces, snow up high, the
// trail), the cleared nodes and decor, and the few nodes it places.
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
	footLvl := max(1, int(math.Round(M.spec.foot*float64(M.spec.h))))
	for li := 0; li < M.B.n; li++ {
		if M.rise[li] <= 0 && !M.pinned[li] {
			continue
		}
		lx, ly := li%bw, li/bw
		x, y := M.B.x0+lx, M.B.y0+ly
		i := y*SIZE + x
		e := M.el[li]
		// a cliff edge: a neighbour 2+ levels below, so this stack is a face the
		// camera sees (or a brink to fall from); the foot of a wall is a ledge
		steep := false
		for _, d := range dirs4 {
			if M.in(lx+d[0], ly+d[1]) && e-M.el[(ly+d[1])*bw+lx+d[0]] >= 2 {
				steep = true
			}
		}
		trailTile := (onTrail[li] && li != trail[len(trail)-1]) || M.corner[li]
		// The foothills keep the island's own ground — grass, sand, snow, mud — and
		// whatever grows on it, so the rock starts where the mountain does. Only
		// their cliff edges are bare rock.
		if !steep && !trailTile && M.rise[li] <= footLvl && e-M.b0 <= footLvl {
			s.Changes = append(s.Changes, TileChange{I: i, Elev: uint8(e), Vis: w.TileVis[i], BaseElev: w.Elev[i], BaseVis: w.TileVis[i], Keep: true})
			s.Mark[i] = true
			pk.X0, pk.Y0, pk.X1, pk.Y1 = min(pk.X0, x), min(pk.Y0, y), max(pk.X1, x), max(pk.Y1, y)
			// scree: the odd boulder or stone rolled down onto the upper foothills
			_, hasNode := w.Nodes[i]
			_, hasDecor := w.Decor[i]
			if r := M.rng.Next(); !hasNode && !hasDecor && M.rise[li] >= 2 && !beside(li, onTrail) && !onTrail[li] {
				if r > 0.975 {
					s.Nodes[i] = NodeBoulder
				} else if r > 0.94 {
					s.Nodes[i] = NodeStone
				}
			}
			continue
		}
		// the odd outcrop on a terrace
		if !steep && M.noise.Eval(float64(x)*0.37+500, float64(y)*0.37-500) > 0.7 {
			steep = true
		}
		vis := VisLedge
		if steep {
			vis = VisRock
		}
		if M.spec.snowAt > 0 && e-M.b0 >= M.spec.snowAt {
			vis += VisSnow - VisRock
		}
		// the trail and the corners widening it; its last tile, on the summit
		// plateau, stays a ledge like the rest of the plateau
		if trailTile {
			vis = VisTrail
		}
		s.Changes = append(s.Changes, TileChange{I: i, Elev: uint8(e), Vis: vis, BaseElev: w.Elev[i], BaseVis: w.TileVis[i]})
		s.Mark[i] = true
		if vis == VisRock || vis == VisSnow {
			s.Face[i] = true
		}
		if k, ok := w.Nodes[i]; ok {
			s.BaseNodes[i] = k
		}
		if d, ok := w.Decor[i]; ok {
			s.BaseDecor[i] = d
		}
		pk.X0, pk.Y0, pk.X1, pk.Y1 = min(pk.X0, x), min(pk.Y0, y), max(pk.X1, x), max(pk.Y1, y)
		if uint8(e) > pk.Top {
			pk.Top, pk.Summit = uint8(e), [2]int{x, y}
		}
		// a few boulders and stones on the ledges (crystals on the Spire's, which
		// grow no trees), stunted trees on the Woods' lowest
		if vis != VisLedge && vis != VisSnowLedge || onTrail[li] || M.corner[li] || beside(li, onTrail) || e >= M.b0+M.spec.h {
			continue
		}
		r := M.rng.Next()
		switch {
		case M.spec.trees > 0 && e-M.b0 <= footLvl+2 && r < M.spec.trees:
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
		last := trail[len(trail)-1]
		pk.Summit, pk.Top = [2]int{M.B.x0 + last%bw, M.B.y0 + last/bw}, uint8(M.el[last])
	}
	if pk.X1 >= 0 {
		s.Peaks = append(s.Peaks, pk)
	}
}
