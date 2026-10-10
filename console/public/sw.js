/*
 * Bifrost Ops service worker — the home-screen app's offline shell.
 * Pages are network-first (a fresh deploy always wins online); the last good
 * index.html and its hashed assets answer when offline. /api is never cached.
 */
const SHELL_CACHE = 'bifrost-ops-shell-v1'
const ASSET_CACHE = 'bifrost-ops-assets-v1'
const SHELL_KEY = '/'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', event => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key !== SHELL_CACHE && key !== ASSET_CACHE) await caches.delete(key)
      }
      await self.clients.claim()
    })(),
  )
})

self.addEventListener('fetch', event => {
  const request = event.request
  if (request.method !== 'GET') return
  const url = new URL(request.url)
  if (url.origin !== self.location.origin) return
  if (url.pathname.startsWith('/api/') || url.pathname === '/health') return
  if (request.mode === 'navigate') {
    event.respondWith(networkFirstShell(request))
    return
  }
  if (url.pathname.startsWith('/assets/')) event.respondWith(cacheFirstAsset(request))
})

async function networkFirstShell(request) {
  const cache = await caches.open(SHELL_CACHE)
  try {
    const response = await fetch(request)
    if (response.ok) {
      const body = await response.clone().text()
      const previous = await cache.match(SHELL_KEY)
      // A new index.html means a new deploy: drop the old hashed assets.
      if (previous != null && (await previous.text()) !== body) await caches.delete(ASSET_CACHE)
      await cache.put(
        SHELL_KEY,
        new Response(body, { headers: { 'Content-Type': 'text/html; charset=utf-8' } }),
      )
    }
    return response
  } catch {
    const cached = await cache.match(SHELL_KEY)
    if (cached != null) return cached
    return new Response('<!doctype html><title>Bifrost Ops</title><p>Bifrost Ops is offline.</p>', {
      status: 503,
      headers: { 'Content-Type': 'text/html; charset=utf-8' },
    })
  }
}

async function cacheFirstAsset(request) {
  const cache = await caches.open(ASSET_CACHE)
  const hit = await cache.match(request)
  if (hit != null) return hit
  const response = await fetch(request)
  if (response.ok) await cache.put(request, response.clone())
  return response
}
