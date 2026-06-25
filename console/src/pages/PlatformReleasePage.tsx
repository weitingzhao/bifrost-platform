import { cn, DenseTag, StatusLamp } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { Check } from 'lucide-react'
import type { ReactNode } from 'react'
import { useState } from 'react'
import { fetchPipelineRuns, fetchReleaseGate, fetchSelfHealth } from '@/api/platform'
import type {
  DeliveryPipelineRunView,
  ReleaseGateResponse,
  SelfHealthProbe,
  SelfHealthProbeStatus,
} from '@/api/types'
import { DeliveryActiveRunPanel } from '@/components/delivery/DeliveryActiveRunPanel'
import { PlatformDeliverActuatePanel } from '@/components/delivery/PlatformDeliverActuatePanel'
import {
  PlatformGateHistorySection,
  PlatformStageGatePanel,
} from '@/components/promote/PlatformReleaseGateSection'
import { deliveryTargetById } from '@/lib/delivery/deliveryTargets'
import {
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

const PLATFORM_STG_TARGET = deliveryTargetById('platform-stg')
const PLATFORM_PROD_TARGET = deliveryTargetById('platform-prod')

// ---------------------------------------------------------------------------
// Health strip — compact inline status (not a full OpsSection)
// ---------------------------------------------------------------------------

const LAMP: Record<SelfHealthProbeStatus, 'ok' | 'degraded' | 'fail' | 'unknown'> = {
  ok: 'ok', degraded: 'degraded', fail: 'fail', unknown: 'unknown',
}
const TAG_VARIANT: Record<SelfHealthProbeStatus, 'success' | 'warning' | 'danger' | 'category'> = {
  ok: 'success', degraded: 'warning', fail: 'danger', unknown: 'category',
}
const CATEGORY_SHORT: Record<string, string> = {
  api: 'API', console: 'Console', gitops: 'Argo',
}

function ProbeIndicator({ probe }: { probe: SelfHealthProbe }) {
  return (
    <span className="inline-flex items-center gap-1">
      <StatusLamp value={LAMP[probe.status]} kind="reach" />
      <span>{CATEGORY_SHORT[probe.category] ?? probe.category}</span>
    </span>
  )
}

function HealthStrip() {
  const { data, isLoading } = useQuery({
    queryKey: ['platform', 'self-health'],
    queryFn: fetchSelfHealth,
    refetchInterval: 30_000,
  })

  const overall = data?.overall ?? 'unknown'
  const probes = data?.probes ?? []
  const stg = probes.filter(p => p.env === 'stg')
  const prod = probes.filter(p => p.env === 'prod')

  return (
    <div className="flex flex-wrap items-center gap-x-5 gap-y-2 rounded-lg bg-secondary px-4 py-2.5">
      <span className="inline-flex items-center gap-2 text-dense-label font-semibold">
        <StatusLamp value={LAMP[overall]} kind="reach" />
        Platform Health
        {isLoading
          ? <DenseTag variant="category">…</DenseTag>
          : <DenseTag variant={TAG_VARIANT[overall]}>{overall}</DenseTag>}
      </span>

      {stg.length > 0 && (
        <span className="inline-flex items-center gap-2 text-dense-meta text-muted-foreground">
          <span className="font-medium text-foreground">STG</span>
          {stg.map(p => <ProbeIndicator key={p.id} probe={p} />)}
        </span>
      )}
      {prod.length > 0 && (
        <span className="inline-flex items-center gap-2 text-dense-meta text-muted-foreground">
          <span className="font-medium text-foreground">PROD</span>
          {prod.map(p => <ProbeIndicator key={p.id} probe={p} />)}
        </span>
      )}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Flow stepper — top-of-page release pipeline navigation
// ---------------------------------------------------------------------------

type StepStatus = 'done' | 'active' | 'pending' | 'error'

interface FlowStep {
  key: string
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
}

function runStepStatus(run: DeliveryPipelineRunView | undefined): { status: StepStatus; label: string } {
  if (run == null) return { status: 'pending', label: 'Not started' }
  if (isPipelineRunSucceeded(run)) return { status: 'done', label: 'Deployed' }
  if (isPipelineRunRunning(run)) return { status: 'active', label: 'Running…' }
  if (isPipelineRunFailed(run)) return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Pending' }
}

function gateStepStatus(gate: ReleaseGateResponse | undefined): { status: StepStatus; label: string } {
  const result = gate?.result ?? ''
  if (result === 'pass') return { status: 'done', label: 'Passed' }
  if (result === 'fail') return { status: 'error', label: 'Failed' }
  return { status: 'pending', label: 'Not run' }
}

const STEP_CIRCLE: Record<StepStatus, string> = {
  done: 'bg-success text-white',
  active: 'bg-primary text-primary-foreground ring-2 ring-primary/30',
  error: 'bg-destructive text-white',
  pending: 'bg-muted text-muted-foreground',
}

const STEP_STATUS_TEXT: Record<StepStatus, string> = {
  done: 'text-success',
  active: 'text-primary',
  error: 'text-destructive',
  pending: 'text-muted-foreground',
}

function FlowStepper({
  steps,
  activeIndex,
  onSelect,
}: {
  steps: FlowStep[]
  activeIndex: number
  onSelect: (index: number) => void
}) {
  return (
    <div className="flex items-stretch gap-0 overflow-x-auto rounded-lg border border-border bg-card p-2">
      {steps.map((step, i) => {
        const isActive = i === activeIndex
        const connectorDone = i > 0 && steps[i - 1].status === 'done'
        return (
          <div key={step.key} className="flex min-w-0 flex-1 items-center">
            {i > 0 && (
              <div
                className={cn(
                  'h-px flex-1 shrink-0',
                  connectorDone ? 'bg-success' : 'bg-border',
                )}
              />
            )}
            <button
              type="button"
              onClick={() => onSelect(i)}
              className={cn(
                'flex min-w-0 flex-col items-center gap-1 rounded-md px-3 py-2 transition-colors',
                isActive ? 'bg-secondary' : 'hover:bg-secondary/60',
              )}
            >
              <span className="inline-flex items-center gap-2">
                <span
                  className={cn(
                    'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[11px] font-bold leading-none',
                    STEP_CIRCLE[step.status],
                  )}
                >
                  {step.status === 'done' ? <Check className="h-3.5 w-3.5" /> : i + 1}
                </span>
                <span className="flex flex-col items-start leading-tight">
                  <span className={cn('text-dense-label font-medium', isActive ? 'text-foreground' : 'text-muted-foreground')}>
                    {step.label}
                  </span>
                  <span className={cn('text-dense-caption', STEP_STATUS_TEXT[step.status])}>
                    {step.statusLabel}
                  </span>
                </span>
              </span>
            </button>
          </div>
        )
      })}
    </div>
  )
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

const STG_PIPELINE = PLATFORM_STG_TARGET.pipeline
const PROD_PIPELINE = PLATFORM_PROD_TARGET.pipeline

export function PlatformReleasePage() {
  const [activeIndex, setActiveIndex] = useState(0)

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

  let stepContent: ReactNode
  switch (activeIndex) {
    case 0:
      stepContent = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_STG_TARGET} />
          <DeliveryActiveRunPanel target={PLATFORM_STG_TARGET} />
        </>
      )
      break
    case 1:
      stepContent = <PlatformStageGatePanelCard tier="platform-stg" label="STG" />
      break
    case 2:
      stepContent = (
        <>
          <PlatformDeliverActuatePanel target={PLATFORM_PROD_TARGET} />
          <DeliveryActiveRunPanel target={PLATFORM_PROD_TARGET} />
        </>
      )
      break
    default:
      stepContent = <PlatformStageGatePanelCard tier="platform-prod" label="PROD" />
      break
  }

  return (
    <div className="flex w-full min-w-0 flex-col gap-4">
      <HealthStrip />

      <FlowStepper steps={steps} activeIndex={activeIndex} onSelect={setActiveIndex} />

      <div className="flex flex-col gap-4">{stepContent}</div>

      <PlatformGateHistorySection />
    </div>
  )
}

// Gate panel is a bare div; wrap it in an elevated card so it reads as a step surface.
function PlatformStageGatePanelCard({ tier, label }: { tier: 'platform-stg' | 'platform-prod'; label: string }) {
  return (
    <div className="page-section panel-elevated px-3 py-3">
      <PlatformStageGatePanel tier={tier} label={label} />
    </div>
  )
}
