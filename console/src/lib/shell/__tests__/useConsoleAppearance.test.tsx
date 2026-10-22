import { act, cleanup, renderHook } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { APPEARANCE_KEY, STYLE_KEY, readAppearance, readVisualStyle, useConsoleAppearance } from '../useConsoleAppearance'

let values: Map<string, string>
let systemDark: boolean
let changed: Set<() => void>
beforeEach(() => {
  values = new Map(); systemDark = false; changed = new Set()
  vi.stubGlobal('localStorage', { getItem: (key: string) => values.get(key) ?? null, setItem: (key: string, value: string) => values.set(key, value) })
  vi.stubGlobal('matchMedia', () => ({ get matches() { return systemDark }, addEventListener: (_: string, fn: () => void) => changed.add(fn), removeEventListener: (_: string, fn: () => void) => changed.delete(fn) }))
})
afterEach(() => { cleanup(); vi.unstubAllGlobals(); document.documentElement.classList.remove('dark') })

describe('Console appearance preferences', () => {
  it('defaults to blue-violet and follows system changes; a manual choice takes precedence', () => {
    const { result } = renderHook(useConsoleAppearance)
    expect(result.current.style).toBe('gradient')
    expect(result.current.appearance).toBe('system')
    act(() => { systemDark = true; changed.forEach(fn => fn()) })
    expect(result.current.dark).toBe(true)
    act(() => result.current.chooseAppearance('light'))
    expect(result.current.dark).toBe(false)
    act(() => { systemDark = false; changed.forEach(fn => fn()); systemDark = true; changed.forEach(fn => fn()) })
    expect(result.current.dark).toBe(false)
    act(() => result.current.chooseAppearance('system'))
    expect(result.current.dark).toBe(true)
  })
  it('persists independent settings without changing the route or unrelated saved input', () => {
    window.history.replaceState(null, '', '#questions?id=q_test')
    values.set('draft', '尚未提交的目标')
    const { result, unmount } = renderHook(useConsoleAppearance)
    act(() => { result.current.chooseAppearance('dark'); result.current.chooseStyle('grayscale') })
    expect(values.get(APPEARANCE_KEY)).toBe('dark')
    expect(values.get(STYLE_KEY)).toBe('grayscale')
    expect(values.get('draft')).toBe('尚未提交的目标')
    expect(window.location.hash).toBe('#questions?id=q_test')
    unmount()
    const reloaded = renderHook(useConsoleAppearance)
    expect(reloaded.result.current.style).toBe('grayscale')
    expect(reloaded.result.current.dark).toBe(true)
  })
  it('synchronizes another tab and removal of saved preferences', () => {
    const { result } = renderHook(useConsoleAppearance)
    act(() => { values.set(STYLE_KEY, 'grayscale'); window.dispatchEvent(new StorageEvent('storage', { key: STYLE_KEY })) })
    expect(result.current.style).toBe('grayscale')
    act(() => { values.clear(); window.dispatchEvent(new StorageEvent('storage', { key: null })) })
    expect(result.current.style).toBe('gradient')
    expect(result.current.appearance).toBe('system')
  })
  it('ignores invalid saved values and remains usable when storage is blocked', () => {
    values.set(APPEARANCE_KEY, 'bogus'); values.set(STYLE_KEY, 'bogus')
    expect(readAppearance()).toBe('system'); expect(readVisualStyle()).toBe('gradient')
    vi.stubGlobal('localStorage', { getItem: () => { throw new Error('blocked') }, setItem: () => { throw new Error('blocked') } })
    const { result } = renderHook(useConsoleAppearance)
    act(() => result.current.chooseAppearance('dark'))
    expect(result.current.dark).toBe(true)
  })
})
