import { plainTitle } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { noViewReason } from '@/pages/shell/agents/planModuleModel'
import { planNodeHref } from '@/pages/shell/agents/usePlanSnapshot'
import type { ModuleScope } from '@/pages/shell/agents/moduleDetailModel'

/*
 * Design v3.6 ItemDetail (R5:5.5-3): what the adopted plan view says of the module, read from the same plan-view answer
 * (node.view, node.acceptance_status, edges, waiting_on, working, mainline.path). Nothing here is fetched; a module the
 * view document does not list (node.view absent) is Unknown with the reason the module rows give (noViewReason).
 * Claims and sources: WORK_V36_3_CLAIMS in the review notes of R5:5.5-3.
 */

/** "of 4" after "Mainline 1": only when the item's place on mainline.path is its mainline_order (the path is sorted by it). */
export function mainlineOf(view: PlanSnapshot, item: PlanNode): number | null {
  const at = view.mainline.path.findIndex(p => p.plan_node_id === item.plan_node_id)
  return at >= 0 && item.view?.mainline_order === at + 1 ? view.mainline.path.length : null
}

type Reading = NonNullable<PlanNode['acceptance_status']>[number]
export type CriterionVerdict = 'verified' | 'not verified' | 'unknown'
export type Criterion = { id: string; milestone: string; environment: string; criterion: string; evidence: string[]; readings: Reading[]; verdict: CriterionVerdict }
export type AcceptanceView = { state: 'unknown'; reason: string } | { state: 'ok'; criteria: Criterion[]; verified: number; notVerified: number; unknown: number }

/**
 * The module's acceptance criteria from the adopted view, each with the PV2 readings of the bindings on it (by id).
 * A criterion is not verified when a reading is false; else verified when it has readings and every one is true; else
 * Unknown (no binding, or one reads unknown). Each class is counted on its own condition.
 */
export function acceptanceOf(view: PlanSnapshot, item: PlanNode): AcceptanceView {
  if (!item.view) return { state: 'unknown', reason: noViewReason(view) }
  const readings = item.acceptance_status ?? []
  const criteria = item.view.acceptance.map(a => {
    const own = readings.filter(r => r.id === a.id)
    const verdict: CriterionVerdict = own.some(r => r.value === 'false') ? 'not verified'
      : own.length > 0 && own.every(r => r.value === 'true') ? 'verified' : 'unknown'
    return { id: a.id, milestone: a.milestone, environment: a.target_environment, criterion: a.criterion, evidence: a.evidence_required, readings: own, verdict }
  })
  const count = (v: CriterionVerdict) => criteria.filter(c => c.verdict === v).length
  return { state: 'ok', criteria, verified: count('verified'), notVerified: count('not verified'), unknown: count('unknown') }
}

/** The plan view's own estimate for the module (the view document's, not measured), or why there is none. */
export type EstimateView = { state: 'unknown'; reason: string } | { state: 'ok'; days: string; rounds: number; confidence: string; reason: string; source: string }
export function estimateOf(view: PlanSnapshot, item: PlanNode): EstimateView {
  if (!item.view) return { state: 'unknown', reason: noViewReason(view) }
  const e = item.view.estimate
  return { state: 'ok', days: e.work_days?.length ? `${e.work_days.join('–')} work days` : 'work days Unknown', rounds: e.review_rounds, confidence: e.confidence, reason: e.reason, source: e.source }
}

const MILESTONE_HEADING: Record<string, string> = { prod: 'On PROD', stg: 'On STG', dev: 'On DEV', accepted: 'Accepted' }
export type Dependent = { key: string; label: string; title: string; href: string; status: string; requiredFor: string; reason: string }
/** status: the one status every entry of the group shares (a blocking edge's depends only on the module's milestone), else ''. */
export type DependentGroup = { heading: string; related: boolean; status: string; entries: Dependent[] }
export type DependentsView = { state: 'unknown'; reason: string } | { state: 'ok'; groups: DependentGroup[] }

/**
 * What in this round's adopted view waits on the module: the edges its items declare to it (planview/graph.go), the
 * blocking ones by the milestone they need, each with its waiting_on status (Blocked, or milestone Unknown) or, when
 * waiting_on does not hold it, "No wait recorded" (the plan view has no field that says the milestone is verified, so
 * nothing here says so); related edges apart (they do not wait). Other rounds' plans are not read.
 */
export function dependentsOf(view: PlanSnapshot, item: PlanNode): DependentsView {
  if (!item.view) return { state: 'unknown', reason: noViewReason(view) }
  const key = item.view.plan_node_id
  const byKey = new Map(view.nodes.map(n => [n.view?.plan_node_id ?? n.plan_node_id, n]))
  const groups = new Map<string, DependentGroup>()
  view.edges.filter(e => e.to.root === view.root && e.to.node === key).forEach((e, i) => {
    const related = e.relation !== 'blocks'
    const heading = related ? 'Related' : MILESTONE_HEADING[e.milestone] ?? e.milestone
    const group = groups.get(heading) ?? { heading, related, status: '', entries: [] }
    groups.set(heading, group)
    const from = e.from.root === view.root ? byKey.get(e.from.node) : undefined
    const wait = related ? undefined : view.waiting_on.find(w => w.edge.from.root === e.from.root && w.edge.from.node === e.from.node
      && w.edge.to.root === e.to.root && w.edge.to.node === e.to.node && w.edge.milestone === e.milestone)
    const status = related ? e.milestone : wait ? (wait.status === 'Blocked' ? 'Blocked' : 'Milestone Unknown') : 'No wait recorded'
    group.entries.push({ key: `${i}`, label: from?.label ?? `${e.from.root} / ${e.from.node}`, title: from ? plainTitle(from.title) : '', href: planNodeHref(e.from.root, from?.plan_node_id ?? e.from.node),
      status, requiredFor: e.required_for, reason: e.reason })
  })
  for (const g of groups.values()) {
    const statuses = new Set(g.entries.map(d => d.status))
    g.status = g.related ? 'does not wait' : statuses.size === 1 ? [...statuses][0] : ''
  }
  // Waits first (in the order the view lists them), the related edges last.
  return { state: 'ok', groups: [...groups.values()].sort((a, b) => Number(a.related) - Number(b.related)) }
}

/** Registered work now on the module's nodes (plan-view working: running or registered external work). */
export function liveOn(view: PlanSnapshot, scope: ModuleScope): PlanSnapshot['working'] {
  return view.working.filter(w => scope.nodeIds.has(w.plan_node_id))
}
