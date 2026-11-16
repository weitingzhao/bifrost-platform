import { createModuleActivity, type ActivityRow } from '@/domains/plan/observations/moduleActivity'
import type { UseQueryResult } from '@tanstack/react-query'
import { approvalNumbers } from '@/api/agentRuntimePlan'
import { records, text, type RuntimeProgress } from '@/api/agentRuntime'
import { type ApprovalItem } from '@/api/approvals'
import { isLandingAction } from '@/api/landingActions'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { questionNumberLabel, type Question } from '@/api/questions'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { groupOpenApprovals } from '../needs-you/needsYouGroups'
import { SIGNOFF_SECTION_HREF } from '../progress/progressPart'
import { moduleRows, ruleQuestionIds, type ModuleRow, type StepCell } from './planModuleModel'

/*
 * Work → Plan module detail (R5:5.1-7; Design v3.6 ItemDetail): what is left of one module, what needs the Owner,
 * what runs now and next, its steps and what happened lately, computed from reads the Console already makes.
 * Every claim and its source: MODULE_DETAIL_CLAIMS in the review notes of R5:5.1-7.
 */

/** The detail's own element ids: the module, and each step's row (an address naming a step scrolls to it). */
export const moduleDetailId = (id: string) => `module-detail-${id}`
export const moduleStepRowId = (id: string) => `module-detail-step-${id}`

type RecentEvent = PlanSnapshot['recent_changes'][number]

/** A list the shell reads (Needs you's reads): its data when it answered, else why there is none. */
export type ShellRead<T> = { state: 'ok'; data: T; at: number } | { state: 'reading' } | { state: 'unread' } | { state: 'unknown'; reason: string }
/** The four shell reads the detail uses: open questions, requests open and all, and the runtime's progress. */
export type ShellReads = { questions: ShellRead<Question[]>; open: ShellRead<ApprovalItem[]>; all: ShellRead<ApprovalItem[]>; progress: ShellRead<RuntimeProgress> }
/** How often the shell polls each (useNeedsYou): a read older than twice this is stale. */
export const SHELL_POLL_MS = { questions: 30_000, open: 30_000, all: 30_000, progress: 5_000 } as const
export const isStale = (read: ShellRead<unknown>, poll: number, now: number) => read.state === 'ok' && now - read.at > 2 * poll
/** A shell list from its cached query: Unknown while its latest read failed or is paused offline (a list left from before is not now). */
export function shellRead<T>(q: Pick<UseQueryResult<T>, 'isError' | 'error' | 'isPaused' | 'data' | 'dataUpdatedAt' | 'fetchStatus'>): ShellRead<T> {
  if (q.isError) return { state: 'unknown', reason: q.error instanceof Error ? q.error.message : 'Request failed' }
  if (q.isPaused) return { state: 'unknown', reason: 'Offline' }
  if (q.data !== undefined) return { state: 'ok', data: q.data, at: q.dataUpdatedAt }
  return q.fetchStatus === 'fetching' ? { state: 'reading' } : { state: 'unread' }
}

const isRecord = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const str = (v: unknown) => typeof v === 'string' ? v : ''
const cap = (s: string) => s ? s[0].toUpperCase() + s.slice(1) : s
// The controller's word for a step's move: active is not "in progress" (R5:5.5-10: an active step may have nothing running).
const STEP_WORDS: Record<string, string> = { planned: 'Not started', active: 'Active', done: 'Done', stopped: 'Stopped', dropped: 'Dropped' }

/** The module (plan item) an address names: the item itself, or the item a step sits under; null for anything else. */
export function moduleOf(view: PlanSnapshot, selected: string): { item: PlanNode; step: string } | null {
  if (!selected) return null
  const byId = new Map(view.nodes.map(n => [n.plan_node_id, n]))
  const picked = view.nodes.find(n => n.plan_node_id === selected || n.label === selected)
  let node = picked
  const seen = new Set<string>()
  while (node && node.node_kind !== 'item' && node.parent_plan_node_id && !seen.has(node.plan_node_id)) {
    seen.add(node.plan_node_id)
    node = byId.get(node.parent_plan_node_id)
  }
  if (!node || node.node_kind !== 'item') return null
  return { item: node, step: picked?.node_kind === 'step' ? picked.plan_node_id : '' }
}

