import { useCallback, useEffect, useRef, useState, type PointerEvent as ReactPointerEvent, type ReactNode } from 'react'
import { Button, SegmentControl, StatusLamp, cn } from '@bifrost/ui'
import { ChevronDown, ChevronUp, Maximize2, Minimize2, X } from 'lucide-react'
import type { RemediationJob } from '@/api/remediationTypes'
import { AgentPhaseIndicator } from '@/components/agent/AgentPhaseIndicator'
import { ServerConsolePanel } from '@/components/ServerConsolePanel'
import { RemediationApprovalBlock } from '@/components/cluster/RemediationApprovalBlock'
import { useAgentJobLiveSession } from '@/hooks/useAgentJobLiveSession'
import { useAgentHostPulse } from '@/hooks/useAgentHostPulse'
import {
  feedKindLabel,
  formatFeedEventLine,
} from '@/lib/agent/agentLiveFeed'

const DOCK_HEIGHT_KEY = 'bifrost.console.agentExecutionDockHeight'
const TOOL_KEY = 'bifrost.console.operatorDockTool'
const DEFAULT_WORKING_VH = 42
const DEFAULT_WORKING_REM = 28
const MIN_WORKING_PX = 160
const MAX_WORKING_VH = 70

export type OperatorDockMode = 'collapsed' | 'working' | 'maximized'
export type OperatorToolId = 'agent' | 'console'

/** @deprecated Use OperatorDockMode */
export type AgentExecutionDockMode = OperatorDockMode

