package room

import (
	"hash/fnv"
	"math"
	"math/rand"

	"hearth/gameserver/world"
)

// Who the folk are (folk.go, folk_routine.go): their nature, their needs,
// their families and friends, the night watch — and the village's own dice.
//
// Nature comes from a folk member's id, so the same person is the same person
// every time the world starts: whether they are up with the light or last to
// bed, how hard they work, how much they want company, how much a keeper or a
// new build draws them. Needs run between 0 and 1 through the day — energy
// (rest restores it), hunger (the meal does), company (chats and the fire do)
// and the work drive, which builds while they sit about and the diligent feel
// most. The routine weighs them when it chooses what to do next.
//
// Families are the huts: the children of a hut belong to its grown-ups. Each
// pair in a village has a friendship (hut-mates and those of a trade start
// closer), which steers who chats with whom and rises as they do.
//
// The night watch is a roster per village — its watchers (elders, Ashmark
// hunters), the Marsh hunters, and grown-ups enough to make it at least two
// (three in a big village) — in three shifts a night, turning each night, so
// the watch keeps changing hands. The rest sleep.
//
// Every roll the folk make comes from their village's own generator, seeded
// from the world seed and the island, so a day in the villages plays out the
// same way for the same world and the same keepers.

type folkPersonality struct {
	lark      float64 // -1 night owl … +1 up with the light
	diligence float64 // 0..1
	social    float64 // 0..1
	curious   float64 // 0..1
}

type folkNeeds struct {
	energy float64 // 1 rested … 0 spent
	hunger float64 // 0 fed … 1 starving
	social float64 // 1 had their fill of company … 0 lonely
	drive  float64 // 0 worked out … 1 itching to get on
}

// hash01 is a stable 0..1 from a string and a salt.
func hash01(s string, salt byte) float64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	h.Write([]byte{salt})
	return float64(h.Sum64()>>11) / float64(1<<53)
}

func personalityOf(id string) folkPersonality {
	return folkPersonality{
		lark:      hash01(id, 'l')*2 - 1,
		diligence: 0.25 + hash01(id, 'd')*0.75,
		social:    0.2 + hash01(id, 's')*0.8,
		curious:   hash01(id, 'c'),
	}
}

func folkSeed(seed, isle string) int64 {
	h := fnv.New64a()
	h.Write([]byte(seed))
	h.Write([]byte{0})
	h.Write([]byte(isle))
	return int64(h.Sum64() & (1<<63 - 1))
}

// the village's dice
func (v *village) roll() float64       { return v.rng.Float64() }
func (v *village) intn(n int) int      { return v.rng.Intn(n) }
func (v *village) between(a, b int) int { return a + v.rng.Intn(b-a+1) }
func (v *village) chance(p float64) bool { return v.rng.Float64() < p }

func (r *Room) newVillage(c world.FolkCamp) *village {
	v := &village{isle: c.IslandID, fx: float64(c.FireX) + 0.5, fy: float64(c.FireY) + 0.5, yardR: c.YardR,
		rng:  rand.New(rand.NewSource(folkSeed(r.cfg.Seed, c.IslandID))),
		pile: map[int]int{}, fire: 0.7, day: -1, onlookFor: -1, feast: -1,
	}
	v.monoSeen = r.mono // only what is lit from now on is news
	v.clock = r.time
	return v
}

// settleVillage gives a village's people their nature, hours, families,
// friendships and the watch, once its ground is surveyed.
func (r *Room) settleVillage(v *village) {
	hut := map[int][]*Folk{}
	for _, f := range v.members {
		f.pers = personalityOf(f.ID)
		p := f.pers
		f.pace = 0.88 + hash01(f.ID, 'p')*0.24
		late := (1 - p.lark) / 2 // 0 lark … 1 owl
		f.wake = NightEnd + 0.004 + late*0.05
		f.bed = NightStart - 0.056 + late*0.05
		if f.child { // up after the grown-ups, to bed well before them
			f.wake = NightEnd + 0.02 + late*0.03
			f.bed = 0.622 + late*0.022
		}
		f.needs = folkNeeds{energy: 0.85, hunger: 0.3, social: 0.6, drive: 0.5 * p.diligence}
		for k := range f.last {
			f.last[k] = doNone
		}
		hut[f.Camp] = append(hut[f.Camp], f)
	}
	// families: a hut's children belong to its grown-ups
	for _, ci := range v.camps {
		var grown, kids []*Folk
		for _, f := range hut[ci] {
			if f.child {
				kids = append(kids, f)
			} else {
				grown = append(grown, f)
			}
		}
		for _, k := range kids {
			k.parents = grown
		}
		for _, g := range grown {
			g.kids = kids
		}
	}
	// friendships
	n := len(v.members)
	v.aff = make([][]float32, n)
	for a := range v.aff {
		v.aff[a] = make([]float32, n)
	}
	for a, fa := range v.members {
		for b := a + 1; b < n; b++ {
			fb := v.members[b]
			x := 0.15 + 0.35*hash01(fa.ID+"|"+fb.ID, 'f')
			if fa.Camp == fb.Camp {
				x += 0.35
			}
			if fa.role == fb.role {
				x += 0.15
			}
			if fa.child && fb.child {
				x += 0.2
			}
			v.aff[a][b], v.aff[b][a] = float32(x), float32(x)
		}
	}
	// the night watch: watchers and the Marsh hunters, then grown-ups enough
	for _, f := range v.members {
		if f.watcher || f.Kind == "tribal" {
			v.roster = append(v.roster, f)
		}
	}
	want := 2
	if n >= 10 {
		want = 3
	}
	for _, f := range v.members {
		if len(v.roster) >= want {
			break
		}
		if !f.child && !f.watcher && f.Kind != "tribal" && (f.role == roleBuilder || len(v.members) <= 3) {
			v.roster = append(v.roster, f)
		}
	}
	for _, f := range v.roster {
		f.roster = true
	}
	// cooks: the foragers take it in turn; without any, whoever is not the elder
	for _, f := range v.members {
		if f.role == roleForager {
			v.cooks = append(v.cooks, f)
		}
	}
	if len(v.cooks) == 0 {
		for _, f := range v.members {
			if !f.child && f.role != roleElder {
				v.cooks = append(v.cooks, f)
			}
		}
	}
	if len(v.cooks) == 0 {
		v.cooks = v.members[:1]
	}
	for _, f := range v.members {
		if f.role == roleElder {
			v.elder = f
		}
	}
}

