// Command importsave moves one world's JSON save into Postgres, so a world that
// has been saving to gameserver/world.<worldId>.save.json can switch to
// HEARTH_STORE=postgres without losing anything.
//
//	cd gameserver
//	DATABASE_URL=postgres://... go run ./cmd/importsave -world default -file world.default.save.json
//
// It reads the file through persist.JSONStore and writes it through
// pgstore.PgStore — the same two code paths the server uses — then reads it
// back and checks the round trip is exact.
//
// Idempotent: if the database already holds exactly this save, it does
// nothing. If it holds a DIFFERENT save for the world it refuses, because that
// is most likely the world having been played on Postgres since — newer than
// the file. -force overwrites it anyway. Stop hearthd (or keep the world on
// JSON) while importing: the running room is the save's only writer.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"reflect"
	"regexp"
	"time"

	"hearth/gameserver/persist"
	"hearth/gameserver/persist/pgstore"
)

// The same rule hosting.normalizeWorld and the worlds table enforce.
var worldIDRule = regexp.MustCompile(`^[A-Za-z0-9._-]{1,64}$`)

func main() {
	worldID := flag.String("world", "", "worldId to import into (required)")
	file := flag.String("file", "", "path to the JSON save, e.g. world.default.save.json (required)")
	force := flag.Bool("force", false, "overwrite a different save already in the database for this world")
	flag.Parse()

	if *worldID == "" || *file == "" {
		flag.Usage()
		os.Exit(2)
	}
	if !worldIDRule.MatchString(*worldID) || *worldID == "." || *worldID == ".." {
		fail("worldId %q must be 1-64 chars of [A-Za-z0-9._-]", *worldID)
	}
	if _, err := os.Stat(*file); err != nil {
		fail("%v", err)
	}
	snap, err := persist.NewJSONStore(*file).Load()
	if err != nil {
		fail("%v", err)
	}
	if snap == nil {
		fail("%s holds no save", *file)
	}

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fail("DATABASE_URL is not set (postgres://user:pass@host:5432/db)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	pool, err := pgstore.Connect(ctx, url)
	if err != nil {
		fail("%v", err)
	}
	defer pool.Close()
	if err := pgstore.RequireSchema(ctx, pool); err != nil {
		fail("%v", err)
	}

	store := pgstore.New(pool, *worldID, pgstore.Options{OpTimeout: 30 * time.Second})
	existing, err := store.Load()
	if err != nil {
		fail("%v", err)
	}
	switch {
	case existing == nil:
	case reflect.DeepEqual(existing, snap):
		_ = store.Close()
		fmt.Printf("world %q: the database already holds this exact save — nothing to do\n", *worldID)
		return
	case !*force:
		_ = store.Close()
		fail("world %q already has a different save in the database (day %d, %d profiles; the file is day %d, %d profiles) — it may be newer than the file. Re-run with -force to overwrite it",
			*worldID, existing.Day, len(existing.Profiles), snap.Day, len(snap.Profiles))
	default:
		fmt.Printf("world %q: -force: overwriting the save in the database (day %d) with the file (day %d)\n",
			*worldID, existing.Day, snap.Day)
	}

	if err := store.Save(snap); err != nil {
		fail("%v", err)
	}
	if err := store.Close(); err != nil { // Close flushes: the write has landed or failed
		fail("%v", err)
	}
	verify := pgstore.New(pool, *worldID, pgstore.Options{OpTimeout: 30 * time.Second})
	back, err := verify.Load()
	_ = verify.Close()
	if err != nil {
		fail("read-back: %v", err)
	}
	if !reflect.DeepEqual(back, snap) {
		fail("read-back differs from the file — the import is not exact; the database now holds the import, inspect it before switching the world over")
	}
	fmt.Printf("world %q: imported %s (v%d, seed %s, day %d, %d profiles, %d structures, %d modules, %d farms)\n",
		*worldID, *file, snap.Version, snap.Seed, snap.Day, len(snap.Profiles), len(snap.Structures), len(snap.Modules), len(snap.Farms))
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "importsave: "+format+"\n", args...)
	os.Exit(1)
}
