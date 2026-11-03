import { approvalNumbers } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { attemptRuns, firstReason, landingMoving, landingQuiet, LANDING_QUIET_MS, runContext, stepRunning, type RunContext } from './liveStepModel'
import { taskRole, type ModuleTask, type RuntimeTasks } from './moduleDetailModel'
import { heartbeatUnconfirmed, HEARTBEAT_UNCONFIRMED_S } from './needsCleanupModel'
import { moduleRows, ruleQuestionIds, ruleSignoffs, type HeldItem, type ModuleRow, type StepJudge, type StepNow } from './planModuleModel'
import { viewOldReason } from './readAge'

/*
 * Work → Plan: what an active step has now, derived from the plan view already held, no request of its own (R5:5.5-10,
 * the Owner 2026-11-02, Q75: a step nobody works on no longer reads "In progress"). The controller's active state alone
 * says only that the step was opened (a task leased, or a recorded event), not that anything runs. In this order:
 * - unknown, before anything else, while the read is not now (readAge.ts viewOldReason: older than 60 s, its refresh
 *   failed, its database not Fresh), with that reason;
 * - running: liveStepModel's reasons seen moving (stepRunning: a live attempt with its heartbeat confirmed, a moving
 *   landing with a record in the last 30 min, external work Running with its heartbeat confirmed);
 * - unknown: a live attempt stale, or its heartbeat unconfirmed (needsCleanupModel heartbeatUnconfirmed, the Live tab's
 *   60 s rule); a moving landing with no record for over 30 min (LANDING_QUIET_MS: not seen moving; the 5.5-8 ring still
 *   marks it, faint); a task leased or running with no live attempt in the plan view; a task state, attempt state or
 *   landing phase this page does not read; registered external work with no heartbeat confirmed;
 * - queued: a task proposed, ready or back for rework; submitted or verifying (waiting for its check); a landing parked
 *   (the controller unparks it itself when its window, rule or CI clears: landing/drill.go); external work registered
 *   and not started;
 * - held: a task blocked, paused or awaiting the Owner (each moves only to ready or cancelled: store/kernel.go taskMoves,
 *   a decision); a landing awaiting the Owner's grant; an accepted work task (not a check of another) with no landing
 *   recorded (the server lands only some tasks: store/close.go; the plan view does not say which, so no landing is
 *   promised); external work submitted and not read Verified (most is never verified); none of these would ever end
 *   as progress by itself, so none is queued; not Unknown (the lists are whole) and not idle;
 * - yours: an Owner step (owner_step) with none of those, with what the plan view records on it;
 * - idle: none of those.
 * plan-view tasks are every task under the round (planviewsnapshot.go, no limit) and external_work every record of the
 * round, so "nothing queued" and "nothing held" are said of whole lists. Claims: ~/agent-work/modview/STEP_STATE_CLAIMS.md.
 */

/** The page clock a plan view is judged at: why its latest refresh failed (null while it did not), now, the runtime's tasks (roles). */
export type StepClock = { failed: string | null; now: number; runtime?: RuntimeTasks }

/** Every task state there is (agentrtapi/propose.go taskStates; store/kernel.go taskMoves). */
const TASK_STATES = new Set(['proposed', 'awaiting_owner', 'ready', 'leased', 'running', 'submitted', 'verifying', 'accepted', 'rework', 'rejected', 'blocked', 'paused', 'cancelled'])
/** Task states that wait their turn: to be taken (proposed, ready, rework) or for their check (submitted, verifying). */
const QUEUED_TASK: Record<string, string> = {
  proposed: 'proposed', ready: 'ready', rework: 'back for rework', submitted: 'submitted, waiting for its check', verifying: 'being verified',
}
/** Task states held, not queued: each moves only to ready or cancelled (store/kernel.go taskMoves), by a decision. */
const HELD_TASK: Record<string, string> = { blocked: 'blocked', paused: 'paused', awaiting_owner: 'awaiting the Owner' }
/** The landing phases that wait (protocol.go: not Active, not Terminal): parked is the controller's to clear, the grant the Owner's. */
const PARKED = 'parked', AWAITING_GRANT = 'awaiting_grant'
/** Terminal landing phases (protocol.go Operation.Terminal; main_result non_ff too). */
const ENDED_LANDING = new Set(['landed', 'reverted', 'failed', 'discarded', 'rehearsed', 'stg_verified', 'stg_failed'])
const landingEnded = (l: { phase: string; outcome: string }) => ENDED_LANDING.has(l.phase) || (l.phase === 'main_result' && l.outcome === 'non_ff')

const taskName = (t: ModuleTask) => `task ${t.title || t.task_id}`

/** What the plan view records on an Owner step that may ask him: its waits there (owner_waiting) and what its done rule names. */
function recordedAsks(view: PlanSnapshot, step: PlanNode): string[] {
  const out: string[] = []
  for (const w of view.owner_waiting) {
    if (w.plan_node_id !== step.plan_node_id) continue
    if ('task_id' in w) {
      const t = view.tasks.find(x => x.task_id === w.task_id)
      out.push(`${t ? taskName(t) : `task ${w.task_id}`} awaits you (plan-view owner_waiting)`)
    } else out.push(`acceptance sign-off ${w.acceptance} Ready (plan-view owner_waiting)`)
  }
  for (const q of ruleQuestionIds(step.done_rule_json)) out.push(`question ${q} (its done rule)`)
  for (const n of approvalNumbers(step.done_rule_json)) out.push(`request #${n} (its done rule)`)
  for (const s of ruleSignoffs(step.done_rule_json)) out.push(`sign-off “${s}” (its done rule)`)
  return out
}