/** What belongs to one module: its row, every node under it (dropped steps too) and what its rules name. */
export type ModuleScope = {
  item: PlanNode; row: ModuleRow
  /** Every step under the item in plan order, dropped ones too (a link to a dropped step is still this module's). */
  steps: PlanNode[]
  nodeIds: Set<string>; labels: Set<string>
  /** The step whose done rule names a question (question_answered) or a request number (approval_executed). */
  ruleQuestions: Map<string, string>; ruleApprovals: Map<number, string>
}

/**
 * Every module of the plan view, by item id; one pass over its nodes. rows: the module rows as shown (stepStateModel.ts
 * stepRows, each active step by what it has now); without them every active step is Unknown (not judged).
 */
export function moduleScopes(view: PlanSnapshot, shown: ModuleRow[] = moduleRows(view)): Map<string, ModuleScope> {
  const rows = new Map(shown.map(r => [r.id, r]))
  const children = new Map<string, PlanNode[]>()
  view.nodes.forEach(n => { if (n.parent_plan_node_id) children.set(n.parent_plan_node_id, [...(children.get(n.parent_plan_node_id) ?? []), n]) })
  const out = new Map<string, ModuleScope>()
  for (const item of view.nodes.filter(n => n.node_kind === 'item')) {
    const under: PlanNode[] = []
    const walk = (id: string, seen: Set<string>) => {
      if (seen.has(id)) return
      seen.add(id)
      for (const child of [...children.get(id) ?? []].sort((a, b) => a.ordinal - b.ordinal)) { under.push(child); walk(child.plan_node_id, seen) }
    }
    walk(item.plan_node_id, new Set())
    const steps = under.filter(n => n.node_kind === 'step')
    const nodeIds = new Set([item.plan_node_id, ...under.map(n => n.plan_node_id)])
    const ruleQuestions = new Map<string, string>(), ruleApprovals = new Map<number, string>()
    for (const step of steps) {
      for (const q of ruleQuestionIds(step.done_rule_json)) if (!ruleQuestions.has(q)) ruleQuestions.set(q, step.plan_node_id)
      for (const n of approvalNumbers(step.done_rule_json)) if (!ruleApprovals.has(n)) ruleApprovals.set(n, step.plan_node_id)
    }
    out.set(item.plan_node_id, { item, row: rows.get(item.plan_node_id)!, steps, nodeIds, labels: new Set([item.label, ...under.map(n => n.label)]), ruleQuestions, ruleApprovals })
  }
  return out
}

/** The nodes a proposed plan change touches: a node, an added node's parent. */
function touched(e: RecentEvent): string[] {
  const changes = Array.isArray(e.body_json.changes) ? e.body_json.changes : []
  return changes.flatMap(c => isRecord(c) ? [str(c.node), str(c.parent)].filter(Boolean) : [])
}

/** One open card of GET /progress needs_you (store/progressneeds.go): its question and its title. */
export type NeedsYouCard = { question: string; title: string }
function needsYouCards(progress: ShellRead<RuntimeProgress>): { cards: Map<string, NeedsYouCard>; truncated: boolean } | null {
  if (progress.state !== 'ok') return null
  const cards = new Map<string, NeedsYouCard>()
  for (const it of records(progress.data.needs_you.items)) {
    const action = isRecord(it.action) ? it.action : {}
    const question = text(action.question_id)
    if (question) cards.set(question, { question, title: text(it.title) })
  }
  return { cards, truncated: progress.data.needs_you.truncated === true }
}

/**
 * A plan node a card or request names directly, and how: a step whose done rule names it, or a node a plan change it
 * decides touches (a proposal in the snapshot's recent window). Nothing else is evidence: a mission a card holds up is
 * not (two modules can share one), so a card tied only by a mission ties to no module.
 */
