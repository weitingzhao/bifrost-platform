/**
 * Pure Build Desk nav counts — Briefing open programs / In Flight Doing lanes.
 */

import type { ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import type { ProgramSummary } from '@/api/programsTypes'
import {
  isLaneLifecycleHold,
  laneLifecycleFromQueue,
  programsReleasedForLane,
} from '@/lib/briefing/briefingStatus'
import { isProgramSessionReleased } from '@/lib/briefing/programClose'
import { allWorkLanes, buildQueueForLane } from '@/lib/briefing/workLanes'

export type BuildDeskWorkloadCounts = {
  /** Not-sessionReleased board programs (Briefing badge). */
  briefing: number
  /** Doing lanes — same predicate as ActiveSessionPage (In Flight badge). */
  activeSession: number
}

export function countOpenBoardPrograms(programs: ProgramSummary[]): number {
  return programs.filter(p => !isProgramSessionReleased(p)).length
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
    if (laneLifecycleFromQueue(queue, { programsReleased: released }) === 'active') n += 1
  }
  return n
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
    briefing: countOpenBoardPrograms(args.programs),
    activeSession: countDoingLanes(args),
  }
}
