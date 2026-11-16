import type { MatrixResponse, Reachability } from '@/api/matrixTypes'

export type BayLamp = Reachability | 'unknown'

export type MatrixSummary = {
  ok: number
  fail: number
  degraded: number
  unknown: number
  total: number
  worstReach: BayLamp
}

export function summarizeMatrix(m: MatrixResponse): MatrixSummary {
  let ok = 0
  let fail = 0
  let degraded = 0
  let unknown = 0
  for (const t of m.targets) {
    if (t.reachability === 'ok') ok += 1
    else if (t.reachability === 'fail') fail += 1
    else if (t.reachability === 'degraded') degraded += 1
    else unknown += 1
  }
  const worstReach: BayLamp =
    fail > 0 ? 'fail' : degraded > 0 ? 'degraded' : ok > 0 ? 'ok' : 'unknown'
  return { ok, fail, degraded, unknown, total: m.targets.length, worstReach }
}
