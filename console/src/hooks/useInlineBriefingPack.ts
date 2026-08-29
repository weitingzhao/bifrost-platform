import { useCallback, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import type { ClusterObservabilityResponse, ClusterSummary } from '@/api/clusterTypes'
import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { fetchClusterObservability } from '@/api/cluster'
import { buildBriefingPack } from '@/lib/briefing/buildBriefingPack'
import { ensureSessionForPack } from '@/lib/briefing/ensureSessionForPack'
import { saveBriefingActiveSession } from '@/lib/briefing/briefingActiveSession'
import { useBriefingActiveSessionLive } from '@/hooks/useBriefingActiveSessionLive'
import {
  buildQueueForLane,
  laneById,
  type LaneId,
  type QueueItem,
  type WorkLane,
} from '@/lib/briefing/workLanes'
import { componentLineForTaskMode } from '@/lib/briefing/briefingViewTabs'
import { computeAllTracks, type TrackId } from '@/lib/briefing/workTracks'
import type { WorkIntent } from '@/lib/briefing/workIntents'
import type { TaskModeDef } from '@/lib/task-mode/types'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { useDeliveryProgramClosure } from '@/hooks/useDeliveryProgramClosure'

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

export type InlineBriefingPackResult = {
  pack: string
  isReady: boolean
  copyToClipboard: () => Promise<boolean>
  copied: boolean
  /** Set when Copy failed (e.g. session archive POST). */
  copyError: string | null

  /** Track scoped to this Build mode. */
  track: TrackId | null
  /** Active Session lane (TCC does not offer sibling-lane switching). */
  selectedLaneId: LaneId | null
  /** Full ordered queue for the session lane. */
  laneQueue: QueueItem[]
  /** Session lane metadata. */
  selectedLane: WorkLane | null
  /** Resolved intent for the session lane. */
  intent: WorkIntent | null
}

/**
 * Builds a scoped compact briefing pack for the Active Session lane.
 * TCC follows the session — change lane in Agent Briefing, not here.
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
  // Active Session is authoritative for component line / lane when present.
  const activeSession = useBriefingActiveSessionLive()
  const sessionLaneMeta =
    activeSession?.lane != null && activeSession.lane !== ''
      ? laneById(activeSession.lane)
      : null
  const packScope = sessionLaneMeta?.componentLine ?? componentLineForTaskMode(mode.id)
  const trackId: TrackId | null =
    sessionLaneMeta?.track ?? dev?.briefingTrack ?? null
  const selectedLaneId: LaneId | null =
    activeSession?.lane != null && activeSession.lane !== '' ? activeSession.lane : null

  const [copied, setCopied] = useState(false)
  const [copyError, setCopyError] = useState<string | null>(null)
  const operateQueueQ = useOperateQueue()
  const { programs } = useDeliveryProgramClosure()

  const observabilityQ = useQuery({
    queryKey: ['cluster', 'observability'],
    queryFn: fetchClusterObservability,
    refetchInterval: 30_000,
    enabled: enabled && observabilityProp == null && dev != null,
  })
  const clusterObservability = observabilityProp ?? observabilityQ.data

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
    return buildQueueForLane(selectedLaneId, context, matrices, clusterSummary, programs)
  }, [selectedLaneId, context, matrices, clusterSummary, programs])
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
  const linkedPhaseId = resolvedProgramId != null && resolvedProgramId !== '' ? 'briefing' : undefined

  const pack = useMemo(() => {
    if (trackId == null || selectedLaneId == null || intent == null) return ''
    return buildBriefingPack({
      intent,
      packSize: 'compact',
      sessionDelta: null,
      trackSummaries,
      selectedTrack: trackId,
      selectedLane: selectedLaneId,
      selectedScope: packScope,
      laneQueue,
      taskModeContext: {
        modeId: mode.id,
        modeLabel: mode.label,
        loopArchetype: mode.loopArchetype,
        programId: resolvedProgramId,
      },
      programId: resolvedProgramId,
      phaseId: linkedPhaseId,
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
    packScope,
    mode.id,
    mode.label,
    mode.loopArchetype,
    resolvedProgramId,
    linkedPhaseId,
    context,
    matrices,
    clusterSummary,
    clusterObservability,
    platformHealthy,
  ])

  const isReady = trackId != null && selectedLaneId != null && pack.length > 0

  const copyToClipboard = useCallback(async (): Promise<boolean> => {
    if (!isReady || trackId == null || selectedLaneId == null || intent == null) return false
    setCopyError(null)
    try {
      const { pack: anchored } = await ensureSessionForPack({
        programId: resolvedProgramId,
        phaseId: linkedPhaseId,
        laneId: selectedLaneId,
        buildPack: sessionId =>
          buildBriefingPack({
            intent,
            packSize: 'compact',
            sessionDelta: null,
            trackSummaries,
            selectedTrack: trackId,
            selectedLane: selectedLaneId,
            selectedScope: packScope,
            laneQueue,
            taskModeContext: {
              modeId: mode.id,
              modeLabel: mode.label,
              loopArchetype: mode.loopArchetype,
              programId: resolvedProgramId,
            },
            sessionId,
            programId: resolvedProgramId,
            phaseId: linkedPhaseId,
            context,
            matrices,
            clusterSummary,
            clusterObservability,
            platformHealthy,
          }),
      })
      try {
        await navigator.clipboard.writeText(anchored)
      } catch {
        const ta = document.createElement('textarea')
        ta.value = anchored
        ta.setAttribute('readonly', '')
        ta.style.position = 'fixed'
        ta.style.left = '-9999px'
        document.body.appendChild(ta)
        ta.select()
        const ok = document.execCommand('copy')
        document.body.removeChild(ta)
        if (!ok) return false
      }
      saveBriefingActiveSession({
        track: trackId,
        lane: selectedLaneId,
        intent,
        packSize: 'compact',
        startedAt: new Date().toISOString(),
        programId: resolvedProgramId,
      })
      setCopied(true)
      window.setTimeout(() => setCopied(false), 2000)
      return true
    } catch (err) {
      setCopyError(err instanceof Error ? err.message : 'Copy session failed')
      return false
    }
  }, [
    isReady,
    trackId,
    selectedLaneId,
    intent,
    resolvedProgramId,
    linkedPhaseId,
    trackSummaries,
    laneQueue,
    packScope,
    mode.id,
    mode.label,
    mode.loopArchetype,
    context,
    matrices,
    clusterSummary,
    clusterObservability,
    platformHealthy,
  ])

  return {
    pack,
    isReady,
    copyToClipboard,
    copied,
    copyError,
    track: trackId,
    selectedLaneId,
    laneQueue,
    selectedLane: selectedLaneMeta,
    intent,
  }
}
