import { approvalNumbers, plainTitle } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { approvalHref, goalHref, isQuestionId, questionHref } from '@/lib/shell/consoleRoutes'
import { planNodeHref } from './usePlanSnapshot'

/*
 * Work → Plan module rows (Design v3.6 WorkPage, DATA-MAP P25): one row per plan item, one cell per runtime step in
 * plan order, and how many steps are left. Read from plan-view as it is (nodes[].state, edges, waiting_on, tasks):
 * these are controller step states (execution progress), not acceptance or deployment.
 */

/** A step's state as its cell shows it: the controller's state, or unknown when the runtime holds none. */
export type CellState = 'done' | 'active' | 'planned' | 'stopped' | 'unknown'
/** How a cell's state is read aloud ("Step 4.1-3: done"). */
export const cellWords: Record<CellState, string> = { done: 'done', active: 'in progress', planned: 'not started', stopped: 'stopped', unknown: 'unknown' }
/** How a cell's state is written in its detail. */
export const cellLabels: Record<CellState, string> = { done: 'Done', active: 'In progress', planned: 'Not started', stopped: 'Stopped', unknown: 'Unknown' }

/** A record a step links to; kind says which page the link opens. */
export type StepLink = { kind: 'task' | 'goal' | 'approval' | 'question' | 'node'; href: string; text: string; note?: string }
/** A done rule in words: one line per atom, an all / any with its parts under it. */
export type RuleLine = { text: string; children?: RuleLine[] }
export type StepCell = {
  id: string; label: string; title: string; state: CellState; owner: boolean; effect: boolean
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
  /** total = cells.length, the denominator store/plan CountSteps uses (every step not dropped). */
  total: number; done: number; active: number; stopped: number; unknown: number; dropped: number
  /** Steps to go: every step less the done and the dropped ones (stopped and unknown steps are still to go). */
  toGo: number
  /** The blocking edges from this item whose milestone is not verified yet (waiting_on). */
  waits: ModuleWait[]
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
      return { text: key === 'all' ? 'All of these hold:' : 'Any one of these holds:',
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
    case 'step_landed': return { text: 'Every accepted task of this step has landed on main' }
    case 'step_stg_settled': return { text: 'Every landed change of this step has settled on STG' }
    case 'step_deployed': return { text: `Every landed commit of this step is deployed on ${envName(fields.env)}` }
  }
  return { text: `Rule not recognised · ${key}` }
}
/** A done rule in words; [] for the empty rule {} (no condition: the step moves only by a recorded event). */
export function ruleWords(rule: unknown, taskTitle: (id: string) => string = () => ''): RuleLine[] {
  if (isRecord(rule) && Object.keys(rule).length === 0) return []
  return [ruleLine(rule, taskTitle, 0)]
}

function cellState(state: PlanNode['state']): CellState | 'dropped' {
  return state === null ? 'unknown' : state
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
  links.push({ kind: 'node', href: planNodeHref(view.root, step.plan_node_id), text: 'Open in the whole plan' })
  return links
}

/** An item's steps as plan-view counts them (store viewExecutionSummary): every step under it, children in plan order. */
function itemSteps(id: string, children: Map<string, PlanNode[]>, seen = new Set<string>()): PlanNode[] {
  if (seen.has(id)) return []
  seen.add(id)
  return (children.get(id) ?? []).flatMap(child => child.node_kind === 'step' ? [child] : itemSteps(child.plan_node_id, children, seen))
}

/** One row per plan item: the mainline first in its order, then the other items in plan order. */
export function moduleRows(view: PlanSnapshot): ModuleRow[] {
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
      const state = cellState(step.state)
      if (state === 'dropped') return []
      return [{ id: step.plan_node_id, label: step.label, title: plainTitle(step.title), state, owner: step.owner_step, effect: step.effect_step,
        rule: ruleWords(step.done_rule_json, taskTitle), links: stepLinks(view, step),
        external: view.external_work.filter(w => w.plan_node_id === step.plan_node_id).map(w => `${w.description.task} · ${w.description.executor} · ${w.status}${w.verification ? ` · ${w.verification}` : ''}`) }]
    })
    const count = (s: CellState) => cells.filter(c => c.state === s).length
    const key = item.view?.plan_node_id ?? item.plan_node_id
    const seen = new Set<string>()
    const waits = view.waiting_on.filter(w => w.edge.from.root === view.root && w.edge.from.node === key).flatMap(w => {
      const to = w.edge.to.root === view.root ? byKey.get(w.edge.to.node)?.label ?? w.edge.to.node : `${w.edge.to.root} / ${w.edge.to.node}`
      const label = `${to} ${milestoneWords(w.edge.milestone)}`
      if (seen.has(label)) return []
      seen.add(label)
      return [{ label, href: planNodeHref(w.edge.to.root, w.edge.to.node), status: w.status, reason: w.edge.reason }]
    })
    const done = count('done')
    return { id: item.plan_node_id, label: item.label, lane: laneWords(item), title: plainTitle(item.title), href: planNodeHref(view.root, item.plan_node_id),
      current: view.mainline.current?.plan_node_id === item.plan_node_id, cells, total: cells.length, done, active: count('active'),
      stopped: count('stopped'), unknown: count('unknown'), dropped: steps.length - cells.length, toGo: cells.length - done, waits }
  })
}

/** "N steps to go", "All steps done" at 0, "No steps" for an item without a step to count. */
export function toGoText(row: Pick<ModuleRow, 'total' | 'toGo'>): string {
  if (row.total === 0) return 'No steps'
  if (row.toGo === 0) return 'All steps done'
  return `${row.toGo} step${row.toGo === 1 ? '' : 's'} to go`
}
