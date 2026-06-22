import { useMemo, useEffect, useRef, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, StatusLamp } from '@bifrost/ui'
import type { RemediationEvent, RemediationJob, RemediationPhase } from '@/api/types'
import { fetchRemediationJob, respondRemediationJob } from '@/api/platform'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { RemediationHistoryBar } from '@/components/cluster/RemediationHistoryBar'
import { RemediationInitBrief } from '@/components/cluster/RemediationInitBrief'
import { useRemediationStream } from '@/hooks/useRemediationStream'

interface RemediationPanelProps {
  open: boolean
  jobId: string | null
  initialJob?: RemediationJob | null
  variant?: 'cluster' | 'desk'
  /** Session fallback when job.init_brief is missing (e.g. Agent Desk composer text). */
  initBriefFallback?: string
  onClose: () => void
  onStop?: (jobId: string) => void
  onComplete?: (job: RemediationJob) => void
  onOpenServerConsole?: () => void
  stopping?: boolean
}

const PHASE_STEPS: { key: RemediationPhase; label: string }[] = [
  { key: 'starting', label: 'Start' },
  { key: 'diagnosing', label: 'Diagnose' },
  { key: 'awaiting_approval', label: 'Decide' },
  { key: 'remediating', label: 'Remediate' },
  { key: 'verifying', label: 'Verify' },
  { key: 'done', label: 'Done' },
]

function phaseIndex(phase: RemediationPhase | undefined): number {
  if (phase == null) return -1
  if (phase === 'failed' || phase === 'cancelled') return PHASE_STEPS.length
  const idx = PHASE_STEPS.findIndex(s => s.key === phase)
  return idx >= 0 ? idx : -1
}

function reachabilityFromJob(job: RemediationJob | null): 'ok' | 'degraded' | 'fail' | 'unknown' {
  if (job == null) return 'unknown'
  if (job.status === 'done') return 'ok'
  if (job.status === 'failed') return 'fail'
  if (job.status === 'cancelled') return 'degraded'
  if (job.phase === 'awaiting_approval') return 'degraded'
  return 'degraded'
}

function formatTime(at: string): string {
  try {
    return new Date(at).toLocaleTimeString(undefined, {
      hour: '2-digit',
      minute: '2-digit',
      second: '2-digit',
    })
  } catch {
    return ''
  }
}

function durationLabel(start: string, end: string): string {
  try {
    const ms = new Date(end).getTime() - new Date(start).getTime()
    if (ms < 1000) return '<1s'
    if (ms < 60_000) return `${Math.round(ms / 1000)}s`
    return `${Math.floor(ms / 60_000)}m ${Math.round((ms % 60_000) / 1000)}s`
  } catch {
    return ''
  }
}

function buildRemediationCopyText(job: RemediationJob | null, events: RemediationEvent[]): string {
  const lines: string[] = []
  if (job != null) {
    lines.push(`Job ${job.id}`)
    if (job.scope != null && job.scope !== '') lines.push(`Scope: ${job.scope}`)
    if (job.init_brief != null && job.init_brief.trim() !== '') {
      lines.push('')
      lines.push('--- Init brief ---')
      lines.push(job.init_brief.trim())
    }
    lines.push(`Status: ${job.status} · phase: ${job.phase ?? '—'}`)
    if (job.created_at != null) lines.push(`Created: ${job.created_at}`)
  }
  if (job?.summary != null && job.summary.trim() !== '') {
    lines.push('')
    lines.push('--- Summary ---')
    lines.push(job.summary.trim())
  }
  if (job?.error != null && job.error.trim() !== '') {
    lines.push('')
    lines.push('--- Error ---')
    lines.push(job.error.trim())
  }
  const bodyEvents = events.filter(
    e =>
      e.type === 'done' ||
      e.type === 'error' ||
      (e.type === 'tool_result' && e.text.trim() !== '') ||
      (e.type === 'thinking' && e.text.trim() !== ''),
  )
  if (bodyEvents.length > 0) {
    lines.push('')
    lines.push('--- Event log ---')
    for (const e of bodyEvents) {
      lines.push(`[${e.type}] ${e.text.trim()}`)
    }
  }
  return lines.join('\n').trim()
}

interface GroupedBlock {
  type: 'thinking' | 'tool' | 'status' | 'error' | 'done' | 'approval'
  events: RemediationEvent[]
  toolName?: string
}

