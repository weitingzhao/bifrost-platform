import { useEffect, useMemo, useRef, useState } from 'react'
import { useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, cn } from '@bifrost/ui'
import { ChevronDown, ChevronUp } from 'lucide-react'
import type { DeliveryPipelineRunView, ReleaseGateResponse, StgSmokeResponse, TierBStatusResponse } from '@/api/deliveryTypes'
import { respondRemediationJob } from '@/api/remediation'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { DeliveryPipelineStepProgress } from '@/components/delivery/DeliveryPipelineStepProgress'
import { StatusLamp } from '@/components/StatusLamp'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import {
  bannerStatusLabel,
  deriveAgentFeedStats,
  deriveAgentLiveFeed,
  feedKindLabel,
  formatAgentElapsed,
  formatFeedEventLine,
  recentAgentFeedEvents,
} from '@/lib/agent/agentLiveFeed'
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
 * Task CC in-place launch monitor — Agent phases + Tekton steps + post-deploy chips.
 * Shared by Satellite Deploy and Rocket Launch while the matching ambient agent is active.
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
  onOpenAgentDesk,
  onBackToGate,
  onDone,
}: LaunchLiveViewProps) {
  const qc = useQueryClient()
  const { job, events, connected, error } = useRemediationStream(jobId)
  const [expanded, setExpanded] = useState(false)
  const [nowMs, setNowMs] = useState(() => Date.now())
  const doneFiredRef = useRef(false)

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

  const agentTerminal =
    job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
  const agentFailed = job?.status === 'failed' || job?.status === 'cancelled'
  const agentDone = job?.status === 'done'
  const isApproval = job?.phase === 'awaiting_approval' && !agentTerminal

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

  const liveFeed = useMemo(() => deriveAgentLiveFeed(events), [events])
  const feedStats = useMemo(() => deriveAgentFeedStats(events), [events])
  const recentEvents = useMemo(() => recentAgentFeedEvents(events, 10), [events])
  const elapsed = formatAgentElapsed(job?.created_at, nowMs)

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
  const hasIssue =
    agentFailed || pipelineFailed || postDeployPending

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

  const bannerVariant = agentTerminal
    ? agentDone
      ? 'done'
      : 'failed'
    : isApproval
      ? 'approval'
      : 'running'

  useEffect(() => {
    if (agentTerminal) return
    const id = window.setInterval(() => setNowMs(Date.now()), 1000)
    return () => window.clearInterval(id)
  }, [agentTerminal])

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

      {/* Operator decision — keep in Task CC so launch flow does not break to Agent Desk */}
      {pendingApproval != null && (
        <div className="rounded-md border border-warning/40 bg-card px-2.5 py-2">
          <RemediationApprovalBlock
            event={pendingApproval}
            submitting={respondMutation.isPending}
            onRespond={(optionId, note, commitMessage) =>
              respondMutation.mutate({ optionId, note, commitMessage })
            }
          />
        </div>
      )}

      {isApproval && pendingApproval == null && (
        <p className="m-0 rounded-md border border-warning/30 bg-card px-2.5 py-2 text-[var(--text-dense-caption)] text-warning">
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

      {/* Phase 1 — Agent */}
      <section className="rounded-md border border-border/60 bg-card px-2.5 py-2">
        <div className="mb-1.5 flex flex-wrap items-center gap-2">
          <span className="text-[var(--text-dense-caption)] font-semibold uppercase tracking-wide text-muted-foreground">
            1 · Agent
          </span>
          <DenseTag variant="neutral" className="text-[8px]">
            {resolvedTaskLabel}
          </DenseTag>
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">
            {bannerStatusLabel(bannerVariant, job)}
          </span>
          {!connected && !agentTerminal && error == null && (
            <span className="text-[var(--text-dense-micro)] text-muted-foreground">connecting…</span>
          )}
          {onOpenAgentDesk != null && (
            <button
              type="button"
              className="ml-auto text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={() => onOpenAgentDesk(jobId)}
            >
              Agent Desk →
            </button>
          )}
        </div>

        <div className="flex flex-wrap items-center gap-2">
          <AgentPhaseIndicator
            currentPhase={job?.phase}
            failed={job?.status === 'failed'}
            compact
            interactive
          />
          {liveFeed != null && !agentTerminal && !isApproval && (
            <div className="flex min-w-0 flex-1 items-center gap-1.5">
              <span className="shrink-0 text-[var(--text-dense-micro)] font-medium text-muted-foreground">
                {feedKindLabel(liveFeed.kind)}
              </span>
              <span
                className="min-w-0 truncate text-[var(--text-dense-caption)]"
                title={liveFeed.text}
              >
                {liveFeed.text}
              </span>
            </div>
          )}
          {liveFeed == null && !agentTerminal && connected && !isApproval && (
            <span className="text-[var(--text-dense-caption)] text-muted-foreground">
              Waiting for agent activity…
            </span>
          )}
        </div>

        {(feedStats.toolCalls > 0 || feedStats.eventCount > 0) && !agentTerminal && (
          <p className="m-0 mt-1 text-[var(--text-dense-micro)] text-muted-foreground">
            {feedStats.toolCalls > 0 &&
              `${feedStats.toolCalls} tool${feedStats.toolCalls === 1 ? '' : 's'}`}
            {feedStats.toolCalls > 0 && feedStats.eventCount > feedStats.toolCalls && ' · '}
            {feedStats.eventCount > feedStats.toolCalls && `${feedStats.eventCount} events`}
          </p>
        )}

        {error != null && !agentTerminal && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-destructive">
            Connection: {error}
          </p>
        )}

        {agentFailed && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-destructive">
            {job?.error ?? job?.summary ?? 'Agent task failed'}
          </p>
        )}

        {agentDone && job?.summary != null && job.summary !== '' && (
          <p className="m-0 mt-1 text-[var(--text-dense-caption)] text-muted-foreground">
            {job.summary}
          </p>
        )}

        {!agentTerminal && (
          <div className="mt-1.5">
            <Button
              variant="ghost"
              size="xs"
              onClick={() => setExpanded(v => !v)}
              aria-expanded={expanded}
            >
              {expanded ? (
                <ChevronUp className="size-3" aria-hidden />
              ) : (
                <ChevronDown className="size-3" aria-hidden />
              )}
              {expanded ? 'Hide feed' : 'Details'}
            </Button>
            {expanded && (
              <ul className="m-0 mt-1 max-h-40 list-none space-y-0.5 overflow-y-auto p-0 dense-scroll-y">
                {recentEvents.length === 0 ? (
                  <li className="text-[var(--text-dense-caption)] text-muted-foreground">
                    Waiting for agent activity…
                  </li>
                ) : (
                  recentEvents.map(ev => (
                    <li
                      key={ev.id}
                      className="flex gap-1.5 text-[var(--text-dense-caption)] text-muted-foreground"
                    >
                      <span className="shrink-0 font-mono text-[var(--text-dense-micro)]">
                        {ev.type}
                      </span>
                      <span className="min-w-0 truncate">{formatFeedEventLine(ev)}</span>
                    </li>
                  ))
                )}
              </ul>
            )}
          </div>
        )}
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
