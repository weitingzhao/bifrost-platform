import { useEffect, useRef, useState } from 'react'
import { plainTitle } from '@/api/agentRuntimePlan'
import type { PlanSnapshot } from '@/api/planSnapshot'
import type { AcceptanceSignoff } from '@/api/planSnapshotSchema'
import { goalHref, NEEDS_YOU_HREF } from '@/lib/shell/consoleRoutes'
import { hashNamesSection, scrollAfterShell } from '@/lib/shell/useHashSection'
import { ActionLink } from '../owner/OwnerParts'
import { roundLabel } from './planRoundModel'
import { AcceptanceSignoffQueue } from './SignoffWaiting'
import { SignOffButton, SignOffButtons } from './StepSignoffs'
import { StepBadge, StepTitle } from './StepText'
import { planNodeHref } from './usePlanSnapshot'
import { signoffGaps, signoffsComplete, stepsReach, type SignoffCount, type SignoffRow } from './workSummaryModel'

/** How many of the later rows show before "Show n more". */
const LATER_SHOWN = 8
const panel = 'flex min-w-0 scroll-mt-4 flex-col gap-2.5 rounded-[20px] border border-[var(--ops-edge)] p-3.5 shadow-[var(--ops-shadow)] [background:var(--ops-panel)] [overflow-wrap:anywhere]'
const caps = 'text-xs font-semibold uppercase tracking-[.08em] text-muted-foreground'
const row = 'flex min-w-0 flex-wrap items-center gap-x-3 gap-y-1.5 rounded-[14px] px-3 py-2.5 [background:var(--ops-selection)]'
const chip = (dashed: boolean) => `inline-flex h-[22px] items-center whitespace-nowrap rounded-full px-2 text-xs font-semibold ${dashed ? 'border border-dashed border-[var(--color-lamp-gray)] text-muted-foreground' : 'bg-[var(--control-fill)] text-foreground'}`
const rootOf = (node: string) => node.split(':')[0]
const CLOSED = ['done', 'stopped', 'dropped']

function Evidence({ signoff }: { signoff: AcceptanceSignoff }) {
  if (signoff.candidate_evidence.length === 0) return null
  return <details className="basis-full text-xs"><summary className="cursor-pointer text-muted-foreground pointer-coarse:min-h-11 pointer-coarse:py-3">Candidate evidence · {signoff.candidate_evidence.length}</summary>
    <ul className="m-0 mt-1 list-none space-y-1 p-0">{signoff.candidate_evidence.map(c => <li key={c.artifact_id}>{c.label} · {c.source}<br />{c.artifact_id}</li>)}</ul></details>
}

function ReadyRow({ entry, view, stale }: { entry: SignoffRow; view: PlanSnapshot | undefined; stale: boolean }) {
  if (entry.kind === 'step') {
    const w = entry.wait
    return <li data-signoff-wait={w.node} className={`${row} flex-col items-stretch sm:flex-row sm:items-center`}>
      <div className="flex min-w-0 flex-1 items-center gap-2 text-sm"><StepBadge label={w.label} /><StepTitle text={w.title} /></div>
      <SignOffButtons node={w.node} label={w.label} refs={w.refs} named={w.named} state={w.state} title={w.title} />
    </li>
  }
  if (entry.kind !== 'acceptance') return null
  const s = entry.signoff
  const n = view?.nodes.find(x => x.plan_node_id === s.plan_node_id)
  return <li data-signoff-acceptance={s.ref} className={row}>
    <StepBadge label={entry.label} />
    <span className="min-w-0 flex-[1_1_15rem] text-sm">{plainTitle(entry.title) || s.plan_node_id} <span className="text-muted-foreground">· acceptance {s.acceptance} · plan v{s.plan_version}</span></span>
    <SignOffButton node={s.plan_node_id} label={entry.label} signoffRef={s.ref} named={false} reviewContext={JSON.stringify([s, n?.state, n?.title])} candidates={s.candidate_evidence}
      unavailable={stale ? 'Refresh the plan before signing.' : n && CLOSED.includes(n.state ?? '') ? 'This step is closed.' : undefined} />
    <Evidence signoff={s} />
  </li>
}

