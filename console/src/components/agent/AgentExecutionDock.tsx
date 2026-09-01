import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { Button, SegmentControl, StatusLamp, cn } from '@bifrost/ui'
import { Bot, ChevronDown, ChevronUp, LifeBuoy, Maximize2, Minimize2, X } from 'lucide-react'
import type { RemediationJob } from '@/api/remediationTypes'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { DenseMarkdown } from '@/components/agent/DenseMarkdown'
import { looksLikeMarkdown } from '@/components/agent/denseMarkdownUtils'
import { DockDevSessionsPanel } from '@/components/agent/DockDevSessionsPanel'
import { DockRecentAgentTasks } from '@/components/agent/DockRecentAgentTasks'
import { ServerConsolePanel } from '@/components/ServerConsolePanel'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useAgentJobLiveSession } from '@/hooks/useAgentJobLiveSession'
import { useAgentHostPulse } from '@/hooks/useAgentHostPulse'
import { useAgentApprovalMode } from '@/hooks/useAgentApprovalMode'
import {
  feedKindLabel,
} from '@/lib/agent/agentLiveFeed'
import { buildAutoApprovalResponse } from '@/lib/agent/agentApprovalMode'
import { DockProcessFeed } from '@/components/agent/DockProcessFeed'
import { updateActivityPhase, getActivityEvents } from '@/lib/activity/activityStore'
import type { AmbientAgentJob } from '@/lib/agent/ambientAgent'
import {
  persistOperatorTool,
  readStoredTool,
  type OperatorToolId,
} from '@/components/agent/operatorDockStorage'
import { isBenignRemediationStreamError } from '@/lib/remediation/remediationJobDisplay'

const DOCK_HEIGHT_KEY = 'bifrost.console.agentExecutionDockHeight'
const AGENT_H_SPLIT_KEY = 'bifrost.console.dockAgentHSplitPct.v1'
const AGENT_V_SPLIT_KEY = 'bifrost.console.dockAgentVSplitPct.v1'
const DEFAULT_WORKING_VH = 42
const DEFAULT_WORKING_REM = 28
const MIN_WORKING_PX = 160
const MAX_WORKING_VH = 70
const DEFAULT_AGENT_LEFT_PCT = 75
const MIN_AGENT_LEFT_PCT = 55
const MAX_AGENT_LEFT_PCT = 88
const DEFAULT_DETAIL_TOP_PCT = 55
const MIN_DETAIL_TOP_PCT = 28
const MAX_DETAIL_TOP_PCT = 78

type AgentFocusPane = 'result' | 'process' | null

function readStoredPct(
  key: string,
  fallback: number,
  min: number,
  max: number,
): number {
  try {
    const raw = localStorage.getItem(key)
    if (raw == null || raw === '') return fallback
    const n = Number(raw)
    if (!Number.isFinite(n)) return fallback
    return Math.min(max, Math.max(min, n))
  } catch {
    return fallback
  }
}

function persistPct(key: string, value: number) {
  try {
    localStorage.setItem(key, String(value))
  } catch {
    /* ignore */
  }
}

function SummaryBody({
  text,
  tone,
}: {
  text: string
  tone: 'done' | 'failed' | 'plain'
}) {
  const md = looksLikeMarkdown(text)
  if (md) {
    return (
      <DenseMarkdown
        source={text}
        className={cn(
          'console-agent-execution-dock__summary-md',
          tone === 'done' && 'console-agent-execution-dock__summary-md--done',
          tone === 'failed' && 'console-agent-execution-dock__summary-md--failed',
        )}
      />
    )
  }
  return (
    <p
      className={cn(
        'console-agent-execution-dock__summary',
        tone === 'done' && 'console-agent-execution-dock__summary--done',
        tone === 'failed' && 'console-agent-execution-dock__summary--failed',
      )}
    >
      {text}
    </p>
  )
}

export type OperatorDockMode = 'collapsed' | 'working' | 'maximized'
export type { OperatorToolId }

/** @deprecated Use OperatorDockMode */
export type AgentExecutionDockMode = OperatorDockMode

export type OperatorDockProps = {
  /** Ambient job id; null = idle shell (no live stream). */
  jobId: string | null
  label?: string
  scope?: string
  /** From Recent list / start payload — terminal skips live SSE. */
  jobStatus?: AmbientAgentJob['status']
  onDismiss: () => void
  onOpenAgentDesk?: (jobId?: string) => void
  /**
   * Adopt a Recent task into the left detail pane (in-dock observe).
   * Must not force Agent Desk tab — Agent Desk stays archive-only.
   */
  onSelectJob?: (job: AmbientAgentJob) => void
  /** Deep-link to Engineer → Operator Plane (L-1 heartbeats / MCP / AI Fix). */
  onOpenOperatorPlane?: () => void
  /** Deep-link to Launch Desk → Agent (Mac Mini host publish SSOT). */
  onOpenAgentLaunch?: () => void
  /** Deep-link to Engineer → Dev Sessions (local host runtime). */
  onOpenDevSessions?: () => void
  /** Current console view — highlights matching page link in dock head. */
  activePage?: 'operator-plane' | 'agent-release' | 'agent-desk' | 'dev-sessions' | null
  onComplete?: (job: RemediationJob) => void
  /**
   * Sync live/archive terminal status back to the shell so Agent CTAs unlock
   * while the dock still shows the finished task (Dismiss is optional).
   */
  onJobStatus?: (status: NonNullable<AmbientAgentJob['status']>) => void
  /** Uncontrolled initial mode when expanded defaults to working. */
  defaultExpanded?: boolean
  /** Controlled expanded (working/maximized). When false → collapsed. */
  expanded?: boolean
  onExpandedChange?: (expanded: boolean) => void
  /** Controlled tool slot. Ambient job forces Agent. */
  toolId?: OperatorToolId
  onToolIdChange?: (toolId: OperatorToolId) => void
}

