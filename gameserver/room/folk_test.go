package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// Island camps and their folk (folk.go).

func TestCampsPlacedAndBlocked(t *testing.T) {
	f := newFixture(t)
	// a village on every island: the Woods largest, the Dunes a little smaller,
	// three huts in the Marsh, one beside the Spire's medic
	huts := map[string]int{}
	fire := map[string][2]int{}
	hutAt := map[int]string{}
	for _, c := range f.r.camps {
		huts[c.IslandID]++
		if at, ok := fire[c.IslandID]; ok && at != [2]int{c.FireX, c.FireY} {
			t.Fatalf("%s: huts of one village round different fires", c.ID)
		}
		fire[c.IslandID] = [2]int{c.FireX, c.FireY}
		for _, i := range world.CampBlockTiles(c) {
			if !f.r.medicTiles[i] {
				t.Fatalf("%s: tile %d not blocked", c.ID, i)
			}
		}
		door := (c.HutY+1)*world.SIZE + c.HutX + 1
		if f.r.medicTiles[door] {
			t.Fatalf("%s: the hut door is blocked", c.ID)
		}
		hut := c.HutY*world.SIZE + c.HutX
		if other, ok := hutAt[hut]; ok {
			t.Fatalf("%s and %s share a hut tile", c.ID, other)
		}
		hutAt[hut] = c.ID
	}
	// no two huts' art overlaps on screen (96×88 px; a tile step is 32 px across, 16 down)
	for i, a := range f.r.camps {
		for _, b := range f.r.camps[i+1:] {
			dx, ds := (a.HutX-a.HutY)-(b.HutX-b.HutY), (a.HutX+a.HutY)-(b.HutX+b.HutY)
			if dx < 0 {
				dx = -dx
			}
			if ds < 0 {
				ds = -ds
			}
			if dx < 3 && ds < 6 {
				t.Fatalf("%s and %s overlap on screen", a.ID, b.ID)
			}
		}
	}
	for id, n := range map[string]int{"woods": 6, "dunes": 5, "marsh": 3, "spire": 1} {
		if huts[id] != n {
			t.Fatalf("%s: want %d huts, got %d", id, n, huts[id])
		}
	}
	// population: Woods > Dunes > Marsh > Spire; an elder in every village;
	// tribal folk only in the Marsh, Ashmark hunters only in the Dunes
	pop, elders := map[string]int{}, map[string]int{}
	for _, fk := range f.r.folk {
		isle := f.r.camps[fk.Camp].IslandID
		pop[isle]++
		switch fk.Kind {
		case "elder_woods", "shaman", "elder_yvenne", "elder_spire":
			elders[isle]++
		case "tribal":
			if isle != "marsh" {
				t.Fatalf("tribal folk living on %s", isle)
			}
		case "ashmark_hunter":
			if isle != "dunes" {
				t.Fatalf("Ashmark hunter living on %s", isle)
			}
		}
	}
	if !(pop["woods"] > pop["dunes"] && pop["dunes"] > pop["marsh"] && pop["marsh"] > pop["spire"] && pop["spire"] > 0) {
		t.Fatalf("village sizes out of order: %v", pop)
	}
	for _, id := range []string{"woods", "dunes", "marsh", "spire"} {
		if elders[id] != 1 {
			t.Fatalf("%s: want one elder, got %d", id, elders[id])
		}
	}
	// deterministic: a second room on the same world places them identically
	g := newFixture(t)
	for i := range f.r.camps {
		a, b := f.r.camps[i], g.r.camps[i]
		if a.HutX != b.HutX || a.HutY != b.HutY || a.FireX != b.FireX || a.FireY != b.FireY {
			t.Fatalf("camp %s moved between rooms", a.ID)
		}
	}
}

