import { useEffect, useState } from 'react'
import { authedFetch, authHeaders } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import type { BusEnvelope, BusTraceResponse } from './busTrace'
import { SseParser } from './sseParser'

/** Events kept in memory, newest first. */
export const BUS_LIST_CAP = 200

/** Name this Console's receipts carry (`gate:stream:console`). */
const CLIENT = 'console'

export type BusConnection = 'connecting' | 'live' | 'reconnecting' | 'off' | 'denied'

export interface BusStreamState {
  events: BusEnvelope[]
  connection: BusConnection
  detail: string | null
  /** Cursor the stream resumes from (Last-Event-ID). */
  cursor: string | null
}

const RETRY_MS = 2000
const OFF_RETRY_MS = 15000

/** Prepend newest-first, dropping ids already listed, capped at BUS_LIST_CAP. */
export function mergeEvents(list: BusEnvelope[], arrived: BusEnvelope[]): BusEnvelope[] {
  if (arrived.length === 0) return list
  const seen = new Set(list.map(e => e.id))
  const fresh = arrived.filter(e => !seen.has(e.id)).reverse()
  return [...fresh, ...list].slice(0, BUS_LIST_CAP)
}

async function errorText(r: Response): Promise<string> {
  try {
    const body = (await r.json()) as { error?: string }
    // The gate's own error text; it never echoes the bearer.
    return body.error ?? `HTTP ${r.status}`
  } catch {
    return `HTTP ${r.status}`
  }
}

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise(resolve => {
    const t = setTimeout(resolve, ms)
    signal.addEventListener('abort', () => { clearTimeout(t); resolve() }, { once: true })
  })
}

/**
 * GET /api/v1/bus/stream read with fetch streaming (the route keeps the viewer bearer). It starts from
 * now — the gate has no "latest N" read — and resumes from the last cursor with Last-Event-ID.
 */
export function useBusStream(): BusStreamState {
  const [state, setState] = useState<BusStreamState>({ events: [], connection: 'connecting', detail: null, cursor: null })

  useEffect(() => {
    const abort = new AbortController()
    const { signal } = abort
    let cursor: string | undefined
    let retry = RETRY_MS

    const run = async () => {
      while (!signal.aborted) {
        let wait = retry
        try {
          const headers = authHeaders()
          headers.set('Accept', 'text/event-stream')
          if (cursor) headers.set('Last-Event-ID', cursor)
          const r = await serverFetch(`/api/v1/bus/stream?client=${CLIENT}`, { headers, signal, cache: 'no-store' })
          if (r.status === 503 || r.status === 404 || r.status === 502) {
            const detail = r.status === 404 ? 'this platform-api has no bus routes' : await errorText(r)
            setState(s => ({ ...s, connection: 'off', detail }))
            wait = OFF_RETRY_MS
          } else if (r.status === 401 || r.status === 403) {
            const detail = await errorText(r)
            setState(s => ({ ...s, connection: 'denied', detail }))
            wait = OFF_RETRY_MS
          } else if (!r.ok || r.body == null) {
            const detail = await errorText(r)
            setState(s => ({ ...s, connection: 'reconnecting', detail }))
          } else {
            setState(s => ({ ...s, connection: 'live', detail: null }))
            const parser = new SseParser()
            const reader = r.body.getReader()
            const decoder = new TextDecoder()
            for (;;) {
              const { value, done } = await reader.read()
              if (done) break
              const batch = parser.push(decoder.decode(value, { stream: true }))
              if (batch.retry != null) retry = batch.retry
              const error = batch.comments.find(c => c.startsWith('error '))
              const arrived: BusEnvelope[] = []
              for (const m of batch.messages) {
                try {
                  arrived.push(JSON.parse(m.data) as BusEnvelope)
                } catch {
                  // a message that is not an envelope is skipped; its id still moves the cursor
                }
              }
              if (parser.lastEventId) cursor = parser.lastEventId
              if (arrived.length > 0 || error) {
                setState(s => ({
                  ...s,
                  events: mergeEvents(s.events, arrived),
                  cursor: cursor ?? s.cursor,
                  detail: error ? error.slice('error '.length) : s.detail,
                }))
              }
            }
            if (!signal.aborted) setState(s => ({ ...s, connection: 'reconnecting' }))
          }
        } catch (error) {
          if (signal.aborted) return
          // A fetch failure names the URL, never the request's headers.
          const detail = error instanceof Error ? error.message : 'stream failed'
          setState(s => ({ ...s, connection: 'reconnecting', detail }))
        }
        await sleep(wait, signal)
        if (!signal.aborted) setState(s => (s.connection === 'live' ? { ...s, connection: 'reconnecting' } : s))
      }
    }
    void run()
    return () => abort.abort()
  }, [])

  return state
}

/** GET /api/v1/bus/trace?id=ev-<n>: one event's journey. */
export async function fetchBusTrace(id: string): Promise<BusTraceResponse> {
  const r = await authedFetch('bus trace', `/api/v1/bus/trace?id=${encodeURIComponent(id)}`)
  return r.json() as Promise<BusTraceResponse>
}
