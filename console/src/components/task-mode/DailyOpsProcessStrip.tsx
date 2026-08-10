import { useEffect, useState } from 'react'
import { useQueryClient } from '@tanstack/react-query'
import { DenseTag, cn } from '@bifrost/ui'
import { Check, Hand } from 'lucide-react'
import { AgentTriggerButton } from '@/components/agent/AgentTriggerButton'
import { ViewerEnvBadge } from '@/components/task-mode/ViewerEnvBadge'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  deriveAgentLiveFeed,
  formatAgentElapsed,
} from '@/lib/agent/agentLiveFeed'
import {
  DAILY_OPS_WORKFLOW_PHASES,
  dailyOpsStepStatuses,
  type DailyOpsStepStatus,
  type DailyOpsWorkflowResult,
} from '@/lib/control-room/dailyOpsWorkflow'
import type { FleetVerdictKind } from '@/lib/control-room/fleetSnapshot'
import type { OpenAgentDeskArg } from '@/lib/agent/openAgentDesk'
import { useDailyOpsContext } from '@/components/task-mode/daily-ops/useDailyOpsContext'

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

/** Deep links only — live Discover→Clear lives in the Ops loop stepper + CTA. */
const DAILY_OPS_HELP_LINKS: { label: string; tabId: string }[] = [
  { label: 'Control Room (posture)', tabId: 'control-room' },
  { label: 'Observability (health)', tabId: 'observability' },
  { label: 'Topology', tabId: 'runtime-map' },
  { label: 'Cluster', tabId: 'cluster' },
  { label: 'Operator Plane', tabId: 'operator-plane' },
  { label: 'Queue', tabId: 'queue' },
  { label: 'Defects', tabId: 'defects' },
  { label: 'Agent Protocol', tabId: 'agent-protocol' },
]

export type DailyOpsProcessStripProps = {
  workflow: DailyOpsWorkflowResult
  agentFixError?: string | null
  /** W3: Assisted hint only — Auto-remediate stays OFF. */
  showReadyHint?: boolean
  onPrimaryAction: () => void
  /** Outline secondary when primary is manual but an AI-fixable sibling exists. */
  onSecondaryAction?: () => void
  /** Escape hatch when inline Operator Plan is not enough (MCP / deploy / smoke). */
  onOpenFullOperatorPlane?: () => void
  /** Help · reference deep links (muted, collapsed) — not a phase strip. */
  onNavigate?: (tabId: string) => void
  /** Operator Plane Fix (operator-plane-remediate) — not Checklist AI Check. */
  operatorPlanFixPending?: boolean
  operatorPlanFixDisabled?: boolean
  operatorPlanFixTitle?: string
  /** Open operate-queue count — when > 0 during Remediate/Clear, show Sweep Queue CTA. */
  queueOpen?: number
  onSweepQueue?: () => void
  sweepQueuePending?: boolean
}

/**
 * Ops loop (UI name) — viewer env + GO/NO-GO + circle stepper + one CTA.
 * Help · reference is a muted collapsed deep-link entry in the strip header (not a footer row).
 * Checklist + Fleet Board live beside each other in DailyOpsFleetDesk; Agent progress follows Ops loop when a job is active.
 */
