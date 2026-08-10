import type { MatrixResponse } from '@/api/matrixTypes'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { AuditPageLink } from '@/components/AuditPageLink'
import { ActiveAgentJobsStrip } from '@/components/control-room/ActiveAgentJobsStrip'
import { AgentFocusDock } from '@/components/control-room/AgentFocusDock'
import { BayDetailDrawer } from '@/components/control-room/BayDetailDrawer'
import { CommandIntentStrip } from '@/components/control-room/CommandIntentStrip'
import { ControlRoomBay } from '@/components/control-room/ControlRoomBay'
import { ControlRoomBayCards } from '@/components/control-room/ControlRoomBayCards'
import { ControlRoomAttentionStrip } from '@/components/control-room/ControlRoomAttentionStrip'
import { ControlRoomVerdictStrip } from '@/components/control-room/ControlRoomVerdictStrip'
import { AgentTriadStrip } from '@/components/task-mode/AgentTriadStrip'
import type { TaskModeId } from '@/lib/task-mode/types'
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
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useMissionVerification } from '@/hooks/useMissionVerification'
import { useNetworkLiveProbe } from '@/hooks/useNetworkLiveProbe'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePendingDecisionBriefs } from '@/hooks/useDecisionBriefs'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { computeAllTracks } from '@/lib/briefing/workTracks'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import {
  buildControlRoomAttentionItems,
  buildControlRoomBaySignals,
  loadOpenControlRoomBayIds,
  nextOpenBayIds,
  parseControlRoomBayHash,
  persistOpenControlRoomBayIds,
  resolveInitialOpenBayIds,
  scrollToControlRoomBay,
  type ControlRoomBayId,
} from '@/lib/control-room/controlRoomBays'
import {
  collectMissionDegradationItems,
  missionDegradationSummary,
} from '@/lib/control-room/missionSignals'
import type { OpenRuntimeMapFn } from '@/lib/runtime-map/runtimeMapNavigation'
import {
  buildPromoteCutoverModel,
  stashPromotePreflightPack,
} from '@/lib/control-room/promoteCutover'
import { useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import { DELIVER_STG_RECOVER_SCOPE } from '@/lib/agent/agentScopes'
import { buildDeliverStgRecoverPrompt } from '@/lib/agent/deliverStgRecoverPrompt'
import { fetchSupplyChain } from '@/api/delivery'
import { fetchRemediationJobs, startRemediation } from '@/api/remediation'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import { findActiveRemediationJobs } from '@/lib/remediation/remediationJobDisplay'

import type { ClusterSummary } from '@/api/clusterTypes'
import type { ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'

type ControlRoomPageProps = {
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
  stgGate?: ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: TierBStatusResponse
  onOpenRuntimeMap: OpenRuntimeMapFn
  onOpenDelivery: () => void
  onOpenCluster: () => void
  onOpenAudit: () => void
  onOpenBriefing: (opts?: BriefingUrlState) => void
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  onOpenPlatformRelease?: () => void
  onOpenTradeDeploy?: () => void
  onOpenPluginRelease?: () => void
  onOpenPromote?: () => void
  onOpenAgentProtocol?: () => void
  onOpenNetwork?: () => void
  onOpenSatelliteBus?: () => void
  onOpenCompute?: () => void
  onOpenDefects?: () => void
  onOpenAgentDeskTab?: () => void
  onOpenLaunchView?: (mode: 'ops') => void
  /** Trade readiness IB Fleet CTA → Daily Ops TCC */
  onOpenFleetVendor?: () => void
  onModeChange?: (landingTab: string, modeId: TaskModeId) => void
} & AmbientAgentShellProps

function bayById(
  bays: ReturnType<typeof buildControlRoomBaySignals>,
  id: ControlRoomBayId,
) {
  return bays.find(b => b.id === id)
}

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
  onOpenPluginRelease,
  onOpenPromote,
  onOpenAgentProtocol,
  onOpenNetwork,
  onOpenSatelliteBus,
  onOpenCompute,
  onOpenDefects,
  onOpenAgentDeskTab,
  onOpenLaunchView,
  onOpenFleetVendor,
  onModeChange,
  ambientJobId,
  onStartAgentJob,
}: ControlRoomPageProps) {
  const [selection, setSelection] = useState<ControlRoomSelection>(null)
  const [activeBay, setActiveBay] = useState<ControlRoomBayId | null>(() =>
    parseControlRoomBayHash(typeof window !== 'undefined' ? window.location.hash : ''),
  )
  /** Accordion only — Bay Scan is the sole bay picker (no Multi / chip nav). */
  const [openBayIds, setOpenBayIds] = useState<Set<ControlRoomBayId>>(() => {
    const preferred = parseControlRoomBayHash(
      typeof window !== 'undefined' ? window.location.hash : '',
    )
    const ids = resolveInitialOpenBayIds({
      mode: 'single',
      preferredId: preferred,
      storedOpen: loadOpenControlRoomBayIds(),
    })
    return new Set(ids)
  })
  const didAutoOpenUnhealthy = useRef(
    (typeof window !== 'undefined' && parseControlRoomBayHash(window.location.hash) != null) ||
      loadOpenControlRoomBayIds().length > 0,
  )
  const { snapshot, matrices: liveMatrices, dataUpdatedAt, isLoading: missionLoading } = useMissionSnapshot()
  const { banner, dismissBanner, pendingVerify } = useMissionVerification()
  const { canOperate } = usePlatformAuth()
  const operateQueueQuery = useOperateQueue()
  const briefsQuery = usePendingDecisionBriefs()
  const networkProbe = useNetworkLiveProbe()
  const matrixList = liveMatrices.length > 0 ? liveMatrices : matrices

  const jobsQuery = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 10_000,
  })
  const activeAgentJobCount = findActiveRemediationJobs(jobsQuery.data?.jobs ?? []).length
  const recentRemediationFail = useMemo(() => {
    const jobs = jobsQuery.data?.jobs ?? []
    if (jobs.length === 0) return false
    const latest = [...jobs].sort(
      (a, b) => Date.parse(b.updated_at) - Date.parse(a.updated_at),
    )[0]
    return latest?.status === 'failed' || latest?.phase === 'failed'
  }, [jobsQuery.data?.jobs])

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

  const showHealth = onOpenAgentProtocol != null
  const promoteLamp = useMemo(() => {
    if (context == null) return undefined
    return buildPromoteCutoverModel({
      context,
      matrices: matrixList,
      stgSmoke,
      stgGate,
      lastDeliverSucceeded,
      tierB,
    }).prodLamp
  }, [context, matrixList, stgSmoke, stgGate, lastDeliverSucceeded, tierB])

  const baySignals = useMemo(
    () =>
      buildControlRoomBaySignals({
        snapshot,
        operateOpenCount: operateQueueQuery.data?.open.length ?? 0,
        pendingBriefCount: briefsQuery.pendingCount,
        activeAgentJobCount,
        networkProbe: showHealth ? networkProbe.probeReach : undefined,
        promoteLamp,
        showHealth,
      }),
    [
      snapshot,
      operateQueueQuery.data?.open.length,
      briefsQuery.pendingCount,
      activeAgentJobCount,
      showHealth,
      networkProbe.probeReach,
      promoteLamp,
    ],
  )

  const attentionItems = useMemo(
    () => buildControlRoomAttentionItems(baySignals),
    [baySignals],
  )

  useEffect(() => {
    if (didAutoOpenUnhealthy.current) return
    if (missionLoading || baySignals.length === 0) return
    const unhealthy = baySignals
      .filter(b => b.signal === 'degraded' || b.signal === 'fail')
      .map(b => b.id)
    didAutoOpenUnhealthy.current = true
    if (unhealthy.length === 0) return
    const nextIds = unhealthy.slice(0, 1)
    setOpenBayIds(new Set(nextIds))
    persistOpenControlRoomBayIds(new Set(nextIds))
    if (nextIds[0] != null) setActiveBay(nextIds[0])
  }, [baySignals, missionLoading])

  const missionPrimaryCause = useMemo(() => {
    if (snapshot.missionOverall === 'ok') return 'Mission probes nominal'
    return missionDegradationSummary(collectMissionDegradationItems(snapshot))
  }, [snapshot])

  const jumpToBay = useCallback((id: ControlRoomBayId) => {
    setActiveBay(id)
    setOpenBayIds(() => {
      const next = new Set<ControlRoomBayId>([id])
      persistOpenControlRoomBayIds(next)
      return next
    })
    requestAnimationFrame(() => {
      scrollToControlRoomBay(id)
    })
  }, [])

  const setBayOpen = useCallback((id: ControlRoomBayId, open: boolean) => {
    if (open) setActiveBay(id)
    setOpenBayIds(prev => {
      const next = nextOpenBayIds('single', prev, id, open)
      persistOpenControlRoomBayIds(next)
      return next
    })
  }, [])

  useEffect(() => {
    const fromHash = parseControlRoomBayHash(window.location.hash)
    if (fromHash == null) return
    requestAnimationFrame(() => {
      scrollToControlRoomBay(fromHash, { updateHash: false })
    })
  }, [])

  if (contextLoading || matrixLoading || missionLoading) {
    return (
      <div className="flex min-h-[12rem] flex-col justify-center gap-1.5 py-6">
        <p className="text-[var(--text-dense-body)] text-foreground">Loading mission control…</p>
        <p className="text-[var(--text-dense-meta)] text-muted-foreground">
          Matrix and mission probes can take several seconds on first load.
        </p>
      </div>
    )
  }

  if (matrixError != null) {
    return (
      <p className="lamp-fail">
        Failed to load matrix: {matrixError.message}
      </p>
    )
  }

  const missionBay = bayById(baySignals, 'mission')
  const launchBay = bayById(baySignals, 'launch')
  const operateBay = bayById(baySignals, 'operate')
  const releaseBay = bayById(baySignals, 'release')
  const healthBay = bayById(baySignals, 'health')
  const governanceBay = bayById(baySignals, 'governance')

  return (
    <div className="control-room-layout flex w-full min-w-0 flex-col gap-3">
      <ControlRoomVerdictStrip
        missionSignal={snapshot.missionOverall}
        primaryCause={missionPrimaryCause}
        dataUpdatedAt={dataUpdatedAt}
        bays={baySignals}
        isLoading={missionLoading}
        onSelectBay={jumpToBay}
      />

      {onModeChange != null && (
        <AgentTriadStrip
          onModeChange={onModeChange}
          operateQueueOpen={operateQueueQuery.data?.open.length ?? 0}
          recentRemediationFail={recentRemediationFail}
        />
      )}

      <ControlRoomAttentionStrip items={attentionItems} onSelectBay={jumpToBay} />

      <ControlRoomBayCards
        bays={baySignals}
        activeBay={activeBay}
        openBayIds={openBayIds}
        onSelectBay={jumpToBay}
      />

      <div className="control-room-diagnosis flex flex-col gap-3" aria-label="Room posture detail">
        {openBayIds.size === 0 && (
          <p className="m-0 rounded-md border border-dashed border-border px-3 py-4 text-center text-[var(--text-dense-meta)] text-muted-foreground">
            Select a bay above to open posture detail.
          </p>
        )}

        {missionBay != null && openBayIds.has('mission') && (
          <ControlRoomBay
            bayId="mission"
            title="Mission"
            signal={missionBay.signal}
            reason={missionBay.reason}
            open
            onOpenChange={open => setBayOpen('mission', open)}
          >
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
              onOpenFleetVendor={onOpenFleetVendor}
              onOpenPromote={handleOpenPromotePreflight}
              onPlaybookFix={handlePlaybookFix}
              playbookFixPending={playbookFixMutation.isPending}
              canOperate={canOperate}
            />
          </ControlRoomBay>
        )}

        {launchBay != null && openBayIds.has('launch') && (
          <ControlRoomBay
            bayId="launch"
            title="Launch"
            signal={launchBay.signal}
            reason={launchBay.reason}
            open
            onOpenChange={open => setBayOpen('launch', open)}
          >
            <LaunchPad
              role="posture"
              onOpenTaskControlCenter={() => onOpenLaunchView?.('ops')}
              onOpenPlatformRelease={onOpenPlatformRelease ?? onOpenDelivery}
              onOpenTradeDeploy={onOpenTradeDeploy ?? onOpenDelivery}
              onOpenPluginRelease={onOpenPluginRelease ?? onOpenLaunchView?.bind(null, 'ops')}
            />
          </ControlRoomBay>
        )}

        {operateBay != null && openBayIds.has('operate') && (
          <ControlRoomBay
            bayId="operate"
            title="Operate"
            signal={operateBay.signal}
            reason={operateBay.reason}
            open
            onOpenChange={open => setBayOpen('operate', open)}
          >
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

            <OperateQueueStrip onOpenAgentDesk={onOpenAgentDesk} />
          </ControlRoomBay>
        )}

        {releaseBay != null && openBayIds.has('release') && (
          <ControlRoomBay
            bayId="release"
            title="Release"
            signal={releaseBay.signal}
            reason={releaseBay.reason}
            open
            onOpenChange={open => setBayOpen('release', open)}
          >
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

            <MissionSignalProgramStrip onOpenDelivery={onOpenDelivery} />
          </ControlRoomBay>
        )}

        {showHealth && healthBay != null && openBayIds.has('health') && (
          <ControlRoomBay
            bayId="health"
            title="Health"
            signal={healthBay.signal}
            reason={healthBay.reason}
            open
            onOpenChange={open => setBayOpen('health', open)}
          >
            <NetworkHealthPanel
              context={context}
              onOpenAgentProtocol={onOpenAgentProtocol!}
              onOpenNetwork={onOpenNetwork}
            />
          </ControlRoomBay>
        )}

        {governanceBay != null && openBayIds.has('governance') && (
          <ControlRoomBay
            bayId="governance"
            title="Governance"
            signal={governanceBay.signal}
            reason={governanceBay.reason}
            open
            onOpenChange={open => setBayOpen('governance', open)}
          >
            <ProgramContextSection embedded>
              <div className="flex flex-col gap-3">
                <div className="flex flex-col gap-1.5">
                  <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
                    Rocket — Ops Platform subsystems
                  </span>
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
                </div>

                {onOpenSatelliteBus != null &&
                  onOpenNetwork != null &&
                  onOpenCompute != null &&
                  onOpenDefects != null &&
                  onOpenAgentDeskTab != null && (
                  <div className="flex flex-col gap-1.5">
                    <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
                      Spokes — Satellite · Ground Systems · Engineer
                    </span>
                    <SpokeSignalCards
                      onOpenSatelliteBus={onOpenSatelliteBus}
                      onOpenNetwork={onOpenNetwork}
                      onOpenCompute={onOpenCompute}
                      onOpenAgentDesk={onOpenAgentDeskTab}
                      onOpenDefects={onOpenDefects}
                    />
                  </div>
                )}

                <WorkTracksStrip tracks={trackSummaries} onOpenBriefing={onOpenBriefing} />

                <div className="flex flex-col gap-1.5">
                  <div className="flex flex-wrap items-center justify-between gap-2">
                    <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
                      Dual flywheel · Pipeline
                    </span>
                    <AuditPageLink onOpenAudit={onOpenAudit} />
                  </div>
                  <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                    Flywheel A ↔ Coupling ↔ Flywheel B. CI/CD path on{' '}
                    <button type="button" className="focus-strip-link" onClick={onOpenDelivery}>
                      Delivery
                    </button>
                    .
                  </p>
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
                </div>

                <AgentFocusDock
                  context={context}
                  matrices={matrices}
                  selection={selection}
                  onOpenAgentDesk={() => onOpenAgentDesk?.()}
                />
              </div>
            </ProgramContextSection>
          </ControlRoomBay>
        )}
      </div>

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
