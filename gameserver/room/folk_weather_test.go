package room

import "testing"

// tickFolk runs the folk sim n ticks with the player parked out of everyone's way.
func tickFolk(f *fixture, n int) {
	f.p.X, f.p.Y = 640, 600 // open water south of the Core: near no village
	for k := 0; k < n; k++ {
		f.r.folkTick()
	}
}

func countState(v *village, want folkState) (n int) {
	for _, fk := range v.members {
		if fk.state == want {
			n++
		}
	}
	return n
}

// A sandstorm sends the whole Dunes village in, watchers included, and leaves the
// Woods alone; when it passes they come back out.
func TestSandstormSendsTheDunesIn(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	dunes, woods := f.r.villageOf("dunes"), f.r.villageOf("woods")
	if dunes == nil || woods == nil {
		t.Fatal("fixture: missing a village")
	}
	f.r.weather.kind, f.r.weather.until = "sandstorm", f.now+600000
	tickFolk(f, 250)
	if n := countState(dunes, fsHidden); n != len(dunes.members) {
		t.Fatalf("sandstorm: %d of %d Dunes folk are in", n, len(dunes.members))
	}
	if n := countState(woods, fsHidden); n == len(woods.members) {
		t.Fatal("a sandstorm sent the Woods in too")
	}

	f.r.weather.kind = ""
	tickFolk(f, folkClearTk+5)
	if countState(dunes, fsHidden) == len(dunes.members) {
		t.Fatal("nobody came back out once the sandstorm passed")
	}
}

// While they head in they are `shelter`, not `run`: the client shouts about the
// storm, not about the blight.
func TestStormRunIsShelter(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	spire := f.r.villageOf("spire")
	f.r.weather.kind, f.r.weather.until = "snowstorm", f.now+600000
	tickFolk(f, 1)
	saw := false
	for _, fk := range spire.members {
		if fk.state == fsFlee {
			saw = true
			if fk.Act != "shelter" {
				t.Fatalf("%s heads in from the snowstorm as %q, want shelter", fk.Kind, fk.Act)
			}
		}
	}
	if !saw && countState(spire, fsHidden) != len(spire.members) {
		t.Fatal("the snowstorm moved nobody on the Spire")
	}
}

// Rain calls the Woods children in; the adults carry on.
func TestRainCallsTheChildrenIn(t *testing.T) {
	f := newFixture(t)
	f.setDay()
	woods := f.r.villageOf("woods")
	f.r.weather.kind, f.r.weather.until = "rain", f.now+600000
	tickFolk(f, 250)
	kids, adultsOut := 0, 0
	for _, fk := range woods.members {
		if fk.child {
			kids++
			if fk.state != fsHidden {
				t.Fatalf("a Woods child is out in the rain: %q", fk.Act)
			}
		} else if fk.state != fsHidden {
			adultsOut++
		}
	}
	if kids == 0 {
		t.Fatal("fixture: no Woods children")
	}
	if adultsOut == 0 {
		t.Fatal("the rain sent every Woods adult in; only the children should go")
	}
}
