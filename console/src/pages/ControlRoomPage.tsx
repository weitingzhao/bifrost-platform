import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { AuditPageLink } from '@/components/AuditPageLink'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import { MissionControlHeader } from '@/components/control-room/MissionControlHeader'
import { WorkTracksStrip } from '@/components/control-room/WorkTracksStrip'
import {
  DualFlywheelPanel,
  type ControlRoomSelection,
} from '@/components/control-room/DualFlywheelPanel'
import { PipelineFlow } from '@/components/control-room/PipelineFlow'
import { OpsSection } from '@/components/layout/OpsSection'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { computeAllTracks } from '@/lib/briefing/workTracks'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import { useMemo, useState } from 'react'

interface ControlRoomPageProps {
  context: OpsContextResponse | undefined
  contextLoading: boolean
  matrices: MatrixResponse[]
  matrixLoading: boolean
  matrixError: Error | null
  platformHealthy: boolean
  clusterSummary?: import('@/api/types').ClusterSummary
  clusterLoading?: boolean
  stgSmoke?: import('@/api/types').StgSmokeResponse
  stgSmokeLoading?: boolean
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenProgram: () => void
  onOpenDelivery: () => void
  onOpenCluster: () => void
  onOpenAudit: () => void
  onOpenBriefing: () => void
  onOpenAgentDesk?: (opts?: { prefill: string }) => void
  onOpenPlatformRelease?: () => void
}

export function ControlRoomPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  matrixError,
  clusterSummary,
  onOpenRuntimeMap,
  onOpenProgram,
  onOpenDelivery,
  onOpenCluster,
  onOpenAudit,
  onOpenBriefing,
  onOpenAgentDesk,
  onOpenPlatformRelease,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)
  const { snapshot, matrices: liveMatrices, dataUpdatedAt, isLoading: missionLoading } = useMissionSnapshot()

  const trackSummaries = useMemo(() => {
    const clusterFailingPods = clusterSummary?.failing_pods
    const clusterReach = clusterSummary?.reachability
    return computeAllTracks(context, matrices, clusterFailingPods, clusterReach)
  }, [context, matrices, clusterSummary])

  if (contextLoading || matrixLoading || missionLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading mission control…</p>
  }

  if (matrixError != null) {
    return (
      <p className="lamp-fail">
        Failed to load matrix: {matrixError.message}
      </p>
    )
  }

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-4">
      <MissionControlHeader
        snapshot={snapshot}
        matrices={liveMatrices.length > 0 ? liveMatrices : matrices}
        context={context}
        dataUpdatedAt={dataUpdatedAt}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onOpenCluster={onOpenCluster}
        onOpenDelivery={onOpenDelivery}
        onOpenPlatformRelease={onOpenPlatformRelease ?? onOpenDelivery}
        onOpenAgentDesk={onOpenAgentDesk ?? (() => undefined)}
      />

      <WorkTracksStrip tracks={trackSummaries} onOpenBriefing={onOpenBriefing} />

      <OpsSection
        title="Dual flywheel governance"
        description={
          <>
            Flywheel A (product iteration) ↔ Coupling (release gate) ↔ Flywheel B (runtime stability).
            Ops Platform is the rocket; Trade is the payload. CI/CD path diagram lives on{' '}
            <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
              Delivery
            </button>
            .
          </>
        }
        headerExtra={<AuditPageLink onOpenAudit={onOpenAudit} className="mt-2" />}
        overflow="visible"
      />

      <DualFlywheelPanel
        context={context}
        matrices={matrices}
        selection={selection}
        onSelectBay={id => setSelection({ kind: 'bay', id })}
        onOpenProgram={onOpenProgram}
        onOpenDelivery={onOpenDelivery}
      />

      {context != null && (
        <PipelineFlow
          context={context}
          selectionId={selection?.kind === 'milestone' ? selection.id : null}
          onSelectMilestone={id => setSelection({ kind: 'milestone', id })}
        />
      )}

      <AgentFocusDock
        context={context}
        matrices={matrices}
        selection={selection}
        onOpenAgentDesk={onOpenAgentDesk}
      />

      <BayDetailDrawer
        selection={selection}
        context={context}
        matrices={matrices}
        onClose={() => setSelection(null)}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onOpenProgram={onOpenProgram}
      />
    </div>
  )
}
