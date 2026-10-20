package room

import (
	"math"
	"sort"
)

// The village's work (folk_routine.go plans it, folk.go runs it): duties,
// loads carried, the woodpiles, the hunt, and the watch at the door.
//
// Duties come round each day and fall to someone (villageTick):
//
//	stoking   whoever is up first takes a log from a hut's pile to the fire
//	          and stokes it at dawn; the elder (or the watch) again at dusk
//	cooking   the cook of the day (the foragers in turn) fetches water from
//	          the shore, cooks at the fire through the late morning, and calls
//	          everyone in for the meal — those who hear it down tools
//	speaking  the elder's words at the evening fire, the others listening
//	calling   a parent calls each child in at the child's bedtime
//
// Work that moves something carries it, and it shows: wood felled goes onto
// the hut's pile (the pile is what the fire is fed from and what the walls are
// mended with), the catch and the game go to the fire for the pot, herbs and
// reeds to the doorstep and the roof, water to the pot. A plan cut short
// still sees its load delivered.
//
// The hunt goes out in pairs (folkParty): the two walk out side by side at the
// slower one's pace, wait for each other at each stage — the stalk, the kill —
// and come home together with the game.
//
// When a creature comes for the village its guards stand between it and the
// huts (folkGuard), spread across the way it is coming, facing it.

const folkPileMax = 8

type folkDuty uint8

const (
	dutyNone folkDuty = iota
	dutyStoke
	dutyCook
	dutySpeak
	dutyCall
)

// stepFx is what doing a step changes, when it ends (or, for a call, begins).
type stepFx uint8

const (
	fxNone     stepFx = iota
	fxTake            // pick up s.item
	fxDrop            // put down what they carry (used up, or worked)
	fxStack           // their wood goes on the hut's pile
	fxFromPile        // take a log from the hut's pile
	fxFeed            // their wood (if any) goes on the fire
	fxLarder          // their load goes to the pot
	fxCooked          // the meal is ready
	fxCall            // the cook calls everyone in
	fxCallKid         // a parent calls a child in
	fxSpeak           // the elder has the floor
)

// puts: the step leaves their hands empty.
func (x stepFx) puts() bool {
	return x == fxDrop || x == fxStack || x == fxFeed || x == fxLarder
}

func (s *folkStep) take(item string) *folkStep {
	s.fx, s.item = fxTake, item
	return s
}

func (v *village) stack(ci int) {
	v.pile[ci] = min(folkPileMax, v.pile[ci]+1)
}

// stepStarts: the act of s begins.
func (r *Room) stepStarts(f *Folk, s *folkStep) {
	v := f.vil
	if s.until > 0 { // as long as it takes until then, and s.dur more
		f.timer = r.ticksUntil(s.until) + s.dur
	}
	if s.fx.puts() { // the load goes down first, then the work
		r.putDown(f, s)
		return
	}
	switch s.fx {
	case fxCall:
		v.called = true
	case fxCallKid:
		if s.with != nil {
			s.with.called = true
		}
	case fxSpeak:
		v.speaker = f
	}
}

// stepDone: the act of s is over.
func (r *Room) stepDone(f *Folk, s *folkStep) {
	v := f.vil
	switch s.fx {
	case fxTake:
		f.Item = s.item
	case fxFromPile:
		if v.pile[s.camp] > 0 {
			v.pile[s.camp]--
			f.Item = "wood"
		}
	case fxFeed:
		v.fire = math.Min(1, v.fire+0.15) // stoked
	case fxCooked:
		f.needs.drive = 0
	case fxSpeak:
		if v.speaker == f {
			v.speaker = nil
		}
		v.spoke = true
	}
	if s.act == "sit" && f.doing == doGather && f.part == partMeal {
		f.needs.hunger = 0 // a full belly
	}
}

// putDown: the load of f goes where step s takes it.
func (r *Room) putDown(f *Folk, s *folkStep) {
	v := f.vil
	switch s.fx {
	case fxStack:
		if f.Item == "wood" {
			v.stack(s.camp)
		}
	case fxFeed:
		if f.Item == "wood" {
			v.fire = math.Min(1, v.fire+0.35)
		}
	case fxLarder:
		if f.Item != "" {
			v.larder++
		}
	}
	f.Item = ""
}

