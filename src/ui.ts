import { RECIPES, NAMES, RESOURCES, MATERIALS, MODULES, canAfford } from '../shared/defs.js';
// Decor kinds that are zone-restricted (for UI greying)
const DECOR_ZONE_IN  = new Set(Object.entries(RECIPES as any).filter(([,r]: any) => r.decor && r.zone === 'in').map(([k]) => k));
const DECOR_ZONE_OUT = new Set(Object.entries(RECIPES as any).filter(([,r]: any) => r.decor && r.zone === 'out').map(([k]) => k));
import { isNightTime } from '../shared/time.js';
import { actionsFor, isMenuAction, keyLabel, matches, paintKeyLabels, setMenuCapture, type Action } from './keybinds.ts';
import { mountControls } from './controlsUI.ts';

const $ = (id: string) => document.getElementById(id)!;
/** A board's scrolling inside (index.html: plaque, close stud and hint stay above it). */
const body = (id: string) => $(id).querySelector<HTMLElement>('.board-body') || $(id);
// set by initUI() so reset() can reach the singleton's closure state without exporting it
let resetSession: (() => void) | null = null;

// ---- notifications -------------------------------------------------------------------
// The game's messages stack in the top-right corner (#toasts), newest on top. Each wears an
// icon in a socket and the colour of its kind, with a bar running down the time it has left.
// A message already showing is refreshed rather than shown twice; a click dismisses one.
export type ToastKind = 'info' | 'hint' | 'good' | 'danger' | 'weather' | 'player' | 'note' | 'status';
const TOAST_ICON: Record<ToastKind, string> = {
  info: '✦', hint: '✋', good: '✨', danger: '⚠️', weather: '☁️', player: '👤', note: '📜', status: '⏳',
};
// the small heading over the text; the kinds without one are plain messages
const TOAST_HEAD: Partial<Record<ToastKind, string>> = {
  danger: 'Danger', weather: 'Weather', player: 'Keepers', note: 'A note',
};
// a message's own leading icon (🌧 Rain sweeps…) moves into the socket, and picks its kind
// when the caller named none
const LEAD_ICON = /^(\p{Extended_Pictographic}️?)\s*/u;
const ICON_KIND: Record<string, ToastKind> = {
  '🌧': 'weather', '🌪': 'weather', '❄': 'weather', '☀': 'weather',
  '⚔': 'danger', '💥': 'danger', '🔥': 'danger',
  '✨': 'good', '⚡': 'good', '💊': 'good', '🌱': 'good',
};
const TOAST_MAX = 4;
const toastTimers = new Map<HTMLElement, number>();

function dropToast(el: HTMLElement) {
  clearTimeout(toastTimers.get(el));
  toastTimers.delete(el);
  if (el.classList.contains('out')) return;
  el.classList.add('out');
  window.setTimeout(() => el.remove(), 260);   // after the slide out
}

/** Show a notification. An empty message clears them all (quitting to the menu). */
export function showMsg(s: string, ms = 3500, kind?: ToastKind) {
  const box = $('toasts');
  if (!s) {
    for (const el of Array.from(box.children) as HTMLElement[]) { clearTimeout(toastTimers.get(el)); el.remove(); }
    toastTimers.clear();
    return;
  }
  const lead = s.match(LEAD_ICON);
  const text = lead ? s.slice(lead[0].length) : s;
  const k: ToastKind = kind ?? (lead ? ICON_KIND[lead[1].replace('️', '')] : undefined) ?? 'info';
  let el = (Array.from(box.children) as HTMLElement[]).find((x) => x.dataset.msg === s && !x.classList.contains('out'));
  if (el) {
    box.prepend(el);
    // restart the time bar
    const bar = el.querySelector<HTMLElement>('.toast-bar')!;
    bar.style.animation = 'none';
    void bar.offsetWidth;
    bar.style.animation = '';
  } else {
    el = document.createElement('div');
    el.className = `toast t-${k}`;
    el.dataset.msg = s;
    const head = TOAST_HEAD[k];
    el.innerHTML = `<span class="toast-ic"></span><div class="toast-tx">${head ? `<small>${head}</small>` : ''}<span></span></div><i class="toast-bar"></i>`;
    el.querySelector('.toast-ic')!.textContent = lead ? lead[1] : TOAST_ICON[k];
    el.querySelector('.toast-tx > span')!.textContent = text;   // server text: never parsed as markup
    el.addEventListener('click', () => dropToast(el!));
    box.prepend(el);
    const live = (Array.from(box.children) as HTMLElement[]).filter((x) => !x.classList.contains('out'));
    for (const old of live.slice(TOAST_MAX)) dropToast(old);
  }
  el.querySelector<HTMLElement>('.toast-bar')!.style.animationDuration = `${ms}ms`;
  clearTimeout(toastTimers.get(el));
  toastTimers.set(el, window.setTimeout(() => dropToast(el!), ms));
}

