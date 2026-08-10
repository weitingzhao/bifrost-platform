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
import { useEffect, useState } from 'react'
import type { DeliveryPipelineRunView, ReleaseGateResponse } from '@/api/deliveryTypes'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { DeployStepSummary, GateStepSummary } from '@/components/delivery/ReleaseStepSummaries'
import { CollapseExpandIcon } from '@/components/layout/CollapseExpandIcon'
import {
  deriveReleaseIdentity,
  deriveReleaseOutcome,
  stepRevisionForIndex,
  type FlowStep,
  type StepStatus,
} from '@/lib/delivery/releaseStepTypes'

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
  suppressContinue = false,
}: {
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
  stepRevision?: string
  nextStep: FlowStep | undefined
  onContinue: () => void
  suppressContinue?: boolean
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
          {suppressContinue && status === 'done' ? 'Previous cycle · ' : banner.prefix}
          {statusLabel}
        </span>
        {stepRevision != null && stepRevision !== '' && (
          <>
            <span className="text-muted-foreground/30">·</span>
            <span className="font-mono text-dense-caption text-muted-foreground">{stepRevision}</span>
          </>
        )}
      </div>

      {!suppressContinue && status === 'done' && nextStep != null && (
        <Button size="sm" onClick={onContinue} className="shadow-sm">
          Continue to {nextStep.label}
          <ArrowRight className="ml-1 h-3.5 w-3.5" />
        </Button>
      )}
      {!suppressContinue && status === 'done' && nextStep == null && (
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
  preferPrimaryActions = false,
}: {
  activeIndex: number
  status: StepStatus
  renderStepActions: (activeIndex: number) => ReactNode
  /** Next-cycle start: show Deploy/Gate immediately even if step still reads done. */
  preferPrimaryActions?: boolean
}) {
  if (!preferPrimaryActions && status === 'done') {
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

function ManualOverrideDetails({
  children,
}: {
  children: ReactNode
}) {
  return (
    <details className="group rounded-md border border-border/50 bg-background/40">
      <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-1.5 text-dense-caption text-muted-foreground hover:text-foreground">
        <ChevronDown className="h-3.5 w-3.5 transition-transform group-open:rotate-180" />
        Manual override (advanced)
      </summary>
      <div className="border-t border-border/50 px-3 py-2.5">
        <p className="m-0 mb-2 text-dense-meta text-muted-foreground">
          Prefer AI Release on the lane strip and decisions in Agent Session. Use this only when the
          agent path is unavailable.
        </p>
        {children}
      </div>
    </details>
  )
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
  /**
   * When true, identity + stepper stay visible; action zone collapses.
   * Defaults open when the active step is not done.
   */
  collapsibleBody?: boolean
  /**
   * Released cycle terminal — hide primary Deploy/Gate actions; show Start next release.
   */
  cycleTerminal?: boolean
  onStartNextRelease?: () => void
  /** After Start next release — show Deploy/Gate primary even while stepper still reads done. */
  nextCycleActive?: boolean
  /**
   * Rocket AI-first path: Step detail is observe-only; Deploy/Gate live under Manual override.
   * Primary CTA is AI Release (lane strip / Start next).
   */
  agentDriven?: boolean
}

function ReleaseCycleTerminalPanel({
  revision,
  onStartNextRelease,
  renderStepActions,
  activeIndex,
  agentDriven = false,
  aiReleasePending = false,
}: {
  revision: string | null
  onStartNextRelease?: () => void
  renderStepActions: (activeIndex: number) => ReactNode
  activeIndex: number
  agentDriven?: boolean
  aiReleasePending?: boolean
}) {
  return (
    <div className="release-cc__action-zone flex flex-col gap-3 px-4 py-3">
      <div className="flex flex-wrap items-center gap-2">
        <CheckCircle2 className="h-4 w-4 shrink-0 text-success" aria-hidden />
        <span className="text-dense-label font-medium text-success">Release complete</span>
        {revision != null && revision !== '' && (
          <span className="font-mono text-dense-caption text-muted-foreground">{revision}</span>
        )}
        <span className="text-dense-caption text-muted-foreground">
          {agentDriven
            ? '— use AI Release on the lane strip to start the next cycle'
            : '— start a new cycle to deploy again'}
        </span>
      </div>
      {!agentDriven && onStartNextRelease != null && (
        <div className="flex justify-end">
          <Button
            size="sm"
            onClick={onStartNextRelease}
            disabled={aiReleasePending}
            className="shadow-sm"
          >
            Start next release
            <ArrowRight className="ml-1 h-3.5 w-3.5" />
          </Button>
        </div>
      )}
      {agentDriven ? (
        <ManualOverrideDetails>{renderStepActions(activeIndex)}</ManualOverrideDetails>
      ) : (
        <details className="group rounded-md border border-border/50 bg-background/40">
          <summary className="flex cursor-pointer list-none items-center gap-1.5 px-3 py-1.5 text-dense-caption text-muted-foreground hover:text-foreground">
            <ChevronDown className="h-3.5 w-3.5 transition-transform group-open:rotate-180" />
            Re-run a stage (advanced)
          </summary>
          <div className="border-t border-border/50 px-3 py-2.5">
            {renderStepActions(activeIndex)}
          </div>
        </details>
      )}
    </div>
  )
}

function AgentDrivenStepDetail({
  label,
  env,
  status,
  statusLabel,
  stepRevision,
  renderStepActions,
  activeIndex,
}: {
  label: string
  env: 'STG' | 'PROD'
  status: StepStatus
  statusLabel: string
  stepRevision?: string
  renderStepActions: (activeIndex: number) => ReactNode
  activeIndex: number
}) {
  return (
    <div className="release-cc__action-zone flex flex-col gap-3 px-4 py-3">
      <StepStatusBanner
        label={label}
        env={env}
        status={status}
        statusLabel={statusLabel}
        stepRevision={stepRevision}
        nextStep={undefined}
        onContinue={() => {}}
        suppressContinue
      />
      <p className="m-0 rounded-md border border-primary/20 bg-primary/5 px-3 py-2 text-dense-meta text-muted-foreground">
        Agent-driven release — this panel is observe-only. Launch with{' '}
        <span className="font-medium text-foreground">AI Release</span> on the lane strip; decide in{' '}
        <span className="font-medium text-foreground">Agent Session</span> below.
      </p>
      <ManualOverrideDetails>{renderStepActions(activeIndex)}</ManualOverrideDetails>
    </div>
  )
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
  collapsibleBody = false,
  cycleTerminal = false,
  onStartNextRelease,
  nextCycleActive = false,
  agentDriven = false,
}: ReleaseStepCommandCenterProps) {
  const isStg = activeIndex < 2
  const accentClass = isStg ? 'release-cc__accent--stg' : 'release-cc__accent--prod'
  const stepRevision = stepRevisionForIndex(activeIndex, stgRun, prodRun, stgGate, prodGate)
  const activeStatus = steps[activeIndex]?.status
  const identity = deriveReleaseIdentity(stgRun, prodRun, stgGate, prodGate)
  const bodyDefaultOpen = cycleTerminal || activeStatus !== 'done'

  const [bodyOpen, setBodyOpen] = useState(bodyDefaultOpen)
  useEffect(() => {
    if (collapsibleBody) setBodyOpen(bodyDefaultOpen)
  }, [collapsibleBody, bodyDefaultOpen, activeIndex, cycleTerminal])

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

  const actionBody = cycleTerminal ? (
    <>
      <ReleaseCycleTerminalPanel
        revision={identity.revision}
        onStartNextRelease={onStartNextRelease}
        renderStepActions={renderStepActions}
        activeIndex={activeIndex}
        agentDriven={agentDriven}
        aiReleasePending={aiReleasePending}
      />
      <div className="border-t border-border/40 px-4 py-1.5">{summary}</div>
    </>
  ) : agentDriven ? (
    <>
      <AgentDrivenStepDetail
        label={stepLabels[activeIndex] ?? steps[activeIndex]?.label ?? ''}
        env={isStg ? 'STG' : 'PROD'}
        status={steps[activeIndex].status}
        statusLabel={steps[activeIndex].statusLabel}
        stepRevision={stepRevision}
        renderStepActions={renderStepActions}
        activeIndex={activeIndex}
      />
      <div className="border-t border-border/40 px-4 py-1.5">
        {summary}
        {renderStepDetail?.(activeIndex)}
      </div>
    </>
  ) : (
    <>
      <div className="release-cc__action-zone px-4 py-3">
        <StepStatusBanner
          label={stepLabels[activeIndex] ?? steps[activeIndex]?.label ?? ''}
          env={isStg ? 'STG' : 'PROD'}
          status={steps[activeIndex].status}
          statusLabel={steps[activeIndex].statusLabel}
          stepRevision={stepRevision}
          nextStep={steps[activeIndex + 1]}
          onContinue={() => onSelect(activeIndex + 1)}
          suppressContinue={nextCycleActive}
        />
        <StepActionZone
          activeIndex={activeIndex}
          status={steps[activeIndex].status}
          renderStepActions={renderStepActions}
          preferPrimaryActions={nextCycleActive}
        />
      </div>

      <div className="border-t border-border/40 px-4 py-1.5">
        {summary}
        {renderStepDetail?.(activeIndex)}
      </div>
    </>
  )

  const sectionTitle = cycleTerminal
    ? 'Release complete'
    : agentDriven
      ? 'Step detail'
      : 'Step actions'
  const sectionHint = cycleTerminal
    ? bodyOpen
      ? agentDriven
        ? 'Observe · next via AI Release'
        : 'Start next or re-run'
      : 'Summary · stepper only'
    : bodyOpen
      ? agentDriven
        ? 'Observe · AI path'
        : 'Detail'
      : 'Summary · stepper only'

  return (
    <div className="relative overflow-hidden rounded-lg border border-border bg-card">
      <div className={cn('release-cc__accent', accentClass)} />

      <ReleaseIdentityHeader
        steps={steps}
        stgRun={stgRun}
        prodRun={prodRun}
        stgGate={stgGate}
        prodGate={prodGate}
        // Lane strip owns the primary AI Release CTA when agentDriven.
        onAiRelease={agentDriven ? undefined : onAiRelease}
        aiReleasePending={aiReleasePending}
        aiReleaseDisabled={aiReleaseDisabled}
        aiReleaseDisabledReason={aiReleaseDisabledReason}
        aiReleaseLabel={aiReleaseLabel}
      />

      <FlowStepper steps={steps} activeIndex={activeIndex} onSelect={onSelect} />

      {collapsibleBody ? (
        <details
          className="group border-t border-border"
          open={bodyOpen}
          onToggle={e => setBodyOpen((e.currentTarget as HTMLDetailsElement).open)}
        >
          <summary className="flex cursor-pointer list-none items-center justify-between gap-2 px-4 py-2 hover:bg-secondary/30 [&::-webkit-details-marker]:hidden">
            <span className="inline-flex items-center gap-1.5 text-dense-caption font-medium text-muted-foreground">
              <CollapseExpandIcon open={bodyOpen} size={14} />
              {sectionTitle}
              <span className="text-dense-micro text-muted-foreground/70">{sectionHint}</span>
            </span>
            <span className="font-mono text-dense-micro text-muted-foreground/60">
              {cycleTerminal
                ? 'Released'
                : `${steps[activeIndex]?.label}: ${steps[activeIndex]?.statusLabel}`}
            </span>
          </summary>
          {actionBody}
        </details>
      ) : (
        <div className="border-t border-border">{actionBody}</div>
      )}
    </div>
  )
}
