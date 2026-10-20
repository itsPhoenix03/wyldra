package room

import (
	"bytes"
	"encoding/json"
	"math"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"hearth/gameserver/persist"
	"hearth/gameserver/proto"
	"hearth/gameserver/world"
)

// Coverage for the feature flags (features.go) and the tile mountains' room
// side (mountains.go): the tester panel's `devcmd feat`, the toggle re-stamping
// the ground and re-streaming its chunks, the flags surviving a save, and the
// creature climbing rule on the mountains.

// peerAt adds a second connected player somewhere else, already streamed in.
func (f *fixture) peerAt(id string, x, y float64) (*Player, *Session) {
	s := NewSession(id, "u_"+id, "Peer", "test")
	p := newPlayer(s, [2]int{int(x), int(y)}, f.now, f.r.defs)
	f.r.players[s.ID] = p
	f.r.playerOrder = append(f.r.playerOrder, p)
	p.chunkInit = true
	p.chunkCX, p.chunkCY = chunkOf(p.X, p.Y)
	return p, s
}

func TestFeatureDefaultsAndParse(t *testing.T) {
	f := newFixture(t)
	if !f.r.feature("tileMountains") || !f.r.feature("cameraPeek") {
		t.Fatalf("defaults: %v, want both on", f.r.features)
	}
	if !f.r.mtn.applied {
		t.Fatal("tileMountains defaults on, but the mountains are not stamped")
	}
	got, problems := ParseFeatures(" tileMountains=0, cameraPeek = on ,bogus=1,cameraPeek")
	if !reflect.DeepEqual(got, map[string]bool{"tileMountains": false, "cameraPeek": true}) {
		t.Fatalf("ParseFeatures = %v", got)
	}
	if len(problems) != 2 {
		t.Fatalf("want the unknown flag and the value-less pair reported, got %v", problems)
	}
	r, err := New(Config{Seed: "hearth-1", World: sharedWorld, Features: map[string]bool{"tileMountains": false}})
	if err != nil {
		t.Fatal(err)
	}
	if r.feature("tileMountains") || r.mtn.applied || !r.feature("cameraPeek") {
		t.Fatal("Config.Features did not set the starting flags")
	}
	if !bytes.Equal(r.world.Elev, sharedWorld.Elev) {
		t.Fatal("a room with tileMountains off changed the ground")
	}
}

func TestInitCarriesFeatures(t *testing.T) {
	f := newFixture(t)
	f.r.features["cameraPeek"] = false
	init, _ := f.joinFresh("feat01")
	ft, ok := init["features"].(map[string]any)
	if !ok {
		t.Fatalf("init.features missing: %T", init["features"])
	}
	if ft["tileMountains"] != true || ft["cameraPeek"] != false || len(ft) != len(featureDefs) {
		t.Fatalf("init.features = %v", ft)
	}
}

func TestDevCmdFeatGatingAndValidation(t *testing.T) {
	// no dev claim: refused like every other tester command, nothing changes
	f := newFixture(t)
	f.drain()
	f.devcmd(map[string]any{"cmd": "feat", "name": "cameraPeek", "on": false})
	if ms := f.msgs(); len(ms) != 1 || ms[0] != devCmdOffMsg {
		t.Fatalf("an unprivileged feat got %v", ms)
	}
	if !f.r.feature("cameraPeek") {
		t.Fatal("an unprivileged feat changed a flag")
	}

	f = devFixture(t)
	_, ps := f.peerAt("peer01", 1100, 1100)
	f.drain()
	// an unknown name is answered; a non-boolean `on` and a no-op are not
	f.devcmd(map[string]any{"cmd": "feat", "name": "flyingBoats", "on": true})
	if ms := f.msgs(); len(ms) != 1 || !strings.Contains(ms[0], "No feature flag") {
		t.Fatalf("unknown flag got %v", ms)
	}
	for _, bad := range []any{1.0, "true", nil} {
		f.devcmd(map[string]any{"cmd": "feat", "name": "cameraPeek", "on": bad})
	}
	f.devcmd(map[string]any{"cmd": "feat", "name": "cameraPeek", "on": true}) // already on
	if out := f.drain(); len(out) != 0 {
		t.Fatalf("a bad or no-op feat sent %v", out)
	}
	if len(drainSession(t, ps)) != 0 {
		t.Fatal("a bad or no-op feat reached another player")
	}

	// a real change: everyone gets `feat`, the tester gets a toast, and a
	// non-terrain flag streams no chunks
	f.devcmd(map[string]any{"cmd": "feat", "name": "cameraPeek", "on": false})
	if f.r.feature("cameraPeek") {
		t.Fatal("feat did not change the flag")
	}
	out := f.drain()
	feat := frameOfType(out, "feat")
	if feat == nil || feat["features"].(map[string]any)["cameraPeek"] != false {
		t.Fatalf("the tester got no feat broadcast: %v", out)
	}
	if countType(out, "chunk") != 0 {
		t.Fatal("a camera flag re-streamed terrain")
	}
	if ms := f.msgsOf(out); len(ms) != 1 || !strings.Contains(ms[0], "cameraPeek OFF") {
		t.Fatalf("toast = %v", ms)
	}
	if pf := frameOfType(drainSession(t, ps), "feat"); pf == nil || pf["features"].(map[string]any)["cameraPeek"] != false {
		t.Fatal("another player did not get the feat broadcast")
	}
}

