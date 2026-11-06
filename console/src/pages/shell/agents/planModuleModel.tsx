import type { CSSProperties } from 'react'
import { approvalNumbers, plainTitle } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { approvalHref, goalHref, isQuestionId, questionHref } from '@/lib/shell/consoleRoutes'
import type { StepWhyModel } from './explainModel'
import { planNodeHref } from './usePlanSnapshot'

/*
 * Work → Plan module rows (Design v3.6 WorkPage, DATA-MAP P25): one row per plan item, one cell per runtime step in
 * plan order, and how many steps are left. Read from plan-view as it is (nodes[].state, edges, waiting_on, tasks):
 * these are controller step states (execution progress), not acceptance or deployment.
 */

/**
 * A step's state as its cell shows it (R5:5.5-10): done, not started and stopped are the controller's; an active step
 * is shown by what it has now (stepStateModel.ts): in progress (work running or queued), waiting on you (something on
 * it only the Owner moves), held (a task waits on a decision), active with nothing running (idle), the Owner's (yours),
 * or active with its work now Unknown; unknown is a step the runtime holds no state for.
 */
export type CellState = 'done' | 'active' | 'waiting' | 'held' | 'idle' | 'yours' | 'planned' | 'stopped' | 'active-unknown' | 'unknown'
/** How a cell's state is read aloud ("Step 4.1-3: done"); spokenState says the derived ones whole. */
export const cellWords: Record<CellState, string> = {
  done: 'done', active: 'in progress', waiting: 'waiting on you', held: 'active, held', idle: 'active, nothing running now', yours: 'yours', planned: 'not started',
  stopped: 'stopped', 'active-unknown': 'active, what runs on it now is Unknown', unknown: 'unknown',
}
/** How a cell's state is written in its detail and the legend. */
export const cellLabels: Record<CellState, string> = {
  done: 'Done', active: 'In progress', waiting: 'Waiting on you', held: 'Active · held', idle: 'Active · nothing running now', yours: 'Yours', planned: 'Not started',
  stopped: 'Stopped', 'active-unknown': 'Active · Unknown now', unknown: 'Unknown',
}
/**
 * Design v3.6 WorkPage stage bar: solid = done, hatched = in progress, recessed = not started; waiting on you is filled
 * on the attention tone with a heavier ring, held is barred on the attention tone, an active step with nothing running
 * an empty outline (nothing in it), the Owner's filled on the attention tone; an active step whose work now is Unknown is
 * dashed in the primary tone, apart from a step with no state (dashed grey); stopped is hatched red. Ops tokens only
 * (styles/opsAppearance.css).
 */
export const cellStyle: Record<CellState, CSSProperties> = {
  done: { background: 'var(--primary)' },
  active: { background: 'repeating-linear-gradient(135deg, var(--primary) 0 3px, transparent 3px 6px)', boxShadow: 'inset 0 0 0 1px var(--primary)' },
  waiting: { background: 'var(--ops-attention-fill)', boxShadow: 'inset 0 0 0 2px var(--ops-attention-text)' },
  held: { background: 'repeating-linear-gradient(90deg, var(--ops-attention-text) 0 2px, transparent 2px 5px)', boxShadow: 'inset 0 0 0 1px var(--ops-attention-text)' },
  idle: { background: 'transparent', boxShadow: 'inset 0 0 0 1px var(--primary)' },
  yours: { background: 'var(--ops-attention-fill)', boxShadow: 'inset 0 0 0 1px var(--ops-attention-text)' },
  planned: { background: 'var(--muted)', boxShadow: 'inset 0 0 0 1px var(--border), var(--ops-inset)' },
  stopped: { background: 'repeating-linear-gradient(45deg, var(--color-lamp-red) 0 2px, transparent 2px 5px)', boxShadow: 'inset 0 0 0 1px var(--color-lamp-red)' },
  'active-unknown': { background: 'transparent', outline: '1px dashed var(--primary)', outlineOffset: '-1px' },
  unknown: { background: 'transparent', outline: '1px dashed var(--color-lamp-gray)', outlineOffset: '-1px' },
}

/**
 * What an active step has now, derived from the plan view held (stepStateModel.ts judgeStep), each with what made it so:
 * running (a live attempt with its heartbeat confirmed, a landing seen moving, running external work), queued, unknown
 * (the condition tested), held (each item with what holds it; on an Owner step with what the plan view records on it),
 * the Owner's with what the plan view records on it, or idle. Queued and Unknown keep what is held too, so that none is
 * hidden.
 */
