import Phaser from "phaser";
import { pickView, viewKey, type View } from "./facing.ts";

/* ────────────────────────────────────────────────────────────────────────────
 * Island folk — the villages' villagers, hunters, children and elders.
 *
 * The Go server runs them (gameserver/room/folk.go): camps ride `init` as
 * `camps` (one per hut; a village's huts share one fire, drawn once), the folk
 * ride every `cre` frame as `f: [id, x, y, kind, act, face]`, where act is
 * idle / walk / run / play / talk / chat / speak / watch / sit / collect /
 * build / craft / hide and face the way they face at it (folk.go). This view
 * only draws: the hut and fire of each camp, a lerped sprite per folk member
 * with its drawn clips where it has them and a procedural bob where it does
 * not, front / back / side art from the way they walk, the way the server
 * says they face, or the keeper they talk to (src/facing.ts), and speech
 * bubbles — a greeting when a keeper walks up, a line on E, a shout when they
 * bolt for the hut, chatter overheard between neighbours, an elder speaking
 * at the fire.
 * ──────────────────────────────────────────────────────────────────────────── */

export type Camp = {
  id: string;
  islandId: string;
  hutSprite: string;
  hutX: number;
  hutY: number;
  fireX: number;
  fireY: number;
};

/** [id, x, y, kind, act, face]: face is world-space degrees while at something, else null. */
type FolkRow = [string, number, number, string, string, (number | null)?];

/** What FolkView needs from the game scene. */
export interface FolkHost extends Phaser.Scene {
  isoE(x: number, y: number): { x: number; y: number };
  iso(x: number, y: number): { x: number; y: number };
  world: { loadedAt(x: number, y: number): boolean };
  me: Phaser.GameObjects.Container | undefined;
  px: number;
  py: number;
  z: number;
  /** A folk member of this kind is in view: stream its clips in if they are not yet (src/atlas.ts). */
  needHuman(kind: string): void;
  /** The weather now blowing (server `wx`): null when the sky is clear. */
  weather: string | null;
  /** The folk just spoke to or about the keeper (talk / scold / shun / weather / clear): they answer. */
  keeperReply(topic: string, kind: string, afterMs?: number): void;
}

const NAMES: Record<string, string> = {
  villager: "Hearthfolk",
  villager2: "Hearthfolk",
  woods_child: "Child",
  ashmark_hunter: "Ashmark hunter",
  elder_yvenne: "Elder Yvenne",
  elder_woods: "Elder of the Woods",
  shaman: "Ashmark shaman",
  tribal: "Marsh hunter",
  elder_spire: "Elder of the Spire",
  villager_snow: "Spire folk",
};

// Grounded, short, in-world: what people living on a blighted island would say.
const LINES: Record<string, string[]> = {
  villager: [
    "Keeper. Wood's stacked if the fire needs it.",
    "Crawlers came through the ferns last night. Mind your step.",
    "We patch the walls every morning. Blight eats the old posts.",
    "Light a fire before dark. They don't like the flame.",
    "My father kept the monolith on the north ridge. Before.",
  ],
  villager2: [
    "Mending nets, mostly. Fish don't care about the blight.",
    "If you find fiber, the reeds by the shore are best.",
    "The hut's small, but the door bars. That's what counts.",
    "The wolves hunt in threes. One comes straight, the others go wide.",
    "Rest a while, keeper. You look half-frozen.",
  ],
  woods_child: [
    "I saw a wisp! It was blue and it went right through a tree!",
    "Mum says I can't go past the stones.",
    "Are you a keeper? Do you fight the crawlers?",
    "The deer let me get really close this morning.",
  ],
  ashmark_hunter: [
    "Stalkers circle before they strike. Turn when they turn.",
    "Sand hides tracks fast. Read them early or not at all.",
    "Lancers keep their distance. Close it, or find a wall.",
    "I keep the fire. Somebody has to.",
    "The heat cloak's worth the hides. Trust me.",
  ],
  elder_yvenne: [
    "The marsh remembers what the land forgets, child.",
    "Bog shamblers are slow. Their rot is not.",
    "The old songs say the monoliths sang once. Light them and listen.",
    "Mud on your boots, blight in your blood. Wash both.",
    "Sit. The fire is warm, and the night is long.",
  ],
  elder_woods: [
    "Six roofs now. When I was young it was two, and a fire we hid.",
    "The young ones build. I remember where the old posts stood.",
    "Keep the north path clear. That's the way the blight comes.",
    "Every hearth here was lit from this one. Mind it, keeper.",
  ],
  shaman: [
    "The sand speaks to those who sit still long enough.",
    "My hunters keep the fire. I keep the old words.",
    "The monolith on the dune is not dead. It only sleeps.",
    "Walk soft, keeper. The Ashmark do not forget a heavy step.",
  ],
  tribal: [
    "The water hides the shamblers. Watch for bubbles.",
    "Yvenne says the marsh is our mother. Mothers bite.",
    "Reeds for the roof, reeds for the spear. The marsh gives.",
    "Three huts. Enough for us. Too many and the mud swallows them.",
  ],
  elder_spire: [
    "Only the medic and I stay up here. The cold keeps the rest away.",
    "The wind carries voices off the peak. Don't answer them.",
    "Fur first, then fire, then sleep. In that order, or not at all.",
    "I've buried three winters' worth of keepers. Don't be the fourth.",
  ],
  villager_snow: [
    "Snow's deep past the pines. Stay where the ground's packed.",
    "The medic's hut has the warm stove. Ours has the wood for it.",
    "Frost wraiths drift down at dusk. Get inside before the light goes.",
    "My hands haven't been warm since the thaw that never came.",
  ],
};

