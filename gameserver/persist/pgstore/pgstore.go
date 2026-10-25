// Package pgstore is the Postgres implementation of the persist seam: one
// PgStore per world, writing the same persist.Snapshot a JSONStore would, split
// into a world row (world_snapshots) and one row per player
// (player_world_profiles). The schema lives in migrations/ (see migrate.go).
//
// # Why Save does not touch the database
//
// The room calls Store.Save on its own Run goroutine — the same goroutine that
// runs the 200ms sim tick (room.Run's saveT case, and the final save on
// shutdown). JSONStore gets away with that because a local file write is
// sub-millisecond. A database call is a network round trip at best and, when
// Postgres is slow, failing over or unreachable, a stall of up to the call's
// timeout: every player in the world would freeze for that long, every 30
// seconds. Invariant 1 (.agents/PROJECT_STATE.md §3) says nothing may block the
// room goroutine, so a PgStore never does I/O in Save:
//
//   - Save encodes the snapshot to JSON on the caller's goroutine — the same
//     CPU work JSONStore already does there — so nothing reachable from the
//     room (Snapshot.Profiles IS the room's live profile map) escapes to
//     another goroutine.
//   - It then parks the encoded bytes in a one-slot mailbox and returns. A
//     single writer goroutine per store takes the latest bytes and runs the
//     transaction. Saves are whole-world snapshots, so if one arrives while the
//     previous write is still in flight it simply replaces the queued one:
//     writes coalesce, they never pile up, and memory stays bounded at one
//     pending snapshot however long the database is gone.
//   - A failed write is kept for retry (unless a newer snapshot replaced it)
//     and reported by the next Save's return value, so the room's existing
//     "save failed" log line still fires — one save interval late.
//   - Close flushes: it waits (bounded by FlushTimeout) for the last snapshot
//     to commit, retrying a failed write, so the final save on shutdown still
//     lands. hosting.Manager calls it after the room's Run returns.
//
// Load and Archive are synchronous: the room calls them only while it is being
// built (room.New -> loadSave -> migrateSave), before any tick runs. Both flush
// first, so they always see this store's own latest save.
package pgstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"hearth/gameserver/persist"
)

const (
	opTimeoutDefault    = 5 * time.Second
	flushTimeoutDefault = 15 * time.Second
	retryPause          = 250 * time.Millisecond
)

// Options tune a PgStore. The zero value is the production default.
type Options struct {
	// OpTimeout bounds every single database call (one transaction, one
	// read). Default 5s.
	OpTimeout time.Duration
	// FlushTimeout bounds how long Close waits for the final save, retries
	// included. Default 15s — inside a typical container stop grace period.
	FlushTimeout time.Duration
}

// PgStore is persist.Store and persist.Archiver for one world, backed by a
// shared connection pool. Save never blocks on the database (package doc).
type PgStore struct {
	pool    *pgxpool.Pool
	worldID string
	opt     Options

	wake chan struct{} // cap 1: "there may be something to write"
	quit chan struct{}
	done chan struct{} // closed when the writer goroutine exits

	mu        sync.Mutex
	next      *encoded      // latest snapshot not yet committed; nil when none
	queued    uint64        // seq of the newest Save
	committed uint64        // seq of the newest committed snapshot
	lastErr   error         // most recent failed write, cleared by a success
	reported  bool          // lastErr has already been returned by a Save
	attempt   chan struct{} // closed (and replaced) after every write attempt
	closed    bool
	closeOnce sync.Once
	closeErr  error
}

// encoded is one Save, already serialised, so it holds no reference to room
// state.
type encoded struct {
	seq      uint64
	version  int
	seed     string
	world    string   // the snapshot JSON, profiles emptied
	users    []string // parallel to profiles
	profiles []string // one persist.Profile JSON each
}

// New returns the store for worldID and starts its writer goroutine. Call
// Close when the room has stopped.
func New(pool *pgxpool.Pool, worldID string, opt Options) *PgStore {
	if opt.OpTimeout <= 0 {
		opt.OpTimeout = opTimeoutDefault
	}
	if opt.FlushTimeout <= 0 {
		opt.FlushTimeout = flushTimeoutDefault
	}
	s := &PgStore{
		pool: pool, worldID: worldID, opt: opt,
		wake: make(chan struct{}, 1), quit: make(chan struct{}), done: make(chan struct{}),
		attempt: make(chan struct{}),
	}
	go s.run()
	return s
}

