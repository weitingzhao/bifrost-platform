import { describe, expect, it } from 'vitest'
import { contractGraph, generationSegments, shortAddress } from '../busNextgen'

// 2026-11-17 (the Owner: the Bus page must show the layer as it is, with
// pictures; nothing kept by hand). The bar's segments come from the
// register's counts, and the contract graph from the registers' topics and
// subscription rows.
describe('generation bar', () => {
  it('splits each disposition into its retired and remaining share of the start', () => {
    const segs = generationSegments([
      { disposition: 'deleted', total: 12, retired: 2 },
      { disposition: 'kept', total: 31, retired: 0 },
      { disposition: 'reserved', total: 0, retired: 0 },
    ], 153)
    expect(segs.map(s => s.disposition)).toEqual(['deleted', 'kept'])
    expect(segs[0].retired).toBeCloseTo((2 / 153) * 100)
    expect(segs[0].left).toBeCloseTo((10 / 153) * 100)
    expect(generationSegments([], 0)).toEqual([])
  })
})

describe('contract graph', () => {
  it('joins publisher, topic and subscriber, readers and wakes', () => {
    const g = contractGraph({
      topics: [
        { tenant: 'rocket', type: 'ci.run.finished', version: 1, publisher: 'gate-cluster', guarantee: 'durable', consumers: ['platform-workers'] },
        { tenant: 'rocket', type: 'seat.mail.arrived', version: 1, publisher: 'gate-db', guarantee: 'durable', consumers: [] },
      ],
      subscriptions: [
        { tenant: 'rocket', id: 'land-tripwire-to-ci', on: ['ci.run.finished'], deliver: 'wake', to: 'thread:local_7de640c4-4b19-4a71', by: 'dispatcher' },
        { tenant: 'rocket', id: 'unknown-topic', on: ['nope'], deliver: 'wake', to: 'seat:x', by: 'dispatcher' },
      ],
      clients: [],
    })
    expect(g.publishers).toEqual(['gate-cluster', 'gate-db'])
    expect(g.topics).toEqual(['ci.run.finished', 'seat.mail.arrived'])
    expect(g.subscribers).toEqual(['platform-workers', 'seat:x', 'thread:7de640c4…'])
    expect(g.edges).toContainEqual({ from: 'p:gate-cluster', to: 't:ci.run.finished', kind: 'publishes' })
    expect(g.edges).toContainEqual({ from: 't:ci.run.finished', to: 's:thread:7de640c4…', kind: 'wakes', label: 'land-tripwire-to-ci' })
    expect(g.edges.some(e => e.to === 's:seat:x')).toBe(false)
  })
  it('shortens a thread address', () => {
    expect(shortAddress('thread:local_ab850773-fe26')).toBe('thread:ab850773…')
    expect(shortAddress('commit.session')).toBe('commit.session')
  })
})
