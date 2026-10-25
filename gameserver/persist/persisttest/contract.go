// Package persisttest is the behaviour every persist.Store must share, written
// once and run against each implementation. The room was built and tested
// against JSONStore; any other store is only a drop-in if it gives back exactly
// what JSONStore would, field for field. Imported by tests only.
package persisttest

import (
	"context"
	"fmt"
	"reflect"
	"testing"
	"time"

	"hearth/gameserver/persist"
)

// Backend opens stores on one shared backing (a directory, a database schema).
type Backend struct {
	// Open returns a store for worldID. Two calls with the same worldID must
	// reach the same saved data — that is what "durable" means here.
	Open func(t *testing.T, worldID string) persist.Store
	// ReadArchive returns the archived save for (worldID, version), or nil.
	ReadArchive func(t *testing.T, s persist.Store, worldID string, version int) *persist.Snapshot
}

// Full is a snapshot with every field set, the way a played world looks.
func Full(version int) *persist.Snapshot {
	return &persist.Snapshot{
		Version: version, Seed: "hearth-1", Day: 12, Time: 0.4375, Won: true,
		Mono: [4]bool{true, false, true, false},
		Profiles: map[string]*persist.Profile{
			"u_0123456789abcdef01234567": {
				Name: "Keeper", X: 812.5, Y: 403.25, Z: 1, HP: 7, Hunger: 4.5, Thirst: 9.125,
				Inv: map[string]int{"wood": 30, "stone": 12}, Tools: []string{"axe", "pickaxe"},
				Gear: []string{"cloak", "armor"}, Worn: "cloak", Armor: true,
			},
			"u_fedcba9876543210fedcba98": {
				Name: "Wren", X: persist.NoPos, Y: persist.NoPos, HP: 10,
				Inv: map[string]int{}, Tools: []string{}, Gear: []string{},
			},
		},
		Removed:     []persist.NodeRespawn{{I: 1000, Remaining: 120000}, {I: 2048, Remaining: 1}},
		Mud:         []int{5, 6, 7},
		SectorChops: map[string]int{"1234": 8, "99": 1},
		Structures: map[string]*persist.Struct{
			"5001": {Kind: "wall", HP: 40, Owner: "u_0123456789abcdef01234567", Dir: 1, Lvl: 2},
			"5002": {Kind: "farmplot", HP: 10, Lvl: 1},
		},
		Digs:        []int{70001, 70002},
		Torches:     []int{70002},
		Furn:        map[string]*persist.Furn{"70003": {Kind: "chest", Owner: "u_0123456789abcdef01234567", Z: 1}},
		BrokenBergs: []int{88},
		// Elapsed, not absolute: the field most likely to be mis-ported.
		Farms: map[string]*persist.Farm{
			"5002": {Crop: "wheat", Elapsed: 300, Owner: "u_0123456789abcdef01234567"},
			"5003": {Crop: "glowcap", Elapsed: 9_000_000_000},
		},
		ChestInv: map[string]map[string]int{"70003": {"wood": 7, "iron": 2}},
		Modules: map[string]*persist.Module{
			"6001:0": {Kind: "plank_floor", HP: 20, Dir: 2, Owner: "u_0123456789abcdef01234567"},
			"6001:3": {Kind: "stone_wall", HP: 60},
		},
		Features: map[string]bool{"tileMountains": true, "cameraPeek": false},
	}
}

