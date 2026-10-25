// Network configuration + the control-plane join handshake.
//
// Slice 1 splits the old single Node server in two:
//   * the Node CONTROL PLANE (:8090) authenticates the player and issues a short-lived
//     signed ticket plus the URL of the game server that owns the world;
//   * the Go GAME SERVER (:8082) is authoritative for the world itself. Its first frame
//     must be `{t:'auth', ticket}` — see docs/10_GO_WIRE_PROTOCOL.md §1.
//
// Every environment-dependent URL lives here. Nothing else in src/ may hardcode a port.

/** Base URL of the Node control plane. Override with VITE_CONTROL_URL at build/dev time. */
export const CONTROL_URL: string =
  (import.meta as any).env?.VITE_CONTROL_URL ||
  `${location.protocol}//${location.hostname}:8090`;

/** Legacy single-server (:8081) fallback. See LEGACY below. */
export const LEGACY_WS_PORT: string =
  (import.meta as any).env?.VITE_LEGACY_PORT || "8081";

/**
 * Legacy mode: talk to the old all-in-one Node server on :8081, which sends no chunks and
 * expects the client to generate terrain itself. Off by default; enable with `?legacy=1`
 * in the URL or VITE_LEGACY=1. Kept so the pre-migration server stays playable while the
 * Go runtime is still gaining features.
 */
export const LEGACY: boolean =
  new URLSearchParams(location.search).get("legacy") === "1" ||
  (import.meta as any).env?.VITE_LEGACY === "1";

export interface JoinTicket {
  ticket: string;
  ws: string;
  worldId: string;
  name: string;
}

/** The persistent identity token. Never cleared — it is the save key. */
export function identityToken(): string {
  let tok = localStorage.getItem("hearth-tok");
  if (!tok) {
    tok = Math.random().toString(36).slice(2) + Date.now().toString(36);
    localStorage.setItem("hearth-tok", tok);
  }
  return tok;
}

// A join requested ahead of time (boot.ts, the moment Play is pressed), so the ticket
// round trip overlaps loading the atlases and the game chunk instead of following them.
let early: { name: string; worldId: string | null; at: number; p: Promise<JoinTicket> } | null = null;
/** Tickets live 30s (control/PROTOCOL.md); one older than this is re-requested rather than risked. */
const EARLY_MAX_AGE_MS = 20_000;

export function prefetchJoin(name: string, worldId?: string) {
  early = { name, worldId: worldId ?? null, at: Date.now(), p: requestJoin(name, worldId) };
  early.p.catch(() => {}); // surfaced by takeJoin's caller, not as an unhandled rejection
}

/**
 * The prefetched join if it is for `name` (and the same `worldId`) and still fresh,
 * otherwise a new request. Single use.
 */
export function takeJoin(name: string, worldId?: string): Promise<JoinTicket> {
  const e = early;
  early = null;
  if (e && e.name === name && e.worldId === (worldId ?? null) && Date.now() - e.at < EARLY_MAX_AGE_MS)
    return e.p;
  return requestJoin(name, worldId);
}

/**
 * A failed join. `status` is the control plane's HTTP status when it answered at all; it is
 * absent when it could not be reached (or answered nonsense), which a reconnect treats as
 * worth retrying, whereas a 4xx is a refusal and is final.
 */
export class JoinError extends Error {
  constructor(message: string, readonly status?: number) {
    super(message);
  }
}

/**
 * POST /api/join on the control plane. Resolves with the ticket + the game-server WS URL.
 * Rejects with a player-readable JoinError on any transport or protocol failure.
 *
 * `worldId` asks for a particular world (a reconnect pins the one the session was in). It
 * is a request, not an assertion: the control plane answers with the world it resolved,
 * which is the default one when the id is unknown (control/PROTOCOL.md §1).
 */
export async function requestJoin(name: string, worldId?: string): Promise<JoinTicket> {
  let res: Response;
  try {
    res = await fetch(`${CONTROL_URL}/api/join`, {
      method: "POST",
      headers: { "content-type": "application/json" },
      body: JSON.stringify(worldId ? { name, tok: identityToken(), worldId } : { name, tok: identityToken() }),
    });
  } catch {
    throw new JoinError(
      `Could not reach the login server at ${CONTROL_URL}. Is it running? (npm run control)`,
    );
  }
  let body: any = null;
  try {
    body = await res.json();
  } catch {
    /* fall through to the status check */
  }
  if (!res.ok)
    throw new JoinError(
      `Login refused (${res.status}): ${body?.error || "unknown error"}`,
      res.status,
    );
  if (!body?.ticket || !body?.ws)
    throw new JoinError("Login server returned an incomplete ticket.");
  return body as JoinTicket;
}
