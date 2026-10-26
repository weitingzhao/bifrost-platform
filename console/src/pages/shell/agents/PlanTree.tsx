import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { goalHref } from '@/lib/shell/consoleRoutes'
import { responsibility } from './planSnapshotModel'
import { ExternalRecord } from './PlanLiveWork'
import { planNodeDOMId, planNodeHref } from './usePlanSnapshot'

function NodeCard({ node, view, selected, children }: { node: PlanNode; view: PlanSnapshot; selected: string; children?: React.ReactNode }) {
  const current = view.mainline.current?.plan_node_id === node.plan_node_id
  const item = node.view
  const dependencies = view.edges.filter(e => e.from.root === view.root && e.from.node === item?.plan_node_id)
  const work = view.external_work.filter(w => w.plan_node_id === node.plan_node_id)
  const steps = view.nodes.filter(n => n.parent_plan_node_id === node.plan_node_id && n.node_kind === 'step' && !n.view)
  // Tasks bound to this node itself: a classified step (with `view`) is its own card, not a row of steps (review 629 B2).
  const ownTasks = view.tasks.filter(t => t.plan_node_id === node.plan_node_id)
  // Open when the address names this card or one of the ordinary steps folded inside it, on the main line and on branches alike (review 629 B1).
  const named = (n: PlanNode) => selected === n.plan_node_id || selected === n.label
  const open = named(node) || steps.some(named)
  const counts = node.execution_progress
  return <li id={planNodeDOMId(node.plan_node_id)} tabIndex={-1} aria-current={current ? 'step' : undefined} className={`min-w-0 scroll-mt-4 rounded-[var(--card-radius)] border p-3 [overflow-wrap:anywhere] ${current || selected === node.plan_node_id || selected === node.label ? 'border-[var(--color-info)] bg-[var(--control-fill)]' : 'border-[var(--card-border)] bg-[var(--card-fill)]'}`}>
    <div className="flex flex-wrap items-center gap-2"><a className="inline-flex min-h-11 items-center font-semibold" href={planNodeHref(view.root, node.plan_node_id)}>{node.label} {node.title}</a>{current && <strong>Current</strong>}</div>
    <p className="m-0 text-sm">{node.lane_kind} · Execution {counts.state} · {counts.done}/{counts.total} steps · Delivery Unknown{node.owner_step && ' · Owner'}{node.effect_step && ' · ★'}</p>
    <p className="m-0 text-sm">Next responsible · {responsibility(node)}</p>
    {item && <p className="m-0 text-xs text-muted-foreground">Parent · {item.parent} · {item.work_id}</p>}
    {dependencies.length > 0 && <ul className="my-2 space-y-1 text-sm">{dependencies.map((edge, i) => <li key={i}>
      {edge.relation === 'blocks' ? 'Waiting on' : 'Related to'} · <a className="inline-flex min-h-11 items-center" href={planNodeHref(edge.to.root, edge.to.node)}>{edge.to.root} / {edge.to.node}</a> · {edge.milestone} → {edge.required_for}<br />{edge.reason} · {edge.relation === 'blocks' ? 'Milestone Unknown' : 'Not blocking'}
    </li>)}</ul>}
    <details open={open || undefined}>
      <summary className="min-h-11 cursor-pointer py-3">Acceptance, steps and evidence</summary>
      {item ? <>
        <ul className="space-y-2">{item.acceptance.map(a => <li key={a.id}>{a.criterion}<span className="block text-xs">{a.target_environment} · {a.rule_ref} · {a.evidence_required.join('; ')}</span></li>)}</ul>
        <p>Plan estimate · {item.estimate.work_days?.join('–') ?? 'Unknown'} work days · {item.estimate.review_rounds} review rounds · {item.estimate.confidence}<br />{item.estimate.reason}</p>
        <ul>{item.evidence.map((e, i) => <li key={i}>{e.classification} · {e.assessment}<span className="block text-xs">{e.source} · {e.observed_at} · {e.live_verified ? 'Verified' : 'Not live verified'}</span></li>)}</ul>
      </> : <p>Unclassified · No adopted acceptance metadata.</p>}
      {ownTasks.length > 0 && <ul aria-label="Tasks on this node" className="space-y-1">{ownTasks.map(t => <li key={t.task_id}><a className="inline-flex min-h-11 items-center" href={goalHref(t.mission_id, t.task_id)}>{t.title} · {t.state}</a></li>)}</ul>}
      <ul className="space-y-2">{steps.map(step => <li key={step.plan_node_id} id={planNodeDOMId(step.plan_node_id)} tabIndex={-1} className="scroll-mt-4">
        <a className="inline-flex min-h-11 items-center" href={planNodeHref(view.root, step.plan_node_id)}>{step.label} {step.title}</a> · {step.state} {step.owner_step && '· Owner'} {step.effect_step && '· ★'}
        {view.tasks.filter(t => t.plan_node_id === step.plan_node_id).map(t => <div key={t.task_id}><a className="inline-flex min-h-11 items-center" href={goalHref(t.mission_id, t.task_id)}>{t.title} · {t.state}</a></div>)}
        <ul>{view.external_work.filter(w => w.plan_node_id === step.plan_node_id).map(w => <ExternalRecord key={w.external_work_id} work={w} />)}</ul>
      </li>)}</ul>
      {work.length > 0 && <ul>{work.map(w => <ExternalRecord key={w.external_work_id} work={w} />)}</ul>}
    </details>
    {children}
  </li>
}
export function PlanTree({ view, selected }: { view: PlanSnapshot; selected: string }) {
  const shown = new Set<string>()
  const path = view.mainline.path.flatMap(p => { const n = view.nodes.find(n => n.plan_node_id === p.plan_node_id); return n ? [n] : [] })
  const render = (node: PlanNode) => {
    shown.add(node.plan_node_id)
    const branches = view.nodes.filter(n => n.view?.anchor_node_id === node.view?.plan_node_id && n.view?.anchor_node_id != null)
    branches.forEach(n => shown.add(n.plan_node_id))
    return <NodeCard key={node.plan_node_id} node={node} view={view} selected={selected}>{branches.length > 0 && <ol className="mt-2 flex list-none flex-col gap-2 border-l-2 border-[var(--table-rule)] pl-3">{branches.map(n => <NodeCard key={n.plan_node_id} node={n} view={view} selected={selected} />)}</ol>}</NodeCard>
  }
  const mainline = path.map(render)
  const other = view.nodes.filter(n => !shown.has(n.plan_node_id) && n.node_kind === 'item').map(render)
  return <section aria-label="Whole plan" className="flex min-w-0 flex-col gap-3">
    <h3 className="text-base font-semibold">Whole plan · {view.root}</h3>
    {path.length === 0 && <p>Main line Unknown · No adopted mainline classification.</p>}
    <ol aria-label="Mainline path" className="m-0 flex list-none flex-col gap-3 border-l-2 border-[var(--table-rule)] pl-3">{mainline}</ol>
    {other.length > 0 && <><h4>Other plan items</h4><ol className="m-0 flex list-none flex-col gap-3 p-0">{other}</ol></>}
  </section>
}
