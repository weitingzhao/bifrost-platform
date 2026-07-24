export type McpToolLevel = 'read' | 'routine' | 'confirm' | 'pr' | 'forbidden'

export type McpToolFunction =
  | 'discover'
  | 'observe'
  | 'verify'
  | 'provision'
  | 'operate'
  | 'deliver'
  | 'govern'
  | 'release'
/** Apollo fleet roles (teams) — Mission Control is a Console module, not a role. */

export type McpToolOwnerRole =
  | 'rocket'
  | 'satellite'
  | 'engineer'
  | 'ground_systems'
  | 'subcontractors'

export interface McpToolView {
  name: string
  description: string
  level: McpToolLevel
  method?: string
  route?: string
  role?: string
  /** Historical delivery batch (P0–P6 / Agent) — kept for Blueprint projection. */
  phase?: string
  /** Functional domain for Governance Tool Catalog (meta/mission/cluster/…). */
  capability?: string
  /** Stable action taxonomy, independent of the capability domain. */
  function: McpToolFunction
  /** Apollo fleet role primarily served by this tool; not an auth role. */
  owner_role: McpToolOwnerRole
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

/** Per-repo dirty summary from git-bridge via /api/v1/agent/bridge. */

export interface GitDirtyRepoDetail {
  repo: string
  branch?: string
  staged?: string[]
  modified?: string[]
  untracked?: string[]
  insertions: number
  deletions: number
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
    dirty_repo_details?: GitDirtyRepoDetail[]
    error?: string
  }
  satellite_probe_bridge: {
    url?: string
    status: string
    trade_nginx_base?: string
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
  data_source?: string
  job_count?: number
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
  suggested_level?: HermesActuationLevel
  suggested_level_reason?: string
}

export interface TrustOverrideRequest {
  level?: HermesActuationLevel
  action?: 'accept_promotion' | 'apply_demotion'
  reason?: string
  applied_by?: string
}

export interface TrustMatrixResponse {
  entries: TrustMatrixEntry[]
  generated_at: string
  data_source?: string
}

export interface CapabilityMapEntry {
  task_scope: string
  task_label: string
  autonomy: string
  /** API may emit null for unset Go nil slices — treat as []. */
  mcp_tools: string[] | null
  mission_signals: string[] | null
  has_gap: boolean
  gap_detail?: string
}

export interface CapabilityMapResponse {
  generated_at: string
  entries: CapabilityMapEntry[]
  gap_count: number
  mcp_tool_count: number
}

export interface FlightDirectorBriefing {
  period_hours: number
  jobs_completed: number
  jobs_failed: number
  escalations: number
  promotion_pending: number
  demotions: number
  summary: string
}

export interface FlightDirectorSnapshotResponse {
  generated_at: string
  hermes_available: boolean
  data_sources: string[]
  performance: AgentPerformanceResponse & { data_source?: string; job_count?: number }
  trust_matrix: TrustMatrixResponse
  capability_map: CapabilityMapResponse
  briefing: FlightDirectorBriefing
  program_complete: boolean
  note?: string
}

export type RetrospectiveRootCause =
  | 'transient'
  | 'probe_drift'
  | 'platform_defect'
  | 'config_drift'
  | 'resource_limit'
  | 'external'
  | 'unknown'

export interface HermesLlmKeyStatus {
  configured: boolean
  source: string
  provider_hint?: string
  note?: string
}

export interface HermesFirstTaskDefinition {
  id: string
  title: string
  autonomy: string
  prompt: string
  required_mcp_tools: string[]
  success_criteria: string[]
}

export interface HermesNousProbe {
  url?: string
  status: string
  version?: string
  gateway_running: boolean
  gateway_state?: string
  mcp_tool_count: number
  llm_key_configured: boolean
  dashboard_url?: string
  error?: string
}

export interface HermesReadinessBlockerDetail {
  code: string
  message: string
  remediation?: string
  owner_action?: boolean
}

export interface HermesReadinessResponse {
  generated_at: string
  ready: boolean
  blockers: string[]
  blocker_details?: HermesReadinessBlockerDetail[]
  llm_key: HermesLlmKeyStatus
  nous_hermes: HermesNousProbe
  platform_mcp_tools: number
  platform_mcp_agent_tools: number
  first_task: HermesFirstTaskDefinition
}

// Retrospective Agent — cross-job pattern analysis

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

export interface SessionSnapshotLatestResponse {
  snapshot: Record<string, unknown> | null
  saved_at?: string
  saved_by?: string
}

export interface SessionSnapshotSaveResponse {
  ok: boolean
  saved_at: string
  saved_by: string
}

export interface BriefingSessionPackResponse {
  pack: string
  pack_size: string
  track?: string
  lane?: string
  intent?: string
  char_count: number
  generated_at: string
  has_baseline: boolean
  baseline_at?: string
}

export interface BriefingSessionResult {
  id: string
  closed_at: string
  closed_by: string
  job_id?: string
  outcome: string
  summary: string
  track?: string
  lane?: string
  intent?: string
  spine_note?: string
}

export interface BriefingSessionResultsResponse {
  results: BriefingSessionResult[]
}

export interface CloseBriefingSessionRequest {
  job_id?: string
  outcome: 'done' | 'failed' | 'cancelled'
  summary: string
  track?: string
  lane?: string
  intent?: string
  spine_note?: string
  request_spine_update?: boolean
}

export interface CloseBriefingSessionResponse {
  ok: boolean
  result: BriefingSessionResult
}
