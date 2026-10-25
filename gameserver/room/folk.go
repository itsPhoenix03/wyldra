package room

import (
	"math"

	"hearth/gameserver/world"
)

// Island folk: the villagers, hunters, children and elders who live in the
// island villages (world.FindFolkCamps; each camp is one hut, and a village's
// huts share one fire). They cannot be hurt, traded with or built on; they
// make the islands feel lived in and react to the same dangers players do.
//
// Each folk member is somebody: an early riser or a night owl, more or less
// diligent, sociable and curious (folk_life.go, from their id), with needs that
// run down through the day — rest, food, company, the itch to get on with the
// work. Their day (folk_routine.go) is chosen a piece at a time by weighing
// what their trade offers against the hour, the weather, those needs and what
// they did last; the village's own duties (folk_work.go) are drawn each day:
// someone stokes the fire at dawn and at dusk with wood from a hut's pile, a
// cook fetches water and gets the pot on before midday and calls everyone in,
// builders fell and stack wood and mend the walls with it, hunters go out in
// pairs and come home together with the game, fishers bring in the catch. Work
// that moves something carries it: it rides the wire as the row's item.
// Between times they chat with friends, wave to a neighbour going by, and the
// evening brings everyone to the fire, where the elder speaks and the rest
// listen — and dance, the evening after a monolith is lit (folk_social.go).
// Families are their huts: children play near a parent, are called in at dusk,
// and go to bed first. At night a watch keeps the village in shifts, each
// watcher with a light (folk_life.go).
//
// They get about on foot (folk_nav.go): routes over each village's own grid of
// the ground — round huts, the fire, trees, rocks, water and anything keepers
// build — up and down one level at a time, pulled straight, and giving each
// other and the keepers room.
//
// On top of all that, every one of them:
//
//   - turns to watch a keeper coming, waves, and stops to talk when the keeper
//     is close (the client shows what they say); a curious child may tag after
//     a keeper round the yard, and a keeper building near the village draws an
//     onlooker;
//   - runs for the hut the moment a creature comes near them or the fire and
//     ducks inside — except the village's guards (hunters, watchers, the
//     strongest adults), who stand between the threat and the huts facing it
//     until it closes on them; once the coast has been clear a while they come
//     out, look round, and get on;
//   - tells off a keeper who harms the land, or runs from one the village has
//     turned against (folk_regard.go), and heads in from a storm on their
//     island (folk_weather.go).
//
// Hunting creatures with nothing better to do chase folk off (creature_ai.go,
// nearestFolk), so a camp under pressure is a visible sign of creatures about.
//
// On the wire a folk member is [id, x, y, kind, act, face, item] in the `cre`
// frame's `f` list. act is idle / walk / run / play / carry / talk / chat /
// speak / listen / watch / wave / call / sit / collect / build / craft / cook /
// tend / guard / cheer / dance / hide / shun / shelter / scold; face is the way
// they face while at it, in whole degrees in world space (0 = +x, 90 = +y), or
// null to let the client choose (along the walk, toward the keeper they talk
// to); item is what they carry — wood / stone / fish / game / herbs / berries /
// reeds / water / torch — or null.

type folkState uint8

const (
	fsTask   folkState = iota // following its steps: walking to a spot, or doing something there
	fsFlee                    // running for the hut
	fsHome                    // walking home to turn in
	fsHidden                  // inside the hut
	fsGuard                   // standing guard between a creature and the huts
)

const (
	folkWalkSp     = 0.11 // tiles / tick
	folkPlaySp     = 0.2  // children at play
	folkRunSp      = 0.3
	folkGuardSp    = 0.2  // off to a guard post
	folkThreatR    = 7.0  // a creature this close sends folk running
	folkClearR     = 10.0 // and must be this far away before they come out
	folkClearTk    = 15   // ticks of all-clear before coming back out
	folkTalkR      = 2.4  // a player this close gets their attention
	folkNoticeR    = 6.5  // and this close, a look and a wave
	folkGoingTk    = 160  // ticks circling for the door before they slip in anyway
	folkGuardBackR = 3.5  // a creature this close to a guard sends them in too
	folkMaxGuards  = 3
)

// folkWatchers stand their ground (they scold a keeper the village has turned
// against rather than hide from them) and take their turn at the night watch.
var folkWatchers = map[string]bool{
	"elder_woods": true, "shaman": true, "elder_yvenne": true, "elder_spire": true, "ashmark_hunter": true,
}

