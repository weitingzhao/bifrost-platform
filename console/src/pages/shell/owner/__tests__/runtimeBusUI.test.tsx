import '@testing-library/jest-dom/vitest'
import { QueryClient, QueryClientProvider } from '@tanstack/react-query'
import { act, cleanup, fireEvent, render, screen, waitFor } from '@testing-library/react'
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { parseRuntimeProgress } from '@/api/agentRuntime'
const mocks = vi.hoisted(() => ({ actions: vi.fn(), stop: vi.fn(), lift: vi.fn(), thread: vi.fn(), ack: vi.fn(), send: vi.fn(), caps: vi.fn(), env: 'stg' }))
vi.mock('@/api/agentRuntimeBus', async original => ({ ...await original<object>(), fetchAttemptActions: mocks.actions, setGlobalStop: mocks.stop, liftControl: mocks.lift,
  fetchChiefThread: mocks.thread, ackMessages: mocks.ack, sendChiefMessage: mocks.send }))
vi.mock('@/api/cluster', async original => ({ ...await original<object>(), fetchAuthCapabilities: mocks.caps }))
// The platform's own word on which environment it is; 'loading' never answers.
vi.mock('@/api/core', async original => ({ ...await original<object>(),
  fetchSelfHealth: () => mocks.env === 'loading' ? new Promise(() => {}) : mocks.env === 'error' ? Promise.reject(new Error('down')) : Promise.resolve({ viewer_env: mocks.env }) }))
