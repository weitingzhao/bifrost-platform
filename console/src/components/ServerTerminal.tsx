import { useCallback, useEffect, useMemo, useState } from 'react'
import { Button, SegmentControl, cn } from '@bifrost/ui'
import { X } from 'lucide-react'
import type { ConsoleHost } from '@/api/console'
import { ConsoleHostBrandIcon } from '@/components/ConsoleHostBrandIcon'
import { ConsoleHostSegmentLabel } from '@/components/ConsoleHostSegmentLabel'
import { SshSessionPane, type SshConnState } from '@/components/SshSessionPane'
import {
  resolveMacAgentRole,
  type MacAgentHostRole,
} from '@/lib/agent/macHostRole'

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

function gridColsClass(count: number): string {
  if (count <= 1) return 'grid-cols-1'
  if (count === 2) return 'grid-cols-1 lg:grid-cols-2'
  if (count <= 4) return 'grid-cols-1 md:grid-cols-2'
  return 'grid-cols-1 md:grid-cols-2 xl:grid-cols-3'
}

function paneMinHeightClass(count: number, density: 'page' | 'dock'): string {
  if (density === 'dock') {
    if (count <= 2) return 'min-h-[180px]'
    return 'min-h-[140px]'
  }
  if (count <= 2) return 'min-h-[420px]'
  if (count <= 4) return 'min-h-[320px]'
  return 'min-h-[260px]'
}

function isLinuxConsoleHost(host: ConsoleHost): boolean {
  return host.group === 'linux' || host.group === 'compute'
}

function isMacConsoleHost(host: ConsoleHost): boolean {
  return host.group === 'mac'
}

