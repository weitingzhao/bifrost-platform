/**
 * Smart initial step focus for the lane detail pages (Launch Rocket / Deploy
 * Satellite). Both pages share the fixed 4-step layout:
 * [Staging Deploy, Staging Gate, Production Deploy, Production Gate].
 *
 * Pure and deterministic — tested in scripts/lane-detail-context-test.ts.
 */
import type { LaneDetailReason } from '@/lib/delivery/laneDetailContext'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'

const STG_GATE_INDEX = 1
const GATE_STEP_INDICES: readonly number[] = [1, 3]

/**
 * Resolve which step the detail page should focus when it first has data.
 *
 * A deep-link reason takes precedence when it clearly points at a step kind:
 * - `acceptance-detail` → Staging Gate
 * - `manual-gate` → first gate that has not passed
 * - `failed-run` → first failed step
 *
 * Otherwise the generic rules apply, in priority order:
 * 1. first `error` step (triage failures first)
 * 2. first `active` step (follow the running work)
 * 3. first not-`done` step (what to do next)
 * 4. all done → last step (show the final gate result, not a re-deploy invite)
 */
export function resolveInitialLaneStep(
  statuses: readonly StepStatus[],
  reason: LaneDetailReason = 'direct',
): number {
  if (statuses.length === 0) return 0
  const lastIndex = statuses.length - 1

  if (reason === 'acceptance-detail') return Math.min(STG_GATE_INDEX, lastIndex)
  if (reason === 'manual-gate') {
    const gate = GATE_STEP_INDICES.find(i => i <= lastIndex && statuses[i] !== 'done')
    if (gate != null) return gate
  }
  if (reason === 'failed-run') {
    const failed = statuses.indexOf('error')
    if (failed >= 0) return failed
  }

  const error = statuses.indexOf('error')
  if (error >= 0) return error
  const active = statuses.indexOf('active')
  if (active >= 0) return active
  const notDone = statuses.findIndex(s => s !== 'done')
  if (notDone >= 0) return notDone
  return lastIndex
}
