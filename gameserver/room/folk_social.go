package room

import (
	"math"

	"hearth/gameserver/world"
)

// The folk among themselves and with the keepers (folk.go, folk_routine.go).
//
// Among themselves: they chat with the friend they like best of those who are
// free and near (village.aff, folk_life.go), and like each other a little more
// for it; a neighbour at work looks up and waves when a friend goes by; two
// who are close by when the meal or the evening comes walk to the fire
// together; at the fire the elder speaks and the rest turn to listen; the
// evening after a monolith is lit the village cheers and dances round the fire.
//
// With the keepers: a keeper coming into the yard is watched — those standing
// about turn to face them, and one and another wave — and talked to close up
// (folk.go). A curious child may tag after a keeper round the yard a while,
// keeping a few steps off. A keeper who builds near the village draws its most
// curious soul over to stand and watch.

// chat: go and talk to the friendliest free neighbour within rad.
func (r *Room) chat(f *Folk, p *plan, rad float64) bool {
	if f.chatCD > 0 {
		return false
	}
	v := f.vil
	var o *Folk
	bs := -1.0
	for _, c := range v.members {
		if c == f || c.state != fsTask || c.partner != nil || c.chatCD > 0 || c.walking || c.waiting || c.party != nil ||
			c.duty != dutyNone || c.doing == doDuty || c.gest.tk > 0 || c.follow != nil || c.Item != "" || !r.folkUp(c, r.time) {
			continue
		}
		d := math.Hypot(c.X-f.X, c.Y-f.Y)
		if d > rad {
			continue
		}
		if s := v.affinity(f, c)*1.5 - d*0.05 + v.roll()*0.4; s > bs {
			o, bs = c, s
		}
	}
	if o == nil {
		return false
	}
	d := math.Max(math.Hypot(f.X-o.X, f.Y-o.Y), 0.01)
	base := math.Atan2(f.Y-o.Y, f.X-o.X)
	x, y := o.X+(f.X-o.X)/d*1.1, o.Y+(f.Y-o.Y)/d*1.1
	for _, da := range [5]float64{0, 0.7, -0.7, 1.4, -1.4} {
		x, y = o.X+math.Cos(base+da)*1.1, o.Y+math.Sin(base+da)*1.1
		if v.reach(x, y) {
			break
		}
	}
	if !v.reach(x, y) {
		return false
	}
	if x, y = r.freeSpot(f, x, y); math.Hypot(x-o.X, y-o.Y) > 2 {
		return false
	}
	p.steps = append(p.steps, folkStep{x: x, y: y, act: "chat", dur: v.between(40, 90), face: math.NaN(), with: o, watchI: -1})
	return true
}

// folkGreet: f walking by — a friend standing at their work looks up and waves.
func (r *Room) folkGreet(f *Folk) {
	v := f.vil
	for _, o := range v.members {
		if o == f || o.state != fsTask || o.walking || o.waiting || o.partner != nil || o.gest.tk > 0 || o.greetCD > 0 ||
			math.Abs(o.X-f.X) > 1.8 || math.Abs(o.Y-f.Y) > 1.8 || math.Hypot(o.X-f.X, o.Y-f.Y) > 1.8 || v.affinity(f, o) < 0.3 {
			continue
		}
		switch o.Act {
		case "idle", "watch", "collect", "craft", "build", "tend":
		default:
			continue
		}
		o.gest = folkGesture{act: "wave", tk: 7, flip: -1, face: math.Atan2(f.Y-o.Y, f.X-o.X)}
		cd := 350 + v.intn(200)
		o.greetCD, f.greetCD = cd, cd
		return
	}
}

// nearestKeeper is the surface keeper nearest f within rad, or nil.
func (r *Room) nearestKeeper(f *Folk, rad float64) *Player {
	var best *Player
	for _, q := range r.playerOrder {
		if q.Z != 0 {
			continue
		}
		if d := math.Hypot(q.X-f.X, q.Y-f.Y); d < rad {
			best, rad = q, d
		}
	}
	return best
}

