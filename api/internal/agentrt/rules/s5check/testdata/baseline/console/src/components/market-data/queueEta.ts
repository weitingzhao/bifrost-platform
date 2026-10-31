/**
 * The queue's ETA as something a person can act on.
 *
 * The API hands over `eta_minutes_at_current_rate`; the panel showed it as
 * "1695.4m". Nobody converts that in their head. Three readings instead: the
 * duration in the units it deserves, the clock time it lands on, and the net
 * drain — done minus newly enqueued — because an ETA at the gross rate
 * understates whenever the queue is also being fed.
 */
import { formatDurationParts } from '@/lib/patrol/cronSchedule'

/** "28h 15m", "1d 4h", "45m" — from minutes. Null when there is no rate. */
export function formatEtaMinutes(etaMinutes: number | null | undefined): string | null {
  if (etaMinutes == null || !Number.isFinite(etaMinutes) || etaMinutes < 0) return null
  return formatDurationParts(etaMinutes * 60_000)
}

/**
 * The clock time the queue would be empty, in the viewer's zone.
 * Same day → "05:56"; another day → "Tue 05:56"; a week out or more → the date.
 */
export function etaClockLabel(etaMinutes: number | null | undefined, now: number = Date.now()): string | null {
  if (etaMinutes == null || !Number.isFinite(etaMinutes) || etaMinutes < 0) return null
  const at = new Date(now + etaMinutes * 60_000)
  const today = new Date(now)
  const sameDay = at.toDateString() === today.toDateString()
  const hhmm = at.toLocaleTimeString(undefined, { hour: '2-digit', minute: '2-digit', hour12: false })
  if (sameDay) return hhmm
  const days = (at.getTime() - now) / 86_400_000
  if (days >= 7) return at.toLocaleDateString(undefined, { month: 'short', day: 'numeric' })
  return `${at.toLocaleDateString(undefined, { weekday: 'short' })} ${hhmm}`
}

/**
 * Net drain per minute from the Ready delta between two checks. Positive means
 * the queue is shrinking. Null when the two checks are the same instant.
 */
export function netPerMinute(readyDelta: number | null | undefined, elapsedMs: number | null | undefined): number | null {
  if (readyDelta == null || elapsedMs == null || elapsedMs <= 0) return null
  return -readyDelta / (elapsedMs / 60_000)
}

/** Minutes to empty at the net rate; null when the queue is not shrinking. */
export function etaMinutesAtNet(readyNow: number, net: number | null): number | null {
  if (net == null || net <= 0 || readyNow <= 0) return null
  return readyNow / net
}
