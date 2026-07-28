import { useCallback, useEffect, useMemo, useRef, useState, type PointerEvent as ReactPointerEvent } from 'react'
import { useMutation, useQuery, useQueryClient } from '@tanstack/react-query'
import { Button, DenseTag, StatusLamp, cn } from '@bifrost/ui'
import { Maximize2, Minimize2, Play, RotateCw, Square } from 'lucide-react'
import {
  controlDevSession,
  fetchDevSessionLogs,
  fetchDevSessions,
  type DevSession,
} from '@/api/devSession'
import { usePlatformAuth } from '@/hooks/usePlatformAuth'
import {
  DOCK_SESSION_LOG_LINES_MAXIMIZED,
  DOCK_SESSION_LOG_LINES_TILED,
  SessionLogLine,
} from '@/components/agent/sessionLogColor'

type Lamp = 'ok' | 'fail' | 'degraded' | 'unknown'

const SPLIT_KEY = 'bifrost.console.dockSessionsSplitPct.v2'
const DEFAULT_LEFT_PCT = 75
const MIN_LEFT_PCT = 55
const MAX_LEFT_PCT = 88

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

function logOverCap(s: DevSession): boolean {
  const max = s.log_max_bytes ?? 5 * 1024 * 1024
  return (s.log_bytes ?? 0) > max
}

function logNearCap(s: DevSession): boolean {
  const max = s.log_max_bytes ?? 5 * 1024 * 1024
  const bytes = s.log_bytes ?? 0
  return bytes > max * 0.8
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

function SessionConsolePane({
  session,
  active,
  maximized,
  logsEnabled,
  onSelect,
  onToggleMaximize,
}: {
  session: DevSession
  active: boolean
  maximized: boolean
  /** False when dock collapsed or Sessions tool hidden — stops log polling. */
  logsEnabled: boolean
  onSelect: () => void
  onToggleMaximize: () => void
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
        </button>
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
  canOperate,
  isActing,
  onSelect,
  onStart,
  onRestart,
  onStop,
}: {
  session: DevSession
  active: boolean
  canOperate: boolean
  isActing: boolean
  onSelect: () => void
  onStart: () => void
  onRestart: () => void
  onStop: () => void
}) {
  const running = session.status === 'running'
  return (
    <div
      className={cn(
        'console-dock-sessions__status-row',
        active && 'console-dock-sessions__status-row--active',
      )}
    >
      <button
        type="button"
        className="console-dock-sessions__status-main"
        onClick={onSelect}
        title={`Show console: ${session.label}`}
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
          </div>
          <div className="mt-0.5 truncate font-mono text-[10px] text-muted-foreground">
            {session.ports != null && session.ports.length > 0
              ? `:${session.ports.join(', :')}`
              : session.name}
            {running ? ` · ${formatUptime(session.uptime_sec)}` : ''}
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
          </div>
        </div>
      </button>
      <div className="flex shrink-0 items-center gap-0.5">
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
  const [actingOn, setActingOn] = useState<string | null>(null)
  const [leftPct, setLeftPct] = useState(readStoredSplitPct)
  const splitRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ startX: number; startPct: number } | null>(null)
  const leftPctRef = useRef(leftPct)

  useEffect(() => {
    leftPctRef.current = leftPct
  }, [leftPct])

  const { data: sessions, isLoading, isError } = useQuery({
    queryKey: ['dev-sessions', 'shell'],
    queryFn: fetchDevSessions,
    enabled,
    refetchInterval: enabled ? 10_000 : false,
  })

  const list = sessions ?? []
  const running = list.filter(s => s.status === 'running').length
  const total = list.length

  useEffect(() => {
    if (list.length === 0) return
    if (activeName !== '' && list.some(s => s.name === activeName)) return
    setActiveName(list[0].name)
  }, [list, activeName])

  useEffect(() => {
    if (maximizedName == null) return
    if (!list.some(s => s.name === maximizedName)) setMaximizedName(null)
  }, [list, maximizedName])

  const controlMutation = useMutation({
    mutationFn: ({ name, action }: { name: string; action: string }) =>
      controlDevSession(name, action),
    onSuccess: () => {
      setActingOn(null)
      void qc.invalidateQueries({ queryKey: ['dev-sessions'] })
    },
    onError: () => setActingOn(null),
  })

  const grouped = useMemo(() => {
    const groups: Record<string, DevSession[]> = {}
    for (const s of list) {
      const g = s.group || 'other'
      if (groups[g] == null) groups[g] = []
      groups[g].push(s)
    }
    return groups
  }, [list])

  const selectSession = useCallback((name: string) => {
    setActiveName(name)
    setMaximizedName(prev => (prev != null ? name : prev))
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

  const visiblePanes =
    maximizedName != null ? list.filter(s => s.name === maximizedName) : list

  return (
    <div className="console-dock-sessions min-h-0 flex-1 flex flex-col gap-1.5">
      <div className="console-dock-sessions__verdict">
        <StatusLamp value={verdictLamp} kind="reach" />
        <span className="font-medium text-[var(--text-dense-caption)]">
          {isLoading
            ? 'Loading…'
            : isError
              ? 'Sessions unreachable'
              : `${running}/${total} running`}
        </span>
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
              Cannot reach /api/v1/dev-sessions — start platform with bdev.
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
                  enabled &&
                  (maximizedName == null || maximizedName === s.name)
                }
                onSelect={() => selectSession(s.name)}
                onToggleMaximize={() => toggleMaximize(s.name)}
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
          </div>
          {list.length === 0 && !isLoading && (
            <p className="console-agent-execution-dock__recent-empty">No sessions</p>
          )}
          {Object.entries(grouped).map(([group, rows]) => (
            <div key={group} className="console-dock-sessions__group">
              <div className="console-dock-sessions__group-label">{group.toUpperCase()}</div>
              {rows.map(s => (
                <SessionStatusRow
                  key={s.name}
                  session={s}
                  active={activeName === s.name}
                  canOperate={canOperate}
                  isActing={actingOn === s.name || controlMutation.isPending}
                  onSelect={() => selectSession(s.name)}
                  onStart={() => {
                    setActingOn(s.name)
                    controlMutation.mutate({ name: s.name, action: 'start' })
                  }}
                  onRestart={() => {
                    setActingOn(s.name)
                    controlMutation.mutate({ name: s.name, action: 'restart' })
                  }}
                  onStop={() => {
                    setActingOn(s.name)
                    controlMutation.mutate({ name: s.name, action: 'stop' })
                  }}
                />
              ))}
            </div>
          ))}
        </aside>
      </div>
    </div>
  )
}
