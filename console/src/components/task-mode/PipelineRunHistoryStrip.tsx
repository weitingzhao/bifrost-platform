import { useState } from 'react'
import { useQuery } from '@tanstack/react-query'
import { cn, DenseTag } from '@bifrost/ui'
import { ChevronDown, ChevronRight } from 'lucide-react'
import { fetchPipelineRunSteps } from '@/api/platform'
import type { DeliveryPipelineRunView } from '@/api/types'
import { OpsSection } from '@/components/layout/OpsSection'
import { StatusLamp } from '@/components/StatusLamp'
import {
  formatPipelineRunStatus,
  isPipelineRunFailed,
  isPipelineRunRunning,
  isPipelineRunSucceeded,
  runElapsedLabel,
} from '@/lib/delivery/pipelineRunAskPack'

const DEFAULT_LIMIT = 5
const COMPACT_LIMIT = 5

function relativeTime(iso: string | undefined): string {
  if (iso == null || iso === '') return '—'
  const t = new Date(iso).getTime()
  if (Number.isNaN(t)) return '—'
  const sec = Math.round((Date.now() - t) / 1000)
  if (sec < 60) return `${sec}s ago`
  const min = Math.round(sec / 60)
  if (min < 60) return `${min}m ago`
  const hr = Math.round(min / 60)
  if (hr < 48) return `${hr}h ago`
  return new Date(iso).toLocaleDateString()
}

function absoluteTime(iso: string | undefined): string | null {
  if (iso == null || iso === '') return null
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return null
  return d.toLocaleString(undefined, {
    month: 'short',
    day: 'numeric',
    hour: '2-digit',
    minute: '2-digit',
  })
}

function runLamp(run: DeliveryPipelineRunView): 'ok' | 'fail' | 'degraded' | 'unknown' {
  if (isPipelineRunSucceeded(run)) return 'ok'
  if (isPipelineRunFailed(run)) return 'fail'
  if (isPipelineRunRunning(run)) return 'degraded'
  return 'unknown'
}

const PHASE_TEXT_CLASS: Record<string, string> = {
  succeeded: 'text-muted-foreground/60',
  running: 'text-primary font-medium',
  failed: 'text-destructive font-medium',
  pending: 'text-muted-foreground/30',
}

function ExpandedRunPhases({ run }: { run: DeliveryPipelineRunView }) {
  const running = isPipelineRunRunning(run)
  const stepsQuery = useQuery({
    queryKey: ['delivery', 'steps', run.name, run.namespace],
    queryFn: () => fetchPipelineRunSteps(run.name, run.namespace),
    staleTime: 30_000,
    refetchInterval: running ? 3_000 : false,
  })
  const phases = stepsQuery.data?.phases ?? []
  const taskCount = stepsQuery.data?.tasks?.length ?? 0

  if (stepsQuery.isLoading && phases.length === 0) {
    return <p className="m-0 text-[var(--text-dense-micro)] text-muted-foreground">Loading phases…</p>
  }
  if (phases.length === 0) {
    return (
      <p className="m-0 text-[var(--text-dense-micro)] text-muted-foreground">
        No phase detail available for this run.
      </p>
    )
  }

  const succeeded = phases.filter(p => p.status === 'succeeded').length
  return (
    <div className="flex flex-col gap-1">
      <span className="text-[var(--text-dense-micro)] text-muted-foreground">
        {succeeded}/{phases.length} phases
        {taskCount > 0 ? ` · ${taskCount} tasks` : ''}
      </span>
      <div className="flex flex-wrap items-center gap-x-0.5 gap-y-0.5 font-mono text-[var(--text-dense-micro)]">
        {phases.map((phase, i) => (
          <span key={phase.id} className="inline-flex items-center">
            {i > 0 && <span className="mx-0.5 text-border">→</span>}
            <span className={PHASE_TEXT_CLASS[phase.status] ?? 'text-muted-foreground/30'}>
              {phase.label}
            </span>
          </span>
        ))}
      </div>
    </div>
  )
}

