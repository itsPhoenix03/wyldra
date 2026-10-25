// Rebindable key map. A leaf module like settings.ts: the game scene, the DOM panels and
// both settings screens read the bindings here, and nothing here reaches back into them.
//
// Bindings are physical keys (KeyboardEvent.code), so they sit in the same place on any
// layout; the labels follow the player's layout where the browser can tell us (below).
// Each action has two slots, a primary and a secondary, either of which may be empty.

export type Action =
  | "moveUp" | "moveDown" | "moveLeft" | "moveRight"
  | "jump" | "interact" | "attack" | "place" | "rotate" | "torch"
  | "hotbar1" | "hotbar2" | "hotbar3" | "hotbar4" | "hotbar5"
  | "drink" | "eat" | "medicine" | "cloak" | "armor" | "boat"
  | "medicAccept" | "medicDecline"
  | "inventory" | "craft" | "build" | "help" | "controls" | "mute"
  | "uiUp" | "uiDown" | "uiLeft" | "uiRight" | "uiConfirm" | "uiNext";

type Slots = [string | null, string | null];

interface ActionDef {
  id: Action;
  label: string;
  group: string;
  keys: Slots;
}

/**
 * Two layers. A key may be shared across them (the arrows walk and also move the
 * selection in a panel), never within one: while a panel holds the keyboard its
 * "menu" keys go to the panel and the game does not see them (ui.ts, isHeld below).
 */
const MENU_GROUP = "Menus";

export const ACTIONS: readonly ActionDef[] = [
  { id: "moveUp", label: "Move up", group: "Movement", keys: ["KeyW", "ArrowUp"] },
  { id: "moveDown", label: "Move down", group: "Movement", keys: ["KeyS", "ArrowDown"] },
  { id: "moveLeft", label: "Move left", group: "Movement", keys: ["KeyA", "ArrowLeft"] },
  { id: "moveRight", label: "Move right", group: "Movement", keys: ["KeyD", "ArrowRight"] },
  { id: "jump", label: "Jump", group: "Movement", keys: ["Space", null] },

  { id: "interact", label: "Gather / use / talk", group: "Actions", keys: ["KeyE", null] },
  { id: "attack", label: "Attack / demolish", group: "Actions", keys: ["KeyF", null] },
  { id: "place", label: "Place in front of you", group: "Actions", keys: ["KeyG", null] },
  { id: "rotate", label: "Rotate while placing", group: "Actions", keys: ["KeyR", null] },
  { id: "torch", label: "Plant a torch (mines)", group: "Actions", keys: ["KeyT", null] },

  { id: "hotbar1", label: "Axe", group: "Tools", keys: ["Digit1", "Numpad1"] },
  { id: "hotbar2", label: "Wooden pickaxe", group: "Tools", keys: ["Digit2", "Numpad2"] },
  { id: "hotbar3", label: "Stone pickaxe", group: "Tools", keys: ["Digit3", "Numpad3"] },
  { id: "hotbar4", label: "Sword", group: "Tools", keys: ["Digit4", "Numpad4"] },
  { id: "hotbar5", label: "Iron sword", group: "Tools", keys: ["Digit5", "Numpad5"] },

  { id: "drink", label: "Drink water", group: "Items", keys: ["KeyQ", null] },
  { id: "eat", label: "Eat (best food first)", group: "Items", keys: ["KeyX", null] },
  { id: "medicine", label: "Take medicine", group: "Items", keys: ["KeyV", null] },
  { id: "cloak", label: "Change cloak", group: "Items", keys: ["KeyZ", null] },
  { id: "armor", label: "Starmetal armor on / off", group: "Items", keys: ["KeyU", null] },
  { id: "boat", label: "Ready a boat", group: "Items", keys: ["KeyL", null] },
  { id: "medicAccept", label: "Accept a medic's offer", group: "Items", keys: ["KeyY", null] },
  { id: "medicDecline", label: "Decline a medic's offer", group: "Items", keys: ["KeyN", null] },

  { id: "inventory", label: "Bag", group: "Panels", keys: ["KeyI", null] },
  { id: "craft", label: "Crafting", group: "Panels", keys: ["KeyC", null] },
  { id: "build", label: "Building", group: "Panels", keys: ["KeyB", null] },
  { id: "help", label: "How to play", group: "Panels", keys: ["KeyH", null] },
  { id: "controls", label: "Controls", group: "Panels", keys: ["KeyO", null] },
  { id: "mute", label: "Sound on / off", group: "Panels", keys: ["KeyM", null] },

  { id: "uiUp", label: "Select up", group: MENU_GROUP, keys: ["ArrowUp", null] },
  { id: "uiDown", label: "Select down", group: MENU_GROUP, keys: ["ArrowDown", null] },
  { id: "uiLeft", label: "Select left", group: MENU_GROUP, keys: ["ArrowLeft", null] },
  { id: "uiRight", label: "Select right", group: MENU_GROUP, keys: ["ArrowRight", null] },
  { id: "uiConfirm", label: "Choose", group: MENU_GROUP, keys: ["Enter", "NumpadEnter"] },
  { id: "uiNext", label: "Next open panel", group: MENU_GROUP, keys: ["Tab", null] },
];