/**
 * What holds an active step with nothing running or queued: a task waiting on a decision (blocked, paused), an accepted
 * task with no landing recorded, external work submitted whose verification read failed; or what waits on the Owner
 * (OWNER_KINDS): a task awaiting him, a landing waiting for his grant, an acceptance sign-off Ready, an open question on
 * the step, a request its done rule names awaiting his decision.
 */
export type HeldItem = {
  kind: 'stopped' | 'decision' | 'owner-decision' | 'grant' | 'signoff' | 'question' | 'request' | 'no-landing' | 'verify-failed'
  text: string
}
/** The held items only the Owner moves: a step with any of them, and nothing running, Unknown or queued, waits on him. */
export const OWNER_KINDS: ReadonlySet<HeldItem['kind']> = new Set(['owner-decision', 'grant', 'signoff', 'question', 'request'])
/**
 * External work on the step submitted and not read Verified or failed: its title and its verification as read
 * (Unknown, Stale, or '' when none is recorded). A note beside the step's state, never what holds it.
 */
export type Unverified = { task: string; verification: string }
export type StepNow = (
  | { kind: 'running'; what: string; others: number }
  | { kind: 'queued'; why: string[]; held: string[]; waiting: string[] }
  | { kind: 'unknown'; why: string[]; queued: string[]; held: string[]; waiting: string[] }
  | { kind: 'waiting'; waiting: HeldItem[]; held: HeldItem[]; recorded: string[] | null }
  | { kind: 'held'; held: HeldItem[]; recorded: string[] | null }
  | { kind: 'yours'; recorded: string[] }
  | { kind: 'idle' }
  // unread: the shell's lists that could tie a question or request to the step and were not read now (ownerAsksModel).
) & { unverified?: Unverified[]; unread?: string[] }
/** Judges one active step; moduleRows without one judges none (each active step Unknown: not judged). */
export type StepJudge = (step: PlanNode) => StepNow
/** The controller's state of a step drawn (dropped steps are not drawn); null when the runtime holds none. */
export type ControllerState = 'done' | 'active' | 'planned' | 'stopped' | null
const NOT_JUDGED: StepNow = { kind: 'unknown', why: ['what runs on it now is not judged here (no plan-view clock given)'], queued: [], held: [], waiting: [] }

/** A record a step links to; kind says which page the link opens. */
export type StepLink = { kind: 'task' | 'goal' | 'approval' | 'question' | 'node'; href: string; text: string; note?: string }
/** A done rule in words: one line per atom, an all / any with its parts under it (join says which). */
export type RuleLine = { text: string; children?: RuleLine[]; join?: 'all' | 'any' }
export type StepCell = {
  id: string; label: string; title: string; state: CellState; owner: boolean; effect: boolean
  /** The controller's state (nodes[].state). */
  step: ControllerState
  /** What an active step has now (null for a step not active). */
  now: StepNow | null
  /**
   * Why an active step is not done, from the plan view's explain block (R5:5.5-19, laid out in R5:5.5-20: explainModel.ts
   * stepWhy); absent or null when nothing is said of it.
   */
  why?: StepWhyModel | null
  /** Its done rule is {}: nothing closes it by evidence (R5:5.5-10 c). */
  noRule: boolean
  /** When the step took its state: the time of its state event (state_event_id) while that event is in recent_changes; else null. */
  since: string | null
  /** The done rule in words; empty for the empty rule {}. */
  rule: RuleLine[]
  links: StepLink[]
  /** External work registered on this step, one line each (no page of its own to link to). */
  external: string[]
}
export type ModuleWait = { label: string; href: string; status: string; reason: string }
export type ModuleRow = {
  id: string; label: string; lane: string; title: string; current: boolean; href: string
  /** The steps not dropped, in plan order: a dropped step is neither drawn nor counted. */
  cells: StepCell[]
  /**
   * total = cells.length, the denominator store/plan CountSteps uses (every step not dropped). active: the steps the
   * controller has active, each counted once more by what it has now (running + queued + waiting + held + idle + yours
   * + nowUnknown = active); unknown: the steps with no state.
   */
  total: number; done: number; active: number; stopped: number; unknown: number; dropped: number
  running: number; queued: number; waiting: number; held: number; idle: number; yours: number; nowUnknown: number
  /** Steps to go: every step less the done and the dropped ones (stopped and unknown steps are still to go). */
  toGo: number
  /** The blocking edges from this item whose milestone is not verified yet (waiting_on). */
  waits: ModuleWait[]
  /**
   * The item is in the adopted view document (node.view): only then are its blocking edges read, so no waits means none.
   * The server sets view only when the document parsed and lists the item (store planviewsnapshot.go).
   */
  inView: boolean
}

