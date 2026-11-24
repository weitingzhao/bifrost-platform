// 2026-11-24 the Owner's phone showed a blank page on ops.bifrost.lan after a deploy. sw.js v1 fell back to the
// cached old index.html on a network error after it had dropped the old hashed assets, handed a 502 page through,
// and had no answer when an asset fetch threw. These run the real public/sw.js against a fake network and cache.
// Control: against v1 sw.js five of the first six fail; only the offline-after-deploy case passed there too.
import fs from 'node:fs'
import path from 'node:path'
import { fileURLToPath } from 'node:url'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'

const source = fs.readFileSync(path.resolve(path.dirname(fileURLToPath(import.meta.url)), '../../../public/sw.js'), 'utf8')
const ORIGIN = 'https://ops.bifrost.lan'

class FakeCache {
  store = new Map<string, Response>()
  key = (r: RequestInfo | URL) => new URL(typeof r === 'string' ? r : r instanceof URL ? r.href : r.url, ORIGIN).pathname
  async match(r: RequestInfo | URL) { const hit = this.store.get(this.key(r)); return hit ? hit.clone() : undefined }
  async put(r: RequestInfo | URL, res: Response) { this.store.set(this.key(r), res.clone()) }
}
class FakeCaches {
  named = new Map<string, FakeCache>()
  async open(n: string) { if (!this.named.has(n)) this.named.set(n, new FakeCache()); return this.named.get(n)! }
  async keys() { return [...this.named.keys()] }
  async delete(n: string) { return this.named.delete(n) }
  async match(r: RequestInfo | URL) { for (const c of this.named.values()) { const h = await c.match(r); if (h) return h } return undefined }
}

const html = (build: string) => `<!doctype html><script type="module" crossorigin src="/assets/index-${build}.js"></script><link rel="stylesheet" href="/assets/index-${build}.css">`

/** The server: which build index.html names, which files exist, and whether the network is up. */
let server: { build: string; files: Map<string, number>; down: boolean; hang: boolean; indexStatus: number }
let caches: FakeCaches
let onFetch: (e: { request: Request; respondWith: (p: Promise<Response>) => void }) => void

function deploy(build: string, keepOld = false) {
  if (!keepOld) server.files.clear()
  server.build = build
  server.files.set(`/assets/index-${build}.js`, 200)
  server.files.set(`/assets/index-${build}.css`, 200)
}

const net = async (r: RequestInfo | URL) => {
  if (server.down) throw new TypeError('net::ERR_HTTP2_PROTOCOL_ERROR')
  if (server.hang) return new Promise<Response>(() => {})
  const p = new URL(typeof r === 'string' ? r : r instanceof URL ? r.href : r.url, ORIGIN).pathname
  if (p === '/' || p === '/index.html') return new Response(server.indexStatus === 200 ? html(server.build) : 'Bad Gateway', { status: server.indexStatus })
  const s = server.files.get(p)
  return new Response(s === 200 ? `// ${p}` : 'Not Found', { status: s ?? 404 })
}

function request(p: string, mode: RequestMode = 'no-cors') {
  const r = new Request(ORIGIN + p)
  Object.defineProperty(r, 'mode', { value: mode })
  let out: Promise<Response> | undefined
  onFetch({ request: r, respondWith: x => { out = x } })
  if (!out) throw new Error('not handled')
  return out
}
const open = () => request('/', 'navigate')
const pageAssets = async () => Promise.all([request(`/assets/index-${server.build}.js`)])

beforeEach(() => {
  server = { build: '', files: new Map(), down: false, hang: false, indexStatus: 200 }
  caches = new FakeCaches()
  const listeners: Record<string, (e: never) => void> = {}
  const self = { location: { origin: ORIGIN }, addEventListener: (t: string, f: (e: never) => void) => { listeners[t] = f }, skipWaiting() {}, clients: { claim: async () => {} } }
  new Function('self', 'caches', 'fetch', source)(self, caches, net)
  onFetch = listeners.fetch as never
  deploy('OLD')
})

const shellBuild = async () => (await (await caches.open('bifrost-ops-shell-v2')).match('/'))?.headers.get('X-Bifrost-Build')

