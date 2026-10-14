import { serverFetch } from '@/api/serverContact'
import type { AuditResponse } from './auditTypes'
import type { AuthCapabilities } from './matrixTypes'
import type {
  ClusterEventsResponse,
  ClusterGovernanceResponse,
  ClusterMetricsResponse,
  ClusterNamespacesResponse,
  ClusterNodesResponse,
  ClusterObservabilityResponse,
  ClusterPlacementResponse,
  ClusterPostgresBackupStatusResponse,
  ClusterPostgresStatusResponse,
  ClusterRedisStatusResponse,
  ClusterServiceReadinessResponse,
  ClusterSummary,
  ClusterWorkloadsResponse,
  DataCloneJob,
  DataCloneSchedule,
  DataFreshnessResponse,
  NodePowerResponse,
} from './clusterTypes'
import { authHeaders, authedFetch, operatorToken, parseError } from './client'

export async function fetchCluster(): Promise<ClusterSummary> {
  const r = await serverFetch('/api/v1/cluster')
  if (!r.ok) throw new Error(`cluster: HTTP ${r.status}`)
  return r.json() as Promise<ClusterSummary>
}

export async function fetchClusterNodes(): Promise<ClusterNodesResponse> {
  const r = await serverFetch('/api/v1/cluster/nodes')
  if (!r.ok) throw new Error(`cluster nodes: HTTP ${r.status}`)
  return r.json() as Promise<ClusterNodesResponse>
}

export async function fetchClusterGovernance(): Promise<ClusterGovernanceResponse> {
  const r = await serverFetch('/api/v1/cluster/governance')
  if (!r.ok) throw new Error(`cluster governance: HTTP ${r.status}`)
  return r.json() as Promise<ClusterGovernanceResponse>
}

export async function fetchClusterServiceReadiness(): Promise<ClusterServiceReadinessResponse> {
  const r = await serverFetch('/api/v1/cluster/service-readiness')
  if (!r.ok) throw new Error(`cluster service-readiness: HTTP ${r.status}`)
  return r.json() as Promise<ClusterServiceReadinessResponse>
}

