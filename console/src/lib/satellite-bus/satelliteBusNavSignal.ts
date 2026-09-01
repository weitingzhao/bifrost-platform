import { isAllSatelliteBusDeep } from '@/api/core'
import type { AllSatelliteBusDeepResponse, SatelliteBusDeepResponse } from '@/api/satelliteBusTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import { tradeApiTargetCounts } from '@/lib/satellite/tradeApiTargets'
import type { BusEnvId, TradeEnvId } from '@/lib/satellite/socketHealthSemantics'
import { worst, type Signal } from '@/lib/control-room/missionSignals'
import {
  buildSatelliteBusViewModel,
  busHealthToReach,
} from '@/lib/satellite-bus/satelliteBusViewModel'

const TRADE_ENVS: readonly TradeEnvId[] = ['dev', 'stg', 'prod']

export function indexSatelliteBuses(
  data: SatelliteBusDeepResponse | AllSatelliteBusDeepResponse | undefined,
): Partial<Record<BusEnvId, SatelliteBusDeepResponse>> {
  if (data == null) return {}
  if (isAllSatelliteBusDeep(data)) {
    return Object.fromEntries(data.buses.map(b => [b.environment as BusEnvId, b])) as Partial<
      Record<BusEnvId, SatelliteBusDeepResponse>
    >
  }
  const env = data.environment as BusEnvId
  if (env === 'dev' || env === 'stg' || env === 'prod' || env === 'dev-local') {
    return { [env]: data }
  }
  return {}
}

/** Worst-of DEV/STG/PROD bus verdict — same view-model as Bus Status page. */
export function rollupSatelliteBusNav(
  buses: Partial<Record<BusEnvId, SatelliteBusDeepResponse>>,
  matrices: MatrixResponse[],
): { signal: Signal; title: string } {
  const parts: string[] = []
  const signals: Signal[] = []
  for (const env of TRADE_ENVS) {
    const vm = buildSatelliteBusViewModel({
      selectedEnv: env,
      buses,
      tradeApi: tradeApiTargetCounts(matrices.find(m => m.environment === env)),
    })
    signals.push(busHealthToReach(vm.health))
    parts.push(`${env.toUpperCase()} ${vm.healthLabel}`)
  }
  return {
    signal: signals.length > 0 ? worst(...signals) : 'unknown',
    title: `Bus Status: ${parts.join(' · ')}`,
  }
}