// Folk is one camp resident.
type Folk struct {
	ID   string
	Kind string
	Camp int // index into r.camps: their hut
	X, Y float64
	Act  string
	// Face is the way they face at what they are doing (radians, world space),
	// or NaN to let the client choose.
	Face float64
	// Item is what they are carrying ("" for nothing).
	Item string

	role    folkRole
	vil     *village
	idx     int // their place in vil.members
	state   folkState
	steps   []folkStep // what they mean to do next, in order
	walking bool       // on the way to steps[0]'s spot (else doing its act)
	waiting bool       // at a party's meeting point, waiting for the rest
	timer   int        // ticks left of the act, or of the walk before giving up
	calm    int
	stuck   int // ticks in a row they could not move
	going   int // ticks spent heading for the door; past goMax they get in some other way
	goMax   int
	moved   bool // took a step this tick
	nav     folkNav

	watcher bool // stands its ground, keeps the watch (folkWatchers)
	roster  bool // on the village's night watch
	child   bool // runs about at play
	pers    folkPersonality
	needs   folkNeeds
	pace    float64 // their own walking pace (× folkWalkSp)
	wake    float64 // day time they get up
	bed     float64 // and turn in
	seat    int     // their place in the circle round the fire

	part    dayPart  // the part of the day their plans were made for
	joins   bool     // whether they come to this part's gathering at the fire
	doing   activity // what the steps in hand are for
	last    [3]activity
	duty    folkDuty // a village duty waiting to be planned (folk_work.go)
	heard   bool     // heard the meal call
	called  bool     // a child called in by a parent
	partner *Folk    // who they are chatting with
	chatCD  int      // ticks before they are up for another chat
	greetCD int
	callKid *Folk      // the child they are about to call in
	party   *folkParty // out with others (a hunt, the walk to the fire)
	syncAt  int8       // the last meeting point of the party's steps they reached
	waitTk  int        // ticks waited there
	parents []*Folk
	kids    []*Folk

	gest     folkGesture // a moment's wave or look round, over whatever they are at
	noticeCD int
	follow   *Player // a keeper a child is tagging after
	followTk int
	followCD int
	followAt navPt
	scared   bool // came in from a creature: they look round before they get on

	scoldTk int     // ticks left telling off a keeper they saw harm the land (folk_regard.go)
	scoldAt *Player // that keeper
}

// folkStep is one thing a folk member means to do: go somewhere, then do
// something there for a while.
type folkStep struct {
	x, y   float64 // where to go first (NaN: right here)
	act    string  // what to do there ("": just passing through)
	dur    int     // for how many ticks
	face   float64 // which way to face doing it (NaN: none)
	run    bool    // go at a run (children at play)
	with   *Folk   // chat: who with; listen / watch: who to face
	fx     stepFx  // what doing it changes (folk_work.go)
	item   string  // fxTake: what they pick up
	camp   int     // fxStack / fxFromPile: whose pile
	sync   int8    // a party's meeting point: wait for the rest here before going on
	until  float64 // if set, the act lasts until this time of day, then dur more
	leave  bool    // the party's outing ends as this act begins
	watchI int     // a keeper's build being watched (onlookers), or -1
}

// folkGesture is a short act laid over whatever f is doing, which waits for it.
type folkGesture struct {
	act         string
	tk, flip    int // ticks left; at tk == flip the face turns to face2
	face, face2 float64
}

// setupCamps places the camps once the save is loaded (so no camp lands on a
// player's structure), reserves their tiles, seats the folk at their fires and
// works out each village's ground, places and people (folk_routine.go,
// folk_life.go).
func (r *Room) setupCamps() {
	reserved := map[int]bool{r.spawn[1]*world.SIZE + r.spawn[0]: true}
	for i := range r.medicTiles {
		reserved[i] = true
	}
	for i := range r.structures {
		reserved[i] = true
	}
	r.camps = world.FindFolkCamps(r.world, reserved)
	r.folk = nil
	r.villages = nil
	for _, c := range r.camps {
		for _, t := range world.CampBlockTiles(c) {
			r.medicTiles[t] = true
		}
	}
	byIsle := map[string]*village{}
	for ci, c := range r.camps {
		v := byIsle[c.IslandID]
		if v == nil {
			v = r.newVillage(c)
			byIsle[c.IslandID] = v
			r.villages = append(r.villages, v)
		}
		v.camps = append(v.camps, ci)
		for k, kind := range c.Folk {
			f := &Folk{
				ID: c.ID + "-" + itoa(k), Kind: kind, Camp: ci, Act: "idle", Face: math.NaN(),
				role: folkRoleOf(kind), vil: v, idx: len(v.members), state: fsTask,
				watcher: folkWatchers[kind], child: kind == "woods_child",
				seat: len(v.members), part: partNone,
			}
			v.members = append(v.members, f)
			r.folk = append(r.folk, f)
		}
	}
	for _, v := range r.villages {
		v.nav = r.newNavGrid(int(v.fx), int(v.fy))
		r.navRefresh(v.nav, true)
		r.surveyVillage(v)
		r.settleVillage(v)
		// everyone starts in their seat at the fire, a moment apart
		for k, f := range v.members {
			f.X, f.Y = v.seats[f.seat][0], v.seats[f.seat][1]
			f.steps = []folkStep{{x: math.NaN(), y: math.NaN(), act: "idle", dur: 5 + (k%8)*7, face: v.faceFire(f.X, f.Y), watchI: -1}}
			r.folkBegin(f)
		}
	}
}

