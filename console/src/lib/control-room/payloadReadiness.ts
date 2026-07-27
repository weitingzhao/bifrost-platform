/**
 * Trade readiness rows for Control Room / Satellite Bus — projected from FleetSnapshot
 * (same truth as TCC Fleet Desk). No independent matrix/L0-blocked path.
 */

import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { evaluatePromoteStatus, type PromoteStatus } from '@/lib/control-room/matrixSummary'
import { type Signal } from '@/lib/control-room/missionSignals'
import type {
  FleetCell,
  FleetCellSignal,
  FleetEnvColumn,
  FleetSnapshot,
  FleetStandard,
} from '@/lib/control-room/fleetSnapshot'
import { cellKey } from '@/lib/control-room/fleetSnapshot'

export type PayloadReadinessRowId = 'daemon' | 'celery' | 'ib' | 'datastore' | 'frontend'

export type PayloadMapMode = 'runtime-map' | 'fleet-vendor'

export type PayloadReadinessRowDef = {
  id: PayloadReadinessRowId
  label: string
  role: string
  fleetRole: 'satellite' | 'vendor'
  mapMode: PayloadMapMode
}

export const PAYLOAD_READINESS_ROWS: PayloadReadinessRowDef[] = [
  {
    id: 'daemon',
    label: 'Daemon',
    role: 'GsTrading FSM · monitor API health',
    fleetRole: 'satellite',
    mapMode: 'runtime-map',
  },
  {
    id: 'celery',
    label: 'Celery / Ops',
    role: 'Workers · Flower · ops API health',
    fleetRole: 'satellite',
    mapMode: 'runtime-map',
  },
  {
    id: 'ib',
    label: 'IB edge',
    role: 'Operator RPC · ingestor · account agent',
    fleetRole: 'vendor',
    mapMode: 'fleet-vendor',
  },
  {
    id: 'datastore',
    label: 'PG / Redis',
    role: 'PostgreSQL + Redis TCP from probe host',
    fleetRole: 'satellite',
    mapMode: 'runtime-map',
  },
  {
    id: 'frontend',
    label: 'Trade UI',
    role: 'Nginx SPA entry',
    fleetRole: 'satellite',
    mapMode: 'runtime-map',
  },
]

export const PAYLOAD_READINESS_ENVS: FleetEnvColumn[] = ['dev', 'stg', 'prod']

export type EnvReadinessCell = {
  signal: Signal
  detail: string
  /** Runtime Map target when mapMode is runtime-map */
  mapTargetId?: string | null
}

export type PayloadReadinessRow = PayloadReadinessRowDef & {
  dev: EnvReadinessCell
  stg: EnvReadinessCell
  prod: EnvReadinessCell
  /** True when any pair of env signals diverge (ignoring unknown) */
  envDiverges: boolean
}

export type PayloadCouplingSummary = {
  promote: PromoteStatus
  lamp: Signal
  headline: string
  detail: string
}

function toDisplaySignal(s: FleetCellSignal): Signal {
  if (s === 'unavailable') return 'unknown'
  return s
}

function worstFleetSignals(signals: FleetCellSignal[]): FleetCellSignal {
  if (signals.length === 0) return 'unknown'
  if (signals.includes('fail')) return 'fail'
  if (signals.includes('degraded')) return 'degraded'
  if (signals.includes('unavailable')) return 'unavailable'
  if (signals.includes('unknown')) return 'unknown'
  return 'ok'
}

function findFleetCell(
  fleet: FleetSnapshot,
  role: 'satellite' | 'vendor',
  env: FleetEnvColumn,
): FleetCell | undefined {
  const key = cellKey(role, env)
  const exact = fleet.cells.find(c => c.key === key)
  if (exact != null) return exact
  // Platform IB Gateway / vendor feeds are shared across Trade NS (span cell only).
  if (role === 'vendor') {
    return fleet.cells.find(c => c.key === cellKey('vendor', 'span'))
  }
  return undefined
}

function matchDaemon(s: FleetStandard): boolean {
  const id = s.id.toLowerCase()
  if (id.includes('massive')) return false
  return id === 'api-monitor' || id.includes('api-monitor') || (id.includes('monitor') && s.group === 'api')
}

