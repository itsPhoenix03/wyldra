package hosting

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"hearth/gameserver/defs"
	"hearth/gameserver/persist"
	"hearth/gameserver/persist/pgstore"
	"hearth/gameserver/room"
)

// resolveError is an error a client is allowed to see: its reason string is the
// `authfail` reason the net layer sends back. Anything else that goes wrong
// while resolving is reported generically, so internal failures never leak.
type resolveError struct{ reason string }

func (e *resolveError) Error() string          { return "hosting: " + e.reason }
func (e *resolveError) AuthFailReason() string { return e.reason }

// Resolution failures. WrongInstance is the allocation boundary being real: the
// ticket is perfectly valid and correctly signed, it simply binds a world that
// some other process hosts, and this one must not quietly serve it anyway.
var (
	ErrWrongInstance = &resolveError{"wrong-instance"}
	ErrUnknownWorld  = &resolveError{"unknown-world"}
	ErrNotRunning    = &resolveError{"world-unavailable"}
)

// Options configure a Manager. Everything here is process-wide; per-world
// settings come from Config.Worlds.
type Options struct {
	Config    Config
	Defs      *defs.Defs
	Logger    *log.Logger
	AllowWarp bool
	Dev       bool // the GROW_DIV crop-growth knob, not a permission
	Persist   bool
	SaveEvery time.Duration
	// DB is the process-wide Postgres pool, required when Config.Store is
	// StorePostgres (and Persist is on); every world's store shares it.
	DB *pgxpool.Pool
}

// Manager owns the rooms this process hosts.
//
// Rooms are built LAZILY, on the first join to each world, unless Config.Eager
// is set. Worldgen is ~2s of CPU and a few hundred MB per world, so a process
// configured with eight worlds would otherwise spend a quarter of a minute at
// boot and hold every one of them resident even if nobody ever joins. The cost
// is paid by the first player into each world instead, inside their handshake
// window; every player after that finds the room already up. HEARTH_EAGER_WORLDS
// flips it for operators who would rather pay at boot and have a warm process.
//
// Two simultaneous first-joins cannot build the same world twice: the entry
// (with its done channel) is published under the lock before the build starts,
// so the second caller finds it and waits on the channel.
type Manager struct {
	opts Options
	log  *log.Logger

	// identities is the process-wide "one live session per identity" registry,
	// shared by every room this manager builds. It is what makes the
	// duplicate-tab takeover work ACROSS worlds: the same userId joining world
	// B while still live in world A evicts the world-A session. It guards no
	// game state — see room/registry.go.
	identities *room.Registry

	// build is the room constructor, swappable in tests so they do not pay for
	// real worldgen.
	build func(WorldSpec) (*room.Room, error)

	mu      sync.Mutex
	rooms   map[string]*entry
	closers map[string]io.Closer // stores to close once their room stops
	ctx     context.Context      // room lifetime; nil until Start
	stopped bool
	wg      sync.WaitGroup
}

// entry is one world's room, plus the once-only build that produces it. done is
// closed when room/err are final; both are written before the close and read
// only after it, so no lock is needed to read them.
type entry struct {
	spec WorldSpec
	done chan struct{}
	room *room.Room
	err  error
}

// NewManager prepares the manager. No world is generated here; see Start.
func NewManager(o Options) *Manager {
	if o.Logger == nil {
		o.Logger = log.Default()
	}
	m := &Manager{opts: o, log: o.Logger, rooms: map[string]*entry{}, closers: map[string]io.Closer{}, identities: room.NewRegistry()}
	m.build = m.buildRoom
	return m
}

// Config returns the hosting config, for logging and tests.
func (m *Manager) Config() Config { return m.opts.Config }

// Start binds the room lifetime to ctx and, when configured eager, builds every
// hosted world before returning. Rooms created later (lazily) run under the same
// ctx, so one cancellation stops and saves all of them.
func (m *Manager) Start(ctx context.Context) error {
	m.mu.Lock()
	m.ctx = ctx
	m.mu.Unlock()

	if !m.opts.Config.Eager {
		return nil
	}
	for _, w := range m.opts.Config.Worlds {
		if _, err := m.Resolve(ctx, m.opts.Config.InstanceID, w.WorldID); err != nil {
			return err
		}
	}
	return nil
}