// ── the day's duties ────────────────────────────────────────────────────────

// villageTick is the village's own business each tick: its ground, its
// danger, its news, and the day's duties coming round.
func (r *Room) villageTick(v *village, t float64) {
	v.navBudget = navBudget
	// the clock moved under them (the tester panel's `time`, a save loaded at
	// another hour): plans made for the old hour go, and the day's duties are
	// drawn again. A tick moves it 0.00022; midnight wraps it by 1.
	if d := math.Abs(t - v.clock); d > 0.004 && d < 0.996 {
		v.day = -1
		for _, f := range v.members {
			if f.state == fsTask {
				f.dropPlans()
				f.part, f.gest.tk, f.duty = partNone, 0, dutyNone
			}
		}
	}
	v.clock = t
	if fresh := r.navRefresh(v.nav, false); len(fresh) > 0 {
		r.folkOnlooker(v, fresh)
	}
	if v.onlookCD > 0 {
		v.onlookCD--
	}
	v.fire = math.Max(0, v.fire-1.0/3200)

	// danger: the nearest creature to the fire raises the alarm, and keeps it
	// raised until there is none near
	v.threat = nil
	rad := folkAlarmR
	if v.alarm {
		rad = folkAlarmClearR
	}
	for _, c := range r.creOrder {
		if d := math.Hypot(c.X-v.fx, c.Y-v.fy); d < rad {
			v.threat, rad = c, d
		}
	}
	if v.threat != nil && !v.alarm {
		r.pickGuards(v)
	}
	v.alarm = v.threat != nil
	if v.alarm && (r.tickN%5 == 0 || len(v.posts) != len(v.guards)) {
		v.placeGuards(v.threat) // it moves: so do the posts, now and then
	}
	if !v.alarm {
		v.guards = v.guards[:0:0]
		for _, f := range v.members {
			if f.state == fsGuard {
				v.guards = append(v.guards, f) // still standing down
			}
		}
	}

	// a monolith lit since they last looked: the coming evening is a feast
	for k, lit := range r.mono {
		if lit && !v.monoSeen[k] {
			v.monoSeen[k], v.feast = true, r.day
			if t >= folkEveningAt+0.02 || t < NightEnd {
				v.feast = r.day + 1 // too late tonight: tomorrow evening
				if t < NightEnd {
					v.feast = r.day
				}
			}
		}
	}

	// a new day: draw its duties
	if (v.day != r.day && t >= NightEnd && t < 0.5) || (v.day < 0 && !isNightAt(t)) {
		v.day = r.day
		v.cookSet, v.called, v.stoked, v.duskSet, v.spoke, v.speaker, v.larder = false, false, false, false, false, nil, 0
		v.cook = v.cooks[r.day%len(v.cooks)]
		v.stoker = nil
		for _, f := range v.members {
			f.heard, f.called = false, false
			if !f.child && f != v.cook && (v.stoker == nil || f.wake < v.stoker.wake) {
				v.stoker = f
			}
		}
		v.duskStoker = v.elder
		if v.duskStoker == nil {
			v.duskStoker = v.stoker
		}
	}
	give := func(f *Folk, d folkDuty, cap int) bool {
		if f == nil || f.state != fsTask || f.party != nil || f.duty != dutyNone || !r.folkUp(f, t) {
			return false
		}
		f.duty = d
		if f.doing != doDuty {
			r.folkWrapUp(f, cap)
		}
		return true
	}
	switch {
	case !v.stoked && t < 0.25 && give(v.stoker, dutyStoke, 10):
		v.stoked = true
	case !v.cookSet && t >= folkCookAt && t < folkCallAt && give(v.cook, dutyCook, 30):
		v.cookSet = true
	case !v.duskSet && t >= 0.598 && t < folkEveningAt && give(v.duskStoker, dutyStoke, 20):
		v.duskSet = true
	case !v.spoke && v.speaker == nil && v.elder != nil && v.elder.joins && t >= folkSpeechAt && t < v.elder.bed &&
		dayPartAt(t) == partEvening && v.elder.duty == dutyNone && v.elder.doing != doDuty && give(v.elder, dutySpeak, 4):
	}
	if t > folkSpeechAt+0.03 {
		v.spoke = true // too late for words tonight
	}
	// the children's bedtime: a parent calls them in
	for _, k := range v.members {
		if !k.child || k.called || k.state != fsTask || t < k.bed-0.006 || t >= k.bed {
			continue
		}
		k.called = true
		var best *Folk
		for _, p := range k.parents {
			if p.state == fsTask && p.duty == dutyNone && p.party == nil && r.folkUp(p, t) &&
				(best == nil || math.Hypot(p.X-k.X, p.Y-k.Y) < math.Hypot(best.X-k.X, best.Y-k.Y)) {
				best = p
			}
		}
		if best != nil && give(best, dutyCall, 6) {
			best.callKid = k
		}
	}
}

