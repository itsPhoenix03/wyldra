package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// The attack layer (creature_attack.go): telegraphed melee, aimed shots.

// duel stands a keeper (100 hp) in the middle of an open patch with a creature
// of typ dist tiles to its east.
func duel(t *testing.T, typ string, dist float64) (*fixture, *Creature) {
	t.Helper()
	f := newFixture(t)
	f.p.HP = 100
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.stand(gx, gy, 0)
	return f, f.spawnAt(typ, gx+dist, gy, false)
}

// step runs one creature tick without sampling keeper velocities, so a test
// can set VX/VY by hand.
func (f *fixture) step(c *Creature) {
	f.r.tickN++
	f.r.stepCreature(c, 1, f.now)
}

func isNum(v any) bool { _, ok := v.(float64); return ok }

// A crawler commits to a windup (catk), stands still through it, then strikes.
func TestAttackWindupThenStrike(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	hp := f.p.HP
	f.p.LastDamageAt = 0
	f.reset()
	f.run(c, 1)
	m := f.lastOfType("catk")
	if m == nil || m["id"] != c.ID || m["tk"] != float64(2) || !isNum(m["ang"]) {
		t.Fatalf("catk = %v, want {id, numeric ang, tk 2}", m)
	}
	x, y := c.X, c.Y
	f.run(c, 1)
	if f.p.HP != hp {
		t.Fatalf("crawler hurt a keeper before its windup ran out: hp %d", f.p.HP)
	}
	if c.X != x || c.Y != y {
		t.Fatal("crawler moved during its windup")
	}
	f.run(c, 1)
	if f.p.HP != hp-creTypes["crawler"].dmg {
		t.Fatalf("strike left hp %d, want %d", f.p.HP, hp-1)
	}
	if f.p.LastDamageAt != f.now {
		t.Fatal("the strike did not stamp LastDamageAt")
	}
	if h := f.lastOfType("hp"); h == nil || !isNum(h["ang"]) {
		t.Fatalf("hp frame has no numeric ang: %v", h)
	}
}

// The windup does not track: a keeper who steps out of reach during it takes
// nothing, and the creature's heading stays where it was committed.
func TestAttackKeeperWhoStepsOutIsMissed(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	hp := f.p.HP
	f.run(c, 1)
	ang := c.AtkAng
	f.p.Y -= 2
	f.run(c, 2)
	if c.AtkState != atkRecover {
		t.Fatalf("the strike did not resolve: state %d", c.AtkState)
	}
	if f.p.HP != hp {
		t.Fatalf("a keeper who stepped aside took damage: hp %d", f.p.HP)
	}
	if c.AtkAng != ang {
		t.Fatal("the windup heading tracked the keeper")
	}
}

// A keeper who goes underground mid-windup is not struck.
func TestAttackUndergroundKeeperUntouched(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	hp := f.p.HP
	f.run(c, 1)
	f.p.Z = 1
	f.run(c, 2)
	if c.AtkState != atkRecover || f.p.HP != hp {
		t.Fatalf("underground keeper: state %d hp %d (want recover, %d)", c.AtkState, f.p.HP, hp)
	}
}

// A keeper's hit cancels a light creature's windup, and a lancer's aim.
func TestAttackHitCancelsLightWindup(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	hp := f.p.HP
	f.run(c, 1)
	f.r.creatureSurvived(f.p, c, 0, nil, f.now)
	if c.AtkState != atkReady {
		t.Fatalf("a struck crawler kept its windup: state %d", c.AtkState)
	}
	f.run(c, 3) // stunned
	if f.p.HP != hp {
		t.Fatalf("a struck crawler still landed its blow: hp %d", f.p.HP)
	}

	f, l := duel(t, "blight_lancer", 5)
	f.r.lancerBeam(l, f.now)
	f.r.creatureSurvived(f.p, l, 0, nil, f.now)
	f.reset()
	f.run(l, 4)
	if l.AtkState != atkReady || f.lastOfType("shot") != nil {
		t.Fatal("a struck lancer still fired its beam")
	}
}

// Heavies have super armor: a hit only pauses the slam (the stun), it does not
// cancel it.
func TestAttackHeavyWindupSurvivesAHit(t *testing.T) {
	f, b := duel(t, "brute", 1.0)
	hp := f.p.HP
	f.run(b, 1)
	f.r.creatureSurvived(f.p, b, 0, nil, f.now)
	if b.AtkState != atkWindup {
		t.Fatalf("a struck brute lost its windup: state %d", b.AtkState)
	}
	f.run(b, 3) // stunned: the windup is paused, not spent
	for n := 0; n < 16 && f.p.HP == hp; n++ {
		f.run(b, 1)
	}
	if f.p.HP != hp-2 {
		t.Fatalf("the brute's slam did not land after it was struck: hp %d", f.p.HP)
	}
}

