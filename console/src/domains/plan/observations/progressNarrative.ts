import type { PlanSnapshot } from '@/api/planSnapshot'
import { activeCounts, noViewReason, type ModuleRow } from '@/pages/shell/agents/planModuleModel'
import { roundLabel } from '@/domains/plan/readings/rounds'

/** The mainline's basis once acceptance bindings are adopted (store/planverificationview.go); else controller step states. */
const PV2_BASIS = 'Versioned acceptance observations'

/** "Round 5 · 10 items · plan v1": the round, its plan items (all of them: a view over 500 nodes is refused whole), the adopted version. */
export function roundHeader(view: PlanSnapshot): string {
  const items = view.nodes.filter(n => n.node_kind === 'item').length
  return `${roundLabel(view.root)} · ${items} item${items === 1 ? '' : 's'} · ${view.artifact_id ? `plan v${view.plan_version}` : 'no adopted plan view'}`
}

/**
 * One module (plan item) by its steps, in this order: the first condition that holds names it. An active step counts by
 * what it has now (R5:5.5-10): work running or queued is in progress; an active step with nothing running is never
 * folded into it, and one whose work now is Unknown is neither.
 */
export type ModuleClass = 'in-progress' | 'active-unknown' | 'waiting' | 'held' | 'yours' | 'idle' | 'stopped' | 'unknown' | 'no-steps' | 'all-done' | 'not-started' | 'partly'
export function moduleClass(row: ModuleRow): ModuleClass {
  if (row.running + row.queued > 0) return 'in-progress'
  if (row.nowUnknown > 0) return 'active-unknown'
  if (row.waiting > 0) return 'waiting'
  if (row.held > 0) return 'held'
  if (row.yours > 0) return 'yours'
  if (row.idle > 0) return 'idle'
  if (row.stopped > 0) return 'stopped'
  if (row.unknown > 0) return 'unknown'
  if (row.total === 0) return 'no-steps'
  if (row.toGo === 0) return 'all-done'
  if (row.done === 0) return 'not-started'
  return 'partly'
}

const CLASS_ORDER: ModuleClass[] = ['in-progress', 'waiting', 'held', 'yours', 'idle', 'active-unknown', 'not-started', 'all-done', 'partly', 'stopped', 'unknown', 'no-steps']
/**
 * Each class in the runtime's own terms (nodes[].state, the controller's record of a step, and the plan view's
 * external work for an active one): never a claim about work outside it (R5:5.5-2 review: on
 * PROD every step of 5.5-1 was still planned while its work had been done and reviewed).
 */
const CLASS_WORDS: Record<ModuleClass, [string, string]> = {
  'in-progress': ['has a step in progress', 'have steps in progress'],
  waiting: ['has a step waiting on you', 'have steps waiting on you'],
  held: ['has an active step held', 'have active steps held'],
  idle: ['has an active step with nothing running now', 'have active steps with nothing running now'],
  yours: ['has an Owner step active', 'have Owner steps active'],
  'active-unknown': ['has an active step whose work now is Unknown', 'have active steps whose work now is Unknown'],
  'not-started': ['has no step started', 'have no step started'],
  'all-done': ['has every step done', 'have every step done'],
  partly: ['has steps done and none active', 'have steps done and none active'],
  stopped: ['has a stopped step', 'have stopped steps'],
  unknown: ['has a step with no state', 'have steps with no state'],
  'no-steps': ['has no steps', 'have no steps'],
}
/** What each class means, for "How this is read". */
export const CLASS_MEANINGS: [string, string][] = [
  ['a step in progress', 'the controller has a step active and the plan view, read now, shows work on it running (external work running with its heartbeat confirmed) or queued (external work registered and not started).'],
  ['a step waiting on you', 'the controller has a step active, nothing on it runs or is queued, and something on it is only yours to move: an acceptance sign-off Ready for it (the plan view), an open question named by its done rule, or a request its done rule names awaiting your decision, as Needs you counts one: pending, not expired, not a drill, not a landing request (the questions and requests Needs you reads every 30 s, while read within two of its polls; when those were not read now, the step says so and is judged by the plan view alone).'],
  ['an active step held', 'the controller has a step active, nothing on it runs or is queued, nothing on it was read as waiting on you (the plan view, and the questions and requests while they were read now; a step whose lists were not read says so), and something on it waits: submitted external work whose verification result is Verification failed. It is not counted as progress. Submitted external work with verification Unknown or not recorded is noted on its step separately; it does not by itself establish a hold (most is never verified). A stale verification reads Unknown now.'],
  ['an Owner step active', 'the controller has an Owner step (owner_step) active and, read now, nothing on it runs, is queued or is held, and nothing was read as waiting on you (the plan view, and the questions and requests while they were read now; a step whose lists were not read says so).'],
  ['an active step with nothing running now', 'the controller has a step active and, read now, nothing on it runs, is queued or is held, and nothing was read as waiting on you (the plan view, and the questions and requests while they were read now; a step whose lists were not read says so). The active state alone is not work: a recorded event can move a step to active with no work on it.'],
  ['an active step whose work now is Unknown', 'the plan view is not now (older than 60 s, its refresh failed or its database not Fresh), or external work on the step has no heartbeat confirmed, or submitted external work on it has a stale verification reading (the last recorded successful read is over 25 min old).'],
  ['no step started', 'every step is planned in the runtime. Work done outside the runtime (a thread, an external run not linked to a step) moves no step, so this does not say nothing was done.'],
  ['every step done', 'the controller marked each step done: not acceptance, not deployment.'],
  ['steps done and none active', 'some steps done, the rest planned, none active.'],
  ['a stopped step', 'the controller stopped a step.'],
  ['a step with no state', 'the runtime holds no state for a step.'],
  ['no steps', 'the item has no steps in the runtime.'],
]
const joinWords = (parts: string[]) => parts.length <= 1 ? parts.join('') : `${parts.slice(0, -1).join(', ')} and ${parts[parts.length - 1]}`

