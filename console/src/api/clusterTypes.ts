import type { Reachability } from './matrixTypes'

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

export interface ClusterPostgresBackupStatusResponse {
  fresh: boolean
  signal: string
  detail: string
  last_completed_at?: string
  last_backup_name?: string
  last_backup_phase?: string
  max_age_hours: number
  age_hours?: number
  backup_count: number
  stuck_backups?: string[]
  wal_archiving_ok?: boolean
  wal_archiving_detail?: string
  generated_at: string
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

export interface DataFreshnessDatabase {
  name: string
  environment: string
  last_activity_ts?: string
  /** Wall-clock age (now − last_activity). Informational only. */
  age_days?: number
  /** Lag vs bifrost_prod activity (days). Drives Sync verdict for non-prod. */
  lag_vs_prod_days?: number
  /**
   * Sync signal for non-prod: mirrors lag_vs_prod_days.
   * @deprecated Prefer lag_vs_prod_days; kept for backward compatibility.
   */
  stale_days?: number
  /** fresh (<3d lag) | aging (3–7d) | stale (≥7d) | reference (prod) | unknown */
  verdict: 'fresh' | 'aging' | 'stale' | 'reference' | 'unknown' | string
  detail?: string
  sources?: string[]
  last_clone_at?: string
}

export interface DataFreshnessResponse {
  cluster_id: string
  primary_pod?: string
  reference_db: string
  databases: DataFreshnessDatabase[]
  cache_hit: boolean
  detail?: string
  generated_at: string
  fresh_threshold_days: number
  stale_threshold_days: number
  last_clone_at?: string
}

export interface DataCloneVerifyResult {
  database: string
  table_count: number
  sample_rows: number
  ok: boolean
  detail?: string
}

export interface DataCloneJob {
  id: string
  action: string
  status: string
  step: string
  source: string
  targets: string[]
  mode: string
  tables?: string[]
  progress: number
  detail: string
  verify?: DataCloneVerifyResult[]
  actor?: string
  trigger: string
  created_at: string
  updated_at: string
  finished_at?: string
}

export interface DataCloneSchedule {
  enabled: boolean
  interval: string
  source: string
  targets: string[]
  mode: string
  tables?: string[]
  last_auto_run_at?: string
  last_auto_run_id?: string
  last_status?: string
  updated_at: string
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
  /** Deployment rollout counters (absent/0 for Pods). */
  desired_replicas?: number
  ready_replicas?: number
  updated_replicas?: number
  available_replicas?: number
  generation?: number
  observed_generation?: number
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
  /** required = counts toward Layer B ready; planned = Phase 5+ */
  phase?: 'required' | 'planned'
}

export interface ClusterObservabilityResponse {
  cluster_id: string
  namespace: string
  layer_b_status: LayerBStatus
  layer_b_install_enabled: boolean
  reachability: Reachability
  detail: string
  components: ObservabilityComponent[]
  grafana_url?: string
  prometheus_url?: string
  docs_url?: string
  generated_at: string
}

export interface TelemetrySamplePoint {
  labels: Record<string, string>
  value: number
  timestamp?: number
}

export interface TelemetryMetricResult {
  id: string
  title: string
  unit?: string
  status: 'ok' | 'empty' | 'error'
  detail?: string
  points: TelemetrySamplePoint[]
}

export interface TelemetryOverviewResponse {
  namespace: string
  prometheus_url?: string
  layer_b_status?: LayerBStatus
  reachability?: Reachability
  metrics: TelemetryMetricResult[]
  generated_at: string
}

export interface TelemetryAlertEntry {
  labels: Record<string, string>
  annotations: Record<string, string>
  state: string
  active_at?: string
  value?: string
}

export interface TelemetryAlertsResponse {
  prometheus_url?: string
  alerts: TelemetryAlertEntry[]
  generated_at: string
}

export interface TelemetryTargetEntry {
  labels: Record<string, string>
  scrape_pool?: string
  scrape_url?: string
  health: string
  last_error?: string
  last_scrape?: string
  last_scrape_duration?: number
}

export interface TelemetryTargetsResponse {
  prometheus_url?: string
  active_targets: TelemetryTargetEntry[]
  dropped_targets?: TelemetryTargetEntry[]
  generated_at: string
}