// Drop every scrap of per-session state (signature caches, selection, chest + medic offer)
// on quit-to-menu. The caches exist to skip redundant DOM writes, so a re-join that produced
// an identical signature — e.g. the same medic offer — would otherwise never be drawn at all,
// leaving its panel hidden. initUI() itself is deliberately NOT re-run (listeners would double).
export function reset() {
  showMsg('');
  resetSession?.();
}

export interface UIState {
  hp: number; hunger: number; thirst: number;
  inv: any; tools: Set<string>; gear: Set<string>; equipped: string | null;
  wornGear: string | null;
  armorOn: boolean;
  inWater: boolean;
  selectedVehicle: 'boat' | 'sboat' | null;
  mono: boolean[]; day: number; time: number; won: boolean;
  nearWorkbench: boolean; nearForge: boolean; nearCampfire: boolean;
  waveSecs: number;
  engineHp: number | null;
  zone: 'in' | 'out';
  medicOffer: { medicId: string; offerId: string; resource: string; amount: number; expiresAt: number } | null;
}

const icon = (k: string) =>
  ({ wood: '🪵', stone: '🪨', fiber: '🌿', crystal: '💎', essence: '🟣', water: '💧', meat: '🥩',
     cookedmeat: '🍖', wall: '🧱', campfire: '🔥', workbench: '🛠', forge: '⚙', core: '🔮',
     engine: '💠', axe: '🪓', pick: '⛏', spick: '⛏', sword: '🗡', heatcloak: '🧥', furcloak: '🧣',
     iron: '🔩', diamond: '🔷', mineshaft: '🕳', isword: '⚔',
     starmetal: '✨', boat: '🛶', sboat: '🚤', torch: '🕯', chest: '📦', bed: '🛏',
     banner: '🚩', stone_path: '🪨', lantern: '🏮', reed_vase: '🌾', rug: '🟫', trophy_antler: '🦌',
     fence: '🪵', farmplot: '🌱', grain: '🌾', glowcap: '✨', bread: '🍞', medicine: '🧪',
     wood_planks: '🟫', stone_blocks: '⬜', reed_thatch: '🌾', rope_coil: '🪢', cloth_roll: '🧵',
     clay_bricks: '🧱', glass_pane: '🪟', crystal_lattice: '🔷',
     starmetal_armor: '🛡' } as any)[k] || '▪';

const HOTBAR = ['axe', 'pick', 'spick', 'sword', 'isword'];
const hotKey = (t: string) => keyLabel(`hotbar${HOTBAR.indexOf(t) + 1}` as Action);
// the food the eat key reaches for first (main.ts eat(): most filling first)
const EAT_ORDER = ['cookedmeat', 'bread', 'glowcap'];
const kb = (a: Action) => `<small class="kb">${keyLabel(a)}</small>`;
const CLOAKS = ['heatcloak', 'furcloak', 'starmetal_armor'];   // wearables shown on the quickbar
const isWorn = (st: { wornGear: string | null; armorOn: boolean }, t: string) =>
  t === 'starmetal_armor' ? st.armorOn : st.wornGear === t;

