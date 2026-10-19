package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// The base layer of the enemy brain (creature_base.go): perception, grudge and
// stickiness, attack slots and the ring, momentum, steering and stuck recovery.

// addBaseKeeper joins a second surface keeper standing at (x, y).
func (f *fixture) addBaseKeeper(x, y float64) *Player {
	s := NewSession("test02", "u_test2", "Second", "test")
	q := newPlayer(s, f.r.spawn, f.now, f.r.defs)
	q.X, q.Y = x, y
	q.chunkInit = true
	f.r.players[s.ID] = q
	f.r.playerOrder = append(f.r.playerOrder, q)
	return q
}

// A wall hides a keeper from sight; hearing and noise see through it.
func TestBasePerception(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	c := f.spawnAt("crawler", gx, gy, true)
	seen := func(dist float64) bool {
		f.p.X, f.p.Y = gx+dist, gy
		q, _ := f.r.selectPrey(c, "crawler", 26, 40)
		return q != nil
	}
	if !seen(15) {
		t.Fatal("keeper 15 tiles away in the open was not seen")
	}
	for dy := -1; dy <= 1; dy++ {
		f.wallAt(int(gx)+2, int(gy)+dy)
	}
	if seen(15) {
		t.Fatal("keeper 15 tiles away behind a wall was seen")
	}
	if !seen(5.5) {
		t.Fatal("keeper 5.5 tiles away behind a wall was not heard")
	}
	if seen(10) {
		t.Fatal("silent keeper 10 tiles away behind a wall was detected")
	}
	f.p.LastAtk = f.now
	if !seen(10) {
		t.Fatal("keeper swinging 10 tiles away behind a wall was not heard")
	}
	if seen(15) {
		t.Fatal("a swing was heard 15 tiles away")
	}
	f.p.LastAtk = f.now - 2000
	if seen(10) {
		t.Fatal("a swing from 2 s ago is still audible")
	}
}

// What blocks a sight line: walls, blocking structures and medic huts, but not
// the tiles at either end, a farm plot or open ground.
func TestBaseLOSBlockers(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	los := func() bool { return f.r.creLOS(gx, gy, gx+10, gy) }
	if !los() {
		t.Fatal("open ground blocks sight")
	}
	f.wallAt(int(gx), int(gy))
	f.wallAt(int(gx)+10, int(gy))
	if !los() {
		t.Fatal("a wall on the start or end tile blocks sight")
	}
	mid := ti(gx+4, gy)
	f.r.structures[mid] = &Structure{Kind: "farmplot", HP: 1}
	if !los() {
		t.Fatal("a farm plot blocks sight")
	}
	delete(f.r.structures, mid)
	f.r.medicTiles[mid] = true
	blocked := !los()
	delete(f.r.medicTiles, mid)
	if !blocked {
		t.Fatal("a medic hut tile does not block sight")
	}
	f.wallAt(int(gx)+4, int(gy))
	if los() {
		t.Fatal("a wall mid-way does not block sight")
	}
}

// A keeper carrying essence or meat is smelled from far beyond sight.
func TestBaseScent(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	c := f.spawnAt("stalker", gx, gy, true)
	f.p.X, f.p.Y = gx+33, gy
	pick := func() (*Player, float64) { return f.r.selectPrey(c, "stalker", 26, 40) }
	if q, _ := pick(); q != nil {
		t.Fatal("empty-handed keeper 33 tiles away was detected")
	}
	f.p.Inv["essence"] = 1
	if q, d := pick(); q != f.p || math.Abs(d-33) > 1e-9 {
		t.Fatalf("keeper with essence 33 tiles away: got %v at %.2f, want it at 33", q != nil, d)
	}
	f.p.Inv["essence"], f.p.Inv["meat"] = 0, 1
	if q, _ := pick(); q == nil {
		t.Fatal("keeper with meat 33 tiles away was not smelled")
	}
	f.p.X = gx + 41
	if q, _ := pick(); q != nil {
		t.Fatal("scent reached past its radius")
	}
}

