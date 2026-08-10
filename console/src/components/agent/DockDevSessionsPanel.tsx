import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { createPortal } from 'react-dom'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, ConfirmDialog, DenseTag, SegmentControl, StatusLamp, cn } from '@bifrost/ui'
import {
  ChevronDown,
  ChevronRight,
  Eraser,
  Loader2,
  Maximize2,
  Minimize2,
  Play,
  RefreshCw,
  RotateCw,
  Square,
  X,
} from 'lucide-react'
import {
  controlDevSession,
  fetchDevSessionLogs,
  fetchDevSessions,
  type DevSession,
} from '@/api/devSession'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import { SessionLogLine } from '@/components/agent/sessionLogColor'
import {
  DOCK_SESSION_LOG_LINES_MAXIMIZED,
  DOCK_SESSION_LOG_LINES_TILED,
} from '@/components/agent/sessionLogTone'

type Lamp = 'ok' | 'fail' | 'degraded' | 'unknown'

const SPLIT_KEY = 'bifrost.console.dockSessionsSplitPct.v2'
const PANE_CAP_KEY = 'bifrost.console.dockSessionsPaneCap'
const PINNED_KEY = 'bifrost.console.dockSessionsPinned.v1'
const COLLAPSED_GROUPS_KEY = 'bifrost.console.dockSessionsCollapsedGroups.v1'
const DEFAULT_LEFT_PCT = 75
const MIN_LEFT_PCT = 55
const MAX_LEFT_PCT = 88
const PANE_CAP_OPTIONS = [2, 4, 6, 8, 10] as const
type PaneCap = (typeof PANE_CAP_OPTIONS)[number]
const DEFAULT_PANE_CAP: PaneCap = 4
const MAX_PANE_CAP = 10

function sessionLamp(status: string): Lamp {
  if (status === 'running') return 'ok'
  if (status === 'error') return 'degraded'
  return 'fail'
}

function formatUptime(seconds?: number): string {
  if (seconds == null || seconds <= 0) return '—'
  const h = Math.floor(seconds / 3600)
  const m = Math.floor((seconds % 3600) / 60)
  if (h > 0) return `${h}h ${m}m`
  if (m > 0) return `${m}m`
  return `${seconds}s`
}

function formatLogBytes(n?: number): string {
  if (n == null || n <= 0) return ''
  if (n < 1024) return `${n}B`
  if (n < 1024 * 1024) return `${Math.round(n / 1024)}KiB`
  return `${(n / (1024 * 1024)).toFixed(1)}MiB`
}

function formatLastOutputAgo(epochSec?: number): string | null {
  if (epochSec == null || epochSec <= 0) return null
  const deltaSec = Math.max(0, Math.floor(Date.now() / 1000) - epochSec)
  if (deltaSec < 10) return 'just now'
  if (deltaSec < 60) return `${deltaSec}s ago`
  const m = Math.floor(deltaSec / 60)
  if (m < 60) return `${m}m ago`
  const h = Math.floor(m / 60)
  if (h < 24) return `${h}h ${m % 60}m ago`
  return `${Math.floor(h / 24)}d ago`
}

function isStale(epochSec?: number): boolean {
  if (epochSec == null || epochSec <= 0) return false
  const deltaSec = Math.floor(Date.now() / 1000) - epochSec
  return deltaSec > 120
}

function logOverCap(s: DevSession): boolean {
  const max = s.log_max_bytes ?? 5 * 1024 * 1024
  return (s.log_bytes ?? 0) > max
}

function logNearCap(s: DevSession): boolean {
  const max = s.log_max_bytes ?? 5 * 1024 * 1024
  const bytes = s.log_bytes ?? 0
  return bytes > max * 0.8
}

/** Catalog maps session name `platform` → Deployment `platform-api` (K8s). */
const PLATFORM_SESSION_NAMES = new Set(['platform', 'platform-api'])
const RECONNECT_POLL_MS = 2000
const RECONNECT_MAX_WAIT_MS = 60_000

function isPlatformSession(name: string): boolean {
  return PLATFORM_SESSION_NAMES.has(name)
}

function isClusterMode(sessions: DevSession[]): boolean {
  return sessions.some(s => s.mode === 'k8s')
}

function sessionEnvBadge(sessions: DevSession[]): string | null {
  const env = sessions.find(s => s.env != null && s.env !== '')?.env
  if (env == null || env === '') return null
  const upper = env.toUpperCase()
  if (upper === 'STG' || upper === 'PROD') return upper
  if (upper === 'DEV' || upper === 'DEV-LOCAL') return null
  return upper
}

/** Env identity only — never danger/warning (those are for health). */
function envBadgeVariant(env: string): 'info' | 'neutral' {
  return env === 'PROD' ? 'info' : 'neutral'
}

function isSessionIssue(status: string): boolean {
  return status !== 'running'
}

/** Anomaly-first within a group: error → stopped/other → running. */
function statusSortKey(status: string): number {
  if (status === 'error') return 0
  if (status === 'running') return 2
  return 1
}

