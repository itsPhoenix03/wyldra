import Phaser from "phaser";
import { pickView, viewKey, type View } from "./facing.ts";
import { isNightTime, NIGHT_START } from "../shared/time.js";

/* ────────────────────────────────────────────────────────────────────────────
 * Island folk — the villages' villagers, hunters, children and elders.
 *
 * The Go server runs them (gameserver/room/folk.go and friends): who they are,
 * their day, their work, their routes round the village. Camps ride `init` as
 * `camps` (one per hut; a village's huts share one fire, drawn once), and the
 * folk ride every `cre` frame as `f: [id, x, y, kind, act, face, item]`: act is
 * what they are at (folk.go lists them), face the way they face at it (world
 * degrees, or null), item what they carry (wood / stone / fish / game / herbs /
 * berries / reeds / water / torch, or null).
 *
 * This view only draws, and keeps to what the tick loop already did per folk
 * member: a lerped sprite with its drawn clips where it has them (walk, sit,
 * collect, build, craft — carry is the walk, listen is the sit, cook and tend
 * the work clips) and procedural motion where it does not (a wave, a hop for
 * the cheer and the dance, standing tall on guard, a lean into a call), front /
 * back / side art from the way they walk, the way the server says they face,
 * or the keeper they talk to (src/facing.ts); the load in their hands or over
 * their shoulder; a watcher's light at night, and a glow in the door of each
 * hut with someone asleep inside (any of its folk — ids are `<campId>-<k>` —
 * in `hide` after dark); and speech bubbles — a greeting when a keeper walks
 * up or a wave as they go by, a line on E, a shout when they bolt for the hut
 * or stand guard, chatter overheard between neighbours, an elder speaking at
 * the fire, the cook calling everyone to eat, a parent calling a child in, a
 * cheer the evening a monolith is lit.
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

/** [id, x, y, kind, act, face, item]: face is world-space degrees while at something, else null. */
type FolkRow = [string, number, number, string, string, (number | null)?, (string | null)?];