// A whole day in the villages: everyone gets up and turns in at their own hour,
// works at their own trade in the places it belongs, eats together at midday
// and chats with the neighbours.
func TestFolkDay(t *testing.T) {
	f := newFixture(t)
	f.p.X, f.p.Y = 0, 0 // nobody to talk to
	step := (TickMS / 1000.0) / DayLengthSec
	nearHut := func(x, y float64) bool {
		for _, c := range f.r.camps {
			if math.Hypot(x-(float64(c.HutX)+0.5), y-(float64(c.HutY)+0.5)) < 1.6 {
				return true
			}
		}
		return false
	}
	nearNode := func(x, y float64, kind uint8) bool {
		for dy := -1; dy <= 1; dy++ {
			for dx := -1; dx <= 1; dx++ {
				if k, ok := f.r.world.Nodes[ti(x+float64(dx), y+float64(dy))]; ok && k == kind {
					return true
				}
			}
		}
		return false
	}
	atShore := func(x, y float64) bool {
		for _, d := range [4][2]float64{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
			if f.r.tileAt(ti(x+d[0], y+d[1])) == world.TWater {
				return true
			}
		}
		return false
	}
	seen := map[string]bool{}
	mark := func(what string) { seen[what] = true }
	var upAt10, upAt14, hiddenAtDusk, outAtDusk, mealSit, mealUp int
	// Start in the night, as a real dawn does: everyone walks home from their seat
	// at the fire and turns in (folkGoingTk ticks at most) before the first wakes.
	// Starting at dawn instead leaves the late risers still walking home at 0.10
	// whenever the huts stand on a full ring round the fire, 4-10 tiles from the seats.
	for f.r.time = 0; f.r.time < NightStart; f.r.time += step {
		f.r.folkTick()
		tm := f.r.time
		for _, fk := range f.r.folk {
			v := fk.vil
			dFire := math.Hypot(fk.X-v.fx, fk.Y-v.fy)
			if fk.state != fsTask || fk.walking {
				if fk.Act == "play" {
					mark("child plays")
				}
				continue
			}
			if !math.IsNaN(fk.Face) {
				mark("faces its work")
			}
			switch {
			case fk.role == roleBuilder && fk.Act == "build" && nearHut(fk.X, fk.Y):
				mark("builder mends a hut")
			case fk.role == roleBuilder && fk.Act == "collect" && nearNode(fk.X, fk.Y, world.NodeTree):
				mark("builder fetches wood")
			case fk.role == roleForager && fk.Act == "craft":
				mark("forager crafts")
			case fk.role == roleForager && fk.Act == "collect" && nearNode(fk.X, fk.Y, world.NodeBush):
				mark("forager picks a bush")
			case fk.role == roleHunter && fk.Act == "watch" && dFire > v.yardR-1:
				mark("hunter watches the edge")
			case fk.role == roleHunter && dFire > 10:
				mark("hunter goes out hunting")
			case fk.role == roleFisher && fk.Act == "collect" && atShore(fk.X, fk.Y):
				mark("tribal fishes the shore")
			case fk.role == roleElder && (fk.Act == "speak" || (fk.Act == "watch" && nearHut(fk.X, fk.Y))):
				mark("elder calls round or speaks")
			}
			if fk.Act == "chat" && fk.partner != nil && fk.partner.partner == fk && fk.partner.Act == "chat" {
				mark("neighbours chat")
			}
		}
		count := func(pred func(*Folk) bool) (n int) {
			for _, fk := range f.r.folk {
				if !fk.watcher && pred(fk) {
					n++
				}
			}
			return
		}
		up := func(fk *Folk) bool { return fk.state != fsHidden }
		switch {
		case math.Abs(tm-0.10) < step/2:
			upAt10 = count(up)
		case math.Abs(tm-0.145) < step/2:
			upAt14 = count(up)
		case math.Abs(tm-0.41) < step/2:
			for _, fk := range f.r.folk {
				if fk.state == fsTask && fk.role != roleHunter && fk.role != roleChild {
					mealUp++
					if fk.Act == "sit" && math.Hypot(fk.X-fk.vil.fx, fk.Y-fk.vil.fy) < 4 {
						mealSit++
					}
				}
			}
		case math.Abs(tm-0.70) < step/2:
			hiddenAtDusk = count(func(fk *Folk) bool { return fk.state == fsHidden || fk.state == fsHome })
			outAtDusk = count(func(fk *Folk) bool { return fk.state == fsTask })
		}
	}
	nonWatchers := 0
	for _, fk := range f.r.folk {
		if !fk.watcher {
			nonWatchers++
		}
	}
	if upAt10 == nonWatchers || upAt14 != nonWatchers {
		t.Fatalf("folk should get up at their own hour: %d up at 0.10, %d of %d at 0.145", upAt10, upAt14, nonWatchers)
	}
	if hiddenAtDusk == 0 || outAtDusk == 0 {
		t.Fatalf("folk should turn in at their own hour: %d in, %d out at 0.70", hiddenAtDusk, outAtDusk)
	}
	if mealSit*2 < mealUp {
		t.Fatalf("the midday meal: only %d of %d sitting at the fire", mealSit, mealUp)
	}
	for _, want := range []string{
		"faces its work", "builder mends a hut", "builder fetches wood", "forager crafts", "forager picks a bush",
		"hunter watches the edge", "hunter goes out hunting", "tribal fishes the shore", "elder calls round or speaks",
		"child plays", "neighbours chat",
	} {
		if !seen[want] {
			t.Errorf("never saw: %s", want)
		}
	}
}

