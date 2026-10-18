import Phaser from 'phaser';

/* ────────────────────────────────────────────────────────────────────────────
 * Dev-session overlays (only for a session with the dev claim — F9/F10 rules):
 *
 *  - a performance meter, always on in dev: FPS, average and worst frame time,
 *    and inbound network (messages and KB per second). A long frame is exactly
 *    what lets the server's per-player outbound queue back up, so this is the
 *    first thing to look at when the game feels slow.
 *  - combat ranges, toggled from the F10 tester panel: every player's and
 *    enemy's attack radius, drawn as the iso ellipse it covers on the ground,
 *    and a motion line showing where it is heading over the next second.
 *
 * The radii mirror the authoritative Go server; keep them in step with it:
 *   player swing      2.4   gameserver/room/actions.go  (target search: nearest within 2.4)
 *   creature contact  1.1   gameserver/room/creatures.go (contact damage, < 1.1)
 *   brute water bolt  1.5–7 creatures.go bruteBolt
 *   lancer beam       1.5–9 creatures.go lancerBeam
 *   frost wraith dart 10    creatures.go (drift: darts at the nearest player inside 10)
 * ──────────────────────────────────────────────────────────────────────────── */

const TW = 64, TH = 32;
const PLAYER_REACH = 2.4;
const CONTACT = 1.1;
const RANGED: Record<string, [number, number]> = { brute: [1.5, 7], blight_lancer: [1.5, 9] };
const AGGRO: Record<string, number> = { frost_wraith: 10 };
/** The motion line shows this many seconds of travel at the current speed. */
const LEAD_S = 1;
const RANGES_KEY = 'hearth-dbg-ranges';

export interface DevHost extends Phaser.Scene {
  devEnabled: boolean;
  ready: boolean;
  z: number;
  px: number;
  py: number;
  me: Phaser.GameObjects.Container | undefined;
  others: Map<string, { rig: Phaser.GameObjects.Container; wx: number; wy: number; z: number }>;
  creSpr: Map<string, Phaser.GameObjects.Sprite>;
}

type Track = { x: number; y: number; at: number; vx: number; vy: number };

export class DevOverlay {
  private meter: HTMLDivElement;
  private g: Phaser.GameObjects.Graphics;
  private ranges = false;
  // performance window
  private winStart = 0;
  private frames = 0;
  private worst = 0;
  private lastT = 0;
  private netMsgs = 0;
  private netBytes = 0;
  // per-entity velocity from successive positions (tiles per second)
  private tracks = new Map<string, Track>();

  constructor(private s: Phaser.Scene) {
    try { this.ranges = localStorage.getItem(RANGES_KEY) === '1'; } catch { /* storage blocked: default off */ }
    this.g = s.add.graphics().setDepth(999998);
    let m = document.getElementById('devMeter') as HTMLDivElement | null;
    if (!m) {
      m = document.createElement('div');
      m.id = 'devMeter';
      m.style.cssText =
        'position:fixed;bottom:8px;left:8px;z-index:31;display:none;pointer-events:none;' +
        'background:rgba(10,15,20,.82);border:1px solid #57a;border-radius:6px;padding:4px 8px;' +
        'color:#cfe;font:11px/1.45 monospace;white-space:pre';
      document.body.appendChild(m);
    }
    this.meter = m;
  }

  get rangesOn() { return this.ranges; }

  setRanges(on: boolean) {
    this.ranges = on;
    try { localStorage.setItem(RANGES_KEY, on ? '1' : '0'); } catch { /* not persisted */ }
    if (!on) this.g.clear();
  }

  /** Count one inbound socket frame. */
  onNet(bytes: number) {
    this.netMsgs++;
    this.netBytes += bytes;
  }

  /** Remove the DOM meter (the scene is quitting). */
  destroy() {
    this.meter.remove();
  }

  update(t: number) {
    const h = this.s as DevHost;
    const dev = h.devEnabled;
    if (!dev) {
      if (this.meter.style.display !== 'none') this.meter.style.display = 'none';
      if (this.ranges) this.g.clear();
      return;
    }
    this.meterTick(t);
    if (this.ranges && h.ready) this.drawRanges(h, t);
  }

  private meterTick(t: number) {
    if (this.lastT) this.worst = Math.max(this.worst, t - this.lastT);
    this.lastT = t;
    this.frames++;
    if (!this.winStart) this.winStart = t;
    const span = t - this.winStart;
    if (span < 500) return;
    const fps = (this.frames * 1000) / span;
    const kbs = this.netBytes / 1024 / (span / 1000);
    const mps = this.netMsgs / (span / 1000);
    this.meter.style.display = 'block';
    this.meter.style.color = fps < 30 || this.worst > 100 ? '#ff8a7a' : fps < 50 ? '#ffd27a' : '#cfe';
    this.meter.textContent =
      `${fps.toFixed(0).padStart(3)} fps  ${(span / this.frames).toFixed(1)} ms avg  ${this.worst.toFixed(0)} ms worst\n` +
      `net in ${mps.toFixed(0)} msg/s  ${kbs.toFixed(1)} KB/s`;
    this.winStart = t;
    this.frames = 0;
    this.worst = 0;
    this.netMsgs = 0;
    this.netBytes = 0;
  }

