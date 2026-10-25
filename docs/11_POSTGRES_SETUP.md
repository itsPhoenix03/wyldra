# 11 — Postgres setup

Postgres is an opt-in save backend for the Go game server. **JSON saves stay the default**: with
nothing configured, `hearthd` boots and saves to `gameserver/world.<worldId>.save.json` exactly
as before. Redis is still deferred (`.agents/PROJECT_STATE.md` §6).

What exists today: the schema and its migration runner, the Go `PgStore` (world snapshots and
per-player profiles), a JSON-save importer, and tests. What does not: anything in the Node
control plane. See [What is left](#what-is-left).

## 1. What the database needs

- **Postgres 14 or newer** (any currently supported release; 16 or 17 recommended). Nothing
  beyond core features is used: `jsonb`, `timestamptz`, no extensions.
- **One database and one login role that owns it.** Owning the database lets the role create
  tables in `public`, which Postgres 15+ no longer grants to everyone.

```sql
CREATE ROLE hearth LOGIN PASSWORD 'change-me';
CREATE DATABASE hearth OWNER hearth;
-- optional, for the DB tests (each run creates and drops its own schema in it):
CREATE DATABASE hearth_test OWNER hearth;
```

A managed Postgres (Neon, Supabase, RDS, Cloud SQL...) works the same way. Use its connection
string and add `?sslmode=require` (or `verify-full`).

## 2. Environment

| Variable | Read by | Meaning |
|---|---|---|
| `HEARTH_STORE` | hearthd | `json` (default) or `postgres`. Any other value refuses to boot. |
| `DATABASE_URL` | hearthd, `dbmigrate`, `importsave` | `postgres://user:pass@host:5432/db?sslmode=...`. Required with `HEARTH_STORE=postgres`: boot **fails** without it and never falls back to JSON. |
| `HEARTH_NO_PERSIST` | hearthd | Wins over both of the above. Nothing is saved, so no database is needed. |
| `HEARTH_TEST_DATABASE_URL` | `go test` | Turns on the Postgres tests. Without it they skip. |

`.env.example` at the repo root lists them. Copy it to `.env`, which is gitignored. Nothing loads
`.env` automatically, so export the variables in your shell or service manager. Logs never print
the URL: hearthd logs `host:port/db` only.

## 3. Apply the schema

```bash
cd gameserver
DATABASE_URL=postgres://hearth:change-me@localhost:5432/hearth go run ./cmd/dbmigrate
DATABASE_URL=... go run ./cmd/dbmigrate -status     # list pending migrations, apply nothing
```

PowerShell: `$env:DATABASE_URL = "postgres://..."; go run ./cmd/dbmigrate`.

- The schema is `gameserver/persist/pgstore/migrations/NNNN_name.sql`, embedded into the Go
  binaries. These files are **the one source of truth for both services**. The control plane
  reads and writes the same tables and never runs DDL of its own.
- Each file runs once, in order, in its own transaction, under an advisory lock, so running it
  twice or concurrently is safe. `schema_migrations` records each file's sha256.
- **Never edit an applied migration.** The runner refuses a checksum mismatch. Add
  `0002_...sql` instead.
- `hearthd` with `HEARTH_STORE=postgres` checks the schema at boot and refuses to start if a
  migration is pending, naming this command.

### The schema (0001_init)

| Table | Written by | Columns |
|---|---|---|
| `players` | Node | `user_id` PK, `display_name`, `dev`, `created_at`, `last_seen_at` |
| `worlds` | Node | `world_id` PK, `name`, `seed`, `owner_user_id` → players (nullable), `join_code` unique (nullable), `instance_id`, `ws_url`, `max_players` (4), `visibility` (`private`/`public`), `created_at`, `updated_at`, `archived_at` |
| `world_members` | Node | (`world_id` → worlds, `user_id` → players) PK, `role` (`owner`/`member`), `joined_at`, `last_played_at` |
| `world_snapshots` | Go | `world_id` PK, `version`, `seed`, `snapshot` jsonb (the save minus profiles), `saved_at`, `fence` (0, unused) |
| `player_world_profiles` | Go | (`world_id`, `user_id`) PK, `profile` jsonb, `updated_at` |
| `world_snapshot_archive` | Go | (`world_id`, `version`) PK, `snapshot` jsonb (with profiles), `archived_at` |

The Go-written tables deliberately have **no foreign keys** to `players` or `worlds`. Go must
never write a Node table, so it cannot create a parent row itself. A save must also never fail
because the control plane is behind or down. The full reasoning is in the SQL. Once Node writes
`worlds`, a later migration can add the `world_id` FKs.

## 4. Move existing saves over

Stop `hearthd` first. A running room is the only writer of its world's save.

```bash
cd gameserver
DATABASE_URL=... go run ./cmd/importsave -world default -file world.default.save.json
```

Run it once per `world.<worldId>.save.json`. It reads the file through the same `JSONStore` the
server uses, writes it through `PgStore`, then reads it back and checks the round trip is exact.

- **Idempotent.** If the database already holds this exact save, it does nothing.
- If the database holds a **different** save for that world, it refuses: that is most likely
  newer play on Postgres. `-force` overwrites it.
- Keep the JSON files. They are your rollback (see below).

## 5. Switch the server to Postgres

`HEARTH_STORE` is per process, so every world this `hearthd` hosts switches together:

```bash
HEARTH_STORE=postgres DATABASE_URL=... go run ./cmd/hearthd
```

The boot log shows `store=postgres` and `saves: postgres host:port/db`, and each world logs
`save=postgres:world_snapshots/<worldId>` when it is built.

**Rollback:** unset `HEARTH_STORE`. The server goes back to the JSON files *as they were when
you switched*. There is no Postgres-to-JSON exporter yet, so play since the switch would not
carry back.

### How saving behaves (and why)

The room calls `Save` on its own goroutine, the one that runs the 200 ms tick. A database call
there would freeze the whole world whenever Postgres is slow. So `PgStore.Save` only encodes the
snapshot (the same CPU work the JSON save already does there) and hands it to a per-world writer
goroutine. That goroutine writes the latest snapshot and coalesces any that arrive while a write
is in flight. Each write is one transaction with a 5 s timeout. The other behaviours:

- A failed write is retried with the next save and logged as `save failed` one interval later.
- On shutdown (SIGINT/SIGTERM) the final save is flushed, waiting up to 15 s and retrying. If the
  database is down for that whole window, the final save is lost and the log says so. The
  previous periodic save (≤30 s old) is still in the database.
- If the save cannot be **read** when a world is built (database unreachable), the world refuses
  to start and the next join retries. It never starts empty and overwrites the real save.
- Unchanged profiles are not rewritten. `updated_at` is the last real change.

## 6. Run the DB tests

```bash
cd gameserver
HEARTH_TEST_DATABASE_URL=postgres://hearth:change-me@localhost:5432/hearth_test go test ./persist/... ./hosting/
```

PowerShell: `$env:HEARTH_TEST_DATABASE_URL = "postgres://..."; go test ./persist/... ./hosting/`.

- `persist/persisttest` is the store contract: every field round-trips, `nil, nil` on an empty
  world, a save replaces the whole snapshot, worlds are isolated, and the `Archive` semantics
  hold. It runs against `JSONStore` always and against `PgStore` when the variable is set.
- The migration tests cover a fresh apply, an idempotent re-run, concurrent runners, and
  refusing an edited or unknown migration.
- Each test creates its own `hearth_test_<time>_<random>` schema and drops it afterwards, so a
  shared database is safe.
- Without the variable these tests **skip** with a message, and `go vet ./... && go test ./...`
  stays green. The tests that prove a hung database cannot stall `Save` (and that `Load`,
  `Archive` and `Close` are time-bounded) need no database and always run.

## What is left

Everything below needs the live database.

1. **Verify against real Postgres.** Run the DB tests, `dbmigrate`, an `importsave` of the real
   saves, then a play session with `HEARTH_STORE=postgres` across a restart.
2. **Control plane → `players`.** `control/store.js` is still in-memory. When converting it:
   - Every store function is **synchronous** today, and `control/index.js` calls them inline
     (`getProfile`/`putProfile` in `handleJoin`, `getWorlds` in the `/api/worlds` handlers).
     `handleJoin` already runs inside a promise `.then`, so it can become `async` cheaply. The
     worlds handlers and `resolveWorld`/`getDefaultWorld` need to become async, or the registry
     needs an in-memory cache that is refreshed from the table.
   - Profiles are keyed by **`tok`** in memory. The table is keyed by the derived `user_id`.
     Never store the raw `tok`: it is a bearer credential, and `userId` is already
     `HMAC(secret, tok)`.
   - `dev` is recomputed from the env allowlist on every join. Decide whether `players.dev` is a
     cache of that or becomes the grant itself.
   - Node needs the `pg` driver and must not run migrations.
3. **Control plane → `worlds` / `world_members`.** Room CRUD and join codes (`join_code` is
   already unique). This replaces `control/worlds.json`. `hosting.LoadConfig` reads the same
   file today. After the switch, Go reads `worlds` (Node writes, Go reads, which keeps one writer
   per table). Then add the `world_id` FKs on the Go tables in a new migration.
4. **Multi-instance.** This needs Redis room-owner leases. The `fence` column and the TODO in
   `pgstore.write` mark where the fencing token goes, so a stale instance cannot overwrite a newer
   save. Redis stays deferred.
5. Smaller items: a Postgres-to-JSON export (for rollback and local debugging), archive retention,
   and backups (`pg_dump` or the provider's point-in-time recovery).
