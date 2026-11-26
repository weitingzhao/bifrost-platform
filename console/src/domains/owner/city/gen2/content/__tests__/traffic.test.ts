import { describe, expect, it } from 'vitest'
import { PICTURE_READER, trafficPanel, tripsOf, type TrafficContract } from '../traffic'

// 2026-11-21 (ISSUE-9): the communication line's traffic in the City. The contract below is the bus registry
// of that day (api/internal/bus/registry: git.main.advanced is lineage's, read by headrelease in delivery and
// unit-watch in verification; seat.mail.arrived is coordination's, read by inbox-waiters in coordination).
const contract: TrafficContract = {
  topics: [{ type: 'git.main.advanced', domain: 'lineage' }, { type: 'seat.mail.arrived', domain: 'coordination' }, { type: 'loop.health.changed' }],
  clients: [{ name: 'headrelease', domain: 'delivery' }, { name: 'unit-watch', domain: 'verification' }, { name: 'inbox-waiters', domain: 'coordination' }, { name: PICTURE_READER }],
}
const places = new Set(['lineage', 'delivery', 'verification', 'coordination'])
const at = '2026-11-21T18:00:00Z'

describe('the bus as traffic', () => {
  it('a receipt is a trip from the topic’s domain to the reader’s', () => {
    const t = tripsOf(contract, [{ id: 'e1', type: 'git.main.advanced' }], [{ id: 'e1', consumer: 'headrelease', at }, { id: 'e1', consumer: 'unit-watch', at }], places)
    expect(t.trips.map(x => [x.from[0], x.to[0]])).toEqual([['lineage', 'delivery'], ['lineage', 'verification']])
    expect(new Set(t.trips.map(x => x.id)).size).toBe(2)
    expect(t.unplaced).toBe(0)
  })
  it('the picture’s own reader is not traffic, and an event the stream did not pass on is not drawn', () => {
    const t = tripsOf(contract, [{ id: 'e1', type: 'git.main.advanced' }], [{ id: 'e1', consumer: PICTURE_READER, at }, { id: 'e9', consumer: 'headrelease', at }], places)
    expect(t.trips).toEqual([])
    expect(t.unplaced).toBe(0)
  })
  it('a topic without a domain is counted as not placed, never guessed', () => {
    const t = tripsOf(contract, [{ id: 'e2', type: 'loop.health.changed' }], [{ id: 'e2', consumer: 'inbox-waiters', at }], places)
    expect(t.trips).toEqual([])
    expect(t.unplaced).toBe(1)
  })
  it('no contract read: no traffic', () => {
    expect(tripsOf(undefined, [], [], places)).toEqual({ trips: [], unplaced: 0 })
  })
  it('the card says the stream’s state, so an empty road is not read as quiet', () => {
    const p = trafficPanel({ trips: [], unplaced: 0, connection: 'denied', detail: 'viewer token required' })
    expect(JSON.stringify(p.cards)).toContain('denied')
  })
})
