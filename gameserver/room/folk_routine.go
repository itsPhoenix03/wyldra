package room

import (
	"math"
	"math/rand"
	"sort"

	"hearth/gameserver/world"
)

// The folk's days (folk.go runs them): who does what, where and when.
//
// A day has its parts — up at their own hour, morning work, a meal at the
// fire at midday, afternoon work, an evening round the fire, then bed at their
// own hour — and each folk member works at their trade in between:
//
//   elder     morning rounds of the huts, a word with whoever is about; the
//             rest of the day by the fire; leads the evening; keeps the fire
//             at night
//   builder   (villagers) works on the hut walls, and fetches wood from the
//             trees round the village to stack by their hut
//   forager   (villager women) picks the bushes round the village, then
//             crafts on their doorstep
//   hunter    (Ashmark) walks the village edge, stops to watch outward, goes
//             out on the hunt and brings it home; keeps the edge at night
//   fisher    (Marsh tribal) fishes from the shore, and walks the edge
//   child     chases the other children, runs round the fire, tags after the
//             grown-ups
//
// and any of them may stop to chat with a neighbour. Everything they work at
// is a real place in the world (folkSpot): a tree or bush node, the shore, a
// hut wall, the village edge — found once when the camps are set up, each a
// straight, clear walk from the fire.

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

// village is one island's settlement: its fire, its folk and its places.
type village struct {
	isle   string
	fx, fy float64 // the fire's centre
	yardR  float64
	camps  []int // its huts (indices into r.camps)

	members []*Folk
	seats   [][2]float64 // round the fire, one per member

	trees, bushes []folkSpot         // beside a tree / bush node
	shore         []folkSpot         // at the water's edge, facing it
	edge          []folkSpot         // round the village edge, facing out
	far           []folkSpot         // hunting grounds out past the edge
	walls         map[int][]folkSpot // per hut: beside its walls, facing them
	doorstep      map[int]folkSpot   // per hut: just off the door, facing the fire
}

func (v *village) faceFire(x, y float64) float64 { return math.Atan2(v.fy-y, v.fx-x) }

