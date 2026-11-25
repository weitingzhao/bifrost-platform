import type { ReadEvidence, SignalTrend } from '@bifrost/ui/city/observation'

type Get = (path: string) => Promise<unknown>
type Points = { points?: { labels?: Record<string, string>; value: number }[] }
const queryPath = (q: string) => '/api/v1/telemetry/promql?q=' + encodeURIComponent(q)

/**
 * The control room's trends (箭10 2026-11-24): real history only, from Prometheus recording rules. The platform's
 * PromQL route answers instant vectors, so a history is one instant query per point with `offset` on the selector:
 * every point is a value Prometheus kept, never interpolated. A point whose read fails stays null (a gap).
 */
export const STEP_MINUTES = 30
export const POINTS = 13 // six hours
const HISTORY = [
  // warning and critical only, as the alert lamps count them (info and none alerts are not findings)
  { id: 'alerts', label: 'Warnings firing', expr: (o: string) => `count(ALERTS{alertstate="firing",severity=~"warning|critical"}${o}) or vector(0)`, unit: '' },
  { id: 'cpu', label: 'CPU busy cores', expr: (o: string) => `sum(bifrost_node:cpu_busy_cores_5m${o})`, unit: '' },
  { id: 'memory', label: 'Memory used', expr: (o: string) => `sum(bifrost_node:memory_used_bytes${o}) / sum(node_memory_MemTotal_bytes${o}) * 100`, unit: '%' },
] as const

export async function readHistory(get: Get): Promise<{ trends: SignalTrend[]; evidence: Record<string, ReadEvidence[]> }> {
  const evidence: Record<string, ReadEvidence[]> = {}
  const trends = await Promise.all(HISTORY.map(async h => {
    const offsets = Array.from({ length: POINTS }, (_, i) => (POINTS - 1 - i) * STEP_MINUTES)
    const reads: ReadEvidence[] = []
    const values = await Promise.all(offsets.map(async min => {
      const q = h.expr(min ? ` offset ${min}m` : ''), receivedAt = new Date().toISOString()
      try {
        const b = await get(queryPath(q)) as Points
        const v = b.points?.[0]?.value
        reads.push({ source: queryPath(q), receivedAt, outcome: typeof v === 'number' ? 'read' : 'missing', detail: `Prometheus value ${min} min before now` })
        return typeof v === 'number' && Number.isFinite(v) ? v : null
      } catch (e) {
        reads.push({ source: queryPath(q), receivedAt, outcome: 'error', detail: String(e) })
        return null
      }
    }))
    evidence['trend:' + h.id] = reads
    return { id: h.id, label: h.label, unit: h.unit, values, stepMinutes: STEP_MINUTES, source: h.expr('') + ` at ${STEP_MINUTES}-minute offsets` }
  }))
  return { trends, evidence }
}

export type DomainEdge = { from: string; to: string; imports: number }
/** Who imports whom between domains, at main (the map line's package graph). Shown only when a domain is opened. */
export async function readDomainEdges(get: Get): Promise<{ edges: DomainEdge[] | undefined; evidence: ReadEvidence }> {
  const source = '/api/v1/ci/domains?repo=bifrost-platform', receivedAt = new Date().toISOString()
  try {
    const b = await get(source) as { edges?: DomainEdge[]; commit?: string }
    if (!Array.isArray(b.edges)) return { edges: undefined, evidence: { source, receivedAt, outcome: 'missing', detail: 'incomplete answer: no edges' } }
    return { edges: b.edges.map(e => ({ from: e.from, to: e.to, imports: e.imports })), evidence: { source, receivedAt, outcome: 'read', detail: `package imports at ${b.commit?.slice(0, 8) ?? 'unknown commit'}` } }
  } catch (e) {
    return { edges: undefined, evidence: { source, receivedAt, outcome: 'error', detail: String(e) } }
  }
}
