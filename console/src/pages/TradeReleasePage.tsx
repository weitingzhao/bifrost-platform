import type { SupplyChainResponse } from '@/api/deliveryTypes'
import { Button, DenseTag } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useEffect, useMemo, useState } from 'react'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchDeliveryPipelines, fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchGitOpsApps } from '@/api/gitOps'
import { fetchReleaseGate, fetchStgSmoke, fetchTierBStatus } from '@/api/promote'
import type { OpsContextResponse } from '@/api/opsContextTypes'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { ConstellationStrip } from '@/components/delivery/ConstellationStrip'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeliveryFlow } from '@/components/delivery/DeliveryFlow'
import { GitOpsQuickActionsPanel } from '@/components/delivery/GitOpsQuickActionsPanel'
import { DeliveryReleaseWorkflowPanel } from '@/components/delivery/DeliveryReleaseWorkflowPanel'
import { DeployActionBar } from '@/components/delivery/DeployActionBar'
import { GateActionBar } from '@/components/delivery/GateActionBar'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneGateSummaryLine,
  LaneStateStrip,
  LiveTradingFreezeNote,
} from '@/components/delivery/LaneDetailShell'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { PipelineRunsPanel } from '@/components/delivery/PipelineRunsPanel'
import { ReleaseStepCommandCenter } from '@/components/delivery/ReleaseStepCommandCenter'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import {
  usePromoteVerifyReadiness,
  useSatelliteProdReadiness,
} from '@/components/task-mode/readiness/hooks'
import {
  runStepStatus,
  gateStepStatus,
  deriveReleaseOutcome,
  pickDeployPipelineRun,
  pickNextCycleDeployRun,
  isReleaseCycleTerminal,
  type FlowStep,
} from '@/lib/delivery/releaseStepTypes'
import { ReleaseStateBanner } from '@/components/delivery/ReleaseStateBanner'
import { StgSmokePanel } from '@/components/delivery/StgSmokePanel'
import { StgTierBChecklistPanel } from '@/components/delivery/StgTierBChecklistPanel'
import { SupplyChainPanel } from '@/components/delivery/SupplyChainPanel'
import { TradeEnvAccessBar } from '@/components/delivery/TradeEnvAccessBar'
import { PlatformGateHistorySection } from '@/components/promote/PlatformReleaseGateSection'
import { ReleaseCycleHistorySection } from '@/components/promote/ReleaseCycleHistorySection'
import { ReleaseGateCompareSection } from '@/components/promote/ReleaseGateCompareSection'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { useLaneStepFocus } from '@/hooks/useLaneStepFocus'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { isAmbientAgentActive } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildTradeDeployPrompt,
  TRADE_DEPLOY_SCOPE,
} from '@/lib/agent/tradeDeployAgentPrompt'
import {
  buildTradeProdFixPrompt,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
  type ProdFixSignal,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildClusterPackBody,
  buildDispatchedFixPrompt,
} from '@/lib/agent/readinessFixDispatch'
import { missionStatus } from '@/lib/control-room/missionSignals'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { useConstellationImpact } from '@/hooks/useConstellationImpact'
import { useConstellationLaunch } from '@/hooks/useConstellationLaunch'
import {
  buildLaunchCheckpoints,
  hasDeliverInFlight,
  resolveLaunchVerdict,
} from '@/lib/task-mode/satelliteLaunchVerdict'
import { useResearchLiveTag } from '@/hooks/useResearchLiveTag'

const AI_DEPLOY_LABEL = 'AI Deploy'
const AI_DEPLOY_TASK_LABEL = scopeToLabel(TRADE_DEPLOY_SCOPE)
const AI_RESOLVE_LABEL = 'AI Resolve'
const AI_RESOLVE_TITLE =
  'AI Resolve release conditions — clear NO-GO checkpoints before AI Deploy'

const TRADE_STG_TARGET = deliveryTargetById('trade-stg')
const TRADE_PROD_TARGET = deliveryTargetById('trade-prod')
const STG_PIPELINE = TRADE_STG_TARGET.pipeline
const PROD_PIPELINE = TRADE_PROD_TARGET.pipeline

const STEP_LABELS = ['Staging Deploy', 'Staging Gate', 'Production Deploy', 'Production Gate'] as const

