// Preserve public type paths for existing execution and protected callers.
import type { DeliveryPipelineRunView, SupplyChainTaskRunView } from '@/domains/delivery/api/deliveryTypes'
export type { ArgoCDStatus, CompareResponse, DeliveryPipelineRunView, DeliveryPipelineRunsResponse, DeliveryPipelineView, DeliveryPipelinesResponse, DeliveryRunLogsResponse, DockerfileConfigMapView, GitOpsApplicationCondition, GitOpsApplicationView, GitOpsAppsResponse, GitOpsArgoCDServerView, ReleaseWindowRecord, ReleaseWindowResponse, StgSmokeResponse, StgSmokeTargetView, StgWorkloadImageView, SupplyChainResponse, SupplyChainTaskRunView } from '@/domains/delivery/api/deliveryTypes'
import type { Reachability, ActuationResponse } from './matrixTypes'

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

export interface DeliveryPipelinePreflightResponse {
  cluster_id: string
  pipeline: string
  build_ready: boolean
  reason?: string
  reachability: Reachability
  generated_at: string
}

export interface DeliveryStartRunResponse extends ActuationResponse {
  run?: DeliveryPipelineRunView
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

export type ReleaseCycleLane = 'trade' | 'platform'

export type ReleaseCycleStepKind =
  | 'stg_deploy'
  | 'stg_gate'
  | 'prod_deploy'
  | 'prod_gate'

export type ReleaseCycleOutcome = 'released' | 'failed' | 'in_progress' | 'superseded'

export interface ReleaseCycleStepView {
  kind: ReleaseCycleStepKind
  started_at?: string
  completed_at?: string
  result?: string
  run_name?: string
  detail?: string
  gate_checks?: ReleaseGateCheckView[]
}

export interface ReleaseCycleView {
  id: string
  lane: ReleaseCycleLane
  revision: string
  outcome: ReleaseCycleOutcome | string
  started_at: string
  completed_at?: string
  steps: ReleaseCycleStepView[]
  agent_session_id?: string
  triggered_by?: string
}

export interface ReleaseCyclesResponse {
  lane: ReleaseCycleLane
  entries: ReleaseCycleView[]
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
