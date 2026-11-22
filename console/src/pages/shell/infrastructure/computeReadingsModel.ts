import type { TelemetryMetricResult, TelemetrySamplePoint } from '@/api/clusterTypes'

export function formatReading(value: number, unit?: string): string {
  if (!Number.isFinite(value)) return '—'
  if (unit === 'ratio') return `${(value * 100).toFixed(1)}%`
  if (unit === 'x') return `${value.toFixed(2)}×`
  if (unit === 's') return `${value.toFixed(1)} s`
  if (unit === 'ms') return `${value >= 100 ? value.toFixed(0) : value.toFixed(1)} ms`
  if (unit === 'kills' || unit === 'guests' || unit === 'alerts' || unit === 'min') return value.toFixed(0)
  return value >= 100 ? value.toFixed(0) : value.toFixed(2)
}

function pointLabel(p: TelemetrySamplePoint): string {
  const l = p.labels
  if (l.nodename != null && l.nodename !== '' && l.home != null && l.home !== '') return `${l.nodename} → ${l.home}`
  if (l.nodename != null && l.nodename !== '' && l.model != null && l.model !== '') return `${l.nodename} ${l.device ?? ''}: ${l.model} (${l.firmware_revision ?? '?'})`
  if (l.nodename != null && l.nodename !== '' && l.device != null && l.device !== '') return `${l.nodename} ${l.device}`
  if (l.node != null && l.node !== '' && l.pod != null && l.pod !== '') return `${l.node}/${l.pod}`
  if (l.nodename != null && l.nodename !== '') return l.nodename
  if (l.node != null && l.node !== '') return l.node
  if (l.alertname != null && l.alertname !== '') return l.alertname
  if (l.container != null && l.container !== '') return `${l.namespace ?? ''}/${l.container}`
  return ''
}

/** One reading's values, highest first, each with the host or container it belongs to. */
export function readingValues(m: TelemetryMetricResult): string {
  if (m.status === 'error') return `Could not be read: ${m.detail ?? 'no detail'}`
  if (m.points.length === 0) return 'No series'
  // A register or list row (unit "home" or "list") is a list, not a quantity: show who, not the value.
  if (m.unit === 'home' || m.unit === 'list') return m.points.map(pointLabel).filter(s => s !== '').sort().join(' · ')
  return [...m.points]
    .sort((a, b) => b.value - a.value)
    .map(p => {
      const label = pointLabel(p)
      return label === '' ? formatReading(p.value, m.unit) : `${label} ${formatReading(p.value, m.unit)}`
    })
    .join(' · ')
}