/** The deepest a done rule nests (plan/rule.go maxDepth). */
const RULE_DEPTH = 8
const isRecord = (v: unknown): v is Record<string, unknown> => v !== null && typeof v === 'object' && !Array.isArray(v)
const str = (v: unknown) => typeof v === 'string' ? v : ''

/** Every value of one atom in a done rule, an atom or nested in all / any as plan/rule.go reads it, in the rule's order. */
function atomValues(rule: unknown, atom: string, depth = 0): unknown[] {
  if (depth > RULE_DEPTH || !isRecord(rule)) return []
  const out: unknown[] = Object.hasOwn(rule, atom) ? [rule[atom]] : []
  for (const key of ['all', 'any']) {
    const list = rule[key]
    if (Array.isArray(list)) for (const child of list) out.push(...atomValues(child, atom, depth + 1))
  }
  return out
}
/** The task ids a done rule names (task_accepted), each once. */
export function ruleTaskIds(rule: unknown): string[] {
  return [...new Set(atomValues(rule, 'task_accepted').map(str).filter(Boolean))]
}
/** The question ids a done rule names (question_answered), each once. */
export function ruleQuestionIds(rule: unknown): string[] {
  return [...new Set(atomValues(rule, 'question_answered').map(q => isRecord(q) ? str(q.question) : '').filter(Boolean))]
}

/** The sign-offs a done rule waits on (owner_signoff), each once. */
export function ruleSignoffs(rule: unknown): string[] {
  return [...new Set(atomValues(rule, 'owner_signoff').map(str).filter(Boolean))]
}

const envName = (v: unknown) => str(v) ? str(v).toUpperCase() : 'an unnamed environment'
const short = (sha: string) => sha.length > 12 ? sha.slice(0, 7) : sha
const bound: Record<string, string> = {
  main_effective: 'the change is effective on main', deployment_verified: 'the deployment is verified',
  review_valid: 'the review is valid', external_verified: 'the external work is verified',
}

/**
 * One part of a done rule in words. The grammar is plan/rule.go's, plus the step atoms of design D3 (step_tasks,
 * step_landed, step_stg_settled, step_deployed); anything else is shown as not recognised, never guessed.
 */
function ruleLine(rule: unknown, taskTitle: (id: string) => string, depth: number): RuleLine {
  if (!isRecord(rule)) return { text: 'Unreadable rule part' }
  if (Object.hasOwn(rule, 'gates_passed')) {
    const tests = Array.isArray(rule.gates_passed) ? rule.gates_passed.map(g => isRecord(g) ? str(g.test) : '').filter(Boolean) : []
    return { text: `Gates ${tests.join(', ') || '(none named)'} pass on ${envName(rule.env)}` }
  }
  const keys = Object.keys(rule)
  if (keys.length !== 1) return { text: `Rule not recognised · ${JSON.stringify(rule)}` }
  const key = keys[0], value = rule[key], fields = isRecord(value) ? value : {}
  switch (key) {
    case 'all': case 'any': {
      const list = Array.isArray(value) ? value : []
      return { text: key === 'all' ? 'All of these hold:' : 'Any one of these holds:', join: key,
        children: depth >= RULE_DEPTH ? [{ text: 'Nested too deep to read' }] : list.map(child => ruleLine(child, taskTitle, depth + 1)) }
    }
    case 'task_accepted': return { text: `Task ${taskTitle(str(value)) || str(value) || '(unnamed)'} is accepted` }
    case 'approval_executed': return { text: `Request #${String(value)} is approved and executed` }
    case 'question_answered': return { text: `Question ${str(fields.question) || '(unnamed)'} is answered “${str(fields.option)}”` }
    case 'ci_succeeded': return { text: `CI passes on ${str(fields.repo) || '(unnamed repo)'} at ${short(str(fields.commit))}` }
    case 'owner_signoff': return { text: `You sign off “${str(value)}”` }
    case 'manual': return { text: `Recorded by hand: ${str(value)}` }
    case 'main_effective': case 'deployment_verified': case 'review_valid': case 'external_verified':
      return { text: `Binding ${str(fields.binding) || '(unnamed)'} v${String(fields.version ?? '?')}: ${bound[key]}` }
    case 'step_tasks': return { text: `At least ${typeof fields.min === 'number' ? fields.min : 1} of this step's own tasks ${str(fields.state) || 'accepted'}` }
    case 'step_landed': return { text: 'Every accepted task of this step has landed on main, or you declined it and the same change landed from another task of the step' }
    case 'step_stg_settled': return { text: 'Every landed change of this step has settled on STG (a repository with no STG release, such as infra, counts once landed)' }
    case 'step_deployed': return { text: `Every landed commit of this step is deployed on ${envName(fields.env)}` }
  }
  return { text: `Rule not recognised · ${key}` }
}
/** A done rule in words; [] for the empty rule {} (no condition: the step moves only by a recorded event). */
export function ruleWords(rule: unknown, taskTitle: (id: string) => string = () => ''): RuleLine[] {
  if (isRecord(rule) && Object.keys(rule).length === 0) return []
  return [ruleLine(rule, taskTitle, 0)]
}

