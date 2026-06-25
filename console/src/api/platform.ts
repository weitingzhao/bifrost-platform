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
  McpToolsResponse,
  McpStatusResponse,
  ClusterGovernanceResponse,
  ClusterPostgresStatusResponse,
  ClusterRedisStatusResponse,
  ClusterServiceReadinessResponse,
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
  RunVisionV1GateResponse,
  VisionV1GateResponse,
  GateHistoryResponse,
  TierBSignoffResponse,
  TierBStatusResponse,
  EnvironmentSummary,
  MatrixResponse,
  OpsContextResponse,
  PodLogsResponse,
  RolloutRestartRequest,
  ScaleRequest,
  TopologyResponse,
  StartRemediationRequest,
  RemediationJob,
  RemediationJobsResponse,
  AgentNightlyReportResponse,
  NightlyTriggerResponse,
  AgentDeployStatusResponse,
  AgentDeployStartResponse,
  RemediationHealthResponse,
  AgentBridgeResponse,
  DriftProposal,
  DriftProposalsResponse,
  ApproveDriftProposalResponse,
  BuildPhaseGateResponse,
  RunBuildPhaseGateResponse,
  SelfHealthResponse,
} from './types'
import { getPlatformOperatorToken } from '@/lib/platformAuth'

function operatorToken(): string {
  return getPlatformOperatorToken()
}

