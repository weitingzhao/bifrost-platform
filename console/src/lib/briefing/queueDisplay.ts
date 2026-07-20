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

/** One-line lane stage for pack headers (active/total + top status + completed count). */
export function formatQueueStageSummary(items: QueueItem[]): string {
  const { active, completed } = splitQueueByCompletion(items)
  const total = items.length
  const top = active[0]
  const topPart =
    top != null ? `top: [${top.status}] ${top.label}` : 'top: (none active)'
  return `active ${active.length}/${total} · ${topPart} · completed: ${completed.length}`
}