// Run is the contract. Each subtest uses its own world id.
func Run(t *testing.T, b Backend) {
	t.Run("EmptyIsNilNil", func(t *testing.T) {
		s := b.Open(t, "empty")
		snap, err := s.Load()
		if err != nil || snap != nil {
			t.Fatalf("Load on a never-saved world = (%v, %v), want (nil, nil)", snap, err)
		}
	})

	t.Run("RoundTripEveryField", func(t *testing.T) {
		want := Full(5)
		save(t, b.Open(t, "full"), want)
		got := load(t, b.Open(t, "full"))
		same(t, "reloaded snapshot", got, Full(5))
	})

	t.Run("ProfilesShape", func(t *testing.T) {
		// nil and empty profile maps come back as they went in, exactly as a
		// JSON round trip ("profiles":null vs {}) gives them.
		for _, tc := range []struct {
			id       string
			profiles map[string]*persist.Profile
		}{{"nilprofiles", nil}, {"emptyprofiles", map[string]*persist.Profile{}}} {
			snap := &persist.Snapshot{Version: 1, Seed: "s", Profiles: tc.profiles}
			save(t, b.Open(t, tc.id), snap)
			got := load(t, b.Open(t, tc.id))
			same(t, tc.id, got, &persist.Snapshot{Version: 1, Seed: "s", Profiles: tc.profiles})
		}
	})

	t.Run("SaveReplacesWholeSnapshot", func(t *testing.T) {
		s := b.Open(t, "replace")
		if err := s.Save(Full(5)); err != nil {
			t.Fatal(err)
		}
		next := Full(5)
		next.Day = 13
		delete(next.Profiles, "u_fedcba9876543210fedcba98")
		next.Profiles["u_0123456789abcdef01234567"].Inv["wood"] = 1
		next.Structures = nil
		next.Farms["5002"].Elapsed = 301
		save(t, s, next)
		// The caller owns the snapshot again the moment Save returns (the room
		// hands over its live profile map): mutating it must not leak in.
		next.Profiles["u_0123456789abcdef01234567"].Inv["wood"] = 999
		next.Day = 999

		want := Full(5)
		want.Day = 13
		delete(want.Profiles, "u_fedcba9876543210fedcba98")
		want.Profiles["u_0123456789abcdef01234567"].Inv["wood"] = 1
		want.Structures = nil
		want.Farms["5002"].Elapsed = 301
		same(t, "after a second save", load(t, b.Open(t, "replace")), want)
	})

	t.Run("WorldsAreIsolated", func(t *testing.T) {
		a := Full(5)
		save(t, b.Open(t, "iso-a"), a)
		bb := &persist.Snapshot{Version: 5, Seed: "other", Profiles: map[string]*persist.Profile{}}
		save(t, b.Open(t, "iso-b"), bb)
		same(t, "world a", load(t, b.Open(t, "iso-a")), Full(5))
		same(t, "world b", load(t, b.Open(t, "iso-b")), &persist.Snapshot{Version: 5, Seed: "other", Profiles: map[string]*persist.Profile{}})
	})

	t.Run("Archive", func(t *testing.T) {
		s := b.Open(t, "archive")
		a, ok := s.(persist.Archiver)
		if !ok {
			t.Fatalf("%T is not a persist.Archiver", s)
		}
		// nothing saved: nothing written
		if path, created, err := a.Archive(4); err != nil || created || path != "" {
			t.Fatalf("Archive with nothing saved = (%q, %v, %v), want (\"\", false, nil)", path, created, err)
		}
		if got := b.ReadArchive(t, s, "archive", 4); got != nil {
			t.Fatalf("an archive appeared with nothing saved: %+v", got)
		}

		if err := s.Save(Full(4)); err != nil {
			t.Fatal(err)
		}
		path, created, err := a.Archive(4)
		if err != nil || !created || path == "" {
			t.Fatalf("first Archive = (%q, %v, %v), want (<path>, true, nil)", path, created, err)
		}
		same(t, "archived copy", b.ReadArchive(t, s, "archive", 4), Full(4))

		// A second archive of the same version (a crash and rerun, half
		// migrated) must leave the pristine copy alone.
		changed := Full(4)
		changed.Day = 99
		changed.Profiles = map[string]*persist.Profile{}
		save(t, s, changed)
		path2, created2, err := a.Archive(4)
		if err != nil || created2 || path2 != path {
			t.Fatalf("second Archive = (%q, %v, %v), want (%q, false, nil)", path2, created2, err, path)
		}
		same(t, "archived copy after a rerun", b.ReadArchive(t, s, "archive", 4), Full(4))
		// and archiving never disturbs the live save
		want := Full(4)
		want.Day = 99
		want.Profiles = map[string]*persist.Profile{}
		same(t, "live save after archiving", load(t, b.Open(t, "archive")), want)
	})
}

// flusher is a store whose Save returns before the data is durable (PgStore).
type flusher interface{ Flush(context.Context) error }

// save saves and, for a store that writes in the background, waits until the
// save is durable, so a store opened afterwards on the same backing sees it.
func save(t *testing.T, s persist.Store, snap *persist.Snapshot) {
	t.Helper()
	if err := s.Save(snap); err != nil {
		t.Fatal(err)
	}
	if f, ok := s.(flusher); ok {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if err := f.Flush(ctx); err != nil {
			t.Fatal(err)
		}
	}
}

func load(t *testing.T, s persist.Store) *persist.Snapshot {
	t.Helper()
	snap, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if snap == nil {
		t.Fatal("Load returned nil after a save")
	}
	return snap
}

func same(t *testing.T, what string, got, want *persist.Snapshot) {
	t.Helper()
	if reflect.DeepEqual(got, want) {
		return
	}
	t.Errorf("%s differs:\n got  %s\n want %s", what, dump(got), dump(want))
}

func dump(s *persist.Snapshot) string {
	if s == nil {
		return "<nil>"
	}
	out := fmt.Sprintf("%+v", *s)
	out += " profiles={"
	for k, p := range s.Profiles {
		if p == nil {
			out += k + ":<nil> "
			continue
		}
		out += fmt.Sprintf("%s:%+v ", k, *p)
	}
	return out + "}"
}
