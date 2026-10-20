package room

import (
	"math"
	"testing"

	"hearth/gameserver/world"
)

// The folk's reactions and the village's duties (folk.go, folk_work.go, folk_social.go).

// runFolk ticks the folk n times, keeping the clock (and its days) moving.
func runFolk(f *fixture, n int, each func()) {
	step := (TickMS / 1000.0) / DayLengthSec
	for range n {
		f.r.tickN++
		was := f.r.time
		if f.r.time = math.Mod(f.r.time+step, 1); f.r.time < was {
			f.r.day++
		}
		f.r.folkTick()
		if each != nil {
			each()
		}
	}
}

func angDiff(a, b float64) float64 {
	d := math.Mod(math.Abs(a-b), 2*math.Pi)
	return math.Min(d, 2*math.Pi-d)
}

// A creature at the Dunes: its hunters stand guard on the way it is coming,
// facing it, while everyone else is in. One it closes on goes in too; once it
// is gone they stand down, and the rest come out looking round.
func TestFolkGuardsFaceTheThreat(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	f.p.X, f.p.Y = 640, 600
	v := f.r.villageOf("dunes")
	c := f.spawnAt("crawler", v.fx+12, v.fy+1, false)
	runFolk(f, 80, nil)
	var guards []*Folk
	for _, fk := range v.members {
		switch fk.state {
		case fsGuard:
			guards = append(guards, fk)
		case fsHidden:
		default:
			t.Fatalf("%s %s is %q, neither in nor on guard", fk.ID, fk.Kind, fk.Act)
		}
	}
	if len(guards) == 0 || len(guards) > folkMaxGuards {
		t.Fatalf("%d guards, want 1..%d", len(guards), folkMaxGuards)
	}
	dFire := math.Hypot(c.X-v.fx, c.Y-v.fy)
	for _, g := range guards {
		if g.role != roleHunter {
			t.Fatalf("%s (%s) stands guard while the Ashmark hunters hide", g.ID, g.Kind)
		}
		if g.Act != "guard" {
			t.Fatalf("%s on guard is %q", g.ID, g.Act)
		}
		if want := math.Atan2(c.Y-g.Y, c.X-g.X); angDiff(g.Face, want) > 0.2 {
			t.Fatalf("%s faces %.2f, the crawler is at %.2f", g.ID, g.Face, want)
		}
		if d := math.Hypot(c.X-g.X, c.Y-g.Y); d >= dFire || math.Hypot(g.X-v.fx, g.Y-v.fy) > v.yardR {
			t.Fatalf("%s is not between the crawler and the huts: %.1f from it (the fire %.1f)", g.ID, d, dFire)
		}
	}
	for a, ga := range guards { // spread across the way, not bunched
		for _, gb := range guards[a+1:] {
			if d := math.Hypot(ga.X-gb.X, ga.Y-gb.Y); d < 0.9 {
				t.Fatalf("guards %s and %s stand %.2f apart", ga.ID, gb.ID, d)
			}
		}
	}
	// on the wire as a guard, facing it
	var row []any
	for _, w := range f.r.folkWire() {
		if w[0] == guards[0].ID {
			row = w
		}
	}
	if row[4] != "guard" || row[5] == nil {
		t.Fatalf("guard row %#v", row)
	}

	// it comes for one of them: in they go
	g := guards[0]
	c.X, c.Y = g.X+2, g.Y
	runFolk(f, 30, nil)
	if g.state != fsHidden && g.state != fsFlee {
		t.Fatalf("a guard stood fast with a crawler 2 tiles off: %q", g.Act)
	}

	// gone: guards stand down and look round, the rest come out and look round
	f.r.delCreature(c.ID)
	sawLook := false
	runFolk(f, folkClearTk+40, func() {
		for _, fk := range v.members {
			if fk.state == fsTask && fk.gest.tk > 0 && fk.Act == "watch" {
				sawLook = true
			}
		}
	})
	out := 0
	for _, fk := range v.members {
		if fk.state == fsGuard {
			t.Fatalf("%s still on guard with nothing about", fk.ID)
		}
		if fk.state != fsHidden {
			out++
		}
	}
	if out == 0 || !sawLook {
		t.Fatalf("after the all-clear: %d out, looked round %v", out, sawLook)
	}
}

