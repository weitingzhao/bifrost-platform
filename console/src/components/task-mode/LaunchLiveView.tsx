import { useEffect, useMemo, useRef } from 'react'
import { DenseTag, cn } from '@bifrost/ui'
import type { DeliveryPipelineRunView, ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { DeliveryPipelineStepProgress } from '@/components/delivery/DeliveryPipelineStepProgress'
import { StatusLamp } from '@/components/StatusLamp'
import { useAgentJobLiveSession } from '@/hooks/useAgentJobLiveSession'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
} from '@/lib/delivery/pipelineRunAskPack'

export type LaunchLiveVariant = 'satellite' | 'rocket'

export type LaunchLiveViewProps = {
  variant: LaunchLiveVariant
  jobId: string
  taskLabel?: string
  /** Active deliver pipeline runs (STG trade or platform). */
  pipelineRuns: DeliveryPipelineRunView[] | undefined
  pipelineNamespace?: string
  /** Satellite post-deploy */
  stgSmoke?: StgSmokeResponse
  stgGate?: ReleaseGateResponse
  tierB?: TierBStatusResponse
  /** Rocket post-deploy */
  platformStgGate?: ReleaseGateResponse
  platformProdGate?: ReleaseGateResponse
  supplyCmsPresent?: number
  supplyCmsTotal?: number
  onOpenDetail: () => void
  detailLabel?: string
  /** Expand shell Agent Execution Dock — SSOT for live feed / approvals. */
  onExpandAgentDock?: () => void
  /** Explicit archive escape only — do not use for approvals. */
  onOpenAgentDesk?: (jobId: string) => void
  /** Return to Go/No-Go summary without dismissing the ambient agent banner. */
  onBackToGate?: () => void
  /** Fired once when launch reaches a clear success terminal (optional). */
  onDone?: () => void
}

function pickFocusRun(runs: DeliveryPipelineRunView[] | undefined): DeliveryPipelineRunView | undefined {
  if (runs == null || runs.length === 0) return undefined
  return runs.find(r => isPipelineRunRunning(r)) ?? runs[0]
}

function smokeChipVariant(smoke: StgSmokeResponse | undefined): 'success' | 'warning' | 'neutral' {
  if (smoke == null) return 'neutral'
  return smoke.reachability === 'ok' ? 'success' : 'warning'
}

function gateChipVariant(gate: ReleaseGateResponse | undefined): 'success' | 'warning' | 'neutral' {
  if (gate == null) return 'neutral'
  return gate.result === 'pass' ? 'success' : 'warning'
}

function tierBChipVariant(tierB: TierBStatusResponse | undefined): 'success' | 'warning' | 'neutral' {
  if (tierB == null) return 'neutral'
  return tierB.ready || tierB.signed_off ? 'success' : 'warning'
}

function supplyChipVariant(present: number | undefined, total: number | undefined): 'success' | 'warning' | 'neutral' {
  if (present == null || total == null || total === 0) return 'neutral'
  return present === total ? 'success' : 'warning'
}

/**
 * Task CC in-place launch monitor — Agent one-line + Tekton steps + post-deploy chips.
 * Approvals / Commit & push live only in shell Agent Execution Dock (SSOT).
 */
