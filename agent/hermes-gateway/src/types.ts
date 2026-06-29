export type SkillTrigger = 'cron' | 'webhook' | 'manual'
export type SkillStatus = 'enabled' | 'disabled' | 'error'
export type ActuationLevel = 'L0' | 'L1' | 'L2'
export type ExecutionResult = 'success' | 'failure' | 'escalated' | 'skipped'

export interface SkillDefinition {
  id: string
  label: string
  description: string
  trigger: SkillTrigger
  schedule?: string
  actuation_level: ActuationLevel
  status: SkillStatus
  tags?: string[]
  command?: string
  script?: string
  timeout_ms?: number
}

export interface SkillView {
  id: string
  label: string
  description: string
  trigger: SkillTrigger
  schedule?: string
  actuation_level: ActuationLevel
  status: SkillStatus
  last_run_at?: string
  last_result?: ExecutionResult
  tags?: string[]
}

export interface ScheduleView {
  skill_id: string
  cron: string
  enabled: boolean
  next_run_at?: string
  timezone?: string
}

export interface ExecutionRecord {
  id: string
  skill_id: string
  skill_label: string
  trigger: SkillTrigger
  result: ExecutionResult
  started_at: string
  finished_at?: string
  duration_ms?: number
  summary?: string
  error?: string
  escalated_to?: string
}
