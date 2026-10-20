import { useEffect, type RefObject } from 'react'
import { hashQueryParam } from './consoleRoutes'

/** `section=<id>` in the address names this section. */
export function hashNamesSection(hash: string, id: string): boolean {
  const query = hash.split('?')[1] ?? ''
  return hashQueryParam(query, 'section') === id
}

/**
 * Scroll an element into view once the shell is done: the shell scrolls a newly opened place
 * to its top in its own effect (ConsolePage), which runs after the effects of the place it just
 * drew and would undo a scroll made there. Returns a cancel for an effect's cleanup.
 */
export function scrollAfterShell(target: () => Element | null | undefined, options: ScrollIntoViewOptions = { block: 'start' }): () => void {
  const timer = window.setTimeout(() => target()?.scrollIntoView?.(options), 0)
  return () => window.clearTimeout(timer)
}

/**
 * Scroll a section of an entry into view when the address names it (`#you?section=waiting`), on
 * arrival and on every later hash change.
 */
export function useHashSection(id: string, ref: RefObject<HTMLElement | null>): void {
  useEffect(() => {
    let cancel = () => {}
    const go = () => {
      if (!hashNamesSection(window.location.hash, id)) return
      cancel()
      cancel = scrollAfterShell(() => ref.current)
    }
    go()
    window.addEventListener('hashchange', go)
    return () => {
      cancel()
      window.removeEventListener('hashchange', go)
    }
  }, [id, ref])
}
