/**
 * Trade API target selection from the connectivity matrix.
 * Shared between SatelliteBusPage and the Observability hub so both feed
 * buildSatelliteBusViewModel with the same tradeApi counts (verdict SSOT).
 */

import type { MatrixResponse, Target } from '@/api/types'

export function filterTradeApiTargets(matrix: MatrixResponse): Target[] {
  return matrix.targets.filter(
    t =>
      t.category === 'trade_api' ||
      t.category === 'trade_frontend' ||
      t.id === 'nginx-spa' ||
      t.id.startsWith('api-'),
  )
}

export function tradeApiTargetCounts(
  matrix: MatrixResponse | undefined,
): { ok: number; total: number } {
  if (matrix == null) return { ok: 0, total: 0 }
  const targets = filterTradeApiTargets(matrix)
  const ok = targets.filter(t => t.reachability === 'ok').length
  return { ok, total: targets.length }
}
