import { useEffect, useState } from 'react'

/** True at phone width. The timeline and the dependency graph draw a narrower picture there instead of shrinking the wide one. */
export function useNarrow(): boolean {
  const query = '(max-width: 640px)'
  const read = () => typeof window !== 'undefined' && typeof window.matchMedia === 'function' && window.matchMedia(query).matches
  const [narrow, setNarrow] = useState(read)
  useEffect(() => {
    if (typeof window === 'undefined' || typeof window.matchMedia !== 'function') return
    const list = window.matchMedia(query)
    const on = () => setNarrow(list.matches)
    list.addEventListener('change', on)
    return () => list.removeEventListener('change', on)
  }, [])
  return narrow
}
