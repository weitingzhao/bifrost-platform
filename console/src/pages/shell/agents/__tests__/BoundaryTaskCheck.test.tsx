import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { authedFetch } from '@/api/client'
import { fetchTaskDelivery } from '@/api/deliveryStages'
import { BoundaryTaskCheck } from '../BoundaryTaskCheck'

vi.mock('@/api/client', () => ({ authedFetch: vi.fn() }))
vi.mock('@/api/deliveryStages', () => ({ fetchTaskDelivery: vi.fn() }))
vi.mock('@/api/delivery', () => ({ fetchReleaseWindow: async () => ({ open: false }) }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const task = 't_01ARZ3NDEKTSV4RRFFQ69G5FAV'
function show() { render(<QueryClientProvider client={new QueryClient({ defaultOptions: { queries: { retry: false } } })}><BoundaryTaskCheck /></QueryClientProvider>) }
it('reads only on demand and displays the server refusal without writing', async () => {
  vi.mocked(authedFetch).mockImplementation(async () => new Response(JSON.stringify({ task_id: task, repo: 'bifrost-platform', state: 'blocked', reason: 'limit', boundary_id: 'landing.claim.limit', source: 'landing.EligibleToClaim', as_of: '2026-11-09T04:00:00Z', note: 'Not release authorization' })))
  vi.mocked(fetchTaskDelivery).mockResolvedValue({ state: 'none' })
  show()
  expect(vi.mocked(authedFetch).mock.calls.some(call => String(call[1]).includes('task_precheck'))).toBe(false)
  expect(fetchTaskDelivery).not.toHaveBeenCalled()
  fireEvent.change(screen.getByLabelText('Task ID'), { target: { value: task } })
  fireEvent.click(screen.getByRole('button', { name: 'Read task checks' }))
  await screen.findByText('blocked')
  expect(screen.getByText(/landing.claim.limit/, { selector: 'p' })).toBeInTheDocument()
  expect(vi.mocked(authedFetch).mock.calls.every(call => !call[2]?.method || call[2].method === 'GET')).toBe(true)
})
it('rejects a response for another task instead of showing eligibility', async () => {
  vi.mocked(authedFetch).mockImplementation(async () => new Response(JSON.stringify({ task_id: 'other', state: 'claim_eligible', reason: 'pass' })))
  vi.mocked(fetchTaskDelivery).mockResolvedValue({ state: 'none' })
  show()
  fireEvent.change(screen.getByLabelText('Task ID'), { target: { value: task } })
  fireEvent.click(screen.getByRole('button', { name: 'Read task checks' }))
  await screen.findByRole('alert')
  expect(screen.queryByText('claim_eligible')).not.toBeInTheDocument()
})
