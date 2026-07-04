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
  completed_at?: string
  cursor_agent_id?: string
}

export interface DevAgentStatusResponse {
  project: string
  program: DevAgentProgramInfo
  phases: DevAgentPhase[]
  active_job: DevAgentJob | null
  history: DevAgentJob[]
}

export interface DevAgentProgramsResponse {
  programs: DevAgentProgramSummary[]
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

export interface DevAgentPersistenceFile {
  program_id: string
  path: string
  updated_at?: string
  bytes: number
}

export interface DevAgentPersistenceResponse {
  state_dir: string
  active_program_id: string
  active_program_path: string
  files: DevAgentPersistenceFile[]
}