export async function fetchClusterPostgresStatus(): Promise<ClusterPostgresStatusResponse> {
  const r = await serverFetch('/api/v1/cluster/postgres')
  if (!r.ok) throw new Error(`cluster postgres: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPostgresStatusResponse>
}

export async function fetchClusterPostgresBackupStatus(): Promise<ClusterPostgresBackupStatusResponse> {
  const r = await serverFetch('/api/v1/cluster/postgres/backup-status')
  if (!r.ok) throw new Error(`cluster postgres backup-status: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPostgresBackupStatusResponse>
}

export async function fetchDataFreshness(): Promise<DataFreshnessResponse> {
  const r = await serverFetch('/api/v1/cluster/data-freshness')
  if (!r.ok) throw new Error(`cluster data-freshness: HTTP ${r.status}`)
  return r.json() as Promise<DataFreshnessResponse>
}

export async function triggerDataClone(body: {
  source?: string
  targets?: string[]
  mode?: 'full' | 'selective'
  tables?: string[]
  confirmation_token: string
  confirm: boolean
}): Promise<DataCloneJob> {
  const r = await serverFetch('/api/v1/cluster/data-clone', {
    method: 'POST',
    headers: authHeaders(true),
    body: JSON.stringify(body),
  })
  if (r.status === 409) {
    const refusal = (await r
      .clone()
      .json()
      .catch(() => null)) as { error?: string; missing_tables?: string[] } | null
    if (refusal?.missing_tables != null && refusal.missing_tables.length > 0) {
      throw new DataCloneRefusedError(`data-clone: ${refusal.error ?? 'refused'}`, refusal.missing_tables)
    }
  }
  if (!r.ok) throw await parseError('data-clone', r)
  return r.json() as Promise<DataCloneJob>
}

/**
 * Selective clone refused because tables outside the selection reference the selected ones
 * (TRUNCATE would have to empty them). `missingTables` is what to add to `tables`.
 */
export class DataCloneRefusedError extends Error {
  readonly missingTables: string[]

  constructor(message: string, missingTables: string[]) {
    super(message)
    this.name = 'DataCloneRefusedError'
    this.missingTables = missingTables
  }
}

export async function fetchDataCloneStatus(id: string): Promise<DataCloneJob> {
  const r = await serverFetch(`/api/v1/cluster/data-clone/${encodeURIComponent(id)}`)
  if (!r.ok) throw new Error(`data-clone status: HTTP ${r.status}`)
  return r.json() as Promise<DataCloneJob>
}

export async function fetchDataCloneSchedule(): Promise<DataCloneSchedule> {
  const r = await serverFetch('/api/v1/cluster/data-clone/schedule')
  if (!r.ok) throw new Error(`data-clone schedule: HTTP ${r.status}`)
  return r.json() as Promise<DataCloneSchedule>
}

export async function updateDataCloneSchedule(body: {
  enabled?: boolean
  interval?: string
  source?: string
  targets?: string[]
  mode?: string
  tables?: string[]
}): Promise<DataCloneSchedule> {
  const r = await authedFetch('data-clone-schedule', '/api/v1/cluster/data-clone/schedule', {
    method: 'PUT',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<DataCloneSchedule>
}

export async function fetchClusterRedisStatus(): Promise<ClusterRedisStatusResponse> {
  const r = await serverFetch('/api/v1/cluster/redis')
  if (!r.ok) throw new Error(`cluster redis: HTTP ${r.status}`)
  return r.json() as Promise<ClusterRedisStatusResponse>
}

export async function fetchNodePower(nodeName: string): Promise<NodePowerResponse> {
  const r = await serverFetch(`/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/power`)
  if (!r.ok) throw new Error(`node power: HTTP ${r.status}`)
  return r.json() as Promise<NodePowerResponse>
}

export async function fetchClusterPlacement(): Promise<ClusterPlacementResponse> {
  const r = await serverFetch('/api/v1/cluster/placement')
  if (!r.ok) throw new Error(`cluster placement: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPlacementResponse>
}

export async function fetchClusterMetrics(limit = 8): Promise<ClusterMetricsResponse> {
  const r = await serverFetch(`/api/v1/cluster/metrics?limit=${limit}`)
  if (!r.ok) throw new Error(`cluster metrics: HTTP ${r.status}`)
  return r.json() as Promise<ClusterMetricsResponse>
}

export async function fetchClusterObservability(): Promise<ClusterObservabilityResponse> {
  const r = await serverFetch('/api/v1/cluster/observability')
  if (!r.ok) throw new Error(`cluster observability: HTTP ${r.status}`)
  return r.json() as Promise<ClusterObservabilityResponse>
}

export async function fetchClusterNamespaces(
  watch?: 'bifrost' | '',
): Promise<ClusterNamespacesResponse> {
  const qs = watch === 'bifrost' ? '?watch=bifrost' : ''
  const r = await serverFetch(`/api/v1/cluster/namespaces${qs}`)
  if (!r.ok) throw new Error(`cluster namespaces: HTTP ${r.status}`)
  return r.json() as Promise<ClusterNamespacesResponse>
}

export async function fetchClusterWorkloads(ns: string): Promise<ClusterWorkloadsResponse> {
  const r = await serverFetch(`/api/v1/cluster/workloads?ns=${encodeURIComponent(ns)}`)
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
  const r = await serverFetch(`/api/v1/cluster/events?${params}`)
  if (!r.ok) throw new Error(`cluster events: HTTP ${r.status}`)
  return r.json() as Promise<ClusterEventsResponse>
}

export async function fetchAuthCapabilities(token = operatorToken(), signal?: AbortSignal): Promise<AuthCapabilities> {
  const headers = new Headers()
  if (token !== '') headers.set('Authorization', `Bearer ${token}`)
  const controller = new AbortController()
  const abort = () => controller.abort()
  signal?.addEventListener('abort', abort, { once: true })
  if (signal?.aborted) abort()
  let timer: ReturnType<typeof setTimeout> | undefined
  let rejectAbort!: (error: Error) => void
  const cancelled = new Promise<never>((_, reject) => { rejectAbort = reject })
  const onAbort = () => rejectAbort(new Error('auth capabilities read cancelled'))
  controller.signal.addEventListener('abort', onAbort, { once: true })
  if (controller.signal.aborted) onAbort()
  const deadline = new Promise<never>((_, reject) => {
    timer = setTimeout(() => {
      reject(new Error('auth capabilities read timed out'))
      controller.abort()
    }, 15_000)
  })
  try {
    // The deadline covers the body as well as the headers, even if the transport
    // does not settle its promise after cancellation.
    return await Promise.race([
      (async () => {
        const r = await serverFetch('/api/v1/auth/capabilities', { headers, signal: controller.signal })
        if (!r.ok) throw new Error(`auth capabilities: HTTP ${r.status}`)
        return await r.json() as AuthCapabilities
      })(),
      deadline,
      cancelled,
    ])
  } finally {
    clearTimeout(timer)
    signal?.removeEventListener('abort', abort)
    controller.signal.removeEventListener('abort', onAbort)
  }
}

export async function fetchAudit(): Promise<AuditResponse> {
  const r = await serverFetch('/api/v1/audit')
  if (!r.ok) throw new Error(`audit: HTTP ${r.status}`)
  return r.json() as Promise<AuditResponse>
}

