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
  prerequisites?: string[]
}

export interface MigrateTrack {
  label: string
  streams: MigrateStream[]
}

export interface OperateTrack {
  label: string
  note?: string
}

export interface AutomateTrack {
  label: string
  streams: MigrateStream[]
}

export interface InfraTrack {
  label: string
  streams: MigrateStream[]
}

export interface OpsContextTracks {
  build?: BuildTrack
  migrate?: MigrateTrack
  automate?: AutomateTrack
  infra?: InfraTrack
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
  api_reachability?: Reachability
  detail: string
  server_version?: string
  nodes_ready: number
  nodes_total: number
  elastic_standby?: number
  elastic_degraded?: number
  nodes_registered?: number
  nodes_registered_ready?: number
  failing_pods: number
  failing_pod_details?: FailingPodView[]
  running_pods: number
  pending_pods: number
  cpu_allocatable?: string
  memory_allocatable?: string
  generated_at: string
}

export interface FailingPodView {
  namespace: string
  name: string
  phase: string
  reason: string
  node?: string
  age?: string
}

export interface ClusterNodeCapability {
  id: string
  label: string
  category?: string
  detail?: string
}

export interface ClusterCapabilityCoverage {
  id: string
  label: string
  category: string
  scope: string
  label_hint?: string
  required_for?: string
  nodes_ready: number
  nodes_total: number
  node_names: string[]
  reachability: Reachability
  gap_reason?: string
}

export interface ClusterCapabilityProbe {
  id: string
  label: string
  category: string
  status: string
  reachability: Reachability
  detail: string
}

export interface ClusterGovernanceResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  catalog: {
    id: string
    label: string
    category: string
    scope: string
    label_hint?: string
    required_for?: string
  }[]
  node_coverage: ClusterCapabilityCoverage[]
  cluster_capabilities: ClusterCapabilityProbe[]
  generated_at: string
}

export type ServiceDomainStatus = 'ready' | 'partial' | 'standby' | 'unavailable'

export interface ServiceDependency {
  id: string
  label: string
  reachability: Reachability
  detail?: string
}

export interface ServiceDomain {
  id: string
  label: string
  status: ServiceDomainStatus | string
  reachability: Reachability
  summary: string
  dependencies: ServiceDependency[]
}

export interface ClusterServiceReadinessResponse {
  cluster_id: string
  reachability: Reachability
  detail: string
  domains: ServiceDomain[]
  generated_at: string
}

export interface PostgresInstance {
  pod_name: string
  role: string
  node: string
  phase: string
  reachability: Reachability
  detail?: string
}

export interface PostgresDatabase {
  name: string
  environment: string
  cr_name?: string
  reachability: Reachability
  detail?: string
}

export interface PostgresLegacyEndpoint {
  kind: string
  namespace?: string
  host?: string
  reachability: Reachability
  detail?: string
}

export interface PostgresLanAccess {
  available: boolean
  host?: string
  node_port?: number
  endpoint?: string
  user?: string
  reachability: Reachability
  detail?: string
}

export interface ClusterPostgresStatusResponse {
  cluster_id: string
  reachability: Reachability
  summary: string
  migration_phase: string
  migration_step: number
  migration_total: number
  operator: ServiceDependency
  cnpg_cluster: ServiceDependency
  instances: PostgresInstance[]
  instances_spec: number
  instances_ready: number
  primary_pod?: string
  primary_node?: string
  rw_service: string
  ro_service: string
  lan_access: PostgresLanAccess
  storage_class: string
  storage_size: string
  backup: ServiceDependency
  minio: ServiceDependency
  databases: PostgresDatabase[]
  legacy: PostgresLegacyEndpoint[]
  embedded: PostgresLegacyEndpoint[]
  postgres_role: ServiceDependency
  generated_at: string
}

export interface RedisTargetInstance {
  name: string
  environment: string
  role: string
  service: string
  maxmemory_policy?: string
  reachability: Reachability
  detail?: string
}

export interface RedisEnvEndpoint {
  environment: string
  live_service: string
  queue_service: string
  live_reachability: Reachability
  queue_reachability: Reachability
  network_policy: string
  detail?: string
}

export interface RedisEmbeddedEndpoint {
  namespace: string
  host: string
  image?: string
  reachability: Reachability
  detail?: string
}

export interface RedisLanEndpoint {
  name: string
  environment: string
  role: string
  host?: string
  node_port?: number
  endpoint?: string
  database?: string
  available: boolean
  reachability: Reachability
  detail?: string
}

