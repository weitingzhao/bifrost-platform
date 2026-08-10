import {
  useCallback,
  useEffect,
  useMemo,
  useRef,
  useState,
  type PointerEvent as ReactPointerEvent,
} from 'react'
import { Button, cn } from '@bifrost/ui'
import { X } from 'lucide-react'
import type { ConsoleHost } from '@/api/console'
import { ConsoleHostBrandIcon } from '@/components/ConsoleHostBrandIcon'
import { SshSessionPane, type SshConnState } from '@/components/SshSessionPane'
import {
  resolveMacAgentRole,
  macAgentRoleLabel,
  type MacAgentHostRole,
} from '@/lib/agent/macHostRole'
import { randomId } from '@/lib/randomId'

export type ServerTerminalProps = {
  hosts: ConsoleHost[]
  selectedId: string | null
  onSelectHost: (id: string) => void
  k8sNodeByIp?: Record<string, string>
  /** Bridge-derived L-1 roles keyed by host IP — only runner hosts. */
  agentRoleByHost?: Record<string, MacAgentHostRole>
  /** Page uses taller panes; dock uses compact flex-fill panes. */
  density?: 'page' | 'dock'
}

type SessionTab = {
  id: string
  hostId: string
  connState: SshConnState
  error: string | null
  connectAttempt: number
}

const SPLIT_KEY = 'bifrost.console.serverTerminalRailPct.v1'
const DEFAULT_RAIL_PCT = 23
const MIN_RAIL_PCT = 16
const MAX_RAIL_PCT = 38

function readStoredRailPct(): number {
  try {
    const value = Number(localStorage.getItem(SPLIT_KEY))
    if (!Number.isFinite(value)) return DEFAULT_RAIL_PCT
    return Math.min(MAX_RAIL_PCT, Math.max(MIN_RAIL_PCT, value))
  } catch {
    return DEFAULT_RAIL_PCT
  }
}

function isLinuxConsoleHost(host: ConsoleHost): boolean {
  return host.group === 'linux' || host.group === 'compute'
}

function isMacConsoleHost(host: ConsoleHost): boolean {
  return host.group === 'mac'
}

function hostRailLabel(
  host: ConsoleHost,
  k8sNodeByIp?: Record<string, string>,
  agentRoleByHost?: Record<string, MacAgentHostRole>,
) {
  const k8sName = k8sNodeByIp?.[host.host]
  const agentRole = resolveMacAgentRole(host.host, agentRoleByHost)
  if (k8sName != null) return k8sName
  if (agentRole != null) return macAgentRoleLabel(agentRole)
  return host.label || host.host
}

