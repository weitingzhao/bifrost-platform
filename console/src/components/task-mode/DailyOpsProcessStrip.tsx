import { useEffect, useMemo, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { DenseTag, cn } from '@bifrost/ui'
import { Check } from 'lucide-react'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { ViewerEnvBadge } from '@/components/task-mode/ViewerEnvBadge'
import { respondRemediationJob } from '@/api/platform'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  deriveAgentLiveFeed,
  formatAgentElapsed,
  formatFeedEventLine,
  recentAgentFeedEvents,
} from '@/lib/agent/agentLiveFeed'
import {
  DAILY_OPS_WORKFLOW_PHASES,
  dailyOpsStepStatuses,
  type DailyOpsStepStatus,
  type DailyOpsWorkflowResult,
} from '@/lib/control-room/dailyOpsWorkflow'
import type { FleetSnapshot, FleetVerdictKind } from '@/lib/control-room/fleetSnapshot'

const VERDICT_VARIANT: Record<FleetVerdictKind, 'success' | 'warning' | 'danger'> = {
  GO: 'success',
  HOLD: 'warning',
  'NO-GO': 'danger',
}

const STEP_CIRCLE: Record<DailyOpsStepStatus, string> = {
  done: 'border-2 border-success/40 text-success bg-success/10',
  active:
    'border-2 border-primary bg-primary text-primary-foreground shadow-[0_0_0_3px_color-mix(in_oklab,var(--primary)_15%,transparent)]',
  blocked: 'border-2 border-destructive text-destructive bg-destructive/10',
  planned: 'border-2 border-border text-muted-foreground/50 bg-transparent',
}

const STEP_LABEL: Record<DailyOpsStepStatus, string> = {
  done: 'text-success/80',
  active: 'text-primary font-medium',
  blocked: 'text-destructive',
  planned: 'text-muted-foreground/50',
}

export type DailyOpsProcessStripProps = {
  fleet: FleetSnapshot
  workflow: DailyOpsWorkflowResult
  isLoading?: boolean
  canOperate?: boolean
  agentFixPending?: boolean
  agentFixError?: string | null
  /** W3: Assisted hint only — Auto-remediate stays OFF. */
  showReadyHint?: boolean
  /** Ambient remediation job — drives View agent CTA state. */
  ambientJobId?: string | null
  ambientJobScope?: string | null
  onPrimaryAction: () => void
  onOpenAgentDesk?: (jobId?: string) => void
  /** Escape hatch when inline Operator Plan is not enough (MCP / deploy / smoke). */
  onOpenFullOperatorPlane?: () => void
  /** Operator Plane Fix (operator-plane-remediate) — not Checklist AI Check. */
  operatorPlanFixPending?: boolean
  operatorPlanFixDisabled?: boolean
  operatorPlanFixTitle?: string
  /** Checklist AI Check (daily-ops-checklist-run) — Discover / Clear strip primary. */
  checklistCheckPending?: boolean
  checklistCheckDisabled?: boolean
  checklistCheckTitle?: string
  checklistCheckActive?: boolean
  checklistCheckStatusHint?: string | null
}

/**
 * Single Daily Ops Process strip — viewer env + GO/NO-GO + circle stepper + one CTA.
 * Checklist + Fleet Board live beside each other in DailyOpsFleetDesk; Agent progress pins above.
 */