// campDoor is the spot just in front of the hut where folk duck inside.
func (r *Room) campDoor(ci int) (float64, float64) {
	c := &r.camps[ci]
	return float64(c.HutX) + 1.35, float64(c.HutY) + 1.35
}

func (r *Room) campsWire() []map[string]any {
	out := make([]map[string]any, 0, len(r.camps))
	for _, c := range r.camps {
		out = append(out, map[string]any{
			"id": c.ID, "islandId": c.IslandID, "hutSprite": c.HutSprite,
			"hutX": c.HutX, "hutY": c.HutY, "fireX": c.FireX, "fireY": c.FireY,
		})
	}
	return out
}

func (r *Room) folkWire() [][]any {
	out := make([][]any, 0, len(r.folk))
	for _, f := range r.folk {
		var face, item any
		if !math.IsNaN(f.Face) {
			face = int(math.Round(f.Face * 180 / math.Pi))
		}
		if f.Item != "" && f.state != fsHidden {
			item = f.Item
		}
		out = append(out, []any{f.ID, toFixed(f.X, 2), toFixed(f.Y, 2), f.Kind, f.Act, face, item})
	}
	return out
}

// nearestFolk is the closest folk member out in the open within rad, for idle
// hunters looking for something to chase.
func (r *Room) nearestFolk(x, y, rad float64) *Folk {
	var best *Folk
	for _, f := range r.folk {
		if f.state == fsHidden {
			continue
		}
		if d := math.Hypot(f.X-x, f.Y-y); d < rad {
			rad, best = d, f
		}
	}
	return best
}

// folkWalkable: open land a folk member can stand on.
func (r *Room) folkWalkable(x, y float64) bool {
	if x < 0 || y < 0 || x >= world.SIZE || y >= world.SIZE {
		return false
	}
	i := ti(x, y)
	if !r.creFooting(i) || r.medicTiles[i] || world.LandmarkBlock[i] || r.wallBlocks(i) {
		return false
	}
	if s, ok := r.structures[i]; ok && !r.defs.DecorNonBlk[s.Kind] && s.Kind != "farmplot" {
		return false
	}
	return true
}

// campFire: (x, y) is a village fire.
func (r *Room) campFire(x, y int) bool {
	for i := range r.camps {
		if r.camps[i].FireX == x && r.camps[i].FireY == y {
			return true
		}
	}
	return false
}

// villageFireNear: a village fire burns within radius of (x, y). A village fire
// is a campfire like any a keeper builds: it heals, warms off a blizzard, cooks
// and keeps fire-shy creatures back at night (nearStruct, campfires).
func (r *Room) villageFireNear(x, y, radius float64) bool {
	for i := range r.camps {
		if math.Hypot(float64(r.camps[i].FireX)-x, float64(r.camps[i].FireY)-y) <= radius {
			return true
		}
	}
	return false
}

// folkThreatened: some creature is within rad of f.
func (r *Room) folkThreatened(f *Folk, rad float64) bool {
	return r.creatureNear(f.X, f.Y, rad)
}

// dropPlans clears f's plans, ending any chat it was in and leaving any party.
func (f *Folk) dropPlans() {
	f.steps, f.walking = nil, false
	f.doing = doNone
	f.endChat()
	f.leaveParty()
	f.nav.reset()
}

func (f *Folk) endChat() {
	if p := f.partner; p != nil {
		f.partner = nil
		if p.partner == f {
			p.partner = nil
			if p.walking || len(p.steps) == 0 || p.steps[0].act != "chat" {
				return
			}
			p.timer = 0 // they wrap up too
		}
	}
}

func (r *Room) folkTick() {
	if len(r.players) == 0 || len(r.folk) == 0 {
		return
	}
	t := r.time
	for _, v := range r.villages {
		r.villageTick(v, t)
		for _, f := range v.members {
			r.folkUpdate(f, t)
		}
		r.folkSeparate(v)
	}
}

