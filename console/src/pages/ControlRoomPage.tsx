import type { ClusterSummary, MatrixResponse, OpsContextResponse, StgSmokeResponse } from '@/api/types'
import { AuditPageLink } from '@/components/AuditPageLink'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import { ControlRoomLiveStatus } from '@/components/control-room/ControlRoomLiveStatus'
import { WorkTracksStrip } from '@/components/control-room/WorkTracksStrip'
import {
  DualFlywheelPanel,
  type ControlRoomSelection,
} from '@/components/control-room/DualFlywheelPanel'
import { PipelineFlow } from '@/components/control-room/PipelineFlow'
import { OpsSection } from '@/components/layout/OpsSection'
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
  clusterSummary?: ClusterSummary
  clusterLoading?: boolean
  stgSmoke?: StgSmokeResponse
  stgSmokeLoading?: boolean
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenProgram: () => void
  onOpenDelivery: () => void
  onOpenCluster: () => void
  onOpenAudit: () => void
  onOpenBriefing: () => void
  onOpenAgentDesk?: () => void
}

export function ControlRoomPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  matrixError,
  platformHealthy,
  clusterSummary,
  clusterLoading,
  stgSmoke,
  stgSmokeLoading,
  onOpenRuntimeMap,
  onOpenProgram,
  onOpenDelivery,
  onOpenCluster,
  onOpenAudit,
  onOpenBriefing,
  onOpenAgentDesk,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)

  const trackSummaries = useMemo(() => {
    const clusterFailingPods = clusterSummary?.failing_pods
    const clusterReach = clusterSummary?.reachability
    return computeAllTracks(context, matrices, clusterFailingPods, clusterReach)
  }, [context, matrices, clusterSummary])

  if (contextLoading || matrixLoading) {
    return <p className="text-[var(--muted-foreground)]">Loading control room…</p>
  }

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-4">
      <ControlRoomLiveStatus
        context={context}
        contextLoading={contextLoading}
        matrices={matrices}
        matrixLoading={matrixLoading}
        matrixError={matrixError}
        platformHealthy={platformHealthy}
        clusterSummary={clusterSummary}
        clusterLoading={clusterLoading}
        stgSmoke={stgSmoke}
        stgSmokeLoading={stgSmokeLoading}
        onOpenRuntimeMap={onOpenRuntimeMap}
        onOpenProgram={onOpenProgram}
        onOpenCluster={onOpenCluster}
        onOpenDelivery={onOpenDelivery}
      />

      <WorkTracksStrip tracks={trackSummaries} onOpenBriefing={onOpenBriefing} />

      <OpsSection
        title="Dual flywheel governance"
        description={
          <>
            Program milestone spine, bay lamps, and Agent context packs. CI/CD path diagram lives on{' '}
            <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
              Delivery
            </button>
            . Read-only probes — no write actions (L0).
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
