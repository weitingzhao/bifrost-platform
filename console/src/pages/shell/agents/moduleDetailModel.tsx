import type { UseQueryResult } from '@tanstack/react-query'
import { approvalNumbers, plainTitle } from '@/api/agentRuntimePlan'
import { records, text, type RuntimeProgress } from '@/api/agentRuntime'
import { isUnsetDecidedAt, type ApprovalItem } from '@/api/approvals'
import { isLandingAction } from '@/api/landingActions'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { optionLabel, questionNumberLabel, type Question } from '@/api/questions'
import { approvalHref, questionHref } from '@/lib/shell/consoleRoutes'
import { groupOpenApprovals } from '../needs-you/needsYouGroups'
import { SIGNOFF_SECTION_HREF } from '../progress/progressPart'
import { moduleRows, noViewReason, ruleQuestionIds, type ModuleRow, type StepCell } from './planModuleModel'
import { planNodeHref } from './usePlanSnapshot'

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

/** One line of Recent activity: when, what kind, what happened, and where it opens. */
export type ActivityRow = { key: string; at: string; kind: string; text: string; href: string; source: string }

/**
 * The plan events of the snapshot's recent window that this module's nodes own. Nothing is counted of the
 * rest: what is not shown here is not said to be anything. A plan change's events in the window (its proposal, its
 * decision, the change applied) are one group, joined by the ids they name (proposal_event_id, decided_event_id; an
 * event whose id is missing or not a positive integer joins no group and stands on its own evidence); the group is this
 * module's when any of them has direct evidence: the nodes the proposal touches, the question the
 * decision answers, the nodes the change added. Then every event of the group in the window is shown.
 */
/** An event id an event names: a positive integer, else 0 (none). */
const eventRef = (v: unknown) => typeof v === 'number' && Number.isSafeInteger(v) && v > 0 ? v : 0

export function planActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>): { rows: ActivityRow[] } {
  const byId = new Map(view.nodes.map(n => [n.plan_node_id, n]))
  const byStateEvent = new Map(view.nodes.filter(n => n.state_event_id != null && scope.nodeIds.has(n.plan_node_id)).map(n => [n.state_event_id!, n]))
  const ordered = [...view.recent_changes].sort((a, b) => a.event_id - b.event_id)
  // Plan changes first: the group of each event and whether any event of a group is this module's.
  const groupOf = new Map<number, string>(), mineGroups = new Set<string>()
  for (const e of ordered) {
    const b = e.body_json
    let group = '', mine = false
    if (e.event_type === 'plan_change_proposed') {
      group = `p${e.event_id}`
      mine = touched(e).some(n => scope.nodeIds.has(n))
    } else if (e.event_type === 'plan_change_decided') {
      const proposal = eventRef(b.proposal_event_id)
      group = proposal ? groupOf.get(proposal) ?? `p${proposal}` : `e${e.event_id}`
      const q = str(b.question_id)
      mine = q !== '' && within(questionEvidence(q, view, scopes), scope) !== null
    } else if (e.event_type === 'plan_changed') {
      const decided = eventRef(b.decided_event_id)
      group = decided ? groupOf.get(decided) ?? `d${decided}` : `e${e.event_id}`
      mine = Array.isArray(b.nodes) && b.nodes.some(n => typeof n === 'string' && scope.nodeIds.has(n))
    }
    if (!group) continue
    groupOf.set(e.event_id, group)
    if (mine) mineGroups.add(group)
  }
  const rows: ActivityRow[] = []
  for (const e of ordered) {
    const b = e.body_json
    const row = (kind: string, words: string, href = '') => rows.push({ key: `plan-${e.event_id}`, at: e.at, kind, text: words, href, source: 'plan' })
    const ours = mineGroups.has(groupOf.get(e.event_id) ?? '')
    switch (e.event_type) {
      case 'plan_node_created': case 'plan_node_retitled': {
        const label = str(b.label)
        if (!scope.labels.has(label)) break
        const node = view.nodes.find(n => n.label === label)
        row(e.event_type === 'plan_node_created' ? 'Created' : 'Retitled', `${label} · ${plainTitle(str(b.title))}`, node ? planNodeHref(view.root, node.plan_node_id) : '')
        break
      }
      case 'plan_node_state_changed': {
        const node = byStateEvent.get(e.event_id)
        if (!node) break
        row('Step', `${node.label} · ${STEP_WORDS[str(b.from)] ?? str(b.from)} → ${STEP_WORDS[str(b.to)] ?? str(b.to)}${str(b.cause) ? ` · ${str(b.cause)}` : ''}`, planNodeHref(view.root, node.plan_node_id))
        break
      }
      case 'plan_change_proposed':
        if (ours) row('Plan change', `Proposed · ${plainTitle(str(b.reason))}`)
        break
      case 'plan_change_decided':
        if (ours) row('Plan change', `${cap(str(b.decision)) || 'Decided'}${str(b.by) ? ` by ${str(b.by)}` : ''}`)
        break
      case 'plan_changed':
        if (ours) row('Plan change', `Applied · ${str(b.change)}`)
        break
      case 'note': {
        const work = isRecord(b.external_work) ? b.external_work : null
        const observation = isRecord(b.plan_observation) ? b.plan_observation : null
        if (work && scope.nodeIds.has(str(work.plan_node_id))) {
          const record = view.external_work.find(w => w.external_work_id === str(work.external_work_id))
          row('External work', `${byId.get(str(work.plan_node_id))?.label ?? ''} · ${str(b.text) || str(work.type)}${record ? ` · ${record.description.task}` : ''}`)
        } else if (observation && scope.nodeIds.has(str(observation.step))) {
          row('Evidence', `${byId.get(str(observation.step))?.label ?? ''} · ${str(observation.binding)} reads ${str(observation.value) || 'Unknown'}`)
        }
        break
      }
    }
  }
  return { rows }
}