export type Link = { step: string; via: string }
function questionEvidence(id: string, view: PlanSnapshot, scopes: Map<string, ModuleScope>): Link[] {
  const out: Link[] = []
  for (const s of scopes.values()) { const rule = s.ruleQuestions.get(id); if (rule) out.push({ step: rule, via: "a step's done rule" }) }
  for (const e of view.recent_changes) {
    if (e.event_type === 'plan_change_proposed' && str(e.body_json.question_id) === id) out.push(...touched(e).map(step => ({ step, via: 'the plan change it decides' })))
  }
  return out
}
function approvalEvidence(a: ApprovalItem, scopes: Map<string, ModuleScope>): Link[] {
  const out: Link[] = []
  for (const s of scopes.values()) { const rule = a.number != null ? s.ruleApprovals.get(a.number) : undefined; if (rule) out.push({ step: rule, via: "a step's done rule" }) }
  return out
}
/** The first evidence inside this module; null when none is (evidence elsewhere, or none at all). */
const within = (evidence: Link[], scope: ModuleScope) => evidence.find(e => scope.nodeIds.has(e.step)) ?? null

/** One thing that waits on the Owner now; counted is false for what this page cannot confirm still needs him. */
export type NeedItem = { key: string; kind: 'signoff' | 'question' | 'approval'; text: string; href: string; step: string; counted: boolean; note: string }
export type NeedsNow = {
  items: NeedItem[]
  /** A list it could not read, said in words ("Questions Unknown · …"). */
  gaps: string[]
  /** The count is a floor: a list is unread, or Needs you's cards were cut at their bound. */
  floor: boolean
}

const readGap = (what: string, read: ShellRead<unknown>) =>
  read.state === 'unknown' ? `${what} Unknown · ${read.reason}` : read.state === 'reading' ? `${what} · reading…` : read.state === 'unread' ? `${what} not read yet` : ''

/** What Needs you asks of the Owner on these requests (its own groups): decide, run it in a terminal, check the result. */
function asksOf(approvals: ApprovalItem[], now: number): [ApprovalItem, string][] {
  const groups = groupOpenApprovals(approvals, now)
  return [...groups.decide.map(a => [a, 'decide'] as [ApprovalItem, string]), ...groups.terminal.map(a => [a, 'run it in a terminal'] as [ApprovalItem, string]),
    ...groups.check.map(a => [a, 'check the result'] as [ApprovalItem, string])]
}

export function needsNow(view: PlanSnapshot, scopes: Map<string, ModuleScope>, scope: ModuleScope, reads: ShellReads, now: number): NeedsNow {
  const items: NeedItem[] = []
  const label = (id: string) => view.nodes.find(n => n.plan_node_id === id)?.label ?? id
  const needs = needsYouCards(reads.progress)
  const cards = needs?.cards ?? new Map<string, NeedsYouCard>()
  for (const w of view.owner_waiting) {
    if (!scope.nodeIds.has(w.plan_node_id)) continue
    items.push({ key: `signoff-${w.ref}`, kind: 'signoff', text: `${label(w.plan_node_id)} · sign off acceptance ${w.acceptance}`, href: SIGNOFF_SECTION_HREF,
      step: w.plan_node_id, counted: true, note: `Ready · plan v${w.plan_version}` })
  }
  if (reads.questions.state === 'ok') {
    for (const q of reads.questions.data) {
      if (q.status !== 'open') continue
      const link = within(questionEvidence(q.id, view, scopes), scope)
      if (link) items.push({ key: `question-${q.id}`, kind: 'question', text: `${questionNumberLabel(q.number)} · ${q.title}`, href: questionHref(q.id), step: link.step,
        counted: true, note: `On ${label(link.step)}, by ${link.via}` })
    }
  } else {
    // Needs you's cards without the question list: whether each is still open is the question store's to say.
    for (const card of cards.values()) {
      const link = within(questionEvidence(card.question, view, scopes), scope)
      if (link) items.push({ key: `question-${card.question}`, kind: 'question', text: card.title, href: questionHref(card.question), step: link.step,
        counted: false, note: `On ${label(link.step)}, by ${link.via} · whether the card is still open is not read` })
    }
  }
  if (reads.open.state === 'ok') {
    for (const [a, ask] of asksOf(reads.open.data, now)) {
      const link = within(approvalEvidence(a, scopes), scope)
      if (!link) continue
      // Whether a landing request is still the Owner's decision only landing/operations says, which this page does not read.
      const unconfirmed = ask === 'decide' && isLandingAction(a.action)
      items.push({ key: `approval-${a.id}`, kind: 'approval', text: `Request #${a.number ?? '?'} · ${a.summary || a.action}`, href: approvalHref(a.id), step: link.step,
        counted: !unconfirmed, note: unconfirmed ? 'Landing request: whether it still needs you is confirmed on Needs you, not here' : `On ${label(link.step)}, by ${link.via} · ${ask}` })
    }
  }
  const gaps = [readGap('Questions', reads.questions), readGap('Requests', reads.open), readGap("Needs you's cards (runtime progress)", reads.progress)].filter(Boolean)
  if (needs?.truncated) gaps.push("Needs you's cards were cut at their bound: some may be missing")
  return { items, gaps, floor: gaps.length > 0 }
}

