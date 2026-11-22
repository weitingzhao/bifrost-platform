import type { ObservationSnapshot, ReadEvidence } from '@bifrost/ui/city/observation'
import type { Place } from '@bifrost/ui/city/places'

/** Explicitly synthetic acceptance cases. Never mixed into the live snapshot. */
export function qualityFixture(): ObservationSnapshot {
  const now = Date.now(), at = new Date(now).toISOString()
  const evidence: ObservationSnapshot['evidence'] = {}
  const children: Place[] = ['zero', 'missing', 'failed', 'stale', 'unknown-time'].map(id => {
    const e: ReadEvidence = { source: 'fixture:' + id, receivedAt: at, evaluatedAt: id === 'unknown-time' ? undefined : new Date(now - (id === 'stale' ? 120000 : 1000)).toISOString(), expiresAt: new Date(now + (id === 'stale' ? -60000 : 60000)).toISOString(), outcome: id === 'missing' ? 'missing' : id === 'failed' ? 'error' : 'read', detail: id === 'failed' ? 'Simulated HTTP 503' : 'Synthetic boundary case; not a device' }
    evidence[id] = [e]; evidence[id + '/cpu'] = [e]
    return { id, kind: 'building', shape: '4x4', label: id, question: 'Quality acceptance case', dashboard: null, state: id === 'missing' || id === 'failed' ? 'unread' : 'online', children: [{ id: 'cpu', kind: 'room', shape: '4x4', label: 'CPU', question: 'Synthetic value', dashboard: null, meter: { used: id === 'missing' || id === 'failed' ? null : 0, of: 8, unit: 'cores', source: e.source }, children: [] }] }
  })
  return { version: 1, id: 'quality-fixture', capturedAt: at, provenance: 'fixture', evidence, content: { tree: { id: 'town', kind: 'town', shape: '8x8', label: 'Quality fixture', question: 'Can uncertainty be distinguished?', dashboard: null, children }, alerts: { source: 'fixture:alerts', why: 'Simulated read failure; absence is not healthy' } } }
}
