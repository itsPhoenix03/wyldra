package room

import (
	"fmt"
	"hash/fnv"
	"math"
	"sort"
	"strings"
	"testing"
)

// Days in every village at once, watched tick by tick (folk.go and friends).

// folkSimStats is what a run saw.
type folkSimStats struct {
	out         int // folk-ticks out of the huts
	stuck       int // ...unable to move a step they meant to take
	maxStuckRun int
	maxFrozen   int      // longest a walker stood still meaning to walk
	badTile     []string // stood on a tile nobody can stand on
	badStep     []string // a step of more than one level
	outAtNight  []string // out in the dead of night, not on watch
	slowHome    []string // still walking home long after setting off
	overlap     int      // folk-pair-ticks closer than 0.35
	overlapLong int      // the longest any pair stayed so
	acts        map[string]map[string]int // "isle/role" -> act[/item] -> folk-ticks
	meal        map[string][2]int         // isle -> (sitting at the fire at 0.40, members), summed over the days
	watch       map[string]map[string]bool // isle -> who kept a watch
	pairHome    [2]int                     // hunt parties walking home with game: (within 2 tiles of each other, all)
	hash        uint64
}

var roleNames = map[folkRole]string{roleElder: "elder", roleBuilder: "builder", roleForager: "forager",
	roleHunter: "hunter", roleFisher: "fisher", roleChild: "child"}

// simFolk runs the folk of a fresh room for days whole days from day time start,
// with the keeper parked out at sea.
func simFolk(t *testing.T, days int, start float64) folkSimStats {
	f := newFixture(t)
	r := f.r
	f.p.X, f.p.Y = 640, 600
	step := (TickMS / 1000.0) / DayLengthSec
	st := folkSimStats{acts: map[string]map[string]int{}, meal: map[string][2]int{}, watch: map[string]map[string]bool{}}
	h := fnv.New64a()
	prev := map[*Folk]int{}
	run, frozen, homeTk := map[*Folk]int{}, map[*Folk]int{}, map[*Folk]int{}
	ov := map[[2]*Folk]int{}
	r.time = start
	for k, n := 0, int(float64(days)/step); k < n; k++ {
		r.tickN++
		was := r.time
		if r.time = math.Mod(r.time+step, 1); r.time < was {
			r.day++
		}
		r.folkTick()
		tm := r.time
		deep := tm > NightStart+0.04 || tm < NightEnd-0.03
		for _, fk := range r.folk {
			v := fk.vil
			key := v.isle + "/" + roleNames[fk.role]
			if st.acts[key] == nil {
				st.acts[key] = map[string]int{}
			}
			a := fk.Act
			if fk.Item != "" {
				a += "/" + fk.Item
			}
			st.acts[key][a]++
			fmt.Fprintf(h, "%s %.5f %.5f %s %s;", fk.ID, fk.X, fk.Y, fk.Act, fk.Item)
			if fk.state == fsHidden {
				delete(prev, fk)
				run[fk], frozen[fk], homeTk[fk] = 0, 0, 0
				continue
			}
			st.out++
			if fk.stuck > 0 {
				st.stuck++
				run[fk]++
				st.maxStuckRun = max(st.maxStuckRun, run[fk])
			} else {
				run[fk] = 0
			}
			if fk.walking && !fk.moved && fk.state == fsTask && fk.gest.tk == 0 {
				frozen[fk]++
				st.maxFrozen = max(st.maxFrozen, frozen[fk])
			} else {
				frozen[fk] = 0
			}
			if c := v.nav.cellAt(fk.X, fk.Y); c < 0 || !v.nav.open[c] {
				st.badTile = append(st.badTile, fmt.Sprintf("%s at %.2f,%.2f t=%.3f %s", fk.ID, fk.X, fk.Y, tm, fk.Act))
			}
			i := ti(fk.X, fk.Y)
			if p, ok := prev[fk]; ok && p != i {
				if d := int(r.world.Elev[i]) - int(r.world.Elev[p]); d > 1 || d < -1 {
					st.badStep = append(st.badStep, fmt.Sprintf("%s %d->%d at %.2f,%.2f", fk.ID, r.world.Elev[p], r.world.Elev[i], fk.X, fk.Y))
				}
			}
			prev[fk] = i
			if fk.state == fsHome {
				if homeTk[fk]++; homeTk[fk] == 400 {
					st.slowHome = append(st.slowHome, fmt.Sprintf("%s at %.2f,%.2f t=%.3f", fk.ID, fk.X, fk.Y, tm))
				}
			} else {
				homeTk[fk] = 0
			}
			duty := r.onDuty(fk, tm)
			if duty {
				if st.watch[v.isle] == nil {
					st.watch[v.isle] = map[string]bool{}
				}
				st.watch[v.isle][fk.ID] = true
			}
			if deep && !duty && fk.state != fsHome {
				st.outAtNight = append(st.outAtNight, fmt.Sprintf("%s %s t=%.3f state %d act %s", fk.ID, fk.Kind, tm, fk.state, fk.Act))
			}
			if fk.party != nil && fk.Item == "game" && fk.walking && fk.moved {
				for _, m := range fk.party.members {
					if m != fk {
						st.pairHome[1]++
						if math.Hypot(m.X-fk.X, m.Y-fk.Y) < 2 {
							st.pairHome[0]++
						}
					}
				}
			}
		}
		for _, v := range r.villages {
			for a, fa := range v.members {
				for _, fb := range v.members[a+1:] {
					k := [2]*Folk{fa, fb}
					if fa.state != fsHidden && fb.state != fsHidden && math.Hypot(fa.X-fb.X, fa.Y-fb.Y) < 0.35 {
						st.overlap++
						ov[k]++
						st.overlapLong = max(st.overlapLong, ov[k])
					} else {
						delete(ov, k)
					}
				}
			}
			if math.Abs(tm-0.40) < step/2 {
				n := 0
				for _, fk := range v.members {
					if fk.state == fsTask && (fk.Act == "sit" || fk.Act == "listen") && math.Hypot(fk.X-v.fx, fk.Y-v.fy) < 4.5 {
						n++
					}
				}
				m := st.meal[v.isle]
				st.meal[v.isle] = [2]int{m[0] + n, m[1] + len(v.members)}
			}
		}
	}
	st.hash = h.Sum64()
	return st
}

