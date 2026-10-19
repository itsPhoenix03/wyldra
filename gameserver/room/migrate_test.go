package room

import (
	"bytes"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"hearth/gameserver/persist"
	"hearth/gameserver/world"
)

// mapChangeSnap is a save with a live world in it: everything tile-indexed, plus
// two keepers carrying real gear. landI is a tile the tests can stand a keeper
// on. version and seed are the knobs the migration rule turns on.
func mapChangeSnap(version int, seed string, landI int) *persist.Snapshot {
	return &persist.Snapshot{
		Version: version, Seed: seed, Day: 9, Time: 0.61, Won: true,
		Mono: [4]bool{true, false, true, false},
		Profiles: map[string]*persist.Profile{
			"u_a": {
				Name: "Old Keeper", X: float64(landI % world.SIZE), Y: float64(landI / world.SIZE), Z: 1,
				HP: 7, Hunger: 3.5, Thirst: 2.5, Inv: map[string]int{"wood": 5, "iron": 2},
				Tools: []string{"axe", "pick"}, Gear: []string{ArmorKey}, Armor: true,
			},
			"u_b": {Name: "Other Keeper", X: 3, Y: 3, HP: 10, Hunger: 8, Thirst: 8, Inv: map[string]int{"stone": 4}},
		},
		Removed:     []persist.NodeRespawn{{I: landI + 9, Remaining: 90_000}},
		Mud:         []int{landI + 10},
		SectorChops: map[string]int{"1234": 8},
		Structures: map[string]*persist.Struct{
			strconv.Itoa(landI + 1): {Kind: "wall", HP: 40, Owner: "u_a", Dir: 1, Lvl: 2},
			strconv.Itoa(landI + 2): {Kind: "campfire", HP: 20, Owner: "u_a", Lvl: 1},
		},
		Digs:        []int{landI + 3},
		Torches:     []int{landI + 3},
		Furn:        map[string]*persist.Furn{strconv.Itoa(landI + 4): {Kind: "bed", Owner: "u_a", Z: 1}},
		BrokenBergs: []int{landI + 11},
		Farms:       map[string]*persist.Farm{strconv.Itoa(landI + 5): {Crop: "wheat", Elapsed: 300, Owner: "u_a"}},
		ChestInv:    map[string]map[string]int{strconv.Itoa(landI + 6): {"wood": 7}},
		Modules:     map[string]*persist.Module{modKey(landI+7, "floor"): {Kind: "mod_floor_wood", HP: 10}},
	}
}

// awayFromSpawn finds a dry tile well away from the spawn (and the world edge,
// so the tiles after it stay in bounds), so a keeper restored to it cannot be
// mistaken for one standing at spawn.
func awayFromSpawn(t *testing.T, w *world.World, spawn [2]int) int {
	t.Helper()
	for i, tile := range w.Tiles {
		x, y := i%world.SIZE, i/world.SIZE
		if tile != world.TWater && abs(x-spawn[0])+abs(y-spawn[1]) > 40 && x < world.SIZE-16 && y < world.SIZE-16 {
			return i
		}
	}
	t.Fatal("no dry tile away from the spawn")
	return 0
}

// newRoomLogged builds a room over the save at path and returns what it logged.
func newRoomLogged(t *testing.T, seed, path string) (*Room, string) {
	t.Helper()
	var buf bytes.Buffer
	r, err := New(Config{Seed: seed, World: sharedWorldOrGen(), Store: persist.NewJSONStore(path), Logger: log.New(&buf, "", 0)})
	if err != nil {
		t.Fatal(err)
	}
	return r, buf.String()
}

