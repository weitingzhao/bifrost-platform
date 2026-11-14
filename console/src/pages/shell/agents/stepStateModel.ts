import { approvalNumbers } from '@/api/agentRuntimePlan'
import type { PlanNode, PlanSnapshot } from '@/api/planSnapshot'
import { runContext, stepRunning, type RunContext } from './liveStepModel'
import { heartbeatUnconfirmed, HEARTBEAT_UNCONFIRMED_S } from './needsCleanupModel'
import { stepAsks, type OwnerAsks } from './ownerAsksModel'
import { OWNER_KINDS, moduleRows, ruleQuestionIds, ruleSignoffs, type HeldItem, type ModuleRow, type StepJudge, type StepNow, type Unverified } from './planModuleModel'
import { viewOldReason } from './readAge'

/*
 * Work → Plan: what an active step has now, derived from the plan view already held, no request of its own (R5:5.5-10,
 * the Owner 2026-11-02, Q75: a step nobody works on no longer reads "In progress"). The controller's active state alone
 * says only that the step was opened (a recorded event), not that anything runs. In this order:
 * - unknown, before anything else, while the read is not now (readAge.ts viewOldReason: older than 60 s, its refresh
 *   failed, its database not Fresh), with that reason;
 * - running: liveStepModel's reasons (stepRunning: external work Running with its heartbeat confirmed);
 * - unknown: registered external work with no heartbeat confirmed (needsCleanupModel heartbeatUnconfirmed, the Live
 *   tab's 60 s rule); external work submitted whose verification reads Stale (a binding on it exists, its last
 *   successful read is over 25 min old: a stale reading is Unknown and does not show that an earlier failure cleared,
 *   and Stale is told before a failure on another binding: store/planfactssnapshot.go reading, planobservations.go
 *   planObservationTTL, planverificationview.go externalVerification);
 * - queued: external work registered and not started;
 * - waiting on you (slice 2): what only the Owner moves, with nothing running, Unknown or queued: an acceptance
 *   sign-off Ready for the step (plan-view owner_waiting); an open question named by its done rule, a request its done
 *   rule names awaiting his decision (ownerAsksModel.ts, from the shell's lists);
 * - held: external work submitted whose verification read failed (an adopted external_verified binding on it read
 *   false: store/planverificationview.go externalVerification); it would never end as progress by itself, so it is not
 *   queued; not Unknown (the lists are whole) and not idle;
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
 * plan-view external_work is every record of the round, so "nothing queued" and "nothing held" are said of a whole
 * list. Claims and sources: the R5:5.5-10 hand-in report.
 */

/**
 * The page clock a plan view is judged at: why its latest refresh failed (null while it did not), now, and the shell's
 * open questions and requests (asks; without them none is read, as in a test of the plan view alone: every page caller
 * passes them).
 */
export type StepClock = { failed: string | null; now: number; asks?: OwnerAsks }

/** What the plan view records on an Owner step that may ask him: its waits there (owner_waiting) and what its done rule names. */
function recordedAsks(view: PlanSnapshot, step: PlanNode): string[] {
  const out: string[] = []
  for (const w of view.owner_waiting) {
    if (w.plan_node_id === step.plan_node_id) out.push(`acceptance sign-off ${w.acceptance} Ready (plan-view owner_waiting)`)
  }
  for (const q of ruleQuestionIds(step.done_rule_json)) out.push(`question ${q} (its done rule)`)
  for (const n of approvalNumbers(step.done_rule_json)) out.push(`request #${n} (its done rule)`)
  for (const s of ruleSignoffs(step.done_rule_json)) out.push(`sign-off “${s}” (its done rule)`)
  return out
}

/**
 * External work on the step submitted and not read Verified or failed, the latest recorded first: the step's note, not
 * its state.
 */
function unverifiedWork(step: PlanNode, { view }: RunContext): Unverified[] {
  return view.external_work.filter(w => w.plan_node_id === step.plan_node_id
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
  const { view, now } = ctx
  const moving = stepRunning(step.plan_node_id, ctx).reasons
  if (moving.length > 0) return { kind: 'running', what: moving[0].what, others: moving.length - 1 }
  const unknown: string[] = [], queued: string[] = [], held: HeldItem[] = []
  for (const w of view.external_work) {
    // Cancelled has ended; submitted and not read failed is the step's note (unverifiedWork), not held; a Stale reading
    // is Unknown besides.
    if (w.plan_node_id !== step.plan_node_id) continue
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
  // An acceptance sign-off Ready for the step is his to give (plan-view owner_waiting).
  for (const w of view.owner_waiting) {
    if (w.plan_node_id === step.plan_node_id) held.push({ kind: 'signoff', text: `acceptance sign-off ${w.acceptance} is Ready for you` })
  }
  const asked = asks ? stepAsks(step, asks, now) : { items: [], unread: [] }
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
  const ctx = runContext(view, clock.now)
  return step => judgeStep(step, ctx, old, clock.asks)
}

/** The module rows with each active step shown by what it has now. */
export function stepRows(view: PlanSnapshot, clock: StepClock): ModuleRow[] {
  return moduleRows(view, stepJudge(view, clock))
}
