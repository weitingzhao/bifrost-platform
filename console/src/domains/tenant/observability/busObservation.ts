import { isAllMatrices, isAllSatelliteBusDeep } from '@/api/core'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { fetchSatelliteBusDeep, fetchMatrix } from '@/api/core'
import type { fetchIbGatewayStatus } from '@/api/network'
import { buildSatelliteBusViewModel } from '@/lib/satellite-bus/satelliteBusViewModel'
import type { BusEnvId } from '@/lib/satellite/socketHealthSemantics'
import { tradeApiTargetCounts } from '@/lib/satellite/tradeApiTargets'
import type { BusHealthInput } from '@/domains/foundation/observability/observabilityViewModel'
export function buildTenantBusObservation(data: Awaited<ReturnType<typeof fetchSatelliteBusDeep>> | undefined, matrixData: Awaited<ReturnType<typeof fetchMatrix>> | undefined, effectiveEnv: 'dev' | 'stg' | 'prod', ib: Awaited<ReturnType<typeof fetchIbGatewayStatus>> | undefined): BusHealthInput | null {
    if (data == null) return null
    const buses = isAllSatelliteBusDeep(data)
      ? Object.fromEntries(data.buses.map(b => [b.environment as BusEnvId, b]))
      : { [data.environment as BusEnvId]: data }
    let matrices: MatrixResponse[] = []
    if (matrixData != null) {
      matrices = isAllMatrices(matrixData) ? matrixData.matrices : [matrixData]
    }
    const envMatrix = matrices.find(m => m.environment === effectiveEnv)
    const vm = buildSatelliteBusViewModel({
      selectedEnv: effectiveEnv,
      buses,
      tradeApi: tradeApiTargetCounts(envMatrix),
    })
    // Traefik NodePort flaps → bus-deep timeout looks like "IB consumer down".
    // Prefer Platform IB Gateway plugin when the failure is clearly a probe timeout.
    const probeTimeout = /context deadline|Timeout exceeded|Client\.Timeout/i.test(vm.topReason)
    const pluginOk =
      ib?.reachability === 'ok' ||
      ib?.reachable === true ||
      String(ib?.summary ?? '').toLowerCase().includes('redis-ib ok')
    if (
      probeTimeout &&
      pluginOk &&
      (vm.health === 'unavailable' || vm.health === 'unknown' || vm.health === 'degraded')
    ) {
      return {
        health: 'healthy',
        topReason: `Monitor probe timed out; Platform IB Gateway plugin ok — ${ib?.summary ?? 'reachable'}`,
      }
    }
    return { health: vm.health, topReason: vm.topReason }
}
