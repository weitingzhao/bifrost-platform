/*
 * Bifrost Ops service worker — the home-screen app's offline shell.
 * Pages are network-first (a fresh deploy always wins online); the last good
 * index.html and its hashed assets answer when offline. /api is never cached.
 *
 * 事故 2026-11-24: a phone showed a blank page on ops.bifrost.lan after a deploy. v1 fell back to the
 * cached old index.html on a network error but had already dropped the old hashed assets when it saw the new
 * index, so the old page asked for scripts that no longer existed; a 502 page was handed through as it came;
 * an asset fetch that threw had no answer at all. v2 binds a shell to its build:
 * - a shell is kept as "last good" only once its own /assets files answered 200 and are cached with it;
 * - each build's assets live in their own cache; the current and the previous build are kept;
 * - a page whose answer is not ok, or whose files do not all load, is not shown: the last good shell is,
 *   and when there is none, a page that says "Bifrost Ops failed to load" with a Reload.
 * The same day the cause on the phone turned out to be the network (a Wi-Fi that skipped the VPN and was not on
 * 192.168.10.x): the page waited with no end. A page now waits 4 s, then the last good version answers with a
 * line that says so (the fresh one is still fetched for next time); with nothing cached it says it cannot reach
 * Bifrost Ops and which network the phone must be on, never white.
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
    event.respondWith(networkFirstShell(request, event))
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

/** How long a page waits for the server before the last good version answers. */
const SHELL_WAIT_MS = 4000
/** With nothing cached to show, how long it waits before saying it cannot reach the server. */
const UNREACHABLE_MS = 15000
const NETWORK_HINT = "Check the phone is on the home Wi-Fi (192.168.10.x) or the bifrost VPN (WireGuard) is connected."

/**
 * The last good shell with a line above the app that says so, outside #root (React owns that): the page works
 * from this device's copy, and its live reads may fail until the server answers.
 */
async function lastGoodWithNotice(why) {
  const shell = await lastGoodShell()
  if (shell == null) return null
  const html = await shell.text()
  const notice =
    '<div id="console-last-good" role="status" style="position:sticky;top:0;z-index:2147483647;display:flex;gap:8px;' +
    'align-items:center;flex-wrap:wrap;padding:6px 12px;font:12px/1.4 system-ui,sans-serif;background:#3a2f0b;color:#f5e6b3">' +
    '<b>Showing the last good version</b><span>' + why + '</span>' +
    '<button type="button" onclick="location.reload()" style="font:inherit;padding:2px 10px;margin-left:auto">Reload</button></div>'
  const marked = /<body[^>]*>/i.test(html) ? html.replace(/<body[^>]*>/i, m => m + notice) : html + notice
  return new Response(marked, {
    status: 200,
    headers: { 'Content-Type': 'text/html; charset=utf-8', [BUILD_HEADER]: shell.headers.get(BUILD_HEADER) ?? '' },
  })
}

function failedPage(title, reason, button) {
  const html =
    '<!doctype html><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">' +
    '<title>Bifrost Ops</title>' +
    '<body style="margin:16px;font:14px/1.5 system-ui,sans-serif;background:#111720;color:#e6e9ef">' +
    '<p style="font-weight:600">' + title + ' ' +
    '<button type="button" onclick="location.reload()" style="font:inherit;margin-left:8px;padding:4px 12px">' + button + '</button></p>' +
    '<p>' + reason + '</p></body>'
  return new Response(html, { status: 503, headers: { 'Content-Type': 'text/html; charset=utf-8' } })
}

const unreachablePage = () => failedPage("Can't reach Bifrost Ops", NETWORK_HINT, 'Retry')

const timer = ms => new Promise(resolve => setTimeout(() => resolve('timeout'), ms))

async function networkFirstShell(request, event) {
  // The network answer, settled: the response, or 'error' when it threw.
  const network = fetch(request).then(r => r, () => 'error')
  const quick = await Promise.race([network, timer(SHELL_WAIT_MS)])
  if (quick === 'timeout') {
    // Slow or no network: the last good version answers now; the fresh one is still fetched and kept for next time.
    if (event && event.waitUntil) event.waitUntil(network.then(r => (r !== 'error' && r.ok ? settleShell(r) : null)).catch(() => {}))
    const fallback = await lastGoodWithNotice('The server did not answer within ' + SHELL_WAIT_MS / 1000 + ' s. ' + NETWORK_HINT)
    if (fallback) return fallback
    const late = await Promise.race([network, timer(UNREACHABLE_MS - SHELL_WAIT_MS)])
    if (late === 'timeout' || late === 'error') return unreachablePage()
    return answer(late)
  }
  if (quick === 'error') {
    return (await lastGoodWithNotice('No answer from the server. ' + NETWORK_HINT)) ?? unreachablePage()
  }
  return answer(quick)
}

/** A network answer that came in: show it when it and its build's files are good; otherwise the last good version. */
async function answer(response) {
  if (!response.ok) {
    return (
      (await lastGoodWithNotice('The server answered ' + response.status + ' (often a deploy in progress).')) ??
      failedPage('Bifrost Ops failed to load', 'The server answered ' + response.status + ' (often a deploy in progress).', 'Reload')
    )
  }
  const settled = await settleShell(response.clone())
  if (settled === 'not-ready') {
    // The new build's files are not all there yet (a rollout in progress): stay on the last good build.
    return (await lastGoodWithNotice('The new version is still being deployed.')) ?? response
  }
  return response
}

/** Cache a good page and its build's files as the last good version; 'not-ready' when its files do not all load. */
async function settleShell(response) {
  const body = await response.text()
  const { build, assets } = shellAssets(body)
  if (!build) return 'no-build'
  if (!(await cacheShellAssets(build, assets))) return 'not-ready'
  const shell = await caches.open(SHELL_CACHE)
  const previous = (await shell.match(SHELL_KEY))?.headers.get(BUILD_HEADER) ?? null
  await shell.put(
    SHELL_KEY,
    new Response(body, { headers: { 'Content-Type': 'text/html; charset=utf-8', [BUILD_HEADER]: build } }),
  )
  await pruneAssetCaches(build, previous === build ? null : previous)
  return 'kept'
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
