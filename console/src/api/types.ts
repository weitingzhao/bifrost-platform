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

export interface TrackTask {
  id: string
  label: string
  status: 'done' | 'in_progress' | 'next' | 'pending' | 'blocked'
}

export interface BuildTrack {
  label: string
  current_phase: string
  tasks: TrackTask[]
}

export interface MigrateStream {
  id: string
  label: string
  total: number
  done: number
  status: string
  next_task?: string | null
  note?: string
}

export interface MigrateTrack {
  label: string
  streams: MigrateStream[]
}

export interface OperateTrack {
  label: string
  note?: string
}

export interface OpsContextTracks {
  build?: BuildTrack
  migrate?: MigrateTrack
  operate?: OperateTrack
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
  tracks?: OpsContextTracks
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
  architecture?: string
  os_image?: string
  workload_label?: string
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
  compute_managed?: boolean
  unschedulable?: boolean
}

export interface ClusterNodesResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  nodes: ClusterNode[]
  generated_at: string
}

export interface ComputeWorkloadStatus {
  namespace: string
  name: string
  label: string
  replicas: number
  ready_replicas: number
}

export interface NodePowerResponse {
  cluster_id: string
  node_name: string
  compute_managed: boolean
  node_status: string
  power_state: 'online' | 'offline' | string
  wol_mac?: string
  power_policy?: string
  power_manager_active?: string
  pending_compute_pods: number
  user_pods_on_node: number
  workloads: ComputeWorkloadStatus[]
  reachability: Reachability
  detail: string
  generated_at: string
}

export interface JoinProfile {
  id: string
  label: string
  expected_node?: string
  script: string
}

export interface JoinProfilesResponse {
  cluster_id: string
  profiles: JoinProfile[]
  enabled: boolean
  detail?: string
  generated_at: string
}

export interface DrainNodeRequest {
  force?: boolean
  delete_local_data?: boolean
  grace_period_seconds?: number
}

export interface JoinNodeRequest {
  profile: string
}

export interface ClusterPlacementPool {
  id: string
  label: string
  arch?: string
  workload_label?: string
  status: 'live' | 'planned' | 'degraded'
  nodes_total: number
  nodes_ready: number
  planned_host?: string
  node_names: string[]
}

export interface ClusterPlacementRule {
  workload_class: string
  namespace: string
  services?: string
  required_selector: string
  pool_id: string
  satisfied: boolean
  reachability: Reachability
  gap_reason?: string
  planned_binding?: string
}

export interface ClusterPlacementViolation {
  severity: 'critical' | 'warning'
  code: string
  message: string
}

export interface ClusterPlacementResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  pools: ClusterPlacementPool[]
  rules: ClusterPlacementRule[]
  violations: ClusterPlacementViolation[]
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

export type ArgoCDStatus = 'not_installed' | 'installed' | 'degraded' | 'unavailable'

export interface GitOpsArgoCDServerView {
  kind: string
  name: string
  ready: string
  status: string
  reachability: Reachability
  detail?: string
}

export interface GitOpsApplicationCondition {
  type: string
  message: string
  last_transition_time?: string
}

export interface GitOpsApplicationView {
  name: string
  namespace: string
  project?: string
  sync_status: string
  health_status: string
  destination?: string
  destination_namespace?: string
  revision?: string
  source_repo?: string
  source_path?: string
  source_target_revision?: string
  automated_sync?: boolean
  self_heal?: boolean
  prune?: boolean
  history_count?: number
  conditions?: GitOpsApplicationCondition[]
  primary_condition?: string
  operation_phase?: string
  operation_message?: string
}

export interface GitOpsAppsResponse {
  cluster_id: string
  argocd_namespace: string
  applications_namespace: string
  argocd_status: ArgoCDStatus
  reachability: Reachability
  detail: string
  server?: GitOpsArgoCDServerView
  apps: GitOpsApplicationView[]
  generated_at: string
}

export type StackAddonStatus = 'not_installed' | 'installed' | 'degraded'

export interface StackAddonView {
  id: string
  label: string
  status: StackAddonStatus
  reachability: Reachability
  kind?: string
  name?: string
  ready?: string
  detail?: string
}

