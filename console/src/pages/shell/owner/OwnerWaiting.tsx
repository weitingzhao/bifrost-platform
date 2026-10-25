import { useEffect, useRef } from 'react'
import { WAITING_SECTION, NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { hashNamesSection, scrollAfterShell, useHashSection } from '@/lib/shell/useHashSection'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'

/** Everything waiting on the Owner, in full, below For you's cards (it was the Needs you page). */
export function OwnerWaiting() {
  const ref = useRef<HTMLElement>(null)
  const detail = useRef<HTMLDetailsElement>(null)
  useEffect(() => {
    let cancel = () => {}
    const open = () => {
      if (detail.current) detail.current.open = true
      cancel()
      cancel = scrollAfterShell(() => ref.current)
    }
    const sync = () => { if (hashNamesSection(window.location.hash, WAITING_SECTION)) open() }
    // An already-current deep link must still reveal its destination.
    const follow = (event: MouseEvent) => {
      if (event.button !== 0 || event.ctrlKey || event.metaKey || event.altKey || event.shiftKey) return
      const link = event.target instanceof Element ? event.target.closest('a') : null
      if (link?.getAttribute('href') === NEEDS_YOU_HREF) open()
    }
    sync()
    window.addEventListener('hashchange', sync)
    document.addEventListener('click', follow)
    return () => { cancel(); window.removeEventListener('hashchange', sync); document.removeEventListener('click', follow) }
  }, [])
  useHashSection(WAITING_SECTION, ref)
  return <section id={WAITING_SECTION} ref={ref} aria-label="Everything waiting on you" className="flex scroll-mt-4 flex-col gap-3">
    <details ref={detail} className="ops-waiting-detail">
      <summary><span>Everything waiting on you</span><span className="text-sm text-muted-foreground">Detail</span></summary>
      <div className="ops-waiting-body"><NeedsYouPage /></div>
    </details>
  </section>
}