// A creature near a villager sends them into the hut; once it is gone and the
// coast has been clear a while, they come back out.
func TestFolkFleeHideAndReturn(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	f.p.X, f.p.Y = 0, 0 // nobody to talk to
	fk := f.r.folk[0]
	f.r.newCreature(fk.X+3, fk.Y, 1, "crawler", ti(fk.X, fk.Y), true)
	for k := 0; k < 80 && fk.state != fsHidden; k++ {
		f.r.folkTick()
	}
	if fk.state != fsHidden || fk.Act != "hide" {
		t.Fatalf("villager did not take cover: state %d act %s", fk.state, fk.Act)
	}
	if f.r.nearestFolk(fk.X, fk.Y, 50) == fk {
		t.Fatal("hidden villager is still visible to hunters")
	}
	f.r.creatures, f.r.creOrder = map[string]*Creature{}, nil
	for k := 0; k < folkClearTk+2; k++ {
		f.r.folkTick()
	}
	if fk.state == fsHidden {
		t.Fatal("villager stayed hidden after the danger passed")
	}
}

// At night the folk turn in; the watcher keeps the fire.
func TestFolkTurnInAtNight(t *testing.T) {
	f := newFixture(t)
	f.setNight()
	f.p.X, f.p.Y = 0, 0
	for k := 0; k < 300; k++ {
		f.r.folkTick()
	}
	for _, fk := range f.r.folk {
		if fk.watcher && fk.state == fsHidden {
			t.Fatalf("%s (watcher) went to bed", fk.Kind)
		}
		if !fk.watcher && fk.state != fsHidden {
			t.Fatalf("%s still out at night (act %s)", fk.Kind, fk.Act)
		}
	}
}

// A player close by gets their attention: they stop and talk.
func TestFolkStopForPlayer(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	fk := f.r.folk[0]
	f.p.X, f.p.Y, f.p.Z = fk.X+1, fk.Y, 0
	x, y := fk.X, fk.Y
	for k := 0; k < 20; k++ {
		f.r.folkTick()
	}
	if fk.X != x || fk.Y != y {
		t.Fatal("villager walked off mid-conversation")
	}
	if fk.Act != "talk" && fk.Act != "sit" {
		t.Fatalf("villager act %q, want talk", fk.Act)
	}
}

// With no player to hunt, a crawler goes after a villager in the open.
func TestIdleCrawlerHarassesFolk(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	f.p.X, f.p.Y = 0, 0
	fk := f.r.folk[0]
	c := f.r.newCreature(fk.X+8, fk.Y, 1, "crawler", ti(fk.X+8, fk.Y), true)
	d0 := math.Hypot(c.X-fk.X, c.Y-fk.Y)
	for k := 0; k < 4; k++ {
		f.r.tickN++
		f.r.stepCreature(c, 1, f.now)
	}
	if d1 := math.Hypot(c.X-fk.X, c.Y-fk.Y); d1 >= d0-0.5 {
		t.Fatalf("idle crawler ignored the villager: %.2f -> %.2f", d0, d1)
	}
}

// Folk ride the cre frame, as far as the player can be told about them
// (InterestRadius) and no farther; camps ride init.
func TestFolkOnTheWire(t *testing.T) {
	f := newFixture(t)
	f.p.X, f.p.Y = f.r.folk[0].X, f.r.folk[0].Y
	f.reset()
	f.r.broadcastCre()
	m := f.lastOfType("cre")
	want := 0
	for _, o := range f.r.folk {
		if math.Hypot(o.X-f.p.X, o.Y-f.p.Y) <= InterestRadius {
			want++
		}
	}
	if want == 0 || want == len(f.r.folk) {
		t.Fatalf("fixture: %d of %d folk in range, want some but not all", want, len(f.r.folk))
	}
	fl, ok := m["f"].([]any)
	if !ok || len(fl) != want {
		t.Fatalf("cre frame folk list: got %d rows, want the %d in range: %#v", len(fl), want, m["f"])
	}
	if m["r"] != InterestRadius {
		t.Fatalf("cre frame radius %v, want %v", m["r"], InterestRadius)
	}
	row := fl[0].([]any)
	if len(row) != 6 || row[0] != f.r.folk[0].ID || row[3] != f.r.folk[0].Kind {
		t.Fatalf("folk row shape: %#v", row)
	}
	if len(f.r.campsWire()) != len(f.r.camps) {
		t.Fatal("campsWire dropped a camp")
	}
}

// A client that is falling behind is skipped for the snapshot frame instead of
// having it queued, and is not dropped for it.
func TestCreSkipsBackloggedClient(t *testing.T) {
	f := newFixture(t)
	f.reset()
	for i := 0; i < OutboundBuffer/2; i++ {
		f.sess.Out <- []byte(`{"t":"filler"}`)
	}
	f.r.broadcastCre()
	if f.lastOfType("cre") != nil {
		t.Fatal("cre frame queued for a backlogged client")
	}
	if _, ok := f.r.players[f.sess.ID]; !ok {
		t.Fatal("backlogged client was dropped")
	}
	f.reset()
	f.r.broadcastCre()
	if f.lastOfType("cre") == nil {
		t.Fatal("no cre frame once the client caught up")
	}
}
