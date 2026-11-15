import { serverFetch } from '@/api/serverContact'
import type { AllMatricesResponse, MatrixResponse, SelfHealthResponse, TopologyResponse } from './matrixTypes'
import type { AllSatelliteBusDeepResponse, SatelliteBusDeepResponse } from './satelliteBusTypes'
import type { OpsContextResponse } from './opsContextTypes'

export async function fetchMatrix(env?: string): Promise<MatrixResponse | AllMatricesResponse> {
  const url = env ? `/api/v1/matrix?env=${encodeURIComponent(env)}` : '/api/v1/matrix'
  const r = await serverFetch(url)
  if (!r.ok) throw new Error(`matrix: HTTP ${r.status}`)
  return r.json() as Promise<MatrixResponse | AllMatricesResponse>
}

export function isAllMatrices(
  data: MatrixResponse | AllMatricesResponse
): data is AllMatricesResponse {
  return 'matrices' in data
}

export function isAllSatelliteBusDeep(
  data: SatelliteBusDeepResponse | AllSatelliteBusDeepResponse
): data is AllSatelliteBusDeepResponse {
  return 'buses' in data
}

export async function fetchSatelliteBusDeep(
  env?: string,
): Promise<SatelliteBusDeepResponse | AllSatelliteBusDeepResponse> {
  const url = env ? `/api/v1/satellite/bus-deep?env=${encodeURIComponent(env)}` : '/api/v1/satellite/bus-deep'
  const r = await serverFetch(url)
  if (!r.ok) throw new Error(`satellite bus deep: HTTP ${r.status}`)
  return r.json() as Promise<SatelliteBusDeepResponse | AllSatelliteBusDeepResponse>
}

export async function fetchTopology(env: string): Promise<TopologyResponse> {
  const r = await serverFetch(`/api/v1/topology?env=${encodeURIComponent(env)}`)
  if (!r.ok) throw new Error(`topology: HTTP ${r.status}`)
  return r.json() as Promise<TopologyResponse>
}

export async function fetchContext(): Promise<OpsContextResponse> {
  const r = await serverFetch('/api/v1/context')
  if (!r.ok) throw new Error(`context: HTTP ${r.status}`)
  return r.json() as Promise<OpsContextResponse>
}

export async function fetchSelfHealth(): Promise<SelfHealthResponse> {
  const r = await serverFetch('/api/v1/self-health')
  if (!r.ok) throw new Error(`self-health: HTTP ${r.status}`)
  return r.json() as Promise<SelfHealthResponse>
}