function ReconnectingOverlay({ onCancel }: { onCancel: () => void }) {
  const [elapsed, setElapsed] = useState(0)
  const [status, setStatus] = useState<'waiting' | 'recovered' | 'timeout'>('waiting')

  useEffect(() => {
    const t0 = Date.now()
    const interval = setInterval(async () => {
      const dt = Date.now() - t0
      setElapsed(Math.floor(dt / 1000))
      if (dt > RECONNECT_MAX_WAIT_MS) {
        setStatus('timeout')
        clearInterval(interval)
        return
      }
      try {
        const res = await fetch('/api/v1/dev-sessions', { signal: AbortSignal.timeout(2000) })
        if (res.ok) {
          setStatus('recovered')
          clearInterval(interval)
          setTimeout(() => window.location.reload(), 500)
        }
      } catch {
        // still down
      }
    }, RECONNECT_POLL_MS)
    return () => clearInterval(interval)
  }, [])

  return createPortal(
    <div className="fixed inset-0 z-[9999] flex flex-col items-center justify-center gap-4 bg-background/95 backdrop-blur-sm">
      {status === 'waiting' && (
        <>
          <Loader2 className="h-8 w-8 animate-spin text-muted-foreground" />
          <p className="text-sm font-medium text-foreground">
            Platform restarting — reconnecting…
          </p>
          <p className="text-xs text-muted-foreground">{elapsed}s elapsed</p>
          <Button variant="ghost" size="sm" className="mt-2" onClick={onCancel}>
            Cancel (force reload)
          </Button>
        </>
      )}
      {status === 'recovered' && (
        <>
          <StatusLamp value="ok" kind="reach" />
          <p className="text-sm font-medium text-foreground">Connected — reloading…</p>
        </>
      )}
      {status === 'timeout' && (
        <>
          <StatusLamp value="fail" kind="reach" />
          <p className="text-sm font-medium text-foreground">
            Platform did not come back within {RECONNECT_MAX_WAIT_MS / 1000}s
          </p>
          <Button variant="outline" size="sm" onClick={() => window.location.reload()}>
            Retry now
          </Button>
        </>
      )}
    </div>,
    document.body,
  )
}

function readStoredSplitPct(): number {
  try {
    const raw = localStorage.getItem(SPLIT_KEY)
    if (raw == null || raw === '') return DEFAULT_LEFT_PCT
    const n = Number(raw)
    if (!Number.isFinite(n)) return DEFAULT_LEFT_PCT
    return Math.min(MAX_LEFT_PCT, Math.max(MIN_LEFT_PCT, n))
  } catch {
    return DEFAULT_LEFT_PCT
  }
}

function readStoredPaneCap(): PaneCap {
  try {
    const n = Number(localStorage.getItem(PANE_CAP_KEY))
    if ((PANE_CAP_OPTIONS as readonly number[]).includes(n)) return n as PaneCap
  } catch {
    /* ignore */
  }
  return DEFAULT_PANE_CAP
}

function readStoredPinned(): string[] {
  try {
    const raw = localStorage.getItem(PINNED_KEY)
    if (raw == null || raw === '') return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    return parsed.filter((x): x is string => typeof x === 'string' && x !== '')
  } catch {
    return []
  }
}

function persistPinned(names: string[]) {
  try {
    localStorage.setItem(PINNED_KEY, JSON.stringify(names))
  } catch {
    /* ignore */
  }
}

function readStoredCollapsedGroups(): Set<string> {
  try {
    const raw = localStorage.getItem(COLLAPSED_GROUPS_KEY)
    if (raw == null || raw === '') return new Set()
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return new Set()
    return new Set(parsed.filter((x): x is string => typeof x === 'string'))
  } catch {
    return new Set()
  }
}

function persistCollapsedGroups(groups: Set<string>) {
  try {
    localStorage.setItem(COLLAPSED_GROUPS_KEY, JSON.stringify([...groups]))
  } catch {
    /* ignore */
  }
}

