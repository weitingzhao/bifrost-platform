import { useRef } from 'react'
import { WAITING_SECTION } from '@/lib/shell/consoleRoutes'
import { useHashSection } from '@/lib/shell/useHashSection'
import { NeedsYouPage } from '@/pages/shell/needs-you/NeedsYouPage'

/** Everything waiting on the Owner, in full, below For you's cards (it was the Needs you page). */
export function OwnerWaiting() {
  const ref = useRef<HTMLElement>(null)
  useHashSection(WAITING_SECTION, ref)
  return <section id={WAITING_SECTION} ref={ref} aria-label="Everything waiting on you" className="flex scroll-mt-4 flex-col gap-3">
    <h3 className="m-0 text-base font-semibold">Everything waiting on you</h3>
    <NeedsYouPage />
  </section>
}
