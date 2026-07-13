import type { QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'
import type { Reachability } from '@bifrost/ui'

/** Unified Briefing work-status vocabulary (Scope / Lane / Queue). */
export type BriefingWorkStatus = 'doing' | 'planned' | 'ready' | 'done' | 'new' | 'blocked'

export type LaneLifecycle = 'active' | 'planned' | 'empty' | 'complete'

/** Digest-driven filter for the Lanes board (null = show all lifecycles). */
export type BriefingLaneLifecycleFilter = LaneLifecycle

export function briefingLifecycleFilterLabel(filter: BriefingLaneLifecycleFilter): string {
  switch (filter) {
    case 'active':
      return 'Doing'
    case 'planned':
      return 'Planned'
    case 'empty':
      return 'Ready'
    case 'complete':
      return 'Done'
  }
}

export const BRIEFING_STATUS_LABEL: Record<BriefingWorkStatus, string> = {
  doing: 'Doing',
  planned: 'Planned',
  ready: 'Ready',
  done: 'Done',
  new: 'New',
  blocked: 'Blocked',
}

/** StatusLamp reach mapping — one lamp language for every surface. */
export function lampForBriefingStatus(status: BriefingWorkStatus): Reachability {
  switch (status) {
    case 'done':
      return 'ok'
    case 'doing':
      return 'degraded'
    case 'blocked':
      return 'fail'
    case 'planned':
    case 'ready':
    case 'new':
      return 'unknown'
  }
}

export function queueItemToBriefingStatus(status: QueueItemStatus): BriefingWorkStatus {
  switch (status) {
    case 'done':
    case 'closed':
      return 'done'
    case 'in_progress':
    case 'next':
    case 'ready_for_signoff':
      return 'doing'
    case 'issue':
    case 'blocked':
      return 'blocked'
    case 'pending':
    default:
      return 'planned'
  }
}

export function lifecycleToBriefingStatus(lifecycle: LaneLifecycle): BriefingWorkStatus {
  switch (lifecycle) {
    case 'active':
      return 'doing'
    case 'planned':
      return 'planned'
    case 'empty':
      return 'ready'
    case 'complete':
      return 'done'
  }
}

/** Classify a lane queue the same way TrackLaneSection does. */
export function laneLifecycleFromQueue(queue: QueueItem[]): LaneLifecycle {
  if (queue.length === 0) return 'empty'
  const hasActive = queue.some(
    q =>
      q.status === 'in_progress' ||
      q.status === 'next' ||
      q.status === 'ready_for_signoff' ||
      q.status === 'issue',
  )
  if (hasActive) return 'active'
  const allDone = queue.every(q => q.status === 'done' || q.status === 'closed')
  if (allDone) return 'complete'
  return 'planned'
}

export function progressToBriefingStatus(
  progress: { percent: number } | null | undefined,
): BriefingWorkStatus {
  if (progress == null) return 'ready'
  if (progress.percent >= 100) return 'done'
  if (progress.percent > 0) return 'doing'
  return 'planned'
}

export interface ScopeWorkSummary {
  status: BriefingWorkStatus
  progress: { done: number; total: number; percent: number } | null
  /** Next actionable queue item label under this scope × track type. */
  nextStep: string | null
  laneCounts: {
    doing: number
    planned: number
    ready: number
    done: number
  }
}

/**
 * Aggregate status for the currently selected (scope, trackType) — same source of
 * truth as the Lanes section (lane queues), not the whole spine track.
 */
export function computeScopeWorkSummary(
  laneQueues: Array<{ label: string; queue: QueueItem[] }>,
): ScopeWorkSummary {
  const laneCounts = { doing: 0, planned: 0, ready: 0, done: 0 }
  let doneItems = 0
  let totalItems = 0
  let nextStep: string | null = null

  for (const { label, queue } of laneQueues) {
    const life = laneLifecycleFromQueue(queue)
    switch (life) {
      case 'active':
        laneCounts.doing += 1
        break
      case 'planned':
        laneCounts.planned += 1
        break
      case 'empty':
        laneCounts.ready += 1
        break
      case 'complete':
        laneCounts.done += 1
        break
    }

    for (const item of queue) {
      if (item.status === 'issue') continue
      totalItems += 1
      if (item.status === 'done' || item.status === 'closed') doneItems += 1
    }

    if (nextStep == null) {
      const activeItem = queue.find(
        q =>
          q.status === 'in_progress' ||
          q.status === 'next' ||
          q.status === 'ready_for_signoff' ||
          q.status === 'issue',
      )
      if (activeItem != null) {
        nextStep = activeItem.label
      } else {
        const plannedItem = queue.find(q => q.status === 'pending' || q.status === 'blocked')
        if (plannedItem != null) nextStep = `${label}: ${plannedItem.label}`
      }
    }
  }

  let status: BriefingWorkStatus
  if (laneQueues.length === 0) {
    status = 'ready'
  } else if (laneCounts.doing > 0) {
    status = 'doing'
  } else if (laneCounts.planned > 0) {
    status = 'planned'
  } else if (laneCounts.done === laneQueues.length) {
    status = 'done'
  } else if (laneCounts.ready > 0) {
    status = 'ready'
  } else {
    status = 'planned'
  }

  const progress =
    totalItems > 0
      ? {
          done: doneItems,
          total: totalItems,
          percent: Math.round((doneItems / totalItems) * 100),
        }
      : null

  return { status, progress, nextStep, laneCounts }
}
