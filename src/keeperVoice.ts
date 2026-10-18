import Phaser from 'phaser';

/* ────────────────────────────────────────────────────────────────────────────
 * The keeper's own voice: a speech bubble over the player's keeper.
 *
 *  - it explains what the world is telling them (a ledge lip, a fall), the
 *    first time at length and briefly after that;
 *  - it answers the folk: a greeting, a telling-off, folk running from them, a
 *    remark on the weather (src/folk.ts calls reply()).
 *
 * Client-side and personal: other players do not see it, and nothing about it
 * goes over the wire. Every topic has a cooldown so the keeper never chatters.
 * ──────────────────────────────────────────────────────────────────────────── */

export interface VoiceHost extends Phaser.Scene {
  me: Phaser.GameObjects.Container | undefined;
  showNames: boolean;
  z: number;
}

const pick = <T>(xs: readonly T[]) => xs[(Math.random() * xs.length) | 0];

const LEDGE_FIRST =
  "See that worn, crumbling edge? The ground drops away there — go over it and I'll take a hard fall.";
const LEDGE = ["Careful — that edge drops away.", "Long way down from here.", "Not over that lip. I'd feel it.", "Mind the crumbling edge."];
const FELL = ["Oof — should've watched that edge.", "That one hurt. The worn edges mean it.", "Ow. Note to self: crumbling lip, long drop."];

/** What the keeper says back, by what the folk just did. */
const REPLY: Record<string, string[]> = {
  talk: ["Good to see you too.", "I'll keep that in mind.", "Thanks — stay safe out here.", "Fair enough.", "I'll do what I can."],
  scold: ["Sorry — I needed the wood.", "I'll go easier on your land.", "Point taken.", "You're right. I'll take less.", "I didn't mean harm by it."],
  shun: ["Wait — I mean you no harm!", "I'm not here to hurt anyone.", "I'll make it right, I promise."],
  weather: ["Better find shelter.", "Here it comes.", "I'd best not linger in this.", "Good to know — thanks."],
  clear: ["About time.", "Back to work, then.", "Finally."],
};
/** A few folk get answered in kind. */
const REPLY_TO: Record<string, Partial<Record<string, string[]>>> = {
  woods_child: { talk: ["Stay close to the huts, little one.", "Ha — maybe I do!", "Keep your eyes open out there."], scold: ["I know. I'm sorry."] },
  elder_woods: { talk: ["I'll listen, elder.", "Your village has done well."], scold: ["I hear you, elder. I'll leave the rest standing."] },
  elder_yvenne: { talk: ["I'll remember that, Yvenne."], scold: ["Forgive me, Yvenne. I'll tread lighter."] },
  shaman: { talk: ["I'll listen for it."], scold: ["I'll walk softer on your sand."] },
  elder_spire: { talk: ["I don't plan on being the fourth."], scold: ["I'll find my own wood."] },
};

export class KeeperVoice {
  private bubble: Phaser.GameObjects.Text | null = null;
  private saidAt = new Map<string, number>();
  private ledgeExplained = false;

  constructor(private s: VoiceHost) {}

  /**
   * Say `text` over the keeper for `ms`. With a `topic`, nothing is said if that
   * topic was spoken within `cooldownMs`. True if it was said.
   */
  say(text: string, ms = 3000, topic?: string, cooldownMs = 0): boolean {
    const me = this.s.me;
    if (!me || !me.visible) return false;
    const now = this.s.time.now;
    if (topic) {
      if (now - (this.saidAt.get(topic) ?? -1e12) < cooldownMs) return false;
      this.saidAt.set(topic, now);
    }
    this.bubble?.destroy();
    const b = this.s.add
      .text(me.x, this.anchorY(), text, {
        fontFamily: 'Georgia, serif',
        fontSize: '11px',
        color: '#1d2a36',
        backgroundColor: '#dde9f2ee', // cooler than the folk's parchment: this one is you
        padding: { x: 6, y: 4 },
        align: 'center',
        wordWrap: { width: 190 },
      })
      .setOrigin(0.5, 1)
      .setDepth(1e6);
    this.bubble = b;
    this.s.time.delayedCall(ms, () => {
      if (this.bubble !== b) return;
      this.bubble = null;
      this.s.tweens.add({ targets: b, alpha: 0, duration: 250, onComplete: () => b.destroy() });
    });
    return true;
  }

  /**
   * The folk just spoke to or about the keeper: answer when they are done —
   * `afterMs` is how long their own bubble shows, so the two take turns instead
   * of stacking up (default: a short beat, for shouts and remarks).
   */
  reply(topic: string, kind: string, afterMs = 1300 + Math.random() * 700) {
    const lines = REPLY_TO[kind]?.[topic] ?? REPLY[topic];
    if (!lines) return;
    // one answer per topic every 8s, so a crowd talking at once gets one reply
    if (now(this.s) - (this.saidAt.get(`reply:${topic}`) ?? -1e12) < 8000) return;
    this.saidAt.set(`reply:${topic}`, now(this.s));
    this.s.time.delayedCall(afterMs, () => this.say(pick(lines), 2800));
  }

  /** The keeper stands within reach of a ledge lip (main.ts update). */
  nearLedge() {
    if (!this.ledgeExplained) {
      if (this.say(LEDGE_FIRST, 5200, 'ledge', 0)) this.ledgeExplained = true;
      return;
    }
    this.say(pick(LEDGE), 2400, 'ledge', 45000);
  }

  /** The server reported a fall. */
  fell() {
    this.ledgeExplained = true; // they know now
    this.say(pick(FELL), 2800, 'fell', 4000);
  }

  /** Keep the bubble over the keeper (and its name label) every frame. */
  tick() {
    const me = this.s.me;
    if (!this.bubble || !me) return;
    this.bubble.setPosition(me.x, this.anchorY()).setVisible(me.visible);
  }

  private anchorY() {
    const me = this.s.me!;
    return me.y - (this.s.showNames ? 74 : 50); // clear of the name label (main.ts: me.y - 58, ~12px tall)
  }
}

const now = (s: Phaser.Scene) => s.time.now;
