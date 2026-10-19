package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// The level features (creature_elite.go): flank, pack call, dodge, hit and
// run, charge and the alpha. Hooks are driven directly where they can be, so
// none of this depends on the attack machine's timing.

// eliteArena puts the keeper at the centre of an open grass patch, by day.
func eliteArena(t *testing.T) (f *fixture, gx, gy float64) {
	t.Helper()
	f = newFixture(t)
	f.setDay()
	gx, gy = landPatch(t, f.r, world.TGrass, 7)
	f.stand(gx, gy, 0)
	f.p.HP = 100
	return f, gx, gy
}

// leveled spawns a creature of a type at a level.
func (f *fixture) leveled(typ string, lvl int, x, y float64) *Creature {
	c := f.spawnAt(typ, x, y, false)
	c.setLevel(lvl)
	return c
}

// forceRoll makes every dodge roll come up v for the rest of the test.
func forceRoll(t *testing.T, v float64) {
	t.Helper()
	old := eliteRoll
	eliteRoll = func() float64 { return v }
	t.Cleanup(func() { eliteRoll = old })
}

// addKeeper joins a second keeper standing at (x, y).
func (f *fixture) addKeeper(x, y float64) *Player {
	s := NewSession("test02", "u_two", "Two", "test")
	q := newPlayer(s, f.r.spawn, f.now, f.r.defs)
	q.X, q.Y, q.HP = x, y, 100
	f.r.players[s.ID] = q
	f.r.playerOrder = append(f.r.playerOrder, q)
	return q
}

func near(a, b float64) bool { return math.Abs(a-b) < 1e-6 }

// frames returns every message of a type seen since the last reset.
func (f *fixture) frames(typ string) []map[string]any {
	f.lastOfType(typ) // drains into f.seen
	var out []map[string]any
	for _, m := range f.seen {
		if m["t"] == typ {
			out = append(out, m)
		}
	}
	return out
}

func TestEliteLevelOneHasNoAbilities(t *testing.T) {
	forceRoll(t, 0)
	f, gx, gy := eliteArena(t)
	f.p.FaceX, f.p.FaceY = 1, 0
	for typ := range creTypes {
		c := f.spawnAt(typ, gx+5, gy+1, false)
		if c.Abil != 0 || c.Level != 1 {
			t.Fatalf("%s: level %d abilities %b, want a plain level 1", typ, c.Level, c.Abil)
		}
		if _, ok := f.r.tryDodge(f.p, c); ok {
			t.Fatalf("%s dodged at level 1", typ)
		}
		if tx, ty := f.r.eliteTactics(c, typ, f.p, f.p.X, f.p.Y); tx != f.p.X || ty != f.p.Y || c.ChargeTk != 0 {
			t.Fatalf("%s bent its aim or charged at level 1", typ)
		}
		if f.r.eliteSpeed(c, typ) != 1 || f.r.eliteOverride(c, typ, 0.5, f.now) {
			t.Fatalf("%s: speed or override changed at level 1", typ)
		}
		struckRetreat(c, typ)
		if want := map[string]int{"stalker": 7, "husk_wolf": 4, "drowned": 4}[typ]; c.Retreat != want {
			t.Fatalf("%s retreat %d after a blow, want %d", typ, c.Retreat, want)
		}
	}
	if f.lastOfType("ccharge") != nil || f.lastOfType("crally") != nil {
		t.Fatal("a level-1 creature sent a level-feature frame")
	}
}

func TestEliteKitLimitsWhatATypeLearns(t *testing.T) {
	for _, typ := range []string{"brute", "bog_shambler", "blight_lancer"} {
		if abilitiesFor(typ, 5)&abDodge != 0 {
			t.Errorf("%s learned to dodge", typ)
		}
	}
	if abilitiesFor("wisp", 5)&(abCharge|abFlank|abAlpha) != 0 {
		t.Error("a wisp learned more than the dodge")
	}
}

// --- flank -----------------------------------------------------------------