export function initUI(
  onCraft: (r: string) => void,
  onSelectPlace: (kind: string | null) => void,
  onUse: (k: string) => void,
  onEquip: (k: string) => void,
  onWear: (k: string | null) => void,
  onVehicle: (k: 'boat' | 'sboat') => void,
  onChestMove: (i: number, res: string, n: number) => void,
  onMedicAccept: (medicId: string, offerId: string) => void,
  onMedicDecline: (medicId: string) => void,
  onSelectModule: (kind: string | null) => void
) {
  let selected: string | null = null;
  let selectedMod: string | null = null;
  let invSig = '', panelSig = '', buildSig = '';
  let chestOpenI = -1;
  let chestSlots: Record<string, number> = {};
  let chestSig = '';
  let medicOffer: UIState['medicOffer'] = null;
  let medicSig = '';

  // ---- panels and the keyboard ----------------------------------------------------
  // The panels a player can open are kept in the order they were opened; the newest one
  // still showing holds the keyboard: the menu keys (arrows, Enter, Tab by default) move
  // and choose in it instead of reaching the game. Each panel names what can be chosen.
  const NAV_ITEMS: Record<string, string> = {
    invModal: '.slot[data-use], .slot[data-eq], .slot[data-wear], .slot[data-veh], .slot[data-k]',
    craftPanel: 'button[data-r]',
    buildPanel: 'button[data-mod]',
    chestPanel: 'button[data-res]',
    controlsModal: 'button:not(.board-x)',
    helpModal: '',
    quitModal: 'button[data-quit]',
  };
  let stack: string[] = [];
  let navSel: HTMLElement | null = null;
  let navIn = '';
  const navKeys: Record<string, { key: string; i: number }> = {};
  // boards are flex columns (a fixed plaque over a scrolling body), so "open" is display:flex
  const isOpen = (id: string) => $(id).style.display === 'flex';
  const setOpen = (id: string, open: boolean) => {
    $(id).style.display = open ? 'flex' : 'none';
    stack = stack.filter((x) => x !== id);
    if (open) stack.push(id);
    if (id === 'craftPanel') panelSig = '';
    if (id === 'buildPanel') buildSig = '';
  };
  const navPanel = () => {
    for (let i = stack.length - 1; i >= 0; i--) if (isOpen(stack[i])) return stack[i];
    return null;
  };
  const itemsOf = (id: string) =>
    NAV_ITEMS[id] ? Array.from($(id).querySelectorAll<HTMLElement>(NAV_ITEMS[id])) : [];
  // panels rebuild their markup when the state changes, so the choice is remembered by
  // its data-* attributes (the recipe, the module, the chest row...) and found again
  const keyOf = (el: HTMLElement) =>
    el.getAttributeNames().filter((n) => n.startsWith('data-')).map((n) => `${n}=${el.getAttribute(n)}`).join('&');
  const mark = (id: string | null, el: HTMLElement | null) => {
    if (navSel !== el) navSel?.classList.remove('kbsel');
    navSel = el;
    navIn = id || '';
    if (!el || !id) return;
    el.classList.add('kbsel');
    navKeys[id] = { key: keyOf(el), i: itemsOf(id).indexOf(el) };
  };
  /** Re-attach the keyboard choice after a rebuild; run every UI frame and before each key. */
  const navSync = () => {
    const id = navPanel();
    setMenuCapture(!!id);
    if (!id) return mark(null, null);
    if (navSel?.isConnected && navIn === id && $(id).contains(navSel)) return;
    const items = itemsOf(id);
    const was = navKeys[id];
    const el = (was && (items.find((x) => keyOf(x) === was.key) || items[Math.min(was.i, items.length - 1)])) || items[0] || null;
    mark(id, el);
  };
  /** Move the choice to the nearest item on screen that way (lists, the bag's wrapped rows, the chest grid). */
  const navMove = (id: string, dx: number, dy: number) => {
    const items = itemsOf(id);
    if (!items.length) { body(id).scrollTop += dy * 48; return; }   // the help sheet just scrolls
    const cur = navSel && items.includes(navSel) ? navSel : null;
    if (!cur) return mark(id, items[0]);
    const r = cur.getBoundingClientRect();
    let best: HTMLElement | null = null, bestScore = Infinity;
    for (const el of items) {
      if (el === cur) continue;
      const q = el.getBoundingClientRect();
      const ox = q.left + q.width / 2 - (r.left + r.width / 2), oy = q.top + q.height / 2 - (r.top + r.height / 2);
      const along = ox * dx + oy * dy, across = Math.abs(dx ? oy : ox);
      if (along <= 2 || (dx && across > r.height)) continue;   // left / right stay on the row
      const score = along + across * 3;
      if (score < bestScore) { bestScore = score; best = el; }
    }
    // nothing that way: step through in reading order, wrapping at the ends
    if (!best) best = items[(items.indexOf(cur) + (dx + dy > 0 ? 1 : items.length - 1)) % items.length];
    mark(id, best);
    best.scrollIntoView({ block: 'nearest', inline: 'nearest' });
  };

  const closeChestPanel = () => {
    chestOpenI = -1;
    chestSlots = {};
    chestSig = '';
    setOpen('chestPanel', false);
  };
  const togglePanel = () => setOpen('craftPanel', !isOpen('craftPanel'));
  const toggleModal = (id: string) => setOpen(id, !isOpen(id));
  const toggleBuildPanel = () => setOpen('buildPanel', !isOpen('buildPanel'));
  const clearSelection = () => {
    if (selected) { selected = null; invSig = ''; onSelectPlace(null); }
    if (selectedMod) { selectedMod = null; buildSig = ''; onSelectModule(null); }
  };
  $('craftBtn').onclick = togglePanel;
  $('buildBtn').onclick = toggleBuildPanel;
  $('invBtn').onclick = () => toggleModal('invModal');
  $('helpBtn').onclick = () => toggleModal('helpModal');
  $('controlsBtn').onclick = () => toggleModal('controlsModal');
  mountControls($('controlsBody'));
  // Clicking a HUD button or a panel entry must not leave the browser focus on it, or the
  // next Space / Enter would press it again instead of reaching the game.
  for (const id of ['btns', 'quickbar', 'invModal', 'helpModal', 'craftPanel', 'buildPanel', 'chestPanel', 'medicPanel', 'controlsModal', 'quitModal'])
    $(id).addEventListener('mousedown', (e) => e.preventDefault());
  // the leave-the-world question (api.confirmQuit): Stay closes it, Leave hands over to main.ts
  let onLeave: (() => void) | null = null;
  $('quitModal').addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest<HTMLElement>('[data-quit]');
    if (!b) return;
    setOpen('quitModal', false);
    navSync();
    const leave = onLeave;
    onLeave = null;
    if (b.dataset.quit === 'leave') leave?.();
  });
  // every board's close stud (the × on its corner)
  document.addEventListener('click', (e) => {
    const board = (e.target as HTMLElement).closest('[data-close]')?.closest('.board');
    if (!board) return;
    if (board.id === 'chestPanel') closeChestPanel();
    else setOpen(board.id, false);
    navSync();
  });
  // The choice outline shows only while the keyboard is in use; the pointer hides it again.
  document.addEventListener('keydown', () => document.body.classList.add('kb-nav'), true);
  document.addEventListener('pointerdown', () => document.body.classList.remove('kb-nav'), true);
  // Rebinding a key rewrites every label that names one: the static markup through
  // data-key, the panels by dropping their signatures so they redraw.
  const relabel = () => {
    paintKeyLabels();
    invSig = panelSig = buildSig = medicSig = chestSig = '';
  };
  window.addEventListener('hearth:keybinds', relabel);
  relabel();

  /**
   * The panels' share of a key press (main.ts routes every in-game key press here first).
   * True when the press was used up. Escape closes the newest panel; with none open it
   * drops a pending placement and reports false so the scene clears its ghost as well.
   */
  const handleKey = (e: KeyboardEvent): boolean => {
    if (e.code === 'Escape') {
      const top = navPanel();
      if (top === 'chestPanel') closeChestPanel();
      else if (top) setOpen(top, false);
      if (top) { navSync(); return true; }
      clearSelection();
      return false;
    }
    navSync();
    const id = navPanel();
    if (id) {
      const act = actionsFor(e.code).find(isMenuAction);
      if (act === 'uiUp') navMove(id, 0, -1);
      else if (act === 'uiDown') navMove(id, 0, 1);
      else if (act === 'uiLeft') navMove(id, -1, 0);
      else if (act === 'uiRight') navMove(id, 1, 0);
      else if (act === 'uiConfirm') { if (!e.repeat) navSel?.click(); }
      else if (act === 'uiNext' && !e.repeat) {
        // hand the keyboard to the next panel that is showing
        const open = stack.filter(isOpen);
        if (open.length > 1) { stack = [id, ...stack.filter((x) => x !== id)]; navSync(); }
      }
      if (act) return true;
    }
    if (e.repeat) return false;
    if (matches('inventory', e)) toggleModal('invModal');
    else if (matches('craft', e)) togglePanel();
    else if (matches('build', e)) toggleBuildPanel();
    else if (matches('help', e)) toggleModal('helpModal');
    else if (matches('controls', e)) toggleModal('controlsModal');
    // the medic's own buttons do the checking (Accept is disabled when you cannot pay)
    else if (matches('medicAccept', e) && medicOffer) $('medicPanel').querySelector<HTMLElement>('[data-medic-accept]')?.click();
    else if (matches('medicDecline', e) && medicOffer) $('medicPanel').querySelector<HTMLElement>('[data-medic-decline]')?.click();
    else return false;
    navSync();
    return true;
  };

  // Delegated chest panel buttons — attached once, never rebuilt
  $('chestPanel').addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!btn || !btn.dataset.res || chestOpenI < 0) return;
    const res = btn.dataset.res;
    const n = parseInt(btn.dataset.n || '0', 10);
    if (n) onChestMove(chestOpenI, res, n);
  });

  // Delegated build-panel clicks. Selecting a module is a toggle, and selecting
  // one cancels any structure placement — the scene shares one ghost between them.
  $('buildPanel').addEventListener('click', (e) => {
    const btn = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!btn || !btn.dataset.mod) return;
    selectedMod = selectedMod === btn.dataset.mod ? null : btn.dataset.mod!;
    if (selectedMod && selected) { selected = null; invSig = ''; onSelectPlace(null); }
    buildSig = '';
    onSelectModule(selectedMod);
  });

  // Delegated clicks: elements are only rebuilt when state changes, so clicks land reliably.
  $('inv').addEventListener('click', (e) => {
    const el = (e.target as HTMLElement).closest('.slot') as HTMLElement | null;
    if (!el) return;
    if (el.dataset.use) onUse(el.dataset.use);
    else if (el.dataset.eq) onEquip(el.dataset.eq);
    // Task 4: wear delegate — data-wear="" means null (unequip), data-wear="key" means equip
    else if (el.dataset.wear !== undefined) onWear(el.dataset.wear || null);
    // Task 4a: vehicle selection toggle
    else if (el.dataset.veh) onVehicle(el.dataset.veh as 'boat' | 'sboat');
    else if (el.dataset.k && RECIPES[el.dataset.k]?.place) {
      selected = selected === el.dataset.k ? null : el.dataset.k!;
      invSig = '';
      if (selected && selectedMod) { selectedMod = null; buildSig = ''; onSelectModule(null); }
      onSelectPlace(selected);
      if (selected) setOpen('invModal', false);   // clear the view for placing
    }
  });
  $('quickbar').addEventListener('click', (e) => {
    const el = (e.target as HTMLElement).closest('.slot') as HTMLElement | null;
    if (!el) return;
    if (el.dataset.use) onUse(el.dataset.use);
    else if (el.dataset.eq) onEquip(el.dataset.eq);
    // Task 4: wear from quickbar
    else if (el.dataset.wear !== undefined) onWear(el.dataset.wear || null);
  });
  $('craftPanel').addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (b && !b.disabled && b.dataset.r) onCraft(b.dataset.r);
  });
  // Delegated medic panel buttons — same rebuild-safe pattern as craftPanel/chestPanel above.
  $('medicPanel').addEventListener('click', (e) => {
    const b = (e.target as HTMLElement).closest('button') as HTMLButtonElement | null;
    if (!b || !medicOffer) return;
    if (b.dataset.medicAccept !== undefined && !b.disabled) onMedicAccept(medicOffer.medicId, medicOffer.offerId);
    else if (b.dataset.medicDecline !== undefined) onMedicDecline(medicOffer.medicId);
  });

  // Public surface for chest panel management (called from main.ts)
  const api = {
    openChest(i: number, slots: Record<string, number>) {
      chestOpenI = i;
      chestSlots = { ...slots };
      chestSig = '';
      setOpen('chestPanel', true);
    },
    updateChest(i: number, slots: Record<string, number>) {
      if (i !== chestOpenI) return;
      chestSlots = { ...slots };
      chestSig = '';  // force redraw
    },
    closeChest: closeChestPanel,
    isChestOpen: () => chestOpenI >= 0,
    handleKey,
    /** Ask before leaving the world; `leave` runs only if the player picks Leave. */
    confirmQuit(leave: () => void) {
      onLeave = leave;
      delete navKeys.quitModal;   // the keyboard choice always starts on Stay
      setOpen('quitModal', true);
      navSync();
    },
    /** Close every window and drop any pending placement (quitting to the menu). */
    closeAll() {
      for (const id of ['invModal', 'helpModal', 'controlsModal', 'quitModal']) setOpen(id, false);
      closeChestPanel();
      clearSelection();
      navSync();
    },
  };

  // see reset() above — the whole closure state, in one place
  resetSession = () => {
    closeChestPanel();
    selected = null;
    selectedMod = null;
    stack = [];
    mark(null, null);
    invSig = ''; panelSig = '';
    medicOffer = null; medicSig = '';
    $('medicPanel').style.display = 'none';
  };

  return Object.assign(function update(st: UIState) {
    // Task 2: use isNightTime
    const night = isNightTime(st.time);
    const hour = ((st.time * 24 + 6) % 24) | 0;
    const warn = (v: number) =>
      v <= 3 ? `<b style="color:#ff6a6a">${v}</b>` : `${v}`;
    $('hud').innerHTML =
      `${'❤'.repeat(Math.max(0, st.hp))}${'🖤'.repeat(Math.max(0, 10 - st.hp))}` +
      ` 💧${warn(st.thirst)} 🍖${warn(st.hunger)}<br>` +
      `Day ${st.day} · ${String(hour).padStart(2, '0')}:00 ${night ? '🌙 <b style="color:#b98ef0">BLIGHT STORM</b>' : '☀'}` +
      ` · 🗿 ${st.mono.filter(Boolean).length}/4` +
      (st.waveSecs > 0 ? `<br><b style="color:#ff6a6a">⚔ DEFEND THE ENGINE: ${st.waveSecs}s</b>` : '') +
      (st.engineHp != null ? `<br>💠 <b style="color:${st.engineHp > 60 ? '#6dd6c8' : st.engineHp > 30 ? '#ffd97a' : '#ff6a6a'}">Engine: ${st.engineHp}/120 HP</b>` : '') +
      (st.won ? '<br>✨ <b style="color:#6dd6c8">THE BLIGHT IS PURGED — VICTORY</b>' : '');

    // inventory (modal) + quickbar — rebuild only when contents change
    // Task 4: add wornGear + selectedVehicle to signature
    const sig = JSON.stringify([st.inv, [...st.tools], [...st.gear], selected, st.equipped, st.wornGear, st.armorOn, st.selectedVehicle, st.inWater, st.zone]);
    if (sig !== invSig) {
      invSig = sig;
      // the bag: one grid of sockets per kind of thing, each socket its icon, name, count
      // and what clicking it does
      const tile = (k: string, cls: string, attrs: string, count: number | null, act = '') =>
        `<span class="slot ${cls}" ${attrs}><span class="ic">${icon(k)}</span><span class="nm">${NAMES[k] || k}</span>` +
        (count != null ? `<span class="ct">${count}</span>` : '') + (act ? `<span class="act">${act}</span>` : '') + '</span>';
      const section = (title: string, tiles: string) =>
        tiles ? `<div class="board-group">${title}</div><div class="inv-grid">${tiles}</div>` : '';
      const food = EAT_ORDER.find((k) => st.inv[k]);
      const useAct: Record<string, string> = {
        water: `Drink [${keyLabel('drink')}]`, medicine: `Take [${keyLabel('medicine')}]`,
        cookedmeat: 'Eat', bread: 'Eat', glowcap: 'Eat',
      };
      if (food) useAct[food] = `Eat [${keyLabel('eat')}]`;
      let tiles = RESOURCES.map((r) => tile(r, st.inv[r] ? '' : 'empty', '', st.inv[r] || 0)).join('');
      let html = section('Resources', tiles);
      tiles = '';
      for (const k of ['water', 'meat', 'cookedmeat', 'grain', 'glowcap', 'bread', 'medicine'])
        if (st.inv[k]) tiles += useAct[k] ? tile(k, 'use', `data-use="${k}"`, st.inv[k], useAct[k]) : tile(k, '', '', st.inv[k]);
      html += section('Food & drink', tiles);
      // tools and wearables: equip / wear with a click
      tiles = HOTBAR.filter((t) => st.tools.has(t)).map((t) =>
        tile(t, `tool ${st.equipped === t ? 'eq' : ''}`, `data-eq="${t}"`, null, st.equipped === t ? '✓ In hand' : `Equip [${hotKey(t)}]`)).join('');
      // data-wear="" means null (unequip), data-wear="key" means equip
      tiles += [...st.gear].map((t) =>
        tile(t, `tool ${isWorn(st, t) ? 'eq' : ''}`, `data-wear="${t}"`, null, isWorn(st, t) ? '✓ Worn' : 'Wear')).join('');
      for (const k of ['boat', 'sboat', 'torch']) {
        if (!st.inv[k]) continue;
        if (k === 'torch') tiles += tile(k, '', '', st.inv[k], `In mines [${keyLabel('torch')}]`);
        else if (st.inWater) tiles += tile(k, 'off', '', st.inv[k], 'Reach land');
        else {
          const sel = st.selectedVehicle === k;
          tiles += tile(k, `tool ${sel ? 'sel' : ''}`, `data-veh="${k}"`, st.inv[k], sel ? '⛵ Selected' : 'Select');
        }
      }
      html += section('Tools & gear', tiles);
      // building materials — crafted intermediates, shown as a plain count
      // (driven by MATERIALS so a new material never needs a UI edit)
      tiles = '';
      for (const k of MATERIALS)
        if (st.inv[k]) tiles += tile(k, '', '', st.inv[k]);
      html += section('Building materials', tiles);
      tiles = '';
      for (const k of ['wall', 'campfire', 'workbench', 'forge', 'mineshaft', 'chest', 'bed', 'core', 'engine',
                        'banner', 'stone_path', 'lantern', 'reed_vase', 'rug', 'trophy_antler', 'fence', 'farmplot']) {
        if (!st.inv[k]) continue;
        const r = (RECIPES as any)[k];
        // hide (not grey) placeables that cannot go in the current zone:
        // the mines accept only chest/bed and indoor-eligible decor;
        // the surface hides indoor-only decor.
        if (r?.place) {
          const interiorOk = k === 'chest' || k === 'bed' || (r.decor && (r.zone === 'in' || r.zone === 'both'));
          if (st.zone === 'in' && !interiorOk) continue;
          if (st.zone === 'out' && r.zone === 'in') continue;
        }
        tiles += tile(k, `place ${selected === k ? 'sel' : ''}`, `data-k="${k}"`, st.inv[k],
          r?.place ? (selected === k ? '🔨 Placing' : '🔨 Place') : '');
      }
      html += section('To place', tiles);
      $('inv').innerHTML = html;
      // quickbar: equipped tools + quick consumables + owned cloaks
      let qb = HOTBAR.filter((t) => st.tools.has(t)).map((t) =>
        `<span class="slot tool ${st.equipped === t ? 'eq' : ''}" data-eq="${t}">${hotKey(t)} ${icon(t)}</span>`).join('');
      // each quick slot wears the key that uses it: drink, the food eat would pick, medicine, cloak / armor
      for (const k of ['water', 'cookedmeat', 'bread', 'glowcap', 'medicine'])
        if (st.inv[k]) {
          const key = k === 'water' ? kb('drink') : k === 'medicine' ? kb('medicine') : k === food ? kb('eat') : '';
          qb += `<span class="slot use" data-use="${k}">${icon(k)}${st.inv[k]}${key}</span>`;
        }
      // Task 4: cloaks in quickbar
      for (const t of CLOAKS) {
        if (st.gear.has(t)) {
          qb += `<span class="slot tool ${isWorn(st, t) ? 'eq' : ''}" data-wear="${t}">${icon(t)}${isWorn(st, t) ? '✓' : ''}${kb(t === 'starmetal_armor' ? 'armor' : 'cloak')}</span>`;
        }
      }
      $('quickbar').innerHTML = qb;
    }

    // chest panel — rebuild only when slots signature changes
    if (chestOpenI >= 0) {
      const cSig = JSON.stringify([chestSlots, st.inv]);
      if (cSig !== chestSig) {
        chestSig = cSig;
        const CHEST_RES = ['wood','stone','fiber','crystal','essence','iron','diamond','starmetal'] as const;
        // − takes out of the chest, + puts in from the bag
        let cHtml = '<table class="chest-table"><tr><th>Resource</th><th>In chest</th><th>In bag</th><th></th></tr>';
        for (const r of CHEST_RES) {
          const inChest = chestSlots[r] || 0;
          const inBag = st.inv[r] || 0;
          cHtml += `<tr><td>${icon(r)} ${NAMES[r] || r}</td><td class="n">${inChest}</td><td class="n">${inBag}</td>` +
            `<td><div class="mv">` +
            `<button data-res="${r}" data-n="-10" title="Take 10" ${inChest === 0 ? 'disabled' : ''}>−10</button>` +
            `<button data-res="${r}" data-n="-1" title="Take 1" ${inChest === 0 ? 'disabled' : ''}>−</button>` +
            `<button data-res="${r}" data-n="1" title="Store 1" ${inBag === 0 ? 'disabled' : ''}>+</button>` +
            `<button data-res="${r}" data-n="10" title="Store 10" ${inBag === 0 ? 'disabled' : ''}>+10</button>` +
            `</div></td></tr>`;
        }
        cHtml += '</table>';
        $('chestPanel').querySelector('.chest-body')!.innerHTML = cHtml;
      }
    }

    // medic offer panel — rebuild is gated on offer id + affordability (never the countdown),
    // so the live "expires in Ns" text is patched into an existing node every frame instead
    // of tearing down the buttons (that pattern is what made the craft panel unclickable once).
    medicOffer = st.medicOffer;
    const medicEl = $('medicPanel');
    if (medicOffer) {
      medicEl.style.display = 'flex';
      const canPay = (st.inv[medicOffer.resource] || 0) >= medicOffer.amount;
      const msig = `${medicOffer.medicId}:${medicOffer.offerId}:${canPay}`;
      if (msig !== medicSig) {
        medicSig = msig;
        body('medicPanel').innerHTML =
          `<p class="medic-offer">Full healing for <b>${icon(medicOffer.resource)} ${medicOffer.amount} ${NAMES[medicOffer.resource] || medicOffer.resource}</b></p>` +
          `<p class="medic-time" id="medicCountdown"></p>` +
          `<div class="medic-acts"><button class="primary" data-medic-accept ${canPay ? '' : 'disabled'}>Accept [${keyLabel('medicAccept')}]</button>` +
          `<button data-medic-decline>Decline [${keyLabel('medicDecline')}]</button></div>`;
      }
      const secsLeft = Math.max(0, Math.ceil((medicOffer.expiresAt - Date.now()) / 1000));
      const cdEl = document.getElementById('medicCountdown');
      if (cdEl) cdEl.textContent = `Expires in ${secsLeft}s`;
    } else {
      medicEl.style.display = 'none';
      medicSig = '';
    }

    // craft panel — rebuild only when relevant state changes
    if (isOpen('craftPanel')) {
      const psig = JSON.stringify([st.inv, [...st.tools], [...st.gear], st.nearWorkbench, st.nearForge, st.nearCampfire]);
      if (psig !== panelSig) {
        panelSig = psig;
        // grouped by the station each recipe needs ('' = by hand), in recipe order; the
        // group's heading says whether you are standing near that station
        const near = (s: string) => s === 'workbench' ? st.nearWorkbench : s === 'forge' ? st.nearForge : st.nearCampfire;
        const groups = new Map<string, string>();
        for (const [key, r] of Object.entries(RECIPES) as [string, any][]) {
          if (r.tool && st.tools.has(key)) continue;
          if (r.gear && st.gear.has(key)) continue;
          const s = r.station || '';
          const ok = canAfford(st.inv, r.cost) && (!s || near(s));
          const cost = Object.entries(r.cost).map(([k, v]) => `${icon(k)}${v}`).join(' ');
          groups.set(s, (groups.get(s) || '') +
            `<div class="recipe"><button data-r="${key}" ${ok ? '' : 'disabled'}>${NAMES[key] || key}</button><span class="cost">${cost}</span></div>`);
        }
        let html = '';
        for (const [s, rows] of groups)
          html += `<div class="board-group">${s ? `${icon(s)} ${NAMES[s] || s}` : '✋ By hand'}` +
            (s ? `<span class="tag ${near(s) ? '' : 'off'}">${near(s) ? 'nearby' : 'not nearby'}</span>` : '') + `</div>${rows}`;
        body('craftPanel').innerHTML = html;
      }
    }

    // build panel — modular pieces, grouped by the slot they occupy. Costs are
    // paid in crafted materials, so affordability reads the same inventory.
    if (isOpen('buildPanel')) {
      const bsig = JSON.stringify([st.inv, selectedMod, st.zone]);
      if (bsig !== buildSig) {
        buildSig = bsig;
        let html = '';
        const order = ['floor', 'wall', 'fixture', 'decor'];
        const label: Record<string, string> = {
          floor: 'Floors', wall: 'Walls & openings', fixture: 'Fixtures', decor: 'Decor',
        };
        for (const group of order) {
          const kinds = Object.entries(MODULES as any)
            .filter(([, d]) => (d as any).slot === group);
          if (!kinds.length) continue;
          html += `<div class="board-group">${label[group]}</div>`;
          for (const [key, d] of kinds as [string, any][]) {
            const ok = canAfford(st.inv, d.cost);
            const cost = Object.entries(d.cost).map(([k, v]) => `${icon(k)}${v}`).join(' ');
            html += `<div class="recipe"><button data-mod="${key}" class="${selectedMod === key ? 'sel' : ''}" ${ok ? '' : 'disabled'}>` +
              `${NAMES[key] || key}</button><span class="cost">${cost}</span>` +
              (selectedMod === key ? '<span class="placing">placing</span>' : '') + '</div>';
          }
        }
        body('buildPanel').innerHTML = html;
      }
    }
    navSync();   // a panel redrawn above gets its keyboard choice back
  }, api);
}
