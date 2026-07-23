import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { Button, StatusLamp, cn } from '@bifrost/ui'
import { ChevronDown, ChevronUp, Maximize2, Minimize2, X } from 'lucide-react'
import type { RemediationJob } from '@/api/remediationTypes'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useAgentJobLiveSession } from '@/hooks/useAgentJobLiveSession'
import {
  feedKindLabel,
  formatFeedEventLine,
} from '@/lib/agent/agentLiveFeed'

const DOCK_HEIGHT_KEY = 'bifrost.console.agentExecutionDockHeight'
const DEFAULT_WORKING_VH = 42
const DEFAULT_WORKING_REM = 28
const MIN_WORKING_PX = 160
const MAX_WORKING_VH = 70

export type AgentExecutionDockMode = 'collapsed' | 'working' | 'maximized'

export type AgentExecutionDockProps = {
  jobId: string
  label?: string
  scope?: string
  onDismiss: () => void
  onOpenAgentDesk?: (jobId: string) => void
  onComplete?: (job: RemediationJob) => void
  /** Uncontrolled initial mode when expanded defaults to working. */
  defaultExpanded?: boolean
  /** Controlled expanded (working/maximized). When false → collapsed. */
  expanded?: boolean
  onExpandedChange?: (expanded: boolean) => void
}

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

/**
 * Global bottom Execution Dock for ambient Agent Fix.
 * Collapsed / Working / Maximized — main decision boards stay mounted.
 * Agent Desk is archive only (explicit Open in Agent Desk).
 */
