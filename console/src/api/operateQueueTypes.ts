export type OperateLane = 'governance' | 'troubleshoot' | 'release' | 'business-advisory'

export type OperateQueueItem = {
  id: string
  program_id: string
  lane?: OperateLane | string
  title: string
  description?: string
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
  title: string
  description?: string
}
