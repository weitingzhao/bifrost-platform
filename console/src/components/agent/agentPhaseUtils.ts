import type { RemediationPhase } from '@/api/remediationTypes'

export const PHASE_STEPS: { key: RemediationPhase; label: string }[] = [
  { key: 'starting', label: 'Start' },
  { key: 'diagnosing', label: 'Diagnose' },
  { key: 'awaiting_approval', label: 'Decide' },
  { key: 'remediating', label: 'Remediate' },
  { key: 'verifying', label: 'Verify' },
  { key: 'done', label: 'Done' },
]

export function phaseIndex(phase: RemediationPhase | undefined): number {
  if (phase == null) return -1
  if (phase === 'failed' || phase === 'cancelled') return PHASE_STEPS.length
  const idx = PHASE_STEPS.findIndex(s => s.key === phase)
  return idx >= 0 ? idx : -1
}

export type StepVisualState = 'done' | 'active' | 'pending' | 'failed'

export function stepVisualState(
  index: number,
  current: number,
  failed: boolean,
  currentPhase: RemediationPhase | undefined,
): StepVisualState {
  if (failed && index === current) return 'failed'
  if (index < current || currentPhase === 'done') return 'done'
  if (index === current) return 'active'
  return 'pending'
}

export function phaseProgressSummary(
  current: number,
  currentPhase: RemediationPhase | undefined,
): { doneCount: number; remainingCount: number; total: number } {
  const total = PHASE_STEPS.length
  if (currentPhase === 'done') return { doneCount: total, remainingCount: 0, total }
  if (current < 0) return { doneCount: 0, remainingCount: total - 1, total }
  const doneCount = current
  const remainingCount = Math.max(0, total - doneCount - 1)
  return { doneCount, remainingCount, total }
}
