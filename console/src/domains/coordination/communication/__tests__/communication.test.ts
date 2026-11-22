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
  it('keeps both memberships and unknown participants', () => {
    const memberships = [{ seat: 'one', domain: 'crew', organization: 'platform', role: 'owner' as const }, { seat: 'two', domain: 'audit', organization: 'platform', role: 'owner' as const }]
    const xs = exchangesOf([{ ...fact('sent', '1'), from: 'a', to: 'b' }])
    const ms = membersOf(xs, [{ id: 'a', label: 'A', memberships }])
    expect(ms[0].memberships).toHaveLength(2); expect(ms[1].memberships).toEqual([])
  })
})