// The pick: a grudge outweighs a slightly nearer keeper, the current prey is
// kept against a marginally nearer one, a much nearer one still wins.
func TestBaseGrudgeAndStickiness(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	a := f.p
	b := f.addBaseKeeper(gx, gy+8.5)
	a.X, a.Y = gx+10, gy
	c := f.spawnAt("crawler", gx, gy, true)
	pick := func() *Player { q, _ := f.r.selectPrey(c, "crawler", 26, 40); return q }
	if pick() != b {
		t.Fatal("nearest keeper was not picked")
	}

	c.CarrySp = 1
	f.r.baseOnStruck(c, a)
	if c.CarrySp != 0 {
		t.Fatal("being struck kept the creature's momentum")
	}
	if q, d := f.r.selectPrey(c, "crawler", 26, 40); q != a || math.Abs(d-10) > 1e-9 {
		t.Fatalf("grudge did not win against a nearer keeper (distance %.2f)", d)
	}
	for k := 0; k < baseGrudgeTicks; k++ {
		f.r.baseTimers(c)
	}
	if pick() != b {
		t.Fatal("the grudge outlived its 50 ticks")
	}

	c.AggroID = a.S.ID
	if pick() != a {
		t.Fatal("stickiness did not keep the current prey against a keeper 1.5 nearer")
	}
	b.Y = gy + 5
	if pick() != b {
		t.Fatal("a keeper 5 tiles nearer did not win against stickiness")
	}

	c.AggroID = ""
	b.Y = gy + 11
	if pick() != a {
		t.Fatal("nearest keeper was not picked")
	}
	b.HP = 2
	if pick() != b {
		t.Fatal("a wounded keeper 1 tile farther was not finished first")
	}
}

// Five crawlers on one keeper: three close in, two hold the ring.
func TestBaseSlotsAndRing(t *testing.T) {
	f := newFixture(t)
	f.p.HP = 1 << 20
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.p.X, f.p.Y = gx, gy
	var cs []*Creature
	for k := 0; k < 5; k++ {
		a := float64(k) * 2 * math.Pi / 5
		rr := 6 + 0.2*float64(k)
		cs = append(cs, f.spawnAt("crawler", gx+math.Cos(a)*rr, gy+math.Sin(a)*rr, true))
	}
	dist := func(c *Creature) float64 { return math.Hypot(c.X-f.p.X, c.Y-f.p.Y) }
	minWait := math.Inf(1)
	for tk := 1; tk <= 24; tk++ {
		f.r.tickN++
		f.r.creatureTick(1, f.now)
		if tk < 2 {
			continue // no prey is chosen before the first step
		}
		slots := 0
		for _, c := range cs {
			if !c.NoSlot {
				slots++
			} else {
				minWait = math.Min(minWait, dist(c))
			}
		}
		if slots != aiSlotMax {
			t.Fatalf("tick %d: %d creatures hold a slot, want %d", tk, slots, aiSlotMax)
		}
	}
	if minWait < aiRingRadius-0.8 {
		t.Fatalf("a waiter came within %.2f of the keeper, ring is %.1f", minWait, aiRingRadius)
	}
	for _, c := range cs {
		if c.NoSlot {
			if d := dist(c); d > aiRingRadius+1.3 {
				t.Fatalf("a waiter drifted %.2f from the keeper instead of holding the ring", d)
			}
		} else if d := dist(c); d > 1.8 {
			t.Fatalf("a slot holder is still %.2f tiles from the keeper", d)
		}
	}
}

