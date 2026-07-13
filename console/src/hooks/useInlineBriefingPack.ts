import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type {
  ClusterObservabilityResponse,
  ClusterSummary,
  MatrixResponse,
  OpsContextResponse,
} from '@/api/types'
import { fetchClusterObservability } from '@/api/platform'
import { buildBriefingPack } from '@/lib/briefing/buildBriefingPack'
import {
  buildQueueForLane,
  laneById,
  lanesForLineTrack,
  queueProgress,
  type ComponentLineId,
  type LaneId,
  type QueueItem,
  type WorkLane,
  type WorkTrackType,
} from '@/lib/briefing/workLanes'
import {
  componentLineForTaskMode,
  defaultLaneForLineTrack,
  trackTypeForTaskMode,
} from '@/lib/briefing/briefingViewTabs'
import { computeAllTracks, type TrackId } from '@/lib/briefing/workTracks'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import { splitQueueByCompletion } from '@/lib/briefing/queueDisplay'
import type { TaskModeDef } from '@/lib/task-mode/types'
import { useOperateQueue } from '@/hooks/useOperateQueue'

export type UseInlineBriefingPackArgs = {
  mode: TaskModeDef
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  clusterSummary?: ClusterSummary
  clusterObservability?: ClusterObservabilityResponse
  platformHealthy?: boolean
  programId?: string
  enabled?: boolean
}

export type LaneOption = {
  lane: WorkLane
  progress: { done: number; total: number; percent: number } | null
}

export type InlineBriefingPackResult = {
  pack: string
  isReady: boolean
  copyToClipboard: () => Promise<boolean>
  copied: boolean

  /** Track scoped to this Build mode. */
  track: TrackId | null
  /** All lanes under the scoped track — for Lane selector UI. */
  laneOptions: LaneOption[]
  /** Currently selected lane. */
  selectedLaneId: LaneId | null
  /** Switch lane — user-driven selection. */
  selectLane: (id: LaneId) => void
  /** Active queue items for the selected lane. */
  activeQueue: QueueItem[]
  /** Completed queue items for the selected lane. */
  completedQueue: QueueItem[]
  /** Selected lane metadata. */
  selectedLane: WorkLane | null
  /** Resolved intent for the selected lane. */
  intent: WorkIntent | null
}

/**
 * Builds a scoped compact briefing pack for the active Dev task mode.
 * Exposes lane selection so the user can pick which sub-task to work on
 * without leaving Task CC.
 */
export function useInlineBriefingPack({
  mode,
  context,
  matrices = [],
  clusterSummary,
  clusterObservability: observabilityProp,
  platformHealthy,
  programId,
  enabled = true,
}: UseInlineBriefingPackArgs): InlineBriefingPackResult {
  const dev = mode.dev
  const componentLine: ComponentLineId | null =
    dev?.briefingComponentLine ?? (dev != null ? componentLineForTaskMode(mode.id) : null)
  const trackType: WorkTrackType | null =
    dev?.briefingTrackType ?? (dev != null ? trackTypeForTaskMode(mode.id) : null)
  const trackId: TrackId | null = dev?.briefingTrack ?? null
  const defaultLane: LaneId | null =
    dev?.briefingLane ??
    (componentLine != null && trackType != null
      ? defaultLaneForLineTrack(componentLine, trackType)
      : null)

  const [userSelectedLane, setUserSelectedLane] = useState<LaneId | null>(null)
  const selectedLaneId = userSelectedLane ?? defaultLane
  const [copied, setCopied] = useState(false)
  const operateQueueQ = useOperateQueue()

  const observabilityQ = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    enabled: enabled && observabilityProp == null && dev != null,
  })
  const clusterObservability = observabilityProp ?? observabilityQ.data

  const lanes = useMemo(
    () =>
      componentLine != null && trackType != null
        ? lanesForLineTrack(componentLine, trackType)
        : [],
    [componentLine, trackType],
  )

  const laneOptions = useMemo((): LaneOption[] => {
    return lanes.map(lane => {
      const q = buildQueueForLane(lane.id, context, matrices, clusterSummary)
      return { lane, progress: queueProgress(q) }
    })
  }, [lanes, context, matrices, clusterSummary])

  const selectedLaneMeta = useMemo(
    () => (selectedLaneId != null ? laneById(selectedLaneId) : null),
    [selectedLaneId],
  )

  const intent: WorkIntent | null = useMemo(() => {
    if (selectedLaneMeta != null) return selectedLaneMeta.workIntent
    return dev?.briefingIntent ?? null
  }, [selectedLaneMeta, dev?.briefingIntent])

  const laneQueue = useMemo(() => {
    if (selectedLaneId == null) return []
    return buildQueueForLane(selectedLaneId, context, matrices, clusterSummary)
  }, [selectedLaneId, context, matrices, clusterSummary])

  const { active: activeQueue, completed: completedQueue } = useMemo(
    () => splitQueueByCompletion(laneQueue),
    [laneQueue],
  )

  const trackSummaries = useMemo(
    () =>
      computeAllTracks(
        context,
        matrices,
        clusterSummary?.failing_pods,
        clusterSummary?.reachability,
        operateQueueQ.data?.open,
      ),
    [context, matrices, clusterSummary, operateQueueQ.data?.open],
  )

  const resolvedProgramId = programId ?? dev?.programId

  const pack = useMemo(() => {
    if (trackId == null || selectedLaneId == null || intent == null) return ''
    return buildBriefingPack({
      intent,
      packSize: 'compact',
      sessionDelta: null,
      trackSummaries,
      selectedTrack: trackId,
      selectedLane: selectedLaneId,
      laneQueue,
      taskModeContext: {
        modeId: mode.id,
        modeLabel: mode.label,
        loopArchetype: mode.loopArchetype,
        programId: resolvedProgramId,
      },
      context,
      matrices,
      clusterSummary,
      clusterObservability,
      platformHealthy,
    })
  }, [
    trackId,
    selectedLaneId,
    intent,
    trackSummaries,
    laneQueue,
    mode.id,
    mode.label,
    mode.loopArchetype,
    resolvedProgramId,
    context,
    matrices,
    clusterSummary,
    clusterObservability,
    platformHealthy,
  ])

  const isReady = trackId != null && selectedLaneId != null && pack.length > 0

  const copyToClipboard = useCallback(async (): Promise<boolean> => {
    if (!isReady) return false
    try {
      await navigator.clipboard.writeText(pack)
    } catch {
      try {
        const ta = document.createElement('textarea')
        ta.value = pack
        ta.setAttribute('readonly', '')
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.select()
        const ok = document.execCommand('copy')
        document.body.removeChild(ta)
        if (!ok) return false
      } catch {
        return false
      }
    }
    setCopied(true)
    window.setTimeout(() => setCopied(false), 2000)
    return true
  }, [isReady, pack])

  return {
    pack,
    isReady,
    copyToClipboard,
    copied,
    track: trackId,
    laneOptions,
    selectedLaneId,
    selectLane: setUserSelectedLane,
    activeQueue,
    completedQueue,
    selectedLane: selectedLaneMeta,
    intent,
  }
}
