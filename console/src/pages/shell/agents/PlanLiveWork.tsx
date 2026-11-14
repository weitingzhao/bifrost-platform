import { useQuery } from '@tanstack/react-query'
import { AGENT_RUNTIME_POLL_MS } from '@/api/agentRuntime'
import { fetchAttemptActions } from '@/api/agentRuntimeBus'
import { ActionLabel, ago, ATTEMPT_ACTION_STUCK_MS } from '../progress/attemptActionWords'
import { waitWords, type WaitWords } from './waitWords'
import { useState } from 'react'
import type { ExternalWork, PlanSnapshotRead as PlanSnapshot, PlanWork } from '@/api/planSnapshot'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { useNow } from '../progress/progressClock'
import { WorkDeliveryStrips } from './DeliveryStrip'
import { heartbeatUnconfirmed } from './needsCleanupModel'
import { roundLabel } from './planRoundModel'
import { planNodeHref } from './usePlanSnapshot'
import { workSectionHref } from './workViewModel'
import { coverageGroups, shortSession, unlinkedSessions, type SessionsRead } from './unlinkedSessionsModel'

function WaitLine({ wait }: { wait: WaitWords }) {
  return <span className="basis-full text-xs text-muted-foreground">{wait.href ? <a href={wait.href}>{wait.text}</a> : wait.text}</span>
}

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
function LiveAttemptAction({ attempt, now }: { attempt: NonNullable<Exclude<PlanWork, ExternalWork>['attempt']>; now: number }) {
  const query = useQuery({ queryKey: ['agent-runtime-actions-last', attempt.attempt_id],
    queryFn: ({ signal }) => fetchAttemptActions(attempt.attempt_id, { last: 1 }, signal), refetchInterval: q => q.state.data?.unavailable ? false : AGENT_RUNTIME_POLL_MS, retry: false })
  const action = query.data?.actions.at(-1)
  if (query.isError) return <div className="basis-full text-xs text-muted-foreground">Last action Unknown · {query.error.message}</div>
  if (query.data?.unavailable) return <div className="basis-full text-xs text-muted-foreground">Last action Unknown · actions are not kept on this platform</div>
  if (query.isPending) return <div className="basis-full text-xs text-muted-foreground">Reading actions…</div>
  const age = now - Date.parse(action ? action.at : attempt.started_at ?? '')
  return <div className="flex basis-full min-w-0 flex-col gap-1 text-xs">
    {action ? <div className="flex min-w-0 gap-2"><ActionLabel action={action} /><code className="min-w-0 flex-1 truncate" title={action.target}>{action.target}</code><span className="shrink-0">{ago(action.at, now)}</span></div> : <span>no actions yet</span>}
    {age > ATTEMPT_ACTION_STUCK_MS && <span style={{ color: 'var(--color-danger)' }}>Possibly stuck · no action for {Math.floor(age / 60_000)} min</span>}
  </div>
}
/** One registered piece of work now (Live and a module's Live on card): its executor, task, step, state and heartbeat. */
export function WorkRow({ work, view, stale }: { work: PlanWork; view: PlanSnapshot; stale: boolean }) {
  const now = useNow()
  const drift = Math.max(0, (now - Date.parse(view.as_of)) / 1000)
  const external = 'external_work_id' in work
  const run = external ? work : work.attempt!
  const task = external ? undefined : view.tasks.find(t => t.task_id === work.task_id)
  const wait = task ? waitWords(task, view.explain, now) : undefined
  const heartbeatAge = run.heartbeat_age_s + drift
  const unconfirmed = stale || heartbeatUnconfirmed(run, view, now)
  const node = view.nodes.find(n => n.plan_node_id === work.plan_node_id)
  const elapsed = run.elapsed_s === undefined ? undefined : run.elapsed_s + (stale ? 0 : drift)
  const state = unconfirmed ? 'Unconfirmed' : run.status || run.state
  const tone = unconfirmed ? 'unknown' : state === 'Running' || state === 'running' ? 'ok' : 'info'
  return <li className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-[14px] px-3 py-2.5 [overflow-wrap:anywhere] [background:var(--ops-selection)]">
    <span className="min-w-[6.875rem] text-[13px] font-semibold">{external ? work.description.executor : work.attempt!.executor}</span>
    <strong className="min-w-0 flex-[1_1_13.75rem] text-[15px] font-normal leading-[1.45]">{external ? work.description.task : work.title}</strong>
    <a className={`${mono} inline-flex min-h-6 items-center pointer-coarse:min-h-11`} href={planNodeHref(view.root, work.plan_node_id)} title={node?.title}>Step · {node?.label}</a>
    <span className={`inline-flex h-[22px] items-center whitespace-nowrap rounded-full px-2 text-xs font-semibold ${PILL[tone]}`}>{state}</span>
    <span className="basis-full text-xs text-muted-foreground">
      {external ? work.description.provider : work.attempt!.model || 'Model unknown'} · {external ? 'External work' : `Runtime attempt · ${work.attempt!.attempt_id}`} · Elapsed {duration(elapsed)}{stale && ' at last read'} · Last heartbeat · {duration(heartbeatAge)} ago
      {external && <> · <time dateTime={work.heartbeat_at}>{work.heartbeat_at}</time> · {work.description.repo} · {work.description.branch}{work.backfilled && ' · Backfilled'}</>}
      {!external && <> · Host · {work.attempt!.host_id || 'Unknown'}{work.external_work?.length ? ` · Linked external history · ${work.external_work.length} records` : ''} · {work.attempt!.reason}</>}
    </span>
    {external && work.commit_shas?.length ? <span className="basis-full"><WorkDeliveryStrips commits={work.commit_shas} /></span> : null}
    {!external && <LiveAttemptAction attempt={work.attempt!} now={now} />}
    {wait?.text && <WaitLine wait={wait} />}
    {!external && <a className="inline-flex min-h-11 items-center text-sm" href={goalHref(work.mission_id, work.task_id)}>Open task</a>}
  </li>
}
export function ExternalRecord({ work }: { work: ExternalWork }) {
  return <li className="[overflow-wrap:anywhere]">{work.description.task} · {work.description.executor} · {work.status}{work.backfilled && ' · Backfilled'}{work.verification && ` · ${work.verification}`}
    {work.report_digest && <span className="block">Report · {work.report_digest}</span>}
    {work.commit_shas?.map(sha => <span className="block" key={sha}>Commit · {sha}</span>)}
    <WorkDeliveryStrips commits={work.commit_shas} />
  </li>
}
const endedAt = (w: ExternalWork) => w.ended_at ?? w.heartbeat_at

