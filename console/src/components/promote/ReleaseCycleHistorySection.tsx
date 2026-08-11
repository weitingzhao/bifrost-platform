import {
  Button,
  DenseDataTable,
  DenseTableBody,
  DenseTableCell,
  DenseTableDetailRow,
  DenseTableHead,
  DenseTableHeadRow,
  DenseTableHeader,
  DenseTableRow,
  DenseTag,
  StatusLamp,
} from '@bifrost/ui'
import { useQuery } from '@tanstack/react-query'
import { Fragment, useState } from 'react'
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
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())}`
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
    <div className="flex flex-col gap-2">
      {(steps ?? []).map(step => (
        <div
          key={step.kind}
          className="rounded-md border border-border/60 bg-background/40 px-3 py-2"
        >
          <div className="flex flex-wrap items-center gap-2">
            <span className="text-dense-label font-medium text-foreground">
              {cycleStepLabel(step.kind)}
            </span>
            {stepResultTag(step.result)}
            {step.run_name && (
              <span className="font-mono text-dense-caption text-muted-foreground">
                {step.run_name}
              </span>
            )}
            <span className="font-mono-tabular text-dense-caption text-muted-foreground">
              {formatTime(step.started_at)}
              {step.completed_at ? ` → ${formatTime(step.completed_at)}` : ''}
            </span>
          </div>
          {step.detail && (
            <p className="m-0 mt-1 text-dense-meta text-muted-foreground">{step.detail}</p>
          )}
          {(step.gate_checks?.length ?? 0) > 0 && (
            <ul className="m-0 mt-1.5 list-none space-y-0.5 p-0">
              {step.gate_checks!.map((c: ReleaseGateCheckView) => (
                <li
                  key={c.id}
                  className="flex flex-wrap items-center gap-1.5 text-dense-caption text-muted-foreground"
                >
                  <StatusLamp value={c.reachability} kind="reach" />
                  <span className="font-medium text-foreground">{c.label}</span>
                  <span>{c.detail}</span>
                </li>
              ))}
            </ul>
          )}
        </div>
      ))}
    </div>
  )
}

interface ReleaseCycleHistorySectionProps {
  lane: ReleaseCycleLane
  description?: string
}

/** Bare table body for release cycles — wrap with LaneDetailCollapse at the page level. */
export function ReleaseCycleHistorySection({
  lane,
  description = 'Full STG → PROD release cycles. Expand a row for stage detail; Copy for AI exports structured JSON for process analysis.',
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
    <div className="flex flex-col">
      {description ? (
        <p className="m-0 border-b border-border/40 px-3 py-2 text-dense-meta text-muted-foreground">
          {description}
        </p>
      ) : null}
      {isLoading && (
        <p className="px-3 py-2 text-dense-meta text-muted-foreground">Loading cycles…</p>
      )}
      {error instanceof Error && (
        <p className="px-3 py-2 text-dense-meta text-destructive">{error.message}</p>
      )}
      {!isLoading && entries.length === 0 && (
        <p className="px-3 py-2 text-dense-meta text-muted-foreground">
          No release cycles recorded yet. Cycles are created when AI Release / AI Deploy starts a
          deliver PipelineRun.
        </p>
      )}
      {entries.length > 0 && (
        <DenseDataTable>
          <DenseTableHeader>
            <DenseTableHeadRow>
              <DenseTableHead className="w-[16%]">Revision</DenseTableHead>
              <DenseTableHead className="w-[12%]">Outcome</DenseTableHead>
              <DenseTableHead className="w-[10%]">Duration</DenseTableHead>
              <DenseTableHead className="w-[18%]">Started</DenseTableHead>
              <DenseTableHead className="w-[14%]">Triggered by</DenseTableHead>
              <DenseTableHead className="w-[14%]">Agent</DenseTableHead>
              <DenseTableHead className="w-[16%]">Actions</DenseTableHead>
            </DenseTableHeadRow>
          </DenseTableHeader>
          <DenseTableBody>
            {entries.map((cycle: ReleaseCycleView) => {
              const open = openId === cycle.id
              const cs = copyState[cycle.id] ?? 'idle'
              return (
                <Fragment key={cycle.id}>
                  <DenseTableRow>
                    <DenseTableCell>
                      <button
                        type="button"
                        className="font-mono text-dense-meta font-semibold text-primary hover:underline"
                        aria-expanded={open}
                        onClick={() => setOpenId(open ? null : cycle.id)}
                      >
                        {cycle.revision || '—'}
                      </button>
                    </DenseTableCell>
                    <DenseTableCell>{outcomeTag(cycle.outcome)}</DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-dense-meta">
                      {formatCycleDuration(cycle)}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono-tabular text-dense-meta text-muted-foreground">
                      {formatTime(cycle.started_at)}
                    </DenseTableCell>
                    <DenseTableCell className="text-dense-meta">
                      {cycle.triggered_by || '—'}
                    </DenseTableCell>
                    <DenseTableCell className="font-mono text-dense-caption text-muted-foreground">
                      {cycle.agent_session_id
                        ? cycle.agent_session_id.slice(0, 12)
                        : '—'}
                    </DenseTableCell>
                    <DenseTableCell>
                      <div className="flex flex-wrap items-center gap-1.5">
                        <Button
                          size="sm"
                          variant="outline"
                          onClick={() => setOpenId(open ? null : cycle.id)}
                        >
                          {open ? 'Hide' : 'Detail'}
                        </Button>
                        <Button size="sm" onClick={() => void handleCopy(cycle)}>
                          {cs === 'copied'
                            ? 'Copied'
                            : cs === 'error'
                              ? 'Copy failed'
                              : 'Copy for AI'}
                        </Button>
                      </div>
                    </DenseTableCell>
                  </DenseTableRow>
                  {open && (
                    <DenseTableDetailRow>
                      <DenseTableCell colSpan={7}>
                        <div className="flex flex-col gap-2 py-1">
                          <div className="flex flex-wrap items-center gap-2 text-dense-caption text-muted-foreground">
                            <span className="font-mono">{cycle.id}</span>
                            <span>·</span>
                            <span>{cycle.lane}</span>
                            {cycle.completed_at && (
                              <>
                                <span>·</span>
                                <span>Completed {formatTime(cycle.completed_at)}</span>
                              </>
                            )}
                          </div>
                          <CycleStepTimeline steps={cycle.steps ?? []} />
                        </div>
                      </DenseTableCell>
                    </DenseTableDetailRow>
                  )}
                </Fragment>
              )
            })}
          </DenseTableBody>
        </DenseDataTable>
      )}
    </div>
  )
}
