export type Reachability = 'ok' | 'degraded' | 'fail' | 'unknown'
export type AuthStatus = 'ok' | 'missing' | 'invalid' | 'skipped' | 'blocked'

export interface EnvironmentSummary {
  id: string
  label: string
}

export interface Target {
  id: string
  category: string
  reachability: Reachability
  auth: AuthStatus
  authorization_level: string
  detail: string
  url?: string
}

export interface MatrixResponse {
  environment: string
  label: string
  generated_at: string
  principal: { name: string; level: string }
  targets: Target[]
}

export interface AllMatricesResponse {
  matrices: MatrixResponse[]
}

export interface TopologyMatrixService {
  id: string
  reachability: Reachability
  detail: string
  category: string
}

export interface TopologyNode {
  id: string
  label: string
  host?: string
  group: string
  compose_roles: string[]
  k3s_roles: string[]
  in_k3s_cluster: boolean
  grid: { row: number; col: number }
  status: Reachability
  detail: string
  matrix_services: TopologyMatrixService[]
}

export interface TopologyEdge {
  id: string
  from: string
  to: string
  label: string
  kind: string
  matrix_target?: string
  status: Reachability
  detail: string
}

export interface TopologyResponse {
  environment: string
  label: string
  deployment_phase: string
  generated_at: string
  nodes: TopologyNode[]
  edges: TopologyEdge[]
}

export interface OpsContextMeta {
  version: string
  catalog_version: string
}

export interface OpsContextDeployment {
  phase: string
  active_track: string
}

export interface OpsContextFocus {
  headline: string
  flywheel_primary: string
  blocker?: string
}

export interface OpsContextMilestone {
  id: string
  label?: string
  status: string
  blocker?: string
  signed_at?: string
  authority?: string
  pipeline_lane?: 'main' | 'parallel'
  pipeline_after?: string
}

export interface OpsContextDecision {
  id: string
  status: string
  topic?: string
  conclusion: string
  signed_at?: string
  authority?: string
}

export interface OpsContextPlatformPhase {
  id: string
  label: string
  timeframe: string
  deliverables: string
}

export interface OpsContextLastGate {
  at: string | null
  result: string | null
  log_path: string
}

export interface OpsContextPromotion {
  last_gate: OpsContextLastGate
}

export interface OpsContextEnvironmentExtended {
  status: string
  note?: string
}

export interface OpsContextProbeHint {
  target_id: string
  trade_route: string
  hint: string
}

export interface OpsContextNorthStar {
  id: string
  statement: string
  strategy: string
  principles: string[]
  owner_exception: string
  authority: string
  success_criteria: string[]
}

export interface OpsContextResponse {
  meta: OpsContextMeta
  north_star?: OpsContextNorthStar
  deployment: OpsContextDeployment
  focus: OpsContextFocus
  milestones: OpsContextMilestone[]
  decisions: OpsContextDecision[]
  platform_phases: OpsContextPlatformPhase[]
  coupling_surfaces: string[]
  promotion: OpsContextPromotion
  environments_extended: Record<string, OpsContextEnvironmentExtended>
  probe_hints: OpsContextProbeHint[]
}

export interface ClusterSummary {
  cluster_id: string
  label: string
  distribution: string
  api_server: string
  kubeconfig_path: string
  reachability: Reachability
  detail: string
  server_version?: string
  nodes_ready: number
  nodes_total: number
  failing_pods: number
  running_pods: number
  pending_pods: number
  cpu_allocatable?: string
  memory_allocatable?: string
  generated_at: string
}

export interface ClusterNode {
  name: string
  status: string
  roles: string
  version: string
  internal_ip: string
  reachability: Reachability
  cpu_allocatable?: string
  memory_allocatable?: string
  storage_allocatable?: string
  cpu_usage_percent?: number
  memory_usage_percent?: number
  cpu_reachability?: Reachability
  memory_reachability?: Reachability
}

export interface ClusterNodesResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  nodes: ClusterNode[]
  generated_at: string
}

export interface ClusterNamespace {
  name: string
  status: string
  pod_count: number
  running_pods: number
  failing_pods: number
}

export interface ClusterNamespacesResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  filter: string
  namespaces: ClusterNamespace[]
  generated_at: string
}

export interface ClusterWorkload {
  namespace: string
  kind: string
  name: string
  ready: string
  status: string
  restarts: number
  age: string
  reachability: Reachability
}

export interface ClusterWorkloadsResponse {
  cluster_id: string
  namespace: string
  reachability: Reachability
  detail: string
  workloads: ClusterWorkload[]
  generated_at: string
}

export interface ClusterEvent {
  namespace: string
  type: string
  reason: string
  object: string
  message: string
  count: number
  first_seen: string
  last_seen: string
}

export interface ClusterEventsResponse {
  cluster_id: string
  namespace: string
  reachability: Reachability
  detail: string
  events: ClusterEvent[]
  generated_at: string
}

export interface ClusterSyncResponse {
  ok: boolean
  path: string
  message: string
}

export interface ClusterPodMetric {
  namespace: string
  name: string
  cpu: string
  memory: string
}

export interface ClusterMetricsResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  metrics_server_available: boolean
  metrics_server_detail?: string
  cpu_usage_percent?: number
  memory_usage_percent?: number
  cpu_reachability?: Reachability
  memory_reachability?: Reachability
  top_pods: ClusterPodMetric[]
  generated_at: string
}

export type LayerBStatus = 'not_installed' | 'partial' | 'ready'

export interface ObservabilityComponent {
  id: string
  label: string
  kind: string
  name: string
  ready: string
  status: string
  reachability: Reachability
  detail: string
}

export interface ClusterObservabilityResponse {
  cluster_id: string
  namespace: string
  layer_b_status: LayerBStatus
  reachability: Reachability
  detail: string
  components: ObservabilityComponent[]
  grafana_url?: string
  prometheus_url?: string
  docs_url?: string
  generated_at: string
}

export interface AuthCapabilities {
  authenticated: boolean
  principal?: string
  role: 'viewer' | 'operator' | 'admin'
  can_operate: boolean
  can_admin: boolean
}

export interface ActuationResponse {
  ok: boolean
  action: string
  target: string
  changed: boolean
  message: string
  namespaces?: string[]
  generated_at: string
}

export interface RolloutRestartRequest {
  namespace: string
  kind: 'Deployment'
  name: string
}

export interface ScaleRequest {
  namespace: string
  kind: 'Deployment'
  name: string
  replicas: number
}

export interface PodLogsResponse {
  namespace: string
  pod: string
  container?: string
  tail_lines: number
  logs: string
}

export interface AuditRecord {
  id: string
  at: string
  actor: string
  role: 'viewer' | 'operator' | 'admin'
  action: string
  target: string
  status: string
  detail: string
}

export interface AuditResponse {
  records: AuditRecord[]
}
