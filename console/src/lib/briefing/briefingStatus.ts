import type { ProgramSummary } from '@/api/programsTypes'
import type { QueueItem, QueueItemStatus } from '@/lib/briefing/workLanes'
import {
  isProgramCatalogComplete,
  isProgramSessionReleased,
} from '@/lib/briefing/programClose'

export {
  boardCloseTag,
  isGatesComplete,
  isProgramCatalogComplete,
  isProgramDeliveryClosed,
  isProgramSessionReleased,
  type BoardCloseTag,
  type ProgramCloseFields,
} from '@/lib/briefing/programClose'

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

type LaneProgram = Pick<ProgramSummary, 'lane_id'> &
  Parameters<typeof isProgramSessionReleased>[0] & {
    id?: string
    title?: string
    label?: string
  }

export function openDeliveryProgramsForLane<T extends LaneProgram>(
  laneId: string,
  programs: T[],
): T[] {
  return programs.filter(p => p.lane_id === laneId && !isProgramSessionReleased(p))
}

function buildLanePredicateMap(
  programs: Array<Pick<ProgramSummary, 'lane_id'> & Parameters<typeof isProgramSessionReleased>[0]>,
  pred: (p: Parameters<typeof isProgramSessionReleased>[0]) => boolean,
): Map<string, boolean> {
  const byLane = new Map<string, Array<Parameters<typeof isProgramSessionReleased>[0]>>()
  for (const p of programs) {
    const laneId = p.lane_id?.trim()
    if (laneId == null || laneId === '') continue
    const list = byLane.get(laneId) ?? []
    list.push(p)
    byLane.set(laneId, list)
  }
  const out = new Map<string, boolean>()
  for (const [laneId, list] of byLane) {
    out.set(laneId, list.every(pred))
  }
  return out
}

/** Per-lane AND of sessionReleased (Active Session / Briefing Doing). */
export function buildLaneProgramsSessionReleasedMap(
  programs: Array<Pick<ProgramSummary, 'lane_id'> & Parameters<typeof isProgramSessionReleased>[0]>,
): Map<string, boolean> {
  return buildLanePredicateMap(programs, isProgramSessionReleased)
}

/** Per-lane AND of catalogComplete (Delivery Board Complete). */
export function buildLaneProgramsCatalogCompleteMap(
  programs: Array<Pick<ProgramSummary, 'lane_id'> & Parameters<typeof isProgramCatalogComplete>[0]>,
): Map<string, boolean> {
  return buildLanePredicateMap(programs, isProgramCatalogComplete)
}

/** @deprecated Use buildLaneProgramsSessionReleasedMap for Session; catalog map for Board. */
export function buildLaneProgramsClosedMap(
  programs: Array<Pick<ProgramSummary, 'lane_id'> & Parameters<typeof isProgramSessionReleased>[0]>,
): Map<string, boolean> {
  return buildLaneProgramsSessionReleasedMap(programs)
}

/** `undefined` while the programs board is still loading. Missing lane → no programs → released. */
export function programsReleasedForLane(
  laneId: string,
  releasedByLane: Map<string, boolean> | undefined,
): boolean | undefined {
  if (releasedByLane == null) return undefined
  return releasedByLane.get(laneId) ?? true
}

/** Alias of programsReleasedForLane (sessionReleased map). */
export function programsClosedForLane(
  laneId: string,
  closedByLane: Map<string, boolean> | undefined,
): boolean | undefined {
  return programsReleasedForLane(laneId, closedByLane)
}

function queueHasActiveWork(queue: QueueItem[]): boolean {
  return queue.some(
    q =>
      q.status === 'in_progress' ||
      q.status === 'next' ||
      q.status === 'ready_for_signoff' ||
      q.status === 'issue',
  )
}

function queueAllDone(queue: QueueItem[]): boolean {
  return queue.length > 0 && queue.every(q => q.status === 'done' || q.status === 'closed')
}

/**
 * Board map not loaded yet — all-done lanes must not count as Doing or Archive.
 * Callers skip these until `programsReady`.
 */
export function isLaneLifecycleHold(
  queue: QueueItem[],
  programsReleased: boolean | undefined,
): boolean {
  return programsReleased === undefined && queueAllDone(queue) && !queueHasActiveWork(queue)
}

function queueHasOperationalWork(queue: QueueItem[]): boolean {
  return queue.some(
    q => q.status === 'in_progress' || q.status === 'next' || q.status === 'issue',
  )
}

/** Classify a lane queue the same way TrackLaneSection does. */
export function laneLifecycleFromQueue(
  queue: QueueItem[],
  opts?: { programsReleased?: boolean; programsClosed?: boolean },
): LaneLifecycle {
  if (queue.length === 0) return 'empty'
  const released = opts?.programsReleased ?? opts?.programsClosed
  // D3: sessionReleased leaves In Flight. Catalog `ready_for_signoff` is stale
  // after Owner sign-off + no_handoff; do not keep a Closed session in Doing.
  if (released === true && !queueHasOperationalWork(queue)) return 'complete'
  if (queueHasActiveWork(queue)) return 'active'
  if (queueAllDone(queue)) {
    if (released === true) return 'complete'
    // Known not-released → Doing. Unknown → caller must use isLaneLifecycleHold.
    return 'active'
  }
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
  laneQueues: Array<{ label: string; queue: QueueItem[]; laneId?: string }>,
  opts?: {
    programsReleasedByLane?: Map<string, boolean>
    programsClosedByLane?: Map<string, boolean>
  },
): ScopeWorkSummary {
  const laneCounts = { doing: 0, planned: 0, ready: 0, done: 0 }
  let doneItems = 0
  let totalItems = 0
  let nextStep: string | null = null
  const releasedMap = opts?.programsReleasedByLane ?? opts?.programsClosedByLane

  for (const { label, queue, laneId } of laneQueues) {
    const programsReleased =
      laneId != null ? programsReleasedForLane(laneId, releasedMap) : undefined
    if (isLaneLifecycleHold(queue, programsReleased)) continue
    const life = laneLifecycleFromQueue(queue, { programsReleased })
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
      } else if (life === 'active' && programsReleased === false) {
        nextStep = `${label}: Program sign-off`
      } else {
        const plannedItem = queue.find(q => q.status === 'pending' || q.status === 'blocked')
        if (plannedItem != null) nextStep = `${label}: ${plannedItem.label}`
      }
    }
  }

  const considered =
    laneCounts.doing + laneCounts.planned + laneCounts.ready + laneCounts.done
  let status: BriefingWorkStatus
  if (laneQueues.length === 0 || considered === 0) {
    status = 'ready'
  } else if (laneCounts.doing > 0) {
    status = 'doing'
  } else if (laneCounts.planned > 0) {
    status = 'planned'
  } else if (laneCounts.done === considered) {
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
