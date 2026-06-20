export type RemediationPhase =
  | 'starting'
  | 'diagnosing'
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
  | 'done'
  | 'error'

export interface RemediationEvent {
  id: string
  at: string
  type: RemediationEventType
  text: string
  meta?: Record<string, unknown>
}

export interface RemediationJob {
  id: string
  phase: RemediationPhase
  status: 'running' | 'done' | 'failed' | 'cancelled'
  summary?: string
  error?: string
  created_at: string
  updated_at: string
  events: RemediationEvent[]
}

export interface StartRunRequest {
  scope?: string
  actor?: string
  cluster_summary?: unknown
  service_readiness?: unknown
  governance?: unknown
  issues?: unknown
  prompt?: string
}
