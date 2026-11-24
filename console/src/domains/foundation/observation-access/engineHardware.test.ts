import { afterEach, expect, it, vi } from 'vitest'
import { captureReads, snapshotOf } from '../runtime-observation/read'
import { HARDWARE_QUERIES } from '@/domains/owner/city/gen2/content/hardware'
import { capturedFromEngine, engineHardwareOn, ENGINE_HARDWARE, type EngineHardware } from './engineHardware'

// 2026-11-24, 箭10: Signals reads the hardware through the engine (step 2) behind a switch, the direct read kept as the
// way back. The switch is only safe if both sources make the same snapshot from the same platform answers.
const AT = '2026-11-24T04:00:00.000Z'
const GENERATED = '2026-11-24T03:59:59.500Z'
const ALERTS_GENERATED = '2026-11-24T03:59:58.000Z'
const queryPath = (q: string) => '/api/v1/telemetry/promql?q=' + encodeURIComponent(q)
const TOPOLOGY = '/api/v1/topology?env=prod'
const ALERTS = '/api/v1/telemetry/alerts'
const nodes = [{ id: 'node-a', label: 'mini-pc-a', host: '10.0.0.1', in_k3s_cluster: true }, { id: 'win', label: 'Win11 Host', host: '10.0.0.9' }]
const alerts = [{ labels: { alertname: 'NodeDiskFull', instance: '10.0.0.1:9100', severity: 'warning' }, state: 'firing', active_at: AT }]
const pointsOf = (key: string) => key === 'wakeable' ? [] // a query that returns no series
  : key === 'nodeIp' ? [{ labels: { internal_ip: '10.0.0.1', node: 'ubt-k3s-02' }, value: 1, timestamp: 1795492790 }]
  : [{ labels: { instance: '10.0.0.1:9100', node: 'ubt-k3s-02', mountpoint: '/' }, value: key === 'up' ? 1 : 42, timestamp: 1795492790 }]
const keyOf = (path: string) => Object.entries(HARDWARE_QUERIES).find(([, q]) => queryPath(q) === path)?.[0]
const platform = async (path: string): Promise<unknown> => {
  if (path === TOPOLOGY) return { generated_at: GENERATED, nodes }
  if (path === ALERTS) return { generated_at: ALERTS_GENERATED, alerts }
  const key = keyOf(path)
  if (!key) throw new Error('HTTP 404')
  if (key === 'arrayDisks') throw new Error('HTTP 502') // one read fails on both sides
  return { points: pointsOf(key) }
}
/** What the engine answers for the same platform (bifrost-engine src/hardware.ts readHardware; generated_at as evaluatedAt since 20086b8). */
function engineAnswer(topologyEvaluated: string | undefined): EngineHardware {
  const keys = Object.keys(HARDWARE_QUERIES)
  const evidence: EngineHardware['evidence'] = [
    { source: TOPOLOGY, outcome: 'read', receivedAt: AT, ...(topologyEvaluated ? { evaluatedAt: topologyEvaluated } : {}) },
    { source: ALERTS, outcome: 'read', receivedAt: AT, ...(topologyEvaluated ? { evaluatedAt: ALERTS_GENERATED } : {}) },
    ...keys.map(k => k === 'arrayDisks'
      ? { source: queryPath(HARDWARE_QUERIES[k as keyof typeof HARDWARE_QUERIES]), outcome: 'error' as const, receivedAt: AT, detail: 'Error: HTTP 502' }
      : { source: queryPath(HARDWARE_QUERIES[k as keyof typeof HARDWARE_QUERIES]), outcome: pointsOf(k).length ? 'read' as const : 'missing' as const, receivedAt: AT }),
  ]
  return { version: 1, evidence, data: { topology: nodes, alerts, queries: HARDWARE_QUERIES,
    prom: Object.fromEntries(keys.map(k => [k, k === 'arrayDisks' ? null : pointsOf(k)])) } }
}
afterEach(() => { vi.useRealTimers() })

it('makes the same snapshot as the direct read from the same platform answers', async () => {
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date(AT))
  const direct = await snapshotOf(await captureReads(platform), 'live')
  const viaEngine = await snapshotOf(capturedFromEngine(engineAnswer(GENERATED)), 'live')
  expect(viaEngine).toEqual(direct)
})

it("keeps the engine's reason for a failed read and says a read the engine did not make is missing", () => {
  const answer = engineAnswer(GENERATED)
  const reads = capturedFromEngine({ ...answer, evidence: answer.evidence.filter(e => e.source !== ALERTS) })
  expect(reads[queryPath(HARDWARE_QUERIES.arrayDisks)]).toEqual({ error: 'Error: HTTP 502', receivedAt: AT })
  expect(reads[ALERTS]).toBeUndefined()
  // No data at all (the topology failed): every capture is the engine's failure, none is an empty list.
  const down = capturedFromEngine({ version: 1, data: null, evidence: [{ source: TOPOLOGY, outcome: 'error', receivedAt: AT, detail: 'platform unreachable: ECONNREFUSED' }] })
  expect(down).toEqual({ [TOPOLOGY]: { error: 'platform unreachable: ECONNREFUSED', receivedAt: AT } })
})

it('reads through the engine only where switched on: STG, for the comparison; dev and prod read straight', () => {
  expect(ENGINE_HARDWARE).toEqual({ stg: true })
  expect(engineHardwareOn('stg')).toBe(true)
  expect(engineHardwareOn(' STG ')).toBe(true)
  for (const env of ['dev', 'prod', 'dev-local', '', undefined, 'constructor']) expect(engineHardwareOn(env)).toBe(false)
})
