import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, render, screen, within } from '@testing-library/react'
import { afterEach, expect, it, vi } from 'vitest'
import { CapabilityEvidence } from '../CapabilityEvidence'
import { DeliveryLadderReadings } from '../DeliveryMeasurement'
import type { DeliveryReading } from '@/api/deliveryReadings'

vi.mock('../useRuntimePlan', () => ({ useRuntimeArch: () => ({ from: 'absent' }), ARCH_NOT_IMPORTED: 'Architecture not imported' }))
const baseline: DeliveryReading = { metric_id: 'lead_time_s', definition_version: 1, definition_sha256: 'a'.repeat(64), scope_kind: 'all', scope_ref: '', label: 'baseline:pre-group-0', window_kind: 'span', window_start: '2026-11-03T00:00:00Z', window_end: '2026-11-10T00:00:00Z', source: 'imported', unit: 'seconds', samples: 1, value: 9720, stats: {}, conclusive: false }
const wave = { ...baseline, label: 'group:M-c', source: 'computed', definition_version: 2, value: 3600, stats: { wave_open_to_prod_s: 4200, coding: 1800, landing_ci: 600, card_wait: 300, push: 100, STG: 400, PROD: 1000 }, conclusive: true }
vi.mock('@/api/client', () => ({
  parseError: async () => 'unavailable',
  authedFetch: vi.fn(async (_prefix: string, url: string) => {
    const u = new URL(url, 'http://localhost')
    if (u.pathname.endsWith('/compare')) return { ok: true, json: async () => ({ metric_id: 'lead_time_s', baseline, current: wave, same_definition: false, conclusive: false, change: -6120 }) }
    const metric = u.searchParams.get('metric')
    const readings = metric === 'lead_time_s' ? [wave, baseline, { ...wave, label: 'group:M-a', conclusive: false }, { ...wave, label: 'group:M-374' }] : metric === 'owner_wait_share' ? [{ ...baseline, metric_id: metric, unit: 'ratio', value: 0.38, stats: { owner_cards: 147, days: 7 } }] : metric === 'ci_runs_per_commit' ? [{ ...wave, metric_id: metric, unit: 'count', value: 1.2, stats: { reuses: 3 } }] : []
    return { ok: true, json: async () => ({ readings }) }
  }),
}))
afterEach(cleanup)
function show(child: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false, gcTime: 0 } } })
  return render(<QueryClientProvider client={client}>{child}</QueryClientProvider>)
}
it('renders Capability delivery from stored API readings, comparisons, stages and CI reuses', async () => {
  show(<CapabilityEvidence />)
  const section = screen.getByRole('region', { name: 'Delivery measurement' })
  expect(await within(section).findByText(/Start to PROD: 2.7 h/)).toHaveTextContent('imported · not conclusive')
  expect(await within(section).findAllByText(/definitions differ/)).not.toHaveLength(0)
  expect(within(section).getByRole('region', { name: 'group:M-c' })).toHaveTextContent('reuses: 3')
  expect(within(section).getByRole('region', { name: 'group:M-c' })).toHaveTextContent('wave_open_to_prod_s')
  expect(within(section).getByRole('region', { name: 'group:M-a' })).toHaveTextContent('not conclusive')
  expect(within(section).getByRole('region', { name: 'group:M-b' })).toHaveTextContent('No stored readings')
  expect(within(section).getByRole('region', { name: 'group:M-374' })).toHaveTextContent('1 h')
  expect(section).toHaveTextContent('raw data')
  expect(section).toHaveTextContent('Owner cards: 147 in 7 days')
  expect(within(section).getByRole('region', { name: 'group:M-c' })).toHaveTextContent('Coding: 0.5 h')
})
it('feeds the ladder measured items from the same readings', async () => {
  show(<DeliveryLadderReadings />)
  expect(await screen.findByText(/Start to PROD: 1 h · group:M-c/)).toBeInTheDocument()
  expect(screen.getByText(/CI runs per commit reaching main: 1.2/)).toBeInTheDocument()
  expect(screen.getByText(/Waiting on the Owner: 38%/)).toBeInTheDocument()
})
