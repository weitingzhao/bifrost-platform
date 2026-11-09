import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { readBoundaries, ruleRequest, type BoundarySnapshot } from '@/api/deliveryBoundaries'
import { fetchSelfHealth } from '@/api/core'
import { createApprovalRequest } from '@/api/approvals'
import { DeliveryBoundaries } from '../DeliveryBoundaries'

vi.mock('@/api/deliveryBoundaries', () => ({ readBoundaries: vi.fn(), ruleRequest: vi.fn() }))
vi.mock('@/api/core', () => ({ fetchSelfHealth: vi.fn() }))
vi.mock('@/api/approvals', () => ({ createApprovalRequest: vi.fn() }))
vi.mock('../DeliveryConfiguration', () => ({ DeliveryConfiguration: () => null }))
vi.mock('../BoundaryTaskCheck', () => ({ BoundaryTaskCheck: () => null }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const current = { rule_id: 'concurrency', rule_version: 1, per_host: 3 }
const draft = { ...current, rule_version: 2, per_host: 5 }
const snapshot: BoundarySnapshot = {
  as_of: '2026-11-09T03:00:00Z', rules: [{ rule_id: 'concurrency', in_force: current, latest: current, draft, status: 'in_force', notes: [] }],
  tuning: [], landing: [], hosts: [], pools: [], code_limits: [], disk_floor_bytes: 15 * 2 ** 30, unknown: ['Current disk reading is unknown.'],
}
function setup(env = 'prod') {
  vi.mocked(readBoundaries).mockResolvedValue(structuredClone(snapshot))
  vi.mocked(fetchSelfHealth).mockResolvedValue({ viewer_env: env, generated_at: snapshot.as_of, overall: 'ok', probes: [] })
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  render(<QueryClientProvider client={client}><DeliveryBoundaries /></QueryClientProvider>)
  return client
}
it('opens without writes, exposes unknowns, and invalidates preview on an edit', async () => {
  setup()
  fireEvent.click(await screen.findByRole('button', { name: 'Inspect / edit' }))
  expect(ruleRequest).not.toHaveBeenCalled()
  expect(createApprovalRequest).not.toHaveBeenCalled()
  expect(screen.getByText('Current disk reading is unknown.')).toBeInTheDocument()
  vi.mocked(ruleRequest).mockResolvedValue({ rule_id: 'concurrency', rule_version: 2, previous_version: 1, apply_via: 'capacity_approval', body: draft })
  fireEvent.click(screen.getByRole('button', { name: 'Preview and validate' }))
  await screen.findByRole('button', { name: 'Request capacity change' })
  fireEvent.change(screen.getByRole('spinbutton', { name: 'per_host' }), { target: { value: '8' } })
  expect(screen.queryByRole('button', { name: 'Request capacity change' })).not.toBeInTheDocument()
  expect(createApprovalRequest).not.toHaveBeenCalled()
})
it('rechecks before requesting a card and never presents pending as applied', async () => {
  setup()
  vi.mocked(ruleRequest).mockImplementation(async (_id, action) => action === 'proposals' ? { text_artifact_id: 'sha256:fixture' } : { rule_id: 'concurrency', rule_version: 2, previous_version: 1, apply_via: 'capacity_approval', body: draft })
  vi.mocked(createApprovalRequest).mockResolvedValue({ kind: 'pending', id: 'appr_fixture', action: 'agentrt_rule_approve', tier: 'D', status: 'pending' })
  fireEvent.click(await screen.findByRole('button', { name: 'Inspect / edit' }))
  fireEvent.click(screen.getByRole('button', { name: 'Preview and validate' }))
  fireEvent.change(await screen.findByLabelText('Reason'), { target: { value: 'Adjust capacity' } })
  fireEvent.click(screen.getByRole('button', { name: 'Request capacity change' }))
  await screen.findByText('Request recorded. The rule is unchanged until approved and applied.')
  expect(vi.mocked(ruleRequest).mock.calls.map(call => call[1])).toEqual(['check', 'check', 'proposals'])
  expect(screen.getByRole('link', { name: 'Open rule approval' })).toHaveAttribute('href', '#approvals?id=appr_fixture')
  expect(screen.getByRole('button', { name: 'Request capacity change' })).toBeDisabled()
})
it('blocks submission from a non-PROD seat', async () => {
  setup('stg')
  fireEvent.click(await screen.findByRole('button', { name: 'Inspect / edit' }))
  await waitFor(() => expect(screen.getByRole('button', { name: 'Preview and validate' })).toBeDisabled())
  expect(ruleRequest).not.toHaveBeenCalled()
})

it('separates boundary categories without creating changes', async () => {
  setup()
  await screen.findByRole('region', { name: 'Runtime rules' })
  expect(screen.queryByRole('region', { name: 'Resource observations' })).not.toBeInTheDocument()
  fireEvent.click(screen.getByRole('button', { name: 'Resources & quota' }))
  expect(screen.getByRole('region', { name: 'Resource observations' })).toBeInTheDocument()
  expect(screen.queryByRole('region', { name: 'Runtime rules' })).not.toBeInTheDocument()
  expect(ruleRequest).not.toHaveBeenCalled()
  expect(createApprovalRequest).not.toHaveBeenCalled()
})
