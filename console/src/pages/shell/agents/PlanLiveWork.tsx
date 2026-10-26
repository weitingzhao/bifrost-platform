import type { ExternalWork, PlanSnapshot, PlanWork } from '@/api/planSnapshot'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { Card } from '../owner/OwnerParts'
import { useNow } from '../progress/progressClock'
import { planNodeHref } from './usePlanSnapshot'

function duration(seconds: number | undefined) {
  if (seconds === undefined) return 'Unknown'
  if (seconds < 60) return `${Math.floor(seconds)}s`
  if (seconds < 3600) return `${Math.floor(seconds / 60)}m ${Math.floor(seconds % 60)}s`
  return `${Math.floor(seconds / 3600)}h ${Math.floor(seconds % 3600 / 60)}m`
}
function WorkRow({ work, view, stale }: { work: PlanWork; view: PlanSnapshot; stale: boolean }) {
  const now = useNow()
  const drift = Math.max(0, (now - Date.parse(view.as_of)) / 1000)
  const external = 'external_work_id' in work
  const run = external ? work : work.attempt!
  const heartbeatAge = run.heartbeat_age_s + drift
  const unconfirmed = heartbeatAge > 60 || stale || run.status === 'Unconfirmed'
  const node = view.nodes.find(n => n.plan_node_id === work.plan_node_id)
  const elapsed = run.elapsed_s === undefined ? undefined : run.elapsed_s + (stale ? 0 : drift)
  return <li className="flex min-w-0 flex-col gap-1 rounded border border-[var(--card-border)] p-3 [overflow-wrap:anywhere]">
    <strong>{external ? work.description.task : work.title}</strong>
    <span>{external ? work.description.executor : work.attempt!.executor} · {external ? work.description.provider : work.attempt!.model || 'Model unknown'} · {external ? 'External work' : `Runtime attempt · ${work.attempt!.attempt_id}`}</span>
    <span><strong>{unconfirmed ? 'Unconfirmed' : run.status || run.state}</strong> · Elapsed {duration(elapsed)}{stale && ' at last read'}</span>
    <span>Last heartbeat · {duration(heartbeatAge)} ago{external && <> · <time dateTime={work.heartbeat_at}>{work.heartbeat_at}</time></>}</span>
    <a className="inline-flex min-h-11 items-center" href={planNodeHref(view.root, work.plan_node_id)}>Step · {node?.label} {node?.title}</a>
    {!external && <><span>Host · {work.attempt!.host_id || 'Unknown'}</span><a className="inline-flex min-h-11 items-center" href={goalHref(work.mission_id, work.task_id)}>Open task</a></>}
    {external && <span>{work.description.repo} · {work.description.branch}{work.backfilled && ' · Backfilled'}</span>}
    {!external && work.external_work?.length ? <span>Linked external history · {work.external_work.length} records</span> : null}
    {!external && <span>{work.attempt!.reason}</span>}
  </li>
}
export function ExternalRecord({ work }: { work: ExternalWork }) {
  return <li className="[overflow-wrap:anywhere]">{work.description.task} · {work.description.executor} · {work.status}{work.backfilled && ' · Backfilled'}
    {work.report_digest && <span className="block">Report · {work.report_digest}</span>}
    {work.commit_shas?.map(sha => <span className="block" key={sha}>Commit · {sha}</span>)}
  </li>
}
export function PlanLiveWork({ view, stale }: { view: PlanSnapshot; stale: boolean }) {
  return <Card label="Live work" title={`Live work · ${stale ? 'last read' : view.working.length}`}>
    <p className="m-0 text-sm">Runtime attempts and registered external work · Unconfirmed after 60s without a heartbeat.</p>
    {view.working.length === 0 && <p>{stale ? 'Current activity is Unknown. Last read contained no active work.' : 'No active work in this snapshot.'}</p>}
    <ul className="m-0 grid list-none gap-3 p-0 md:grid-cols-2">{view.working.map(work => <WorkRow key={'external_work_id' in work ? work.external_work_id : work.task_id} work={work} view={view} stale={stale} />)}</ul>
    <details><summary className="min-h-11 cursor-pointer py-3">Finished today · {view.finished_today.length} · {view.day_timezone}</summary>
      <p>Execution ended; this does not mean the plan is complete.</p>
      <ul>{view.finished_today.map(work => 'external_work_id' in work ? <ExternalRecord key={work.external_work_id} work={work} /> : <li key={work.attempt_id}>{work.task_id} · {work.state} · {work.ended_at}</li>)}</ul>
    </details>
    <p className="m-0 text-sm">Coverage · {view.coverage.unregistered_work} unregistered · {view.coverage.reconciled ? 'Reconciled' : 'Not reconciled'}<br />{view.coverage.scope}</p>
  </Card>
}