/** A part of a sentence after its first: lower-cased at its start unless it starts with a name ("CI", "STG"). */
const later = (s: string) => /^[A-Z][a-z]/.test(s) ? `${s[0].toLowerCase()}${s.slice(1)}` : s
/**
 * A done rule in words as one sentence (the step card's "Done when", R5:5.5-20): its parts joined with "and" for all,
 * "or" for any, a nested group in parentheses; '' for the empty rule.
 */
export function ruleSentence(lines: RuleLine[]): string {
  const part = (l: RuleLine): string => {
    if (!l.children) return l.text
    const parts = l.children.map((c, i) => { const t = i === 0 ? part(c) : later(part(c)); return c.children ? `(${t})` : t })
    return parts.join(l.join === 'any' ? ', or ' : ', and ')
  }
  const text = lines.map(part).join('; ')
  return text ? `${text}.` : ''
}

/** The empty rule {}: no condition, so nothing closes the step by evidence. */
const isEmptyRule = (rule: unknown) => isRecord(rule) && Object.keys(rule).length === 0

/** The cell's state: the controller's, an active step's by what it has now. */
export function shownState(step: ControllerState, now: StepNow | null): CellState {
  if (step === null) return 'unknown'
  if (step !== 'active') return step
  const kind = (now ?? NOT_JUDGED).kind
  return kind === 'running' || kind === 'queued' ? 'active' : kind === 'unknown' ? 'active-unknown' : kind
}

/** A row's counts from its cells: each by its own condition, none a total less the others. */
function rowCounts(cells: StepCell[]) {
  const count = (s: CellState) => cells.filter(c => c.state === s).length
  const nowCount = (k: StepNow['kind']) => cells.filter(c => c.now?.kind === k).length
  const done = count('done')
  return { total: cells.length, done, active: cells.filter(c => c.step === 'active').length, running: nowCount('running'), queued: nowCount('queued'),
    waiting: nowCount('waiting'), held: nowCount('held'), idle: nowCount('idle'), yours: nowCount('yours'), nowUnknown: nowCount('unknown'), stopped: count('stopped'),
    unknown: cells.filter(c => c.step === null).length, toGo: cells.length - done }
}

/**
 * One row with its active steps judged again (the module page: its row judged each second on the page clock while the
 * rest of the module's scope is built once per plan view).
 */
export function judgeRow(view: PlanSnapshot, row: ModuleRow, judge: StepJudge): ModuleRow {
  const cells = row.cells.map(c => {
    const step = c.step === 'active' ? view.nodes.find(n => n.plan_node_id === c.id) : undefined
    if (!step) return c
    const now = judge(step)
    return { ...c, now, state: shownState('active', now) }
  })
  return { ...row, cells, ...rowCounts(cells) }
}

/** When the step took its state, from its state event while the plan's recent window holds it (no other field says). */
function stateSince(view: PlanSnapshot, step: PlanNode): string | null {
  if (step.state_event_id == null) return null
  const e = view.recent_changes.find(r => r.event_id === step.state_event_id)
  return e && e.event_type === 'plan_node_state_changed' && e.body_json.to === step.state ? e.at : null
}
function milestoneWords(milestone: string) {
  const env: Record<string, string> = { prod: 'on PROD', stg: 'on STG', dev: 'on DEV' }
  return env[milestone] ?? (milestone === 'accepted' ? 'accepted' : `· ${milestone}`)
}
function laneWords(node: PlanNode) {
  if (node.lane_kind === 'mainline') return `Mainline${node.view?.mainline_order != null ? ` ${node.view.mainline_order}` : ''}`
  return node.lane_kind === 'Unclassified' ? 'Unclassified' : node.lane_kind[0].toUpperCase() + node.lane_kind.slice(1)
}