// WorldID is the world this store saves.
func (s *PgStore) WorldID() string { return s.worldID }

// Location names where this world's save lives, for logs.
func (s *PgStore) Location() string { return "postgres:world_snapshots/" + s.worldID }

// Save encodes snap and queues it for the writer goroutine; it does no I/O. The
// error is either an encoding failure or the previous background write's
// failure (reported once).
func (s *PgStore) Save(snap *persist.Snapshot) error {
	e, err := encode(snap)
	if err != nil {
		return err
	}
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return errors.New("pgstore: Save after Close")
	}
	s.queued++
	e.seq = s.queued
	s.next = e
	var prev error
	if s.lastErr != nil && !s.reported {
		prev, s.reported = s.lastErr, true
	}
	s.mu.Unlock()
	s.poke()
	return prev
}

func encode(snap *persist.Snapshot) (*encoded, error) {
	if snap == nil {
		return nil, errors.New("pgstore: nil snapshot")
	}
	// The world row keeps the profiles field's shape — {} or null — so Load can
	// give back exactly what was saved, as a JSON round trip would.
	world := *snap
	if world.Profiles != nil {
		world.Profiles = map[string]*persist.Profile{}
	}
	wb, err := json.Marshal(&world)
	if err != nil {
		return nil, fmt.Errorf("pgstore: encode snapshot: %w", err)
	}
	e := &encoded{version: snap.Version, seed: snap.Seed, world: string(wb)}
	for uid, p := range snap.Profiles {
		pb, err := json.Marshal(p)
		if err != nil {
			return nil, fmt.Errorf("pgstore: encode profile %s: %w", uid, err)
		}
		e.users = append(e.users, uid)
		e.profiles = append(e.profiles, string(pb))
	}
	return e, nil
}

func (s *PgStore) poke() {
	select {
	case s.wake <- struct{}{}:
	default: // already signalled; the writer will take the latest
	}
}

// run is the writer goroutine: one write at a time, always the latest.
func (s *PgStore) run() {
	defer close(s.done)
	for {
		select {
		case <-s.wake:
		case <-s.quit:
			return
		}
		s.mu.Lock()
		e := s.next
		s.next = nil
		s.mu.Unlock()
		if e == nil {
			continue
		}

		ctx, cancel := context.WithTimeout(context.Background(), s.opt.OpTimeout)
		err := s.write(ctx, e)
		cancel()

		s.mu.Lock()
		if err != nil {
			s.lastErr = fmt.Errorf("pgstore: save world %s: %w", s.worldID, err)
			s.reported = false
			if s.next == nil {
				s.next = e // retry it, unless a newer snapshot already replaced it
			}
		} else {
			if e.seq > s.committed {
				s.committed = e.seq
			}
			s.lastErr = nil
		}
		close(s.attempt)
		s.attempt = make(chan struct{})
		s.mu.Unlock()
	}
}

const (
	upsertWorld = `INSERT INTO world_snapshots (world_id, version, seed, snapshot, saved_at)
VALUES ($1, $2, $3, $4::text::jsonb, now())
ON CONFLICT (world_id) DO UPDATE
SET version = EXCLUDED.version, seed = EXCLUDED.seed, snapshot = EXCLUDED.snapshot, saved_at = EXCLUDED.saved_at`

	// Unchanged profiles are skipped (IS DISTINCT FROM): most players in a
	// save have not moved since the last one, and rewriting them is pure WAL.
	upsertProfiles = `INSERT INTO player_world_profiles (world_id, user_id, profile, updated_at)
SELECT $1, u, p::jsonb, now() FROM unnest($2::text[], $3::text[]) AS t(u, p)
ON CONFLICT (world_id, user_id) DO UPDATE
SET profile = EXCLUDED.profile, updated_at = EXCLUDED.updated_at
WHERE player_world_profiles.profile IS DISTINCT FROM EXCLUDED.profile`

	// A snapshot is the whole truth, as a JSON save file is: a profile it no
	// longer carries is gone. (The room never drops one today; this keeps Load
	// exact if it ever does.)
	deleteStaleProfiles = `DELETE FROM player_world_profiles WHERE world_id = $1 AND user_id <> ALL($2::text[])`
)