export function ServerTerminal({
  hosts,
  selectedId,
  onSelectHost,
  k8sNodeByIp,
  agentRoleByHost,
  density = 'page',
}: ServerTerminalProps) {
  const [tabs, setTabs] = useState<SessionTab[]>([])
  const [railPct, setRailPct] = useState(readStoredRailPct)
  const splitRef = useRef<HTMLDivElement>(null)
  const dragRef = useRef<{ start: number; startPct: number } | null>(null)
  const railPctRef = useRef(railPct)

  const linuxHosts = useMemo(() => hosts.filter(isLinuxConsoleHost), [hosts])
  const macHosts = useMemo(() => hosts.filter(isMacConsoleHost), [hosts])

  const pickerHost = hosts.find(h => h.id === selectedId) ?? hosts[0] ?? null

  useEffect(() => {
    if (hosts.length > 0 && selectedId == null) {
      onSelectHost(hosts[0].id)
    }
  }, [hosts, selectedId, onSelectHost])

  useEffect(() => {
    railPctRef.current = railPct
  }, [railPct])

  const ensureSession = useCallback((hostId: string) => {
    setTabs(prev => {
      if (prev.some(t => t.hostId === hostId)) return prev
      return [
        ...prev,
        {
          id: randomId(),
          hostId,
          connState: 'connecting',
          error: null,
          connectAttempt: 0,
        },
      ]
    })
  }, [])

  const handleHostChange = useCallback(
    (hostId: string) => {
      onSelectHost(hostId)
      ensureSession(hostId)
    },
    [onSelectHost, ensureSession],
  )

  useEffect(() => {
    if (pickerHost) {
      ensureSession(pickerHost.id)
    }
  }, [pickerHost, ensureSession])

  const closeTab = useCallback((tabId: string) => {
    setTabs(prev => prev.filter(t => t.id !== tabId))
  }, [])

  const retrySession = useCallback((tabId: string) => {
    setTabs(prev =>
      prev.map(t =>
        t.id === tabId
          ? { ...t, connState: 'connecting', error: null, connectAttempt: t.connectAttempt + 1 }
          : t,
      ),
    )
  }, [])

  const updateTabState = useCallback((tabId: string, connState: SshConnState, error: string | null = null) => {
    setTabs(prev => prev.map(t => (t.id === tabId ? { ...t, connState, error } : t)))
  }, [])

  const liveCount = tabs.filter(t => t.connState === 'open').length
  const visibleTab = tabs.find(tab => tab.hostId === pickerHost?.id) ?? null
  const reachableCount = hosts.filter(host => host.reachable).length
  const railGroups = [
    { label: 'Linux', hosts: linuxHosts },
    { label: 'Mac', hosts: macHosts },
  ].filter(group => group.hosts.length > 0)

  const onSplitPointerDown = (event: ReactPointerEvent<HTMLDivElement>) => {
    event.preventDefault()
    const narrow = window.matchMedia('(max-width: 52rem)').matches
    dragRef.current = {
      start: narrow ? event.clientY : event.clientX,
      startPct: railPctRef.current,
    }
    event.currentTarget.setPointerCapture(event.pointerId)
  }

  const onSplitPointerMove = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current == null || splitRef.current == null) return
    const narrow = window.matchMedia('(max-width: 52rem)').matches
    const rect = splitRef.current.getBoundingClientRect()
    const span = narrow ? rect.height : rect.width
    if (span <= 0) return
    const delta = ((narrow ? event.clientY : event.clientX) - dragRef.current.start) / span
    const next = Math.min(
      MAX_RAIL_PCT,
      Math.max(MIN_RAIL_PCT, dragRef.current.startPct + delta * 100),
    )
    railPctRef.current = next
    setRailPct(next)
  }

  const onSplitPointerUp = (event: ReactPointerEvent<HTMLDivElement>) => {
    if (dragRef.current == null) return
    dragRef.current = null
    try {
      event.currentTarget.releasePointerCapture(event.pointerId)
      localStorage.setItem(SPLIT_KEY, String(railPctRef.current))
    } catch {
      // localStorage and pointer capture may be unavailable in embedded previews.
    }
  }

  return (
    <section
      className={cn(
        'server-console flex min-h-0 flex-1 flex-col overflow-hidden',
        density === 'dock'
          ? 'border-0 bg-transparent shadow-none'
          : 'panel-elevated',
      )}
    >
      <div
        ref={splitRef}
        className="server-console__split"
        style={{
          gridTemplateColumns: `minmax(9.5rem, ${railPct}fr) 0.35rem minmax(0, ${100 - railPct}fr)`,
          ['--server-console-rail' as string]: String(railPct),
          ['--server-console-terminal' as string]: String(100 - railPct),
        }}
      >
        <aside className="server-console__rail dense-scroll-y" aria-label="SSH hosts">
          <div className="server-console__rail-summary">
            <span>Hosts</span>
            <span className="text-muted-foreground">{reachableCount}/{hosts.length} reachable</span>
          </div>
          {railGroups.map(group => (
            <div key={group.label} className="server-console__rail-group">
              <div className="server-console__rail-group-label">{group.label}</div>
              {group.hosts.map(host => {
                const active = host.id === pickerHost?.id
                return (
                  <button
                    key={host.id}
                    type="button"
                    className={cn('server-console__host-row', active && 'server-console__host-row--active')}
                    onClick={() => handleHostChange(host.id)}
                    title={`${host.label} · ${host.host}${host.reachable ? ' · SSH reachable' : ' · SSH unreachable'}`}
                    aria-pressed={active}
                  >
                    <ConsoleHostBrandIcon host={host} className="size-3.5 shrink-0" />
                    <span className="min-w-0 flex-1 truncate text-left">
                      {hostRailLabel(host, k8sNodeByIp, agentRoleByHost)}
                    </span>
                    <span
                      className={cn(
                        'server-console__reach',
                        host.reachable
                          ? 'server-console__reach--ok'
                          : 'server-console__reach--down',
                      )}
                      aria-label={host.reachable ? 'SSH reachable' : 'SSH unreachable'}
                    />
                  </button>
                )
              })}
            </div>
          ))}
          {hosts.length === 0 && (
            <p className="px-2 py-3 text-[var(--text-dense-meta)] text-muted-foreground">
              No SSH hosts in topology.
            </p>
          )}
        </aside>

        <div
          className="server-console__resize"
          role="separator"
          aria-orientation="vertical"
          aria-label="Resize host rail and terminal"
          title="Drag to resize host rail"
          onPointerDown={onSplitPointerDown}
          onPointerMove={onSplitPointerMove}
          onPointerUp={onSplitPointerUp}
        />

        <div className="server-console__terminal">
          {pickerHost == null || visibleTab == null ? (
            <div className="flex min-h-0 flex-1 items-center justify-center bg-[#0a0c0f] px-3 text-center text-[var(--text-dense-meta)] text-muted-foreground">
              {pickerHost == null ? 'Select a host to connect.' : 'Opening SSH session…'}
            </div>
          ) : (
            <>
              <div className="server-console__terminal-head">
                <ConsoleHostBrandIcon host={pickerHost} className="size-3.5 shrink-0" />
                <span className="min-w-0 flex-1 truncate font-semibold text-foreground">
                  {hostRailLabel(pickerHost, k8sNodeByIp, agentRoleByHost)}
                </span>
                {visibleTab.connState === 'open' && (
                  <span className="inline-flex items-center gap-1 text-dense-meta text-muted-foreground">
                    <span className="server-console-live-dot" /> Live
                  </span>
                )}
                {visibleTab.connState === 'connecting' && (
                  <span className="text-dense-meta text-muted-foreground">Connecting…</span>
                )}
                {visibleTab.connState === 'error' && (
                  <>
                    <span className="max-w-[10rem] truncate text-dense-meta text-destructive">
                      {visibleTab.error ?? 'Failed'}
                    </span>
                    <Button size="sm" className="h-6 px-2 text-dense-meta" onClick={() => retrySession(visibleTab.id)}>
                      Retry
                    </Button>
                  </>
                )}
                <button
                  type="button"
                  className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                  aria-label={`Close ${pickerHost.host} session`}
                  onClick={() => closeTab(visibleTab.id)}
                >
                  <X className="size-3.5" />
                </button>
              </div>
              <div className="server-console__terminal-body">
                {tabs.map(tab => {
                  const host = hosts.find(candidate => candidate.id === tab.hostId)
                  if (host == null) return null
                  return (
                    <div key={tab.id} className={cn('server-console__session', tab.id !== visibleTab.id && 'hidden')}>
                      <SshSessionPane
                        key={`${tab.id}-${tab.connectAttempt}`}
                        host={host}
                        active={tab.id === visibleTab.id}
                        onConnectionChange={(state, error) => updateTabState(tab.id, state, error ?? null)}
                      />
                    </div>
                  )
                })}
              </div>
            </>
          )}
        </div>
      </div>
      <div className="server-console__meta">
        {pickerHost != null && (
          <span className={pickerHost.reachable ? 'lamp-ok' : 'lamp-fail'}>
            SSH {pickerHost.reachable ? 'reachable' : 'unreachable'}
          </span>
        )}
        {tabs.length > 0 && <span>{liveCount}/{tabs.length} sessions live</span>}
      </div>
    </section>
  )
}