/** What a step links to: its tasks, its goal, the requests and questions its done rule waits on, and its place in the plan. */
export function stepLinks(view: PlanSnapshot, step: PlanNode): StepLink[] {
  const links: StepLink[] = []
  const own = view.tasks.filter(t => t.plan_node_id === step.plan_node_id)
  const named = ruleTaskIds(step.done_rule_json).flatMap(id => view.tasks.filter(t => t.task_id === id && !own.includes(t)))
  const tasks = [...own, ...named]
  for (const t of tasks) {
    const landing = t.landing ? `Landing ${t.landing.operation} · ${t.landing.phase} · ${t.landing.outcome}${t.landing.main_commit ? ` · main ${short(t.landing.main_commit)}` : ''}` : ''
    links.push({ kind: 'task', href: goalHref(t.mission_id, t.task_id), text: `Task · ${t.title || t.task_id}`, note: [t.state, t.delivery, landing].filter(Boolean).join(' · ') })
  }
  // The step's own goal, when none of its tasks already opens that goal's card.
  if (step.mission_id && !tasks.some(t => t.mission_id === step.mission_id)) {
    links.push({ kind: 'goal', href: goalHref(step.mission_id), text: `Goal ${step.mission_id}` })
  }
  for (const n of approvalNumbers(step.done_rule_json)) links.push({ kind: 'approval', href: approvalHref(String(n)), text: `Request #${n}` })
  for (const q of ruleQuestionIds(step.done_rule_json).filter(isQuestionId)) links.push({ kind: 'question', href: questionHref(q), text: `Question ${q}` })
  // The step's address opens its module's detail with the step marked (Work → Plan, R5:5.1-7).
  const item = view.nodes.find(n => n.plan_node_id === step.parent_plan_node_id)
  links.push({ kind: 'node', href: planNodeHref(view.root, step.plan_node_id), text: item ? `Open in module ${item.label}` : 'Open in its module' })
  return links
}

/** An item's steps as plan-view counts them (store viewExecutionSummary): every step under it, children in plan order. */
function itemSteps(id: string, children: Map<string, PlanNode[]>, seen = new Set<string>()): PlanNode[] {
  if (seen.has(id)) return []
  seen.add(id)
  return (children.get(id) ?? []).flatMap(child => child.node_kind === 'step' ? [child] : itemSteps(child.plan_node_id, children, seen))
}

/**
 * One row per plan item: the mainline first in its order, then the other items in plan order. An active step is shown
 * by what judge says it has now (stepStateModel.ts stepRows); with no judge, as Unknown (not judged), never in progress.
 */
export function moduleRows(view: PlanSnapshot, judge?: StepJudge): ModuleRow[] {
  const children = new Map<string, PlanNode[]>()
  view.nodes.forEach(n => { if (n.parent_plan_node_id) children.set(n.parent_plan_node_id, [...(children.get(n.parent_plan_node_id) ?? []), n]) })
  for (const list of children.values()) list.sort((a, b) => a.ordinal - b.ordinal)
  const byKey = new Map(view.nodes.map(n => [n.view?.plan_node_id ?? n.plan_node_id, n]))
  const mainline = new Map(view.mainline.path.map((p, i) => [p.plan_node_id, i]))
  const rank = (n: PlanNode) => mainline.get(n.plan_node_id) ?? mainline.size
  const items = view.nodes.filter(n => n.node_kind === 'item').sort((a, b) => rank(a) - rank(b) || a.ordinal - b.ordinal)
  const taskTitle = (id: string) => view.tasks.find(t => t.task_id === id)?.title ?? ''
  return items.map(item => {
    const steps = itemSteps(item.plan_node_id, children)
    const cells: StepCell[] = steps.flatMap(step => {
      if (step.state === 'dropped') return []
      const now = step.state === 'active' ? judge ? judge(step) : NOT_JUDGED : null
      return [{ id: step.plan_node_id, label: step.label, title: plainTitle(step.title), state: shownState(step.state, now), owner: step.owner_step, effect: step.effect_step,
        step: step.state, now, noRule: isEmptyRule(step.done_rule_json), since: stateSince(view, step),
        rule: ruleWords(step.done_rule_json, taskTitle), links: stepLinks(view, step),
        external: view.external_work.filter(w => w.plan_node_id === step.plan_node_id).map(w => `${w.description.task} · ${w.description.executor} · ${w.status}${w.verification ? ` · ${w.verification}` : ''}`) }]
    })
    const key = item.view?.plan_node_id ?? item.plan_node_id
    const seen = new Set<string>()
    const waits = view.waiting_on.filter(w => w.edge.from.root === view.root && w.edge.from.node === key).flatMap(w => {
      const to = w.edge.to.root === view.root ? byKey.get(w.edge.to.node)?.label ?? w.edge.to.node : `${w.edge.to.root} / ${w.edge.to.node}`
      const label = `${to} ${milestoneWords(w.edge.milestone)}`
      if (seen.has(label)) return []
      seen.add(label)
      return [{ label, href: planNodeHref(w.edge.to.root, w.edge.to.node), status: w.status, reason: w.edge.reason }]
    })
    return { id: item.plan_node_id, label: item.label, lane: laneWords(item), title: plainTitle(item.title), href: planNodeHref(view.root, item.plan_node_id),
      current: view.mainline.current?.plan_node_id === item.plan_node_id, cells, ...rowCounts(cells), dropped: steps.length - cells.length, waits, inView: item.view != null }
  })
}

