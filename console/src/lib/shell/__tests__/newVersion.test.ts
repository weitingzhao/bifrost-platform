// 事故 2026-11-23 (箭10): a tablet keeps the Console open; after a deploy it kept the old bundle (UI / Console saw
// index-BtAdWEWW on PROD while the server served index-CMxX5eLY), so the view of our changes was simply old. The frame
// now says "New version · Reload" and an idle page reloads itself; a page in use does not, and the decision page keeps
// its own rule.
import { act, renderHook, waitFor } from '@testing-library/react'
import { describe, expect, it, vi } from 'vitest'
import { approvalHref } from '@/lib/shell/consoleRoutes'
import { onDecisionPage, shouldAutoReload } from '@/lib/shell/newVersion'

describe('new version', () => {
  it('reloads on its own only after five idle minutes with a newer build', () => {
    expect(shouldAutoReload(true, 0, 5 * 60_000)).toBe(true)
    expect(shouldAutoReload(true, 0, 4 * 60_000)).toBe(false) // touched four minutes ago: someone is using it
    expect(shouldAutoReload(false, 0, 60 * 60_000)).toBe(false) // no newer build: never
  })
  it('leaves the decision page to its own banner', () => {
    expect(onDecisionPage(approvalHref('r1'))).toBe(true)
    expect(onDecisionPage('#status')).toBe(false)
  })
})

// 2026-11-23 箭10, after the first PROD check: the idle clock started when the newer build was found, so a page nobody
// had touched since 02:09 still waited five more minutes. Idle counts from the last input since the page opened.
describe('useIdleReloadWhenStale', () => {
  it('reloads at once a page idle for ten minutes when a newer build is found', async () => {
    vi.resetModules()
    const drift = { stale: false }
    vi.doMock('@/pwa/useConsoleBuildDrift', () => ({ useConsoleBuildDrift: () => drift.stale }))
    const { useIdleReloadWhenStale } = await import('@/lib/shell/newVersion')
    let clock = 0
    const reload = vi.fn()
    const { rerender } = renderHook(() => useIdleReloadWhenStale({ reload, now: () => clock, ready: async () => true }))
    clock = 10 * 60_000
    drift.stale = true
    rerender()
    await waitFor(() => expect(reload).toHaveBeenCalledTimes(1))
    vi.doUnmock('@/pwa/useConsoleBuildDrift')
  })

  it('waits while someone touched the page a minute ago', async () => {
    vi.resetModules()
    const drift = { stale: false }
    vi.doMock('@/pwa/useConsoleBuildDrift', () => ({ useConsoleBuildDrift: () => drift.stale }))
    const { useIdleReloadWhenStale } = await import('@/lib/shell/newVersion')
    let clock = 0
    const reload = vi.fn()
    const { rerender } = renderHook(() => useIdleReloadWhenStale({ reload, now: () => clock, ready: async () => true }))
    clock = 9 * 60_000
    act(() => { window.dispatchEvent(new Event('keydown')) })
    clock = 10 * 60_000
    drift.stale = true
    rerender()
    expect(reload).not.toHaveBeenCalled()
    vi.doUnmock('@/pwa/useConsoleBuildDrift')
  })
})

// 事故 2026-11-24: a phone could not open ops.bifrost.lan after a deploy: while a rollout splits requests between
// old and new replicas, the new page's script can 502 and a reload lands on a white page. A reload waits until the
// newer build's script answers 200; otherwise the working page stays.
describe('reload only when the newer build loads', () => {
  it('does not reload an idle page while the newer build cannot load', async () => {
    vi.resetModules()
    const drift = { stale: false }
    vi.doMock('@/pwa/useConsoleBuildDrift', () => ({ useConsoleBuildDrift: () => drift.stale }))
    const { useIdleReloadWhenStale } = await import('@/lib/shell/newVersion')
    let clock = 0
    const reload = vi.fn()
    const ready = vi.fn(async () => false)
    const { rerender } = renderHook(() => useIdleReloadWhenStale({ reload, now: () => clock, ready }))
    clock = 10 * 60_000
    drift.stale = true
    rerender()
    await waitFor(() => expect(ready).toHaveBeenCalled())
    expect(reload).not.toHaveBeenCalled()
    vi.doUnmock('@/pwa/useConsoleBuildDrift')
  })
  it('reads the newer index and its script: 502 is not ready, 200 and 200 is', async () => {
    const { liveBuildReady } = await import('@/lib/shell/newVersion')
    const index = '<script type="module" crossorigin src="/assets/index-NEW.js"></script>'
    const ok = (body = '') => new Response(body, { status: 200 })
    const fetcher = (script: Response) => (async (u: RequestInfo | URL) => String(u) === '/index.html' ? ok(index) : script) as typeof fetch
    expect(await liveBuildReady(fetcher(new Response('', { status: 502 })))).toBe(false)
    expect(await liveBuildReady(fetcher(ok()))).toBe(true)
    expect(await liveBuildReady((async () => { throw new Error('offline') }) as typeof fetch)).toBe(false)
  })
})
