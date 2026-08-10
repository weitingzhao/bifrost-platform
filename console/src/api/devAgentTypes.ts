export interface DevAgentProgramInfo {
  id: string
  title: string
  description: string
  status: string
}

export interface DevAgentProgramSummary extends DevAgentProgramInfo {
  phase_count: number
  phases_done: number
  all_phases_done: boolean
  active: boolean
  lane_id?: string
  runtime_job_status?: string
  pending_count?: number
  prompt_ready?: boolean
  runtime_bucket?: 'running' | 'ready' | 'idle' | 'settled' | string
}

export interface DevAgentNamingWarning {
  program_id: string
  field: string
  message: string
}

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
  started_at?: string
  completed_at?: string
  approved_by?: string
  cursor_agent_id?: string
}

export interface DevAgentJobTrace {
  active_job: DevAgentJob | null
  history: DevAgentJob[]
}

export interface DevAgentStatusResponse extends DevAgentJobTrace {
  project: string
  program: DevAgentProgramInfo
  phases: DevAgentPhase[]
}

export interface DevAgentProgramJobsResponse extends DevAgentJobTrace {
  program_id: string
}

export interface DevAgentProgramsResponse {
  programs: DevAgentProgramSummary[]
  naming_warnings?: DevAgentNamingWarning[]
}

export interface DevAgentProgramDetailResponse {
  program: DevAgentProgramInfo
  phases: Array<{
    id: string
    title: string
    status: string
    verify_cmd?: string
    acceptance?: string[]
    depends_on?: string[]
    rendered_prompt?: string
    skill_injected?: boolean
  }>
  bridge: {
    workspace: string
    model: string
    skill_path?: string
    skill_loaded: boolean
  }
  active: boolean
}
