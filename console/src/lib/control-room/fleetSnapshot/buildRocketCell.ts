/**
 * Rocket Fleet cells (Control + GitOps).
 */
import type { SelfHealthResponse } from '@/api/matrixTypes'
import type { StgSmokeResponse, SupplyChainResponse } from '@/api/deliveryTypes'
import { PLATFORM_SELF_HEALTH_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import {
  controlSignal,
  type ModuleState,
  type Signal,
  worst,
} from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetCellSignal,
  type FleetEnvColumn,
  type FleetStandard,
  type FleetViewerEnv,
} from '@/lib/control-room/fleetSnapshot/types'
import {
  signalFromStandards,
  standardsFromSelfProbes,
} from '@/lib/control-room/fleetSnapshot/standards'
import { cellKey } from '@/lib/control-room/fleetSnapshot/nav'
import { moduleToCellSignal, unavailableCell } from '@/lib/control-room/fleetSnapshot/cellHelpers'

function selfHealthEnvSignal(
  self: SelfHealthResponse | undefined,
  env: 'dev' | 'stg' | 'prod',
): ModuleState {
  if (!self) return { signal: 'unknown', value: '…', detail: 'Self-health: probing' }
  const probes = self.probes.filter(p => p.env === env)
  if (probes.length === 0) {
    return {
      signal: 'unknown',
      value: 'n/a',
      detail: `No self-health probes tagged env=${env}`,
    }
  }
  const statuses = probes.map(p => p.status as Signal)
  const signal = worst(...statuses)
  const ok = probes.filter(p => p.status === 'ok').length
  return {
    signal,
    value: `${ok}/${probes.length}`,
    detail: `Platform self-health ${env}: ${ok}/${probes.length} probes OK`,
  }
}

/**
 * Rocket cell for a column — Control + GitOps only.
 * Deliver / STG smoke live on Launch Pad + Promote, not on Fleet Rocket
 * (Owner: Rocket board is platform health, not the release pipeline lane).
 */
export function buildRocketCell(input: {
  env: FleetEnvColumn
  viewerEnv: FleetViewerEnv
  self?: SelfHealthResponse
  supply?: SupplyChainResponse
  stg?: StgSmokeResponse
}): FleetCell {
  const { env, viewerEnv, self } = input
  const key = cellKey('rocket', env)

  if (env === 'stg') {
    const stgHealth = selfHealthEnvSignal(self, 'stg')
    const unreachable =
      viewerEnv !== 'dev' &&
      stgHealth.signal === 'unknown' &&
      (self == null || self.probes.filter(p => p.env === 'stg').length === 0)
    if (unreachable) {
      return unavailableCell(
        key,
        'rocket',
        env,
        'STG platform pull probes not reachable from this viewer seat',
        'Probe path unavailable from this viewer',
      )
    }
    const standards = standardsFromSelfProbes(self, 'stg')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        ...stgHealth,
        signal: derived === 'unavailable' ? stgHealth.signal : (derived as Signal),
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  if (env === 'prod') {
    const standards = standardsFromSelfProbes(self, 'prod')
    const prodHealth = selfHealthEnvSignal(self, 'prod')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        ...prodHealth,
        signal: derived === 'unavailable' ? prodHealth.signal : (derived as Signal),
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  // env === 'dev'
  if (viewerEnv === 'dev' || viewerEnv === 'dev-local') {
    const control = controlSignal(self)
    // Local seat: Control + GitOps from this platform-api view (not a dump of STG/PROD as "DEV")
    const standards = standardsFromSelfProbes(self, 'local')
    const derived = signalFromStandards(standards)
    return rocketCellFromState(
      key,
      env,
      {
        signal: derived === 'unavailable' ? control.signal : (derived as Signal),
        value: control.value,
        detail: control.detail,
      },
      PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
      standards,
    )
  }

  // Viewer on prod/stg — Rocket DEV via cluster pull
  const devPull = selfHealthEnvSignal(self, 'dev')
  if (devPull.signal === 'unknown' && (self == null || self.probes.filter(p => p.env === 'dev').length === 0)) {
    return unavailableCell(
      key,
      'rocket',
      env,
      'DEV platform pull probes not configured / unreachable from this viewer seat',
      'Probe path unavailable from this viewer',
    )
  }
  const standards = standardsFromSelfProbes(self, 'dev')
  const derived = signalFromStandards(standards)
  return rocketCellFromState(
    key,
    env,
    {
      ...devPull,
      signal: derived === 'unavailable' ? devPull.signal : (derived as Signal),
    },
    PLATFORM_SELF_HEALTH_RECOVER_SCOPE,
    standards,
  )
}


function rocketCellFromState(
  key: string,
  env: FleetEnvColumn,
  state: ModuleState,
  fixScope: string,
  standards: FleetStandard[],
): FleetCell {
  const signal = signalFromStandards(standards)
  const cellSignal: FleetCellSignal =
    signal === 'unavailable' ? moduleToCellSignal(state) : signal
  const ok = cellSignal === 'ok'
  return {
    key,
    role: 'rocket',
    env,
    span: false,
    signal: cellSignal,
    value: state.value,
    detail: state.detail,
    probePath: '',
    standards,
    fixScope: ok ? null : fixScope,
    agentFixEnabled: !ok && cellSignal !== 'unknown',
    agentFixDisabledReason: ok
      ? undefined
      : cellSignal === 'unknown'
        ? 'Still probing'
        : undefined,
  }
}