// folkSeparate eases apart any two of a village's folk who have ended up on
// top of each other (a spot two went for at once, a squeeze past in a gap):
// a little each tick, never into anything solid.
func (r *Room) folkSeparate(v *village) {
	const room = 0.4
	g := v.nav
	for a, fa := range v.members {
		if fa.state == fsHidden {
			continue
		}
		for _, fb := range v.members[a+1:] {
			if fb.state == fsHidden || math.Abs(fa.X-fb.X) > room || math.Abs(fa.Y-fb.Y) > room {
				continue
			}
			dx, dy := fb.X-fa.X, fb.Y-fa.Y
			d := math.Hypot(dx, dy)
			if d >= room {
				continue
			}
			if d < 1e-6 {
				dx, dy, d = 0.7071, 0.7071, 1 // dead on top: part along a diagonal
			}
			push := math.Min(0.035, (room-d)/2)
			ux, uy := dx/d*push, dy/d*push
			if g.moveOK(fb.X, fb.Y, fb.X+ux, fb.Y+uy) {
				fb.X, fb.Y = fb.X+ux, fb.Y+uy
			}
			if g.moveOK(fa.X, fa.Y, fa.X-ux, fa.Y-uy) {
				fa.X, fa.Y = fa.X-ux, fa.Y-uy
			}
		}
	}
}

// folkUpdate is one tick of one folk member: danger first, then bed, then the
// keepers about, then their day.
func (r *Room) folkUpdate(f *Folk, t float64) {
	v := f.vil
	f.moved = false
	f.tickNeeds(t)
	if f.Item == "torch" && !r.onDuty(f, t) {
		f.Item = "" // the watch is over: the light goes out
	}
	f.chatCD, f.greetCD, f.noticeCD, f.followCD = max(0, f.chatCD-1), max(0, f.greetCD-1), max(0, f.noticeCD-1), max(0, f.followCD-1)
	dx, dy := r.campDoor(f.Camp)

	if f.state == fsHidden {
		if r.folkThreatened(f, folkClearR) || r.villageAlarmed(f, folkAlarmClearR) || r.shunning(f, folkScoldR+3) || r.stormDrivesIn(f) {
			f.calm = 0
			return
		}
		f.calm++
		if f.calm >= folkClearTk && r.folkUp(f, t) {
			r.folkComeOut(f, t)
		}
		return
	}

	// danger first. A guard stands between it and the huts until it closes on
	// them; everyone else runs for the hut and ducks inside — from a creature
	// near them or near the village fire, from a keeper the village has turned
	// against, or from the storm blowing on their island (folk_weather.go).
	// "shun" and "shelter" are the same run, so the client can tell what they
	// run from.
	creDanger := r.folkThreatened(f, folkThreatR) || r.villageAlarmed(f, folkAlarmR)
	if (creDanger || f.state == fsGuard) && f.state != fsFlee && v.isGuard(f) {
		if r.folkGuard(f) {
			return
		}
		creDanger = true // it came too close: in they go too
	}
	shun := !creDanger && r.shunning(f, folkScoldR)
	storm := !creDanger && !shun && r.stormDrivesIn(f)
	if creDanger || shun || storm || f.state == fsFlee {
		if f.state != fsFlee {
			f.going, f.goMax = 0, folkGoingTk
			f.dropPlans()
			f.gest.tk = 0
			f.follow = nil
		}
		run := f.Act // still on the way in: keep running from what sent them
		switch {
		case creDanger:
			run = "run"
		case shun:
			run = "shun"
		case storm:
			run = "shelter"
		case run != "shun" && run != "shelter":
			run = "run"
		}
		f.scared = f.scared || creDanger
		f.state, f.Act, f.Face = fsFlee, run, math.NaN()
		f.going++
		if r.folkGo(f, dx, dy, folkRunSp) || f.stuck > 6 || f.going > f.goMax {
			r.folkHide(f)
		}
		return
	}

	// their hour to turn in (the watch keeps its watch)
	if !r.folkUp(f, t) {
		if f.state != fsHome {
			f.going = 0
			f.goMax = folkGoingTk + int(math.Hypot(dx-f.X, dy-f.Y)/r.folkSpeed(f, false)*1.5)
			f.dropPlans()
			f.gest.tk = 0
			f.follow = nil
		}
		f.state, f.Act, f.Face = fsHome, walkAct(false, f.Item), math.NaN()
		f.going++
		if r.folkGo(f, dx, dy, r.folkSpeed(f, false)) || f.stuck > 10 || f.going > f.goMax {
			r.folkHide(f)
		}
		return
	}
	if f.state == fsHome {
		f.state = fsTask
	}

	// a keeper they saw harm the land, or one their village resents: tell them off
	if f.scoldTk > 0 {
		f.scoldTk--
		if q := f.scoldAt; q != nil && r.players[q.S.ID] == q && q.Z == 0 {
			f.scold(q)
			return
		}
		f.scoldTk, f.scoldAt = 0, nil
	}
	if q := r.resentedNear(f, folkScoldR, standWary); q != nil {
		f.scold(q)
		return
	}

	// a player close by: stop and talk (a sitter stays seated)
	if r.folkNearPlayer(f) {
		if f.Act != "sit" {
			f.Act, f.Face = "talk", math.NaN()
		}
		return
	}
	if f.gest.tk > 0 {
		f.gesture()
		return
	}
	if r.folkNotice(f) {
		return
	}
	r.folkMaybeFollow(f)
	if f.follow != nil && r.folkFollow(f, t) {
		return
	}
	r.folkRoutine(f, t)
	r.folkFaceKeeper(f)
}