// The ring aim, the back-off, and who is exempt.
func TestBaseRingTactics(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.p.X, f.p.Y = gx, gy
	c := f.spawnAt("crawler", gx+5, gy, true)
	pd := func(x, y float64) float64 { return math.Hypot(x-gx, y-gy) }

	if x, y, m := f.r.baseTactics(c, "crawler", f.p, gx, gy); x != gx || y != gy || m != 1 {
		t.Fatal("a creature with a slot had its aim bent")
	}
	c.NoSlot = true
	x, y, m := f.r.baseTactics(c, "crawler", f.p, gx, gy)
	if math.Abs(pd(x, y)-aiRingRadius) > 1e-9 || m >= 1 {
		t.Fatalf("waiter aim is %.2f from the keeper at speed x%.2f, want the ring at a crawl", pd(x, y), m)
	}
	c.X = gx + 2
	if x, y, _ = f.r.baseTactics(c, "crawler", f.p, gx, gy); pd(x, y) <= pd(c.X, c.Y) {
		t.Fatal("a waiter crowding the keeper did not back off")
	}
	c.X = gx + 8
	if x, y, m = f.r.baseTactics(c, "crawler", f.p, gx, gy); x != gx || y != gy || m != 1 {
		t.Fatal("a waiter far out had its aim bent")
	}
	c.X = gx + 5
	c.Retreat = 3
	if x, y, m = f.r.baseTactics(c, "crawler", f.p, gx, gy); x != gx || y != gy || m != 1 {
		t.Fatal("a retreating waiter had its aim bent")
	}
	c.Retreat = 0
	b := f.spawnAt("brute", gx+5, gy, true)
	b.NoSlot = true
	if x, y, m = f.r.baseTactics(b, "brute", f.p, gx, gy); x != gx || y != gy || m != 1 {
		t.Fatal("a brute holds a ring")
	}
}

// From rest the first step is short and full speed comes by the third; a
// reversal halves the carried speed; a pause starts again from rest.
func TestBaseMomentum(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.p.X, f.p.Y = gx+6, gy
	c := f.spawnAt("crawler", gx, gy, true)
	var steps []float64
	for k := 0; k < 4; k++ {
		px, py := c.X, c.Y
		f.run(c, 1)
		steps = append(steps, math.Hypot(c.X-px, c.Y-py))
	}
	const full = 0.47 // crawler 0.44 + 0.03 per strength
	if steps[0] >= full-0.05 {
		t.Fatalf("first step from rest was %.3f, full is %.2f", steps[0], full)
	}
	if math.Abs(steps[2]-full) > 1e-9 || math.Abs(steps[3]-full) > 1e-9 {
		t.Fatalf("steps %v: not at full speed %.2f by the third tick", steps, full)
	}

	r := f.r
	r.tickN = 50
	c.CarrySp, c.CarryAng, c.CarryAt, c.HeadOK = full, 0, 50, true
	r.tickN = 51
	if out := r.rampSpeed(c, full, math.Pi, aiHunt); math.Abs(c.CarrySp-full/2) > 1e-9 || out > full/2+1e-9 {
		t.Fatalf("reversal left carried speed %.3f (step %.3f), want %.3f", c.CarrySp, out, full/2)
	}
	c.CarrySp, c.CarryAng, c.CarryAt = full, 0, 51
	r.tickN = 52
	if out := r.rampSpeed(c, full, 0.5, aiHunt); out != full {
		t.Fatalf("a gentle turn cost speed: %.3f", out)
	}
	c.CarryAt = 52
	r.tickN = 56
	if out := r.rampSpeed(c, full, 0, aiHunt); out >= full-0.05 {
		t.Fatalf("a pause did not reset momentum: %.3f", out)
	}
	c.CarrySp, c.CarryAt = full, 56
	r.tickN = 57
	if out := r.rampSpeed(c, 0.05, 0, aiHunt); out != 0.05 {
		t.Fatalf("slowing was not immediate or exceeded sp: %.3f", out)
	}
}

