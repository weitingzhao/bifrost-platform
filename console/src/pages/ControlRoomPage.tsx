import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { AuditPageLink } from '@/components/AuditPageLink'
import { ActiveAgentJobsStrip } from '@/components/control-room/ActiveAgentJobsStrip'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import { CommandIntentStrip } from '@/components/control-room/CommandIntentStrip'
import { MissionTimelinePanel } from '@/components/control-room/MissionTimelinePanel'
import { NetworkHealthPanel } from '@/components/control-room/NetworkHealthPanel'
import { PromoteCutoverStrip } from '@/components/control-room/PromoteCutoverStrip'
import { MissionControlHeader } from '@/components/control-room/MissionControlHeader'
import { MissionVerifyBanner } from '@/components/control-room/MissionVerifyBanner'
import { ProgramContextSection } from '@/components/control-room/ProgramContextSection'
import { ReleaseAgentCallout } from '@/components/control-room/ReleaseAgentCallout'
import { RocketSubsystemsGrid } from '@/components/control-room/RocketSubsystemsGrid'
import { WorkTracksStrip } from '@/components/control-room/WorkTracksStrip'
import {
  DualFlywheelPanel,
  type ControlRoomSelection,
} from '@/components/control-room/DualFlywheelPanel'
import { PipelineFlow } from '@/components/control-room/PipelineFlow'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { OpsSection } from '@/components/layout/OpsSection'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useMissionVerification } from '@/hooks/useMissionVerification'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { computeAllTracks } from '@/lib/briefing/workTracks'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import {
  buildPromoteCutoverModel,
  stashPromotePreflightPack,
} from '@/lib/control-room/promoteCutover'
import { useCallback, useMemo, useState } from 'react'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'

type ControlRoomPageProps = {
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
  stgGate?: import('@/api/types').ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: import('@/api/types').TierBStatusResponse
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenDelivery: () => void
  onOpenCluster: () => void
  onOpenAudit: () => void
  onOpenBriefing: (opts?: BriefingUrlState) => void
  onOpenAgentDesk?: (arg?: string | { prefill: string }) => void
  onOpenPlatformRelease?: () => void
  onOpenPromote?: () => void
  onOpenAgentProtocol?: () => void
  onOpenNetwork?: () => void
} & AmbientAgentShellProps

