import { hardwareDistrict, readHardware, HARDWARE_QUERIES, type HardwareReads } from '../domains/owner/city/gen2/content/hardware'
import { alertsRead, deviceKeys, type PromAlert } from '../domains/owner/city/gen2/content/alerts'
import { objectsIn, pathKey, type ObservationSnapshot, type ReadEvidence } from '@bifrost/ui/city/observation'

type Envelope = { body?: unknown; error?: string; receivedAt: string }
export type CapturedReads = Record<string, Envelope>
const TOPOLOGY = '/api/v1/topology?env=prod'
const ALERTS = '/api/v1/telemetry/alerts'
const queryPath = (q: string) => '/api/v1/telemetry/promql?q=' + encodeURIComponent(q)
/** Same existing reader and state adapter as City. Capture response evidence without changing their contract. */
export async function captureReads(get: (path: string) => Promise<unknown>): Promise<CapturedReads> {
  const reads: CapturedReads = {}
  const capture = async (path: string) => {
    try { const body = await get(path); reads[path] = { body, receivedAt: new Date().toISOString() }; return body }
    catch (e) { reads[path] = { error: String(e), receivedAt: new Date().toISOString() }; throw e }
  }
  await Promise.all([readHardware(capture), capture(ALERTS).catch(() => undefined)])
  return reads
}
export async function snapshotOf(reads: CapturedReads, provenance: ObservationSnapshot['provenance']): Promise<ObservationSnapshot> {
  const hardware: HardwareReads = await readHardware(async path => {
    const r = reads[path]
    if (!r || r.error) throw new Error(r?.error ?? 'Missing capture')
    return r.body
  })
  const district = hardwareDistrict(hardware)
  const tree = { id: 'town', kind: 'town' as const, shape: '8x8' as const, label: 'Runtime', question: 'What does the system report?', dashboard: null, children: [district] }
  const capturedAt = Object.values(reads).map(r => r.receivedAt).sort().at(-1) ?? new Date().toISOString()
  const evidence: ObservationSnapshot['evidence'] = {}
  const nodes = hardware.topology ?? []
  const evidenceOf = (source: string, match?: (labels: Record<string, string>) => boolean): ReadEvidence => {
    const r = reads[source]
    const body = r?.body as { points?: { labels?: Record<string, string>; timestamp?: number }[]; generated_at?: string } | undefined
    const points = body?.points?.filter(p => !match || match(p.labels ?? {}))
    const times = points?.map(p => p.timestamp).filter((t): t is number => typeof t === 'number' && Number.isFinite(t)) ?? []
    // PromQL instant-query timestamps are evaluation times, not exporter scrape timestamps.
    const evaluatedAt = points ? (points.length > 0 && times.length === points.length ? new Date(Math.min(...times) * 1000).toISOString() : undefined) : body?.generated_at
    const receivedAt = r?.receivedAt ?? capturedAt
    return { source, receivedAt, evaluatedAt, outcome: !r || r.error ? 'error' : points?.length === 0 ? 'missing' : 'read',
      // Two missed 30-second reads indicate stale display data, not unhealthy hardware.
      expiresAt: new Date(Math.min(Date.parse(receivedAt), evaluatedAt ? Date.parse(evaluatedAt) : Infinity) + 60000).toISOString(), detail: r?.error ?? (points ? 'Prometheus query evaluation; scrape freshness not supplied' : 'Platform response generation time') }
  }
  evidence[''] = [evidenceOf(TOPOLOGY)]
  evidence.utilities = evidence['']
  evidence['@alerts'] = [evidenceOf(ALERTS)]
  const keys = deviceKeys(hardware)
  for (const row of objectsIn(tree)) {
    const node = nodes.find(n => n.id === row.path[1])
    const device = keys.find(k => k.id === node?.id)
    const instance = node?.host ? `${node.host}:9100` : ''
    const match = (l: Record<string,string>) => !!((instance && l.instance === instance) || (device?.node && l.node === device.node))
    const stateKeys = device?.node ? ['nodeIp', 'ready', 'cordoned', 'wakeable'] as const : ['up'] as const
    const stateReads = [evidenceOf(TOPOLOGY), ...stateKeys.map(k => evidenceOf(queryPath(HARDWARE_QUERIES[k]), k === 'nodeIp' ? l => l.internal_ip === node?.host : match))]
    // Optional standby indicators are not required when Ready is true; do not turn absent optional series into failed health.
    const stateEvidence = row.place.state === 'online' && device?.node ? stateReads.slice(0, 3) : stateReads
    if (row.place.kind === 'building') evidence[pathKey(row.path)] = stateEvidence
    else if (row.place.kind === 'room') {
      const source = row.place.meter?.source
      const capacity = row.place.id === 'cpu' ? 'cores' : row.place.id === 'memory' ? 'memTotal' : 'diskSize'
      const volume = row.place.id === 'disk' ? '/' : row.place.label.startsWith('Volume ') ? '/volume' + row.place.label.split(' ')[1] : undefined
      const roomMatch = (l: Record<string,string>) => match(l) && (!volume || l.mountpoint === volume)
      evidence[pathKey(row.path)] = source && Object.values(HARDWARE_QUERIES).includes(source)
        ? [...stateEvidence, evidenceOf(queryPath(source), roomMatch), evidenceOf(queryPath(HARDWARE_QUERIES[capacity]), roomMatch)]
        : [{ source: source ?? 'No source supplied', receivedAt: capturedAt, outcome: 'missing', detail: 'Not measured by existing adapter' }]
    }
  }
  const alert = reads[ALERTS]
  return { version: 1, id: capturedAt, capturedAt, provenance, evidence, content: { version: 2, tree,
    alerts: alertsRead((alert?.body as { alerts?: PromAlert[] } | undefined)?.alerts, alert?.error, keys, tree) } }
}
