import { initMenu } from "./menu";
import { ATLAS_FILES, BOOT_PACKS, packBitmap } from "./atlasData";
import { LEGACY, prefetchJoin } from "./net";

// Keep the atlases in Cache Storage across visits (assets/sw.js). Only on a secure
// context (https or localhost); elsewhere the browser's HTTP cache is all we get.
if ("serviceWorker" in navigator && window.isSecureContext) {
  navigator.serviceWorker
    .register("/sw.js")
    .then(() => navigator.serviceWorker.ready)
    .then((reg) => reg.active?.postMessage({ t: "keep", files: ATLAS_FILES }))
    .catch(() => {});
  // ask the browser not to evict that cache under storage pressure (it may say no)
  navigator.storage?.persist?.().catch(() => {});
}

// While the player is still on the menu: decode the boot atlases (off the main
// thread) and fetch the game chunk, so Play has nothing left to wait for.
const idle = (fn: () => void) =>
  "requestIdleCallback" in window ? (window as any).requestIdleCallback(fn, { timeout: 1500 }) : setTimeout(fn, 200);
idle(() => {
  for (const p of BOOT_PACKS) packBitmap(p).catch(() => {}); // startGame reports a real failure
  import("./main").catch(() => {});
});

initMenu(({ name, room }) => {
  localStorage.setItem("hearth-name", name);
  if (room) localStorage.setItem("hearth-room", room);
  // the ticket round trip overlaps the rest of the boot (main.ts takeJoin); the scene
  // joins under the same name it will pick (a ?name= URL override wins)
  if (!LEGACY) prefetchJoin(new URLSearchParams(location.search).get("name") || name);
  import("./main").then((m) => m.startGame());
});