/** @deprecated Use OperatorDockProps */
export type AgentExecutionDockProps = OperatorDockProps

const TOOL_OPTIONS = [
  { value: 'agent', label: 'Agent' },
  { value: 'sessions', label: 'Sessions' },
  { value: 'console', label: 'Console' },
] as const

function readStoredHeight(): number | null {
  try {
    const raw = localStorage.getItem(DOCK_HEIGHT_KEY)
    if (raw == null || raw === '') return null
    const n = Number(raw)
    return Number.isFinite(n) && n >= MIN_WORKING_PX ? n : null
  } catch {
    return null
  }
}

function defaultWorkingHeightPx(): number {
  const fromVh = Math.round((window.innerHeight * DEFAULT_WORKING_VH) / 100)
  const fromRem = DEFAULT_WORKING_REM * 16
  return Math.min(fromVh, fromRem)
}

/** Colored P✓ / S✗ marks — green ok, red down (not muted gray). */
function HostMetaMarks({
  pulse,
}: {
  pulse: ReturnType<typeof useAgentHostPulse>
}) {
  if (!pulse.bridgeReady || (pulse.primary == null && pulse.standby == null)) {
    return <>{pulse.hostMetaShort}</>
  }
  const bits: ReactNode[] = []
  if (pulse.primary != null) {
    bits.push(
      <span key="p">
        P
        <span className={pulse.primaryOk ? 'lamp-ok' : 'lamp-fail'}>
          {pulse.primaryOk ? '✓' : '✗'}
        </span>
      </span>,
    )
  }
  if (pulse.standby != null) {
    bits.push(
      <span key="s">
        S
        <span className={pulse.standbyOk ? 'lamp-ok' : 'lamp-fail'}>
          {pulse.standbyOk ? '✓' : '✗'}
        </span>
      </span>,
    )
  }
  if (bits.length === 0) return <>{pulse.hostMetaShort}</>
  return (
    <>
      Host ·{' '}
      {bits.map((b, i) => (
        <span key={i}>
          {i > 0 ? ' ' : null}
          {b}
        </span>
      ))}
    </>
  )
}

/**
 * Shell-level Operator Dock — Agent (ambient Fix) + Console (SSH) tool slots.
 * Agent Desk remains archive only (explicit Open in Agent Desk on Agent slot).
 * Collapse keeps Console mounted so SSH sessions survive.
 */
