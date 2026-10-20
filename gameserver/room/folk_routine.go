package room

import (
	"math"
	"math/rand"
	"sort"

	"hearth/gameserver/world"
)

// The folk's days (folk.go runs them): who does what, where and when.
//
// A day has its parts — up at their own hour (folk_life.go), morning work, a
// meal at the fire at midday, afternoon work, an evening round the fire, then
// bed at their own hour, while the watch keeps the night — and in the working
// parts each folk member chooses what to do next by weighing what their trade
// offers (roleActs) against the hour, the weather on their island, their needs
// and nature, and what they did last (never the same thing twice running):
//
//	elder     rounds of the huts and a word at each door, herbs for the pot,
//	          tending the fire, long sits by it; leads the evening; the
//	          shaman speaks over the fire
//	builder   (villagers) fells wood and stacks it on the hut's pile, mends
//	          the hut walls with it; where no trees grow, hauls stone instead
//	forager   (villager women) picks berries for the pot and herbs to work up
//	          on the doorstep, fetches water, cooks in turn
//	hunter    (Ashmark) walks the village edge watching out, goes out on the
//	          hunt in pairs and brings the game home, mends gear
//	fisher    (Marsh tribal) fishes from the shore and brings in the catch,
//	          cuts reeds to thatch with, walks the edge
//	child     tag with the other children near a parent, tags after the
//	          grown-ups at their work, digs in the dirt
//
// and any of them may rest, stroll, or go and find a friend to chat to. On top
// of that the village's duties come round (folk_work.go) — the dawn and dusk
// stoking, the cook, the elder's words, calling the children in — and anyone
// in the middle of a bit of work finishes it before they come to the fire,
// unless the cook calls. Everything they work at is a real place in the world
// (folkSpot) any of them can walk to (folk_nav.go): a tree, a bush, a rock, the
// shore, the village edge, the hunting grounds beyond it, a hut's walls, its
// doorstep and its woodpile, the ring round the fire.

type folkRole uint8

const (
	roleElder folkRole = iota
	roleBuilder
	roleForager
	roleHunter
	roleFisher
	roleChild
)

func folkRoleOf(kind string) folkRole {
	switch kind {
	case "elder_woods", "shaman", "elder_yvenne", "elder_spire":
		return roleElder
	case "villager", "villager_snow":
		return roleBuilder
	case "ashmark_hunter":
		return roleHunter
	case "tribal":
		return roleFisher
	case "woods_child":
		return roleChild
	}
	return roleForager
}

type dayPart uint8

const (
	partNone dayPart = iota
	partNight
	partMorning
	partMeal
	partAfternoon
	partEvening
)

// Day parts, as fractions of the day (r.time); night is NightStart..NightEnd.
const (
	folkMealAt    = 0.36
	folkMealEnd   = 0.44
	folkEveningAt = 0.62
	folkCookAt    = 0.28  // the cook gets the pot on
	folkCallAt    = 0.354 // and calls everyone in
	folkSpeechAt  = 0.632 // the elder's words at the evening fire
)

func dayPartAt(t float64) dayPart {
	switch {
	case t > NightStart || t < NightEnd:
		return partNight
	case t < folkMealAt:
		return partMorning
	case t < folkMealEnd:
		return partMeal
	case t < folkEveningAt:
		return partAfternoon
	}
	return partEvening
}

// folkSpot is a place to work: where to stand and which way to face.
type folkSpot struct {
	x, y, face float64
	node       int // the resource node worked there, or -1
}

// village is one island's settlement: its fire, its folk, its ground and places.
type village struct {
	isle   string
	fx, fy float64 // the fire's centre
	yardR  float64
	camps  []int // its huts (indices into r.camps)

	members []*Folk
	seats   [][2]float64 // round the fire, one per member

	trees, bushes, rocks []folkSpot // beside a tree / bush / rock node
	shore                []folkSpot // at the water's edge, facing it
	edge                 []folkSpot // round the village edge, facing out
	far                  []folkSpot // hunting grounds out past the edge
	ring                 []folkSpot // round the fire, facing it: feeding it, the pot, the catch set down
	walls                map[int][]folkSpot // per hut: beside its walls, facing them
	doorstep             map[int]folkSpot   // per hut: just off the door, facing the fire
	pileAt               map[int]folkSpot   // per hut: where its wood is stacked

	rng       *rand.Rand
	nav       *navGrid
	home      int32 // the nav region the fire stands in
	navBudget int   // routes left to plan this tick

	// the people
	aff    [][]float32
	roster []*Folk // the night watch, in turn
	cooks  []*Folk // who cooks, in turn
	elder  *Folk

	// the day (folk_work.go)
	day                               int
	cook, stoker, duskStoker          *Folk
	cookSet, called, stoked, duskSet  bool
	spoke                             bool
	speaker                           *Folk
	pile                              map[int]int // logs on each hut's pile
	fire                              float64     // how well the fire burns, 0..1
	larder                            int         // what has come in for the pot today
	feast                             int         // the day whose evening is a feast (a monolith was lit), or -1
	monoSeen                          [4]bool
	onlookFor                         int // a keeper's build someone has gone to look at, or -1
	onlookCD                          int
	clock                             float64 // the day time last tick, to notice the clock being moved

	// danger (folk.go folkGuard)
	threat *Creature
	alarm  bool
	guards []*Folk
	posts  []navPt // where each guard stands
}

