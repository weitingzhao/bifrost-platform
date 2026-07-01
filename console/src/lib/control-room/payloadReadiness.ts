/**
 * Control Room Phase 2 — Payload depth: trade readiness rows mapped from matrix probes.
 */

import type { MatrixResponse, OpsContextResponse, Reachability, Target } from '@/api/types'
import { evaluatePromoteStatus, type PromoteStatus } from '@/lib/control-room/matrixSummary'
import { worst, type Signal } from '@/lib/control-room/missionSignals'

export type PayloadReadinessRowId = 'daemon' | 'celery' | 'ib' | 'datastore' | 'frontend'

export type PayloadReadinessRowDef = {
  id: PayloadReadinessRowId
  label: string
  role: string
  /** Primary matrix target id for this row */
  targetId: string
  /** Platform L0 policy — never probed for write path */
  policyBlocked?: boolean
}

export const PAYLOAD_READINESS_ROWS: PayloadReadinessRowDef[] = [
  {
    id: 'daemon',
    label: 'Daemon',
    role: 'GsTrading FSM · monitor API health',
    targetId: 'api-monitor',
  },
  {
    id: 'celery',
    label: 'Celery / Ops',
    role: 'Workers · Flower · ops API health',
    targetId: 'api-ops',
  },
  {
    id: 'ib',
    label: 'IB edge',
    role: 'Operator RPC · ingestor · account agent',
    targetId: 'ib-operator-rpc',
    policyBlocked: true,
  },
  {
    id: 'datastore',
    label: 'PG / Redis',
    role: 'PostgreSQL + Redis TCP from probe host',
    targetId: 'postgres',
  },
  {
    id: 'frontend',
    label: 'Trade UI',
    role: 'Nginx SPA entry',
    targetId: 'nginx-spa',
  },
]

export type EnvReadinessCell = {
  signal: Signal
  detail: string
  policyBlocked: boolean
}

export type PayloadReadinessRow = PayloadReadinessRowDef & {
  dev: EnvReadinessCell
  prod: EnvReadinessCell
  /** dev and prod reachability differ (ignores policy-blocked IB row) */
  envDiverges: boolean
}

export type PayloadCouplingSummary = {
  promote: PromoteStatus
  lamp: Signal
  headline: string
  detail: string
}

function findTarget(matrices: MatrixResponse[], env: string, targetId: string): Target | undefined {
  const matrix = matrices.find(m => m.environment === env)
  return matrix?.targets.find(t => t.id === targetId)
}

function datastoreSignal(matrices: MatrixResponse[], env: string): EnvReadinessCell {
  const pg = findTarget(matrices, env, 'postgres')
  const redis = findTarget(matrices, env, 'redis')
  const signals = [pg?.reachability, redis?.reachability].filter(Boolean) as Reachability[]
  const signal = worst(...signals.map(r => r as Signal))
  const parts: string[] = []
  if (pg != null) parts.push(`PG ${pg.reachability}`)
  if (redis != null) parts.push(`Redis ${redis.reachability}`)
  return {
    signal: signals.length === 0 ? 'unknown' : signal,
    detail: parts.length > 0 ? parts.join(' · ') : 'probing',
    policyBlocked: false,
  }
}

function cellFromTarget(t: Target | undefined, policyBlocked: boolean): EnvReadinessCell {
  if (policyBlocked) {
    return {
      signal: 'unknown',
      detail: t?.detail ?? 'L0 blocked — platform must not invoke trade write path',
      policyBlocked: true,
    }
  }
  if (t == null) {
    return { signal: 'unknown', detail: 'not probed', policyBlocked: false }
  }
  return {
    signal: t.reachability as Signal,
    detail: t.detail || t.reachability,
    policyBlocked: false,
  }
}

function cellForRow(
  matrices: MatrixResponse[],
  env: string,
  row: PayloadReadinessRowDef,
): EnvReadinessCell {
  if (row.id === 'datastore') return datastoreSignal(matrices, env)
  return cellFromTarget(findTarget(matrices, env, row.targetId), row.policyBlocked === true)
}

export function buildPayloadReadinessRows(matrices: MatrixResponse[]): PayloadReadinessRow[] {
  return PAYLOAD_READINESS_ROWS.map(def => {
    const dev = cellForRow(matrices, 'dev', def)
    const prod = cellForRow(matrices, 'prod', def)
    const envDiverges =
      !def.policyBlocked &&
      dev.signal !== prod.signal &&
      dev.signal !== 'unknown' &&
      prod.signal !== 'unknown'
    return { ...def, dev, prod, envDiverges }
  })
}

export function countEnvDivergences(rows: PayloadReadinessRow[]): number {
  return rows.filter(r => r.envDiverges).length
}

export function buildPayloadCouplingSummary(
  context: OpsContextResponse | undefined,
  matrices: MatrixResponse[],
): PayloadCouplingSummary | null {
  if (context == null) return null
  const promote = evaluatePromoteStatus(context, matrices)
  const lamp: Signal = promote.ready
    ? 'ok'
    : promote.blockedByDecision || promote.prodFails
      ? 'fail'
      : 'degraded'

  if (promote.ready) {
    return {
      promote,
      lamp,
      headline: 'Coupling gate open',
      detail: 'Prod matrix healthy and release gate pass — Flywheel A may promote when Owner approves.',
    }
  }

  const primaryReason = promote.reasons[0] ?? 'Promote blocked'
  return {
    promote,
    lamp,
    headline: 'Coupling gate blocked',
    detail: primaryReason,
  }
}