function LaterRow({ entry }: { entry: SignoffRow }) {
  const state = entry.kind === 'step' ? 'Not started' : entry.kind === 'acceptance' ? entry.signoff.readiness : 'Readiness Unknown'
  const why = entry.kind === 'step' ? 'Its done rule waits on your sign-off; no work on it has started yet.'
    : entry.kind === 'acceptance' ? `${entry.signoff.reason} · acceptance ${entry.signoff.acceptance}` : entry.reason
  return <li data-signoff-later={entry.node} className={`${row} shadow-[var(--ops-inset)] [background:transparent]`}>
    <StepBadge label={entry.label} />
    <a href={planNodeHref(rootOf(entry.node), entry.node)} className="min-w-0 flex-[1_1_15rem] text-sm text-foreground no-underline hover:underline">{plainTitle(entry.title) || entry.node}</a>
    <span className={chip(state === 'Readiness Unknown')}>{state}</span>
    <span className="basis-full text-xs text-muted-foreground">{why}</span>
    {entry.kind === 'acceptance' && <Evidence signoff={entry.signoff} />}
  </li>
}

/**
 * Work → Sign-offs (Design v3.6 WorkPage): what is ready for the Owner's sign-off now, with its Sign off, and what is
 * later, each from the two reads Work makes (workSummaryModel.ts signoffCount: every round's GET /plan and this round's
 * plan-view). Later is "not marked ready", never "not your turn": a readiness the runtime cannot judge says Unknown.
 * `#work?section=signoff` (Command's way here) opens this tab and brings it into view once it is read.
 */
