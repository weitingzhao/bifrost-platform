import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, PageHeader } from '@bifrost/ui'
import {
  fetchCluster,
  fetchClusterServiceReadiness,
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchStgSmoke,
} from '@/api/platform'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { OpsTaskStrips, OpsTaskSummaryRow } from '@/components/task-mode/OpsTaskStrips'
import {
  useRocketProdReadiness,
  useSatelliteProdReadiness,
} from '@/components/task-mode/TaskModeReadinessStrip'
import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useDevProgramInstance } from '@/hooks/useDevProgramInstance'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import {
  buildPlatformProdFixPrompt,
  buildTradeProdFixPrompt,
  PROD_ENV_FIX_SCOPE,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
import { missionStatus } from '@/lib/control-room/missionSignals'
import { collectClusterIssues } from '@/lib/cluster/collectClusterIssues'
import { PLATFORM_RELEASE_SCOPE } from '@/lib/agent/platformReleaseAgentPrompt'
import {
  buildTradeDeployPrompt,
  TRADE_DEPLOY_SCOPE,
} from '@/lib/agent/tradeDeployAgentPrompt'
import { buildStgReleasePhases } from '@/lib/architecture/deliveryMainlineCatalog'
import { DELIVER_PLATFORM_PIPELINE } from '@/lib/delivery/deliverPlatformPhases'
import { DELIVER_STG_PIPELINE } from '@/lib/delivery/deliverStgPhases'
import {
  resolveAllTaskPhaseStatuses,
  type TaskPhaseStatusInput,
} from '@/lib/task-mode/navLens'
import {
  buildDailyOpsMissionFixPrompt,
  buildPhaseHints,
  type TaskPhaseFixAction,
} from '@/lib/task-mode/taskPhaseDiagnostics'
import { pickDeployPipelineRun } from '@/components/delivery/ReleaseStepCommandCenter'
import { useTaskMode } from '@/lib/task-mode/TaskModeContext'
import type { TaskPhaseDef } from '@/lib/task-mode/types'
import type { BriefingUrlState } from '@/lib/briefing/briefingUrlState'

export type TaskControlCenterProps = AmbientAgentShellProps & {
  context?: OpsContextResponse
  matrices?: MatrixResponse[]
  stgSmoke?: import('@/api/types').StgSmokeResponse
  stgGate?: import('@/api/types').ReleaseGateResponse
  lastDeliverSucceeded?: boolean
  tierB?: import('@/api/types').TierBStatusResponse
  onNavigate: (tabId: string) => void
  onOpenBriefing?: (opts?: BriefingUrlState) => void
  onOpenPromote?: () => void
  onOpenDelivery?: () => void
}

export function TaskControlCenter({
  context,
  matrices = [],
  stgSmoke,
  stgGate,
  lastDeliverSucceeded,
  tierB,
  onNavigate,
  onOpenBriefing,
  onOpenPromote,
  onOpenDelivery,
  ambientJobId,
  onStartAgentJob,
}: TaskControlCenterProps) {
  const { mode } = useTaskMode()
  const { canOperate } = usePlatformAuth()
  const { snapshot } = useMissionSnapshot()
  const queueQ = useOperateQueue()

  const rocketProd = useRocketProdReadiness(mode.id === 'rocket-launch')
  const satelliteProd = useSatelliteProdReadiness(mode.id === 'satellite-deploy')

  const clusterForFixQ = useQuery({
    queryKey: ['task-cc', 'cluster-fix'],
    queryFn: fetchCluster,
    refetchInterval: 20_000,
    enabled:
      (mode.id === 'rocket-launch' && rocketProd.prodBlocked) ||
      (mode.id === 'satellite-deploy' && satelliteProd.prodBlocked) ||
      (mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok'),
  })

  const serviceReadinessForFixQ = useQuery({
    queryKey: ['task-cc', 'service-readiness-fix'],
    queryFn: fetchClusterServiceReadiness,
    refetchInterval: 20_000,
    enabled:
      (mode.id === 'rocket-launch' && rocketProd.prodBlocked) ||
      (mode.id === 'satellite-deploy' && satelliteProd.prodBlocked) ||
      (mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok'),
  })

  const devProgram = useDevProgramInstance(mode)
  const programQ = devProgram

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
        matrices,
        stgSmoke,
        tierB,
      }),
    }),
  })

  const prodFixLabel = scopeToLabel(PROD_ENV_FIX_SCOPE)

  const aiPlatformProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildPlatformProdFixPrompt({
          prodOverall: rocketProd.prodOverall,
          namespace: rocketProd.prodNamespace ?? 'bifrost-platform-prod',
          signals: rocketProd.fixSignals ?? [],
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
      }
    },
  })

  const aiTradeProdFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      const issues = collectClusterIssues({
        summary: cluster,
        serviceReadiness,
      })
      return {
        prompt: buildTradeProdFixPrompt({
          prodOverall: satelliteProd.prodOverall,
          stgNamespace: satelliteProd.stgNamespace ?? 'bifrost-stg',
          prodNamespace: satelliteProd.prodNamespace ?? 'bifrost-prod',
          signals: satelliteProd.fixSignals ?? [],
        }),
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues,
      }
    },
  })

  const dispatchReleaseAgent = () => {
    if (!canOperate || aiRelease.disabled) return
    aiRelease.trigger()
  }

  const dispatchTradeDeployAgent = () => {
    if (!canOperate || aiTradeDeploy.disabled) return
    aiTradeDeploy.trigger()
  }

  const supplyQ = useQuery({
    queryKey: ['task-cc', 'supply'],
    queryFn: fetchSupplyChain,
    refetchInterval: 20_000,
    enabled: mode.id === 'rocket-launch',
  })

  const platformRunsQ = useQuery({
    queryKey: ['task-cc', 'platform-runs'],
    queryFn: () => fetchPipelineRuns(DELIVER_PLATFORM_PIPELINE),
    refetchInterval: 20_000,
    enabled: mode.id === 'rocket-launch' || mode.id === 'rocket-build',
  })

  const platformStgGateQ = useQuery({
    queryKey: ['task-cc', 'platform-stg-gate'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 20_000,
    enabled: mode.id === 'rocket-launch' || mode.id === 'rocket-build',
  })

  const platformProdGateQ = useQuery({
    queryKey: ['task-cc', 'platform-prod-gate'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: 20_000,
    enabled: mode.id === 'rocket-launch',
  })

  const tradeRunsQ = useQuery({
    queryKey: ['task-cc', 'trade-runs-detail'],
    queryFn: () => fetchPipelineRuns(DELIVER_STG_PIPELINE),
    refetchInterval: 20_000,
    enabled: mode.id === 'satellite-deploy' || mode.id === 'satellite-build',
  })

  const tradeGateQ = useQuery({
    queryKey: ['task-cc', 'trade-gate-detail'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 20_000,
    enabled: mode.id === 'satellite-deploy' || mode.id === 'satellite-build',
  })

  const smokeQ = useQuery({
    queryKey: ['task-cc', 'smoke-detail'],
    queryFn: fetchStgSmoke,
    refetchInterval: 20_000,
    enabled: mode.id === 'satellite-deploy' || mode.id === 'satellite-build',
  })

  const statusInput = useMemo((): TaskPhaseStatusInput => {
    const platformRuns = platformRunsQ.data?.runs
    const tradeRuns = tradeRunsQ.data?.runs
    const tradeGateData = tradeGateQ.data
    const tradeSmokeOk = smokeQ.data?.reachability === 'ok'
    const platformRun = pickDeployPipelineRun(platformRuns, {
      gatePassed: platformStgGateQ.data?.result === 'pass',
    })
    const tradeRun = pickDeployPipelineRun(tradeRuns, {
      gatePassed: tradeGateData?.result === 'pass',
      smokeOk: tradeSmokeOk,
    })
    return {
      context,
      snapshot,
      supplyChain: supplyQ.data,
      stgReleasePhases: buildStgReleasePhases(context),
      operateQueueOpenCount: queueQ.data?.open.length ?? 0,
      programDetail: programQ.programDetail,
      platformStgRun: platformRun,
      platformStgGate: platformStgGateQ.data,
      platformProdGate: platformProdGateQ.data,
      tradeStgRun: tradeRun,
      tradeStgGate: tradeGateData,
      tradeStgSmokeOk: tradeSmokeOk,
    }
  }, [
    context,
    snapshot,
    supplyQ.data,
    queueQ.data,
    programQ.programDetail,
    platformRunsQ.data,
    platformStgGateQ.data,
    platformProdGateQ.data,
    tradeRunsQ.data,
    tradeGateQ.data,
    smokeQ.data,
  ])

  const phases = mode.phases ?? []
  const statuses = useMemo(
    () => resolveAllTaskPhaseStatuses(mode.id, statusInput),
    [mode.id, statusInput],
  )

  const phaseHints = useMemo(
    () => buildPhaseHints(mode.id, phases, statuses, statusInput),
    [mode.id, phases, statuses, statusInput],
  )

  const aiDailyOpsFix = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PROD_ENV_FIX_SCOPE,
    label: prodFixLabel,
    buildRequest: async () => {
      const prompt = buildDailyOpsMissionFixPrompt(snapshot)
      if (prompt == null) {
        throw new Error('Mission is NOMINAL — nothing to fix')
      }
      const cluster = clusterForFixQ.data ?? (await fetchCluster())
      const serviceReadiness =
        serviceReadinessForFixQ.data ?? (await fetchClusterServiceReadiness())
      return {
        prompt,
        cluster_summary: cluster,
        service_readiness: serviceReadiness,
        issues: collectClusterIssues({ summary: cluster, serviceReadiness }),
      }
    },
  })

  const doneCount = phases.filter((p: TaskPhaseDef) => statuses[p.id] === 'done').length
  const loopLabel =
    mode.loopArchetype === 'ops' ? 'Ops loop' : mode.loopArchetype === 'dev' ? 'Dev loop' : 'System'

  const handleOpenPhasePage = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }

  const handlePhaseFixAction = (action: TaskPhaseFixAction, _phase: TaskPhaseDef) => {
    if (action.kind === 'agent-fix') {
      if (mode.id === 'daily-ops') {
        aiDailyOpsFix.trigger()
        return
      }
      if (mode.id === 'rocket-launch') {
        aiPlatformProdFix.trigger()
        return
      }
      if (mode.id === 'satellite-deploy') {
        aiTradeProdFix.trigger()
        return
      }
      return
    }
    if (action.tabId != null) onNavigate(action.tabId)
  }

  const showLaunchPad = mode.ops?.showLaunchPad === true
  const resolvedProgramId = devProgram.programId ?? mode.dev?.programId

  const releaseDispatchAllowed = showLaunchPad && !aiRelease.disabled && !rocketProd.prodBlocked
  const tradeDeployDispatchAllowed = showLaunchPad && !aiTradeDeploy.disabled && !satelliteProd.prodBlocked
  const releaseDisabledReason = rocketProd.prodBlocked
    ? rocketProd.prodDisabledReason
    : aiRelease.disabledReason
  const tradeDeployDisabledReason = satelliteProd.prodBlocked
    ? satelliteProd.prodDisabledReason
    : aiTradeDeploy.disabledReason

  return (
    <div className="flex flex-col gap-4">
      <PageHeader
        title="Task Control Center"
        description={`${mode.label} · ${loopLabel} — phased playbook with live status projection.`}
        actions={
          mode.loopArchetype === 'system' ? undefined : (
            <DenseTag variant={mode.loopArchetype === 'dev' ? 'info' : 'warning'}>{loopLabel}</DenseTag>
          )
        }
      />

      {mode.loopArchetype === 'ops' && showLaunchPad && (
        <>
          {!canOperate && (
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
            <OpsFeedback variant="error" title="Failed to start Trade Deploy agent">
              {aiTradeDeploy.error.message}
            </OpsFeedback>
          )}
          {aiPlatformProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiPlatformProdFix.error.message}
            </OpsFeedback>
          )}
          {aiTradeProdFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiTradeProdFix.error.message}
            </OpsFeedback>
          )}
          {aiDailyOpsFix.error != null && (
            <OpsFeedback variant="error" title="Failed to start Agent Fix">
              {aiDailyOpsFix.error.message}
            </OpsFeedback>
          )}
          {mode.id === 'daily-ops' && snapshot.missionOverall !== 'ok' && (
            <OpsFeedback
              variant="warning"
              title={`Mission ${missionStatus(snapshot.missionOverall)} — fix signals before continuing`}
              actions={
                <AgentTriggerButton
                  label="Agent Fix"
                  size="xs"
                  pending={aiDailyOpsFix.isPending}
                  disabled={aiDailyOpsFix.disabled}
                  title={
                    aiDailyOpsFix.disabledReason ??
                    'Diagnose failing rocket/payload signals and remediate via Cluster · Remediate'
                  }
                  onClick={() => aiDailyOpsFix.trigger()}
                />
              }
            >
              Phase 1 stays blocked until mission signals are NOMINAL. Select step 1 in Phase progress for
              root-cause breakdown, or open Control Room for the full mission board.
            </OpsFeedback>
          )}
          {mode.id === 'rocket-launch' && rocketProd.prodBlocked && (
            <OpsFeedback
              variant="warning"
              title="Fix Prod environment before release"
              actions={
                <AgentTriggerButton
                  label="Agent Fix"
                  size="xs"
                  pending={aiPlatformProdFix.isPending}
                  disabled={aiPlatformProdFix.disabled}
                  title={
                    aiPlatformProdFix.disabledReason ??
                    'Start Cluster · Remediate focused on Platform Prod readiness'
                  }
                  onClick={() => aiPlatformProdFix.trigger()}
                />
              }
            >
              Platform Prod readiness is {missionStatus(rocketProd.prodOverall)} — resolve Prod namespace,
              self-health, or release gate issues before launching release agents.
            </OpsFeedback>
          )}
          {mode.id === 'satellite-deploy' && satelliteProd.prodBlocked && (
            <OpsFeedback
              variant="warning"
              title="Fix Prod environment before release"
              actions={
                <AgentTriggerButton
                  label="Agent Fix"
                  size="xs"
                  pending={aiTradeProdFix.isPending}
                  disabled={aiTradeProdFix.disabled}
                  title={
                    aiTradeProdFix.disabledReason ??
                    'Start Cluster · Remediate focused on Trade Prod readiness'
                  }
                  onClick={() => aiTradeProdFix.trigger()}
                />
              }
            >
              Trade Prod readiness is {missionStatus(satelliteProd.prodOverall)} — resolve Prod workloads,
              datastore, IB socket, or API reachability before deploying.
            </OpsFeedback>
          )}
        </>
      )}

      {mode.loopArchetype === 'ops' &&
        (mode.id === 'rocket-launch' || mode.id === 'satellite-deploy' || mode.id === 'daily-ops') && (
          <OpsTaskSummaryRow
            mode={mode}
            context={context}
            matrices={matrices}
            stgSmoke={stgSmoke}
            stgGate={stgGate}
            lastDeliverSucceeded={lastDeliverSucceeded}
            tierB={tierB}
            onNavigate={onNavigate}
            onOpenPromote={onOpenPromote}
            onOpenDelivery={onOpenDelivery}
            onDispatchRelease={showLaunchPad ? dispatchReleaseAgent : undefined}
            onDispatchTradeDeploy={showLaunchPad ? dispatchTradeDeployAgent : undefined}
            releasePending={aiRelease.isPending}
            tradeDeployPending={aiTradeDeploy.isPending}
            canDispatchRelease={releaseDispatchAllowed}
            canDispatchTradeDeploy={tradeDeployDispatchAllowed}
            releaseDisabledReason={releaseDisabledReason}
            tradeDeployDisabledReason={tradeDeployDisabledReason}
          />
        )}

      {phases.length > 0 && (
        <div className="rounded-lg border border-border bg-card px-3 py-1.5">
          <div className="mb-1 flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-meta)] font-semibold">Phase progress</span>
            <DenseTag variant="neutral" className="text-[9px]">
              {doneCount}/{phases.length} complete
            </DenseTag>
          </div>
          <TaskPhaseProgress
            phases={phases}
            statuses={statuses}
            hints={phaseHints}
            onOpenFullPage={handleOpenPhasePage}
            onFixAction={handlePhaseFixAction}
          />
        </div>
      )}

      {mode.loopArchetype === 'ops' && (
        <OpsTaskStrips
          mode={mode}
          context={context}
          matrices={matrices}
          stgSmoke={stgSmoke}
          stgGate={stgGate}
          lastDeliverSucceeded={lastDeliverSucceeded}
          tierB={tierB}
          onNavigate={onNavigate}
          onOpenPromote={onOpenPromote}
          onOpenDelivery={onOpenDelivery}
          onDispatchRelease={showLaunchPad ? dispatchReleaseAgent : undefined}
          onDispatchTradeDeploy={showLaunchPad ? dispatchTradeDeployAgent : undefined}
          releasePending={aiRelease.isPending}
          tradeDeployPending={aiTradeDeploy.isPending}
          canDispatchRelease={releaseDispatchAllowed}
          canDispatchTradeDeploy={tradeDeployDispatchAllowed}
          releaseDisabledReason={releaseDisabledReason}
          tradeDeployDisabledReason={tradeDeployDisabledReason}
          promoteOnly={
            mode.id === 'rocket-launch' || mode.id === 'satellite-deploy' || mode.id === 'daily-ops'
          }
        />
      )}

      {mode.loopArchetype === 'dev' && (
        <DevTaskStrips
          mode={mode}
          canOperate={canOperate}
          programDetail={devProgram.programDetail}
          programLoading={devProgram.programLoading}
          programError={devProgram.programError}
          resolvedProgramId={resolvedProgramId}
          createPending={devProgram.createPending}
          onCreateProgram={devProgram.ensureProgram}
          onCreateNewInstance={() => devProgram.createNewInstance({ instanceLabel: mode.label })}
          onNavigate={onNavigate}
          onOpenBriefing={onOpenBriefing}
        />
      )}

      {resolvedProgramId != null && devProgram.programDetail != null && (
        <div className="rounded-lg border border-border bg-secondary px-3 py-2.5">
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-label)] font-semibold">Program sign-off</span>
            <DenseTag variant="neutral">{devProgram.programDetail.program.title}</DenseTag>
            <DenseTag variant={devProgram.programDetail.program.complete ? 'success' : 'warning'}>
              {devProgram.programDetail.program.phases_signed ?? 0}/
              {devProgram.programDetail.program.phase_count} signed
            </DenseTag>
          </div>
          <Button
            variant="ghost"
            size="xs"
            className="mt-2"
            onClick={() => onNavigate('delivery-board')}
          >
            Open Delivery Board →
          </Button>
        </div>
      )}
    </div>
  )
}
