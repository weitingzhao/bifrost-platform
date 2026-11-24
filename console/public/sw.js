/*
 * Bifrost Ops service worker — the home-screen app's offline shell.
 * Pages are network-first (a fresh deploy always wins online); the last good
 * index.html and its hashed assets answer when offline. /api is never cached.
 *
 * 2026-11-24 the Owner's phone showed a blank page on ops.bifrost.lan after a deploy. v1 fell back to the
 * cached old index.html on a network error but had already dropped the old hashed assets when it saw the new
 * index, so the old page asked for scripts that no longer existed; a 502 page was handed through as it came;
 * an asset fetch that threw had no answer at all. v2 binds a shell to its build:
 * - a shell is kept as "last good" only once its own /assets files answered 200 and are cached with it;
 * - each build's assets live in their own cache; the current and the previous build are kept;
 * - a page whose answer is not ok, or whose files do not all load, is not shown: the last good shell is,
 *   and when there is none, a page that says "Bifrost Ops failed to load" with a Reload.
 */
const PREFIX = 'bifrost-ops-'
const SHELL_CACHE = PREFIX + 'shell-v2'
const ASSET_PREFIX = PREFIX + 'assets-v2-'
const SHELL_KEY = '/'
const BUILD_HEADER = 'X-Bifrost-Build'

self.addEventListener('install', () => {
  self.skipWaiting()
})

self.addEventListener('activate', event => {
  event.waitUntil(
    (async () => {
      for (const key of await caches.keys()) {
        if (key.startsWith(PREFIX) && key !== SHELL_CACHE && !key.startsWith(ASSET_PREFIX)) await caches.delete(key)
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

/** The /assets files an index.html names; its first module script is its build. */
function shellAssets(html) {
  const entry = /<script[^>]*type="module"[^>]*src="(\/assets\/[^"]+)"/.exec(html)
  const all = [...html.matchAll(/(?:src|href)="(\/assets\/[^"]+)"/g)].map(m => m[1])
  return { build: entry ? entry[1] : null, assets: [...new Set(all)] }
}

const assetCacheName = build => ASSET_PREFIX + build.replace(/^\/assets\//, '')

/** Keep the new build's assets and the previous build's; drop older ones. */
async function pruneAssetCaches(current, previous) {
  const keep = new Set([current, previous].filter(Boolean).map(assetCacheName))
  for (const key of await caches.keys()) {
    if (key.startsWith(ASSET_PREFIX) && !keep.has(key)) await caches.delete(key)
  }
}

/** Fetch and cache every asset a shell names; true only when all answered 200. */
async function cacheShellAssets(build, assets) {
  const cache = await caches.open(assetCacheName(build))
  for (const path of assets) {
    if (await cache.match(path)) continue
    try {
      const response = await fetch(path, { cache: 'no-store' })
      if (!response.ok) return false
      await cache.put(path, response)
    } catch {
      return false
    }
  }
  return true
}

/** The last good shell, only while its entry script is still cached with it. */
async function lastGoodShell() {
  const cached = await (await caches.open(SHELL_CACHE)).match(SHELL_KEY)
  if (cached == null) return null
  const build = cached.headers.get(BUILD_HEADER)
  if (!build) return null
  const entry = await (await caches.open(assetCacheName(build))).match(build)
  return entry != null ? cached : null
}

function failedPage(reason) {
  const html =
    '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">' +
    '<title>Bifrost Ops</title>' +
    '<body style="margin:16px;font:14px/1.5 system-ui,sans-serif;background:#111720;color:#e6e9ef">' +
    '<p style="font-weight:600">Bifrost Ops failed to load ' +
    '<button type="button" onclick="location.reload()" style="font:inherit;margin-left:8px;padding:4px 12px">Reload</button></p>' +
    '<p style="opacity:.7">' + reason + '</p></body>'
  return new Response(html, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } })
}

async function networkFirstShell(request) {
  let response
  try {
    response = await fetch(request)
  } catch {
    return (await lastGoodShell()) ?? failedPage('No answer from the server and no earlier copy on this device.')
  }
  if (!response.ok) {
    return (await lastGoodShell()) ?? failedPage('The server answered ' + response.status + ' (often a deploy in progress).')
  }
  const body = await response.clone().text()
  const { build, assets } = shellAssets(body)
  if (!build) return response
  if (!(await cacheShellAssets(build, assets))) {
    // The new build's files are not all there yet (a rollout in progress): stay on the last good build.
    return (await lastGoodShell()) ?? response
  }
  const shell = await caches.open(SHELL_CACHE)
  const previous = (await shell.match(SHELL_KEY))?.headers.get(BUILD_HEADER) ?? null
  await shell.put(
    SHELL_KEY,
    new Response(body, { headers: { 'Content-Type': 'text/html; charset=utf-8', [BUILD_HEADER]: build } }),
  )
  await pruneAssetCaches(build, previous === build ? null : previous)
  return response
}

async function cacheFirstAsset(request) {
  const hit = await caches.match(request)
  if (hit != null) return hit
  let response
  try {
    response = await fetch(request)
  } catch {
    return new Response('', { status: 503, statusText: 'Bifrost Ops asset unreachable' })
  }
  if (response.ok) {
    const shell = await (await caches.open(SHELL_CACHE)).match(SHELL_KEY)
    const build = shell?.headers.get(BUILD_HEADER)
    if (build) await (await caches.open(assetCacheName(build))).put(request, response.clone())
  }
  return response
}