const SHOUTS = ["Blight! Inside, quick!", "They're coming — get in!", "Run!", "To the hut!"];

// The folk turn on a keeper who harms their island (server: gameserver/room/folk_regard.go).
/** `shelter`: heading in from the storm on their island (server: folk_weather.go). */
const STORM_SHOUTS: Record<string, string[]> = {
  sandstorm: ["Sand's rising — get inside!", "Cover your eyes, in you go!", "Storm off the dunes!"],
  snowstorm: ["White-out — to the stove!", "Blizzard! Everyone in!", "Bar the door against it!"],
  rain: ["It's pouring, Mum!", "Race you inside!", "I'm soaked!"],
};
/** Folk in earshot remark when the weather turns. Keyed by kind, then by whether it is their island's. */
const WEATHER_TALK: Record<string, { here: string[]; there: string[] }> = {
  sandstorm: { here: ["Here it comes. Inside, all of you.", "The dunes are walking again."], there: ["Someone's eating sand on the Dunes today."] },
  snowstorm: { here: ["Feel that wind? Blizzard's coming.", "Stoke the stove. Now."], there: ["The Spire's catching it today."] },
  rain: { here: ["Here comes the rain.", "Good for the crops, at least.", "Little ones — inside!"], there: ["Smells like rain over the lowlands."] },
  clear: { here: ["It's passing.", "Sky's clearing.", "Out you come, it's done."], there: [] },
};
/** Which weather bites which island (server weather.go). */
const WEATHER_ISLES: Record<string, string[]> = { sandstorm: ["dunes"], snowstorm: ["spire"], rain: ["woods", "marsh"] };

/** `shun`: running from a keeper the village has turned against. */
const SHUN_SHOUTS = ["It's the tree-cutter — inside!", "Don't let them near the little ones!", "Get in, they're back!", "Bar the door!"];
/** `scold`: standing to tell a keeper off — for harm they just saw, or harm they remember. */
const SCOLDS: Record<string, string[]> = {
  _: ["Leave our trees be!", "You'll strip this island bare.", "That's our game you're killing.", "Take what you need. Not all of it.", "The ground dies where the trees go."],
  woods_child: ["You cut down the climbing tree!", "Mum says you're greedy.", "Leave the deer alone!"],
  elder_woods: ["Every tree you fell, the blight takes the ground behind it.", "We have kept this wood for three lifetimes. Leave it standing."],
  shaman: ["The sand remembers every step. Yours are heavy, keeper.", "You hunt like the blight hunts — for nothing."],
  ashmark_hunter: ["We hunt to eat. You hunt to empty the dunes.", "Leave some for the rest of us."],
  elder_yvenne: ["The marsh gives, and you only take.", "Mud on your boots, rot in your wake. Go."],
  tribal: ["You've muddied the water for all of us.", "Go fish somewhere else."],
  elder_spire: ["Wood is life up here. You're taking ours.", "Burn your own trees, keeper."],
  villager_snow: ["Those pines took a hundred winters to grow.", "We'll freeze if you take them all."],
};