function SessionConsolePane({
  session,
  active,
  maximized,
  logsEnabled,
  canOperate,
  isActing,
  showClearLogs,
  canUnpin,
  onSelect,
  onToggleMaximize,
  onClearLogs,
  onReload,
  onUnpin,
}: {
  session: DevSession
  active: boolean
  maximized: boolean
  /** False when dock collapsed or Sessions tool hidden — stops log polling. */
  logsEnabled: boolean
  canOperate: boolean
  isActing: boolean
  showClearLogs: boolean
  canUnpin: boolean
  onSelect: () => void
  onToggleMaximize: () => void
  onClearLogs: () => void
  onReload: () => void
  onUnpin: () => void
}) {
  const scrollRef = useRef<HTMLDivElement>(null)
  const lineLimit = maximized
    ? DOCK_SESSION_LOG_LINES_MAXIMIZED
    : DOCK_SESSION_LOG_LINES_TILED

  const { data: lines } = useQuery({
    queryKey: ['dev-sessions', 'logs', session.name, 'dock', lineLimit],
    queryFn: () => fetchDevSessionLogs(session.name, lineLimit),
    enabled: logsEnabled,
    refetchInterval: !logsEnabled
      ? false
      : session.status === 'running'
        ? 5_000
        : 15_000,
  })

  useEffect(() => {
    if (scrollRef.current) {
      scrollRef.current.scrollTop = scrollRef.current.scrollHeight
    }
  }, [lines])

  return (
    <div
      className={cn(
        'console-dock-sessions__pane',
        active && 'console-dock-sessions__pane--active',
        maximized && 'console-dock-sessions__pane--maximized',
      )}
    >
      <div className="console-dock-sessions__pane-head">
        <button
          type="button"
          className="console-dock-sessions__pane-head-main"
          onClick={onSelect}
          onDoubleClick={onToggleMaximize}
          title={`Focus ${session.label} · double-click to ${maximized ? 'restore tile' : 'maximize'}`}
        >
          <StatusLamp value={sessionLamp(session.status)} kind="reach" />
          <span className="console-dock-sessions__pane-title">{session.label}</span>
          <DenseTag
            variant={
              session.status === 'running'
                ? 'success'
                : session.status === 'error'
                  ? 'warning'
                  : 'neutral'
            }
          >
            {session.status}
          </DenseTag>
          {session.last_output_at != null && (
            <span
              className={cn(
                'ml-1 font-mono text-[10px]',
                isStale(session.last_output_at)
                  ? 'text-warning'
                  : 'text-muted-foreground',
              )}
              title={`Last log output: ${new Date(session.last_output_at * 1000).toLocaleTimeString()}`}
            >
              {formatLastOutputAgo(session.last_output_at)}
            </span>
          )}
        </button>
        {showClearLogs && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-6 w-6 shrink-0 px-0"
            disabled={!canOperate || isActing}
            onClick={e => {
              e.stopPropagation()
              onClearLogs()
            }}
            title="Clear logs"
            aria-label={`Clear logs for ${session.label}`}
          >
            <Eraser className="h-3 w-3" aria-hidden />
          </Button>
        )}
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-6 w-6 shrink-0 px-0"
          disabled={!canOperate || isActing || session.status !== 'running'}
          onClick={e => {
            e.stopPropagation()
            onReload()
          }}
          title={session.mode === 'k8s' ? 'Rollout restart Deployment' : 'Restart session'}
          aria-label={`Restart ${session.label}`}
        >
          <RefreshCw className="h-3 w-3" aria-hidden />
        </Button>
        <Button
          type="button"
          variant="ghost"
          size="xs"
          className="h-6 w-6 shrink-0 px-0"
          onClick={e => {
            e.stopPropagation()
            onToggleMaximize()
          }}
          title={maximized ? 'Restore tiled layout' : 'Maximize console'}
          aria-label={maximized ? 'Restore tiled layout' : `Maximize ${session.label}`}
        >
          {maximized ? (
            <Minimize2 className="h-3 w-3" aria-hidden />
          ) : (
            <Maximize2 className="h-3 w-3" aria-hidden />
          )}
        </Button>
        {canUnpin && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-6 w-6 shrink-0 px-0"
            onClick={e => {
              e.stopPropagation()
              onUnpin()
            }}
            title="Remove from consoles"
            aria-label={`Remove ${session.label} from consoles`}
          >
            <X className="h-3 w-3" aria-hidden />
          </Button>
        )}
      </div>
      <div ref={scrollRef} className="console-dock-sessions__pane-log dense-scroll-y">
        {lines != null && lines.length > 0 ? (
          lines.map((line, i) => <SessionLogLine key={i} line={line} index={i} />)
        ) : (
          <span className="text-muted-foreground">No log output</span>
        )}
      </div>
    </div>
  )
}