/** Questions about this module asked or answered (GET /questions, the shell's read). */
export function questionActivity(view: PlanSnapshot, scope: ModuleScope, scopes: Map<string, ModuleScope>, questions: ShellRead<Question[]>): ActivityRow[] {
  if (questions.state !== 'ok') return []
  return questions.data.flatMap(q => {
    if (!within(questionEvidence(q.id, view, scopes), scope)) return []
    const name = `${questionNumberLabel(q.number)} · ${q.title}`
    const rows: ActivityRow[] = [{ key: `q-${q.id}-asked`, at: q.created_at, kind: 'Question', text: `Asked · ${name}`, href: questionHref(q.id), source: 'questions' }]
    if (q.answer) rows.push({ key: `q-${q.id}-answered`, at: q.answer.at, kind: 'Question', text: `Answered “${q.answer.option ? optionLabel(q, q.answer.option) : q.answer.text ?? ''}” · ${name}`, href: questionHref(q.id), source: 'questions' })
    return rows
  })
}

/** Requests about this module made or decided (GET /approvals?status=all, the shell's read). */
export function approvalActivity(scope: ModuleScope, scopes: Map<string, ModuleScope>, approvals: ShellRead<ApprovalItem[]>): ActivityRow[] {
  if (approvals.state !== 'ok') return []
  return approvals.data.flatMap(a => {
    if (!within(approvalEvidence(a, scopes), scope)) return []
    const name = `Request #${a.number ?? '?'} · ${a.summary || a.action}`
    const rows: ActivityRow[] = [{ key: `a-${a.id}-made`, at: a.created_at, kind: 'Request', text: `Made · ${name}`, href: approvalHref(a.id), source: 'requests' }]
    if (!isUnsetDecidedAt(a.decided_at)) rows.push({ key: `a-${a.id}-decided`, at: a.decided_at!, kind: 'Request', text: `${cap(a.status)} · ${name}`, href: approvalHref(a.id), source: 'requests' })
    return rows
  })
}

/** Every source's rows, newest first; rows with no readable time last. */
export function mergeActivity(...lists: ActivityRow[][]): ActivityRow[] {
  const time = (r: ActivityRow) => { const t = Date.parse(r.at); return Number.isFinite(t) ? t : -Infinity }
  return lists.flat().sort((a, b) => time(b) - time(a) || a.key.localeCompare(b.key))
}

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
