package room

import (
	"math"
	"strings"
	"testing"
)

// woodsVillage is the Woods village and its fire, with the player parked far
// away so nobody is distracted by them.
func woodsVillage(t *testing.T, f *fixture) *village {
	t.Helper()
	v := f.r.villageOf("woods")
	if v == nil || len(v.members) < 4 {
		t.Fatal("fixture: no Woods village")
	}
	f.setDay()
	f.p.X, f.p.Y = v.fx+60, v.fy
	return v
}

// A creature at the village's edge sends the WHOLE village in — not only those
// within folkThreatR of it — and they stay in until it is gone; all but its
// guards, who stand between it and the huts facing it (folk.go folkGuard).
func TestVillageAlarmSendsEveryoneIn(t *testing.T) {
	f := newFixture(t)
	v := woodsVillage(t, f)
	c := f.spawnAt("crawler", v.fx+folkAlarmR-1, v.fy, false)
	far := 0
	for _, fk := range v.members {
		if math.Hypot(fk.X-c.X, fk.Y-c.Y) > folkThreatR {
			far++
		}
	}
	if far == 0 {
		t.Fatal("fixture: every villager is already within folkThreatR of the creature")
	}
	for k := 0; k < 200; k++ {
		f.r.folkTick()
	}
	guards := 0
	for _, fk := range v.members {
		if fk.state == fsGuard {
			guards++
			continue
		}
		if fk.state != fsHidden {
			t.Fatalf("%s (%s) is %q, not hidden, with a crawler %.1f tiles from the fire",
				fk.ID, fk.Kind, fk.Act, math.Hypot(c.X-v.fx, c.Y-v.fy))
		}
	}
	if guards == 0 || guards > folkMaxGuards {
		t.Fatalf("%d guards stood out against the crawler, want 1..%d", guards, folkMaxGuards)
	}
	// gone: they come back out once the all-clear has held
	f.r.delCreature(c.ID)
	for k := 0; k < folkClearTk+5; k++ {
		f.r.folkTick()
	}
	out := 0
	for _, fk := range v.members {
		if fk.state != fsHidden {
			out++
		}
	}
	if out == 0 {
		t.Fatal("nobody came back out after the creature was gone")
	}
}

// Harm to an island turns its folk: wary scolds, hostile sends villagers in
// while watchers stand and scold; each change is told to the keeper; it fades.
func TestHarmTurnsTheVillage(t *testing.T) {
	f := newFixture(t)
	v := woodsVillage(t, f)
	f.reset()

	// fell enough of their trees to make them wary
	for f.r.standingWith(f.p, "woods") < standWary {
		f.r.addHarm(f.p, v.fx+30, v.fy, harmTree)
	}
	if m := f.lastOfType("msg"); m == nil || !strings.Contains(m["s"].(string), "warily") {
		t.Fatalf("no wary notice: %v", m)
	}
	var fk *Folk
	for _, m := range v.members {
		if m.state != fsHidden {
			fk = m
			break
		}
	}
	f.p.X, f.p.Y = fk.X+2, fk.Y
	f.r.folkTick()
	if fk.Act != "scold" {
		t.Fatalf("a wary villager greeted the keeper: act %q", fk.Act)
	}
	if want := math.Atan2(f.p.Y-fk.Y, f.p.X-fk.X); math.Abs(fk.Face-want) > 1e-9 {
		t.Fatalf("scolding villager faces %.2f, want the keeper at %.2f", fk.Face, want)
	}

	// hostile: walk into the village; villagers run (shun), watchers scold
	f.p.X, f.p.Y = v.fx+60, v.fy
	f.reset()
	for f.r.standingWith(f.p, "woods") < standHostile {
		f.r.addHarm(f.p, v.fx+30, v.fy, harmAnimal)
	}
	if m := f.lastOfType("msg"); m == nil || !strings.Contains(m["s"].(string), "want you gone") {
		t.Fatalf("no hostile notice: %v", m)
	}
	var villager, watcher *Folk
	for _, m := range v.members {
		if m.state == fsHidden {
			continue
		}
		if m.watcher && watcher == nil {
			watcher = m
		} else if !m.watcher && villager == nil {
			villager = m
		}
	}
	if villager == nil || watcher == nil {
		t.Fatal("fixture: need a villager and a watcher up and about")
	}
	f.p.X, f.p.Y = villager.X+2, villager.Y
	f.r.folkTick()
	if villager.Act != "shun" && villager.state != fsHidden {
		t.Fatalf("a villager stayed out with a hostile keeper beside them: act %q", villager.Act)
	}
	f.p.X, f.p.Y = watcher.X+2, watcher.Y
	f.r.folkTick()
	if watcher.Act != "scold" {
		t.Fatalf("a watcher did not stand and scold a hostile keeper: act %q", watcher.Act)
	}

	// it fades, and the keeper hears when it does
	f.reset()
	for k := 0; k < int(harmMax/harmFade)+1; k++ {
		f.r.tickN = int64((k + 1) * harmFadeTk)
		f.r.harmFadeTick()
	}
	if f.r.standingWith(f.p, "woods") != standWelcome {
		t.Fatal("harm never faded")
	}
	if m := f.lastOfType("msg"); m == nil || !strings.Contains(m["s"].(string), "open to you again") {
		t.Fatalf("no welcome-back notice: %v", m)
	}
}

// Harm on one island does not sour another, and Core / open sea count for nobody.
func TestHarmIsPerIsland(t *testing.T) {
	f := newFixture(t)
	v := woodsVillage(t, f)
	for k := 0; k < 20; k++ {
		f.r.addHarm(f.p, v.fx, v.fy, harmBlight)
		f.r.addHarm(f.p, 640, 640, harmBlight) // the Core
	}
	if f.r.standingWith(f.p, "woods") != standHostile {
		t.Fatal("woods harm not recorded")
	}
	if f.r.standingWith(f.p, "dunes") != standWelcome || len(f.r.harm[f.p.S.UserID]) != 1 {
		t.Fatalf("harm leaked past the Woods: %v", f.r.harm[f.p.S.UserID])
	}
}