/** What drifts over when two neighbours chat (only to a keeper close enough to hear). */
const CHATTER: Record<string, string[]> = {
  villager: ["Roof's holding?", "Hand me the twine.", "Wolves were close last night.", "Fire wants feeding.", "Hear that, up by the ridge?"],
  villager2: ["Berries are coming in.", "Mind the little ones.", "These nets won't mend themselves.", "Your hut or mine for supper?"],
  woods_child: ["Tag! You're it!", "Can't catch me!", "I found a beetle!", "Let's go see the keeper!"],
  ashmark_hunter: ["Tracks by the east dune.", "Wind's turned.", "Two stalkers. Maybe three.", "Keep your spear close."],
  elder_yvenne: ["How is the little one?", "The water's high this year.", "Rest your back a while."],
  elder_woods: ["How's that wall holding?", "Good work today.", "Keep the north path clear."],
  shaman: ["The sand is restless.", "Bring me ash from the fire.", "Listen to the wind."],
  tribal: ["Fish are biting.", "Watch for bubbles.", "Reeds are tall this year."],
  elder_spire: ["Wrap up, it's bitter.", "Is the medic's stove lit?"],
  villager_snow: ["Snow's coming again.", "Pass the furs.", "Ice on the path."],
};

/** An elder speaking to the gathering at the fire. */
const SPEECH: Record<string, string[]> = {
  elder_woods: ["We built these roofs with our own hands. We keep them.", "Eat well. Tomorrow the wall needs all of us.", "The blight takes what's left untended. So we tend."],
  shaman: ["The sand remembers every step. Walk true.", "Hunters — the wind is yours tonight.", "The monolith sleeps. We keep its fire."],
  elder_yvenne: ["The marsh gives, and the marsh takes. Give thanks for both.", "Three roofs, one fire. That is enough.", "Hush. The water is speaking."],
  elder_spire: ["The cold is patient. Be more patient.", "Keep the stove fed and the door barred."],
};

/** Moving: the walk loop (play and run are the walk sped up). */
const MOVE_ACTS = new Set(["walk", "run", "shun", "shelter", "play"]);
/** Work: a one-shot clip looped with a breather, facing the work. */
const WORK_ACTS = new Set(["collect", "build", "craft"]);
/** Holding still: drawn facing the way the server says (`face`), not turned by drift. */
const STILL_ACTS = new Set(["sit", "collect", "build", "craft", "chat", "speak", "watch", "idle", "scold"]);

type FolkSpr = Phaser.GameObjects.Sprite;

export class FolkView {
  private camps: Camp[] = [];
  private pending: Camp[] = [];
  private huts: Phaser.GameObjects.Image[] = [];
  private fires: { img: Phaser.GameObjects.Image; glow: Phaser.GameObjects.Arc; seed: number }[] = [];
  /** Fire tiles already drawn: a village's huts share one fire. */
  private fireAt = new Set<number>();
  private spr = new Map<string, FolkSpr>();
  private bubbles = new Map<string, Phaser.GameObjects.Text>();
  private greetAt = -1e9; // clock of the last greeting to the keeper, from anyone
  private lineIdx = new Map<string, number>();
  private clock = 0;

  constructor(private s: FolkHost) {}

  /** The camp roster from `init`. Call blockTiles() for the tiles to block. */
  setCamps(camps: Camp[] | undefined) {
    this.camps = Array.isArray(camps) ? camps : [];
    this.pending = this.camps.slice();
    this.placePending();
  }

  /** Hut and fire tiles of every camp, as tile indices on a SIZE-wide world. */
  blockTiles(size: number): number[] {
    return this.camps.flatMap((c) => [c.hutY * size + c.hutX, c.fireY * size + c.fireX]);
  }