export function ControlRoomPage({
  context,
  contextLoading,
  matrices,
  matrixLoading,
  matrixError,
  clusterSummary,
  stgSmoke,
  stgGate,
  lastDeliverSucceeded = false,
  tierB,
  onOpenRuntimeMap,
  onOpenDelivery,
  onOpenCluster,
  onOpenAudit,
  onOpenBriefing,
  onOpenAgentDesk,
  onOpenPlatformRelease,
  onOpenPromote,
  onOpenAgentProtocol,
  onOpenNetwork,
  ambientJobId,
  onStartAgentJob,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)
  const { snapshot, matrices: liveMatrices, dataUpdatedAt, isLoading: missionLoading } = useMissionSnapshot()
  const { banner, dismissBanner, pendingVerify } = useMissionVerification()
  const { canOperate } = usePlatformAuth()

  const aiRelease = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLATFORM_RELEASE_SCOPE,
    label: scopeToLabel(PLATFORM_RELEASE_SCOPE),
    buildRequest: () => {
      const spineNote =
        context?.focus?.headline != null ? `Spine focus: ${context.focus.headline}\n\n` : ''
      return { prompt: `${spineNote}${PLATFORM_RELEASE_AGENT_PROMPT}` }
    },
  })

  const dispatchReleaseAgent = () => {
    if (!canOperate) return
    aiRelease.trigger()
  }

  const trackSummaries = useMemo(() => {
    const clusterFailingPods = clusterSummary?.failing_pods
    const clusterReach = clusterSummary?.reachability
    return computeAllTracks(context, matrices, clusterFailingPods, clusterReach)
  }, [context, matrices, clusterSummary])

  const openAgentDeskPrefill = (opts?: { prefill: string }) => {
    if (opts?.prefill != null) onOpenAgentDesk?.({ prefill: opts.prefill })
    else onOpenAgentDesk?.()
  }

  const matrixList = liveMatrices.length > 0 ? liveMatrices : matrices

  const handleOpenPromotePreflight = useCallback(() => {
    if (context != null) {
      const pack = buildPromoteCutoverModel({
        context,
        matrices: matrixList,
        stgSmoke,
        lastDeliverSucceeded,
        tierB,
      }).preflightPack
      stashPromotePreflightPack(pack)
    }
    onOpenPromote?.()
  }, [context, matrixList, stgSmoke, lastDeliverSucceeded, tierB, onOpenPromote])

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
      <section className="control-room-diagnosis flex flex-col gap-4" aria-label="Mission diagnosis">
        {banner != null && (
          <MissionVerifyBanner
            state={banner}
            onDismiss={dismissBanner}
            onOpenJob={jobId => onOpenAgentDesk?.(jobId)}
          />
        )}

        {pendingVerify && banner == null && (
          <p className="control-room-verify-pending m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Agent run finished — refreshing mission probes and verify_mission_snapshot…
          </p>
        )}

        <MissionControlHeader
          snapshot={snapshot}
          matrices={matrixList}
          context={context}
          dataUpdatedAt={dataUpdatedAt}
          showRocketSubsystems={false}
          onOpenRuntimeMap={onOpenRuntimeMap}
          onOpenCluster={onOpenCluster}
          onOpenDelivery={onOpenDelivery}
          onOpenPlatformRelease={onOpenPlatformRelease ?? onOpenDelivery}
          onOpenAgentDesk={openAgentDeskPrefill}
          onOpenPromote={handleOpenPromotePreflight}
        />

        <ReleaseAgentCallout
          release={snapshot.release}
          onDispatch={dispatchReleaseAgent}
          pending={aiRelease.isPending}
          canDispatch={!aiRelease.disabled}
          disabledReason={aiRelease.disabledReason}
        />

        {!canOperate && snapshot.release.signal !== 'ok' && (
          <OpsFeedback variant="warning" title="Authenticate as operator to run AI Release">
            Use the header auth control before starting release-scoped Agent tasks.
          </OpsFeedback>
        )}

        {aiRelease.error != null && (
          <OpsFeedback variant="error" title="Failed to start AI Release">
            {aiRelease.error.message}
          </OpsFeedback>
        )}

        <ActiveAgentJobsStrip
          onOpenAgentDesk={jobId => onOpenAgentDesk?.(jobId)}
          onOpenAudit={onOpenAudit}
        />

        <CommandIntentStrip
          snapshot={snapshot}
          matrices={matrixList}
          context={context}
          onOpenAgentDesk={openAgentDeskPrefill}
          onDispatchReleaseAgent={dispatchReleaseAgent}
          onOpenBriefing={onOpenBriefing}
          onOpenDelivery={onOpenDelivery}
          onOpenPromote={handleOpenPromotePreflight}
        />

        <MissionTimelinePanel
          snapshot={snapshot}
          probeObservedAt={dataUpdatedAt}
          onOpenAudit={onOpenAudit}
          onOpenAgentDesk={jobId => onOpenAgentDesk?.(jobId)}
        />

        <PromoteCutoverStrip
          context={context}
          matrices={matrixList}
          stgSmoke={stgSmoke}
          stgGate={stgGate}
          lastDeliverSucceeded={lastDeliverSucceeded}
          tierB={tierB}
          onOpenPromote={handleOpenPromotePreflight}
          onOpenDelivery={onOpenDelivery}
        />

        {onOpenAgentProtocol != null && (
          <NetworkHealthPanel
            context={context}
            onOpenAgentProtocol={onOpenAgentProtocol}
            onOpenNetwork={onOpenNetwork}
          />
        )}
      </section>

      <ProgramContextSection>
        <div className="flex flex-col gap-4">
          <OpsSection
            title="Rocket — Ops Platform subsystems"
            description="Launch vehicle health — drill into Infra, Release, Control, or Agent."
            bodyPadding="compact"
            overflow="visible"
          >
            <RocketSubsystemsGrid
              snapshot={snapshot}
              onOpenCluster={onOpenCluster}
              onOpenDelivery={onOpenDelivery}
              onOpenPlatformRelease={onOpenPlatformRelease ?? onOpenDelivery}
              onOpenAgentDesk={() => onOpenAgentDesk?.()}
              onDispatchReleaseAgent={dispatchReleaseAgent}
              releaseDispatchPending={aiRelease.isPending}
              canDispatchRelease={!aiRelease.disabled}
            />
          </OpsSection>

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
            onOpenAgentDesk={() => onOpenAgentDesk?.()}
          />
        </div>
      </ProgramContextSection>

      <BayDetailDrawer
        selection={selection}
        context={context}
        matrices={matrices}
        onClose={() => setSelection(null)}
        onOpenRuntimeMap={onOpenRuntimeMap}
      />
    </div>
  )
}