// A brute's first windup of an engagement is 8 ticks, the follow-ups 5; each
// slam hits for 2 and each telegraph (ctel) carries ang and tk.
func TestAttackHeavyWindups(t *testing.T) {
	f, b := duel(t, "brute", 1.0)
	hp := f.p.HP
	f.reset()
	f.run(b, 1)
	m := f.lastOfType("ctel")
	if m == nil || m["id"] != b.ID || m["tk"] != float64(8) || !isNum(m["ang"]) {
		t.Fatalf("first ctel = %v, want {id, numeric ang, tk 8}", m)
	}
	f.run(b, 7)
	if f.p.HP != hp {
		t.Fatalf("slammed during the telegraph: hp %d", f.p.HP)
	}
	f.run(b, 1)
	if f.p.HP != hp-2 {
		t.Fatalf("brute slam left hp %d, want %d", f.p.HP, hp-2)
	}

	f.reset()
	for n := 0; n < 12 && f.lastOfType("ctel") == nil; n++ {
		f.run(b, 1)
	}
	m = f.lastOfType("ctel")
	if m == nil || m["tk"] != float64(5) || !isNum(m["ang"]) {
		t.Fatalf("follow-up ctel = %v, want tk 5", m)
	}
	f.run(b, 4)
	if f.p.HP != hp-2 {
		t.Fatalf("second slam landed early: hp %d", f.p.HP)
	}
	f.run(b, 1)
	if f.p.HP != hp-4 {
		t.Fatalf("second slam left hp %d, want %d", f.p.HP, hp-4)
	}

	// everyone leaves: the next engagement opens long again
	f.p.X += 10
	f.run(b, 20)
	if b.WindupTriggered {
		t.Fatal("the brute stayed engaged with nobody in reach")
	}
}

// The frost wraith (which never reaches meleeTrigger) winds up from its dart,
// and its strike carries the slow.
func TestAttackFrostWraithSlow(t *testing.T) {
	f, c := duel(t, "frost_wraith", 1.0)
	hp := f.p.HP
	f.reset()
	f.run(c, 1)
	if m := f.lastOfType("catk"); m == nil || m["tk"] != float64(1) {
		t.Fatalf("wraith catk = %v, want tk 1", m)
	}
	if f.p.HP != hp {
		t.Fatal("wraith hurt a keeper before its windup ran out")
	}
	f.run(c, 1)
	if f.p.HP != hp-1 {
		t.Fatalf("wraith strike left hp %d, want %d", f.p.HP, hp-1)
	}
	if m := f.lastOfType("slow"); m == nil || m["ticks"] != float64(30) {
		t.Fatalf("wraith strike slow = %v, want ticks 30", m)
	}
}

// A lead-shot wraith (level 4) aims its windup where the keeper is going.
func TestAttackLeadShotOffsetsMeleeAim(t *testing.T) {
	f, c := duel(t, "frost_wraith", 1.0)
	c.setLevel(4)
	f.p.VX, f.p.VY = 0.4, -0.3
	f.reset()
	f.step(c)
	m := f.lastOfType("catk")
	if m == nil {
		t.Fatal("no catk")
	}
	want := math.Atan2(f.p.Y-0.3*2-c.Y, f.p.X+0.4*2-c.X)
	direct := math.Atan2(f.p.Y-c.Y, f.p.X-c.X)
	if got := m["ang"].(float64); math.Abs(got-want) > 1e-9 || math.Abs(got-direct) < 0.05 {
		t.Fatalf("catk ang %.3f, want the led aim %.3f (direct %.3f)", got, want, direct)
	}
}

// A wisp never attacks; a panicking creature may not begin an attack, and
// drops one it had begun.
func TestAttackWispNeverStrikes(t *testing.T) {
	f, c := duel(t, "wisp", 0.5)
	hp, gx, gy := f.p.HP, f.p.X, f.p.Y
	c.DX, c.DY = 0, 0
	f.reset()
	for i := 0; i < 20; i++ {
		c.X, c.Y = gx+0.5, gy
		f.run(c, 1)
	}
	if f.p.HP != hp || c.AtkState != atkReady || f.lastOfType("catk") != nil {
		t.Fatalf("a wisp attacked: hp %d state %d", f.p.HP, c.AtkState)
	}
}