  /** Velocity of entity `id` now at tile (x, y), smoothed over its last moves. */
  private velocity(id: string, x: number, y: number, t: number): Track {
    let tr = this.tracks.get(id);
    if (!tr) {
      tr = { x, y, at: t, vx: 0, vy: 0 };
      this.tracks.set(id, tr);
      return tr;
    }
    const dt = (t - tr.at) / 1000;
    if (x !== tr.x || y !== tr.y) {
      if (dt > 0 && dt < 2) {
        tr.vx = tr.vx * 0.5 + ((x - tr.x) / dt) * 0.5;
        tr.vy = tr.vy * 0.5 + ((y - tr.y) / dt) * 0.5;
      }
      tr.x = x; tr.y = y; tr.at = t;
    } else if (dt > 0.6) {
      tr.vx = tr.vy = 0; // no new position for a while: standing still
    }
    return tr;
  }

  /** Ground circle of radius r tiles around screen point (sx, sy), as its iso ellipse. */
  private ring(sx: number, sy: number, r: number) {
    const g = this.g, n = 28;
    g.beginPath();
    for (let i = 0; i <= n; i++) {
      const a = (i / n) * Math.PI * 2, dx = Math.cos(a) * r, dy = Math.sin(a) * r;
      const px = sx + ((dx - dy) * TW) / 2, py = sy + ((dx + dy) * TH) / 2;
      if (i) g.lineTo(px, py);
      else g.moveTo(px, py);
    }
    g.strokePath();
  }

  /** Motion line: where (vx, vy) tiles/s carries the entity in LEAD_S, with an arrowhead. */
  private motion(sx: number, sy: number, vx: number, vy: number, color: number) {
    const dx = vx * LEAD_S, dy = vy * LEAD_S;
    if (Math.hypot(dx, dy) < 0.15) return;
    const ex = sx + ((dx - dy) * TW) / 2, ey = sy + ((dx + dy) * TH) / 2;
    const g = this.g;
    g.lineStyle(2, color, 0.95);
    g.lineBetween(sx, sy, ex, ey);
    const ang = Math.atan2(ey - sy, ex - sx);
    g.fillStyle(color, 0.95);
    g.fillTriangle(
      ex, ey,
      ex - Math.cos(ang - 0.45) * 8, ey - Math.sin(ang - 0.45) * 8,
      ex - Math.cos(ang + 0.45) * 8, ey - Math.sin(ang + 0.45) * 8,
    );
  }

  private drawRanges(h: DevHost, t: number) {
    const g = this.g;
    g.clear();
    const live = new Set<string>();

    // the local player: swing reach, motion, and the line to whatever a swing would hit now
    if (h.me) {
      live.add('me');
      const tr = this.velocity('me', h.px, h.py, t);
      g.lineStyle(1.5, 0x5fd4ff, 0.9);
      this.ring(h.me.x, h.me.y, PLAYER_REACH);
      this.motion(h.me.x, h.me.y, tr.vx, tr.vy, 0x5fd4ff);
      let best: Phaser.GameObjects.Sprite | null = null, bd = PLAYER_REACH;
      for (const c of h.creSpr.values()) {
        const d = Math.hypot((c.getData('cx') ?? 1e9) - h.px, (c.getData('cy') ?? 1e9) - h.py);
        if (d < bd) { bd = d; best = c; }
      }
      if (best) {
        g.lineStyle(1, 0xffffff, 0.8);
        g.lineBetween(h.me.x, h.me.y - 20, best.x, best.y - 14);
        g.strokeCircle(best.x, best.y - 14, 5);
      }
    }

    // other players on this layer
    for (const [id, o] of h.others) {
      if (o.z !== h.z || !o.rig.visible) continue;
      live.add(id);
      const tr = this.velocity(id, o.wx, o.wy, t);
      g.lineStyle(1.2, 0x9be37a, 0.8);
      this.ring(o.rig.x, o.rig.y, PLAYER_REACH);
      this.motion(o.rig.x, o.rig.y, tr.vx, tr.vy, 0x9be37a);
    }

    // enemies: contact radius, ranged band or aggro radius, motion
    for (const [id, c] of h.creSpr) {
      if (!c.visible || !c.active) continue;
      live.add(id);
      const type: string = c.getData('ctype') ?? '';
      const tr = this.velocity(id, c.getData('cx'), c.getData('cy'), t);
      g.lineStyle(1.5, 0xff5a4a, 0.9);
      this.ring(c.x, c.y, CONTACT);
      const band = RANGED[type];
      if (band) {
        g.lineStyle(1, 0xffa040, 0.75);
        this.ring(c.x, c.y, band[0]);
        this.ring(c.x, c.y, band[1]);
      }
      if (AGGRO[type]) {
        g.lineStyle(1, 0xc08cff, 0.55);
        this.ring(c.x, c.y, AGGRO[type]);
      }
      this.motion(c.x, c.y, tr.vx, tr.vy, 0xff5a4a);
    }

    for (const id of this.tracks.keys()) if (!live.has(id)) this.tracks.delete(id);
  }
}
