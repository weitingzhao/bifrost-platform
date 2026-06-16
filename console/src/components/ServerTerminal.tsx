import { useCallback, useEffect, useState } from 'react'
import { Button, SegmentControl, cn } from '@bifrost/ui'
import { X } from 'lucide-react'
import type { ConsoleHost } from '@/api/console'
import { ConsoleHostBrandIcon } from '@/components/ConsoleHostBrandIcon'
import { SshSessionPane, type SshConnState } from '@/components/SshSessionPane'

export type ServerTerminalProps = {
  hosts: ConsoleHost[]
  selectedId: string | null
  onSelectHost: (id: string) => void
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

function paneMinHeightClass(count: number): string {
  if (count <= 2) return 'min-h-[420px]'
  if (count <= 4) return 'min-h-[320px]'
  return 'min-h-[260px]'
}

export function ServerTerminal({ hosts, selectedId, onSelectHost }: ServerTerminalProps) {
  const [tabs, setTabs] = useState<SessionTab[]>([])

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

  const hostOptions = hosts.map(h => ({
    value: h.id,
    label: (
      <span className="inline-flex items-center gap-1.5">
        <ConsoleHostBrandIcon host={h} />
        <span>
          {h.label} · {h.host}
          {h.jump_label ? ` · via ${h.jump_label}` : ''}
        </span>
      </span>
    ),
  }))

  return (
    <section className="server-console panel-elevated overflow-hidden flex flex-col">
      <div className="server-console-toolbar flex flex-wrap items-center gap-2 px-3 py-2 border-b border-[var(--border)] bg-[var(--color-surface-elevated)]">
        <span className="env-strip-label">Host</span>
        {hosts.length > 0 ? (
          <>
            <SegmentControl
              ariaLabel="SSH host"
              value={pickerHost?.id ?? ''}
              onChange={handleHostChange}
              options={hostOptions}
              size="sm"
            />
            {pickerHost != null && (
              <span
                className={
                  pickerHost.reachable
                    ? 'lamp-ok text-[var(--text-dense-meta)]'
                    : 'lamp-fail text-[var(--text-dense-meta)]'
                }
              >
                SSH {pickerHost.reachable ? 'reachable' : 'unreachable'}
              </span>
            )}
          </>
        ) : (
          <span className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)]">
            No SSH hosts in topology (set node host in topology.yaml)
          </span>
        )}
        <div className="ml-auto flex items-center gap-2 text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
          {tabs.length > 0 && (
            <span>
              {liveCount} live · {tabs.length} pane{tabs.length === 1 ? '' : 's'}
            </span>
          )}
        </div>
      </div>

      {tabs.length === 0 ? (
        <div className="flex min-h-[420px] items-center justify-center bg-[#0a0c0f] px-6 text-center text-[var(--text-dense-meta)] text-muted-foreground">
          Select a host above to connect. Choose several hosts to compare consoles side by side.
        </div>
      ) : (
        <div className={cn('grid gap-px bg-[var(--border)]', gridColsClass(tabs.length))}>
          {tabs.map(tab => {
            const host = hosts.find(h => h.id === tab.hostId)
            if (!host) return null
            return (
              <div
                key={tab.id}
                className={cn(
                  'flex min-w-0 flex-col bg-[#0a0c0f]',
                  paneMinHeightClass(tabs.length),
                )}
              >
                <div className="flex items-center gap-2 border-b border-[var(--border)] bg-[var(--color-surface-elevated)] px-2 py-1">
                  <ConsoleHostBrandIcon host={host} className="size-3.5 shrink-0" />
                  <span className="min-w-0 flex-1 truncate text-dense-body font-semibold text-foreground">
                    {host.label} · {host.host}
                    {host.jump_label ? ` · via ${host.jump_label}` : ''}
                  </span>
                  {tab.connState === 'open' && (
                    <span className="inline-flex items-center gap-1 text-dense-label text-[var(--text-dense-meta)]">
                      <span className="server-console-live-dot" /> Live
                    </span>
                  )}
                  {tab.connState === 'connecting' && (
                    <span className="text-dense-label text-muted-foreground">Connecting…</span>
                  )}
                  {tab.connState === 'error' && (
                    <>
                      <span className="max-w-[8rem] truncate text-dense-label text-destructive">
                        {tab.error ?? 'Failed'}
                      </span>
                      <Button
                        size="sm"
                        className="shrink-0 px-2 py-0.5 text-dense-label"
                        onClick={() => retrySession(tab.id)}
                      >
                        Retry
                      </Button>
                    </>
                  )}
                  <button
                    type="button"
                    className="shrink-0 rounded p-0.5 text-muted-foreground hover:bg-muted/60 hover:text-foreground"
                    aria-label={`Close ${host.label} session`}
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
