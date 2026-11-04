import { useEffect, useRef, useState, type ReactNode } from 'react'
import { useQuery } from '@tanstack/react-query'
import { fetchRuntimeProgress } from '@/api/agentRuntime'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { scrollAfterShell } from '@/lib/shell/useHashSection'
import { NowClock, useNow } from '../progress/progressClock'
import { PlanLiveWork } from './PlanLiveWork'
import { PlanModuleDetail } from './PlanModuleDetail'
import { PlanModuleRows } from './PlanModuleRows'
import { useOwnerAsks } from './ownerAsksModel'
import { moduleDetailId, moduleOf, moduleStepRowId, runtimeTasks } from './moduleDetailModel'
import { returnToModuleRow } from './moduleRowReturn'
import { PlanTree } from './PlanTree'
import { PlanRecentChanges } from './PlanRecentChanges'
import { useSignoffWaits, type SignoffWaitsRead } from './useRuntimePlan'
import { viewOldReason } from './readAge'
import { planNodeDOMId, planNodeHref, usePlanAddress, usePlanSnapshot } from './usePlanSnapshot'
import { WorkSignoffs } from './WorkSignoffs'
import { PlanReadFailure, WorkSummaryCard } from './WorkSummaryCard'
import { WorkTabs } from './WorkTabs'
import { useWorkView, type WorkTab } from './workViewModel'
import { dependencyWaits, lastReadRows, signoffCount, type SignoffScope } from './workSummaryModel'
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

/** Work → the round tab: every module as a row, and folded: the dependency waits and the whole plan. */
function RoundTab({ view, selected, failed }: { view: PlanSnapshot; selected: string; failed: string | null }) {
  // GET /progress as the shell already holds it (ConsolePage polls it every 5 s): never fetched here (enabled: false),
  // read only for a running task's role on the rows' Now line.
  const progress = useQuery({ queryKey: ['agent-runtime-progress'], queryFn: ({ signal }) => fetchRuntimeProgress(signal), enabled: false, retry: false })
  const runtime = runtimeTasks(progress.data === undefined ? { state: 'unread' } : { state: 'ok', data: progress.data, at: progress.dataUpdatedAt })
  // The open questions and requests Needs you already polls (every 30 s), from the cache: no request of its own.
  const asks = useOwnerAsks()
  const targetId = selected ? view.nodes.find(n => n.plan_node_id === selected || n.label === selected)?.plan_node_id : undefined
  // An address naming a node of no module (a module or a step of one opens on its own page: ModulePage) opens the
  // whole plan's fold and brings the node into view there.
  const scrollTo = targetId ? planNodeDOMId(targetId) : ''
  useEffect(() => {
    if (scrollTo) return scrollAfterShell(() => document.getElementById(scrollTo))
  }, [scrollTo, selected])
  const waits = dependencyWaits(view, moduleRows(view))
  return <>
    {selected && !targetId && <p role="alert">Node not found in {view.root}: {selected}</p>}
    <PlanModuleRows view={view} selected={selected} failed={failed} runtime={runtime} asks={asks} />
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
    {/* The whole plan's cards (acceptance, steps, evidence), folded: an address naming a node of no module opens the
        fold and is brought into view there. */}
    <Fold title={`Whole plan · acceptance, steps and evidence`} open={!!targetId}>
      <PlanTree view={view} selected={selected} />
    </Fold>
  </>
}

/**
 * Work (R5:5.5-2, Design v3.6 WorkPage): where the round stands in one card, then one of four tabs: the round (its
 * modules), Sign-offs, Live and Changes. Every tab reads the same plan view and the round's own GET /plan for its step
 * sign-offs; only the Sign-offs tab reads every round's GET /plan (SignoffWaiting's read; R5:5.5-4: the round tab no
 * longer reads the earlier rounds' trees every 15 s, and its count says it is the round's only). The tab is in the
 * address (`#work?tab=live`); with no tab, the round. An address naming a module or a step of one (`node=`) opens that
 * module on a page of its own instead (R5:5.5-3).
 */