/** Keys no binding may take: Escape always closes and cancels, F9 / F10 are the tester kit. */
export const RESERVED = new Set(["Escape", "F9", "F10"]);

const STORE = "hearth-keybinds";
const DEF = new Map(ACTIONS.map((a) => [a.id, a]));
const layerOf = (a: Action) => (DEF.get(a)!.group === MENU_GROUP ? "menu" : "game");
export const isMenuAction = (a: Action) => layerOf(a) === "menu";

let binds = load();
let byCode = index();

function load(): Map<Action, Slots> {
  const m = new Map<Action, Slots>(ACTIONS.map((a) => [a.id, [...a.keys] as Slots]));
  try {
    const saved = JSON.parse(localStorage.getItem(STORE) || "null");
    if (saved && typeof saved === "object")
      for (const a of ACTIONS) {
        const s = saved[a.id];
        if (Array.isArray(s) && s.length === 2)
          m.set(a.id, s.map((c: unknown) => (typeof c === "string" && !RESERVED.has(c) ? c : null)) as Slots);
      }
  } catch { /* storage blocked or corrupt: the defaults stand */ }
  return m;
}

function index(): Map<string, Action[]> {
  const m = new Map<string, Action[]>();
  for (const [a, slots] of binds)
    for (const c of slots) if (c) m.set(c, [...(m.get(c) || []), a]);
  return m;
}

function commit() {
  byCode = index();
  try { localStorage.setItem(STORE, JSON.stringify(Object.fromEntries(binds))); } catch { /* not persisted */ }
  window.dispatchEvent(new Event("hearth:keybinds"));
}

export function getKeys(a: Action): Slots {
  return binds.get(a)!;
}

/** Every action the key is bound to (at most one per layer). */
export function actionsFor(code: string): Action[] {
  return byCode.get(code) || [];
}

/** Is this key press the given action? */
export function matches(a: Action, e: KeyboardEvent): boolean {
  const [p, s] = binds.get(a)!;
  return e.code !== "" && (e.code === p || e.code === s);
}

/**
 * Bind `code` to one slot of `a` (null clears the slot). Any other action of the same
 * layer that had the key loses it, as in most games; their labels come back so the
 * settings screen can say what moved.
 */
export function setKey(a: Action, slot: 0 | 1, code: string | null): string[] {
  if (code && RESERVED.has(code)) return [];
  const freed: string[] = [];
  if (code)
    for (const [b, slots] of binds) {
      if (layerOf(b) !== layerOf(a)) continue;
      slots.forEach((c, i) => {
        if (c !== code || (b === a && i === slot)) return;
        slots[i] = null;
        if (b !== a) freed.push(DEF.get(b)!.label);
      });
    }
  binds.get(a)![slot] = code;
  commit();
  return freed;
}

export function resetKeys(): void {
  binds = new Map(ACTIONS.map((a) => [a.id, [...a.keys] as Slots]));
  commit();
}

// ---- labels ------------------------------------------------------------------------

const NAMED: Record<string, string> = {
  Space: "Space", Enter: "Enter", NumpadEnter: "Num Enter", Tab: "Tab", Backspace: "Backspace",
  ArrowUp: "↑", ArrowDown: "↓", ArrowLeft: "←", ArrowRight: "→",
  ShiftLeft: "L Shift", ShiftRight: "R Shift", ControlLeft: "L Ctrl", ControlRight: "R Ctrl",
  AltLeft: "L Alt", AltRight: "R Alt", MetaLeft: "L Meta", MetaRight: "R Meta", CapsLock: "Caps",
  Insert: "Ins", Delete: "Del", Home: "Home", End: "End", PageUp: "PgUp", PageDown: "PgDn",
  Backquote: "`", Minus: "-", Equal: "=", BracketLeft: "[", BracketRight: "]", Backslash: "\\",
  Semicolon: ";", Quote: "'", Comma: ",", Period: ".", Slash: "/", IntlBackslash: "\\",
  NumpadAdd: "Num +", NumpadSubtract: "Num -", NumpadMultiply: "Num *", NumpadDivide: "Num /",
  NumpadDecimal: "Num .", ContextMenu: "Menu",
};

