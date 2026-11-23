import { useEffect, useState } from 'react'

/**
 * How the four entries are laid out: a bottom tab bar on a phone, an icon rail on a tablet either
 * way up, the side nav on a desktop. A Fire HD 10 held sideways is 1280 px wide, so the desktop
 * starts at 1440, not 1280.
 *
 * 2026-11-23 (UI line, default, the Owner may overrule): the rail starts at 768, not 1024. Design
 * system v2 §10.2 gave an upright tablet the phone's tabs, so the Owner's Fire HD 10 upright (800
 * wide) looked like a phone. Phones (< 768) keep the tabs. Undo: set RAIL_QUERY back to 1024.
 * Content density and touch sizes below 1024 (ops*.css) are not changed by this.
 */
export type ShellLayout = 'tabs' | 'rail' | 'side'

export const RAIL_QUERY = '(min-width: 768px)'
export const SIDE_QUERY = '(min-width: 1440px)'

function matches(query: string): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia(query).matches
}

export function readShellLayout(): ShellLayout {
  if (matches(SIDE_QUERY)) return 'side'
  if (matches(RAIL_QUERY)) return 'rail'
  // No matchMedia (an old browser, a test without one): the desktop, as before this layout existed.
  if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return 'side'
  return 'tabs'
}

/** Read on the first render, so a phone never draws the side nav first. */
export function useShellLayout(): ShellLayout {
  const [layout, setLayout] = useState<ShellLayout>(readShellLayout)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const queries = [window.matchMedia(SIDE_QUERY), window.matchMedia(RAIL_QUERY)]
    const update = () => setLayout(readShellLayout())
    queries.forEach(q => q.addEventListener('change', update))
    update()
    return () => queries.forEach(q => q.removeEventListener('change', update))
  }, [])
  return layout
}
