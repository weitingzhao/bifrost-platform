// 2026-11-23 箭10: the Owner's tablet keeps the Console open; after a deploy it kept the old bundle (UI / Console saw
// index-BtAdWEWW on PROD while the server served index-CMxX5eLY), so his view of our changes was simply old. The frame
// now says "New version · Reload" and an idle page reloads itself; a page in use does not, and the decision page keeps
// its own rule.
import { act, renderHook } from '@testing-library/react'
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
    const { rerender } = renderHook(() => useIdleReloadWhenStale({ reload, now: () => clock }))
    clock = 10 * 60_000
    drift.stale = true
    rerender()
    expect(reload).toHaveBeenCalledTimes(1)
    vi.doUnmock('@/pwa/useConsoleBuildDrift')
  })

  it('waits while someone touched the page a minute ago', async () => {
    vi.resetModules()
    const drift = { stale: false }
    vi.doMock('@/pwa/useConsoleBuildDrift', () => ({ useConsoleBuildDrift: () => drift.stale }))
    const { useIdleReloadWhenStale } = await import('@/lib/shell/newVersion')
    let clock = 0
    const reload = vi.fn()
    const { rerender } = renderHook(() => useIdleReloadWhenStale({ reload, now: () => clock }))
    clock = 9 * 60_000
    act(() => { window.dispatchEvent(new Event('keydown')) })
    clock = 10 * 60_000
    drift.stale = true
    rerender()
    expect(reload).not.toHaveBeenCalled()
    vi.doUnmock('@/pwa/useConsoleBuildDrift')
  })
})
