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

function AgentDrivenStepDetail({
  label,
  laneLabel,
  statusLabel,
  launchLabel,
}: {
  label: string
  laneLabel: string
  statusLabel: string
  launchLabel: string
}) {
  return (
    <div className="flex flex-col gap-3">
      <div className="flex flex-wrap items-center gap-2">
        <span className="release-cc__step-label text-dense-label font-semibold">{label}</span>
        <span className="rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider text-muted-foreground">
          {laneLabel}
        </span>
        <span className="text-dense-caption text-muted-foreground">{statusLabel}</span>
      </div>
      <p className="m-0 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-dense-meta text-muted-foreground">
        Agent-driven publish — this panel is observe-only. Launch with{' '}
        <span className="font-medium text-foreground">{launchLabel}</span> on the lane strip; decide
        in <span className="font-medium text-foreground">Agent Session</span> / Operator Dock.
      </p>
    </div>
  )
}

export interface PluginStepCommandCenterProps {
  steps: PluginFlowStep[]
  activeIndex: number
  onSelect: (i: number) => void
  /** Precomputed evidence line (Plugin / Agent lanes). */
  evidenceSummary: string
  modeLabel?: string
  revisionHint?: string
  /** Lane identity badge — Plugin (default) or Agent. */
  laneLabel?: string
  idleHint?: string
  completeMessage?: string
  /**
   * Manual / record actions. Shown in Step body only when agentDriven is false;
   * when agentDriven, keep these in Toolbox (escape hatch).
   */
  renderStepActions: (activeIndex: number) => ReactNode
  /** Optional read-only detail under the observe banner (agentDriven). */
  renderStepDetail?: (activeIndex: number) => ReactNode
  onAiLaunch?: () => void
  aiLaunchPending?: boolean
  aiLaunchDisabled?: boolean
  aiLaunchDisabledReason?: string
  aiLaunchLabel?: string
  /**
   * Published cycle terminal — hide Continue; show Start next publish
   * (clears this-cycle evidence; not Tekton nextCycle).
   */
  cycleTerminal?: boolean
  onStartNextCycle?: () => void
  startNextLabel?: string
  /**
   * AI-first path (align Rocket / Satellite): Step detail is observe-only.
   * Primary CTA stays on LaneStateStrip; decisions in Agent Session / Dock.
   */
  agentDriven?: boolean
}

/**
 * Full-step command center for Launch Plugin / Launch Agent — mirrors
 * ReleaseStepCommandCenter visuals but does NOT pretend to be Tekton.
 */
export function PluginStepCommandCenter({
  steps,
  activeIndex,
  onSelect,
  evidenceSummary,
  modeLabel,
  revisionHint,
  laneLabel = 'Plugin',
  idleHint = 'make install · not Tekton',
  completeMessage = 'Plugin publish complete',
  renderStepActions,
  renderStepDetail,
  onAiLaunch,
  aiLaunchPending = false,
  aiLaunchDisabled = false,
  aiLaunchDisabledReason,
  aiLaunchLabel = 'AI Launch Plugin',
  cycleTerminal = false,
  onStartNextCycle,
  startNextLabel = 'Start next publish',
  agentDriven = false,
}: PluginStepCommandCenterProps) {
  const outcome = derivePluginLaunchOutcome(steps)
  const step = steps[activeIndex]
  const nextStep = steps[activeIndex + 1]
  // Lane strip owns the primary AI Launch CTA when agentDriven.
  const showInlineAiLaunch = onAiLaunch != null && !agentDriven

  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card">
      <div className="release-cc__accent release-cc__accent--stg" />

      <div className="release-cc__identity border-b border-border px-4 py-3">
        <div className="flex flex-wrap items-center justify-between gap-x-4 gap-y-1.5">
          <div className="flex min-w-0 items-center gap-3">
            <span className="shrink-0 text-dense-label font-semibold uppercase tracking-wider text-muted-foreground">
              {laneLabel}
            </span>
            {revisionHint != null && revisionHint !== '' ? (
              <span className="truncate font-mono text-[15px] font-semibold tracking-tight text-foreground">
                {revisionHint}
              </span>
            ) : (
              <span className="text-dense-caption italic text-muted-foreground">
                {idleHint}
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
            {showInlineAiLaunch && (
              <Button
                size="sm"
                disabled={aiLaunchDisabled || aiLaunchPending || cycleTerminal}
                title={
                  cycleTerminal
                    ? 'Start next publish before launching again'
                    : (aiLaunchDisabledReason ?? aiLaunchLabel)
                }
                onClick={onAiLaunch}
              >
                {aiLaunchPending ? 'Starting…' : aiLaunchLabel}
              </Button>
            )}
            <span className="text-dense-caption text-muted-foreground/70">{outcome.detail}</span>
          </div>
        </div>
        <p className="m-0 mt-1.5 text-dense-micro text-muted-foreground">
          {evidenceSummary}
        </p>
      </div>

      <PluginFlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      <div className="border-t border-border">
        <div className="release-cc__action-zone px-4 py-3">
          {cycleTerminal ? (
            <div className="flex flex-col gap-3">
              <div className="flex flex-wrap items-center justify-between gap-2">
                <div className="flex min-w-0 flex-col gap-1">
                  <span className="inline-flex items-center gap-1.5 text-dense-caption font-medium text-success">
                    <CheckCircle2 className="h-4 w-4" />
                    {completeMessage}
                  </span>
                  <span className="text-dense-caption text-muted-foreground">
                    Published — clear boundary before the next cycle (align Rocket / Satellite)
                    {agentDriven ? ` · or ${aiLaunchLabel} on the lane strip` : ''}
                  </span>
                </div>
                {onStartNextCycle != null && (
                  <Button size="sm" onClick={onStartNextCycle} className="shadow-sm">
                    {startNextLabel}
                    <ArrowRight className="ml-1 h-3.5 w-3.5" />
                  </Button>
                )}
              </div>
              {!agentDriven && (
                <details className="group rounded-md border border-border/50 bg-background/40">
                  <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-1.5 text-dense-caption text-muted-foreground hover:text-foreground">
                    Re-record a stage (advanced)
                  </summary>
                  <div className="border-t border-border/50 px-3 py-2.5">
                    {renderStepActions(activeIndex)}
                  </div>
                </details>
              )}
            </div>
          ) : agentDriven ? (
            <div className="flex flex-col gap-3">
              <AgentDrivenStepDetail
                label={step?.label ?? ''}
                laneLabel={laneLabel}
                statusLabel={step?.statusLabel ?? ''}
                launchLabel={aiLaunchLabel}
              />
              {renderStepDetail?.(activeIndex)}
            </div>
          ) : (
            <>
              <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
                <div className="flex flex-wrap items-center gap-2">
                  <span className="release-cc__step-label text-dense-label font-semibold">
                    {step?.label}
                  </span>
                  <span className="rounded px-1 py-px text-dense-micro font-bold uppercase tracking-wider text-muted-foreground">
                    {laneLabel}
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
                    {completeMessage}
                  </span>
                )}
              </div>
              {renderStepActions(activeIndex)}
            </>
          )}
        </div>
      </div>
    </div>
  )
}
