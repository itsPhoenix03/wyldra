// Command dbmigrate applies the Postgres schema (persist/pgstore/migrations)
// to the database in DATABASE_URL. It is the only thing that runs DDL: hearthd
// refuses to boot against a schema that is behind, and the control plane never
// migrates. Safe to run repeatedly and concurrently — each file is applied once,
// in order, in its own transaction, under an advisory lock.
//
//	cd gameserver
//	DATABASE_URL=postgres://user:pass@host:5432/hearth go run ./cmd/dbmigrate
//	DATABASE_URL=... go run ./cmd/dbmigrate -status   # list, apply nothing
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"hearth/gameserver/persist/pgstore"
)

func main() {
	status := flag.Bool("status", false, "list pending migrations and exit without applying them")
	timeout := flag.Duration("timeout", 2*time.Minute, "give up after this long")
	flag.Parse()

	url := os.Getenv("DATABASE_URL")
	if url == "" {
		fail("DATABASE_URL is not set (postgres://user:pass@host:5432/db)")
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	pool, err := pgstore.Connect(ctx, url)
	if err != nil {
		fail("%v", err)
	}
	defer pool.Close()
	fmt.Printf("database: %s\n", pgstore.Redact(url))

	if *status {
		pending, err := pgstore.Pending(ctx, pool)
		if err != nil {
			fail("%v", err)
		}
		if len(pending) == 0 {
			fmt.Println("schema is up to date")
			return
		}
		for _, m := range pending {
			fmt.Printf("pending: %04d_%s\n", m.Version, m.Name)
		}
		return
	}

	applied, err := pgstore.Migrate(ctx, pool)
	for _, m := range applied {
		fmt.Printf("applied: %04d_%s\n", m.Version, m.Name)
	}
	if err != nil {
		fail("%v", err)
	}
	if len(applied) == 0 {
		fmt.Println("schema is up to date")
	}
}

func fail(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "dbmigrate: "+format+"\n", args...)
	os.Exit(1)
}