func (v *village) faceFire(x, y float64) float64 { return math.Atan2(v.fy-y, v.fx-x) }

// reach: (x, y) is ground the village can walk to.
func (v *village) reach(x, y float64) bool { return v.nav.reach(x, y, v.home) }

// spotOK: s can still be walked to and its node (if any) still stands.
func (r *Room) spotOK(v *village, s folkSpot) bool {
	return v.reach(s.x, s.y) && (s.node < 0 || !r.removedAt(s.node))
}

// surveyVillage finds a village's seats and places, once, after the camps are
// placed (their tiles blocked) and its ground is read (folk_nav.go).
func (r *Room) surveyVillage(v *village) {
	g := v.nav
	v.home = -1
	if c := g.nearestOpen(g.cellAt(v.fx, v.fy), -1, 3); c >= 0 {
		v.home = g.comp[c]
	}
	n := len(v.members)
	sitR := math.Max(1.6, 0.95*float64(n)/(2*math.Pi)+0.6)
	for k := range n {
		base := float64(k) * 2 * math.Pi / float64(n)
		seat := [2]float64{math.NaN(), 0}
	find:
		for _, d := range [5]float64{0, 0.22, -0.22, 0.45, -0.45} {
			for _, rr := range [2]float64{sitR, sitR + 0.7} {
				x, y := v.fx+math.Cos(base+d)*rr, v.fy+math.Sin(base+d)*rr
				if v.reach(x, y) {
					seat = [2]float64{x, y}
					break find
				}
			}
		}
		if math.IsNaN(seat[0]) { // nowhere at the fire: by their own door
			dx, dy := r.campDoor(v.members[k].Camp)
			seat = [2]float64{dx, dy}
		}
		v.seats = append(v.seats, seat)
	}
	for k := range 8 {
		a := float64(k)*math.Pi/4 + math.Pi/8
		x, y := v.fx+math.Cos(a)*1.2, v.fy+math.Sin(a)*1.2
		if v.reach(x, y) {
			v.ring = append(v.ring, folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1})
		}
	}

	// beside resource nodes and along the shore: anywhere they can walk to
	fx, fy := int(v.fx), int(v.fy)
	const reach, shoreReach = 18, 24
	for y := fy - shoreReach; y <= fy+shoreReach; y++ {
		for x := fx - shoreReach; x <= fx+shoreReach; x++ {
			if x < 1 || y < 1 || x >= world.SIZE-1 || y >= world.SIZE-1 {
				continue
			}
			i := y*world.SIZE + x
			near := max(abs(x-fx), abs(y-fy)) <= reach
			if k, ok := r.world.Nodes[i]; ok && near && k != world.NodeStarmetal {
				// stand on the side of it nearest the fire
				best, bd := folkSpot{node: -1}, math.Inf(1)
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					sx, sy := float64(x+d[0])+0.5, float64(y+d[1])+0.5
					if dd := math.Hypot(sx-v.fx, sy-v.fy); dd < bd && v.reach(sx, sy) && !r.isNodeTile(x+d[0], y+d[1]) {
						best, bd = folkSpot{x: sx, y: sy, face: math.Atan2(-float64(d[1]), -float64(d[0])), node: i}, dd
					}
				}
				if best.node >= 0 {
					switch k {
					case world.NodeTree:
						v.trees = append(v.trees, best)
					case world.NodeBush:
						v.bushes = append(v.bushes, best)
					default:
						v.rocks = append(v.rocks, best)
					}
				}
			}
			if r.tileAt(i) != world.TWater && v.reach(float64(x)+0.5, float64(y)+0.5) {
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if r.tileAt((y+d[1])*world.SIZE+x+d[0]) == world.TWater {
						v.shore = append(v.shore, folkSpot{x: float64(x) + 0.5, y: float64(y) + 0.5, face: math.Atan2(float64(d[1]), float64(d[0])), node: -1})
						break
					}
				}
			}
		}
	}
	v.trees, v.bushes = nearestSpread(v, v.trees, 20, 1.5), nearestSpread(v, v.bushes, 16, 1.5)
	v.rocks, v.shore = nearestSpread(v, v.rocks, 10, 1.5), nearestSpread(v, v.shore, 12, 1.6)

	// lookouts round the edge and hunting grounds beyond it, facing out
	for k := range 12 {
		a := float64(k) * 2 * math.Pi / 12
		for _, rr := range [4]float64{v.yardR + 1, v.yardR, v.yardR + 2, v.yardR - 1} {
			x, y := v.fx+math.Cos(a)*rr, v.fy+math.Sin(a)*rr
			if v.reach(x, y) {
				v.edge = append(v.edge, folkSpot{x: x, y: y, face: a, node: -1})
				break
			}
		}
	}
	for k := range 10 {
		a := float64(k)*2*math.Pi/10 + 0.3
		for rr := 17.0; rr >= 11; rr-- {
			x, y := v.fx+math.Cos(a)*rr, v.fy+math.Sin(a)*rr
			if v.reach(x, y) {
				v.far = append(v.far, folkSpot{x: x, y: y, face: a, node: -1})
				break
			}
		}
	}

	// the huts: the two front walls to work on (in front of the hut's art, not
	// hidden behind it), the doorstep, and the woodpile by the side wall; and the
	// backs of the huts are walked only when there is no going round the front
	v.walls, v.doorstep, v.pileAt = map[int][]folkSpot{}, map[int]folkSpot{}, map[int]folkSpot{}
	for _, ci := range v.camps {
		c := r.camps[ci]
		hx, hy := float64(c.HutX)+0.5, float64(c.HutY)+0.5
		for _, d := range [3][2]int{{-1, 0}, {0, -1}, {-1, -1}} {
			if cc := g.cellOf((c.HutY+d[1])*world.SIZE + c.HutX + d[0]); cc >= 0 {
				g.cost[cc] = max(g.cost[cc], 8)
			}
		}
		for _, p := range [2][2]float64{{hx - 0.7, hy + 1.0}, {hx + 1.0, hy - 0.7}} {
			if v.reach(p[0], p[1]) {
				v.walls[ci] = append(v.walls[ci], folkSpot{x: p[0], y: p[1], face: math.Atan2(hy-p[1], hx-p[0]), node: -1})
			}
		}
		dx, dy := r.campDoor(ci)
		a := math.Atan2(v.fy-dy, v.fx-dx)
		sx, sy := dx+math.Cos(a)*0.8, dy+math.Sin(a)*0.8
		if !v.reach(sx, sy) {
			sx, sy = dx, dy
		}
		v.doorstep[ci] = folkSpot{x: sx, y: sy, face: v.faceFire(sx, sy), node: -1}
		pile := folkSpot{x: hx + 1.0, y: hy - 0.2, face: math.Atan2(0.2, -1.0), node: -1}
		if !v.reach(pile.x, pile.y) {
			pile = folkSpot{x: hx - 0.2, y: hy + 1.0, face: math.Atan2(-1.0, 0.2), node: -1}
		}
		if !v.reach(pile.x, pile.y) {
			pile = v.doorstep[ci]
		}
		v.pileAt[ci] = pile
		v.pile[ci] = 2
	}
}

