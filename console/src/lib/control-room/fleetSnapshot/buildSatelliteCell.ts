/**
 * Satellite Fleet cells.
 */
import type { ClusterPostgresBackupStatusResponse } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { StgSmokeResponse } from '@/api/deliveryTypes'
import { DATA_LAYER_BACKUP_SCOPE, DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { PROD_ENV_FIX_SCOPE } from '@/lib/agent/prodEnvironmentFixPrompt'
import { tradeEnvSignal, type ModuleState, type Signal } from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetCellSignal,
  type FleetEnvColumn,
  type FleetStandard,
} from '@/lib/control-room/fleetSnapshot/types'
import {
  signalFromStandards,
  standardsFromMatrix,
  stgSmokeStandard,
  std,
} from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey } from '@/lib/control-room/fleetSnapshot/nav'
import { moduleToCellSignal, unavailableCell } from '@/lib/control-room/fleetSnapshot/cellHelpers'

export function backupFreshStandard(
  backup?: ClusterPostgresBackupStatusResponse,
): FleetStandard {
  if (backup == null) {
    return std(
      'db-backup-fresh',
      'CNPG backup < 48h',
      'unknown',
      'CNPG backup status unavailable',
      'datastore',
    )
  }
  const raw = (backup.signal || '').toLowerCase()
  const signal: Signal =
    raw === 'ok' || raw === 'degraded' || raw === 'fail'
      ? raw
      : backup.fresh
        ? 'ok'
        : 'fail'
  return std(
    'db-backup-fresh',
    'CNPG backup < 48h',
    signal,
    backup.detail || 'CNPG barman freshness',
    'datastore',
  )
}

export function buildSatelliteCell(input: {
  env: FleetEnvColumn
  matrices: MatrixResponse[]
  stg?: StgSmokeResponse
  postgresBackup?: ClusterPostgresBackupStatusResponse
}): FleetCell {
  const { env, matrices, stg } = input
  const key = cellKey('satellite', env)

  if (env === 'stg') {
    const matrix = matrices.find(m => m.environment === 'stg')
    if (matrix) {
      return satelliteCellFromState(key, env, tradeEnvSignal(matrix), standardsFromMatrix(matrix))
    }
    // Fallback: STG smoke when matrix omitted (legacy gap — must not drop STG)
    if (stg && stg.targets.length > 0) {
      const ok = stg.targets.filter(t => t.reachability === 'ok').length
      const total = stg.targets.length
      const anyFail = stg.targets.some(t => t.reachability === 'fail')
      const anyDeg = stg.targets.some(t => t.reachability === 'degraded')
      const signal: Signal = anyFail ? 'fail' : anyDeg ? 'degraded' : ok === total ? 'ok' : 'degraded'
      return satelliteCellFromState(
        key,
        env,
        { signal, value: `${ok}/${total}`, detail: `STG smoke ${ok}/${total} targets` },
        [stgSmokeStandard(stg)],
      )
    }
    return unavailableCell(
      key,
      'satellite',
      env,
      'No STG matrix or smoke probe payload',
      'STG probe path missing',
    )
  }

  const matrix = matrices.find(m => m.environment === env)
  if (!matrix) {
    return unavailableCell(
      key,
      'satellite',
      env,
      `No matrix for environment=${env}`,
      'Matrix missing for this environment',
    )
  }
  const standards = standardsFromMatrix(matrix)
  if (env === 'prod') {
    standards.push(backupFreshStandard(input.postgresBackup))
  }
  return satelliteCellFromState(key, env, tradeEnvSignal(matrix), standards)
}

function satelliteFixScopeForEnv(env: FleetEnvColumn): string {
  if (env === 'stg') return DELIVER_STG_RECOVER_SCOPE
  return PROD_ENV_FIX_SCOPE
}

function satelliteCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  standards: FleetStandard[],
): FleetCell {
  const derived = signalFromStandards(standards)
  const cellSignal: FleetCellSignal =
    derived === 'unavailable' ? moduleToCellSignal(state) : derived
  const ok = cellSignal === 'ok'
  const backupBlocking = standards.some(
    s => s.id === 'db-backup-fresh' && (s.signal === 'fail' || s.signal === 'degraded'),
  )
  const backupOnly =
    !ok &&
    backupBlocking &&
    standards
      .filter(s => s.required !== false && s.id !== 'db-backup-fresh')
      .every(s => s.signal === 'ok')
  const fixScope = backupOnly ? DATA_LAYER_BACKUP_SCOPE : satelliteFixScopeForEnv(env)
  return {
    key,
    role: 'satellite',
    env,
    span: false,
    signal: cellSignal,
    value: state.value,
    detail: state.detail,
    probePath: '',
    standards,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && cellSignal !== 'unknown',
    agentFixDisabledReason: cellSignal === 'unknown' ? 'Still probing' : undefined,
    countsTowardVerdict: true,
  }
}

/**
 * Engineer = AI Agent plane + Mac seat (probe-bridge / thin-client).
 * Mac is not a board column — it is the Engineer's physical workstation.
 * Prod/STG viewers cannot reach Mac 127.0.0.1 — Mac seat is informational only there.
 */