/** What FolkView needs from the game scene. */
export interface FolkHost extends Phaser.Scene {
  isoE(x: number, y: number): { x: number; y: number };
  iso(x: number, y: number): { x: number; y: number };
  world: { loadedAt(x: number, y: number): boolean };
  me: Phaser.GameObjects.Container | undefined;
  px: number;
  py: number;
  z: number;
  /** The world clock (server `time`), 0..1. */
  wtime: number;
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

/** `wave`: a keeper coming into the yard (or a neighbour going by). */
const GREETS: Record<string, string[]> = {
  _: ["Keeper!", "Fair day to you.", "Ho there, keeper.", "Back again?", "Mind the fire, it's lit."],
  morning: ["Morning, keeper.", "You're up early.", "Morning! Kettle's on."],
  evening: ["Evening, keeper.", "Come and sit by the fire.", "Getting dark. Stay close."],
  woods_child: ["Hi! Hi! Keeper!", "Keeper's here!", "Look, it's the keeper!"],
  ashmark_hunter: ["Keeper.", "Wind's fair today."],
  tribal: ["Keeper. Mind the mud.", "Ho."],
};
/** `call`: the cook calling the village to eat, or a parent calling a child in. */
const MEAL_CALLS = ["Food's on! Come and eat!", "Pot's ready — come on, all of you!", "Eat while it's hot!", "Bowls out! It's ready!"];
const KID_CALLS = ["Inside, little one. It's late.", "Bed! Now, please.", "In you come — the dark's coming.", "Where's that child? Inside!"];
/** `guard`: standing between a creature and the huts. */
const GUARD_SHOUTS: Record<string, string[]> = {
  _: ["Stay back!", "Inside, all of you — I'll hold here.", "Easy... let it come.", "Not past me, you won't."],
  ashmark_hunter: ["Spears up.", "It's circling. Watch its flank.", "Hold. Let it come to us."],
  tribal: ["Back in the water with you!", "Hold the path!"],
  elder_woods: ["Get the little ones in!", "I've seen worse than you."],
  shaman: ["The sand will not hide you here.", "Turn back, blight-born."],
};
/** `cheer` / `dance`: the evening after a monolith is lit. */
const CHEERS: Record<string, string[]> = {
  _: ["The stone sings again!", "Light on the ridge!", "A monolith lit — a keeper did that!", "Dance! Dance!", "Hear it hum!"],
  woods_child: ["It's glowing! It's glowing!", "Dance with me!", "Again! Again!"],
  elder_woods: ["My grandmother saw that light. Now so have I."],
  shaman: ["The sleeper wakes. Rejoice."],
  elder_yvenne: ["The marsh remembers that song."],
  elder_spire: ["Warmth, at last. A little."],
};
/** At the pot, or feeding the fire — only overheard close up. */
const COOKING = ["Nearly ready.", "Mind the pot, it spits.", "Who took the salt?", "Another handful of berries, then."];
const TENDING = ["More wood here.", "Keep it fed, keep it bright.", "There. That'll hold till dusk.", "Catch, you."];
/** The watch at night, to a keeper passing close. */
const WATCH_NIGHT = ["All quiet.", "Go careful, keeper. The dark's long.", "I'll keep the light up.", "Something moved by the edge. Gone now."];

/** Moving: the walk loop (play and run are the walk sped up; carry is the walk with a load). */
const MOVE_ACTS = new Set(["walk", "run", "shun", "shelter", "play", "carry"]);
/** Work: a one-shot clip looped with a breather, facing the work. */
const WORK_ACTS = new Set(["collect", "build", "craft", "cook", "tend"]);
/** The work clips each act borrows, in order of preference. */
const WORK_CLIPS: Record<string, string[]> = {
  collect: ["collect"], build: ["build", "collect"], craft: ["craft", "collect"],
  cook: ["craft", "collect"], tend: ["collect", "build"],
};
/** Seated: the sit clip, or a crouch. */
const SIT_ACTS = new Set(["sit", "listen"]);
/** Holding still: drawn facing the way the server says (`face`), not turned by drift. */
const STILL_ACTS = new Set(["sit", "listen", "collect", "build", "craft", "cook", "tend", "chat", "speak", "watch", "idle", "scold",
  "wave", "guard", "call", "cheer", "dance"]);

/** A load: texture, scale, and whether it rides over the shoulder. Missing textures are drawn once into one. */
const ITEMS: Record<string, { tex: string; scale: number; shoulder: boolean }> = {
  wood: { tex: "wood_pop_05", scale: 0.5, shoulder: true },
  stone: { tex: "stone_pop_05", scale: 0.42, shoulder: true },
  game: { tex: "meat_pop_05", scale: 0.48, shoulder: true },
  fish: { tex: "fish", scale: 0.5, shoulder: false },
  herbs: { tex: "folk_herbs", scale: 1, shoulder: false },
  reeds: { tex: "folk_reeds", scale: 1, shoulder: true },
  berries: { tex: "folk_berries", scale: 1, shoulder: false },
  water: { tex: "folk_water", scale: 1, shoulder: false },
  torch: { tex: "folk_torch", scale: 1, shoulder: false },
};
const POPS = new Set(["wood_pop_05", "stone_pop_05", "meat_pop_05"]); // pickup art: crop its ground shadow off

type FolkSpr = Phaser.GameObjects.Sprite;
/** What a folk member has beyond its sprite: its load, its light, and how fast it is going. */
type Extra = {
  item: string | null;
  img?: Phaser.GameObjects.Image;
  light?: Phaser.GameObjects.Image;
  spd: number;
  sayAt: number;
};

export class FolkView {
  private camps: Camp[] = [];
  private pending: Camp[] = [];
  private huts: Phaser.GameObjects.Image[] = [];
  /** A warm glow in each hut's door, lit while someone sleeps inside after dark. */
  private hutGlow = new Map<string, { img: Phaser.GameObjects.Image; on: number; seed: number }>();
  private fires: { img: Phaser.GameObjects.Image; glow: Phaser.GameObjects.Arc; night: Phaser.GameObjects.Image; seed: number }[] = [];
  /** Fire tiles already drawn: a village's huts share one fire. */
  private fireAt = new Set<number>();
  private spr = new Map<string, FolkSpr>();
  private extra = new Map<string, Extra>();
  private bubbles = new Map<string, Phaser.GameObjects.Text>();
  private greetAt = -1e9; // clock of the last greeting to the keeper, from anyone
  private callAt = -1e9; // and of the last call heard
  private lineIdx = new Map<string, number>();
  private asleep = new Map<string, number>(); // camp id -> folk inside, this frame
  private clock = 0;
  private dark = 0;

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
      // the door's glow: above the night's veil, so it lights the dark rather than being dimmed by it
      const glow = this.glowImage(hp.x + 3, hp.y + 4, 0xffa04a).setScale(0.2, 0.15).setAlpha(0);
      this.hutGlow.set(c.id, { img: glow, on: 0, seed: (c.hutX * 13 + c.hutY * 7) % 100 / 100 });
      const fk = c.fireY * 100000 + c.fireX;
      if (this.fireAt.has(fk)) continue;
      this.fireAt.add(fk);
      const fp = s.isoE(c.fireX, c.fireY);
      const img = s.add
        .image(fp.x, fp.y + 16, "campfire")
        .setOrigin(0.5, 0.85)
        .setDepth(fp.y + 18)
        .setVisible(s.z === 0);
      const fglow = s.add
        .circle(fp.x, fp.y + 12, 70, 0xffaa44, 0.13)
        .setDepth(fp.y + 19)
        .setBlendMode(Phaser.BlendModes.ADD)
        .setVisible(s.z === 0);
      // the fire the folk keep: by night its light reaches through the dark
      const night = this.glowImage(fp.x, fp.y + 8, 0xff9a3c);
      this.fires.push({ img, glow: fglow, night, seed: (c.fireX * 31 + c.fireY * 17) % 100 / 100 });
    }
    this.pending = still;
  }

  /** A soft additive light (the scene's `glow-s` disc, tinted), drawn over the night veil. */
  private glowImage(x: number, y: number, tint: number) {
    const s = this.s;
    const key = s.textures.exists("glow-s") ? "glow-s" : this.itemTex("torch");
    return s.add
      .image(x, y, key)
      .setTint(tint)
      .setBlendMode(Phaser.BlendModes.ADD)
      .setDepth(999992)
      .setVisible(false);
  }

  /** Apply the `f` list of a `cre` frame. */
  sync(rows: FolkRow[] | undefined) {
    if (!rows) return;
    const s = this.s;
    const seen = new Set<string>();
    this.asleep.clear();
    for (const [id, x, y, kind, act, face, item] of rows) {
      seen.add(id);
      if (act === "hide") {
        const camp = id.slice(0, id.lastIndexOf("-"));
        this.asleep.set(camp, (this.asleep.get(camp) ?? 0) + 1);
      }
      const p = s.isoE(x, y);
      let sp = this.spr.get(id);
      if (!sp) {
        s.needHuman(kind);
        sp = s.add.sprite(p.x, p.y, kind).setOrigin(0.5, 0.92).setVisible(s.z === 0);
        sp.setData({ kind, act: "", view: "front", face: null, gvx: p.x, gvy: p.y, phase: Math.random() * 6, talkAt: -1e9, shoutAt: -1e9, chatAt: -1e9 });
        this.spr.set(id, sp);
        this.extra.set(id, { item: null, spd: 0, sayAt: -1e9 });
      }
      sp.setData("tx", p.x).setData("ty", p.y);
      const f = typeof face === "number" ? face : null;
      const turned = f !== sp.getData("face");
      sp.setData("face", f);
      const prev = sp.getData("act");
      if (act !== prev) this.enter(id, sp, kind, act, prev);
      else if (turned && STILL_ACTS.has(act) && this.orient(sp)) this.enter(id, sp, kind, act, act); // turned to a new view mid-act
      this.hold(id, typeof item === "string" && act !== "hide" ? item : null);
    }
    for (const [id, sp] of this.spr)
      if (!seen.has(id)) {
        sp.destroy();
        const e = this.extra.get(id);
        e?.img?.destroy();
        e?.light?.destroy();
        this.extra.delete(id);
        this.bubbles.get(id)?.destroy();
        this.bubbles.delete(id);
        this.spr.delete(id);
      }
    // the huts with someone asleep inside after dark glow in the door
    for (const [camp, g] of this.hutGlow) g.on = (this.asleep.get(camp) ?? 0) > 0 ? 1 : 0;
  }

  /** f's load changed (or not): show it, or put it away. */
  private hold(id: string, item: string | null) {
    const e = this.extra.get(id);
    if (!e || e.item === item) return;
    e.item = item;
    if (!item) {
      e.img?.setVisible(false);
      e.light?.setVisible(false);
      return;
    }
    const def = ITEMS[item];
    const tex = this.itemTex(item);
    if (!e.img) e.img = this.s.add.image(0, 0, tex).setOrigin(0.5, 0.5);
    else e.img.setTexture(tex);
    e.img.setScale(def?.scale ?? 1).setVisible(this.s.z === 0);
    if (POPS.has(tex)) e.img.setCrop(0, 6, 32, 27);
    else e.img.setCrop();
    if (item === "torch" && !e.light) e.light = this.glowImage(0, 0, 0xffb257);
  }

  /**
   * The texture for a load: the game's own art where it has some, else a small
   * drawing made once.
   */
  private itemTex(item: string): string {
    const def = ITEMS[item];
    const s = this.s;
    if (def && s.textures.exists(def.tex)) return def.tex;
    const key = def && !POPS.has(def.tex) && def.tex !== "fish" ? def.tex : `folk_${item}`;
    if (s.textures.exists(key)) return key;
    const g = s.make.graphics({}, false);
    switch (item) {
      case "herbs": // a green bunch, tied
        g.fillStyle(0x4f7f3a).fillTriangle(2, 12, 6, 1, 9, 12).fillTriangle(5, 12, 10, 2, 12, 12);
        g.fillStyle(0x7fb05a).fillTriangle(4, 12, 7, 4, 9, 12);
        g.fillStyle(0x8a6a3e).fillRect(4, 10, 6, 2);
        g.generateTexture(key, 14, 14);
        break;
      case "reeds": // a sheaf of long stalks
        g.lineStyle(1.4, 0x8fa856);
        for (let k = 0; k < 5; k++) g.lineBetween(3 + k * 2, 18, 1 + k * 3, 1);
        g.fillStyle(0x7a5a34).fillRect(3, 11, 9, 2);
        g.generateTexture(key, 16, 19);
        break;
      case "berries": // a little basket of them
        g.fillStyle(0x8a6236).fillRoundedRect(1, 6, 12, 7, 2);
        g.fillStyle(0xb5333f).fillCircle(4, 6, 2).fillCircle(7, 5, 2).fillCircle(10, 6, 2);
        g.fillStyle(0x5a3f22).fillRect(1, 9, 12, 1);
        g.generateTexture(key, 14, 14);
        break;
      case "water": // a clay pot, brimming
        g.fillStyle(0x9a5a34).fillEllipse(7, 9, 11, 10);
        g.fillStyle(0x7a4428).fillRect(4, 2, 6, 3);
        g.fillStyle(0x6fb3d6).fillEllipse(7, 2.5, 6, 2);
        g.generateTexture(key, 14, 15);
        break;
      case "torch": // a brand, burning
        g.fillStyle(0x5b3c22).fillRect(3, 6, 2, 12);
        g.fillStyle(0xff8a2a).fillTriangle(0, 7, 4, -1, 8, 7);
        g.fillStyle(0xffd36a).fillTriangle(2, 7, 4, 2, 6, 7);
        g.generateTexture(key, 8, 18);
        break;
      default: // something in a bundle
        g.fillStyle(0x8a7a5a).fillRoundedRect(1, 3, 10, 8, 3);
        g.generateTexture(key, 12, 12);
    }
    g.destroy();
    return key;
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

  /** One of `lines` (a kind's own, sometimes, if it has them), at random. */
  private pick(table: Record<string, string[]>, kind: string) {
    const own = table[kind];
    const lines = own && Math.random() < 0.6 ? own : table._;
    return lines[(Math.random() * lines.length) | 0];
  }

  /** A line from folk member `id` if the keeper is in earshot and they have not just spoken. */
  private remark(id: string, sp: FolkSpr, text: string, tiles: number, gap: number, ms = 2600, named = true) {
    const e = this.extra.get(id);
    if (!e || this.clock - e.sayAt < gap || !this.near(sp, tiles)) return false;
    e.sayAt = this.clock;
    this.say(id, text, ms, named);
    return true;
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
      return;
    }
    if (SIT_ACTS.has(act)) {
      const k = this.clip(sp, "sit");
      sp.setData("clip", k);
      if (k) sp.play(k);
      else this.look(sp);
      return;
    }
    if (WORK_ACTS.has(act)) {
      this.work(sp, act);
      if (prev !== act && act === "cook" && Math.random() < 0.35) this.remark(id, sp, COOKING[(Math.random() * COOKING.length) | 0], 5, 20, 2400, false);
      else if (prev !== act && act === "tend" && Math.random() < 0.25) this.remark(id, sp, TENDING[(Math.random() * TENDING.length) | 0], 5, 20, 2400, false);
      return;
    }
    this.look(sp);
    if (prev === act) return; // only turned
    switch (act) {
      case "scold":
        if (this.clock - (sp.getData("scoldAt") ?? -1e9) > 6 && this.near(sp, 9)) {
          sp.setData("scoldAt", this.clock);
          this.say(id, this.pick(SCOLDS, kind), 3000);
          this.s.keeperReply("scold", kind, 3100);
        }
        break;
      case "talk":
        // one greeting at a time: a crowd round the keeper used to speak all at once
        if (this.clock - sp.getData("talkAt") > 12 && this.clock - this.greetAt > 4 && this.near(sp, 3.2)) {
          this.greetAt = this.clock;
          sp.setData("talkAt", this.clock);
          this.say(id, this.nextLine(id, kind), 3600);
          this.s.keeperReply("talk", kind, 3700); // once their bubble is gone: turns, not a pile-up
        }
        break;
      case "chat":
        if (this.clock - sp.getData("chatAt") > 8 && this.near(sp, 10) && Math.random() < 0.55) {
          // overheard: one line, a beat after they turn to each other
          sp.setData("chatAt", this.clock);
          const lines = CHATTER[kind] || CHATTER.villager;
          s.time.delayedCall(300 + Math.random() * 1500, () => {
            if (sp.active && sp.getData("act") === "chat") this.say(id, lines[(Math.random() * lines.length) | 0], 2600, false);
          });
        }
        break;
      case "speak":
        if (this.near(sp, 12)) {
          const lines = SPEECH[kind];
          if (lines) this.say(id, lines[(Math.random() * lines.length) | 0], 4600);
        }
        break;
      case "wave": {
        // to the keeper, now and then a word; one greeting at a time from the whole yard
        const me = this.s.me;
        if (me && this.clock - this.greetAt > 5 && Math.random() < 0.45 && this.near(sp, 7)) {
          const t = this.s.wtime;
          const part = t < 0.3 ? GREETS.morning : t > 0.6 ? GREETS.evening : null;
          const text = part && Math.random() < 0.5 ? part[(Math.random() * part.length) | 0] : this.pick(GREETS, kind);
          if (this.remark(id, sp, text, 7, 15, 2200)) this.greetAt = this.clock;
        }
        break;
      }
      case "call":
        if (this.clock - this.callAt > 4) {
          const kid = this.s.wtime > 0.55;
          const lines = kid ? KID_CALLS : MEAL_CALLS;
          if (this.remark(id, sp, lines[(Math.random() * lines.length) | 0], kid ? 9 : 14, 8, 2600)) this.callAt = this.clock;
        }
        break;
      case "guard":
        if (this.clock - sp.getData("shoutAt") > 8) {
          sp.setData("shoutAt", this.clock);
          this.remark(id, sp, this.pick(GUARD_SHOUTS, kind), 12, 8, 2200);
        }
        break;
      case "cheer":
      case "dance":
        if (Math.random() < 0.4) this.remark(id, sp, this.pick(CHEERS, kind), 12, 10, 2400);
        break;
      case "watch":
        if (this.extra.get(id)?.item === "torch" && Math.random() < 0.3) this.remark(id, sp, WATCH_NIGHT[(Math.random() * WATCH_NIGHT.length) | 0], 4, 40, 2600);
        break;
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
        sp.anims.timeScale = act === "run" || act === "shun" || act === "shelter" ? 1.7 : act === "play" ? 1.5 : act === "carry" ? 0.9 : 1;
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
    let key = "";
    for (const c of WORK_CLIPS[act] ?? ["collect"]) if ((key = this.clip(sp, c))) break;
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
      f.night.setVisible(false); // tick lights it again up top, after dark
    }
    for (const [id, sp] of this.spr) {
      const out = z === 0 && sp.getData("act") !== "hide";
      sp.setVisible(out);
      const e = this.extra.get(id);
      e?.img?.setVisible(out && !!e.item);
      e?.light?.setVisible(out && e.item === "torch");
    }
    for (const g of this.hutGlow.values()) g.img.setVisible(false); // tick turns them back on up top
    for (const b of this.bubbles.values()) b.setVisible(z === 0);
  }

  /** Per frame: lerp, face, procedural motion, the loads and lights, fire flicker, bubbles follow. */
  tick(dt: number) {
    this.clock += dt;
    const s = this.s;
    // how dark it is: the night veil's own curve (main.ts), 0 by day … 1 at night
    const t = s.wtime;
    const dusk = NIGHT_START - 0.1;
    this.dark = isNightTime(t) ? 1 : t > dusk ? Math.min(1, (t - dusk) * 10) : 0;
    const up = s.z === 0;
    for (const f of this.fires) {
      const n = Math.sin(this.clock * (5 + f.seed * 4) + f.seed * 30) * 0.6 + Math.sin(this.clock * (11 + f.seed * 6)) * 0.4;
      f.glow.setAlpha(0.13 * (1 + n * 0.22)).setScale(1 + n * 0.05);
      f.img.setScale(1 + n * 0.02, 1 - n * 0.02);
      const lit = up && this.dark > 0.05;
      if (lit !== f.night.visible) f.night.setVisible(lit);
      if (lit) f.night.setAlpha(0.42 * this.dark * (1 + n * 0.12)).setScale(0.55 + n * 0.02, 0.36 + n * 0.015);
    }
    for (const g of this.hutGlow.values()) {
      const on = up && g.on > 0 && this.dark > 0.05;
      if (on !== g.img.visible) g.img.setVisible(on);
      if (on) g.img.setAlpha(0.42 * this.dark * (1 + Math.sin(this.clock * (2.3 + g.seed) + g.seed * 20) * 0.08));
    }
    const me = s.me;
    for (const [id, sp] of this.spr) {
      const tx = sp.getData("tx"), ty = sp.getData("ty");
      let vx = sp.getData("gvx"), vy = sp.getData("gvy");
      const nx = vx + (tx - vx) * 0.15, ny = vy + (ty - vy) * 0.15;
      const mx = nx - vx, my = ny - vy;
      vx = nx;
      vy = ny;
      sp.setData("gvx", vx).setData("gvy", vy);
      const act: string = sp.getData("act");
      const e = this.extra.get(id)!;
      const faced = STILL_ACTS.has(act) && sp.getData("face") !== null;
      // facing: along the walk, toward the keeper they talk to, or the way the server says
      if (act === "talk" && me) sp.setFlipX(me.x < vx);
      else if (!faced && mx < -0.15) sp.setFlipX(true);
      else if (!faced && mx > 0.15) sp.setFlipX(false);
      if (!faced && act !== "hide" && !WORK_ACTS.has(act) && !SIT_ACTS.has(act)) {
        const cur: View = sp.getData("view");
        let view = cur;
        if (act === "talk" && me) view = pickView(me.x - vx, me.y - vy, cur);
        else if (Math.hypot(mx, my) > 0.15) view = pickView(mx, my, cur);
        if (view !== cur) {
          sp.setData("view", view);
          this.look(sp);
        }
      }
      // the stride keeps up with the ground covered: a guard hurrying to a post, a pair out on the hunt
      if (dt > 0) e.spd += (Math.hypot(mx, my) / dt - e.spd) * 0.1;
      const drawn = !!sp.getData("clip");
      if (drawn && (act === "walk" || act === "carry")) sp.anims.timeScale = Phaser.Math.Clamp(e.spd / 22, act === "carry" ? 0.85 : 1, 1.8);
      // procedural motion where there is no drawn clip, and small life on top of standing
      const fast = act === "run" || act === "shun" || act === "shelter";
      const ph = sp.getData("phase") + dt * (fast ? 16 : act === "play" ? 13 : 9);
      sp.setData("phase", ph);
      let lift = 0, rot = 0, sy = 1;
      if (MOVE_ACTS.has(act)) {
        if (!drawn) {
          lift = Math.abs(Math.sin(ph)) * (act === "walk" || act === "carry" ? 1.6 : 3);
          rot = Math.sin(ph) * (act === "walk" || act === "carry" ? 0.05 : 0.09);
        } else if (act !== "walk" && act !== "carry") {
          lift = Math.abs(Math.sin(ph)) * 1.5;
        }
      } else if (act === "wave") {
        rot = Math.sin(ph * 2.2) * 0.1; // a hand up, side to side
        lift = Math.abs(Math.sin(ph * 2.2)) * 1.2;
      } else if (act === "guard") {
        sy = 1.03; // standing tall, braced
        rot = Math.sin(ph * 0.4) * 0.015;
      } else if (act === "call") {
        rot = -0.06 + Math.sin(ph * 1.6) * 0.04; // leaning into the shout
        sy = 1.02;
      } else if (act === "cheer") {
        lift = Math.abs(Math.sin(ph * 1.8)) * 5; // jumping for joy
        rot = Math.sin(ph * 0.9) * 0.05;
      } else if (act === "dance") {
        lift = Math.abs(Math.sin(ph * 1.5)) * 4;
        rot = Math.sin(ph * 0.75) * 0.13;
      } else if (!drawn) {
        if (SIT_ACTS.has(act)) sy = 0.82;
        else if (WORK_ACTS.has(act)) {
          sy = 0.94 + Math.sin(ph * 0.5) * 0.04;
          rot = Math.sin(ph * 0.5) * 0.04;
        } else if (act === "chat") sy = 1 + Math.sin(ph * 0.7) * 0.018; // nodding along
        else if (act === "speak") rot = Math.sin(ph * 0.3) * 0.035; // gesturing to the gathering
        else if (act === "scold") rot = Math.sin(ph * 1.1) * 0.07; // waving the keeper off
        else sy = 1 + Math.sin(ph * 0.25) * 0.012; // breathing
      }
      sp.setPosition(vx, vy - lift).setRotation(rot).setScale(1, sy).setDepth(vy);
      if (e.img && e.item && sp.visible) this.place(sp, e, vx, vy - lift);
      const b = this.bubbles.get(id);
      if (b) b.setPosition(sp.x, sp.y - sp.displayHeight - 4);
    }
  }

  /** The load at the hands (or over the shoulder), on the side they face; and a watcher's light. */
  private place(sp: FolkSpr, e: Extra, x: number, y: number) {
    const img = e.img!;
    if (!img.visible) img.setVisible(true);
    const h = sp.displayHeight, view: View = sp.getData("view"), side = sp.flipX ? -1 : 1;
    const def = ITEMS[e.item!];
    let ox = 0, oy = -h * 0.4, dz = 0.5;
    if (e.item === "torch") {
      ox = (view === "back" ? -9 : 9) * side;
      oy = -h * 0.58;
    } else if (def?.shoulder) {
      ox = (view === "back" ? -9 : 9) * side; // up on the shoulder beside the head, seen from behind too
      oy = -h * 0.6;
    } else if (view === "side") {
      ox = 8 * side;
      oy = -h * 0.42;
    } else if (view === "back") {
      ox = 3 * side;
      oy = -h * 0.46;
      dz = -0.5; // in their arms, the far side of them
    } else {
      ox = 5 * side;
      oy = -h * 0.36;
    }
    img.setPosition(x + ox, y + oy).setDepth(sp.depth + dz).setFlipX(sp.flipX);
    if (e.light) {
      const on = this.dark > 0.05 && e.item === "torch";
      if (on !== e.light.visible) e.light.setVisible(on);
      if (on)
        e.light
          .setPosition(x + ox, y + oy - 6)
          .setScale(0.27 + Math.sin(this.clock * 9 + x) * 0.01)
          .setAlpha(0.45 * this.dark * (0.92 + Math.sin(this.clock * 13 + y) * 0.08));
    }
  }
}