func (r *Room) isNodeTile(x, y int) bool {
	_, ok := r.world.Nodes[y*world.SIZE+x]
	return ok
}

// nearestSpread keeps up to max spots, nearest the fire first, at least gap
// tiles apart so folk do not crowd one bush.
func nearestSpread(v *village, s []folkSpot, max int, gap float64) []folkSpot {
	sort.SliceStable(s, func(a, b int) bool {
		return math.Hypot(s[a].x-v.fx, s[a].y-v.fy) < math.Hypot(s[b].x-v.fx, s[b].y-v.fy)
	})
	var out []folkSpot
	for _, p := range s {
		ok := true
		for _, q := range out {
			if math.Hypot(p.x-q.x, p.y-q.y) < gap {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, p)
			if len(out) == max {
				break
			}
		}
	}
	return out
}

// ── running a day ───────────────────────────────────────────────────────────

// folkRoutine carries f through its plans, making new ones as the day turns
// and when it runs out.
func (r *Room) folkRoutine(f *Folk, t float64) {
	v := f.vil
	if part := dayPartAt(t); part != f.part {
		f.part = part
		f.joins = r.joinsGathering(f, part)
		// the meal and the evening: those who come finish the bit in hand first
		if (part == partMeal || part == partEvening) && f.joins && f.partner == nil && f.party == nil &&
			f.duty == dutyNone && f.doing != doGather && f.doing != doDuty {
			r.folkWrapUp(f, 50)
		}
	}
	// the cook's call: down tools
	if v.called && !f.heard && f.joins && (f.part == partMorning || f.part == partMeal) {
		f.heard = true
		if f.duty == dutyNone && f.party == nil && f.partner == nil && f.doing != doGather && f.doing != doDuty {
			r.folkWrapUp(f, 8)
		}
	}
	if len(f.steps) == 0 {
		f.steps = r.folkPlan(f, t)
		r.folkBegin(f)
	}
	r.folkDo(f)
}

// folkWrapUp cuts f's plans short: the act in hand is finished (at most cap
// more ticks), anything carried is seen to where it goes, and the rest dropped.
func (r *Room) folkWrapUp(f *Folk, cap int) {
	if len(f.steps) == 0 {
		return
	}
	if f.walking && (f.Item == "" || f.Item == "torch") && f.steps[0].sync == 0 {
		f.dropPlans() // not started yet: come straight away
		return
	}
	keep := 1
	if f.Item != "" && f.Item != "torch" {
		for k, s := range f.steps {
			if s.fx.puts() {
				keep = k + 1
				break
			}
		}
	}
	f.steps = f.steps[:keep]
	for k := range f.steps {
		f.steps[k].dur = min(f.steps[k].dur, cap)
	}
	if !f.walking && f.timer > cap {
		f.timer = cap
	}
}