function renderTradeStepActions(activeIndex: number, agentSessionId?: string | null) {
  switch (activeIndex) {
    case 0:
      return (
        <DeployActionBar
          target={TRADE_STG_TARGET}
          releaseStateTier="trade"
          deployButtonLabel={`Run ${STG_PIPELINE}`}
          agentSessionId={agentSessionId}
        />
      )
    case 1:
      return <GateActionBar tier="stg" label="STG" />
    case 2:
      return (
        <DeployActionBar
          target={TRADE_PROD_TARGET}
          releaseStateTier="trade"
          deployButtonLabel={`Run ${PROD_PIPELINE}`}
          agentSessionId={agentSessionId}
        />
      )
    default:
      return <GateActionBar tier="prod" label="PROD" />
  }
}

function SupplyChainSummaryLine({
  data,
  isLoading,
}: {
  data: SupplyChainResponse | undefined
  isLoading: boolean
}) {
  if (isLoading) return <span className="text-dense-meta text-muted-foreground">Supply chain…</span>
  const repos = data?.tracked_repos ?? []
  const cms = data?.dockerfile_configmaps?.filter(cm => cm.present).length ?? 0
  const cmTotal = data?.dockerfile_configmaps?.length ?? 0
  return (
    <span className="inline-flex flex-wrap items-center gap-2 text-dense-meta text-muted-foreground">
      <DenseTag variant={repos.length >= 7 ? 'success' : 'warning'}>
        {repos.length} Gitea repos
      </DenseTag>
      <DenseTag variant={cms === cmTotal && cmTotal > 0 ? 'success' : 'warning'}>
        Dockerfile CMs {cms}/{cmTotal}
      </DenseTag>
    </span>
  )
}

type TradeReleasePageProps = {
  context: OpsContextResponse | undefined
  isLoading?: boolean
  onOpenPlacement?: () => void
  onOpenSatelliteBus?: () => void
  onOpenObservability?: () => void
  onOpenApiHealth?: () => void
} & AmbientAgentShellProps

