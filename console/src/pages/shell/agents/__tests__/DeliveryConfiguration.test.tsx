import '@testing-library/jest-dom/vitest'
import { cleanup, fireEvent, render, screen } from '@testing-library/react'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { afterEach, expect, it, vi } from 'vitest'
import { authedFetch } from '@/api/client'
import { submitRuntimeGoal } from '@/api/agentRuntime'
import { DeliveryConfiguration } from '../DeliveryConfiguration'

vi.mock('@/api/client', () => ({ authedFetch: vi.fn() }))
vi.mock('@/api/agentRuntime', () => ({ submitRuntimeGoal: vi.fn() }))
afterEach(() => { cleanup(); vi.resetAllMocks() })
const view = { as_of: '2026-11-09T03:00:00Z', note: 'Existing runs retain their settings.', request_history_limit: 50, requests: [], rows: [{ boundary_id: 'ci.platform.go_timeout', revision: 'base1', path: 'task.yaml', selector: 'spec/steps/@go-test/script', unit: 'duration', source_value: '900s', installed_value: '600s', state: 'source_differs_from_installed' }], preview: null }
const preview = { boundary_id: 'ci.platform.go_timeout', requested_value: '30m', previous_value: '900s', source_revision: 'base1', goal: 'Pinned configuration change', repo: 'bifrost-trade-infra' }
const response = (body: unknown) => new Response(JSON.stringify(body))
function renderPage() { const client = new QueryClient({ defaultOptions: { queries: { retry: false } } }); render(<QueryClientProvider client={client}><DeliveryConfiguration canSubmit /></QueryClientProvider>) }

it('explains unchanged values without sending a preview or a goal', async () => {
  vi.mocked(authedFetch).mockResolvedValue(response(view))
  renderPage()
  fireEvent.click(await screen.findByRole('button', { name: 'Propose change' }))
  expect(screen.getByRole('button', { name: 'Preview configuration change' })).toBeDisabled()
  expect(screen.getByText(/Enter a different value/)).toBeInTheDocument()
  expect(authedFetch).toHaveBeenCalledTimes(1)
  expect(submitRuntimeGoal).not.toHaveBeenCalled()
})

it('shows drift without writing and rechecks the source before creating one normal goal', async () => {
  vi.mocked(authedFetch).mockImplementation(async (_prefix, url) => response(String(url).includes('key=') ? { ...view, preview } : view))
  vi.mocked(submitRuntimeGoal).mockResolvedValue({ mission_id: 'M-999', mission_state: 'proposed', waiting: '', task_id: 't', repo: 'bifrost-trade-infra', repos: ['bifrost-trade-infra'], base_commit: 'base1', spec_artifact_id: 'a' })
  renderPage()
  await screen.findByText('source_differs_from_installed')
  expect(submitRuntimeGoal).not.toHaveBeenCalled()
  fireEvent.click(screen.getByRole('button', { name: 'Propose change' }))
  fireEvent.change(screen.getByLabelText(/Requested value/), { target: { value: '30m' } })
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration change' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Submit configuration change' }))
  await screen.findByRole('link', { name: 'Track M-999' })
  expect(submitRuntimeGoal).toHaveBeenCalledTimes(1)
  expect(vi.mocked(authedFetch).mock.calls.filter(call => String(call[1]).includes('key='))).toHaveLength(2)
  expect(screen.getByRole('button', { name: 'Submit configuration change' })).toBeDisabled()
  expect(screen.getByText(/installed value has not been changed/)).toBeInTheDocument()
})

it('refuses to submit a preview after the source moves', async () => {
  let reads = 0
  vi.mocked(authedFetch).mockImplementation(async (_prefix, url) => {
    if (!String(url).includes('key=')) return response(view)
    reads++
    return response({ ...view, preview: { ...preview, source_revision: reads === 1 ? 'base1' : 'base2' } })
  })
  renderPage()
  fireEvent.click(await screen.findByRole('button', { name: 'Propose change' }))
  fireEvent.change(screen.getByLabelText(/Requested value/), { target: { value: '30m' } })
  fireEvent.click(screen.getByRole('button', { name: 'Preview configuration change' }))
  fireEvent.click(await screen.findByRole('button', { name: 'Submit configuration change' }))
  await screen.findByText('The source changed. Review the updated preview before submitting.')
  expect(submitRuntimeGoal).not.toHaveBeenCalled()
})