/**
 * What the key prints on this player's keyboard where the browser exposes it
 * (navigator.keyboard, Chromium): an AZERTY player bound to KeyQ reads "A". Filled once,
 * asynchronously; until then (and on other browsers) the QWERTY name stands.
 */
let layout: Map<string, string> | null = null;
(navigator as any).keyboard?.getLayoutMap?.().then((m: Map<string, string>) => {
  layout = m;
  window.dispatchEvent(new Event("hearth:keybinds"));
}).catch(() => { /* not allowed here (e.g. in a frame): QWERTY names */ });

export function codeLabel(code: string): string {
  const mapped = layout?.get(code);
  if (mapped && /^\S$/.test(mapped)) return mapped.toUpperCase();
  if (code.startsWith("Key")) return code.slice(3);
  if (code.startsWith("Digit")) return code.slice(5);
  if (code.startsWith("Numpad")) return "Num " + code.slice(6);
  return NAMED[code] || code;
}

/** "I", "W / ↑", or "unbound": the action's keys as the HUD and hints show them. */
export function keyLabel(a: Action, both = false): string {
  const ks = binds.get(a)!.filter(Boolean) as string[];
  if (!ks.length) return "unbound";
  return (both ? ks : ks.slice(0, 1)).map(codeLabel).join(" / ");
}

/**
 * Fill every `[data-key="action"]` element under root with the action's key, and
 * `[data-keys]` with both of its keys. Static markup (the HUD buttons, the help sheet)
 * names the action and this writes the key in, again whenever the bindings change.
 */
export function paintKeyLabels(root: ParentNode = document): void {
  root.querySelectorAll<HTMLElement>("[data-key]").forEach((el) => {
    el.textContent = keyLabel(el.dataset.key as Action);
  });
  root.querySelectorAll<HTMLElement>("[data-keys]").forEach((el) => {
    el.textContent = keyLabel(el.dataset.keys as Action, true);
  });
}

// ---- held keys ---------------------------------------------------------------------

/** True when the key press belongs to a text field, which keeps it from the game. */
export function isTyping(e: Event): boolean {
  const t = e.target as HTMLElement | null;
  if (!t || !t.tagName) return false;
  if (t.isContentEditable || t.tagName === "TEXTAREA") return true;
  if (t.tagName !== "INPUT") return false;
  const type = (t as HTMLInputElement).type;
  return type !== "checkbox" && type !== "radio" && type !== "button" && type !== "range";
}

const held = new Set<string>();
let menuCapture = false;
let capture: ((code: string | null) => void) | null = null;

/** ui.ts sets this while a panel holds the keyboard: its menu keys stop counting as held game keys. */
export function setMenuCapture(on: boolean): void {
  menuCapture = on;
}

/** Is any key of the action down right now (movement, the held gather key)? */
export function isHeld(a: Action): boolean {
  for (const c of binds.get(a)!) {
    if (!c || !held.has(c)) continue;
    if (menuCapture && layerOf(a) === "game" && actionsFor(c).some((b) => layerOf(b) === "menu")) continue;
    return true;
  }
  return false;
}

/**
 * The settings screens' "press a key" prompt: the next key press goes to `cb` and nowhere
 * else (Escape cancels with null; Backspace / Delete clear, reported as ""). Taken in the
 * capture phase on window, so neither the game nor the panels ever see it.
 */
export function captureNextKey(cb: (code: string | null) => void): void {
  const prev = capture;
  capture = cb;
  prev?.(null);   // a prompt still waiting elsewhere is cancelled, not left hanging
}

export function cancelCapture(): void {
  capture = null;
}

window.addEventListener("keydown", (e) => {
  if (capture) {
    e.preventDefault();
    e.stopImmediatePropagation();
    if (e.repeat) return;
    const cb = capture;
    capture = null;
    cb(e.code === "Escape" ? null : e.code === "Backspace" || e.code === "Delete" ? "" : e.code);
    return;
  }
  if (!isTyping(e)) held.add(e.code);
}, true);
window.addEventListener("keyup", (e) => held.delete(e.code), true);
// a key released while the window was in the background never sends its keyup
window.addEventListener("blur", () => held.clear());
document.addEventListener("visibilitychange", () => held.clear());
