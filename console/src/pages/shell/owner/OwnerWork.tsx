import { useEffect, useRef } from 'react'
import { COORDINATOR_WINDOW } from '@/api/agentRuntimeBus'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { canonicalQuery, hashNamesSection, scrollAfterShell } from '@/lib/shell/useHashSection'
import { NeedsCleanupSection } from '../agents/NeedsCleanup'
import { WorkPlanView } from '../agents/WorkPlanView'
import { WORK_SECTION_TITLES, useWorkView, type WorkSection } from '../agents/workViewModel'
import { ProgressPage } from '../progress/ProgressPage'
import { Card, Stat } from './OwnerParts'
import { OwnerReleaseLane } from './OwnerReleaseLane'
import { OwnerCrew } from './OwnerCrew'
import { OwnerGoals } from './OwnerGoals'
import { ago, runtimeUnreadLine } from './ownerModel'
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
  // A page a step away starts at its top; a goal the address names is brought into view by the goal cards instead.
  useEffect(() => {
    if (section === 'goals' && /[?&]goal=/.test(window.location.hash)) return
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
      <ProgressPage planView part="activity" />
    </> : <ProgressPage planView part={section} />}
  </section>
}

/** Work → Activity: the runtime as a whole, read from GET /progress (Now, open goals, crew) and the coordinator's mailbox. */
export function OwnerRuntimeCards({ data }: { data: OwnerConsoleData }) {
  const { work, now } = data
  return <div className="flex min-w-0 flex-col gap-3">
    <div className="grid gap-4 @3xl:grid-cols-2 @6xl:grid-cols-3">
      <Card label="Now" title="Now" more={{ href: goalHref(), text: 'Goals' }}>
        <div className="grid grid-cols-3 gap-2">
          <Stat value={work?.running ?? '?'} label="running" tone={work && work.running > 0 ? 'info' : undefined} />
          <Stat value={work?.stuck ?? '?'} label="stuck" tone={work && work.stuck > 0 ? 'fail' : undefined} />
          <Stat value={work?.open ?? '?'} label="open goals" />
        </div>
        {/* T24: an attempt on a host that stopped polling, or maybe stuck, is not counted as running; it may still run
            (review 471 B1), so it is "not confirmed", by the runtime's own words; Crew says which. */}
        {work && work.stalled > 0 && <p className="m-0 text-xs font-semibold text-[var(--color-warning)]" data-stalled>Not confirmed running: {work.stalledWords} (Crew)</p>}
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Last result</span>
          {data.last
            ? <><div className="flex flex-wrap justify-between gap-2"><span className="min-w-0 break-words"><strong>{data.last.mission}</strong> {data.lastGoal.split('\n')[0]}</span>
                <span className={`font-semibold ${data.last.status === 'done' ? 'text-[var(--color-success)]' : 'text-muted-foreground'}`} title={data.last.at}>
                  {data.last.status === 'done' ? `Done ${ago(data.last.at, now)}` : `Implementation accepted ${ago(data.last.at, now)}${data.last.status === 'open' ? `; goal ${data.last.state}` : ''}`}</span></div>
              <a href={goalHref(data.last.mission)} className="inline-flex min-h-11 items-center text-sm">Step by step</a></>
            : <span className="text-sm text-muted-foreground">{data.runtimeError ? runtimeUnreadLine(data.runtimeReadAt, data.runtimeOpenedAt, data.now) : work ? 'No implementation accepted recently.' : 'Reading the agent runtime…'}</span>}
        </div>
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Coordinator</span>
          <CoordinatorNote view={data.coordinator} now={now} />
        </div>
      </Card>
      <OwnerGoals missions={data.missions ?? []} now={now} read={data.controlsReadable ? 'ok' : data.runtimeError ? { error: runtimeUnreadLine(data.runtimeReadAt, data.runtimeOpenedAt, now) } : 'loading'} />
      <OwnerCrew hosts={data.hosts ?? []} live={data.live ?? []} missions={data.missions ?? []} now={now}
        executors={data.runtime?.executors} unserved={data.runtime?.jobs_without_executor}
        read={data.controlsReadable ? 'ok' : data.runtimeError ? { error: runtimeUnreadLine(data.runtimeReadAt, data.runtimeOpenedAt, now) } : 'loading'} />
      <Card label="Quota" title="Quota" more={{ href: '#crew?section=capacity', text: 'Crew capacity' }}>
        <p className="m-0 text-sm">Quota holds stay on the affected goals and attempts. Pool readings and reset times are in Crew.</p>
      </Card>
    </div>
  </div>
}
