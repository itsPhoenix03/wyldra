package hosting

import (
	"context"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
)

// The default must stay exactly what it was before Postgres existed: JSON
// files, no database needed.
func TestStoreDefaultsToJSON(t *testing.T) {
	isolateEnv(t)
	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != StoreJSON {
		t.Fatalf("Store = %q, want %q", cfg.Store, StoreJSON)
	}
	if got, want := cfg.SaveTarget("default"), cfg.SavePath("default"); got != want {
		t.Errorf("SaveTarget = %q, want the JSON save path %q", got, want)
	}
}

// Asking for Postgres without saying where it is must stop the boot, never
// quietly save to JSON instead.
func TestPostgresWithoutDatabaseURLFailsFast(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", "postgres")
	_, err := LoadConfig(nil)
	if err == nil {
		t.Fatal("HEARTH_STORE=postgres without DATABASE_URL booted")
	}
	if !strings.Contains(err.Error(), "DATABASE_URL") {
		t.Errorf("error should name DATABASE_URL: %v", err)
	}
}

func TestPostgresStoreConfig(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", " Postgres ")
	t.Setenv("DATABASE_URL", "postgres://u:secret@db:5432/hearth")
	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Store != StorePostgres || cfg.DatabaseURL == "" {
		t.Fatalf("Store=%q DatabaseURL set=%v, want postgres and the URL", cfg.Store, cfg.DatabaseURL != "")
	}
	if got := cfg.SaveTarget("w1"); got != "postgres:world_snapshots/w1" {
		t.Errorf("SaveTarget = %q", got)
	}
	if strings.Contains(cfg.Summary(), "secret") {
		t.Errorf("Summary leaks the database password: %s", cfg.Summary())
	}
}

// HEARTH_NO_PERSIST wins: nothing is saved, so no database is required.
func TestNoPersistWinsOverPostgres(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", "postgres")
	t.Setenv("HEARTH_NO_PERSIST", "1")
	if _, err := LoadConfig(func(string, ...any) {}); err != nil {
		t.Fatalf("HEARTH_NO_PERSIST with HEARTH_STORE=postgres and no URL: %v", err)
	}
}

func TestUnknownStoreIsRefused(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", "sqlite")
	if _, err := LoadConfig(nil); err == nil {
		t.Fatal("HEARTH_STORE=sqlite booted")
	}
}

// A Postgres world with no pool must refuse to build rather than fall back.
func TestPostgresWorldWithoutPoolRefusesToBuild(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", "postgres")
	t.Setenv("DATABASE_URL", "postgres://u:p@127.0.0.1:1/x")
	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewManager(Options{Config: cfg, Logger: quietLogger(t), Persist: true})
	if _, err := m.openStore(cfg.Worlds[0]); err == nil {
		t.Fatal("openStore built a Postgres world with no database pool")
	}
}

// An unreachable database at build time must refuse the world, not start it
// empty: room.New treats a Load error as "no save", and the first save would
// then overwrite the real one.
func TestUnreadableDatabaseRefusesToBuildTheWorld(t *testing.T) {
	isolateEnv(t)
	t.Setenv("HEARTH_STORE", "postgres")
	t.Setenv("DATABASE_URL", "postgres://u:p@127.0.0.1:1/x?sslmode=disable&connect_timeout=2")
	cfg, err := LoadConfig(nil)
	if err != nil {
		t.Fatal(err)
	}
	pool, err := pgxpool.New(context.Background(), cfg.DatabaseURL) // lazy; nothing listens on :1
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	m := NewManager(Options{Config: cfg, Logger: quietLogger(t), Persist: true, DB: pool})
	_, err = m.openStore(cfg.Worlds[0])
	if err == nil || !strings.Contains(err.Error(), "not starting it empty") {
		t.Fatalf("openStore with an unreachable database = %v, want a refusal", err)
	}
}
