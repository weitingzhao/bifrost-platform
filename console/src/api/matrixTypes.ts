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

export type PayloadClassification = 'NOMINAL' | 'PROBE_DRIFT' | 'DATA_LAYER' | 'HTTP_FAIL' | 'UNKNOWN'

export interface DatastoreComponentVerification {
  matrix_reachability: Reachability
  cluster_reachability: Reachability
  classification: PayloadClassification
  detail: string
}

export interface EnvPayloadVerification {
  environment: string
  label: string
  classification: PayloadClassification
  postgres: DatastoreComponentVerification
  redis: DatastoreComponentVerification
  http_failures: string[] | null
  detail: string
}

export interface VerifyPayloadSummary {
  overall: PayloadClassification
  probe_drift_count: number
  data_layer_count: number
  http_fail_count: number
  nominal_count: number
}

export interface VerifyPayloadResponse {
  generated_at: string
  environments: EnvPayloadVerification[]
  summary: VerifyPayloadSummary
}

export type MissionMatrixSignal = 'ok' | 'degraded' | 'fail' | 'unknown'

export interface TradeEnvSnapshotView {
  environment: string
  label: string
  signal: MissionMatrixSignal
  reachable: number
  total: number
  detail: string
}

export interface PostFixVerificationView {
  passed: boolean
  mission_matrix_nominal: boolean
  datastore_verification_nominal: boolean
  probe_drift_remaining: boolean
  detail: string
  agent_guidance: string
}

export interface VerifyMissionSnapshotResponse {
  generated_at: string
  trade_dev: TradeEnvSnapshotView
  trade_stg: TradeEnvSnapshotView
  trade_prod: TradeEnvSnapshotView
  payload_overall: MissionMatrixSignal
  payload_verification: VerifyPayloadResponse
  post_fix_verification: PostFixVerificationView
}

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
  /** Seat where this platform-api instance is viewed from (OPS_VIEWER_ENV / cluster). */
  viewer_env?: string
}

export type EscapeRouteStatus = 'ok' | 'degraded' | 'fail' | 'unknown' | 'documented'

export interface EscapeRouteProbe {
  id: string
  label: string
  url?: string
  status: EscapeRouteStatus
  detail: string
  latency_ms?: number
}

export interface EscapeRouteView {
  id: string
  label: string
  layer: string
  summary: string
  command?: string
  status: EscapeRouteStatus
  detail: string
  probes?: EscapeRouteProbe[]
  runbook_refs?: string[]
}

export interface EscapeHatchQuarterly {
  interval_days: number
  last_drill_at?: string
  last_drill_by?: string
  notes?: string
  next_due_at?: string
  overdue: boolean
  days_since_last_drill?: number
}

export interface EscapeHatchResponse {
  generated_at: string
  runbook_version: string
  overall: EscapeRouteStatus
  routes: EscapeRouteView[]
  quarterly: EscapeHatchQuarterly
  agent_guidance?: string
}

export interface EscapeHatchDrillResponse {
  ok: boolean
  drill?: {
    at: string
    by: string
    notes?: string
    route_ids?: string[]
  }
}
