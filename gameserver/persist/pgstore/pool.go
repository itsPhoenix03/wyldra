package pgstore

import (
	"context"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Connect opens the process-wide pool for databaseURL and proves the database
// is reachable before returning, so a wrong URL or a down server fails at boot
// rather than on the first save. Every world's PgStore shares this one pool.
func Connect(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// pgx's parse errors can echo the URL, password included; never log it.
		return nil, fmt.Errorf("pgstore: DATABASE_URL is not a valid Postgres connection string")
	}
	cfg.ConnConfig.RuntimeParams["application_name"] = "hearthd"
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("pgstore: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, opTimeoutDefault)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("pgstore: cannot reach %s: %w", Redact(databaseURL), err)
	}
	return pool, nil
}

// RequireSchema fails unless every embedded migration has been applied, naming
// the command that fixes it.
func RequireSchema(ctx context.Context, pool *pgxpool.Pool) error {
	pending, err := Pending(ctx, pool)
	if err != nil {
		return err
	}
	if len(pending) == 0 {
		return nil
	}
	names := make([]string, len(pending))
	for i, m := range pending {
		names[i] = fmt.Sprintf("%04d_%s", m.Version, m.Name)
	}
	return fmt.Errorf("pgstore: the database schema is behind this build (pending: %s) — run `go run ./cmd/dbmigrate` from gameserver/",
		strings.Join(names, ", "))
}

// Redact returns host/database of a connection string, for logs: never the
// user or password.
func Redact(databaseURL string) string {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		return "<unparseable DATABASE_URL>"
	}
	c := cfg.ConnConfig
	return fmt.Sprintf("%s:%d/%s", c.Host, c.Port, c.Database)
}