// joinsGathering rolls whether f comes to the fire for this part of the day.
func (r *Room) joinsGathering(f *Folk, part dayPart) bool {
	v := f.vil
	if f.party != nil && f.doing == doHunt {
		return false // out on the hunt: they eat when they are back
	}
	odds := 0.0
	switch part {
	case partMeal:
		odds = map[folkRole]float64{roleElder: 1, roleChild: 0.85, roleHunter: 0.6, roleFisher: 0.65}[f.role]
		if odds == 0 {
			odds = 0.8
		}
		odds += f.needs.hunger*0.35 + (f.pers.social-0.5)*0.2
	case partEvening:
		odds = map[folkRole]float64{roleElder: 1, roleHunter: 0.7, roleChild: 0.92}[f.role]
		if odds == 0 {
			odds = 0.86
		}
		odds += (f.pers.social - 0.5) * 0.3
		if r.feast(v) {
			odds += 0.4
		}
	}
	return v.chance(odds)
}

func (r *Room) folkSpeed(f *Folk, run bool) float64 {
	if run {
		return folkPlaySp
	}
	sp := folkWalkSp * f.pace
	if f.party != nil {
		sp = folkWalkSp * f.party.pace
	}
	if f.child && f.party == nil {
		sp *= 1.25
	}
	switch f.Item {
	case "wood", "stone", "water", "game":
		sp *= 0.88 // laden
	}
	return sp
}

// folkBegin starts f on its first step: off to its spot, or straight into the act.
func (r *Room) folkBegin(f *Folk) {
	f.waiting = false
	for len(f.steps) > 0 {
		s := &f.steps[0]
		if s.sync > 0 && f.party != nil {
			if f.syncAt < s.sync {
				f.syncAt, f.waitTk = s.sync, 0
			}
			if !f.party.ready(s.sync) {
				f.waiting, f.walking = true, false
				r.folkWaitPose(f)
				return
			}
		}
		if !math.IsNaN(s.x) && math.Hypot(s.x-f.X, s.y-f.Y) > 0.06 {
			sp := r.folkSpeed(f, s.run)
			f.walking = true
			f.timer = int(math.Hypot(s.x-f.X, s.y-f.Y)/sp*2.4) + 40
			f.Act, f.Face = walkAct(s.run, f.Item), math.NaN()
			return
		}
		f.walking = false
		if s.act == "" || (s.act == "chat" && !r.folkStartChat(f, s)) {
			r.stepDone(f, s)
			f.steps = f.steps[1:]
			continue
		}
		f.timer = s.dur
		f.Act, f.Face = s.act, s.face
		if s.leave {
			f.leaveParty()
		}
		r.stepStarts(f, s)
		return
	}
	f.walking = false
	f.Act, f.Face = "idle", math.NaN()
}

func walkAct(run bool, item string) string {
	switch {
	case run:
		return "play"
	case item != "" && item != "torch":
		return "carry"
	}
	return "walk"
}

// folkDo advances f one tick along its current step.
func (r *Room) folkDo(f *Folk) {
	if len(f.steps) == 0 {
		return
	}
	s := &f.steps[0]
	if f.waiting {
		f.waitTk++
		if f.party == nil || f.party.ready(s.sync) || f.waitTk > 90 {
			if f.waitTk > 90 {
				f.leaveParty() // they are not coming: on without them
			}
			r.folkBegin(f)
		} else {
			r.folkWaitPose(f)
		}
		return
	}
	if f.walking {
		f.Act, f.Face = walkAct(s.run, f.Item), math.NaN()
		f.timer--
		if r.folkGo(f, s.x, s.y, r.folkSpeed(f, s.run)) {
			s.x, s.y = f.X, f.Y // there, or as near as they can get with someone on the spot
			f.walking = false
			r.folkBegin(f)
		} else if f.nav.none || f.stuck > 12 || f.timer <= 0 {
			// cannot get there: think again
			f.stuck = 0
			f.dropPlans()
			f.Act = "idle"
		} else if f.moved && f.greetCD == 0 {
			r.folkGreet(f)
		}
		return
	}
	f.Act, f.Face = s.act, s.face // (again, after stopping to talk to a keeper)
	switch s.act {
	case "chat":
		p := f.partner
		if p == nil || p.partner != f || p.state != fsTask || math.Hypot(p.X-f.X, p.Y-f.Y) > 2.6 {
			f.timer = 0
		} else {
			f.Face = math.Atan2(p.Y-f.Y, p.X-f.X)
			if f.idx < p.idx {
				f.vil.befriend(f, p, 0.0008)
			}
		}
	case "sit":
		// the elder has the floor: the gathering turns to listen
		if sp := f.vil.speaker; sp != nil && sp != f && f.doing == doGather && sp.Act == "speak" {
			f.Act, f.Face = "listen", math.Atan2(sp.Y-f.Y, sp.X-f.X)
		}
	case "watch", "wave", "dance":
		if s.with != nil {
			f.Face = math.Atan2(s.with.Y-f.Y, s.with.X-f.X)
		}
		if s.act == "dance" && f.timer%9 == 0 { // round and round
			s.face = math.Mod(s.face+1.1, 2*math.Pi)
		}
	}
	f.timer--
	if r.tickN%2 == int64(f.idx%2) {
		r.folkMakeRoom(f)
	}
	if f.timer <= 0 {
		if s.act == "chat" {
			f.endChat()
			f.chatCD = 150 + f.vil.intn(200)
		}
		r.stepDone(f, s)
		f.steps = f.steps[1:]
		r.folkBegin(f)
	}
}

