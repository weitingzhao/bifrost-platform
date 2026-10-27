import { useEffect } from 'react'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { goalHref, NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { scrollAfterShell } from '@/lib/shell/useHashSection'
import { ActionLink, Card } from '../owner/OwnerParts'
import { NowClock, useNow } from '../progress/progressClock'
import { SIGNOFF_SECTION_HREF } from '../progress/progressPart'
import { PlanLiveWork } from './PlanLiveWork'
import { PlanTree } from './PlanTree'
import { responsibility, validity } from './planSnapshotModel'
import { PlanRecentChanges } from './PlanRecentChanges'
import { planNodeDOMId, planNodeHref, usePlanAddress, usePlanSnapshot } from './usePlanSnapshot'

function Snapshot({ view, selected, failed }: { view: PlanSnapshot; selected: string; failed: boolean }) {
  const now = useNow()
  const stale = failed || now - Date.parse(view.as_of) > 60_000 || view.freshness.database.status !== 'Fresh'
  const current = view.nodes.find(n => n.plan_node_id === view.mainline.current?.plan_node_id)
  const waits = view.owner_waiting.flatMap(w => 'task_id' in w ? [w] : [])
  const signoffs = view.owner_waiting.flatMap(w => 'ref' in w ? [w] : [])
  const targetId = selected ? view.nodes.find(n => n.plan_node_id === selected || n.label === selected)?.plan_node_id : undefined
  useEffect(() => {
    if (targetId) return scrollAfterShell(() => document.getElementById(planNodeDOMId(targetId)))
  }, [targetId, selected])
  return <>
    <div className="grid min-w-0 gap-3 md:grid-cols-2">
      <Card label="Main line" title="Main line">
        <strong className="[overflow-wrap:anywhere]">{current ? `${current.label} ${current.title}` : 'Current position Unknown'}</strong>
        {view.next.map((next, i) => <div key={i} className="text-sm [overflow-wrap:anywhere]"><strong>Next · </strong>{next.action}<br />{responsibility(view.nodes.find(n => n.plan_node_id === next.plan_node_id))}</div>)}
        {view.next.length === 0 && <p className="m-0">Next · Unknown</p>}
        {view.waiting_on.filter(w => w.edge.from.node === current?.view?.plan_node_id).map((w, i) => <span key={i} className="text-sm">Waiting on · <a className="inline-flex min-h-11 items-center" href={planNodeHref(w.edge.to.root, w.edge.to.node)}>{w.edge.to.node}</a> · {w.edge.reason}</span>)}
        <span className="text-sm">ETA · Unknown</span>
        <span className="text-xs text-muted-foreground">{view.freshness.database.status === 'Unknown' ? 'Unknown' : stale ? 'Stale' : 'Fresh'} · Last successful snapshot {view.as_of}</span>
        {view.mainline.current?.current_validity && <span className="text-sm">Acceptance · {validity(view.mainline.current.current_validity)}</span>}
        <details><summary className="min-h-11 cursor-pointer py-3 text-sm">Basis and freshness</summary><p>{view.mainline.basis}{view.mainline.milestone_status && ` · Milestones ${view.mainline.milestone_status}`} · Verification {view.freshness.verification}</p><p>{view.eta.reason}</p>{view.warnings.map(w => <p key={w}>{w}</p>)}<p>Plan version {view.plan_version} · {view.artifact_id || 'No adopted view'}</p><p>{view.freshness.external_work}</p><p>Database observed · {view.freshness.database.observed_at}</p></details>
      </Card>
      <Card label="Needs you" title={signoffs.length > 0 ? `Needs you · ${signoffs.length} sign-off${signoffs.length === 1 ? '' : 's'} ready` : 'Needs you · Unknown'} accent>
        <p className="m-0 text-sm">{waits.length} observed task waits{stale && ' at last read'}. {signoffs.length > 0 ? `${signoffs.length} acceptance sign-offs ready.` : 'Approval, question and sign-off readiness is not confirmed.'}</p>
        {waits.map(wait => { const task = view.tasks.find(t => t.task_id === wait.task_id); return <div key={wait.task_id} className="text-sm"><span>{wait.reason}</span><ActionLink href={task ? goalHref(task.mission_id, task.task_id) : planNodeHref(view.root, wait.plan_node_id)}>Review task</ActionLink></div> })}
        {signoffs.map(s => { const n = view.nodes.find(x => x.plan_node_id === s.plan_node_id); return <div key={s.ref} className="text-sm [overflow-wrap:anywhere]"><span>Sign off · {n ? `${n.label} ${n.title}` : s.plan_node_id} · acceptance {s.acceptance} · plan v{s.plan_version}</span><ActionLink href={planNodeHref(view.root, s.plan_node_id)}>Open step</ActionLink></div> })}
        <div className="flex flex-wrap gap-2"><ActionLink href={NEEDS_YOU_HREF}>Review requests</ActionLink><ActionLink href={SIGNOFF_SECTION_HREF}>Plan sign-offs</ActionLink></div>
        <details><summary className="min-h-11 cursor-pointer py-3">Later · {view.owner_later.length}</summary><p>{view.freshness.owner_waiting}</p><ul>{view.owner_later.map(n => <li key={'ref' in n ? n.ref : n.plan_node_id}><a className="inline-flex min-h-11 items-center" href={planNodeHref(view.root, n.plan_node_id)}>{'title' in n ? n.title : view.nodes.find(x => x.plan_node_id === n.plan_node_id)?.title ?? n.plan_node_id}</a> · {n.readiness}<br />{n.reason}{'acceptance' in n && ` · acceptance ${n.acceptance}`}</li>)}</ul></details>
      </Card>
    </div>
    {selected && !view.nodes.some(n => n.plan_node_id === selected || n.label === selected) && <p role="alert">Node not found in {view.root}: {selected}</p>}
    <PlanLiveWork view={view} stale={stale} />
    <PlanTree view={view} selected={selected} />
    <Card label="Waiting on" title="Waiting on">
      {view.waiting_on.length === 0 && <p>{view.freshness.verification === 'Unknown' ? 'No blocking edges recorded. Verification remains Unknown.' : 'Every required milestone is verified.'}</p>}
      <ul className="m-0 list-none space-y-2 p-0">{view.waiting_on.map((wait, i) => <li key={i} className="[overflow-wrap:anywhere]">
        <a className="inline-flex min-h-11 items-center" href={planNodeHref(wait.edge.from.root, wait.edge.from.node)}>{wait.edge.from.node}</a> → <a className="inline-flex min-h-11 items-center" href={planNodeHref(wait.edge.to.root, wait.edge.to.node)}>{wait.edge.to.node}</a> · {wait.edge.milestone} · {wait.status}<br />{wait.edge.reason}<br />{wait.responsible.kind} · {wait.responsible.actor_ref || wait.responsible.seat_ref}<br /><span className="text-xs">{wait.reason} · Evidence {wait.edge.sources.join(', ')}</span>
      </li>)}</ul>
    </Card>
    <PlanRecentChanges view={view} />
  </>
}
export function WorkPlanView() {
  const { root, node } = usePlanAddress()
  const query = usePlanSnapshot(root)
  const failed = query.isError || query.isPaused
  return <NowClock><section id="plan" aria-label="Plan and live view" className="flex min-w-0 scroll-mt-4 flex-col gap-3">
    {query.isPending && !failed && <p role="status">Loading plan and live work…</p>}
    {failed && <div role="alert" className="rounded border border-[var(--color-warning)] p-3 [overflow-wrap:anywhere]">Unknown · {query.isPaused ? 'Offline' : query.error?.message}. {query.data ? `Stale · showing the last successful snapshot from ${query.data.as_of}.` : 'No successful snapshot yet.'}<button className="ml-2 min-h-11 rounded border px-3" onClick={() => void query.refetch()}>Retry</button></div>}
    {query.data && <Snapshot key={root} view={query.data} selected={node} failed={failed} />}
  </section></NowClock>
}
