package pgstore

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"regexp"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// The schema is plain, ordered SQL files — the one source of truth for both
// services. They are embedded here because the Go binary is what applies them
// (`go run ./cmd/dbmigrate`); the Node control plane reads the same tables and
// must never run DDL of its own.
//
//go:embed migrations/*.sql
var migrationFS embed.FS

// Migration is one schema file, migrations/NNNN_name.sql.
type Migration struct {
	Version  int
	Name     string
	SQL      string
	Checksum string // sha256 of SQL; an applied file must never change
}

var migrationName = regexp.MustCompile(`^(\d{4})_([a-z0-9_]+)\.sql$`)

// migrationLockKey serialises concurrent runners (two hearthd boots, a deploy
// job and a developer) on one advisory lock. Any constant works; this one is
// "hearth" in ASCII.
const migrationLockKey int64 = 0x6865617274680001

// Migrations returns every embedded migration in version order.
func Migrations() ([]Migration, error) {
	return parseMigrations(migrationFS)
}

func parseMigrations(fsys fs.FS) ([]Migration, error) {
	files, err := fs.Glob(fsys, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	var out []Migration
	seen := map[int]string{}
	for _, f := range files {
		base := f[len("migrations/"):]
		m := migrationName.FindStringSubmatch(base)
		if m == nil {
			return nil, fmt.Errorf("pgstore: migration %q is not named NNNN_name.sql", base)
		}
		v, _ := strconv.Atoi(m[1])
		if prev, dup := seen[v]; dup {
			return nil, fmt.Errorf("pgstore: migrations %q and %q share version %d", prev, base, v)
		}
		seen[v] = base
		b, err := fs.ReadFile(fsys, f)
		if err != nil {
			return nil, err
		}
		sum := sha256.Sum256(b)
		out = append(out, Migration{Version: v, Name: m[2], SQL: string(b), Checksum: hex.EncodeToString(sum[:])})
	}
	sort.Slice(out, func(a, b int) bool { return out[a].Version < out[b].Version })
	return out, nil
}

const createMigrationsTable = `CREATE TABLE IF NOT EXISTS schema_migrations (
    version    integer     PRIMARY KEY,
    name       text        NOT NULL,
    checksum   text        NOT NULL,
    applied_at timestamptz NOT NULL DEFAULT now()
)`

// Migrate applies every pending migration, each in its own transaction, and
// returns the ones it applied. It holds an advisory lock for the whole run, so
// concurrent runners apply each file exactly once. An applied migration whose
// file has since changed is an error: schema history is append-only, so the
// fix is a new file, never an edit.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]Migration, error) {
	all, err := Migrations()
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, "SELECT pg_advisory_lock($1)", migrationLockKey); err != nil {
		return nil, fmt.Errorf("pgstore: migration lock: %w", err)
	}
	defer func() {
		// A fresh context: the caller's may be the reason we are leaving.
		unlockCtx, cancel := context.WithTimeout(context.Background(), opTimeoutDefault)
		defer cancel()
		_, _ = conn.Exec(unlockCtx, "SELECT pg_advisory_unlock($1)", migrationLockKey)
	}()

	if _, err := conn.Exec(ctx, createMigrationsTable); err != nil {
		return nil, fmt.Errorf("pgstore: create schema_migrations: %w", err)
	}
	pending, err := pendingOn(ctx, conn.Conn(), all)
	if err != nil {
		return nil, err
	}
	var applied []Migration
	for _, m := range pending {
		err := pgx.BeginFunc(ctx, conn.Conn(), func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, m.SQL); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, "INSERT INTO schema_migrations (version, name, checksum) VALUES ($1, $2, $3)",
				m.Version, m.Name, m.Checksum)
			return err
		})
		if err != nil {
			return applied, fmt.Errorf("pgstore: migration %04d_%s: %w", m.Version, m.Name, err)
		}
		applied = append(applied, m)
	}
	return applied, nil
}

// Pending reports the migrations not yet applied, without applying anything.
// hearthd calls it at boot so a stale schema fails fast with an instruction,
// instead of failing on the first save minutes later.
func Pending(ctx context.Context, pool *pgxpool.Pool) ([]Migration, error) {
	all, err := Migrations()
	if err != nil {
		return nil, err
	}
	conn, err := pool.Acquire(ctx)
	if err != nil {
		return nil, fmt.Errorf("pgstore: connect: %w", err)
	}
	defer conn.Release()
	var exists bool
	if err := conn.QueryRow(ctx, "SELECT to_regclass('schema_migrations') IS NOT NULL").Scan(&exists); err != nil {
		return nil, fmt.Errorf("pgstore: read schema_migrations: %w", err)
	}
	if !exists {
		return all, nil
	}
	return pendingOn(ctx, conn.Conn(), all)
}

func pendingOn(ctx context.Context, conn *pgx.Conn, all []Migration) ([]Migration, error) {
	rows, err := conn.Query(ctx, "SELECT version, name, checksum FROM schema_migrations")
	if err != nil {
		return nil, fmt.Errorf("pgstore: read schema_migrations: %w", err)
	}
	type row struct{ name, sum string }
	done := map[int]row{}
	for rows.Next() {
		var v int
		var r row
		if err := rows.Scan(&v, &r.name, &r.sum); err != nil {
			rows.Close()
			return nil, err
		}
		done[v] = r
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	known := map[int]bool{}
	var pending []Migration
	for _, m := range all {
		known[m.Version] = true
		r, ok := done[m.Version]
		if !ok {
			pending = append(pending, m)
			continue
		}
		if r.sum != m.Checksum {
			return nil, fmt.Errorf("pgstore: migration %04d_%s was edited after it was applied (checksum %s, applied %s) — add a new migration instead",
				m.Version, m.Name, short(m.Checksum), short(r.sum))
		}
	}
	for v, r := range done {
		if !known[v] {
			return nil, fmt.Errorf("pgstore: the database has migration %04d_%s, which this build does not know — it was written by a newer build", v, r.name)
		}
	}
	return pending, nil
}

func short(sum string) string {
	if len(sum) > 12 {
		return sum[:12]
	}
	return sum
}
