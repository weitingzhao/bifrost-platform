import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useCallback, useEffect, useRef, useState } from 'react'
import { SegmentControl } from '@bifrost/ui'
import type { ConsoleHost } from '@/api/console'
import { consoleWebSocketUrl } from '@/api/console'

export type ServerTerminalProps = {
  hosts: ConsoleHost[]
  selectedId: string | null
  onSelectHost: (id: string) => void
}

type ConnState = 'idle' | 'connecting' | 'open' | 'closed' | 'error'

export function ServerTerminal({ hosts, selectedId, onSelectHost }: ServerTerminalProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const [connState, setConnState] = useState<ConnState>('idle')
  const [error, setError] = useState<string | null>(null)

  const selected = hosts.find(h => h.id === selectedId) ?? hosts[0] ?? null

  const disconnect = useCallback(() => {
    wsRef.current?.close()
    wsRef.current = null
    setConnState('closed')
  }, [])

  const sendResize = useCallback(() => {
    const ws = wsRef.current
    const term = termRef.current
    if (!ws || ws.readyState !== WebSocket.OPEN || !term) return
    ws.send(
      JSON.stringify({
        type: 'resize',
        cols: term.cols,
        rows: term.rows,
      }),
    )
  }, [])

  const connect = useCallback(() => {
    if (!selected) return
    disconnect()
    setError(null)
    setConnState('connecting')

    const term = termRef.current
    if (term) {
      term.clear()
      term.writeln(`\x1b[90mConnecting to ${selected.user}@${selected.host}:${selected.port} …\x1b[0m`)
    }

    const ws = new WebSocket(consoleWebSocketUrl(selected))
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      setConnState('open')
      sendResize()
    }
    ws.onmessage = ev => {
      const term = termRef.current
      if (!term) return
      if (typeof ev.data === 'string') {
        term.write(ev.data)
      } else if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data))
      }
    }
    ws.onerror = () => {
      setConnState('error')
      setError('WebSocket error')
    }
    ws.onclose = () => {
      setConnState(prev => (prev === 'connecting' ? 'error' : 'closed'))
      termRef.current?.writeln('\r\n\x1b[90m— session closed —\x1b[0m')
    }
  }, [selected, disconnect, sendResize])

  useEffect(() => {
    if (!containerRef.current) return

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 12,
      fontFamily: "'JetBrains Mono', ui-monospace, monospace",
      theme: {
        background: '#0a0c0f',
        foreground: '#e4e9ef',
        cursor: '#a3e635',
        selectionBackground: '#a3e63533',
      },
      scrollback: 5000,
    })
    const fit = new FitAddon()
    term.loadAddon(fit)
    term.open(containerRef.current)
    fit.fit()
    termRef.current = term
    fitRef.current = fit

    term.onData(data => {
      const ws = wsRef.current
      if (ws?.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    const onResize = () => {
      fit.fit()
      sendResize()
    }
    window.addEventListener('resize', onResize)
    const ro = new ResizeObserver(onResize)
    ro.observe(containerRef.current)

    return () => {
      window.removeEventListener('resize', onResize)
      ro.disconnect()
      wsRef.current?.close()
      term.dispose()
      termRef.current = null
      fitRef.current = null
    }
  }, [sendResize])

  useEffect(() => {
    if (selected && connState === 'open') {
      disconnect()
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps -- reconnect only when host changes
  }, [selected?.id])

  const hostOptions = hosts.map(h => ({
    value: h.id,
    label: `${h.label} · ${h.host}`,
  }))

  return (
    <section className="server-console panel-elevated overflow-hidden flex flex-col">
      <div className="server-console-toolbar flex flex-wrap items-center gap-2 px-3 py-2 border-b border-[var(--border)] bg-[var(--color-surface-elevated)]">
        <span className="env-strip-label">Host</span>
        {hosts.length > 0 ? (
          <>
            <SegmentControl
              ariaLabel="SSH host"
              value={selected?.id ?? ''}
              onChange={onSelectHost}
              options={hostOptions}
              size="sm"
            />
            {selected != null && (
              <span
                className={
                  selected.reachable ? 'lamp-ok text-[var(--text-dense-meta)]' : 'lamp-fail text-[var(--text-dense-meta)]'
                }
              >
                SSH {selected.reachable ? 'reachable' : 'unreachable'}
              </span>
            )}
          </>
        ) : (
          <span className="text-[var(--muted-foreground)] text-[var(--text-dense-meta)]">
            No SSH hosts in topology (set node host in topology.yaml)
          </span>
        )}
        <div className="ml-auto flex items-center gap-2">
          <span className="text-[var(--text-dense-meta)] text-[var(--muted-foreground)]">
            {connState === 'open' && (
              <>
                <span className="server-console-live-dot" /> Live
              </>
            )}
            {connState === 'connecting' && 'Connecting…'}
            {connState === 'idle' && 'Disconnected'}
            {connState === 'closed' && 'Disconnected'}
            {connState === 'error' && (error ?? 'Error')}
          </span>
          <button
            type="button"
            className="btn-ui btn-ui-primary"
            disabled={!selected || connState === 'connecting' || connState === 'open'}
            onClick={connect}
          >
            Connect
          </button>
          <button
            type="button"
            className="btn-ui"
            disabled={connState !== 'open' && connState !== 'connecting'}
            onClick={disconnect}
          >
            Disconnect
          </button>
        </div>
      </div>
      <div ref={containerRef} className="server-console-term min-h-[420px] w-full min-w-0 p-1" />
    </section>
  )
}