export interface ClusterRedisStatusResponse {
  cluster_id: string
  reachability: Reachability
  summary: string
  migration_phase: string
  migration_step: number
  migration_total: number
  migration_redis_step: number
  targets_ready: number
  targets_total: number
  embedded_active: number
  target_instances: RedisTargetInstance[]
  env_endpoints: RedisEnvEndpoint[]
  lan_endpoints: RedisLanEndpoint[]
  embedded: RedisEmbeddedEndpoint[]
  legacy: PostgresLegacyEndpoint[]
  backup: ServiceDependency
  minio: ServiceDependency
  generated_at: string
}

export interface ClusterNode {
  name: string
  status: string
  roles: string
  architecture?: string
  os_image?: string
  workload_label?: string
  capabilities?: ClusterNodeCapability[]
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
  elastic_mode?: 'active' | 'standby' | 'degraded'
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

export type McpToolLevel = 'read' | 'routine' | 'confirm' | 'pr' | 'forbidden'

export interface McpToolView {
  name: string
  description: string
  level: McpToolLevel
  method?: string
  route?: string
  role?: string
  phase?: string
  implemented: boolean
}

export interface McpToolsResponse {
  server_name: string
  server_version: string
  contract_version: string
  tools: McpToolView[]
  implemented_count: number
  generated_at: string
}

export interface McpStatusResponse {
  server_name: string
  server_version: string
  transport: string
  platform_api_url: string
  script_path: string
  cursor_config: {
    command: string
    args: string[]
    env: string[]
  }
  tool_count: number
  implemented_count: number
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
  revision?: string
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

export interface GiteaTagView {
  name: string
  repo: string
  commit?: string
}

export interface GiteaBranchView {
  name: string
  repo: string
  commit?: string
}

export interface RevisionsResponse {
  cluster_id: string
  repos: string[]
  default_ref: string
  tags: GiteaTagView[]
  branches: GiteaBranchView[]
  /** Ref names present in every tracked repo (safe for multi-repo deploy). */
  common_refs: string[]
  reachability: Reachability
  detail: string
  generated_at: string
}

export interface RepoRefStatus {
  repo: string
  exists: boolean
  /** "branch" | "tag" | "commit" | "missing" */
  kind: string
  commit?: string
  detail?: string
}

export interface RefPreflightResponse {
  cluster_id: string
  pipeline: string
  revision: string
  repos: RepoRefStatus[]
  missing: string[]
  ready: boolean
  reachability: Reachability
  detail: string
  generated_at: string
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
  tier?: 'stg' | 'prod' | 'platform-stg' | 'platform-prod'
  result: string
  revision?: string
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

export interface GateHistoryEntry {
  tier?: 'stg' | 'prod'
  at: string
  result: string
  revision?: string
  log_path: string
  checks: ReleaseGateCheckView[]
  triggered_by?: string
  summary?: string
}

export interface GateHistoryResponse {
  tier: 'stg' | 'prod'
  entries: GateHistoryEntry[]
}

export interface ReleaseStageState {
  revision?: string
  status: string
  at?: string
  detail?: string
}

export interface ReleaseAction {
  action: string
  label: string
  description: string
  mcp_tool?: string
  params?: Record<string, string>
}

export interface ReleaseStateResponse {
  stg_deploy: ReleaseStageState
  stg_gate: ReleaseStageState
  prod_deploy: ReleaseStageState
  prod_gate: ReleaseStageState
  consistent: boolean
  warnings?: string[]
  next_action?: ReleaseAction
  available_actions: ReleaseAction[]
  generated_at: string
}

export interface VisionV1GateCheckView {
  id: string
  label: string
  required: boolean
  reachability: Reachability
  detail?: string
}

export interface VisionV1GateResponse {
  milestone: string
  result: string
  ready: boolean
  blockers?: string[]
  checks: VisionV1GateCheckView[]
  at?: string
  signed_at?: string
  signed_by?: string
  reachability: Reachability
  detail?: string
  generated_at: string
}

export interface RunVisionV1GateResponse extends ActuationResponse {
  gate: VisionV1GateResponse
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

export type RemediationPhase =
  | 'starting'
  | 'diagnosing'
  | 'awaiting_approval'
  | 'remediating'
  | 'verifying'
  | 'done'
  | 'failed'
  | 'cancelled'

export type RemediationEventType =
  | 'thinking'
  | 'tool_call'
  | 'tool_result'
  | 'status'
  | 'approval_request'
  | 'done'
  | 'error'

export interface RemediationApprovalOption {
  id: string
  label: string
  description?: string
  destructive?: boolean
}

export interface RemediationEvent {
  id: string
  at: string
  type: RemediationEventType
  text: string
  meta?: Record<string, unknown>
}

export type RemediationJobStatus = 'running' | 'done' | 'failed' | 'cancelled'

export interface RemediationJob {
  id: string
  phase: RemediationPhase
  status: RemediationJobStatus
  summary?: string
  error?: string
  actor?: string
  scope?: string
  /** Operator-visible mission brief at job start (prompt, issues, cluster context). */
  init_brief?: string
  created_at: string
  updated_at: string
  events?: RemediationEvent[]
}

export interface RemediationJobsResponse {
  jobs: RemediationJob[]
}

export interface StartRemediationRequest {
  scope?: string
  cluster_summary?: ClusterSummary
  service_readiness?: ClusterServiceReadinessResponse
  governance?: ClusterGovernanceResponse
  issues?: unknown
  prompt?: string
}

export interface AgentNightlyReportResponse {
  available: boolean
  content?: string
  source?: string
  generated_at?: string
  hint?: string
}

export interface NightlyTriggerResponse {
  status: string
  script?: string
  log_path?: string
  reports_dir?: string
  hint?: string
  error?: string
}

export interface AgentDeployJob {
  id: string
  status: 'running' | 'done' | 'failed'
  remote: string
  role?: 'primary' | 'standby' | 'custom'
  started_at: string
  finished_at?: string
  exit_code?: number
  log: string
  error?: string
}

export interface AgentDeployTarget {
  id: string
  role: 'primary' | 'standby'
  remote: string
  peer_ssh?: string
  peer_url?: string
}

export interface AgentDeployStatusResponse {
  enabled: boolean
  remote: string
  targets?: AgentDeployTarget[]
  script_path?: string
  hint?: string
  current?: AgentDeployJob
  last?: AgentDeployJob
}

export interface AgentDeployStartResponse {
  status: string
  job?: AgentDeployJob
  error?: string
}

export interface RemediationHealthResponse {
  status: string
  error?: string
  service?: string
  cursor_api_key?: boolean
}

export type DriftProposalStatus =
  | 'pending_approval'
  | 'approved'
  | 'rejected'
  | 'running'
  | 'done'
  | 'failed'

export interface DriftProposal {
  id: string
  status: DriftProposalStatus
  host?: string
  platform_api?: string
  report_source?: string
  layers_failed: string[]
  findings_count: number
  summary: string
  created_at: string
  updated_at: string
  remediation_job_id?: string
  approved_by?: string
  approved_at?: string
  rejected_by?: string
  rejected_at?: string
  reject_note?: string
  error?: string
}

export interface DriftProposalsResponse {
  proposals: DriftProposal[]
}

export interface ApproveDriftProposalResponse {
  proposal: DriftProposal
  remediation_job: RemediationJob
}

export interface RunnerStatus {
  url: string
  role?: 'primary' | 'standby'
  status: string
  version?: string
  active?: boolean
  cursor_api_key?: boolean
  service?: string
  error?: string
}

export interface AgentBridgeResponse {
  generated_at: string
  remediation_runner: RunnerStatus
  runners?: RunnerStatus[]
  git_bridge: {
    url?: string
    status: string
    workspace?: string
    repo_count?: number
    dirty_repos?: number
    error?: string
  }
  hermes_mcp: {
    url?: string
    status: string
    error?: string
    note?: string
  }
  nous_hermes: {
    url?: string
    status: string
    version?: string
    release_date?: string
    gateway_running: boolean
    gateway_state?: string
    active_agents: number
    active_sessions: number
    mcp_tool_count: number
    dashboard_url?: string
    error?: string
  }
  platform_mcp: {
    server_name: string
    server_version: string
    tool_count: number
    implemented_count: number
    agent_tool_count: number
    transport: string
    script_path: string
  }
  nightly_report: {
    available: boolean
    generated_at?: string
    source?: string
    hint?: string
  }
}

export interface BuildPhaseGateCheck {
  id: string
  label: string
  status: 'pass' | 'in_progress' | 'pending' | 'blocked'
  required: boolean
  detail?: string
}

export interface BuildPhaseGateResponse {
  phase: string
  total_tasks: number
  done_tasks: number
  ready: boolean
  result: string
  checks: BuildPhaseGateCheck[]
  blockers?: string[]
  signed_at?: string
  signed_by?: string
  last_run_at?: string
  last_run_result?: string
  generated_at: string
}

export interface RunBuildPhaseGateResponse extends ActuationResponse {
  gate: BuildPhaseGateResponse
}

// Hermes Gateway — Autonomous Agent types

export type HermesSkillTrigger = 'cron' | 'webhook' | 'manual'
export type HermesSkillStatus = 'enabled' | 'disabled' | 'error'
export type HermesActuationLevel = 'L0' | 'L1' | 'L2'

export interface HermesSkill {
  id: string
  label: string
  description: string
  trigger: HermesSkillTrigger
  schedule?: string
  actuation_level: HermesActuationLevel
  status: HermesSkillStatus
  last_run_at?: string
  last_result?: 'success' | 'failure' | 'skipped'
  tags?: string[]
}

export interface HermesSchedule {
  skill_id: string
  cron: string
  enabled: boolean
  next_run_at?: string
  timezone?: string
}

export type HermesExecutionResult = 'success' | 'failure' | 'escalated' | 'skipped'

export interface HermesExecution {
  id: string
  skill_id: string
  skill_label: string
  trigger: HermesSkillTrigger
  result: HermesExecutionResult
  started_at: string
  finished_at?: string
  duration_ms?: number
  summary?: string
  error?: string
  escalated_to?: string
}

export interface HermesSkillsResponse {
  gateway_status: string
  skills: HermesSkill[]
  generated_at: string
}

export interface HermesSchedulesResponse {
  schedules: HermesSchedule[]
  generated_at: string
}

export interface HermesExecutionsResponse {
  executions: HermesExecution[]
  total: number
  generated_at: string
}

export interface RunnerSmokeCheck {
  id: string
  label: string
  status: 'pass' | 'fail'
  detail?: string
}

export interface RunnerSmokeResponse {
  status: 'pass' | 'fail'
  version: string
  role: string
  checks: RunnerSmokeCheck[]
}

export interface HermesGatewayHealth {
  status: string
  version?: string
  skill_count?: number
  uptime_seconds?: number
  error?: string
}

// Agent Governance — Flight Director types

export interface AgentPerformanceWindow {
  window: '7d' | '30d'
  total_executions: number
  success_count: number
  failure_count: number
  escalation_count: number
  success_rate: number
  mean_duration_ms: number
  intervention_rate: number
}

export interface AgentPerformanceResponse {
  windows: AgentPerformanceWindow[]
  mttr_seconds?: number
  generated_at: string
}

export interface TrustMatrixEntry {
  skill_id: string
  skill_label: string
  current_level: HermesActuationLevel
  consecutive_successes: number
  promotion_eligible: boolean
  demotion_triggered: boolean
  last_override_at?: string
  last_override_by?: string
}

export interface TrustMatrixResponse {
  entries: TrustMatrixEntry[]
  generated_at: string
}

// Retrospective Agent — cross-job pattern analysis

export type RetrospectiveRootCause =
  | 'transient'
  | 'platform_defect'
  | 'config_drift'
  | 'resource_limit'
  | 'external'
  | 'unknown'

export type RetrospectiveSeverity = 'critical' | 'high' | 'medium' | 'low'

export interface RetrospectiveComponentRef {
  namespace?: string
  deployment?: string
  pod?: string
  pipeline?: string
  service?: string
}

export interface RetrospectiveActionTaken {
  tool: string
  count: number
}

export interface RetrospectiveJobRef {
  id: string
  scope: string
  status: string
  created_at: string
}

export interface RetrospectiveClassificationSignal {
  name: string
  weight: number
  cause: RetrospectiveRootCause
  detail?: string
}

export interface RetrospectivePatternCluster {
  id: string
  label: string
  description: string
  root_cause: RetrospectiveRootCause
  confidence: number
  signals?: RetrospectiveClassificationSignal[]
  severity: RetrospectiveSeverity
  component: RetrospectiveComponentRef
  occurrences: number
  first_seen: string
  last_seen: string
  jobs: RetrospectiveJobRef[]
  top_actions: RetrospectiveActionTaken[]
  success_rate: number
  avg_duration_seconds: number
  trending: 'up' | 'stable' | 'down'
}

export interface RetrospectiveRootCauseDistribution {
  cause: RetrospectiveRootCause
  count: number
  fraction: number
}

export interface RetrospectiveScopeStats {
  scope: string
  total: number
  done: number
  failed: number
  cancelled: number
  running: number
  success_rate: number
  avg_duration_seconds: number
}

export interface RetrospectiveToolUsage {
  tool: string
  count: number
  jobs: number
}

export interface RetrospectiveNamespaceActivity {
  namespace: string
  tool_calls: number
  jobs: number
  top_actions: RetrospectiveActionTaken[]
}

export interface RetrospectiveReport {
  generated_at: string
  total_jobs: number
  analysis_window: string
  patterns: RetrospectivePatternCluster[]
  root_cause_distribution: RetrospectiveRootCauseDistribution[]
  scope_stats: RetrospectiveScopeStats[]
  tool_usage: RetrospectiveToolUsage[]
  namespaces: RetrospectiveNamespaceActivity[]
  health_score: number
  insights: string[]
}

// Self-health probe (L1 control plane liveness)
export type SelfHealthProbeStatus = 'ok' | 'degraded' | 'fail' | 'unknown'

export interface SelfHealthProbe {
  id: string
  category: string
  env: string
  url?: string
  status: SelfHealthProbeStatus
  detail: string
  latency_ms: number
}

export interface SelfHealthResponse {
  generated_at: string
  probes: SelfHealthProbe[]
  overall: SelfHealthProbeStatus
}
