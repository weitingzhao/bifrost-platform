/**
 * Shared Fleet cell construction helpers.
 */
import type { ModuleState } from '@/lib/control-room/missionSignals'
import {
  type FleetCell,
  type FleetCellSignal,
  type FleetEnvColumn,
  type FleetRole,
} from '@/lib/control-room/fleetSnapshot/types'
import { std } from '@/lib/control-room/fleetSnapshot/standards'

export function moduleToCellSignal(state: ModuleState): FleetCellSignal {
  return state.signal
}

export function unavailableCell(
  key: string,
  role: FleetRole,
  env: FleetEnvColumn,
  detail: string,
  disabledReason: string,
): FleetCell {
  return {
    key,
    role,
    env,
    span: false,
    signal: 'unavailable',
    value: '—',
    detail,
    probePath: '',
    standards: [std('probe-path', 'Probe path reachable', 'unavailable', detail, 'path', false)],
    fixScope: null,
    agentFixEnabled: false,
    agentFixDisabledReason: disabledReason,
    countsTowardVerdict: false,
  }
}
