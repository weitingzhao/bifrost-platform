import type { TelemetryMetricResult, TelemetrySamplePoint } from '@/api/clusterTypes'

/** Readings the design names that nothing measures yet, and what each waits for. */
export const NOT_MEASURED: { title: string; missing: string }[] = [
  { title: 'Time from asking for a result to having it, by kind of work', missing: 'Needs the CI evidence ledger to time each kind of task.' },
  { title: 'GPU node: share of awake hours with work, tokens served, time to wake', missing: 'The node has been off; nothing wakes it or records its use yet.' },
  { title: 'Times the Owner acted or was asked about compute or quota', missing: 'Nothing records it under this heading.' },
]

export function formatReading(value: number, unit?: string): string {
  if (!Number.isFinite(value)) return '—'
  if (unit === 'ratio') return `${(value * 100).toFixed(1)}%`
  if (unit === 'x') return `${value.toFixed(2)}×`
  if (unit === 's') return `${value.toFixed(1)} s`
  if (unit === 'kills') return value.toFixed(0)
  return value >= 100 ? value.toFixed(0) : value.toFixed(2)
}

function pointLabel(p: TelemetrySamplePoint): string {
  const l = p.labels
  if (l.nodename != null && l.nodename !== '') return l.nodename
  if (l.container != null && l.container !== '') return `${l.namespace ?? ''}/${l.container}`
  return ''
}

/** One reading's values, highest first, each with the host or container it belongs to. */
export function readingValues(m: TelemetryMetricResult): string {
  if (m.status === 'error') return `Could not be read: ${m.detail ?? 'no detail'}`
  if (m.points.length === 0) return 'No series'
  return [...m.points]
    .sort((a, b) => b.value - a.value)
    .map(p => {
      const label = pointLabel(p)
      return label === '' ? formatReading(p.value, m.unit) : `${label} ${formatReading(p.value, m.unit)}`
    })
    .join(' · ')
}