  /** Place the camps whose tiles have streamed in. */
  placePending() {
    if (!this.pending.length) return;
    const s = this.s;
    const still: Camp[] = [];
    for (const c of this.pending) {
      if (!s.world.loadedAt(c.hutX, c.hutY) || !s.world.loadedAt(c.fireX, c.fireY)) {
        still.push(c);
        continue;
      }
      const hp = s.isoE(c.hutX, c.hutY);
      this.huts.push(
        s.add
          .image(hp.x, hp.y + 16, c.hutSprite)
          .setOrigin(0.5, 0.92)
          .setDepth(hp.y + 18) // lifted like the folk themselves (tick: setDepth(vy)), or they sort behind it on hills
          .setVisible(s.z === 0),
      );
      const fk = c.fireY * 100000 + c.fireX;
      if (this.fireAt.has(fk)) continue;
      this.fireAt.add(fk);
      const fp = s.isoE(c.fireX, c.fireY);
      const img = s.add
        .image(fp.x, fp.y + 16, "campfire")
        .setOrigin(0.5, 0.85)
        .setDepth(fp.y + 18)
        .setVisible(s.z === 0);
      const glow = s.add
        .circle(fp.x, fp.y + 12, 70, 0xffaa44, 0.13)
        .setDepth(fp.y + 19)
        .setBlendMode(Phaser.BlendModes.ADD)
        .setVisible(s.z === 0);
      this.fires.push({ img, glow, seed: (c.fireX * 31 + c.fireY * 17) % 100 / 100 });
    }
    this.pending = still;
  }

  /** Apply the `f` list of a `cre` frame. */
  sync(rows: FolkRow[] | undefined) {
    if (!rows) return;
    const s = this.s;
    const seen = new Set<string>();
    for (const [id, x, y, kind, act, face] of rows) {
      seen.add(id);
      const p = s.isoE(x, y);
      let sp = this.spr.get(id);
      if (!sp) {
        s.needHuman(kind);
        sp = s.add.sprite(p.x, p.y, kind).setOrigin(0.5, 0.92).setVisible(s.z === 0);
        sp.setData({ kind, act: "", view: "front", face: null, gvx: p.x, gvy: p.y, phase: Math.random() * 6, talkAt: -1e9, shoutAt: -1e9, chatAt: -1e9 });
        this.spr.set(id, sp);
      }
      sp.setData("tx", p.x).setData("ty", p.y);
      const f = typeof face === "number" ? face : null;
      const turned = f !== sp.getData("face");
      sp.setData("face", f);
      const prev = sp.getData("act");
      if (act !== prev) this.enter(id, sp, kind, act, prev);
      else if (turned && STILL_ACTS.has(act) && this.orient(sp)) this.enter(id, sp, kind, act, act); // turned to a new view mid-act
    }
    for (const [id, sp] of this.spr)
      if (!seen.has(id)) {
        sp.destroy();
        this.bubbles.get(id)?.destroy();
        this.bubbles.delete(id);
        this.spr.delete(id);
      }
  }

  /**
   * The weather turned (`kind` null: it cleared). Up to two folk in earshot remark
   * on it — urgently if it is their island's weather, idly if it is somewhere else.
   * `isle` is the island the keeper is on (src/zones.ts), which is theirs.
   */
  onWeather(kind: string | null, isle: string | null) {
    const talk = WEATHER_TALK[kind ?? "clear"];
    const was = this.lastWeather;
    this.lastWeather = kind;
    if (!talk || !isle) return;
    // a clear sky only matters where the weather was biting
    const here = (WEATHER_ISLES[kind ?? was ?? ""] ?? []).includes(isle);
    const lines = here ? talk.here : talk.there;
    if (!lines.length) return;
    let n = 0;
    for (const [id, sp] of this.spr) {
      if (n >= 2) break;
      if (!sp.visible || sp.getData("act") === "hide" || !this.near(sp, 10)) continue;
      n++;
      this.s.time.delayedCall(200 + n * 900 + Math.random() * 500, () => {
        if (sp.active && sp.visible) this.say(id, lines[(Math.random() * lines.length) | 0], 2800);
      });
    }
    // the keeper answers once they have had their say
    if (n) this.s.time.delayedCall(n * 900 + 400, () => this.s.keeperReply(kind ? "weather" : "clear", ""));
  }
  private lastWeather: string | null = null;

