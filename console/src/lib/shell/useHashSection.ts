import { useEffect, type RefObject } from 'react'
import { hashQueryParam } from './consoleRoutes'

/** `section=<id>` in the address names this section. */
export function hashNamesSection(hash: string, id: string): boolean {
  const query = hash.split('?')[1] ?? ''
  return hashQueryParam(query, 'section') === id
}

/**
 * Scroll a section of an entry into view when the address names it (`#you?section=waiting`), on
 * arrival and on every later hash change. The scroll waits a tick: the shell scrolls a newly
 * opened place to its top after the place has drawn (ConsolePage), and would undo it.
 */
export function useHashSection(id: string, ref: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    let timer: number | undefined
    const go = () => {
      if (!hashNamesSection(window.location.hash, id)) return
      window.clearTimeout(timer)
      timer = window.setTimeout(() => ref.current?.scrollIntoView?.({ block: 'start' }), 0)
    }
    go()
    window.addEventListener('hashchange', go)
    return () => {
      window.clearTimeout(timer)
      window.removeEventListener('hashchange', go)
    }
  }, [id, ref])
}