// feast: this evening is a feast at f's village (a monolith was lit).
func (r *Room) feast(v *village) bool {
	return v.feast == r.day && r.time >= folkEveningAt-0.02 && r.time < NightStart
}

// planDuty lays out duty d for f.
func (r *Room) planDuty(f *Folk, p *plan, d folkDuty) {
	v := f.vil
	switch d {
	case dutyStoke:
		r.planTend(f, p)
	case dutyCook:
		if r.ticksUntil(folkCallAt) < 40 {
			return
		}
		// water from the shore, if there is time to fetch it and still cook
		if s, ok := r.pickNear(v, v.shore, f.X, f.Y); ok &&
			(math.Hypot(s.x-f.X, s.y-f.Y)+math.Hypot(s.x-v.fx, s.y-v.fy))*1.3/r.folkSpeed(f, false) < float64(r.ticksUntil(folkCallAt)-120) {
			p.at(s, "collect", v.between(20, 30)).take("water")
		}
		spot := r.seatOf(f)
		if s, ok := r.pick(v, v.ring); ok {
			spot = s
		}
		p.at(spot, "cook", 30).fx = fxLarder // the water into the pot
		c := p.at(spot, "cook", 20) // until it is time to call (stepStarts)
		c.fx, c.until = fxCooked, folkCallAt
		p.here("call", 16, spot.face+math.Pi).fx = fxCall
		m := p.at(r.seatOf(f), "sit", v.between(0, 40))
		m.until = folkMealEnd
		f.joins, f.heard = true, true
	case dutySpeak:
		a := math.Atan2(v.seats[f.seat][1]-v.fy, v.seats[f.seat][0]-v.fx)
		x, y := v.fx+math.Cos(a)*1.25, v.fy+math.Sin(a)*1.25
		if !v.reach(x, y) {
			x, y = v.seats[f.seat][0], v.seats[f.seat][1]
		}
		p.at(folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1}, "speak", v.between(70, 100)).fx = fxSpeak
		p.at(r.seatOf(f), "sit", v.between(100, 180))
		f.doing = doGather
	case dutyCall:
		k := f.callKid
		f.callKid = nil
		if k == nil {
			return
		}
		face := math.Atan2(k.Y-f.Y, k.X-f.X)
		s := p.here("call", 16, face)
		s.fx, s.with = fxCallKid, k
	}
}

// ── loads ───────────────────────────────────────────────────────────────────

