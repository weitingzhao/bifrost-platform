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
  it('tabs on an iPhone and a Fire HD 10 held upright, the rail held sideways, the side nav on a desktop', () => {
    const cases: [number, string][] = [[390, 'tabs'], [800, 'tabs'], [1023, 'tabs'], [1024, 'rail'], [1280, 'rail'], [1439, 'rail'], [1440, 'side'], [1920, 'side']]
    for (const [width, layout] of cases) {
      screenOf(width)
      expect(readShellLayout(), String(width)).toBe(layout)
    }
  })

  it('keeps the side nav where the browser cannot answer a media query', () => {
    window.matchMedia = undefined as unknown as typeof window.matchMedia
    expect(readShellLayout()).toBe('side')
  })

  it('follows the screen when a tablet turns', () => {
    const screen = screenOf(800)
    const { result } = renderHook(() => useShellLayout())
    expect(result.current).toBe('tabs')
    act(() => screen.resize(1280))
    expect(result.current).toBe('rail')
    act(() => screen.resize(800))
    expect(result.current).toBe('tabs')
  })
})