func TestEliteFlankBendsTheAimOffTheDirectLine(t *testing.T) {
	f, gx, gy := eliteArena(t)
	f.p.FaceX, f.p.FaceY = 1, 0 // the keeper looks east; the creature is east of it
	c := f.leveled("crawler", 2, gx+5, gy+1)
	ax, ay := f.r.eliteTactics(c, "crawler", f.p, gx, gy)
	// on the keeper's left (+y): prey + perp*2.2 - face*1
	if !near(ax, gx-1) || !near(ay, gy+2.2) {
		t.Fatalf("flank aim (%.2f, %.2f), want (%.2f, %.2f)", ax, ay, gx-1, gy+2.2)
	}
	// and off the line from the creature to the keeper
	dx, dy := gx-c.X, gy-c.Y
	if off := math.Abs(dx*(ay-c.Y)-dy*(ax-c.X)) / math.Hypot(dx, dy); off < 1 {
		t.Fatalf("flank aim is only %.2f tiles off the direct line", off)
	}

	l1 := f.leveled("crawler", 1, gx+5, gy+1)
	if x, y := f.r.eliteTactics(l1, "crawler", f.p, gx, gy); x != gx || y != gy {
		t.Fatal("a level-1 crawler flanked")
	}
}

func TestEliteFlankGoesStraightInFromBehindOrBeside(t *testing.T) {
	f, gx, gy := eliteArena(t)
	c := f.leveled("crawler", 2, gx+5, gy+1)
	for _, face := range [][2]float64{{-1, 0}, {0, 1}, {0, 0}} { // back turned, beside, no facing yet
		f.p.FaceX, f.p.FaceY = face[0], face[1]
		if x, y := f.r.eliteTactics(c, "crawler", f.p, gx, gy); x != gx || y != gy {
			t.Fatalf("flanked a keeper facing %v", face)
		}
	}
}

func TestEliteFlankLeavesOtherAimsAlone(t *testing.T) {
	f, gx, gy := eliteArena(t)
	f.p.FaceX, f.p.FaceY = 1, 0
	c := f.leveled("crawler", 2, gx+5, gy+1)
	c.Retreat = 3
	rx, ry := c.X+4, c.Y+2 // a retreat point
	if x, y := f.r.eliteTactics(c, "crawler", f.p, rx, ry); x != rx || y != ry {
		t.Fatal("bent a retreat aim")
	}
	c.Retreat = 0
	if x, y := f.r.eliteTactics(c, "crawler", f.p, c.X, c.Y+4); x != c.X || y != c.Y+4 {
		t.Fatal("bent an aim that does not point at the keeper")
	}
	far := f.leveled("crawler", 2, gx+12, gy+1) // beyond 9 tiles
	if x, y := f.r.eliteTactics(far, "crawler", f.p, gx, gy); x != gx || y != gy {
		t.Fatal("flanked from beyond 9 tiles")
	}
	inside := f.leveled("crawler", 2, gx+1.5, gy) // within 2 tiles
	if x, y := f.r.eliteTactics(inside, "crawler", f.p, gx, gy); x != gx || y != gy {
		t.Fatal("flanked from inside 2 tiles")
	}
}

func TestEliteRingWaiterCirclesToTheKeepersBack(t *testing.T) {
	f, gx, gy := eliteArena(t)
	f.p.FaceX, f.p.FaceY = 1, 0
	c := f.leveled("crawler", 2, gx-3, gy+1) // behind the keeper
	c.NoSlot, c.Orbit = true, 1
	ax, ay := f.r.eliteTactics(c, "crawler", f.p, gx, gy)
	if d := math.Hypot(ax-gx, ay-gy); !near(d, aiRingRadius) {
		t.Fatalf("ring spot %.2f tiles out, want %.2f", d, aiRingRadius)
	}
	if dot := (ax - gx) / aiRingRadius; dot > -0.9 {
		t.Fatalf("ring spot is not behind the keeper (cos %.2f to its heading)", dot)
	}
	// from the front it first goes round to the side, never across the keeper
	front := f.leveled("crawler", 2, gx+3, gy+0.5)
	front.NoSlot = true
	bx, by := f.r.eliteTactics(front, "crawler", f.p, gx, gy)
	if !near(bx, gx) || !near(by, gy+aiRingRadius) {
		t.Fatalf("front waiter aims (%.2f, %.2f), want the ring beside the keeper", bx, by)
	}
}