  /**
   * The clips of `kind` just streamed in: restart whatever each of them is doing,
   * so one who has been standing in for a missing clip picks it up.
   */
  refreshKind(kind: string) {
    for (const [id, sp] of this.spr) {
      const act: string = sp.getData("act");
      if (sp.getData("kind") === kind && act !== "hide") this.enter(id, sp, kind, act, act);
    }
  }

  /**
   * Turn a still folk member the way the server says they face (world-space
   * degrees): pick the view for it on screen, and flip side art to face left.
   * True if the view changed.
   */
  private orient(sp: FolkSpr): boolean {
    const face: number | null = sp.getData("face");
    if (face === null) return false;
    const a = (face * Math.PI) / 180, wx = Math.cos(a), wy = Math.sin(a);
    const sx = (wx - wy) * 32, sy = (wx + wy) * 16; // iso: a tile is 64×32
    if (sx < -1) sp.setFlipX(true);
    else if (sx > 1) sp.setFlipX(false);
    const cur: View = sp.getData("view"), view = pickView(sx, sy, cur);
    sp.setData("view", view);
    return view !== cur;
  }

  /** A folk member changed what they are doing. */
  private enter(id: string, sp: FolkSpr, kind: string, act: string, prev: string) {
    const s = this.s;
    sp.setData("act", act).setData("clip", "");
    sp.anims.stop();
    sp.anims.timeScale = 1;
    if (act === "hide") {
      s.tweens.add({ targets: sp, alpha: 0, duration: 250, onComplete: () => sp.setVisible(false) });
      return;
    }
    if (prev === "hide" || sp.alpha < 1) {
      sp.setVisible(s.z === 0);
      s.tweens.add({ targets: sp, alpha: 1, duration: 300 });
    }
    if (STILL_ACTS.has(act)) this.orient(sp);
    if (MOVE_ACTS.has(act)) {
      this.look(sp);
      if ((act === "run" || act === "shun" || act === "shelter") && this.clock - sp.getData("shoutAt") > 6) {
        sp.setData("shoutAt", this.clock);
        const lines = act === "shun" ? SHUN_SHOUTS : act === "shelter" ? STORM_SHOUTS[this.s.weather ?? ""] ?? SHOUTS : SHOUTS;
        this.say(id, lines[(Math.random() * lines.length) | 0], 1800);
        if (act === "shun" && this.near(sp, 8)) this.s.keeperReply("shun", kind);
      }
    } else if (act === "sit") {
      const k = this.clip(sp, "sit");
      sp.setData("clip", k);
      if (k) sp.play(k);
      else this.look(sp);
    } else if (WORK_ACTS.has(act)) {
      this.work(sp, act);
    } else if (act === "scold") {
      this.look(sp);
      if (this.clock - (sp.getData("scoldAt") ?? -1e9) > 6 && this.near(sp, 9)) {
        sp.setData("scoldAt", this.clock);
        const lines = SCOLDS[kind] && Math.random() < 0.6 ? SCOLDS[kind] : SCOLDS._;
        this.say(id, lines[(Math.random() * lines.length) | 0], 3000);
        this.s.keeperReply("scold", kind, 3100);
      }
    } else {
      this.look(sp);
      if (prev === act) return; // only turned
      // one greeting at a time: a crowd round the keeper used to speak all at once
      if (act === "talk" && this.clock - sp.getData("talkAt") > 12 && this.clock - this.greetAt > 4 && this.near(sp, 3.2)) {
        this.greetAt = this.clock;
        sp.setData("talkAt", this.clock);
        this.say(id, this.nextLine(id, kind), 3600);
        this.s.keeperReply("talk", kind, 3700); // once their bubble is gone: turns, not a pile-up
      } else if (act === "chat" && this.clock - sp.getData("chatAt") > 8 && this.near(sp, 10) && Math.random() < 0.55) {
        // overheard: one line, a beat after they turn to each other
        sp.setData("chatAt", this.clock);
        const lines = CHATTER[kind] || CHATTER.villager;
        s.time.delayedCall(300 + Math.random() * 1500, () => {
          if (sp.active && sp.getData("act") === "chat") this.say(id, lines[(Math.random() * lines.length) | 0], 2600, false);
        });
      } else if (act === "speak" && this.near(sp, 12)) {
        const lines = SPEECH[kind];
        if (lines) this.say(id, lines[(Math.random() * lines.length) | 0], 4600);
      }
    }
  }

