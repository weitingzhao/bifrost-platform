import { useMemo, useEffect, useRef, useState } from 'react'
import { useQuery, useMutation, useQueryClient } from '@tanstack/react-query'
import { Button, StatusLamp } from '@bifrost/ui'
import type { RemediationEvent, RemediationJob } from '@/api/types'
import { fetchRemediationJob, respondRemediationJob, cancelRemediationJob } from '@/api/platform'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { RemediationHistoryBar } from '@/components/cluster/RemediationHistoryBar'
import { RemediationInitBrief } from '@/components/cluster/RemediationInitBrief'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { useRemediationStream } from '@/hooks/useRemediationStream'
import { isRemediationStreamOrphanError } from '@/lib/remediation/remediationJobDisplay'

interface RemediationPanelProps {
  open: boolean
  jobId: string | null
  initialJob?: RemediationJob | null
  variant?: 'cluster' | 'desk'
  /** Session fallback when job.init_brief is missing (e.g. Agent Desk composer text). */
  initBriefFallback?: string
  onClose: () => void
  onStop?: (jobId: string) => void
  onDismiss?: () => void
  onComplete?: (job: RemediationJob) => void
  onOpenServerConsole?: () => void
  /** Desk variant: record briefing session close (S9) instead of dismiss-only. */
  onCloseSession?: () => void
  stopping?: boolean
}

const DECISION_HEIGHT_STORAGE = 'bifrost.remediation.decisionZoneHeight'
const DECISION_HEIGHT_DEFAULT = 480
const DECISION_HEIGHT_MIN = 280
const DECISION_HEIGHT_MAX_RATIO = 0.85

function readDecisionZoneHeight(): number {
  if (typeof window === 'undefined') return DECISION_HEIGHT_DEFAULT
  const raw = localStorage.getItem(DECISION_HEIGHT_STORAGE)
  const parsed = raw != null ? Number(raw) : NaN
  const max = Math.round(window.innerHeight * DECISION_HEIGHT_MAX_RATIO)
  if (Number.isFinite(parsed) && parsed >= DECISION_HEIGHT_MIN) {
    return Math.min(parsed, max)
  }
  return Math.min(Math.round(window.innerHeight * 0.55), 560)
}