export function LaunchLiveView({
  variant,
  jobId,
  taskLabel,
  pipelineRuns,
  pipelineNamespace,
  stgSmoke,
  stgGate,
  tierB,
  platformStgGate,
  platformProdGate,
  supplyCmsPresent,
  supplyCmsTotal,
  onOpenDetail,
  detailLabel,
  onExpandAgentDock,
  onOpenAgentDesk,
  onBackToGate,
  onDone,
}: LaunchLiveViewProps) {
  const {
    job,
    connected,
    error,
    isTerminal: agentTerminal,
    isApproval,
    liveFeed,
    elapsed,
    statusLabel,
  } = useAgentJobLiveSession(jobId, { autoDismissMs: 0 })

  const doneFiredRef = useRef(false)
  const nudgedApprovalRef = useRef(false)

  const isSatellite = variant === 'satellite'
  const resolvedTaskLabel =
    taskLabel ?? (isSatellite ? 'Trade · Deploy' : 'Platform · Release')
  const resolvedDetailLabel =
    detailLabel ?? (isSatellite ? 'Deploy Satellite →' : 'Launch Rocket →')
  const pipelineNameHint = isSatellite ? 'bifrost-deliver-stg' : 'bifrost-deliver-platform'
  const completeVerb = isSatellite ? 'Deploy complete' : 'Launch complete'

  const focusRun = useMemo(() => pickFocusRun(pipelineRuns), [pipelineRuns])
  const pipelineRunning = focusRun != null && isPipelineRunRunning(focusRun)
  const pipelineSucceeded = focusRun != null && isPipelineRunSucceeded(focusRun)
  const pipelineFailed = focusRun != null && isPipelineRunFailed(focusRun)
  const pipelineTerminal = pipelineSucceeded || pipelineFailed

  const agentFailed = job?.status === 'failed' || job?.status === 'cancelled'
  const agentDone = job?.status === 'done'

  const smokeOk = stgSmoke?.reachability === 'ok'
  const tradeGatePass = stgGate?.result === 'pass'
  const tierBOk = tierB != null && (tierB.ready || tierB.signed_off)
  const platformStgPass = platformStgGate?.result === 'pass'
  const supplyOk =
    supplyCmsPresent != null &&
    supplyCmsTotal != null &&
    supplyCmsTotal > 0 &&
    supplyCmsPresent === supplyCmsTotal

  const postDeployReady = pipelineSucceeded && (agentDone || agentTerminal)
  const postDeployClear = isSatellite
    ? smokeOk && tradeGatePass
    : platformStgPass
  const postDeployPending = postDeployReady && !postDeployClear

  const allClear = postDeployReady && postDeployClear
  const hasIssue = agentFailed || pipelineFailed || postDeployPending

  const headerLamp: 'ok' | 'degraded' | 'fail' | 'unknown' = hasIssue
    ? 'fail'
    : allClear
      ? 'ok'
      : 'degraded'

  const headerTitle = allClear
    ? completeVerb
    : isApproval
      ? 'Needs your decision'
      : agentFailed
        ? 'Agent failed'
        : pipelineFailed
          ? 'Pipeline failed'
          : postDeployPending
            ? 'Post-deploy checks pending'
            : pipelineRunning
              ? 'Pipeline running'
              : 'Agent working'

  const agentStatusHint = isApproval
    ? 'Needs your decision — expand dock to approve'
    : agentFailed
      ? (job?.error ?? job?.summary ?? 'Agent task failed')
      : agentDone && job?.summary != null && job.summary !== ''
        ? job.summary
        : liveFeed?.text ??
          (connected
            ? statusLabel
            : error != null
              ? error
              : agentTerminal
                ? statusLabel
                : 'Connecting…')

  // P0: awaiting_approval → auto-expand Dock once per approval cycle (SSOT).
  useEffect(() => {
    if (!isApproval) {
      nudgedApprovalRef.current = false
      return
    }
    if (nudgedApprovalRef.current) return
    nudgedApprovalRef.current = true
    onExpandAgentDock?.()
  }, [isApproval, onExpandAgentDock])

  useEffect(() => {
    if (!allClear || doneFiredRef.current) return
    doneFiredRef.current = true
    const t = window.setTimeout(() => onDone?.(), 5000)
    return () => window.clearTimeout(t)
  }, [allClear, onDone])

  const ns = pipelineNamespace ?? focusRun?.namespace

  return (
    <div
      className={cn(
        'flex flex-col gap-2 rounded-lg border px-3 py-2.5',
        allClear
          ? 'border-success/40 bg-success/5'
          : hasIssue
            ? 'border-destructive/40 bg-destructive/5'
            : isApproval
              ? 'border-warning/50 bg-warning/5'
              : 'border-primary/30 bg-primary/5',
      )}
      role="region"
      aria-label="Launch live view"
    >
      {/* Header */}
      <div className="flex flex-wrap items-center gap-x-2.5 gap-y-1">
        <StatusLamp value={headerLamp} kind="reach" />
        <span className="text-[var(--text-dense-meta)] font-semibold uppercase tracking-wide">
          Launch live
        </span>
        <DenseTag
          variant={allClear ? 'success' : hasIssue ? 'danger' : isApproval ? 'warning' : 'warning'}
          className="text-[9px]"
        >
          {headerTitle}
        </DenseTag>
        {elapsed != null && !allClear && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">{elapsed}</span>
        )}
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {onBackToGate != null && (
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={onBackToGate}
            >
              Back to Go/No-Go
            </button>
          )}
          <button
            type="button"
            className="text-[var(--text-dense-caption)] text-primary hover:underline"
            onClick={onOpenDetail}
          >
            {resolvedDetailLabel}
          </button>
        </div>
      </div>

      {allClear && (
        <p className="m-0 text-[var(--text-dense-caption)] text-success">
          {completeVerb} — reverting to Go/No-Go when the agent banner dismisses (or use Back).
        </p>
      )}

      {/* Phase 1 — Agent (one-line summary; Dock is SSOT for feed/approvals) */}
      <section
        className={cn(
          'flex flex-wrap items-center gap-x-2 gap-y-1 rounded-md border px-2.5 py-1.5',
          isApproval
            ? 'border-warning/50 bg-warning/5'
            : agentFailed
              ? 'border-destructive/35 bg-destructive/5'
              : agentDone
                ? 'border-success/35 bg-success/5'
                : 'border-border/60 bg-card',
        )}
      >
        <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
          1 · Agent
        </span>
        <DenseTag variant="neutral" className="text-[8px]">
          {resolvedTaskLabel}
        </DenseTag>
        <AgentPhaseIndicator
          currentPhase={job?.phase}
          failed={job?.status === 'failed'}
          compact
          interactive
        />
        {isApproval && (
          <DenseTag variant="warning" className="text-[8px]">
            Needs your decision
          </DenseTag>
        )}
        <span
          className={cn(
            'min-w-0 flex-1 truncate text-[var(--text-dense-caption)]',
            agentFailed ? 'text-destructive' : 'text-muted-foreground',
          )}
          title={agentStatusHint}
        >
          {agentStatusHint}
        </span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          {onExpandAgentDock != null ? (
            <button
              type="button"
              className="text-[var(--text-dense-meta)] font-medium text-primary hover:underline"
              onClick={onExpandAgentDock}
              title="Expand Agent Execution Dock for live feed and approvals"
            >
              Expand dock
            </button>
          ) : (
            <span className="text-[var(--text-dense-caption)] text-warning">
              Expand the Agent Execution Dock to approve or view the live feed
            </span>
          )}
          {onOpenAgentDesk != null && (
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-muted-foreground hover:text-primary hover:underline"
              onClick={() => onOpenAgentDesk(jobId)}
            >
              Open in Agent Desk
            </button>
          )}
        </div>
      </section>

      {/* Phase 2 — Pipeline */}
      <section className="rounded-md border border-border/60 bg-card px-2.5 py-2">
        <div className="mb-1.5 flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
            2 · Pipeline
          </span>
          {focusRun != null ? (
            <>
              <DenseTag
                variant={
                  pipelineSucceeded
                    ? 'success'
                    : pipelineFailed
                      ? 'danger'
                      : pipelineRunning
                        ? 'warning'
                        : 'neutral'
                }
                className="text-[8px]"
              >
                {formatPipelineRunStatus(focusRun)}
              </DenseTag>
              <span className="min-w-0 truncate font-mono text-[var(--text-dense-micro)] text-muted-foreground">
                {focusRun.name}
              </span>
            </>
          ) : (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Waiting for {pipelineNameHint} PipelineRun…
            </span>
          )}
        </div>

        {focusRun != null ? (
          <DeliveryPipelineStepProgress
            runName={focusRun.name}
            namespace={ns}
            pollUntilTerminal={pipelineRunning || !pipelineTerminal}
            runTerminal={
              pipelineTerminal ? (pipelineSucceeded ? 'succeeded' : 'failed') : undefined
            }
            runRunning={pipelineRunning}
          />
        ) : (
          <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
            Agent will start the deliver pipeline — steps appear here once Tekton schedules the run.
          </p>
        )}
      </section>

      {/* Phase 3 — Post-deploy */}
      <section className="rounded-md border border-border/60 bg-card px-2.5 py-2">
        <div className="mb-1.5 flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
            3 · Post-deploy
          </span>
          {!postDeployReady && (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Activates after pipeline succeeds
            </span>
          )}
        </div>
        <div className="flex flex-wrap gap-1.5">
          {isSatellite ? (
            <>
              <DenseTag variant={smokeChipVariant(stgSmoke)} className="text-[9px]">
                Smoke ·{' '}
                {stgSmoke == null ? '…' : smokeOk ? 'pass' : stgSmoke.reachability}
              </DenseTag>
              <DenseTag variant={gateChipVariant(stgGate)} className="text-[9px]">
                Gate · {stgGate == null ? '…' : stgGate.result}
              </DenseTag>
              <DenseTag variant={tierBChipVariant(tierB)} className="text-[9px]">
                Tier B · {tierB == null ? '…' : tierBOk ? 'ready' : 'pending'}
              </DenseTag>
            </>
          ) : (
            <>
              <DenseTag variant={gateChipVariant(platformStgGate)} className="text-[9px]">
                STG gate · {platformStgGate == null ? '…' : platformStgGate.result}
              </DenseTag>
              <DenseTag variant={gateChipVariant(platformProdGate)} className="text-[9px]">
                PROD gate · {platformProdGate == null ? '…' : platformProdGate.result}
              </DenseTag>
              <DenseTag
                variant={supplyChipVariant(supplyCmsPresent, supplyCmsTotal)}
                className="text-[9px]"
              >
                CMs ·{' '}
                {supplyCmsPresent == null || supplyCmsTotal == null
                  ? '…'
                  : `${supplyCmsPresent}/${supplyCmsTotal}`}
                {supplyOk ? ' ok' : ''}
              </DenseTag>
            </>
          )}
        </div>
        {postDeployPending && (
          <p className="m-0 mt-1.5 text-[var(--text-dense-caption)] text-warning">
            {isSatellite
              ? 'Pipeline succeeded — confirm smoke and STG gate on Deploy Satellite if chips stay yellow.'
              : 'Pipeline succeeded — confirm platform STG gate on Launch Rocket if chips stay yellow.'}
          </p>
        )}
      </section>
    </div>
  )
}