/** The words of a landing that moves by protocol but has no record for LANDING_QUIET_MS. */
const quietWords = (name: string, l: { phase: string; observed_at: string }) =>
  `landing of ${name} at phase "${l.phase}": no landing record for over ${LANDING_QUIET_MS / 60_000} min (last ${l.observed_at})`

/** One active step, judged at ctx; old is viewOldReason's answer ('' when the read is now). */
export function judgeStep(step: PlanNode, ctx: RunContext, old: string): StepNow {
  if (old) return { kind: 'unknown', why: [old], queued: [], held: [] }
  const { view, now, runtime } = ctx
  // A landing silent past LANDING_QUIET_MS is not seen moving: it is Unknown below, not running.
  const moving = stepRunning(step.plan_node_id, ctx).reasons.filter(r => !r.quiet)
  if (moving.length > 0) return { kind: 'running', what: firstReason(moving).what, others: moving.length - 1 }
  const unknown: string[] = [], queued: string[] = [], held: HeldItem[] = []
  const drift = Math.max(0, Math.round((now - Date.parse(view.as_of)) / 1000))
  for (const t of view.tasks) {
    if (t.plan_node_id !== step.plan_node_id) continue
    const name = taskName(t), a = t.attempt, l = t.landing
    // A task with a live attempt is told by its attempt: one that ran now was a running reason above.
    if (a) {
      if (a.state === 'stale') unknown.push(`${name}: its attempt is stale`)
      else if (attemptRuns(a.state)) unknown.push(`${name}: its attempt's heartbeat is unconfirmed (${a.status === 'Unconfirmed' ? 'the server says Unconfirmed' : `${a.heartbeat_age_s + drift} s, over ${HEARTBEAT_UNCONFIRMED_S} s`})`)
      else unknown.push(`${name}: its attempt is in state "${a.state}", which this page does not read`)
    } else if (!TASK_STATES.has(t.state)) unknown.push(`${name}: its state "${t.state}" is not one this page reads`)
    else if (t.state === 'leased' || t.state === 'running') unknown.push(`${name} is ${t.state}, but the plan view holds no live attempt for it`)
    else if (Object.hasOwn(QUEUED_TASK, t.state)) queued.push(`${name} ${QUEUED_TASK[t.state]}`)
    else if (Object.hasOwn(HELD_TASK, t.state)) held.push({ kind: 'decision', text: `${name} ${HELD_TASK[t.state]}` })
    // Accepted work with no landing record: whether it will land the plan view does not say; a check of another does not land.
    else if (t.state === 'accepted' && !l && !taskRole(t, runtime).check) held.push({ kind: 'no-landing', text: `${name} accepted · no landing recorded` })
    if (!l || landingEnded(l)) continue
    if (landingMoving(l)) { if (landingQuiet(l, now)) unknown.push(quietWords(name, l)); continue }
    if (l.phase === PARKED) queued.push(`landing of ${name} parked${l.reason ? ` (${l.reason})` : ''}`)
    else if (l.phase === AWAITING_GRANT) held.push({ kind: 'grant', text: `landing of ${name} waits for your grant` })
    else unknown.push(`landing of ${name}: its phase "${l.phase}" is not one this page reads`)
  }
  for (const w of view.external_work) {
    // Cancelled has ended; under a task with a live attempt it is that attempt's history.
    if (w.plan_node_id !== step.plan_node_id || (w.task_id && ctx.attempted.has(w.task_id))) continue
    const what = `external work “${w.description.task}”`
    if (w.state === 'Running' || w.state === 'Registered') {
      if (heartbeatUnconfirmed(w, view, now)) unknown.push(`${what} ${w.state}: no heartbeat confirmed for over ${HEARTBEAT_UNCONFIRMED_S} s`)
      else if (w.state === 'Registered') queued.push(`${what} registered, not started`)
    } else if (w.state === 'Submitted' && w.verification === 'Verification failed') {
      held.push({ kind: 'verify-failed', text: `${what} submitted · verification failed` })
    } else if (w.state === 'Submitted' && w.verification !== 'Verified') {
      held.push({ kind: 'unverified', text: `${what} submitted · waits for its verification (${w.verification ? `verification ${w.verification}` : 'no verification recorded'})` })
    }
  }
  const heldText = held.map(h => h.text)
  if (unknown.length > 0) return { kind: 'unknown', why: unknown, queued, held: heldText }
  if (queued.length > 0) return { kind: 'queued', why: queued, held: heldText }
  if (held.length > 0) return { kind: 'held', held, recorded: step.owner_step ? recordedAsks(view, step) : null }
  if (step.owner_step) return { kind: 'yours', recorded: recordedAsks(view, step) }
  return { kind: 'idle' }
}

/** The judge of one plan view at one clock: the read's age is judged once for every step. */
export function stepJudge(view: PlanSnapshot, clock: StepClock): StepJudge {
  const old = viewOldReason(view, view.root, clock.failed, clock.now)
  const ctx = runContext(view, clock.now, clock.runtime)
  return step => judgeStep(step, ctx, old)
}

/** The module rows with each active step shown by what it has now. */
export function stepRows(view: PlanSnapshot, clock: StepClock): ModuleRow[] {
  return moduleRows(view, stepJudge(view, clock))
}
