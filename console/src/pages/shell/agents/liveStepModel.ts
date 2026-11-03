import type { PlanSnapshot } from '@/api/planSnapshot'
import { taskRole, type ModuleTask, type RuntimeTasks } from './moduleDetailModel'
import { heartbeatUnconfirmed } from './needsCleanupModel'
import type { ModuleRow } from './planModuleModel'
import { sinceWords, viewOldReason } from './readAge'

/*
 * Work → Plan: which step has work running right now (R5:5.5-8, M-267). Read from the plan view the page already
 * holds, and only while that read is now (readAge.ts viewOldReason: not older than 60 s, its refresh not failed, its
 * database Fresh); an old read says nothing is known of now, never that nothing runs. Waiting is not running: a step's
 * state alone (a step imported active with no task), a task queued or waiting for its check, and a landing parked or
 * waiting for the Owner's grant (Needs you lists grants) are not marked.
 */

/**
 * A task runs while it has a live attempt (plan-view tasks[].attempt, store/progress.go liveAttempts) that is not
 * stale and whose heartbeat is confirmed (needsCleanupModel heartbeatUnconfirmed: the Live tab's rule, 60 s with the
 * time since the snapshot). A task submitted or verifying waits for its check (progress.go: waiting_role); the check
 * task's own attempt lights the step while it really runs.
 */
const ATTEMPT_NOW: Record<string, string> = { leased: 'leased to a host', running: 'running' }

/**
 * Landing phases in which the landing itself is moving: landing/protocol.go Operation.Active (a phase, not parked, not
 * awaiting_grant, not Terminal: landed, reverted, failed, discarded, rehearsed, stg_verified, stg_failed, main_result
 * non_ff). ci_kick, ci_seen and ci_rerun keep the phase they came in; a phase not in the protocol is not read as moving.
 */
const LANDING_NOW: Record<string, string> = {
  claimed: 'claimed', prepared: 'prepared', branch_intent: 'pushing its branch', branch_pushed: 'branch pushed',
  main_intent: 'pushing to main', main_result: 'main answered the push',
  stg_pending: 'STG release pending', stg_snapshot: 'STG release prepared', stg_started: 'STG release running', stg_result: 'STG release answered',
}
/**
 * A moving landing with no record for this long is drawn quiet (a still, faint ring): the controller parks a landing
 * whose CI has run 30 minutes (landing/drill.go, ci_stuck), so silence past it is not a landing seen moving. A landing
 * has no other time bound short of its root's 6 h claim window (protocol.go ClaimWindow).
 */
export const LANDING_QUIET_MS = 30 * 60_000

/** Since when, as the data gives it: an attempt's or external work's start, or only the landing's last record. */
export type LiveSince = { kind: 'started' | 'last_record'; at: number }
/** One reason a step is live; quiet when it is a landing with no record for LANDING_QUIET_MS. */
export type Reason = { what: string; since: LiveSince | null; rank: number; quiet: boolean }
/**
 * A step with work running now: what (the first of its reasons), since when, how many other reasons it has, and quiet
 * when its only reasons are landings silent for LANDING_QUIET_MS.
 */
export type LiveStep = { id: string; label: string; row: string; href: string; what: string; since: LiveSince | null; others: number; quiet: boolean }
/**
 * Live now: unknown (the plan view is not now, with why and its snapshot time), or ok with the live steps (the moving
 * ones first, each group in the rows' order: the mainline first, then plan order), by step id, and how many entries of
 * plan-view working mark no step (unconfirmed, stale, registered but not started, or on no step drawn here).
 */
export type LiveNow = { state: 'unknown'; reason: string; asOf: number } | { state: 'ok'; steps: LiveStep[]; byStep: Map<string, LiveStep>; unmarked: number }

const NO_RUNTIME: RuntimeTasks = { read: false, work: new Map(), ended: new Map() }
const at = (time: string | undefined) => { const t = time ? Date.parse(time) : NaN; return Number.isFinite(t) ? t : null }
const cap = (s: string) => s ? s[0].toUpperCase() + s.slice(1) : s

/** The landing is moving (landing/protocol.go Operation.Active, as LANDING_NOW lists it): main_result non_ff is Terminal. */
export const landingMoving = (l: { phase: string; outcome: string }) => Object.hasOwn(LANDING_NOW, l.phase) && !(l.phase === 'main_result' && l.outcome === 'non_ff')
/** A moving landing with no record for over LANDING_QUIET_MS: not a landing seen moving (still ringed, faint). */
export const landingQuiet = (l: { observed_at: string }, now: number) => { const last = at(l.observed_at); return last === null || now - last > LANDING_QUIET_MS }
/** Attempt states that run (ATTEMPT_NOW): leased to a host, or running. */
export const attemptRuns = (state: string) => Object.hasOwn(ATTEMPT_NOW, state)

/** The task's attempt that runs now, or null: live, not stale, its heartbeat confirmed. */
export function runningAttempt(task: ModuleTask, view: PlanSnapshot, now: number) {
  const a = task.attempt
  return a && Object.hasOwn(ATTEMPT_NOW, a.state) && !heartbeatUnconfirmed(a, view, now) ? a : null
}

