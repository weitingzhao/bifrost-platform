import { afterEach, expect, it, vi } from 'vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import type { PipelineTimings } from '@/api/ciTimings'
import { runReport } from './run'
import { compareRunReports, fetchEngineRunReport, REPORT_SOURCE_BY_ENV, REPORT_SOURCE_KEY, reportSource } from './engineRun'
import { RunSourceReport } from '@/pages/shell/ci/RunSourceReport'

const wire = vi.hoisted(() => ({ read: vi.fn() }))
vi.mock('@/lib/platformAuth', () => ({ getPlatformOperatorToken: () => 'test-viewer' }))
vi.mock('@/api/serverContact', () => ({ serverFetch: (...args: unknown[]) => wire.read(...args) }))
afterEach(() => { cleanup(); wire.read.mockReset(); localStorage.clear(); viewerEnv = 'dev' })

const raw: PipelineTimings = { as_of: '2026-11-24T04:00:00Z', retained: 1, limit: 60, runs: [{ name: 'run-one', pipeline: 'bifrost-ci-platform',
  revision: 'input-sha', status: 'Succeeded', created_at: '2026-11-24T03:50:00Z', params: {}, results: [{ name: 'commit', value: 'output-sha' }],
  tasks: [{ name: 'task-one', task: 'check', status: 'Succeeded', results: [], steps: [{ name: 'check', status: 'terminated', exit_code: 0 }] }] }] }
// The engine's report of the same source, as /engine/v1/report?kind=run answers it: another read, so another id,
// capture time and receive time; everything else the same.
const engineReport = () => runReport(raw, 'bifrost-ci-platform', 'run-one', '2026-11-24T04:00:05Z')
const envelope = (data: unknown, evidence = [{ source: '/api/v1/delivery/pipelines/bifrost-ci-platform/timings', outcome: 'read', receivedAt: '2026-11-24T04:00:05Z' }]) =>
  ({ version: 1, model: 'report', capturedAt: '2026-11-24T04:00:05Z', provenance: 'live', evidence, data })
let viewerEnv = 'dev'
const answer = (url: string) => Response.json(url.startsWith('/engine/') ? envelope(engineReport()) : url.startsWith('/api/v1/self-health') ? { viewer_env: viewerEnv } : raw)
const show = () => {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><RunSourceReport pipeline="bifrost-ci-platform" run="run-one" /></QueryClientProvider>)
  fireEvent.click(screen.getByRole('button', { name: 'Read run source report' }))
  return client
}

// 2026-11-24, 箭10 (engine step 6): Ledger's CI report reads the engine, with the Console's own read kept as the way
// back and the switch off by default. Off, nothing is asked of the engine.
it('reads only the Console source while the switch is off', async () => {
  expect(reportSource()).toBe('platform')
  wire.read.mockImplementation(async (url: string) => answer(url))
  show()
  await screen.findAllByText('input-sha')
  expect(wire.read.mock.calls.map(call => String(call[0])).some(url => url.startsWith('/engine/'))).toBe(false)
})

// Same task: switched to engine, the report is the engine's and the Console's timings route is not read.
it('reads the engine report with the viewer token when switched to engine', async () => {
  localStorage.setItem(REPORT_SOURCE_KEY, 'engine')
  wire.read.mockImplementation(async (url: string) => answer(url))
  show()
  await screen.findAllByText('input-sha')
  const urls = wire.read.mock.calls.map(call => String(call[0]))
  const engineCalls = wire.read.mock.calls.filter(call => String(call[0]).startsWith('/engine/'))
  expect(urls.filter(url => !url.startsWith('/api/v1/self-health'))).toEqual(['/engine/v1/report?kind=run&pipeline=bifrost-ci-platform&run=run-one'])
  expect(new Headers(engineCalls[0][1].headers).get('Authorization')).toBe('Bearer test-viewer')
})

// Same task: the check before the switch flips — both reads, rows and evidence compared item by item.
it('compares the two reads and says equal, or lists every difference', async () => {
  localStorage.setItem(REPORT_SOURCE_KEY, 'compare')
  wire.read.mockImplementation(async (url: string) => answer(url))
  show()
  expect(await screen.findByText(/Console and engine reports are equal/)).toBeTruthy()
  const changed = engineReport()
  const table = changed.panels[0].blocks.find(block => block.kind === 'table' && block.head?.[0]?.label === 'Task run')
  if (table?.kind !== 'table') throw new Error('expected the task table')
  const edited = { ...changed, panels: [{ ...changed.panels[0], blocks: changed.panels[0].blocks.map(block => block === table
    ? { ...table, rows: table.rows.map(row => ({ ...row, cells: row.cells.map(cell => (cell === 'Succeeded' ? 'Failed' : cell)) })) } : block) }] }
  const differences = compareRunReports(runReport(raw, 'bifrost-ci-platform', 'run-one', '2026-11-24T04:00:01Z'), edited)
  expect(differences).toHaveLength(1)
  expect(differences[0]).toMatch(/^panels\[0\]\.blocks\[3\]\.rows\[.*task-one.*\]\.cells\[2\]: console "Succeeded" · engine "Failed"$/)
})

// Same task: an engine that could not read says so with its own evidence; it is never shown as an empty report.
it('turns an engine answer with no report into an error naming its evidence', async () => {
  wire.read.mockResolvedValue(Response.json(envelope(null, [{ source: '/api/v1/delivery/pipelines/p/timings', outcome: 'error', receivedAt: 'x', detail: 'viewer token required' } as never])))
  await expect(fetchEngineRunReport('p', 'r')).rejects.toThrow('/api/v1/delivery/pipelines/p/timings: error (viewer token required)')
  wire.read.mockResolvedValue(Response.json({ runs: [] }))
  await expect(fetchEngineRunReport('p', 'r')).rejects.toThrow('not an engine v1 report answer')
})

// 2026-11-24, 箭10: the engine runs on STG, so STG's cell is on; DEV and PROD stay on the Console's read (PROD has no
// engine yet), and an environment not read or not known never turns the engine on by a guess.
it('takes the source from the environment table, with the viewer override first', async () => {
  expect(REPORT_SOURCE_BY_ENV).toEqual({ dev: 'platform', stg: 'engine', prod: 'platform' })
  expect(reportSource('stg')).toBe('engine')
  expect(reportSource('PROD')).toBe('platform')
  expect(reportSource(undefined)).toBe('platform')
  expect(reportSource('dev-local')).toBe('platform')
  localStorage.setItem(REPORT_SOURCE_KEY, 'compare')
  expect(reportSource('prod')).toBe('compare')
  localStorage.clear()
  viewerEnv = 'stg'
  wire.read.mockImplementation(async (url: string) => answer(url))
  show()
  await screen.findAllByText('input-sha')
  const urls = wire.read.mock.calls.map(call => String(call[0]))
  expect(urls.some(url => url.startsWith('/engine/v1/report'))).toBe(true)
  expect(urls.some(url => url.includes('/timings'))).toBe(false)
})