export function DailyOpsProcessStrip({
  fleet,
  workflow,
  isLoading = false,
  canOperate = false,
  agentFixPending = false,
  agentFixError = null,
  showReadyHint = false,
  ambientJobId = null,
  onPrimaryAction,
  onOpenAgentDesk,
  onOpenFullOperatorPlane,
  operatorPlanFixPending = false,
  operatorPlanFixDisabled = false,
  operatorPlanFixTitle,
  checklistCheckPending = false,
  checklistCheckDisabled = false,
  checklistCheckTitle,
  checklistCheckActive = false,
  checklistCheckStatusHint = null,
}: DailyOpsProcessStripProps) {
  const { verdict, viewerEnv } = fleet
  const action = workflow.primaryAction
  const stepStatuses = dailyOpsStepStatuses(workflow)
  const isAgentFix = action.kind === 'agent-fix'
  const isOperatorPlan = action.kind === 'operator-plan'
  const isViewAgent = action.kind === 'view-agent'
  /** Discover AI Check + Clear idle re-check — same Checklist probe scope. */
  const isChecklistPrimary =
    action.kind === 'ai-check' || action.kind === 'run-check'
  const fixPending = isOperatorPlan ? operatorPlanFixPending : agentFixPending
  const checkBusy = checklistCheckPending || checklistCheckActive
  const checkActiveLabel =
    checklistCheckStatusHint != null && checklistCheckStatusHint !== ''
      ? `Checking… · ${checklistCheckStatusHint}`
      : 'Checking…'
  const showOperatorPlanPanel =
    isOperatorPlan ||
    (workflow.activePhase === 'remediate' &&
      workflow.blockers.some(b => /Engineer|Operator Plan/i.test(b)))

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2">
      <div className="flex min-h-[44px] flex-wrap items-center gap-x-3 gap-y-1.5">
        <ViewerEnvBadge viewerEnv={viewerEnv} isLoading={isLoading} />
        <DenseTag variant={VERDICT_VARIANT[verdict.kind]}>
          {isLoading ? 'Probing…' : verdict.kind}
        </DenseTag>
        <span className="min-w-0 max-w-[14rem] truncate text-[var(--text-dense-caption)] text-muted-foreground sm:max-w-xs">
          {verdict.topReason}
        </span>

        <ol
          className="flex min-w-0 flex-1 flex-wrap items-center justify-center gap-0"
          aria-label="Daily Ops process"
        >
          {DAILY_OPS_WORKFLOW_PHASES.map((phase, idx) => {
            const status = stepStatuses[phase.id]
            const connectorDone =
              idx > 0 &&
              (stepStatuses[DAILY_OPS_WORKFLOW_PHASES[idx - 1].id] === 'done' ||
                stepStatuses[DAILY_OPS_WORKFLOW_PHASES[idx - 1].id] === 'active')
            return (
              <li key={phase.id} className="flex min-w-0 items-center">
                {idx > 0 && (
                  <div
                    className={cn(
                      'mx-1 h-px w-3 shrink-0 sm:w-5',
                      connectorDone ? 'bg-success/30' : 'bg-border',
                    )}
                    aria-hidden
                  />
                )}
                <span
                  className={cn(
                    'flex flex-col items-center gap-0.5 rounded-md px-1 py-0.5',
                    status === 'active' && 'bg-primary/8',
                  )}
                  title={`${phase.label}: ${status}`}
                >
                  <span
                    className={cn(
                      'flex h-6 w-6 shrink-0 items-center justify-center rounded-full text-[10px] font-bold leading-none',
                      STEP_CIRCLE[status],
                    )}
                  >
                    {status === 'done' ? <Check className="h-3 w-3" aria-hidden /> : idx + 1}
                  </span>
                  <span
                    className={cn(
                      'text-[9px] leading-none',
                      STEP_LABEL[status],
                      status === 'active' && 'font-semibold',
                    )}
                  >
                    {phase.label}
                  </span>
                </span>
              </li>
            )
          })}
        </ol>

        <div className="flex shrink-0 flex-wrap items-center gap-1.5">
          {showReadyHint &&
            (isAgentFix || isOperatorPlan || isChecklistPrimary) &&
            !fixPending &&
            !checkBusy &&
            canOperate && (
              <DenseTag variant="info" className="text-[9px]">
                {isOperatorPlan
                  ? 'Ready · Operator Plan'
                  : isChecklistPrimary
                    ? 'Ready · AI Check'
                    : 'Ready to Agent Fix'}
              </DenseTag>
            )}
          {isChecklistPrimary && (
            <AgentTriggerButton
              label={action.label}
              size="xs"
              pending={checklistCheckPending}
              pendingLabel="Checking…"
              active={checklistCheckActive && !checklistCheckPending}
              activeLabel={checkActiveLabel}
              disabled={
                !canOperate || checklistCheckDisabled || checklistCheckPending
              }
              title={
                !canOperate
                  ? 'Authenticate as operator to run AI Check'
                  : (checklistCheckTitle ??
                    'Start daily-ops-checklist-run probe → report_checklist_signals')
              }
              onClick={onPrimaryAction}
            />
          )}
          {isAgentFix && (
            <AgentTriggerButton
              label={action.label}
              size="xs"
              pending={agentFixPending}
              pendingLabel="Starting…"
              disabled={!canOperate || agentFixPending}
              title={
                !canOperate
                  ? 'Authenticate as operator to run Agent Fix'
                  : workflow.blockers.find(b => b.includes('D10')) ?? action.label
              }
              onClick={onPrimaryAction}
            />
          )}
          {isOperatorPlan && (
            <>
              <AgentTriggerButton
                label={action.label}
                size="xs"
                pending={operatorPlanFixPending}
                pendingLabel="Starting…"
                disabled={!canOperate || operatorPlanFixDisabled || operatorPlanFixPending}
                title={
                  !canOperate
                    ? 'Authenticate as operator to run Operator Plan AI Fix'
                    : (operatorPlanFixTitle ??
                      'Start Operator · Remediate with current bridge probe')
                }
                onClick={onPrimaryAction}
              />
              {onOpenFullOperatorPlane != null && (
                <button
                  type="button"
                  className="rounded border border-transparent px-1.5 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:text-primary hover:underline"
                  onClick={onOpenFullOperatorPlane}
                  title="MCP, host deploy, self-smoke"
                >
                  Full page →
                </button>
              )}
            </>
          )}
          {isViewAgent && (
            <AgentTriggerButton
              label="Agent Fix"
              size="xs"
              pending={fixPending && (ambientJobId == null || ambientJobId === '')}
              pendingLabel="Starting…"
              active={ambientJobId != null && ambientJobId !== ''}
              activeLabel="View agent →"
              disabled={false}
              title="Open Agent Desk — live progress pinned at top of Daily Ops"
              onClick={() => {
                if (onOpenAgentDesk != null) onOpenAgentDesk()
                else onPrimaryAction()
              }}
            />
          )}
          {(action.kind === 'navigate' ||
            action.kind === 'clear-queue' ||
            action.kind === 'verify') && (
            <button
              type="button"
              className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-meta)] font-medium text-primary hover:bg-muted"
              onClick={onPrimaryAction}
            >
              {action.kind === 'navigate' || action.kind === 'clear-queue'
                ? `${action.label} →`
                : action.label}
            </button>
          )}
          {action.kind === 'none' && (
            <DenseTag variant="success" className="text-[9px]">
              {action.label}
            </DenseTag>
          )}
        </div>
      </div>

      {(workflow.blockers.length > 0 || agentFixError) && (
        <div className="mt-1.5 flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-t border-border/50 pt-1.5">
          {showOperatorPlanPanel && (
            <p className="m-0 text-[var(--text-dense-caption)] text-warning">
              Engineer CRITICAL — use Operator Plan AI Fix (fleet cell Agent Fix disabled)
            </p>
          )}
          {workflow.activePhase !== 'clear' &&
            workflow.blockers.some(b => b.includes('D10')) &&
            (isAgentFix || isOperatorPlan || isViewAgent || workflow.activePhase === 'remediate') && (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                D10: live trading remains BLOCKED.
              </p>
            )}
          {agentFixError != null && agentFixError !== '' && (
            <p className="m-0 text-[var(--text-dense-caption)] text-destructive">{agentFixError}</p>
          )}
        </div>
      )}
    </div>
  )
}

