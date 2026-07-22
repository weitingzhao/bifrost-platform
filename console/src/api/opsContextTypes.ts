import type { ActuationResponse } from './matrixTypes'

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
  /** D-A: delivered-but-unsigned waves (spineIndex in [done, done+ready_for_signoff)). */
  ready_for_signoff?: number
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

export interface MigrateWaveActuationResponse extends ActuationResponse {
  stream: MigrateStream
  headline: string
}

// Hermes Gateway — Autonomous Agent types
