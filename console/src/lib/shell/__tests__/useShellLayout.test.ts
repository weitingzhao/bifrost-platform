import { act, renderHook } from '@testing-library/react'
import { afterEach, describe, expect, it } from 'vitest'
import { RAIL_QUERY, SIDE_QUERY, readShellLayout, useShellLayout } from '@/lib/shell/useShellLayout'

type Listener = () => void
const original = window.matchMedia

/** A screen of a given width: each query answers by its min-width, and tells its listeners on resize. */
function screenOf(width: number) {
  const listeners = new Map<string, Set<Listener>>()
  const state = { width }
  window.matchMedia = ((query: string) => {
    const min = Number(/min-width:\s*(\d+)px/.exec(query)?.[1] ?? 0)
    return {
      get matches() {
        return state.width >= min
      },
      media: query,
      addEventListener: (_: string, fn: Listener) => {
        if (!listeners.has(query)) listeners.set(query, new Set())
        listeners.get(query)?.add(fn)
      },
      removeEventListener: (_: string, fn: Listener) => listeners.get(query)?.delete(fn),
    }
  }) as unknown as typeof window.matchMedia
  return {
    resize(next: number) {
      state.width = next
      for (const query of [SIDE_QUERY, RAIL_QUERY]) listeners.get(query)?.forEach(fn => fn())
    },
  }
}

afterEach(() => {
  window.matchMedia = original
})

describe('shell layout', () => {
  // Origin (2026-11-23): the Owner's Fire HD 10 upright (800 wide) drew the phone's tab bar; the
  // UI line moved the rail from 1024 down to 768 so a tablet either way up gets the rail.
  it('tabs on an iPhone, the rail on a Fire HD 10 either way up, the side nav on a desktop', () => {
    const cases: [number, string][] = [[390, 'tabs'], [767, 'tabs'], [768, 'rail'], [800, 'rail'], [1023, 'rail'], [1024, 'rail'], [1280, 'rail'], [1439, 'rail'], [1440, 'side'], [1920, 'side']]
    for (const [width, layout] of cases) {
      screenOf(width)
      expect(readShellLayout(), String(width)).toBe(layout)
    }
  })

  it('keeps the side nav where the browser cannot answer a media query', () => {
    window.matchMedia = undefined as unknown as typeof window.matchMedia
    expect(readShellLayout()).toBe('side')
  })

  it('follows the screen when it crosses a breakpoint', () => {
    const screen = screenOf(390)
    const { result } = renderHook(() => useShellLayout())
    expect(result.current).toBe('tabs')
    act(() => screen.resize(800))
    expect(result.current).toBe('rail')
    act(() => screen.resize(390))
    expect(result.current).toBe('tabs')
  })
})