func TestTileMountainsToggleRestoresAndRestreams(t *testing.T) {
	f := devFixture(t)
	s := f.r.mtn.stamp
	wood := s.Peaks[0]
	key := chunkKey(wood.Site[0]/proto.ChunkSize, wood.Site[1]/proto.ChunkSize)
	// the tester stands by the Woods mountain; a peer far off once streamed it
	f.place(float64(wood.Site[0])+18, float64(wood.Site[1])+18, 0)
	f.p.chunkCX, f.p.chunkCY = chunkOf(f.p.X, f.p.Y)
	far, fs := f.peerAt("peer02", 1100, 1100)
	far.sentChunks[key] = true
	stamped := f.r.buildStatic(key%ChunkGrid, key/ChunkGrid)
	if !world.IsMountainVis(f.r.world.TileVis[s.Changes[len(s.Changes)/2].I]) && !s.Changes[len(s.Changes)/2].Keep {
		t.Fatal("the stamp is not on the room's world")
	}
	creX, creY := float64(wood.Summit[0]), float64(wood.Summit[1])
	c := f.r.newCreature(creX, creY, 5, "crawler", 0, false)
	f.drain()

	f.devcmd(map[string]any{"cmd": "feat", "name": "tileMountains", "on": false})
	if f.r.mtn.applied {
		t.Fatal("tileMountains off left the stamp on")
	}
	// the exact base world, byte for byte
	if !bytes.Equal(f.r.world.Elev, sharedWorld.Elev) || !bytes.Equal(f.r.world.TileVis, sharedWorld.TileVis) {
		t.Fatal("lifting the stamp did not restore elev / tileVis byte for byte")
	}
	if !reflect.DeepEqual(f.r.world.Nodes, sharedWorld.Nodes) || !reflect.DeepEqual(f.r.world.Decor, sharedWorld.Decor) {
		t.Fatal("lifting the stamp did not restore the nodes and decor")
	}
	// the shared world was never written
	if f.r.world == sharedWorld || &f.r.world.Elev[0] == &sharedWorld.Elev[0] {
		t.Fatal("the room stamped the shared world in place")
	}
	// the cached chunk was dropped and the tester got the rebuilt one
	if f.r.chunkCache[key] == stamped {
		t.Fatal("the stamped chunk is still cached")
	}
	out := f.drain()
	var got map[string]any
	for _, m := range out {
		if m["t"] == "chunk" && int(m["cx"].(float64)) == key%ChunkGrid && int(m["cy"].(float64)) == key/ChunkGrid {
			got = m
		}
	}
	if got == nil {
		t.Fatalf("the mountain chunk was not re-pushed to the player beside it (%d frames)", len(out))
	}
	elev, err := proto.DecodeRLE(got["elev"].(string), proto.ChunkTiles)
	if err != nil {
		t.Fatal(err)
	}
	x0, y0 := key%ChunkGrid*proto.ChunkSize, key/ChunkGrid*proto.ChunkSize
	for ly := 0; ly < proto.ChunkSize; ly++ {
		if !bytes.Equal(elev[ly*proto.ChunkSize:(ly+1)*proto.ChunkSize], sharedWorld.Elev[(y0+ly)*world.SIZE+x0:(y0+ly)*world.SIZE+x0+proto.ChunkSize]) {
			t.Fatalf("re-pushed chunk row %d is not the base ground", ly)
		}
	}
	if frameOfType(out, "feat") == nil {
		t.Fatal("no feat broadcast")
	}
	// the far player gets the flags but not the chunk — it is re-sent when they
	// come near, because it is no longer marked sent
	// (it does get the Marsh mountain's chunk, which is beside it)
	fo := drainSession(t, fs)
	if frameOfType(fo, "feat") == nil {
		t.Fatal("the far player got no feat broadcast")
	}
	marsh := chunkKey(1100/proto.ChunkSize, 1100/proto.ChunkSize)
	for _, m := range fo {
		if m["t"] != "chunk" {
			continue
		}
		k := chunkKey(int(m["cx"].(float64)), int(m["cy"].(float64)))
		if k == key {
			t.Fatal("the far player was sent the Woods mountain's chunk")
		}
		if abs(k%ChunkGrid-marsh%ChunkGrid) > proto.ChunkRadius || abs(k/ChunkGrid-marsh/ChunkGrid) > proto.ChunkRadius {
			t.Fatalf("the far player was sent chunk %d outside its window", k)
		}
	}
	if far.sentChunks[key] {
		t.Fatal("the far player's stale mountain chunk is still marked sent")
	}
	// creatures stay where they stood
	if c.X != creX || c.Y != creY {
		t.Fatal("the toggle moved a creature")
	}

	// and back on: the same stamp, streamed again
	f.devcmd(map[string]any{"cmd": "feat", "name": "tileMountains", "on": true})
	if !f.r.mtn.applied || int(f.r.world.Elev[wood.Summit[1]*world.SIZE+wood.Summit[0]]) != int(wood.Top) {
		t.Fatal("tileMountains on did not stamp the mountains back")
	}
	if countType(f.drain(), "chunk") == 0 {
		t.Fatal("re-stamping pushed no chunks")
	}
}