/**
 * Why an item's dependencies are not known (it has no node.view): the view document could not be read or there is
 * none (the server's warning says which), or the document does not list the item.
 */
export function noViewReason(view: PlanSnapshot): string {
  return view.warnings.find(w => /view document unavailable|has no adopted view/.test(w))
    ?? (view.artifact_id === '' ? 'no adopted plan view' : 'this module is not in the adopted plan view')
}

/** The id of a module row's own link: the keyboard goes back to it when the module's detail closes. */
export const moduleRowLinkId = (id: string) => `module-row-${id}`

/** "N steps to go", "All steps done" at 0, "No steps" for an item without a step to count. */
export function toGoText(row: Pick<ModuleRow, 'total' | 'toGo'>): string {
  if (row.total === 0) return 'No steps'
  if (row.toGo === 0) return 'All steps done'
  return `${row.toGo} step${row.toGo === 1 ? '' : 's'} to go`
}

/*
 * What an active step has now, in words (R5:5.5-10). Claims and sources: the R5:5.5-10 hand-in report.
 * "In progress" is said only of work running or queued; an active step with neither, read now, is "Active · nothing
 * running"; anything not judged is Unknown with the condition tested, never idle.
 */

/** The active steps of a row by what they have now, each part only when it holds any: "1 running", "2 idle", "1 active, Unknown now". */
export function activeCounts(row: Pick<ModuleRow, 'running' | 'queued' | 'waiting' | 'held' | 'idle' | 'yours' | 'nowUnknown'>): string[] {
  return [row.running && `${row.running} running`, row.queued && `${row.queued} queued`, row.waiting && `${row.waiting} waiting on you`,
    row.held && `${row.held} held`, row.idle && `${row.idle} idle`, row.yours && `${row.yours} yours`,
    row.nowUnknown && `${row.nowUnknown} active, Unknown now`].filter((p): p is string => !!p)
}
/** A row's steps by state, each step once: "2 / 5 done · 1 running · 1 idle · 1 stopped · 1 unknown · 1 dropped". */
export function stepCounts(row: ModuleRow): string {
  return [`${row.done} / ${row.total} done`, ...activeCounts(row), row.stopped && `${row.stopped} stopped`,
    row.unknown && `${row.unknown} unknown`, row.dropped && `${row.dropped} dropped`].filter(Boolean).join(' · ')
}

/** The "No done rule" pill of a step's detail and the module page's timeline: the attention tone, Ops tokens only. */
export const NO_RULE_PILL = 'shrink-0 rounded-full px-1.5 text-xs font-semibold text-[var(--ops-attention-text)] [background:var(--ops-attention-fill)]'
/** A done rule {} is marked on the cell only while the step is active or stopped (a planned one only in its detail). */
export const noRuleMarked = (cell: Pick<StepCell, 'noRule' | 'step'>) => cell.noRule && (cell.step === 'active' || cell.step === 'stopped')
/** Under "Done when" for the empty rule {}. */
export function noRuleText(cell: Pick<StepCell, 'noRule' | 'step'>): string {
  return `${noRuleMarked(cell) ? 'No done rule: nothing closes it by evidence.' : 'No done rule yet.'} The runtime does not move this step on evidence; it changes only by a recorded event.`
}