async function parseError(prefix: string, r: Response): Promise<Error> {
  let detail = `HTTP ${r.status}`
  try {
    const body = (await r.json()) as {
      error?: string
      message?: string
      detail?: string
      hint?: string
    }
    const head = body.error ?? body.message ?? detail
    const parts = [head]
    if (body.detail != null && body.detail.trim() !== '' && body.detail !== head) {
      parts.push(body.detail.trim())
    }
    if (body.hint != null && body.hint.trim() !== '') {
      parts.push(body.hint.trim())
    }
    detail = parts.join(' — ')
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
  body: DrainNodeRequest = { force: true, grace_period_seconds: 60 },
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

export async function fetchSelfHealth(): Promise<SelfHealthResponse> {
  const r = await fetch('/api/v1/self-health')
  if (!r.ok) throw new Error(`self-health: HTTP ${r.status}`)
  return r.json() as Promise<SelfHealthResponse>
}

export async function fetchStackAddons(): Promise<StackAddonsResponse> {
  const r = await fetch('/api/v1/stack/addons')
  if (!r.ok) throw new Error(`stack addons: HTTP ${r.status}`)
  return r.json() as Promise<StackAddonsResponse>
}

export async function installStackAddon(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'stack install',
    `/api/v1/stack/addons/${encodeURIComponent(name)}/install`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function upgradeStackAddon(name: string): Promise<ActuationResponse> {
  const r = await authedFetch(
    'stack upgrade',
    `/api/v1/stack/addons/${encodeURIComponent(name)}/upgrade`,
    { method: 'POST' },
  )
  return r.json() as Promise<ActuationResponse>
}

export async function fetchMcpTools(): Promise<McpToolsResponse> {
  const r = await fetch('/api/v1/mcp/tools')
  if (!r.ok) throw new Error(`mcp tools: HTTP ${r.status}`)
  return r.json() as Promise<McpToolsResponse>
}

export async function fetchMcpStatus(): Promise<McpStatusResponse> {
  const r = await fetch('/api/v1/mcp/status')
  if (!r.ok) throw new Error(`mcp status: HTTP ${r.status}`)
  return r.json() as Promise<McpStatusResponse>
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

export type ReleaseGateTier = 'stg' | 'prod' | 'platform-stg' | 'platform-prod'

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

export async function fetchGateHistory(tier: ReleaseGateTier = 'prod'): Promise<GateHistoryResponse> {
  const r = await fetch(`/api/v1/promote/gate-history?tier=${tier}`)
  if (!r.ok) throw new Error(`gate history: HTTP ${r.status}`)
  return r.json() as Promise<GateHistoryResponse>
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

export async function fetchVisionV1Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v1/gate')
  if (!r.ok) throw new Error(`vision v1 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV1Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v1 gate', '/api/v1/vision/v1/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV1(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v1 signoff', '/api/v1/vision/v1/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionS3Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/s3/gate')
  if (!r.ok) throw new Error(`vision s3 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionS3Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision s3 gate', '/api/v1/vision/s3/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionS3(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision s3 signoff', '/api/v1/vision/s3/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV2Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v2/gate')
  if (!r.ok) throw new Error(`vision v2 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV2Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v2 gate', '/api/v1/vision/v2/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV2(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v2 signoff', '/api/v1/vision/v2/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV3Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v3/gate')
  if (!r.ok) throw new Error(`vision v3 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV3Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v3 gate', '/api/v1/vision/v3/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV3(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v3 signoff', '/api/v1/vision/v3/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV4Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v4/gate')
  if (!r.ok) throw new Error(`vision v4 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV4Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v4 gate', '/api/v1/vision/v4/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV4(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v4 signoff', '/api/v1/vision/v4/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function fetchVisionV5Gate(): Promise<VisionV1GateResponse> {
  const r = await fetch('/api/v1/vision/v5/gate')
  if (!r.ok) throw new Error(`vision v5 gate: HTTP ${r.status}`)
  return r.json() as Promise<VisionV1GateResponse>
}

export async function runVisionV5Gate(): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v5 gate', '/api/v1/vision/v5/gate', { method: 'POST' })
  return r.json() as Promise<RunVisionV1GateResponse>
}

export async function signVisionV5(notes = ''): Promise<RunVisionV1GateResponse> {
  const r = await authedFetch('vision v5 signoff', '/api/v1/vision/v5/signoff', {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunVisionV1GateResponse>
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

export async function rollbackGitOpsApp(
  name: string,
  revision?: string,
): Promise<ActuationResponse> {
  const body =
    revision != null && revision !== '' ? JSON.stringify({ revision }) : JSON.stringify({})
  const r = await authedFetch(
    'gitops rollback',
    `/api/v1/gitops/apps/${encodeURIComponent(name)}/rollback`,
    { method: 'POST', body },
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

export async function startRemediation(body: StartRemediationRequest): Promise<RemediationJob> {
  const r = await authedFetch('remediation start', '/api/v1/remediation/start', {
    method: 'POST',
    body: JSON.stringify(body),
  })
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJob(id: string): Promise<RemediationJob> {
  const r = await authedFetch('remediation job', `/api/v1/remediation/${encodeURIComponent(id)}`)
  return r.json() as Promise<RemediationJob>
}

export async function fetchRemediationJobs(): Promise<RemediationJobsResponse> {
  const r = await authedFetch('remediation jobs', '/api/v1/remediation/')
  return r.json() as Promise<RemediationJobsResponse>
}

export async function cancelRemediationJob(id: string): Promise<RemediationJob> {
  const r = await authedFetch('remediation cancel', `/api/v1/remediation/${encodeURIComponent(id)}/cancel`, {
    method: 'POST',
  })
  return r.json() as Promise<RemediationJob>
}

export async function respondRemediationJob(
  id: string,
  optionId: string,
  note?: string,
): Promise<void> {
  const r = await authedFetch('remediation respond', `/api/v1/remediation/${encodeURIComponent(id)}/respond`, {
    method: 'POST',
    body: JSON.stringify({ option_id: optionId, note: note ?? '' }),
  })
  if (!r.ok) throw await parseError('remediation respond', r)
}

export function remediationStreamUrl(id: string): string {
  return `/api/v1/remediation/${encodeURIComponent(id)}/stream`
}

export async function fetchAgentNightlyReport(): Promise<AgentNightlyReportResponse> {
  const r = await fetch('/api/v1/agent/nightly-report')
  if (!r.ok) throw await parseError('agent nightly-report', r)
  return r.json() as Promise<AgentNightlyReportResponse>
}

export async function triggerNightlyDriftScan(): Promise<NightlyTriggerResponse> {
  const r = await authedFetch('agent nightly-run', '/api/v1/agent/nightly-run', { method: 'POST' })
  return r.json() as Promise<NightlyTriggerResponse>
}

export async function fetchAgentDeployStatus(): Promise<AgentDeployStatusResponse> {
  const r = await fetch('/api/v1/agent/deploy')
  if (!r.ok) throw await parseError('agent deploy status', r)
  return r.json() as Promise<AgentDeployStatusResponse>
}

export async function startAgentDeploy(remote?: string): Promise<AgentDeployStartResponse> {
  const body =
    remote != null && remote.trim() !== '' ? JSON.stringify({ remote: remote.trim() }) : '{}'
  const r = await authedFetch('agent deploy', '/api/v1/agent/deploy', {
    method: 'POST',
    body,
  })
  return r.json() as Promise<AgentDeployStartResponse>
}

export async function fetchRemediationHealth(): Promise<RemediationHealthResponse> {
  const r = await fetch('/api/v1/remediation/health')
  if (!r.ok) {
    const body = (await r.json().catch(() => ({}))) as RemediationHealthResponse
    return { status: 'unavailable', error: body.error ?? r.statusText }
  }
  return r.json() as Promise<RemediationHealthResponse>
}

export async function fetchAgentBridge(): Promise<AgentBridgeResponse> {
  const r = await fetch('/api/v1/agent/bridge')
  if (!r.ok) throw await parseError('agent bridge', r)
  return r.json() as Promise<AgentBridgeResponse>
}

export async function fetchDriftProposals(): Promise<DriftProposalsResponse> {
  const r = await fetch('/api/v1/agent/drift-proposals')
  if (!r.ok) throw await parseError('drift proposals', r)
  return r.json() as Promise<DriftProposalsResponse>
}

export async function approveDriftProposal(id: string): Promise<ApproveDriftProposalResponse> {
  const r = await authedFetch(
    'drift proposal approve',
    `/api/v1/agent/drift-proposals/${encodeURIComponent(id)}/approve`,
    { method: 'POST' },
  )
  return r.json() as Promise<ApproveDriftProposalResponse>
}

export async function rejectDriftProposal(id: string, note?: string): Promise<DriftProposal> {
  const r = await authedFetch(
    'drift proposal reject',
    `/api/v1/agent/drift-proposals/${encodeURIComponent(id)}/reject`,
    {
      method: 'POST',
      body: JSON.stringify({ note: note ?? '' }),
    },
  )
  return r.json() as Promise<DriftProposal>
}

export async function fetchBuildPhases(): Promise<BuildPhaseGateResponse[]> {
  const r = await fetch('/api/v1/build-phase')
  if (!r.ok) throw new Error(`build phases: HTTP ${r.status}`)
  return r.json() as Promise<BuildPhaseGateResponse[]>
}

export async function fetchBuildPhaseGate(phase: string): Promise<BuildPhaseGateResponse> {
  const r = await fetch(`/api/v1/build-phase/${phase}/gate`)
  if (!r.ok) throw new Error(`build phase gate: HTTP ${r.status}`)
  return r.json() as Promise<BuildPhaseGateResponse>
}

export async function runBuildPhaseGate(phase: string): Promise<RunBuildPhaseGateResponse> {
  const r = await authedFetch('build phase gate', `/api/v1/build-phase/${phase}/gate`, { method: 'POST' })
  return r.json() as Promise<RunBuildPhaseGateResponse>
}

export async function signBuildPhase(phase: string, notes = ''): Promise<RunBuildPhaseGateResponse> {
  const r = await authedFetch('build phase signoff', `/api/v1/build-phase/${phase}/signoff`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ notes }),
  })
  return r.json() as Promise<RunBuildPhaseGateResponse>
}
