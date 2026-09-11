/**
 * `pending` — the evidence has not arrived yet. `unknown` — it cannot arrive
 * (the read failed, or the session is unavailable). Neither is a finding, and
 * neither may be counted as `missing`.
 */
export type VitalKind = 'ok' | 'scheduled' | 'missing' | 'unknown' | 'pending'

export type VitalVerdict = {
  text: string
  kind: VitalKind
}

/** Widen scheduled window so Mon afternoon → tonight EOD shows Scheduled, not Missing. */
const SCHEDULED_WINDOW_MS = 12 * 60 * 60 * 1000
/** Align with platform/plugin weekend freshness (72h Sat/Sun/Mon-before-22UTC). */
const SESSION_GAP_MAX_AGE_MS = 72 * 60 * 60 * 1000

export function utcToday(now = new Date()): string {
  return now.toISOString().slice(0, 10)
}

/** Sat/Sun, or Monday before UTC 22:00 (next Cron/Dagster EOD window). */
export function isWeekendGapWindow(now = new Date()): boolean {
  const day = now.getUTCDay() // 0 Sun … 6 Sat
  const hour = now.getUTCHours()
  return day === 0 || day === 6 || (day === 1 && hour < 22)
}

export function classifyVitalText(text: string): VitalKind {
  if (text === 'Today OK' || text === 'Session OK') return 'ok'
  if (text.startsWith('Scheduled')) return 'scheduled'
  if (text === 'Missing') return 'missing'
  if (text === PENDING.text) return 'pending'
  return 'unknown'
}

export function vitalFill(kind: VitalKind, ratioPct?: number): number {
  if (ratioPct != null) return Math.max(0, Math.min(100, ratioPct))
  if (kind === 'ok') return 100
  if (kind === 'scheduled') return 50
  return 0
}

export function vitalTagVariant(
  kind: VitalKind,
): 'success' | 'warning' | 'danger' | 'neutral' {
  if (kind === 'ok') return 'success'
  if (kind === 'scheduled') return 'warning'
  if (kind === 'missing') return 'danger'
  return 'neutral'
}

/**
 * Whether the feed has run for the session the tables should hold.
 *
 * `session` comes from the plugin, which owns the one definition (C-F1). Pass
 * it. Without it this falls back to the UTC calendar date, which was the bug:
 * at 2026-09-11 01:52 UTC — 21:52 on the 10th in New York, after that
 * session's batch had completed — the strip read `Missing` for stock_daily
 * while the table held 13,750,512 rows and the doctor called the session's EOD
 * checks complete. Every weekday between 00:00 UTC and the next evening's
 * batch looked like that, which is most of the day.
 */
export function computeVerdict(
  lastRunAt?: string,
  nextRunAt?: string,
  now = new Date(),
  session?: string | null,
): VitalVerdict {
  const lastDate = lastRunAt?.trim().slice(0, 10)
  if (session) {
    // Ran for that session or later. A slot that fired after the session closed
    // is current, whatever the calendar has since done.
    if (lastDate && lastDate >= session) {
      return { text: 'Session OK', kind: 'ok' }
    }
  } else if (lastDate && lastDate === utcToday(now)) {
    return { text: 'Today OK', kind: 'ok' }
  }
  if (lastRunAt?.trim() && isWeekendGapWindow(now)) {
    const lastMs = new Date(lastRunAt).getTime()
    if (Number.isFinite(lastMs)) {
      const age = now.getTime() - lastMs
      if (age >= 0 && age <= SESSION_GAP_MAX_AGE_MS) {
        return { text: 'Session OK', kind: 'ok' }
      }
    }
  }
  if (nextRunAt?.trim()) {
    const nextMs = new Date(nextRunAt).getTime()
    if (Number.isFinite(nextMs)) {
      const delta = nextMs - now.getTime()
      if (delta >= 0 && delta <= SCHEDULED_WINDOW_MS) {
        const hours = Math.max(1, Math.round(delta / (60 * 60 * 1000)))
        return { text: `Scheduled ~${hours}h`, kind: 'scheduled' }
      }
    }
  }
  return { text: 'Missing', kind: 'missing' }
}

export function freshnessToday(
  items: Array<{ last_run_at?: string | null }>,
  now = new Date(),
  session?: string | null,
) {
  // Same correction as computeVerdict: "how many ran for the session we should
  // hold", not "how many ran since midnight UTC". At 01:52 UTC the second
  // question answers 6/20 on an estate where every feed had run.
  const bound = session || utcToday(now)
  const todayCount = items.filter(i => (i.last_run_at?.trim().slice(0, 10) ?? '') >= bound).length
  const total = items.length
  const ratio = total > 0 ? (todayCount / total) * 100 : 0
  // No rows is an empty answer, not a finding — the text already says '—'.
  let kind: VitalKind = total === 0 ? 'unknown' : 'missing'
  if (total > 0 && todayCount === total) kind = 'ok'
  else if (todayCount > 0) kind = 'scheduled'
  const unit = session ? 'for session' : 'today'
  return { todayCount, total, ratio, kind, text: total > 0 ? `${todayCount}/${total} ${unit}` : '—' }
}

export function countByKind(kinds: VitalKind[]): {
  ok: number
  scheduled: number
  missing: number
  pending: number
  unknown: number
} {
  // `unknown` used to be counted as missing. On a hard reload the header read
  // `missing 4` at four seconds and `4/4 Session OK` at twenty-four, on the same
  // data: the four cards had simply not loaded yet (2026-09-11, 16:5x UTC).
  return {
    ok: kinds.filter(k => k === 'ok').length,
    scheduled: kinds.filter(k => k === 'scheduled').length,
    missing: kinds.filter(k => k === 'missing').length,
    pending: kinds.filter(k => k === 'pending').length,
    unknown: kinds.filter(k => k === 'unknown').length,
  }
}

/** Whether a read has answered: its data is in, it failed, or neither yet. */
export type Arrival = 'arrived' | 'pending' | 'failed'

export const PENDING: VitalVerdict = { text: '…', kind: 'pending' }
const UNREADABLE: VitalVerdict = { text: '—', kind: 'unknown' }

/**
 * The verdict a card may state now — `computeVerdict`, but only once its
 * evidence is in. A value that has not arrived is not a finding.
 *
 * `session` is `undefined` while the plugin's answer is in flight and `null`
 * when it cannot be had; only a string lets the card judge. There is no
 * fallback to the UTC calendar here: that second definition of "the session"
 * is what painted a complete estate `Missing` most of every weekday (C-F1).
 *
 * `schedule` is the read carrying the worker's next run. It is what turns
 * `Missing` into `Scheduled`, so a `Missing` stated without it is premature.
 */
export function judgeVital(
  lastRunAt: string | undefined,
  nextRunAt: string | undefined,
  evidence: { inputs: Arrival; session: string | null | undefined; schedule: Arrival },
  now = new Date(),
): VitalVerdict {
  if (evidence.inputs === 'pending' || evidence.session === undefined) return PENDING
  if (evidence.inputs === 'failed' || evidence.session === null) return UNREADABLE
  const v = computeVerdict(lastRunAt, nextRunAt, now, evidence.session)
  if (v.kind === 'missing' && evidence.schedule === 'pending') return PENDING
  return v
}