// folkStartChat: f has walked up to s.with — if they are still free, both
// turn to each other and talk a while.
func (r *Room) folkStartChat(f *Folk, s *folkStep) bool {
	p := s.with
	if p == nil || p == f || p.partner != nil || f.partner != nil || p.state != fsTask || p.walking || p.waiting || p.Item != "" ||
		p.party != nil || p.duty != dutyNone || p.gest.tk > 0 || !r.folkUp(p, r.time) || math.Hypot(p.X-f.X, p.Y-f.Y) > 2.2 {
		return false
	}
	f.partner, p.partner = p, f
	p.steps = []folkStep{{x: math.NaN(), y: math.NaN(), act: "chat", dur: s.dur, with: f, watchI: -1}}
	p.walking, p.timer = false, s.dur
	p.doing = doChat
	p.Act, p.Face = "chat", math.Atan2(f.Y-p.Y, f.X-p.X)
	s.face = math.Atan2(p.Y-f.Y, p.X-f.X)
	return true
}

// ── making plans ────────────────────────────────────────────────────────────

// ticksUntil: ticks from now until day time t1 (0 if it has passed).
func (r *Room) ticksUntil(t1 float64) int {
	return max(0, int((t1-r.time)/((TickMS/1000.0)/DayLengthSec)))
}

// plan builds a list of steps.
type plan struct {
	r     *Room
	f     *Folk
	v     *village
	x, y  float64 // where the last step leaves them
	steps []folkStep
}

func (r *Room) newPlan(f *Folk) *plan { return &plan{r: r, f: f, v: f.vil, x: f.X, y: f.Y} }

// at: go to spot s (or the free place nearest it) and do act there for dur ticks.
func (p *plan) at(s folkSpot, act string, dur int) *folkStep {
	x, y := p.r.freeSpot(p.f, s.x, s.y)
	p.steps = append(p.steps, folkStep{x: x, y: y, act: act, dur: dur, face: s.face, watchI: -1})
	p.x, p.y = x, y
	return &p.steps[len(p.steps)-1]
}

// run: dash to (x, y) at play.
func (p *plan) run(x, y float64) *plan {
	p.steps = append(p.steps, folkStep{x: x, y: y, face: math.NaN(), run: true, watchI: -1})
	p.x, p.y = x, y
	return p
}

func (p *plan) here(act string, dur int, face float64) *folkStep {
	p.steps = append(p.steps, folkStep{x: math.NaN(), y: math.NaN(), act: act, dur: dur, face: face, watchI: -1})
	return &p.steps[len(p.steps)-1]
}

// last is the step just added.
func (p *plan) last() *folkStep { return &p.steps[len(p.steps)-1] }

// freeSpot is (x, y), or if someone else stands or is headed there, the nearest
// open place a step round it, so two folk never work on top of each other.
func (r *Room) freeSpot(f *Folk, x, y float64) (float64, float64) {
	v := f.vil
	taken := func(x, y float64) bool {
		for _, o := range v.members {
			if o == f || o.state == fsHidden {
				continue
			}
			if math.Hypot(o.X-x, o.Y-y) < 0.55 && !o.walking {
				return true
			}
			if len(o.steps) > 0 && !math.IsNaN(o.steps[0].x) && math.Hypot(o.steps[0].x-x, o.steps[0].y-y) < 0.55 {
				return true
			}
		}
		return false
	}
	if !taken(x, y) {
		return x, y
	}
	for _, rr := range [2]float64{0.7, 1.1} {
		for k := range 8 {
			a := float64(k) * math.Pi / 4
			nx, ny := x+math.Cos(a)*rr, y+math.Sin(a)*rr
			if v.reach(nx, ny) && !taken(nx, ny) {
				return nx, ny
			}
		}
	}
	return x, y
}

// pick is a random spot of s that can still be worked, or false.
func (r *Room) pick(v *village, s []folkSpot) (folkSpot, bool) {
	var ok []folkSpot
	for _, p := range s {
		if r.spotOK(v, p) {
			ok = append(ok, p)
		}
	}
	if len(ok) == 0 {
		return folkSpot{}, false
	}
	return ok[v.intn(len(ok))], true
}

// pickNear is a workable spot of s near (x, y): one of the nearest few.
func (r *Room) pickNear(v *village, s []folkSpot, x, y float64) (folkSpot, bool) {
	var ok []folkSpot
	for _, p := range s {
		if r.spotOK(v, p) {
			ok = append(ok, p)
		}
	}
	if len(ok) == 0 {
		return folkSpot{}, false
	}
	sort.SliceStable(ok, func(a, b int) bool {
		return math.Hypot(ok[a].x-x, ok[a].y-y) < math.Hypot(ok[b].x-x, ok[b].y-y)
	})
	return ok[v.intn(min(len(ok), 4))], true
}

// yardSpot: a random open spot in the yard (within maxR of (cx, cy)) they can walk to.
func (r *Room) yardSpot(v *village, cx, cy, minR, maxR float64) (folkSpot, bool) {
	for range 10 {
		a, d := v.roll()*2*math.Pi, minR+v.roll()*(maxR-minR)
		x, y := cx+math.Cos(a)*d, cy+math.Sin(a)*d
		if v.reach(x, y) && math.Hypot(x-v.fx, y-v.fy) > 1.4 && math.Hypot(x-v.fx, y-v.fy) < v.yardR+2 {
			return folkSpot{x: x, y: y, face: v.roll() * 2 * math.Pi, node: -1}, true
		}
	}
	return folkSpot{}, false
}