import { AttemptActionLine } from '../AttemptActionLine'
import { OwnerStop } from '../OwnerStop'
import { ChiefConversation } from '../ChiefConversation'
import { appendDictated } from '../dictation'
import { PlatformAuthContext, type PlatformAuthContextValue } from '@/hooks/platformAuthContext'
import { PlatformAuthProvider } from '@/hooks/PlatformAuthProvider'
import { SIGN_IN_REFUSED } from '@/api/agentRuntimeBus'
import { setPlatformOperatorToken } from '@/lib/platformAuth'
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
  expect(mocks.actions.mock.calls[0][1]).toEqual({ after: '', limit: 500 })
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
  it('tries a failed acknowledgement again, and does not send a done one twice after a remount', async () => {
    mocks.thread.mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages })
    mocks.ack.mockRejectedValueOnce(new Error('network down'))
    const { client, unmount } = show(<ChiefConversation active />)
    expect(await screen.findByText('network down')).toBeInTheDocument()
    await act(async () => { await client.refetchQueries({ queryKey: ['agent-runtime-chief-thread'] }) })
    await waitFor(() => expect(mocks.ack).toHaveBeenCalledTimes(2))
    expect(mocks.ack.mock.calls[1][0]).toEqual(['m_2', 'm_3'])
    await waitFor(() => expect(screen.queryByText('network down')).not.toBeInTheDocument())
    unmount()
    mocks.thread.mockImplementation(() => new Promise(() => {}))
    render(<QueryClientProvider client={client}><ChiefConversation active /></QueryClientProvider>)
    await new Promise(r => setTimeout(r, 50))
    expect(mocks.ack).toHaveBeenCalledTimes(2)
  })
  it('acknowledges nothing for a seat whose token the Console holds', async () => {
    mocks.thread.mockResolvedValue({ actor: 'seat:chief_of_staff', with: 'owner', messages: [
      { message_id: 'm_9', from: 'owner', to: 'seat:chief_of_staff', kind: 'ask', body: 'Question', created_at: at }] })
    show(<ChiefConversation active />)
    expect(await screen.findByText('Question')).toBeInTheDocument()
    await new Promise(r => setTimeout(r, 50))
    expect(mocks.ack).not.toHaveBeenCalled()
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

describe('chief conversation on every device (Owner 2026-10-18)', () => {
  const auth = (canAdmin: boolean): PlatformAuthContextValue => ({ token: canAdmin ? 'owner' : 'operator', caps: undefined, capsLoading: false,
    capsError: false, canOperate: true, canAdmin, setToken: () => {}, signOut: () => {}, refreshCapabilities: () => {} })
  const signedIn = (canAdmin: boolean) => <PlatformAuthContext.Provider value={auth(canAdmin)}><ChiefConversation active /></PlatformAuthContext.Provider>
  afterEach(() => { delete (window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition })
  it('says a browser not signed in as the Owner sees no conversation, and reads nothing', async () => {
    show(signedIn(false))
    expect(screen.getByText(/not signed in with the Owner token/)).toBeInTheDocument()
    await new Promise(r => setTimeout(r, 30))
    expect(mocks.thread).not.toHaveBeenCalled()
  })
  it('reads nothing while the sign-in is still being read', async () => {
    show(<PlatformAuthContext.Provider value={{ ...auth(true), capsLoading: true }}><ChiefConversation active /></PlatformAuthContext.Provider>)
    await new Promise(r => setTimeout(r, 30))
    expect(mocks.thread).not.toHaveBeenCalled()
  })
  it('reads the conversation when signed in as the Owner', async () => {
    show(signedIn(true))
    await waitFor(() => expect(mocks.thread).toHaveBeenCalled())
    expect(screen.queryByText(/not signed in with the Owner token/)).not.toBeInTheDocument()
  })
  it('types at 16 px, so Safari on the iPhone does not zoom in', () => {
    show(<ChiefConversation active />)
    expect(screen.getByLabelText('Message')).toHaveClass('text-base')
  })
  it('drops the conversation it showed once the browser is no longer signed in as the Owner (review 271 A1)', async () => {
    mocks.thread.mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages: [
      { message_id: 'm_1', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: 'An earlier answer', created_at: at, acked_at: at }] })
    const { client, rerender } = show(signedIn(true))
    expect(await screen.findByText('An earlier answer')).toBeInTheDocument()
    rerender(<QueryClientProvider client={client}>{signedIn(false)}</QueryClientProvider>)
    await waitFor(() => expect(screen.queryByText('An earlier answer')).not.toBeInTheDocument())
    expect(screen.getByText(/not signed in with the Owner token/)).toBeInTheDocument()
    expect(client.getQueryData(['agent-runtime-chief-thread'])).toBeUndefined()
  })
  it('says the sign-in could not be checked, offers to check again, and reads nothing on a stale role (review 271 B2, B3)', async () => {
    const refresh = vi.fn()
    show(<PlatformAuthContext.Provider value={{ ...auth(true), capsError: true, refreshCapabilities: refresh }}><ChiefConversation active /></PlatformAuthContext.Provider>)
    expect(screen.getByText(/could not be checked/)).toBeInTheDocument()
    expect(screen.queryByText(/not signed in with the Owner token/)).not.toBeInTheDocument()
    fireEvent.click(screen.getByRole('button', { name: 'Check again' }))
    expect(refresh).toHaveBeenCalled()
    await new Promise(r => setTimeout(r, 30))
    expect(mocks.thread).not.toHaveBeenCalled()
  })
  it('says it is checking while the sign-in is read', () => {
    show(<PlatformAuthContext.Provider value={{ ...auth(true), capsLoading: true }}><ChiefConversation active /></PlatformAuthContext.Provider>)
    expect(screen.getByText('Checking your sign-in…')).toBeInTheDocument()
  })
  it('stops reading and checks the sign-in again when the platform refuses it', async () => {
    const refresh = vi.fn()
    mocks.thread.mockRejectedValue(new Error('Connect with the Owner token to talk to the chief of staff.'))
    const { client } = show(<PlatformAuthContext.Provider value={{ ...auth(true), refreshCapabilities: refresh }}><ChiefConversation active /></PlatformAuthContext.Provider>)
    await waitFor(() => expect(refresh).toHaveBeenCalled())
    const query = client.getQueryCache().find({ queryKey: ['agent-runtime-chief-thread'] })
    const interval = (query?.options as { refetchInterval?: (q: unknown) => unknown }).refetchInterval
    expect(typeof interval === 'function' ? interval(query) : interval).toBe(false)
  })
  describe('with the real sign-in (review 273 B8, B9)', () => {
    const ownerCaps = { authenticated: true, role: 'admin', can_operate: true, can_admin: true }
    beforeEach(() => {
      // jsdom here has no working localStorage; the sign-in is kept in a map for the test
      const mem = new Map<string, string>()
      Object.defineProperty(window, 'localStorage', { configurable: true, value: {
        get length() { return mem.size }, clear: () => mem.clear(), key: (i: number) => [...mem.keys()][i] ?? null,
        getItem: (k: string) => mem.get(k) ?? null, removeItem: (k: string) => { mem.delete(k) }, setItem: (k: string, v: string) => { mem.set(k, String(v)) } } })
      setPlatformOperatorToken('owner-test')
    })
    afterEach(() => setPlatformOperatorToken(''))
    const mount = () => show(<PlatformAuthProvider><ChiefConversation active /></PlatformAuthProvider>)
    it('checks the sign-in again once when the conversation keeps refusing it, then stops at Try again', async () => {
      mocks.caps.mockImplementation(() => new Promise(r => setTimeout(() => r(ownerCaps), 10)))
      mocks.thread.mockRejectedValue(new Error(SIGN_IN_REFUSED))
      mount()
      expect(await screen.findByRole('button', { name: 'Try again' }, { timeout: 2000 })).toBeInTheDocument()
      await new Promise(r => setTimeout(r, 1000))
      expect(mocks.caps).toHaveBeenCalledTimes(2)
      expect(mocks.thread.mock.calls.length).toBeLessThanOrEqual(3)
    })
    it('reads the conversation again once the sign-in is read back as the Owner, without waiting for the next poll', async () => {
      mocks.caps.mockResolvedValue(ownerCaps)
      mocks.thread.mockRejectedValueOnce(new Error(SIGN_IN_REFUSED)).mockResolvedValue({ actor: 'owner', with: 'seat:chief_of_staff', messages: [
        { message_id: 'm_1', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: 'Back again', created_at: at, acked_at: at }] })
      mount()
      expect(await screen.findByText('Back again', undefined, { timeout: 2000 })).toBeInTheDocument()
      expect(screen.queryByText(SIGN_IN_REFUSED)).not.toBeInTheDocument()
    })
  })
  it('stops dictating when the entry is hidden, and drops a late phrase (review 271 A4)', () => {
    const made: { aborted: number; fire: ((e: unknown) => void) | null }[] = []
    class FakeRecognition {
      lang = ''; interimResults = true; continuous = false; onresult: ((e: unknown) => void) | null = null
      onerror: ((e: unknown) => void) | null = null; onend: (() => void) | null = null
      me = { aborted: 0, fire: null as ((e: unknown) => void) | null }
      constructor() { made.push(this.me) }
      start() { this.me.fire = e => this.onresult?.(e) }
      stop() {}
      abort() { this.me.aborted++ }
    }
    ;(window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition = FakeRecognition
    const { client, rerender, unmount } = show(<ChiefConversation active />)
    fireEvent.click(screen.getByRole('button', { name: 'Speak' }))
    rerender(<QueryClientProvider client={client}><ChiefConversation active={false} /></QueryClientProvider>)
    expect(made[0].aborted).toBe(1)
    act(() => { made[0].fire?.({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: 'late' } }] }) })
    rerender(<QueryClientProvider client={client}><ChiefConversation active /></QueryClientProvider>)
    expect(screen.getByLabelText('Message')).toHaveValue('')
    fireEvent.click(screen.getByRole('button', { name: 'Speak' }))
    unmount()
    expect(made[1].aborted).toBe(1)
  })
  it('dictates into the message, and stops', () => {
    const made: { started: number; stopped: number; onresult: ((e: unknown) => void) | null; onend: (() => void) | null }[] = []
    class FakeRecognition {
      lang = ''; interimResults = true; continuous = false; onresult: ((e: unknown) => void) | null = null
      onerror: ((e: unknown) => void) | null = null; onend: (() => void) | null = null
      me = { started: 0, stopped: 0, onresult: null as ((e: unknown) => void) | null, onend: null as (() => void) | null }
      constructor() { made.push(this.me) }
      start() { this.me.started++; this.me.onresult = this.onresult; this.me.onend = this.onend }
      stop() { this.me.stopped++ }
      abort() {}
    }
    ;(window as unknown as { webkitSpeechRecognition?: unknown }).webkitSpeechRecognition = FakeRecognition
    show(<ChiefConversation active />)
    fireEvent.change(screen.getByLabelText('Message'), { target: { value: '现在' } })
    fireEvent.click(screen.getByRole('button', { name: 'Speak' }))
    expect(made).toHaveLength(1); expect(made[0].started).toBe(1)
    act(() => { made[0].onresult?.({ resultIndex: 0, results: [{ isFinal: true, 0: { transcript: '有哪些任务在跑' } }] }) })
    expect(screen.getByLabelText('Message')).toHaveValue('现在有哪些任务在跑')
    fireEvent.click(screen.getByRole('button', { name: 'Stop dictating' }))
    expect(made[0].stopped).toBe(1)
    expect(screen.getByRole('button', { name: 'Stop dictating' })).toBeInTheDocument() // until the recognizer ends
    act(() => { made[0].onend?.() })
    expect(screen.getByRole('button', { name: 'Speak' })).toBeInTheDocument()
  })
  it('points to the keyboard microphone where the browser has no speech recognition', () => {
    show(<ChiefConversation active />)
    expect(screen.queryByRole('button', { name: 'Speak' })).not.toBeInTheDocument()
    expect(screen.getByText(/microphone key on your keyboard/)).toBeInTheDocument()
  })
  it('joins dictated text with a space only between Latin words', () => {
    expect(appendDictated('', ' 你好 ')).toBe('你好')
    expect(appendDictated('你好', '世界')).toBe('你好世界')
    expect(appendDictated('Hello', 'world')).toBe('Hello world')
    expect(appendDictated('Hello', '  ')).toBe('Hello')
  })
})
