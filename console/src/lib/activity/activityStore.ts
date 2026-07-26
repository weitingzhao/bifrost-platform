import { useCallback, useEffect, useMemo, useState, useSyncExternalStore } from 'react'
import {
  ACTIVITY_DROPDOWN_MAX,
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

function pruneList(list: ActivityEvent[], now = Date.now()): ActivityEvent[] {
  return list
    .filter(ev => !isTerminalPhase(ev.phase) || now - ev.ts <= ACTIVITY_SETTLED_TTL_MS)
    .slice(0, ACTIVITY_DROPDOWN_MAX)
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

function persistToSessionStorage(list: ActivityEvent[]): void {
  if (typeof sessionStorage === 'undefined') return
  try {
    sessionStorage.setItem(ACTIVITY_SESSION_KEY, JSON.stringify(list))
  } catch {
    /* quota / private mode — ignore */
  }
}

let events: ActivityEvent[] = loadFromSessionStorage()
const listeners = new Set<Listener>()

function emit(): void {
  for (const l of listeners) l()
}

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
  events = pruneList(nextList)
  persistToSessionStorage(events)
  emit()
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
    const id = window.setInterval(() => setTick(t => t + 1), ACTIVITY_FEED_PRUNE_TICK_MS)
    return () => window.clearInterval(id)
  }, [])
  return useMemo(() => {
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
  return { upsert, updatePhase }
}
