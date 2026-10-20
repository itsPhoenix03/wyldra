package room

import (
	"sort"
	"sync"

	"hearth/gameserver/proto"
	"hearth/gameserver/world"
)

// The tileMountains feature (features.go): the room's side of the Go-only
// mountain pass in gameserver/world/mountains.go.
//
// The stamp is a pure function of the generated world and the seed, so it is
// built once per world and shared, read-only, by every room on that world —
// like the world itself. What a room stamps is its own copy of the four layers
// the mountains change (elevation, tileVis, nodes, decor); the world handed in
// through Config.World may be shared between rooms and tests and is never
// written. Tiles, veins, water temperature and icebergs stay shared: the
// mountains keep every tile's type.
//
// Stamping happens in New before the save is applied and before the camps are
// placed, and again whenever a tester flips the flag. A runtime flip changes
// nothing but terrain: players, creatures, animals and folk stay where they
// stand — at the new height — and the chunks the stamp touches are dropped from
// the chunk cache and streamed again to everyone connected, so their clients
// rebuild the ground under them.

// mountainState is the room's stamp bookkeeping.
type mountainState struct {
	stamp *world.MountainStamp
	// own: r.world is the room's private copy of the stamped layers.
	own bool
	// applied: the stamp is on r.world right now (tileMountains is on).
	applied bool
	// chunks are the chunk keys the stamp touches, ascending.
	chunks []int
}

var (
	stampMu    sync.Mutex
	stampCache = map[stampKey]*world.MountainStamp{}
)

type stampKey struct {
	w    *world.World
	seed string
}

// mountainStamp builds (or fetches) the stamp for this room's base world.
func (r *Room) mountainStamp() *world.MountainStamp {
	if r.mtn.stamp != nil {
		return r.mtn.stamp
	}
	k := stampKey{r.baseWorld, r.cfg.Seed}
	stampMu.Lock()
	s := stampCache[k]
	if s == nil {
		s = world.BuildMountains(r.baseWorld, r.cfg.Seed)
		stampCache[k] = s
	}
	stampMu.Unlock()
	r.mtn.stamp = s
	seen := map[int]bool{}
	for _, c := range s.Changes {
		seen[chunkKey((c.I%world.SIZE)/proto.ChunkSize, (c.I/world.SIZE)/proto.ChunkSize)] = true
	}
	for k := range seen {
		r.mtn.chunks = append(r.mtn.chunks, k)
	}
	sort.Ints(r.mtn.chunks)
	return s
}

// ownWorld gives the room its private copy of the layers the stamp writes,
// once. Everything else stays shared with the base world.
func (r *Room) ownWorld() {
	if r.mtn.own {
		return
	}
	b := r.world
	w := *b
	w.Elev = append([]uint8(nil), b.Elev...)
	w.TileVis = append([]uint8(nil), b.TileVis...)
	w.Nodes = make(map[int]uint8, len(b.Nodes)+64)
	for k, v := range b.Nodes {
		w.Nodes[k] = v
	}
	w.Decor = make(map[int]string, len(b.Decor))
	for k, v := range b.Decor {
		w.Decor[k] = v
	}
	r.world = &w
	r.mtn.own = true
}

// applyTerrainFeatures brings the ground in line with the terrain flags:
// stamps the mountains when tileMountains is on and they are not, lifts them
// off when it is off and they are on. live means players may be connected, so
// the touched chunks are re-streamed.
func (r *Room) applyTerrainFeatures(live bool) {
	want := r.feature("tileMountains")
	if want == r.mtn.applied {
		return
	}
	s := r.mountainStamp()
	r.ownWorld()
	if want {
		s.Apply(r.world)
	} else {
		s.Revert(r.world)
	}
	r.mtn.applied = want
	if live {
		r.restreamChunks(r.mtn.chunks)
	}
}

// restreamChunks drops the given chunks from the cache and sends them again to
// every player whose push window holds them; anyone it does not reach now has
// them marked unsent, so pushChunks sends the new ground when they come near.
func (r *Room) restreamChunks(keys []int) {
	for _, k := range keys {
		delete(r.chunkCache, k)
	}
	for _, p := range r.playerOrder {
		for _, k := range keys {
			delete(p.sentChunks, k)
		}
		if !p.chunkInit {
			continue // the join push has not happened yet: it sends everything
		}
		for _, k := range keys {
			cx, cy := k%ChunkGrid, k/ChunkGrid
			if abs(cx-p.chunkCX) > proto.ChunkRadius || abs(cy-p.chunkCY) > proto.ChunkRadius {
				continue
			}
			p.sentChunks[k] = true
			r.send(p, r.chunkMessage(cx, cy))
		}
	}
}

// landmarkBlocks is world.LandmarkBlock as the room sees it: with the tile
// mountains up there is no sprite on the mountains' 3x3 footprints to walk
// into — the massif's own cliffs are the obstacle — so only the temple pieces
// still block.
func (r *Room) landmarkBlocks(i int) bool {
	return world.LandmarkBlock[i] && !(r.mtn.applied && world.MountainBlock[i])
}

// mtnClimbBlocked is the climbing rule for creatures and animals on the tile
// mountains: a step up of more than one level onto or off a mountain tile is
// refused. It is gated to mountain tiles so creature and animal movement
// everywhere else — where the AI has always ignored elevation — is untouched.
func (r *Room) mtnClimbBlocked(from, to int) bool {
	if !r.mtn.applied || from == to || !inBounds(from) || !inBounds(to) {
		return false
	}
	m := r.mtn.stamp.Mark
	if !m[from] && !m[to] {
		return false
	}
	return int(r.world.Elev[to])-int(r.world.Elev[from]) > 1
}

// onMountainFace reports a steep mountain tile (a cliff edge), where nothing
// spawns.
func (r *Room) onMountainFace(x, y float64) bool {
	if !r.mtn.applied || x < 0 || y < 0 || x >= world.SIZE || y >= world.SIZE {
		return false
	}
	return r.mtn.stamp.Face[ti(x, y)]
}