// What a folk member carries rides the wire as the row's seventh field.
func TestFolkCarryOnTheWire(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	f.p.X, f.p.Y = 640, 600
	seen := map[string]bool{}
	runFolk(f, 2500, func() {
		for _, w := range f.r.folkWire() {
			if len(w) != 7 {
				t.Fatalf("folk row %#v", w)
			}
			if it, ok := w[6].(string); ok {
				seen[it] = true
				if w[4] == "hide" {
					t.Fatalf("a hidden folk member shows a load: %#v", w)
				}
			} else if w[6] != nil {
				t.Fatalf("item field %#v", w[6])
			}
		}
	})
	for _, it := range []string{"wood", "fish", "game"} {
		if !seen[it] {
			t.Errorf("never saw anyone carry %s (saw %v)", it, seen)
		}
	}
	if !seen["berries"] && !seen["herbs"] && !seen["water"] {
		t.Errorf("no forager ever carried anything home (saw %v)", seen)
	}
	for _, fk := range f.r.folk {
		if fk.Act == "carry" && fk.Item == "" {
			t.Fatalf("%s carrying nothing", fk.ID)
		}
	}
}

// The evening after a monolith is lit is a feast: the village cheers and dances.
func TestFolkFeastAfterAMonolith(t *testing.T) {
	f := newFixture(t)
	f.p.X, f.p.Y = 640, 600
	f.r.time = 0.5
	runFolk(f, 5, nil)
	f.r.mono[1] = true
	acts := map[string]int{}
	runFolk(f, int((NightStart-0.5)*DayLengthSec*1000/TickMS), func() {
		for _, fk := range f.r.folk {
			acts[fk.Act]++
		}
	})
	if acts["cheer"] == 0 || acts["dance"] == 0 {
		t.Fatalf("no feast: cheer %d, dance %d", acts["cheer"], acts["dance"])
	}
	// and only the once
	acts = map[string]int{}
	runFolk(f, int(1.0*DayLengthSec*1000/TickMS), func() {
		for _, fk := range f.r.folk {
			acts[fk.Act]++
		}
	})
	if acts["cheer"] != 0 || acts["dance"] != 0 {
		t.Fatalf("the feast went on the next evening too: cheer %d, dance %d", acts["cheer"], acts["dance"])
	}
}

// The cook gets the pot on before midday and calls everyone in.
func TestFolkCookCallsTheMeal(t *testing.T) {
	f := newFixture(t)
	f.p.X, f.p.Y = 640, 600
	f.r.time = 0.27
	v := f.r.villageOf("woods")
	cooked, called := false, false
	runFolk(f, int((folkMealAt+0.01-0.27)*DayLengthSec*1000/TickMS), func() {
		cooked = cooked || (v.cook.Act == "cook" && math.Hypot(v.cook.X-v.fx, v.cook.Y-v.fy) < 2.2)
		called = called || v.cook.Act == "call"
	})
	if !cooked || !called || !v.called {
		t.Fatalf("%s (the cook): cooked at the fire %v, called %v", v.cook.ID, cooked, called)
	}
}

// At a child's bedtime a parent calls them in, and they go before the grown-ups.
func TestFolkChildrenCalledIn(t *testing.T) {
	f := newFixture(t)
	f.p.X, f.p.Y = 640, 600
	f.r.time = 0.6
	v := f.r.villageOf("woods")
	calls := 0
	var kidIn, grownIn []float64
	in := map[*Folk]bool{}
	runFolk(f, int((NightStart-0.6)*DayLengthSec*1000/TickMS), func() {
		for _, fk := range v.members {
			if fk.Act == "call" && len(fk.kids) > 0 {
				calls++
			}
			if fk.state == fsHidden && !in[fk] {
				in[fk] = true
				if fk.child {
					kidIn = append(kidIn, f.r.time)
				} else if !fk.roster {
					grownIn = append(grownIn, f.r.time)
				}
			}
		}
	})
	if calls == 0 {
		t.Fatal("no parent called a child in")
	}
	if len(kidIn) == 0 || len(grownIn) == 0 || kidIn[len(kidIn)-1] > grownIn[0]+0.02 {
		t.Fatalf("children in at %v, grown-ups at %v", kidIn, grownIn)
	}
}

