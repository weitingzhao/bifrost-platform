// Preserve public type paths for existing execution and protected callers.
export type { AgentBridgeResponse, AgentDeployJob, AgentDeployStatusResponse, AgentDeployTarget, RunnerStatus } from '@/domains/foundation/agent-observation/api/agentTypes'

export type RetrospectiveRootCause =
  | 'transient'
  | 'probe_drift'
  | 'platform_defect'
  | 'config_drift'
  | 'resource_limit'
  | 'external'
  | 'unknown'

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