export function DailyOpsProcessStrip({
  workflow,
  agentFixError = null,
  showReadyHint = false,
  onPrimaryAction,
  onSecondaryAction,
  onOpenFullOperatorPlane,
  onNavigate,
  operatorPlanFixPending = false,
  operatorPlanFixDisabled = false,
  operatorPlanFixTitle,
  queueOpen = 0,
  onSweepQueue,
  sweepQueuePending = false,
}: DailyOpsProcessStripProps) {
  const {
    fleet,
    isLoading = false,
    canOperate = false,
    agentFixPending = false,
    ambientJobId = null,
    onOpenAgentDesk,
    onExpandAgentDock,
    checklistCheckPending = false,
    checklistCheckDisabled = false,
    checklistCheckTitle,
    checklistCheckActive = false,
    checklistCheckStatusHint = null,
  } = useDailyOpsContext()
  const { verdict, viewerEnv } = fleet
  const action = workflow.primaryAction
  const stepStatuses = dailyOpsStepStatuses(workflow)
  const showSweepQueue =
    queueOpen > 0 &&
    onSweepQueue != null &&
    (workflow.activePhase === 'remediate' || workflow.activePhase === 'clear')
  const isAgentFix = action.kind === 'agent-fix'
  const isOperatorPlan = action.kind === 'operator-plan'
  const isProposeCommit = action.kind === 'propose-commit'
  const isManualNext = action.kind === 'manual-next'
  const isViewAgent = action.kind === 'view-agent'
  /** Discover AI Check + Clear idle re-check — same Checklist probe scope. */
  const isChecklistPrimary =
    action.kind === 'ai-check' || action.kind === 'run-check'
  const fixPending =
    isOperatorPlan || isProposeCommit ? operatorPlanFixPending : agentFixPending
  const checkBusy = checklistCheckPending || checklistCheckActive
  const checkActiveLabel =
    checklistCheckStatusHint != null && checklistCheckStatusHint !== ''
      ? `Checking… · ${checklistCheckStatusHint}`
      : 'Checking…'
  const nextBanner = workflow.blockers.find(b => b.startsWith('Next:'))
  const showRemediateBanner =
    workflow.activePhase === 'remediate' &&
    (isManualNext ||
      isOperatorPlan ||
      isProposeCommit ||
      nextBanner != null ||
      workflow.blockers.some(b => /Engineer|Operator Plan|Next:|Propose commit|dirty/i.test(b)))
  const [manualCopied, setManualCopied] = useState(false)
  const [manualCopyError, setManualCopyError] = useState<string | null>(null)

  const handleManualPrimary = () => {
    const hint = action.manualHint ?? action.label
    void navigator.clipboard?.writeText(hint).then(
      () => {
        setManualCopyError(null)
        setManualCopied(true)
        window.setTimeout(() => setManualCopied(false), 2500)
      },
      () => {
        setManualCopied(false)
        setManualCopyError('Clipboard copy failed — copy the hint manually from Detail')
        window.setTimeout(() => setManualCopyError(null), 4000)
      },
    )
    onPrimaryAction()
  }

  return (
    <div className="rounded-lg border border-border bg-secondary px-3 py-2">
      <div className="mb-1.5 flex flex-wrap items-baseline gap-x-2 gap-y-0.5">
        <span className="text-[var(--text-dense-meta)] font-semibold text-foreground">
          Ops loop
        </span>
        <span className="text-[var(--text-dense-caption)] text-muted-foreground">
          Discover → Remediate → Verify → Clear — Fleet Desk is health ground truth
        </span>
        {onNavigate != null && (
          <details className="ml-auto min-w-0 max-w-full">
            <summary className="cursor-pointer list-none text-[var(--text-dense-caption)] text-muted-foreground/80 underline-offset-2 hover:text-muted-foreground hover:underline [&::-webkit-details-marker]:hidden">
              Help
              <span className="ml-1 no-underline text-muted-foreground/60">· reference</span>
            </summary>
            <div className="mt-1 rounded border border-border/40 bg-background/50 px-2 py-1.5">
              <p className="m-0 mb-1.5 text-[var(--text-dense-micro)] text-muted-foreground">
                Reference only — Ops loop + Fleet board are authoritative.
              </p>
              <ul className="m-0 flex list-none flex-wrap gap-x-3 gap-y-1 p-0">
                {DAILY_OPS_HELP_LINKS.map(link => (
                  <li key={link.tabId}>
                    <button
                      type="button"
                      className="text-[var(--text-dense-caption)] text-muted-foreground hover:text-primary hover:underline"
                      onClick={() => onNavigate(link.tabId)}
                    >
                      {link.label} →
                    </button>
                  </li>
                ))}
              </ul>
            </div>
          </details>
        )}
      </div>
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
          aria-label="Ops loop"
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
            (isAgentFix ||
              isOperatorPlan ||
              isProposeCommit ||
              isChecklistPrimary ||
              isManualNext) &&
            !fixPending &&
            !checkBusy &&
            (isManualNext || canOperate) && (
              <DenseTag variant="info" className="text-[9px]">
                {isManualNext
                  ? 'Ready · Manual next'
                  : isProposeCommit
                    ? 'Ready · Propose commit'
                    : isOperatorPlan
                      ? 'Ready · Operator Plan'
                      : isChecklistPrimary
                        ? 'Ready · AI Check'
                        : 'Ready to Agent Fix'}
              </DenseTag>
            )}
          {isManualNext && (
            <button
              type="button"
              className="inline-flex items-center gap-1 rounded border border-fuchsia-500/45 bg-background px-2 py-0.5 text-[var(--text-dense-meta)] font-medium text-fuchsia-700 hover:bg-fuchsia-500/10 dark:text-fuchsia-300"
              title={
                action.manualHint != null && action.manualHint !== ''
                  ? action.manualHint
                  : 'Physical / manual next step — Agent Fix cannot finish this alone'
              }
              onClick={handleManualPrimary}
            >
              <Hand className="size-3 shrink-0" aria-hidden />
              {action.label}
            </button>
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
          {isProposeCommit && (
            <>
              <AgentTriggerButton
                label={action.label}
                size="xs"
                pending={operatorPlanFixPending}
                pendingLabel="Starting…"
                disabled={!canOperate || operatorPlanFixDisabled || operatorPlanFixPending}
                title={
                  !canOperate
                    ? 'Authenticate as operator to propose commit'
                    : (operatorPlanFixTitle ??
                      'Start git-dirty-remediate — approval required before commit/stash')
                }
                onClick={onPrimaryAction}
              />
              {action.secondary != null && onSecondaryAction != null && (
                <button
                  type="button"
                  className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:border-primary/40 hover:text-primary"
                  disabled={!canOperate || operatorPlanFixPending}
                  title="Optional — stash dirty repos after operator approval (never drops WIP)"
                  onClick={onSecondaryAction}
                >
                  {action.secondary.label}
                </button>
              )}
            </>
          )}
          {(isManualNext || isProposeCommit) && action.secondary != null && onSecondaryAction != null && isManualNext && (
            <button
              type="button"
              className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:border-primary/40 hover:text-primary"
              disabled={!canOperate || operatorPlanFixPending}
              title={
                !canOperate
                  ? 'Authenticate as operator to run secondary action'
                  : 'Secondary — Propose commit for git dirty sibling (approval required)'
              }
              onClick={onSecondaryAction}
            >
              {action.secondary.label}
            </button>
          )}
          {isManualNext && onOpenFullOperatorPlane != null && (
            <button
              type="button"
              className="rounded border border-transparent px-1.5 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:text-primary hover:underline"
              onClick={onOpenFullOperatorPlane}
              title="MCP, host deploy, self-smoke"
            >
              Full page →
            </button>
          )}
          {isViewAgent && (
            <AgentTriggerButton
              label="Agent Fix"
              size="xs"
              pending={fixPending && (ambientJobId == null || ambientJobId === '')}
              pendingLabel="Starting…"
              active={ambientJobId != null && ambientJobId !== ''}
              activeLabel="Expand dock"
              disabled={false}
              title="Expand Agent Execution Dock — live progress stays on Daily Ops"
              onClick={() => {
                if (onExpandAgentDock != null) onExpandAgentDock()
                else if (onOpenAgentDesk != null) onOpenAgentDesk()
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
          {showSweepQueue && (
            <button
              type="button"
              className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-caption)] text-muted-foreground hover:border-primary/40 hover:text-primary"
              disabled={!canOperate || sweepQueuePending}
              title={
                canOperate
                  ? `Triage ${queueOpen} open queue item${queueOpen === 1 ? '' : 's'} (stale dismiss + re-classify; auto-drain off)`
                  : 'Operator authentication required'
              }
              onClick={onSweepQueue}
            >
              {sweepQueuePending ? 'Sweeping…' : `Sweep Queue (${queueOpen})`}
            </button>
          )}
        </div>
      </div>

      {(workflow.blockers.length > 0 || agentFixError || manualCopyError) && (
        <div className="mt-1.5 flex flex-wrap items-baseline gap-x-3 gap-y-0.5 border-t border-border/50 pt-1.5">
          {showRemediateBanner && (
            <p
              className={cn(
                'm-0 text-[var(--text-dense-caption)]',
                isManualNext ? 'text-fuchsia-700 dark:text-fuchsia-300' : 'text-warning',
              )}
            >
              {nextBanner ??
                (isManualNext
                  ? `Next: ${action.label}`
                  : isOperatorPlan
                    ? `Next: ${action.label}`
                    : 'Engineer plane — review Operator Plan')}
              {manualCopied && isManualNext && (
                <span className="ml-2 text-muted-foreground">· Copied next step</span>
              )}
            </p>
          )}
          {workflow.activePhase !== 'clear' &&
            workflow.blockers.some(b => b.includes('D10')) &&
            (isAgentFix ||
              isOperatorPlan ||
              isManualNext ||
              isViewAgent ||
              workflow.activePhase === 'remediate') && (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                D10: live trading remains BLOCKED.
              </p>
            )}
          {agentFixError != null && agentFixError !== '' && (
            <p className="m-0 text-[var(--text-dense-caption)] text-destructive">{agentFixError}</p>
          )}
          {manualCopyError != null && (
            <p className="m-0 text-[var(--text-dense-caption)] text-destructive">{manualCopyError}</p>
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
  onExpandAgentDock,
  onVerifyReprobe,
}: {
  jobId: string
  jobScope?: string | null
  onOpenAgentDesk?: (arg?: OpenAgentDeskArg) => void
  onExpandAgentDock?: () => void
  /** Same as Ops loop Verify → Re-probe fleet (invalidate cockpit / checklist). */
  onVerifyReprobe?: () => void
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
  const succeeded = job?.status === 'done'
  const failReason =
    job?.error != null && job.error.trim() !== ''
      ? job.error.trim().slice(0, 160)
      : job?.summary != null && job.summary.trim() !== ''
        ? job.summary.trim().slice(0, 160)
        : null

  const feed = deriveAgentLiveFeed(events)
  const elapsed = formatAgentElapsed(job?.created_at, nowMs)
  const scopeLabel = jobScope != null && jobScope !== '' ? jobScope : job?.scope
  const statusHint = isApproval
    ? 'Needs your decision'
    : succeeded
      ? 'Done — confirm surface'
      : failed
        ? job?.status === 'cancelled'
          ? 'Cancelled'
          : 'Failed'
        : feed?.text ?? (connected ? 'Working…' : error != null ? error : 'Connecting…')

  return (
    <div
      className={cn(
        'mt-1.5 flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border px-2.5 py-1.5',
        isApproval
          ? 'border-warning/50 bg-warning/5'
          : succeeded
            ? 'border-emerald-500/35 bg-emerald-500/5'
            : failed
              ? 'border-destructive/35 bg-destructive/5'
              : 'border-sky-500/35 bg-background/80',
      )}
    >
      <span className="text-[var(--text-dense-caption)] font-semibold text-foreground">
        Agent progress
      </span>
      {scopeLabel != null && scopeLabel !== '' && (
        <DenseTag
          variant="neutral"
          className="max-w-[10rem] truncate text-[8px] border-sky-500/40 text-sky-700 dark:text-sky-300"
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
      <span
        className={cn(
          'min-w-0 flex-1 truncate text-[var(--text-dense-caption)]',
          failed ? 'text-destructive' : 'text-muted-foreground',
        )}
        title={failReason ?? statusHint}
      >
        {failReason != null && failed ? failReason : statusHint}
      </span>
      <div className="ml-auto flex flex-wrap items-center gap-2">
        {onExpandAgentDock != null && (
          <button
            type="button"
            className="text-[var(--text-dense-meta)] font-medium text-primary hover:underline"
            onClick={onExpandAgentDock}
            title="Expand Agent Execution Dock for live feed and approvals"
          >
            Expand dock
          </button>
        )}
        {onOpenAgentDesk != null && (
          <button
            type="button"
            className="text-[var(--text-dense-caption)] text-muted-foreground hover:text-primary hover:underline"
            onClick={() => onOpenAgentDesk(jobId)}
          >
            Open in Queue
          </button>
        )}
        {succeeded && onVerifyReprobe != null && (
          <button
            type="button"
            className="rounded border border-border bg-background px-2 py-0.5 text-[var(--text-dense-meta)] font-medium text-primary hover:bg-muted"
            onClick={() => {
              onVerifyReprobe()
              void qc.invalidateQueries({ queryKey: ['checklist', 'signals'] })
              void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
            }}
          >
            Re-probe fleet
          </button>
        )}
      </div>
    </div>
  )
}
