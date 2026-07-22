import type { AuditResponse } from './auditTypes'
import type { AuthCapabilities } from './matrixTypes'
import type { ClusterEventsResponse, ClusterGovernanceResponse, ClusterMetricsResponse, ClusterNamespacesResponse, ClusterNodesResponse, ClusterObservabilityResponse, ClusterPlacementResponse, ClusterPostgresStatusResponse, ClusterRedisStatusResponse, ClusterServiceReadinessResponse, ClusterSummary, ClusterWorkloadsResponse, JoinProfilesResponse, NodePowerResponse } from './clusterTypes'
import { operatorToken } from './client'

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

export async function fetchClusterGovernance(): Promise<ClusterGovernanceResponse> {
  const r = await fetch('/api/v1/cluster/governance')
  if (!r.ok) throw new Error(`cluster governance: HTTP ${r.status}`)
  return r.json() as Promise<ClusterGovernanceResponse>
}

export async function fetchClusterServiceReadiness(): Promise<ClusterServiceReadinessResponse> {
  const r = await fetch('/api/v1/cluster/service-readiness')
  if (!r.ok) throw new Error(`cluster service-readiness: HTTP ${r.status}`)
  return r.json() as Promise<ClusterServiceReadinessResponse>
}

export async function fetchClusterPostgresStatus(): Promise<ClusterPostgresStatusResponse> {
  const r = await fetch('/api/v1/cluster/postgres')
  if (!r.ok) throw new Error(`cluster postgres: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPostgresStatusResponse>
}

export async function fetchClusterRedisStatus(): Promise<ClusterRedisStatusResponse> {
  const r = await fetch('/api/v1/cluster/redis')
  if (!r.ok) throw new Error(`cluster redis: HTTP ${r.status}`)
  return r.json() as Promise<ClusterRedisStatusResponse>
}

export async function fetchNodePower(nodeName: string): Promise<NodePowerResponse> {
  const r = await fetch(`/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/power`)
  if (!r.ok) throw new Error(`node power: HTTP ${r.status}`)
  return r.json() as Promise<NodePowerResponse>
}

export async function fetchJoinProfiles(): Promise<JoinProfilesResponse> {
  const r = await fetch('/api/v1/cluster/join-profiles')
  if (!r.ok) throw new Error(`join profiles: HTTP ${r.status}`)
  return r.json() as Promise<JoinProfilesResponse>
}

export async function fetchClusterPlacement(): Promise<ClusterPlacementResponse> {
  const r = await fetch('/api/v1/cluster/placement')
  if (!r.ok) throw new Error(`cluster placement: HTTP ${r.status}`)
  return r.json() as Promise<ClusterPlacementResponse>
}

export async function fetchClusterMetrics(limit = 8): Promise<ClusterMetricsResponse> {
  const r = await fetch(`/api/v1/cluster/metrics?limit=${limit}`)
  if (!r.ok) throw new Error(`cluster metrics: HTTP ${r.status}`)
  return r.json() as Promise<ClusterMetricsResponse>
}

export async function fetchClusterObservability(): Promise<ClusterObservabilityResponse> {
  const r = await fetch('/api/v1/cluster/observability')
  if (!r.ok) throw new Error(`cluster observability: HTTP ${r.status}`)
  return r.json() as Promise<ClusterObservabilityResponse>
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