const stepsLine = (row: ModuleRow) => row.total === 0 ? 'no runtime steps'
  : `runtime steps ${row.done} of ${row.total} done${activeCounts(row).map(p => `, ${p}`).join('')}${row.stopped ? `, ${row.stopped} stopped` : ''}${row.unknown ? `, ${row.unknown} with no state` : ''}`

/**
 * Where the mainline stands: plan-view's mainline.current (PV1: the first mainline item whose steps are not all done;
 * PV2: the first not verified accepted), never "being worked on". Unknown when the view has no mainline path.
 */
export function mainlineSentence(view: PlanSnapshot, rows: ModuleRow[]): { text: string; unknown: boolean } {
  if (view.mainline.path.length === 0) return { text: 'Mainline Unknown · the plan view has no adopted mainline.', unknown: true }
  const pv2 = view.mainline.basis === PV2_BASIS
  const current = view.mainline.current
  if (current === null) return { text: pv2 ? 'Every mainline item is verified accepted.' : 'Every mainline item has every runtime step done.', unknown: false }
  const row = rows.find(r => r.id === current.plan_node_id)
  if (!row) return { text: `The mainline stands at ${current.plan_node_id}.`, unknown: false }
  return { text: `The mainline stands at ${row.label} · ${stepsLine(row)}.`, unknown: false }
}

/** The other items by runtime step state, each counted by its own class; only the classes that hold any. '' with none. */
export function othersSentence(rows: ModuleRow[], currentId: string | null): string {
  const others = rows.filter(r => r.id !== currentId)
  if (others.length === 0) return ''
  const counts = new Map<ModuleClass, number>()
  for (const row of others) counts.set(moduleClass(row), (counts.get(moduleClass(row)) ?? 0) + 1)
  const parts = CLASS_ORDER.flatMap(c => { const n = counts.get(c) ?? 0; return n ? [`${n} ${CLASS_WORDS[c][n === 1 ? 0 : 1]}`] : [] })
  const lead = `By runtime step state, of the ${others.length} ${currentId === null ? '' : 'other '}item${others.length === 1 ? '' : 's'}`
  return `${lead}, ${joinWords(parts)}.`
}

/** The mainline path by its items' labels. */
export function mainlineLabels(view: PlanSnapshot): string[] {
  return view.mainline.path.map(p => view.nodes.find(n => n.plan_node_id === p.plan_node_id)?.label ?? p.plan_node_id)
}

/**
 * Blocking edges whose milestone is not verified (waiting_on, every one), read only for the modules the adopted view
 * document lists (row.inView, as the module rows read them; R5:5.5-1): Unknown, with the reason the rows give
 * (noViewReason), when no module is in it; unread is how many modules are not, whose waits are left out.
 */
export type DependencyWaits = { state: 'unknown'; reason: string } | { state: 'ok'; total: number; blocked: number; unverified: number; unread: number }
export function dependencyWaits(view: PlanSnapshot, rows: ModuleRow[]): DependencyWaits {
  const unread = rows.filter(r => !r.inView).length
  if (view.artifact_id === '' || (rows.length > 0 && unread === rows.length)) return { state: 'unknown', reason: noViewReason(view) }
  return { state: 'ok', total: view.waiting_on.length, blocked: view.waiting_on.filter(w => w.status === 'Blocked').length,
    unverified: view.waiting_on.filter(w => w.status === 'Unknown').length, unread }
}

/** The summary's and the round tab's words for the dependency waits. */
export function dependencyWords(waits: DependencyWaits): string {
  if (waits.state === 'unknown') return `Dependencies Unknown · ${waits.reason}`
  const known = waits.total === 0 ? (waits.unread ? 'No dependency waits recorded' : 'No dependency waits') : `${waits.total} dependency wait${waits.total === 1 ? '' : 's'}`
  return waits.unread ? `${known} · ${waits.unread} module${waits.unread === 1 ? '' : 's'} not in the adopted plan view, Unknown` : known
}

/** Runtime countdown only; delivery percent requires the stage model, absent from this wire contract. */
export function planCountdown(rows: Pick<ModuleRow, 'total' | 'done'>[]): { done: number; left: number } {
  return rows.reduce((count, row) => ({ done: count.done + row.done, left: count.left + row.total - row.done }), { done: 0, left: 0 })
}

export const PLAN_PROGRESS_HELP = 'Weights: planned 10, handed in 40, verified 60, on main 70, STG 85, PROD 100. A stage in progress adds nothing. Delivery stages are not available in this plan view, so the weighted percent and number of unknown stages are Unknown. Done and left count runtime steps; dropped steps are excluded.'
