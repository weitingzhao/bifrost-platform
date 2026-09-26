import { Button, DenseTag, StatusLamp, cn } from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { Check, ChevronDown, ChevronRight, ClipboardCopy } from 'lucide-react'
import { useState } from 'react'
import { fetchReleaseCycles } from '@/api/promote'
import type {
  ReleaseCycleLane,
  ReleaseCycleStepView,
  ReleaseCycleView,
  ReleaseGateCheckView,
} from '@/api/deliveryTypes'
import {
  buildCycleExportBundle,
  cycleStepLabel,
  formatCycleDuration,
} from '@/lib/promote/buildCycleExportBundle'

type CopyState = 'idle' | 'copied' | 'error'

function formatTime(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function formatTimeShort(iso?: string): string {
  if (!iso) return '—'
  const d = new Date(iso)
  if (Number.isNaN(d.getTime())) return iso
  const pad = (n: number) => String(n).padStart(2, '0')
  return `${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function outcomeTag(outcome: string) {
  switch (outcome) {
    case 'released':
      return <DenseTag variant="success">released</DenseTag>
    case 'failed':
      return <DenseTag variant="danger">failed</DenseTag>
    case 'superseded':
      return <DenseTag variant="neutral">superseded</DenseTag>
    case 'in_progress':
      return <DenseTag variant="warning">in progress</DenseTag>
    default:
      return <DenseTag variant="neutral">{outcome || '—'}</DenseTag>
  }
}

function stepResultTag(result?: string) {
  if (result === 'pass' || result === 'success') {
    return <DenseTag variant="success">{result}</DenseTag>
  }
  if (result === 'fail' || result === 'failed') {
    return <DenseTag variant="danger">{result}</DenseTag>
  }
  if (result === 'running') {
    return <DenseTag variant="warning">running</DenseTag>
  }
  return <DenseTag variant="neutral">{result || 'pending'}</DenseTag>
}

function CycleStepTimeline({ steps }: { steps: ReleaseCycleStepView[] }) {
  return (
    <div className="flex flex-col gap-1.5">
      {(steps ?? []).map(step => (
        <div
          key={step.kind}
          className="rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)] px-2.5 py-1.5"
        >
          <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <span className="text-dense-meta font-medium text-foreground">
              {cycleStepLabel(step.kind)}
            </span>
            {stepResultTag(step.result)}
          </div>
          <div className="mt-0.5 flex flex-wrap items-center gap-x-1.5 text-dense-caption text-muted-foreground">
            {step.run_name ? (
              <span className="font-mono truncate max-w-full">{step.run_name}</span>
            ) : null}
            <span className="font-mono-tabular">{formatTimeShort(step.started_at)}</span>
            {step.completed_at ? (
              <span className="font-mono-tabular">→ {formatTimeShort(step.completed_at)}</span>
            ) : null}
          </div>
          {step.detail ? (
            <p className="m-0 mt-1 text-dense-caption text-muted-foreground">{step.detail}</p>
          ) : null}
          {(step.gate_checks?.length ?? 0) > 0 ? (
            <ul className="m-0 mt-1 list-none space-y-0.5 p-0">
              {step.gate_checks!.map((c: ReleaseGateCheckView) => (
                <li
                  key={c.id}
                  className="flex flex-wrap items-center gap-1 text-dense-caption text-muted-foreground"
                >
                  <StatusLamp value={c.reachability} kind="reach" />
                  <span className="font-medium text-foreground">{c.label}</span>
                  <span className="min-w-0 break-words">{c.detail}</span>
                </li>
              ))}
            </ul>
          ) : null}
        </div>
      ))}
    </div>
  )
}

function CycleCard({
  cycle,
  open,
  copyState,
  onToggle,
  onCopy,
}: {
  cycle: ReleaseCycleView
  open: boolean
  copyState: CopyState
  onToggle: () => void
  onCopy: () => void
}) {
  const ExpandIcon = open ? ChevronDown : ChevronRight
  return (
    <div
      className={cn(
        'rounded-[var(--card-radius)] border border-[var(--card-border)] bg-[var(--card-fill)]',
        open && 'bg-[var(--card-fill-hover)]',
      )}
    >
      <div className="flex items-start gap-1.5 px-2.5 py-2">
        <button
          type="button"
          className="mt-0.5 inline-flex h-5 w-5 shrink-0 items-center justify-center rounded text-muted-foreground hover:bg-secondary hover:text-foreground"
          aria-expanded={open}
          aria-label={open ? 'Hide cycle detail' : 'Show cycle detail'}
          title={open ? 'Hide detail' : 'Show detail'}
          onClick={onToggle}
        >
          <ExpandIcon className="h-3.5 w-3.5" />
        </button>

        <div className="min-w-0 flex-1">
          <div className="flex flex-wrap items-center gap-x-1.5 gap-y-1">
            <button
              type="button"
              className="font-mono text-dense-label font-semibold text-primary hover:underline"
              aria-expanded={open}
              onClick={onToggle}
            >
              {cycle.revision || '—'}
            </button>
            {outcomeTag(cycle.outcome)}
          </div>
          <div className="mt-1 flex flex-wrap items-center gap-x-2 gap-y-0.5 text-dense-caption text-muted-foreground">
            <span className="font-mono-tabular">{formatCycleDuration(cycle)}</span>
            <span aria-hidden>·</span>
            <span className="font-mono-tabular" title={formatTime(cycle.started_at)}>
              {formatTimeShort(cycle.started_at)}
            </span>
            {cycle.triggered_by ? (
              <>
                <span aria-hidden>·</span>
                <span className="truncate">{cycle.triggered_by}</span>
              </>
            ) : null}
          </div>
        </div>

        <div className="flex shrink-0 items-center gap-1">
          <Button
            size="sm"
            variant="outline"
            className="h-7 px-2 text-dense-caption"
            onClick={onToggle}
          >
            {open ? 'Hide' : 'Detail'}
          </Button>
          <Button
            size="sm"
            variant="outline"
            className="h-7 gap-1 px-2 text-dense-caption"
            title="Copy structured cycle JSON for AI analysis"
            onClick={onCopy}
          >
            {copyState === 'copied' ? (
              <>
                <Check className="h-3 w-3 text-success" />
                Copied
              </>
            ) : copyState === 'error' ? (
              'Failed'
            ) : (
              <>
                <ClipboardCopy className="h-3 w-3" />
                Copy
              </>
            )}
          </Button>
        </div>
      </div>

      {open ? (
        <div className="border-t border-[var(--table-rule)] px-2.5 py-2">
          <div className="mb-1.5 flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-dense-caption text-muted-foreground">
            <span className="font-mono truncate">{cycle.id}</span>
            <span aria-hidden>·</span>
            <span>{cycle.lane}</span>
            {cycle.agent_session_id ? (
              <>
                <span aria-hidden>·</span>
                <span className="font-mono" title={cycle.agent_session_id}>
                  agent {cycle.agent_session_id.slice(0, 10)}
                </span>
              </>
            ) : null}
            {cycle.completed_at ? (
              <>
                <span aria-hidden>·</span>
                <span>done {formatTimeShort(cycle.completed_at)}</span>
              </>
            ) : null}
          </div>
          <CycleStepTimeline steps={cycle.steps ?? []} />
        </div>
      ) : null}
    </div>
  )
}

interface ReleaseCycleHistorySectionProps {
  lane: ReleaseCycleLane
  description?: string
}

/** Compact cycle list for the narrow LaneOperateSplit support rail. */
export function ReleaseCycleHistorySection({
  lane,
  description = 'Full STG → PROD release cycles. Expand a row for stage detail; Copy exports structured JSON for process analysis.',
}: ReleaseCycleHistorySectionProps) {
  const [openId, setOpenId] = useState<string | null>(null)
  const [copyState, setCopyState] = useState<Record<string, CopyState>>({})

  const { data, isLoading, error } = useQuery({
    queryKey: ['promote', 'release-cycles', lane],
    queryFn: () => fetchReleaseCycles(lane),
    refetchInterval: 30_000,
  })

  const entries = data?.entries ?? []

  const handleCopy = async (cycle: ReleaseCycleView) => {
    try {
      await navigator.clipboard.writeText(buildCycleExportBundle(cycle))
      setCopyState(prev => ({ ...prev, [cycle.id]: 'copied' }))
      window.setTimeout(() => {
        setCopyState(prev => ({ ...prev, [cycle.id]: 'idle' }))
      }, 2500)
    } catch {
      setCopyState(prev => ({ ...prev, [cycle.id]: 'error' }))
      window.setTimeout(() => {
        setCopyState(prev => ({ ...prev, [cycle.id]: 'idle' }))
      }, 2500)
    }
  }

  return (
    <div className="flex min-w-0 flex-col">
      <div className="flex flex-wrap items-center justify-between gap-2 border-b border-[var(--table-rule)] px-3 py-2">
        {description ? (
          <p className="m-0 min-w-0 flex-1 text-dense-meta text-muted-foreground">{description}</p>
        ) : (
          <span />
        )}
        {entries.length > 0 ? (
          <span className="shrink-0 font-mono text-dense-caption text-muted-foreground">
            {entries.length} cycle{entries.length === 1 ? '' : 's'}
          </span>
        ) : null}
      </div>

      {isLoading ? (
        <p className="px-3 py-2 text-dense-meta text-muted-foreground">Loading cycles…</p>
      ) : null}
      {error instanceof Error ? (
        <p className="px-3 py-2 text-dense-meta text-destructive">{error.message}</p>
      ) : null}
      {!isLoading && entries.length === 0 ? (
        <p className="px-3 py-2 text-dense-meta text-muted-foreground">
          No release cycles recorded yet. Cycles are created when AI Release / AI Deploy starts a
          deliver PipelineRun.
        </p>
      ) : null}

      {entries.length > 0 ? (
        <div className="dense-scroll-y max-h-[28rem] space-y-1.5 overflow-y-auto px-2.5 py-2">
          {entries.map(cycle => (
            <CycleCard
              key={cycle.id}
              cycle={cycle}
              open={openId === cycle.id}
              copyState={copyState[cycle.id] ?? 'idle'}
              onToggle={() => setOpenId(openId === cycle.id ? null : cycle.id)}
              onCopy={() => void handleCopy(cycle)}
            />
          ))}
        </div>
      ) : null}
    </div>
  )
}
