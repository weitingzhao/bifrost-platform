import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it, vi } from 'vitest'
import { NARROW_SCREEN_QUERY, useNarrowScreen } from '../useNarrowScreen'

function media(initial: boolean) {
  const listeners = new Set<() => void>()
  const list = { matches: initial, addEventListener: (_: string, f: () => void) => listeners.add(f), removeEventListener: (_: string, f: () => void) => listeners.delete(f) }
  vi.stubGlobal('matchMedia', vi.fn((q: string) => (q === NARROW_SCREEN_QUERY ? list : { ...list, matches: false })))
  return { set: (v: boolean) => { list.matches = v; listeners.forEach(f => f()) } }
}

afterEach(() => vi.unstubAllGlobals())

describe('useNarrowScreen', () => {
  it('is narrow on the first render on a phone, and follows a rotation', () => {
    const m = media(true)
    const { result } = renderHook(() => useNarrowScreen())
    expect(result.current).toBe(true)
    act(() => m.set(false))
    expect(result.current).toBe(false)
  })
  it('is wide where the browser has no matchMedia', () => {
    vi.stubGlobal('matchMedia', undefined)
    expect(renderHook(() => useNarrowScreen()).result.current).toBe(false)
  })
})