// TestOlderMapSaveKeepsPlayersAndResetsWorld is the "map changed" rule: a save
// from an older WorldVersion on the same seed keeps the people, the monoliths,
// the win flag and the clock, and drops every tile-indexed thing, because those
// tile indices belong to terrain that no longer exists. Keepers rejoin at the
// spawn, on the surface, and the next save is in the current version so the
// migration happens exactly once.
func TestOlderMapSaveKeepsPlayersAndResetsWorld(t *testing.T) {
	w := sharedWorldOrGen()
	landI := awayFromSpawn(t, w, world.FindSpawn(w))
	path := filepath.Join(t.TempDir(), "world.save.json")
	if err := persist.NewJSONStore(path).Save(mapChangeSnap(world.WorldVersion-1, "hearth-1", landI)); err != nil {
		t.Fatal(err)
	}

	r, logged := newRoomLogged(t, "hearth-1", path)
	if !strings.Contains(logged, "map changed") || !strings.Contains(logged, "kept 2 player profiles") {
		t.Errorf("no migration line in the log: %q", logged)
	}
	if r.day != 9 || r.time != 0.61 || !r.won || r.mono != [4]bool{true, false, true, false} {
		t.Errorf("clock / win / monoliths not kept: day=%d time=%v won=%v mono=%v", r.day, r.time, r.won, r.mono)
	}
	// Camps and the like may seed a few entries of their own at construction, so
	// look for the saved entries by index rather than counting whole maps.
	if r.structures[landI+1] != nil || r.structures[landI+2] != nil || r.furn[landI+4] != nil {
		t.Error("a saved structure / bed survived the map change")
	}
	if r.digs[landI+3] || r.torches[landI+3] || r.farms[landI+5] != nil || r.chestInv[landI+6] != nil {
		t.Error("a saved dig / torch / farm / chest survived the map change")
	}
	if _, placed := r.moduleAt(landI+7, "floor"); placed || len(r.modules) != 0 {
		t.Error("a saved module survived the map change")
	}
	if _, ok := r.removed[landI+9]; ok || r.mudTiles[landI+10] || r.sectorChops[1234] != 0 || r.brokenBergs[landI+11] {
		t.Error("saved node respawns / mud / chops / broken bergs survived the map change")
	}
	a := r.profiles["u_a"]
	if a == nil || a.Name != "Old Keeper" || a.HP != 7 || a.Hunger != 3.5 || a.Thirst != 2.5 ||
		a.Inv["wood"] != 5 || a.Inv["iron"] != 2 || len(a.Tools) != 2 || !a.Armor || len(a.Gear) != 1 {
		t.Fatalf("profile not kept whole: %+v", a)
	}
	if a.Z != 0 || a.X >= 0 || a.Y >= 0 {
		t.Errorf("profile kept a position from the old map: x=%v y=%v z=%d", a.X, a.Y, a.Z)
	}
	if r.profiles["u_b"] == nil {
		t.Fatal("second profile was not kept")
	}

	// Join: the keeper arrives at the normal spawn with the saved vitals and kit.
	s, reason := admitSession(t, r, "s1", "u_a")
	if reason != "" {
		t.Fatalf("keeper refused: %q", reason)
	}
	p := r.players[s.ID]
	if p.X != float64(r.spawn[0]) || p.Y != float64(r.spawn[1]) || p.Z != 0 {
		t.Errorf("keeper did not spawn at the world spawn: (%v,%v,z%d) want %v", p.X, p.Y, p.Z, r.spawn)
	}
	if p.LastLandX != float64(r.spawn[0]) || p.LastLandY != float64(r.spawn[1]) {
		t.Errorf("last-land point is not the spawn: (%v,%v)", p.LastLandX, p.LastLandY)
	}
	if p.HP != 7 || p.Hunger != 3.5 || p.Inv["wood"] != 5 || !p.Tools["axe"] || !p.Armor {
		t.Errorf("vitals / kit not restored on join: hp=%d hunger=%v inv=%v tools=%v armor=%v", p.HP, p.Hunger, p.Inv, p.Tools, p.Armor)
	}

	// Save: current version, the old world gone, the joined keeper now carries
	// the spawn position, and loading that is an ordinary load, not a second
	// migration.
	if err := r.saveGame(); err != nil {
		t.Fatal(err)
	}
	saved, err := persist.NewJSONStore(path).Load()
	if err != nil || saved == nil {
		t.Fatalf("reload: %v %v", saved, err)
	}
	if saved.Version != world.WorldVersion {
		t.Errorf("migrated world saved as v%d, want v%d", saved.Version, world.WorldVersion)
	}
	if saved.Structures[strconv.Itoa(landI+1)] != nil || len(saved.Digs) != 0 || len(saved.Farms) != 0 || len(saved.Modules) != 0 {
		t.Errorf("old world leaked back into the save: %+v", saved)
	}
	if sp := saved.Profiles["u_a"]; sp == nil || sp.X != float64(r.spawn[0]) || sp.Y != float64(r.spawn[1]) || sp.Z != 0 {
		t.Errorf("saved keeper is not at spawn: %+v", sp)
	}
	again, logged := newRoomLogged(t, "hearth-1", path)
	if strings.Contains(logged, "map changed") || !strings.Contains(logged, "save loaded") {
		t.Errorf("second load was not a normal load: %q", logged)
	}
	if again.day != 9 || !again.won || again.profiles["u_a"] == nil || again.profiles["u_a"].HP != 7 {
		t.Errorf("second load lost the kept state: day=%d won=%v", again.day, again.won)
	}
}

