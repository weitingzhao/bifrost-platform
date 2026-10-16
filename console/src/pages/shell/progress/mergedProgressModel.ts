import type { ProgressDocument, ProgressWork } from '@/api/agentProgress'
import type { RuntimeProgress } from '@/api/agentRuntime'
import type { NeedsYouCount } from '../needs-you/needsYouModel'

// Task states in which the next step is the runtime's: queued for the controller or a host, held by one, or
// waiting on a verification, a review or the next implementation round (store.TaskStatus).
const AGENT_MOVES = new Set(['proposed', 'ready', 'leased', 'running', 'submitted', 'verifying', 'rework'])

export function whoseMove(count: NeedsYouCount, runtime: RuntimeProgress | undefined, board: ProgressDocument | null | undefined, failed = false, silentThreads = 0): string {
  if (failed || count.state !== 'known' || !runtime) return 'Unknown'
  if (count.count > silentThreads) return "Owner's move"
  if (runtime.live.length || runtime.work.missions.some(m => m.tasks.some(t => AGENT_MOVES.has(t.state)))) return "Agents' move"
  if (!board) return 'Unknown'
  if (['working', 'running', 'run', 'doing'].includes(board.coordinator?.state || '') || (!board.coordinator?.state && board.coordinator?.doing?.trim())) return "Coordinator's move"
  return 'All done'
}
export function workRunning(work: ProgressWork): boolean {
  return (work.steps ?? []).some(s => ['running', 'slow', 'quiet'].includes(s.state || ''))
}
export function workWaiting(work: ProgressWork): boolean {
  return work.owner?.now === true || (work.steps ?? []).some(s => s.needs_owner && s.state !== 'done')
}
