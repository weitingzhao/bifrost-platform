import { useEffect, useSyncExternalStore } from 'react'
import { authedFetch, authHeaders, operatorToken } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import type { BusEnvelope, BusTraceResponse } from './busTrace'
import { SseParser } from './sseParser'
import { IdleWatch, LIVE_IDLE_MS } from './liveRefresh'
import type { BusReceipt } from './busNerve'

/** Events kept in memory, newest first. */
export const BUS_LIST_CAP = 200

/** Receipts kept in memory, newest first (the nerve map lands pulses by them). */
export const BUS_RECEIPT_CAP = 500

/** Name this Console's receipts carry (`gate:stream:console`). */
const CLIENT = 'console'

export type BusConnection = 'connecting' | 'live' | 'reconnecting' | 'off' | 'denied'

export interface BusStreamState {
  events: BusEnvelope[]
  /** Live receipts the stream passed on (receipts=1), newest first. */
  receipts: BusReceipt[]
  /** The gate follows no receipts (": receipts off"). */
  receiptsOff: boolean
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
const initialStream = (): BusStreamState => ({ events: [], receipts: [], receiptsOff: false, connection: 'connecting', detail: null, cursor: null })
let sharedState = initialStream()
const subscribers = new Set<() => void>()
let users = 0
let stopStream: (() => void) | undefined
let scope = ''
const getStream = () => sharedState
const subscribeStream = (listener: () => void) => { subscribers.add(listener); return () => { subscribers.delete(listener) } }
function setState(update: (state: BusStreamState) => BusStreamState) {
  sharedState = update(sharedState)
  subscribers.forEach(listener => listener())
}
/** One reader per authenticated Console, shared by City, Bus and collaboration. */
export function useBusStream(): BusStreamState {
  const state = useSyncExternalStore(subscribeStream, getStream, getStream)
  const identity = operatorToken()
  useEffect(() => {
    if (scope !== identity) {
      stopStream?.()
      stopStream = undefined
      scope = identity
      setState(initialStream)
    }
    users++
    if (!stopStream) stopStream = startStream()
    return () => { if (--users === 0) { stopStream?.(); stopStream = undefined } }
  }, [identity])
  return state
}
function startStream(): () => void {
    const abort = new AbortController()
    const { signal } = abort
    let cursor: string | undefined = sharedState.cursor ?? undefined
    let retry = RETRY_MS

    const run = async () => {
      while (!signal.aborted) {
        let wait = retry
        // Its own abort per attempt: a stream silent for LIVE_IDLE_MS (three missed
        // heartbeats) is dropped and reopened, as the Console's live refresh does.
        const attempt = new AbortController()
        const leave = () => attempt.abort()
        signal.addEventListener('abort', leave, { once: true })
        const idle = new IdleWatch(LIVE_IDLE_MS, () => attempt.abort())
        try {
          const headers = authHeaders()
          headers.set('Accept', 'text/event-stream')
          if (cursor) headers.set('Last-Event-ID', cursor)
          const r = await serverFetch(`/api/v1/bus/stream?client=${CLIENT}&receipts=1`, { headers, signal: attempt.signal, cache: 'no-store' })
          if (signal.aborted) return
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
            setState(s => ({ ...s, connection: 'live', detail: null, receiptsOff: false }))
            const parser = new SseParser()
            const reader = r.body.getReader()
            const decoder = new TextDecoder()
            for (;;) {
              const { value, done } = await reader.read()
              if (signal.aborted) return
              if (done) break
              idle.poke()
              const batch = parser.push(decoder.decode(value, { stream: true }))
              if (batch.retry != null) retry = batch.retry
              const error = batch.comments.find(c => c.startsWith('error '))
              const arrived: BusEnvelope[] = []
              const receipts: BusReceipt[] = []
              for (const m of batch.messages) {
                try {
                  if (m.event === 'rcpt') receipts.push(JSON.parse(m.data) as BusReceipt)
                  else arrived.push(JSON.parse(m.data) as BusEnvelope)
                } catch {
                  // a message that is not an envelope is skipped; its id still moves the cursor
                }
              }
              const off = batch.comments.includes('receipts off')
              if (parser.lastEventId) cursor = parser.lastEventId
              if (arrived.length > 0 || receipts.length > 0 || error || off) {
                setState(s => ({
                  ...s,
                  events: mergeEvents(s.events, arrived),
                  receipts: receipts.length > 0 ? [...new Map([...receipts.reverse(), ...s.receipts].map(r => [r.id + ':' + r.consumer + ':' + r.at, r])).values()].slice(0, BUS_RECEIPT_CAP) : s.receipts,
                  receiptsOff: s.receiptsOff || off,
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
          const detail = attempt.signal.aborted ? `no data for ${LIVE_IDLE_MS / 1000} s; reopening` : error instanceof Error ? error.message : 'stream failed'
          setState(s => ({ ...s, connection: 'reconnecting', detail }))
        } finally {
          idle.stop()
          signal.removeEventListener('abort', leave)
        }
        await sleep(wait, signal)
        if (!signal.aborted) setState(s => (s.connection === 'live' ? { ...s, connection: 'reconnecting' } : s))
      }
    }
    void run()
    return () => abort.abort()
}

/** GET /api/v1/bus/trace?id=ev-<n>: one event's journey. */
export async function fetchBusTrace(id: string): Promise<BusTraceResponse> {
  const r = await authedFetch('bus trace', `/api/v1/bus/trace?id=${encodeURIComponent(id)}`)
  return r.json() as Promise<BusTraceResponse>
}

/** One user of the bus (GET /api/v1/bus/clients). */
export interface BusClient {
  name: string
  kind?: string
  declared: boolean
  lost_after?: string
  last_seen?: string
  receipts: number
  /** gate roster: active, lost, new, idle (a dispatcher row with nothing yet); the page shows any other as itself. */
  status: 'active' | 'lost' | 'new' | 'idle' | (string & {})
  note?: string
}

export interface BusClientsResponse {
  tenant: string
  env: string
  hours: number
  clients: BusClient[]
  receipts_error?: string
}

/** GET /api/v1/bus/clients: who uses the bus, declared or new, and who went quiet. */
export async function fetchBusClients(): Promise<BusClientsResponse> {
  const r = await authedFetch('bus clients', '/api/v1/bus/clients?hours=24')
  return r.json() as Promise<BusClientsResponse>
}

/** Pages a replay may read: 100 events each, a day of PROD is some 2,300. */
export const REPLAY_MAX_PAGES = 40

/**
 * The last hours of events for the nerve map's replay, with the receipts they
 * had: GET /api/v1/bus/events with since= and receipts=1, each page from the
 * time of the last event the one before gave (an after= cursor would make it
 * a delivery and write receipts; a since= read writes none). Kept once by
 * id; stops at an empty page or one with nothing new. One read when asked,
 * not a poll. receiptsNote says why receipts are missing, when they are.
 */
export async function fetchBusReplay(sinceIso: string, withReceipts = true): Promise<{ events: BusEnvelope[]; receipts: BusReceipt[]; receiptsNote?: string }> {
  const out: BusEnvelope[] = []
  const receipts: BusReceipt[] = []
  let receiptsNote: string | undefined
  const seen = new Set<string>()
  let since = sinceIso
  for (let page = 0; page < REPLAY_MAX_PAGES; page++) {
    const r = await authedFetch('bus replay', `/api/v1/bus/events?since=${encodeURIComponent(since)}&limit=100&wait=0${withReceipts ? '&receipts=1' : ''}&client=console`)
    const body = (await r.json()) as { events?: BusEnvelope[]; receipts?: BusReceipt[]; receipts_note?: string }
    const fresh = (body.events ?? []).filter(e => !seen.has(e.id))
    if (fresh.length === 0) break
    for (const e of fresh) seen.add(e.id)
    out.push(...fresh)
    receipts.push(...(body.receipts ?? []))
    receiptsNote ??= body.receipts_note
    since = fresh[fresh.length - 1].time
  }
  return { events: out, receipts, receiptsNote }
}
