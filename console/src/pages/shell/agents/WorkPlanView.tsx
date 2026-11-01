import { useEffect, useRef, useState, type ReactNode } from 'react'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { scrollAfterShell } from '@/lib/shell/useHashSection'
import { NowClock, useNow } from '../progress/progressClock'
import { PlanLiveWork } from './PlanLiveWork'
import { PlanModuleDetail } from './PlanModuleDetail'
import { PlanModuleRows } from './PlanModuleRows'
import { moduleDetailId, moduleOf, moduleStepRowId } from './moduleDetailModel'
import { returnToModuleRow } from './moduleRowReturn'
import { PlanTree } from './PlanTree'
import { PlanRecentChanges } from './PlanRecentChanges'
import { useSignoffWaits } from './useRuntimePlan'
import { planNodeDOMId, planNodeHref, usePlanAddress, usePlanSnapshot } from './usePlanSnapshot'
import { WorkSignoffs } from './WorkSignoffs'
import { WorkSummaryCard } from './WorkSummaryCard'
import { WorkTabs } from './WorkTabs'
import { useWorkView, type WorkTab } from './workViewModel'
import { dependencyWaits, signoffCount } from './workSummaryModel'
import { moduleRows } from './planModuleModel'

/** A fold of the round tab: closed, and not drawn, unless opened or the address names what is inside it. */
function Fold({ title, open, children }: { title: string; open: boolean; children: ReactNode }) {
  const [shown, setShown] = useState(open)
  useEffect(() => { if (open) setShown(true) }, [open])
  return <details open={shown} onToggle={e => setShown(e.currentTarget.open)} className="min-w-0 rounded-[16px] px-3.5 shadow-[var(--ops-inset)]">
    <summary className="flex min-h-11 cursor-pointer items-center text-sm font-semibold">{title}</summary>
    {shown && <div className="flex min-w-0 flex-col gap-3 pb-3.5">{children}</div>}
  </details>
}

/** Work → the round tab: an opened module's detail, every module as a row, and folded: the dependency waits and the whole plan. */
function RoundTab({ view, selected, stale }: { view: PlanSnapshot; selected: string; stale: boolean }) {
  const targetId = selected ? view.nodes.find(n => n.plan_node_id === selected || n.label === selected)?.plan_node_id : undefined
  // An address naming a module, or a step of one, opens that module's detail first and brings it into view.
  const opened = moduleOf(view, selected)
  const detailId = opened ? moduleDetailId(opened.item.plan_node_id) : ''
  const scrollTo = opened?.step ? moduleStepRowId(opened.step) : detailId || (targetId ? planNodeDOMId(targetId) : '')
  useEffect(() => {
    if (scrollTo) return scrollAfterShell(() => document.getElementById(scrollTo) ?? (detailId ? document.getElementById(detailId) : null))
  }, [scrollTo, detailId, selected])
  // Leaving a module's detail (‹ Work, its round in the breadcrumb, or Back) puts the keyboard on that module's own
  // link in the rows, and the row where it was on the screen when it was opened.
  const waits = dependencyWaits(view, moduleRows(view))
  const openedId = opened?.item.plan_node_id ?? ''
  const lastOpened = useRef(openedId)
  useEffect(() => {
    const left = lastOpened.current
    lastOpened.current = openedId
    if (!left || openedId) return
    return returnToModuleRow(left)
  }, [openedId])
  return <>
    {opened && <PlanModuleDetail key={opened.item.plan_node_id} view={view} item={opened.item} step={opened.step} stale={stale} />}
    {selected && !targetId && <p role="alert">Node not found in {view.root}: {selected}</p>}
    <PlanModuleRows view={view} selected={selected} />
    <Fold title={waits.state === 'unknown' ? 'Dependency waits · Unknown' : `Dependency waits · ${waits.total}${waits.unread ? ' · some Unknown' : ''}`} open={false}>
      <section aria-label="Waiting on" className="flex min-w-0 flex-col gap-2 text-sm">
        {view.waiting_on.length === 0 && <p className="m-0">{waits.state === 'unknown' ? `Dependencies Unknown · ${waits.reason}.`
          : waits.unread ? `No blocking edges recorded for the modules in the adopted plan view; ${waits.unread} module${waits.unread === 1 ? ' is' : 's are'} not in it, ${waits.unread === 1 ? 'its' : 'their'} dependencies Unknown.`
          : view.freshness.verification === 'Unknown' ? 'No blocking edges recorded. Verification remains Unknown.' : 'Every required milestone is verified.'}</p>}
        <ul className="m-0 list-none space-y-2 p-0">{view.waiting_on.map((wait, i) => <li key={i} className="[overflow-wrap:anywhere]">
          <a className="inline-flex min-h-11 items-center" href={planNodeHref(wait.edge.from.root, wait.edge.from.node)}>{wait.edge.from.node}</a> → <a className="inline-flex min-h-11 items-center" href={planNodeHref(wait.edge.to.root, wait.edge.to.node)}>{wait.edge.to.node}</a> · {wait.edge.milestone} · {wait.status}<br />{wait.edge.reason}<br />{wait.responsible.kind} · {wait.responsible.actor_ref || wait.responsible.seat_ref}<br /><span className="text-xs">{wait.reason} · Evidence {wait.edge.sources.join(', ')}</span>
        </li>)}</ul>
      </section>
    </Fold>
    {/* The whole plan's cards (acceptance, steps, evidence), folded: an address naming a node outside the module rows
        (no module of its own) opens the fold and is brought into view there. */}
    <Fold title={`Whole plan · acceptance, steps and evidence`} open={!!targetId && !opened}>
      <PlanTree view={view} selected={selected} />
    </Fold>
  </>
}

