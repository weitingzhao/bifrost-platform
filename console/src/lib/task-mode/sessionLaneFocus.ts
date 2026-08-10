import {
  isLaneLifecycleHold,
  laneLifecycleFromQueue,
  lifecycleToBriefingStatus,
  type BriefingWorkStatus,
  type LaneLifecycle,
} from '@/lib/briefing/briefingStatus'
import { queueProgress, type QueueItem } from '@/lib/briefing/workLanes'

export type SessionLaneFocusKind = 'doing' | 'start' | 'plan' | 'signoff' | 'archive' | 'pick-session'

export type SessionLaneFocus = {
  lifecycle: LaneLifecycle
  status: BriefingWorkStatus
  progress: { done: number; total: number; percent: number } | null
  kind: SessionLaneFocusKind
  /** One-line Focus / Next for the Session lane strip. */
  line: string
  focusItem?: QueueItem
  /** Next pending item after the Doing head (optional). */
  nextItem?: QueueItem
}

const ACTIVE_STATUSES = new Set(['in_progress', 'next', 'ready_for_signoff', 'issue'])
const PLANNED_STATUSES = new Set(['pending', 'blocked'])

function firstMatching(queue: QueueItem[], statuses: Set<string>): QueueItem | undefined {
  return queue.find(q => statuses.has(q.status))
}

export type ResolveSessionLaneFocusInput = {
  queue: QueueItem[]
  hasActiveSession: boolean
  hasProgram?: boolean
  /** sessionReleased for the lane — from useDeliveryProgramClosure. */
  programsReleased?: boolean
  /** Alias of programsReleased. */
  programsClosed?: boolean
}

/**
 * TCC Build — pin Active Session lane and tell the operator what to push next.
 * Ready / Planned / Doing / Done is lane maturity (not a sibling-lane picker).
 */
export function resolveSessionLaneFocus(input: ResolveSessionLaneFocusInput): SessionLaneFocus {
  if (!input.hasActiveSession) {
    return {
      lifecycle: 'empty',
      status: 'ready',
      progress: null,
      kind: 'pick-session',
      line: 'Next: Open Briefing to pick a lane and Copy session',
    }
  }

  const programsReleased = input.programsReleased ?? input.programsClosed
  if (isLaneLifecycleHold(input.queue, programsReleased)) {
    return {
      lifecycle: 'planned',
      status: 'planned',
      progress: queueProgress(input.queue),
      kind: 'plan',
      line: 'Next: Wait for Delivery close state',
    }
  }
  const lifecycle = laneLifecycleFromQueue(input.queue, { programsReleased })
  const status = lifecycleToBriefingStatus(lifecycle)
  const progress = queueProgress(input.queue)

  if (lifecycle === 'empty') {
    return {
      lifecycle,
      status,
      progress,
      kind: 'plan',
      line: 'Next: Plan — break down work (no tasks yet)',
    }
  }

  if (lifecycle === 'complete') {
    return {
      lifecycle,
      status,
      progress,
      kind: 'archive',
      line: 'Next: Archive session in Briefing — catalog stays on Delivery',
    }
  }

  if (lifecycle === 'active') {
    const focusItem = firstMatching(input.queue, ACTIVE_STATUSES)
    const nextItem = firstMatching(input.queue, PLANNED_STATUSES)
    if (programsReleased === false && focusItem == null) {
      return {
        lifecycle,
        status,
        progress,
        kind: 'signoff',
        line: 'Focus: Program sign-off in In Flight',
      }
    }
    return {
      lifecycle,
      status,
      progress,
      kind: 'doing',
      line: focusItem != null ? `Focus: ${focusItem.label}` : 'Focus: Continue in-progress work',
      focusItem,
      nextItem,
    }
  }

  const focusItem = firstMatching(input.queue, PLANNED_STATUSES)
  return {
    lifecycle,
    status,
    progress,
    kind: 'start',
    line: focusItem != null ? `Next: Start → ${focusItem.label}` : 'Next: Start planned work',
    focusItem,
  }
}