func TestEliteLancerStrafesTowardTheKeepersBack(t *testing.T) {
	f, gx, gy := eliteArena(t)
	f.p.FaceX, f.p.FaceY = 1, 0
	// cosBack: how nearly (x, y) lies straight behind the keeper (who faces +x)
	cosBack := func(x, y float64) float64 { return (gx - x) / math.Hypot(x-gx, y-gy) }
	for _, y := range []float64{gy + 1, gy - 1} {
		for _, orbit := range []float64{1, -1} {
			c := f.leveled("blight_lancer", 3, gx+6, y)
			c.Orbit = orbit
			f.r.eliteTactics(c, "blight_lancer", f.p, gx, gy)
			// creatureTactics strafes along (-ay, ax)*Orbit, a being the unit vector at the keeper
			h := math.Hypot(gx-c.X, gy-c.Y)
			ax, ay := (gx-c.X)/h, (gy-c.Y)/h
			nx, ny := c.X-ay*c.Orbit*0.5, c.Y+ax*c.Orbit*0.5
			if cosBack(nx, ny) <= cosBack(c.X, c.Y) {
				t.Fatalf("lancer at y%+.0f strafes away from the keeper's back (Orbit %v)", y-gy, c.Orbit)
			}
		}
	}
	lv2 := f.leveled("blight_lancer", 2, gx+6, gy+1)
	lv2.Orbit = -1
	f.r.eliteTactics(lv2, "blight_lancer", f.p, gx, gy)
	if lv2.Orbit != -1 {
		t.Fatal("a level-2 lancer chose its strafe side")
	}
}

// --- pack call -------------------------------------------------------------

func TestElitePackCallRousesIdleAlliesWithinTwelveTiles(t *testing.T) {
	f, gx, gy := eliteArena(t)
	caller := f.leveled("crawler", 2, gx, gy)
	idle := f.spawnAt("crawler", gx+6, gy, false)
	far := f.spawnAt("crawler", gx+13, gy, false)
	busy := f.spawnAt("crawler", gx-6, gy, false)
	busy.Hunting, busy.LastX = true, -1
	wisp := f.spawnAt("wisp", gx+3, gy, false)
	f.r.creatureSawPrey(caller, f.p)
	if idle.Memory != aiMemoryTicks || idle.LastX != f.p.X || idle.LastY != f.p.Y {
		t.Fatalf("idle crawler not roused: memory %d at (%v,%v)", idle.Memory, idle.LastX, idle.LastY)
	}
	if far.Memory != 0 || busy.LastX != -1 || wisp.Memory != 0 {
		t.Fatal("rousing reached a creature out of range, already hunting, or a wisp")
	}
	if f.lastOfType("crally") != nil {
		t.Fatal("a plain pack call sent crally")
	}
}

func TestElitePackCallNeedsTheAbility(t *testing.T) {
	f, gx, gy := eliteArena(t)
	caller := f.leveled("crawler", 1, gx, gy)
	idle := f.spawnAt("crawler", gx+6, gy, false)
	f.r.creatureSawPrey(caller, f.p)
	if idle.Memory != 0 {
		t.Fatal("a level-1 crawler's lock-on roused its neighbour")
	}
	st := f.leveled("stalker", 4, gx, gy) // stalkers never learn the call
	f.r.packCall(st, f.p)
	if idle.Memory != 0 {
		t.Fatal("a stalker called its pack")
	}
}

// --- dodge -----------------------------------------------------------------

func TestEliteDodgeMakesTheSwingMiss(t *testing.T) {
	forceRoll(t, 0)
	f, gx, gy := eliteArena(t)
	f.p.Equip = "sword" // 3 damage would kill a 2 hp crawler
	c := f.leveled("crawler", 3, gx+1, gy)
	f.reset()
	f.r.handleAtk(f.p, map[string]any{"t": "atk", "seq": float64(9), "dx": float64(1)})

	if act := f.lastOfType("act"); act == nil || act["targetI"] != nil {
		t.Fatalf("act = %v, want targetI null", act)
	}
	cd := f.lastOfType("cdodge")
	if cd == nil || cd["id"] != c.ID || cd["by"] != f.p.S.ID || cd["seq"] != float64(9) {
		t.Fatalf("cdodge = %v", cd)
	}
	ang, ok := cd["ang"].(float64)
	if !ok || !near(math.Abs(math.Sin(ang)), 1) {
		t.Fatalf("cdodge ang = %v, want a hop at right angles to the swing", cd["ang"])
	}
	if c.HP != 2 || f.lastOfType("chit") != nil {
		t.Fatalf("the dodged swing still landed: hp %d", c.HP)
	}
	if !near(math.Hypot(c.X-(gx+1), c.Y-gy), 1) || !near(c.X, gx+1) {
		t.Fatalf("hopped to (%.2f, %.2f) from (%.2f, %.2f), want 1 tile sideways", c.X, c.Y, gx+1, gy)
	}
	if c.DodgeCd != 12 {
		t.Fatalf("dodge cooldown %d, want 12", c.DodgeCd)
	}
}

