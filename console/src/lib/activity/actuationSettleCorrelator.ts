import {
  getActivityEvents,
  updateActivityPhase,
} from '@/lib/activity/activityStore'
import type { ActivityEvent } from '@/lib/activity/activityTypes'
import {
  chipCorrelateKey,
  normalizeActivityEnvScope,
} from '@/lib/activity/signalTransitionDetector'

/** Correlate signal recovery with a recent open actuation on the same chip/target. */
export const ACTUATION_CORRELATE_WINDOW_MS = 2 * 60 * 1000

function normalizeKey(s: string): string {
  return s.trim().toLowerCase()
}

function matchesCorrelate(
  ev: ActivityEvent,
  chipLabel: string,
  envScope?: string,
): boolean {
  const scoped =
    envScope != null && envScope !== ''
      ? chipCorrelateKey(envScope, chipLabel)
      : null
  if (scoped != null && ev.correlateKey != null) {
    if (normalizeKey(ev.correlateKey) === normalizeKey(scoped)) return true
  }
  // Legacy / unscoped: match chip label only when no envScope on either side.
  if (scoped == null && ev.correlateKey != null) {
    const key = normalizeKey(chipLabel)
    if (normalizeKey(ev.correlateKey) === key) return true
    if (normalizeKey(ev.correlateKey).endsWith(`:${key}`)) return true
  }
  if (ev.target != null && normalizeKey(ev.target).includes(normalizeKey(chipLabel))) {
    return true
  }
  if (normalizeKey(ev.title).includes(normalizeKey(chipLabel))) return true
  return false
}

/**
 * When a readiness chip recovers to ok, mark the newest matching in-flight /
 * recently-applied actuation as settled·resolved (smart settle).
 */
export function correlateActuationSettle(
  chipLabel: string,
  envScope?: string,
  now = Date.now(),
): ActivityEvent | null {
  const scope = envScope != null ? normalizeActivityEnvScope(envScope) : undefined
  const candidates = getActivityEvents().filter(ev => {
    if (ev.kind !== 'actuation') return false
    if (ev.phase !== 'requested' && ev.phase !== 'applying' && ev.phase !== 'settled') {
      return false
    }
    // Re-open timeout / signal-unchanged when probe recovers.
    if (
      ev.phase === 'settled' &&
      ev.settledOutcome !== 'timeout' &&
      ev.settledOutcome !== 'signal-unchanged'
    ) {
      return false
    }
    if (now - ev.ts > ACTUATION_CORRELATE_WINDOW_MS) return false
    return matchesCorrelate(ev, chipLabel, scope)
  })

  if (candidates.length === 0) return null
  // Newest first (store keeps newest at front, but sort defensively).
  candidates.sort((a, b) => b.ts - a.ts)
  const hit = candidates[0]
  const scopeLabel = scope != null ? `${scope}/` : ''
  return updateActivityPhase(hit.id, 'settled', {
    settledOutcome: 'resolved',
    detail: `${scopeLabel}${chipLabel} recovered — correlated with actuation`,
  })
}
