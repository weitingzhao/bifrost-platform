import { moduleRowLinkId } from './planModuleModel'

/**
 * Where a module's row was on the screen when it was opened (R5:5.5-1): leaving the module's detail (‹ Work, the
 * round in its breadcrumb, or the browser's Back) scrolls the page so that the row is there again. One entry: only
 * the last module opened from its row is returned to this way.
 */
let opened: { id: string; top: number } | null = null

/** Note the row's place on the screen as its module is opened. */
export function rememberModuleRow(id: string) {
  const link = document.getElementById(moduleRowLinkId(id))
  opened = link ? { id, top: link.getBoundingClientRect().top } : null
}

/**
 * Back on the rows, once the shell has drawn them: the keyboard on the module's own link, and the row where it was on
 * the screen; a module reached by its address (nothing noted) has its row brought to the middle. Returns a cancel.
 */
export function returnToModuleRow(id: string): () => void {
  const top = opened?.id === id ? opened.top : null
  opened = null
  let tries = 0
  let timer = 0
  const attempt = () => {
    const link = document.getElementById(moduleRowLinkId(id))
    if (link == null) {
      if (++tries < 20) timer = window.setTimeout(attempt, 50)
      return
    }
    link.focus({ preventScroll: true })
    if (top === null) link.scrollIntoView?.({ block: 'center' })
    else window.scrollBy({ top: link.getBoundingClientRect().top - top, behavior: 'instant' })
  }
  timer = window.setTimeout(attempt, 0)
  return () => window.clearTimeout(timer)
}
