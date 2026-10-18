// Wyldra asset cache (registered by src/boot.ts).
//
// Only /atlas/ files are handled. Their names carry a content hash
// (tools/atlas/build-atlases.mjs), so a cached copy can never be stale: serve it
// straight from Cache Storage, and fetch + store it the first time it is asked for.
// Everything else (HTML, JS, the menu's SVGs, the servers) goes to the network as usual.
//
// The page posts {t:'keep', files:[...]} with the current build's atlas list on
// every load; anything cached that is not on it belongs to an older build and is
// dropped, so the cache holds one build's atlases at most.

const CACHE = 'wyldra-atlas-v1';

self.addEventListener('install', () => self.skipWaiting());
self.addEventListener('activate', (e) => e.waitUntil(self.clients.claim()));

self.addEventListener('fetch', (e) => {
  const url = new URL(e.request.url);
  if (e.request.method !== 'GET' || url.origin !== location.origin || !url.pathname.startsWith('/atlas/')) return;
  e.respondWith(
    (async () => {
      // Cache Storage can be unavailable (private windows, blocked site data, quota,
      // a broken profile): the cache is an optimisation, so any failure falls back
      // to the network and the game still loads.
      let cache = null;
      try {
        cache = await caches.open(CACHE);
        const hit = await cache.match(url.pathname);
        if (hit) return hit;
      } catch { /* no cache: network only */ }
      const res = await fetch(e.request);
      if (cache && res.ok) cache.put(url.pathname, res.clone()).catch(() => {});
      return res;
    })(),
  );
});

self.addEventListener('message', (e) => {
  if (e.data?.t !== 'keep' || !Array.isArray(e.data.files)) return;
  const keep = new Set(e.data.files);
  e.waitUntil(
    caches.open(CACHE).then(async (cache) => {
      for (const req of await cache.keys()) {
        if (!keep.has(new URL(req.url).pathname)) await cache.delete(req);
      }
    }).catch(() => { /* no cache to prune */ }),
  );
});