describe('sw.js: a shell is bound to its build', () => {
  it('keeps the old build and its files when the network breaks after a deploy', async () => {
    await open()
    await pageAssets()
    deploy('NEW')
    server.down = true
    const page = await open()
    expect(page.status).toBe(200)
    expect(await page.text()).toContain('index-OLD.js')
    // The old page's script still answers from this device though the server no longer has it.
    expect((await request('/assets/index-OLD.js')).status).toBe(200)
  })

  it('does not show a 502 page: the last good shell answers, or a page that says it failed with a Reload', async () => {
    server.indexStatus = 502
    const first = await open()
    expect(first.status).toBe(503)
    expect(await first.text()).toMatch(/Bifrost Ops failed to load[\s\S]*Reload/)
    server.indexStatus = 200
    await open()
    server.indexStatus = 502
    expect(await (await open()).text()).toContain('index-OLD.js')
  })

  it('stays on the last good build while the new index names files the server cannot serve yet (mid-rollout)', async () => {
    await open()
    server.build = 'NEW' // index.html already says NEW; its script answers 502 on this replica
    server.files.set('/assets/index-NEW.js', 502)
    expect(await (await open()).text()).toContain('index-OLD.js')
    expect(await shellBuild()).toBe('/assets/index-OLD.js')
    server.files.set('/assets/index-NEW.js', 200)
    server.files.set('/assets/index-NEW.css', 200)
    expect(await (await open()).text()).toContain('index-NEW.js')
    expect(await shellBuild()).toBe('/assets/index-NEW.js')
  })

  it('keeps the current and the previous build, drops older ones', async () => {
    await open()
    deploy('B'); await open()
    deploy('C'); await open()
    const keys = await caches.keys()
    expect(keys.filter(k => k.startsWith('bifrost-ops-assets-v2-')).sort()).toEqual(['bifrost-ops-assets-v2-index-B.js', 'bifrost-ops-assets-v2-index-C.js'])
  })

  it('answers an asset fetch that throws with a 503 instead of no answer', async () => {
    await open()
    server.down = true
    expect((await request('/assets/chunk-never-seen.js')).status).toBe(503)
  })

  it('a cached shell whose script is gone is not served (no white page on first install offline)', async () => {
    await open()
    for (const k of await caches.keys()) if (k.startsWith('bifrost-ops-assets-v2-')) await caches.delete(k)
    server.down = true
    expect(await (await open()).text()).toContain("Can't reach Bifrost Ops")
  })

  it('says it is the last good version when it answers instead of the server', async () => {
    await open()
    server.down = true
    const text = await (await open()).text()
    expect(text).toContain('Showing the last good version')
    expect(text).toContain('Reload')
    // The line sits outside #root, which React owns.
    expect(text.indexOf('console-last-good')).toBeGreaterThan(-1)
  })
})

// 2026-11-24 the Owner's phone was on a Wi-Fi that skipped the VPN and was not on 192.168.10.x: the page request
// never answered and the screen stayed white with the progress bar stuck. A page waits 4 s, then the last good
// version answers and says so; with nothing cached it says it cannot reach Bifrost Ops and which network to use.
describe('sw.js: a page request that does not answer', () => {
  afterEach(() => vi.useRealTimers())

  it('answers with the last good version after 4 s, saying so and naming the network to check', async () => {
    await open()
    vi.useFakeTimers()
    server.hang = true
    const page = open()
    await vi.advanceTimersByTimeAsync(4_000)
    const text = await (await page).text()
    expect(text).toContain('index-OLD.js')
    expect(text).toContain('Showing the last good version')
    expect(text).toMatch(/home Wi-Fi[\s\S]*VPN/)
  })

  it('with nothing cached, waits longer and then says it cannot reach Bifrost Ops, with Retry, never white', async () => {
    vi.useFakeTimers()
    server.hang = true
    let settled = false
    const page = open().then(r => { settled = true; return r })
    await vi.advanceTimersByTimeAsync(4_000)
    expect(settled).toBe(false)
    await vi.advanceTimersByTimeAsync(11_000)
    const r = await page
    expect(r.status).toBe(503)
    const text = await r.text()
    expect(text).toContain("Can't reach Bifrost Ops")
    expect(text).toMatch(/home Wi-Fi \(192\.168\.10\.x\)[\s\S]*VPN/)
    expect(text).toContain('Retry')
  })

  it('answers at once when the server is quick', async () => {
    vi.useFakeTimers()
    const page = open()
    await vi.advanceTimersByTimeAsync(0)
    expect(await (await page).text()).not.toContain('Showing the last good version')
  })
})
