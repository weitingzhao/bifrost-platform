import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { fetchPipelineRuns } from '@/api/delivery'
import { fetchStackAddons } from '@/api/stack'
import { fetchReleaseGate, fetchReleaseState } from '@/api/promote'
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
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { ReleaseEnvAccessBar } from '@/components/delivery/ReleaseEnvAccessBar'
import { ReleaseHealthStrip } from '@/components/delivery/ReleaseHealthStrip'
import { SelfHealthPanel } from '@/components/architecture/SelfHealthPanel'
import { EscapeHatchPanel } from '@/components/architecture/EscapeHatchPanel'
import { ReleaseStepCommandCenter } from '@/components/delivery/ReleaseStepCommandCenter'
import {
  runStepStatus,
  gateStepStatus,
  deriveReleaseOutcome,
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
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildPlatformReleasePrompt,
  PLATFORM_RELEASE_SCOPE,
} from '@/lib/agent/platformReleaseAgentPrompt'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { readLaneDetailReasonFromLocation } from '@/lib/delivery/laneDetailContext'
import { deriveReleaseIdentity } from '@/lib/delivery/releaseStepTypes'
import { stackNeedsOperatePanel } from '@/lib/delivery/stackWizard'

const AI_RELEASE_LABEL = 'AI Release'
const AI_RELEASE_TASK_LABEL = scopeToLabel(PLATFORM_RELEASE_SCOPE)

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
  onStartAgentJob,
}: PlatformReleasePageProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [detailReason] = useState(readLaneDetailReasonFromLocation)

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

  const stgDeploy = runStepStatus(stgRuns.data?.runs?.[0])
  const prodDeploy = runStepStatus(prodRuns.data?.runs?.[0])
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

  const releaseIdentity = deriveReleaseIdentity(
    stgRuns.data?.runs?.[0],
    prodRuns.data?.runs?.[0],
    stgGate.data,
    prodGate.data,
  )
  const releaseOutcome = deriveReleaseOutcome(steps)

  const aiRelease = useAmbientAgentTask({
    canOperate,
    ambientJobId,
    onStartAgentJob,
    scope: PLATFORM_RELEASE_SCOPE,
    label: AI_RELEASE_TASK_LABEL,
    buildRequest: () => ({
      prompt: buildPlatformReleasePrompt({
        releaseState: releaseStateQuery.data,
        stgRun: stgRuns.data?.runs?.[0],
        prodRun: prodRuns.data?.runs?.[0],
        stgGate: stgGate.data,
        prodGate: prodGate.data,
        outcomeKind: releaseOutcome.kind,
        outcomeDetail: releaseOutcome.detail,
        activeRevision: releaseIdentity.revision,
      }),
    }),
  })

  let stepDetail: ReactNode
  switch (activeIndex) {
    case 0:
      stepDetail = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_STG_TARGET} hideActions />
          <DeliveryActiveRunPanel target={PLATFORM_STG_TARGET} />
        </>
      )
      break
    case 1:
      stepDetail = (
        <LaneDetailCollapse
          key="stg-gate-detail"
          title="STG gate check detail"
          summaryExtra={<LaneGateSummaryLine gate={stgGate.data} />}
          defaultOpen={stgGateStep.status === 'error'}
          bodyClassName="p-3"
        >
          <PlatformStageGatePanel tier="platform-stg" label="STG" hideActions />
        </LaneDetailCollapse>
      )
      break
    case 2:
      stepDetail = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_PROD_TARGET} hideActions />
          <DeliveryActiveRunPanel target={PLATFORM_PROD_TARGET} />
        </>
      )
      break
    default:
      stepDetail = (
        <LaneDetailCollapse
          key="prod-gate-detail"
          title="PROD gate check detail"
          summaryExtra={<LaneGateSummaryLine gate={prodGate.data} />}
          defaultOpen={prodGateStep.status === 'error'}
          bodyClassName="p-3"
        >
          <PlatformStageGatePanel tier="platform-prod" label="PROD" hideActions />
        </LaneDetailCollapse>
      )
      break
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-3">
      {aiRelease.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiRelease.error.message}</p>
      )}

      <LaneDetailContextStrip reason={detailReason} />

      <LaneStateStrip
        laneLabel="Rocket"
        actions={
          <AgentTriggerButton
            label={AI_RELEASE_LABEL}
            pending={aiRelease.isPending}
            disabled={aiRelease.disabled}
            title={aiRelease.disabledReason ?? AI_RELEASE_LABEL}
            onClick={() => aiRelease.trigger()}
          />
        }
      >
        <ReleaseEnvAccessBar />
        <ReleaseStateBanner tier="platform" />
      </LaneStateStrip>

      <ReleaseStepCommandCenter
        steps={steps}
        activeIndex={activeIndex}
        onSelect={setActiveIndex}
        stepLabels={STEP_LABELS}
        stgRun={stgRuns.data?.runs?.[0]}
        prodRun={prodRuns.data?.runs?.[0]}
        stgGate={stgGate.data}
        prodGate={prodGate.data}
        renderStepActions={renderPlatformStepActions}
      />

      <div className="flex flex-col gap-3">{stepDetail}</div>

      <LaneDetailCollapse
        title="Evidence · control plane self-health"
        summaryExtra={<ReleaseHealthStrip />}
        bodyClassName="p-3"
      >
        <SelfHealthPanel />
      </LaneDetailCollapse>

      <LaneDetailCollapse title="Advanced recovery" bodyClassName="flex flex-col gap-3 p-3">
        <p className="m-0 text-dense-meta text-muted-foreground">
          Escape hatches for this lane. Primary Agent Launch lives in Mission Launch TCC; AI Release
          is on the lane state strip above.
        </p>
        <EscapeHatchPanel />
      </LaneDetailCollapse>

      <LaneDetailCollapse title="Audit · gate run history">
        <PlatformGateHistorySection />
      </LaneDetailCollapse>

      <LaneDetailCollapse
        title="CI/CD stack · install wizard"
        defaultOpen={stackNeedsOperatePanel(stackQuery.data?.addons ?? [])}
        bodyClassName="p-3"
      >
        <StackInstallWizardPanel
          data={stackQuery.data}
          isLoading={stackQuery.isLoading}
          errorMessage={stackQuery.error instanceof Error ? stackQuery.error.message : null}
          layout="operate"
        />
      </LaneDetailCollapse>
    </div>
  )
}