const oneOrMany = (n: number, one: string, many: string) => n === 1 ? one : many
/** Each kind of held item aloud, counted; and what moves it on, for the detail. */
const HELD_ORDER: HeldItem['kind'][] = ['owner-decision', 'grant', 'signoff', 'question', 'request', 'stopped', 'decision', 'no-landing', 'verify-failed']
const HELD_SPOKEN: Record<HeldItem['kind'], (n: number) => string> = {
  'owner-decision': n => `${n} ${oneOrMany(n, 'task awaits', 'tasks await')} your decision`,
  signoff: n => `${n} acceptance ${oneOrMany(n, 'sign-off', 'sign-offs')} Ready for you`,
  question: n => `${n} open ${oneOrMany(n, 'question', 'questions')} for you`,
  request: n => `${n} ${oneOrMany(n, 'request awaits', 'requests await')} your decision`,
  stopped: n => `${n} ${oneOrMany(n, 'task is', 'tasks are')} stopped`,
  decision: n => `${n} ${oneOrMany(n, 'task waits', 'tasks wait')} on a decision`,
  grant: n => `${n} ${oneOrMany(n, 'landing waits', 'landings wait')} for your grant`,
  'no-landing': n => `${n} accepted ${oneOrMany(n, 'task', 'tasks')} with no landing recorded`,
  'verify-failed': n => `${n} external ${oneOrMany(n, 'work', 'works')} failed verification`,
}
const HELD_MOVES: Record<HeldItem['kind'], string> = {
  'owner-decision': 'a task awaiting you moves when you decide on its card (Needs you lists it)',
  signoff: 'an acceptance sign-off Ready is yours to give',
  question: 'an open question moves when you answer it (Needs you lists questions)',
  request: 'a request awaiting your decision moves when you approve or reject it (Needs you lists requests)',
  stopped: 'only you can lift a stopped task',
  decision: 'a held task moves only when it is made ready again or cancelled',
  grant: 'a landing awaiting the grant waits on your grant (Needs you lists grants)',
  'no-landing': 'whether an accepted task lands the plan view does not say: it may never land',
  'verify-failed': 'a submitted external work has a verification result of Verification failed',
}
/** What was read for an Owner step's records: the plan view's waits on it and its done rule, nothing else. */
const RECORD_SOURCES = 'none in plan-view owner_waiting or its done rule'

/**
 * A cell's state aloud: a held step says how many tasks wait on a decision, an Owner step what the plan view records
 * on it (or the sources it read when none), never that nothing asks the Owner.
 */
export function spokenState(cell: StepCell): string {
  const now = cell.now
  const counted = (list: HeldItem[]) => HELD_ORDER.flatMap(k => { const n = list.filter(h => h.kind === k).length; return n ? [HELD_SPOKEN[k](n)] : [] }).join(', ')
  if (now?.kind === 'yours') return now.recorded.length ? `yours, recorded on it: ${now.recorded.join(', ')}` : `yours, ${RECORD_SOURCES}`
  if (now?.kind === 'waiting') return `waiting on you: ${counted(now.waiting)}${now.held.length ? `; held: ${counted(now.held)}` : ''}`
  if (now?.kind === 'held') return `active, held: ${counted(now.held)}`
  return cellWords[cell.state]
}

/*
 * What an active step has now, in words (R5:5.5-10). Claims and sources: the R5:5.5-10 hand-in reports.
 * "In progress" is said only of work running or queued; an active step with neither, read now, waits on you while
 * something on it is only yours to move, is held while a task on it waits on a decision, else "Active · nothing
 * running"; anything not judged is Unknown with the condition tested. Questions and requests are read from the shell's
 * lists: when those were not read now, nothing is said of them but that.
 */
/**
 * How an active step with nothing running, queued, waiting on you or held was read (R5:5.5-20, Design v3.15 "How “state”
 * was read"): what each of the four was read from, one row each, the conditions judgeNow tests. Folded on the card; the
 * state word says the result.
 */
export type ReadRow = { label: 'Running' | 'Queued' | 'Waits on you' | 'Held'; text: string }
/** What "nothing waits on you" was read from: the plan view always, the shell's lists only when they were read now. */
const nothingYours = (unread: boolean) => unread
  ? 'Nothing in the plan view: no task awaiting your decision, no landing awaiting your grant, no acceptance sign-off Ready for it. Questions and requests were not read now.'
  : 'Nothing: no task awaiting your decision, no landing awaiting your grant, no acceptance sign-off Ready for it, no open question on its tasks or named by its done rule, no request its done rule names awaiting your decision.'