export function AgentExecutionDock({
  jobId,
  label,
  scope,
  onDismiss,
  onOpenAgentDesk,
  onComplete,
  defaultExpanded = true,
  expanded: expandedProp,
  onExpandedChange,
}: AgentExecutionDockProps) {
  const controlled = expandedProp != null
  const [mode, setMode] = useState<AgentExecutionDockMode>(() =>
    defaultExpanded ? 'working' : 'collapsed',
  )
  const [heightPx, setHeightPx] = useState(() => readStoredHeight() ?? defaultWorkingHeightPx())
  const dragRef = useRef<{ startY: number; startH: number } | null>(null)

  const session = useAgentJobLiveSession(jobId, {
    onComplete,
    onDismiss,
    autoDismissMs: 5000,
  })

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
    dragRef.current = { startY: e.clientY, startH: heightPx }
    ;(e.target as HTMLElement).setPointerCapture(e.pointerId)
  }

  const onResizePointerMove = (e: ReactPointerEvent) => {
    if (dragRef.current == null) return
    const delta = dragRef.current.startY - e.clientY
    const maxH = Math.round((window.innerHeight * MAX_WORKING_VH) / 100)
    const next = Math.min(maxH, Math.max(MIN_WORKING_PX, dragRef.current.startH + delta))
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
      localStorage.setItem(DOCK_HEIGHT_KEY, String(heightPx))
    } catch {
      /* ignore */
    }
  }

  const {
    job,
    connected,
    error,
    isTerminal,
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

  const showInlineFeed = !isTerminal && liveFeed != null
  const showFeedPlaceholder = !isTerminal && liveFeed == null && connected
  const showStats = !isTerminal && (feedStats.toolCalls > 0 || feedStats.eventCount > 0)

  const bodyStyle =
    mode === 'working'
      ? { height: `${heightPx}px` }
      : mode === 'maximized'
        ? undefined
        : undefined

  return (
    <div
      className={cn(
        'console-agent-execution-dock',
        `console-agent-execution-dock--${mode}`,
        `console-agent-execution-dock--${bannerVariant}`,
      )}
      role="region"
      aria-label="Agent execution dock"
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
          aria-label="Resize agent dock"
        />
      )}

      <div className="console-agent-execution-dock__head">
        <StatusLamp value={reach} kind="reach" />
        <span className="console-agent-execution-dock__kicker">Agent Fix</span>
        {label != null && label !== '' && (
          <span className="console-agent-execution-dock__label" title={label}>
            {label}
          </span>
        )}
        {scope != null && scope !== '' && (
          <span className="console-agent-execution-dock__scope" title={scope}>
            {scope}
          </span>
        )}
        <span className="console-agent-execution-dock__status">{statusLabel}</span>
        {mode !== 'collapsed' && showInlineFeed && liveFeed != null && (
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
        {mode === 'collapsed' && showInlineFeed && liveFeed != null && (
          <span className="console-agent-execution-dock__feed-text" title={liveFeed.text}>
            {liveFeed.text}
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
        {mode !== 'collapsed' && (
          <AgentPhaseIndicator currentPhase={job?.phase} failed={job?.status === 'failed'} compact />
        )}
        {elapsed != null && !isTerminal && (
          <span className="console-agent-execution-dock__elapsed">{elapsed}</span>
        )}
        {!connected && !isTerminal && error == null && (
          <span className="console-agent-execution-dock__connecting">connecting…</span>
        )}

        <div className="console-agent-execution-dock__actions">
          {mode === 'collapsed' ? (
            <Button variant="outline" size="xs" onClick={expandWorking}>
              <ChevronUp className="console-agent-execution-dock__action-icon" aria-hidden />
              Expand
            </Button>
          ) : (
            <>
              <Button
                variant="outline"
                size="xs"
                onClick={toggleMaximize}
                title={mode === 'maximized' ? 'Restore dock height' : 'Maximize dock'}
              >
                {mode === 'maximized' ? (
                  <Minimize2 className="console-agent-execution-dock__action-icon" aria-hidden />
                ) : (
                  <Maximize2 className="console-agent-execution-dock__action-icon" aria-hidden />
                )}
                {mode === 'maximized' ? 'Restore' : 'Maximize'}
              </Button>
              <Button variant="outline" size="xs" onClick={collapse} aria-expanded>
                <ChevronDown className="console-agent-execution-dock__action-icon" aria-hidden />
                Collapse
              </Button>
            </>
          )}
          {onOpenAgentDesk != null && (
            <Button variant="ghost" size="xs" onClick={() => onOpenAgentDesk(jobId)}>
              Open in Agent Desk
            </Button>
          )}
          {isTerminal && (
            <Button variant="outline" size="xs" onClick={onDismiss}>
              <X className="console-agent-execution-dock__action-icon" aria-hidden />
              Dismiss
            </Button>
          )}
        </div>
      </div>

      {mode !== 'collapsed' && (
        <div className="console-agent-execution-dock__body">
          {bannerVariant === 'done' && job?.summary != null && job.summary !== '' && (
            <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--done">
              {job.summary}
            </p>
          )}
          {bannerVariant === 'failed' && (
            <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--failed">
              {job?.error ?? job?.summary ?? 'Unknown error'}
            </p>
          )}
          {error != null && !isTerminal && (
            <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--failed">
              Connection: {error}
            </p>
          )}

          {pendingApproval != null && (
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

          {!isTerminal && (
            <div className="console-agent-execution-dock__log dense-scroll-y">
              {recentEvents.length === 0 ? (
                <p className="console-agent-execution-dock__log-empty">Waiting for agent activity…</p>
              ) : (
                <ul className="console-agent-execution-dock__log-list">
                  {recentEvents.map(ev => (
                    <li
                      key={ev.id}
                      className={cn(
                        'console-agent-execution-dock__log-item',
                        `console-agent-execution-dock__log-item--${ev.type}`,
                      )}
                    >
                      <span className="console-agent-execution-dock__log-type">{ev.type}</span>
                      <span className="console-agent-execution-dock__log-text">
                        {formatFeedEventLine(ev)}
                      </span>
                    </li>
                  ))}
                </ul>
              )}
            </div>
          )}
        </div>
      )}
    </div>
  )
}
