import type { ClusterSummary, ClusterServiceReadinessResponse, ClusterGovernanceResponse } from './clusterTypes'

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