// folkNotice: a keeper coming near — now and then f stops and waves. True if f
// is waving this tick.
func (r *Room) folkNotice(f *Folk) bool {
	if f.noticeCD > 0 || f.state != fsTask || f.partner != nil || f.waiting || f.party != nil || f.duty != dutyNone ||
		f.doing == doDuty || f.follow != nil {
		return false
	}
	q := r.nearestKeeper(f, folkNoticeR)
	if q == nil {
		return false
	}
	v := f.vil
	f.noticeCD = 300 + v.intn(240)
	if !v.chance(0.3 + f.pers.social*0.45) {
		return false
	}
	f.gest = folkGesture{act: "wave", tk: 10, flip: -1, face: math.Atan2(q.Y-f.Y, q.X-f.X)}
	f.gesture()
	return true
}

// folkFaceKeeper: standing about with a keeper coming near, f turns to watch them.
func (r *Room) folkFaceKeeper(f *Folk) {
	if f.walking || f.state != fsTask {
		return
	}
	switch f.Act {
	case "idle", "watch", "sit":
	default:
		return
	}
	if q := r.nearestKeeper(f, folkNoticeR); q != nil {
		f.Face = math.Atan2(q.Y-f.Y, q.X-f.X)
	}
}

// folkMaybeFollow: a curious child with a keeper in the yard may tag after them.
func (r *Room) folkMaybeFollow(f *Folk) {
	v := f.vil
	if !f.child || f.follow != nil || f.followCD > 0 || f.pers.curious < 0.4 || f.state != fsTask || f.party != nil ||
		f.partner != nil || f.duty != dutyNone || f.doing == doGather || f.called {
		return
	}
	q := r.nearestKeeper(f, 7)
	if q == nil || math.Hypot(q.X-v.fx, q.Y-v.fy) > v.yardR+4 || !v.chance(1.0/150) {
		return
	}
	f.dropPlans()
	f.follow, f.followTk, f.followAt = q, 150+v.intn(150), navPt{math.NaN(), 0}
}

// folkFollow: child f tags after its keeper, a few steps off; false once it is over.
func (r *Room) folkFollow(f *Folk, t float64) bool {
	q, v := f.follow, f.vil
	f.followTk--
	if f.followTk <= 0 || r.players[q.S.ID] != q || q.Z != 0 || math.Hypot(q.X-v.fx, q.Y-v.fy) > v.yardR+7 ||
		t >= f.bed-0.008 || f.called {
		f.follow, f.followCD = nil, 900
		f.dropPlans()
		f.part = partNone // back to whatever the day holds
		return false
	}
	d := math.Hypot(q.X-f.X, q.Y-f.Y)
	if d <= 3.2 {
		f.Act, f.Face = "watch", math.Atan2(q.Y-f.Y, q.X-f.X)
		return true
	}
	// a place a few steps behind them, re-aimed when they have moved on
	if math.IsNaN(f.followAt.x) || math.Hypot(f.followAt.x-q.X, f.followAt.y-q.Y) > 3.4 {
		base := math.Atan2(f.Y-q.Y, f.X-q.X)
		f.followAt = navPt{math.NaN(), 0}
		for _, da := range [5]float64{0, 0.6, -0.6, 1.2, -1.2} {
			x, y := q.X+math.Cos(base+da)*2.6, q.Y+math.Sin(base+da)*2.6
			if v.reach(x, y) {
				f.followAt = navPt{x, y}
				break
			}
		}
		if math.IsNaN(f.followAt.x) {
			f.followTk = 0
			return true
		}
	}
	if r.folkGo(f, f.followAt.x, f.followAt.y, folkPlaySp*0.85) {
		f.Act, f.Face = "watch", math.Atan2(q.Y-f.Y, q.X-f.X)
	} else {
		f.Act, f.Face = "play", math.NaN()
		if f.nav.none || f.stuck > 10 {
			f.followTk = 0
		}
	}
	return true
}

