import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import {
  ACTIVITY_DROPDOWN_MAX,
  ACTIVITY_INFLIGHT_STALE_MS,
  ACTIVITY_SETTLED_TTL_MS,
  type ActivityEvent,
  type ActivityPhase,
  type ActivitySettledOutcome,
} from '@/lib/activity/activityTypes'

/** Recompute visible feed so TTL can hide the indicator without a new upsert. */
const ACTIVITY_FEED_PRUNE_TICK_MS = 60_000
const ACTIVITY_SESSION_KEY = 'bifrost.activity.events'

type Listener = () => void

function isTerminalPhase(phase: ActivityPhase): boolean {
  return phase === 'settled' || phase === 'completed' || phase === 'failed'
}

/**
 * Drop terminal rows past TTL and orphaned in-flight rows past stale window
 * (e.g. page reload killed the settle poller but sessionStorage kept APPLYING).
 */
function pruneList(list: ActivityEvent[], now = Date.now()): ActivityEvent[] {
  return list
    .filter(ev => {
      if (!isTerminalPhase(ev.phase)) {
        return now - ev.ts <= ACTIVITY_INFLIGHT_STALE_MS
      }
      return now - ev.ts <= ACTIVITY_SETTLED_TTL_MS
    })
    .slice(0, ACTIVITY_DROPDOWN_MAX)
}

function persistToSessionStorage(list: ActivityEvent[]): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(ACTIVITY_SESSION_KEY, JSON.stringify(list))
  } catch {
    /* quota / private mode — ignore */
  }
}

function emit(): void {
  for (const l of listeners) l()
}

function commit(next: ActivityEvent[]): void {
  events = pruneList(next)
  persistToSessionStorage(events)
  emit()
}

function loadFromSessionStorage(): ActivityEvent[] {
  if (typeof sessionStorage === 'undefined') return []
  try {
    const raw = sessionStorage.getItem(ACTIVITY_SESSION_KEY)
    if (raw == null || raw === '') return []
    const parsed = JSON.parse(raw) as unknown
    if (!Array.isArray(parsed)) return []
    const list: ActivityEvent[] = []
    for (const item of parsed) {
      if (item == null || typeof item !== 'object') continue
      const ev = item as Partial<ActivityEvent>
      if (typeof ev.id !== 'string' || typeof ev.kind !== 'string') continue
      if (typeof ev.phase !== 'string' || typeof ev.title !== 'string') continue
      if (typeof ev.ts !== 'number') continue
      list.push(ev as ActivityEvent)
    }
    return pruneList(list)
  } catch {
    return []
  }
}

let events: ActivityEvent[] = loadFromSessionStorage()
const listeners = new Set<Listener>()

function subscribe(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}

function getSnapshot(): ActivityEvent[] {
  return events
}

export function getActivityEvents(): ActivityEvent[] {
  return events
}

/** Re-run TTL / stale-inflight prune (safe to call on an interval). */
export function pruneActivityFeed(now = Date.now()): boolean {
  const next = pruneList(events, now)
  if (next.length === events.length && next.every((ev, i) => ev.id === events[i]?.id)) {
    return false
  }
  events = next
  persistToSessionStorage(events)
  emit()
  return true
}

export type UpsertActivityInput = {
  id: string
  kind: ActivityEvent['kind']
  phase: ActivityPhase
  title: string
  target?: string
  detail?: string
  settledOutcome?: ActivitySettledOutcome
  linkTo?: string
  correlateKey?: string
  ts?: number
  bumpTs?: boolean
}

