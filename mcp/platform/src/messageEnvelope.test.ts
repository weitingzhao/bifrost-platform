import assert from 'node:assert/strict'
import { beforeEach, describe, it } from 'node:test'
import { buildEnvelope, parseEnvelope, present, wholeUnderAuto } from './messageEnvelope.js'
import { STATUS_BOARD, ack, forgetKeptMessagesForTests, readMessage, reportStatus, send, waitDetailed } from './messageTools.js'
import type { RuntimeDeps } from './runtimeTools.js'

// Origin: 2026-11-14, ISSUE-7: seats talk through the platform to
// save tokens (agent-tool-comm design v1 §4). That day the main coordinator
// thread took about 1000 automatic seat summaries and was compacted about 95
// times; a message in a model's context is paid for on every later turn.

function deps(unread: unknown[] = []) {
  const calls: { kind: string; path: string; body?: unknown }[] = []
  const d: RuntimeDeps = {
    get: async (path) => { calls.push({ kind: 'get', path }); return { actor: 'thread:local_t', messages: unread } },
    getText: async () => ({ media_type: 'text/plain', text: '' }),
    send: async (_m, path, body) => {
      calls.push({ kind: 'send', path, body })
      if (path.endsWith('/ack')) return { acknowledged: (body as { message_ids: string[] }).message_ids }
      return { message_id: 'm_new' }
    },
    upload: async () => ({}),
    session: () => 'local_t',
    fellBack: () => false,
  }
  return { d, calls }
}

describe('envelope', () => {
  it('a typed message is stored as JSON under the kind its type maps to', async () => {
    const { d, calls } = deps()
    await send({ to: 'seat:ci', type: 'status', summary: 'U01 pushed to ready/, waiting for CI', refs: ['sha256:' + 'a'.repeat(64)] }, d)
    const sent = calls[0].body as { kind: string; body: string }
    assert.equal(sent.kind, 'note')
    assert.deepEqual(parseEnvelope(sent.body), { v: 1, type: 'status', summary: 'U01 pushed to ready/, waiting for CI', refs: ['sha256:' + 'a'.repeat(64)] })
    for (const [type, kind] of [['ask', 'ask'], ['decide', 'ask'], ['deliver', 'handover'], ['note', 'note']] as const) {
      assert.equal((buildEnvelope({ type, summary: 's' }) as { kind: string }).kind, kind)
    }
  })

  it('refuses what would mislead the receiver or not fit', async () => {
    const { d, calls } = deps()
    assert.match(String(((await send({ to: 'owner', type: 'ask' }, d)) as { error: string }).error), /needs a summary/)
    assert.match(String(((await send({ to: 'owner', type: 'ask', summary: 'a\nb' }, d)) as { error: string }).error), /one line/)
    assert.match(String(((await send({ to: 'owner', type: 'deliver', summary: 's', kind: 'note' }, d)) as { error: string }).error), /leave kind out/)
    assert.match(String(((await send({ to: 'owner', kind: 'note', body: 'x', summary: 's' }, d)) as { error: string }).error), /go with a type/)
    assert.match(String(((await send({ to: 'owner', type: 'note', summary: 's', body: 'x'.repeat(9000) }, d)) as { error: string }).error), /artifact/)
    assert.match(String(((await send({ to: 'owner', body: 'x' }, d)) as { error: string }).error), /needs kind and body/)
    assert.equal(calls.length, 0)
  })

  it('an untyped message is sent as before', async () => {
    const { d, calls } = deps()
    await send({ to: 'thread:x', kind: 'handover', body: 'do block 55' }, d)
    assert.deepEqual(calls[0].body, { to: 'thread:x', kind: 'handover', body: 'do block 55' })
  })
})

