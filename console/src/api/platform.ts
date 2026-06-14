import type {
  ActuationResponse,
  AllMatricesResponse,
  AuditResponse,
  AuthCapabilities,
  ClusterEventsResponse,
  ClusterMetricsResponse,
  ClusterNamespacesResponse,
  ClusterNodesResponse,
  ClusterSummary,
  ClusterSyncResponse,
  ClusterWorkloadsResponse,
  EnvironmentSummary,
  MatrixResponse,
  OpsContextResponse,
  PodLogsResponse,
  RolloutRestartRequest,
  ScaleRequest,
  TopologyResponse,
} from './types'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

function operatorToken(): string {
  return getPlatformOperatorToken()
}

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as { error?: string; message?: string }
    detail = body.error ?? body.message ?? detail
  } catch {
    // keep status detail
  }
  return new Error(`${prefix}: ${detail}`)
}

async function authedFetch(prefix: string, input: RequestInfo | URL, init: RequestInit = {}) {
  const token = operatorToken()
  const headers = new Headers(init.headers)
  headers.set('Content-Type', 'application/json')
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch(input, { ...init, headers })
  if (!r.ok) throw await parseError(prefix, r)
  return r
}

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

export async function fetchContext(): Promise<OpsContextResponse> {
  const r = await fetch('/api/v1/context')
  if (!r.ok) throw new Error(`context: HTTP ${r.status}`)
  return r.json() as Promise<OpsContextResponse>
}

export async function fetchCluster(): Promise<ClusterSummary> {
  const r = await fetch('/api/v1/cluster')
  if (!r.ok) throw new Error(`cluster: HTTP ${r.status}`)
  return r.json() as Promise<ClusterSummary>
}

export async function fetchClusterNodes(): Promise<ClusterNodesResponse> {
  const r = await fetch('/api/v1/cluster/nodes')
  if (!r.ok) throw new Error(`cluster nodes: HTTP ${r.status}`)
  return r.json() as Promise<ClusterNodesResponse>
}

export async function fetchClusterMetrics(limit = 8): Promise<ClusterMetricsResponse> {
  const r = await fetch(`/api/v1/cluster/metrics?limit=${limit}`)
  if (!r.ok) throw new Error(`cluster metrics: HTTP ${r.status}`)
  return r.json() as Promise<ClusterMetricsResponse>
}

export async function fetchClusterNamespaces(
  watch?: 'bifrost' | '',
): Promise<ClusterNamespacesResponse> {
  const qs = watch === 'bifrost' ? '?watch=bifrost' : ''
  const r = await fetch(`/api/v1/cluster/namespaces${qs}`)
  if (!r.ok) throw new Error(`cluster namespaces: HTTP ${r.status}`)
  return r.json() as Promise<ClusterNamespacesResponse>
}

export async function fetchClusterWorkloads(ns: string): Promise<ClusterWorkloadsResponse> {
  const r = await fetch(`/api/v1/cluster/workloads?ns=${encodeURIComponent(ns)}`)
  if (!r.ok) throw new Error(`cluster workloads: HTTP ${r.status}`)
  return r.json() as Promise<ClusterWorkloadsResponse>
}

export async function fetchClusterEvents(
  ns?: string,
  limit = 50,
): Promise<ClusterEventsResponse> {
  const params = new URLSearchParams()
  if (ns) params.set('ns', ns)
  params.set('limit', String(limit))
  const r = await fetch(`/api/v1/cluster/events?${params}`)
  if (!r.ok) throw new Error(`cluster events: HTTP ${r.status}`)
  return r.json() as Promise<ClusterEventsResponse>
}

export async function syncClusterKubeconfig(): Promise<ClusterSyncResponse> {
  const r = await fetch('/api/v1/cluster/sync-kubeconfig', { method: 'POST' })
  if (!r.ok) throw new Error(`sync kubeconfig: HTTP ${r.status}`)
  return r.json() as Promise<ClusterSyncResponse>
}

export async function fetchAuthCapabilities(): Promise<AuthCapabilities> {
  const token = operatorToken()
  const headers = new Headers()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const r = await fetch('/api/v1/auth/capabilities', { headers })
  if (!r.ok) throw new Error(`auth capabilities: HTTP ${r.status}`)
  return r.json() as Promise<AuthCapabilities>
}

export async function fetchAudit(): Promise<AuditResponse> {
  const r = await fetch('/api/v1/audit')
  if (!r.ok) throw new Error(`audit: HTTP ${r.status}`)
  return r.json() as Promise<AuditResponse>
}

export async function ensureBifrostNamespaces(): Promise<ActuationResponse> {
  const r = await authedFetch('ensure bifrost namespaces', '/api/v1/cluster/namespaces/ensure-bifrost', {
    method: 'POST',
  })
  return r.json() as Promise<ActuationResponse>
}

export async function rolloutRestartDeployment(
  body: RolloutRestartRequest,
): Promise<ActuationResponse> {
  const r = await authedFetch('rollout restart', '/api/v1/cluster/workloads/rollout-restart', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ActuationResponse>
}

export async function scaleDeployment(body: ScaleRequest): Promise<ActuationResponse> {
  const r = await authedFetch('scale deployment', '/api/v1/cluster/workloads/scale', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<ActuationResponse>
}

export async function deletePod(namespace: string, name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'delete pod',
    `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}`,
    { method: 'DELETE' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function fetchPodLogs(
  namespace: string,
  name: string,
  tailLines = 200,
): Promise<PodLogsResponse> {
  const params = new URLSearchParams({ tailLines: String(tailLines) })
  const r = await fetch(
    `/api/v1/cluster/workloads/pods/${encodeURIComponent(namespace)}/${encodeURIComponent(name)}/logs?${params}`,
  )
  if (!r.ok) throw await parseError('pod logs', r)
  return r.json() as Promise<PodLogsResponse>
}
