import { useEffect, useMemo, useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { Bot, ChevronRight, History, Loader2, Target } from 'lucide-react'
import { DenseTag, SegmentControl, cn } from '@bifrost/ui'
import { fetchRemediationJobs } from '@/api/platform'
import type { OperateQueueItem } from '@/api/operateQueueTypes'
import type { RemediationJob } from '@/api/types'
import { DailyOpsAgentLivePanel } from '@/components/task-mode/DailyOpsProcessStrip'
import { useOperateQueue } from '@/hooks/useOperateQueue'
import { scopeToLabel } from '@/lib/agent/agentTaskCatalog'
import {
  isRecentClosedItem,
  linkedRemediationJob,
  originFromOperateItem,
  originFromRemediationJob,
  partitionOpenQueue,
  queueLinkedJobChip,
  type QueueLane,
  type QueueOriginKind,
} from '@/lib/control-room/dailyOpsExecutionQueue'
import {
  fixPathLabel,
  fixTargetNextStep,
  resolveAmbientJobFixTarget,
  type DailyOpsBlocker,
} from '@/lib/control-room/dailyOpsPrimaryBlocker'
import { operateQueueClearLabel } from '@/lib/control-room/fleetSnapshot'
import {
  formatRemediationJobWhen,
  remediationJobStatusLabel,
  remediationScopeShortLabel,
} from '@/lib/remediation/remediationJobDisplay'

export type DailyOpsExecutionTab = 'now' | 'queue-history'

export type DailyOpsExecutionPanelProps = {
  onNavigate: (tab: string) => void
  fleetClear: boolean
  /** When remediating with active job or open queue, strip uses warning tone. */
  remediating?: boolean
  ambientJobId?: string | null
  ambientJobScope?: string | null
  onOpenAgentDesk?: (jobId?: string) => void
  /** Pending start before ambient job id is assigned. */
  showStartingHint?: boolean
  /** Empty Now → Ops loop primary CTA (Discover / Remediate / …). */
  onOpsLoopAction?: () => void
  opsLoopActionLabel?: string
  /**
   * Same primary blocker as Ops loop CTA (from resolveDailyOpsWorkflow).
   * Do not recompute here — keeps Fix target aligned with the strip.
   */
  primaryBlocker?: DailyOpsBlocker | null
  /** Ops loop primary CTA label (same string as strip / empty-state button). */
  primaryActionLabel?: string | null
  /** Checklist row Fix active id — correlates ambient job with item when present. */
  checklistItemFixActiveId?: string | null
  /** Verify / Re-probe — same as Ops loop verify CTA (invalidate cockpit). */
  onVerifyReprobe?: () => void
  /** Adopt an existing remediation job as ambient (Queue → Now). */
  onAdoptJob?: (job: { id: string; scope: string; label: string }) => void
}

function originTagProps(kind: QueueOriginKind): {
  label: string
  variant: 'neutral' | 'warning' | 'success' | 'danger' | 'category'
} {
  switch (kind) {
    case 'human':
      return { label: 'human', variant: 'neutral' }
    case 'skipped':
      return { label: 'skipped', variant: 'neutral' }
    case 'ask-ai':
      return { label: 'ask-ai', variant: 'category' }
    case 'queue':
      return { label: 'queue', variant: 'warning' }
    case 'handoff':
      return { label: 'handoff', variant: 'category' }
    case 'agent':
    default:
      return { label: 'agent', variant: 'warning' }
  }
}

type HistoryRow =
  | {
      key: string
      kind: 'job'
      at: string
      title: string
      statusLabel: string
      origin: QueueOriginKind
      jobId: string
    }
  | {
      key: string
      kind: 'closed'
      at: string
      title: string
      statusLabel: string
      origin: QueueOriginKind
    }

const HISTORY_LIMIT = 12

type QueueFilter = 'all' | QueueLane

function DailyOpsFixTargetBar({
  primaryBlocker,
  primaryActionLabel,
  jobTarget,
  hasAmbientJob,
}: {
  primaryBlocker?: DailyOpsBlocker | null
  primaryActionLabel?: string | null
  jobTarget: ReturnType<typeof resolveAmbientJobFixTarget>
  hasAmbientJob: boolean
}) {
  if (jobTarget == null && primaryBlocker == null) return null

  const showJobAsPrimary = hasAmbientJob && jobTarget != null
  const title = showJobAsPrimary ? jobTarget.label : (primaryBlocker?.label ?? '')
  const path = showJobAsPrimary
    ? jobTarget.pathLabel
    : primaryBlocker != null
      ? fixPathLabel(primaryBlocker)
      : ''
  const nextStep =
    primaryBlocker != null ? fixTargetNextStep(primaryBlocker, primaryActionLabel) : null
  const itemId = showJobAsPrimary ? jobTarget.itemId : (primaryBlocker?.itemId ?? null)
  const scopeHint = showJobAsPrimary ? jobTarget.fixScope : primaryBlocker?.fixScope
  const misaligned =
    showJobAsPrimary &&
    jobTarget != null &&
    !jobTarget.alignsWithPrimary &&
    primaryBlocker != null

  return (
    <div
      className="mb-2 rounded-md border border-border/50 bg-background/70 px-2.5 py-1.5"
      aria-label="Current fix target"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-x-2 gap-y-0.5">
        <Target size={12} className="shrink-0 text-muted-foreground" aria-hidden />
        <span className="text-[var(--text-dense-caption)] font-semibold text-foreground">
          Fix target:
        </span>
        <span className="min-w-0 truncate text-[var(--text-dense-meta)] text-foreground">
          {title}
        </span>
        {path !== '' && (
          <DenseTag variant="neutral" className="shrink-0 text-[8px]">
            Path: {path}
          </DenseTag>
        )}
        {showJobAsPrimary && jobTarget?.alignsWithPrimary && primaryBlocker != null && (
          <DenseTag variant="success" className="shrink-0 text-[8px]">
            Matches loop
          </DenseTag>
        )}
      </div>
      {!showJobAsPrimary && nextStep != null && (
        <p className="m-0 mt-0.5 pl-[1.25rem] text-[var(--text-dense-caption)] text-muted-foreground">
          Next: {nextStep}
        </p>
      )}
      {misaligned && (
        <p className="m-0 mt-0.5 pl-[1.25rem] text-[var(--text-dense-micro)] text-muted-foreground">
          Running: {jobTarget.label}
          {' · '}
          Loop next: {nextStep ?? primaryBlocker.label}
        </p>
      )}
      {showJobAsPrimary && !misaligned && scopeHint != null && scopeHint !== '' && (
        <p className="m-0 mt-0.5 pl-[1.25rem] text-[var(--text-dense-caption)] text-muted-foreground">
          Scope: {remediationScopeShortLabel(scopeHint)}
        </p>
      )}
      {itemId != null && itemId !== '' && (
        <p
          className="m-0 mt-0.5 pl-[1.25rem] font-mono text-[var(--text-dense-micro)] text-muted-foreground/80"
          title={itemId}
        >
          {itemId}
        </p>
      )}
    </div>
  )
}

function QueueItemRow({
  item,
  allJobs,
  ambientJobId,
  onNavigate,
  onFocusNow,
  onAdoptJob,
}: {
  item: OperateQueueItem
  allJobs: RemediationJob[]
  ambientJobId?: string | null
  onNavigate: (tab: string) => void
  onFocusNow: () => void
  onAdoptJob?: (job: { id: string; scope: string; label: string }) => void
}) {
  const origin = originFromOperateItem(item)
  const tag = originTagProps(origin)
  const linked = linkedRemediationJob(item, allJobs)
  const jobChip = linked != null ? queueLinkedJobChip(linked) : null
  const canOpenNow =
    linked != null &&
    jobChip != null &&
    (linked.id === ambientJobId || onAdoptJob != null)

  return (
    <li className="flex min-w-0 items-center gap-1.5 rounded border border-border/40 bg-background/50 px-2 py-1">
      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-foreground">
        {item.title}
      </span>
      <DenseTag variant={tag.variant} className="shrink-0 text-[8px]">
        {tag.label}
      </DenseTag>
      {jobChip != null && (
        <DenseTag variant={jobChip.variant} className="shrink-0 text-[8px]">
          {jobChip.label}
        </DenseTag>
      )}
      {canOpenNow ? (
        <button
          type="button"
          className="inline-flex shrink-0 items-center gap-0.5 text-[var(--text-dense-caption)] text-primary hover:underline"
          onClick={() => {
            if (linked != null && linked.id !== ambientJobId && onAdoptJob != null) {
              onAdoptJob({
                id: linked.id,
                scope: linked.scope ?? '',
                label: scopeToLabel(linked.scope),
              })
            }
            onFocusNow()
          }}
        >
          Now
          <ChevronRight className="size-3" aria-hidden />
        </button>
      ) : (
        <button
          type="button"
          className="inline-flex shrink-0 items-center gap-0.5 text-[var(--text-dense-caption)] text-primary hover:underline"
          onClick={() => onNavigate('control-room')}
        >
          Open Control Room
          <ChevronRight className="size-3" aria-hidden />
        </button>
      )}
    </li>
  )
}

export function DailyOpsExecutionPanel({
  onNavigate,
  fleetClear,
  remediating = false,
  ambientJobId,
  ambientJobScope,
  onOpenAgentDesk,
  showStartingHint = false,
  onOpsLoopAction,
  opsLoopActionLabel = 'Ops loop →',
  primaryBlocker = null,
  primaryActionLabel = null,
  checklistItemFixActiveId = null,
  onVerifyReprobe,
  onAdoptJob,
}: DailyOpsExecutionPanelProps) {
  const hasAmbientJob = ambientJobId != null && ambientJobId !== ''
  const preferNow =
    hasAmbientJob || showStartingHint || checklistItemFixActiveId != null

  const [tab, setTab] = useState<DailyOpsExecutionTab>(preferNow ? 'now' : 'queue-history')
  const [queueFilter, setQueueFilter] = useState<QueueFilter>('all')

  useEffect(() => {
    if (preferNow) setTab('now')
  }, [preferNow, ambientJobId, checklistItemFixActiveId, showStartingHint])

  const queueQ = useOperateQueue()
  const open = queueQ.data?.open ?? []
  const recentClosed = queueQ.data?.recent_closed ?? []

  const jobsQ = useQuery({
    queryKey: ['remediation', 'jobs'],
    queryFn: fetchRemediationJobs,
    refetchInterval: 15_000,
  })
  const allJobs = jobsQ.data?.jobs ?? []

  const partitioned = useMemo(() => partitionOpenQueue(open), [open])
  const badgeCount = partitioned.actionable

  const visibleQueue = useMemo(() => {
    if (queueFilter === 'human') return partitioned.human
    if (queueFilter === 'agent') return partitioned.agent
    return [...partitioned.human, ...partitioned.agent]
  }, [queueFilter, partitioned])

  const jobFixTarget = useMemo(
    () =>
      resolveAmbientJobFixTarget({
        jobScope: ambientJobScope,
        checklistItemId: checklistItemFixActiveId,
        primaryBlocker,
        scopeFallbackLabel:
          ambientJobScope != null && ambientJobScope !== ''
            ? remediationScopeShortLabel(ambientJobScope)
            : null,
      }),
    [ambientJobScope, checklistItemFixActiveId, primaryBlocker],
  )

  const historyRows = useMemo(() => {
    const terminalJobs: HistoryRow[] = allJobs
      .filter(j => j.status === 'done' || j.status === 'failed' || j.status === 'cancelled')
      .map(j => ({
        key: `job-${j.id}`,
        kind: 'job' as const,
        at: j.updated_at || j.created_at,
        title: remediationScopeShortLabel(j.scope),
        statusLabel: remediationJobStatusLabel(j),
        origin: originFromRemediationJob(j),
        jobId: j.id,
      }))

    const closedRows: HistoryRow[] = recentClosed
      .filter(item => isRecentClosedItem(item))
      .filter(item => {
        const origin = originFromOperateItem(item)
        if (origin !== 'skipped') return true
        const blob = [item.reason, item.title].filter(Boolean).join(' ').toLowerCase()
        return !(blob.includes('dedup') || blob.includes('24h'))
      })
      .map(item => ({
        key: `closed-${item.id}`,
        kind: 'closed' as const,
        at: item.closed_at || item.updated_at || item.created_at,
        title: item.title,
        statusLabel: 'Closed',
        origin: originFromOperateItem(item),
      }))

    return [...terminalJobs, ...closedRows]
      .sort((a, b) => Date.parse(b.at) - Date.parse(a.at))
      .slice(0, HISTORY_LIMIT)
  }, [allJobs, recentClosed])

  const historyLoading = jobsQ.isLoading || queueQ.isLoading
  const historyAvailable = !jobsQ.isError || recentClosed.length > 0 || allJobs.length > 0
  const clearLabel = operateQueueClearLabel(badgeCount, fleetClear)
  const clearButFleetNot = badgeCount === 0 && !fleetClear
  const activeTone =
    remediating && (hasAmbientJob || showStartingHint || badgeCount > 0)

  const focusNow = () => {
    setTab('now')
    requestAnimationFrame(() => {
      document
        .querySelector('[data-daily-ops-execution]')
        ?.scrollIntoView({ behavior: 'smooth', block: 'nearest' })
    })
  }

  return (
    <div
      data-daily-ops-execution
      className={cn(
        'rounded-md border px-2.5 py-2',
        activeTone
          ? 'border-amber-500/45 bg-amber-500/10'
          : 'border-border/60 bg-secondary/80',
      )}
      aria-label="Execution"
    >
      <div className="flex min-w-0 flex-wrap items-center gap-2">
        <Bot
          size={14}
          className={cn(
            'shrink-0',
            activeTone ? 'text-amber-800 dark:text-amber-200' : 'text-muted-foreground',
          )}
          aria-hidden
        />
        <div className="min-w-0">
          <span
            className={cn(
              'text-[var(--text-dense-caption)] font-semibold',
              activeTone ? 'text-amber-900 dark:text-amber-100' : 'text-foreground',
            )}
          >
            Execution
          </span>
          <span className="ml-1.5 text-[var(--text-dense-micro)] text-muted-foreground">
            Agent & operate
          </span>
        </div>

        <SegmentControl
          size="xs"
          ariaLabel="Execution view"
          className="shrink-0"
          value={tab}
          onChange={v => setTab(v as DailyOpsExecutionTab)}
          options={[
            {
              value: 'now',
              label: (
                <span className="inline-flex items-center gap-1">
                  Now
                  {(hasAmbientJob || showStartingHint) && (
                    <span className="inline-block size-1.5 rounded-full bg-sky-500" aria-hidden />
                  )}
                </span>
              ),
            },
            {
              value: 'queue-history',
              label: (
                <span className="inline-flex items-center gap-1">
                  Queue & History
                  {badgeCount > 0 && (
                    <DenseTag variant="warning" className="text-[8px]">
                      {badgeCount}
                    </DenseTag>
                  )}
                </span>
              ),
            },
          ]}
        />

        <span className="ml-auto flex shrink-0 items-center gap-2">
          <DenseTag
            variant={
              badgeCount === 0 && fleetClear ? 'success' : clearButFleetNot ? 'neutral' : 'warning'
            }
            className="text-[8px]"
          >
            {clearLabel}
          </DenseTag>
          <button
            type="button"
            className={cn(
              'text-[var(--text-dense-caption)] font-medium hover:underline',
              activeTone ? 'text-amber-900 dark:text-amber-100' : 'text-primary',
            )}
            onClick={() => onNavigate('control-room')}
          >
            Open Control Room
          </button>
        </span>
      </div>

      {tab === 'now' ? (
        <div className="mt-2">
          <DailyOpsFixTargetBar
            primaryBlocker={primaryBlocker}
            primaryActionLabel={primaryActionLabel}
            jobTarget={hasAmbientJob || showStartingHint ? jobFixTarget : null}
            hasAmbientJob={hasAmbientJob || showStartingHint}
          />
          {hasAmbientJob && ambientJobId != null ? (
            <DailyOpsAgentLivePanel
              jobId={ambientJobId}
              jobScope={ambientJobScope}
              onOpenAgentDesk={onOpenAgentDesk}
              onVerifyReprobe={onVerifyReprobe}
            />
          ) : showStartingHint ? (
            <div className="flex items-center gap-1.5 rounded-md border border-border/60 bg-background/80 px-2.5 py-2 text-[var(--text-dense-caption)] text-muted-foreground">
              <Loader2 className="size-3 animate-spin" aria-hidden />
              Starting Agent…
            </div>
          ) : (
            <div className="rounded-md border border-border/50 bg-background/60 px-2.5 py-3 text-center">
              {primaryBlocker != null ? (
                <>
                  <p className="m-0 text-[var(--text-dense-meta)] font-medium text-muted-foreground">
                    No agent run — start from Ops loop
                  </p>
                  <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground/80">
                    Primary action is in Ops loop
                    {primaryActionLabel != null && primaryActionLabel !== ''
                      ? ` · ${primaryActionLabel.replace(/\s*→\s*$/, '')}`
                      : ''}
                    .
                  </p>
                </>
              ) : (
                <>
                  <p className="m-0 text-[var(--text-dense-meta)] font-medium text-muted-foreground">
                    No open fix target
                  </p>
                  <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground/80">
                    Primary action is in Ops loop — Discover, Remediate, or Verify.
                  </p>
                </>
              )}
              {onOpsLoopAction != null && (
                <button
                  type="button"
                  className="mt-2 text-[var(--text-dense-caption)] font-medium text-primary hover:underline"
                  onClick={onOpsLoopAction}
                >
                  {opsLoopActionLabel.startsWith('Start')
                    ? opsLoopActionLabel
                    : `Start from Ops loop · ${opsLoopActionLabel}`}
                </button>
              )}
            </div>
          )}
        </div>
      ) : (
        <div className="mt-2 flex flex-col gap-2.5">
          <section aria-label="Operate queue">
            <div className="mb-1 flex flex-wrap items-center gap-1.5">
              <span className="text-[var(--text-dense-caption)] font-medium text-foreground">
                Queue
              </span>
              <DenseTag variant={badgeCount > 0 ? 'warning' : 'success'} className="text-[8px]">
                {badgeCount} pending
              </DenseTag>
              {partitioned.humanActionable > 0 && (
                <DenseTag variant="neutral" className="text-[8px]">
                  Human {partitioned.humanActionable}
                </DenseTag>
              )}
              {partitioned.agentActionable > 0 && (
                <DenseTag variant="warning" className="text-[8px]">
                  Agent {partitioned.agentActionable}
                </DenseTag>
              )}
              {partitioned.noise.length > 0 && (
                <span className="text-[var(--text-dense-micro)] text-muted-foreground">
                  · {partitioned.noise.length} skip/dedup hidden
                </span>
              )}
              {clearButFleetNot && (
                <span className="text-[var(--text-dense-micro)] text-muted-foreground">
                  Queue clear ≠ fleet clear
                </span>
              )}
            </div>
            <SegmentControl
              size="xs"
              ariaLabel="Queue lane"
              className="mb-1.5"
              value={queueFilter}
              onChange={v => setQueueFilter(v as QueueFilter)}
              options={[
                { value: 'all', label: `All (${badgeCount})` },
                { value: 'human', label: `Human (${partitioned.humanActionable})` },
                { value: 'agent', label: `Agent (${partitioned.agentActionable})` },
              ]}
            />
            {queueQ.isLoading ? (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                Loading queue…
              </p>
            ) : visibleQueue.length === 0 ? (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                {badgeCount === 0
                  ? 'No actionable operate-queue items'
                  : `No ${queueFilter === 'all' ? '' : `${queueFilter} `}items in this lane`}
              </p>
            ) : (
              <ul className="m-0 list-none space-y-1 p-0">
                {visibleQueue.slice(0, 8).map(item => (
                  <QueueItemRow
                    key={item.id}
                    item={item}
                    allJobs={allJobs}
                    ambientJobId={ambientJobId}
                    onNavigate={onNavigate}
                    onFocusNow={focusNow}
                    onAdoptJob={onAdoptJob}
                  />
                ))}
                {visibleQueue.length > 8 && (
                  <li className="text-[var(--text-dense-micro)] text-muted-foreground">
                    +{visibleQueue.length - 8} more in Control Room
                  </li>
                )}
              </ul>
            )}
          </section>

          <section aria-label="Execution history">
            <div className="mb-1 flex items-center gap-1.5">
              <History size={12} className="text-muted-foreground" aria-hidden />
              <span className="text-[var(--text-dense-caption)] font-medium text-foreground">
                History
              </span>
              <span className="text-[var(--text-dense-micro)] text-muted-foreground">
                terminal only
              </span>
            </div>
            {historyLoading ? (
              <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">
                Loading history…
              </p>
            ) : !historyAvailable && historyRows.length === 0 ? (
              <div className="rounded-md border border-dashed border-border/60 px-2.5 py-2">
                <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                  History coming online
                </p>
                <p className="m-0 mt-0.5 text-[var(--text-dense-caption)] text-muted-foreground/80">
                  Remediation jobs and closed handoffs will appear here when available.
                </p>
                <div className="mt-1.5 flex flex-wrap gap-2">
                  <button
                    type="button"
                    className="text-[var(--text-dense-caption)] text-primary hover:underline"
                    onClick={() => onOpenAgentDesk?.()}
                  >
                    Agent Desk →
                  </button>
                  <button
                    type="button"
                    className="text-[var(--text-dense-caption)] text-primary hover:underline"
                    onClick={() => onNavigate('control-room')}
                  >
                    Open Control Room
                  </button>
                </div>
              </div>
            ) : historyRows.length === 0 ? (
              <div className="rounded-md border border-dashed border-border/60 px-2.5 py-2">
                <p className="m-0 text-[var(--text-dense-meta)] text-muted-foreground">
                  No recent terminal runs
                </p>
                <div className="mt-1.5 flex flex-wrap gap-2">
                  <button
                    type="button"
                    className="text-[var(--text-dense-caption)] text-primary hover:underline"
                    onClick={() => onOpenAgentDesk?.()}
                  >
                    Agent Desk →
                  </button>
                  <button
                    type="button"
                    className="text-[var(--text-dense-caption)] text-primary hover:underline"
                    onClick={() => onNavigate('control-room')}
                  >
                    Open Control Room
                  </button>
                </div>
              </div>
            ) : (
              <ul className="m-0 max-h-40 list-none space-y-1 overflow-y-auto p-0">
                {historyRows.map(row => {
                  const tag = originTagProps(row.origin)
                  return (
                    <li
                      key={row.key}
                      className="flex min-w-0 items-center gap-1.5 rounded border border-border/40 bg-background/50 px-2 py-1"
                    >
                      <span className="shrink-0 font-mono text-[var(--text-dense-micro)] text-muted-foreground">
                        {formatRemediationJobWhen(row.at)}
                      </span>
                      <span className="min-w-0 flex-1 truncate text-[var(--text-dense-meta)] text-foreground">
                        {row.title}
                      </span>
                      <DenseTag
                        variant={
                          row.statusLabel === 'Done' || row.statusLabel === 'Closed'
                            ? 'success'
                            : row.statusLabel === 'Failed'
                              ? 'danger'
                              : 'neutral'
                        }
                        className="shrink-0 text-[8px]"
                      >
                        {row.statusLabel}
                      </DenseTag>
                      <DenseTag variant={tag.variant} className="shrink-0 text-[8px]">
                        {tag.label}
                      </DenseTag>
                      {row.kind === 'job' && onOpenAgentDesk != null && (
                        <button
                          type="button"
                          className="shrink-0 text-[var(--text-dense-caption)] text-primary hover:underline"
                          onClick={() => onOpenAgentDesk(row.jobId)}
                        >
                          Open
                        </button>
                      )}
                    </li>
                  )
                })}
              </ul>
            )}
            {jobsQ.isError && historyRows.length > 0 && (
              <p className="m-0 mt-1 text-[var(--text-dense-micro)] text-muted-foreground">
                Remediation list partially unavailable — showing closed queue items.
              </p>
            )}
            {jobsQ.isError && historyRows.length === 0 && (
              <p className="m-0 mt-1 text-[var(--text-dense-micro)] text-muted-foreground">
                Could not load remediation jobs — try Agent Desk.
              </p>
            )}
          </section>
        </div>
      )}
    </div>
  )
}
