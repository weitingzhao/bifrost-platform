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
import { LaunchPad } from '@/components/control-room/LaunchPad'
import { SpokeSignalCards } from '@/components/control-room/SpokeSignalCards'
import { RocketSubsystemsGrid } from '@/components/control-room/RocketSubsystemsGrid'
import { OperateQueueStrip } from '@/components/control-room/OperateQueueStrip'
import { MissionSignalProgramStrip } from '@/components/control-room/MissionSignalProgramStrip'
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
import { useOperateQueue } from '@/hooks/useOperateQueue'
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
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import { fetchSupplyChain } from '@/api/platform'
import { startRemediation } from '@/api/platform'
import {
  buildTradeDeployPrompt,
  TRADE_DEPLOY_SCOPE,
} from '@/lib/agent/tradeDeployAgentPrompt'
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
  onOpenTradeDeploy?: () => void
  onOpenPromote?: () => void
  onOpenAgentProtocol?: () => void
  onOpenNetwork?: () => void
  onOpenSatelliteBus?: () => void
  onOpenCompute?: () => void
  onOpenDefects?: () => void
  onOpenAgentDeskTab?: () => void
  onOpenLaunchView?: (mode: 'mission-launch') => void
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
  onOpenTradeDeploy,
  onOpenPromote,
  onOpenAgentProtocol,
  onOpenNetwork,
  onOpenSatelliteBus,
  onOpenCompute,
  onOpenDefects,
  onOpenAgentDeskTab,
  onOpenLaunchView,
  ambientJobId,
  onStartAgentJob,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)
  const { snapshot, matrices: liveMatrices, dataUpdatedAt, isLoading: missionLoading } = useMissionSnapshot()
  const { banner, dismissBanner, pendingVerify } = useMissionVerification()
  const { canOperate } = usePlatformAuth()
  const operateQueueQuery = useOperateQueue()
  const matrixList = liveMatrices.length > 0 ? liveMatrices : matrices

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

  const aiTradeDeploy = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: TRADE_DEPLOY_SCOPE,
    label: scopeToLabel(TRADE_DEPLOY_SCOPE),
    buildRequest: () => ({
      prompt: buildTradeDeployPrompt({
        matrices: matrixList,
        stgSmoke,
        tierB,
      }),
    }),
  })

  const dispatchReleaseAgent = () => {
    if (!canOperate) return
    aiRelease.trigger()
  }

  const dispatchTradeDeployAgent = () => {
    if (!canOperate) return
    aiTradeDeploy.trigger()
  }

  const qc = useQueryClient()
  const playbookFixMutation = useMutation({
    mutationFn: ({ scope, prompt }: { scope: string; prompt: string }) =>
      startRemediation({ scope, prompt }),
    onSuccess: (job, vars) => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      onStartAgentJob?.({ id: job.id, scope: vars.scope, label: scopeToLabel(vars.scope) })
    },
  })

  const handlePlaybookFix = useCallback(
    async ({ scope, prompt }: { scope: string; prompt: string }) => {
      if (!canOperate) return
      if (scope === DELIVER_STG_RECOVER_SCOPE) {
        const supply = await fetchSupplyChain()
        playbookFixMutation.mutate({
          scope,
          prompt: buildDeliverStgRecoverPrompt({ supply, stgSmoke }),
        })
        return
      }
      playbookFixMutation.mutate({ scope, prompt })
    },
    [canOperate, playbookFixMutation, stgSmoke],
  )

  const trackSummaries = useMemo(() => {
    const clusterFailingPods = clusterSummary?.failing_pods
    const clusterReach = clusterSummary?.reachability
    return computeAllTracks(
      context,
      matrices,
      clusterFailingPods,
      clusterReach,
      operateQueueQuery.data?.open,
    )
  }, [context, matrices, clusterSummary, operateQueueQuery.data?.open])

  const openAgentDeskPrefill = (opts?: { prefill: string }) => {
    if (opts?.prefill != null) onOpenAgentDesk?.({ prefill: opts.prefill })
    else onOpenAgentDesk?.()
  }

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
          onOpenLaunchView={mode => onOpenLaunchView?.(mode)}
          onOpenPromote={handleOpenPromotePreflight}
          onPlaybookFix={handlePlaybookFix}
          playbookFixPending={playbookFixMutation.isPending}
          canOperate={canOperate}
        />

        <LaunchPad
          onDispatchRelease={dispatchReleaseAgent}
          onDispatchTradeDeploy={dispatchTradeDeployAgent}
          releasePending={aiRelease.isPending}
          tradeDeployPending={aiTradeDeploy.isPending}
          canDispatchRelease={!aiRelease.disabled}
          canDispatchTradeDeploy={!aiTradeDeploy.disabled}
          releaseDisabledReason={aiRelease.disabledReason}
          tradeDeployDisabledReason={aiTradeDeploy.disabledReason}
          onOpenPlatformRelease={onOpenPlatformRelease ?? onOpenDelivery}
          onOpenTradeDeploy={onOpenTradeDeploy ?? onOpenDelivery}
        />

        {!canOperate && (snapshot.release.signal !== 'ok' || snapshot.payloadOverall !== 'ok') && (
          <OpsFeedback variant="warning" title="Authenticate as operator to run Launch Pad agents">
            Use the header auth control before starting release or trade-deploy Agent tasks.
          </OpsFeedback>
        )}

        {aiRelease.error != null && (
          <OpsFeedback variant="error" title="Failed to start AI Release">
            {aiRelease.error.message}
          </OpsFeedback>
        )}

        {aiTradeDeploy.error != null && (
          <OpsFeedback variant="error" title="Failed to start Deploy Satellite agent">
            {aiTradeDeploy.error.message}
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

        <OperateQueueStrip />

        <MissionSignalProgramStrip onOpenDelivery={onOpenDelivery} />

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

          {onOpenSatelliteBus != null &&
            onOpenNetwork != null &&
            onOpenCompute != null &&
            onOpenDefects != null &&
            onOpenAgentDeskTab != null && (
            <OpsSection
              title="Spokes — Satellite · Ground Systems · Engineer"
              description="Hub drill-down into payload bus, infrastructure, and Agent loop health."
              bodyPadding="compact"
              overflow="visible"
            >
              <SpokeSignalCards
                onOpenSatelliteBus={onOpenSatelliteBus}
                onOpenNetwork={onOpenNetwork}
                onOpenCompute={onOpenCompute}
                onOpenAgentDesk={onOpenAgentDeskTab}
                onOpenDefects={onOpenDefects}
              />
            </OpsSection>
          )}

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
