package room

import (
	"math"
	"math/rand"

	"hearth/gameserver/world"
)

// Island folk: the villagers, hunters, children and elders who live in the
// island villages (world.FindFolkCamps; each camp is one hut, and a village's
// huts share one fire). They cannot be hurt, traded with or built on;
// they make the islands feel lived in and react to the same dangers players do.
//
// Each folk member lives a day shaped by who they are (folk_routine.go): they
// get up and turn in at their own hour, work at their trade at real places
// round the village (a tree, a bush, a shore, a hut wall, the village edge),
// gather at the fire to eat at midday and to talk in the evening, and stop to
// chat with each other. On top of that routine, every one of them:
//
//   - stops and turns to a player who comes within talking range (the client
//     shows what they say);
//   - runs for the hut the moment a creature comes near, ducks inside, and
//     comes back out once the coast has been clear for a few seconds;
//   - turns in for the night; the watchers (each village's elder, and the
//     Ashmark hunters) instead keep the fire or the edge, and only take cover
//     when something comes for them.
//
// Hunting creatures with nothing better to do chase folk off (creature_ai.go),
// so a camp under pressure is a visible sign of creatures about.
//
// On the wire a folk member is [id, x, y, kind, act, face] in the `cre` frame's
// `f` list. act is idle / walk / run / play / talk / chat / speak / watch / sit
// / collect / build / craft / hide; face is the way they face while at it, in
// whole degrees in world space (0 = +x, 90 = +y), or null to let the client
// choose (along the walk, toward the keeper they talk to).

type folkState uint8

const (
	fsTask   folkState = iota // following its steps: walking to a spot, or doing something there
	fsFlee                    // running for the hut
	fsHome                    // walking home to turn in
	fsHidden                  // inside the hut
)

const (
	folkWalkSp  = 0.11 // tiles / tick
	folkPlaySp  = 0.2  // children at play
	folkRunSp   = 0.3
	folkThreatR = 7.0  // a creature this close sends folk running
	folkClearR  = 10.0 // and must be this far away before they come out
	folkClearTk = 15   // ticks of all-clear before coming back out
	folkTalkR   = 2.4  // a player this close gets their attention
	folkGoingTk = 160  // ticks circling for the door before they slip in anyway
)

// folkWatchers keep the village fire (or its edge) at night.
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

	role    folkRole
	vil     *village
	state   folkState
	steps   []folkStep // what they mean to do next, in order
	walking bool       // on the way to steps[0]'s spot (else doing its act)
	timer   int        // ticks left of the act, or of the walk before giving up
	calm    int
	stuck   int
	going   int // ticks spent heading for the door; past folkGoingTk they get in some other way

	watcher bool    // keeps the fire / the edge at night
	child   bool    // runs about at play
	pace    float64 // their own walking pace (× folkWalkSp)
	wake    float64 // day time they get up (non-watchers)
	bed     float64 // and turn in
	seat    int     // their place in the circle round the fire

	part    dayPart // the part of the day their plans were made for
	joins   bool    // whether they come to this part's gathering at the fire
	partner *Folk   // who they are chatting with
	chatCD  int     // ticks before they are up for another chat
}

// folkStep is one thing a folk member means to do: go somewhere, then do
// something there for a while.
type folkStep struct {
	x, y float64 // where to go first (NaN: right here)
	act  string  // what to do there ("": just passing through)
	dur  int     // for how many ticks
	face float64 // which way to face doing it (NaN: none)
	run  bool    // go at a run (children at play)
	with *Folk   // chat: who with
}