/** Insert or update by id (dedup) — prefer update phase over spam. */
export function upsertActivity(input: UpsertActivityInput): ActivityEvent {
  const idx = events.findIndex(e => e.id === input.id)
  const prev = idx >= 0 ? events[idx] : undefined
  const next: ActivityEvent = {
    id: input.id,
    kind: input.kind,
    phase: input.phase,
    title: input.title,
    target: input.target ?? prev?.target,
    detail: input.detail ?? prev?.detail,
    settledOutcome: input.settledOutcome ?? prev?.settledOutcome,
    linkTo: input.linkTo ?? prev?.linkTo,
    correlateKey: input.correlateKey ?? prev?.correlateKey,
    ts: input.ts ?? (input.bumpTs === true || prev == null ? Date.now() : prev.ts),
  }

  let nextList: ActivityEvent[]
  if (idx >= 0) {
    nextList = events.filter(e => e.id !== input.id)
    nextList = [next, ...nextList]
  } else {
    nextList = [next, ...events]
  }
  commit(nextList)
  return next
}

export function updateActivityPhase(
  id: string,
  phase: ActivityPhase,
  patch?: Partial<
    Pick<ActivityEvent, 'detail' | 'settledOutcome' | 'title' | 'linkTo' | 'target'>
  >,
): ActivityEvent | null {
  const prev = events.find(e => e.id === id)
  if (prev == null) return null
  return upsertActivity({
    id,
    kind: prev.kind,
    phase,
    title: patch?.title ?? prev.title,
    target: patch?.target ?? prev.target,
    detail: patch?.detail ?? prev.detail,
    settledOutcome: patch?.settledOutcome ?? prev.settledOutcome,
    linkTo: patch?.linkTo ?? prev.linkTo,
    correlateKey: prev.correlateKey,
    bumpTs: true,
  })
}

/** Remove one event from the feed (Ignore / Dismiss). Does not cancel K8s / Agent work. */
export function dismissActivity(id: string): boolean {
  const next = events.filter(e => e.id !== id)
  if (next.length === events.length) return false
  commit(next)
  return true
}

/** Remove all non-terminal (requested/applying) rows. */
export function dismissAllInFlight(): number {
  const before = events.length
  const next = events.filter(e => isTerminalPhase(e.phase))
  const removed = before - next.length
  if (removed === 0) return 0
  commit(next)
  return removed
}

/** Test / reset helper — not for production UI. */
export function __resetActivityStoreForTests(): void {
  events = []
  if (typeof sessionStorage !== 'undefined') {
    try {
      sessionStorage.removeItem(ACTIVITY_SESSION_KEY)
    } catch {
      /* ignore */
    }
  }
  emit()
}

export function useActivityEvents(): ActivityEvent[] {
  return useSyncExternalStore(subscribe, getSnapshot, getSnapshot)
}

/** Visible feed: in-flight + recent terminal within TTL. */
export function useActivityFeed(): {
  events: ActivityEvent[]
  hasActivity: boolean
  inFlightCount: number
} {
  const all = useActivityEvents()
  const [tick, setTick] = useState(0)
  useEffect(() => {
    pruneActivityFeed()
    const id = window.setInterval(() => {
      pruneActivityFeed()
      setTick(t => t + 1)
    }, ACTIVITY_FEED_PRUNE_TICK_MS)
    return () => window.clearInterval(id)
  }, [])
  return useMemo(() => {
    void tick
    const now = Date.now()
    const visible = pruneList(all, now)
    const inFlightCount = visible.filter(ev => !isTerminalPhase(ev.phase)).length
    return {
      events: visible,
      hasActivity: visible.length > 0,
      inFlightCount,
    }
  }, [all, tick])
}

export function useActivityActions() {
  const upsert = useCallback((input: UpsertActivityInput) => upsertActivity(input), [])
  const updatePhase = useCallback(
    (
      id: string,
      phase: ActivityPhase,
      patch?: Partial<
        Pick<ActivityEvent, 'detail' | 'settledOutcome' | 'title' | 'linkTo' | 'target'>
      >,
    ) => updateActivityPhase(id, phase, patch),
    [],
  )
  const dismiss = useCallback((id: string) => dismissActivity(id), [])
  const dismissInFlight = useCallback(() => dismissAllInFlight(), [])
  return { upsert, updatePhase, dismiss, dismissInFlight }
}
