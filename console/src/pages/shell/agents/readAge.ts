import { AGENT_PROGRESS_POLL_MS } from '@/api/agentProgress'
import type { PlanSnapshot } from '@/api/planSnapshot'
import { isStale, SHELL_POLL_MS } from './moduleDetailModel'
import { roundLabel } from './planRoundModel'

/*
 * How old each read behind Work's counts may be before what it says is no longer said as now (R5:5.5-4: a refresh that
 * hangs neither fails nor answers, so its last answer stays a success). Each rule is the one Work already applies to
 * that read; none is new:
 * - GET /progress (the rounds, the hosts): older than twice the shell's 5 s poll (moduleDetailModel.ts isStale /
 *   SHELL_POLL_MS, the module page's "Read as of"; PlanRoundNote's "rounds as of" the same);
 * - GET /plan?root= (the step sign-offs): older than twice its own 15 s poll (AGENT_PROGRESS_POLL_MS, its refetchInterval);
 * - the plan view: its snapshot older than 60 s, its refresh failed, or its database not Fresh (the summary's Stale).
 */

/** How long ago, as the summary's "read 9 s ago" says it: seconds below a minute, then minutes, then hours. */
export function sinceWords(at: number, now: number): string {
  const s = Math.max(0, Math.round((now - at) / 1000))
  return s < 60 ? `${s} s` : s < 3600 ? `${Math.floor(s / 60)} min` : `${Math.floor(s / 3600)} h`
}

/** "last answered 31 s ago (2026-11-01T12:00:00.000Z)"; an answer with no read time says so. */
export function answeredWords(at: number, now: number): string {
  return at > 0 ? `last answered ${sinceWords(at, now)} ago (${new Date(at).toISOString()})` : 'its answer has no read time'
}

/** GET /progress answered at `at` is old by now: more than twice the shell's 5 s poll. */
export const progressOld = (at: number, now: number) => isStale({ state: 'ok', data: null, at }, SHELL_POLL_MS.progress, now)

/** GET /plan answered at `at` is old by now: more than twice its 15 s poll. */
export const PLAN_TREE_OLD_MS = 2 * AGENT_PROGRESS_POLL_MS
export const planTreeOld = (at: number, now: number) => now - at > PLAN_TREE_OLD_MS

/**
 * Why a plan view held is not now ('' when it is): its refresh failed (or is paused offline), its snapshot is older
 * than 60 s, or its database is not Fresh; with the snapshot's time.
 */
export function viewOldReason(view: Pick<PlanSnapshot, 'as_of' | 'freshness'>, root: string, failed: string | null, now: number): string {
  const old = now - Date.parse(view.as_of) > 60_000
  if (!failed && !old && view.freshness.database.status === 'Fresh') return ''
  const why = failed ? `could not be read (${failed})` : old ? 'is older than 60 s' : `reads the database as ${view.freshness.database.status}`
  return `${roundLabel(root)}'s plan view ${why}; as of ${view.as_of}`
}
