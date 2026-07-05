import { Button, cn } from '@bifrost/ui'
import {
  ArrowRight,
  Check,
  CheckCircle2,
  ChevronDown,
  Circle,
  Loader2,
  XCircle,
} from 'lucide-react'
import type { ReactNode } from 'react'
import type { DeliveryPipelineRunView, ReleaseGateResponse } from '@/api/types'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { DeployStepSummary, GateStepSummary } from '@/components/delivery/ReleaseStepSummaries'
import {
  deriveReleaseIdentity,
  deriveReleaseOutcome,
  stepRevisionForIndex,
  type FlowStep,
  type StepStatus,
} from '@/lib/delivery/releaseStepTypes'

export type { FlowStep, StepStatus } from '@/lib/delivery/releaseStepTypes'
export { runStepStatus, gateStepStatus, deriveReleaseOutcome } from '@/lib/delivery/releaseStepTypes'

const RELEASE_OUTCOME_BADGE: Record<string, string> = {
  released: 'border-success/40 bg-success/10 text-success',
  in_progress: 'border-primary/40 bg-primary/10 text-primary',
  failed: 'border-destructive/40 bg-destructive/10 text-destructive',
  idle: 'border-border bg-secondary/40 text-muted-foreground',
}

const STEP_CIRCLE: Record<StepStatus, string> = {
  done: 'border-2 border-success/40 text-success/60 bg-transparent',
  active: 'bg-primary text-primary-foreground shadow-[0_0_0_3px_rgba(var(--primary-rgb,59,130,246),0.15)]',
  error: 'border-2 border-destructive text-destructive bg-transparent',
  pending: 'border-2 border-border text-muted-foreground/40 bg-transparent',
}
const STEP_STATUS_TEXT: Record<StepStatus, string> = {
  done: 'text-muted-foreground/60',
  active: 'text-primary font-medium',
  error: 'text-destructive',
  pending: 'text-muted-foreground/40',
}

const STEP_BANNER_CONFIG: Record<StepStatus, { textClass: string; prefix: string }> = {
  done: { textClass: 'text-success', prefix: '' },
  active: { textClass: 'text-primary', prefix: '' },
  error: { textClass: 'text-destructive', prefix: '' },
  pending: { textClass: 'text-muted-foreground', prefix: 'Ready · ' },
}

