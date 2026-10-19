import { beforeEach, describe, expect, it, vi } from 'vitest'
const mocks = vi.hoisted(() => ({ fetch: vi.fn() }))
vi.mock('@/api/client', () => ({ authedFetch: mocks.fetch }))
import { COORDINATOR_THREAD, ackMessages, fetchAttemptActions, fetchChiefThread, fetchCoordinatorNote, latestNote, liftControl, parseAttemptActions, parseChiefThread, sendChiefMessage, setGlobalStop } from '../agentRuntimeBus'

beforeEach(() => { mocks.fetch.mockReset(); mocks.fetch.mockResolvedValue(new Response('{}')) })
describe('runtime bus', () => {
  it('parses result exit codes and safely defaults malformed fields', () => {
    expect(parseAttemptActions({ attempt_id: 'a_1', actions: [{ id: '1-0', kind: 'result', target: 'go test ./...', at: '2026-10-18T07:20:01.123Z', code: 2, truncated: true }], next: '1-0' }))
      .toMatchObject({ next: '1-0', actions: [{ code: 2, truncated: true }] })
    expect(parseAttemptActions({ actions: [null, { code: '2' }] }).actions[0].code).toBeUndefined()
    expect(parseChiefThread(null)).toEqual({ actor: '', with: '', messages: [] })
  })
  it('parses both message directions, optional task and acknowledgement fields', () => {
    const parsed = parseChiefThread({ actor: 'owner', with: 'seat:chief_of_staff', messages: [
      { message_id: 'm_1', from: 'owner', to: 'seat:chief_of_staff', kind: 'ask', body: 'Question', created_at: '2026-10-18T07:20:01.123Z', acked_at: '2026-10-18T07:21:01.123Z' },
      { message_id: 'm_2', from: 'seat:chief_of_staff', to: 'owner', kind: 'answer', body: 'Answer', created_at: '2026-10-18T07:22:01.123Z', task_id: 't_1' },
    ] })
    expect(parsed.messages[0]).toMatchObject({ body: 'Question', acked_at: '2026-10-18T07:21:01.123Z' })
    expect(parsed.messages[1]).toMatchObject({ body: 'Answer', acked_at: '', task_id: 't_1' })
  })
  it('represents actions 503 as an unavailable value', async () => {
    mocks.fetch.mockImplementationOnce(async (prefix, _path, _init, errorParser) => { throw await errorParser(prefix, new Response(JSON.stringify({ error: 'unavailable', message: 'this platform keeps no actions (no redis-agents)' }), { status: 503 })) })
    expect(await fetchAttemptActions('a_1', { last: 1 })).toEqual({ attempt_id: 'a_1', actions: [], next: '', unavailable: true })
  })
  it('shows a 503 of a runtime not connected yet as an error, not as no actions', async () => {
    mocks.fetch.mockImplementationOnce(async (prefix, _path, _init, errorParser) => { throw await errorParser(prefix, new Response(JSON.stringify({ error: 'not_connected', message: 'agentrt: not connected yet' }), { status: 503 })) })
    await expect(fetchAttemptActions('a_1', { last: 1 })).rejects.toThrow('agentrt: not connected yet')
  })
  it('encodes the attempt and cursor and propagates cancellation', async () => {
    const signal = new AbortController().signal
    await fetchAttemptActions('a/1', { after: '1-0', limit: 200 }, signal)
    expect(mocks.fetch).toHaveBeenCalledWith('Actions', '/api/v1/agentrt/attempts/a%2F1/actions?after=1-0&limit=200', { signal }, expect.any(Function))
  })
  it('sends fresh stop keys and lifts the selected control', async () => {
    mocks.fetch.mockImplementation(async () => new Response('{}'))
    await setGlobalStop('why'); await setGlobalStop('why'); await liftControl('c_1')
    const bodies = mocks.fetch.mock.calls.slice(0, 2).map(c => JSON.parse(c[2].body))
    expect(bodies[0]).toMatchObject({ scope: 'global', kind: 'stop', reason: 'why' })
    expect(bodies[0].idempotency_key).not.toBe(bodies[1].idempotency_key)
    expect(mocks.fetch.mock.calls[2]).toMatchObject(['Control', '/api/v1/agentrt/controls/c_1', { method: 'DELETE' }, expect.any(Function)])
  })
  it('sends exact message and acknowledgement contracts and enforces UTF-8 bytes', async () => {
    mocks.fetch.mockImplementation(async () => new Response('{}'))
    await sendChiefMessage('hello'); await ackMessages(['m_1'])
    expect(JSON.parse(mocks.fetch.mock.calls[0][2].body)).toEqual({ to: 'seat:chief_of_staff', kind: 'ask', body: 'hello' })
    expect(JSON.parse(mocks.fetch.mock.calls[1][2].body)).toEqual({ message_ids: ['m_1'] })
    await expect(sendChiefMessage('你'.repeat(2731))).rejects.toThrow('8192')
    expect(mocks.fetch).toHaveBeenCalledTimes(2)
  })
  it.each([400, 401])('gives Owner-token guidance for message HTTP %s', async status => {
    mocks.fetch.mockImplementationOnce(async (prefix, _path, _init, errorParser) => { throw await errorParser(prefix, new Response('{}', { status })) })
    await expect(fetchChiefThread()).rejects.toThrow('Connect with the Owner token to talk to the chief of staff.')
  })
  it('reads the coordinating thread\'s latest note, not its other kinds or the Owner\'s messages', async () => {
    const at = (m: number) => `2026-10-19T12:0${m}:00Z`
    const thread = { actor: 'owner', with: COORDINATOR_THREAD, messages: [
      { message_id: 'm_1', from: COORDINATOR_THREAD, to: 'owner', kind: 'note', body: 'Doing G6', created_at: at(1) },
      { message_id: 'm_2', from: COORDINATOR_THREAD, to: 'owner', kind: 'note', body: 'Doing G8\nNext: G3', created_at: at(2) },
      { message_id: 'm_3', from: COORDINATOR_THREAD, to: 'owner', kind: 'help', body: 'Stuck', created_at: at(3) },
      { message_id: 'm_4', from: 'owner', to: COORDINATOR_THREAD, kind: 'note', body: 'From the Owner', created_at: at(4) },
    ] }
    mocks.fetch.mockResolvedValueOnce(new Response(JSON.stringify(thread)))
    expect(await fetchCoordinatorNote()).toEqual({ body: 'Doing G8\nNext: G3', at: at(2) })
    expect(mocks.fetch.mock.calls[0][1]).toBe(`/api/v1/agentrt/messages/thread?with=${encodeURIComponent(COORDINATOR_THREAD)}&limit=50`)
    expect(latestNote(parseChiefThread({ messages: [thread.messages[2]] }), COORDINATOR_THREAD)).toBeNull()
    expect(COORDINATOR_THREAD).toMatch(/^thread:[A-Za-z0-9._:-]{1,128}$/)
  })
  it.each([400, 401, 403])('asks for the Owner token when the coordinator note is refused with HTTP %s', async status => {
    mocks.fetch.mockImplementationOnce(async (prefix, _path, _init, errorParser) => { throw await errorParser(prefix, new Response('{}', { status })) })
    await expect(fetchCoordinatorNote()).rejects.toThrow('Connect with the Owner token to read the coordinator’s notes.')
  })
  it('shows the server error message', async () => {
    mocks.fetch.mockImplementationOnce(async (prefix, _path, _init, errorParser) => { throw await errorParser(prefix, new Response('{"error":"failed","message":"Try again later"}', { status: 500 })) })
    await expect(setGlobalStop('why')).rejects.toThrow('Try again later')
  })
})