func (r *Room) seatOf(f *Folk) folkSpot {
	s := f.vil.seats[f.seat]
	return folkSpot{x: s[0], y: s[1], face: f.vil.faceFire(s[0], s[1]), node: -1}
}

// folkPlan decides what f does next.
func (r *Room) folkPlan(f *Folk, t float64) []folkStep {
	v, p := f.vil, r.newPlan(f)
	f.doing = doNone
	if f.party != nil && !(f.party.gather && f.joins) {
		f.leaveParty() // whatever they were out together for is over
	}
	switch {
	case f.Item != "" && f.Item != "torch":
		// still carrying something (a plan cut short): see it to where it goes
		r.deliver(f, p)
		f.doing = doCarry
	case f.duty != dutyNone:
		d := f.duty
		f.duty = dutyNone
		r.planDuty(f, p, d)
		f.doing = doDuty
	case isNightAt(t):
		r.planWatch(f, p)
	case f.joins && (f.part == partMeal || f.part == partEvening || (f.heard && v.called && f.part == partMorning)):
		r.planGather(f, p, t)
		f.doing = doGather
	default:
		a := r.chooseActivity(f, t)
		if !r.planActivity(f, p, a) {
			p.steps = nil
		}
		if len(p.steps) > 0 {
			f.doing = a
			f.last = [3]activity{a, f.last[0], f.last[1]}
		}
	}
	if len(p.steps) == 0 { // nothing came of it: stand a while
		p.here("idle", v.between(15, 35), math.NaN())
	}
	return p.steps
}

// ── choosing what to do ─────────────────────────────────────────────────────

type activity uint8

const (
	doNone activity = iota
	doRest
	doChat
	doStroll
	doRounds
	doHerbs
	doSpeakFire
	doFireSit
	doTend
	doWood
	doMend
	doStone
	doForage
	doCraft
	doWater
	doPatrol
	doHunt
	doGear
	doFish
	doReeds
	doTag
	doTagAlong
	doDig
	// not chosen by weighing: what a plan is for
	doGather // the meal, the evening
	doDuty   // a village duty (folk_work.go)
	doCarry  // seeing a load home
	doWatch  // the night watch
	doLook   // gone to look at a keeper's build
)

const (
	kWork uint8 = iota
	kRest
	kSocial
	kPlay
	kRoam
)

type actDef struct {
	a    activity
	base float64
	kind uint8
	part [6]float64 // × by day part (partNone … partEvening); 0 means 1
}

// outdoors: work the rain spoils
var outdoors = map[activity]bool{doWood: true, doForage: true, doHunt: true, doFish: true, doPatrol: true,
	doStroll: true, doWater: true, doReeds: true, doStone: true, doHerbs: true}

// what each trade does with its day (part: none, night, morning, meal, afternoon, evening)
var roleActs = map[folkRole][]actDef{
	roleElder: {
		{doRounds, 1.1, kWork, [6]float64{0, 0, 1.5, 0, 0.45, 0.3}},
		{doHerbs, 0.55, kWork, [6]float64{}},
		{doTend, 0.6, kWork, [6]float64{}},
		{doFireSit, 0.9, kRest, [6]float64{0, 0, 0.8, 0, 1.2, 1.3}},
		{doSpeakFire, 0.6, kSocial, [6]float64{}},
		{doChat, 0.8, kSocial, [6]float64{}},
		{doStroll, 0.3, kRoam, [6]float64{}},
	},
	roleBuilder: {
		{doWood, 1.15, kWork, [6]float64{0, 0, 1.2, 0, 1, 0.5}},
		{doMend, 1.0, kWork, [6]float64{0, 0, 1.1, 0, 1, 0.6}},
		{doStone, 0.9, kWork, [6]float64{}},
		{doRest, 0.6, kRest, [6]float64{0, 0, 0.8, 0, 1.2, 1.3}},
		{doChat, 0.7, kSocial, [6]float64{}},
		{doStroll, 0.3, kRoam, [6]float64{}},
	},
	roleForager: {
		{doForage, 1.15, kWork, [6]float64{0, 0, 1.25, 0, 0.9, 0.5}},
		{doCraft, 0.9, kWork, [6]float64{0, 0, 0.8, 0, 1.3, 1.1}},
		{doWater, 0.6, kWork, [6]float64{}},
		{doRest, 0.5, kRest, [6]float64{0, 0, 0.8, 0, 1.2, 1.2}},
		{doChat, 0.85, kSocial, [6]float64{}},
		{doStroll, 0.3, kRoam, [6]float64{}},
	},
	roleHunter: {
		{doPatrol, 1.0, kWork, [6]float64{}},
		{doHunt, 1.25, kWork, [6]float64{0, 0, 1.35, 0, 1.0, 0.3}},
		{doGear, 0.5, kWork, [6]float64{}},
		{doRest, 0.45, kRest, [6]float64{}},
		{doChat, 0.6, kSocial, [6]float64{}},
	},
	roleFisher: {
		{doFish, 1.3, kWork, [6]float64{}},
		{doPatrol, 0.7, kWork, [6]float64{}},
		{doReeds, 0.6, kWork, [6]float64{}},
		{doHunt, 0.5, kWork, [6]float64{}},
		{doRest, 0.45, kRest, [6]float64{}},
		{doChat, 0.6, kSocial, [6]float64{}},
	},
	roleChild: {
		{doTag, 1.2, kPlay, [6]float64{}},
		{doTagAlong, 0.9, kPlay, [6]float64{}},
		{doDig, 0.6, kPlay, [6]float64{}},
		{doFireSit, 0.35, kRest, [6]float64{}},
		{doChat, 0.5, kSocial, [6]float64{}},
	},
}