func TestFeaturesPersistInSave(t *testing.T) {
	path := filepath.Join(t.TempDir(), "world.save.json")
	src, err := New(Config{Seed: "hearth-1", World: sharedWorld, Store: persist.NewJSONStore(path)})
	if err != nil {
		t.Fatal(err)
	}
	src.setFeature("tileMountains", false)
	src.setFeature("cameraPeek", false)
	if err := src.saveGame(); err != nil {
		t.Fatal(err)
	}
	dst, err := New(Config{Seed: "hearth-1", World: sharedWorld, Store: persist.NewJSONStore(path)})
	if err != nil {
		t.Fatal(err)
	}
	if dst.feature("tileMountains") || dst.feature("cameraPeek") || dst.mtn.applied {
		t.Fatalf("saved flags not restored: %v", dst.features)
	}
	if !bytes.Equal(dst.world.Elev, sharedWorld.Elev) {
		t.Fatal("a world saved with tileMountains off came back stamped")
	}
	// the save wins over the configured starting value
	dst2, err := New(Config{Seed: "hearth-1", World: sharedWorld, Store: persist.NewJSONStore(path),
		Features: map[string]bool{"tileMountains": true}})
	if err != nil {
		t.Fatal(err)
	}
	if dst2.feature("tileMountains") {
		t.Fatal("Config.Features overrode the flag the save recorded")
	}

	// an old save, from before flags: it loads as it always did, with the defaults
	old := filepath.Join(t.TempDir(), "old.save.json")
	if err := persist.NewJSONStore(old).Save(&persist.Snapshot{Version: world.WorldVersion, Seed: "hearth-1", Day: 3, Time: 0.5,
		Profiles: map[string]*persist.Profile{}}); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(&persist.Snapshot{Version: world.WorldVersion, Seed: "hearth-1"})
	if strings.Contains(string(raw), "features") {
		t.Fatal("an empty Features field is written to the save")
	}
	r, err := New(Config{Seed: "hearth-1", World: sharedWorld, Store: persist.NewJSONStore(old)})
	if err != nil {
		t.Fatal(err)
	}
	if r.day != 3 || !r.feature("tileMountains") || !r.feature("cameraPeek") || !r.mtn.applied {
		t.Fatalf("old save: day %d flags %v stamped %v", r.day, r.features, r.mtn.applied)
	}
}