export function WorkSignoffs({ count, view, stale }: { count: SignoffCount; view: PlanSnapshot | undefined; stale: boolean }) {
  const ref = useRef<HTMLElement>(null)
  const [all, setAll] = useState(false)
  const settled = count.steps !== 'reading' && count.round !== 'reading'
  useEffect(() => {
    if (!settled || !hashNamesSection(window.location.hash, 'signoff')) return
    return scrollAfterShell(() => ref.current)
  }, [settled])
  const gaps = signoffGaps(count)
  const reading = [count.steps === 'reading' && 'the step sign-offs of every round (GET /plan)', count.round === 'reading' && `${count.root ? roundLabel(count.root) : 'the round'}'s plan view`].filter(Boolean)
  const status = <>
    {gaps.map(gap => <p key={gap} role="alert" className="m-0 text-sm">Sign-offs partly Unknown · {gap}: what it lists is left out of the counts here.</p>)}
    {reading.length > 0 && <p role="status" className="m-0 text-sm text-muted-foreground">Reading {reading.join(' and ')}…</p>}
  </>
  if (!view && count.steps !== 'ok') {
    return <section ref={ref} id="signoff" aria-label="Sign-offs" className={panel}>
      {status}
      {gaps.length === 2 && <p className="m-0 text-sm text-muted-foreground">Which steps wait for your sign-off is not known.</p>}
      <div className="flex flex-wrap gap-2"><ActionLink href={NEEDS_YOU_HREF}>Review requests</ActionLink></div>
    </section>
  }
  const complete = signoffsComplete(count)
  const unknown = count.later.filter(r => r.kind === 'owner-step').length
  const later = all ? count.later : count.later.slice(0, LATER_SHOWN)
  const round = count.root ? roundLabel(count.root) : 'the round'
  // A source not read leaves its rows out: a group from it is marked "+" (at least), and says so.
  const stepsPart = count.steps === 'ok' ? '' : '+'
  const roundPart = count.round === 'ok' ? '' : '+'
  return <section ref={ref} id="signoff" aria-label="Sign-offs" className={panel}>
    <div data-signoff-ready={count.ready.length} className="flex items-center gap-2.5 rounded-[14px] px-3 py-2.5 [background:var(--ops-selection)]">
      <span className="font-mono text-[22px] font-semibold" title={complete ? undefined : 'Not every source was read: this counts what was'}>{count.ready.length}{!complete && '+'}</span>
      <div className="flex min-w-0 flex-col">
        <span className="text-[15px] font-semibold">Ready for your sign-off now</span>
        <span className="text-[13px] text-muted-foreground">Steps ({stepsReach(count)}) whose work has started and whose done rule waits on your sign-off (GET /plan), and {count.root ? roundLabel(count.root) : 'the round'}'s acceptance sign-offs its plan view marks Ready{stale && ' · as last read'}.
          {count.uncounted.length > 0 && ` Acceptance sign-offs of ${count.uncounted.join(', ')} are below and not counted here.`}</span>
      </div>
    </div>
    {status}
    {count.ready.length > 0 && <ul aria-label="Ready for your sign-off" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">
      {count.ready.map(entry => <ReadyRow key={`${entry.node}|${entry.kind === 'acceptance' ? entry.signoff.ref : ''}`} entry={entry} view={view} stale={stale} />)}</ul>}
    {count.tasks.length > 0 && view && <>
      <span className={caps}>Tasks waiting on you · {count.tasks.length}</span>
      <ul aria-label="Tasks waiting on you" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">{count.tasks.map(wait => {
        const task = view.tasks.find(t => t.task_id === wait.task_id)
        return <li key={wait.task_id} className={row}><span className="min-w-0 flex-[1_1_15rem] text-sm">{task?.title || wait.task_id} <span className="text-muted-foreground">· {wait.reason}</span></span>
          <ActionLink href={task ? goalHref(task.mission_id, task.task_id) : planNodeHref(view.root, wait.plan_node_id)}>Review task</ActionLink></li>
      })}</ul>
    </>}
    {(count.notStarted.length > 0 || count.steps !== 'ok') && <>
      <span className={`${caps} pt-1`}>Not started · {stepsPart && count.notStarted.length === 0 ? 'Unknown' : `${count.notStarted.length}${stepsPart}`} · {stepsReach(count)}</span>
      <p className="m-0 text-xs text-muted-foreground">Step sign-offs whose done rule waits on you, set ahead of the work (TD-425): no task on the step has started. A step moves up to Ready once one does. From GET /plan ({stepsReach(count)}).</p>
      {count.notStarted.length > 0 && <ul aria-label="Not started sign-offs" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">
        {count.notStarted.map(entry => <LaterRow key={entry.node} entry={entry} />)}</ul>}
    </>}
    <div className="flex flex-wrap items-center justify-between gap-2 pt-1">
      <span className={caps}>Later in {round} · {roundPart ? 'Unknown' : count.later.length} · not marked ready</span>
      {unknown > 0 && <span className={chip(true)} title="Owner steps whose readiness the plan view cannot judge yet (owner_later, readiness Unknown)">Readiness Unknown on {unknown}</span>}
    </div>
    <p className="m-0 text-xs text-muted-foreground">{round}'s plan view only (owner_later); other rounds' later sign-offs and Owner steps are not read here. Blocked: its other conditions or dependencies are not verified. Not started: no work on it and no candidate evidence yet. Readiness Unknown: {view?.freshness.owner_waiting ?? 'the plan view cannot judge it yet'}.</p>
    {count.later.length > 0 ? <ul aria-label="Later sign-offs" className="m-0 flex min-w-0 list-none flex-col gap-2 p-0">
      {later.map(entry => <LaterRow key={`${entry.node}|${entry.kind === 'acceptance' ? entry.signoff.ref : ''}`} entry={entry} />)}</ul>
      : <p className="m-0 text-sm">{count.round === 'ok' ? `None in ${round}'s plan view.` : `${round}'s plan view not read.`}</p>}
    {count.later.length > LATER_SHOWN && !all && <button type="button" onClick={() => setAll(true)}
      className="min-h-11 self-start border-0 bg-transparent p-0 text-sm font-semibold text-[var(--primary)]">Show {count.later.length - LATER_SHOWN} more</button>}
    {count.uncounted.map(root => <div key={root} className="flex min-w-0 flex-col gap-2 pt-1">
      <span className={caps}>Acceptance sign-offs in {roundLabel(root)}</span><AcceptanceSignoffQueue root={root} /></div>)}
    <div className="flex flex-wrap gap-2 pt-1"><ActionLink href={NEEDS_YOU_HREF}>Review requests</ActionLink></div>
  </section>
}