// deliver sees what f carries to where it belongs.
func (r *Room) deliver(f *Folk, p *plan) {
	v := f.vil
	switch f.Item {
	case "wood":
		if f.role != roleBuilder { // not theirs to stack: it goes on the fire
			s, ok := r.pick(v, v.ring)
			if !ok {
				s = r.seatOf(f)
			}
			p.at(s, "tend", v.between(30, 50)).fx = fxFeed
			return
		}
		s := p.at(v.pileAt[f.Camp], "collect", v.between(10, 16))
		s.fx, s.camp = fxStack, f.Camp
	case "herbs":
		p.at(v.doorstep[f.Camp], "craft", v.between(60, 110)).fx = fxDrop
	case "reeds", "stone":
		if w, ok := r.pick(v, v.walls[f.Camp]); ok {
			p.at(w, "build", v.between(50, 90)).fx = fxDrop
		} else {
			p.at(v.doorstep[f.Camp], "collect", 12).fx = fxDrop
		}
	default: // the catch, the game, berries, water: to the pot
		s, ok := r.pick(v, v.ring)
		if !ok {
			s = v.doorstep[f.Camp]
		}
		p.at(s, "collect", v.between(10, 16)).fx = fxLarder
	}
}

// fromPile: walk to the fullest woodpile near f and take a log, true if there was one.
func (r *Room) fromPile(f *Folk, p *plan) bool {
	v := f.vil
	best, bs := -1, -1.0
	for _, ci := range v.camps {
		if v.pile[ci] == 0 {
			continue
		}
		s := v.pileAt[ci]
		score := float64(v.pile[ci]) - math.Hypot(s.x-p.x, s.y-p.y)*0.3
		if ci == f.Camp {
			score += 1
		}
		if score > bs {
			best, bs = ci, score
		}
	}
	if best < 0 {
		return false
	}
	s := p.at(v.pileAt[best], "collect", v.between(8, 14))
	s.fx, s.camp = fxFromPile, best
	return true
}

// planTend: a log to the fire, and a while stoking it.
func (r *Room) planTend(f *Folk, p *plan) {
	v := f.vil
	if !r.fromPile(f, p) && f.role == roleBuilder {
		if s, ok := r.pickNear(v, v.trees, v.fx, v.fy); ok {
			p.at(s, "collect", v.between(30, 50)).take("wood")
		}
	}
	s, ok := r.pick(v, v.ring)
	if !ok {
		s = r.seatOf(f)
	}
	p.at(s, "tend", v.between(40, 65)).fx = fxFeed
}

// planWood: a wood run or two, stacked on the hut's pile.
func (r *Room) planWood(f *Folk, p *plan) {
	v := f.vil
	for k := range v.between(1, 2) {
		s, ok := r.pickNear(v, v.trees, p.x, p.y)
		if !ok || (k > 0 && v.pile[f.Camp]+k >= folkPileMax) {
			break
		}
		p.at(s, "collect", v.between(40, 70)).take("wood")
		st := p.at(v.pileAt[f.Camp], "collect", v.between(10, 16))
		st.fx, st.camp = fxStack, f.Camp
	}
}

// planMend: wood from the pile (or a tree) to a hut wall, and the work.
func (r *Room) planMend(f *Folk, p *plan) {
	v := f.vil
	ci := f.Camp
	if v.chance(0.35) {
		ci = v.camps[v.intn(len(v.camps))] // a neighbour's
	}
	w, ok := r.pick(v, v.walls[ci])
	if !ok {
		return
	}
	if v.pile[ci] > 0 {
		s := p.at(v.pileAt[ci], "collect", v.between(8, 14))
		s.fx, s.camp = fxFromPile, ci
	} else if t, ok := r.pickNear(v, v.trees, w.x, w.y); ok {
		p.at(t, "collect", v.between(40, 60)).take("wood")
	}
	p.at(w, "build", v.between(70, 140)).fx = fxDrop
	if w2, ok := r.pick(v, v.walls[ci]); ok && v.chance(0.5) {
		p.at(w2, "build", v.between(40, 90))
	}
}

// planStone: where no trees grow the walls are mended with stone.
func (r *Room) planStone(f *Folk, p *plan) {
	v := f.vil
	s, ok := r.pickNear(v, v.rocks, f.X, f.Y)
	if !ok {
		return
	}
	w, ok := r.pick(v, v.walls[f.Camp])
	if !ok {
		return
	}
	p.at(s, "collect", v.between(50, 80)).take("stone")
	p.at(w, "build", v.between(60, 110)).fx = fxDrop
}

