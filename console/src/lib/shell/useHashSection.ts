import { useEffect, useState, type RefObject } from 'react'
import { hashQueryParam, resolveConsoleHash } from './consoleRoutes'

/**
 * The address's query as the shell will show it. On a first load of an old bookmark (`#progress`,
 * `#needs-you`) the page draws before the shell rewrites the address to `#work?section=goals`, and
 * the rewrite raises no hash change, so a page reads the address in its new form (review 425 B2).
 */
export function canonicalQuery(hash: string): string {
  return resolveConsoleHash(hash.replace(/^#/, '')).canonical.split('?')[1] ?? ''
}

/** `section=<id>` in the address names this section. */
export function hashNamesSection(hash: string, id: string): boolean {
  return hashQueryParam(canonicalQuery(hash), 'section') === id
}

/**
 * Scroll an element into view once the shell is done: the shell scrolls a newly opened place
 * to its top in its own effect (ConsolePage), which runs after the effects of the place it just
 * drew and would undo a scroll made there. An element the same hash change is still drawing is
 * looked for again for up to a second. Returns a cancel for an effect's cleanup.
 */
export function scrollAfterShell(target: () => Element | null | undefined, options: ScrollIntoViewOptions = { block: 'start' }): () => void {
  let tries = 0
  let timer = 0
  const attempt = () => {
    const element = target()
    if (element != null) {
      // Domain navigation wraps on phones; keep the destination below the whole sticky chrome.
      const chrome = document.querySelector('.console-shell-chrome')
      if (element instanceof HTMLElement && chrome) element.style.scrollMarginTop = `${chrome.getBoundingClientRect().height + 16}px`
      element.scrollIntoView?.(options)
    }
    else if (++tries < 20) timer = window.setTimeout(attempt, 50)
  }
  timer = window.setTimeout(attempt, 0)
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

/** Selected domain detail; unselected siblings stay mounted so their filters and drafts survive. */
export function useDomainSection(): string | null {
  const read = () => hashQueryParam(canonicalQuery(window.location.hash), 'section')
  const [section, setSection] = useState(read)
  useEffect(() => {
    const sync = () => setSection(read())
    sync(); window.addEventListener('hashchange', sync)
    return () => window.removeEventListener('hashchange', sync)
  }, [])
  return section
}
