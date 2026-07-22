import type { AllMatricesResponse, EnvironmentSummary, EscapeHatchDrillResponse, EscapeHatchResponse, MatrixResponse, SelfHealthResponse, TopologyResponse, VerifyMissionSnapshotResponse, VerifyPayloadResponse } from './matrixTypes'
import type { AllSatelliteBusDeepResponse, SatelliteBusDeepResponse } from './satelliteBusTypes'
import type { OpsContextResponse } from './opsContextTypes'
import { authedFetch } from './client'

export async function fetchEnvironments(): Promise<EnvironmentSummary[]> {
  const r = await fetch('/api/v1/environments')
  if (!r.ok) throw new Error(`environments: HTTP ${r.status}`)
  const data = (await r.json()) as { environments: EnvironmentSummary[] }
  return data.environments
}

export async function fetchMatrix(env?: string): Promise<MatrixResponse | AllMatricesResponse> {
  const url = env ? `/api/v1/matrix?env=${encodeURIComponent(env)}` : '/api/v1/matrix'
  const r = await fetch(url)
  if (!r.ok) throw new Error(`matrix: HTTP ${r.status}`)
  return r.json() as Promise<MatrixResponse | AllMatricesResponse>
}

export async function fetchVerifyPayload(): Promise<VerifyPayloadResponse> {
  const r = await fetch('/api/v1/mission/verify-payload')
  if (!r.ok) throw new Error(`verify-payload: HTTP ${r.status}`)
  return r.json() as Promise<VerifyPayloadResponse>
}

export async function fetchVerifyMissionSnapshot(): Promise<VerifyMissionSnapshotResponse> {
  const r = await fetch('/api/v1/mission/verify-snapshot')
  if (!r.ok) throw new Error(`verify-snapshot: HTTP ${r.status}`)
  return r.json() as Promise<VerifyMissionSnapshotResponse>
}

export async function fetchPlatformHealth(): Promise<boolean> {
  try {
    const r = await fetch('/health')
    return r.ok
  } catch {
    return false
  }
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
  const r = await fetch(url)
  if (!r.ok) throw new Error(`satellite bus deep: HTTP ${r.status}`)
  return r.json() as Promise<SatelliteBusDeepResponse | AllSatelliteBusDeepResponse>
}

export async function fetchTopology(env: string): Promise<TopologyResponse> {
  const r = await fetch(`/api/v1/topology?env=${encodeURIComponent(env)}`)
  if (!r.ok) throw new Error(`topology: HTTP ${r.status}`)
  return r.json() as Promise<TopologyResponse>
}

export async function fetchContext(): Promise<OpsContextResponse> {
  const r = await fetch('/api/v1/context')
  if (!r.ok) throw new Error(`context: HTTP ${r.status}`)
  return r.json() as Promise<OpsContextResponse>
}

export async function fetchSelfHealth(): Promise<SelfHealthResponse> {
  const r = await fetch('/api/v1/self-health')
  if (!r.ok) throw new Error(`self-health: HTTP ${r.status}`)
  return r.json() as Promise<SelfHealthResponse>
}

export async function fetchEscapeHatch(): Promise<EscapeHatchResponse> {
  const r = await fetch('/api/v1/platform/escape-hatch')
  if (!r.ok) throw new Error(`escape-hatch: HTTP ${r.status}`)
  return r.json() as Promise<EscapeHatchResponse>
}

export async function recordEscapeHatchDrill(body?: {
  notes?: string
  route_ids?: string[]
}): Promise<EscapeHatchDrillResponse> {
  const r = await authedFetch('escape hatch drill', '/api/v1/platform/escape-hatch/drill', {
    method: 'POST',
    body: JSON.stringify(body ?? {}),
  })
  return r.json() as Promise<EscapeHatchDrillResponse>
}

