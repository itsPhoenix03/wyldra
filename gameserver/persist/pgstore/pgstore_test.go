package pgstore

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hearth/gameserver/persist"
	"hearth/gameserver/persist/persisttest"
)

// The tests in this file need a real Postgres and skip without one:
//
//	HEARTH_TEST_DATABASE_URL=postgres://user:pass@localhost:5432/hearth_test go test ./persist/pgstore/
//
// Each test creates its own throwaway schema (hearth_test_<time>_<random>) and
// drops it afterwards, so they are safe against a shared database — the role
// only needs CREATE on the database.

const skipMsg = "HEARTH_TEST_DATABASE_URL is not set — skipping the Postgres tests (see docs/11_POSTGRES_SETUP.md)"

// testPool returns a pool whose search_path is a fresh, empty schema, plus the
// schema's name.
func testPool(t *testing.T) (*pgxpool.Pool, string) {
	t.Helper()
	url := os.Getenv("HEARTH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip(skipMsg)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	admin, err := Connect(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	schema := fmt.Sprintf("hearth_test_%d_%s", time.Now().Unix(), hex.EncodeToString(b))
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+schema); err != nil {
		admin.Close()
		t.Fatalf("create test schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := admin.Exec(ctx, "DROP SCHEMA "+schema+" CASCADE"); err != nil {
			t.Logf("could not drop test schema %s: %v", schema, err)
		}
		admin.Close()
	})

	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		t.Fatal(err)
	}
	cfg.ConnConfig.RuntimeParams["search_path"] = schema
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close) // registered after the drop, so it runs before it
	return pool, schema
}

func migrated(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, _ := testPool(t)
	if _, err := Migrate(context.Background(), pool); err != nil {
		t.Fatal(err)
	}
	return pool
}

// TestPgStoreContract: PgStore must give back exactly what JSONStore would.
func TestPgStoreContract(t *testing.T) {
	pool := migrated(t)
	persisttest.Run(t, persisttest.Backend{
		Open: func(t *testing.T, worldID string) persist.Store {
			s := New(pool, worldID, Options{})
			t.Cleanup(func() {
				if err := s.Close(); err != nil {
					t.Error(err)
				}
			})
			return s
		},
		ReadArchive: func(t *testing.T, s persist.Store, _ string, version int) *persist.Snapshot {
			snap, err := s.(*PgStore).ReadArchive(version)
			if err != nil {
				t.Fatal(err)
			}
			return snap
		},
	})
}

// TestCloseFlushesTheFinalSave is the shutdown path: the room's last Save is
// immediately followed by Close, and that save must be in the database after.
func TestCloseFlushesTheFinalSave(t *testing.T) {
	pool := migrated(t)
	s := New(pool, "final", Options{})
	for day := 1; day <= 20; day++ {
		snap := persisttest.Full(5)
		snap.Day = day
		if err := s.Save(snap); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	got, err := New(pool, "final", Options{}).Load()
	if err != nil || got == nil {
		t.Fatalf("Load after Close = (%v, %v)", got, err)
	}
	if got.Day != 20 {
		t.Fatalf("Load after Close has day %d, want the final save's 20", got.Day)
	}
}

func TestMigrateIsIdempotent(t *testing.T) {
	pool, _ := testPool(t)
	ctx := context.Background()

	pending, err := Pending(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	all, _ := Migrations()
	if len(pending) != len(all) {
		t.Fatalf("fresh schema: %d pending, want all %d", len(pending), len(all))
	}
	if err := RequireSchema(ctx, pool); err == nil || !strings.Contains(err.Error(), "dbmigrate") {
		t.Fatalf("RequireSchema on a fresh schema = %v, want an error naming cmd/dbmigrate", err)
	}

	applied, err := Migrate(ctx, pool)
	if err != nil {
		t.Fatal(err)
	}
	if len(applied) != len(all) {
		t.Fatalf("applied %d, want %d", len(applied), len(all))
	}
	again, err := Migrate(ctx, pool)
	if err != nil || len(again) != 0 {
		t.Fatalf("second Migrate applied %d (err %v), want 0", len(again), err)
	}
	if err := RequireSchema(ctx, pool); err != nil {
		t.Fatalf("RequireSchema after Migrate: %v", err)
	}
}

func TestMigrateRunsConcurrentlyOnce(t *testing.T) {
	pool, _ := testPool(t)
	errs := make(chan error, 4)
	for i := 0; i < 4; i++ {
		go func() {
			_, err := Migrate(context.Background(), pool)
			errs <- err
		}()
	}
	for i := 0; i < 4; i++ {
		if err := <-errs; err != nil {
			t.Fatalf("concurrent Migrate: %v", err)
		}
	}
	var n int
	if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM schema_migrations").Scan(&n); err != nil {
		t.Fatal(err)
	}
	if all, _ := Migrations(); n != len(all) {
		t.Fatalf("schema_migrations has %d rows, want %d", n, len(all))
	}
}

func TestMigrateRefusesAnEditedOrUnknownMigration(t *testing.T) {
	pool := migrated(t)
	ctx := context.Background()

	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET checksum = 'tampered' WHERE version = 1"); err != nil {
		t.Fatal(err)
	}
	if _, err := Migrate(ctx, pool); err == nil || !strings.Contains(err.Error(), "edited") {
		t.Fatalf("Migrate with an edited migration = %v, want a refusal", err)
	}
	all, _ := Migrations()
	if _, err := pool.Exec(ctx, "UPDATE schema_migrations SET checksum = $1 WHERE version = 1", all[0].Checksum); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES (9999, 'from_the_future', 'x')"); err != nil {
		t.Fatal(err)
	}
	if _, err := Pending(ctx, pool); err == nil || !strings.Contains(err.Error(), "newer build") {
		t.Fatalf("Pending with an unknown applied migration = %v, want a refusal", err)
	}
}
