import { useEffect, useRef } from 'react'
import { COORDINATOR_WINDOW } from '@/api/agentRuntimeBus'
import { canonicalQuery, hashNamesSection, scrollAfterShell } from '@/lib/shell/useHashSection'
import { NeedsCleanupSection } from '../agents/NeedsCleanup'
import { WorkPlanView } from '../agents/WorkPlanView'
import { WORK_SECTION_TITLES, useWorkView, type WorkSection } from '../agents/workViewModel'
import { DeliveryBoundaries } from '../agents/DeliveryBoundaries'
import { ProgressPage } from '../progress/ProgressPage'
import { Card } from './OwnerParts'
import { OwnerReleaseLane } from './OwnerReleaseLane'
import { ago } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

/** The coordinating thread's latest note to the Owner, from the mailbox (2.5 G3). */
function CoordinatorNote({ view, now }: { view: OwnerConsoleData['coordinator']; now: number }) {
  if (view.state === 'loading') return <span className="text-sm text-muted-foreground">Reading the mailbox…</span>
  if (view.state === 'error') return <span role="alert" className="break-words text-sm text-muted-foreground">Coordinator note unavailable: {view.error}</span>
  if (view.note == null) return <span className="text-sm text-muted-foreground">{view.full ? `No note among the latest ${COORDINATOR_WINDOW} messages with the coordinator.` : 'No note from the coordinator.'}</span>
  return <>
    <span className="whitespace-pre-line break-words">{view.note.body}</span>
    <span className="text-xs text-muted-foreground" title={view.note.at}>Note {ago(view.note.at, now)}</span>
  </>
}

/**
 * Work (R5:5.5-2, Design v3.6 WorkPage): the round's summary and its tabs (WorkPlanView); or, a step away, one of the
 * pages that were further down the same page before: Goals (the goal cards), Activity (the runtime as a whole: what
 * needs cleanup (R5:5.5-4: offline hosts and registered work with no heartbeat, moved off Work's top), Now, open goals,
 * crew, quota, the release lane, threads in progress), Code health and Commit lineage. Each is drawn, and read, only
 * while the address names it (`#work?section=…`, `#work?goal=…`).
 */
export function OwnerWork({ data }: { data: OwnerConsoleData }) {
  const view = useWorkView()
  const key = view.kind === 'tab' ? 'tabs' : view.section
  const last = useRef(key)
  // Back on the round from a page a step away starts at Work's top; an address naming the plan (`section=plan`, no
  // module) brings the plan into view, on arrival and on a later hash change, as it did when it was further down.
  useEffect(() => {
    const from = last.current
    last.current = key
    if (key !== 'tabs') return
    let cancel = () => {}
    const toPlan = () => {
      if (!hashNamesSection(window.location.hash, 'plan') || new URLSearchParams(canonicalQuery(window.location.hash)).has('node')) return
      cancel(); cancel = scrollAfterShell(() => document.getElementById('plan'))
    }
    if (from !== 'tabs') document.scrollingElement?.scrollTo?.({ top: 0 })
    toPlan()
    window.addEventListener('hashchange', toPlan)
    return () => { cancel(); window.removeEventListener('hashchange', toPlan) }
  }, [key])
  if (view.kind === 'tab') return <section aria-label="Work" className="flex min-w-0 flex-col gap-3">
    <h2 className="sr-only">Work</h2>
    <WorkPlanView tab={view.tab} />
  </section>
  return <WorkSectionPage section={view.section} data={data} />
}

function WorkSectionPage({ section, data }: { section: WorkSection; data: OwnerConsoleData }) {
  // A page a step away starts at its top.
  useEffect(() => {
    return scrollAfterShell(() => document.getElementById(`work-${section}`))
  }, [section])
  return <section id={`work-${section}`} aria-label="Work" className="flex min-w-0 scroll-mt-4 flex-col gap-4">
    <h2 className="sr-only">Work</h2>
    {/* Not "Breadcrumb": the shell's header has a landmark of that name on the same page. */}
    <nav aria-label="Work page" className="flex min-w-0 flex-wrap items-center gap-2 text-sm">
      <a href="#work" aria-label="Back to Work" className="inline-flex min-h-11 items-center font-semibold text-[var(--primary)] no-underline">‹ Work</a>
      <span aria-hidden className="text-muted-foreground">›</span>
      <span aria-current="page" className="font-semibold">{WORK_SECTION_TITLES[section]}</span>
    </nav>
    {section === 'activity' ? <>
      <NeedsCleanupSection />
      <OwnerRuntimeCards data={data} />
      <OwnerReleaseLane data={data} />
      <ProgressPage part="activity" />
    </> : section === 'delivery-boundaries' ? <DeliveryBoundaries /> : <ProgressPage part={section} />}
  </section>
}

/** Work → Activity: the coordinator's latest note, from its mailbox. */
export function OwnerRuntimeCards({ data }: { data: OwnerConsoleData }) {
  return <div className="flex min-w-0 flex-col gap-3">
    <Card label="Coordinator" title="Coordinator">
      <CoordinatorNote view={data.coordinator} now={data.now} />
    </Card>
  </div>
}