// A keeper walking into the village is watched and waved to; a keeper building
// near it draws an onlooker.
func TestFolkKeeperIsNoticed(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	v := f.r.villageOf("woods")
	f.p.X, f.p.Y = v.fx+60, v.fy
	runFolk(f, 10, nil)
	f.p.X, f.p.Y = v.fx+3.5, v.fy+3.5
	waved, faced := 0, 0
	runFolk(f, 300, func() {
		for _, fk := range v.members {
			d := math.Hypot(f.p.X-fk.X, f.p.Y-fk.Y)
			if fk.Act == "wave" && d < folkNoticeR && angDiff(fk.Face, math.Atan2(f.p.Y-fk.Y, f.p.X-fk.X)) < 0.3 {
				waved++
			}
			if (fk.Act == "idle" || fk.Act == "watch" || fk.Act == "sit") && d < folkNoticeR && d > folkTalkR &&
				angDiff(fk.Face, math.Atan2(f.p.Y-fk.Y, f.p.X-fk.X)) < 0.05 {
				faced++
			}
		}
	})
	if waved == 0 || faced == 0 {
		t.Fatalf("a keeper in the yard: waved %d, watched %d", waved, faced)
	}

	// a build in the yard, of an afternoon (not while they are all at the meal)
	f.p.X, f.p.Y = v.fx+60, v.fy
	f.r.time = 0.47
	runFolk(f, 60, nil)
	bi := -1
	for _, d := range [][2]float64{{-7, 3}, {7, -3}, {-6, -5}, {6, 5}, {0, 8}} {
		if x, y := v.fx+d[0], v.fy+d[1]; v.reach(x, y) && v.reach(x+1, y) {
			bi = ti(x, y)
			break
		}
	}
	if bi < 0 {
		t.Fatal("fixture: nowhere to build")
	}
	f.r.structures[bi] = &Structure{Kind: "workbench", HP: 50, Lvl: 1}
	watched := false
	bx, by := float64(bi%world.SIZE)+0.5, float64(bi/world.SIZE)+0.5
	runFolk(f, 400, func() {
		for _, fk := range v.members {
			if fk.doing == doLook && fk.Act == "watch" && math.Hypot(fk.X-bx, fk.Y-by) < 3.5 &&
				angDiff(fk.Face, math.Atan2(by-fk.Y, bx-fk.X)) < 0.3 {
				watched = true
			}
		}
	})
	if !watched {
		t.Fatal("nobody came to look at the keeper's build")
	}
}

// A curious child tags after a keeper strolling round the yard, a few steps
// behind, and gives up once the keeper leaves.
func TestFolkChildFollowsTheKeeper(t *testing.T) {
	f := newFixture(t)
	f.r.time = 0.16
	v := f.r.villageOf("woods")
	f.p.X, f.p.Y = v.fx+60, v.fy
	runFolk(f, 20, nil)
	var kid *Folk
	trailed := 0
	k := 0
	runFolk(f, 900, func() {
		k++
		// a slow circle round the fire, 4 tiles out
		a := float64(k) * 0.004
		if x, y := v.fx+math.Cos(a)*4, v.fy+math.Sin(a)*4; v.reach(x, y) {
			f.p.X, f.p.Y = x, y
		}
		for _, fk := range v.members {
			if fk.follow == f.p {
				kid = fk
				if d := math.Hypot(fk.X-f.p.X, fk.Y-f.p.Y); d < 4.5 && d > 1.5 {
					trailed++
				}
			}
		}
	})
	if kid == nil || !kid.child {
		t.Fatal("no child tagged after the keeper")
	}
	if trailed < 30 {
		t.Fatalf("%s followed, but only kept near for %d ticks", kid.ID, trailed)
	}
	f.p.X, f.p.Y = v.fx+60, v.fy
	runFolk(f, 30, nil)
	if kid.follow != nil {
		t.Fatal("still following a keeper who has left the village")
	}
}

// Same nature for the same id, every time; and folk differ.
func TestFolkPersonalityFromID(t *testing.T) {
	a, b := personalityOf("camp-woods-1-0"), personalityOf("camp-woods-1-0")
	if a != b {
		t.Fatal("one id, two natures")
	}
	f := newFixture(t)
	early, late := 1.0, 0.0
	for _, fk := range f.r.folk {
		early, late = math.Min(early, fk.wake), math.Max(late, fk.wake)
	}
	if late-early < 0.02 {
		t.Fatalf("everyone gets up together: %.3f..%.3f", early, late)
	}
}