  /**
   * Show the folk member's current view: the walk loop for that view (keeping
   * the stride where it was), or the standing pose.
   */
  private look(sp: FolkSpr) {
    const kind: string = sp.getData("kind"), act: string = sp.getData("act"), view: View = sp.getData("view");
    if (MOVE_ACTS.has(act)) {
      const k = view === "front" ? `${kind}_walk` : `${kind}_${view}_walk`;
      if (this.s.anims.exists(k)) {
        const at = sp.anims.isPlaying ? sp.anims.currentFrame?.index ?? 1 : 1;
        sp.play({ key: k, startFrame: at - 1 });
        sp.anims.timeScale = act === "run" || act === "shun" || act === "shelter" ? 1.7 : act === "play" ? 1.5 : 1;
        sp.setData("clip", k);
        return;
      }
    }
    sp.anims.stop();
    const key = viewKey(kind, view);
    sp.setTexture(this.s.textures.exists(key) ? key : kind);
  }

  /** The `<kind>_<view>_<a>` anim for the folk member's view (front if that view lacks it), or "". */
  private clip(sp: FolkSpr, a: string) {
    const kind: string = sp.getData("kind"), view: View = sp.getData("view");
    const k = view === "front" ? `${kind}_${a}` : `${kind}_${view}_${a}`;
    return this.s.anims.exists(k) ? k : this.s.anims.exists(`${kind}_${a}`) ? `${kind}_${a}` : "";
  }

  /** Loop a one-shot work clip with a short breather between passes; kinds without one stand in their view. */
  private work(sp: FolkSpr, act: string) {
    const key = this.clip(sp, act) || (act !== "collect" ? this.clip(sp, "collect") : "");
    sp.setData("clip", key);
    if (!key) return this.look(sp);
    sp.play(key);
    sp.once(Phaser.Animations.Events.ANIMATION_COMPLETE, () => {
      if (!sp.active || sp.getData("act") !== act) return;
      this.look(sp);
      sp.setData("clip", key); // still at work, between passes
      this.s.time.delayedCall(350 + Math.random() * 900, () => {
        if (sp.active && sp.getData("act") === act) this.work(sp, act);
      });
    });
  }

  private near(sp: FolkSpr, tiles: number) {
    const me = this.s.me;
    return !!me && Math.hypot(sp.x - me.x, sp.y - me.y) < tiles * 32;
  }

  private nextLine(id: string, kind: string) {
    const lines = LINES[kind] || LINES.villager;
    const n = this.lineIdx.get(id) ?? (id.length * 7) % lines.length;
    this.lineIdx.set(id, (n + 1) % lines.length);
    return lines[n];
  }

  /** A speech bubble over a folk member's head (with their name, unless it is overheard chatter). */
  private say(id: string, text: string, ms: number, named = true) {
    const sp = this.spr.get(id);
    if (!sp || !sp.visible) return;
    this.bubbles.get(id)?.destroy();
    const who = named ? NAMES[sp.getData("kind")] || "" : "";
    const b = this.s.add
      .text(sp.x, sp.y - sp.displayHeight - 4, who ? `${who}\n${text}` : text, {
        fontFamily: "Georgia, serif",
        fontSize: named ? "11px" : "10px",
        color: "#2b2118",
        backgroundColor: named ? "#f1e6cbee" : "#f1e6cbcc",
        padding: { x: 6, y: 4 },
        align: "center",
        wordWrap: { width: 170 },
      })
      .setOrigin(0.5, 1)
      .setDepth(1e6);
    this.bubbles.set(id, b);
    this.s.time.delayedCall(ms, () => {
      if (this.bubbles.get(id) === b) {
        this.s.tweens.add({ targets: b, alpha: 0, duration: 250, onComplete: () => b.destroy() });
        this.bubbles.delete(id);
      }
    });
  }

  /** E near a folk member: they say their next line. True if anyone answered. */
  talk(): boolean {
    let best: [string, FolkSpr] | null = null;
    let bd = 2.4 * 32;
    const me = this.s.me;
    if (!me) return false;
    for (const e of this.spr) {
      const sp = e[1];
      if (!sp.visible || sp.getData("act") === "run") continue;
      const d = Math.hypot(sp.x - me.x, sp.y - me.y);
      if (d < bd) [bd, best] = [d, e];
    }
    if (!best) return false;
    const [id, sp] = best;
    sp.setData("talkAt", this.clock);
    this.say(id, this.nextLine(id, sp.getData("kind")), 4200);
    return true;
  }