/** The four rows of an idle step or an Owner step read now; null for any other (its own lines say what it has). */
export function howRead(cell: Pick<StepCell, 'now'>): ReadRow[] | null {
  const now = cell.now
  if (now?.kind !== 'idle' && now?.kind !== 'yours') return null
  return [
    { label: 'Running', text: 'Nothing: no running attempt with its heartbeat confirmed, no landing seen moving, no running external work.' },
    { label: 'Queued', text: 'Nothing: no task proposed, ready, back for rework, submitted or being verified; no landing parked; no external work registered and not started.' },
    { label: 'Waits on you', text: nothingYours(!!now.unread?.length) },
    { label: 'Held', text: 'Nothing: no task blocked or paused; no accepted work task with no landing recorded; no external work whose verification read failed.' },
  ]
}
const heldLine = (held: string[], lead = 'Held') => held.length ? [`${lead}: ${held.join('; ')}.`] : []
/** The shell's lists not read now: what the step's state could not include. */
const unreadLine = (now: StepNow) => now.unread?.length
  ? [`Not read now: ${now.unread.join('; ')}. Whether a question or request of yours waits on this step is Unknown.`] : []
/** An Owner step's records: "Yours ·" while that is its state, "Owner step ·" beside another (held, waiting). */
const recordedLines = (recorded: string[], lead: string) => [recorded.length
  ? `${lead} · recorded on it: ${recorded.join('; ')}. Whether each still asks you is on Needs you.`
  : `${lead} · ${RECORD_SOURCES}: no wait for you listed on it, and no question, request or sign-off named.`]
/** What moves each kind of item on, once each, as one clause. */
const movesOf = (list: HeldItem[]) => {
  const moves = [...new Set(HELD_ORDER.filter(k => list.some(h => h.kind === k)).map(k => HELD_MOVES[k]))].join('; ')
  return `${moves[0].toUpperCase()}${moves.slice(1)}`
}

/**
 * What the detail says of an active step's state now, line by line; [] for a step not active. An idle step and an Owner
 * step read now say how in howRead's four rows, not here.
 */
export function nowLines(cell: StepCell): string[] {
  const now = cell.now
  if (!now) return []
  const since = cell.since ? [`Active since ${cell.since} (its state event).`] : []
  switch (now.kind) {
    case 'running': return [`In progress · running now: ${now.what}${now.others > 0 ? ` (+${now.others} more)` : ''}.`, ...since]
    case 'queued': return [`In progress · queued: ${now.why.join('; ')}. Nothing on it runs now.`, ...heldLine(now.waiting, 'Waiting on you'),
      ...heldLine(now.held), ...unreadLine(now), ...since]
    case 'unknown': return [`Unknown · ${now.why.join('; ')}.`, ...(now.queued.length ? [`Queued as read: ${now.queued.join('; ')}.`] : []),
      ...heldLine(now.waiting, 'Waiting on you'), ...heldLine(now.held), ...unreadLine(now), ...since]
    case 'waiting': return [`Waiting on you: ${now.waiting.map(h => h.text).join('; ')}. ${movesOf(now.waiting)}; nothing on the step runs or is queued.`,
      ...heldLine(now.held.map(h => h.text), 'Also held'), ...(now.recorded ? recordedLines(now.recorded, 'Owner step') : []), ...unreadLine(now), ...since]
    case 'held': return [`Active · held: ${now.held.map(h => h.text).join('; ')}. ${movesOf(now.held)}; nothing on the step runs or is queued.`,
      ...(now.recorded ? recordedLines(now.recorded, 'Owner step') : []), ...unreadLine(now), ...since]
    case 'yours': return [...recordedLines(now.recorded, 'Yours'), ...unreadLine(now), ...since]
    case 'idle': return [...unreadLine(now), ...since]
  }
}

/** The most of a title the note quotes (a title may run to 2000 bytes: externalwork.go); the step's external work list has it whole. */
const NOTE_TITLE = 80
/**
 * The note beside an active step's state for its external work submitted and not read Verified or failed: how many,
 * the latest title (cut at NOTE_TITLE characters), +N for the rest; null when there is none. It says what was read,
 * never that the step waits on it.
 */
export function unverifiedNote(cell: Pick<StepCell, 'now'>): string | null {
  const list = cell.now?.unverified ?? []
  if (list.length === 0) return null
  const read = [...new Set(list.map(w => w.verification || 'not recorded'))].join(', ')
  const chars = [...list[0].task]
  const title = chars.length > NOTE_TITLE ? `${chars.slice(0, NOTE_TITLE).join('')}…` : list[0].task
  return `${list.length} external ${oneOrMany(list.length, 'work', 'works')} submitted, not read Verified (verification ${read}): “${title}”${list.length > 1 ? ` +${list.length - 1}` : ''}`
}