func (st folkSimStats) report() string {
	var b strings.Builder
	keys := make([]string, 0, len(st.acts))
	for k := range st.acts {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		m := st.acts[k]
		acts := make([]string, 0, len(m))
		for a := range m {
			acts = append(acts, a)
		}
		sort.Slice(acts, func(i, j int) bool { return m[acts[i]] > m[acts[j]] })
		fmt.Fprintf(&b, "%-16s", k)
		for _, a := range acts {
			fmt.Fprintf(&b, " %s:%d", a, m[a])
		}
		b.WriteByte('\n')
	}
	fmt.Fprintf(&b, "out %d, stuck %d (%.3f%%, longest %d), longest frozen walker %d, overlap %d pair-ticks (longest %d), hunt pairs together %d/%d\n",
		st.out, st.stuck, 100*float64(st.stuck)/float64(max(1, st.out)), st.maxStuckRun, st.maxFrozen, st.overlap, st.overlapLong, st.pairHome[0], st.pairHome[1])
	fmt.Fprintf(&b, "meal %v, watch %v\n", st.meal, st.watch)
	return b.String()
}

// Three days in every village: nobody stands where nobody can stand or climbs
// a cliff, nobody is stuck, every trade does its work, the meal is eaten
// together, the night is slept through by all but the watch (which changes
// hands), and the hunters come home in pairs.
func TestFolkThreeDays(t *testing.T) {
	st := simFolk(t, 3, NightEnd)
	t.Log("\n" + st.report())
	for what, l := range map[string][]string{"on a closed tile": st.badTile, "up or down a cliff": st.badStep,
		"out at night off watch": st.outAtNight, "slow getting home": st.slowHome} {
		if len(l) > 0 {
			t.Errorf("%d folk-ticks %s, e.g. %s", len(l), what, l[0])
		}
	}
	if pct := float64(st.stuck) / float64(st.out); pct >= 0.01 {
		t.Errorf("stuck %.2f%% of the time out (want under 1%%)", 100*pct)
	}
	if st.maxFrozen > 40 {
		t.Errorf("a walker stood still %d ticks on the way", st.maxFrozen)
	}
	if st.overlapLong > 40 {
		t.Errorf("two folk stood on top of each other for %d ticks", st.overlapLong)
	}
	// each trade at its work, in every village that has it
	sign := map[string][][]string{ // any one of each inner list
		"elder":   {{"speak"}, {"tend", "tend/torch"}, {"sit", "sit/torch"}},
		"builder": {{"build"}, {"carry/wood", "carry/stone"}, {"collect"}},
		"forager": {{"craft"}, {"cook"}, {"call"}, {"carry/water", "carry/berries", "carry/herbs"}, {"collect"}},
		"hunter":  {{"watch", "watch/torch"}, {"carry/game"}, {"collect"}},
		"fisher":  {{"carry/fish"}, {"collect"}},
		"child":   {{"play"}, {"watch"}, {"sit"}},
	}
	for key, m := range st.acts {
		role := key[strings.Index(key, "/")+1:]
		for _, any := range sign[role] {
			ok := false
			for _, a := range any {
				ok = ok || m[a] > 0
			}
			if !ok {
				t.Errorf("%s never did any of %v", key, any)
			}
		}
	}
	listen := 0
	for _, m := range st.acts {
		listen += m["listen"]
	}
	if listen == 0 {
		t.Error("nobody ever listened to the elder at the evening fire")
	}
	for isle, m := range st.meal {
		if m[0]*2 < m[1] {
			t.Errorf("%s: only %d of %d at the meal over the days", isle, m[0], m[1])
		}
	}
	for isle, w := range st.watch {
		if len(w) < 2 {
			t.Errorf("%s: the watch never changed hands (%v)", isle, w)
		}
	}
	if len(st.watch) != 4 {
		t.Errorf("a village kept no watch: %v", st.watch)
	}
	if st.pairHome[1] == 0 || st.pairHome[0]*10 < st.pairHome[1]*8 {
		t.Errorf("hunters came home apart: together %d of %d ticks", st.pairHome[0], st.pairHome[1])
	}
}

// The same world and the same keepers make the same days: every position, act
// and load, tick for tick.
func TestFolkSameSeedSameDays(t *testing.T) {
	a, b := simFolk(t, 1, 0.3), simFolk(t, 1, 0.3)
	if a.hash != b.hash {
		t.Fatal("two rooms on the same seed lived different days")
	}
}
