import { useEffect, useState } from 'react'

/**
 * How the four entries are laid out (design system v2 §10.2): a bottom tab bar on a phone or a
 * tablet held upright, an icon rail on a tablet held sideways, the side nav on a desktop. A Fire
 * HD 10 held sideways is 1280 px wide, so the desktop starts at 1440, not 1280.
 */
export type ShellLayout = 'tabs' | 'rail' | 'side'

export const RAIL_QUERY = '(min-width: 1024px)'
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