// surveyVillage finds a village's seats and places, once, after the camps are
// placed (their tiles blocked) so nothing is chosen on a hut or the fire.
func (r *Room) surveyVillage(v *village) {
	n := len(v.members)
	sitR := math.Max(1.6, 0.95*float64(n)/(2*math.Pi)+0.6)
	for k := range n {
		base := float64(k) * 2 * math.Pi / float64(n)
		seat := [2]float64{math.NaN(), 0}
	find:
		for _, d := range [5]float64{0, 0.22, -0.22, 0.45, -0.45} {
			for _, rr := range [2]float64{sitR, sitR + 0.7} {
				x, y := v.fx+math.Cos(base+d)*rr, v.fy+math.Sin(base+d)*rr
				if r.folkWalkable(x, y) {
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

	clear := func(x, y float64) bool { return r.folkWalkable(x, y) && r.folkLineClear(v.fx, v.fy, x, y) }
	// beside resource nodes and along the shore, nearest the fire first
	fx, fy := int(v.fx), int(v.fy)
	const reach = 13
	for y := fy - reach; y <= fy+reach; y++ {
		for x := fx - reach; x <= fx+reach; x++ {
			if x < 1 || y < 1 || x >= world.SIZE-1 || y >= world.SIZE-1 {
				continue
			}
			i := y*world.SIZE + x
			if k, ok := r.world.Nodes[i]; ok && (k == world.NodeTree || k == world.NodeBush) {
				// stand on the side of it nearest the fire
				best, bd := folkSpot{node: -1}, math.Inf(1)
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					sx, sy := float64(x+d[0])+0.5, float64(y+d[1])+0.5
					if dd := math.Hypot(sx-v.fx, sy-v.fy); dd < bd && clear(sx, sy) {
						best, bd = folkSpot{x: sx, y: sy, face: math.Atan2(-float64(d[1]), -float64(d[0])), node: i}, dd
					}
				}
				if best.node >= 0 {
					if k == world.NodeTree {
						v.trees = append(v.trees, best)
					} else {
						v.bushes = append(v.bushes, best)
					}
				}
			}
			if r.tileAt(i) != world.TWater && clear(float64(x)+0.5, float64(y)+0.5) {
				for _, d := range [4][2]int{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
					if r.tileAt((y+d[1])*world.SIZE+x+d[0]) == world.TWater {
						v.shore = append(v.shore, folkSpot{x: float64(x) + 0.5, y: float64(y) + 0.5, face: math.Atan2(float64(d[1]), float64(d[0])), node: -1})
						break
					}
				}
			}
		}
	}
	v.trees, v.bushes, v.shore = nearestSpread(v, v.trees, 16), nearestSpread(v, v.bushes, 16), nearestSpread(v, v.shore, 10)

	// lookouts round the edge and hunting grounds beyond it, facing out
	for k := range 10 {
		a := float64(k) * 2 * math.Pi / 10
		for _, rr := range [3]float64{v.yardR + 1, v.yardR, v.yardR + 2} {
			x, y := v.fx+math.Cos(a)*rr, v.fy+math.Sin(a)*rr
			if clear(x, y) {
				v.edge = append(v.edge, folkSpot{x: x, y: y, face: a, node: -1})
				break
			}
		}
	}
	for k := range 8 {
		a := float64(k)*2*math.Pi/8 + 0.3
		for rr := 16.0; rr >= 11; rr-- {
			x, y := v.fx+math.Cos(a)*rr, v.fy+math.Sin(a)*rr
			if clear(x, y) {
				v.far = append(v.far, folkSpot{x: x, y: y, face: a, node: -1})
				break
			}
		}
	}

	// the huts: the two front walls to work on (in front of the hut's art, not
	// hidden behind it), and the doorstep
	v.walls, v.doorstep = map[int][]folkSpot{}, map[int]folkSpot{}
	for _, ci := range v.camps {
		c := r.camps[ci]
		hx, hy := float64(c.HutX)+0.5, float64(c.HutY)+0.5
		for _, p := range [2][2]float64{{hx - 0.7, hy + 1.0}, {hx + 1.0, hy - 0.7}} {
			if r.folkWalkable(p[0], p[1]) {
				v.walls[ci] = append(v.walls[ci], folkSpot{x: p[0], y: p[1], face: math.Atan2(hy-p[1], hx-p[0]), node: -1})
			}
		}
		dx, dy := r.campDoor(ci)
		a := math.Atan2(v.fy-dy, v.fx-dx)
		sx, sy := dx+math.Cos(a)*0.8, dy+math.Sin(a)*0.8
		if !r.folkWalkable(sx, sy) {
			sx, sy = dx, dy
		}
		v.doorstep[ci] = folkSpot{x: sx, y: sy, face: v.faceFire(sx, sy), node: -1}
	}
}

// nearestSpread keeps up to max spots, nearest the fire first, at least 1.5
// tiles apart so folk do not crowd one bush.
func nearestSpread(v *village, s []folkSpot, max int) []folkSpot {
	sort.SliceStable(s, func(a, b int) bool {
		return math.Hypot(s[a].x-v.fx, s[a].y-v.fy) < math.Hypot(s[b].x-v.fx, s[b].y-v.fy)
	})
	var out []folkSpot
	for _, p := range s {
		ok := true
		for _, q := range out {
			if math.Hypot(p.x-q.x, p.y-q.y) < 1.5 {
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
	if part := dayPartAt(t); part != f.part {
		f.part = part
		f.joins = r.joinsGathering(f, part)
		// the meal and the evening call those who come away from their work
		if (part == partMeal || part == partEvening) && f.joins && f.partner == nil {
			f.dropPlans()
		}
	}
	if len(f.steps) == 0 {
		f.steps = r.folkPlan(f)
		r.folkBegin(f)
	}
	r.folkDo(f)
}

// joinsGathering rolls whether f comes to the fire for this part of the day.
func (r *Room) joinsGathering(f *Folk, part dayPart) bool {
	odds := 0.0
	switch part {
	case partMeal:
		odds = map[folkRole]float64{roleElder: 1, roleChild: 0.7, roleHunter: 0.45, roleFisher: 0.5}[f.role]
		if odds == 0 {
			odds = 0.75
		}
	case partEvening:
		odds = map[folkRole]float64{roleElder: 1, roleHunter: 0.6}[f.role]
		if odds == 0 {
			odds = 0.9
		}
	}
	return rand.Float64() < odds
}

func (r *Room) folkSpeed(f *Folk, run bool) float64 {
	if run {
		return folkPlaySp
	}
	if f.child {
		return folkWalkSp * f.pace * 1.25
	}
	return folkWalkSp * f.pace
}

// folkBegin starts f on its first step: off to its spot, or straight into the act.
func (r *Room) folkBegin(f *Folk) {
	for len(f.steps) > 0 {
		s := &f.steps[0]
		if !math.IsNaN(s.x) && math.Hypot(s.x-f.X, s.y-f.Y) > 0.3 {
			sp := r.folkSpeed(f, s.run)
			f.walking = true
			f.timer = int(math.Hypot(s.x-f.X, s.y-f.Y)/sp*1.6) + 12
			f.Act, f.Face = walkAct(s.run), math.NaN()
			return
		}
		f.walking = false
		if s.act == "" || (s.act == "chat" && !r.folkStartChat(f, s)) {
			f.steps = f.steps[1:]
			continue
		}
		f.timer = s.dur
		f.Act, f.Face = s.act, s.face
		return
	}
	f.walking = false
	f.Act, f.Face = "idle", math.NaN()
}

func walkAct(run bool) string {
	if run {
		return "play"
	}
	return "walk"
}

// folkDo advances f one tick along its current step.
func (r *Room) folkDo(f *Folk) {
	if len(f.steps) == 0 {
		return
	}
	s := &f.steps[0]
	if f.walking {
		f.Act, f.Face = walkAct(s.run), math.NaN()
		f.timer--
		if r.folkStep(f, s.x, s.y, r.folkSpeed(f, s.run)) {
			f.walking = false
			r.folkBegin(f)
		} else if f.stuck > 8 || f.timer <= 0 {
			// cannot get there: think again
			f.stuck = 0
			f.dropPlans()
			f.Act = "idle"
		}
		return
	}
	f.Act, f.Face = s.act, s.face // (again, after stopping to talk to a keeper)
	if s.act == "chat" {
		p := f.partner
		if p == nil || p.partner != f || p.state != fsTask || math.Hypot(p.X-f.X, p.Y-f.Y) > 2.6 {
			f.timer = 0
		} else {
			f.Face = math.Atan2(p.Y-f.Y, p.X-f.X)
		}
	}
	f.timer--
	if f.timer <= 0 {
		if s.act == "chat" {
			f.endChat()
			f.chatCD = 150 + rand.Intn(200)
		}
		f.steps = f.steps[1:]
		r.folkBegin(f)
	}
}

// folkStartChat: f has walked up to s.with — if they are still free, both
// turn to each other and talk a while.
func (r *Room) folkStartChat(f *Folk, s *folkStep) bool {
	p := s.with
	if p == nil || p == f || p.partner != nil || f.partner != nil || p.state != fsTask || p.walking ||
		!p.up(r.time) || math.Hypot(p.X-f.X, p.Y-f.Y) > 2.2 {
		return false
	}
	f.partner, p.partner = p, f
	p.steps = []folkStep{{x: math.NaN(), y: math.NaN(), act: "chat", dur: s.dur, with: f}}
	p.walking, p.timer = false, s.dur
	p.Act, p.Face = "chat", math.Atan2(f.Y-p.Y, f.X-p.X)
	s.face = math.Atan2(p.Y-f.Y, p.X-f.X)
	return true
}

// ── making plans ────────────────────────────────────────────────────────────

func between(a, b int) int { return a + rand.Intn(b-a+1) }

// ticksUntil: ticks from now until day time t1 (0 if it has passed).
func (r *Room) ticksUntil(t1 float64) int {
	return max(0, int((t1-r.time)/((TickMS/1000.0)/DayLengthSec)))
}

// plan builds a list of steps, routing each walk through the fire's ring when
// the straight way is blocked (every place was chosen a clear walk from the fire).
type plan struct {
	r     *Room
	v     *village
	x, y  float64
	steps []folkStep
}

func (r *Room) newPlan(f *Folk) *plan { return &plan{r: r, v: f.vil, x: f.X, y: f.Y} }

func (p *plan) to(x, y float64, run bool) {
	if !p.r.folkLineClear(p.x, p.y, x, y) {
		a := math.Atan2(y-p.v.fy, x-p.v.fx)
		wx, wy := p.v.fx+math.Cos(a)*2.4, p.v.fy+math.Sin(a)*2.4
		if p.r.folkWalkable(wx, wy) {
			p.steps = append(p.steps, folkStep{x: wx, y: wy, face: math.NaN(), run: run})
		}
	}
	p.x, p.y = x, y
}

// at: go to spot s and do act there for dur ticks.
func (p *plan) at(s folkSpot, act string, dur int) *plan {
	p.to(s.x, s.y, false)
	p.steps = append(p.steps, folkStep{x: s.x, y: s.y, act: act, dur: dur, face: s.face})
	return p
}

// run: dash to (x, y) at play.
func (p *plan) run(x, y float64) *plan {
	p.to(x, y, true)
	p.steps = append(p.steps, folkStep{x: x, y: y, face: math.NaN(), run: true})
	return p
}

func (p *plan) here(act string, dur int, face float64) *plan {
	p.steps = append(p.steps, folkStep{x: math.NaN(), y: math.NaN(), act: act, dur: dur, face: face})
	return p
}

func pick(s []folkSpot) (folkSpot, bool) {
	if len(s) == 0 {
		return folkSpot{}, false
	}
	return s[rand.Intn(len(s))], true
}

// live: a node spot whose node has not been harvested by a keeper.
func (r *Room) live(s []folkSpot) []folkSpot {
	out := s[:0:0]
	for _, p := range s {
		if p.node < 0 || !r.removedAt(p.node) {
			out = append(out, p)
		}
	}
	return out
}

// yardSpot: a random open spot in the yard, a clear walk from the fire.
func (r *Room) yardSpot(v *village, minR, maxR float64) (folkSpot, bool) {
	for range 8 {
		a, d := rand.Float64()*2*math.Pi, minR+rand.Float64()*(maxR-minR)
		x, y := v.fx+math.Cos(a)*d, v.fy+math.Sin(a)*d
		if r.folkWalkable(x, y) && r.folkLineClear(v.fx, v.fy, x, y) {
			return folkSpot{x: x, y: y, face: rand.Float64() * 2 * math.Pi, node: -1}, true
		}
	}
	return folkSpot{}, false
}

func (r *Room) seatOf(f *Folk) folkSpot {
	s := f.vil.seats[f.seat]
	return folkSpot{x: s[0], y: s[1], face: f.vil.faceFire(s[0], s[1]), node: -1}
}

// folkPlan decides what f does next.
func (r *Room) folkPlan(f *Folk) []folkStep {
	v, p := f.vil, r.newPlan(f)
	switch f.part {
	case partNight: // only the watchers are up
		if f.role == roleHunter {
			r.patrol(f, p, 2, 50, 90)
		} else {
			p.at(r.seatOf(f), "sit", between(140, 260))
		}
		return p.steps
	case partMeal, partEvening:
		if f.joins {
			switch {
			case f.role == roleElder && f.part == partEvening && rand.Float64() < 0.4:
				// stand up and speak to the gathering
				a := math.Atan2(v.seats[f.seat][1]-v.fy, v.seats[f.seat][0]-v.fx)
				x, y := v.fx+math.Cos(a)*1.2, v.fy+math.Sin(a)*1.2
				if r.folkWalkable(x, y) {
					p.at(folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1}, "speak", between(40, 70))
				}
				p.at(r.seatOf(f), "sit", between(80, 140))
			case f.role == roleChild && rand.Float64() < 0.45:
				r.play(f, p)
			case f.part == partMeal:
				// the meal: sit and eat till it is over, lingering a little
				p.at(r.seatOf(f), "sit", r.ticksUntil(folkMealEnd)+between(0, 40))
			case rand.Float64() < 0.2 && r.chat(f, p, 3.5):
			default:
				p.at(r.seatOf(f), "sit", between(150, 300))
			}
			return p.steps
		}
	}
	// working hours (and anyone sitting out a gathering)
	if rand.Float64() < 0.14 && r.chat(f, p, 9) {
		return p.steps
	}
	switch f.role {
	case roleElder:
		r.elderDay(f, p)
	case roleBuilder:
		r.builderDay(f, p)
	case roleForager:
		r.foragerDay(f, p)
	case roleHunter:
		r.hunterDay(f, p)
	case roleFisher:
		r.fisherDay(f, p)
	case roleChild:
		r.play(f, p)
	}
	if len(p.steps) == 0 { // nothing came of it: stand a while
		p.here("idle", between(15, 35), math.NaN())
	}
	return p.steps
}

// chat: go and talk to a free neighbour within rad.
func (r *Room) chat(f *Folk, p *plan, rad float64) bool {
	if f.chatCD > 0 {
		return false
	}
	var free []*Folk
	for _, o := range f.vil.members {
		if o != f && o.state == fsTask && o.partner == nil && o.chatCD == 0 && !o.walking && o.up(r.time) &&
			math.Hypot(o.X-f.X, o.Y-f.Y) < rad {
			free = append(free, o)
		}
	}
	if len(free) == 0 {
		return false
	}
	o := free[rand.Intn(len(free))]
	d := math.Max(math.Hypot(f.X-o.X, f.Y-o.Y), 0.01)
	x, y := o.X+(f.X-o.X)/d*1.1, o.Y+(f.Y-o.Y)/d*1.1
	if !r.folkWalkable(x, y) {
		x, y = o.X, o.Y
	}
	p.to(x, y, false)
	p.steps = append(p.steps, folkStep{x: x, y: y, act: "chat", dur: between(40, 90), face: math.NaN(), with: o})
	return true
}

func (r *Room) elderDay(f *Folk, p *plan) {
	v := f.vil
	switch roll := rand.Float64(); {
	case f.part == partMorning && roll < 0.6:
		// the morning rounds: call at a few huts, and a word with whoever is about
		huts := append([]int(nil), v.camps...)
		rand.Shuffle(len(huts), func(a, b int) { huts[a], huts[b] = huts[b], huts[a] })
		for _, ci := range huts[:min(len(huts), between(2, 3))] {
			p.at(v.doorstep[ci], "watch", between(18, 35))
		}
		p.at(r.seatOf(f), "sit", between(60, 120))
	case roll < 0.75 && len(r.live(v.bushes)) > 0 && f.Kind != "elder_woods":
		// herbs for the fire and the pot
		s, _ := pick(r.live(v.bushes))
		p.at(s, "collect", between(50, 80))
		p.at(r.seatOf(f), "sit", between(80, 140))
	case roll < 0.85 && f.Kind == "shaman":
		// the shaman's words over the fire
		a := rand.Float64() * 2 * math.Pi
		x, y := v.fx+math.Cos(a)*1.3, v.fy+math.Sin(a)*1.3
		if r.folkWalkable(x, y) {
			p.at(folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1}, "speak", between(40, 70))
		}
	default:
		p.at(r.seatOf(f), "sit", between(150, 260))
	}
}

func (r *Room) builderDay(f *Folk, p *plan) {
	v := f.vil
	trees := r.live(v.trees)
	own := v.walls[f.Camp]
	switch roll := rand.Float64(); {
	case roll < 0.4:
		// mend a hut: their own, or a neighbour's
		ci := f.Camp
		if rand.Float64() < 0.5 {
			ci = v.camps[rand.Intn(len(v.camps))]
		}
		if s, ok := pick(v.walls[ci]); ok {
			p.at(s, "build", between(70, 140))
			if rand.Float64() < 0.5 {
				if s2, ok := pick(v.walls[ci]); ok {
					p.at(s2, "build", between(50, 100))
				}
			}
		}
	case roll < 0.78 && len(trees) > 0:
		// a wood run: gather at a tree, stack it by the hut
		s, _ := pick(trees)
		p.at(s, "collect", between(40, 70))
		if w, ok := pick(own); ok {
			p.at(w, "collect", between(15, 25))
		} else {
			p.at(v.doorstep[f.Camp], "collect", between(15, 25))
		}
	case roll < 0.9:
		p.at(v.doorstep[f.Camp], "sit", between(40, 80)) // a rest on the doorstep
	default:
		if s, ok := r.yardSpot(v, 2, v.yardR); ok {
			p.at(s, "idle", between(20, 40))
		}
	}
}

func (r *Room) foragerDay(f *Folk, p *plan) {
	v := f.vil
	bushes := r.live(v.bushes)
	switch roll := rand.Float64(); {
	case roll < 0.5 && len(bushes) > 0:
		// pick a bush or two, then work it up on the doorstep
		s, _ := pick(bushes)
		p.at(s, "collect", between(45, 80))
		if rand.Float64() < 0.5 {
			s2, _ := pick(bushes)
			p.at(s2, "collect", between(40, 70))
		}
		p.at(v.doorstep[f.Camp], "craft", between(80, 150))
	case roll < 0.85:
		p.at(v.doorstep[f.Camp], "craft", between(100, 180))
	default:
		if s, ok := r.yardSpot(v, 2, v.yardR); ok {
			p.at(s, "collect", between(30, 50))
		}
	}
}

// patrol: walk n lookouts round the edge from the nearest one, watching outward at each.
func (r *Room) patrol(f *Folk, p *plan, n, minW, maxW int) {
	e := f.vil.edge
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
	if rand.Intn(2) == 0 {
		dir = -1
	}
	for range n {
		k = ((k+dir)%len(e) + len(e)) % len(e)
		p.at(e[k], "watch", between(minW, maxW))
	}
}

func (r *Room) hunterDay(f *Folk, p *plan) {
	v := f.vil
	switch roll := rand.Float64(); {
	case roll < 0.5:
		r.patrol(f, p, 3, 25, 50)
	case roll < 0.8 && len(v.far) > 0:
		// out on the hunt, and home with it
		s, _ := pick(v.far)
		p.at(s, "watch", between(30, 60))
		p.at(folkSpot{x: s.x, y: s.y, face: s.face + math.Pi, node: -1}, "collect", between(40, 60))
		p.at(v.doorstep[f.Camp], "collect", between(20, 30))
	default:
		p.at(v.doorstep[f.Camp], "collect", between(40, 70)) // mending gear on the doorstep
	}
}

func (r *Room) fisherDay(f *Folk, p *plan) {
	v := f.vil
	bushes := r.live(v.bushes)
	switch roll := rand.Float64(); {
	case roll < 0.55 && len(v.shore) > 0:
		s, _ := pick(v.shore)
		p.at(s, "collect", between(120, 220)) // fishing off the shore
		p.at(v.doorstep[f.Camp], "collect", between(20, 30))
	case roll < 0.8:
		r.patrol(f, p, 2, 25, 45)
	case len(bushes) > 0:
		s, _ := pick(bushes) // reeds for the roof
		p.at(s, "collect", between(50, 80))
		p.at(v.doorstep[f.Camp], "collect", between(25, 40))
	}
}

// play: the children's games.
func (r *Room) play(f *Folk, p *plan) {
	v := f.vil
	var kids, grown []*Folk
	for _, o := range v.members {
		if o == f || o.state != fsTask || !o.up(r.time) {
			continue
		}
		if o.child {
			kids = append(kids, o)
		} else {
			grown = append(grown, o)
		}
	}
	switch roll := rand.Float64(); {
	case roll < 0.45:
		// tag: dash at another child (or round the fire alone), then away
		if len(kids) > 0 {
			o := kids[rand.Intn(len(kids))]
			p.run(o.X+rand.Float64()-0.5, o.Y+rand.Float64()-0.5)
		}
		for range between(2, 3) {
			if s, ok := r.yardSpot(v, 1.8, v.yardR*0.7); ok {
				p.run(s.x, s.y)
			}
		}
		p.here("idle", between(8, 20), math.NaN())
	case roll < 0.6:
		p.at(r.seatOf(f), "sit", between(40, 80))
	case roll < 0.85 && len(grown) > 0:
		// tag after a grown-up and watch them work
		o := grown[rand.Intn(len(grown))]
		a := rand.Float64() * 2 * math.Pi
		x, y := o.X+math.Cos(a)*1.3, o.Y+math.Sin(a)*1.3
		if r.folkWalkable(x, y) {
			p.at(folkSpot{x: x, y: y, face: math.Atan2(o.Y-y, o.X-x), node: -1}, "watch", between(30, 50))
		}
	default:
		if s, ok := r.yardSpot(v, 1.8, v.yardR*0.7); ok {
			p.at(s, "collect", between(30, 50)) // digging in the dirt
		}
	}
}