/**
 * Unlinked sessions (R5:5.5-12 §3.3): sessions the platform heard from whose work is not in the runtime, one row per
 * stable identity (vendor · host · session · epoch) with its status, first sighting, last event and why; no title (the
 * title store is keyed by session alone). A Codex hook thread is counted, not judged. While the thread list or the
 * runtime cannot be read, it says so instead of a count.
 */
export function UnlinkedSessions({ read }: { read: SessionsRead }) {
  if (read.state === 'loading') return <section aria-label="Unlinked sessions" className="flex flex-col gap-1.5 text-sm"><h3 className={`m-0 ${caps}`}>Unlinked sessions · reading</h3></section>
  if (read.state === 'failed') return <section aria-label="Unlinked sessions" className="flex flex-col gap-1.5 text-sm">
    <h3 className={`m-0 ${caps}`}>Unlinked sessions · Unknown</h3>
    <p role="status" className="m-0 [overflow-wrap:anywhere]">The thread list could not be read ({read.error}); which sessions are unlinked is Unknown.</p>
  </section>
  const { rows, unsupported, runtimeUnreadable } = unlinkedSessions(read.data)
  return <section aria-label="Unlinked sessions" className="flex min-w-0 flex-col gap-1.5 text-sm">
    <h3 className={`m-0 ${caps}`}>Unlinked sessions · {read.stale ? 'last read ' : ''}{rows.length}</h3>
    {runtimeUnreadable && <p role="status" className="m-0">The runtime could not be read: these sessions' bindings are Unknown, not unlinked.</p>}
    {rows.length === 0 ? <p className="m-0 text-muted-foreground">No unlinked sessions in the threads retained.</p>
      : <ul aria-label="Unlinked sessions" className="m-0 flex list-none flex-col gap-1.5 p-0">{rows.map(row => <li key={row.key} data-session={row.key}
        className="flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1 rounded-xl px-3 py-2 shadow-[var(--ops-inset)] [overflow-wrap:anywhere]">
        <span className="min-w-0 flex-[1_1_15rem] leading-[1.45]">{row.vendor} · {row.host} · {shortSession(row.session)}{row.epoch > 0 ? ` · epoch ${row.epoch}` : ''}</span>
        <span className={`${mono} whitespace-nowrap`}>{row.state} · {row.status}</span>
        <span className="basis-full text-xs text-muted-foreground">First seen · {row.firstSeen ? <time dateTime={row.firstSeen}>{row.firstSeen}</time> : 'Unknown'} · Last event · {row.event} · {row.reason}</span>
        <details className="basis-full text-xs"><summary className="min-h-11 cursor-pointer py-3 pointer-fine:min-h-6 pointer-fine:py-1">Details</summary>
          <span className="block">Thread · {row.thread.thread}</span>
          {row.thread.tool && <span className="block">Tool · {row.thread.tool}</span>}
          {row.thread.token_kind && <span className="block">Token · {row.thread.token_kind}</span>}
          {row.thread.host_id_source && <span className="block">Host id from · {row.thread.host_id_source}</span>}
        </details>
      </li>)}</ul>}
    {unsupported > 0 && <p className="m-0 text-xs text-muted-foreground">Codex hook threads · {unsupported} · not judged until the S1-0 calibration binds them</p>}
  </section>
}

/**
 * Work → Live (Design v3.6 WorkPage): this round's work now (plan-view working: runtime attempts and registered external
 * work, Unconfirmed after 60 s without a heartbeat) and its submitted runs, newest first. Submitted is the executor's
 * word that it ended, not verification and not plan completion. What is not registered is not seen here; the runtime
 * as a whole (running attempts, hosts, open goals, release runs) is on Activity.
 */
export function PlanLiveWork({ view, stale, sessions }: { view: PlanSnapshot; stale: boolean; sessions?: SessionsRead }) {
  const now = useNow()
  const waiting = view.tasks.filter(t => waitWords(t, view.explain, now).text && !view.working.some(w => !('external_work_id' in w) && w.task_id === t.task_id))
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
    <p className="m-0 text-xs text-muted-foreground">Runtime attempts and registered external work in {roundLabel(view.root)} · Unconfirmed after 60s without a heartbeat.</p>
    {view.working.length === 0 && <p className="m-0 text-sm">{stale ? 'Current activity is Unknown. Last read contained no active work.' : 'No active work in this snapshot.'}</p>}
    {view.working.length > 0 && <ul aria-label="Work now" className="m-0 flex list-none flex-col gap-2 p-0">{view.working.map(work => <WorkRow key={'external_work_id' in work ? work.external_work_id : work.task_id} work={work} view={view} stale={stale} />)}</ul>}
    {waiting.length > 0 && <div className="flex flex-col gap-2">
      <h4 className={`m-0 ${caps}`}>Waiting</h4>
      <ul aria-label="Waiting" className="m-0 flex list-none flex-col gap-2 p-0">{waiting.map(task => <li key={task.task_id} className="flex flex-wrap gap-x-3 gap-y-1 px-3 py-2">
        <a href={goalHref(task.mission_id, task.task_id)}>{task.title || task.task_id}</a>
        <WaitLine wait={waitWords(task, view.explain, now)} />
      </li>)}</ul>
    </div>}
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
      <ul>{view.finished_today.map(work => 'external_work_id' in work ? <ExternalRecord key={work.external_work_id} work={work} /> : <li key={work.attempt_id}>{work.task_id} · {work.state} · {work.ended_at}</li>)}</ul>
    </details>
    {sessions && <UnlinkedSessions read={sessions} />}
    <p className="m-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">Coverage · {coverageGroups(view.coverage).join(' · ')} · {view.coverage.scope}{view.coverage.sessions && ` · ${view.coverage.sessions.scope}`}</p>
    <p className="m-0 text-sm">Running attempts, hosts, open goals and release runs across the runtime: <a href={workSectionHref('activity')} className="inline-flex min-h-11 items-center">Activity ›</a></p>
  </section>
}