function groupEvents(events: RemediationEvent[]): GroupedBlock[] {
  const blocks: GroupedBlock[] = []
  let thinkingBuf: RemediationEvent[] = []

  function flushThinking() {
    if (thinkingBuf.length > 0) {
      blocks.push({ type: 'thinking', events: [...thinkingBuf] })
      thinkingBuf = []
    }
  }

  for (const ev of events) {
    if (ev.type === 'thinking') {
      thinkingBuf.push(ev)
    } else {
      flushThinking()
      if (ev.type === 'tool_call') {
        const toolName = typeof ev.meta?.name === 'string' ? ev.meta.name : 'tool'
        blocks.push({ type: 'tool', events: [ev], toolName })
      } else if (ev.type === 'tool_result') {
        const toolName = typeof ev.meta?.name === 'string' ? ev.meta.name : undefined
        const prev = blocks[blocks.length - 1]
        if (prev?.type === 'tool' && (toolName == null || prev.toolName === toolName)) {
          prev.events.push(ev)
        } else {
          blocks.push({ type: 'tool', events: [ev], toolName })
        }
      } else if (ev.type === 'approval_request') {
        blocks.push({ type: 'approval', events: [ev] })
      } else if (ev.type === 'status') {
        blocks.push({ type: 'status', events: [ev] })
      } else if (ev.type === 'error') {
        blocks.push({ type: 'error', events: [ev] })
      } else if (ev.type === 'done') {
        blocks.push({ type: 'done', events: [ev] })
      }
    }
  }
  flushThinking()
  return blocks
}

function ThinkingBlock({ events }: { events: RemediationEvent[] }) {
  const [expanded, setExpanded] = useState(false)
  const text = events.map(e => e.text).join('')

  return (
    <div className="remediation-block remediation-block--thinking">
      <button
        type="button"
        className="remediation-block-header remediation-block-header--toggle"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="remediation-block-kicker">AI Reasoning</span>
        <span className="remediation-block-meta">
          {events.length} fragment{events.length > 1 ? 's' : ''}
        </span>
        <span className="remediation-block-chevron">{expanded ? '▾' : '▸'}</span>
      </button>
      {expanded && <p className="remediation-block-body remediation-block-body--thinking">{text}</p>}
    </div>
  )
}

function ToolBlock({ block }: { block: GroupedBlock }) {
  const [expanded, setExpanded] = useState(false)
  const call = block.events.find(e => e.type === 'tool_call')
  const result = block.events.find(e => e.type === 'tool_result')
  const resultLen = result?.text.length ?? 0

  return (
    <div className="remediation-block remediation-block--tool">
      <button
        type="button"
        className="remediation-block-header remediation-block-header--toggle"
        onClick={() => setExpanded(!expanded)}
      >
        <span className="remediation-block-kicker remediation-block-kicker--tool">
          {block.toolName ?? 'tool'}
        </span>
        {result != null && (
          <span className="remediation-block-meta remediation-block-meta--result">
            {resultLen > 0 ? `${resultLen} chars` : 'done'}
          </span>
        )}
        <span className="remediation-block-chevron">{expanded ? '▾' : '▸'}</span>
      </button>
      {expanded && (
        <>
          {call != null && call.text.trim() !== '' && (
            <pre className="remediation-block-code remediation-block-code--call dense-scroll-y">{call.text}</pre>
          )}
          {result != null && result.text.trim() !== '' && (
            <pre className="remediation-block-code remediation-block-code--result dense-scroll-y">{result.text}</pre>
          )}
        </>
      )}
    </div>
  )
}

function StatusBlock({ event }: { event: RemediationEvent }) {
  return (
    <div className="remediation-block remediation-block--status">
      <span className="remediation-block-status-dot" />
      <span className="remediation-block-status-text">{event.text}</span>
      <span className="remediation-block-status-time">{formatTime(event.at)}</span>
    </div>
  )
}

function ErrorBlock({ event }: { event: RemediationEvent }) {
  return (
    <div className="remediation-block remediation-block--error">
      <span className="remediation-block-kicker remediation-block-kicker--error">Error</span>
      <p className="remediation-block-body remediation-block-body--error">{event.text}</p>
    </div>
  )
}

function PhaseStepper({ currentPhase, failed }: { currentPhase: RemediationPhase | undefined; failed: boolean }) {
  const current = phaseIndex(currentPhase)

  return (
    <nav className="remediation-stepper" aria-label="Remediation progress">
      {PHASE_STEPS.map((step, i) => {
        let state: 'done' | 'active' | 'pending' | 'failed' = 'pending'
        if (failed && i === current) state = 'failed'
        else if (i < current || currentPhase === 'done') state = 'done'
        else if (i === current) state = 'active'
        return (
          <div key={step.key} className={`remediation-step remediation-step--${state}`}>
            <div className="remediation-step-dot" />
            <span className="remediation-step-label">{step.label}</span>
            {i < PHASE_STEPS.length - 1 && <div className="remediation-step-line" />}
          </div>
        )
      })}
    </nav>
  )
}

