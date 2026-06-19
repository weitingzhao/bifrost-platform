import type {
  ActuationResponse,
  AllMatricesResponse,
  AuditResponse,
  AuthCapabilities,
  ClusterEventsResponse,
  ClusterMetricsResponse,
  ClusterObservabilityResponse,
  GitOpsAppsResponse,
  StackAddonsResponse,
  ClusterNamespacesResponse,
  ClusterNodesResponse,
  NodePowerResponse,
  ClusterPlacementResponse,
  ClusterSummary,
  JoinProfilesResponse,
  DrainNodeRequest,
  ClusterSyncResponse,
  ClusterWorkloadsResponse,
  DeliveryPipelineRunsResponse,
  DeliveryPipelinesResponse,
  DeliveryPipelinePreflightResponse,
  DeliveryRunLogsResponse,
  DeliveryStartRunResponse,
  PipelineRunStepsResponse,
  SupplyChainActuationResponse,
  SupplyChainResponse,
  StgSmokeResponse,
  ReleaseGateResponse,
  RunReleaseGateResponse,
  TierBSignoffResponse,
  TierBStatusResponse,
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

export async function fetchNodePower(nodeName: string): Promise<NodePowerResponse> {
  const r = await fetch(`/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/power`)
  if (!r.ok) throw new Error(`node power: HTTP ${r.status}`)
  return r.json() as Promise<NodePowerResponse>
}

export async function wakeComputeNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'wake compute node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/wake`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function powerOffComputeNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'power off compute node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/poweroff`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function fetchJoinProfiles(): Promise<JoinProfilesResponse> {
  const r = await fetch('/api/v1/cluster/join-profiles')
  if (!r.ok) throw new Error(`join profiles: HTTP ${r.status}`)
  return r.json() as Promise<JoinProfilesResponse>
}

export async function cordonNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'cordon node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/cordon`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function uncordonNode(nodeName: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'uncordon node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/uncordon`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function drainNode(
  nodeName: string,
  body: DrainNodeRequest = { delete_local_data: true, force: true, grace_period_seconds: 60 },
): Promise<ActuationResponse> {
  const r = await authedFetch(
    'drain node',
    `/api/v1/cluster/nodes/${encodeURIComponent(nodeName)}/drain`,
    { method: 'POST', body: JSON.stringify(body) },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function joinClusterNode(profile: string): Promise<ActuationResponse> {
  const r = await authedFetch('join cluster node', '/api/v1/cluster/nodes/join', {
    method: 'POST',
    body: JSON.stringify({ profile }),
  })
  return r.json() as Promise<ActuationResponse>
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

export async function fetchGitOpsApps(): Promise<GitOpsAppsResponse> {
  const r = await fetch('/api/v1/gitops/apps')
  if (!r.ok) throw new Error(`gitops apps: HTTP ${r.status}`)
  return r.json() as Promise<GitOpsAppsResponse>
}

export async function fetchStackAddons(): Promise<StackAddonsResponse> {
  const r = await fetch('/api/v1/stack/addons')
  if (!r.ok) throw new Error(`stack addons: HTTP ${r.status}`)
  return r.json() as Promise<StackAddonsResponse>
}

export async function fetchDeliveryPipelines(): Promise<DeliveryPipelinesResponse> {
  const r = await fetch('/api/v1/delivery/pipelines')
  if (!r.ok) throw new Error(`delivery pipelines: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelinesResponse>
}

export async function fetchPipelinePreflight(
  pipelineName: string,
): Promise<DeliveryPipelinePreflightResponse> {
  const r = await fetch(`/api/v1/delivery/pipelines/${encodeURIComponent(pipelineName)}/preflight`)
  if (!r.ok) throw new Error(`delivery preflight: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelinePreflightResponse>
}

export async function fetchStgSmoke(): Promise<StgSmokeResponse> {
  const r = await fetch('/api/v1/delivery/stg/smoke')
  if (!r.ok) throw new Error(`stg smoke: HTTP ${r.status}`)
  return r.json() as Promise<StgSmokeResponse>
}

export type ReleaseGateTier = 'stg' | 'prod'

export async function fetchReleaseGate(tier: ReleaseGateTier = 'prod'): Promise<ReleaseGateResponse> {
  const r = await fetch(`/api/v1/promote/release-gate?tier=${tier}`)
  if (!r.ok) throw new Error(`release gate: HTTP ${r.status}`)
  return r.json() as Promise<ReleaseGateResponse>
}

export async function runReleaseGate(tier: ReleaseGateTier = 'prod'): Promise<RunReleaseGateResponse> {
  const r = await authedFetch('release gate', `/api/v1/promote/release-gate?tier=${tier}`, {
    method: 'POST',
  })
  return r.json() as Promise<RunReleaseGateResponse>
}

export async function fetchTierBStatus(): Promise<TierBStatusResponse> {
  const r = await fetch('/api/v1/promote/tier-b')
  if (!r.ok) throw new Error(`tier b: HTTP ${r.status}`)
  return r.json() as Promise<TierBStatusResponse>
}

export async function signTierB(notes = ''): Promise<TierBSignoffResponse> {
  const r = await authedFetch('tier b signoff', '/api/v1/promote/tier-b/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<TierBSignoffResponse>
}

export async function fetchPipelineRuns(name: string): Promise<DeliveryPipelineRunsResponse> {
  const r = await fetch(`/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`)
  if (!r.ok) throw new Error(`pipeline runs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryPipelineRunsResponse>
}

export async function fetchSupplyChain(): Promise<SupplyChainResponse> {
  const r = await fetch('/api/v1/delivery/supply-chain')
  if (!r.ok) throw new Error(`supply chain: HTTP ${r.status}`)
  return r.json() as Promise<SupplyChainResponse>
}

export async function triggerMirrorSync(): Promise<SupplyChainActuationResponse> {
  const r = await authedFetch('mirror sync', '/api/v1/delivery/supply-chain/mirror-sync', {
    method: 'POST',
  })
  return r.json() as Promise<SupplyChainActuationResponse>
}

export async function refreshDockerfileConfigMaps(
  revision = 'main',
): Promise<SupplyChainActuationResponse> {
  const r = await authedFetch(
    'refresh dockerfile configmaps',
    '/api/v1/delivery/supply-chain/dockerfile-configmaps/refresh',
    { method: 'POST', body: JSON.stringify({ revision }) },
  )
  return r.json() as Promise<SupplyChainActuationResponse>
}

export async function startPipelineRun(
  name: string,
  revision?: string,
): Promise<DeliveryStartRunResponse> {
  const body =
    revision != null && revision.trim() !== '' ? JSON.stringify({ revision: revision.trim() }) : undefined
  const r = await authedFetch(
    'pipeline run',
    `/api/v1/delivery/pipelines/${encodeURIComponent(name)}/runs`,
    { method: 'POST', body },
  )
  return r.json() as Promise<DeliveryStartRunResponse>
}

export async function fetchPipelineRunLogs(
  runId: string,
  namespace?: string,
): Promise<DeliveryRunLogsResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await fetch(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/logs${qs}`)
  if (!r.ok) throw new Error(`pipeline logs: HTTP ${r.status}`)
  return r.json() as Promise<DeliveryRunLogsResponse>
}

export async function fetchPipelineRunSteps(
  runId: string,
  namespace?: string,
): Promise<PipelineRunStepsResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await fetch(`/api/v1/delivery/runs/${encodeURIComponent(runId)}/steps${qs}`)
  if (!r.ok) throw new Error(`pipeline steps: HTTP ${r.status}`)
  return r.json() as Promise<PipelineRunStepsResponse>
}

export async function deletePipelineRun(
  runId: string,
  namespace?: string,
): Promise<ActuationResponse> {
  const qs = namespace != null && namespace !== '' ? `?ns=${encodeURIComponent(namespace)}` : ''
  const r = await authedFetch(
    'delete pipeline run',
    `/api/v1/delivery/runs/${encodeURIComponent(runId)}${qs}`,
    { method: 'DELETE' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function syncGitOpsApp(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'gitops sync',
    `/api/v1/gitops/apps/${encodeURIComponent(name)}/sync`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
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

export async function ensureMetricsServer(): Promise<ActuationResponse> {
  const r = await authedFetch('ensure metrics-server', '/api/v1/cluster/addons/metrics-server/ensure', {
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
