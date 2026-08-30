/**
 * Pure Build Desk nav counts — Briefing Ready lanes / In Flight Doing lanes.
 */

import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  isLaneLifecycleHold,
  laneLifecycleFromQueue,
  programsReleasedForLane,
  type LaneLifecycle,
} from '@/lib/briefing/briefingStatus'
import { allWorkLanes, buildQueueForLane } from '@/lib/briefing/workLanes'

export type BuildDeskWorkloadCounts = {
  /** Ready lanes (empty queue) — same “r” as Briefing Scope r/p/d. */
  briefing: number
  /** Doing lanes — same predicate as ActiveSessionPage (In Flight badge). */
  activeSession: number
}

function countLanesWithLifecycle(
  args: {
    context: OpsContextResponse | undefined
    matrices: MatrixResponse[]
    clusterSummary: ClusterSummary | undefined
    programs: ProgramSummary[]
    releasedByLane: Map<string, boolean> | undefined
  },
  lifecycle: LaneLifecycle,
): number {
  let n = 0
  for (const lane of allWorkLanes()) {
    const queue = buildQueueForLane(
      lane.id,
      args.context,
      args.matrices,
      args.clusterSummary,
      args.programs,
    )
    const released = programsReleasedForLane(lane.id, args.releasedByLane)
    if (isLaneLifecycleHold(queue, released)) continue
    if (laneLifecycleFromQueue(queue, { programsReleased: released }) === lifecycle) n += 1
  }
  return n
}

/** Ready = empty queue (matches Briefing Scope “r”). */
export function countReadyLanes(args: {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  programs: ProgramSummary[]
  releasedByLane: Map<string, boolean> | undefined
}): number {
  return countLanesWithLifecycle(args, 'empty')
}

export function countDoingLanes(args: {
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  programs: ProgramSummary[]
  releasedByLane: Map<string, boolean> | undefined
  programsReady: boolean
}): number {
  if (!args.programsReady || args.releasedByLane == null) return 0
  return countLanesWithLifecycle(args, 'active')
}

export function computeBuildDeskWorkloadCounts(args: {
  programs: ProgramSummary[]
  context: OpsContextResponse | undefined
  matrices: MatrixResponse[]
  clusterSummary: ClusterSummary | undefined
  releasedByLane: Map<string, boolean> | undefined
  programsReady: boolean
}): BuildDeskWorkloadCounts {
  return {
    briefing: countReadyLanes(args),
    activeSession: countDoingLanes(args),
  }
}