// write commits one snapshot: the world row and every profile, atomically.
//
// TODO(fence): once more than one hearthd can host a world, the room-owner
// lease (Redis, docs/MULTIPLAYER_ROOM_ARCHITECTURE_GUIDE.md) hands each owner a
// fencing token. Write it to world_snapshots.fence and add
// `WHERE world_snapshots.fence <= $fence` to the upsert, treating zero rows as
// "a newer owner exists — stop simulating", so a stale instance can never
// overwrite a newer one's save. Single-instance today, so fence stays 0.
func (s *PgStore) write(ctx context.Context, e *encoded) error {
	users := e.users
	if users == nil {
		users = []string{} // a NULL array would make <> ALL match nothing
	}
	profiles := e.profiles
	if profiles == nil {
		profiles = []string{}
	}
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, upsertWorld, s.worldID, e.version, e.seed, e.world); err != nil {
			return err
		}
		if len(users) > 0 {
			if _, err := tx.Exec(ctx, upsertProfiles, s.worldID, users, profiles); err != nil {
				return err
			}
		}
		_, err := tx.Exec(ctx, deleteStaleProfiles, s.worldID, users)
		return err
	})
}

// Flush waits until every Save made before the call has committed, retrying a
// failed write until ctx ends. It returns nil once committed, else the last
// write error (or ctx's error if no attempt finished).
func (s *PgStore) Flush(ctx context.Context) error {
	s.mu.Lock()
	target := s.queued
	s.mu.Unlock()
	for {
		s.mu.Lock()
		if s.committed >= target {
			s.mu.Unlock()
			return nil
		}
		wait := s.attempt
		lastErr := s.lastErr
		s.mu.Unlock()

		s.poke()
		select {
		case <-wait:
		case <-s.done:
			return errors.New("pgstore: writer stopped before the save committed")
		case <-ctx.Done():
			if lastErr != nil {
				return lastErr
			}
			return ctx.Err()
		}

		s.mu.Lock()
		failed := s.committed < target && s.lastErr != nil
		s.mu.Unlock()
		if failed {
			// Do not spin on a dead database; the next poke retries.
			select {
			case <-time.After(retryPause):
			case <-ctx.Done():
				s.mu.Lock()
				err := s.lastErr
				s.mu.Unlock()
				return err
			}
		}
	}
}

// Close flushes the last save (bounded by FlushTimeout) and stops the writer.
// Safe to call more than once; later calls return the first call's result.
func (s *PgStore) Close() error {
	s.closeOnce.Do(func() {
		ctx, cancel := context.WithTimeout(context.Background(), s.opt.FlushTimeout)
		s.closeErr = s.Flush(ctx)
		cancel()
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		close(s.quit)
		<-s.done
		if s.closeErr != nil {
			s.closeErr = fmt.Errorf("pgstore: final save of world %s did not land: %w", s.worldID, s.closeErr)
		}
	})
	return s.closeErr
}

// opCtx is the bound on one synchronous call.
func (s *PgStore) opCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), s.opt.OpTimeout)
}

