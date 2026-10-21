import type { RuntimeAttempt } from '@/api/agentRuntime'

/**
 * How Progress tells a live attempt (store.TaskStatus; v4 §20.2: doing, maybe stuck and host lost are told apart):
 * running, unless the runtime says its host is lost, that it may be stuck, or that its status is unknown. A stale
 * attempt (its lease lapsed) is maybe stuck whatever status it carries. T24: an attempt on a host that stopped
 * polling is not shown as doing.
 */
export type AttemptActivity = 'running' | 'host_lost' | 'maybe_stuck' | 'unknown'

export function attemptActivity(a: RuntimeAttempt): AttemptActivity {
  if (a.status === 'host_lost' || a.status === 'maybe_stuck' || a.status === 'unknown') return a.status
  return a.state === 'stale' ? 'maybe_stuck' : 'running'
}

const COUNT_WORD: Record<AttemptActivity, string> = { running: 'running', host_lost: 'host lost', maybe_stuck: 'maybe stuck', unknown: 'status unknown' }
const ORDER: AttemptActivity[] = ['running', 'host_lost', 'maybe_stuck', 'unknown']

/** Live attempts counted by activity, in this order: "1 running · 1 host lost"; '' for none. */
export function activityCounts(live: readonly RuntimeAttempt[]): string {
  const n = new Map<AttemptActivity, number>()
  for (const a of live) n.set(attemptActivity(a), (n.get(attemptActivity(a)) ?? 0) + 1)
  return ORDER.filter(k => n.has(k)).map(k => `${n.get(k)} ${COUNT_WORD[k]}`).join(' · ')
}

const WORD: Record<Exclude<AttemptActivity, 'running'>, string> = { host_lost: 'Host lost', maybe_stuck: 'Maybe stuck', unknown: 'Status unknown' }

/**
 * Why an attempt is not counted as running: the runtime's word and its reason, up to the clause on its result
 * (unpersistedWords says that); '' for a running one.
 */
export function stalledWords(a: RuntimeAttempt): string {
  const activity = attemptActivity(a)
  if (activity === 'running') return ''
  const why = a.reason.split('; result not persisted')[0].trim()
  return why ? `${WORD[activity]}: ${why}` : WORD[activity]
}