func TestEliteDodgeChanceByLevel(t *testing.T) {
	f, gx, gy := eliteArena(t)
	for _, tc := range []struct {
		lvl     int
		yes, no float64
	}{{3, 0.29, 0.31}, {4, 0.34, 0.36}, {5, 0.44, 0.46}} {
		for roll, want := range map[float64]bool{tc.yes: true, tc.no: false} {
			forceRoll(t, roll)
			c := f.leveled("crawler", tc.lvl, gx+1, gy)
			if _, ok := f.r.tryDodge(f.p, c); ok != want {
				t.Errorf("level %d, roll %.2f: dodged %v, want %v", tc.lvl, roll, ok, want)
			}
			f.r.delCreature(c.ID)
		}
	}
}

func TestEliteDodgeNeedsAFreeCreature(t *testing.T) {
	forceRoll(t, 0)
	f, gx, gy := eliteArena(t)
	for name, mutate := range map[string]func(*Creature){
		"mid-windup": func(c *Creature) { c.AtkState = atkReady + 1 },
		"stunned":    func(c *Creature) { c.Stun = 2 },
		"charging":   func(c *Creature) { c.ChargeTk = 2 },
		"cooldown":   func(c *Creature) { c.DodgeCd = 3 },
	} {
		c := f.leveled("crawler", 3, gx+1, gy)
		mutate(c)
		if _, ok := f.r.tryDodge(f.p, c); ok {
			t.Errorf("%s creature dodged", name)
		}
		f.r.delCreature(c.ID)
	}
	// mid-windup through the real swing: the keeper lands it
	f.p.Equip = "sword"
	c := f.leveled("crawler", 3, gx+1, gy)
	c.AtkState = atkReady + 1
	f.r.handleAtk(f.p, map[string]any{"t": "atk", "seq": float64(1), "dx": float64(1)})
	if _, alive := f.r.creatures[c.ID]; alive {
		t.Fatal("a creature mid-windup dodged the swing that should have killed it")
	}
}

func TestEliteDodgeCooldownThenReady(t *testing.T) {
	forceRoll(t, 0)
	f, gx, gy := eliteArena(t)
	c := f.leveled("wisp", 3, gx+1, gy)
	if _, ok := f.r.tryDodge(f.p, c); !ok {
		t.Fatal("first dodge failed")
	}
	if _, ok := f.r.tryDodge(f.p, c); ok {
		t.Fatal("dodged again with the cooldown running")
	}
	for i := 0; i < 12; i++ {
		f.r.eliteTick(c, "wisp")
	}
	if _, ok := f.r.tryDodge(f.p, c); !ok {
		t.Fatal("cooldown never ended")
	}
}

func TestEliteDodgeHopsBackWhenBoxedInAndNotAtAllWhenWalled(t *testing.T) {
	forceRoll(t, 0)
	f, gx, gy := eliteArena(t)
	f.p.X = gx - 1
	c := f.leveled("crawler", 3, gx, gy)
	x, y := int(gx), int(gy)
	f.wallAt(x, y+1)
	f.wallAt(x, y-1)
	ang, ok := f.r.tryDodge(f.p, c)
	if !ok || !near(ang, 0) || !near(c.X, gx+1) || !near(c.Y, gy) {
		t.Fatalf("boxed in: ok=%v ang=%.2f at (%.2f, %.2f), want a hop straight back", ok, ang, c.X, c.Y)
	}
	c.X, c.DodgeCd = gx, 0
	f.wallAt(x+1, y)
	if _, ok := f.r.tryDodge(f.p, c); ok || c.X != gx || c.DodgeCd != 0 {
		t.Fatal("dodged with every landing tile walled")
	}
}

// --- hit and run -----------------------------------------------------------

func TestEliteHitAndRun(t *testing.T) {
	f, _, _ := eliteArena(t)
	for _, tc := range []struct {
		typ  string
		lvl  int
		want int
	}{
		{"crawler", 1, 0}, {"crawler", 2, 0}, {"crawler", 3, 3},
		{"stalker", 1, 7}, {"husk_wolf", 5, 4}, {"drowned", 3, 4}, {"brute", 5, 0},
	} {
		c := f.leveled(tc.typ, tc.lvl, 10, 10)
		struckRetreat(c, tc.typ)
		if c.Retreat != tc.want {
			t.Errorf("%s level %d retreats %d after a blow, want %d", tc.typ, tc.lvl, c.Retreat, tc.want)
		}
	}
}