function hostSegmentOptions(
  hosts: ConsoleHost[],
  k8sNodeByIp?: Record<string, string>,
  agentRoleByHost?: Record<string, MacAgentHostRole>,
  dense = false,
) {
  return hosts.map(h => ({
    value: h.id,
    label: (
      <ConsoleHostSegmentLabel
        host={h}
        k8sNodeByIp={k8sNodeByIp}
        agentRole={resolveMacAgentRole(h.host, agentRoleByHost)}
        dense={dense}
      />
    ),
  }))
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

  const linuxHosts = useMemo(() => hosts.filter(isLinuxConsoleHost), [hosts])
  const macHosts = useMemo(() => hosts.filter(isMacConsoleHost), [hosts])

  const pickerHost = hosts.find(h => h.id === selectedId) ?? hosts[0] ?? null

  useEffect(() => {
    if (hosts.length > 0 && selectedId == null) {
      onSelectHost(hosts[0].id)
    }
  }, [hosts, selectedId, onSelectHost])

  const ensureSession = useCallback((hostId: string) => {
    setTabs(prev => {
      if (prev.some(t => t.hostId === hostId)) return prev
      return [
        ...prev,
        {
          id: crypto.randomUUID(),
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
  }, [pickerHost?.id, ensureSession])

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
  const dock = density === 'dock'

  const linuxValue = linuxHosts.some(h => h.id === pickerHost?.id) ? pickerHost!.id : ''
  const macValue = macHosts.some(h => h.id === pickerHost?.id) ? pickerHost!.id : ''

  const statusMeta =
    hosts.length > 0 ? (
      <div
        className={cn(
          'flex shrink-0 items-center gap-2 text-[var(--text-dense-meta)]',
          dock ? 'flex-row' : 'flex-col items-end gap-1',
        )}
      >
        {pickerHost != null && (
          <span
            className={
              pickerHost.reachable
                ? 'lamp-ok text-[var(--text-dense-meta)]'
                : 'lamp-fail text-[var(--text-dense-meta)]'
            }
          >
            SSH {pickerHost.reachable ? 'ok' : 'down'}
          </span>
        )}
        {tabs.length > 0 && (
          <span className="text-muted-foreground">
            {liveCount}/{tabs.length} live
          </span>
        )}
      </div>
    ) : null

  return (
    <section
      className={cn(
        'server-console overflow-hidden flex flex-col',
        dock
          ? 'min-h-0 flex-1 border-0 bg-transparent shadow-none'
          : 'panel-elevated',
      )}
    >
      <div
        className={cn(
          'server-console-toolbar border-b border-[var(--border)]',
          dock
            ? 'flex flex-wrap items-center gap-x-3 gap-y-1 px-2 py-1'
            : 'flex flex-col gap-2 bg-[var(--color-surface-elevated)] px-3 py-2',
        )}
      >
        {hosts.length > 0 ? (
          <>
            <div
              className={cn(
                'flex min-w-0 flex-1',
                dock
                  ? 'flex-wrap items-center gap-x-3 gap-y-1'
                  : 'flex-col gap-2',
              )}
            >
              {linuxHosts.length > 0 && (
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className="env-strip-label shrink-0">Linux</span>
                  <div className={cn('min-w-0', !dock && 'overflow-x-auto dense-scroll-x')}>
                    <SegmentControl
                      ariaLabel="SSH host — Linux K3s cluster"
                      value={linuxValue}
                      onChange={handleHostChange}
                      options={hostSegmentOptions(linuxHosts, k8sNodeByIp, agentRoleByHost, dock)}
                      size={dock ? 'xs' : 'sm'}
                      className={dock ? 'max-w-full flex-wrap' : undefined}
                    />
                  </div>
                </div>
              )}
              {macHosts.length > 0 && (
                <div className="flex min-w-0 items-center gap-1.5">
                  <span className="env-strip-label shrink-0">Mac</span>
                  <div className={cn('min-w-0', !dock && 'overflow-x-auto dense-scroll-x')}>
                    <SegmentControl
                      ariaLabel="SSH host — Mac Agent hosts"
                      value={macValue}
                      onChange={handleHostChange}
                      options={hostSegmentOptions(macHosts, k8sNodeByIp, agentRoleByHost, dock)}
                      size={dock ? 'xs' : 'sm'}
                      className={dock ? 'max-w-full flex-wrap' : undefined}
                    />
                  </div>
                </div>
              )}
            </div>
            {statusMeta}
          </>
        ) : (
          <span className="text-muted-foreground text-[var(--text-dense-meta)]">
            No SSH hosts in topology (set node host in topology.yaml)
          </span>
        )}
      </div>

      {tabs.length === 0 ? (
        <div
          className={cn(
            'flex items-center justify-center bg-[#0a0c0f] text-center text-[var(--text-dense-meta)] text-muted-foreground',
            dock ? 'min-h-0 flex-1 px-3' : 'min-h-[420px] px-6',
          )}
        >
          {dock
            ? 'Select a host to connect.'
            : 'Select a host above to connect. Linux row = K3s cluster nodes; Mac row = Agent hosts (native macOS).'}
        </div>
      ) : (
        <div
          className={cn(
            'grid gap-px bg-[var(--border)]',
            dock && 'min-h-0 flex-1',
            gridColsClass(tabs.length),
          )}
        >
          {tabs.map(tab => {
            const host = hosts.find(h => h.id === tab.hostId)
            if (!host) return null
            return (
              <div
                key={tab.id}
                className={cn(
                  'flex min-w-0 flex-col bg-[#0a0c0f]',
                  paneMinHeightClass(tabs.length, density),
                  dock && 'min-h-0 flex-1',
                )}
              >
                <div
                  className={cn(
                    'flex items-center gap-1.5 border-b border-[var(--border)] bg-[var(--color-surface-elevated)]',
                    dock ? 'px-1.5 py-0.5' : 'px-2 py-1 gap-2',
                  )}
                >
                  <ConsoleHostBrandIcon host={host} className="size-3.5 shrink-0" />
                  <span
                    className={cn(
                      'min-w-0 flex-1 truncate font-semibold text-foreground',
                      dock ? 'text-dense-meta' : 'text-dense-body',
                    )}
                    title={host.label}
                  >
                    <ConsoleHostSegmentLabel
                      host={host}
                      k8sNodeByIp={k8sNodeByIp}
                      agentRole={resolveMacAgentRole(host.host, agentRoleByHost)}
                      dense={dock}
                    />
                  </span>
                  {tab.connState === 'open' && (
                    <span className="inline-flex items-center gap-1 text-dense-meta text-muted-foreground">
                      <span className="server-console-live-dot" /> Live
                    </span>
                  )}
                  {tab.connState === 'connecting' && (
                    <span className="text-dense-meta text-muted-foreground">Connecting…</span>
                  )}
                  {tab.connState === 'error' && (
                    <>
                      <span className="max-w-[8rem] truncate text-dense-meta text-destructive">
                        {tab.error ?? 'Failed'}
                      </span>
                      <Button
                        size="sm"
                        className="shrink-0 px-2 py-0.5 text-dense-meta"
                        onClick={() => retrySession(tab.id)}
                      >
                        Retry
                      </Button>
                    </>
                  )}
                  <button
                    type="button"
                    className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                    aria-label={`Close ${host.host} session`}
                    onClick={() => closeTab(tab.id)}
                  >
                    <X className="size-3.5" />
                  </button>
                </div>
                <div className="relative min-h-0 flex-1">
                  <SshSessionPane
                    key={`${tab.id}-${tab.connectAttempt}`}
                    host={host}
                    onConnectionChange={(state, error) => updateTabState(tab.id, state, error ?? null)}
                  />
                </div>
              </div>
            )
          })}
        </div>
      )}
    </section>
  )
}
