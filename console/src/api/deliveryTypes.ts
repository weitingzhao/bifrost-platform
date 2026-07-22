import type { Reachability, ActuationResponse } from './matrixTypes'

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
