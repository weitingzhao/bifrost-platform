import { useMemo } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Button, DenseTag, PageHeader } from '@bifrost/ui'
import {
  fetchPipelineRuns,
  fetchReleaseGate,
  fetchSupplyChain,
  fetchStgSmoke,
} from '@/api/platform'
import type { MatrixResponse, OpsContextResponse } from '@/api/types'
import { OpsTaskStrips } from '@/components/task-mode/OpsTaskStrips'
import { DevTaskStrips } from '@/components/task-mode/DevTaskStrips'
import { TaskPhaseProgress } from '@/components/task-mode/TaskPhaseProgress'
import { OpsFeedback } from '@/components/feedback/OpsFeedback'
import { useDevProgramInstance } from '@/hooks/useDevProgramInstance'
import { useMissionSnapshot } from '@/hooks/useMissionSnapshot'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { PLATFORM_RELEASE_AGENT_PROMPT } from '@/lib/control-room/controlRoomOperatePack'
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
    const platformRun = platformRunsQ.data?.runs?.[0]
    const tradeRun = tradeRunsQ.data?.runs?.[0]
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
      tradeStgGate: tradeGateQ.data,
      tradeStgSmokeOk: smokeQ.data?.reachability === 'ok',
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

  const doneCount = phases.filter((p: TaskPhaseDef) => statuses[p.id] === 'done').length
  const loopLabel =
    mode.loopArchetype === 'ops' ? 'Ops loop' : mode.loopArchetype === 'dev' ? 'Dev loop' : 'System'

  const handleNavigatePhase = (phase: TaskPhaseDef) => {
    if (phase.navigateTab != null) onNavigate(phase.navigateTab)
  }

  const showLaunchPad = mode.ops?.showLaunchPad === true
  const resolvedProgramId = devProgram.programId ?? mode.dev?.programId

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

      {phases.length > 0 && (
        <div className="rounded-lg border border-border bg-card px-3 py-2">
          <div className="mb-2 flex flex-wrap items-center gap-2">
            <span className="text-[var(--text-dense-label)] font-semibold">Phase progress</span>
            <DenseTag variant="neutral">
              {doneCount}/{phases.length} complete
            </DenseTag>
          </div>
          <TaskPhaseProgress
            phases={phases}
            statuses={statuses}
            onNavigatePhase={handleNavigatePhase}
          />
        </div>
      )}

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
        </>
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
          canDispatchRelease={showLaunchPad && !aiRelease.disabled}
          canDispatchTradeDeploy={showLaunchPad && !aiTradeDeploy.disabled}
          releaseDisabledReason={aiRelease.disabledReason}
          tradeDeployDisabledReason={aiTradeDeploy.disabledReason}
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