// planForage: berries for the pot before the meal, herbs to work up after.
func (r *Room) planForage(f *Folk, p *plan) {
	v := f.vil
	item := "herbs"
	if f.part == partMorning {
		item = "berries"
	}
	s, ok := r.pickNear(v, v.bushes, f.X, f.Y)
	if !ok {
		return
	}
	p.at(s, "collect", v.between(45, 80))
	if s2, ok := r.pickNear(v, v.bushes, s.x, s.y); ok && v.chance(0.5) {
		p.at(s2, "collect", v.between(40, 70))
	}
	p.last().take(item)
	r.deliverPlan(f, p, item)
}

// deliverPlan: the steps that see item (picked up by the last step) home.
func (r *Room) deliverPlan(f *Folk, p *plan, item string) {
	was := f.Item
	f.Item = item
	r.deliver(f, p)
	f.Item = was
}

func (r *Room) planWater(f *Folk, p *plan) {
	v := f.vil
	s, ok := r.pickNear(v, v.shore, f.X, f.Y)
	if !ok {
		return
	}
	p.at(s, "collect", v.between(20, 35)).take("water")
	r.deliverPlan(f, p, "water")
}

func (r *Room) planFish(f *Folk, p *plan) {
	v := f.vil
	s, ok := r.pick(v, v.shore)
	if !ok {
		return
	}
	p.at(s, "collect", v.between(120, 220)).take("fish") // fishing off the shore
	r.deliverPlan(f, p, "fish")
}

func (r *Room) planReeds(f *Folk, p *plan) {
	v := f.vil
	s, ok := r.pickNear(v, v.bushes, f.X, f.Y)
	if !ok {
		return
	}
	p.at(s, "collect", v.between(50, 80)).take("reeds")
	r.deliverPlan(f, p, "reeds")
}

// ── the hunt, in pairs ──────────────────────────────────────────────────────

// folkParty is a few folk out together: they keep the slower one's pace and
// wait for each other at the meeting points of their steps.
type folkParty struct {
	members []*Folk
	pace    float64
	gather  bool // walking to the fire together (folk_social.go)
}

// ready: everyone in the party has reached meeting point k.
func (p *folkParty) ready(k int8) bool {
	for _, m := range p.members {
		if m.syncAt < k {
			return false
		}
	}
	return true
}

func newParty(fs ...*Folk) *folkParty {
	p := &folkParty{members: fs, pace: 2}
	for _, f := range fs {
		f.party, f.syncAt, f.waitTk = p, 0, 0
		p.pace = math.Min(p.pace, f.pace)
	}
	return p
}

// leaveParty takes f out of its party; a party of one is no party.
func (f *Folk) leaveParty() {
	p := f.party
	if p == nil {
		return
	}
	f.party = nil
	for k, m := range p.members {
		if m == f {
			p.members = append(p.members[:k:k], p.members[k+1:]...)
			break
		}
	}
	if len(p.members) < 2 {
		for _, m := range p.members {
			m.party = nil
		}
		p.members = nil
	}
}

// huntMate is a free hunter (or fisher) for f to go out with, its best friend first.
func (r *Room) huntMate(f *Folk) *Folk {
	v := f.vil
	var best *Folk
	for _, o := range v.members {
		if o == f || (o.role != roleHunter && o.role != roleFisher) || o.state != fsTask || o.party != nil ||
			o.partner != nil || o.duty != dutyNone || o.doing == doDuty || o.doing == doGather || o.waiting ||
			o.Item != "" || !r.folkUp(o, r.time) || math.Hypot(o.X-f.X, o.Y-f.Y) > 14 {
			continue
		}
		if best == nil || v.affinity(f, o) > v.affinity(f, best) {
			best = o
		}
	}
	return best
}

