import { planRounds } from '@/api/agentRuntimePlan'
import type { RuntimeProgress } from '@/api/agentRuntime'
import { formatShellHash } from '@/lib/shell/consoleRoutes'

/** A round the runtime holds: its root id (R5) and its title (第 5 轮 …). */
export type PlanRound = { id: string; title: string }

/**
 * The rounds Work → Plan can show, from GET /progress's plan (every root the runtime holds, no limit: store
 * progress.go planView): ok with the rounds in number order and the newest, or reading, or Unknown with why. A progress
 * answer without its plan list is Unknown, never "no rounds"; so is one whose rounds all have ids that are not R<n>,
 * which no newest can be told from. ignored are the round ids that are not R<n> beside ones that are (left out of the
 * newest). failed is the reason the latest read failed while an earlier answer stands ('' when it did not).
 */
export type RoundsRead =
  | { state: 'ok'; rounds: PlanRound[]; newest: string | null; ignored: string[]; at: number; failed: string }
  | { state: 'reading' }
  | { state: 'unknown'; reason: string }

type ProgressRead = { data: RuntimeProgress | undefined; error: unknown; isError: boolean; isPaused: boolean; dataUpdatedAt: number }

const reasonOf = (q: ProgressRead) => q.isError ? (q.error instanceof Error ? q.error.message : 'Request failed') : q.isPaused ? 'Offline' : ''

/** The rounds from one read of GET /progress, the newest by number (planRounds' rule, without its R2 fallback). */
export function roundsRead(q: ProgressRead): RoundsRead {
  if (q.data === undefined) {
    const reason = reasonOf(q)
    return reason ? { state: 'unknown', reason } : { state: 'reading' }
  }
  if (q.data.planRead === false) return { state: 'unknown', reason: "the runtime's progress answer has no plan list" }
  const listed = q.data.plan.flatMap(p => {
    const r = p !== null && typeof p === 'object' ? p as Record<string, unknown> : {}
    return r.kind === 'round' ? [{ id: typeof r.plan_node_id === 'string' ? r.plan_node_id : '', title: typeof r.title === 'string' ? r.title : '' }] : []
  })
  const rounds = planRounds(q.data.plan).map(id => ({ id, title: listed.find(r => r.id === id)?.title ?? '' }))
  const ignored = [...new Set(listed.map(r => r.id).filter(id => !rounds.some(r => r.id === id)))].map(id => id || '(no id)')
  if (rounds.length === 0 && ignored.length > 0) return { state: 'unknown', reason: `the runtime lists rounds whose ids are not R<n> (${ignored.join(', ')})` }
  return { state: 'ok', rounds, newest: rounds.length > 0 ? rounds[rounds.length - 1].id : null, ignored, at: q.dataUpdatedAt, failed: reasonOf(q) }
}

/** "Round 5" for R5; any other root as it is. */
export const roundLabel = (root: string) => /^R\d+$/.test(root) ? `Round ${root.slice(1)}` : root

/** Work showing a round: the newest when none is named, else that round. No `section=`, so nothing scrolls to the plan's top. */
export const workRoundHref = (root = '') => formatShellHash('work', root ? new URLSearchParams({ root }).toString() : '')