// Load reassembles the snapshot JSONStore would return, or (nil, nil) when the
// world has never been saved. It flushes first, so it sees this store's own
// saves; a flush failure is not fatal here — Load then returns what is durable.
func (s *PgStore) Load() (*persist.Snapshot, error) {
	fctx, fcancel := s.opCtx()
	_ = s.Flush(fctx)
	fcancel()
	ctx, cancel := s.opCtx()
	defer cancel()

	var snap *persist.Snapshot
	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly},
		func(tx pgx.Tx) error {
			snap = nil
			var world []byte
			err := tx.QueryRow(ctx, `SELECT snapshot::text FROM world_snapshots WHERE world_id = $1`, s.worldID).Scan(&world)
			if errors.Is(err, pgx.ErrNoRows) {
				return nil
			}
			if err != nil {
				return err
			}
			var out persist.Snapshot
			if err := json.Unmarshal(world, &out); err != nil {
				return fmt.Errorf("world_snapshots row is not a snapshot: %w", err)
			}
			rows, err := tx.Query(ctx, `SELECT user_id, profile::text FROM player_world_profiles WHERE world_id = $1`, s.worldID)
			if err != nil {
				return err
			}
			defer rows.Close()
			for rows.Next() {
				var uid string
				var pb []byte
				if err := rows.Scan(&uid, &pb); err != nil {
					return err
				}
				var p *persist.Profile
				if err := json.Unmarshal(pb, &p); err != nil {
					return fmt.Errorf("profile %s is not a profile: %w", uid, err)
				}
				if out.Profiles == nil {
					out.Profiles = map[string]*persist.Profile{}
				}
				out.Profiles[uid] = p
			}
			if err := rows.Err(); err != nil {
				return err
			}
			snap = &out
			return nil
		})
	if err != nil {
		return nil, fmt.Errorf("pgstore: load world %s: %w", s.worldID, err)
	}
	return snap, nil
}

// archiveLocation is what Archive reports, for the room's migration log line.
func (s *PgStore) archiveLocation(version int) string {
	return fmt.Sprintf("postgres:world_snapshot_archive/%s/v%d", s.worldID, version)
}

// Archive copies the current save — world plus profiles, i.e. the document a
// JSON save file holds — to world_snapshot_archive under version. An existing
// archive for that version is left alone, and nothing saved means nothing
// written (persist.Archiver).
func (s *PgStore) Archive(version int) (string, bool, error) {
	fctx, fcancel := s.opCtx()
	err := s.Flush(fctx)
	fcancel()
	if err != nil {
		// Archiving a stale row would be worse than failing: the room logs a
		// failed backup and carries on.
		return "", false, fmt.Errorf("pgstore: archive world %s: pending save did not land: %w", s.worldID, err)
	}
	ctx, cancel := s.opCtx()
	defer cancel()
	// One statement, so the copy is a consistent read; ON CONFLICT keeps the
	// first copy if two runs race.
	tag, err := s.pool.Exec(ctx, `INSERT INTO world_snapshot_archive (world_id, version, snapshot, archived_at)
SELECT w.world_id, $2,
       CASE WHEN p.agg IS NULL THEN w.snapshot ELSE w.snapshot || jsonb_build_object('profiles', p.agg) END,
       now()
FROM world_snapshots w
LEFT JOIN LATERAL (
    SELECT jsonb_object_agg(user_id, profile) AS agg FROM player_world_profiles WHERE world_id = w.world_id
) p ON true
WHERE w.world_id = $1
ON CONFLICT (world_id, version) DO NOTHING`, s.worldID, version)
	if err != nil {
		return "", false, fmt.Errorf("pgstore: archive world %s: %w", s.worldID, err)
	}
	if tag.RowsAffected() == 1 {
		return s.archiveLocation(version), true, nil
	}
	var exists bool
	if err := s.pool.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM world_snapshot_archive WHERE world_id = $1 AND version = $2)`,
		s.worldID, version).Scan(&exists); err != nil {
		return "", false, fmt.Errorf("pgstore: archive world %s: %w", s.worldID, err)
	}
	if exists {
		return s.archiveLocation(version), false, nil
	}
	return "", false, nil
}

// ReadArchive returns an archived save, or (nil, nil) when there is none. It is
// the read side of Archive, for restore tooling and tests.
func (s *PgStore) ReadArchive(version int) (*persist.Snapshot, error) {
	ctx, cancel := s.opCtx()
	defer cancel()
	var b []byte
	err := s.pool.QueryRow(ctx, `SELECT snapshot::text FROM world_snapshot_archive WHERE world_id = $1 AND version = $2`,
		s.worldID, version).Scan(&b)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("pgstore: read archive %s v%d: %w", s.worldID, version, err)
	}
	var snap persist.Snapshot
	if err := json.Unmarshal(b, &snap); err != nil {
		return nil, fmt.Errorf("pgstore: archive %s v%d is not a snapshot: %w", s.worldID, version, err)
	}
	return &snap, nil
}

// Compile-time proof of the seam.
var (
	_ persist.Store    = (*PgStore)(nil)
	_ persist.Archiver = (*PgStore)(nil)
)
