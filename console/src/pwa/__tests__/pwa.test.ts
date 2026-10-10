import { describe, expect, it, vi } from 'vitest'
import { syncAppBadge } from '@/pwa/appBadge'
import { registerServiceWorker } from '@/pwa/serviceWorker'

function badgingNavigator() {
  return {
    setAppBadge: vi.fn(async () => undefined),
    clearAppBadge: vi.fn(async () => undefined),
  }
}

describe('app badge', () => {
  it('sets the count, and clears on zero or unknown', () => {
    const nav = badgingNavigator()
    syncAppBadge(3, nav as unknown as Navigator)
    expect(nav.setAppBadge).toHaveBeenCalledWith(3)
    syncAppBadge(0, nav as unknown as Navigator)
    syncAppBadge(null, nav as unknown as Navigator)
    expect(nav.clearAppBadge).toHaveBeenCalledTimes(2)
  })

  it('does nothing without the Badging API', () => {
    expect(() => syncAppBadge(3, {} as Navigator)).not.toThrow()
  })
})

describe('service worker registration', () => {
  function fakeWindow(secure: boolean) {
    const register = vi.fn(async () => ({}))
    const listeners: Array<() => void> = []
    const win = {
      isSecureContext: secure,
      navigator: { serviceWorker: { register } },
      addEventListener: (_type: string, fn: () => void) => listeners.push(fn),
    }
    return { win: win as unknown as Window, register, listeners }
  }

  it('registers /sw.js on a production build in a secure context', () => {
    const { win, register, listeners } = fakeWindow(true)
    expect(registerServiceWorker({ prod: true }, win)).toBe(true)
    listeners.forEach(fn => fn())
    expect(register).toHaveBeenCalledWith('/sw.js', { scope: '/' })
  })

  it('skips dev builds and plain http', () => {
    expect(registerServiceWorker({ prod: false }, fakeWindow(true).win)).toBe(false)
    expect(registerServiceWorker({ prod: true }, fakeWindow(false).win)).toBe(false)
  })
})