  setVisible(z: number) {
    for (const h of this.huts) h.setVisible(z === 0);
    for (const f of this.fires) {
      f.img.setVisible(z === 0);
      f.glow.setVisible(z === 0);
    }
    for (const sp of this.spr.values()) sp.setVisible(z === 0 && sp.getData("act") !== "hide");
    for (const b of this.bubbles.values()) b.setVisible(z === 0);
  }

  /** Per frame: lerp, face, procedural motion, fire flicker, bubbles follow. */
  tick(dt: number) {
    this.clock += dt;
    for (const f of this.fires) {
      const n = Math.sin(this.clock * (5 + f.seed * 4) + f.seed * 30) * 0.6 + Math.sin(this.clock * (11 + f.seed * 6)) * 0.4;
      f.glow.setAlpha(0.13 * (1 + n * 0.22)).setScale(1 + n * 0.05);
      f.img.setScale(1 + n * 0.02, 1 - n * 0.02);
    }
    const me = this.s.me;
    for (const [id, sp] of this.spr) {
      const tx = sp.getData("tx"), ty = sp.getData("ty");
      let vx = sp.getData("gvx"), vy = sp.getData("gvy");
      const nx = vx + (tx - vx) * 0.15, ny = vy + (ty - vy) * 0.15;
      const mx = nx - vx, my = ny - vy;
      vx = nx;
      vy = ny;
      sp.setData("gvx", vx).setData("gvy", vy);
      const act: string = sp.getData("act");
      const faced = STILL_ACTS.has(act) && sp.getData("face") !== null;
      // facing: along the walk, toward the keeper they talk to, or the way the server says
      if (act === "talk" && me) sp.setFlipX(me.x < vx);
      else if (!faced && mx < -0.15) sp.setFlipX(true);
      else if (!faced && mx > 0.15) sp.setFlipX(false);
      if (!faced && act !== "hide" && !WORK_ACTS.has(act) && act !== "sit") {
        const cur: View = sp.getData("view");
        let view = cur;
        if (act === "talk" && me) view = pickView(me.x - vx, me.y - vy, cur);
        else if (Math.hypot(mx, my) > 0.15) view = pickView(mx, my, cur);
        if (view !== cur) {
          sp.setData("view", view);
          this.look(sp);
        }
      }
      // procedural motion where there is no drawn clip, and small life on top of standing
      const ph = sp.getData("phase") + dt * (act === "run" || act === "shun" || act === "shelter" ? 16 : act === "play" ? 13 : 9);
      sp.setData("phase", ph);
      let lift = 0, rot = 0, sy = 1;
      const drawn = !!sp.getData("clip");
      if (MOVE_ACTS.has(act)) {
        if (!drawn) {
          lift = Math.abs(Math.sin(ph)) * (act === "walk" ? 1.6 : 3);
          rot = Math.sin(ph) * (act === "walk" ? 0.05 : 0.09);
        } else if (act !== "walk") {
          lift = Math.abs(Math.sin(ph)) * 1.5;
        }
      } else if (!drawn) {
        if (act === "sit") sy = 0.82;
        else if (WORK_ACTS.has(act)) {
          sy = 0.94 + Math.sin(ph * 0.5) * 0.04;
          rot = Math.sin(ph * 0.5) * 0.04;
        } else if (act === "chat") sy = 1 + Math.sin(ph * 0.7) * 0.018; // nodding along
        else if (act === "speak") rot = Math.sin(ph * 0.3) * 0.035; // gesturing to the gathering
        else if (act === "scold") rot = Math.sin(ph * 1.1) * 0.07; // waving the keeper off
        else sy = 1 + Math.sin(ph * 0.25) * 0.012; // breathing
      }
      sp.setPosition(vx, vy - lift).setRotation(rot).setScale(1, sy).setDepth(vy);
      const b = this.bubbles.get(id);
      if (b) b.setPosition(sp.x, sp.y - sp.displayHeight - 4);
    }
  }
}