function CompactRunRow({
  run,
  newest,
  expanded,
  onToggle,
  onOpenRun,
}: {
  run: DeliveryPipelineRunView
  newest?: boolean
  expanded: boolean
  onToggle: () => void
  onOpenRun?: (run: DeliveryPipelineRunView) => void
}) {
  const running = isPipelineRunRunning(run)
  const ok = isPipelineRunSucceeded(run)
  const failed = isPipelineRunFailed(run)
  const status = formatPipelineRunStatus(run)
  const elapsed = runElapsedLabel(run)
  const startedAbs = absoluteTime(run.start_time)
  const completedAbs = absoluteTime(run.completion_time)
  const tone = failed
    ? 'text-destructive'
    : running
      ? 'text-primary'
      : ok
        ? 'text-muted-foreground'
        : 'text-muted-foreground'

  return (
    <li className="border-b border-border/40 py-1.5 last:border-b-0">
      <button
        type="button"
        className="flex w-full min-w-0 flex-col gap-0.5 rounded px-0.5 text-left hover:bg-primary/5"
        onClick={onToggle}
        aria-expanded={expanded}
      >
        <div className="flex min-w-0 items-center gap-1.5">
          {expanded ? (
            <ChevronDown className="size-3 shrink-0 text-muted-foreground" aria-hidden />
          ) : (
            <ChevronRight className="size-3 shrink-0 text-muted-foreground" aria-hidden />
          )}
          <StatusLamp value={runLamp(run)} kind="reach" />
          {newest && (
            <DenseTag variant="neutral" className="shrink-0 text-[8px]">
              New
            </DenseTag>
          )}
          <span className={cn('shrink-0 text-[var(--text-dense-caption)] font-medium', tone)}>
            {status}
          </span>
          {run.revision != null && run.revision !== '' && (
            <span className="min-w-0 truncate font-mono text-[var(--text-dense-micro)] text-muted-foreground">
              {run.revision}
            </span>
          )}
          {elapsed != null && (
            <span className="ml-auto shrink-0 text-[var(--text-dense-micro)] text-muted-foreground">
              {elapsed}
            </span>
          )}
        </div>
        <div className="flex items-center justify-between gap-1 pl-4 text-[var(--text-dense-micro)] text-muted-foreground">
          <span className="min-w-0 truncate font-mono" title={run.name}>
            {run.name}
          </span>
          <span className="shrink-0" title={startedAbs ?? undefined}>
            {relativeTime(run.start_time)}
          </span>
        </div>
      </button>

      {expanded && (
        <div className="mt-1.5 space-y-1.5 border-l border-border/50 pl-3 ml-1.5">
          <dl className="m-0 grid grid-cols-[auto_1fr] gap-x-2 gap-y-0.5 text-[var(--text-dense-micro)]">
            {startedAbs != null && (
              <>
                <dt className="text-muted-foreground">Started</dt>
                <dd className="m-0 font-mono tabular-nums">{startedAbs}</dd>
              </>
            )}
            {completedAbs != null && (
              <>
                <dt className="text-muted-foreground">Finished</dt>
                <dd className="m-0 font-mono tabular-nums">{completedAbs}</dd>
              </>
            )}
            {elapsed != null && (
              <>
                <dt className="text-muted-foreground">Duration</dt>
                <dd className="m-0 font-mono tabular-nums">{elapsed}</dd>
              </>
            )}
            <dt className="text-muted-foreground">Pipeline</dt>
            <dd className="m-0 truncate font-mono" title={run.pipeline}>
              {run.pipeline}
            </dd>
            <dt className="text-muted-foreground">Namespace</dt>
            <dd className="m-0 font-mono">{run.namespace}</dd>
            {run.reason != null && run.reason !== '' && (
              <>
                <dt className="text-muted-foreground">Reason</dt>
                <dd className={cn('m-0', failed && 'text-destructive')}>{run.reason}</dd>
              </>
            )}
          </dl>
          <ExpandedRunPhases run={run} />
          {onOpenRun != null && (
            <button
              type="button"
              className="text-[var(--text-dense-caption)] text-primary hover:underline"
              onClick={e => {
                e.stopPropagation()
                onOpenRun(run)
              }}
            >
              Open this run →
            </button>
          )}
        </div>
      )}
    </li>
  )
}

export type PipelineRunHistoryStripProps = {
  runs: DeliveryPipelineRunView[] | undefined
  isLoading?: boolean
  title?: string
  limit?: number
  linkLabel: string
  onOpenFullHistory: () => void
  /** Open Trade Release focused on a specific PipelineRun. */
  onOpenRun?: (run: DeliveryPipelineRunView) => void
  /** Dense list for Task CC side column */
  compact?: boolean
  /** Skip OpsSection wrapper when parent already provides a section */
  embedded?: boolean
}

/** Compact newest-first deliver history for Task CC (not the full PipelineRunsPanel). */
export function PipelineRunHistoryStrip({
  runs,
  isLoading = false,
  title = 'Recent launches',
  limit,
  linkLabel,
  onOpenFullHistory,
  onOpenRun,
  compact = false,
  embedded = false,
}: PipelineRunHistoryStripProps) {
  const max = limit ?? (compact ? COMPACT_LIMIT : DEFAULT_LIMIT)
  const slice = (runs ?? []).slice(0, max)
  const [expandedName, setExpandedName] = useState<string | null>(null)

  const body = (
    <>
      {isLoading && slice.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">Loading…</p>
      ) : slice.length === 0 ? (
        <p className="m-0 text-[var(--text-dense-caption)] text-muted-foreground">No runs yet</p>
      ) : (
        <ul className="m-0 flex list-none flex-col p-0">
          {slice.map((run, i) => (
            <CompactRunRow
              key={run.name}
              run={run}
              newest={i === 0}
              expanded={expandedName === run.name}
              onToggle={() =>
                setExpandedName(prev => (prev === run.name ? null : run.name))
              }
              onOpenRun={onOpenRun}
            />
          ))}
        </ul>
      )}
      <button
        type="button"
        className="mt-1.5 text-[var(--text-dense-caption)] text-primary hover:underline"
        onClick={onOpenFullHistory}
      >
        {linkLabel}
      </button>
    </>
  )

  if (embedded) return <div className="flex min-h-0 flex-1 flex-col">{body}</div>

  return (
    <OpsSection title={title} bodyPadding="compact" className={compact ? 'h-full' : undefined}>
      {body}
    </OpsSection>
  )
}
