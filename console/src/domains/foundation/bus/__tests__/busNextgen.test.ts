import { describe, expect, it } from 'vitest'
import { contractGraph, generationSegments, problemProofText, shortAddress, topicState, weekCounts } from '../busNextgen'

// ISSUE-29, ISSUE-18 (the Bus page shows the layer as it is, with pictures;
// nothing kept by hand). The bar's segments come from the
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

// 2026-11-18, the Owner's "first make problems visible": a drill proves a
// channel's event can be written, a real run proves it ran. The page must say
// which, and an older platform with no drill count reads as none.
describe('problem channel proof', () => {
  it('counts real runs and drill-only channels apart', () => {
    expect(problemProofText({ proven: 29, drilled: 12 })).toBe('29 proven by a real run · 12 by a drill only')
    expect(problemProofText({ proven: 29 })).toBe('29 proven by a real run · 0 by a drill only')
  })
})

// Origin: 2026-11-20, the Bus page's "registered but no events in 7 days"
// state (comm line ledger item 5). Counts come from the metrics line's
// bus_events readings; a missing reading must not read as flowing.
describe('weekCounts and topicState', () => {
  const readings = [
    { name: 'bus_events', labels: { env: 'prod', type: 'ci.run.finished' }, value: 508, status: 'ok' },
    { name: 'bus_events', labels: { env: 'stg', type: 'ci.run.finished' }, value: 478, status: 'ok' },
    { name: 'bus_events', labels: { env: 'prod', type: 'cluster.node.changed' }, value: 0, status: 'ok' },
    { name: 'bus_events', labels: { env: 'prod', type: 'plan.tree.changed' }, value: null, status: 'unmeasured' },
    { name: 'bus_outage_minutes', labels: { env: 'prod', type: 'x' }, value: 3, status: 'ok' },
  ]
  it('keeps one environment and only readings with a value', () => {
    const m = weekCounts(readings, 'prod')
    expect([...m.entries()]).toEqual([['ci.run.finished', 508], ['cluster.node.changed', 0]])
  })
  it('flowing above zero, silent at zero or with no reading, unknown without the readout', () => {
    const m = weekCounts(readings, 'prod')
    expect(topicState('ci.run.finished', m)).toBe('flowing')
    expect(topicState('cluster.node.changed', m)).toBe('silent')
    expect(topicState('plan.tree.changed', m)).toBe('silent')
    expect(topicState('ci.run.finished', undefined)).toBe('unknown')
  })
})