export function DailyOpsAgentLivePanel({
  jobId,
  jobScope,
  onOpenAgentDesk,
}: {
  jobId: string
  jobScope?: string | null
  onOpenAgentDesk?: (jobId?: string) => void
}) {
  const qc = useQueryClient()
  const { job, events, connected, error } = useRemediationStream(jobId)
  const [nowMs, setNowMs] = useState(() => Date.now())
  useEffect(() => {
    const id = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [])

  const isTerminal =
    job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
  const isApproval = job?.phase === 'awaiting_approval' && !isTerminal
  const failed = job?.status === 'failed' || job?.status === 'cancelled'

  const pendingApproval = useMemo(() => {
    if (!isApproval) return null
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.type === 'approval_request') return ev
      if (ev.type === 'status' && ev.text.startsWith('Operator selected:')) return null
    }
    return null
  }, [events, isApproval])

  const respondMutation = useMutation({
    mutationFn: ({
      optionId,
      note,
      commitMessage,
    }: {
      optionId: string
      note?: string
      commitMessage?: string
    }) => respondRemediationJob(jobId, optionId, note, commitMessage),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })

  const feed = deriveAgentLiveFeed(events)
  const recent = recentAgentFeedEvents(events, isApproval ? 6 : 10)
  const elapsed = formatAgentElapsed(job?.created_at, nowMs)
  const scopeLabel = jobScope != null && jobScope !== '' ? jobScope : job?.scope

  return (
    <div
      className={cn(
        'mt-1.5 rounded-md border bg-background/80 px-2.5 py-2',
        isApproval ? 'border-warning/50' : 'border-sky-500/35',
      )}
    >
      <div className="flex flex-wrap items-center gap-2">
        <span className="text-[var(--text-dense-caption)] font-semibold text-foreground">
          Agent progress
        </span>
        {scopeLabel != null && scopeLabel !== '' && (
          <DenseTag
            variant="neutral"
            className="max-w-[14rem] truncate text-[8px] border-sky-500/40 text-sky-700 dark:text-sky-300"
            title={scopeLabel}
          >
            {scopeLabel}
          </DenseTag>
        )}
        {isApproval && (
          <DenseTag variant="warning" className="text-[8px]">
            Needs your decision
          </DenseTag>
        )}
        {elapsed != null && (
          <span className="font-mono text-[var(--text-dense-caption)] text-muted-foreground">
            {elapsed}
          </span>
        )}
        {!connected && error == null && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">Connecting…</span>
        )}
        {error != null && error !== '' && (
          <span className="text-[var(--text-dense-caption)] text-destructive">{error}</span>
        )}
        {onOpenAgentDesk != null && (
          <button
            type="button"
            className="ml-auto text-[var(--text-dense-meta)] text-primary hover:underline"
            onClick={() => onOpenAgentDesk(jobId)}
          >
            Agent Desk →
          </button>
        )}
      </div>
      <div className="mt-1.5">
        <AgentPhaseIndicator currentPhase={job?.phase} failed={failed} compact />
      </div>

      {/* Operator decision — stay on Daily Ops; do not require Agent Desk */}
      {pendingApproval != null && (
        <div className="mt-2 rounded-md border border-warning/40 bg-card px-2.5 py-2">
          <RemediationApprovalBlock
            event={pendingApproval}
            submitting={respondMutation.isPending}
            onRespond={(optionId, note, commitMessage) =>
              respondMutation.mutate({ optionId, note, commitMessage })
            }
          />
          {respondMutation.isError && (
            <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-destructive">
              {(respondMutation.error as Error)?.message ?? 'Failed to submit decision'}
            </p>
          )}
        </div>
      )}
      {isApproval && pendingApproval == null && (
        <p className="m-0 mt-2 rounded-md border border-warning/30 bg-card px-2.5 py-2 text-[var(--text-dense-caption)] text-warning">
          Waiting for approval options from the agent stream…
          {onOpenAgentDesk != null && (
            <>
              {' '}
              <button
                type="button"
                className="text-primary hover:underline"
                onClick={() => onOpenAgentDesk(jobId)}
              >
                Open Agent Desk →
              </button>
            </>
          )}
        </p>
      )}

      {!isApproval && feed != null && (
        <p className="m-0 mt-1.5 truncate text-[var(--text-dense-caption)] font-medium text-foreground/90">
          {feed.text}
        </p>
      )}
      {recent.length > 0 && !isApproval && (
        <ul className="mt-1.5 max-h-28 list-none space-y-0.5 overflow-y-auto rounded border border-border/50 bg-muted/20 px-2 py-1.5 font-mono text-[10px] leading-snug text-muted-foreground">
          {recent.map((ev, i) => (
            <li key={ev.id !== '' ? ev.id : `${ev.at}-${ev.type}-${i}`} className="truncate">
              <span className="text-muted-foreground/60">{ev.type}</span>
              {' · '}
              {formatFeedEventLine(ev)}
            </li>
          ))}
        </ul>
      )}
      {isApproval && recent.length > 0 && (
        <details className="mt-2">
          <summary className="cursor-pointer text-[var(--text-dense-caption)] text-muted-foreground hover:text-foreground">
            Recent agent log
          </summary>
          <ul className="mt-1 max-h-20 list-none space-y-0.5 overflow-y-auto rounded border border-border/50 bg-muted/20 px-2 py-1.5 font-mono text-[10px] leading-snug text-muted-foreground">
            {recent.map((ev, i) => (
              <li key={ev.id !== '' ? ev.id : `${ev.at}-${ev.type}-${i}`} className="truncate">
                <span className="text-muted-foreground/60">{ev.type}</span>
                {' · '}
                {formatFeedEventLine(ev)}
              </li>
            ))}
          </ul>
        </details>
      )}
    </div>
  )
}
