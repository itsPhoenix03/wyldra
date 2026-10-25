// The key-binding editor. One component, mounted twice: on the title screen's Controls
// page (menu.ts) and in the in-game Controls window (ui.ts). Both re-render from the
// "hearth:keybinds" event, so a change made in either shows in the other.
import {
  ACTIONS, RESERVED, getKeys, setKey, resetKeys, codeLabel, captureNextKey, cancelCapture,
  type Action,
} from "./keybinds.ts";

const esc = (s: string) =>
  s.replace(/[&<>"]/g, (c) => ({ "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;" })[c]!);
const labelOf = (a: Action) => ACTIONS.find((d) => d.id === a)!.label;

export function mountControls(root: HTMLElement): void {
  let listening: { a: Action; slot: 0 | 1 } | null = null;
  let status = "";

  const render = () => {
    // keep keyboard focus on the same key across the rebuild
    const f = document.activeElement as HTMLElement | null;
    const refocus = f && root.contains(f) ? f.dataset.action ? `[data-action="${f.dataset.action}"][data-slot="${f.dataset.slot}"]` : ".kb-reset" : null;
    let html = '<div class="kb-list">';
    let group = "";
    for (const a of ACTIONS) {
      if (a.group !== group) html += `<div class="kb-group">${(group = a.group)}</div>`;
      html += `<span class="kb-name">${a.label}</span>`;
      getKeys(a.id).forEach((c, i) => {
        const on = listening?.a === a.id && listening.slot === i;
        html += `<button type="button" class="kb-key${on ? " listening" : c ? "" : " empty"}" data-action="${a.id}" data-slot="${i}">` +
          `${on ? "Press a key…" : c ? esc(codeLabel(c)) : "—"}</button>`;
      });
    }
    html += `</div><p class="kb-status" aria-live="polite">${esc(status)}</p>` +
      `<div class="kb-foot"><span class="kb-note">Choose a key, then press the new one. Esc cancels, Backspace clears it. ` +
      `Esc (close / cancel), F9 and F10 are fixed.</span>` +
      `<button type="button" class="kb-key kb-reset">Reset all</button></div>`;
    root.innerHTML = html;
    if (refocus) root.querySelector<HTMLElement>(refocus)?.focus();
  };

  const stopListening = (msg: string) => {
    if (!listening) return;
    listening = null;
    cancelCapture();
    status = msg;
    render();
  };

  root.addEventListener("click", (e) => {
    const b = (e.target as HTMLElement).closest("button");
    if (!b || !root.contains(b)) return;
    if (b.classList.contains("kb-reset")) {
      stopListening("");
      status = "Every key is back to its default.";
      resetKeys();   // re-renders through the event
      return;
    }
    const a = b.dataset.action as Action;
    const slot = Number(b.dataset.slot) as 0 | 1;
    if (listening?.a === a && listening.slot === slot) return stopListening("Unchanged.");
    const mine = { a, slot };
    listening = mine;
    status = `Press a key for “${labelOf(a)}”.`;
    render();
    captureNextKey((code) => {
      if (listening !== mine) return;   // superseded by another key's prompt
      listening = null;
      const name = labelOf(a);
      if (code === null) status = "Unchanged.";
      else if (code === "") {
        setKey(a, slot, null);
        status = `${name}: key cleared.`;
      } else if (RESERVED.has(code)) status = `${codeLabel(code)} is reserved — pick another key.`;
      else {
        const freed = setKey(a, slot, code);
        status = `${name}: ${codeLabel(code)}` + (freed.length ? ` (taken from ${freed.join(", ")}, now unbound there).` : ".");
      }
      render();
    });
  });
  // a click anywhere else drops the prompt rather than leaving it waiting
  window.addEventListener("pointerdown", (e) => {
    if (listening && !root.contains(e.target as Node)) stopListening("");
  });
  window.addEventListener("hearth:keybinds", render);
  render();
}
