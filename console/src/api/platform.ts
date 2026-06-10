import type {
  AllMatricesResponse,
  EnvironmentSummary,
  MatrixResponse,
  TopologyResponse,
} from './types'

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

export async function fetchTopology(env: string): Promise<TopologyResponse> {
  const r = await fetch(`/api/v1/topology?env=${encodeURIComponent(env)}`)
  if (!r.ok) throw new Error(`topology: HTTP ${r.status}`)
  return r.json() as Promise<TopologyResponse>
}