// setupCamps places the camps once the save is loaded (so no camp lands on a
// player's structure), reserves their tiles, seats the folk at their fires and
// works out each village's places (folk_routine.go).
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
			v = &village{isle: c.IslandID, fx: float64(c.FireX) + 0.5, fy: float64(c.FireY) + 0.5, yardR: c.YardR}
			byIsle[c.IslandID] = v
			r.villages = append(r.villages, v)
		}
		v.camps = append(v.camps, ci)
		for k, kind := range c.Folk {
			f := &Folk{
				ID: c.ID + "-" + itoa(k), Kind: kind, Camp: ci, Act: "idle", Face: math.NaN(),
				role: folkRoleOf(kind), vil: v, state: fsTask,
				watcher: folkWatchers[kind], child: kind == "woods_child",
				pace: 0.85 + rand.Float64()*0.3,
				wake: NightEnd + rand.Float64()*0.05,
				bed:  NightStart - rand.Float64()*0.06,
				seat: len(v.members),
				part: partNone,
			}
			v.members = append(v.members, f)
			r.folk = append(r.folk, f)
		}
	}
	for _, v := range r.villages {
		r.surveyVillage(v)
		// everyone starts in their seat at the fire, a moment apart
		for k, f := range v.members {
			f.X, f.Y = v.seats[f.seat][0], v.seats[f.seat][1]
			f.steps = []folkStep{{x: math.NaN(), y: math.NaN(), act: "idle", dur: 5 + (k%8)*7, face: v.faceFire(f.X, f.Y)}}
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
		var face any
		if !math.IsNaN(f.Face) {
			face = int(math.Round(f.Face * 180 / math.Pi))
		}
		out = append(out, []any{f.ID, toFixed(f.X, 2), toFixed(f.Y, 2), f.Kind, f.Act, face})
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

// folkLineClear: nothing but a village fire stands on the straight walk from
// (x0, y0) to (x1, y1) — a fire folk can step round.
func (r *Room) folkLineClear(x0, y0, x1, y1 float64) bool {
	n := int(math.Hypot(x1-x0, y1-y0)/0.3) + 1
	for k := 1; k <= n; k++ {
		t := float64(k) / float64(n)
		x, y := x0+(x1-x0)*t, y0+(y1-y0)*t
		if !r.folkWalkable(x, y) && !r.campFire(int(x), int(y)) {
			return false
		}
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

// folkStep moves f toward (tx, ty) at sp, turning up to 90° round obstacles.
// It reports whether f has arrived.
func (r *Room) folkStep(f *Folk, tx, ty, sp float64) bool {
	d := math.Hypot(tx-f.X, ty-f.Y)
	if d < 0.3 {
		f.stuck = 0
		return true
	}
	sp = math.Min(sp, d)
	base := math.Atan2(ty-f.Y, tx-f.X)
	for _, rot := range [5]float64{0, 0.8, -0.8, 1.57, -1.57} {
		nx, ny := f.X+math.Cos(base+rot)*sp, f.Y+math.Sin(base+rot)*sp
		if r.folkWalkable(nx, ny) {
			f.X, f.Y = nx, ny
			f.stuck = 0
			return false
		}
	}
	f.stuck++
	return false
}

// folkThreatened: some creature is within rad of f.
func (r *Room) folkThreatened(f *Folk, rad float64) bool {
	for _, c := range r.creOrder {
		if math.Hypot(c.X-f.X, c.Y-f.Y) < rad {
			return true
		}
	}
	return false
}

// folkUp: f is up and about at this time of day (watchers always are).
func (f *Folk) up(t float64) bool {
	return f.watcher || (t >= f.wake && t < f.bed)
}

// dropPlans clears f's plans, ending any chat it was in.
func (f *Folk) dropPlans() {
	f.steps, f.walking = nil, false
	f.endChat()
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
	for _, f := range r.folk {
		dx, dy := r.campDoor(f.Camp)
		if f.chatCD > 0 {
			f.chatCD--
		}

		if f.state == fsHidden {
			if r.folkThreatened(f, folkClearR) {
				f.calm = 0
				continue
			}
			f.calm++
			if f.calm >= folkClearTk && f.up(t) {
				f.X, f.Y = dx, dy
				f.state, f.Act, f.Face = fsTask, "idle", math.NaN()
				f.dropPlans()
				f.part = partNone // plan afresh for the time of day
			}
			continue
		}

		// danger first: run for the hut and duck inside
		if r.folkThreatened(f, folkThreatR) || f.state == fsFlee {
			if f.state != fsFlee && !r.folkThreatened(f, folkThreatR) {
				f.state = fsTask
			} else {
				if f.state != fsFlee {
					f.going = 0
					f.dropPlans()
				}
				f.state, f.Act, f.Face = fsFlee, "run", math.NaN()
				f.going++
				if r.folkStep(f, dx, dy, folkRunSp) || f.stuck > 6 || f.going > folkGoingTk {
					f.state, f.Act, f.calm, f.stuck = fsHidden, "hide", 0, 0
				}
				continue
			}
		}

		// their hour to turn in (watchers keep their watch)
		if !f.up(t) {
			if f.state != fsHome {
				f.going = 0
				f.dropPlans()
			}
			f.state, f.Act, f.Face = fsHome, "walk", math.NaN()
			f.going++
			if r.folkStep(f, dx, dy, folkWalkSp*f.pace) || f.stuck > 10 || f.going > folkGoingTk {
				f.state, f.Act, f.calm, f.stuck = fsHidden, "hide", 0, 0
			}
			continue
		}
		if f.state == fsHome {
			f.state = fsTask
		}

		// a player close by: stop and talk (a sitter stays seated)
		if r.folkNearPlayer(f) {
			if f.Act != "sit" {
				f.Act, f.Face = "talk", math.NaN()
			}
			continue
		}
		r.folkRoutine(f, t)
	}
}

func (r *Room) folkNearPlayer(f *Folk) bool {
	for _, q := range r.playerOrder {
		if q.Z == 0 && math.Hypot(q.X-f.X, q.Y-f.Y) < folkTalkR {
			return true
		}
	}
	return false
}