func TestAttackPanicBlocksAttack(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	hp := f.p.HP
	c.PanicTk = 99
	f.reset()
	f.run(c, 4)
	if c.AtkState != atkReady || f.p.HP != hp || f.lastOfType("catk") != nil {
		t.Fatal("a panicking crawler began an attack")
	}
	c.PanicTk = 0
	c.X, c.Y = f.p.X+1, f.p.Y // it fled while panicking
	f.run(c, 1)
	if c.AtkState != atkWindup {
		t.Fatal("the crawler did not attack once calm")
	}
	c.PanicTk = 1
	f.run(c, 1)
	if c.AtkState != atkReady || f.p.HP != hp {
		t.Fatalf("panic did not cancel the windup: state %d hp %d", c.AtkState, f.p.HP)
	}
}

// The lunge stops short of a tile the creature cannot enter.
func TestAttackLungeStopsAtWall(t *testing.T) {
	f, c := duel(t, "crawler", 1.0)
	tx, ty := int(c.X), int(c.Y)
	x0 := float64(tx) + 0.05
	lunge := func() float64 {
		c.X, c.Y = x0, float64(ty)+0.5
		c.AtkState, c.AtkTk, c.AtkAng, c.AtkCd = atkWindup, 1, math.Pi, 0
		f.run(c, 1)
		return x0 - c.X
	}
	if d := lunge(); math.Abs(d-0.6) > 1e-6 {
		t.Fatalf("open lunge covered %.3f, want 0.6", d)
	}
	f.wallAt(tx-1, ty)
	if d := lunge(); math.Abs(d) > 1e-9 {
		t.Fatalf("lunge crossed into a wall: covered %.3f", d)
	}
}

// Lance: a keeper who moves off the line is missed; one who stays takes 2.
func TestAttackLanceAimedThenFired(t *testing.T) {
	f, c := duel(t, "blight_lancer", 5)
	hp := f.p.HP
	f.p.LastDamageAt = 0
	f.reset()
	f.r.lancerBeam(c, f.now)
	m := f.lastOfType("caim")
	if m == nil || m["id"] != c.ID || m["kind"] != "lance" || m["tk"] != float64(3) ||
		m["tx"] != toFixed(f.p.X, 1) || m["ty"] != toFixed(f.p.Y, 1) {
		t.Fatalf("caim = %v, want a lance aimed at the keeper, tk 3", m)
	}
	x, y := c.X, c.Y
	f.run(c, 2)
	if f.p.HP != hp || f.lastOfType("shot") != nil || c.X != x || c.Y != y {
		t.Fatal("the lancer fired early or moved while aiming")
	}
	f.run(c, 1)
	if f.p.HP != hp-2 {
		t.Fatalf("beam left hp %d, want %d", f.p.HP, hp-2)
	}
	if f.p.LastDamageAt != f.now {
		t.Fatal("the beam did not stamp LastDamageAt")
	}
	if h := f.lastOfType("hp"); h == nil || !isNum(h["ang"]) {
		t.Fatalf("beam hp frame has no numeric ang: %v", h)
	}
	if s := f.lastOfType("shot"); s == nil || s["kind"] != "lance" || s["tx"] != m["tx"] || s["ty"] != m["ty"] {
		t.Fatalf("shot = %v, want a lance ending at the aim point", s)
	}
}

func TestAttackLanceMissesKeeperWhoMoved(t *testing.T) {
	f, c := duel(t, "blight_lancer", 5)
	hp := f.p.HP
	f.r.lancerBeam(c, f.now)
	f.p.Y += 3
	f.reset()
	f.run(c, 3)
	if f.p.HP != hp {
		t.Fatalf("a keeper off the line took the beam: hp %d", f.p.HP)
	}
	if f.lastOfType("shot") == nil {
		t.Fatal("the beam was never fired")
	}
}

// A wall on the line eats the beam: it is destroyed and the keeper behind it is
// untouched; a keeper in front of it is still hit.
func TestAttackLanceEatenByWall(t *testing.T) {
	f, c := duel(t, "blight_lancer", 5)
	hp := f.p.HP
	wx, wy := int(f.p.X)+2, int(f.p.Y)
	f.wallAt(wx, wy)
	f.r.lancerBeam(c, f.now)
	f.reset()
	f.run(c, 3)
	if f.p.HP != hp {
		t.Fatalf("the keeper behind the wall took the beam: hp %d", f.p.HP)
	}
	if f.r.structures[wy*world.SIZE+wx] != nil {
		t.Fatal("the wall survived the beam")
	}
	if s := f.lastOfType("shot"); s == nil || s["tx"] != float64(wx) || s["ty"] != float64(wy) {
		t.Fatalf("shot = %v, want it to end at the wall tile %d,%d", s, wx, wy)
	}

	f, c = duel(t, "blight_lancer", 5)
	hp = f.p.HP
	f.wallAt(int(f.p.X)-2, int(f.p.Y))
	f.r.lancerBeam(c, f.now)
	f.run(c, 3)
	if f.p.HP != hp-2 {
		t.Fatalf("a keeper in front of the wall left hp %d, want %d", f.p.HP, hp-2)
	}
}

