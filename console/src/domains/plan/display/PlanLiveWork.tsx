import { UnlinkedSessions } from '@/domains/plan/display/UnlinkedSessions'
import { ExternalRecord } from '@/domains/plan/display/ExternalRecord'
import { useState } from 'react'
import type { ExternalWork, PlanSnapshot, PlanWork } from '@/api/planSnapshot'
import { useNow } from '@/pages/shell/progress/progressClock'
import { WorkDeliveryStrips } from '@/pages/shell/agents/DeliveryStrip'
import { heartbeatUnconfirmed } from '@/pages/shell/agents/needsCleanupModel'
import { roundLabel } from '@/domains/plan/readings/rounds'
import { planNodeHref } from '@/pages/shell/agents/usePlanSnapshot'
import { workSectionHref } from '@/pages/shell/agents/workViewModel'
import { coverageGroups, type SessionsRead } from '@/pages/shell/agents/unlinkedSessionsModel'

/** How many submitted runs show before "Show n more". */
const SUBMITTED_SHOWN = 8
const caps = 'text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const mono = 'font-mono text-xs text-muted-foreground'
const PILL: Record<string, string> = {
  ok: 'text-[var(--color-lamp-green)] bg-[color-mix(in_srgb,var(--color-lamp-green)_14%,transparent)]',
  info: 'text-[var(--color-info)] bg-[color-mix(in_srgb,var(--color-info)_14%,transparent)]',
  unknown: 'border border-dashed border-[var(--color-lamp-gray)] text-muted-foreground',
}

function duration(seconds: number | undefined) {
  if (seconds === undefined) return 'Unknown'
  if (seconds < 60) return `${Math.floor(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`
}
/** One registered piece of external work now (Live and a module's Live on card): its executor, task, step, state and heartbeat. */
export function WorkRow({ work, view, stale }: { work: PlanWork; view: PlanSnapshot; stale: boolean }) {
  const now = useNow()
  const drift = Math.max(0, (now - Date.parse(view.as_of)) / 1000)
  const heartbeatAge = work.heartbeat_age_s + drift
  const unconfirmed = stale || heartbeatUnconfirmed(work, view, now)
  const node = view.nodes.find(n => n.plan_node_id === work.plan_node_id)
  const elapsed = work.elapsed_s === undefined ? undefined : work.elapsed_s + (stale ? 0 : drift)
  const state = unconfirmed ? 'Unconfirmed' : work.status || work.state
  const tone = unconfirmed ? 'unknown' : state === 'Running' ? 'ok' : 'info'
  return <li className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-[14px] px-3 py-2.5 [overflow-wrap:anywhere] [background:var(--ops-selection)]">
    <span className="min-w-[6.875rem] text-[13px] font-semibold">{work.description.executor}</span>
    <strong className="min-w-0 flex-[1_1_13.75rem] text-[15px] font-normal leading-[1.45]">{work.description.task}</strong>
    <a className={`${mono} inline-flex min-h-6 items-center pointer-coarse:min-h-11`} href={planNodeHref(view.root, work.plan_node_id)} title={node?.title}>Step · {node?.label}</a>
    <span className={`inline-flex h-[22px] items-center whitespace-nowrap rounded-full px-2 text-xs font-semibold ${PILL[tone]}`}>{state}</span>
    <span className="basis-full text-xs text-muted-foreground">
      {work.description.provider} · External work · Elapsed {duration(elapsed)}{stale && ' at last read'} · Last heartbeat · {duration(heartbeatAge)} ago
      {' '}· <time dateTime={work.heartbeat_at}>{work.heartbeat_at}</time> · {work.description.repo} · {work.description.branch}{work.backfilled && ' · Backfilled'}
    </span>
    {work.commit_shas?.length ? <span className="basis-full"><WorkDeliveryStrips commits={work.commit_shas} /></span> : null}
  </li>
}
export { ExternalRecord } from '@/domains/plan/display/ExternalRecord'

const endedAt = (w: ExternalWork) => w.ended_at ?? w.heartbeat_at

/**
 * Unlinked sessions (R5:5.5-12 §3.3): sessions the platform heard from whose work is not in the runtime, one row per
 * stable identity (vendor · host · session · epoch) with its status, first sighting, last event and why; no title (the
 * title store is keyed by session alone). A Codex hook thread is counted, not judged. While the thread list or the
 * runtime cannot be read, it says so instead of a count.
 */
export { UnlinkedSessions } from '@/domains/plan/display/UnlinkedSessions'