export function TradeReleasePage({
  context,
  isLoading = false,
  onOpenPlacement,
  onOpenSatelliteBus,
  onOpenObservability,
  onOpenApiHealth,
  ambientJobId,
  ambientJobScope,
  ambientJobStatus,
  onStartAgentJob,
  onExpandAgentDock,
}: TradeReleasePageProps) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)
  const [nextCycle, setNextCycle] = useState(false)
  const [nextCycleBaseline, setNextCycleBaseline] = useState<{
    stg: string | null
    prod: string | null
  } | null>(null)

  const satelliteProd = useSatelliteProdReadiness(true)
  const promoteVerify = usePromoteVerifyReadiness(true)

  const stgRuns = useQuery({
    queryKey: ['delivery', 'runs', STG_PIPELINE],
    queryFn: () => fetchPipelineRuns(STG_PIPELINE),
    refetchInterval: 15_000,
  })
  const prodRuns = useQuery({
    queryKey: ['delivery', 'runs', PROD_PIPELINE],
    queryFn: () => fetchPipelineRuns(PROD_PIPELINE),
    refetchInterval: 15_000,
  })
  const stgGate = useQuery({
    queryKey: ['promote', 'release-gate', 'stg'],
    queryFn: () => fetchReleaseGate('stg'),
    refetchInterval: 30_000,
  })
  const prodGate = useQuery({
    queryKey: ['promote', 'release-gate', 'prod'],
    queryFn: () => fetchReleaseGate('prod'),
    refetchInterval: 30_000,
  })
  const stgSmoke = useQuery({
    queryKey: ['delivery', 'stg-smoke'],
    queryFn: fetchStgSmoke,
    refetchInterval: 30_000,
  })
  const tierB = useQuery({
    queryKey: ['promote', 'tier-b'],
    queryFn: fetchTierBStatus,
    refetchInterval: 30_000,
  })
  const pipelines = useQuery({
    queryKey: ['delivery', 'pipelines'],
    queryFn: fetchDeliveryPipelines,
    refetchInterval: 30_000,
  })
  const gitops = useQuery({
    queryKey: ['gitops', 'apps'],
    queryFn: fetchGitOpsApps,
    refetchInterval: 30_000,
  })
  const supplyChain = useQuery({
    queryKey: ['delivery', 'supply-chain'],
    queryFn: fetchSupplyChain,
    refetchInterval: 30_000,
  })

  const deployRevision =
    supplyChain.data?.last_deliver_run?.revision?.trim() ||
    supplyChain.data?.default_revision?.trim() ||
    'main'
  const { tag: researchLiveTag } = useResearchLiveTag()
  const constellationImpact = useConstellationImpact({
    origin: 'trade',
    revision: deployRevision,
  })
  const formation = useConstellationLaunch(constellationImpact)

  const stgGatePassed = stgGate.data?.result === 'pass'
  const prodGatePassed = prodGate.data?.result === 'pass'
  const latestStgRun = stgRuns.data?.runs?.[0]
  const latestProdRun = prodRuns.data?.runs?.[0]
  const smokeOk = stgSmoke.data?.reachability === 'ok'

  const stgRun = nextCycle
    ? pickNextCycleDeployRun(stgRuns.data?.runs, nextCycleBaseline?.stg ?? null)
    : pickDeployPipelineRun(stgRuns.data?.runs, {
        gatePassed: stgGatePassed,
        smokeOk,
      })
  const prodRun = nextCycle
    ? pickNextCycleDeployRun(prodRuns.data?.runs, nextCycleBaseline?.prod ?? null)
    : pickDeployPipelineRun(prodRuns.data?.runs, { gatePassed: prodGatePassed })

  const awaitingNextCycleDeliver = nextCycle && stgRun == null && prodRun == null

  const stgDeploy = awaitingNextCycleDeliver
    ? { status: 'pending' as const, label: 'Awaiting agent' }
    : runStepStatus(stgRun)
  const prodDeploy = awaitingNextCycleDeliver
    ? { status: 'pending' as const, label: 'Not started' }
    : runStepStatus(prodRun)
  const stgGateStep = awaitingNextCycleDeliver
    ? { status: 'pending' as const, label: 'Not run' }
    : gateStepStatus(stgGate.data)
  const prodGateStep = awaitingNextCycleDeliver
    ? { status: 'pending' as const, label: 'Not run' }
    : gateStepStatus(prodGate.data)

  const steps: FlowStep[] = [
    { key: 'stg-deploy', label: 'Staging Deploy', env: 'STG', status: stgDeploy.status, statusLabel: stgDeploy.label },
    { key: 'stg-gate', label: 'Staging Gate', env: 'STG', status: stgGateStep.status, statusLabel: stgGateStep.label },
    { key: 'prod-deploy', label: 'Production Deploy', env: 'PROD', status: prodDeploy.status, statusLabel: prodDeploy.label },
    { key: 'prod-gate', label: 'Production Gate', env: 'PROD', status: prodGateStep.status, statusLabel: prodGateStep.label },
  ]

  const [activeIndex, setActiveIndex] = useLaneStepFocus({
    statuses: [stgDeploy.status, stgGateStep.status, prodDeploy.status, prodGateStep.status],
    ready:
      !stgRuns.isLoading && !prodRuns.isLoading && !stgGate.isLoading && !prodGate.isLoading,
    reason: detailReason,
  })

  const releaseOutcomeBase = deriveReleaseOutcome(steps)
  const releaseOutcome = awaitingNextCycleDeliver
    ? {
        kind: 'in_progress' as const,
        label: 'Starting',
        detail:
          'AI Deploy in progress — decide in Agent Session before Staging Deploy starts',
      }
    : releaseOutcomeBase
  const cycleTerminal = isReleaseCycleTerminal(releaseOutcome, nextCycle)

  const tradeProdFixSignals: ProdFixSignal[] = [
    ...(satelliteProd.rocketBlocked && satelliteProd.rocketFixSignal != null
      ? [satelliteProd.rocketFixSignal]
      : []),
    ...(satelliteProd.fixSignals ?? []),
  ]
  const tradeProdFixScope = pickFixScope(tradeProdFixSignals)
  const tradeProdFixLabel = scopeToLabel(tradeProdFixScope)

  const satelliteVerdictInput = useMemo(
    () => ({
      mode: 'satellite' as const,
      canOperate,
      prodBlocked: satelliteProd.prodBlocked,
      blockKind: satelliteProd.blockKind ?? undefined,
      rocketLabel: missionStatus(satelliteProd.rocketSignal),
      rocketDetail: satelliteProd.rocketDetail,
      tradeProdLabel: missionStatus(satelliteProd.tradeProdOverall),
      tradeProdSignal: satelliteProd.tradeProdOverall,
      rocketSignal: satelliteProd.rocketSignal,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight:
        hasDeliverInFlight(stgRuns.data?.runs) || hasDeliverInFlight(prodRuns.data?.runs),
      agentInFlight:
        isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
        ambientJobScope === TRADE_DEPLOY_SCOPE,
    }),
    [
      canOperate,
      satelliteProd.prodBlocked,
      satelliteProd.blockKind,
      satelliteProd.rocketSignal,
      satelliteProd.rocketDetail,
      satelliteProd.tradeProdOverall,
      promoteVerify.promoteSignal,
      promoteVerify.promoteDetail,
      stgRuns.data?.runs,
      prodRuns.data?.runs,
      ambientJobId,
      ambientJobScope,
      ambientJobStatus,
    ],
  )
  const satelliteVerdict = useMemo(
    () => resolveLaunchVerdict(satelliteVerdictInput),
    [satelliteVerdictInput],
  )
  const satelliteCheckpoints = useMemo(
    () => buildLaunchCheckpoints(satelliteVerdictInput),
    [satelliteVerdictInput],
  )
  const checklistOkCount = satelliteCheckpoints.filter(c => c.ok).length
  const checklistTotal = satelliteCheckpoints.length

  const aiDeploy = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: TRADE_DEPLOY_SCOPE,
    label: AI_DEPLOY_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildTradeDeployPrompt({
        stgRun,
        prodRun,
        stgGate: stgGate.data,
        prodGate: prodGate.data,
        stgSmoke: stgSmoke.data,
        tierB: tierB.data,
        supplyChain: supplyChain.data,
        operatorSurface: 'Deploy Satellite page',
      }),
    }),
  })

  const aiResolve = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    ambientJobStatus,
    onStartAgentJob,
    scope: tradeProdFixScope,
    label: tradeProdFixLabel,
    buildRequest: async () => {
      const signals = tradeProdFixSignals
      const scope = pickFixScope(signals)
      const [cluster, serviceReadiness, supply, smoke] = await Promise.all([
        fetchCluster(),
        fetchClusterServiceReadiness(),
        fetchSupplyChain(),
        fetchStgSmoke(),
      ])
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const fallback = buildTradeProdFixPrompt({
        prodOverall: satelliteProd.prodOverall,
        stgNamespace: satelliteProd.stgNamespace ?? 'bifrost-stg',
        prodNamespace: satelliteProd.prodNamespace ?? 'bifrost-prod',
        signals,
      })
      return {
        prompt: buildDispatchedFixPrompt({
          scope,
          signals,
          clusterFallbackPrompt: fallback,
          extras: {
            supply,
            stgSmoke: smoke,
            pipeline: 'bifrost-deliver-stg',
          },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const deployDispatchAllowed = !aiDeploy.disabled && satelliteVerdict.kind === 'GO'
  const deployDisabledReason =
    satelliteVerdict.kind !== 'GO'
      ? (satelliteVerdict.disabledReason ?? satelliteVerdict.detail)
      : aiDeploy.disabledReason

  const showAiResolve =
    satelliteVerdict.kind === 'NO_GO' && satelliteVerdict.blockKind !== 'auth'
  const resolveRunning =
    isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
    ambientJobScope === tradeProdFixScope &&
    !aiResolve.isPending

  const handleAiResolveClick = () => {
    if (resolveRunning) {
      onExpandAgentDock?.()
      return
    }
    aiResolve.trigger()
  }

  useEffect(() => {
    if (!nextCycle) {
      setNextCycleBaseline(null)
      return
    }
    if (releaseOutcome.kind === 'released') {
      setNextCycle(false)
      setNextCycleBaseline(null)
    }
  }, [nextCycle, releaseOutcome.kind])

  const handleStartNextRelease = () => {
    if (!deployDispatchAllowed) return
    setNextCycle(true)
    setNextCycleBaseline({
      stg: latestStgRun?.name ?? null,
      prod: latestProdRun?.name ?? null,
    })
    setActiveIndex(0)
    aiDeploy.trigger()
  }

  const handleAiDeployClick = () => {
    if (cycleTerminal) {
      handleStartNextRelease()
      return
    }
    if (!deployDispatchAllowed) return
    aiDeploy.trigger()
  }

  if (isLoading || !context) {
    return <p className="text-muted-foreground">Loading release context…</p>
  }

  let stepDetail: ReactNode
  switch (activeIndex) {
    case 0:
      stepDetail = (
        <>
          {awaitingNextCycleDeliver ? (
            <p className="m-0 rounded-md border border-border/60 bg-secondary/40 px-3 py-2 text-dense-meta text-muted-foreground">
              No new Staging Deploy yet — finish Agent Session decisions (Commit &amp; Push /
              approvals) first. A prior failed PipelineRun in Tekton history is not this cycle.
            </p>
          ) : (
            <DeliveryActiveRunPanel target={TRADE_STG_TARGET} collapsible />
          )}
          <LaneDetailCollapse
            title="Deliver readiness · Trade STG"
            defaultOpen={false}
            showModeBadge
            bodyClassName="p-3"
          >
            <PlatformDeliverActuatePanel
              target={TRADE_STG_TARGET}
              hideActions
              agentSessionId={ambientJobId}
            />
          </LaneDetailCollapse>
        </>
      )
      break
    case 1:
      stepDetail = (
        <>
          {awaitingNextCycleDeliver ? (
            <p className="m-0 rounded-md border border-border/60 bg-secondary/40 px-3 py-2 text-dense-meta text-muted-foreground">
              Staging Gate waits until the new Staging Deploy finishes.
            </p>
          ) : (
            <DeliveryActiveRunPanel target={TRADE_STG_TARGET} collapsible />
          )}
          <LaneDetailCollapse
            key="stg-gate-smoke"
            title="STG gate evidence — smoke & Tier B"
            summaryExtra={<LaneGateSummaryLine gate={stgGate.data} />}
            defaultOpen={stgGateStep.status === 'error'}
            showModeBadge
            bodyClassName="flex flex-col gap-3 p-3"
          >
            <StgSmokePanel
              data={stgSmoke.data}
              isLoading={stgSmoke.isLoading}
              isFetching={stgSmoke.isFetching}
              errorMessage={stgSmoke.error instanceof Error ? stgSmoke.error.message : null}
              onRefresh={() => void stgSmoke.refetch()}
              title="STG HTTP smoke"
              description="Post-deliver acceptance via trade-stg gateway."
              collapsible
            />
            <StgTierBChecklistPanel
              tierB={tierB.data}
              tierBLoading={tierB.isLoading}
              layout="observe"
              collapsible
            />
          </LaneDetailCollapse>
        </>
      )
      break
    case 2:
      stepDetail = (
        <>
          <LiveTradingFreezeNote />
          {awaitingNextCycleDeliver ? (
            <p className="m-0 rounded-md border border-border/60 bg-secondary/40 px-3 py-2 text-dense-meta text-muted-foreground">
              Production Deploy waits until Staging Gate passes for this cycle.
            </p>
          ) : (
            <DeliveryActiveRunPanel target={TRADE_PROD_TARGET} collapsible />
          )}
          <LaneDetailCollapse
            title="Deliver readiness · Trade PROD"
            defaultOpen={false}
            showModeBadge
            bodyClassName="p-3"
          >
            <PlatformDeliverActuatePanel
              target={TRADE_PROD_TARGET}
              hideActions
              agentSessionId={ambientJobId}
            />
          </LaneDetailCollapse>
          <LaneDetailCollapse
            title="Pipeline topology · observe"
            defaultOpen={false}
            showModeBadge
            bodyClassName="p-3"
          >
            <PipelineRunsPanel
              pipelines={pipelines.data}
              pipelinesLoading={pipelines.isLoading}
              errorMessage={pipelines.error instanceof Error ? pipelines.error.message : null}
              layout="observe"
              onOpenPlacement={onOpenPlacement}
            />
          </LaneDetailCollapse>
        </>
      )
      break
    default:
      stepDetail = (
        <>
          <LiveTradingFreezeNote />
          {awaitingNextCycleDeliver ? (
            <p className="m-0 rounded-md border border-border/60 bg-secondary/40 px-3 py-2 text-dense-meta text-muted-foreground">
              Production Gate waits until Production Deploy finishes.
            </p>
          ) : (
            <DeliveryActiveRunPanel target={TRADE_PROD_TARGET} collapsible />
          )}
          <LaneDetailCollapse
            key="prod-gate-detail"
            title="Gate check detail — STG vs Prod"
            summaryExtra={<LaneGateSummaryLine gate={prodGate.data} />}
            defaultOpen={prodGateStep.status === 'error'}
            showModeBadge
            bodyClassName="p-3"
          >
            <ReleaseGateCompareSection
              stgGate={stgGate.data}
              stgGateLoading={stgGate.isLoading}
              stgGateError={stgGate.error instanceof Error ? stgGate.error.message : null}
              prodGate={prodGate.data}
              prodGateLoading={prodGate.isLoading}
              prodGateError={prodGate.error instanceof Error ? prodGate.error.message : null}
            />
          </LaneDetailCollapse>
        </>
      )
      break
  }

  const evidenceLinks =
    onOpenSatelliteBus != null || onOpenObservability != null || onOpenApiHealth != null ? (
      <div className="flex min-w-0 flex-wrap items-center gap-1.5">
        <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
          Evidence
        </span>
        {onOpenSatelliteBus != null && (
          <Button size="xs" variant="ghost" onClick={onOpenSatelliteBus}>
            Bus Status
          </Button>
        )}
        {onOpenObservability != null && (
          <Button size="xs" variant="ghost" onClick={onOpenObservability}>
            Observability
          </Button>
        )}
        {onOpenApiHealth != null && (
          <Button size="xs" variant="ghost" onClick={onOpenApiHealth}>
            Satellite Health
          </Button>
        )}
      </div>
    ) : null

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiDeploy.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiDeploy.error.message}</p>
      )}
      {aiResolve.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiResolve.error.message}</p>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Satellite"
        actions={
          <div className="flex min-w-0 flex-wrap items-center justify-end gap-x-2 gap-y-1">
            <AgentTriggerButton
              className="shrink-0"
              label={AI_DEPLOY_LABEL}
              pending={aiDeploy.isPending}
              disabled={!deployDispatchAllowed}
              title={
                cycleTerminal && deployDispatchAllowed
                  ? 'Start the next release cycle with AI Deploy'
                  : (deployDisabledReason ?? AI_DEPLOY_LABEL)
              }
              onClick={handleAiDeployClick}
            />
            {showAiResolve && (
              <AgentTriggerButton
                className="shrink-0"
                label={AI_RESOLVE_LABEL}
                pending={aiResolve.isPending}
                active={resolveRunning}
                activeLabel="Expand dock"
                disabled={aiResolve.disabled && !resolveRunning}
                title={
                  resolveRunning
                    ? 'Expand Agent Execution Dock — live progress stays on this board'
                    : (aiResolve.disabledReason ?? AI_RESOLVE_TITLE)
                }
                onClick={handleAiResolveClick}
              />
            )}
            {evidenceLinks}
          </div>
        }
      >
        <TradeEnvAccessBar />
        <ReleaseStateBanner tier="trade" />
      </LaneStateStrip>

      <ConstellationStrip
        impact={constellationImpact}
        onFormationLaunch={() =>
          formation.requestLaunch({
            revision: deployRevision,
            tag: researchLiveTag,
            env: 'stg',
          })
        }
        formationPending={formation.isPending}
        formationDisabled={
          !canOperate ||
          hasDeliverInFlight(stgRuns.data?.runs) ||
          hasDeliverInFlight(prodRuns.data?.runs)
        }
        formationDisabledReason={
          !canOperate
            ? 'Authenticate to launch'
            : 'Deliver in flight'
        }
      />
      {formation.dialog}

      <LaneOperateSplit
        storageKey="bifrost.console.satelliteLaneOperateSplit"
        primary={
          <>
            <ReleaseStepCommandCenter
              steps={steps}
              activeIndex={activeIndex}
              onSelect={setActiveIndex}
              stepLabels={STEP_LABELS}
              stgRun={stgRun}
              prodRun={prodRun}
              stgGate={stgGate.data}
              prodGate={prodGate.data}
              renderStepActions={i => renderTradeStepActions(i, ambientJobId)}
              collapsibleBody
              agentDriven
              cycleTerminal={cycleTerminal}
              onStartNextRelease={handleStartNextRelease}
              nextCycleActive={nextCycle}
              aiReleasePending={aiDeploy.isPending}
              aiReleaseDisabled={!deployDispatchAllowed}
              aiReleaseDisabledReason={deployDisabledReason ?? undefined}
              aiReleaseLabel={AI_DEPLOY_LABEL}
            />
            <div className="flex flex-col gap-3">{stepDetail}</div>
          </>
        }
        support={
          <>
            <LaneDetailCollapse
              title="Release checklist"
              summaryExtra={
                <span className="inline-flex flex-wrap items-center gap-2">
                  <DenseTag
                    variant={
                      satelliteVerdict.kind === 'GO'
                        ? 'success'
                        : satelliteVerdict.kind === 'IN_FLIGHT'
                          ? 'warning'
                          : 'danger'
                    }
                  >
                    {satelliteVerdict.kind === 'GO'
                      ? 'GO'
                      : satelliteVerdict.kind === 'IN_FLIGHT'
                        ? 'IN FLIGHT'
                        : 'NO-GO'}
                  </DenseTag>
                  <DenseTag
                    variant={checklistOkCount === checklistTotal ? 'success' : 'warning'}
                  >
                    {checklistOkCount}/{checklistTotal} ready
                  </DenseTag>
                </span>
              }
              defaultOpen
              showModeBadge
              bodyClassName="flex flex-col gap-3 p-3"
            >
              <p className="m-0 text-dense-meta text-muted-foreground">
                AI Deploy stays disabled until every checkpoint is green (same gate as Mission
                Launch / Daily Ops Release). When NO-GO, use AI Resolve to clear release
                conditions first.
              </p>
              <LaunchGateBar
                layout="column"
                verdict={satelliteVerdict}
                checkpoints={satelliteCheckpoints}
                hidePrimaryLaunch
                onExpandAgentDock={onExpandAgentDock}
                onAgentFix={handleAiResolveClick}
                agentFixLabel={AI_RESOLVE_LABEL}
                agentFixPending={aiResolve.isPending}
                agentFixActive={
                  isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
                  ambientJobScope === tradeProdFixScope
                }
                agentFixDisabled={aiResolve.disabled}
                agentFixTitle={aiResolve.disabledReason ?? AI_RESOLVE_TITLE}
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Release cycle history"
              defaultOpen
              bodyClassName="p-0"
            >
              <ReleaseCycleHistorySection
                lane="trade"
                description="Full Trade STG → PROD release cycles from AI Deploy. Expand for stage detail; Copy for AI exports JSON for CI/CD analysis."
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Supporting evidence"
              summaryExtra={
                <SupplyChainSummaryLine
                  data={supplyChain.data}
                  isLoading={supplyChain.isLoading}
                />
              }
              defaultOpen={false}
              showModeBadge
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <SupplyChainPanel layout="operate" hideDeliverAction />
              <PlatformGateHistorySection
                stgTier="stg"
                prodTier="prod"
                description="Chronological log of Trade release gate runs."
                collapsible
                defaultCollapsed
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Toolbox"
              defaultOpen={false}
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  Advanced recovery
                </span>
                <p className="m-0 text-dense-meta text-muted-foreground">
                  Escape hatches for this lane (Argo sync / rollback). Primary AI Deploy stays on
                  the lane state strip above — Step detail is observe-only.
                </p>
                <GitOpsQuickActionsPanel
                  data={gitops.data}
                  isLoading={gitops.isLoading}
                  errorMessage={gitops.error instanceof Error ? gitops.error.message : null}
                />
              </div>
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  CI/CD pipeline topology · release workflow
                </span>
                <DeliveryReleaseWorkflowPanel context={context} stgSmoke={stgSmoke.data} />
                <DeliveryFlow context={context} gitops={gitops.data} />
              </div>
            </LaneDetailCollapse>
          </>
        }
      />
    </div>
  )
}
