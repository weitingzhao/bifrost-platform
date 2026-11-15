/**
 * Control Room Operate Loop — dispatch packs for Agent Desk / operator plane.
 */

/**
 * Control Room Operate Loop — dispatch packs for Agent Desk / operator plane.
 */
import type { MatrixResponse } from '@/api/matrixTypes'

import '@/lib/control-room/agentContextPacks'
import '@/lib/control-room/missionSignals'

export type FailingMatrixTarget = {
  environment: string
  id: string
  reachability: string
  detail?: string
}

export function listFailingMatrixTargets(matrices: MatrixResponse[]): FailingMatrixTarget[] {
  const out: FailingMatrixTarget[] = []
  for (const m of matrices) {
    for (const t of m.targets) {
      if (t.reachability === 'fail' || t.reachability === 'degraded') {
        out.push({
          environment: m.environment,
          id: t.id,
          reachability: t.reachability,
          detail: t.detail,
        })
      }
    }
  }
  return out
}
