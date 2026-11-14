import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ actions: vi.fn(), stop: vi.fn(), lift: vi.fn(), thread: vi.fn(), ack: vi.fn(), send: vi.fn(), caps: vi.fn(), env: 'stg' }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions, setGlobalStop: mocks.stop, liftControl: mocks.lift,
  fetchChiefThread: mocks.thread, ackMessages: mocks.ack, sendChiefMessage: mocks.send }))
vi.mock('@/api/cluster', async original => ({ ...await original<object>(), fetchAuthCapabilities: mocks.caps }))
// The platform's own word on which environment it is; 'loading' never answers.
vi.mock('@/api/core', async original => ({ ...await original<object>(),
  fetchSelfHealth: () => mocks.env === 'loading' ? new Promise(() => {}) : mocks.env === 'error' ? Promise.reject(new Error('down')) : Promise.resolve({ viewer_env: mocks.env }) }))
import { OwnerStop } from '../OwnerStop'
const at = '2026-10-18T07:20:01.123Z'
function show(node: React.ReactNode) {
  const client = new QueryClient({ defaultOptions: { queries: { retry: false } } })
  return { client, ...render(<QueryClientProvider client={client}>{node}</QueryClientProvider>) }
}
beforeEach(() => {
  vi.clearAllMocks(); mocks.env = 'stg'
  mocks.actions.mockResolvedValue({ attempt_id: 'a_1', actions: [], next: '', unavailable: false })
  mocks.thread.mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages: [] })
  mocks.stop.mockResolvedValue({ control_id: 'c_1' }); mocks.lift.mockResolvedValue(undefined)
  mocks.ack.mockResolvedValue({ actor: 'owner', acknowledged: ['m_2'] }); mocks.send.mockResolvedValue({ message_id: 'm_3', from: 'owner', to: 'seat:chief_of_staff' })
})
afterEach(cleanup)
describe('global stop', () => {
  const enabled = async (name: string) => waitFor(() => expect(screen.getByRole('button', { name })).toBeEnabled())
  it('disables on prod', async () => {
    mocks.env = 'prod'; show(<OwnerStop controls={[]} readable />)
    expect(await screen.findByText('Stop all and Resume are enabled on STG first')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Stop all' })).toBeDisabled()
  })
  it('stays disabled while the environment is not known', async () => {
    mocks.env = 'loading'; show(<OwnerStop controls={[]} readable />)
    await new Promise(r => setTimeout(r, 50))
    expect(screen.getByRole('button', { name: 'Stop all' })).toBeDisabled()
  })
  it('confirms once before stopping and invalidates progress', async () => {
    const { client } = show(<OwnerStop controls={[]} readable />)
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    await enabled('Stop all')
    fireEvent.click(screen.getByRole('button', { name: 'Stop all' }))
    expect(mocks.stop).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Stop all work' }))
    fireEvent.click(screen.queryByRole('button', { name: 'Stop all work' }) ?? document.body)
    await waitFor(() => expect(mocks.stop).toHaveBeenCalledExactlyOnceWith('Owner: stop all from the Console'))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['agent-runtime-progress'] })
  })
  it('allows cancelling on dev without stopping', async () => {
    mocks.env = 'dev'; show(<OwnerStop controls={[]} readable />)
    await enabled('Stop all')
    fireEvent.click(screen.getByRole('button', { name: 'Stop all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(mocks.stop).not.toHaveBeenCalled()
  })
  it('lifts the active stop only', async () => {
    show(<OwnerStop readable controls={[{ control_id: 'old', scope: 'global', control_kind: 'stop', cleared_at: at },
      { control_id: 'h_1', scope: 'host', control_kind: 'stop', set_at: at, cleared_at: '' },
      { control_id: 'p_1', scope: 'global', control_kind: 'pause', set_at: at, cleared_at: '' },
      { control_id: 'c_1', scope: 'global', control_kind: 'stop', set_at: at, cleared_at: '' }]} />)
    expect(screen.getByText(/All work stopped since/)).toBeInTheDocument()
    await enabled('Resume')
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
    fireEvent.click(screen.getByRole('button', { name: 'Resume work' }))
    await waitFor(() => expect(mocks.lift).toHaveBeenCalledExactlyOnceWith('c_1'))
  })
  it('does what was pressed: a stop set elsewhere while it is open voids the confirmation', async () => {
    const { client, rerender } = show(<OwnerStop controls={[]} readable />)
    await enabled('Stop all')
    fireEvent.click(screen.getByRole('button', { name: 'Stop all' }))
    rerender(<QueryClientProvider client={client}><OwnerStop readable controls={[{ control_id: 'c_agent', scope: 'global', control_kind: 'stop', set_at: at, cleared_at: '' }]} /></QueryClientProvider>)
    expect(screen.getByText('Work control changed while you were deciding; press again.')).toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Resume work' })).not.toBeInTheDocument()
    expect(screen.queryByRole('button', { name: 'Stop all work' })).not.toBeInTheDocument()
    expect(mocks.lift).not.toHaveBeenCalled(); expect(mocks.stop).not.toHaveBeenCalled()
  })
  it('does what was pressed: Resume lifts only the stop that was in force when it was pressed', async () => {
    const c = (id: string) => ({ control_id: id, scope: 'global', control_kind: 'stop', set_at: at, cleared_at: '' })
    const { client, rerender } = show(<OwnerStop readable controls={[c('c_1')]} />)
    const again = (controls: ReturnType<typeof c>[]) => rerender(<QueryClientProvider client={client}><OwnerStop readable controls={controls} /></QueryClientProvider>)
    await enabled('Resume')
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
    for (const controls of [[c('c_2')], []]) {
      again(controls)
      expect(screen.getByText('Work control changed while you were deciding; press again.')).toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Resume work' })).not.toBeInTheDocument()
      expect(screen.queryByRole('button', { name: 'Stop all work' })).not.toBeInTheDocument()
    }
    expect(mocks.lift).not.toHaveBeenCalled()
  })
  it('says why it is disabled when the environment cannot be read', async () => {
    mocks.env = 'error'; show(<OwnerStop controls={[]} readable />)
    expect(await screen.findByText('Environment unknown (self-health did not answer); Stop all is disabled')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Stop all' })).toBeDisabled()
  })
})