// TestOtherSeedOrNewerSaveIsDiscarded: only an older map keeps its players. A
// save from a different seed is someone else's world, and one from a newer build
// (or with no version) holds state this build cannot read, so all start fresh
// (profiles included).
func TestOtherSeedOrNewerSaveIsDiscarded(t *testing.T) {
	w := sharedWorldOrGen()
	landI := awayFromSpawn(t, w, world.FindSpawn(w))
	for _, tc := range []struct {
		name, seed string
		version    int
	}{
		{"different seed, current version", "some-other-seed", world.WorldVersion},
		{"different seed, older version", "some-other-seed", world.WorldVersion - 1},
		{"same seed, newer version", "hearth-1", world.WorldVersion + 1},
		{"same seed, no version", "hearth-1", 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "world.save.json")
			if err := persist.NewJSONStore(path).Save(mapChangeSnap(tc.version, tc.seed, landI)); err != nil {
				t.Fatal(err)
			}
			r, logged := newRoomLogged(t, "hearth-1", path)
			if !strings.Contains(logged, "starting fresh") {
				t.Errorf("save was not reported as discarded: %q", logged)
			}
			if len(r.profiles) != 0 || r.structures[landI+1] != nil || r.digs[landI+3] || r.farms[landI+5] != nil {
				t.Errorf("a discarded save still loaded: %d profiles", len(r.profiles))
			}
			if r.day != 1 || r.won || r.mono != [4]bool{} {
				t.Errorf("a discarded save's clock / win state loaded: day=%d won=%v mono=%v", r.day, r.won, r.mono)
			}
		})
	}
}

// TestCurrentVersionSaveLoadsNormally: nothing about the migration may touch a
// save from this very map. The world loads, and a keeper returns to where they
// logged out rather than to the spawn.
func TestCurrentVersionSaveLoadsNormally(t *testing.T) {
	w := sharedWorldOrGen()
	landI := awayFromSpawn(t, w, world.FindSpawn(w))
	path := filepath.Join(t.TempDir(), "world.save.json")
	if err := persist.NewJSONStore(path).Save(mapChangeSnap(world.WorldVersion, "hearth-1", landI)); err != nil {
		t.Fatal(err)
	}
	r, logged := newRoomLogged(t, "hearth-1", path)
	if strings.Contains(logged, "map changed") || !strings.Contains(logged, "save loaded") {
		t.Errorf("a current-version save did not load normally: %q", logged)
	}
	if r.day != 9 || !r.won || r.structures[landI+1] == nil || !r.digs[landI+3] || r.farms[landI+5] == nil ||
		r.chestInv[landI+6]["wood"] != 7 || r.furn[landI+4] == nil || len(r.modules) != 1 {
		t.Errorf("world state not loaded: day=%d structures=%d digs=%d farms=%d", r.day, len(r.structures), len(r.digs), len(r.farms))
	}
	s, reason := admitSession(t, r, "s1", "u_a")
	if reason != "" {
		t.Fatalf("keeper refused: %q", reason)
	}
	p := r.players[s.ID]
	if want := landI; ti(p.X, p.Y) != want {
		t.Errorf("keeper was not restored to their saved tile: (%v,%v) want tile %d", p.X, p.Y, want)
	}
}

