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

export interface OpsContextResponse {
  meta: OpsContextMeta
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
