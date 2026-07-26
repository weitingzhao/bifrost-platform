import type { Signal } from '@/lib/control-room/missionSignals'
import type { UpsertActivityInput } from '@/lib/activity/activityStore'

export const SIGNAL_TRANSITION_DEBOUNCE_MS = 5 * 60 * 1000

export type ChipSnapshot = {
  label: string
  signal: Signal
  detail?: string
  /** Env / panel scope so STG vs PROD same labels do not collide. */
  envScope?: string
}

export type SignalTransition = {
  chipLabel: string
  from: Signal
  to: Signal
  detail?: string
  envScope: string
}

/** Normalize readiness / panel env into Activity correlate scope. */
export function normalizeActivityEnvScope(env: string | undefined | null): string {
  const raw = (env ?? 'unknown').trim().toLowerCase()
  if (raw === '') return 'unknown'
  return raw
}

/** Stable correlate key: `{envScope}:{chipLabel}` (lowercase). */
export function chipCorrelateKey(envScope: string, chipLabel: string): string {
  return `${normalizeActivityEnvScope(envScope)}:${chipLabel.trim().toLowerCase()}`
}

/**
 * Detect ok ↔ degraded/fail transitions only.
 * Ignores unknown→ok on first load (baseline seeding).
 * Debounce 5min applies to degrade / severity changes only — recovery to ok always emits
 * (so smart settle can correlate after a signal-unchanged timeout).
 */
export class SignalTransitionDetector {
  private prev = new Map<string, Signal>()
  private seeded = false
  private lastDegradeAt = new Map<string, number>()

  observe(chips: ChipSnapshot[], now = Date.now()): SignalTransition[] {
    if (!this.seeded) {
      for (const c of chips) {
        this.prev.set(chipStateKey(c), c.signal)
      }
      this.seeded = true
      return []
    }

    const out: SignalTransition[] = []
    for (const c of chips) {
      const stateKey = chipStateKey(c)
      const envScope = normalizeActivityEnvScope(c.envScope)
      const from = this.prev.get(stateKey) ?? 'unknown'
      const to = c.signal
      this.prev.set(stateKey, to)

      if (from === to) continue
      if (!isNotableTransition(from, to)) continue

      const recovery = isRecoveryToOk(from, to)
      if (!recovery) {
        const prevAt = this.lastDegradeAt.get(stateKey)
        if (prevAt != null && now - prevAt < SIGNAL_TRANSITION_DEBOUNCE_MS) {
          continue
        }
        this.lastDegradeAt.set(stateKey, now)
      } else {
        // Allow a new degrade to surface immediately after recovery.
        this.lastDegradeAt.delete(stateKey)
      }

      out.push({
        chipLabel: c.label,
        from,
        to,
        detail: c.detail,
        envScope,
      })
    }
    return out
  }

  /** Test helper */
  reset(): void {
    this.prev.clear()
    this.seeded = false
    this.lastDegradeAt.clear()
  }
}

function chipStateKey(c: ChipSnapshot): string {
  return chipCorrelateKey(c.envScope ?? 'unknown', c.label)
}

function isRecoveryToOk(from: Signal, to: Signal): boolean {
  return to === 'ok' && (from === 'degraded' || from === 'fail')
}

function isNotableTransition(from: Signal, to: Signal): boolean {
  // Ignore unknown→ok on first observation path (also covered by seed).
  if (from === 'unknown' && to === 'ok') return false
  const fromBad = from === 'degraded' || from === 'fail'
  const toBad = to === 'degraded' || to === 'fail'
  const fromOk = from === 'ok'
  const toOk = to === 'ok'
  // ok ↔ degraded/fail
  if (fromOk && toBad) return true
  if (fromBad && toOk) return true
  // degraded ↔ fail also useful
  if (fromBad && toBad && from !== to) return true
  return false
}

export function signalTransitionActivityId(
  envScope: string,
  chipLabel: string,
  from: Signal,
  to: Signal,
): string {
  return `signal:${normalizeActivityEnvScope(envScope)}:${chipLabel.toLowerCase()}:${from}->${to}`
}

export function signalTransitionToActivity(t: SignalTransition): UpsertActivityInput {
  const recovering = t.to === 'ok'
  return {
    id: signalTransitionActivityId(t.envScope, t.chipLabel, t.from, t.to),
    kind: 'signal-transition',
    phase: recovering ? 'completed' : 'failed',
    title: `${t.chipLabel}: ${t.from} → ${t.to}`,
    target: `${t.envScope}/${t.chipLabel}`,
    detail: t.detail,
    correlateKey: chipCorrelateKey(t.envScope, t.chipLabel),
    settledOutcome: recovering ? 'resolved' : undefined,
    bumpTs: true,
  }
}
