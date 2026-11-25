import type { AlertsRead, CityAlert, Severity } from '@bifrost/ui/city/alerts'
import type { Place } from '@bifrost/ui/city/places'
import type { HardwareReads } from './hardware'
import { counted, gradeOf } from '@/api/alertCount'

/**
 * The monitoring's firing alerts, placed in the City (ISSUE-9: the utilities read the metrics line's
 * judgment, not one of the picture's own; LAW-8). Read from GET /api/v1/telemetry/alerts (Prometheus's own alert
 * rules, owned by the metrics line, k8s/monitoring). Nothing here judges: an alert is shown as its rule says it.
 *
 * Placed only on an exact match, never a guess: an alert's node label is a device's cluster node, its host label
 * is a device's id, or (outside any namespace: a host's own alert) its instance's address is a device's address. Anything else is listed as not placed (a
 * module's alerts wait for the map's workloads to say whose they are).
 */
export type PromAlert = { labels: Record<string, string>; annotations?: Record<string, string>; state: string; active_at?: string }

/** A device's keys, as the hardware read gives them: its id, its address, its cluster node. */
export type DeviceKey = { id: string; ip: string | null; node: string | null }

export function deviceKeys(r: HardwareReads): DeviceKey[] {
  const nodeOfIp = new Map((r.prom.nodeIp ?? []).map(p => [p.labels?.internal_ip ?? '', p.labels?.node ?? '']))
  return (r.topology ?? []).map(n => {
    const ip = n.host && n.host !== '127.0.0.1' ? n.host : null
    return { id: n.id, ip, node: ip ? nodeOfIp.get(ip) || null : null }
  })
}

const addressOf = (instance: string | undefined) => instance?.replace(/:\d+$/, '') ?? ''

/** The device an alert's labels name, exactly, or null. */
export function deviceOf(labels: Record<string, string>, devices: readonly DeviceKey[]): string | null {
  // An alert in a namespace is about a workload: its instance is only the target scraped (a kubelet, an exporter
  // on that machine), so its address does not place it; its node does (where the workload stands).
  const ip = labels.namespace ? '' : addressOf(labels.instance) || labels.internal_ip || ''
  const host = (labels.host ?? '').toLowerCase()
  const d = devices.find(x => (labels.node && x.node === labels.node) || (ip && x.ip === ip) || (host && x.id.toLowerCase() === host))
  return d?.id ?? null
}

/**
 * The firing alerts as the City shows them: the ones the metrics line's rule counts (api/alertCount.ts: firing,
 * not severity none, not a drill), each under its grade, so the corner's count is the Console's.
 * `at` is the device's path in the tree (the district that holds it, then the device), or null.
 */
export function cityAlerts(raw: readonly PromAlert[], devices: readonly DeviceKey[], tree: Place): CityAlert[] {
  const districtOf = new Map<string, string>()
  for (const d of tree.children) for (const b of d.children) districtOf.set(b.id, d.id)
  return raw.filter(counted).map(a => {
    const dev = deviceOf(a.labels, devices)
    const district = dev ? districtOf.get(dev) : undefined
    const sev: Severity = gradeOf(a)
    const id = `${a.labels.alertname}|${Object.entries(a.labels).sort(([x], [y]) => x.localeCompare(y)).map(([k, v]) => `${k}=${v}`).join(',')}`
    return { id, name: a.labels.alertname ?? 'unnamed', severity: sev, summary: a.annotations?.summary ?? '', at: dev && district ? [district, dev] : null, since: a.active_at }
  })
}

export const ALERTS_SOURCE = '/api/v1/telemetry/alerts'

/** What the content hands the engine: the alerts, or why they were not read. */
export function alertsRead(raw: readonly PromAlert[] | undefined, error: unknown, devices: readonly DeviceKey[], tree: Place): AlertsRead {
  if (raw) return { alerts: cityAlerts(raw, devices, tree), source: ALERTS_SOURCE }
  const why = error instanceof Error ? error.message : error ? String(error) : 'reading'
  return { source: ALERTS_SOURCE, why }
}