export interface StackAddonsResponse {
  cluster_id: string
  namespace: string
  reachability: Reachability
  detail: string
  addons: StackAddonView[]
  generated_at: string
}

export interface DeliveryPipelineView {
  name: string
  namespace: string
  detail?: string
  build_ready?: boolean
  block_reason?: string
}

export interface DeliveryPipelinePreflightResponse {
  cluster_id: string
  pipeline: string
  build_ready: boolean
  reason?: string
  reachability: Reachability
  generated_at: string
}

export interface DeliveryPipelinesResponse {
  cluster_id: string
  namespace: string
  reachability: Reachability
  detail: string
  pipelines: DeliveryPipelineView[]
  generated_at: string
}

export interface DeliveryPipelineRunView {
  name: string
  namespace: string
  pipeline: string
  status: string
  reason?: string
  start_time?: string
  completion_time?: string
}

export interface DeliveryPipelineRunsResponse {
  cluster_id: string
  namespace: string
  pipeline: string
  reachability: Reachability
  detail: string
  runs: DeliveryPipelineRunView[]
  generated_at: string
}

export interface DeliveryRunLogsResponse {
  cluster_id: string
  namespace: string
  run_name: string
  logs: string
  generated_at: string
}

export interface DeliveryStartRunResponse extends ActuationResponse {
  run?: DeliveryPipelineRunView
}

export interface DockerfileConfigMapView {
  name: string
  namespace: string
  present: boolean
  resource_version?: string
  updated_at?: string
  file_keys?: string[]
  approx_bytes?: number
  detail?: string
}

export interface StgWorkloadImageView {
  deployment: string
  namespace: string
  image: string
}

export interface SupplyChainTaskRunView {
  name: string
  namespace: string
  task: string
  actuation?: string
  status: string
  reason?: string
  start_time?: string
  completion_time?: string
}

export interface SupplyChainResponse {
  cluster_id: string
  cicd_namespace: string
  stg_namespace: string
  reachability: Reachability
  detail: string
  mirror_credentials_configured: boolean
  default_revision: string
  tracked_repos: string[]
  dockerfile_configmaps: DockerfileConfigMapView[]
  stg_workloads: StgWorkloadImageView[]
  last_deliver_run?: DeliveryPipelineRunView
  last_deliver_success?: DeliveryPipelineRunView
  last_supply_chain_task?: SupplyChainTaskRunView
  generated_at: string
}

export interface SupplyChainActuationResponse extends ActuationResponse {
  run?: SupplyChainTaskRunView
}

export interface PipelineRunStepsResponse {
  cluster_id: string
  namespace: string
  run_name: string
  pipeline: string
  reachability: Reachability
  detail: string
  phases: PipelinePhaseView[]
  tasks?: PipelineTaskRunView[]
  generated_at: string
}

export interface PipelinePhaseView {
  id: string
  label: string
  status: 'pending' | 'running' | 'succeeded' | 'failed' | string
  detail?: string
}

export interface PipelineTaskRunView {
  pipeline_task: string
  name: string
  status: string
  reason?: string
}

export interface StgSmokeTargetView {
  id: string
  url: string
  reachability: Reachability
  detail: string
}

export interface StgSmokeResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  targets: StgSmokeTargetView[]
  generated_at: string
}

export interface ReleaseGateCheckView {
  id: string
  label: string
  required: boolean
  reachability: Reachability
  detail: string
}

export interface ReleaseGateResponse {
  tier?: 'stg' | 'prod'
  result: string
  at?: string
  log_path: string
  checks: ReleaseGateCheckView[]
  ready: boolean
  blockers?: string[]
  generated_at: string
  reachability: Reachability
  detail: string
}

export interface RunReleaseGateResponse extends ActuationResponse {
  gate: ReleaseGateResponse
}

export interface TierBItemView {
  id: string
  label: string
  kind: 'auto' | 'manual'
  required: boolean
  reachability: Reachability
  detail: string
}

export interface TierBStatusResponse {
  cluster_id?: string
  items: TierBItemView[]
  signed_off: boolean
  signoff_at?: string
  signed_by?: string
  notes?: string
  ready: boolean
  reachability: Reachability
  detail: string
  generated_at: string
}

export interface TierBSignoffResponse extends ActuationResponse {
  status: TierBStatusResponse
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