/**
 * Work → Live (Design v3.6 WorkPage): this round's work now (plan-view working: registered external work, Unconfirmed
 * after 60 s without a heartbeat) and its submitted runs, newest first. Submitted is the executor's
 * word that it ended, not verification and not plan completion. What is not registered is not seen here; the runtime
 * as a whole (running attempts, hosts, open goals, release runs) is on Activity.
 */
export function PlanLiveWork({ view, stale, sessions }: { view: PlanSnapshot; stale: boolean; sessions?: SessionsRead }) {
  const [all, setAll] = useState(false)
  const submitted = view.external_work.filter(w => w.state === 'Submitted').sort((a, b) => endedAt(b).localeCompare(endedAt(a)))
  const shown = all ? submitted : submitted.slice(0, SUBMITTED_SHOWN)
  const verification = new Map<string, number>()
  for (const w of submitted) verification.set(w.verification ?? 'Unknown', (verification.get(w.verification ?? 'Unknown') ?? 0) + 1)
  return <section aria-label="Live work" className="flex min-w-0 flex-col gap-2 rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)]">
    <div className="flex flex-wrap items-center justify-between gap-2">
      <h3 className={`m-0 ${caps}`}>Now · {stale ? 'last read ' : ''}{view.working.length} registered</h3>
      <span className={mono}>plan-view {view.root} · working</span>
    </div>
    <p className="m-0 text-xs text-muted-foreground">Registered external work in {roundLabel(view.root)} · Unconfirmed after 60s without a heartbeat.</p>
    {view.working.length === 0 && <p className="m-0 text-sm">{stale ? 'Current activity is Unknown. Last read contained no active work.' : 'No active work in this snapshot.'}</p>}
    {view.working.length > 0 && <ul aria-label="Work now" className="m-0 flex list-none flex-col gap-2 p-0">{view.working.map(work => <WorkRow key={work.external_work_id} work={work} view={view} stale={stale} />)}</ul>}
    <div className="flex flex-wrap items-center justify-between gap-2 pt-1.5">
      <h3 className={`m-0 ${caps}`}>Recently submitted · {shown.length} of {submitted.length}</h3>
      {submitted.length > 0 && <span className="text-xs text-muted-foreground">verification {[...verification].map(([v, n]) => `${v} on ${n === submitted.length ? 'all' : n}`).join(' · ')}</span>}
    </div>
    {submitted.length === 0 ? <p className="m-0 text-sm text-muted-foreground">No submitted external work recorded for {roundLabel(view.root)}.</p>
      : <ul aria-label="Recently submitted" className="m-0 flex list-none flex-col gap-1.5 p-0">{shown.map(w => {
        const node = view.nodes.find(n => n.plan_node_id === w.plan_node_id)
        return <li key={w.external_work_id} className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-xl px-3 py-2 shadow-[var(--ops-inset)] [overflow-wrap:anywhere]">
          <span className="min-w-0 flex-[1_1_15rem] text-sm leading-[1.45]">{w.description.task}</span>
          <span className={`${mono} whitespace-nowrap`} title={`${w.source_run_key} · ended ${endedAt(w)}`}>{node?.label ?? w.plan_node_id} · {duration(w.elapsed_s)} · {w.verification ?? 'Unknown'}</span>
          {w.commit_shas?.length ? <span className="basis-full"><WorkDeliveryStrips commits={w.commit_shas} /></span> : null}
        </li>
      })}</ul>}
    {submitted.length > SUBMITTED_SHOWN && !all && <button type="button" onClick={() => setAll(true)}
      className="min-h-11 self-start border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]">Show {submitted.length - SUBMITTED_SHOWN} more</button>}
    <details className="text-sm"><summary className="min-h-11 cursor-pointer py-3">Finished today · {view.finished_today.length} · {view.day_timezone}</summary>
      <p>Execution ended; this does not mean the plan is complete.</p>
      <ul>{view.finished_today.map(work => <ExternalRecord key={work.external_work_id} work={work} />)}</ul>
    </details>
    {sessions && <UnlinkedSessions read={sessions} />}
    <p className="m-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">Coverage · {coverageGroups(view.coverage).join(' · ')} · {view.coverage.scope}{view.coverage.sessions && ` · ${view.coverage.sessions.scope}`}</p>
    <p className="m-0 text-sm">Running attempts, hosts, open goals and release runs across the runtime: <a href={workSectionHref('activity')} className="inline-flex min-h-11 items-center">Activity ›</a></p>
  </section>
}