function matchCelery(s: FleetStandard): boolean {
  const id = s.id.toLowerCase()
  if (id === 'api-ops' || id.includes('api-ops')) return true
  if (id.includes('celery')) return true
  if (id === 'flower' || id.includes('flower')) return true
  return false
}

function matchIb(s: FleetStandard): boolean {
  return s.id === 'ib-feed'
}

function matchDatastore(s: FleetStandard): boolean {
  const id = s.id.toLowerCase()
  return s.group === 'datastore' || id.includes('postgres') || id.includes('redis')
}

function matchFrontend(s: FleetStandard): boolean {
  const id = s.id.toLowerCase()
  return s.group === 'edge' || id.includes('nginx') || id.includes('spa')
}

function matcherForRow(id: PayloadReadinessRowId): (s: FleetStandard) => boolean {
  switch (id) {
    case 'daemon':
      return matchDaemon
    case 'celery':
      return matchCelery
    case 'ib':
      return matchIb
    case 'datastore':
      return matchDatastore
    case 'frontend':
      return matchFrontend
  }
}

function mapTargetForMatched(id: PayloadReadinessRowId, matched: FleetStandard[]): string | null {
  if (matched.length === 0) return null
  if (id === 'datastore') {
    const pg = matched.find(s => s.id.toLowerCase().includes('postgres'))
    if (pg != null) return pg.id
    const redis = matched.find(s => s.id.toLowerCase().includes('redis'))
    return redis?.id ?? matched[0]?.id ?? null
  }
  return matched[0]?.id ?? null
}

function resolveEnvCell(
  def: PayloadReadinessRowDef,
  fleet: FleetSnapshot,
  env: FleetEnvColumn,
): EnvReadinessCell {
  const cell = findFleetCell(fleet, def.fleetRole, env)
  if (cell == null) {
    return { signal: 'unknown', detail: `No Fleet cell ${def.fleetRole}:${env}`, mapTargetId: null }
  }

  const standards = cell.standards ?? []
  const matched = standards.filter(matcherForRow(def.id))

  if (matched.length > 0) {
    const signal = worstFleetSignals(matched.map(s => s.signal))
    const detail = matched.map(s => s.reason).filter(Boolean).join(' · ') || matched[0].label
    return {
      signal: toDisplaySignal(signal),
      detail,
      mapTargetId: def.mapMode === 'runtime-map' ? mapTargetForMatched(def.id, matched) : null,
    }
  }

  // STG satellite often only has stg-smoke rollup
  if (def.fleetRole === 'satellite') {
    const smoke = standards.find(s => s.id === 'stg-smoke')
    if (smoke != null) {
      return {
        signal: toDisplaySignal(smoke.signal),
        detail: `STG smoke rollup · ${smoke.reason}`,
        mapTargetId: null,
      }
    }
  }

  return {
    signal: 'unknown',
    detail: 'No matching Fleet standard',
    mapTargetId: null,
  }
}

function envDiverges(cells: EnvReadinessCell[]): boolean {
  const known = cells.filter(c => c.signal !== 'unknown')
  if (known.length < 2) return false
  const first = known[0].signal
  return known.some(c => c.signal !== first)
}

/** Project Trade readiness rows from Fleet (TCC ground truth). */
export function projectPayloadReadinessRows(fleet: FleetSnapshot): PayloadReadinessRow[] {
  return PAYLOAD_READINESS_ROWS.map(def => {
    const dev = resolveEnvCell(def, fleet, 'dev')
    const stg = resolveEnvCell(def, fleet, 'stg')
    const prod = resolveEnvCell(def, fleet, 'prod')
    return {
      ...def,
      dev,
      stg,
      prod,
      envDiverges: envDiverges([dev, stg, prod]),
    }
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

/** Display label for readiness cell (shared UI). */
export function payloadReadinessStatusLabel(signal: Signal): string {
  return signal === 'ok'
    ? 'NOMINAL'
    : signal === 'degraded'
      ? 'CAUTION'
      : signal === 'fail'
        ? 'CRITICAL'
        : 'PROBING'
}