export function OperatorDock({
  jobId,
  label,
  scope,
  jobStatus,
  onDismiss,
  onOpenAgentDesk,
  onSelectJob,
  onOpenOperatorPlane,
  onOpenAgentLaunch,
  onOpenDevSessions,
  activePage = null,
  onComplete,
  onJobStatus,
  defaultExpanded = false,
  expanded: expandedProp,
  onExpandedChange,
  toolId: toolIdProp,
  onToolIdChange,
}: OperatorDockProps) {
  const idle = jobId == null || jobId === ''
  const knownTerminal =
    jobStatus === 'done' || jobStatus === 'failed' || jobStatus === 'cancelled'
  const controlled = expandedProp != null
  const toolControlled = toolIdProp != null
  const [mode, setMode] = useState<OperatorDockMode>(() =>
    defaultExpanded ? 'working' : 'collapsed',
  )
  const [heightPx, setHeightPx] = useState(() => readStoredHeight() ?? defaultWorkingHeightPx())
  const [internalToolId, setInternalToolId] = useState<OperatorToolId>(readStoredTool)
  const [agentLeftPct, setAgentLeftPct] = useState(() =>
    readStoredPct(AGENT_H_SPLIT_KEY, DEFAULT_AGENT_LEFT_PCT, MIN_AGENT_LEFT_PCT, MAX_AGENT_LEFT_PCT),
  )
  const [detailTopPct, setDetailTopPct] = useState(() =>
    readStoredPct(AGENT_V_SPLIT_KEY, DEFAULT_DETAIL_TOP_PCT, MIN_DETAIL_TOP_PCT, MAX_DETAIL_TOP_PCT),
  )
  const [focusPane, setFocusPane] = useState<AgentFocusPane>(null)
  const [approvalLogsOpen, setApprovalLogsOpen] = useState(false)
  const { mode: approvalMode, setMode: setApprovalMode } = useAgentApprovalMode()
  const autoRespondedRef = useRef<string | null>(null)
  const [autoPickHint, setAutoPickHint] = useState<string | null>(null)
  const dragRef = useRef<{ startY: number; startH: number } | null>(null)
  const heightPxRef = useRef(heightPx)
  const agentLeftPctRef = useRef(agentLeftPct)
  const detailTopPctRef = useRef(detailTopPct)
  const agentSplitRef = useRef<HTMLDivElement>(null)
  const detailSplitRef = useRef<HTMLDivElement>(null)
  const hDragRef = useRef<{ startX: number; startPct: number } | null>(null)
  const vDragRef = useRef<{ startY: number; startPct: number } | null>(null)

  const toolId = toolControlled ? toolIdProp : internalToolId
  const hostPulse = useAgentHostPulse()

  useEffect(() => {
    agentLeftPctRef.current = agentLeftPct
  }, [agentLeftPct])
  useEffect(() => {
    detailTopPctRef.current = detailTopPct
  }, [detailTopPct])
  useEffect(() => {
    setFocusPane(null)
  }, [jobId])

  const setToolId = useCallback(
    (next: OperatorToolId) => {
      persistOperatorTool(next)
      if (!toolControlled) setInternalToolId(next)
      onToolIdChange?.(next)
    },
    [toolControlled, onToolIdChange],
  )

  useEffect(() => {
    heightPxRef.current = heightPx
  }, [heightPx])

  /** Ambient Fix forces Agent slot. */
  useEffect(() => {
    if (!idle) setToolId('agent')
  }, [idle, jobId, setToolId])

  /** Recent rail + Dismiss replace auto-dismiss — keep selected task visible until user switches or dismisses. */
  const session = useAgentJobLiveSession(jobId, {
    onComplete,
    onDismiss: idle ? undefined : onDismiss,
    autoDismissMs: 0,
    knownTerminal,
  })

  useEffect(() => {
    if (!session.isTerminal || session.job == null) return
    const status = session.job.status
    if (status !== 'done' && status !== 'failed' && status !== 'cancelled') return
    if (jobStatus === status) return
    onJobStatus?.(status)
  }, [session.isTerminal, session.job, jobStatus, onJobStatus])

  useEffect(() => {
    if (!controlled) return
    if (expandedProp) {
      setMode(m => (m === 'collapsed' ? 'working' : m))
    } else {
      setMode('collapsed')
    }
  }, [controlled, expandedProp])

  const setExpanded = useCallback(
    (next: boolean) => {
      if (next) {
        setMode(m => (m === 'collapsed' ? 'working' : m))
      } else {
        setMode('collapsed')
      }
      onExpandedChange?.(next)
    },
    [onExpandedChange],
  )

  const expandWorking = () => setExpanded(true)
  const collapse = () => setExpanded(false)
  const toggleMaximize = () => {
    setMode(m => {
      const next = m === 'maximized' ? 'working' : 'maximized'
      if (controlled) onExpandedChange?.(true)
      return next
    })
  }

  const onResizePointerDown = (e: ReactPointerEvent) => {
    if (mode !== 'working') return
    e.preventDefault()
    dragRef.current = { startY: e.clientY, startH: heightPxRef.current }
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
  }

  const onResizePointerMove = (e: ReactPointerEvent) => {
    if (dragRef.current == null) return
    const delta = dragRef.current.startY - e.clientY
    const maxH = Math.round((window.innerHeight * MAX_WORKING_VH) / 100)
    const next = Math.min(maxH, Math.max(MIN_WORKING_PX, dragRef.current.startH + delta))
    heightPxRef.current = next
    setHeightPx(next)
  }

  const onResizePointerUp = (e: ReactPointerEvent) => {
    if (dragRef.current == null) return
    dragRef.current = null
    try {
      ;(e.target as HTMLElement).releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
    try {
      localStorage.setItem(DOCK_HEIGHT_KEY, String(heightPxRef.current))
    } catch {
      /* ignore */
    }
  }

  const onAgentHPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    const narrow =
      typeof window !== 'undefined' && window.matchMedia('(max-width: 52rem)').matches
    hDragRef.current = {
      startX: narrow ? e.clientY : e.clientX,
      startPct: agentLeftPctRef.current,
    }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  const onAgentHPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (hDragRef.current == null || agentSplitRef.current == null) return
    const narrow =
      typeof window !== 'undefined' && window.matchMedia('(max-width: 52rem)').matches
    const rect = agentSplitRef.current.getBoundingClientRect()
    const span = narrow ? rect.height : rect.width
    if (span <= 0) return
    const delta =
      (((narrow ? e.clientY : e.clientX) - hDragRef.current.startX) / span) * 100
    const next = Math.min(
      MAX_AGENT_LEFT_PCT,
      Math.max(MIN_AGENT_LEFT_PCT, hDragRef.current.startPct + delta),
    )
    agentLeftPctRef.current = next
    setAgentLeftPct(next)
  }
  const onAgentHPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (hDragRef.current == null) return
    hDragRef.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
    persistPct(AGENT_H_SPLIT_KEY, agentLeftPctRef.current)
  }

  const onDetailVPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    vDragRef.current = { startY: e.clientY, startPct: detailTopPctRef.current }
    e.currentTarget.setPointerCapture(e.pointerId)
  }
  const onDetailVPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (vDragRef.current == null || detailSplitRef.current == null) return
    const span = detailSplitRef.current.getBoundingClientRect().height
    if (span <= 0) return
    const next = Math.min(
      MAX_DETAIL_TOP_PCT,
      Math.max(
        MIN_DETAIL_TOP_PCT,
        vDragRef.current.startPct + ((e.clientY - vDragRef.current.startY) / span) * 100,
      ),
    )
    detailTopPctRef.current = next
    setDetailTopPct(next)
  }
  const onDetailVPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (vDragRef.current == null) return
    vDragRef.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
    persistPct(AGENT_V_SPLIT_KEY, detailTopPctRef.current)
  }

  const toggleFocusPane = (pane: Exclude<AgentFocusPane, null>) => {
    setFocusPane(prev => (prev === pane ? null : pane))
  }

  const {
    job,
    connected,
    error,
    isTerminal,
    isArchive,
    historyLoading,
    pendingApproval,
    liveFeed,
    feedStats,
    recentEvents,
    elapsed,
    bannerVariant,
    statusLabel,
    respondPending,
    respond,
    reach,
  } = session

  const connectionError =
    error != null && !isBenignRemediationStreamError(error) ? error : null

  useEffect(() => {
    if (pendingApproval != null) setApprovalLogsOpen(false)
    // eslint-disable-next-line react-hooks/exhaustive-deps -- only reset logs when approval id changes
  }, [pendingApproval?.id])

  useEffect(() => {
    autoRespondedRef.current = null
    setAutoPickHint(null)
  }, [jobId])

  useEffect(() => {
    if (approvalMode !== 'auto') return
    if (pendingApproval == null || isArchive || isTerminal) {
      if (pendingApproval == null) setAutoPickHint(null)
      return
    }
    if (respondPending) return
    if (autoRespondedRef.current === pendingApproval.id) return
    const picked = buildAutoApprovalResponse(pendingApproval)
    if (picked == null) return
    autoRespondedRef.current = pendingApproval.id
    setAutoPickHint(picked.optionLabel)
    respond(picked.optionId, undefined, picked.commitMessage)
  }, [
    approvalMode,
    pendingApproval,
    isArchive,
    isTerminal,
    respondPending,
    respond,
  ])

  // P2-C: mid-flight remediation phases → Activity Feed detail
  const lastActivityPhaseRef = useRef<string | null>(null)
  useEffect(() => {
    if (jobId == null || jobId === '') {
      lastActivityPhaseRef.current = null
      return
    }
    if (job == null) return
    if (isTerminal) {
      const existing = getActivityEvents().find(e => e.id === `agent:${jobId}`)
      if (
        existing == null ||
        existing.phase === 'completed' ||
        existing.phase === 'failed' ||
        existing.phase === 'settled'
      ) {
        lastActivityPhaseRef.current = `terminal:${job.status}`
        return
      }
      const ok = job.status === 'done'
      const terminalKey = `terminal:${job.status}`
      if (lastActivityPhaseRef.current === terminalKey) return
      lastActivityPhaseRef.current = terminalKey
      updateActivityPhase(`agent:${jobId}`, ok ? 'completed' : 'failed', {
        settledOutcome: ok ? 'resolved' : 'error',
        detail: job.summary?.trim() || job.status,
      })
      return
    }
    const phase = job.phase
    if (phase == null || phase === 'done' || phase === 'failed' || phase === 'cancelled') return
    if (phase === lastActivityPhaseRef.current) return
    lastActivityPhaseRef.current = phase
    updateActivityPhase(`agent:${jobId}`, 'applying', { detail: phase })
  }, [jobId, job, job?.phase, job?.status, job?.summary, isTerminal])

  const showInlineFeed = toolId === 'agent' && !idle && !isTerminal && liveFeed != null
  const showFeedPlaceholder =
    toolId === 'agent' && !idle && !isTerminal && liveFeed == null && connected
  const showStats =
    toolId === 'agent' && !idle && !isTerminal && (feedStats.toolCalls > 0 || feedStats.eventCount > 0)
  const headStatus =
    toolId === 'console'
      ? mode === 'collapsed'
        ? 'SSH'
        : 'Console'
      : toolId === 'sessions'
        ? mode === 'collapsed'
          ? 'Sessions'
          : 'Host sessions'
        : idle
          ? 'Idle'
          : statusLabel
  /** Idle Agent: lamp follows L-1 Host pulse (not gray unknown). Active Fix: session reach. */
  const headReach =
    toolId === 'console'
      ? hostPulse.hostReach === 'unknown'
        ? 'ok'
        : hostPulse.hostReach
      : toolId === 'sessions'
        ? 'ok'
        : idle
          ? hostPulse.hostReach
          : reach
  const variantClass =
    toolId === 'console' || toolId === 'sessions' ? 'idle' : idle ? 'idle' : bannerVariant
  const collapsed = mode === 'collapsed'
  const bodyVisible = !collapsed

  const bodyStyle =
    mode === 'working'
      ? { height: `${heightPx}px` }
      : mode === 'maximized'
        ? undefined
        : undefined

  return (
    <div
      className={cn(
        'console-agent-execution-dock console-operator-dock',
        `console-agent-execution-dock--${mode}`,
        `console-agent-execution-dock--${variantClass}`,
      )}
      role="region"
      aria-label="Operator dock"
      style={bodyStyle}
    >
      {mode === 'working' && (
        <div
          className="console-agent-execution-dock__resize"
          onPointerDown={onResizePointerDown}
          onPointerMove={onResizePointerMove}
          onPointerUp={onResizePointerUp}
          role="separator"
          aria-orientation="horizontal"
          aria-label="Resize operator dock"
        />
      )}

      <div className="console-agent-execution-dock__head">
        <StatusLamp value={headReach} kind="reach" />
        <span className="console-agent-execution-dock__kicker">OPERATOR</span>
        <SegmentControl
          ariaLabel="Operator dock tool"
          size="sm"
          value={toolId}
          options={[...TOOL_OPTIONS]}
          onChange={v => {
            const next = v as OperatorToolId
            setToolId(next)
            if (next === 'sessions' && mode === 'collapsed') expandWorking()
          }}
        />
        {toolId === 'agent' && (
          <span
            title={
              approvalMode === 'auto'
                ? 'Auto-default: when the agent asks for a decision, pick the recommended (first) option'
                : 'Manual: confirm each agent decision yourself'
            }
          >
            <SegmentControl
              ariaLabel="Agent approval mode"
              size="sm"
              value={approvalMode}
              options={[
                { value: 'auto', label: 'Auto-default' },
                { value: 'manual', label: 'Manual' },
              ]}
              onChange={v => setApprovalMode(v as 'auto' | 'manual')}
            />
          </span>
        )}
        {toolId === 'agent' && !idle && label != null && label !== '' && (
          <span className="console-agent-execution-dock__label" title={label}>
            {label}
          </span>
        )}
        {toolId === 'agent' && !idle && scope != null && scope !== '' && (
          <span className="console-agent-execution-dock__scope" title={scope}>
            {scope}
          </span>
        )}
        <span className="console-agent-execution-dock__status">{headStatus}</span>
        {onOpenAgentLaunch != null || onOpenOperatorPlane != null ? (
          <button
            type="button"
            className={cn(
              'console-agent-execution-dock__host-meta',
              'console-agent-execution-dock__host-meta--link',
              hostPulse.anyRunnerDown && 'console-agent-execution-dock__host-meta--warn',
            )}
            title={`${hostPulse.hostMetaTitle}\nOpen Launch Desk · Agent (host publish)`}
            onClick={() => (onOpenAgentLaunch ?? onOpenOperatorPlane)?.()}
          >
            <HostMetaMarks pulse={hostPulse} />
          </button>
        ) : (
          <span
            className={cn(
              'console-agent-execution-dock__host-meta',
              hostPulse.anyRunnerDown && 'console-agent-execution-dock__host-meta--warn',
            )}
            title={hostPulse.hostMetaTitle}
          >
            <HostMetaMarks pulse={hostPulse} />
          </span>
        )}
        {hostPulse.deployMetaShort != null && (
          <button
            type="button"
            className="console-agent-execution-dock__deploy-meta"
            title="Host update in progress — open Launch Desk · Agent for log / Update"
            onClick={() => (onOpenAgentLaunch ?? onOpenOperatorPlane)?.()}
            disabled={onOpenAgentLaunch == null && onOpenOperatorPlane == null}
          >
            {hostPulse.deployMetaShort}
          </button>
        )}
        {toolId === 'agent' && mode !== 'collapsed' && showInlineFeed && liveFeed != null && (
          <div className="console-agent-execution-dock__feed">
            <span
              className={cn(
                'console-agent-execution-dock__feed-kind',
                `console-agent-execution-dock__feed-kind--${liveFeed.kind}`,
              )}
            >
              {feedKindLabel(liveFeed.kind)}
            </span>
            <span className="console-agent-execution-dock__feed-text" title={liveFeed.text}>
              {liveFeed.text}
            </span>
          </div>
        )}
        {toolId === 'agent' && mode === 'collapsed' && showInlineFeed && liveFeed != null && (
          <span className="console-agent-execution-dock__feed-text" title={liveFeed.text}>
            {liveFeed.text}
          </span>
        )}
        {mode === 'collapsed' && toolId === 'agent' && idle && (
          <span className="console-agent-execution-dock__feed-text console-agent-execution-dock__feed-text--placeholder">
            {hostPulse.deployRunning
              ? 'Host update in progress'
              : hostPulse.allRunnersDown
                ? 'Runners unreachable'
                : 'No ambient Fix — expand for status'}
          </span>
        )}
        {mode === 'collapsed' && toolId === 'console' && (
          <span className="console-agent-execution-dock__feed-text console-agent-execution-dock__feed-text--placeholder">
            SSH console — expand to connect
          </span>
        )}
        {mode === 'collapsed' && toolId === 'sessions' && (
          <span className="console-agent-execution-dock__feed-text console-agent-execution-dock__feed-text--placeholder">
            Local host sessions — expand for status &amp; consoles
          </span>
        )}
        {showFeedPlaceholder && mode !== 'collapsed' && (
          <span className="console-agent-execution-dock__feed-text console-agent-execution-dock__feed-text--placeholder">
            Waiting for agent activity…
          </span>
        )}
        {showStats && mode !== 'collapsed' && (
          <span className="console-agent-execution-dock__stats">
            {feedStats.toolCalls > 0 && `${feedStats.toolCalls} tool${feedStats.toolCalls === 1 ? '' : 's'}`}
            {feedStats.toolCalls > 0 && feedStats.eventCount > feedStats.toolCalls && ' · '}
            {feedStats.eventCount > feedStats.toolCalls && `${feedStats.eventCount} events`}
          </span>
        )}
        {toolId === 'agent' && mode !== 'collapsed' && !idle && (
          <AgentPhaseIndicator currentPhase={job?.phase} failed={job?.status === 'failed'} compact />
        )}
        {toolId === 'agent' && elapsed != null && !idle && !isTerminal && (
          <span className="console-agent-execution-dock__elapsed">{elapsed}</span>
        )}
        {toolId === 'agent' && !idle && !connected && !isTerminal && connectionError == null && (
          <span className="console-agent-execution-dock__connecting">connecting…</span>
        )}

        <div className="console-agent-execution-dock__actions">
          {/* Page links first; window chrome (max/collapse) icon-only on the right */}
          {onOpenAgentLaunch != null && (
            <Button
              variant="ghost"
              size="xs"
              className={cn(
                'gap-1',
                activePage === 'agent-release'
                  ? 'console-agent-execution-dock__page-link--active'
                  : 'console-agent-execution-dock__page-link',
              )}
              onClick={onOpenAgentLaunch}
              title="Launch Desk · Agent — Mac Mini host publish"
              aria-current={activePage === 'agent-release' ? 'page' : undefined}
            >
              Launch Agent
            </Button>
          )}
          {onOpenOperatorPlane != null && (
            <Button
              variant="ghost"
              size="xs"
              className={cn(
                'gap-1',
                activePage === 'operator-plane'
                  ? 'console-agent-execution-dock__page-link--active'
                  : 'console-agent-execution-dock__page-link',
              )}
              onClick={onOpenOperatorPlane}
              title="Operator Plane (L-1) — heartbeats / smoke / MCP / AI Fix"
              aria-current={activePage === 'operator-plane' ? 'page' : undefined}
            >
              <LifeBuoy className="console-agent-execution-dock__action-icon" aria-hidden />
              Operator Plane
            </Button>
          )}
          {toolId === 'sessions' && onOpenDevSessions != null && (
            <Button
              variant="ghost"
              size="xs"
              className={cn(
                'gap-1',
                activePage === 'dev-sessions'
                  ? 'console-agent-execution-dock__page-link--active'
                  : 'console-agent-execution-dock__page-link',
              )}
              onClick={onOpenDevSessions}
              title="Open Dev Sessions page"
              aria-current={activePage === 'dev-sessions' ? 'page' : undefined}
            >
              Dev Sessions
            </Button>
          )}
          {toolId === 'agent' && onOpenAgentDesk != null && (
            <Button
              variant="ghost"
              size="xs"
              className={cn(
                'gap-1',
                activePage === 'agent-desk'
                  ? 'console-agent-execution-dock__page-link--active'
                  : 'console-agent-execution-dock__page-link',
              )}
              onClick={() => onOpenAgentDesk(jobId ?? undefined)}
              title="Agent Desk — archive / job detail"
              aria-current={activePage === 'agent-desk' ? 'page' : undefined}
            >
              <Bot className="console-agent-execution-dock__action-icon" aria-hidden />
              Agent Desk
            </Button>
          )}
          {toolId === 'agent' && !idle && isTerminal && (
            <Button variant="outline" size="xs" onClick={onDismiss}>
              <X className="console-agent-execution-dock__action-icon" aria-hidden />
              Dismiss
            </Button>
          )}
          {mode === 'collapsed' ? (
            <Button
              variant="outline"
              size="xs"
              className="console-agent-execution-dock__chrome-btn"
              onClick={expandWorking}
              title="Expand dock"
              aria-label="Expand dock"
            >
              <ChevronUp className="console-agent-execution-dock__action-icon" aria-hidden />
            </Button>
          ) : (
            <>
              <Button
                variant="outline"
                size="xs"
                className="console-agent-execution-dock__chrome-btn"
                onClick={toggleMaximize}
                title={mode === 'maximized' ? 'Restore dock height' : 'Maximize dock'}
                aria-label={mode === 'maximized' ? 'Restore dock height' : 'Maximize dock'}
              >
                {mode === 'maximized' ? (
                  <Minimize2 className="console-agent-execution-dock__action-icon" aria-hidden />
                ) : (
                  <Maximize2 className="console-agent-execution-dock__action-icon" aria-hidden />
                )}
              </Button>
              <Button
                variant="outline"
                size="xs"
                className="console-agent-execution-dock__chrome-btn"
                onClick={collapse}
                title="Collapse dock"
                aria-label="Collapse dock"
                aria-expanded
              >
                <ChevronDown className="console-agent-execution-dock__action-icon" aria-hidden />
              </Button>
            </>
          )}
        </div>
      </div>

      {/* Always mounted so Console SSH survives collapse; visibility toggled. */}
      <div
        className={cn(
          'console-agent-execution-dock__body',
          !bodyVisible && 'console-operator-dock__body--collapsed',
        )}
        aria-hidden={!bodyVisible}
        hidden={!bodyVisible ? undefined : undefined}
        style={!bodyVisible ? { display: 'none' } : undefined}
      >
        <div
          className={cn(
            'console-operator-dock__tool',
            'console-operator-dock__tool--agent',
            'min-h-0 flex-1 flex flex-col',
            toolId !== 'agent' && 'console-operator-dock__tool--inactive',
          )}
          style={toolId !== 'agent' ? { display: 'none' } : undefined}
        >
          <div
            ref={agentSplitRef}
            className="console-agent-execution-dock__agent-split"
            style={{
              gridTemplateColumns: `minmax(0, ${agentLeftPct}fr) 0.35rem minmax(8rem, ${100 - agentLeftPct}fr)`,
              ['--dock-agent-left' as string]: String(agentLeftPct),
              ['--dock-agent-right' as string]: String(100 - agentLeftPct),
            }}
          >
            <div className="console-agent-execution-dock__detail">
              {idle ? (
                <div className="console-agent-execution-dock__idle-intro">
                  <p className="console-agent-execution-dock__idle-title">
                    No ambient Agent Fix running
                  </p>
                  <p className="console-agent-execution-dock__idle-copy">
                    Select a Recent task to observe progress, or start Fix from Daily Ops / Mission
                    Launch.
                  </p>
                  {hostPulse.deployRunning && (
                    <p className="console-agent-execution-dock__idle-copy console-agent-execution-dock__idle-copy--warn">
                      Host update in progress — Fix may be flaky until deploy finishes.
                      {(onOpenAgentLaunch ?? onOpenOperatorPlane) != null && (
                        <>
                          {' '}
                          <button
                            type="button"
                            className="console-agent-execution-dock__inline-link"
                            onClick={() => (onOpenAgentLaunch ?? onOpenOperatorPlane)?.()}
                          >
                            Launch Agent
                          </button>
                        </>
                      )}
                    </p>
                  )}
                  {!hostPulse.deployRunning && hostPulse.allRunnersDown && (
                    <p className="console-agent-execution-dock__idle-copy console-agent-execution-dock__idle-copy--warn">
                      L-1 runners unreachable — recover hosts via Launch Agent / Operator Plane.
                      {(onOpenAgentLaunch ?? onOpenOperatorPlane) != null && (
                        <>
                          {' '}
                          <button
                            type="button"
                            className="console-agent-execution-dock__inline-link"
                            onClick={() => (onOpenAgentLaunch ?? onOpenOperatorPlane)?.()}
                          >
                            Launch Agent
                          </button>
                        </>
                      )}
                    </p>
                  )}
                </div>
              ) : (
                <div
                  className={cn(
                    'console-agent-execution-dock__detail-live',
                    pendingApproval != null &&
                      'console-agent-execution-dock__detail-live--awaiting-decision',
                  )}
                >
                  {(hostPulse.deployRunning ||
                    isArchive ||
                    connectionError != null ||
                    pendingApproval != null) && (
                    <div className="console-agent-execution-dock__meta-strip">
                      {hostPulse.deployRunning && (
                        <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--warn">
                          Host update in progress — Fix may be flaky
                          {(onOpenAgentLaunch ?? onOpenOperatorPlane) != null && (
                            <>
                              {' · '}
                              <button
                                type="button"
                                className="console-agent-execution-dock__inline-link"
                                onClick={() => (onOpenAgentLaunch ?? onOpenOperatorPlane)?.()}
                              >
                                Launch Agent
                              </button>
                            </>
                          )}
                        </p>
                      )}
                      {isArchive && (
                        <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--archive">
                          Archive view — runner no longer has this live session
                          {onOpenAgentDesk != null && jobId != null && (
                            <>
                              {' · '}
                              <button
                                type="button"
                                className="console-agent-execution-dock__inline-link"
                                onClick={() => onOpenAgentDesk(jobId)}
                              >
                                Open in Agent Desk
                              </button>
                            </>
                          )}
                        </p>
                      )}
                      {connectionError != null && !isArchive && (
                        <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--failed">
                          Connection: {connectionError}
                        </p>
                      )}
                      {pendingApproval != null && approvalMode === 'manual' && (
                        <div className="console-agent-execution-dock__approval">
                          <RemediationApprovalBlock
                            event={pendingApproval}
                            compact
                            submitting={respondPending}
                            onRespond={(optionId, note, commitMessage) =>
                              respond(optionId, note, commitMessage)
                            }
                          />
                        </div>
                      )}
                      {pendingApproval != null && approvalMode === 'auto' && (
                        <p className="m-0 text-dense-meta text-muted-foreground">
                          Auto-default · selecting{' '}
                          <span className="font-medium text-foreground">
                            {autoPickHint ?? 'recommended option'}
                          </span>
                          …
                        </p>
                      )}
                    </div>
                  )}

                  {pendingApproval != null &&
                    approvalMode === 'manual' &&
                    !approvalLogsOpen && (
                    <button
                      type="button"
                      className="console-agent-execution-dock__logs-collapsed"
                      onClick={() => setApprovalLogsOpen(true)}
                    >
                      Show Result / Process
                    </button>
                  )}

                  {(pendingApproval == null ||
                    approvalLogsOpen ||
                    approvalMode === 'auto') && (
                  <div
                    ref={detailSplitRef}
                    className={cn(
                      'console-agent-execution-dock__detail-split',
                      focusPane === 'result' &&
                        'console-agent-execution-dock__detail-split--focus-result',
                      focusPane === 'process' &&
                        'console-agent-execution-dock__detail-split--focus-process',
                      pendingApproval != null &&
                        approvalMode === 'manual' &&
                        'console-agent-execution-dock__detail-split--under-approval',
                    )}
                    style={
                      focusPane == null
                        ? {
                            gridTemplateRows: `minmax(4rem, ${detailTopPct}fr) 0.35rem minmax(4rem, ${100 - detailTopPct}fr)`,
                          }
                        : undefined
                    }
                  >
                    <section
                      className="console-agent-execution-dock__result"
                      aria-label="Task result"
                    >
                      <div className="console-agent-execution-dock__pane-head">
                        <h3 className="console-agent-execution-dock__pane-title">Result</h3>
                        {pendingApproval != null && approvalMode === 'manual' && (
                          <Button
                            type="button"
                            variant="ghost"
                            size="xs"
                            className="text-muted-foreground"
                            onClick={() => setApprovalLogsOpen(false)}
                            title="Hide Result / Process — focus decision"
                          >
                            Hide
                          </Button>
                        )}
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          className="console-agent-execution-dock__pane-focus"
                          onClick={() => toggleFocusPane('result')}
                          title={
                            focusPane === 'result' ? 'Restore split view' : 'Maximize result'
                          }
                          aria-label={
                            focusPane === 'result' ? 'Restore split view' : 'Maximize result'
                          }
                        >
                          {focusPane === 'result' ? (
                            <Minimize2
                              className="console-agent-execution-dock__action-icon"
                              aria-hidden
                            />
                          ) : (
                            <Maximize2
                              className="console-agent-execution-dock__action-icon"
                              aria-hidden
                            />
                          )}
                        </Button>
                      </div>
                      <div className="console-agent-execution-dock__result-body dense-scroll-y">
                        {bannerVariant === 'done' &&
                          job?.summary != null &&
                          job.summary !== '' && (
                            <SummaryBody text={job.summary} tone="done" />
                          )}
                        {bannerVariant === 'failed' && (
                          <SummaryBody
                            text={
                              job?.error != null &&
                              job.error !== 'orphaned' &&
                              job.error !== ''
                                ? job.error
                                : (job?.summary ?? 'Unknown error')
                            }
                            tone="failed"
                          />
                        )}
                        {bannerVariant !== 'done' &&
                          bannerVariant !== 'failed' &&
                          job?.summary != null &&
                          job.summary !== '' && (
                            <SummaryBody text={job.summary} tone="plain" />
                          )}
                        {(job?.summary == null || job.summary === '') &&
                          bannerVariant !== 'failed' && (
                            <p className="console-agent-execution-dock__log-empty">
                              {isTerminal || isArchive
                                ? 'No result summary stored for this task'
                                : 'Waiting for result…'}
                            </p>
                          )}
                      </div>
                    </section>

                    {focusPane == null && (
                      <div
                        className="console-agent-execution-dock__h-resize"
                        role="separator"
                        aria-orientation="horizontal"
                        aria-label="Resize result and process"
                        title="Drag to resize result / process"
                        onPointerDown={onDetailVPointerDown}
                        onPointerMove={onDetailVPointerMove}
                        onPointerUp={onDetailVPointerUp}
                      />
                    )}

                    <section
                      className="console-agent-execution-dock__process"
                      aria-label="Task process log"
                    >
                      <div className="console-agent-execution-dock__pane-head">
                        <h3 className="console-agent-execution-dock__pane-title">Process</h3>
                        <Button
                          type="button"
                          variant="ghost"
                          size="xs"
                          className="console-agent-execution-dock__pane-focus"
                          onClick={() => toggleFocusPane('process')}
                          title={
                            focusPane === 'process' ? 'Restore split view' : 'Maximize process'
                          }
                          aria-label={
                            focusPane === 'process' ? 'Restore split view' : 'Maximize process'
                          }
                        >
                          {focusPane === 'process' ? (
                            <Minimize2
                              className="console-agent-execution-dock__action-icon"
                              aria-hidden
                            />
                          ) : (
                            <Maximize2
                              className="console-agent-execution-dock__action-icon"
                              aria-hidden
                            />
                          )}
                        </Button>
                      </div>
                      <div className="console-agent-execution-dock__log dense-scroll-y">
                        {historyLoading ? (
                          <p className="console-agent-execution-dock__log-empty">
                            Loading interaction history…
                          </p>
                        ) : recentEvents.length === 0 ? (
                          <p className="console-agent-execution-dock__log-empty">
                            {isTerminal || isArchive
                              ? 'No event log stored for this task'
                              : 'Waiting for agent activity…'}
                          </p>
                        ) : (
                          <DockProcessFeed events={recentEvents} />
                        )}
                      </div>
                    </section>
                  </div>
                  )}
                </div>
              )}
            </div>

            <div
              className="console-agent-execution-dock__v-resize"
              role="separator"
              aria-orientation="vertical"
              aria-label="Resize task detail and recent tasks"
              title="Drag to resize detail / recent"
              onPointerDown={onAgentHPointerDown}
              onPointerMove={onAgentHPointerMove}
              onPointerUp={onAgentHPointerUp}
            />

            <DockRecentAgentTasks
              enabled={bodyVisible && toolId === 'agent'}
              activeJobId={jobId}
              onSelectJob={onSelectJob}
              onOpenDesk={onOpenAgentDesk != null ? () => onOpenAgentDesk() : undefined}
            />
          </div>
        </div>

        <div
          className={cn(
            'console-operator-dock__tool',
            'console-operator-dock__tool--sessions',
            'min-h-0 flex-1 flex flex-col',
          )}
          style={toolId !== 'sessions' ? { display: 'none' } : undefined}
        >
          <DockDevSessionsPanel
            enabled={bodyVisible && toolId === 'sessions'}
            onOpenPage={onOpenDevSessions}
          />
        </div>

        <div
          className={cn(
            'console-operator-dock__tool',
            'console-operator-dock__tool--console',
            'min-h-0 flex-1 flex flex-col',
          )}
          style={toolId !== 'console' ? { display: 'none' } : undefined}
        >
          <ServerConsolePanel density="dock" showVerdict={false} />
        </div>
      </div>
    </div>
  )
}

/** @deprecated Prefer OperatorDock — same component. */
export const AgentExecutionDock = OperatorDock