/**
 * Your actions found (not "left": a card without evidence here is not counted): each item listed under Needs you now
 * that counts, once, plus each Owner step not done (not
 * started, in progress or without a state) with no such item on it, once. A stopped Owner step is said apart.
 */
/** items: what Needs you now counts; notCounted: what it lists without counting (the whole of that list); bareOwnerSteps: Owner steps with nothing counted on them. */
export type ActionsLeft = { total: number; items: number; notCounted: number; ownerSteps: number; bareOwnerSteps: number; stopped: number; floor: boolean }
export function actionsLeft(scope: ModuleScope, needs: NeedsNow): ActionsLeft {
  const open = scope.steps.filter(s => s.owner_step && (s.state === null || s.state === 'planned' || s.state === 'active'))
  const counted = needs.items.filter(n => n.counted)
  const covered = new Set(counted.map(n => n.step))
  const bare = open.filter(s => !covered.has(s.plan_node_id)).length
  return { total: counted.length + bare, items: counted.length, notCounted: needs.items.length - counted.length, ownerSteps: open.length, bareOwnerSteps: bare,
    stopped: scope.steps.filter(s => s.owner_step && s.state === 'stopped').length, floor: needs.floor }
}

/**
 * The Review cell: the verification of the step's latest recorded external work that has one; '' when none has.
 */
export function reviewWords(view: PlanSnapshot, step: string): string {
  const external = view.external_work.filter(w => w.plan_node_id === step && w.verification).sort((a, b) => b.recorded_at.localeCompare(a.recorded_at))[0]
  return external ? `External work · ${external.verification}` : ''
}

/** The steps the controller has active, each with what it has now (in progress, held, nothing running, yours or Unknown) on its cell. */
export function nowSteps(scope: ModuleScope): StepCell[] {
  return scope.row.cells.filter(c => c.step === 'active')
}
/** The first step not started, in plan order: plan order only, not a commitment. */
export function nextInPlanOrder(scope: ModuleScope): StepCell | undefined {
  return scope.row.cells.find(c => c.state === 'planned')
}

export type { ActivityRow } from '@/domains/plan/observations/moduleActivity'
const activityProjection = createModuleActivity({ questionEvidence, approvalEvidence, within, touched, str, cap, isRecord, STEP_WORDS })
export function planActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>): { rows: ActivityRow[] } {
  return activityProjection.planActivity(view, scope, scopes)
}
export function questionActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>, questions: ShellRead<Question[]>): ActivityRow[] {
  return activityProjection.questionActivity(view, scope, scopes, questions)
}
export function approvalActivity(scope: ModuleScope, scopes: Map<string, ModuleScope>, approvals: ShellRead<ApprovalItem[]>): ActivityRow[] {
  return activityProjection.approvalActivity(scope, scopes, approvals)
}
export function mergeActivity(...lists: ActivityRow[][]): ActivityRow[] {
  return activityProjection.mergeActivity(...lists)
}

export { mainlineOf, acceptanceOf, estimateOf, dependentsOf, liveOn } from '@/domains/plan/observations/moduleReadPanels'
export type { CriterionVerdict, Criterion, AcceptanceView, EstimateView, Dependent, DependentGroup, DependentsView } from '@/domains/plan/observations/moduleReadPanels'
