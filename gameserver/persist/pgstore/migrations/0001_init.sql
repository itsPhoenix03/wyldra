-- 0001_init: the first durable schema.
--
-- Ownership (docs/09 §7): one service writes each table.
--   Node (control plane) writes: players, worlds, world_members.
--   Go (game server) writes:     world_snapshots, player_world_profiles,
--                                world_snapshot_archive.
-- Nothing in Go ever writes a Node table, and vice versa.
--
-- Keys are the ids the code already uses: userId is 'u_' + 24 hex
-- (control/identity.js, derived from the tok — the raw tok is a bearer
-- credential and is deliberately not stored anywhere), worldId is the registry
-- id (1-64 chars of [A-Za-z0-9._-], hosting.normalizeWorld).

-- Accounts. `name` is account-level and Node-owned; Go only echoes the ticket's.
CREATE TABLE players (
    user_id      text        PRIMARY KEY,
    display_name text        NOT NULL,
    dev          boolean     NOT NULL DEFAULT false,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_seen_at timestamptz NOT NULL DEFAULT now()
);

-- The world registry that control/worlds.json holds today, plus the room
-- metadata (owner, join code, visibility) the lobby will need.
CREATE TABLE worlds (
    world_id      text        PRIMARY KEY
                  CHECK (world_id ~ '^[A-Za-z0-9._-]{1,64}$' AND world_id NOT IN ('.', '..')),
    name          text        NOT NULL,
    seed          text        NOT NULL,
    -- nullable: the operator-configured worlds of today have no owner
    owner_user_id text        REFERENCES players (user_id) ON DELETE SET NULL,
    -- UNIQUE allows any number of NULLs, so worlds without a code are fine
    join_code     text        UNIQUE,
    instance_id   text,
    ws_url        text,
    max_players   integer     NOT NULL DEFAULT 4 CHECK (max_players > 0),
    visibility    text        NOT NULL DEFAULT 'private' CHECK (visibility IN ('private', 'public')),
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now(),
    archived_at   timestamptz
);

CREATE TABLE world_members (
    world_id       text        NOT NULL REFERENCES worlds (world_id) ON DELETE CASCADE,
    user_id        text        NOT NULL REFERENCES players (user_id) ON DELETE CASCADE,
    role           text        NOT NULL DEFAULT 'member' CHECK (role IN ('owner', 'member')),
    joined_at      timestamptz NOT NULL DEFAULT now(),
    last_played_at timestamptz,
    PRIMARY KEY (world_id, user_id)
);
-- "which worlds is this player in" — the lobby's list query
CREATE INDEX world_members_user_id_idx ON world_members (user_id);

-- The Go-written tables below carry NO foreign keys to players or worlds, on
-- purpose:
--   * Go must never write a Node table (the ownership rule above), so it cannot
--     insert a placeholder parent row to satisfy an FK.
--   * Node does not write players/worlds yet (control/store.js is still
--     in-memory, the registry is still worlds.json), and even once it does, a
--     ticket is minted before any Node write is guaranteed to have committed.
--     An FK would make the room's save fail — and the player lose progress —
--     whenever the control plane is behind or down. A save must never depend on
--     another service's write.
-- Once Node writes players and worlds, a later migration can add the world_id
-- FKs (after backfilling worlds from the snapshots). Profiles -> players should
-- stay FK-less: a profile is world state keyed by an id Node owns, not a child
-- row Node is responsible for.

-- The world as the room last saved it: persist.Snapshot as JSON with the
-- `profiles` map emptied ({} or null, matching what was saved); the profiles
-- live one row per player in player_world_profiles.
CREATE TABLE world_snapshots (
    world_id text        PRIMARY KEY,
    version  integer     NOT NULL,
    seed     text        NOT NULL,
    snapshot jsonb       NOT NULL,
    saved_at timestamptz NOT NULL DEFAULT now(),
    -- Multi-instance fencing token (Redis room-owner lease, docs/
    -- MULTIPLAYER_ROOM_ARCHITECTURE_GUIDE.md). Unused until a second hearthd
    -- can host the same world; see the TODO in pgstore.write.
    fence    bigint      NOT NULL DEFAULT 0
);

-- One persist.Profile per (world, player). Saved in the same transaction as
-- the world snapshot, so the two can never disagree.
CREATE TABLE player_world_profiles (
    world_id   text        NOT NULL,
    user_id    text        NOT NULL,
    profile    jsonb       NOT NULL,
    -- last time the profile actually changed (unchanged rows are not rewritten)
    updated_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (world_id, user_id)
);
CREATE INDEX player_world_profiles_user_id_idx ON player_world_profiles (user_id);

-- persist.Archiver: the full save (profiles included, i.e. exactly what a JSON
-- save file holds) set aside before a map migration overwrites it. The first
-- copy of a version wins and is never replaced.
CREATE TABLE world_snapshot_archive (
    world_id    text        NOT NULL,
    version     integer     NOT NULL,
    snapshot    jsonb       NOT NULL,
    archived_at timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (world_id, version)
);