// folkOnlooker: a keeper has built something near the village — its most
// curious free soul goes over to watch.
func (r *Room) folkOnlooker(v *village, fresh []int) {
	if v.onlookCD > 0 {
		return
	}
	for _, i := range fresh {
		bx, by := float64(i%world.SIZE)+0.5, float64(i/world.SIZE)+0.5
		if math.Hypot(bx-v.fx, by-v.fy) > v.yardR+10 {
			continue
		}
		var best *Folk
		for _, f := range v.members {
			if f.state != fsTask || f.party != nil || f.partner != nil || f.duty != dutyNone || f.doing == doDuty ||
				f.doing == doGather || f.follow != nil || f.Item != "" || f.waiting || !r.folkUp(f, r.time) ||
				math.Hypot(f.X-bx, f.Y-by) > 22 {
				continue
			}
			if best == nil || f.pers.curious > best.pers.curious {
				best = f
			}
		}
		if best == nil || best.pers.curious < 0.25 {
			return
		}
		base := math.Atan2(best.Y-by, best.X-bx)
		for _, rr := range [3]float64{2.4, 3.0, 1.8} {
			for _, da := range [5]float64{0, 0.7, -0.7, 1.4, -1.4} {
				x, y := bx+math.Cos(base+da)*rr, by+math.Sin(base+da)*rr
				if !v.reach(x, y) {
					continue
				}
				best.dropPlans()
				p := r.newPlan(best)
				s := p.at(folkSpot{x: x, y: y, face: math.Atan2(by-y, bx-x), node: -1}, "watch", v.between(50, 90))
				s.watchI = i
				best.steps, best.doing = p.steps, doLook
				r.folkBegin(best)
				v.onlookFor, v.onlookCD = i, 500
				return
			}
		}
	}
}

// companion is someone close to f also about to head for the fire, to walk
// there with: finishing their last bit of work, not already on their way.
func (r *Room) companion(f *Folk) *Folk {
	v := f.vil
	for _, o := range v.members {
		if o == f || !o.joins || o.part != f.part || o.state != fsTask || o.party != nil || o.partner != nil ||
			o.duty != dutyNone || o.doing == doGather || o.doing == doDuty || o.walking || o.waiting || len(o.steps) > 1 ||
			o.follow != nil || math.Hypot(o.X-f.X, o.Y-f.Y) > 4 || math.Hypot(o.X-v.fx, o.Y-v.fy) < 4.5 {
			continue
		}
		return o
	}
	return nil
}

// planGather: to the fire for the meal or the evening.
func (r *Room) planGather(f *Folk, p *plan, t float64) {
	v := f.vil
	// a neighbour just finishing up nearby: wait for them and walk over together
	if f.party == nil && math.Hypot(f.X-v.fx, f.Y-v.fy) > 4.5 {
		if o := r.companion(f); o != nil {
			newParty(f, o).gather = true
			d := math.Max(math.Hypot(f.X-o.X, f.Y-o.Y), 0.01)
			mx, my := o.X+(f.X-o.X)/d*0.9, o.Y+(f.Y-o.Y)/d*0.9
			if !v.reach(mx, my) {
				mx, my = f.X, f.Y
			}
			p.steps = append(p.steps, folkStep{x: mx, y: my, act: "watch", dur: 2, face: math.NaN(), with: o, sync: 1, watchI: -1})
		}
	} else if f.party != nil {
		p.here("idle", 2, math.NaN()).sync = 1 // they are coming over to walk with us
	}
	seat := r.seatOf(f)
	if f.part != partEvening { // the meal: sit and eat till it is over, lingering a little
		s := p.at(seat, "sit", v.between(0, 40))
		s.until, s.leave = folkMealEnd, true
		if f.party != nil {
			s.sync = 2
		}
		return
	}
	if math.Hypot(f.X-v.fx, f.Y-v.fy) < 4.5 && (v.spoke || t > folkSpeechAt+0.03) && !f.child && !r.feast(v) &&
		v.chance(0.2+f.pers.social*0.25) && r.chat(f, p, 4) {
		p.at(seat, "sit", v.between(80, 160)) // a word with a neighbour round the fire
		return
	}
	first := p.at(seat, "sit", v.between(10, 40))
	first.until, first.leave = folkSpeechAt+0.024, true // through the elder's words
	if f.party != nil {
		first.sync = 2
	}
	if f == v.elder || t > folkSpeechAt+0.024 {
		first.until = 0
		first.dur = v.between(15, 30)
	}
	if r.feast(v) {
		p.at(seat, "cheer", v.between(30, 50))
		if f.child || f.pers.social > 0.55 {
			a := v.roll() * 2 * math.Pi
			for range 6 {
				x, y := v.fx+math.Cos(a)*1.9, v.fy+math.Sin(a)*1.9
				if v.reach(x, y) {
					p.at(folkSpot{x: x, y: y, face: a, node: -1}, "dance", v.between(80, 140))
					break
				}
				a += 1.05
			}
		}
		p.at(seat, "sit", v.between(100, 200))
		return
	}
	p.at(seat, "sit", v.between(150, 300))
}