// --- charge ----------------------------------------------------------------

// chargeSetup stands a creature of the type d tiles east of the keeper, who is
// at the arena centre, and starts its charge.
func chargeSetup(t *testing.T, typ string, d float64) (*fixture, *Creature, float64, float64) {
	t.Helper()
	f, gx, gy := eliteArena(t)
	f.p.X = gx - 3
	c := f.leveled(typ, 4, gx-3+d, gy)
	f.reset()
	return f, c, gx, gy
}

func TestEliteChargeWindsUpThenDashesAndHits(t *testing.T) {
	f, c, gx, gy := chargeSetup(t, "husk_wolf", 6)
	sx := c.X
	if ax, ay := f.r.eliteTactics(c, "husk_wolf", f.p, f.p.X, f.p.Y); ax != c.X || ay != c.Y {
		t.Fatalf("the winding-up wolf aims (%v, %v), want to hold still", ax, ay)
	}
	cc := f.lastOfType("ccharge")
	if cc == nil || cc["id"] != c.ID || cc["tk"] != float64(3) || cc["len"] != 7.5 {
		t.Fatalf("ccharge = %v", cc)
	}
	if ang, ok := cc["ang"].(float64); !ok || !near(math.Abs(ang), math.Pi) {
		t.Fatalf("ccharge ang = %v, want west", cc["ang"])
	}
	hp := f.p.HP
	for i := 0; i < 2; i++ { // two more ticks of windup
		if !f.r.eliteOverride(c, "husk_wolf", 0.62, f.now) || c.X != sx {
			t.Fatalf("windup tick %d: the wolf moved or did not own the tick", i+1)
		}
	}
	if !f.r.eliteOverride(c, "husk_wolf", 0.62, f.now) || !near(c.X, sx-1.4) || c.Y != gy {
		t.Fatalf("first dash tick: x %.2f, want %.2f", c.X, sx-1.4)
	}
	if f.p.HP != hp {
		t.Fatal("the keeper was hit before the dash reached it")
	}
	for i := 0; i < 4 && c.ChargeTk > 0; i++ {
		f.r.eliteOverride(c, "husk_wolf", 0.62, f.now)
	}
	if f.p.HP != hp-2 { // wolf dmg 1, +1 for a charge
		t.Fatalf("keeper hp %d -> %d, want -2", hp, f.p.HP)
	}
	if m := f.lastOfType("hp"); m == nil {
		t.Fatal("no hp frame")
	} else if _, ok := m["ang"].(float64); !ok {
		t.Fatalf("hp frame ang = %v, want a number", m["ang"])
	}
	if c.ChargeTk != 0 || c.Dashing || c.ChargeCd != 40 || c.Retreat != 4 {
		t.Fatalf("after the hit: ChargeTk %d, cooldown %d, retreat %d", c.ChargeTk, c.ChargeCd, c.Retreat)
	}
	if c.X < gx-3 {
		t.Fatalf("the wolf ran through the keeper to x %.2f", c.X)
	}
}

func TestEliteChargeEndToEnd(t *testing.T) {
	f, c, _, _ := chargeSetup(t, "husk_wolf", 6)
	hp := f.p.HP
	f.run(c, 12)
	if f.lastOfType("ccharge") == nil {
		t.Fatal("the wolf never announced a charge")
	}
	if f.p.HP > hp-2 {
		t.Fatalf("keeper hp %d -> %d, want at least the charge's 2", hp, f.p.HP)
	}
}

func TestEliteChargeWallStopsDazesAndIsDamaged(t *testing.T) {
	f, c, gx, gy := chargeSetup(t, "husk_wolf", 6)
	f.r.eliteTactics(c, "husk_wolf", f.p, f.p.X, f.p.Y)
	wall := f.wallAt(int(gx), int(gy)) // now in the dash path, 2.5 tiles on
	hp := f.p.HP
	for i := 0; i < 6 && c.ChargeTk > 0; i++ {
		f.r.eliteOverride(c, "husk_wolf", 0.62, f.now)
	}
	if wall.HP != 998 {
		t.Fatalf("wall hp %d, want 998", wall.HP)
	}
	if c.Stun != 5 || c.ChargeTk != 0 || c.ChargeCd != 40 {
		t.Fatalf("after the crash: stun %d, ChargeTk %d, cooldown %d", c.Stun, c.ChargeTk, c.ChargeCd)
	}
	if c.X < gx+0.5 || f.p.HP != hp {
		t.Fatalf("the wolf got through (x %.2f) or the keeper was hurt (hp %d)", c.X, f.p.HP)
	}
	if sd := f.lastOfType("sd"); sd == nil || sd["hp"] != float64(998) {
		t.Fatalf("sd = %v", sd)
	}
}