// chooseActivity weighs what f could do now and picks the best of it, with a
// little chance in it, never what they did last.
func (r *Room) chooseActivity(f *Folk, t float64) activity {
	v, n, pe := f.vil, f.needs, f.pers
	rain := r.weather.kind == "rain" && (v.isle == "woods" || v.isle == "marsh")
	best, bs := doNone, -1.0
	for _, d := range roleActs[f.role] {
		if !r.canDo(f, d.a) {
			continue
		}
		s := d.base
		if m := d.part[f.part]; m != 0 {
			s *= m
		}
		switch d.kind {
		case kWork:
			s *= (0.45 + n.drive*0.9) * (0.5 + n.energy*0.6) * (0.6 + pe.diligence*0.8)
		case kRest:
			s *= (0.25 + (1-n.energy)*1.5) * (1.3 - pe.diligence*0.5)
		case kSocial:
			s *= (0.2 + (1-n.social)*1.3) * (0.4 + pe.social*1.2)
		case kPlay:
			s *= 0.6 + n.energy*0.6
		case kRoam:
			s *= 0.5 + pe.curious
		}
		switch d.a {
		case doForage, doHerbs:
			s *= 0.8 + n.hunger*0.5
		case doWood:
			s *= 1.1 - float64(v.pile[f.Camp])/float64(folkPileMax)
		case doTend:
			s *= 1.3 - v.fire
		}
		if rain {
			if outdoors[d.a] {
				s *= 0.6
			} else if d.kind == kRest || d.a == doCraft || d.a == doGear {
				s *= 1.4
			}
		}
		switch d.a {
		case f.last[0]:
			s = 0 // never the same thing twice running
		case f.last[1]:
			s *= 0.45
		case f.last[2]:
			s *= 0.75
		}
		if s <= 0 {
			continue
		}
		s += v.roll() * 0.3
		if s > bs {
			best, bs = d.a, s
		}
	}
	return best
}

// canDo: there is the means and the place for activity a.
func (r *Room) canDo(f *Folk, a activity) bool {
	v := f.vil
	any := func(s []folkSpot) bool {
		for _, p := range s {
			if r.spotOK(v, p) {
				return true
			}
		}
		return false
	}
	switch a {
	case doRounds:
		return len(v.camps) > 1 || f.part == partMorning
	case doHerbs, doForage:
		return any(v.bushes)
	case doReeds:
		return any(v.bushes) && len(v.walls[f.Camp]) > 0
	case doSpeakFire:
		return f.Kind == "shaman" || (f.role == roleElder && f.pers.social > 0.6)
	case doWood:
		return any(v.trees) && v.pile[f.Camp] < folkPileMax
	case doStone:
		return !any(v.trees) && any(v.rocks) && len(v.walls[f.Camp]) > 0
	case doMend:
		return len(v.walls[f.Camp]) > 0
	case doTend:
		return v.fire < 0.55
	case doWater:
		return any(v.shore)
	case doFish:
		return any(v.shore)
	case doPatrol:
		return len(v.edge) > 0
	case doHunt:
		return len(v.far) > 0 && r.huntMate(f) != nil
	case doChat:
		return f.chatCD == 0
	case doTagAlong:
		return r.nearestUp(f, f.parents) != nil || len(v.members) > 1
	}
	return true
}

// planActivity lays out the steps for a; false if nothing came of it.
func (r *Room) planActivity(f *Folk, p *plan, a activity) bool {
	v := f.vil
	switch a {
	case doRest:
		if v.chance(0.55) {
			p.at(v.doorstep[f.Camp], "sit", v.between(50, 100))
		} else {
			p.at(r.seatOf(f), "sit", v.between(60, 130))
		}
	case doChat:
		return r.chat(f, p, 12)
	case doStroll:
		if s, ok := r.yardSpot(v, v.fx, v.fy, 2, v.yardR+1); ok {
			p.at(s, "watch", v.between(20, 45))
		}
	case doRounds:
		r.planRounds(f, p)
	case doHerbs:
		s, ok := r.pickNear(v, v.bushes, f.X, f.Y)
		if !ok {
			return false
		}
		p.at(s, "collect", v.between(45, 70)).take("herbs")
		if f.role == roleElder { // into the pot
			if rs, ok := r.pick(v, v.ring); ok {
				p.at(rs, "tend", v.between(25, 40)).fx = fxLarder
			}
			p.at(r.seatOf(f), "sit", v.between(60, 120))
		} else {
			p.at(v.doorstep[f.Camp], "craft", v.between(80, 140)).fx = fxDrop
		}
	case doSpeakFire:
		a := v.roll() * 2 * math.Pi
		x, y := v.fx+math.Cos(a)*1.3, v.fy+math.Sin(a)*1.3
		if !v.reach(x, y) {
			return false
		}
		p.at(folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1}, "speak", v.between(40, 70))
		p.at(r.seatOf(f), "sit", v.between(60, 120))
	case doFireSit:
		p.at(r.seatOf(f), "sit", v.between(150, 260))
	case doTend:
		r.planTend(f, p)
	case doWood:
		r.planWood(f, p)
	case doMend:
		r.planMend(f, p)
	case doStone:
		r.planStone(f, p)
	case doForage:
		r.planForage(f, p)
	case doCraft:
		p.at(v.doorstep[f.Camp], "craft", v.between(100, 180))
	case doWater:
		r.planWater(f, p)
	case doPatrol:
		r.patrol(f, p, v.between(2, 3), 25, 55)
	case doHunt:
		return r.planHunt(f, p)
	case doGear:
		p.at(v.doorstep[f.Camp], "collect", v.between(40, 70)) // mending gear on the doorstep
	case doFish:
		r.planFish(f, p)
	case doReeds:
		r.planReeds(f, p)
	case doTag, doTagAlong, doDig:
		r.play(f, p, a)
	}
	return len(p.steps) > 0
}