describe('summaries', () => {
  const status = { message_id: 'm_1', from: 'seat:ci', kind: 'note', body: JSON.stringify({ v: 1, type: 'status', summary: 'green', text: 'long detail '.repeat(100) }) }
  const handover = { message_id: 'm_2', from: 'thread:liaison', kind: 'handover', body: 'Block 55: read-only checks\nstep 1 …' }
  const plainNote = { message_id: 'm_3', from: 'thread:x', kind: 'note', body: 'first line\nsecond line' }

  beforeEach(() => forgetKeptMessagesForTests())

  it('auto gives what is to be acted on whole and notices as their summary', () => {
    assert.equal(wholeUnderAuto(status), false)
    assert.equal(wholeUnderAuto(handover), true)
    assert.equal(wholeUnderAuto(plainNote), false)
    const s = present(status, 'auto')
    assert.equal(s.summary, 'green')
    assert.equal(s.body_omitted, true)
    assert.equal(s.text, undefined)
    assert.ok(!('body' in s))
    assert.equal(present(handover, 'auto').body, handover.body)
    assert.equal(present(plainNote, 'auto').summary, 'first line')
    assert.equal(present(status, 'full').text, 'long detail '.repeat(100))
    assert.equal(present(handover, 'summary').body_omitted, true)
  })

  it('a left-out body is read with read_message before ack, not after', async () => {
    const { d, calls } = deps([status, handover])
    const got = (await waitDetailed({ timeout_seconds: 0 }, d)) as { messages: Record<string, unknown>[]; omitted_note?: string }
    assert.equal(got.messages.length, 2)
    assert.match(got.omitted_note ?? '', /read_message/)
    const read = (await readMessage({ message_id: 'm_1' }, d)) as { message: Record<string, unknown> }
    assert.equal(read.message.text, 'long detail '.repeat(100))
    assert.equal(calls.filter((c) => c.kind === 'get').length, 1, 'served from what wait_for_messages saw')
    await ack({ message_ids: ['m_1'] }, d)
    const { d: empty } = deps([])
    assert.match(String(((await readMessage({ message_id: 'm_1' }, empty)) as { error: string }).error), /no unread message/)
  })

  it('a body not seen in this process is fetched again from the unread inbox', async () => {
    const { d, calls } = deps([plainNote])
    const read = (await readMessage({ message_id: 'm_3' }, d)) as { message: Record<string, unknown> }
    assert.equal(read.message.body, plainNote.body)
    assert.deepEqual(calls, [{ kind: 'get', path: '/api/v1/agentrt/messages?wait=0&limit=100' }])
  })

  it('the summary of a status is much smaller than its whole body', () => {
    const whole = JSON.stringify(present(status, 'full')).length
    const summary = JSON.stringify(present(status, 'auto')).length
    assert.ok(summary * 5 < whole, `${summary} vs ${whole}`)
  })
})

// 2026-11-17 (ISSUE-1: progress was asked for because Ops did not show it;
// after card #587, no route of its own): report_status is a "status" message
// to the status board whose text is the status fields; the summary is what
// the line is doing, one line, cut to the summary bound.
describe('report_status', () => {
  it('sends the fields as a status envelope to the board', async () => {
    const { d, calls } = deps()
    await reportStatus({ goal: 'g', doing: 'release\nthe batch', percent: 40, percent_basis: '2 of 5', next: 'n' }, d)
    const sent = calls[0].body as { to: string; kind: string; body: string }
    assert.equal(calls[0].path, '/api/v1/agentrt/messages')
    assert.equal(sent.to, STATUS_BOARD)
    assert.equal(sent.kind, 'note')
    const env = parseEnvelope(sent.body) as { type: string; summary: string; text: string }
    assert.equal(env.type, 'status')
    assert.equal(env.summary, 'release the batch')
    assert.deepEqual(JSON.parse(env.text), { goal: 'g', doing: 'release\nthe batch', percent: 40, percent_basis: '2 of 5', next: 'n' })
  })
  it('a long doing is cut in the summary, kept whole in the text', async () => {
    const { d, calls } = deps()
    await reportStatus({ goal: 'g', doing: 'x'.repeat(500), percent: 1, percent_basis: 'b', waiting_on: 'CI', next: 'n' }, d)
    const env = parseEnvelope((calls[0].body as { body: string }).body) as { summary: string; text: string }
    assert.ok(env.summary.length <= 200)
    assert.equal(JSON.parse(env.text).doing.length, 500)
    assert.equal(JSON.parse(env.text).waiting_on, 'CI')
  })
})
