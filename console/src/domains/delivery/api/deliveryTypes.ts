import type { Reachability } from '@/api/matrixTypes'
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


export interface DeliveryPipelineView {
  name: string
  namespace: string
  detail?: string
  build_ready?: boolean
  block_reason?: string
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


/** ConfigMap cicd/bifrost-release-window (api/internal/delivery windowRecord). */
export interface ReleaseWindowRecord {
  who: string
  what: string
  env: string
  pid: number
  host: string
  started_at: string
  expires_at?: string
  reason?: string
}


/** GET /api/v1/delivery/release-window (viewer token). */
export interface ReleaseWindowResponse {
  open: boolean
  window?: ReleaseWindowRecord
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
  /** Newest kubectl --timestamps stamp across fetched containers (ISO). */
  last_log_at?: string
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


/** Changed paths between two refs (Gitea compare) — constellation path impact. */
export interface CompareResponse {
  cluster_id: string
  repo: string
  from: string
  to: string
  files: string[]
  reachability: Reachability
  detail: string
  generated_at: string
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
