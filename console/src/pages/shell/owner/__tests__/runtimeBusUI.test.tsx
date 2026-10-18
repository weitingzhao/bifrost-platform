import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
const mocks = vi.hoisted(() => ({ actions: vi.fn(), stop: vi.fn(), lift: vi.fn(), thread: vi.fn(), ack: vi.fn(), send: vi.fn(), env: 'stg' }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions, setGlobalStop: mocks.stop, liftControl: mocks.lift,
  fetchChiefThread: mocks.thread, ackMessages: mocks.ack, sendChiefMessage: mocks.send }))
vi.mock('@/hooks/useFleetSnapshot', () => ({ useFleetSnapshot: () => ({ viewerEnv: mocks.env, viewerEnvLoading: false }) }))
import { AttemptActionLine } from '../AttemptActionLine'
import { OwnerStop } from '../OwnerStop'
import { ChiefConversation } from '../ChiefConversation'
import { AttemptActions } from '../../progress/AttemptActions'
const at = '2026-10-18T07:20:01.123Z'
const attempt = parseRuntimeProgress({ live: [{ attempt_id: 'a_1', task_id: 't_1', mission_id: 'M-1', host_id: 'host', executor: 'exec:codex' }] }).live[0]
const action = (id: number, kind = 'run') => ({ id: `${id}-0`, kind, target: `command-${id}`, at })
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
describe('latest attempt action', () => {
  it.each([['read', 'Read'], ['edit', 'Edit'], ['run', 'Run'], ['result', 'Exit 0']])('labels %s', async (kind, label) => {
    mocks.actions.mockResolvedValue({ attempt_id: 'a_1', actions: [{ ...action(1, kind), ...(kind === 'result' ? { code: 0 } : {}) }], unavailable: false })
    show(<AttemptActionLine attempt={attempt} now={Date.parse(at)} />)
    expect(await screen.findByText(label)).toBeInTheDocument()
    expect(screen.getByText('codex · host')).toBeInTheDocument()
    expect(screen.getByTitle('command-1')).toHaveClass('truncate')
  })
  it('shows nonzero results in red and targets as plain text', async () => {
    mocks.actions.mockResolvedValue({ actions: [{ ...action(1, 'result'), code: 2, target: '<a href="https://example.org">test</a>' }] })
    show(<AttemptActionLine attempt={attempt} now={Date.parse(at)} />)
    expect(await screen.findByText('Exit 2')).toHaveStyle({ color: 'var(--color-danger)' })
    expect(screen.queryByRole('link')).not.toBeInTheDocument()
  })
  it('shows no actions yet', async () => { show(<AttemptActionLine attempt={attempt} now={0} />); expect(await screen.findByText('no actions yet')).toBeInTheDocument() })
  it('shows unavailable storage', async () => {
    mocks.actions.mockResolvedValue({ actions: [], unavailable: true })
    show(<AttemptActionLine attempt={attempt} now={0} />)
    expect(await screen.findByText('actions are not kept on this platform')).toBeInTheDocument()
  })
})
it('fetches only while open, appends with next, and keeps the newest 200', async () => {
  mocks.actions.mockResolvedValueOnce({ attempt_id: 'a_1', actions: Array.from({ length: 200 }, (_, i) => action(i)), next: '199-0' })
    .mockResolvedValueOnce({ attempt_id: 'a_1', actions: [{ ...action(200), truncated: true }], next: '200-0' })
  const { client } = show(<AttemptActions id="a_1" />)
  expect(mocks.actions).not.toHaveBeenCalled()
  const disclosure = screen.getByText('Actions').closest('details')!
  disclosure.open = true; fireEvent(disclosure, new Event('toggle'))
  expect(await screen.findByText('command-199')).toBeInTheDocument()
  await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-actions-stream', 'a_1'] }) })
  expect(mocks.actions.mock.calls[1][1]).toEqual({ after: '199-0', limit: 200 })
  expect(screen.getAllByRole('listitem')).toHaveLength(200)
  await waitFor(() => expect(screen.queryByText('command-0')).not.toBeInTheDocument())
  expect(screen.getByText('command-200')).toBeInTheDocument()
  expect(screen.getByText('…cut')).toBeInTheDocument()
  disclosure.open = false; fireEvent(disclosure, new Event('toggle'))
  await waitFor(() => expect(screen.queryByText('command-200')).not.toBeInTheDocument())
  await client.refetchQueries({ queryKey: ['agent-runtime-actions-stream', 'a_1'] })
  expect(mocks.actions).toHaveBeenCalledTimes(2)
})
describe('global stop', () => {
  it('disables on prod', () => { mocks.env = 'prod'; show(<OwnerStop controls={[]} readable />); expect(screen.getByRole('button', { name: 'Stop all' })).toBeDisabled(); expect(screen.getByText('Stop all is enabled on STG first')).toBeInTheDocument() })
  it('confirms once before stopping and invalidates progress', async () => {
    const { client } = show(<OwnerStop controls={[]} readable />)
    const invalidate = vi.spyOn(client, 'invalidateQueries')
    fireEvent.click(screen.getByRole('button', { name: 'Stop all' }))
    expect(mocks.stop).not.toHaveBeenCalled()
    fireEvent.click(screen.getByRole('button', { name: 'Stop all work' }))
    await waitFor(() => expect(mocks.stop).toHaveBeenCalledExactlyOnceWith('Owner: stop all from the Console'))
    expect(invalidate).toHaveBeenCalledWith({ queryKey: ['agent-runtime-progress'] })
  })
  it('allows cancelling on dev without stopping', () => {
    mocks.env = 'dev'; show(<OwnerStop controls={[]} readable />)
    fireEvent.click(screen.getByRole('button', { name: 'Stop all' }))
    fireEvent.click(screen.getByRole('button', { name: 'Cancel' }))
    expect(mocks.stop).not.toHaveBeenCalled()
  })
  it('lifts the active stop only', async () => {
    show(<OwnerStop readable controls={[{ control_id: 'old', scope: 'global', control_kind: 'stop', cleared_at: at }, { control_id: 'c_1', scope: 'global', control_kind: 'stop', set_at: at, cleared_at: '' }]} />)
    expect(screen.getByText(/All work stopped since/)).toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Resume' }))
    fireEvent.click(screen.getByRole('button', { name: 'Resume work' }))
    await waitFor(() => expect(mocks.lift).toHaveBeenCalledExactlyOnceWith('c_1'))
  })
})
describe('chief conversation', () => {
  const messages = [
    { message_id: 'm_1', from: 'owner', to: 'seat:chief_of_staff', kind: 'ask', body: 'Question', created_at: at },
    { message_id: 'm_2', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: '<b>Answer</b>', created_at: at },
    { message_id: 'm_3', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: 'Another answer', created_at: at, task_id: 't_1' },
    { message_id: 'm_4', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: 'Already read', created_at: at, acked_at: at },
  ]
  it('renders both directions as text, batches unread answers and acks once', async () => {
    mocks.thread.mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages })
    const { client } = show(<ChiefConversation active />)
    expect(await screen.findByText('Question')).toBeInTheDocument(); expect(screen.getByText('<b>Answer</b>')).toBeInTheDocument()
    await waitFor(() => expect(mocks.ack).toHaveBeenCalledExactlyOnceWith(['m_2', 'm_3']))
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-chief-thread'] }) })
    expect(mocks.ack).toHaveBeenCalledTimes(1)
  })
  it('does not read or ack while hidden', () => { show(<ChiefConversation active={false} />); expect(mocks.thread).not.toHaveBeenCalled(); expect(mocks.ack).not.toHaveBeenCalled() })
  it('counts UTF-8 bytes, refuses excess, sends and clears a valid body', async () => {
    show(<ChiefConversation active />)
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: '你'.repeat(2731) } })
    expect(screen.getByText('8193 / 8192 bytes')).toBeInTheDocument()
    expect(screen.getByRole('button', { name: 'Send' })).toBeDisabled()
    fireEvent.submit(screen.getByLabelText('Message').closest('form')!)
    expect(mocks.send).not.toHaveBeenCalled()
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: 'Hello' } }); fireEvent.click(screen.getByRole('button', { name: 'Send' }))
    await waitFor(() => expect(mocks.send).toHaveBeenCalledExactlyOnceWith('Hello'))
    await waitFor(() => expect(screen.getByLabelText('Message')).toHaveValue(''))
  })
  it('shows the waiting note for an unanswered ask', async () => {
    mocks.thread.mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages: messages.slice(0, 1) })
    show(<ChiefConversation active />)
    expect(await screen.findByText('The chief of staff answers when the agent host is awake; usually within a few minutes.')).toBeInTheDocument()
  })
})
