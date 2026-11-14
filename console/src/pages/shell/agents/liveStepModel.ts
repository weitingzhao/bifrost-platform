import type { PlanSnapshot } from '@/api/planSnapshot'
import { heartbeatUnconfirmed } from './needsCleanupModel'
import type { ModuleRow } from './planModuleModel'
import { sinceWords, viewOldReason } from './readAge'

/*
 * Work → Plan: which step has work running right now (R5:5.5-8, M-267). Read from the plan view the page already
 * holds, and only while that read is now (readAge.ts viewOldReason: not older than 60 s, its refresh not failed, its
 * database Fresh); an old read says nothing is known of now, never that nothing runs. Waiting is not running: a step's
 * state alone (a step imported active with no work on it) and external work registered but not started are not marked.
 */

/** Since when, as the data gives it: the external work's start. */
export type LiveSince = { kind: 'started'; at: number }
/** One reason a step is live. */
export type Reason = { what: string; since: LiveSince | null }
/** A step with work running now: what (the first of its reasons), since when, and how many other reasons it has. */
export type LiveStep = { id: string; label: string; row: string; href: string; what: string; since: LiveSince | null; others: number }
/**
 * Live now: unknown (the plan view is not now, with why and its snapshot time), or ok with the live steps (in the
 * rows' order: the mainline first, then plan order), by step id, and how many entries of plan-view working mark no step
 * (unconfirmed, registered but not started, or on no step drawn here).
 */
export type LiveNow = { state: 'unknown'; reason: string; asOf: number } | { state: 'ok'; steps: LiveStep[]; byStep: Map<string, LiveStep>; unmarked: number }

const at = (time: string | undefined) => { const t = time ? Date.parse(time) : NaN; return Number.isFinite(t) ? t : null }

/** What one plan view says runs, at one time: the plan view and now. */
export type RunContext = { view: PlanSnapshot; now: number }
export function runContext(view: PlanSnapshot, now: number): RunContext {
  return { view, now }
}

/**
 * What runs on one step now (R5:5.5-10: the per-step classifier liveNow and the step states share): registered
 * external work on it Running with its heartbeat confirmed (needsCleanupModel heartbeatUnconfirmed: the Live tab's
 * rule, 60 s with the time since the snapshot); with what of plan-view working each marks (by its id). Whether the
 * plan view is now is the caller's to judge first (viewOldReason): this reads it as now.
 */
export function stepRunning(step: string, ctx: RunContext): { reasons: Reason[]; marks: string[] } {
  const { view, now } = ctx
  const reasons: Reason[] = [], marks: string[] = []
  for (const w of view.external_work) {
    if (w.plan_node_id !== step || w.state !== 'Running' || heartbeatUnconfirmed(w, view, now)) continue
    marks.push(w.external_work_id)
    const started = at(w.started_at)
    reasons.push({ what: `External work running (${w.description.executor})`, since: started === null ? null : { kind: 'started', at: started } })
  }
  return { reasons, marks }
}

/**
 * The steps with work running now. A step is live when the plan view is now and stepRunning finds a reason on it:
 * registered external work on it is Running with its heartbeat confirmed. Dropped steps are not drawn, so not listed.
 */
export function liveNow(view: PlanSnapshot, rows: ModuleRow[], opts: { failed: string | null; now: number }): LiveNow {
  const { now } = opts
  const old = viewOldReason(view, view.root, opts.failed, now)
  if (old) return { state: 'unknown', reason: old, asOf: Date.parse(view.as_of) }
  const ctx = runContext(view, now)
  // What of plan-view working marks a step here: external work by its id.
  const marking = new Set<string>()
  const steps: LiveStep[] = []
  for (const row of rows) {
    for (const cell of row.cells) {
      const { reasons, marks } = stepRunning(cell.id, ctx)
      marks.forEach(m => marking.add(m))
      if (reasons.length === 0) continue
      steps.push({ id: cell.id, label: cell.label, row: row.id, href: row.href, what: reasons[0].what, since: reasons[0].since, others: reasons.length - 1 })
    }
  }
  const unmarked = view.working.filter(w => !marking.has(w.external_work_id)).length
  return { state: 'ok', steps, byStep: new Map(steps.map(s => [s.id, s])), unmarked }
}

/** "for 12 min" from a start; '' when none is known. */
export function sinceText(since: LiveSince | null, now: number): string {
  return since ? `for ${sinceWords(since.at, now)}` : ''
}

/** "External work running (codex)", with "(+1 more)" when the step has other reasons. */
export const liveWhat = (step: LiveStep) => `${step.what}${step.others > 0 ? ` (+${step.others} more)` : ''}`
/** What a live cell says aloud: what runs, no clock in it (a duration would change while it is read). */
export const liveSpoken = (step: LiveStep) => `live now: ${liveWhat(step)}`
/** "5.8-2 · External work running (codex) · for 12 min": a line of the Now line. */
export const liveLine = (step: LiveStep, now: number) => [step.label, liveWhat(step), sinceText(step.since, now)].filter(Boolean).join(' · ')

/** The Now line shows this many live steps; the rest are counted. */
export const NOW_SHOWN = 3