// cliffStep finds a mountain tile with a 4-neighbour at least two levels
// higher: the foot of a face.
func cliffStep(t *testing.T, r *Room) (from, to int) {
	t.Helper()
	for _, c := range r.mtn.stamp.Changes {
		for _, d := range [4]int{1, -1, world.SIZE, -world.SIZE} {
			j := c.I + d
			if r.world.Tiles[j] != world.TWater && int(r.world.Elev[j])-int(r.world.Elev[c.I]) >= 2 {
				return c.I, j
			}
		}
	}
	t.Fatal("no cliff on the mountains")
	return 0, 0
}

func TestCreatureClimbRuleOnMountainTiles(t *testing.T) {
	f := newFixture(t)
	from, to := cliffStep(t, f.r)
	if !f.r.mtnClimbBlocked(from, to) {
		t.Fatal("a creature may climb a mountain face")
	}
	if f.r.mtnClimbBlocked(to, from) {
		t.Fatal("a creature may not step down off a mountain face")
	}
	// off the mountains creatures still ignore elevation, as they always have
	sx, sy := openPatch(t, f.r, world.TSand)
	a, b := ti(sx, sy), ti(sx+1, sy)
	f.r.world.Elev[b] = f.r.world.Elev[a] + 3
	if f.r.mtnClimbBlocked(a, b) {
		t.Fatal("the mountain climb rule reached ground that is not a mountain")
	}
	// a crawler lunging up the face stops at its foot
	fx, fy := float64(from%world.SIZE)+0.5, float64(from/world.SIZE)+0.5
	tx, ty := float64(to%world.SIZE)+0.5, float64(to/world.SIZE)+0.5
	c := f.r.newCreature(fx, fy, 5, "crawler", 0, false)
	c.AtkAng = math.Atan2(ty-fy, tx-fx)
	f.r.lunge(c, "crawler", 1.2)
	if ti(c.X, c.Y) == to {
		t.Fatal("a lunge carried a crawler up a mountain face")
	}
	// and with the mountains down the rule is off
	f.r.setFeature("tileMountains", false)
	if f.r.mtnClimbBlocked(from, to) {
		t.Fatal("the climb rule outlived the mountains")
	}
}

func TestMountainFootprintStopsBlockingWithTileMountains(t *testing.T) {
	f := newFixture(t)
	var mtn, temple int
	for i := range world.MountainBlock {
		mtn = i
		break
	}
	temple = world.TemplePieces[1].I
	if f.r.landmarkBlocks(mtn) {
		t.Fatal("a sprite mountain's footprint still blocks with tile mountains up")
	}
	if !f.r.landmarkBlocks(temple) {
		t.Fatal("a temple piece stopped blocking")
	}
	f.r.setFeature("tileMountains", false)
	if !f.r.landmarkBlocks(mtn) {
		t.Fatal("with tile mountains off the sprite mountain's footprint must block again")
	}
}

func TestNothingSpawnsOnMountainFaces(t *testing.T) {
	f := newFixture(t)
	for i := range f.r.mtn.stamp.Face {
		if !f.r.onMountainFace(float64(i%world.SIZE), float64(i/world.SIZE)) {
			t.Fatal("onMountainFace misses a face")
		}
		break
	}
	for i := 0; i < 400; i++ {
		f.r.tickN = int64(i * 5)
		f.r.animalSpawnTick()
	}
	for _, a := range f.r.aniOrder {
		if f.r.onMountainFace(a.X, a.Y) {
			t.Fatalf("animal %s spawned on a mountain face", a.ID)
		}
	}
}
