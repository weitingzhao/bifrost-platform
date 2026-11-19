/**
 * Live refresh (bus step 4, 2026-11-19; the Owner: "if every page polls, it
 * polls you to death"). One stream per Console tab says what changed; the
 * queries that read it are refreshed then, and their own polling slows to a
 * fallback while the stream is live. The data is still read from the same
 * routes: an event only says "read again" (it carries no data).
 *
 * The table below maps a bus event type to the query keys it makes stale.
 * Its test checks every type in it is registered on the bus (or listed as
 * coming), so a renamed type cannot silently stop a refresh.
 */
import { useEffect, useSyncExternalStore } from 'react'
import type { QueryClient, QueryKey } from '@tanstack/react-query'
import { authHeaders } from '@/api/client'
import { serverFetch } from '@/api/serverContact'
import { SseParser } from './sseParser'

export interface LiveRefreshRow {
  /** Event types, exact, or a prefix ending in `.` (approval.card. is every approval.card.* type). */
  types: string[]
  /** Query key prefixes made stale: ['approvals'] is every query whose key starts with 'approvals'. */
  keys: QueryKey[]
  /** How long a burst of these settles before the refresh; LIVE_SETTLE_MS when absent. */
  settleMs?: number
}

export const LIVE_REFRESH: LiveRefreshRow[] = [
  // agent-runtime-progress: of what it reads only the plan and the cards still
  // change (plan domain, 2026-11-19: tasks, attempts, missions last written 11-13).
  { types: ['approval.card.'], keys: [['approvals'], ['cockpit'], ['agent-runtime-progress']] },
  { types: ['owner.question.'], keys: [['questions']] },
  { types: ['seat.liveness.changed', 'seat.status.changed', 'seat.session.changed'], keys: [['agent-threads'], ['seat-statuses']] },
  { types: ['release.window.changed'], keys: [['delivery', 'release-window'], ['ci', 'release-window']] },
  { types: ['ci.run.', 'ci.verdict.changed'], keys: [['delivery', 'runs'], ['delivery', 'pipelines'], ['ci']] },
  { types: ['release.deploy.finished', 'release.gitops.changed'], keys: [['releases'], ['gitops']] },
  { types: ['alert.rule.fired'], keys: [['telemetry', 'alerts']] },
  // Recorded actions come often (release window holds alone: 393 in two days), so
  // they settle for 60 s, never more often than the 60 s poll they replace:
  // PROD read /records 82 times in 30 minutes with a 1 s settle and 43 with 30 s,
  // against 12 to 32 when it polled (Prometheus, 2026-11-19).
  { types: ['audit.action.recorded'], keys: [['records'], ['audit']], settleMs: 60_000 },
  { types: ['plan.tree.changed'], keys: [['agent-runtime-plan'], ['agent-runtime-plan-view'], ['agent-runtime-progress']] },
]

/** Types in the table not on the bus yet (each names who adds it). */
export const LIVE_REFRESH_COMING: Record<string, string> = {}

/** How often a query still polls while the stream is live: the safety net. */
export const LIVE_FALLBACK_MS = 300_000

/** Wait after an event before refreshing, so a burst is one refresh. */
export const LIVE_SETTLE_MS = 1_000

function matches(pattern: string, type: string): boolean {
  return pattern.endsWith('.') ? type.startsWith(pattern) : type === pattern
}

/** The query key prefixes an event of this type makes stale. */
export function keysFor(type: string): QueryKey[] {
  const out: QueryKey[] = []
  for (const row of LIVE_REFRESH) {
    if (row.types.some(p => matches(p, type))) out.push(...row.keys)
  }
  return out
}

/** The keys an event makes stale, each with how long it settles. */
export function staleFor(type: string): { key: QueryKey; settleMs: number }[] {
  const out: { key: QueryKey; settleMs: number }[] = []
  for (const row of LIVE_REFRESH) {
    if (row.types.some(p => matches(p, type))) {
      for (const key of row.keys) out.push({ key, settleMs: row.settleMs ?? LIVE_SETTLE_MS })
    }
  }
  return out
}

// --- whether the stream is live, for the pages' polling ---------------------

let live = false
const listeners = new Set<() => void>()

function setLive(next: boolean): void {
  if (live === next) return
  live = next
  for (const l of listeners) l()
}

function subscribe(l: () => void): () => void {
  listeners.add(l)
  return () => listeners.delete(l)
}

/** Whether this tab's live stream is connected. */
export function useLiveRefreshConnected(): boolean {
  return useSyncExternalStore(subscribe, () => live, () => false)
}

