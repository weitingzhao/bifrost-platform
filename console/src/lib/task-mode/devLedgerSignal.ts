import type { Reachability } from '@bifrost/ui'

/** D-IL2: last_clone_at ≤7d even when lag_vs_prod=0. */
export const DEV_LEDGER_AGING_DAYS = 3
export const DEV_LEDGER_STALE_DAYS = 7

export type DevLedgerSignal = {
  lamp: Reachability
  lastCloneAt: string | null
  lastCloneLabel: string
  ageDays: number | null
  lagDays: number | null
  verdict: string | null
  /** Missing or ≥7d clone (or lag/verdict stale) — elevate Verdict CTA when fleet is clear. */
  blocking: boolean
  chipLabel: string
}

export function cloneAgeDays(lastCloneAt: string | null | undefined, now = Date.now()): number | null {
  if (lastCloneAt == null || lastCloneAt === '') return null
  const ms = Date.parse(lastCloneAt)
  if (!Number.isFinite(ms)) return null
  return (now - ms) / 86_400_000
}

export function formatCloneAge(lastCloneAt: string | null | undefined, now = Date.now()): string {
  if (lastCloneAt == null || lastCloneAt === '') return 'never'
  const age = cloneAgeDays(lastCloneAt, now)
  if (age == null) return lastCloneAt
  if (age * 86_400_000 < 60_000) return 'just now'
  if (age < 1 / 24) return `${Math.floor(age * 1_440)}m ago`
  if (age < 1) return `${Math.floor(age * 24)}h ago`
  if (age < 7) return `${Math.floor(age)}d ago`
  return new Date(Date.parse(lastCloneAt)).toLocaleString()
}

export function resolveDevLedgerSignal(input: {
  lastCloneAt?: string | null
  lagDays?: number | null
  verdict?: string | null
  now?: number
}): DevLedgerSignal {
  const now = input.now ?? Date.now()
  const lastCloneAt = input.lastCloneAt ?? null
  const ageDays = cloneAgeDays(lastCloneAt, now)
  const lagDays = input.lagDays ?? null
  const verdict = input.verdict ?? null
  const lagStale = lagDays != null && lagDays >= DEV_LEDGER_STALE_DAYS
  const lagAging = lagDays != null && lagDays >= DEV_LEDGER_AGING_DAYS
  const ageStale = ageDays == null || ageDays >= DEV_LEDGER_STALE_DAYS
  const ageAging = ageDays != null && ageDays >= DEV_LEDGER_AGING_DAYS
  const blocking = ageStale || verdict === 'stale' || lagStale
  const lamp: Reachability = blocking
    ? 'fail'
    : ageAging || verdict === 'aging' || lagAging
      ? 'degraded'
      : 'ok'
  const chipLabel =
    ageDays == null
      ? 'Ledger never'
      : ageDays < 1
        ? `Ledger ${formatCloneAge(lastCloneAt, now)}`
        : `Ledger ${Math.floor(ageDays)}d`

  return {
    lamp,
    lastCloneAt,
    lastCloneLabel: formatCloneAge(lastCloneAt, now),
    ageDays,
    lagDays,
    verdict,
    blocking,
    chipLabel,
  }
}
