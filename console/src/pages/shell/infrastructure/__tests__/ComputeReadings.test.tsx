import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryMetricResult, TelemetryOverviewResponse } from '@/api/clusterTypes'
import { ComputeReadings } from '../ComputeReadings'
import { NOT_MEASURED, formatReading, readingValues } from '../computeReadingsModel'

const fetchComputeReadings = vi.fn<() => Promise<TelemetryOverviewResponse>>()
vi.mock('@/api/telemetry', () => ({ fetchComputeReadings: () => fetchComputeReadings() }))
afterEach(() => { cleanup(); fetchComputeReadings.mockReset() })

// The shape the platform returns for /telemetry/overview?group=compute
// (read from the running code against Prometheus on 2026-11-13).
const load: TelemetryMetricResult = {
  id: 'compute_load_per_cpu_p99', title: 'Load per CPU thread per host, p99', unit: 'x', target: '<= 1.5',
  note: '1-minute load over the thread count.', status: 'ok',
  points: [
    { labels: { nodename: 'ubt-k3s-02' }, value: 0.895 },
    { labels: { nodename: 'ubt-k3s-06' }, value: 5.576137 },
  ],
}
const throttled: TelemetryMetricResult = {
  id: 'compute_cpu_throttled_top', title: 'CPU-throttled containers, worst five', unit: 'ratio', target: '0 (no CPU limit)',
  note: 'Share of periods cut short.', status: 'ok',
  points: [{ labels: { namespace: 'monitoring', container: 'grafana' }, value: 0.91346 }],
}
const pending: TelemetryMetricResult = {
  id: 'compute_pods_pending_avg', title: 'Pods waiting for a node, average', unit: 'pods', target: '0', note: 'Work waiting.',
  status: 'ok', points: [{ labels: {}, value: 0.037698 }],
}
const broken: TelemetryMetricResult = {
  id: 'compute_oom_kills', title: 'Out-of-memory kills per host', unit: 'kills', target: '0', note: 'Killed for memory.',
  status: 'error', detail: 'prometheus: timeout', points: [],
}
const empty: TelemetryMetricResult = { ...broken, id: 'compute_pods_evicted', title: 'Evicted pods still listed', status: 'empty', detail: 'no series returned' }

function show() {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return render(<QueryClientProvider client={client}><ComputeReadings /></QueryClientProvider>)
}

describe('compute readings', () => {
  it('formats by unit', () => {
    expect(formatReading(0.91346, 'ratio')).toBe('91.3%')
    expect(formatReading(5.576137, 'x')).toBe('5.58×')
    expect(formatReading(5.1, 's')).toBe('5.1 s')
    expect(formatReading(7.00007, 'kills')).toBe('7')
    expect(formatReading(0.037698, 'pods')).toBe('0.04')
    expect(formatReading(Number.NaN, 'x')).toBe('—')
  })
  it('lists a reading highest first with the host or container it belongs to', () => {
    expect(readingValues(load)).toBe('ubt-k3s-06 5.58× · ubt-k3s-02 0.90×')
    expect(readingValues(throttled)).toBe('monitoring/grafana 91.3%')
    expect(readingValues(pending)).toBe('0.04')
    expect(readingValues(broken)).toBe('Could not be read: prometheus: timeout')
    expect(readingValues(empty)).toBe('No series')
  })
  it('shows each reading with its target and note, a failed one as failed, and names what is not measured', async () => {
    fetchComputeReadings.mockResolvedValue({ namespace: 'bifrost-stg', metrics: [pending, load, broken], generated_at: '2026-11-13T07:00:00Z' })
    const view = show()
    const row = (await screen.findByText('Load per CPU thread per host, p99')).closest('tr')!
    expect(row).toHaveTextContent('ubt-k3s-06 5.58×')
    expect(row).toHaveTextContent('<= 1.5')
    expect(row).toHaveTextContent('1-minute load over the thread count.')
    const failed = view.container.querySelector('[data-reading="compute_oom_kills"]')!
    expect(failed.getAttribute('data-status')).toBe('error')
    expect(failed).toHaveTextContent('Could not be read: prometheus: timeout')
    expect(view.container.querySelectorAll('[data-reading="not-measured"]')).toHaveLength(NOT_MEASURED.length)
    expect(screen.getByRole('link', { name: 'Crew › Capacity and cost' })).toHaveAttribute('href', '#crew?section=capacity')
  })
  it('says so when the readings cannot be loaded, and still names what is not measured', async () => {
    fetchComputeReadings.mockRejectedValue(new Error('compute readings: HTTP 503'))
    const view = show()
    expect(await screen.findByText(/Compute readings could not be loaded: compute readings: HTTP 503/)).toBeInTheDocument()
    expect(view.container.querySelectorAll('[data-reading="not-measured"]')).toHaveLength(NOT_MEASURED.length)
  })
})