/**
 * Work (R5:5.5-2, Design v3.6 WorkPage): where the round stands in one card, then one of four tabs: the round (its
 * modules), Sign-offs, Live and Changes. Every tab reads the same plan view; the sign-offs also read every round's
 * GET /plan (SignoffWaiting's read). The tab is in the address (`#work?tab=live`); with no tab, the round.
 */
export function WorkPlanView({ tab: given }: { tab?: WorkTab }) {
  const plan = usePlanAddress()
  const view = useWorkView()
  const tab = given ?? (view.kind === 'tab' ? view.tab : 'round')
  const { root, node } = plan
  const query = usePlanSnapshot(root)
  // GET /progress (which lists the rounds) from the shell's cache: Work polls no progress of its own.
  const waits = useSignoffWaits(true, 'cache')
  const failed = query.isError || query.isPaused
  const count = signoffCount(waits, { view: query.data, failed: failed ? (query.isPaused ? 'Offline' : query.error?.message ?? 'Request failed') : null, root, rounds: plan.rounds })
  return <NowClock><section id="plan" aria-label="Plan and live view" className="flex min-w-0 scroll-mt-4 flex-col gap-2.5">
    <WorkSummaryCard plan={plan} query={query} count={count} />
    <Tabbed plan={plan} tab={tab} query={query} count={count} node={node} failed={failed} />
  </section></NowClock>
}

function Tabbed({ plan, tab, query, count, node, failed }: { plan: ReturnType<typeof usePlanAddress>; tab: WorkTab; query: ReturnType<typeof usePlanSnapshot>; count: ReturnType<typeof signoffCount>; node: string; failed: boolean }) {
  const now = useNow()
  const view = query.data
  const stale = !!view && (failed || now - Date.parse(view.as_of) > 60_000 || view.freshness.database.status !== 'Fresh')
  return <>
    <WorkTabs plan={plan} tab={tab} count={count} view={view} stale={stale} />
    {tab === 'signoffs' && <WorkSignoffs count={count} view={view} stale={stale} />}
    {view && tab === 'round' && <RoundTab key={plan.root} view={view} selected={node} stale={stale} />}
    {view && tab === 'live' && <PlanLiveWork view={view} stale={stale} />}
    {view && tab === 'changes' && <PlanRecentChanges key={plan.root} view={view} />}
  </>
}