// TestMigrationBacksUpTheOldSave: the migration is destructive (the next save
// replaces the old world), so the old file is first copied, as-is, beside itself.
// The copy is made once: a crash and rerun must not replace it with a save that
// was already half-migrated. Saves that are not migrated leave no backup.
func TestMigrationBacksUpTheOldSave(t *testing.T) {
	w := sharedWorldOrGen()
	landI := awayFromSpawn(t, w, world.FindSpawn(w))
	old := world.WorldVersion - 1
	dir := t.TempDir()
	path := filepath.Join(dir, "world.save.json")
	bak := path + ".v" + strconv.Itoa(old) + ".bak"
	if err := persist.NewJSONStore(path).Save(mapChangeSnap(old, "hearth-1", landI)); err != nil {
		t.Fatal(err)
	}
	pristine, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	_, logged := newRoomLogged(t, "hearth-1", path)
	got, err := os.ReadFile(bak)
	if err != nil {
		t.Fatalf("no backup beside the save: %v", err)
	}
	if !bytes.Equal(got, pristine) {
		t.Error("backup is not a byte-for-byte copy of the old save")
	}
	if !strings.Contains(logged, "backed up to "+bak) {
		t.Errorf("backup path missing from the migration line: %q", logged)
	}
	if _, err := os.Stat(bak + ".tmp"); err == nil {
		t.Error("backup temp file left behind")
	}

	// Crash-and-rerun: the save on disk is still an old one (here a different one),
	// and the pristine backup must survive it untouched.
	again := mapChangeSnap(old, "hearth-1", landI)
	again.Day = 77
	if err := persist.NewJSONStore(path).Save(again); err != nil {
		t.Fatal(err)
	}
	_, logged = newRoomLogged(t, "hearth-1", path)
	if got, _ := os.ReadFile(bak); !bytes.Equal(got, pristine) {
		t.Error("a rerun replaced the pristine backup")
	}
	if !strings.Contains(logged, "already backed up at "+bak) {
		t.Errorf("rerun did not report the existing backup: %q", logged)
	}

	// Nothing to migrate, nothing to back up: a current-version save and a save
	// from another seed (neither is migrated) leave no new .bak.
	for _, snap := range []*persist.Snapshot{
		mapChangeSnap(world.WorldVersion, "hearth-1", landI),
		mapChangeSnap(old, "some-other-seed", landI),
	} {
		other := filepath.Join(t.TempDir(), "world.save.json")
		if err := persist.NewJSONStore(other).Save(snap); err != nil {
			t.Fatal(err)
		}
		newRoomLogged(t, "hearth-1", other)
		if baks, _ := filepath.Glob(other + ".v*.bak"); len(baks) != 0 {
			t.Errorf("a save that was not migrated got a backup: %v", baks)
		}
	}

	// No save file, no backup, no error.
	if p, created, err := persist.NewJSONStore(filepath.Join(t.TempDir(), "none.json")).Archive(old); p != "" || created || err != nil {
		t.Errorf("archiving a missing save: %q %v %v", p, created, err)
	}
}

// A store with no backup capability (memory, tests) still migrates.
func TestMigrationWithoutArchiverStillKeepsPlayers(t *testing.T) {
	w := sharedWorldOrGen()
	landI := awayFromSpawn(t, w, world.FindSpawn(w))
	r, err := New(Config{Seed: "hearth-1", World: w, Store: memStore{mapChangeSnap(world.WorldVersion-1, "hearth-1", landI)}})
	if err != nil {
		t.Fatal(err)
	}
	if r.profiles["u_a"] == nil || r.day != 9 || r.structures[landI+1] != nil {
		t.Errorf("migration through a plain store went wrong: profile=%v day=%d", r.profiles["u_a"], r.day)
	}
}