export type OperatorDockProps = {
  /** Ambient job id; null = idle shell (no live stream). */
  jobId: string | null
  label?: string
  scope?: string
  onDismiss: () => void
  onOpenAgentDesk?: (jobId?: string) => void
  /** Deep-link to Engineer → Operator Plane (L-1 Update / smoke SSOT). */
  onOpenOperatorPlane?: () => void
  onComplete?: (job: RemediationJob) => void
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

/** Read last Operator Dock tool slot (Agent | Console). Safe for SSR / private mode. */
export function readStoredTool(): OperatorToolId {
  try {
    const raw = localStorage.getItem(TOOL_KEY)
    if (raw === 'console' || raw === 'agent') return raw
  } catch {
    /* ignore */
  }
  return 'agent'
}

/** Persist Operator Dock tool slot — used in both controlled and uncontrolled modes. */
export function persistOperatorTool(tool: OperatorToolId): void {
  try {
    localStorage.setItem(TOOL_KEY, tool)
  } catch {
    /* ignore */
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
  onDismiss,
  onOpenAgentDesk,
  onOpenOperatorPlane,
  onComplete,
  defaultExpanded = false,
  expanded: expandedProp,
  onExpandedChange,
  toolId: toolIdProp,
  onToolIdChange,
}: OperatorDockProps) {
  const idle = jobId == null || jobId === ''
  const controlled = expandedProp != null
  const toolControlled = toolIdProp != null
  const [mode, setMode] = useState<OperatorDockMode>(() =>
    defaultExpanded ? 'working' : 'collapsed',
  )
  const [heightPx, setHeightPx] = useState(() => readStoredHeight() ?? defaultWorkingHeightPx())
  const [internalToolId, setInternalToolId] = useState<OperatorToolId>(readStoredTool)
  const dragRef = useRef<{ startY: number; startH: number } | null>(null)
  const heightPxRef = useRef(heightPx)

  const toolId = toolControlled ? toolIdProp : internalToolId
  const hostPulse = useAgentHostPulse()

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

  const session = useAgentJobLiveSession(jobId, {
    onComplete,
    onDismiss: idle ? undefined : onDismiss,
    autoDismissMs: idle ? 0 : 5000,
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
      : idle
        ? 'Idle'
        : statusLabel
  /** Idle Agent: lamp follows L-1 Host pulse (not gray unknown). Active Fix: session reach. */
  const headReach =
    toolId === 'console'
      ? hostPulse.hostReach === 'unknown'
        ? 'ok'
        : hostPulse.hostReach
      : idle
        ? hostPulse.hostReach
        : reach
  const variantClass = toolId === 'console' ? 'idle' : idle ? 'idle' : bannerVariant
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
          onChange={v => setToolId(v as OperatorToolId)}
        />
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
        {onOpenOperatorPlane != null ? (
          <button
            type="button"
            className={cn(
              'console-agent-execution-dock__host-meta',
              'console-agent-execution-dock__host-meta--link',
              hostPulse.anyRunnerDown && 'console-agent-execution-dock__host-meta--warn',
            )}
            title={`${hostPulse.hostMetaTitle}\nOpen Operator Plane · Agent hosts`}
            onClick={onOpenOperatorPlane}
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
            title="Host update in progress — open Operator Plane for log / Update"
            onClick={() => onOpenOperatorPlane?.()}
            disabled={onOpenOperatorPlane == null}
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
        {toolId === 'agent' && !idle && !connected && !isTerminal && error == null && (
          <span className="console-agent-execution-dock__connecting">connecting…</span>
        )}

        <div className="console-agent-execution-dock__actions">
          {onOpenOperatorPlane != null && (
            <Button
              variant="ghost"
              size="xs"
              onClick={onOpenOperatorPlane}
              title="Open Operator Plane (L-1) — Update / smoke / MCP"
            >
              Operator Plane
            </Button>
          )}
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
          {toolId === 'agent' && onOpenAgentDesk != null && (
            <Button
              variant="ghost"
              size="xs"
              onClick={() => onOpenAgentDesk(jobId ?? undefined)}
            >
              Open in Agent Desk
            </Button>
          )}
          {toolId === 'agent' && !idle && isTerminal && (
            <Button variant="outline" size="xs" onClick={onDismiss}>
              <X className="console-agent-execution-dock__action-icon" aria-hidden />
              Dismiss
            </Button>
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
            'min-h-0 flex-1 flex flex-col',
            toolId !== 'agent' && 'console-operator-dock__tool--inactive',
          )}
          style={toolId !== 'agent' ? { display: 'none' } : undefined}
        >
          {idle ? (
            <div className="console-agent-execution-dock__idle">
              <p className="console-agent-execution-dock__idle-title">No ambient Agent Fix running</p>
              <p className="console-agent-execution-dock__idle-copy">
                Start Fix from Daily Ops or Mission Launch — live feed and approvals appear here.
                Switch to Console for SSH. Agent Desk is the archive for history and manual tasks.
              </p>
              {hostPulse.deployRunning && (
                <p className="console-agent-execution-dock__idle-copy console-agent-execution-dock__idle-copy--warn">
                  Host update in progress — Fix may be flaky until deploy finishes.
                  {onOpenOperatorPlane != null && (
                    <>
                      {' '}
                      <button
                        type="button"
                        className="console-agent-execution-dock__inline-link"
                        onClick={onOpenOperatorPlane}
                      >
                        Open Operator Plane
                      </button>
                    </>
                  )}
                </p>
              )}
              {!hostPulse.deployRunning && hostPulse.allRunnersDown && (
                <p className="console-agent-execution-dock__idle-copy console-agent-execution-dock__idle-copy--warn">
                  L-1 runners unreachable — recover hosts on Operator Plane.
                  {onOpenOperatorPlane != null && (
                    <>
                      {' '}
                      <button
                        type="button"
                        className="console-agent-execution-dock__inline-link"
                        onClick={onOpenOperatorPlane}
                      >
                        Open Operator Plane
                      </button>
                    </>
                  )}
                </p>
              )}
              {onOpenAgentDesk != null && (
                <Button variant="outline" size="sm" onClick={() => onOpenAgentDesk()}>
                  Open Agent Desk
                </Button>
              )}
            </div>
          ) : (
            <>
              {hostPulse.deployRunning && (
                <p className="console-agent-execution-dock__summary console-agent-execution-dock__summary--warn">
                  Host update in progress — Fix may be flaky
                  {onOpenOperatorPlane != null && (
                    <>
                      {' · '}
                      <button
                        type="button"
                        className="console-agent-execution-dock__inline-link"
                        onClick={onOpenOperatorPlane}
                      >
                        Operator Plane
                      </button>
                    </>
                  )}
                </p>
              )}
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
            </>
          )}
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