/** A query's interval: its own while the stream is down, the fallback while it is live. */
export function liveInterval(ownMs: number, connected: boolean): number {
  return connected ? Math.max(ownMs, LIVE_FALLBACK_MS) : ownMs
}

/** liveInterval for a component: `refetchInterval: useLiveInterval(30_000)`. */
export function useLiveInterval(ownMs: number): number {
  return liveInterval(ownMs, useLiveRefreshConnected())
}

// --- the stream ----------------------------------------------------------------

/** Collects stale keys and refreshes each once its burst settles (a key's own settle, from its row). */
export class RefreshBatch {
  private pending = new Map<string, { key: QueryKey; timer: ReturnType<typeof setTimeout> }>()

  constructor(private readonly refresh: (key: QueryKey) => void, private readonly settleMs = LIVE_SETTLE_MS) {}

  add(type: string): void {
    for (const { key, settleMs } of staleFor(type)) {
      const id = JSON.stringify(key)
      if (this.pending.has(id)) continue
      const wait = settleMs === LIVE_SETTLE_MS ? this.settleMs : settleMs
      this.pending.set(id, { key, timer: setTimeout(() => this.fire(id), wait) })
    }
  }

  private fire(id: string): void {
    const p = this.pending.get(id)
    if (p === undefined) return
    this.pending.delete(id)
    this.refresh(p.key)
  }

  flush(): void {
    for (const id of [...this.pending.keys()]) {
      clearTimeout(this.pending.get(id)!.timer)
      this.fire(id)
    }
  }

  stop(): void {
    for (const p of this.pending.values()) clearTimeout(p.timer)
    this.pending.clear()
  }
}

const CLIENT = 'console-live'
const RETRY_MS = 2_000
const OFF_RETRY_MS = 30_000

function sleep(ms: number, signal: AbortSignal): Promise<void> {
  return new Promise(resolve => {
    const t = setTimeout(resolve, ms)
    signal.addEventListener('abort', () => { clearTimeout(t); resolve() }, { once: true })
  })
}

/**
 * Keeps one bus stream open for this tab and refreshes the queries an event
 * makes stale. Mount it once, inside the QueryClientProvider. All types are
 * read (no types= filter, so a type not registered yet is never refused) and
 * the table picks what matters. On reconnect it refreshes everything it
 * covers once, since events may have been missed while it was down.
 */
export function useLiveRefresh(queryClient: QueryClient): void {
  useEffect(() => {
    const abort = new AbortController()
    const { signal } = abort
    const batch = new RefreshBatch(key => { void queryClient.invalidateQueries({ queryKey: key }) })
    let cursor: string | undefined
    let retry = RETRY_MS
    let wasLive = false

    const run = async () => {
      while (!signal.aborted) {
        let wait = retry
        try {
          const headers = authHeaders()
          headers.set('Accept', 'text/event-stream')
          if (cursor) headers.set('Last-Event-ID', cursor)
          const r = await serverFetch(`/api/v1/bus/stream?client=${CLIENT}`, { headers, signal, cache: 'no-store' })
          if (!r.ok || r.body == null) {
            // 401/403 without a token, 404/503 without the bus: poll as before.
            wait = r.status === 401 || r.status === 403 || r.status === 404 || r.status === 503 ? OFF_RETRY_MS : retry
          } else {
            setLive(true)
            if (wasLive) {
              // Back after a gap: what changed meanwhile was not heard.
              for (const row of LIVE_REFRESH) for (const key of row.keys) void queryClient.invalidateQueries({ queryKey: key })
            }
            wasLive = true
            const parser = new SseParser()
            const reader = r.body.getReader()
            const decoder = new TextDecoder()
            for (;;) {
              const { value, done } = await reader.read()
              if (done) break
              const got = parser.push(decoder.decode(value, { stream: true }))
              if (got.retry != null) retry = got.retry
              for (const m of got.messages) {
                try {
                  const type = (JSON.parse(m.data) as { type?: string }).type
                  if (type) batch.add(type)
                } catch {
                  // not an envelope: its id still moves the cursor
                }
              }
              if (parser.lastEventId) cursor = parser.lastEventId
            }
          }
        } catch {
          if (signal.aborted) break
        }
        setLive(false)
        await sleep(wait, signal)
      }
    }
    void run()
    return () => {
      abort.abort()
      batch.stop()
      setLive(false)
    }
  }, [queryClient])
}

/** useLiveRefresh as a component: mount once inside the QueryClientProvider. */
export function LiveRefresh({ queryClient }: { queryClient: QueryClient }): null {
  useLiveRefresh(queryClient)
  return null
}
