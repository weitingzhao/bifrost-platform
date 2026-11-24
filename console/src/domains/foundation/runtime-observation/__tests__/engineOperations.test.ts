import { afterEach, expect, it, vi } from 'vitest'
import { readOperations } from '../operations'
import { engineOperationsGet, engineReportOn, ENGINE_REPORT, runsFromReport } from '../engineOperations'

// 2026-11-24, 箭10: Signals' CI workshop reads through the engine (step 6) behind a switch, the direct read kept as
// the way back. The switch is only safe if both make the same panels from the same platform answers.
const AT = '2026-11-24T05:00:00.000Z'
const GEN = '2026-11-24T04:59:58.000Z'
type Run = { name: string; namespace: string; pipeline: string; revision?: string; status: string; reason?: string; start_time?: string; completion_time?: string }
const runs: Record<string, { reachability: string; detail: string; runs: Run[] }> = {
  'bifrost-ci-platform': { reachability: 'ok', detail: '', runs: [
    { name: 'ci-platform-a', namespace: 'cicd', pipeline: 'bifrost-ci-platform', revision: 'abc', status: 'Succeeded', reason: 'Succeeded', start_time: '2026-11-24T04:00:00Z', completion_time: '2026-11-24T04:10:00Z' },
    { name: 'ci-platform-b', namespace: 'cicd', pipeline: 'bifrost-ci-platform', status: 'Running', start_time: '2026-11-24T04:50:00Z' },
    { name: 'ci-platform-old', namespace: 'cicd', pipeline: 'bifrost-ci-platform', status: 'Failed', reason: 'PipelineRunTimeout', start_time: '2026-11-22T01:00:00Z', completion_time: '2026-11-22T02:00:00Z' }] },
  'bifrost-ci-infra': { reachability: 'degraded', detail: 'partial list: tekton results slow', runs: [] },
}
const platform = async (path: string): Promise<unknown> => {
  if (path === '/api/v1/delivery/pipelines') return { pipelines: [{ name: 'bifrost-ci-platform' }, { name: 'bifrost-ci-infra' }, { name: 'bifrost-ci-gone' }, { name: 'bifrost-deliver-stg' }], generated_at: GEN }
  const m = /pipelines\/([^/]+)\/runs$/.exec(path)
  if (m && runs[decodeURIComponent(m[1])]) return { cluster_id: 'k3s', namespace: 'cicd', pipeline: decodeURIComponent(m[1]), ...runs[decodeURIComponent(m[1])], generated_at: GEN }
  if (m) throw new Error('HTTP 502')
  if (path === '/api/v1/catalog/status' || path === '/api/v1/data-husbandry') throw new Error('not in this test')
  throw new Error('unexpected ' + path)
}
/** The engine for the same platform (bifrost-engine src/report.ts pipelineReport and readReport, 9fe0ee1; deliveryReads). */
const engine = async (path: string): Promise<unknown> => {
  if (path === '/engine/v1/delivery?read=pipelines') return { version: 1, evidence: [{ source: '/api/v1/delivery/pipelines', outcome: 'read', receivedAt: AT }], data: await platform('/api/v1/delivery/pipelines') }
  const pipeline = new URLSearchParams(path.split('?')[1]).get('pipeline') ?? ''
  const source = `/api/v1/delivery/pipelines/${encodeURIComponent(pipeline)}/runs`
  const d = runs[pipeline]
  if (!d) return { version: 1, evidence: [{ source, outcome: 'error', receivedAt: AT, detail: 'HTTP 502' }], data: null }
  const evidence = [{ source, receivedAt: AT, evaluatedAt: GEN, outcome: 'read', detail: `Source reachability: ${d.reachability}. ${d.detail}. Refresh deadline not supplied.` }]
  const head = ['Run', 'Namespace', 'Pipeline', 'Revision', 'Status', 'Reason', 'Started at', 'Completed at'].map(label => ({ label }))
  const rows = d.runs.map(r => ({ key: r.name, cells: [r.name, r.namespace, r.pipeline, r.revision ?? 'Not supplied', r.status, r.reason ?? 'Not supplied', r.start_time ?? 'Not supplied', r.completion_time ?? 'Not supplied'] }))
  return { version: 1, evidence: [{ source, outcome: 'read', receivedAt: AT }], data: { version: 1, id: JSON.stringify(['pipeline-report', 'k3s', pipeline, AT]), capturedAt: AT,
    scope: `k3s / cicd / ${pipeline}; retained runs only`, evidence, objects: {},
    panels: [{ id: 'runs', label: 'Pipeline runs', blocks: [{ kind: 'note', text: 'Source status is not proof of deployment.' }, { kind: 'table', head, rows }] }] } }
}
afterEach(() => { vi.useRealTimers() })

it('makes the same CI workshop as the direct read from the same platform answers', async () => {
  vi.useFakeTimers({ toFake: ['Date'] }); vi.setSystemTime(new Date(AT))
  const direct = await readOperations(platform)
  const viaEngine = await readOperations(engineOperationsGet(engine, platform))
  expect(viaEngine.panels.ci).toEqual(direct.panels.ci)
  expect(viaEngine.evidence).toEqual(direct.evidence)
  expect(direct.panels.ci.map(p => p.label)).toEqual(['platform', 'infra', 'gone'])
})

it('says a report not in the pipeline report shape is an error, not an empty list', async () => {
  const report = ((await engine('/engine/v1/report?kind=pipeline&pipeline=bifrost-ci-platform')) as { data: Parameters<typeof runsFromReport>[0] }).data
  expect(() => runsFromReport({ ...report, evidence: [{ ...report.evidence[0], detail: 'something else' }] }, 'bifrost-ci-platform')).toThrow('pipeline report shape')
  expect(() => runsFromReport({ ...report, panels: [] }, 'bifrost-ci-platform')).toThrow('pipeline report shape')
})

it('reads through the engine only where switched on: STG, for the comparison; dev and prod read straight', () => {
  expect(ENGINE_REPORT).toEqual({ stg: true })
  expect(engineReportOn('stg')).toBe(true)
  expect(engineReportOn(' STG ')).toBe(true)
  for (const env of ['dev', 'prod', 'dev-local', '', undefined, 'constructor']) expect(engineReportOn(env)).toBe(false)
})
