import { Bar, Card, HandMark, Stat } from './OwnerParts'
import { ago } from './ownerModel'
import type { OwnerConsoleData } from './useOwnerConsole'

const ARCH = { done: 'landed', part: 'part', none: 'none' } as const

/**
 * Work: running, stuck and open goals, the last finished and what is next; the plan as bars; the
 * quota. The last run step by step, done → now → next and the threads are on their detail pages.
 */
export function OwnerWork({ data }: { data: OwnerConsoleData }) {
  const { work, board, plan, quota, now } = data
  const arch = (['done', 'part', 'none'] as const).map(state => ({ state, n: data.arch.filter(r => r.state === state).length }))
  const boardAt = board?.at ? new Date(board.at * 1000).toISOString() : undefined
  return <section aria-label="Work" className="flex flex-col gap-3">
    <h2 className="m-0 text-lg font-semibold">Work</h2>
    <div className="grid gap-4 @3xl:grid-cols-2 @6xl:grid-cols-3">
      <Card label="Now" title="Now" more={{ href: '#progress', text: 'Goals' }}>
        <div className="grid grid-cols-3 gap-2">
          <Stat value={work?.running ?? '?'} label="running" tone={work && work.running > 0 ? 'info' : undefined} />
          <Stat value={work?.stuck ?? '?'} label="stuck" tone={work && work.stuck > 0 ? 'fail' : undefined} />
          <Stat value={work?.open ?? '?'} label="open goals" />
        </div>
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Last finished</span>
          {work?.lastFinished
            ? <><div className="flex flex-wrap justify-between gap-2"><span className="min-w-0 break-words"><strong>{work.lastFinished.mission}</strong> {data.lastGoal.split('\n')[0]}</span>
                <span className="font-semibold text-[var(--color-success)]" title={work.lastFinished.at}>Done {ago(work.lastFinished.at, now)}</span></div>
              <a href={`#progress?goal=${encodeURIComponent(work.lastFinished.mission)}`} className="text-sm">Step by step</a></>
            : <span className="text-sm text-muted-foreground">{work ? 'Nothing finished recently.' : 'Agent runtime not read yet.'}</span>}
        </div>
        <div className="flex flex-col gap-1 border-t border-[var(--table-rule)] pt-2">
          <span className="text-xs uppercase tracking-wide text-muted-foreground">Coordinator</span>
          {board?.coordinator?.doing || board?.coordinator?.next
            ? <>{board.coordinator.doing && <span className="break-words">{board.coordinator.doing}</span>}
              {board.coordinator.next && <span className="break-words text-sm text-muted-foreground">Next: {board.coordinator.next}</span>}</>
            : <span className="text-sm text-muted-foreground">No note from the coordinator.</span>}
          <HandMark title={`Kept by hand by the coordinator thread${boardAt ? ` · ${boardAt}` : ''}${data.boardStale ? ' · stale' : ''}`} />
        </div>
      </Card>
      <Card label="The plan" title={<>The plan{board?.roadmap?.round?.n != null && ` · Round ${board.roadmap.round.n}`} <HandMark title="Kept by hand by the coordinator thread (WORK.md)" /></>} more={{ href: '#progress', text: 'Plan' }}>
        {plan.length === 0 && <p className="m-0 text-sm text-muted-foreground">No plan on the coordinator's board.</p>}
        <ol className="m-0 flex list-none flex-col gap-1.5 p-0">{plan.map(row => <li key={row.key} className={`grid grid-cols-[2.75rem_minmax(0,1fr)_3rem] items-center gap-2 rounded px-1.5 py-1 ${row.tag === 'now' ? 'bg-[var(--control-fill)]' : ''}`}>
          <span className="font-mono text-sm font-semibold">{row.key}</span>
          <div className="flex min-w-0 flex-col gap-1">
            <span className="flex flex-wrap items-center gap-1.5 text-sm"><span className="min-w-0 break-words">{row.name}</span>
              {row.tag && <span className={`rounded-full px-2 text-xs font-bold ${row.tag === 'now' ? 'bg-[var(--color-info)] text-background' : 'bg-[var(--control-fill)]'}`}>{row.tag}</span>}</span>
            <Bar percent={row.total ? (row.done / row.total) * 100 : 0} tone={row.tone} />
          </div>
          <span className="text-right font-mono text-xs text-muted-foreground">{row.done}/{row.total}</span>
        </li>)}</ol>
        {data.arch.length > 0 && <p className="m-0 flex flex-wrap gap-2 border-t border-[var(--table-rule)] pt-2 text-sm">
          <strong>Architecture, {data.arch.length} parts:</strong>{arch.map(a => <span key={a.state}>{a.n} {ARCH[a.state]}</span>)}</p>}
      </Card>
      <Card label="Quota" title={<>Quota <HandMark title="Read by hand into the coordinator's usage file" /></>} more={{ href: '#progress', text: 'Usage' }}>
        {quota.length === 0 && <p className="m-0 text-sm text-muted-foreground">No quota reading on the board.</p>}
        <ul className="m-0 flex list-none flex-col gap-2.5 p-0">{quota.map(q => <li key={q.id} className="flex flex-col gap-1">
          <div className={`flex justify-between gap-2 text-sm ${q.stale ? 'text-muted-foreground' : ''}`}><span className="min-w-0 break-words">{q.name}</span><span className="font-mono">{q.used == null ? '?' : `${q.used}%`}</span></div>
          <Bar percent={q.used ?? 0} tone={q.tone} />
          <span className="flex items-center gap-1.5 text-xs text-muted-foreground">{q.readAt ? `read ${ago(q.readAt, now)}` : 'read time unknown'}
            {q.stale && <span className="rounded-full bg-[var(--control-fill)] px-2 font-bold text-[var(--color-warning)]">stale</span>}</span>
        </li>)}</ul>
      </Card>
    </div>
  </section>
}