// planHunt: out to the hunting grounds with a mate, the stalk, the kill, and
// home together with the game; false if nobody is free to go.
func (r *Room) planHunt(f *Folk, p *plan) bool {
	v := f.vil
	o := r.huntMate(f)
	s, ok := r.pick(v, v.far)
	if o == nil || !ok {
		return false
	}
	// side by side out there, a body apart
	lx, ly := -math.Sin(s.face), math.Cos(s.face)
	ox, oy := s.x+lx*0.9, s.y+ly*0.9
	if !v.reach(ox, oy) {
		ox, oy = s.x-lx*0.9, s.y-ly*0.9
	}
	if !v.reach(ox, oy) {
		return false
	}
	ring := r.seatOf(f)
	if rs, ok := r.pick(v, v.ring); ok {
		ring = rs
	}
	// they meet where f stands, go out side by side, stalk, make the kill and
	// come home together: each stage waits for the other
	mx, my := f.X+lx*0.9, f.Y+ly*0.9
	if !v.reach(mx, my) {
		mx, my = f.X-lx*0.9, f.Y-ly*0.9
	}
	if !v.reach(mx, my) {
		mx, my = f.X, f.Y
	}
	ring2 := ring
	for k := range 8 {
		a := float64(k) * math.Pi / 4
		if x, y := ring.x+math.Cos(a)*0.8, ring.y+math.Sin(a)*0.8; v.reach(x, y) && math.Hypot(x-v.fx, y-v.fy) > 0.9 {
			ring2 = folkSpot{x: x, y: y, face: v.faceFire(x, y), node: -1}
			break
		}
	}
	steps := func(f *Folk, mx, my, x, y float64, ring folkSpot) []folkStep {
		q := r.newPlan(f)
		q.steps = append(q.steps, folkStep{x: mx, y: my, act: "watch", dur: 4, face: math.NaN(), sync: 1, watchI: -1})
		q.steps = append(q.steps, folkStep{x: x, y: y, act: "watch", dur: v.between(40, 70), face: s.face, sync: 2, watchI: -1}) // the stalk
		q.here("collect", v.between(40, 60), s.face).take("game") // the kill
		q.last().sync = 3
		q.at(ring, "collect", v.between(10, 16)).fx = fxLarder
		q.last().sync = 4 // home together
		q.last().leave = true
		return q.steps
	}
	o.dropPlans()
	newParty(f, o)
	p.steps = steps(f, math.NaN(), math.NaN(), s.x, s.y, ring)
	o.steps = steps(o, mx, my, ox, oy, ring2)
	o.doing = doHunt
	o.last = [3]activity{doHunt, o.last[0], o.last[1]}
	r.folkBegin(o)
	return true
}

// folkWaitPose: waiting for the party, facing the nearest of them.
func (r *Room) folkWaitPose(f *Folk) {
	f.Act, f.Face = "idle", math.NaN()
	if f.party == nil {
		return
	}
	bd := math.Inf(1)
	for _, m := range f.party.members {
		if d := math.Hypot(m.X-f.X, m.Y-f.Y); m != f && d < bd {
			bd, f.Face = d, math.Atan2(m.Y-f.Y, m.X-f.X)
		}
	}
	if bd > 1.5 {
		f.Act = "watch"
	}
}

// ── the watch at the door ───────────────────────────────────────────────────

// pickGuards: who stands between a threat and the huts — the hunters first,
// then the watch and the watchers, then (in a village with none of those to
// spare) the strongest grown-ups; never a child, never the cook at the pot.
func (r *Room) pickGuards(v *village) {
	v.guards = v.guards[:0]
	type cand struct {
		f    *Folk
		rank float64
	}
	var cs []cand
	for _, f := range v.members {
		if f.child || f.state == fsHidden || f.state == fsFlee || f.vil != v {
			continue
		}
		rank := 0.0
		switch {
		case f.role == roleHunter || f.role == roleFisher:
			rank = 3
		case f.watcher || r.onDuty(f, r.time):
			rank = 2
		case f.role == roleBuilder:
			rank = 1
		default:
			continue
		}
		rank -= math.Hypot(f.X-v.fx, f.Y-v.fy) * 0.02
		cs = append(cs, cand{f, rank})
	}
	sort.SliceStable(cs, func(a, b int) bool { return cs[a].rank > cs[b].rank })
	for _, c := range cs[:min(len(cs), folkMaxGuards)] {
		v.guards = append(v.guards, c.f)
	}
}

