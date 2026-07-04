export interface DevAgentPhase {
  id: string
  title: string
  status: 'pending' | 'running' | 'done' | 'failed'
  started_at?: string
  completed_at?: string
}

export interface DevAgentJob {
  id: string
  phase_id: string
  status: 'running' | 'awaiting_review' | 'done' | 'failed' | 'cancelled' | 'idle'
  output: string
  summary?: string
  completed_at?: string
  cursor_agent_id?: string
}

export interface DevAgentStatusResponse {
  project: string
  phases: DevAgentPhase[]
  active_job: DevAgentJob | null
  history: DevAgentJob[]
}
