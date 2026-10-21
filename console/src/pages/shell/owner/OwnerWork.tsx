import { COORDINATOR_WINDOW } from '@/api/agentRuntimeBus'
import { archCounts } from '@/api/agentRuntimePlan'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { ARCH_NOT_IMPORTED, PLAN_NOT_IMPORTED } from '../agents/useRuntimePlan'
import { Bar, Card, Stat } from './OwnerParts'
import { OwnerCrew } from './OwnerCrew'
import { OwnerGoals } from './OwnerGoals'
import { PoolReadings } from './PoolReadings'
import { ago } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

/**
 * Work: running, stuck and open goals, the last finished and what is next; each open goal with when it
 * should be done; the crew, host by host; the plan as bars, from the runtime once it holds the round
 * (the board's, marked as such, until then); each pool's last reading. The last run step by
 * step, done → now → next and the threads are on their detail pages.
 */
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

export function OwnerWork({ data }: { data: OwnerConsoleData }) {
  const { work, plan, planSource: source, planRound: round, now } = data
  const arch = data.arch
  const counts = arch.from === 'runtime' ? archCounts(arch.table) : null
  return <section aria-label="Work" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">Work</h2>
    <div className="grid gap-4 @3xl:grid-cols-2 @6xl:grid-cols-3">
      <Card label="Now" title="Now" more={{ href: goalHref(), text: 'Goals' }}>
        <div className="grid grid-cols-3 gap-2">
          <Stat value={work?.running ?? '?'} label="running" tone={work && work.running > 0 ? 'info' : undefined} />
          <Stat value={work?.stuck ?? '?'} label="stuck" tone={work && work.stuck > 0 ? 'fail' : undefined} />
          <Stat value={work?.open ?? '?'} label="open goals" />
        </div>
        {/* T24: an attempt on a host that stopped polling, or maybe stuck, is not counted as running; Crew says which. */}
        {work && work.stalled > 0 && <p className="m-0 text-xs font-semibold text-[var(--color-warning)]" data-stalled>Not running: {work.stalledWords} (Crew)</p>}
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Last result</span>
          {data.last
            ? <><div className="flex flex-wrap justify-between gap-2"><span className="min-w-0 break-words"><strong>{data.last.mission}</strong> {data.lastGoal.split('\n')[0]}</span>
                <span className={`font-semibold ${data.last.status === 'done' ? 'text-[var(--color-success)]' : 'text-muted-foreground'}`} title={data.last.at}>
                  {data.last.status === 'done' ? `Done ${ago(data.last.at, now)}` : `Implementation accepted ${ago(data.last.at, now)}${data.last.status === 'open' ? `; goal ${data.last.state}` : ''}`}</span></div>
              <a href={goalHref(data.last.mission)} className="inline-flex min-h-11 items-center text-sm">Step by step</a></>
            : <span className="text-sm text-muted-foreground">{data.runtimeError ? 'The agent runtime could not be read.' : work ? 'No implementation accepted recently.' : 'Reading the agent runtime…'}</span>}
        </div>
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Coordinator</span>
          <CoordinatorNote view={data.coordinator} now={now} />
        </div>
      </Card>
      <OwnerGoals missions={data.missions ?? []} now={now} read={data.controlsReadable ? 'ok' : data.runtimeError ? 'error' : 'loading'} />
      <OwnerCrew hosts={data.hosts ?? []} live={data.live ?? []} missions={data.missions ?? []} now={now}
        read={data.controlsReadable ? 'ok' : data.runtimeError ? 'error' : 'loading'} />
      <Card label="The plan" title={<>The plan{round?.n != null && ` · Round ${round.n}`}</>} more={{ href: '#work?section=plan', text: 'Plan' }}>
        {source.from === 'absent' && <p className="m-0 text-sm text-muted-foreground">{PLAN_NOT_IMPORTED}</p>}
        {source.from === 'loading' && <p role="status" className="m-0 text-sm text-muted-foreground">Reading the plan from the agent runtime…</p>}
        {source.from === 'error' || round?.unknown !== undefined
          ? <p role="alert" className="m-0 break-words text-sm text-muted-foreground">Plan unavailable: {source.from === 'error' ? source.error : round?.unknown}</p>
          : plan.length === 0 && source.from === 'runtime' && <p className="m-0 text-sm text-muted-foreground">No items in the runtime's plan.</p>}
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0">{plan.map(row => <li key={row.key} className={`grid grid-cols-[2.75rem_minmax(0,1fr)_3rem] items-center gap-2 rounded px-1.5 py-1 ${row.tag === 'now' ? 'bg-[var(--control-fill)]' : ''}`}>
          <span className="font-mono text-sm font-semibold">{row.key}</span>
          <div className="flex min-w-0 flex-col gap-1">
            <span className="flex flex-wrap items-center gap-1.5 text-sm"><span className="min-w-0 break-words">{row.name}</span>
              {row.tag && <span className={`rounded-full px-2 text-xs font-bold ${row.tag === 'now' ? 'bg-[var(--color-info)] text-background' : 'bg-[var(--control-fill)]'}`}>{row.tag}</span>}
              {/* Done by a manual record, counted as done and marked apart (v5 §5.1.2). */}
              {row.manual > 0 && <span className="text-xs text-muted-foreground" title="Steps done by a manual record (an import from the work ledger, or one recorded by hand)">{row.manual} manual</span>}
              {row.stopped > 0 && <span className="text-xs text-muted-foreground">{row.stopped} stopped</span>}</span>
            <Bar percent={row.total ? (row.done / row.total) * 100 : 0} tone={row.tone} />
          </div>
          <span className="text-right font-mono text-xs text-muted-foreground">{row.done}/{row.total}</span>
        </li>)}</ol>
        {/* Each architecture row is ticked by its gate tests against what is deployed now (2.5 ⑤ G2). */}
        {counts && arch.from === 'runtime' && <p className="m-0 flex flex-wrap gap-2 border-t border-[var(--table-rule)] pt-2 text-sm" data-arch-summary>
          <strong>Architecture, {arch.table.rows.length} rows:</strong><span>{counts.passed} passed</span><span>{counts.notYet} not yet</span><span>{counts.noGate} no gate yet</span><span>{counts.unknown} unknown</span></p>}
        {arch.from === 'absent' && <p className="m-0 border-t border-[var(--table-rule)] pt-2 text-xs text-muted-foreground">{ARCH_NOT_IMPORTED}</p>}
        {arch.from === 'error' && <p role="alert" className="m-0 break-words border-t border-[var(--table-rule)] pt-2 text-sm text-muted-foreground">Architecture unavailable: {arch.error}</p>}
      </Card>
      <Card label="Quota" title="Quota" more={{ href: '#system', text: 'System' }}>
        <PoolReadings now={now} />
      </Card>
    </div>
  </section>
}