export function FlowStepper({
  steps,
  activeIndex,
  onSelect,
}: {
  steps: FlowStep[]
  activeIndex: number
  onSelect: (i: number) => void
}) {
  return (
    <div className="flex items-center gap-0 overflow-x-auto px-4 py-3">
      {steps.map((step, i) => {
        const isActive = i === activeIndex
        const connectorDone = i > 0 && steps[i - 1].status === 'done'
        return (
          <div key={step.key} className="flex min-w-0 flex-1 items-center">
            {i > 0 && (
              <div
                className={cn(
                  'h-px flex-1 shrink-0 transition-colors',
                  i === 2 ? 'mx-2' : '',
                  connectorDone ? 'bg-success/30' : 'bg-border',
                )}
              />
            )}
            <button
              type="button"
              onClick={() => onSelect(i)}
              className={cn(
                'group flex min-w-0 items-center gap-2 rounded-md px-2.5 py-1.5 transition-all',
                isActive ? 'bg-primary/5' : 'hover:bg-secondary/50',
              )}
            >
              <span
                className={cn(
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold leading-none transition-all',
                  STEP_CIRCLE[step.status],
                )}
              >
                {step.status === 'done' ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <span className="flex flex-col items-start leading-tight">
                <span
                  className={cn(
                    'text-dense-caption transition-colors',
                    isActive ? 'font-semibold text-foreground' : 'text-muted-foreground group-hover:text-foreground',
                  )}
                >
                  {step.label}
                </span>
                <span className={cn('text-dense-micro', STEP_STATUS_TEXT[step.status])}>
                  {step.statusLabel}
                </span>
              </span>
            </button>
          </div>
        )
      })}
    </div>
  )
}

function ReleaseIdentityHeader({
  steps,
  stgRun,
  prodRun,
  stgGate,
  prodGate,
  onAiRelease,
  aiReleasePending = false,
  aiReleaseDisabled = false,
  aiReleaseDisabledReason,
  aiReleaseLabel = 'AI Release',
}: {
  steps: FlowStep[]
  stgRun: DeliveryPipelineRunView | undefined
  prodRun: DeliveryPipelineRunView | undefined
  stgGate: ReleaseGateResponse | undefined
  prodGate: ReleaseGateResponse | undefined
  onAiRelease?: () => void
  aiReleasePending?: boolean
  aiReleaseDisabled?: boolean
  aiReleaseDisabledReason?: string
  aiReleaseLabel?: string
}) {
  const identity = deriveReleaseIdentity(stgRun, prodRun, stgGate, prodGate)
  const outcome = deriveReleaseOutcome(steps)

  return (
    <div className="release-cc__identity border-b border-border px-4 py-3">
      <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
        <div className="flex min-w-0 items-center gap-3">
          <span className="shrink-0 text-dense-label font-semibold uppercase tracking-wider text-muted-foreground">
            Release
          </span>
          {identity.revision != null ? (
            <span className="truncate font-mono text-[15px] font-semibold tracking-tight text-foreground">
              {identity.revision}
            </span>
          ) : (
            <span className="text-dense-caption italic text-muted-foreground">Not started</span>
          )}
          <span
            className={cn(
              'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-dense-caption font-semibold',
              RELEASE_OUTCOME_BADGE[outcome.kind],
            )}
          >
            {outcome.kind === 'released' && <CheckCircle2 className="h-3.5 w-3.5" />}
            {outcome.kind === 'in_progress' && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
            {outcome.kind === 'failed' && <XCircle className="h-3.5 w-3.5" />}
            {outcome.kind === 'idle' && <Circle className="h-3.5 w-3.5" />}
            {outcome.label}
          </span>
        </div>
        <div className="flex flex-wrap items-center gap-2">
          {onAiRelease != null && (
            <AgentTriggerButton
              label={aiReleaseLabel}
              pending={aiReleasePending}
              disabled={aiReleaseDisabled}
              title={aiReleaseDisabledReason ?? aiReleaseLabel}
              onClick={onAiRelease}
            />
          )}
          <span
            className={cn(
              'text-dense-caption',
              identity.mismatch ? 'font-medium text-warning' : 'text-muted-foreground/70',
            )}
          >
            {identity.mismatch ? `⚠ ${identity.hint}` : outcome.detail}
          </span>
        </div>
      </div>
    </div>
  )
}

function StepStatusBanner({
  label,
  env,
  status,
  statusLabel,
  stepRevision,
  nextStep,
  onContinue,
}: {
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
  stepRevision?: string
  nextStep: FlowStep | undefined
  onContinue: () => void
}) {
  const banner = STEP_BANNER_CONFIG[status]
  return (
    <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
      <div className="flex flex-wrap items-center gap-2">
        <span className={cn('inline-flex items-center gap-1.5', banner.textClass)}>
          {status === 'done' && <CheckCircle2 className="h-4 w-4 text-success" />}
          {status === 'active' && <Loader2 className="h-4 w-4 animate-spin text-primary" />}
          {status === 'error' && <XCircle className="h-4 w-4 text-destructive" />}
          {status === 'pending' && <Circle className="h-4 w-4 text-muted-foreground/40" />}
          <span className="release-cc__step-label">{label}</span>
        </span>
        <span
          className={cn(
            'rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider',
            env === 'STG' ? 'text-env-stg' : 'text-env-prod',
          )}
        >
          {env}
        </span>
        <span className={cn('text-dense-caption font-medium', banner.textClass)}>
          {banner.prefix}
          {statusLabel}
        </span>
        {stepRevision != null && stepRevision !== '' && (
          <>
            <span className="text-muted-foreground/30">·</span>
            <span className="font-mono text-dense-caption text-muted-foreground">{stepRevision}</span>
          </>
        )}
      </div>

      {status === 'done' && nextStep != null && (
        <Button size="sm" onClick={onContinue} className="shadow-sm">
          Continue to {nextStep.label}
          <ArrowRight className="ml-1 h-3.5 w-3.5" />
        </Button>
      )}
      {status === 'done' && nextStep == null && (
        <span className="inline-flex items-center gap-1.5 text-dense-caption font-medium text-success">
          <CheckCircle2 className="h-4 w-4" />
          Release complete
        </span>
      )}
    </div>
  )
}

function StepActionZone({
  activeIndex,
  status,
  renderStepActions,
}: {
  activeIndex: number
  status: StepStatus
  renderStepActions: (activeIndex: number) => ReactNode
}) {
  if (status === 'done') {
    const isDeployStep = activeIndex === 0 || activeIndex === 2
    return (
      <details className="group rounded-md border border-border/50 bg-background/40">
        <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-1.5 text-dense-caption text-muted-foreground hover:text-foreground">
          <ChevronDown className="h-3.5 w-3.5 transition-transform group-open:rotate-180" />
          {isDeployStep ? 'Re-deploy with a different revision' : 'Re-run this gate'}
        </summary>
        <div className="border-t border-border/50 px-3 py-2.5">{renderStepActions(activeIndex)}</div>
      </details>
    )
  }
  return <>{renderStepActions(activeIndex)}</>
}

export interface ReleaseStepCommandCenterProps {
  steps: FlowStep[]
  activeIndex: number
  onSelect: (i: number) => void
  stepLabels: readonly string[]
  stgRun: DeliveryPipelineRunView | undefined
  prodRun: DeliveryPipelineRunView | undefined
  stgGate: ReleaseGateResponse | undefined
  prodGate: ReleaseGateResponse | undefined
  renderStepActions: (activeIndex: number) => ReactNode
  renderStepDetail?: (activeIndex: number) => ReactNode
  onAiRelease?: () => void
  aiReleasePending?: boolean
  aiReleaseDisabled?: boolean
  aiReleaseDisabledReason?: string
  aiReleaseLabel?: string
}

export function ReleaseStepCommandCenter({
  steps,
  activeIndex,
  onSelect,
  stepLabels,
  stgRun,
  prodRun,
  stgGate,
  prodGate,
  renderStepActions,
  renderStepDetail,
  onAiRelease,
  aiReleasePending,
  aiReleaseDisabled,
  aiReleaseDisabledReason,
  aiReleaseLabel,
}: ReleaseStepCommandCenterProps) {
  const isStg = activeIndex < 2
  const accentClass = isStg ? 'release-cc__accent--stg' : 'release-cc__accent--prod'
  const stepRevision = stepRevisionForIndex(activeIndex, stgRun, prodRun, stgGate, prodGate)

  let summary: ReactNode
  switch (activeIndex) {
    case 0:
      summary = <DeployStepSummary run={stgRun} />
      break
    case 1:
      summary = <GateStepSummary gate={stgGate} />
      break
    case 2:
      summary = <DeployStepSummary run={prodRun} />
      break
    default:
      summary = <GateStepSummary gate={prodGate} />
      break
  }

  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card">
      <div className={cn('release-cc__accent', accentClass)} />

      <ReleaseIdentityHeader
        steps={steps}
        stgRun={stgRun}
        prodRun={prodRun}
        stgGate={stgGate}
        prodGate={prodGate}
        onAiRelease={onAiRelease}
        aiReleasePending={aiReleasePending}
        aiReleaseDisabled={aiReleaseDisabled}
        aiReleaseDisabledReason={aiReleaseDisabledReason}
        aiReleaseLabel={aiReleaseLabel}
      />

      <FlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      <div className="border-t border-border">
        <div className="release-cc__action-zone px-4 py-3">
          <StepStatusBanner
            label={stepLabels[activeIndex] ?? steps[activeIndex]?.label ?? ''}
            env={isStg ? 'STG' : 'PROD'}
            status={steps[activeIndex].status}
            statusLabel={steps[activeIndex].statusLabel}
            stepRevision={stepRevision}
            nextStep={steps[activeIndex + 1]}
            onContinue={() => onSelect(activeIndex + 1)}
          />
          <StepActionZone
            activeIndex={activeIndex}
            status={steps[activeIndex].status}
            renderStepActions={renderStepActions}
          />
        </div>

        <div className="border-t border-border/40 px-4 py-1.5">
          {summary}
          {renderStepDetail?.(activeIndex)}
        </div>
      </div>
    </div>
  )
}