// Resolve routes an authenticated ticket binding to its room, building the room
// on first use. It returns ErrWrongInstance / ErrUnknownWorld for a binding this
// process does not serve; both carry an authfail reason for the client.
//
// ctx bounds only the wait — worldgen itself is not interruptible — so a caller
// that gives up leaves the build running for whoever joins next.
func (m *Manager) Resolve(ctx context.Context, instanceID, worldID string) (*room.Room, error) {
	if instanceID != m.opts.Config.InstanceID {
		m.log.Printf("[hearth] refused a ticket for instanceId=%q (this process is %q, world %q)",
			instanceID, m.opts.Config.InstanceID, worldID)
		return nil, ErrWrongInstance
	}
	spec, ok := m.opts.Config.World(worldID)
	if !ok {
		m.log.Printf("[hearth] refused a ticket for worldId=%q, which is not hosted here", worldID)
		return nil, ErrUnknownWorld
	}

	m.mu.Lock()
	if m.ctx == nil || m.stopped {
		m.mu.Unlock()
		return nil, ErrNotRunning
	}
	e, exists := m.rooms[spec.WorldID]
	if !exists {
		e = &entry{spec: spec, done: make(chan struct{})}
		m.rooms[spec.WorldID] = e
	}
	runCtx := m.ctx
	m.mu.Unlock()

	if !exists {
		// This caller won the race to create the entry, so it does the build —
		// outside the lock, because it takes seconds. Everyone else waits below.
		m.start(runCtx, e)
	}

	select {
	case <-e.done:
		return e.room, e.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// start builds one world's room and launches its goroutine. On failure the
// entry is dropped from the map so a later join can retry rather than the world
// staying permanently poisoned.
func (m *Manager) start(runCtx context.Context, e *entry) {
	began := time.Now()
	m.log.Printf("[hearth] building world %q (seed=%s)…", e.spec.WorldID, e.spec.Seed)

	r, err := m.build(e.spec)
	if err != nil {
		m.mu.Lock()
		if m.rooms[e.spec.WorldID] == e {
			delete(m.rooms, e.spec.WorldID)
		}
		m.mu.Unlock()
		m.log.Printf("[hearth] world %q failed to build: %v", e.spec.WorldID, err)
		e.err = ErrNotRunning
		close(e.done)
		return
	}

	// Each room gets its own goroutine and owns all of its own state. The
	// WaitGroup lets the process wait for every final save on shutdown.
	m.wg.Add(1)
	go func() {
		defer m.wg.Done()
		r.Run(runCtx)
		// Run has handed its final save to the store; a store that writes in
		// the background (PgStore) lands it here, before Wait can return.
		m.mu.Lock()
		c := m.closers[e.spec.WorldID]
		delete(m.closers, e.spec.WorldID)
		m.mu.Unlock()
		if c != nil {
			if err := c.Close(); err != nil {
				m.log.Printf("[hearth] world %q: %v", e.spec.WorldID, err)
			}
		}
		m.log.Printf("[hearth] world %q stopped", e.spec.WorldID)
	}()

	spawn := r.Spawn()
	m.log.Printf("[hearth] world %q ready in %s (spawn=(%d,%d), save=%s)",
		e.spec.WorldID, time.Since(began).Round(time.Millisecond),
		spawn[0], spawn[1], m.opts.Config.SaveTarget(e.spec.WorldID))

	e.room = r
	close(e.done)
}

// buildRoom is the real constructor: one world, one store, one room.
func (m *Manager) buildRoom(spec WorldSpec) (*room.Room, error) {
	store, err := m.openStore(spec)
	if err != nil {
		return nil, err
	}
	r, err := room.New(room.Config{
		WorldID:    spec.WorldID,
		Seed:       spec.Seed,
		MaxPlayers: spec.MaxPlayers,
		Registry:   m.identities,
		AllowWarp:  m.opts.AllowWarp,
		Dev:        m.opts.Dev,
		Defs:       m.opts.Defs,
		Store:      store,
		SaveEvery:  m.opts.SaveEvery,
		Features:   m.opts.Config.WorldFeatures(spec),
		// Every room logs under its own world id, so a multi-world process's
		// output stays readable.
		Logger: log.New(m.log.Writer(), m.log.Prefix()+"["+spec.WorldID+"] ", m.log.Flags()),
	})
	c, closes := store.(io.Closer)
	if err != nil {
		if closes {
			_ = c.Close()
		}
		return nil, err
	}
	if closes {
		m.mu.Lock()
		m.closers[spec.WorldID] = c
		m.mu.Unlock()
	}
	return r, nil
}

// openStore picks the world's save backend: nothing (HEARTH_NO_PERSIST), the
// JSON file, or Postgres.
func (m *Manager) openStore(spec WorldSpec) (persist.Store, error) {
	if !m.opts.Persist {
		return persist.NopStore{}, nil
	}
	if m.opts.Config.Store != StorePostgres {
		return persist.NewJSONStore(m.opts.Config.SavePath(spec.WorldID)), nil
	}
	if m.opts.DB == nil {
		return nil, errors.New("hosting: HEARTH_STORE=postgres but no database pool was opened")
	}
	pg := pgstore.New(m.opts.DB, spec.WorldID, pgstore.Options{})
	// The save is read HERE, and a failed read fails the build, because
	// room.New treats a Load error as "no save" and starts the world fresh —
	// harmless for an unreadable local file, but with Postgres it would turn a
	// database blip at boot into the world being overwritten by an empty one
	// on the first save. Refusing the build keeps the save intact; the next
	// join retries.
	snap, err := pg.Load()
	if err != nil {
		_ = pg.Close()
		return nil, fmt.Errorf("hosting: world %q: cannot read its save, not starting it empty: %w", spec.WorldID, err)
	}
	return &preloaded{PgStore: pg, snap: snap}, nil
}

// preloaded hands the room the snapshot openStore already read, so the save is
// read once and the room is built from exactly the read that succeeded. Every
// later call — Save, Archive, Close, and any second Load — goes to the PgStore.
type preloaded struct {
	*pgstore.PgStore
	snap *persist.Snapshot
	used bool
}

func (p *preloaded) Load() (*persist.Snapshot, error) {
	if !p.used {
		p.used = true
		return p.snap, nil
	}
	return p.PgStore.Load()
}

// Identities is the process-wide session registry every room shares. For tests.
func (m *Manager) Identities() *room.Registry { return m.identities }

// Running returns the worldIds with a live room, in no particular order. For
// logging and tests.
func (m *Manager) Running() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, 0, len(m.rooms))
	for id := range m.rooms {
		out = append(out, id)
	}
	return out
}

// Wait blocks until every room goroutine has returned, which happens after the
// Start context is cancelled and each room has written its final save. It also
// closes the manager to new resolutions, so a connection arriving during
// shutdown is refused instead of building a world nobody will play.
func (m *Manager) Wait() {
	m.mu.Lock()
	m.stopped = true
	m.mu.Unlock()
	m.wg.Wait()
}
