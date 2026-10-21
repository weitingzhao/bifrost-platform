import type { RuntimeProgress } from '@/api/agentRuntime'
import type { NeedsYouCount } from '../needs-you/needsYouModel'
import { attemptActivity } from '../owner/attemptActivity'

// Task states in which the next step is the runtime's: queued for the controller or a host, held by one, or
// waiting on a verification, a review or the next implementation round (store.TaskStatus).
const AGENT_MOVES = new Set(['proposed', 'ready', 'leased', 'running', 'submitted', 'verifying', 'rework'])
// Task statuses that say the runtime cannot tell the task moves: its host is lost, it may be stuck, or its state is
// unknown (store.TaskStatus); the runtime then says the goal waits on the coordinator (T24, review 471 A1).
const STALLED = new Set(['host_lost', 'maybe_stuck', 'unknown'])

/**
 * Whose move it is, from the runtime alone (2.5 slice 4: the board's coordinator lines are retired). The
 * coordinator's move is a goal the runtime says waits on it (`waiting_on: coordinator`: no task proposed
 * for its next step, blocked, paused, or a host lost); what the coordinating thread does outside the
 * runtime is in its note on Work.
 */
export function whoseMove(count: NeedsYouCount, runtime: RuntimeProgress | undefined, failed = false, silentThreads = 0): string {
  if (failed || count.state !== 'known' || !runtime) return 'Unknown'
  if (count.count > silentThreads) return "Owner's move"
  const missions = runtime.work.missions
  // Agents' move only for an attempt confirmed running or a task not stalled: one on a lost host is not theirs to make.
  if (runtime.live.some(a => attemptActivity(a) === 'running')
    || missions.some(m => m.tasks.some(t => AGENT_MOVES.has(t.state) && !STALLED.has(t.status)))) return "Agents' move"
  if (missions.some(m => m.waiting_on === 'coordinator')) return "Coordinator's move"
  return 'All done'
}