export function WorkPlanView({ tab: given }: { tab?: WorkTab }) {
  const plan = usePlanAddress()
  const view = useWorkView()
  const tab = given ?? (view.kind === 'tab' ? view.tab : 'round')
  const { root, node } = plan
  const query = usePlanSnapshot(root)
  // An address naming a module, or a step of one, opens that module on its own page (Design v3.6 ItemDetail; R5:5.5-3).
  const opened = tab === 'round' && query.data ? moduleOf(query.data, node) : null
  // The summary's sign-off count: the shown round's GET /plan alone, every round's only on the Sign-offs tab (R5:5.5-4),
  // with GET /progress (which lists the rounds) from the shell's cache: Work polls no progress of its own. A module's
  // page does not show the count: nothing is read for it there.
  const scope = tab === 'signoffs' ? 'every' : 'round'
  const waits = useSignoffWaits(!opened, 'cache', scope === 'every' ? 'every' : { only: root })
  const failed = query.isError || query.isPaused
  // Leaving a module's page (‹ Work, its round in the module path, or Back) puts the keyboard on that module's own
  // link in the rows, and the row where it was on the screen when it was opened (R5:5.5-1, moduleRowReturn).
  const openedId = opened?.item.plan_node_id ?? ''
  const lastOpened = useRef(openedId)
  useEffect(() => {
    const left = lastOpened.current
    lastOpened.current = openedId
    if (!left || openedId) return
    return returnToModuleRow(left)
  }, [openedId])
  return <NowClock><section id="plan" aria-label="Plan and live view" className="flex min-w-0 scroll-mt-4 flex-col gap-2.5">
    {opened && query.data ? <ModulePage query={query} view={query.data} item={opened.item} step={opened.step} failed={failed} />
      : <RoundView plan={plan} tab={tab} query={query} waits={waits} scope={scope} node={node} failed={failed} />}
  </section></NowClock>
}

/**
 * The round's summary and tabs, with the sign-off count taken on the page's clock: a read whose refresh hangs stays a
 * success, so its age is judged here each second (readAge.ts; R5:5.5-4), and an old one is Unknown, never "0 ready".
 */
function RoundView({ plan, tab, query, waits, scope, node, failed }: { plan: ReturnType<typeof usePlanAddress>; tab: WorkTab; query: ReturnType<typeof usePlanSnapshot>
  waits: SignoffWaitsRead; scope: SignoffScope; node: string; failed: boolean }) {
  const now = useNow()
  const count = signoffCount(waits, { view: query.data, failed: failureOf(query), root: plan.root, rounds: plan.rounds, scope, now })
  return <>
    <WorkSummaryCard plan={plan} query={query} count={count} />
    <Tabbed plan={plan} tab={tab} query={query} count={count} node={node} failed={failed} />
  </>
}

/** Why the plan view's latest read failed: offline, or its error; null while it did not. */
const failureOf = (query: ReturnType<typeof usePlanSnapshot>) => query.isPaused ? 'Offline' : query.isError ? query.error?.message ?? 'Request failed' : null

/**
 * A module's own page (Design v3.6 ItemDetail): its path back to Work and its detail, without the round's summary, tabs
 * or rows; the step an address names is open on its timeline and in view. A plan view that can no longer be read is
 * said here as on Work, with the last one marked Stale.
 */
function ModulePage({ query, view, item, step, failed }: { query: ReturnType<typeof usePlanSnapshot>; view: PlanSnapshot; item: PlanNode; step: string; failed: boolean }) {
  const now = useNow()
  const stale = failed || now - Date.parse(view.as_of) > 60_000 || view.freshness.database.status !== 'Fresh'
  const scrollTo = step ? moduleStepRowId(step) : moduleDetailId(item.plan_node_id)
  useEffect(() => scrollAfterShell(() => document.getElementById(scrollTo) ?? document.getElementById(moduleDetailId(item.plan_node_id))), [scrollTo, item.plan_node_id])
  return <>
    {failed && <PlanReadFailure query={query} />}
    <PlanModuleDetail key={item.plan_node_id} view={view} item={item} step={step} stale={stale} failed={failureOf(query)} readAgain={() => { void query.refetch() }} />
  </>
}

function Tabbed({ plan, tab, query, count, node, failed }: { plan: ReturnType<typeof usePlanAddress>; tab: WorkTab; query: ReturnType<typeof usePlanSnapshot>; count: ReturnType<typeof signoffCount>; node: string; failed: boolean }) {
  const now = useNow()
  const view = query.data
  const stale = !!view && (failed || now - Date.parse(view.as_of) > 60_000 || view.freshness.database.status !== 'Fresh')
  return <>
    <WorkTabs plan={plan} tab={tab} count={count} view={view} stale={stale} />
    {/* Every tab below counts from this plan view: when it is not now, the tab says so above its counts (R5:5.5-4). */}
    {view && stale && <p role="status" data-tab-stale className="m-0 text-xs text-muted-foreground [overflow-wrap:anywhere]">
      As last read · {viewOldReason(view, view.root, failureOf(query), now)}. {tab !== 'signoffs' ? 'What this tab counts is that snapshot, not now.'
        : lastReadRows(count) > 0 ? 'Rows listed below as last read, from this plan view or from GET /plan, are not counted.'
        : 'Nothing is listed as last read, and nothing of it is counted.'}</p>}
    {tab === 'signoffs' && <WorkSignoffs count={count} view={view} stale={stale} />}
    {view && tab === 'round' && <RoundTab key={plan.root} view={view} selected={node} failed={failureOf(query)} />}
    {view && tab === 'live' && <PlanLiveWork view={view} stale={stale} />}
    {view && tab === 'changes' && <PlanRecentChanges key={plan.root} view={view} />}
  </>
}