// Steering slides the way it was going, and keeps the straight tile when boxed in.
func TestBaseSteer(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	c := f.spawnAt("crawler", gx, gy, true)
	straight := func(i int, x, y float64) bool { return math.Abs(y-gy) < 1e-9 }
	if _, ny, _, moved := f.r.steer(c, 0, 0.4, straight); !moved || ny <= gy {
		t.Fatalf("default slide: moved=%v ny-gy=%.3f, want a +35 turn", moved, ny-gy)
	}
	c.CarryAng, c.HeadOK = -0.6, true
	if _, ny, _, moved := f.r.steer(c, 0, 0.4, straight); !moved || ny >= gy {
		t.Fatalf("slide against the previous heading: moved=%v ny-gy=%.3f, want a -35 turn", moved, ny-gy)
	}
	all := func(int, float64, float64) bool { return true }
	nx, ny, ni, moved := f.r.steer(c, 0, 0.4, all)
	if moved || ni != ti(gx+0.4, gy) || nx != gx+0.4 || math.Abs(ny-gy) > 1e-9 {
		t.Fatalf("boxed in: moved=%v ni=%d, want the straight tile %d", moved, ni, ti(gx+0.4, gy))
	}
}

// A creature that goes nowhere drops its detour and sidesteps; one that is
// gnawing, holding a ring, close to its goal, moving or merely interrupted does not.
func TestBaseStuckRecovery(t *testing.T) {
	f := newFixture(t)
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	r := f.r
	r.tickN = 100
	drive := func(c *Creature, goal float64, n int) {
		for k := 0; k < n; k++ {
			r.tickN++
			r.trackProgress(c, goal, gy, aiHunt)
		}
	}

	c := f.spawnAt("crawler", gx, gy, true)
	c.Path, c.PathFailAt = []int{1, 2}, 3
	drive(c, gx+10, 9)
	if c.Path == nil || c.UnstickTk != 0 {
		t.Fatal("recovery fired before ten idle ticks")
	}
	drive(c, gx+10, 3)
	if c.Path != nil || c.PathFailAt != 0 || c.UnstickTk == 0 {
		t.Fatalf("stuck creature kept its detour (path %v, fail %d, sidestep %d)", c.Path, c.PathFailAt, c.UnstickTk)
	}
	if math.Abs(math.Cos(c.UnstickAng)) > 1e-9 {
		t.Fatalf("sidestep heading %.2f is not perpendicular to the goal", c.UnstickAng)
	}
	if x, y, _ := r.baseTactics(c, "crawler", f.p, gx+10, gy); math.Abs(x-c.X) > 1e-9 || math.Abs(y-c.Y) != 3 {
		t.Fatalf("baseTactics did not sidestep: aim %.2f,%.2f from %.2f,%.2f", x, y, c.X, c.Y)
	}

	g := f.spawnAt("crawler", gx, gy, true)
	g.PathFailAt = 5 // a failed detour search with no path: it is gnawing through
	drive(g, gx+10, 25)
	if g.PathFailAt != 5 || g.UnstickTk != 0 {
		t.Fatal("a gnawing creature was pulled off its wall")
	}

	h := f.spawnAt("crawler", gx, gy, true)
	h.Path, h.NoSlot = []int{1}, true
	drive(h, gx+10, 25)
	if h.Path == nil || h.UnstickTk != 0 {
		t.Fatal("a ring holder was treated as stuck")
	}

	n := f.spawnAt("crawler", gx, gy, true)
	n.Path = []int{1}
	drive(n, gx+1, 25)
	if n.Path == nil || n.UnstickTk != 0 {
		t.Fatal("a creature at its goal was treated as stuck")
	}

	m := f.spawnAt("crawler", gx, gy, true)
	m.Path = []int{1}
	for k := 0; k < 30; k++ {
		m.X += 0.05 // under the progress threshold per tick, but it walks
		drive(m, gx+20, 1)
	}
	if m.Path == nil || m.UnstickTk != 0 {
		t.Fatal("a creature that is walking was treated as stuck")
	}

	p := f.spawnAt("crawler", gx, gy, true)
	p.Path = []int{1}
	drive(p, gx+10, 6)
	r.tickN += 5 // attacked, stunned or charging: no samples
	drive(p, gx+10, 6)
	if p.Path == nil || p.UnstickTk != 0 {
		t.Fatal("a gap in the samples did not restart the window")
	}
}
