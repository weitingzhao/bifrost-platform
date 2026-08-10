import { Button, cn, DenseTag } from '@bifrost/ui'
import {
  ArrowRight,
  Check,
  CheckCircle2,
  Circle,
  Loader2,
  XCircle,
} from 'lucide-react'
import type { ReactNode } from 'react'
import type { StepStatus } from '@/lib/delivery/releaseStepTypes'
import {
  evidenceSummaryLine,
  type PluginLaunchEvidence,
} from '@/lib/delivery/pluginLaunchEvidence'
import {
  derivePluginLaunchOutcome,
  type PluginFlowStep,
} from '@/components/delivery/pluginLaunchOutcome'

const OUTCOME_BADGE: Record<string, string> = {
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

function PluginFlowStepper({
  steps,
  activeIndex,
  onSelect,
}: {
  steps: PluginFlowStep[]
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
                  'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold leading-none',
                  STEP_CIRCLE[step.status],
                )}
              >
                {step.status === 'done' ? <Check className="h-3 w-3" /> : i + 1}
              </span>
              <span className="flex flex-col items-start leading-tight">
                <span
                  className={cn(
                    'text-dense-caption transition-colors',
                    isActive
                      ? 'font-semibold text-foreground'
                      : 'text-muted-foreground group-hover:text-foreground',
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

export interface PluginStepCommandCenterProps {
  steps: PluginFlowStep[]
  activeIndex: number
  onSelect: (i: number) => void
  evidence: PluginLaunchEvidence
  modeLabel?: string
  revisionHint?: string
  renderStepActions: (activeIndex: number) => ReactNode
  onAiLaunch?: () => void
  aiLaunchPending?: boolean
  aiLaunchDisabled?: boolean
  aiLaunchDisabledReason?: string
  aiLaunchLabel?: string
}

/**
 * Full-step command center for Launch Plugin — mirrors ReleaseStepCommandCenter
 * visuals but does NOT pretend to be Tekton STG/PROD pipelines.
 */
export function PluginStepCommandCenter({
  steps,
  activeIndex,
  onSelect,
  evidence,
  modeLabel,
  revisionHint,
  renderStepActions,
  onAiLaunch,
  aiLaunchPending = false,
  aiLaunchDisabled = false,
  aiLaunchDisabledReason,
  aiLaunchLabel = 'AI Launch Plugin',
}: PluginStepCommandCenterProps) {
  const outcome = derivePluginLaunchOutcome(steps)
  const step = steps[activeIndex]
  const nextStep = steps[activeIndex + 1]

  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card">
      <div className="release-cc__accent release-cc__accent--stg" />

      <div className="release-cc__identity border-b border-border px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
          <div className="flex min-w-0 items-center gap-3">
            <span className="shrink-0 text-dense-label font-semibold uppercase tracking-wider text-muted-foreground">
              Plugin
            </span>
            {revisionHint != null && revisionHint !== '' ? (
              <span className="truncate font-mono text-[15px] font-semibold tracking-tight text-foreground">
                {revisionHint}
              </span>
            ) : (
              <span className="text-dense-caption italic text-muted-foreground">
                make install · not Tekton
              </span>
            )}
            <span
              className={cn(
                'inline-flex shrink-0 items-center gap-1 rounded-full border px-2 py-0.5 text-dense-caption font-semibold',
                OUTCOME_BADGE[outcome.kind],
              )}
            >
              {outcome.kind === 'released' && <CheckCircle2 className="h-3.5 w-3.5" />}
              {outcome.kind === 'in_progress' && <Loader2 className="h-3.5 w-3.5 animate-spin" />}
              {outcome.kind === 'failed' && <XCircle className="h-3.5 w-3.5" />}
              {outcome.kind === 'idle' && <Circle className="h-3.5 w-3.5" />}
              {outcome.label}
            </span>
            {modeLabel != null && modeLabel !== '' && (
              <DenseTag variant="neutral">mode {modeLabel}</DenseTag>
            )}
          </div>
          <div className="flex flex-wrap items-center gap-2">
            {onAiLaunch != null && (
              <Button
                size="sm"
                disabled={aiLaunchDisabled || aiLaunchPending}
                title={aiLaunchDisabledReason ?? aiLaunchLabel}
                onClick={onAiLaunch}
              >
                {aiLaunchPending ? 'Starting…' : aiLaunchLabel}
              </Button>
            )}
            <span className="text-dense-caption text-muted-foreground/70">{outcome.detail}</span>
          </div>
        </div>
        <p className="m-0 mt-1.5 text-dense-micro text-muted-foreground">
          {evidenceSummaryLine(evidence)}
        </p>
      </div>

      <PluginFlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      <div className="border-t border-border">
        <div className="release-cc__action-zone px-4 py-3">
          <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="release-cc__step-label text-dense-label font-semibold">
                {step?.label}
              </span>
              <span className="rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider text-muted-foreground">
                Plugin
              </span>
              <span className="text-dense-caption text-muted-foreground">{step?.statusLabel}</span>
            </div>
            {step?.status === 'done' && nextStep != null && (
              <Button size="sm" onClick={() => onSelect(activeIndex + 1)} className="shadow-sm">
                Continue to {nextStep.label}
                <ArrowRight className="ml-1 h-3.5 w-3.5" />
              </Button>
            )}
            {step?.status === 'done' && nextStep == null && (
              <span className="inline-flex items-center gap-1.5 text-dense-caption font-medium text-success">
                <CheckCircle2 className="h-4 w-4" />
                Plugin publish complete
              </span>
            )}
          </div>
          {renderStepActions(activeIndex)}
        </div>
      </div>
    </div>
  )
}
