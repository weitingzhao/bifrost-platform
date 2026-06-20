import { useEffect, useRef } from 'react'
import { Button, DenseTag, StatusLamp } from '@bifrost/ui'
import type { RemediationEvent, RemediationJob, RemediationPhase } from '@/api/types'
import { useRemediationStream } from '@/hooks/useRemediationStream'

interface RemediationPanelProps {
  open: boolean
  jobId: string | null
  initialJob?: RemediationJob | null
  onClose: () => void
  onStop?: (jobId: string) => void
  stopping?: boolean
}

function phaseLabel(phase: RemediationPhase | undefined): string {
  switch (phase) {
    case 'starting':
      return 'Starting'
    case 'diagnosing':
      return 'Diagnosing'
    case 'remediating':
      return 'Remediating'
    case 'verifying':
      return 'Verifying'
    case 'done':
      return 'Done'
    case 'failed':
      return 'Failed'
    case 'cancelled':
      return 'Cancelled'
    default:
      return 'Idle'
  }
}

function phaseVariant(phase: RemediationPhase | undefined): 'success' | 'warning' | 'danger' | 'neutral' {
  if (phase === 'done') return 'success'
  if (phase === 'failed') return 'danger'
  if (phase === 'cancelled') return 'warning'
  if (phase === 'starting' || phase === 'diagnosing' || phase === 'remediating' || phase === 'verifying') {
    return 'warning'
  }
  return 'neutral'
}

function reachabilityFromJob(job: RemediationJob | null): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  return 'degraded'
}

function EventLine({ event }: { event: RemediationEvent }) {
  if (event.type === 'thinking') {
    return (
      <p className="m-0 whitespace-pre-wrap text-[var(--text-dense-meta)] italic text-[var(--muted-foreground)]">
        {event.text}
      </p>
    )
  }
  if (event.type === 'tool_call') {
    return (
      <pre className="m-0 overflow-x-auto rounded border border-[var(--border)] bg-[var(--background)] p-2 font-mono-tabular text-[var(--text-dense-meta)] text-sky-400">
        {event.text}
      </pre>
    )
  }
  if (event.type === 'tool_result') {
    return (
      <pre className="m-0 max-h-48 overflow-auto rounded border border-[var(--border)] bg-[var(--background)] p-2 font-mono-tabular text-[var(--text-dense-meta)]">
        {event.text}
      </pre>
    )
  }
  if (event.type === 'done') {
    return <p className="m-0 whitespace-pre-wrap text-[var(--text-dense-meta)] text-success">{event.text}</p>
  }
  if (event.type === 'error') {
    return <p className="m-0 whitespace-pre-wrap text-[var(--text-dense-meta)] text-danger">{event.text}</p>
  }
  return <p className="m-0 whitespace-pre-wrap text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">{event.text}</p>
}

export function RemediationPanel({
  open,
  jobId,
  initialJob,
  onClose,
  onStop,
  stopping = false,
}: RemediationPanelProps) {
  const { job: streamJob, events, connected, error, stop } = useRemediationStream(open ? jobId : null)
  const logRef = useRef<HTMLDivElement>(null)
  const job = streamJob ?? initialJob ?? null
  const isRunning = job?.status === 'running'

  useEffect(() => {
    logRef.current?.scrollTo({ top: logRef.current.scrollHeight, behavior: 'smooth' })
  }, [events.length, job?.status])

  if (!open) return null

  return (
    <aside
      className="bay-detail-drawer panel-elevated cluster-drawer remediation-drawer"
      role="dialog"
      aria-label="Auto-remediation"
    >
      <header className="bay-detail-drawer-header">
        <div className="flex min-w-0 items-center gap-2">
          <StatusLamp value={reachabilityFromJob(job)} kind="reach" />
          <div className="min-w-0">
            <h3 className="m-0 text-sm font-semibold">Auto-Remediate</h3>
            <p className="m-0 mt-1 flex flex-wrap items-center gap-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              <DenseTag variant={phaseVariant(job?.phase)}>{phaseLabel(job?.phase)}</DenseTag>
              {connected ? (
                <span className="text-success">Streaming</span>
              ) : isRunning ? (
                <span>Connecting…</span>
              ) : null}
              {job?.id != null && (
                <span className="truncate font-mono-tabular" title={job.id}>
                  {job.id.slice(0, 8)}
                </span>
              )}
            </p>
          </div>
        </div>
        <Button variant="outline" size="sm" onClick={onClose}>
          Close
        </Button>
      </header>

      <div ref={logRef} className="bay-detail-drawer-body flex flex-col gap-2">
        {error != null && (
          <p className="m-0 rounded border border-[var(--border)] bg-[var(--background)] p-2 text-[var(--text-dense-meta)] text-danger">
            {error}
          </p>
        )}
        {events.length === 0 && isRunning && (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Waiting for agent output…
          </p>
        )}
        {events.map(event => (
          <EventLine key={event.id} event={event} />
        ))}
        {job?.summary != null && job.summary !== '' && job.status === 'done' && (
          <div className="mt-2 rounded border border-[var(--border)] bg-[var(--background)] p-2">
            <p className="m-0 mb-1 text-[var(--text-dense-label)] font-medium text-success">Summary</p>
            <p className="m-0 whitespace-pre-wrap text-[var(--text-dense-meta)]">{job.summary}</p>
          </div>
        )}
      </div>

      <footer className="bay-detail-drawer-footer">
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {job?.updated_at != null ? `Updated ${new Date(job.updated_at).toLocaleTimeString()}` : '—'}
        </span>
        <div className="flex items-center gap-2">
          {isRunning && jobId != null && onStop != null && (
            <Button
              variant="destructive"
              size="sm"
              disabled={stopping}
              onClick={() => {
                stop()
                onStop(jobId)
              }}
            >
              {stopping ? 'Stopping…' : 'Stop'}
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
      </footer>
    </aside>
  )
}