// patrol: walk n lookouts round the edge from the nearest one, watching outward at each.
func (r *Room) patrol(f *Folk, p *plan, n, minW, maxW int) {
	v := f.vil
	e := v.edge
	if len(e) == 0 {
		return
	}
	k, bd := 0, math.Inf(1)
	for i, s := range e {
		if d := math.Hypot(s.x-f.X, s.y-f.Y); d < bd {
			k, bd = i, d
		}
	}
	dir := 1
	if v.intn(2) == 0 {
		dir = -1
	}
	for range n {
		k = ((k+dir)%len(e) + len(e)) % len(e)
		if v.reach(e[k].x, e[k].y) {
			p.at(e[k], "watch", v.between(minW, maxW))
		}
	}
}

// planRounds: the elder's morning round of the huts, a word at each door.
func (r *Room) planRounds(f *Folk, p *plan) {
	v := f.vil
	huts := append([]int(nil), v.camps...)
	v.rng.Shuffle(len(huts), func(a, b int) { huts[a], huts[b] = huts[b], huts[a] })
	for _, ci := range huts[:min(len(huts), v.between(2, 3))] {
		d := v.doorstep[ci]
		dx, dy := r.campDoor(ci)
		p.at(folkSpot{x: d.x, y: d.y, face: math.Atan2(dy-d.y, dx-d.x), node: -1}, "watch", v.between(18, 35))
	}
	p.at(r.seatOf(f), "sit", v.between(60, 120))
}

// nearestUp is the nearest of folk who is out and about, or nil.
func (r *Room) nearestUp(f *Folk, folk []*Folk) *Folk {
	var best *Folk
	bd := math.Inf(1)
	for _, o := range folk {
		if o == f || o.state != fsTask || !r.folkUp(o, r.time) {
			continue
		}
		if d := math.Hypot(o.X-f.X, o.Y-f.Y); d < bd {
			best, bd = o, d
		}
	}
	return best
}

// play: the children's games, never far from a parent.
func (r *Room) play(f *Folk, p *plan, a activity) {
	v := f.vil
	cx, cy := v.fx, v.fy
	if par := r.nearestUp(f, f.parents); par != nil && math.Hypot(par.X-v.fx, par.Y-v.fy) < v.yardR+2 {
		cx, cy = par.X, par.Y // stay where a parent can see them
	}
	var kids, grown []*Folk
	for _, o := range v.members {
		if o == f || o.state != fsTask || !r.folkUp(o, r.time) {
			continue
		}
		if o.child {
			kids = append(kids, o)
		} else if math.Hypot(o.X-v.fx, o.Y-v.fy) < v.yardR+2 {
			grown = append(grown, o)
		}
	}
	switch a {
	case doTag:
		// tag: dash at another child (or round the yard alone), then away
		if len(kids) > 0 {
			o := kids[v.intn(len(kids))]
			if x, y := o.X+v.roll()-0.5, o.Y+v.roll()-0.5; v.reach(x, y) {
				p.run(x, y)
			}
		}
		for range v.between(2, 3) {
			if s, ok := r.yardSpot(v, cx, cy, 1.5, 5); ok {
				p.run(s.x, s.y)
			}
		}
		p.here("idle", v.between(8, 20), math.NaN())
	case doTagAlong:
		// tag after a grown-up (a parent by choice) and watch them work
		o := r.nearestUp(f, f.parents)
		if o == nil && len(grown) > 0 {
			o = grown[v.intn(len(grown))]
		}
		if o == nil {
			return
		}
		for range 6 {
			a := v.roll() * 2 * math.Pi
			x, y := o.X+math.Cos(a)*1.4, o.Y+math.Sin(a)*1.4
			if v.reach(x, y) {
				p.at(folkSpot{x: x, y: y, face: math.Atan2(o.Y-y, o.X-x), node: -1}, "watch", v.between(30, 55)).with = o
				break
			}
		}
	case doDig:
		if s, ok := r.yardSpot(v, cx, cy, 1.2, 4); ok {
			p.at(s, "collect", v.between(30, 50)) // digging in the dirt
		}
	}
}