func TestEliteBruteChargeBreaksAWeakWall(t *testing.T) {
	f, c, gx, gy := chargeSetup(t, "brute", 5)
	f.r.eliteTactics(c, "brute", f.p, f.p.X, f.p.Y)
	if f.lastOfType("ccharge") == nil {
		t.Fatal("a level-4 brute did not charge")
	}
	strong := f.wallAt(int(gx), int(gy))
	for i := 0; i < 8 && c.ChargeTk > 0; i++ {
		f.r.eliteOverride(c, "brute", 0.3, f.now)
	}
	if strong.HP != 994 || c.ChargeCd != 50 {
		t.Fatalf("brute: wall hp %d (want 994), cooldown %d (want 50)", strong.HP, c.ChargeCd)
	}

	c.Stun, c.ChargeCd = 0, 0
	weak := f.wallAt(int(gx), int(gy))
	weak.HP = 5
	f.r.eliteTactics(c, "brute", f.p, f.p.X, f.p.Y) // the line is blocked again
	if c.ChargeTk != 0 {
		t.Fatal("charged at a keeper behind a wall")
	}
	c.ChargeTk, c.Dashing, c.ChargeLen, c.ChargeAng = 1, false, 6.5, math.Pi
	c.X, c.Y = gx+1, gy
	f.reset()
	f.r.eliteOverride(c, "brute", 0.3, f.now)
	if _, standing := f.r.structures[ti(gx, gy)]; standing {
		t.Fatal("the weak wall survived the brute")
	}
	if sd := f.lastOfType("sd"); sd == nil || sd["hp"] != float64(0) {
		t.Fatalf("sd = %v, want hp 0", sd)
	}
}

func TestEliteChargeConditions(t *testing.T) {
	for name, tc := range map[string]struct {
		typ    string
		lvl    int
		d      float64
		mutate func(*Creature)
		walls  bool
	}{
		"in range":      {"husk_wolf", 4, 6, nil, false},
		"too close":     {"husk_wolf", 4, 3.5, nil, false},
		"too far":       {"husk_wolf", 4, 8.5, nil, false},
		"level 3":       {"husk_wolf", 3, 6, nil, false},
		"cooldown":      {"husk_wolf", 4, 6, func(c *Creature) { c.ChargeCd = 5 }, false},
		"mid-attack":    {"husk_wolf", 4, 6, func(c *Creature) { c.AtkState = atkReady + 1 }, false},
		"panicking":     {"husk_wolf", 4, 6, func(c *Creature) { c.PanicTk = 3 }, false},
		"awaiting slot": {"husk_wolf", 4, 6, func(c *Creature) { c.NoSlot = true }, false},
		"retreating":    {"husk_wolf", 4, 6, func(c *Creature) { c.Retreat = 2 }, false},
		"wall between":  {"husk_wolf", 4, 6, nil, true},
		"brute 3.5":     {"brute", 4, 3.5, nil, false},
		"brute 7.5":     {"brute", 4, 7.5, nil, false},
		"never charges": {"crawler", 5, 6, nil, false},
	} {
		f, c, gx, gy := chargeSetup(t, tc.typ, tc.d)
		c.setLevel(tc.lvl)
		if tc.mutate != nil {
			tc.mutate(c)
		}
		if tc.walls {
			f.wallAt(int(gx), int(gy))
		}
		f.r.eliteTactics(c, tc.typ, f.p, f.p.X, f.p.Y)
		want := name == "in range" || name == "brute 3.5"
		if got := c.ChargeTk > 0; got != want {
			t.Errorf("%s: charging = %v, want %v", name, got, want)
		}
		if (f.lastOfType("ccharge") != nil) != want {
			t.Errorf("%s: ccharge frame mismatch", name)
		}
	}
}

