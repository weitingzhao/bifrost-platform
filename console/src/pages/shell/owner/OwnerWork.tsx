import { COORDINATOR_WINDOW } from '@/api/agentRuntimeBus'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { WorkPlanView } from '../agents/WorkPlanView'
import { Card, Stat } from './OwnerParts'
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

/** The aggregate plan and live work first, followed by existing runtime and crew details. */
export function OwnerWork({ data }: { data: OwnerConsoleData }) {
  const { work, now } = data
  return <section aria-label="Work" className="flex flex-col gap-3">
    <h2 className="sr-only">Work</h2>
    <WorkPlanView />
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
  </section>
}