func (v *village) affinity(a, b *Folk) float64 { return float64(v.aff[a.idx][b.idx]) }

func (v *village) befriend(a, b *Folk, by float64) {
	x := float32(math.Min(1, float64(v.aff[a.idx][b.idx])+by))
	v.aff[a.idx][b.idx], v.aff[b.idx][a.idx] = x, x
}

// tickNeeds runs f's needs down (or up) by what it is doing.
func (f *Folk) tickNeeds(t float64) {
	n := &f.needs
	if f.state == fsHidden {
		if isNightAt(t) {
			n.energy = math.Min(1, n.energy+1.0/500)
		} else {
			n.energy = math.Min(1, n.energy+1.0/1500)
		}
		n.hunger = math.Min(1, n.hunger+1.0/9000)
		return
	}
	work := false
	switch f.Act {
	case "sit", "listen":
		n.energy = math.Min(1, n.energy+1.0/1300)
		n.social = math.Min(1, n.social+1.0/900)
	case "chat":
		n.social = math.Min(1, n.social+1.0/70)
	case "collect", "build", "craft", "cook", "tend", "carry":
		n.energy = math.Max(0, n.energy-1.0/1900)
		work = true
	case "dance", "play", "cheer":
		n.energy = math.Max(0, n.energy-1.0/1500)
		n.social = math.Min(1, n.social+1.0/300)
	default:
		n.energy = math.Max(0, n.energy-1.0/3600)
	}
	n.hunger = math.Min(1, n.hunger+1.0/2700)
	n.social = math.Max(0, n.social-(0.4+f.pers.social)/2600)
	if work {
		n.drive = math.Max(0, n.drive-1.0/420)
	} else {
		n.drive = math.Min(1, n.drive+f.pers.diligence/1700)
	}
}

func isNightAt(t float64) bool { return t > NightStart || t < NightEnd }

// ── the night watch ─────────────────────────────────────────────────────────

// watchShift is which third of the night t falls in (0 dusk, 1 the dead of
// night, 2 towards dawn) and which night it is (the day it began on).
func watchShift(t float64, day int) (shift, night int) {
	s := t - NightStart
	night = day
	if t < 0.5 {
		s, night = t+1-NightStart, day-1
	}
	shift = min(2, max(0, int(s/((1-NightStart+NightEnd)/3))))
	return shift, max(0, night)
}

// onShift: f keeps that shift of that night.
func (v *village) onShift(f *Folk, shift, night int) bool {
	n := len(v.roster)
	if n == 0 || !f.roster {
		return false
	}
	per := 1
	if n >= 4 {
		per = 2
	}
	for j := range per {
		if v.roster[(night*3*per+shift*per+j)%n] == f {
			return true
		}
	}
	return false
}

// onDuty: f keeps the watch now.
func (r *Room) onDuty(f *Folk, t float64) bool {
	if !f.roster || !isNightAt(t) {
		return false
	}
	shift, night := watchShift(t, r.day)
	return f.vil.onShift(f, shift, night)
}

// folkUp: f is up and about now — its waking hours, or its watch. Whoever
// kept the last watch of the night stays up into the day.
func (r *Room) folkUp(f *Folk, t float64) bool {
	if r.onDuty(f, t) {
		return true
	}
	if t >= f.wake && t < f.bed {
		return true
	}
	if t >= NightEnd && t < f.wake && f.roster {
		return f.vil.onShift(f, 2, max(0, r.day-1))
	}
	return false
}