func TestEliteStruckCreatureLosesItsCharge(t *testing.T) {
	f, c, _, _ := chargeSetup(t, "husk_wolf", 6)
	f.r.eliteTactics(c, "husk_wolf", f.p, f.p.X, f.p.Y)
	f.r.eliteOnStruck(c, f.p)
	if c.ChargeTk != 0 || c.ChargeCd != 40 {
		t.Fatalf("ChargeTk %d cooldown %d after being struck", c.ChargeTk, c.ChargeCd)
	}
}

// --- alpha -----------------------------------------------------------------

// pack lays out an alpha crawler at the centre, its prey (the arena keeper) 10
// tiles west, a follower beside it with a second keeper standing right next to
// the follower, and a far follower out of reach.
func pack(t *testing.T) (f *fixture, alpha, follower, outlier *Creature, near2 *Player) {
	t.Helper()
	f, gx, gy := eliteArena(t)
	f.p.X = gx - 10
	alpha = f.leveled("crawler", 5, gx, gy)
	alpha.AggroID = f.p.S.ID
	follower = f.leveled("crawler", 2, gx+2, gy)
	outlier = f.leveled("crawler", 2, gx+14, gy)
	near2 = f.addKeeper(gx+3, gy)
	return
}

func TestEliteFollowerAdoptsTheAlphasPrey(t *testing.T) {
	f, alpha, follower, outlier, near2 := pack(t)
	if got := f.r.elitePrey(follower, "crawler", near2); got != f.p {
		t.Fatalf("follower picked %v, want its alpha's prey over the nearer keeper", got.S.ID)
	}
	if got := f.r.elitePrey(outlier, "crawler", near2); got != near2 {
		t.Fatal("a creature out of the alpha's reach followed it")
	}
	f.p.X = follower.X - 27 // the alpha's prey is beyond 26 tiles of the follower
	if got := f.r.elitePrey(follower, "crawler", near2); got != near2 {
		t.Fatal("followed the alpha's prey from beyond 26 tiles")
	}
	f.p.X = follower.X - 10
	alpha.AggroID = ""
	if got := f.r.elitePrey(follower, "crawler", near2); got != near2 {
		t.Fatal("followed an alpha that hunts nobody")
	}
	wolf := f.leveled("husk_wolf", 2, follower.X, follower.Y)
	alpha.AggroID = f.p.S.ID
	if got := f.r.elitePrey(wolf, "husk_wolf", near2); got != near2 {
		t.Fatal("a wolf followed a crawler alpha")
	}
}

func TestEliteBruteAlphaLeadsCrawlersOnly(t *testing.T) {
	f, alpha, follower, _, _ := pack(t)
	f.r.delCreature(alpha.ID)
	brute := f.leveled("brute", 5, alpha.X, alpha.Y)
	if !follower.answersTo(brute) {
		t.Fatal("a crawler does not follow a brute alpha")
	}
	if f.leveled("stalker", 2, alpha.X, alpha.Y).answersTo(brute) || brute.answersTo(brute) {
		t.Fatal("only crawlers answer to a brute")
	}
}

func TestEliteFollowersRunHarderNearTheirAlpha(t *testing.T) {
	f, _, follower, outlier, _ := pack(t)
	if f.r.eliteSpeed(follower, "crawler") != 1.1 {
		t.Fatal("no speed-up near the alpha")
	}
	if f.r.eliteSpeed(outlier, "crawler") != 1 {
		t.Fatal("sped up far from the alpha")
	}
}

func TestEliteAlphaFirstLockOnRalliesItsOwn(t *testing.T) {
	f, gx, gy := eliteArena(t)
	alpha := f.leveled("stalker", 5, gx, gy) // no pack call in its kit, still rallies
	mate := f.spawnAt("stalker", gx+5, gy, false)
	other := f.spawnAt("crawler", gx+5, gy+1, false)
	f.reset()
	f.r.creatureSawPrey(alpha, f.p)
	if cr := f.lastOfType("crally"); cr == nil || cr["id"] != alpha.ID {
		t.Fatalf("crally = %v", cr)
	}
	if mate.Memory != aiMemoryTicks || other.Memory != 0 {
		t.Fatalf("stalker memory %d crawler memory %d: want only the follower roused", mate.Memory, other.Memory)
	}
}

