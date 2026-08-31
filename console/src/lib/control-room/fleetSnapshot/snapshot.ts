/**
 * Assemble Fleet snapshot from role cell builders.
 */
import type { AgentBridgeResponse } from '@/api/agentTypes'
import type { ClusterPostgresBackupStatusResponse, ClusterSummary } from '@/api/clusterTypes'
import type { IbGatewayStatusResponse } from '@/api/satelliteBusTypes'
import type { MatrixResponse, SelfHealthResponse } from '@/api/matrixTypes'
import type { RemediationHealthResponse } from '@/api/remediationTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import {
  FLEET_COLUMNS,
  FLEET_ROLES,
  type FleetCell,
  type FleetCellSignal,
  type FleetEnvColumn,
  type FleetRole,
  type FleetSnapshot,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot/types'
import { cellCountsTowardVerdict, resolveCellGate } from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey, normalizeViewerEnv } from '@/lib/control-room/fleetSnapshot/nav'
import { buildRocketCell } from '@/lib/control-room/fleetSnapshot/buildRocketCell'
import { buildSatelliteCell } from '@/lib/control-room/fleetSnapshot/buildSatelliteCell'
import { buildEngineerCell } from '@/lib/control-room/fleetSnapshot/buildEngineerCell'
import { buildGroundCell } from '@/lib/control-room/fleetSnapshot/buildGroundCell'
import { buildVendorCell } from '@/lib/control-room/fleetSnapshot/buildVendorCell'
import { resolveFleetVerdict, severityRank } from '@/lib/control-room/fleetSnapshot/verdict'

export type BuildFleetSnapshotInput = {
  viewerEnv: string
  self?: SelfHealthResponse
  matrices: MatrixResponse[]
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
  runner?: RemediationHealthResponse
  bridge?: AgentBridgeResponse
  /** True when Ground Systems seat (probe-bridge) is reachable — Engineer Mac seat can be N/A. */
  groundBridgeReady?: boolean
  cluster?: ClusterSummary
  postgresBackup?: ClusterPostgresBackupStatusResponse
  /** IB Gateway plugin — required Vendor feed (IB Client). */
  ibGateway?: IbGatewayStatusResponse
  /**
   * Trade monitor reports ib_not_connected (execution arm / D10 observe).
   * Surfaces as informational Vendor chip; does not block GO.
   */
  daemonIbObserve?: boolean
}

/**
 * Probe lattice only — no Checklist virtual chips.
 * Prefer {@link buildFleetSnapshot} (core + union finalize) for UI / scripts.
 */
export function buildFleetSnapshotCore(input: BuildFleetSnapshotInput): FleetSnapshot {
  const viewerEnv = normalizeViewerEnv(input.viewerEnv)
  const columns: FleetEnvColumn[] = [...FLEET_COLUMNS]

  const cells: FleetCell[] = []

  for (const env of columns) {
    cells.push(
      buildRocketCell({
        env,
        viewerEnv,
        self: input.self,
        supply: input.supply,
        stg: input.stg,
      }),
    )
    cells.push(
      buildSatelliteCell({
        env,
        matrices: input.matrices,
        stg: input.stg,
        postgresBackup: input.postgresBackup,
      }),
    )
  }

  cells.push(
    buildEngineerCell({
      runner: input.runner,
      bridge: input.bridge,
      viewerEnv,
      groundBridgeReady: input.groundBridgeReady,
    }),
  )
  cells.push(buildGroundCell({ cluster: input.cluster }))
  cells.push(
    buildVendorCell({
      bridge: input.bridge,
      matrices: input.matrices,
      ibGateway: input.ibGateway,
      daemonIbObserve: input.daemonIbObserve,
    }),
  )

  const annotated = cells.map(c => ({
    ...c,
    countsTowardVerdict: cellCountsTowardVerdict(c),
  }))
  const verdict = resolveFleetVerdict(annotated)
  const scored = annotated.filter(cellCountsTowardVerdict)
  const fleetNominal = scored.length > 0 && scored.every(c => resolveCellGate(c) === 'GO')
  const fleetClear = fleetNominal

  return {
    viewerEnv,
    columns,
    roles: [...FLEET_ROLES],
    cells: annotated,
    verdict,
    fleetNominal,
    fleetClear,
  }
}

export function getCell(
  snap: FleetSnapshot,
  role: FleetRole,
  env: FleetEnvColumn | 'span',
): FleetCell | undefined {
  const key = cellKey(role, env)
  return snap.cells.find(c => c.key === key)
}

/**
 * Map viewer seat onto a Fleet column (dev-local → DEV).
 * Used by Ops icon-rail env strip to underline the current seat.
 */
export function viewerEnvToColumn(env: FleetViewerEnv): FleetEnvColumn {
  return env === 'dev-local' ? 'dev' : env
}

export type FleetEnvColumnPosture = Record<FleetEnvColumn, FleetCellSignal>

/**
 * Per-column posture for Ops TaskModeIconRail: worst scored Rocket+Satellite
 * signal in that env (unavailable / non-scoring cells skipped).
 */
export function fleetEnvColumnPosture(snap: FleetSnapshot): FleetEnvColumnPosture {
  const out: FleetEnvColumnPosture = {
    dev: 'unknown',
    stg: 'unknown',
    prod: 'unknown',
  }
  for (const env of FLEET_COLUMNS) {
    let worst: FleetCellSignal | null = null
    for (const role of ['rocket', 'satellite'] as const) {
      const cell = getCell(snap, role, env)
      if (cell == null || !cellCountsTowardVerdict(cell)) continue
      if (worst == null || severityRank(cell.signal) > severityRank(worst)) {
        worst = cell.signal
      }
    }
    out[env] = worst ?? 'unknown'
  }
  return out
}

/**
 * Operate summary label when queue is empty but fleet is not clear.
 * fleetClear follows scored verdict (unavailable excluded) — must not hardcode Clear.
 */
export function operateQueueClearLabel(queueOpen: number, fleetClear: boolean): string {
  if (queueOpen > 0) return `${queueOpen} open`
  if (!fleetClear) return 'Queue clear · fleet not clear'
  return 'Clear'
}
