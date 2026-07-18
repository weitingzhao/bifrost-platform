import type { OperateQueueItem, OperateQueueResponse } from '@/api/operateQueueTypes'

export function effectiveOperateLane(item: OperateQueueItem): string {
  return item.operate_lane ?? item.lane ?? 'unassigned'
}

export function deriveAssessmentLabel(
  assessmentStatus: string | undefined,
  queue: OperateQueueResponse | undefined,
  programId: string,
): 'NOT ASSESSED' | 'NO HANDOFF' | 'PENDING REVIEW' | 'APPROVED' | 'IN OPERATE' | 'CLOSED' {
  if (queue?.open.some(item => item.program_id === programId)) return 'IN OPERATE'
  if (queue?.recent_closed.some(item => item.program_id === programId)) return 'CLOSED'
  switch (assessmentStatus) {
    case 'no_handoff':
      return 'NO HANDOFF'
    case 'pending_review':
      return 'PENDING REVIEW'
    case 'approved':
    case 'in_operate':
      return assessmentStatus === 'approved' ? 'APPROVED' : 'IN OPERATE'
    case 'closed':
      return 'CLOSED'
    default:
      return 'NOT ASSESSED'
  }
}

export function buildHandoffAgentPrompt(item: OperateQueueItem): string {
  return [
    `Execute Owner-approved handoff ${item.id} for program ${item.program_id}.`,
    `Reason: ${item.reason ?? item.description ?? item.title}`,
    `Acceptance criteria: ${(item.acceptance_criteria ?? []).join('; ')}`,
    `Verification steps: ${(item.verification_steps ?? []).join('; ')}`,
    'D10 live trading remains BLOCKED. Do not enable live order execution.',
    'Return completion and verification evidence; do not close the handoff automatically.',
  ].join('\n')
}