func (v *village) isGuard(f *Folk) bool {
	for _, g := range v.guards {
		if g == f {
			return true
		}
	}
	return false
}

func (v *village) dropGuard(f *Folk) {
	for k, g := range v.guards {
		if g == f {
			v.guards = append(v.guards[:k:k], v.guards[k+1:]...)
			if k < len(v.posts) {
				v.posts = append(v.posts[:k:k], v.posts[k+1:]...)
			}
			return
		}
	}
}

// placeGuards works out where each guard stands against creature c: on the
// way from the fire to it, inside the yard, spread across it a body and more
// apart — trying nearer and farther along the line, and wider and narrower,
// before settling for the nearest open ground (a hut can stand on the line).
func (v *village) placeGuards(c *Creature) {
	a := math.Atan2(c.Y-v.fy, c.X-v.fx)
	ca, sa := math.Cos(a), math.Sin(a)
	d := math.Min(v.yardR*0.6, math.Max(1.8, math.Hypot(c.X-v.fx, c.Y-v.fy)-folkGuardBackR-1.5))
	v.posts = v.posts[:0]
	clearOf := func(x, y float64) bool {
		for _, p := range v.posts {
			if math.Hypot(p.x-x, p.y-y) < 1.1 {
				return false
			}
		}
		return v.reach(x, y)
	}
	for k := range v.guards {
		off := (float64(k) - float64(len(v.guards)-1)/2) * 1.4
		at := navPt{math.NaN(), 0}
	find:
		for _, dd := range [6]float64{0, -0.8, 0.8, -1.6, 1.6, -2.4} {
			for _, ow := range [4]float64{1, 1.5, 0.6, 2.2} {
				o := off * ow
				if off == 0 && ow != 1 {
					o = (ow - 1) * 1.4
				}
				x, y := v.fx+ca*(d+dd)-sa*o, v.fy+sa*(d+dd)+ca*o
				if clearOf(x, y) {
					at = navPt{x, y}
					break find
				}
			}
		}
		if math.IsNaN(at.x) {
			x, y := v.fx+ca*d-sa*off, v.fy+sa*d+ca*off
			for _, rr := range [3]float64{0.8, 1.6, 2.4} {
				for j := range 8 {
					b := float64(j) * math.Pi / 4
					if nx, ny := x+math.Cos(b)*rr, y+math.Sin(b)*rr; clearOf(nx, ny) && math.IsNaN(at.x) {
						at = navPt{nx, ny}
					}
				}
			}
		}
		if math.IsNaN(at.x) {
			g := v.guards[k]
			at = navPt{g.X, g.Y}
		}
		v.posts = append(v.posts, at)
	}
}

// guardPost is where guard f stands (placeGuards).
func (v *village) guardPost(f *Folk) (float64, float64) {
	for k, g := range v.guards {
		if g == f && k < len(v.posts) {
			return v.posts[k].x, v.posts[k].y
		}
	}
	return f.X, f.Y
}

// ── the night ───────────────────────────────────────────────────────────────

// planWatch: the watch on duty keeps the edge, the fire, and its light. (Anyone
// else up at night — just come out after a scare at dusk — sits by the fire.)
func (r *Room) planWatch(f *Folk, p *plan) {
	v := f.vil
	if !r.onDuty(f, r.time) {
		p.at(r.seatOf(f), "sit", v.between(40, 80))
		return
	}
	f.doing = doWatch
	f.Item = "torch"
	switch roll := v.roll(); {
	case roll < 0.25 && v.fire < 0.6:
		s, ok := r.pick(v, v.ring)
		if !ok {
			s = r.seatOf(f)
		}
		p.at(s, "tend", v.between(30, 50)).fx = fxFeed
	case roll < 0.65 && len(v.edge) > 0 && f.role != roleElder:
		r.patrol(f, p, 2, 50, 90)
	default:
		p.at(r.seatOf(f), "sit", v.between(140, 260))
	}
}
