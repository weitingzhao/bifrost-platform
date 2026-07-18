export type OperateLane = 'governance' | 'troubleshoot' | 'release' | 'business-advisory'
export type HandoffKind = 'one_off' | 'recurring_setup'
export type RiskLevel = 'low' | 'medium' | 'high'

export type OperateQueueItem = {
  id: string
  program_id: string
  source_lane_id?: string
  lane?: OperateLane | string
  operate_lane?: OperateLane | string
  title: string
  description?: string
  handoff_kind?: HandoffKind
  reason?: string
  agent_task_id?: string
  acceptance_criteria?: string[]
  verification_steps?: string[]
  risk_level?: RiskLevel
  owner?: string
  due_at?: string
  execution_job_id?: string
  completion_evidence?: string[]
  status: 'open' | 'closed'
  created_at: string
  updated_at?: string
  closed_at?: string
  source?: 'post_completion' | 'manual'
  pending_id?: string
  approved_by?: string
}

export type OperateQueueResponse = {
  open: OperateQueueItem[]
  recent_closed: OperateQueueItem[]
}

export type EnqueueOperateQueueRequest = {
  program_id: string
  lane?: OperateLane | string
  source_lane_id?: string
  operate_lane?: OperateLane | string
  title: string
  description?: string
  handoff_kind?: HandoffKind
  reason?: string
  agent_task_id?: string
  acceptance_criteria?: string[]
  verification_steps?: string[]
  risk_level?: RiskLevel
  owner?: string
  due_at?: string
}

export type CloseOperateQueueRequest = {
  completion_evidence: string[]
  post_fix_verification_passed?: boolean
}