// Bolt: aimed at a keeper in water the brute cannot enter; fires at the aim.
func TestAttackBoltHitsOrMissesByAim(t *testing.T) {
	for _, stays := range []bool{true, false} {
		f, cx, gy := channelFixture(t)
		f.p.X, f.p.Y = float64(cx)+1.5, float64(gy)+0.5
		hp := f.p.HP
		c := f.spawnAt("brute", float64(cx)-3.5, float64(gy)+0.5, true)
		f.reset()
		f.r.bruteBolt(c, f.now)
		m := f.lastOfType("caim")
		if m == nil || m["id"] != c.ID || m["kind"] != "bolt" || m["tk"] != float64(2) ||
			m["tx"] != toFixed(f.p.X, 1) || m["ty"] != toFixed(f.p.Y, 1) {
			t.Fatalf("caim = %v, want a bolt aimed at the keeper, tk 2", m)
		}
		f.run(c, 1)
		if f.p.HP != hp {
			t.Fatal("the bolt landed before its aim ran out")
		}
		if !stays {
			f.p.X = float64(cx) + 8.5 // onto the far shore
		}
		f.run(c, 1)
		s := f.lastOfType("shot")
		if s == nil || s["tx"] != m["tx"] || s["ty"] != m["ty"] {
			t.Fatalf("shot = %v, want it at the aim point", s)
		}
		if stays {
			if f.p.HP != hp-1 {
				t.Fatalf("bolt left hp %d, want %d", f.p.HP, hp-1)
			}
			if h := f.lastOfType("hp"); h == nil || !isNum(h["ang"]) {
				t.Fatalf("bolt hp frame has no numeric ang: %v", h)
			}
		} else if f.p.HP != hp {
			t.Fatalf("a keeper who left the aim point took the bolt: hp %d", f.p.HP)
		}
	}
}

// With lead shot (level 4) the aim point sits ahead of a moving keeper; below
// it, on the keeper.
func TestAttackLeadShotOffsetsRangedAim(t *testing.T) {
	for _, lvl := range []int{1, 4} {
		f, c := duel(t, "blight_lancer", 5)
		c.setLevel(lvl)
		f.p.VX, f.p.VY = 0.4, -0.2
		f.reset()
		f.r.lancerBeam(c, f.now)
		wx, wy := f.p.X, f.p.Y
		if lvl == 4 {
			wx, wy = wx+0.4*4, wy-0.2*4 // the lance aims 3 ticks, so it leads 4
		}
		if m := f.lastOfType("caim"); m == nil || m["tx"] != toFixed(wx, 1) || m["ty"] != toFixed(wy, 1) {
			t.Fatalf("lancer level %d caim = %v, want aim %.1f,%.1f", lvl, m, wx, wy)
		}

		fc, cx, gy := channelFixture(t)
		fc.p.X, fc.p.Y = float64(cx)+1.5, float64(gy)+0.5
		fc.p.VX, fc.p.VY = 0.3, 0.1
		b := fc.spawnAt("brute", float64(cx)-3.5, float64(gy)+0.5, true)
		b.setLevel(lvl)
		fc.reset()
		fc.r.bruteBolt(b, fc.now)
		wx, wy = fc.p.X, fc.p.Y
		if lvl == 4 {
			wx, wy = wx+0.3*3, wy+0.1*3 // the bolt aims 2 ticks, so it leads 3
		}
		if m := fc.lastOfType("caim"); m == nil || m["tx"] != toFixed(wx, 1) || m["ty"] != toFixed(wy, 1) {
			t.Fatalf("brute level %d caim = %v, want aim %.1f,%.1f", lvl, m, wx, wy)
		}
	}
}

// A stalker that lands a blow falls back before coming in again.
func TestStalkerHitAndRun(t *testing.T) {
	f := newFixture(t)
	f.p.HP = 1 << 20
	gx, gy := landPatch(t, f.r, world.TGrass, 7)
	f.p.X, f.p.Y = gx, gy
	c := f.spawnAt("stalker", gx+0.5, gy, true)
	for n := 0; n < 6 && c.Retreat == 0; n++ {
		f.run(c, 1) // windup, then the strike
	}
	if c.Retreat == 0 {
		t.Fatal("stalker did not fall back after striking")
	}
	f.run(c, 1) // recovery
	d0 := math.Hypot(c.X-f.p.X, c.Y-f.p.Y)
	f.run(c, 3)
	if d1 := math.Hypot(c.X-f.p.X, c.Y-f.p.Y); d1 <= d0 {
		t.Fatalf("retreating stalker closed in: %.2f -> %.2f", d0, d1)
	}
}
