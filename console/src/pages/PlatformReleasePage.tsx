import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useEffect, useMemo, useRef, useState } from 'react'
import { fetchCluster, fetchClusterServiceReadiness } from '@/api/cluster'
import { fetchPipelineRuns, fetchSupplyChain } from '@/api/delivery'
import { fetchStackAddons } from '@/api/stack'
import { fetchReleaseGate, fetchReleaseState, fetchStgSmoke } from '@/api/promote'
import { DenseTag } from '@bifrost/ui'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeployActionBar } from '@/components/delivery/DeployActionBar'
import { GateActionBar } from '@/components/delivery/GateActionBar'
import {
  LaneDetailCollapse,
  LaneDetailContextStrip,
  LaneGateSummaryLine,
  LaneStateStrip,
} from '@/components/delivery/LaneDetailShell'
import { LaneOperateSplit } from '@/components/delivery/LaneOperateSplit'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { ReleaseEnvAccessBar } from '@/components/delivery/ReleaseEnvAccessBar'
import { ReleaseHealthStrip } from '@/components/delivery/ReleaseHealthStrip'
import { SelfHealthPanel } from '@/components/architecture/SelfHealthPanel'
import { EscapeHatchPanel } from '@/components/architecture/EscapeHatchPanel'
import { ReleaseStepCommandCenter } from '@/components/delivery/ReleaseStepCommandCenter'
import { LaunchGateBar } from '@/components/task-mode/LaunchGateBar'
import {
  usePromoteVerifyReadiness,
  useRocketProdReadiness,
} from '@/components/task-mode/readiness/hooks'
import {
  runStepStatus,
  gateStepStatus,
  deriveReleaseOutcome,
  pickDeployPipelineRun,
  isReleaseCycleTerminal,
  deriveReleaseIdentity,
  type FlowStep,
} from '@/lib/delivery/releaseStepTypes'
import { ReleaseStateBanner } from '@/components/delivery/ReleaseStateBanner'
import { StackInstallWizardPanel } from '@/components/delivery/StackInstallWizardPanel'
import {
  PlatformGateHistorySection,
  PlatformStageGatePanel,
} from '@/components/promote/PlatformReleaseGateSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { useLaneStepFocus } from '@/hooks/useLaneStepFocus'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { isAmbientAgentActive } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildPlatformReleasePrompt,
  PLATFORM_RELEASE_SCOPE,
} from '@/lib/agent/platformReleaseAgentPrompt'
import {
  buildPlatformProdFixPrompt,
  pickFixScope,
  PROD_ENV_FIX_SCOPE,
} from '@/lib/agent/prodEnvironmentFixPrompt'
import {
  buildClusterPackBody,
  buildDispatchedFixPrompt,
} from '@/lib/agent/readinessFixDispatch'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import { isPipelineRunRunning } from '@/lib/delivery/pipelineRunAskPack'
import { stackNeedsOperatePanel } from '@/lib/delivery/stackWizard'
import { missionStatus } from '@/lib/control-room/missionSignals'
import {
  buildLaunchCheckpoints,
  hasDeliverInFlight,
  resolveLaunchVerdict,
} from '@/lib/task-mode/satelliteLaunchVerdict'

const AI_RELEASE_LABEL = 'AI Release'
const AI_RELEASE_TASK_LABEL = scopeToLabel(PLATFORM_RELEASE_SCOPE)
const AI_RESOLVE_LABEL = 'AI Resolve'
const AI_RESOLVE_TITLE =
  'AI Resolve release conditions — clear NO-GO checkpoints before AI Release'

const PLATFORM_STG_TARGET = deliveryTargetById('platform-stg')
const PLATFORM_PROD_TARGET = deliveryTargetById('platform-prod')
const STG_PIPELINE = PLATFORM_STG_TARGET.pipeline
const PROD_PIPELINE = PLATFORM_PROD_TARGET.pipeline

const STEP_LABELS = ['Staging Deploy', 'Staging Gate', 'Production Deploy', 'Production Gate'] as const

function renderPlatformStepActions(activeIndex: number) {
  switch (activeIndex) {
    case 0:
      return <DeployActionBar target={PLATFORM_STG_TARGET} releaseStateTier="platform" />
    case 1:
      return <GateActionBar tier="platform-stg" label="STG" />
    case 2:
      return <DeployActionBar target={PLATFORM_PROD_TARGET} releaseStateTier="platform" />
    default:
      return <GateActionBar tier="platform-prod" label="PROD" />
  }
}

