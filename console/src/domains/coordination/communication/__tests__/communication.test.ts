// 2026-11-22: Owner requires separate ACK, acceptance and completion; stream receipts are not mail ACKs.
import { describe, it, expect } from 'vitest'
import { exchangesOf, membersOf, evidenceThrough } from '@bifrost/ui/communication'
import type { CommunicationFact } from '@bifrost/ui/communication'
const fact = (stage: CommunicationFact['stage'], id: string): CommunicationFact => ({ id, channel: 'message', communication: 'm_1', stage, evidence: { id, at: '2026-11-22T12:00:00Z', source: 'test' } })
describe('communication evidence', () => {
  it('keeps out-of-order ACK once without inventing accepted or completed', () => {
    const ack = fact('acknowledged', '2'), posted = { ...fact('sent', '1'), from: 'a', to: 'b' }
    const [x] = exchangesOf([ack, posted, ack])
    expect(x.from).toBe('a'); expect(x.stages.acknowledged).toHaveLength(1)
    expect(x.stages.accepted).toBeUndefined(); expect(x.stages.completed).toBeUndefined()
  })
  it('does not merge event and letter identities', () => {
    expect(exchangesOf([fact('sent', '1'), { ...fact('delivered', '2'), channel: 'event' }])).toHaveLength(2)
  })
  it('time filtering removes future ACK without inventing a state', () => {
    const rows = exchangesOf([fact('sent', '1'), { ...fact('acknowledged', '2'), evidence: { id: '2', source: 'test', at: '2026-11-22T12:02:00Z' } }])
    expect(evidenceThrough(rows, '2026-11-22T12:01:00Z')[0].stages.acknowledged).toBeUndefined()
    expect(evidenceThrough(rows, '')[0].stages.acknowledged).toHaveLength(1)
  })
  // 2026-11-22 Owner: issues need explicit evidence; historical views must not reveal later problems.
  it('does not expose a source-reported issue before its evidence timestamp', () => {
    const [exchange] = exchangesOf([fact('sent', '1')])
    exchange.issue = {label:'Worker unavailable', evidence:{id:'issue-1',source:'test',at:'2026-11-22T12:02:00Z'}}
    expect(evidenceThrough([exchange], '2026-11-22T12:01:00Z')[0].issue).toBeUndefined()
    expect(evidenceThrough([exchange], '2026-11-22T12:03:00Z')[0].issue?.label).toBe('Worker unavailable')
  })
  it('keeps both memberships and unknown participants', () => {
    const memberships = [{ seat: 'one', domain: 'crew', organization: 'platform', role: 'owner' as const }, { seat: 'two', domain: 'audit', organization: 'platform', role: 'owner' as const }]
    const xs = exchangesOf([{ ...fact('sent', '1'), from: 'a', to: 'b' }])
    const ms = membersOf(xs, [{ id: 'a', label: 'A', memberships }])
    expect(ms[0].memberships).toHaveLength(2); expect(ms[1].memberships).toEqual([])
  })
})

// 2026-11-22: Owner asks for actual letter round trips, not inferred answers or implicit read receipts.
describe('explicit letter round trips', () => {
  const original = { ...fact('sent', 'sent'), from: 'a', to: 'b' }
  const reply = { ...fact('sent', 'reply'), communication: 'm_2', from: 'b', to: 'a', replyTo: 'm_1', evidence: { id: 'reply', source: 'test', at: '2026-11-22T12:02:00Z' } }
  it('links an out-of-order reply while preserving its own delivery and ACK', () => {
    const rows = exchangesOf([reply, original, reply])
    const parent = rows.find(x => x.id === 'm_1')!
    expect(parent.replies?.map(x => x.id)).toEqual(['m_2'])
    expect(parent.stages.replied).toHaveLength(1)
    expect(parent.stages.opened).toBeUndefined()
    expect(rows.find(x => x.id === 'm_2')?.stages.acknowledged).toBeUndefined()
    const past = evidenceThrough(rows, '2026-11-22T12:01:00Z')
    expect(past).toHaveLength(1)
    expect(past[0].replies).toEqual([])
    expect(past[0].stages.replied).toBeUndefined()
  })
  it('does not invent replies from message kinds, strangers, or self references', () => {
    for (const candidate of [{ ...reply, replyTo: undefined, label: 'answer' }, { ...reply, from: 'stranger' }, { ...reply, replyTo: 'm_2' }]) {
      expect(exchangesOf([original, candidate]).find(x => x.id === 'm_1')?.stages.replied).toBeUndefined()
    }
  })
})

import { messageFacts } from '../adapter'
import { replyObservation } from '@bifrost/ui/communication'
import type { RuntimeEventRow } from '@/api/agentRuntimeEvents'
// 2026-11-22 source audit: seat.mail.arrived records inbox arrival, never opening or ACK.
it('maps actual mailbox arrival separately from receipt by the reader and freezes elapsed time at last read', () => {
  const row = { event_id: 8, event_type: 'seat.mail.arrived', actor_id: 'a', body: { message_id: 'm_1', to: 'b', kind: 'ask' }, at: '2026-11-22T12:00:01Z' } as RuntimeEventRow
  const [delivery] = messageFacts([row])
  expect(delivery.stage).toBe('delivered')
  const [x] = exchangesOf([{ ...fact('sent','1'), from:'a', to:'b' }, delivery])
  expect(x.stages.opened).toBeUndefined()
  expect(x.stages.acknowledged).toBeUndefined()
  expect(replyObservation(x,'2026-11-22T12:02:00Z')).toContain('2m since sent at last read')
  expect(replyObservation(x)).toBe('No linked reply observed')
})

// 2026-11-23 PROD #collaboration: ten letters read "Unknown member → Unknown member" because only their
// ACK was in the latest-500 window. Every letter event sits in the recipient's stream `mbox:<to>`
// (agentrt/coord/messages.go), so the recipient is recorded; the sender is not, and the ACK's actor is not read as either.
it('names the recipient of an ACK-only letter from its mailbox stream and leaves the sender unknown', () => {
  const ack = { event_id: 9, event_stream_id: 'mbox:thread:local_b', event_type: 'message_acked', actor_id: 'thread:local_b', body: { message_id: 'm_2' }, at: '2026-11-22T12:00:02Z' } as RuntimeEventRow
  const [x] = exchangesOf(messageFacts([ack]))
  expect(x.to).toBe('thread:local_b')
  expect(x.from).toBeUndefined()
  expect(x.stages.acknowledged).toHaveLength(1)
  // Control: a stream that is not a mailbox names nobody.
  const [y] = exchangesOf(messageFacts([{ ...ack, event_id: 10, event_stream_id: 'mission:x', body: { message_id: 'm_3' } }]))
  expect(y.to).toBeUndefined()
})