function clampDecisionHeight(height: number): number {
  if (typeof window === 'undefined') return height
  const max = Math.round(window.innerHeight * DECISION_HEIGHT_MAX_RATIO)
  return Math.min(Math.max(height, DECISION_HEIGHT_MIN), max)
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

/**
 * Build a complete, AI-ready dump of the task: metadata, init brief, the live
 * stream/connection error (which Copy report omits), summary, job error and the
 * FULL unfiltered event log. Prefixed with an instruction so the operator can
 * paste it straight into an AI agent to diagnose and fix the cluster issue.
 */
function buildAskAiPrompt(
  job: RemediationJob | null,
  events: RemediationEvent[],
  streamError: string | null,
): string {
  const lines: string[] = []

  lines.push(
    'You are helping debug a Bifrost Ops Platform agent task (auto-remediation) that ran against a K3s cluster.',
    'Analyze the full log and errors below, identify the root cause, and give me concrete step-by-step commands or config changes to fix it.',
  )

  lines.push('')
  lines.push('=== Task metadata ===')
  if (job != null) {
    lines.push(`Job: ${job.id}`)
    if (job.scope != null && job.scope !== '') lines.push(`Scope: ${job.scope}`)
    lines.push(`Status: ${job.status} · Phase: ${job.phase ?? '—'}`)
    if (job.actor != null && job.actor !== '') lines.push(`Actor: ${job.actor}`)
    if (job.created_at != null) lines.push(`Created: ${job.created_at}`)
    if (job.updated_at != null) lines.push(`Updated: ${job.updated_at}`)
  } else {
    lines.push('(no job snapshot available)')
  }

  if (job?.init_brief != null && job.init_brief.trim() !== '') {
    lines.push('')
    lines.push('=== Init brief ===')
    lines.push(job.init_brief.trim())
  }

  if (streamError != null && streamError.trim() !== '') {
    lines.push('')
    lines.push('=== Connection / stream error ===')
    lines.push(streamError.trim())
  }

  if (job?.summary != null && job.summary.trim() !== '') {
    lines.push('')
    lines.push('=== Summary ===')
    lines.push(job.summary.trim())
  }

  if (job?.error != null && job.error.trim() !== '') {
    lines.push('')
    lines.push('=== Job error ===')
    lines.push(job.error.trim())
  }

  lines.push('')
  lines.push(`=== Full event log (${events.length} event${events.length === 1 ? '' : 's'}) ===`)
  if (events.length === 0) {
    lines.push('(no events captured)')
  } else {
    for (const e of events) {
      const toolName = typeof e.meta?.name === 'string' ? e.meta.name : null
      const head = `[${formatTime(e.at)}] [${e.type}]${toolName != null ? ` (tool: ${toolName})` : ''}`
      const text = e.text.trim()
      lines.push(text !== '' ? `${head} ${text}` : head)
    }
  }

  lines.push('')
  lines.push('=== What I need ===')
  lines.push(
    'Pinpoint the root cause from the evidence above and provide an actionable fix (exact kubectl / config / file edits). Flag anything that needs more info.',
  )

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


export function RemediationPanel({
  open,
  jobId,
  initialJob,
  variant = 'cluster',
  initBriefFallback,
  onClose,
  onStop,
  onDismiss,
  onComplete,
  onOpenServerConsole,
  onCloseSession,
  stopping = false,
}: RemediationPanelProps) {
  const qc = useQueryClient()
  const [viewJobId, setViewJobId] = useState<string | null>(jobId)
  const [streamOrphan, setStreamOrphan] = useState(false)
  const [dismissError, setDismissError] = useState<string | null>(null)
  const activityLogRef = useRef<HTMLDivElement>(null)
  const completedJobRef = useRef<string | null>(null)
  const [decisionZoneHeight, setDecisionZoneHeight] = useState(readDecisionZoneHeight)
  const decisionResizeRef = useRef<{ startY: number; startHeight: number } | null>(null)

  useEffect(() => {
    if (jobId != null) setViewJobId(jobId)
    setStreamOrphan(false)
    setDismissError(null)
  }, [jobId])

  useEffect(() => {
    function onPointerMove(e: PointerEvent) {
      const drag = decisionResizeRef.current
      if (drag == null) return
      const next = clampDecisionHeight(drag.startHeight + (e.clientY - drag.startY))
      setDecisionZoneHeight(next)
    }
    function onPointerUp() {
      if (decisionResizeRef.current == null) return
      decisionResizeRef.current = null
      setDecisionZoneHeight(h => {
        localStorage.setItem(DECISION_HEIGHT_STORAGE, String(h))
        return h
      })
    }
    window.addEventListener('pointermove', onPointerMove)
    window.addEventListener('pointerup', onPointerUp)
    return () => {
      window.removeEventListener('pointermove', onPointerMove)
      window.removeEventListener('pointerup', onPointerUp)
    }
  }, [])

  const isLiveView = viewJobId != null && viewJobId === jobId
  const streamEnabled = open && isLiveView && jobId != null && !streamOrphan

  const { job: streamJob, events: liveEvents, connected, error, stop } = useRemediationStream(
    streamEnabled ? jobId : null,
  )

  const snapshotQuery = useQuery({
    queryKey: ['remediation', 'job', viewJobId],
    queryFn: () => fetchRemediationJob(viewJobId!),
    enabled: open && viewJobId != null && (!streamEnabled || streamOrphan),
  })

  const baseJob: RemediationJob | null =
    (isLiveView ? streamJob ?? initialJob ?? snapshotQuery.data : snapshotQuery.data) ?? null

  const job: RemediationJob | null = useMemo(() => {
    if (baseJob == null) return null
    const orphaned =
      streamOrphan ||
      baseJob.error === 'orphaned' ||
      (baseJob.status === 'running' && isRemediationStreamOrphanError(error))
    if (!orphaned) return baseJob
    return {
      ...baseJob,
      status: 'cancelled',
      phase: 'cancelled',
      error: 'orphaned',
      summary:
        baseJob.summary ||
        'Job lost contact with the remediation runner (stale "running" state).',
    }
  }, [baseJob, streamOrphan, error])

  const events: RemediationEvent[] = isLiveView && !streamOrphan ? liveEvents : (snapshotQuery.data?.events ?? liveEvents)
  const isRunning = job?.status === 'running' && isLiveView && !streamOrphan
  const isHistorical = viewJobId != null && !isLiveView
  const isTerminalJob =
    job?.status === 'done' || job?.status === 'failed' || job?.status === 'cancelled'
  const showCloseSession =
    variant === 'desk' && onCloseSession != null && isTerminalJob && !isRunning

  useEffect(() => {
    if (!isLiveView || error == null) return
    if (!isRemediationStreamOrphanError(error)) return
    setStreamOrphan(true)
  }, [error, isLiveView])

  const dismissMutation = useMutation({
    mutationFn: cancelRemediationJob,
    onSuccess: () => {
      setStreamOrphan(false)
      setDismissError(null)
      void qc.invalidateQueries({ queryKey: ['remediation', 'jobs'] })
      void qc.invalidateQueries({ queryKey: ['remediation', 'job', viewJobId] })
      onDismiss?.()
    },
    onError: err => {
      setDismissError(err instanceof Error ? err.message : 'Failed to dismiss job')
    },
  })

  const showOrphanBanner =
    streamOrphan ||
    job?.error === 'orphaned' ||
    (initialJob?.status === 'running' && isRemediationStreamOrphanError(error))

  const respondMutation = useMutation({
    mutationFn: ({
      id,
      optionId,
      note,
      commitMessage,
    }: {
      id: string
      optionId: string
      note?: string
      commitMessage?: string
    }) => respondRemediationJob(id, optionId, note, commitMessage),
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

  const askAiPrompt = useMemo(
    () => buildAskAiPrompt(job, events, isLiveView ? error : null),
    [job, events, error, isLiveView],
  )
  const [askAiState, setAskAiState] = useState<'idle' | 'copied' | 'error'>('idle')

  async function handleAskAi() {
    if (askAiPrompt === '') return
    try {
      await navigator.clipboard.writeText(askAiPrompt)
      setAskAiState('copied')
      window.setTimeout(() => setAskAiState('idle'), 2000)
    } catch {
      setAskAiState('error')
      window.setTimeout(() => setAskAiState('idle'), 3000)
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
            variant="default"
            size="sm"
            disabled={askAiPrompt === ''}
            title="Copy full logs + errors as an AI-ready prompt"
            onClick={() => void handleAskAi()}
          >
            {askAiState === 'copied'
              ? 'Copied for AI!'
              : askAiState === 'error'
                ? 'Copy failed'
                : 'Ask AI to resolve'}
          </Button>
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
        scope={job?.scope ?? initialJob?.scope ?? null}
        onSelectJob={j => setViewJobId(j.id)}
        onBackToLive={() => jobId != null && setViewJobId(jobId)}
      />

      <RemediationInitBrief job={job} fallbackBrief={initBriefFallback} />

      {showOrphanBanner ? (
        <div className="remediation-orphan-banner">
          <p className="remediation-orphan-banner__title">This job is not running on the remediation runner</p>
          <p className="remediation-orphan-banner__body">
            The timeline showed a stale &quot;running&quot; state — usually after a runner restart. Dismiss it to
            clear the record, or read any archived report below.
          </p>
          <div className="flex flex-wrap items-center gap-2">
            <Button
              variant="outline"
              size="sm"
              disabled={dismissMutation.isPending || viewJobId == null}
              onClick={() => viewJobId != null && dismissMutation.mutate(viewJobId)}
            >
              {dismissMutation.isPending ? 'Dismissing…' : 'Dismiss stale job'}
            </Button>
            {dismissError != null ? (
              <span className="text-[var(--text-dense-meta)] text-destructive">{dismissError}</span>
            ) : null}
          </div>
        </div>
      ) : null}

      <div className="remediation-progress-bar">
        <AgentPhaseIndicator currentPhase={job?.phase} failed={failed} />
      </div>

      <section
        className={
          pendingApproval != null
            ? 'remediation-decision-zone remediation-decision-zone--active'
            : 'remediation-decision-zone remediation-decision-zone--idle'
        }
        style={
          pendingApproval != null
            ? { height: decisionZoneHeight, maxHeight: '85vh' }
            : undefined
        }
        aria-label={
          pendingApproval != null
            ? 'Operator decision'
            : job?.phase === 'awaiting_approval'
              ? 'Waiting for decision options'
              : 'Agent status'
        }
      >
        {pendingApproval != null && viewJobId != null ? (
          <>
            <p className="remediation-decision-zone__title">
              {pendingApproval.meta?.kind === 'manual_steps'
                ? 'Your action — manual steps required'
                : 'Your decision — action required'}
            </p>
            <RemediationApprovalBlock
              event={pendingApproval}
              submitting={respondMutation.isPending}
              onOpenServerConsole={onOpenServerConsole}
              onRespond={(optionId, note, commitMessage) =>
                respondMutation.mutate({ id: viewJobId, optionId, note, commitMessage })
              }
            />
          </>
        ) : job?.phase === 'awaiting_approval' && isLiveView ? (
          <>
            <p className="remediation-decision-zone__title">Your decision — waiting</p>
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Agent is preparing options. Choices will appear here — you do not need to act in
              Activity until then.
            </p>
          </>
        ) : isRunning ? (
          <>
            <p className="remediation-decision-zone__title">
              {job?.phase === 'diagnosing'
                ? 'Diagnosing — no decision needed'
                : job?.phase === 'verifying'
                  ? 'Verifying — no decision needed'
                  : 'Remediating — no decision needed'}
            </p>
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              Agent is working (tools stream in Activity). There is nothing to approve right now.
              If this looks stuck, use <span className="font-medium text-foreground">Stop</span>{' '}
              below, then retry or Ask AI.
            </p>
            {jobId != null && onStop != null && (
              <div className="mt-2 flex flex-wrap items-center gap-2">
                <Button
                  variant="destructive"
                  size="sm"
                  disabled={stopping}
                  onClick={() => {
                    stop()
                    onStop(jobId)
                  }}
                >
                  {stopping ? 'Stopping…' : 'Stop agent'}
                </Button>
                <Button
                  variant="outline"
                  size="sm"
                  disabled={askAiPrompt === ''}
                  onClick={() => void handleAskAi()}
                >
                  {askAiState === 'copied' ? 'Copied for AI!' : 'Ask AI to resolve'}
                </Button>
              </div>
            )}
          </>
        ) : (
          <>
            <p className="remediation-decision-zone__title">Session</p>
            <p className="m-0 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
              {job?.status === 'done'
                ? 'Completed — no further decision required.'
                : job?.status === 'failed'
                  ? 'Failed — use Ask AI / Copy report, or start a new task.'
                  : job?.status === 'cancelled'
                    ? 'Stopped — start a new task when ready.'
                    : 'No pending choice.'}
            </p>
          </>
        )}
      </section>

      {pendingApproval != null ? (
        <div
          className="remediation-decision-resize-handle"
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize decision panel"
          title="Drag to resize Agent context area"
          onPointerDown={e => {
            decisionResizeRef.current = { startY: e.clientY, startHeight: decisionZoneHeight }
            e.currentTarget.setPointerCapture(e.pointerId)
          }}
        />
      ) : null}

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
        {error != null && error !== 'unexpected EOF' && isLiveView && !showOrphanBanner && (
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
            variant="default"
            size="sm"
            disabled={askAiPrompt === ''}
            title="Copy full logs + errors as an AI-ready prompt"
            onClick={() => void handleAskAi()}
          >
            {askAiState === 'copied' ? 'Copied for AI!' : 'Ask AI to resolve'}
          </Button>
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
          {showOrphanBanner && viewJobId != null && (
            <Button
              variant="outline"
              size="sm"
              disabled={dismissMutation.isPending}
              onClick={() => dismissMutation.mutate(viewJobId)}
            >
              {dismissMutation.isPending ? 'Dismissing…' : 'Dismiss stale job'}
            </Button>
          )}
          {showCloseSession && (
            <Button variant="default" size="sm" onClick={onCloseSession}>
              Close session
            </Button>
          )}
          <Button variant="outline" size="sm" onClick={onClose}>
            {showCloseSession ? 'Dismiss' : 'Close'}
          </Button>
        </div>
      </footer>
    </aside>
  )
}
