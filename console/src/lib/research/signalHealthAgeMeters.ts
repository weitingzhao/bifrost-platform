/**
 * Pure helpers for Product asof age meters (36h fresh SLA).
 * No chart library — FillBar / StackedBar consumers only.
 */

export const SIGNAL_HEALTH_FRESH_SLA_HOURS = 36
/** Align with Massive / Research API weekend window (Sat/Sun/Mon-before-22:00 UTC). */
export const SIGNAL_HEALTH_WEEKEND_SLA_HOURS = 72
export const SIGNAL_HEALTH_STALE_CAP_HOURS = 168

/** Sat/Sun, or Monday before UTC 22:00 (next Dagster trading_day window). */
export function freshnessSlaHours(now = new Date()): number {
  const day = now.getUTCDay() // 0 Sun … 6 Sat
  const hour = now.getUTCHours()
  if (day === 0 || day === 6 || (day === 1 && hour < 22)) {
    return SIGNAL_HEALTH_WEEKEND_SLA_HOURS
  }
  return SIGNAL_HEALTH_FRESH_SLA_HOURS
}

export type AgeMeterTone = 'success' | 'warning' | 'danger' | 'neutral'

export type FreshnessStatusBucket = 'fresh' | 'stale' | 'other'

export function ageToFillPct(
  ageHours: number | null | undefined,
  slaHours = SIGNAL_HEALTH_FRESH_SLA_HOURS,
): number {
  if (ageHours == null || !Number.isFinite(ageHours) || ageHours < 0) return 0
  if (slaHours <= 0) return 100
  return Math.min(100, (ageHours / slaHours) * 100)
}

export function freshnessStatusTone(status: string | null | undefined): AgeMeterTone {
  const s = (status ?? '').toLowerCase()
  if (s === 'fresh') return 'success'
  if (s === 'stale' || s === 'missing') return 'danger'
  if (s === 'empty' || s === 'unknown') return 'warning'
  return 'neutral'
}

export function freshnessStatusBucket(status: string | null | undefined): FreshnessStatusBucket {
  const s = (status ?? '').toLowerCase()
  if (s === 'fresh') return 'fresh'
  if (s === 'stale' || s === 'missing') return 'stale'
  return 'other'
}

export function toneToMeterClass(tone: AgeMeterTone): string {
  if (tone === 'success') return 'bg-[var(--color-success)]'
  if (tone === 'warning') return 'bg-[var(--color-warning)]'
  if (tone === 'danger') return 'bg-[var(--color-danger,var(--destructive))]'
  return 'bg-[var(--muted-foreground)]'
}

export type FreshnessStackCounts = {
  fresh: number
  stale: number
  other: number
  total: number
  readyPct: number
  thinPct: number
  blockedPct: number
}

export function stackFreshnessStatuses(
  rows: Array<{ status?: string | null }>,
): FreshnessStackCounts {
  let fresh = 0
  let stale = 0
  let other = 0
  for (const row of rows) {
    const b = freshnessStatusBucket(row.status)
    if (b === 'fresh') fresh += 1
    else if (b === 'stale') stale += 1
    else other += 1
  }
  const total = fresh + stale + other
  const denom = total > 0 ? total : 1
  return {
    fresh,
    stale,
    other,
    total,
    readyPct: (fresh / denom) * 100,
    thinPct: (other / denom) * 100,
    blockedPct: (stale / denom) * 100,
  }
}
