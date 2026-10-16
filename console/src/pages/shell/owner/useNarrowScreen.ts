import { useEffect, useState } from 'react'

/** Under 768px: the sidebar's own phone breakpoint. */
export const NARROW_SCREEN_QUERY = '(max-width: 767px)'

function matches(): boolean {
  return typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia(NARROW_SCREEN_QUERY).matches
}

/**
 * True on a phone-width screen. Read on the first render, so a phone opens on its tab without
 * first drawing the desktop one. (`useIsMobile` left @bifrost/ui in 0.14.0, the version the
 * releases build against.)
 */
export function useNarrowScreen(): boolean {
  const [narrow, setNarrow] = useState(matches)
  useEffect(() => {
    if (typeof window.matchMedia !== 'function') return
    const media = window.matchMedia(NARROW_SCREEN_QUERY)
    const update = () => setNarrow(media.matches)
    media.addEventListener('change', update)
    update()
    return () => media.removeEventListener('change', update)
  }, [])
  return narrow
}