function SessionStatusRow({
  session,
  active,
  pinned,
  canOperate,
  isActing,
  showClearLogs,
  onSelect,
  onStart,
  onRestart,
  onStop,
  onClearLogs,
}: {
  session: DevSession
  active: boolean
  pinned: boolean
  canOperate: boolean
  isActing: boolean
  showClearLogs: boolean
  onSelect: () => void
  onStart: () => void
  onRestart: () => void
  onStop: () => void
  onClearLogs: () => void
}) {
  const running = session.status === 'running'
  const readyLine =
    session.mode === 'k8s' && session.desired_replicas != null
      ? `${session.ready_replicas ?? 0}/${session.desired_replicas}`
      : null
  return (
    <div
      className={cn(
        'console-dock-sessions__status-row',
        active && 'console-dock-sessions__status-row--active',
        pinned && !active && 'bg-secondary/50',
      )}
    >
      <button
        type="button"
        className="console-dock-sessions__status-main"
        onClick={onSelect}
        title={
          pinned
            ? `Focus console: ${session.label}`
            : `Show console: ${session.label}`
        }
      >
        <StatusLamp value={sessionLamp(session.status)} kind="reach" />
        <div className="min-w-0 flex-1 text-left">
          <div className="flex items-center gap-1.5">
            <span className="truncate text-[var(--text-dense-caption)] font-medium text-foreground">
              {session.label}
            </span>
            <DenseTag
              variant={
                running ? 'success' : session.status === 'error' ? 'warning' : 'neutral'
              }
            >
              {session.status}
            </DenseTag>
            {pinned && (
              <DenseTag variant="info" size="cell">
                console
              </DenseTag>
            )}
          </div>
          <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
            {session.ports != null && session.ports.length > 0
              ? `:${session.ports.join(', :')}`
              : session.name}
            {readyLine != null ? ` · ${readyLine}` : ''}
            {session.image_tag ? ` · ${session.image_tag}` : ''}
            {running ? ` · ${formatUptime(session.uptime_sec)}` : ''}
            {session.status === 'error' && session.crashed
              ? ' · crashed — restart'
              : ''}
            {session.status === 'error' && session.last_exit_code != null
              ? ` · exit ${session.last_exit_code}`
              : ''}
            {session.restarts != null && session.restarts > 0
              ? ` · restarts ${session.restarts}`
              : ''}
            {session.pid != null && session.pid > 0 ? ` · ${session.pid}` : ''}
            {session.log_bytes != null && session.log_bytes > 0 ? (
              <span
                className={cn(
                  logOverCap(session) && 'text-destructive',
                  !logOverCap(session) && logNearCap(session) && 'text-warning',
                )}
              >
                {` · log ${formatLogBytes(session.log_bytes)}`}
                {logOverCap(session) ? '⚠' : ''}
              </span>
            ) : null}
            {session.last_output_at != null && (
              <span
                className={cn(
                  isStale(session.last_output_at) ? 'text-warning' : 'text-muted-foreground',
                )}
                title={`Last output: ${new Date(session.last_output_at * 1000).toLocaleTimeString()}`}
              >
                {` · ${formatLastOutputAgo(session.last_output_at)}`}
              </span>
            )}
          </div>
        </div>
      </button>
      <div className="flex shrink-0 items-center gap-0.5">
        {showClearLogs && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-6 w-6 px-0"
            disabled={!canOperate || isActing}
            onClick={onClearLogs}
            title="Clear logs"
          >
            <Eraser className="h-3 w-3" />
          </Button>
        )}
        {!running ? (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="h-6 w-6 px-0"
            disabled={!canOperate || isActing}
            onClick={onStart}
            title="Start"
          >
            <Play className="h-3 w-3" />
          </Button>
        ) : (
          <>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="h-6 w-6 px-0"
              disabled={!canOperate || isActing}
              onClick={onRestart}
              title="Restart"
            >
              <RotateCw className="h-3 w-3" />
            </Button>
            <Button
              type="button"
              variant="ghost"
              size="xs"
              className="h-6 w-6 px-0"
              disabled={!canOperate || isActing}
              onClick={onStop}
              title="Stop"
            >
              <Square className="h-3 w-3" />
            </Button>
          </>
        )}
      </div>
    </div>
  )
}

/**
 * Operator Dock · Sessions tool — compact Dev Sessions: status rail + per-session consoles.
 * Supports drag-resize between consoles/status and maximize/restore for a single console.
 */
