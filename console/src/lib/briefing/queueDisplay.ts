import type { QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'

const COMPLETED_STATUSES = new Set<QueueItemStatus>(['done', 'closed'])

export function isCompletedQueueStatus(status: QueueItemStatus): boolean {
  return COMPLETED_STATUSES.has(status)
}

export function splitQueueByCompletion(items: QueueItem[]): {
  active: QueueItem[]
  completed: QueueItem[]
} {
  const active: QueueItem[] = []
  const completed: QueueItem[] = []
  for (const item of items) {
    if (isCompletedQueueStatus(item.status)) {
      completed.push(item)
    } else {
      active.push(item)
    }
  }
  return { active, completed }
}
