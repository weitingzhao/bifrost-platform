import type { IngestHistoryDay, IngestHistoryKindTotal } from '@/api/marketDataPlugin'

export type DailyVolumeDayView = {
  day: string
  label: string
  done: number
  failed: number
  pending: number
  running: number
  active: number
  total: number
  donePct: number
  failedPct: number
  activePct: number
  topKinds: { kind: string; total: number }[]
}

export function buildDailyVolumeViews(
  days: IngestHistoryDay[],
  kindFilter: string,
): { rows: DailyVolumeDayView[]; maxTotal: number } {
  const rows: DailyVolumeDayView[] = days.map(d => {
    const scoped =
      kindFilter === ''
        ? d
        : (d.by_kind ?? []).find(k => k.kind === kindFilter) ?? {
            day: d.day,
            done: 0,
            failed: 0,
            pending: 0,
            running: 0,
            total: 0,
            by_kind: [],
          }
    const done = scoped.done ?? 0
    const failed = scoped.failed ?? 0
    const pending = scoped.pending ?? 0
    const running = scoped.running ?? 0
    const active = pending + running
    const total = scoped.total ?? done + failed + active
    const denom = Math.max(total, 1)
    const topKinds = [...(d.by_kind ?? [])]
      .sort((a, b) => (b.total ?? 0) - (a.total ?? 0))
      .slice(0, 3)
      .map(k => ({ kind: k.kind, total: k.total ?? 0 }))
    return {
      day: d.day,
      label: d.day.slice(5), // MM-DD
      done,
      failed,
      pending,
      running,
      active,
      total,
      donePct: (done / denom) * 100,
      failedPct: (failed / denom) * 100,
      activePct: (active / denom) * 100,
      topKinds,
    }
  })
  const maxTotal = Math.max(1, ...rows.map(r => r.total))
  return { rows, maxTotal }
}

export function topKindChips(
  kindTotals: IngestHistoryKindTotal[],
  limit = 8,
): IngestHistoryKindTotal[] {
  return [...kindTotals].sort((a, b) => (b.total ?? 0) - (a.total ?? 0)).slice(0, limit)
}
