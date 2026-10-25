// Automatic reconnect to the same world after the game socket drops.
//
// One attempt is the whole join handshake again: a FRESH ticket from the control plane
// (tickets live 30s, so the old one is useless), pinned to the world the session was in,
// then a WebSocket to the server that ticket names and `{t:'auth', ticket}`. An attempt
// succeeds when the server answers `init`; from then on every frame is buffered, untouched,
// until the caller has built a new scene to hand the socket to (main.ts swapToSession).
//
// The loop owns exactly one timer (the backoff) and at most one socket. Nothing here runs
// per frame. Retry policy:
//   * control plane unreachable / 5xx, socket refused or closed before init, a transient
//     authfail (`world-unavailable` while a world builds, `expired`)  -> back off, retry;
//   * 4xx from the control plane, any other authfail (`room-full`, `bad-signature`, ...),
//     a `kick`, or the control plane resolving a DIFFERENT world      -> give up now;
//   * the budget (~2 min) runs out                                    -> give up.
// Giving up is the caller's quitToMenu with the message passed to onGiveUp.

import { requestJoin, JoinError } from "./net.ts";

/** A socket that has been admitted to the world (`init` received) but not yet handed to a scene. */
export interface Rejoined {
  ws: WebSocket;
  /** Raw frames received so far, `init` first, in arrival order. */
  buf: string[];
  /** The socket closed while it was waiting to be adopted. */
  closed: boolean;
  worldId: string;
  name: string;
}

export type RejoinPhase = "wait" | "ticket" | "socket";

export interface RejoinHooks {
  /** State changed (not called per frame): `attempt` is 1-based; `waitMs` only for "wait". */
  onProgress(attempt: number, phase: RejoinPhase, waitMs?: number): void;
  onReady(s: Rejoined): void;
  onGiveUp(message: string): void;
  /** Player-facing text for a refusal frame received before init. */
  refusalText(t: "authfail" | "kick", reason: string): string;
}

/** Total time spent trying before giving up. */
export const REJOIN_BUDGET_MS = 120_000;
const BASE_MS = 500;
const CAP_MS = 5_000;
/** A socket that has not opened by now is abandoned (the server is down or unreachable). */
const OPEN_TIMEOUT_MS = 10_000;
/** After `auth`: the server may build a lazy world (bounded at 45s server-side, net/server.go). */
const INIT_TIMEOUT_MS = 50_000;
/** authfail reasons a later attempt can get past; every other one is final. */
const TRANSIENT_AUTHFAIL = new Set(["world-unavailable", "expired"]);

export class Rejoin {
  private timer = 0;
  private ws: WebSocket | null = null;
  private done = false;

  /**
   * `start` / `attempt` let a loss that follows soon after a successful rejoin continue the
   * previous budget and backoff instead of starting over, so a server that admits and then
   * drops every session cannot rebuild the scene forever.
   */
  constructor(
    private readonly name: string,
    readonly worldId: string,
    private readonly hooks: RejoinHooks,
    readonly start = Date.now(),
    public attempt = 0,
  ) {}

  begin() {
    this.schedule();
  }

  /** Stop for good: no callback fires after this, and an attempt's socket is closed. */
  cancel() {
    this.done = true;
    clearTimeout(this.timer);
    this.dropSocket();
  }

  private dropSocket() {
    const ws = this.ws;
    this.ws = null;
    if (!ws) return;
    ws.onopen = ws.onmessage = ws.onclose = ws.onerror = null;
    try {
      if (ws.readyState === WebSocket.OPEN || ws.readyState === WebSocket.CONNECTING) ws.close(1000, "retry");
    } catch {
      /* already closing */
    }
  }

  private fail(message: string) {
    if (this.done) return;
    this.cancel();
    this.hooks.onGiveUp(message);
  }

  /** Exponential backoff (0.5s, 1s, 2s, 4s, 5s, 5s, ...) with ±20% jitter, so a whole room
   *  coming back after a restart does not hit the servers in lockstep. */
  private schedule() {
    if (this.done) return;
    const base = Math.min(CAP_MS, BASE_MS * 2 ** this.attempt);
    const wait = Math.round(base * (0.8 + Math.random() * 0.4));
    if (Date.now() - this.start + wait > REJOIN_BUDGET_MS) {
      this.fail(
        "Lost the connection to the world server and could not get back in for 2 minutes. " +
          "Check that the server is running, then join again.",
      );
      return;
    }
    this.hooks.onProgress(this.attempt + 1, "wait", wait);
    this.timer = window.setTimeout(() => void this.tryOnce(), wait);
  }

  private async tryOnce() {
    if (this.done) return;
    this.attempt++;
    this.hooks.onProgress(this.attempt, "ticket");
    let j;
    try {
      j = await requestJoin(this.name, this.worldId);
    } catch (e) {
      if (this.done) return;
      const st = e instanceof JoinError ? e.status : undefined;
      if (st !== undefined && st >= 400 && st < 500) this.fail((e as Error).message);
      else this.schedule();
      return;
    }
    if (this.done) return;
    // An unknown worldId silently resolves to the default world. Landing there would split
    // this player from everyone they were playing with, so it is a reason to stop instead.
    if (j.worldId !== this.worldId) {
      this.fail("The world you were in is no longer offered by the login server. Pick a world from the menu.");
      return;
    }
    this.hooks.onProgress(this.attempt, "socket");
    let ws: WebSocket;
    try {
      ws = new WebSocket(j.ws);
    } catch {
      this.schedule(); // a malformed URL from the registry: maybe fixed by the next answer
      return;
    }
    this.ws = ws;
    const s: Rejoined = { ws, buf: [], closed: false, worldId: j.worldId, name: j.name };
    let admitted = false;
    const abandon = () => {
      if (admitted || ws !== this.ws) return;
      this.dropSocket();
      this.schedule();
    };
    let guard = window.setTimeout(abandon, OPEN_TIMEOUT_MS);
    ws.onopen = () => {
      clearTimeout(guard);
      guard = window.setTimeout(abandon, INIT_TIMEOUT_MS);
      ws.send(JSON.stringify({ t: "auth", ticket: j.ticket }));
    };
    ws.onmessage = (e) => {
      if (admitted) {
        s.buf.push(e.data);
        return;
      }
      let m: any;
      try {
        m = JSON.parse(e.data);
      } catch {
        return;
      }
      if (m.t === "init") {
        admitted = true;
        clearTimeout(guard);
        s.buf.push(e.data);
        // Hand over: from here the socket belongs to the caller. These handlers keep
        // buffering until the new scene replaces them.
        this.ws = null;
        this.done = true;
        ws.onclose = () => (s.closed = true);
        this.hooks.onReady(s);
      } else if (m.t === "authfail" || m.t === "kick") {
        clearTimeout(guard);
        this.dropSocket();
        if (m.t === "authfail" && TRANSIENT_AUTHFAIL.has(m.reason)) this.schedule();
        else this.fail(this.hooks.refusalText(m.t, m.reason));
      }
    };
    ws.onclose = () => {
      clearTimeout(guard);
      if (ws !== this.ws) return;
      this.ws = null;
      ws.onopen = ws.onmessage = ws.onclose = null;
      this.schedule();
    };
  }
}
