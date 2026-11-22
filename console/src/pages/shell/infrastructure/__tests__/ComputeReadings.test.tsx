import '@testing-library/jest-dom/vitest'
import { cleanup, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, describe, expect, it, vi } from 'vitest'
import type { TelemetryMetricResult, TelemetryOverviewResponse } from '@/api/clusterTypes'
import { ComputeReadings } from '../ComputeReadings'
import { formatReading, readingValues } from '../computeReadingsModel'

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
    // 2026-11-22 headroom and root-disk growth rows (charter R2 table and the CI line's one-off reading, now computed).
    expect(formatReading(5.137, 'cores')).toBe('5.1 cores')
    expect(formatReading(6.7, 'GiB')).toBe('6.7 GiB')
    expect(formatReading(10.82, 'GB/day')).toBe('10.8 GB/day')
    expect(formatReading(Number.NaN, 'x')).toBe('—')
  })
  it('lists a reading highest first with the host or container it belongs to', () => {
    expect(readingValues(load)).toBe('ubt-k3s-06 5.58× · ubt-k3s-02 0.90×')
    expect(readingValues(throttled)).toBe('monitoring/grafana 91.3%')
    expect(readingValues(pending)).toBe('0.04')
    expect(readingValues(broken)).toBe('Could not be read: prometheus: timeout')
    expect(readingValues(empty)).toBe('No series')
  })
  // 2026-11-20 law 43 rows: the home register, crossed pain lines and guests on the database homes
  // were only in the compute seat's notes, not on Ops (ledger step 6). Shapes read from PROD Prometheus 11-21 00:26Z.
  it('shows the home register as a list and the law 43 counts with their node or alert', () => {
    const home: TelemetryMetricResult = {
      id: 'compute_node_home_now', title: 'Whose home each node is', unit: 'home', target: 'every cluster node listed', note: '', status: 'ok',
      points: [
        { labels: { nodename: 'ubt-k3s-04', home: 'database-standby' }, value: 1 },
        { labels: { nodename: 'ubt-k3s-02', home: 'database-primary' }, value: 1 },
      ],
    }
    const guests: TelemetryMetricResult = {
      id: 'compute_guests_on_protected_now', title: 'CI guests running on the database homes', unit: 'guests', target: '', note: '', status: 'ok',
      points: [{ labels: { node: 'ubt-k3s-02' }, value: 2 }, { labels: { node: 'ubt-k3s-04' }, value: 5 }],
    }
    const pain: TelemetryMetricResult = {
      id: 'compute_pain_lines_now', title: 'Pain lines crossed now', unit: 'alerts', target: '0', note: '', status: 'ok',
      points: [{ labels: { alertname: 'ComputePainDbGuestLoad' }, value: 1 }],
    }
    const calm: TelemetryMetricResult = { ...pain, points: [{ labels: {}, value: 0 }] }
    expect(readingValues(home)).toBe('ubt-k3s-02 → database-primary · ubt-k3s-04 → database-standby')
    expect(readingValues(guests)).toBe('ubt-k3s-04 5 · ubt-k3s-02 2')
    expect(readingValues(pain)).toBe('ComputePainDbGuestLoad 1')
    expect(readingValues(calm)).toBe('0')
  })
  // 2026-11-21 main line "seen and felt": guest names, pain-line minutes and down nodes were kept by Prometheus
  // but read by hand (compute ledger, "看不到的" rows 2, 3, 6). Shapes read from PROD Prometheus 11-21 21:xxZ.
  it('shows guests and down nodes as lists and pain-line minutes as whole numbers', () => {
    const names: TelemetryMetricResult = {
      id: 'compute_guests_on_protected_list_now', title: 'Which CI guests run on the database homes now', unit: 'list', target: '', note: '', status: 'ok',
      points: [{ labels: { node: 'ubt-k3s-04', pod: 'ci-platform-x-test-api-pod' }, value: 1 }, { labels: { node: 'ubt-k3s-02', pod: 'ci-platform-y-lint-pod' }, value: 1 }],
    }
    const down: TelemetryMetricResult = {
      id: 'compute_nodes_without_home_now', title: 'Cluster nodes missing from the home register', unit: 'list', target: '', note: '', status: 'ok',
      points: [{ labels: { node: 'ubt-k3s-05' }, value: 1 }, { labels: { node: 'gpu-server' }, value: 1 }],
    }
    const minutes: TelemetryMetricResult = {
      id: 'compute_pain_minutes', title: 'Minutes each pain line fired', unit: 'min', target: '', note: '', status: 'ok',
      points: [{ labels: { alertname: 'ComputePainDbReplication' }, value: 60 }, { labels: { alertname: 'ComputePainDbGuestLoad' }, value: 2062 }],
    }
    expect(readingValues(names)).toBe('ubt-k3s-02/ci-platform-y-lint-pod · ubt-k3s-04/ci-platform-x-test-api-pod')
    expect(readingValues(down)).toBe('gpu-server · ubt-k3s-05')
    expect(readingValues(minutes)).toBe('ComputePainDbGuestLoad 2062 · ComputePainDbReplication 60')
  })
  // 2026-11-22 Owner (relayed by the hardware line): know what each disk is and how fast it really is, so a
  // fast-disk job is not placed on a slow disk. Same model, 07 vs 08: write p99 689 ms vs 4.4 ms (PROD 11-22 00:3xZ).
  it('shows each disk with its model and firmware, and write latency in milliseconds per node and disk', () => {
    const disks: TelemetryMetricResult = {
      id: 'compute_disks_now', title: 'Disks in each cluster node', unit: 'list', target: '', note: '', status: 'ok',
      points: [{ labels: { nodename: 'ubt-k3s-05', device: 'nvme1', model: 'NVME SSD 512GB', firmware_revision: 'VC400618' }, value: 1 }],
    }
    const tail: TelemetryMetricResult = {
      id: 'compute_disk_write_ms_p99', title: 'Disk write latency per node, worst 1% of the time', unit: 'ms', target: '', note: '', status: 'ok',
      points: [{ labels: { nodename: 'ubt-k3s-08', device: 'nvme0n1' }, value: 4.4 }, { labels: { nodename: 'ubt-k3s-07', device: 'nvme0n1' }, value: 689.41 }],
    }
    expect(readingValues(disks)).toBe('ubt-k3s-05 nvme1: NVME SSD 512GB (VC400618)')
    expect(readingValues(tail)).toBe('ubt-k3s-07 nvme0n1 689 ms · ubt-k3s-08 nvme0n1 4.4 ms')
  })
  // 2026-11-17 OWNER_LAW #30: the hand-written "not measured" list is gone; a reading without a series is
  // known only from the API's own status (audit row #31).
  it('shows each reading with its target and note, a failed one as failed, and an empty one as no series', async () => {
    fetchComputeReadings.mockResolvedValue({ namespace: 'bifrost-stg', metrics: [pending, load, broken, empty], generated_at: '2026-11-13T07:00:00Z' })
    const view = show()
    const row = (await screen.findByText('Load per CPU thread per host, p99')).closest('tr')!
    expect(row).toHaveTextContent('ubt-k3s-06 5.58×')
    expect(row).toHaveTextContent('<= 1.5')
    expect(row).toHaveTextContent('1-minute load over the thread count.')
    const failed = view.container.querySelector('[data-reading="compute_oom_kills"]')!
    expect(failed.getAttribute('data-status')).toBe('error')
    expect(failed).toHaveTextContent('Could not be read: prometheus: timeout')
    const none = view.container.querySelector('[data-reading="compute_pods_evicted"]')!
    expect(none.getAttribute('data-status')).toBe('empty')
    expect(none).toHaveTextContent('No series')
  })
  it('says so when the readings cannot be loaded', async () => {
    fetchComputeReadings.mockRejectedValue(new Error('compute readings: HTTP 503'))
    const view = show()
    expect(await screen.findByText(/Compute readings could not be loaded: compute readings: HTTP 503/)).toBeInTheDocument()
    expect(view.container.querySelectorAll('tbody tr')).toHaveLength(1)
  })
})
