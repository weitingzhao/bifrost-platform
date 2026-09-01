import type { QueueItem, WorkLane } from '@/lib/briefing/workLanes'

/** In Flight board chrome: program phase table vs ops issue queue (no Sign-off). */
export type ActiveSessionBoardMode = 'program-phases' | 'ops-issue-queue'

/**
 * Troubleshooting / maintain-debug lanes project matrix+cluster issues — not Delivery phases.
 * Owner Sign-off does not apply; clear = probes healthy.
 */
export function isOpsIssueLane(lane: WorkLane): boolean {
  return (
    lane.id === 'troubleshoot' ||
    (lane.trackType === 'maintain' && lane.workIntent === 'debug')
  )
}

/** Prefer ops-issue chrome when the lane has no linked Delivery program. */
export function resolveActiveSessionBoardMode(
  lane: WorkLane,
  linkedProgramCount: number,
): ActiveSessionBoardMode {
  if (linkedProgramCount > 0) return 'program-phases'
  if (isOpsIssueLane(lane) || lane.trackType === 'maintain' || lane.id === 'release') {
    return 'ops-issue-queue'
  }
  return 'ops-issue-queue'
}

export type OpsIssueNavTarget = 'cluster' | 'operate' | 'control-room'

/** Where to send the Owner to act on a projected issue row. */
export function opsIssueNavTarget(item: QueueItem): OpsIssueNavTarget | null {
  if (item.status !== 'issue' && item.status !== 'blocked') return null
  if (item.id === 'cluster-pods' || item.id === 'cluster-reach') return 'cluster'
  if (item.id.startsWith('matrix-') || item.id.startsWith('dev-matrix-')) return 'control-room'
  if (item.id === 'all-clear') return null
  return 'operate'
}

export function formatOpsIssueAgentClipboard(
  item: QueueItem,
  lane: WorkLane,
): string {
  const lines = [
    `In Flight · ${lane.label} (${lane.id})`,
    `Issue: ${item.label}`,
    `id: ${item.id}`,
    `status: ${item.status}`,
  ]
  if (item.note?.trim()) lines.push(`note: ${item.note.trim()}`)
  lines.push(
    'Ask: diagnose with read-only probes first; propose minimal L1 fix; do not unlock D10.',
  )
  return lines.join('\n')
}

/** Open issues for ops progress chip (not program phase done/total). */
export function opsOpenIssueCount(queue: QueueItem[]): number {
  return queue.filter(
    q => q.status === 'issue' || q.status === 'blocked' || q.status === 'in_progress',
  ).length
}
