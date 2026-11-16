import type { Target } from '@/api/matrixTypes'

import '@/lib/control-room/bayRegistry'

export type { BayLamp, MatrixSummary } from '@/domains/foundation/observation-display/matrixSummary'
export { summarizeMatrix } from '@/domains/foundation/observation-display/matrixSummary'

export type PromoteStatus = {
  ready: boolean
  blockedByDecision: boolean
  prodFails: boolean
  prodOk: boolean
  gateDone: boolean
  gatePass: boolean
  cutoverBlocker: string | null
  reasons: string[]
}


/**
 * Whether a matrix target should affect Trade / Satellite launch readiness.
 *
 * Excludes by design:
 * - `trade_write` / auth=blocked — Platform L0 must not probe write paths (R-DV3)
 * - `ops-capabilities` with auth=skipped — optional token not configured (not a failure)
 */
function countsTowardTradeReadiness(t: Target): boolean {
  if (t.category === 'trade_write') return false
  if (t.auth === 'blocked') return false
  if (t.id === 'ops-capabilities' && t.auth === 'skipped') return false
  return true
}

/** Trade + datastore targets that participate in payload / launch Go-No-Go scoring. */
export function tradeReadinessTargets(targets: Target[]): Target[] {
  return targets.filter(
    t =>
      countsTowardTradeReadiness(t) &&
      (t.category.startsWith('trade') || t.category === 'datastore'),
  )
}

export type StgDeliverStatus = {
  ready: boolean
  smokeOk: boolean
  smokeFails: boolean
  deliverSucceeded: boolean
  reasons: string[]
}

export type StgReleaseStatus = StgDeliverStatus & {
  gatePass: boolean
  tierBReady: boolean
  releaseReady: boolean
  releaseReasons: string[]
}