func TestEliteStruckAlphaRalliesOnAThrottle(t *testing.T) {
	f, alpha, follower, outlier, _ := pack(t)
	f.reset()
	f.r.eliteOnStruck(alpha, f.p)
	if cr := f.frames("crally"); len(cr) != 1 || cr[0]["id"] != alpha.ID {
		t.Fatalf("crally frames = %v", cr)
	}
	if follower.Enraged != 60 || outlier.Enraged != 0 {
		t.Fatalf("enraged %d near, %d far; want 60 and 0", follower.Enraged, outlier.Enraged)
	}
	f.r.eliteOnStruck(alpha, f.p)
	if len(f.frames("crally")) != 1 {
		t.Fatal("rallied twice inside the throttle")
	}
	for i := 0; i < eliteRallyTk; i++ {
		f.r.eliteTick(alpha, "crawler")
	}
	f.r.eliteOnStruck(alpha, f.p)
	if len(f.frames("crally")) != 2 {
		t.Fatal("never rallied again")
	}
	f.reset()
	f.r.eliteOnStruck(follower, f.p)
	if f.lastOfType("crally") != nil {
		t.Fatal("a plain crawler rallied")
	}
}

func TestEliteFallenAlphaPanicsItsFollowers(t *testing.T) {
	f, alpha, follower, outlier, _ := pack(t)
	second := f.leveled("crawler", 1, alpha.X-2, alpha.Y)
	wolf := f.leveled("husk_wolf", 2, alpha.X, alpha.Y+1) // another family
	f.p.X, f.p.Y = alpha.X-1, alpha.Y                     // the killer stands next to the alpha...
	follower.X, follower.Y = f.p.X-0.5, f.p.Y             // ...and a follower right beside it
	f.reset()
	f.r.hitCreature(f.p, alpha, 100, float64(1), f.now)

	cp := f.lastOfType("cpanic")
	if cp == nil {
		t.Fatal("no cpanic frame")
	}
	ids, _ := cp["ids"].([]any)
	if want := []string{follower.ID, second.ID}; len(ids) != 2 || ids[0] != want[0] || ids[1] != want[1] {
		t.Fatalf("cpanic ids = %v, want %v in creOrder order", ids, want)
	}
	if follower.PanicTk != elitePanicTk || outlier.PanicTk != 0 || wolf.PanicTk != 0 {
		t.Fatalf("panic ticks %d / %d / %d", follower.PanicTk, outlier.PanicTk, wolf.PanicTk)
	}

	// they run from the killer, one tick at a time, and never bite
	hp := f.p.HP
	x0, y0 := follower.X, follower.Y
	f.r.eliteOverride(follower, "crawler", 0.44, f.now)
	if got := math.Hypot(follower.X-x0, follower.Y-y0); !near(got, 0.44*1.1) || follower.X >= x0 {
		t.Fatalf("panicked step %.3f toward x%+.3f, want 0.484 away from the killer", got, follower.X-x0)
	}
	for i := 0; i < elitePanicTk-1; i++ {
		f.run(follower, 1)
	}
	if follower.PanicTk != 0 || f.p.HP != hp {
		t.Fatalf("panic left %d ticks, keeper hp %d -> %d", follower.PanicTk, hp, f.p.HP)
	}
	if d := math.Hypot(follower.X-f.p.X, follower.Y-f.p.Y); d < 4 {
		t.Fatalf("the follower fled only to %.1f tiles from the killer", d)
	}
}

func TestEliteFallenPlainCreatureCausesNoPanic(t *testing.T) {
	f, gx, gy := eliteArena(t)
	dead := f.leveled("crawler", 2, gx+1, gy)
	f.leveled("crawler", 2, gx+3, gy)
	f.reset()
	f.r.hitCreature(f.p, dead, 100, float64(1), f.now)
	if f.lastOfType("cpanic") != nil {
		t.Fatal("a non-alpha's death caused a panic")
	}
}

func TestElitePanickedFollowerStepsOnlyOntoOpenGround(t *testing.T) {
	f, gx, gy := eliteArena(t)
	c := f.leveled("crawler", 1, gx+0.4, gy+0.45)
	c.PanicTk, c.PanicX, c.PanicY = 5, gx-2, gy
	f.wallAt(int(gx)+1, int(gy)) // straight away from the killer is walled
	f.r.eliteOverride(c, "crawler", 0.44, f.now)
	if c.X == gx+0.4 && c.Y == gy+0.45 {
		t.Fatal("the panicked creature froze instead of slipping round the wall")
	}
	if f.r.structures[ti(c.X, c.Y)] != nil {
		t.Fatal("the panicked creature stepped into a wall")
	}
}
