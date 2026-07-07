import { useQuery } from '@tanstack/react-query'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { fetchPipelineRuns, fetchReleaseGate, fetchReleaseState } from '@/api/platform'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { DeployActionBar } from '@/components/delivery/DeployActionBar'
import { GateActionBar } from '@/components/delivery/GateActionBar'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import { ReleaseEnvAccessBar } from '@/components/delivery/ReleaseEnvAccessBar'
import { ReleaseHealthStrip } from '@/components/delivery/ReleaseHealthStrip'
import { SelfHealthPanel } from '@/components/architecture/SelfHealthPanel'
import { EscapeHatchPanel } from '@/components/architecture/EscapeHatchPanel'
import {
  ReleaseStepCommandCenter,
  runStepStatus,
  gateStepStatus,
  deriveReleaseOutcome,
  type FlowStep,
} from '@/components/delivery/ReleaseStepCommandCenter'
import { ReleaseStateBanner } from '@/components/delivery/ReleaseStateBanner'
import {
  PlatformGateHistorySection,
  PlatformStageGatePanel,
} from '@/components/promote/PlatformReleaseGateSection'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { useAmbientAgentTask } from '@/hooks/useAmbientAgentTask'
import type { AmbientAgentShellProps } from '@/lib/agent/ambientAgent'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  buildPlatformReleasePrompt,
  PLATFORM_RELEASE_SCOPE,
} from '@/lib/agent/platformReleaseAgentPrompt'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import { deriveReleaseIdentity } from '@/lib/delivery/releaseStepTypes'

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

export function PlatformReleasePage({
  ambientJobId,
  onStartAgentJob,
}: AmbientAgentShellProps = {}) {
  const { canOperate } = usePlatformAuth()
  const [activeIndex, setActiveIndex] = useState(0)

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
        <div className="page-section panel-elevated px-3 py-3">
          <PlatformStageGatePanel tier="platform-stg" label="STG" hideActions />
        </div>
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
        <div className="page-section panel-elevated px-3 py-3">
          <PlatformStageGatePanel tier="platform-prod" label="PROD" hideActions />
        </div>
      )
      break
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      {aiRelease.error != null && (
        <p className="m-0 text-dense-meta text-destructive">{aiRelease.error.message}</p>
      )}

      <div className="flex flex-col gap-2 rounded-lg border border-border/50 bg-secondary/30 px-4 py-2.5">
        <div className="flex flex-wrap items-center justify-between gap-x-6 gap-y-2">
          <ReleaseHealthStrip />
          <ReleaseEnvAccessBar />
        </div>
        <ReleaseStateBanner tier="platform" />
      </div>

      <SelfHealthPanel />

      <EscapeHatchPanel />

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
        onAiRelease={() => aiRelease.trigger()}
        aiReleasePending={aiRelease.isPending}
        aiReleaseDisabled={aiRelease.disabled}
        aiReleaseDisabledReason={aiRelease.disabledReason}
        aiReleaseLabel={AI_RELEASE_LABEL}
      />
      <div className="flex flex-col gap-4">{stepDetail}</div>
      <PlatformGateHistorySection />
    </div>
  )
}
