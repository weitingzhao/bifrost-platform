import { approvalNumbers } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { attemptRuns, firstReason, landingMoving, landingQuiet, LANDING_QUIET_MS, runContext, stepRunning, type RunContext } from './liveStepModel'
import { taskRole, type ModuleTask, type RuntimeTasks } from './moduleDetailModel'
import { heartbeatUnconfirmed, HEARTBEAT_UNCONFIRMED_S } from './needsCleanupModel'
import { stepAsks, type OwnerAsks } from './ownerAsksModel'
import { OWNER_KINDS, moduleRows, ruleQuestionIds, ruleSignoffs, type HeldItem, type ModuleRow, type StepJudge, type StepNow, type Unverified } from './planModuleModel'
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
 *   landing phase this page does not read; registered external work with no heartbeat confirmed; external work submitted
 *   whose verification reads Stale (a binding on it exists, its last successful read is over 25 min old: a stale
 *   reading is Unknown and does not show that an earlier failure cleared, and Stale is told before a failure on another
 *   binding: store/planfactssnapshot.go reading, planobservations.go planObservationTTL, planverificationview.go
 *   externalVerification);
 * - queued: a task proposed, ready or back for rework; submitted or verifying (waiting for its check); a landing parked
 *   (the controller unparks it itself when its window, rule or CI clears: landing/drill.go); external work registered
 *   and not started;
 * - waiting on you (slice 2): what only the Owner moves, with nothing running, Unknown or queued: a task awaiting the
 *   Owner (agentrt awaiting_owner, plan-view owner_waiting lists it too); a landing awaiting the Owner's grant; an
 *   acceptance sign-off Ready for the step (plan-view owner_waiting); an open question on one of the step's tasks or
 *   named by its done rule, a request its done rule names awaiting his decision (ownerAsksModel.ts, from the shell's lists);
 * - held: a task blocked or paused (each moves only to ready or cancelled: store/kernel.go taskMoves, a decision); an
 *   accepted work task (not a check of another) with no landing recorded (the server lands only some tasks:
 *   store/close.go; the plan view does not say which, so no landing is promised); external work submitted whose
 *   verification read failed (an adopted external_verified binding on it read false: store/planverificationview.go
 *   externalVerification); none of these would ever end as progress by itself, so none is queued; not Unknown (the
 *   lists are whole) and not idle;
 * - yours: an Owner step (owner_step) with none of those, with what the plan view records on it;
 * - idle: none of those.
 * Queued and Unknown keep what waits on the Owner and what is held, so that none is hidden. When the shell's lists
 * could tie a question or request to the step and were not read now, the step says so (unread): it is then judged by
 * the plan view alone, and "nothing waits on you" is not said of it.
 * Beside any of these, external work submitted and not read Verified or failed (verification Unknown, Stale or not
 * recorded) is a note on the step (unverified). Unknown or not recorded is the note alone, never what holds the step:
 * its work has finished, and nothing says anything waits on its verification (the platform reads one only through an
 * adopted external_verified binding; most external work has none and is never verified). The round's
 * freshness.verification is not read here: Unknown there is no condition of any one step.
 * plan-view tasks are every task under the round (planviewsnapshot.go, no limit) and external_work every record of the
 * round, so "nothing queued" and "nothing held" are said of whole lists. Claims and sources: the R5:5.5-10 hand-in report.
 */

/**
 * The page clock a plan view is judged at: why its latest refresh failed (null while it did not), now, the runtime's
 * tasks (roles), and the shell's open questions and requests (asks; without them none is read, as in a test of the
 * plan view alone: every page caller passes them).
 */
export type StepClock = { failed: string | null; now: number; runtime?: RuntimeTasks; asks?: OwnerAsks }

/** Every task state there is (agentrtapi/propose.go taskStates; store/kernel.go taskMoves). */
const TASK_STATES = new Set(['proposed', 'awaiting_owner', 'ready', 'leased', 'running', 'submitted', 'verifying', 'accepted', 'rework', 'rejected', 'blocked', 'paused', 'cancelled'])
/** Task states that wait their turn: to be taken (proposed, ready, rework) or for their check (submitted, verifying). */
const QUEUED_TASK: Record<string, string> = {
  proposed: 'proposed', ready: 'ready', rework: 'back for rework', submitted: 'submitted, waiting for its check', verifying: 'being verified',
}
/** Task states held, not queued: each moves only to ready or cancelled (store/kernel.go taskMoves), by a decision. */
const HELD_TASK: Record<string, string> = { blocked: 'blocked', paused: 'paused' }
/** The task state only the Owner moves (a card on Needs you): it waits on him, not held. */
const OWNER_TASK = 'awaiting_owner'
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

/**
 * External work on the step submitted and not read Verified or failed, the latest recorded first: the step's note, not
 * its state. Under a task with a live attempt it is that attempt's history (as judgeNow reads it).
 */
function unverifiedWork(step: PlanNode, { view, attempted }: RunContext): Unverified[] {
  return view.external_work.filter(w => w.plan_node_id === step.plan_node_id && !(w.task_id && attempted.has(w.task_id))
    && w.state === 'Submitted' && w.verification !== 'Verified' && w.verification !== 'Verification failed')
    .sort((a, b) => b.recorded_at.localeCompare(a.recorded_at)).map(w => ({ task: w.description.task, verification: w.verification ?? '' }))
}

/** One active step, judged at ctx; old is viewOldReason's answer ('' when the read is now); asks, the shell's lists. */
export function judgeStep(step: PlanNode, ctx: RunContext, old: string, asks?: OwnerAsks): StepNow {
  if (old) return { kind: 'unknown', why: [old], queued: [], held: [], waiting: [] }
  const now = judgeNow(step, ctx, asks), unverified = unverifiedWork(step, ctx)
  return unverified.length > 0 ? { ...now, unverified } : now
}

/** One active step from a read that is now. */
function judgeNow(step: PlanNode, ctx: RunContext, asks: OwnerAsks | undefined): StepNow {
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
    else if (t.state === OWNER_TASK) held.push({ kind: 'owner-decision', text: `${name} awaits your decision` })
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
    // Cancelled has ended; under a task with a live attempt it is that attempt's history; submitted and not read failed is
    // the step's note (unverifiedWork), not held; a Stale reading is Unknown besides.
    if (w.plan_node_id !== step.plan_node_id || (w.task_id && ctx.attempted.has(w.task_id))) continue
    const what = `external work “${w.description.task}”`
    if (w.state === 'Running' || w.state === 'Registered') {
      if (heartbeatUnconfirmed(w, view, now)) unknown.push(`${what} ${w.state}: no heartbeat confirmed for over ${HEARTBEAT_UNCONFIRMED_S} s`)
      else if (w.state === 'Registered') queued.push(`${what} registered, not started`)
    } else if (w.state === 'Submitted' && w.verification === 'Verification failed') {
      held.push({ kind: 'verify-failed', text: `${what} submitted · verification failed` })
    } else if (w.state === 'Submitted' && w.verification === 'Stale') {
      unknown.push(`${what} submitted: its verification reading is stale (the last recorded successful read is over 25 min old; an earlier failure is not known to have cleared)`)
    }
  }
  // An acceptance sign-off Ready for the step is his to give (plan-view owner_waiting; its task waits are the tasks' own
  // state, read above).
  for (const w of view.owner_waiting) {
    if (w.plan_node_id === step.plan_node_id && !('task_id' in w)) held.push({ kind: 'signoff', text: `acceptance sign-off ${w.acceptance} is Ready for you` })
  }
  const asked = asks ? stepAsks(view, step, asks, now) : { items: [], unread: [] }
  held.push(...asked.items)
  const owner = held.filter(h => OWNER_KINDS.has(h.kind)), others = held.filter(h => !OWNER_KINDS.has(h.kind))
  const unread = asked.unread.length > 0 ? { unread: asked.unread } : {}
  const text = (list: HeldItem[]) => list.map(h => h.text)
  if (unknown.length > 0) return { kind: 'unknown', why: unknown, queued, held: text(others), waiting: text(owner), ...unread }
  if (queued.length > 0) return { kind: 'queued', why: queued, held: text(others), waiting: text(owner), ...unread }
  const recorded = step.owner_step ? recordedAsks(view, step) : null
  if (owner.length > 0) return { kind: 'waiting', waiting: owner, held: others, recorded, ...unread }
  if (others.length > 0) return { kind: 'held', held: others, recorded, ...unread }
  if (step.owner_step) return { kind: 'yours', recorded: recordedAsks(view, step), ...unread }
  return { kind: 'idle', ...unread }
}

/** The judge of one plan view at one clock: the read's age is judged once for every step. */
export function stepJudge(view: PlanSnapshot, clock: StepClock): StepJudge {
  const old = viewOldReason(view, view.root, clock.failed, clock.now)
  const ctx = runContext(view, clock.now, clock.runtime)
  return step => judgeStep(step, ctx, old, clock.asks)
}

/** The module rows with each active step shown by what it has now. */
export function stepRows(view: PlanSnapshot, clock: StepClock): ModuleRow[] {
  return moduleRows(view, stepJudge(view, clock))
}
