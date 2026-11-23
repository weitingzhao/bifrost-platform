import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, waitFor, within } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { fetchCITimings, type PipelineTimings } from '@/api/ciTimings'
import { RunTimingDetail } from '../RunTimingDetail'
import { timingRun } from './timingFixture'

vi.mock('@/api/ciTimings', () => ({ fetchCITimings: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const input: PipelineTimings = { as_of: '2026-11-23T06:00:00Z', retained: 1, limit: 60, runs: [{
  ...timingRun(), name: 'exact-run', pipeline: 'bifrost-ci-platform', revision: 'input-revision', status: 'VendorWaiting',
  results: [{ name: 'platform-commit', value: 'output-commit' }, { name: 'empty-result', value: '' }],
  tasks: [{ name: 'exact-task', task: 'check', status: 'unknown', results: [], steps: [{ name: 'check', status: 'terminated', exit_code: 0 }] }],
}] }
function setup() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  const view = (name = 'exact-run') => <QueryClientProvider client={client}><RunTimingDetail run={{ ...input.runs[0], name }} showJourney={false} /></QueryClientProvider>
  return { client, view, ...render(view()) }
}

// 2026-11-23 Console audit: exact source reading is optional and cannot replace timing/log functionality.
it('reads only the selected run on demand and preserves the original timing and log controls', async () => {
  vi.mocked(fetchCITimings).mockResolvedValue(input)
  setup()
  expect(fetchCITimings).not.toHaveBeenCalled()
  expect(screen.getByRole('button', { name: 'Read scope logs and slow tests' })).toBeInTheDocument()
  expect(screen.getByRole('region', { name: 'check steps' })).toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Read run source report' }))
  const report = within(await screen.findByRole('region', { name: 'Observation report' }))
  await waitFor(() => expect(report.getByText('input-revision')).toBeInTheDocument())
  expect(fetchCITimings).toHaveBeenCalledTimes(1)
  expect(fetchCITimings).toHaveBeenCalledWith('bifrost-ci-platform', 'exact-run', expect.any(AbortSignal))
  expect(report.getByText('output-commit')).toBeInTheDocument()
  expect(report.getByText('(empty)')).toBeInTheDocument()
  expect(report.getByText('VendorWaiting')).toBeInTheDocument()
  expect(report.getByText('Not supplied')).toBeInTheDocument()
  fireEvent.click(report.getByRole('button', { name: 'exact-task' }))
  const detail = within(report.getByRole('region', { name: 'Selected report object' }))
  expect(detail.getByText('0')).toBeInTheDocument()
  expect(detail.getAllByText('Not recorded')).toHaveLength(2)
  expect(screen.getByRole('button', { name: 'Read scope logs and slow tests' })).toBeInTheDocument()
})

// Failed refresh must expose retained-source time; a new run must not inherit the old selection/read opt-in.
it('retains source evidence on refresh failure and resets the report when the selected run changes', async () => {
  vi.mocked(fetchCITimings).mockResolvedValue(input)
  const { client, rerender, view } = setup()
  fireEvent.click(screen.getByRole('button', { name: 'Read run source report' }))
  await screen.findByRole('button', { name: 'exact-task' })
  vi.mocked(fetchCITimings).mockRejectedValue(new Error('source offline'))
  await client.refetchQueries({ queryKey: ['ci-timing-run', 'bifrost-ci-platform', 'exact-run'] })
  expect(await screen.findByRole('alert')).toHaveTextContent('Showing the previous report')
  expect(screen.getByRole('region', { name: 'Report source evidence' })).toHaveTextContent(input.as_of)
  rerender(view('another-run'))
  expect(screen.queryByRole('region', { name: 'Observation report' })).not.toBeInTheDocument()
  expect(fetchCITimings).toHaveBeenCalledTimes(2)
})

// A successful empty read is missing history, while a wrong source identity is a failed report.
it.each([false, true])('distinguishes missing history from mismatched identity (%s)', async mismatch => {
  vi.mocked(fetchCITimings).mockResolvedValue({ ...input, runs: mismatch ? [{ ...input.runs[0], name: 'wrong-run' }] : [] })
  setup()
  fireEvent.click(screen.getByRole('button', { name: 'Read run source report' }))
  if (mismatch) expect(await screen.findByRole('alert')).toHaveTextContent('identity')
  else expect(await screen.findByText('Requested run not represented in this response. History and current state are unknown.')).toBeInTheDocument()
})