export function DockDevSessionsPanel({
  enabled,
  onOpenPage,
}: {
  enabled: boolean
  onOpenPage?: () => void
}) {
  const qc = useQueryClient()
  const { canOperate } = usePlatformAuth()
  const [activeName, setActiveName] = useState<string>('')
  const [maximizedName, setMaximizedName] = useState<string | null>(null)
  const [pinnedNames, setPinnedNames] = useState<string[]>(readStoredPinned)
  const [paneCap, setPaneCap] = useState<PaneCap>(readStoredPaneCap)
  const [collapsedGroups, setCollapsedGroups] = useState<Set<string>>(readStoredCollapsedGroups)
  const [groupFilter, setGroupFilter] = useState<string>('all')
  const [issuesOnly, setIssuesOnly] = useState(false)
  const [actingOn, setActingOn] = useState<string | null>(null)
  const [reconnecting, setReconnecting] = useState(false)
  const [confirmSelfRestart, setConfirmSelfRestart] = useState<{
    open: boolean
    action: string
    batch?: boolean
  } | null>(null)
  const [leftPct, setLeftPct] = useState(readStoredSplitPct)
  const [collapsedInitialized, setCollapsedInitialized] = useState(false)
  const splitRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ startX: number; startPct: number } | null>(null)
  const leftPctRef = useRef(leftPct)
  const pinnedSeededRef = useRef(false)

  useEffect(() => {
    leftPctRef.current = leftPct
  }, [leftPct])

  const { data: sessions, isLoading, isError } = useQuery({
    queryKey: ['dev-sessions', 'shell'],
    queryFn: fetchDevSessions,
    enabled,
    refetchInterval: enabled ? 10_000 : false,
  })

  const list = useMemo(() => sessions ?? [], [sessions])
  const running = list.filter(s => s.status === 'running').length
  const total = list.length
  const issueCount = list.filter(s => isSessionIssue(s.status)).length
  const clusterMode = isClusterMode(list)
  const envBadge = sessionEnvBadge(list)
  const showClearLogs = !clusterMode
  const platformSessionName =
    list.find(s => isPlatformSession(s.name))?.name ?? 'platform'

  const groupNames = useMemo(() => {
    const seen = new Set<string>()
    const order: string[] = []
    for (const s of list) {
      const g = s.group || 'other'
      if (!seen.has(g)) {
        seen.add(g)
        order.push(g)
      }
    }
    return order
  }, [list])

  const groupOptions = useMemo(
    () => [
      { value: 'all', label: 'All' },
      ...groupNames.map(g => ({ value: g, label: g })),
    ],
    [groupNames],
  )

  useEffect(() => {
    if (groupFilter === 'all') return
    if (!groupNames.includes(groupFilter)) setGroupFilter('all')
  }, [groupNames, groupFilter])

  useEffect(() => {
    if (issuesOnly && issueCount === 0) setIssuesOnly(false)
  }, [issuesOnly, issueCount])

  const filteredList = useMemo(() => {
    let rows = list
    if (groupFilter !== 'all') {
      rows = rows.filter(s => (s.group || 'other') === groupFilter)
    }
    if (issuesOnly) {
      rows = rows.filter(s => isSessionIssue(s.status))
    }
    return rows
  }, [list, groupFilter, issuesOnly])

  // Seed / prune pinned consoles against the live session list and pane cap.
  useEffect(() => {
    if (list.length === 0) return
    const known = new Set(list.map(s => s.name))
    setPinnedNames(prev => {
      let next = prev.filter(n => known.has(n))
      if (!pinnedSeededRef.current) {
        pinnedSeededRef.current = true
        if (next.length === 0) {
          next = list.slice(0, Math.min(paneCap, list.length)).map(s => s.name)
        }
      }
      if (next.length > paneCap) next = next.slice(0, paneCap)
      if (next.join('\0') !== prev.join('\0')) persistPinned(next)
      return next.join('\0') === prev.join('\0') ? prev : next
    })
  }, [list, paneCap])

  // First visit in cluster mode: collapse all-healthy groups (issues stay open).
  useEffect(() => {
    if (collapsedInitialized || list.length === 0 || !clusterMode) return
    try {
      if (localStorage.getItem(COLLAPSED_GROUPS_KEY) != null) {
        setCollapsedInitialized(true)
        return
      }
    } catch {
      /* ignore */
    }
    const next = new Set<string>()
    for (const g of groupNames) {
      const rows = list.filter(s => (s.group || 'other') === g)
      if (rows.length > 0 && rows.every(s => s.status === 'running')) next.add(g)
    }
    setCollapsedGroups(next)
    persistCollapsedGroups(next)
    setCollapsedInitialized(true)
  }, [list, groupNames, clusterMode, collapsedInitialized])

  useEffect(() => {
    if (pinnedNames.length === 0) return
    if (activeName !== '' && pinnedNames.includes(activeName)) return
    setActiveName(pinnedNames[0])
  }, [pinnedNames, activeName])

  useEffect(() => {
    if (maximizedName == null) return
    if (!pinnedNames.includes(maximizedName) && !list.some(s => s.name === maximizedName)) {
      setMaximizedName(null)
    }
  }, [pinnedNames, list, maximizedName])

  const controlMutation = useMutation({
    mutationFn: ({ name, action }: { name: string; action: string }) =>
      controlDevSession(name, action),
    onSuccess: (_data, { name, action }) => {
      setActingOn(null)
      if (isPlatformSession(name) && (action === 'restart' || action === 'stop')) {
        setReconnecting(true)
      } else {
        void qc.invalidateQueries({ queryKey: ['dev-sessions'] })
      }
    },
    onError: () => setActingOn(null),
  })

  const doControl = useCallback(
    (name: string, action: string) => {
      if (isPlatformSession(name) && (action === 'restart' || action === 'stop')) {
        setConfirmSelfRestart({ open: true, action })
        return
      }
      setActingOn(name)
      controlMutation.mutate({ name, action })
    },
    [controlMutation],
  )

  const executeSelfRestart = useCallback(() => {
    if (confirmSelfRestart == null) return
    const { action, batch } = confirmSelfRestart
    setConfirmSelfRestart(null)
    if (batch) {
      for (const s of list) {
        setActingOn(s.name)
        controlMutation.mutate({ name: s.name, action })
      }
    } else {
      setActingOn(platformSessionName)
      controlMutation.mutate({ name: platformSessionName, action })
    }
  }, [confirmSelfRestart, controlMutation, list, platformSessionName])

  const grouped = useMemo(() => {
    const groups: Record<string, DevSession[]> = {}
    for (const s of filteredList) {
      const g = s.group || 'other'
      if (groups[g] == null) groups[g] = []
      groups[g].push(s)
    }
    for (const g of Object.keys(groups)) {
      groups[g].sort(
        (a, b) =>
          statusSortKey(a.status) - statusSortKey(b.status) ||
          a.label.localeCompare(b.label),
      )
    }
    // Preserve catalog group order from full list, then filtered-only leftovers.
    const ordered: [string, DevSession[]][] = []
    const seen = new Set<string>()
    for (const g of groupNames) {
      if (groups[g] != null) {
        ordered.push([g, groups[g]])
        seen.add(g)
      }
    }
    for (const [g, rows] of Object.entries(groups)) {
      if (!seen.has(g)) ordered.push([g, rows])
    }
    return ordered
  }, [filteredList, groupNames])

  const selectSession = useCallback(
    (name: string) => {
      setActiveName(name)
      setMaximizedName(prev => (prev != null ? name : prev))
      setPinnedNames(prev => {
        if (prev.includes(name)) return prev
        let next: string[]
        if (prev.length < paneCap) {
          next = [...prev, name]
        } else {
          // At cap: replace the focused slot (or the last slot).
          const idx = prev.indexOf(activeName)
          next = [...prev]
          next[idx >= 0 ? idx : next.length - 1] = name
        }
        persistPinned(next)
        return next
      })
    },
    [paneCap, activeName],
  )

  const unpinSession = useCallback((name: string) => {
    setPinnedNames(prev => {
      const next = prev.filter(n => n !== name)
      persistPinned(next)
      return next
    })
    setMaximizedName(prev => (prev === name ? null : prev))
  }, [])

  const changePaneCap = useCallback((raw: string) => {
    const n = Number(raw)
    if (!(PANE_CAP_OPTIONS as readonly number[]).includes(n)) return
    const cap = n as PaneCap
    setPaneCap(cap)
    try {
      localStorage.setItem(PANE_CAP_KEY, String(cap))
    } catch {
      /* ignore */
    }
    setPinnedNames(prev => {
      if (prev.length <= cap) return prev
      const next = prev.slice(0, cap)
      persistPinned(next)
      return next
    })
  }, [])

  const toggleGroupCollapsed = useCallback((group: string) => {
    setCollapsedGroups(prev => {
      const next = new Set(prev)
      if (next.has(group)) next.delete(group)
      else next.add(group)
      persistCollapsedGroups(next)
      return next
    })
  }, [])

  const toggleMaximize = useCallback((name: string) => {
    setActiveName(name)
    setMaximizedName(prev => (prev === name ? null : name))
  }, [])

  const onSplitPointerDown = (e: ReactPointerEvent<HTMLDivElement>) => {
    e.preventDefault()
    const narrow =
      typeof window !== 'undefined' && window.matchMedia('(max-width: 52rem)').matches
    dragRef.current = {
      startX: narrow ? e.clientY : e.clientX,
      startPct: leftPctRef.current,
    }
    e.currentTarget.setPointerCapture(e.pointerId)
  }

  const onSplitPointerMove = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current == null || splitRef.current == null) return
    const rect = splitRef.current.getBoundingClientRect()
    const narrow =
      typeof window !== 'undefined' && window.matchMedia('(max-width: 52rem)').matches
    const span = narrow ? rect.height : rect.width
    if (span <= 0) return
    const deltaPct =
      (((narrow ? e.clientY : e.clientX) - dragRef.current.startX) / span) * 100
    const next = Math.min(
      MAX_LEFT_PCT,
      Math.max(MIN_LEFT_PCT, dragRef.current.startPct + deltaPct),
    )
    leftPctRef.current = next
    setLeftPct(next)
  }

  const onSplitPointerUp = (e: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current == null) return
    dragRef.current = null
    try {
      e.currentTarget.releasePointerCapture(e.pointerId)
    } catch {
      /* ignore */
    }
    try {
      localStorage.setItem(SPLIT_KEY, String(leftPctRef.current))
    } catch {
      /* ignore */
    }
  }

  const logPressure = list.filter(logNearCap)
  const logOver = list.filter(logOverCap)

  const verdictLamp: Lamp =
    isLoading || isError
      ? 'unknown'
      : logOver.length > 0
        ? 'degraded'
        : running === total && total > 0
          ? 'ok'
          : running === 0
            ? 'fail'
            : 'degraded'

  const pinnedSet = useMemo(() => new Set(pinnedNames), [pinnedNames])

  // Left consoles = pinned sessions (order preserved); maximize shows one.
  const visiblePanes = useMemo(() => {
    const byName = new Map(list.map(s => [s.name, s]))
    if (maximizedName != null) {
      const max = byName.get(maximizedName)
      return max != null ? [max] : []
    }
    return pinnedNames.map(n => byName.get(n)).filter((s): s is DevSession => s != null)
  }, [maximizedName, pinnedNames, list])

  return (
    <div className="console-dock-sessions min-h-0 flex-1 flex flex-col gap-1.5">
      <div className="console-dock-sessions__verdict flex-wrap gap-y-1">
        <StatusLamp value={verdictLamp} kind="reach" />
        <span className="font-medium text-[var(--text-dense-caption)]">
          {isLoading
            ? 'Loading…'
            : isError
              ? 'Sessions unreachable'
              : `${running}/${total} running`}
        </span>
        {envBadge != null && (
          <DenseTag variant={envBadgeVariant(envBadge)}>{envBadge}</DenseTag>
        )}
        {clusterMode && (
          <span className="text-[var(--text-dense-caption)] text-muted-foreground">cluster</span>
        )}
        {issueCount > 0 && !isLoading && !isError && (
          <button
            type="button"
            className={cn(
              'text-[var(--text-dense-caption)] underline-offset-2 hover:underline',
              issuesOnly ? 'font-medium text-warning' : 'text-muted-foreground',
            )}
            onClick={() => setIssuesOnly(v => !v)}
            title={issuesOnly ? 'Show all sessions' : 'Show non-running sessions only'}
            aria-pressed={issuesOnly}
          >
            {issueCount} stopped
          </button>
        )}
        {groupNames.length > 1 && (
          <SegmentControl
            ariaLabel="Session group filter"
            size="sm"
            value={groupFilter}
            options={groupOptions}
            onChange={setGroupFilter}
          />
        )}
        {logPressure.length > 0 && (
          <span
            className={cn(
              'text-[var(--text-dense-caption)]',
              logOver.length > 0 ? 'text-destructive' : 'text-warning',
            )}
            title="On-disk logs near/over bdev cap — rotation should trim; run bdev log-gc if needed"
          >
            log {logOver.length > 0 ? 'over' : 'near'} cap (
            {logPressure.map(s => s.name).join(', ')})
          </span>
        )}
        {maximizedName != null && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="text-[var(--text-dense-caption)] text-muted-foreground"
            onClick={() => setMaximizedName(null)}
            title="Restore tiled console layout"
          >
            Restore tiles
          </Button>
        )}
        {onOpenPage != null && (
          <Button
            type="button"
            variant="ghost"
            size="xs"
            className="ml-auto text-[var(--text-dense-caption)] text-muted-foreground"
            onClick={onOpenPage}
          >
            Full page
          </Button>
        )}
      </div>

      <div
        ref={splitRef}
        className="console-dock-sessions__split"
        style={{
          gridTemplateColumns: `minmax(0, ${leftPct}fr) 0.35rem minmax(8rem, ${100 - leftPct}fr)`,
          ['--dock-sessions-left' as string]: String(leftPct),
          ['--dock-sessions-right' as string]: String(100 - leftPct),
        }}
      >
        <div
          className={cn(
            'console-dock-sessions__panes dense-scroll-y',
            maximizedName != null && 'console-dock-sessions__panes--maximized',
          )}
          aria-label="Session consoles"
        >
          {isLoading && (
            <p className="console-agent-execution-dock__idle-copy px-1">Loading sessions…</p>
          )}
          {isError && !isLoading && (
            <p className="console-agent-execution-dock__idle-copy console-agent-execution-dock__idle-copy--warn px-1">
              Cannot reach /api/v1/dev-sessions
              {clusterMode
                ? ' — check platform-api kube access.'
                : ' — start platform with bdev.'}
            </p>
          )}
          {!isLoading &&
            !isError &&
            visiblePanes.length === 0 && (
              <p className="console-agent-execution-dock__idle-copy px-1">
                {list.length === 0
                  ? 'No sessions'
                  : 'No consoles open — click a session in Status to show logs'}
              </p>
            )}
          {!isLoading &&
            !isError &&
            visiblePanes.map(s => (
              <SessionConsolePane
                key={s.name}
                session={s}
                active={activeName === s.name}
                maximized={maximizedName === s.name}
                logsEnabled={
                  enabled && (maximizedName == null || maximizedName === s.name)
                }
                canOperate={canOperate}
                isActing={actingOn === s.name || controlMutation.isPending}
                showClearLogs={showClearLogs}
                canUnpin={pinnedNames.length > 0}
                onSelect={() => selectSession(s.name)}
                onToggleMaximize={() => toggleMaximize(s.name)}
                onClearLogs={() => doControl(s.name, 'clear-logs')}
                onReload={() => doControl(s.name, 'restart')}
                onUnpin={() => unpinSession(s.name)}
              />
            ))}
        </div>

        <div
          className="console-dock-sessions__v-resize"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize consoles and status"
          title="Drag to resize consoles / status"
          onPointerDown={onSplitPointerDown}
          onPointerMove={onSplitPointerMove}
          onPointerUp={onSplitPointerUp}
        />

        <aside className="console-dock-sessions__status" aria-label="Session status">
          <div className="console-agent-execution-dock__recent-head">
            <h3 className="console-agent-execution-dock__recent-title">Status</h3>
            <label
              className="ml-auto flex items-center gap-1 text-[10px] text-muted-foreground"
              title="Max consoles open on the left"
            >
              <span className="shrink-0">Slots</span>
              <select
                className="h-5 max-w-[3.25rem] rounded border border-border bg-background px-0.5 font-mono text-[10px] text-foreground"
                value={paneCap}
                onChange={e => changePaneCap(e.target.value)}
                aria-label="Max console slots"
              >
                {PANE_CAP_OPTIONS.map(n => (
                  <option key={n} value={n}>
                    {n}
                  </option>
                ))}
              </select>
            </label>
            <div className="flex items-center gap-0.5">
              {showClearLogs && (
                <Button
                  type="button"
                  variant="ghost"
                  size="xs"
                  className="h-5 px-1.5 text-[10px] text-muted-foreground"
                  disabled={!canOperate || controlMutation.isPending || list.length === 0}
                  onClick={() => {
                    for (const s of list) doControl(s.name, 'clear-logs')
                  }}
                  title="Clear all session logs"
                >
                  <Eraser className="mr-0.5 h-3 w-3" />
                  Clear all
                </Button>
              )}
              <Button
                type="button"
                variant="ghost"
                size="xs"
                className="h-5 px-1.5 text-[10px] text-muted-foreground"
                disabled={!canOperate || controlMutation.isPending || list.length === 0}
                onClick={() => {
                  setConfirmSelfRestart({ open: true, action: 'restart', batch: true })
                }}
                title={clusterMode ? 'Rollout restart all Deployments' : 'Restart all sessions'}
              >
                <RotateCw className="mr-0.5 h-3 w-3" />
                Restart all
              </Button>
            </div>
          </div>
          <div className="px-1 pb-0.5 font-mono text-[10px] text-muted-foreground">
            {pinnedNames.length}/{paneCap} consoles
            {paneCap >= MAX_PANE_CAP ? ' · max' : ''}
          </div>
          {list.length === 0 && !isLoading && (
            <p className="console-agent-execution-dock__recent-empty">No sessions</p>
          )}
          {list.length > 0 && filteredList.length === 0 && !isLoading && (
            <p className="console-agent-execution-dock__recent-empty">No sessions match filters</p>
          )}
          {grouped.map(([group, rows]) => {
            const collapsed = collapsedGroups.has(group)
            const groupIssues = rows.filter(s => isSessionIssue(s.status)).length
            return (
              <div key={group} className="console-dock-sessions__group">
                <button
                  type="button"
                  className="console-dock-sessions__group-label flex w-full items-center gap-1 text-left"
                  onClick={() => toggleGroupCollapsed(group)}
                  aria-expanded={!collapsed}
                  title={collapsed ? `Expand ${group}` : `Collapse ${group}`}
                >
                  {collapsed ? (
                    <ChevronRight className="h-3 w-3 shrink-0 opacity-70" aria-hidden />
                  ) : (
                    <ChevronDown className="h-3 w-3 shrink-0 opacity-70" aria-hidden />
                  )}
                  <span className="min-w-0 flex-1 truncate">{group.toUpperCase()}</span>
                  <span className="shrink-0 font-mono text-[10px] font-normal text-muted-foreground">
                    {groupIssues > 0 ? `${groupIssues}! · ${rows.length}` : rows.length}
                  </span>
                </button>
                {!collapsed &&
                  rows.map(s => (
                    <SessionStatusRow
                      key={s.name}
                      session={s}
                      active={activeName === s.name}
                      pinned={pinnedSet.has(s.name)}
                      canOperate={canOperate}
                      isActing={actingOn === s.name || controlMutation.isPending}
                      showClearLogs={showClearLogs}
                      onSelect={() => selectSession(s.name)}
                      onStart={() => doControl(s.name, 'start')}
                      onRestart={() => doControl(s.name, 'restart')}
                      onStop={() => doControl(s.name, 'stop')}
                      onClearLogs={() => doControl(s.name, 'clear-logs')}
                    />
                  ))}
              </div>
            )
          })}
        </aside>
      </div>
      <ConfirmDialog
        open={confirmSelfRestart?.open ?? false}
        title={
          confirmSelfRestart?.batch
            ? clusterMode
              ? 'Restart all Deployments'
              : 'Restart all sessions'
            : 'Restart Platform'
        }
        message={
          confirmSelfRestart?.batch
            ? clusterMode
              ? 'This will rollout-restart all catalog Deployments, including Platform API (hosting this Console). The UI will auto-reconnect when ready.'
              : 'This will restart all sessions including Platform (hosting this Console). The UI will auto-reconnect when ready.'
            : clusterMode
              ? 'This will rollout-restart the Platform API Deployment hosting this Console. The UI will auto-reconnect when ready.'
              : 'This will restart the service hosting this Console. The UI will auto-reconnect when ready.'
        }
        confirmLabel={confirmSelfRestart?.batch ? 'Restart all' : 'Restart'}
        confirming={controlMutation.isPending}
        onConfirm={executeSelfRestart}
        onCancel={() => setConfirmSelfRestart(null)}
      />
      {reconnecting && (
        <ReconnectingOverlay onCancel={() => window.location.reload()} />
      )}
    </div>
  )
}
