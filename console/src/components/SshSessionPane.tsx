import { FitAddon } from '@xterm/addon-fit'
import { Terminal } from '@xterm/xterm'
import '@xterm/xterm/css/xterm.css'
import { useCallback, useEffect, useRef } from 'react'
import type { ConsoleHost } from '@/api/console'
import { consoleWebSocketUrl } from '@/api/console'

export type SshConnState = 'connecting' | 'open' | 'closed' | 'error'

export type SshSessionPaneProps = {
  host: ConsoleHost
  /** Inactive sessions stay connected but must not fit/send a zero-sized terminal. */
  active: boolean
  onConnectionChange: (state: SshConnState, error?: string | null) => void
}

export function SshSessionPane({ host, active, onConnectionChange }: SshSessionPaneProps) {
  const containerRef = useRef<HTMLDivElement>(null)
  const termRef = useRef<Terminal | null>(null)
  const fitRef = useRef<FitAddon | null>(null)
  const wsRef = useRef<WebSocket | null>(null)
  const activeRef = useRef(active)
  const onConnectionChangeRef = useRef(onConnectionChange)

  useEffect(() => {
    activeRef.current = active
  }, [active])

  useEffect(() => {
    onConnectionChangeRef.current = onConnectionChange
  }, [onConnectionChange])

  const sendResize = useCallback(() => {
    const ws = wsRef.current
    const term = termRef.current
    if (
      !activeRef.current ||
      !ws ||
      ws.readyState !== WebSocket.OPEN ||
      !term ||
      term.cols <= 0 ||
      term.rows <= 0
    ) {
      return
    }
    ws.send(
      JSON.stringify({
        type: 'resize',
        cols: term.cols,
        rows: term.rows,
      }),
    )
  }, [])

  const fitTerminal = useCallback(() => {
    const container = containerRef.current
    if (
      !activeRef.current ||
      container == null ||
      container.clientWidth <= 0 ||
      container.clientHeight <= 0
    ) {
      return
    }
    fitRef.current?.fit()
    sendResize()
  }, [sendResize])

  useEffect(() => {
    if (!containerRef.current) return

    let cancelled = false

    const term = new Terminal({
      cursorBlink: true,
      fontSize: 11,
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
    termRef.current = term
    fitRef.current = fit
    fitTerminal()

    term.writeln(
      `\x1b[90mConnecting to ${host.user}@${host.host}:${host.port}${host.jump_label ? ` via ${host.jump_label}` : ''} …\x1b[0m`,
    )
    onConnectionChangeRef.current('connecting')

    const ws = new WebSocket(consoleWebSocketUrl(host))
    ws.binaryType = 'arraybuffer'
    wsRef.current = ws

    ws.onopen = () => {
      if (cancelled) return
      onConnectionChangeRef.current('open')
      fitTerminal()
    }
    ws.onmessage = ev => {
      if (typeof ev.data === 'string') {
        term.write(ev.data)
      } else if (ev.data instanceof ArrayBuffer) {
        term.write(new Uint8Array(ev.data))
      }
    }
    ws.onerror = () => {
      if (cancelled) return
      onConnectionChangeRef.current('error', 'WebSocket error')
    }
    ws.onclose = () => {
      if (cancelled) return
      onConnectionChangeRef.current('error', 'Connection failed')
      term.writeln('\r\n\x1b[90m— connection closed —\x1b[0m')
    }

    term.onData(data => {
      if (ws.readyState === WebSocket.OPEN) {
        ws.send(new TextEncoder().encode(data))
      }
    })

    const onResize = () => fitTerminal()
    window.addEventListener('resize', onResize)
    const ro = new ResizeObserver(onResize)
    ro.observe(containerRef.current)

    return () => {
      cancelled = true
      window.removeEventListener('resize', onResize)
      ro.disconnect()
      ws.close()
      term.dispose()
      termRef.current = null
      fitRef.current = null
      wsRef.current = null
    }
    // A session is keyed by tab id, so it should survive parent data refreshes.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => {
    if (active) {
      requestAnimationFrame(() => {
        fitTerminal()
        termRef.current?.focus()
      })
    }
  }, [active, fitTerminal])

  return <div ref={containerRef} className="absolute inset-0 min-h-0 min-w-0" />
}
