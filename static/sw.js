/* Yomi Reader — service worker (app shell + cover/page caches). */
/* Bump SHELL_CACHE when shell assets change in a breaking way. */
const SHELL_CACHE = 'yomi-shell-v2';
const COVER_CACHE = 'yomi-covers-v1';
const PAGE_CACHE = 'yomi-pages-v1';
const SHELL = [
  '/',
  '/index.html',
  '/style.css',
  '/app.js',
  '/i18n.js',
  '/plex-login.js',
  '/manifest.webmanifest',
  '/icons/icon-192.png',
  '/icons/icon-512.png',
  '/icons/icon-maskable-512.png',
  '/icons/apple-touch-icon.png'
];
const MAX_COVERS = 400;
const MAX_PAGES = 120;

self.addEventListener('install', (event) => {
  event.waitUntil(
    caches.open(SHELL_CACHE).then((cache) => cache.addAll(SHELL)).then(() => self.skipWaiting())
  );
});

self.addEventListener('activate', (event) => {
  const keep = new Set([SHELL_CACHE, COVER_CACHE, PAGE_CACHE]);
  event.waitUntil(
    caches.keys().then((keys) =>
      Promise.all(keys.filter((k) => !keep.has(k)).map((k) => caches.delete(k)))
    ).then(() => self.clients.claim())
  );
});

self.addEventListener('fetch', (event) => {
  const req = event.request;
  if (req.method !== 'GET') return;

  const url = new URL(req.url);
  if (url.origin !== self.location.origin) return;

  const path = url.pathname;

  // Never intercept auth mutations / API writes path via GET-only above;
  // keep live network for API JSON, OPDS, and health.
  if (
    path.startsWith('/api/') &&
    !path.startsWith('/api/covers/') &&
    !/\/api\/chapters\/[^/]+\/page\//.test(path)
  ) {
    return;
  }
  if (path.startsWith('/auth/') || path.startsWith('/opds') || path === '/healthz') {
    return;
  }
  if (path === '/sw.js') return;

  if (path.startsWith('/api/covers/')) {
    event.respondWith(cacheFirst(req, COVER_CACHE, MAX_COVERS));
    return;
  }
  if (/\/api\/chapters\/[^/]+\/page\//.test(path)) {
    event.respondWith(networkFirst(req, PAGE_CACHE, MAX_PAGES));
    return;
  }

  if (req.mode === 'navigate' || path === '/' || path === '/index.html') {
    event.respondWith(networkFirstNavigate(req));
    return;
  }

  // CSS/JS/icons/manifest: stale-while-revalidate
  event.respondWith(staleWhileRevalidate(req, SHELL_CACHE));
});

async function networkFirstNavigate(req) {
  try {
    const fresh = await fetch(req);
    const cache = await caches.open(SHELL_CACHE);
    cache.put('/index.html', fresh.clone());
    return fresh;
  } catch (_) {
    const cached = await caches.match('/index.html');
    if (cached) return cached;
    return new Response('Yomi is offline and has no cached shell yet.', {
      status: 503,
      headers: { 'Content-Type': 'text/plain; charset=utf-8' }
    });
  }
}

async function staleWhileRevalidate(req, cacheName) {
  const cache = await caches.open(cacheName);
  const cached = await cache.match(req);
  const fetching = fetch(req)
    .then((res) => {
      if (res && res.ok) cache.put(req, res.clone());
      return res;
    })
    .catch(() => null);
  if (cached) {
    fetching.catch(() => {});
    return cached;
  }
  const fresh = await fetching;
  if (fresh) return fresh;
  return Response.error();
}

async function cacheFirst(req, cacheName, maxEntries) {
  const cache = await caches.open(cacheName);
  const cached = await cache.match(req);
  if (cached) return cached;
  try {
    const fresh = await fetch(req);
    if (fresh && fresh.ok) {
      await cache.put(req, fresh.clone());
      trimCache(cache, maxEntries);
    }
    return fresh;
  } catch (_) {
    return Response.error();
  }
}

async function networkFirst(req, cacheName, maxEntries) {
  const cache = await caches.open(cacheName);
  try {
    const fresh = await fetch(req);
    if (fresh && fresh.ok) {
      await cache.put(req, fresh.clone());
      trimCache(cache, maxEntries);
    }
    return fresh;
  } catch (_) {
    const cached = await cache.match(req);
    if (cached) return cached;
    return Response.error();
  }
}

async function trimCache(cache, maxEntries) {
  const keys = await cache.keys();
  if (keys.length <= maxEntries) return;
  const drop = keys.length - maxEntries;
  for (let i = 0; i < drop; i++) {
    await cache.delete(keys[i]);
  }
}
