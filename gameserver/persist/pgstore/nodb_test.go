package pgstore

import (
	"context"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hearth/gameserver/persist/persisttest"
)

// These tests need no database. They pin down the property the whole package
// is shaped around: a slow or hung Postgres must never stall the room
// goroutine that calls Save.

// blackHole accepts TCP connections and never answers — a Postgres that is
// up at the network level and hung above it, the worst case for a caller
// (no fast "connection refused").
func blackHole(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	var mu sync.Mutex
	var conns []net.Conn
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			conns = append(conns, c)
			mu.Unlock()
		}
	}()
	t.Cleanup(func() {
		ln.Close()
		mu.Lock()
		for _, c := range conns {
			c.Close()
		}
		mu.Unlock()
	})
	return "postgres://hearth:x@" + ln.Addr().String() + "/hearth?sslmode=disable"
}

func hungStore(t *testing.T, opt Options) *PgStore {
	t.Helper()
	pool, err := pgxpool.New(context.Background(), blackHole(t)) // lazy: no connection yet
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return New(pool, "hung", opt)
}

func TestSaveNeverBlocksOnAHungDatabase(t *testing.T) {
	s := hungStore(t, Options{OpTimeout: 300 * time.Millisecond, FlushTimeout: 400 * time.Millisecond})

	// If Save waited on the database, each of these would cost a full
	// OpTimeout (300ms) and the loop 15s. A room ticks every 200ms.
	const n = 50
	began := time.Now()
	for i := 0; i < n; i++ {
		snap := persisttest.Full(5)
		snap.Day = i
		_ = s.Save(snap)
	}
	if el := time.Since(began); el > time.Second {
		t.Fatalf("%d Saves against a hung database took %s — Save is blocking on I/O", n, el)
	}

	// Coalesced: however many saves queued up, one pending snapshot is held,
	// and it is the newest.
	s.mu.Lock()
	next, queued := s.next, s.queued
	s.mu.Unlock()
	if queued != n {
		t.Fatalf("queued = %d, want %d", queued, n)
	}
	if next != nil && next.seq != n {
		t.Fatalf("pending snapshot is seq %d, want the newest (%d)", next.seq, n)
	}

	// Close is bounded by FlushTimeout (plus the in-flight call's own
	// OpTimeout), and says the final save did not land.
	began = time.Now()
	err := s.Close()
	if err == nil {
		t.Fatal("Close reported success with the database unreachable")
	}
	if el := time.Since(began); el > 3*time.Second {
		t.Fatalf("Close took %s, want it bounded by FlushTimeout", el)
	}
	if !strings.Contains(err.Error(), "did not land") {
		t.Errorf("Close error should say the final save was lost, got: %v", err)
	}
	if err := s.Save(persisttest.Full(5)); err == nil {
		t.Error("Save after Close succeeded")
	}
}

func TestSaveReportsTheLastBackgroundFailureOnce(t *testing.T) {
	s := hungStore(t, Options{OpTimeout: 100 * time.Millisecond, FlushTimeout: 100 * time.Millisecond})
	defer s.Close()

	s.mu.Lock()
	attempt := s.attempt
	s.mu.Unlock()
	if err := s.Save(persisttest.Full(5)); err != nil {
		t.Fatalf("first Save: %v", err)
	}
	select {
	case <-attempt:
	case <-time.After(3 * time.Second):
		t.Fatal("the background write never finished its attempt")
	}
	err := s.Save(persisttest.Full(5))
	if err == nil || !strings.Contains(err.Error(), "save world hung") {
		t.Fatalf("Save after a failed write = %v, want that failure", err)
	}
	// Reported once: the room logs it once, not on every save while the same
	// failure stands.
	if err := s.Save(persisttest.Full(5)); err != nil {
		t.Fatalf("the same failure was reported twice: %v", err)
	}
}

func TestLoadAndArchiveAreBounded(t *testing.T) {
	s := hungStore(t, Options{OpTimeout: 200 * time.Millisecond, FlushTimeout: 200 * time.Millisecond})
	defer s.Close()

	began := time.Now()
	if snap, err := s.Load(); err == nil {
		t.Fatalf("Load against a hung database = (%v, nil), want an error", snap)
	}
	if _, _, err := s.Archive(3); err == nil {
		t.Fatal("Archive against a hung database succeeded")
	}
	if el := time.Since(began); el > 3*time.Second {
		t.Fatalf("Load+Archive took %s against a hung database, want them bounded by OpTimeout", el)
	}
}

func TestEmbeddedMigrationsAreWellFormed(t *testing.T) {
	ms, err := Migrations()
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) == 0 {
		t.Fatal("no migrations embedded")
	}
	for i, m := range ms {
		if m.Version != i+1 {
			t.Errorf("migration %d is %04d_%s: versions must run 0001, 0002, ... with no gaps", i, m.Version, m.Name)
		}
		if strings.TrimSpace(m.SQL) == "" || len(m.Checksum) != 64 {
			t.Errorf("migration %04d_%s is empty or unsummed", m.Version, m.Name)
		}
	}
	// The tables the stores and the control plane are written against.
	for _, table := range []string{"players", "worlds", "world_members", "world_snapshots", "player_world_profiles", "world_snapshot_archive"} {
		if !strings.Contains(ms[0].SQL, "CREATE TABLE "+table+" (") {
			t.Errorf("0001 does not create %s", table)
		}
	}
}

func TestMigrationNamesAreValidated(t *testing.T) {
	for name, fsys := range map[string]fstest.MapFS{
		"bad name":      {"migrations/init.sql": {Data: []byte("SELECT 1")}},
		"dup version":   {"migrations/0001_a.sql": {Data: []byte("SELECT 1")}, "migrations/0001_b.sql": {Data: []byte("SELECT 1")}},
		"upper in name": {"migrations/0001_Init.sql": {Data: []byte("SELECT 1")}},
	} {
		if _, err := parseMigrations(fsys); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	ms, err := parseMigrations(fstest.MapFS{
		"migrations/0002_b.sql": {Data: []byte("SELECT 2")},
		"migrations/0001_a.sql": {Data: []byte("SELECT 1")},
	})
	if err != nil || len(ms) != 2 || ms[0].Version != 1 || ms[1].Name != "b" {
		t.Fatalf("parseMigrations = %+v, %v; want 0001_a then 0002_b", ms, err)
	}
}

func TestRedactNeverShowsTheCredentials(t *testing.T) {
	got := Redact("postgres://hearth:s3cret@db.example:6543/game?sslmode=require")
	if strings.Contains(got, "s3cret") || strings.Contains(got, "hearth:") {
		t.Fatalf("Redact leaked credentials: %q", got)
	}
	if got != "db.example:6543/game" {
		t.Errorf("Redact = %q, want db.example:6543/game", got)
	}
}