type PlatformReleasePageProps = AmbientAgentShellProps

export function PlatformReleasePage({
  ambientJobId,
  ambientJobScope,
  ambientJobStatus,
  onStartAgentJob,
  onExpandAgentDock,
}: PlatformReleasePageProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)
  const [nextCycle, setNextCycle] = useState(false)
  const nextCycleAdvancedRef = useRef(false)

  const rocketProd = useRocketProdReadiness(true)
  const promoteVerify = usePromoteVerifyReadiness(true)

  const releaseStateQuery = useQuery({
    queryKey: ['promote', 'release-state', 'platform'],
    queryFn: () => fetchReleaseState('platform'),
    refetchInterval: 30_000,
  })

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
    queryKey: ['promote', 'release-gate', 'platform-stg'],
    queryFn: () => fetchReleaseGate('platform-stg'),
    refetchInterval: 30_000,
  })
  const prodGate = useQuery({
    queryKey: ['promote', 'release-gate', 'platform-prod'],
    queryFn: () => fetchReleaseGate('platform-prod'),
    refetchInterval: 30_000,
  })
  const stackQuery = useQuery({
    queryKey: ['stack', 'addons'],
    queryFn: fetchStackAddons,
    refetchInterval: 30_000,
  })

  const stgGatePassed = stgGate.data?.result === 'pass'
  const prodGatePassed = prodGate.data?.result === 'pass'
  const latestStgRun = stgRuns.data?.runs?.[0]
  const latestProdRun = prodRuns.data?.runs?.[0]

  const stgRun = nextCycle
    ? latestStgRun
    : pickDeployPipelineRun(stgRuns.data?.runs, { gatePassed: stgGatePassed })
  const prodRun = nextCycle
    ? latestProdRun
    : pickDeployPipelineRun(prodRuns.data?.runs, { gatePassed: prodGatePassed })

  const stgDeploy = runStepStatus(stgRun)
  const prodDeploy = runStepStatus(prodRun)
  const stgGateStep = gateStepStatus(stgGate.data)
  const prodGateStep = gateStepStatus(prodGate.data)

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

  const releaseIdentity = deriveReleaseIdentity(stgRun, prodRun, stgGate.data, prodGate.data)
  const releaseOutcome = deriveReleaseOutcome(steps)
  const cycleTerminal = isReleaseCycleTerminal(releaseOutcome, nextCycle)

  const rocketVerdictInput = useMemo(
    () => ({
      mode: 'rocket' as const,
      canOperate,
      prodBlocked: rocketProd.prodBlocked,
      tradeProdLabel: missionStatus(rocketProd.prodOverall),
      tradeProdSignal: rocketProd.prodOverall,
      promoteSignal: promoteVerify.promoteSignal,
      promoteDetail: promoteVerify.promoteDetail,
      deliverInFlight:
        hasDeliverInFlight(stgRuns.data?.runs) || hasDeliverInFlight(prodRuns.data?.runs),
      agentInFlight:
        isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
        ambientJobScope === PLATFORM_RELEASE_SCOPE,
    }),
    [
      canOperate,
      rocketProd.prodBlocked,
      rocketProd.prodOverall,
      promoteVerify.promoteSignal,
      promoteVerify.promoteDetail,
      stgRuns.data?.runs,
      prodRuns.data?.runs,
      ambientJobId,
      ambientJobScope,
      ambientJobStatus,
    ],
  )
  const rocketVerdict = useMemo(
    () => resolveLaunchVerdict(rocketVerdictInput),
    [rocketVerdictInput],
  )
  const rocketCheckpoints = useMemo(
    () => buildLaunchCheckpoints(rocketVerdictInput),
    [rocketVerdictInput],
  )
  const checklistOkCount = rocketCheckpoints.filter(c => c.ok).length
  const checklistTotal = rocketCheckpoints.length

  const platformProdFixScope = pickFixScope(rocketProd.fixSignals ?? [])
  const platformProdFixLabel = scopeToLabel(platformProdFixScope)

  const aiRelease = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLATFORM_RELEASE_SCOPE,
    label: AI_RELEASE_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildPlatformReleasePrompt({
        releaseState: releaseStateQuery.data,
        stgRun,
        prodRun,
        stgGate: stgGate.data,
        prodGate: prodGate.data,
        outcomeKind: releaseOutcome.kind,
        outcomeDetail: releaseOutcome.detail,
        activeRevision: releaseIdentity.revision,
      }),
    }),
  })

  const aiResolve = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: platformProdFixScope,
    label: platformProdFixLabel,
    buildRequest: async () => {
      const signals = rocketProd.fixSignals ?? []
      const scope = pickFixScope(signals)
      const [cluster, serviceReadiness, supply, smoke] = await Promise.all([
        fetchCluster(),
        fetchClusterServiceReadiness(),
        fetchSupplyChain(),
        fetchStgSmoke(),
      ])
      const pack = buildClusterPackBody({ cluster, serviceReadiness })
      const fallback = buildPlatformProdFixPrompt({
        prodOverall: rocketProd.prodOverall,
        namespace: rocketProd.prodNamespace ?? 'bifrost-platform-prod',
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
            pipeline: 'bifrost-deliver-platform',
          },
        }),
        ...(scope === PROD_ENV_FIX_SCOPE ? pack : {}),
      }
    },
  })

  const releaseDispatchAllowed = !aiRelease.disabled && rocketVerdict.kind === 'GO'
  const releaseDisabledReason =
    rocketVerdict.kind !== 'GO'
      ? (rocketVerdict.disabledReason ?? rocketVerdict.detail)
      : aiRelease.disabledReason

  const showAiResolve =
    rocketVerdict.kind === 'NO_GO' && rocketVerdict.blockKind !== 'auth'
  const resolveRunning =
    isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
    ambientJobScope === platformProdFixScope &&
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
      nextCycleAdvancedRef.current = false
      return
    }
    if (releaseOutcome.kind === 'in_progress' || releaseOutcome.kind === 'failed') {
      nextCycleAdvancedRef.current = true
    }
    if (nextCycleAdvancedRef.current && releaseOutcome.kind === 'released') {
      setNextCycle(false)
      nextCycleAdvancedRef.current = false
    }
  }, [nextCycle, releaseOutcome.kind])

  useEffect(() => {
    if (!nextCycle) return
    if (latestStgRun != null && isPipelineRunRunning(latestStgRun)) {
      nextCycleAdvancedRef.current = true
    }
  }, [nextCycle, latestStgRun])

  const handleStartNextRelease = () => {
    if (!releaseDispatchAllowed) return
    setNextCycle(true)
    nextCycleAdvancedRef.current = false
    setActiveIndex(0)
    aiRelease.trigger()
  }

  const handleAiReleaseClick = () => {
    if (cycleTerminal) {
      handleStartNextRelease()
      return
    }
    if (!releaseDispatchAllowed) return
    aiRelease.trigger()
  }

  const stackNeedsOperate = stackNeedsOperatePanel(stackQuery.data?.addons ?? [])

  let stepDetail: ReactNode
  switch (activeIndex) {
    case 0:
      stepDetail = (
        <>
          <DeliveryActiveRunPanel target={PLATFORM_STG_TARGET} collapsible />
          <LaneDetailCollapse
            title="Deliver readiness · Platform STG"
            defaultOpen={false}
            bodyClassName="p-3"
          >
            <PlatformDeliverActuatePanel target={PLATFORM_STG_TARGET} hideActions />
          </LaneDetailCollapse>
        </>
      )
      break
    case 1:
      stepDetail = (
        <>
          <DeliveryActiveRunPanel target={PLATFORM_STG_TARGET} collapsible />
          <LaneDetailCollapse
            key="stg-gate-detail"
            title="STG gate check detail"
            summaryExtra={<LaneGateSummaryLine gate={stgGate.data} />}
            defaultOpen={stgGateStep.status === 'error'}
            bodyClassName="p-3"
          >
            <PlatformStageGatePanel tier="platform-stg" label="STG" hideActions />
          </LaneDetailCollapse>
        </>
      )
      break
    case 2:
      stepDetail = (
        <>
          <DeliveryActiveRunPanel target={PLATFORM_PROD_TARGET} collapsible />
          <LaneDetailCollapse
            title="Deliver readiness · Platform PROD"
            defaultOpen={false}
            bodyClassName="p-3"
          >
            <PlatformDeliverActuatePanel target={PLATFORM_PROD_TARGET} hideActions />
          </LaneDetailCollapse>
        </>
      )
      break
    default:
      stepDetail = (
        <>
          <DeliveryActiveRunPanel target={PLATFORM_PROD_TARGET} collapsible />
          <LaneDetailCollapse
            key="prod-gate-detail"
            title="PROD gate check detail"
            summaryExtra={<LaneGateSummaryLine gate={prodGate.data} />}
            defaultOpen={prodGateStep.status === 'error'}
            bodyClassName="p-3"
          >
            <PlatformStageGatePanel tier="platform-prod" label="PROD" hideActions />
          </LaneDetailCollapse>
        </>
      )
      break
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiRelease.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiRelease.error.message}</p>
      )}
      {aiResolve.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiResolve.error.message}</p>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Rocket"
        actions={
          <div className="flex flex-wrap items-center gap-2">
            <AgentTriggerButton
              label={AI_RELEASE_LABEL}
              pending={aiRelease.isPending}
              disabled={!releaseDispatchAllowed}
              title={
                cycleTerminal && releaseDispatchAllowed
                  ? 'Start the next release cycle with AI Release'
                  : (releaseDisabledReason ?? AI_RELEASE_LABEL)
              }
              onClick={handleAiReleaseClick}
            />
            {showAiResolve && (
              <AgentTriggerButton
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
          </div>
        }
      >
        <ReleaseEnvAccessBar />
        <ReleaseStateBanner tier="platform" />
      </LaneStateStrip>

      <LaneOperateSplit
        storageKey="bifrost.console.rocketLaneOperateSplit"
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
              renderStepActions={renderPlatformStepActions}
              collapsibleBody
              agentDriven
              cycleTerminal={cycleTerminal}
              onStartNextRelease={handleStartNextRelease}
              nextCycleActive={nextCycle}
              aiReleasePending={aiRelease.isPending}
              aiReleaseDisabled={!releaseDispatchAllowed}
              aiReleaseDisabledReason={releaseDisabledReason ?? undefined}
              aiReleaseLabel={AI_RELEASE_LABEL}
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
                      rocketVerdict.kind === 'GO'
                        ? 'success'
                        : rocketVerdict.kind === 'IN_FLIGHT'
                          ? 'warning'
                          : 'danger'
                    }
                  >
                    {rocketVerdict.kind === 'GO'
                      ? 'GO'
                      : rocketVerdict.kind === 'IN_FLIGHT'
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
                AI Release stays disabled until every checkpoint is green (same gate as Mission
                Launch / Daily Ops Release). When NO-GO, use AI Resolve to clear release
                conditions first.
              </p>
              <LaunchGateBar
                layout="column"
                verdict={rocketVerdict}
                checkpoints={rocketCheckpoints}
                hidePrimaryLaunch
                onExpandAgentDock={onExpandAgentDock}
                onAgentFix={handleAiResolveClick}
                agentFixLabel={AI_RESOLVE_LABEL}
                agentFixPending={aiResolve.isPending}
                agentFixActive={
                  isAmbientAgentActive(ambientJobId, ambientJobStatus) &&
                  ambientJobScope === platformProdFixScope
                }
                agentFixDisabled={aiResolve.disabled}
                agentFixTitle={aiResolve.disabledReason ?? AI_RESOLVE_TITLE}
              />
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Supporting evidence"
              summaryExtra={<ReleaseHealthStrip />}
              defaultOpen={false}
              showModeBadge
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <div className="flex flex-col gap-2">
                <SelfHealthPanel collapsible />
                <PlatformGateHistorySection collapsible defaultCollapsed />
              </div>
            </LaneDetailCollapse>

            <LaneDetailCollapse
              title="Toolbox"
              defaultOpen={stackNeedsOperate}
              bodyClassName="flex flex-col gap-4 p-3"
            >
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  Advanced recovery
                </span>
                <p className="m-0 text-dense-meta text-muted-foreground">
                  Escape hatches for this lane. Primary AI Release stays on the lane state strip
                  above.
                </p>
                <EscapeHatchPanel />
              </div>
              <div className="flex flex-col gap-2">
                <span className="text-dense-micro font-semibold uppercase tracking-wider text-muted-foreground/70">
                  CI/CD stack · install wizard
                </span>
                <StackInstallWizardPanel
                  data={stackQuery.data}
                  isLoading={stackQuery.isLoading}
                  errorMessage={
                    stackQuery.error instanceof Error ? stackQuery.error.message : null
                  }
                  layout="operate"
                />
              </div>
            </LaneDetailCollapse>
          </>
        }
      />
    </div>
  )
}
