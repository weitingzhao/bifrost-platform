import type { IngestQueueKindCount, IngestScheduleSlot } from '@/api/marketDataPlugin'
import { iterCronFiresUtc } from '@/lib/patrol/cronSchedule'

export const SWIMLANE_PAST_MS = 24 * 60 * 60 * 1000
export const SWIMLANE_FUTURE_MS = 6 * 60 * 60 * 1000

export function resolveHorizon(
  nowMs: number,
  horizon?: { start?: string; end?: string } | null,
): { startMs: number; endMs: number } {
  const start = horizon?.start != null ? Date.parse(horizon.start) : Number.NaN
  const end = horizon?.end != null ? Date.parse(horizon.end) : Number.NaN
  return {
    startMs: Number.isFinite(start) ? start : nowMs - SWIMLANE_PAST_MS,
    endMs: Number.isFinite(end) ? end : nowMs + SWIMLANE_FUTURE_MS,
  }
}

export function rangePct(t: number, startMs: number, endMs: number): number {
  if (endMs <= startMs) return 0
  return ((t - startMs) / (endMs - startMs)) * 100
}

export function clipBar(
  barStart: number,
  barEnd: number,
  viewStart: number,
  viewEnd: number,
): { leftPct: number; widthPct: number } | null {
  const a = Math.max(barStart, viewStart)
  const b = Math.min(barEnd, viewEnd)
  if (!(b > a)) return null
  return {
    leftPct: rangePct(a, viewStart, viewEnd),
    widthPct: rangePct(b, viewStart, viewEnd) - rangePct(a, viewStart, viewEnd),
  }
}

export function hourTickTimes(startMs: number, endMs: number, stepHours = 3): number[] {
  const start = new Date(startMs)
  start.setUTCMinutes(0, 0, 0)
  const aligned = start.getUTCHours() - (start.getUTCHours() % stepHours)
  start.setUTCHours(aligned)
  let t = start.getTime()
  const step = stepHours * 60 * 60 * 1000
  while (t < startMs) t += step
  const out: number[] = []
  while (t <= endMs) {
    out.push(t)
    t += step
  }
  return out
}

export function resolveFires(
  slot: IngestScheduleSlot,
  viewStart: number,
  viewEnd: number,
): number[] {
  const fromApi = (slot.fires_in_window ?? [])
    .map(iso => Date.parse(iso))
    .filter(t => Number.isFinite(t) && t >= viewStart && t < viewEnd)
  if (fromApi.length > 0) return fromApi
  if (slot.cron) {
    return iterCronFiresUtc(slot.cron, new Date(viewStart), new Date(viewEnd)).map(d => d.getTime())
  }
  const fallback = [slot.last_fire, ...(slot.next_fires ?? [])]
    .filter((iso): iso is string => iso != null && iso !== '')
    .map(iso => Date.parse(iso))
    .filter(t => Number.isFinite(t) && t >= viewStart && t < viewEnd)
  return fallback
}

export function resolveDrain(
  slot: IngestScheduleSlot,
  kindCounts: IngestQueueKindCount[],
  nowMs: number,
): { startMs: number; endMs: number; active: boolean } | null {
  const drain = slot.drain
  if (drain?.started_at) {
    const start = Date.parse(drain.started_at)
    if (!Number.isFinite(start)) return null
    const ended = drain.ended_at != null ? Date.parse(drain.ended_at) : Number.NaN
    const active = Boolean(drain.active)
    const end = active || !Number.isFinite(ended) ? nowMs : ended
    if (end <= start) return { startMs: start, endMs: start, active }
    return { startMs: start, endMs: end, active }
  }

  const kinds = slot.evidence_kinds ?? []
  let pending = 0
  let running = 0
  for (const kind of kinds) {
    const row = kindCounts.find(k => k.kind === kind)
    pending += row?.pending ?? 0
    running += row?.running ?? 0
  }
  const jw = slot.jobs_in_window
  const active =
    pending + running > 0 || (jw?.pending ?? 0) + (jw?.running ?? 0) > 0
  if (!active) return null
  const start = slot.last_fire != null ? Date.parse(slot.last_fire) : Number.NaN
  if (!Number.isFinite(start)) return null
  return { startMs: start, endMs: nowMs, active: true }
}

export function swimlaneSlots(slots: IngestScheduleSlot[]): IngestScheduleSlot[] {
  return slots.filter(s => s.adherence !== 'migrated')
}

export type ScheduleAdherenceFilter = 'all' | 'on_plan' | 'due' | 'missed'

export function slotAdherenceBucket(
  adherence: string | undefined,
): ScheduleAdherenceFilter | 'other' {
  const a = (adherence ?? '').toLowerCase()
  if (a === 'on_plan') return 'on_plan'
  if (a === 'due' || a === 'draining') return 'due'
  if (a === 'missed') return 'missed'
  return 'other'
}

export function slotMatchesAdherenceFilter(
  slot: IngestScheduleSlot,
  filter: ScheduleAdherenceFilter,
): boolean {
  if (filter === 'all') return true
  return slotAdherenceBucket(slot.adherence) === filter
}

export function filterScheduleSlots(
  slots: IngestScheduleSlot[],
  filter: ScheduleAdherenceFilter,
): IngestScheduleSlot[] {
  return slots.filter(s => slotMatchesAdherenceFilter(s, filter))
}

export function scheduleLaneId(slot: string): string {
  return `md-schedule-lane-${slot}`
}

export function scheduleRowId(slot: string): string {
  return `md-schedule-row-${slot}`
}

export function toggleSlotSelection(current: string | null, next: string): string | null {
  return current === next ? null : next
}