export function RemediationPanel({
  open,
  jobId,
  initialJob,
  variant = 'cluster',
  initBriefFallback,
  onClose,
  onStop,
  onComplete,
  onOpenServerConsole,
  stopping = false,
}: RemediationPanelProps) {
  const qc = useQueryClient()
  const [viewJobId, setViewJobId] = useState<string | null>(jobId)
  const activityLogRef = useRef<HTMLDivElement>(null)
  const completedJobRef = useRef<string | null>(null)

  useEffect(() => {
    if (jobId != null) setViewJobId(jobId)
  }, [jobId])

  const isLiveView = viewJobId != null && viewJobId === jobId
  const streamEnabled = open && isLiveView && jobId != null

  const { job: streamJob, events: liveEvents, connected, error, stop } = useRemediationStream(
    streamEnabled ? jobId : null,
  )

  const snapshotQuery = useQuery({
    queryKey: ['remediation', 'job', viewJobId],
    queryFn: () => fetchRemediationJob(viewJobId!),
    enabled: open && viewJobId != null && !streamEnabled,
  })

  const job: RemediationJob | null =
    (isLiveView ? streamJob ?? initialJob : snapshotQuery.data) ?? null
  const events: RemediationEvent[] = isLiveView ? liveEvents : (snapshotQuery.data?.events ?? [])
  const isRunning = job?.status === 'running' && isLiveView
  const isHistorical = viewJobId != null && !isLiveView

  const respondMutation = useMutation({
    mutationFn: ({ id, optionId, note }: { id: string; optionId: string; note?: string }) =>
      respondRemediationJob(id, optionId, note),
    onSuccess: () => {
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
    },
  })

  const blocks = useMemo(() => groupEvents(events), [events])
  const activityBlocks = useMemo(
    () => blocks.filter(block => block.type !== 'approval'),
    [blocks],
  )

  const pendingApproval = useMemo(() => {
    if (job?.phase !== 'awaiting_approval' || !isLiveView) return null
    for (let i = events.length - 1; i >= 0; i--) {
      const ev = events[i]
      if (ev.type === 'approval_request') return ev
      if (ev.type === 'status' && ev.text.startsWith('Operator selected:')) return null
    }
    return null
  }, [events, job?.phase, isLiveView])

  const stats = useMemo(() => {
    const toolCalls = events.filter(e => e.type === 'tool_call').length
    return { toolCalls }
  }, [events])

  const copyableReport = useMemo(() => buildRemediationCopyText(job, events), [job, events])
  const [copyState, setCopyState] = useState<'idle' | 'copied' | 'error'>('idle')

  async function handleCopyReport() {
    if (copyableReport === '') return
    try {
      await navigator.clipboard.writeText(copyableReport)
      setCopyState('copied')
      window.setTimeout(() => setCopyState('idle'), 2000)
    } catch {
      setCopyState('error')
      window.setTimeout(() => setCopyState('idle'), 3000)
    }
  }

  useEffect(() => {
    completedJobRef.current = null
  }, [jobId])

  useEffect(() => {
    if (job == null || onComplete == null || !isLiveView) return
    const terminal = job.status === 'done' || job.status === 'failed' || job.status === 'cancelled'
    if (!terminal || completedJobRef.current === job.id) return
    completedJobRef.current = job.id
    onComplete(job)
    void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
  }, [job, onComplete, isLiveView, qc])

  useEffect(() => {
    if (pendingApproval != null) return
    activityLogRef.current?.scrollTo({ top: activityLogRef.current.scrollHeight, behavior: 'smooth' })
  }, [activityBlocks.length, job?.status, pendingApproval])

  if (!open) return null

  const failed = job?.status === 'failed'
  const duration =
    job?.created_at != null && job?.updated_at != null && job.status !== 'running'
      ? durationLabel(job.created_at, job.updated_at)
      : null

  const panelTitle =
    variant === 'desk'
      ? isHistorical
        ? 'Task report'
        : 'Agent task'
      : isHistorical
        ? 'Run report'
        : 'Auto-Remediate'
  const panelAria = variant === 'desk' ? 'Agent task' : 'Auto-remediation'

  return (
    <aside
      className="bay-detail-drawer panel-elevated cluster-drawer remediation-drawer"
      role="dialog"
      aria-label={panelAria}
    >
      <header className="bay-detail-drawer-header remediation-header">
        <div className="flex min-w-0 items-center gap-2">
          <StatusLamp value={reachabilityFromJob(job)} kind="reach" />
          <div className="min-w-0">
            <h3 className="m-0 text-sm font-semibold">{panelTitle}</h3>
            <p className="m-0 mt-0.5 flex flex-wrap items-center gap-2 text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
              {viewJobId != null && (
                <span className="font-mono-tabular" title={viewJobId}>
                  {viewJobId.slice(0, 8)}
                </span>
              )}
              {isHistorical && <span>archive</span>}
              {connected && isLiveView && <span className="text-emerald-500">live</span>}
              {duration != null && <span>{duration}</span>}
              {stats.toolCalls > 0 && <span>{stats.toolCalls} tool call{stats.toolCalls > 1 ? 's' : ''}</span>}
            </p>
          </div>
        </div>
        <div className="flex shrink-0 items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={copyableReport === ''}
            onClick={() => void handleCopyReport()}
          >
            {copyState === 'copied'
              ? 'Copied!'
              : copyState === 'error'
                ? 'Copy failed'
                : 'Copy report'}
          </Button>
          <Button variant="outline" size="sm" onClick={onClose}>
            Close
          </Button>
        </div>
      </header>

      <RemediationHistoryBar
        open={open}
        activeJobId={viewJobId}
        liveJobId={jobId}
        onSelectJob={j => setViewJobId(j.id)}
        onBackToLive={() => jobId != null && setViewJobId(jobId)}
      />

      <RemediationInitBrief job={job} fallbackBrief={initBriefFallback} />

      <div className="remediation-progress-bar">
        <PhaseStepper currentPhase={job?.phase} failed={failed} />
      </div>

      <section
        className={
          pendingApproval != null
            ? 'remediation-decision-zone remediation-decision-zone--active'
            : 'remediation-decision-zone remediation-decision-zone--idle'
        }
        aria-label="Operator decision"
      >
        <p className="remediation-decision-zone__title">
          {pendingApproval != null ? (
          pendingApproval.meta?.kind === 'manual_steps'
            ? 'Your action — manual steps required'
            : 'Your decision — action required'
          ) : 'Your decision'}
        </p>
        {pendingApproval != null && viewJobId != null ? (
          <RemediationApprovalBlock
            event={pendingApproval}
            submitting={respondMutation.isPending}
            onOpenServerConsole={onOpenServerConsole}
            onRespond={(optionId, note) =>
              respondMutation.mutate({ id: viewJobId, optionId, note })
            }
          />
        ) : job?.phase === 'awaiting_approval' && isLiveView ? (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Waiting for agent to present options…
          </p>
        ) : (
          <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            No pending choice. Tool calls and reasoning stream in Activity below.
          </p>
        )}
      </section>

      <div ref={activityLogRef} className="bay-detail-drawer-body remediation-activity-log dense-scroll-y">
        <div className="remediation-activity-log__head">
          <p className="remediation-activity-log__title">Activity</p>
          <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
            {stats.toolCalls > 0
              ? `${stats.toolCalls} tool call${stats.toolCalls > 1 ? 's' : ''}`
              : isRunning
                ? 'Running…'
                : '—'}
          </span>
        </div>
        {isHistorical && snapshotQuery.isLoading && (
          <p className="m-0 px-1 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Loading report…
          </p>
        )}
        {error != null && error !== 'unexpected EOF' && isLiveView && (
          <div className="remediation-block remediation-block--error">
            <span className="remediation-block-kicker remediation-block-kicker--error">Connection</span>
            <p className="remediation-block-body remediation-block-body--error">{error}</p>
          </div>
        )}
        {events.length === 0 && isRunning && (
          <p className="m-0 px-1 py-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            Initializing agent…
          </p>
        )}
        {job?.summary != null && job.summary !== '' && (job.status === 'done' || job.status === 'failed') && (
          <div
            className={`remediation-summary${
              job.status === 'done' ? ' remediation-summary--done' : ' remediation-summary--failed'
            }`}
          >
            <p className="remediation-summary-title">
              {job.status === 'done' ? 'Completed' : 'Failed'}
            </p>
            <p className="remediation-summary-body">{job.summary}</p>
            {job.error != null && job.error !== '' && job.status === 'failed' && (
              <p className="remediation-summary-body remediation-summary-body--error">{job.error}</p>
            )}
          </div>
        )}
        {activityBlocks.map((block, i) => {
          if (block.type === 'thinking') return <ThinkingBlock key={i} events={block.events} />
          if (block.type === 'tool') return <ToolBlock key={i} block={block} />
          if (block.type === 'status') return <StatusBlock key={i} event={block.events[0]} />
          if (block.type === 'error') return <ErrorBlock key={i} event={block.events[0]} />
          return null
        })}
      </div>

      <footer className="bay-detail-drawer-footer">
        <span className="text-[var(--text-dense-caption)] text-[var(--muted-foreground)]">
          {job?.updated_at != null ? `Updated ${new Date(job.updated_at).toLocaleTimeString()}` : '—'}
        </span>
        <div className="flex items-center gap-2">
          <Button
            variant="outline"
            size="sm"
            disabled={copyableReport === ''}
            onClick={() => void handleCopyReport()}
          >
            {copyState === 'copied' ? 'Copied!' : 'Copy report'}
          </Button>
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