// folkHide takes f inside: whatever wood they carry goes on the hut's pile.
func (r *Room) folkHide(f *Folk) {
	if f.Item == "wood" {
		f.vil.stack(f.Camp)
	}
	dx, dy := r.campDoor(f.Camp)
	if math.Hypot(dx-f.X, dy-f.Y) < 2 {
		f.X, f.Y = dx, dy
	}
	f.Item = ""
	f.state, f.Act, f.calm, f.stuck = fsHidden, "hide", 0, 0
	f.nav.reset()
}

// folkComeOut: f steps out of the hut — after a scare, to look round first.
func (r *Room) folkComeOut(f *Folk, t float64) {
	dx, dy := r.campDoor(f.Camp)
	f.X, f.Y = dx, dy
	f.state, f.Act, f.Face = fsTask, "idle", math.NaN()
	f.dropPlans()
	f.part = partNone // plan afresh for the time of day
	f.going, f.stuck = 0, 0
	if r.onDuty(f, t) {
		f.Item = "torch"
	}
	if f.scared {
		f.scared = false
		a := f.vil.faceFire(dx, dy)
		f.gest = folkGesture{act: "watch", tk: 26 + f.idx%7, flip: 13, face: a + 0.9, face2: a - 0.9}
	}
}

// gesture plays out f's gesture (its step waits for it).
func (f *Folk) gesture() {
	g := &f.gest
	f.Act, f.Face = g.act, g.face
	if g.tk <= g.flip {
		f.Face = g.face2
	}
	g.tk--
}

// folkGuard keeps guard f between the village's threat and the huts; false
// if the creature has come too close and f must go in too.
func (r *Room) folkGuard(f *Folk) bool {
	v := f.vil
	c := v.threat
	if r.folkThreatened(f, folkGuardBackR) {
		v.dropGuard(f)
		return false
	}
	if c == nil { // gone: once it has been quiet a while, they stand down
		if r.folkThreatened(f, folkClearR) || r.villageAlarmed(f, folkAlarmClearR) {
			f.calm = 0
		} else if f.calm++; f.calm >= folkClearTk {
			v.dropGuard(f)
			f.state, f.calm = fsTask, 0
			f.dropPlans()
			f.part = partNone
			a := v.faceFire(f.X, f.Y) + math.Pi
			f.gest = folkGesture{act: "watch", tk: 22, flip: 11, face: a + 0.8, face2: a - 0.8}
			return true
		}
		f.Act = "guard"
		return true
	}
	if f.state != fsGuard {
		f.dropPlans()
		f.gest.tk = 0
		f.follow = nil
		f.state, f.calm = fsGuard, 0
		if f.Item != "torch" {
			f.Item = ""
		}
	}
	f.calm = 0
	px, py := v.guardPost(f)
	if math.Hypot(px-f.X, py-f.Y) > 0.4 && !r.folkGo(f, px, py, folkGuardSp) {
		f.Act, f.Face = "walk", math.NaN()
		if f.stuck > 8 { // cannot get to the post: stand where they are
			f.Act, f.Face, f.stuck = "guard", math.Atan2(c.Y-f.Y, c.X-f.X), 0
		}
		return true
	}
	f.Act, f.Face = "guard", math.Atan2(c.Y-f.Y, c.X-f.X)
	return true
}

func (r *Room) folkNearPlayer(f *Folk) bool {
	for _, q := range r.playerOrder {
		if q.Z == 0 && math.Hypot(q.X-f.X, q.Y-f.Y) < folkTalkR {
			return true
		}
	}
	return false
}