/** A task's reasons: its running attempt (role from the runtime or a check's title) and its landing moving. */
function taskReasons(task: ModuleTask, view: PlanSnapshot, now: number, runtime: RuntimeTasks): Reason[] {
  const out: Reason[] = []
  const a = runningAttempt(task, view, now)
  if (a) {
    const role = taskRole(task, runtime).role
    const started = at(a.started_at)
    out.push({ what: `${role ? `${cap(role)} task` : 'Task'} ${ATTEMPT_NOW[a.state]}`, since: started === null ? null : { kind: 'started', at: started },
      rank: a.state === 'running' ? 0 : 2, quiet: false })
  }
  const l = task.landing
  if (l && landingMoving(l)) {
    const last = at(l.observed_at)
    out.push({ what: `Landing ${LANDING_NOW[l.phase]}${l.phase === 'main_result' && l.outcome ? ` (${l.outcome})` : ''}`,
      since: last === null ? null : { kind: 'last_record', at: last }, rank: 3, quiet: landingQuiet(l, now) })
  }
  return out
}

/** What one plan view says runs, at one time: the plan view, now, the runtime's tasks (for roles), the tasks with a live attempt. */
export type RunContext = { view: PlanSnapshot; now: number; runtime: RuntimeTasks; attempted: Set<string> }
export function runContext(view: PlanSnapshot, now: number, runtime: RuntimeTasks = NO_RUNTIME): RunContext {
  return { view, now, runtime, attempted: new Set(view.tasks.filter(t => t.attempt).map(t => t.task_id)) }
}

/**
 * What runs on one step now (R5:5.5-10: the per-step classifier liveNow and the step states share): its own tasks
 * (any role) with a running attempt or a moving landing, and registered external work on it Running with its heartbeat
 * confirmed (external work under a task with a live attempt is that attempt's history: planviewsnapshot.go); with what
 * of plan-view working each marks (a task by its running attempt, external work by its id). Whether the plan view is
 * now is the caller's to judge first (viewOldReason): this reads it as now.
 */
export function stepRunning(step: string, ctx: RunContext): { reasons: Reason[]; marks: string[] } {
  const { view, now, runtime } = ctx
  const reasons: Reason[] = [], marks: string[] = []
  for (const t of view.tasks) {
    if (t.plan_node_id !== step) continue
    if (runningAttempt(t, view, now)) marks.push(t.task_id)
    reasons.push(...taskReasons(t, view, now, runtime))
  }
  for (const w of view.external_work) {
    if (w.plan_node_id !== step || w.state !== 'Running' || (w.task_id && ctx.attempted.has(w.task_id)) || heartbeatUnconfirmed(w, view, now)) continue
    marks.push(w.external_work_id)
    const started = at(w.started_at)
    reasons.push({ what: `External work running (${w.description.executor})`, since: started === null ? null : { kind: 'started', at: started }, rank: 1, quiet: false })
  }
  return { reasons, marks }
}
/** The reason a step names first: one seen moving before a quiet landing, then by rank (running, external, leased, landing). */
export const firstReason = (reasons: Reason[]) => [...reasons].sort((a, b) => Number(a.quiet) - Number(b.quiet) || a.rank - b.rank)[0]

/**
 * The steps with work running now. A step is live when the plan view is now and stepRunning finds a reason on it: one
 * of its own tasks (any role) has a running attempt, or has a landing moving, or registered external work on it is
 * Running with its heartbeat confirmed. Dropped steps are not drawn, so not listed.
 */
export function liveNow(view: PlanSnapshot, rows: ModuleRow[], opts: { failed: string | null; now: number; runtime?: RuntimeTasks }): LiveNow {
  const { now } = opts
  const old = viewOldReason(view, view.root, opts.failed, now)
  if (old) return { state: 'unknown', reason: old, asOf: Date.parse(view.as_of) }
  const ctx = runContext(view, now, opts.runtime)
  // What of plan-view working marks a step here: a task by its running attempt, external work by its id.
  const marking = new Set<string>()
  const steps: LiveStep[] = []
  for (const row of rows) {
    for (const cell of row.cells) {
      const { reasons, marks } = stepRunning(cell.id, ctx)
      marks.forEach(m => marking.add(m))
      if (reasons.length === 0) continue
      const first = firstReason(reasons)
      steps.push({ id: cell.id, label: cell.label, row: row.id, href: row.href, what: first.what, since: first.since, others: reasons.length - 1,
        quiet: reasons.every(r => r.quiet) })
    }
  }
  steps.sort((a, b) => Number(a.quiet) - Number(b.quiet))
  const unmarked = view.working.filter(w => !marking.has('external_work_id' in w ? w.external_work_id : w.task_id)).length
  return { state: 'ok', steps, byStep: new Map(steps.map(s => [s.id, s])), unmarked }
}

/** "for 12 min" from a start; "last landing record 3 min ago" when only that is known; '' when nothing is. */
export function sinceText(since: LiveSince | null, now: number): string {
  if (!since) return ''
  return since.kind === 'started' ? `for ${sinceWords(since.at, now)}` : `last landing record ${sinceWords(since.at, now)} ago`
}

/** "Landing branch pushed", with "(+1 more)" when the step has other reasons. */
export const liveWhat = (step: LiveStep) => `${step.what}${step.others > 0 ? ` (+${step.others} more)` : ''}`
/** What a live cell says aloud: what runs, no clock in it (a duration would change while it is read). */
export const liveSpoken = (step: LiveStep) => `live now: ${liveWhat(step)}${step.quiet ? ', no landing record for over 30 min' : ''}`
/** "5.8-2 · Landing branch pushed · last landing record 1 min ago": a line of the Now line. */
export const liveLine = (step: LiveStep, now: number) => [step.label, liveWhat(step), sinceText(step.since, now)].filter(Boolean).join(' · ')

/** The Now line shows this many live steps; the rest are counted. */
export const NOW_SHOWN = 3
