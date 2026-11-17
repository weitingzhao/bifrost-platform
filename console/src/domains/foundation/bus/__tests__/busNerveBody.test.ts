import { describe, expect, it } from 'vitest'
import { buildBody } from '../busNerveBody'
import { nerveModel } from '../busNerve'

// Origin: 2026-11-17, the Owner on STG: the map opened with every detail
// showing. Folded, the body is the bus and one cloud per domain, each cloud
// carrying its parts and its last day's events; opening coordination brings
// out its seats, and nothing else of the other domains.
describe('buildBody', () => {
  const contract = {
    topics: [
      { tenant: 'rocket', type: 'ci.run.finished', version: 1, publisher: 'gate-cluster', domain: 'verification', guarantee: '', consumers: [] },
      { tenant: 'rocket', type: 'seat.mail.arrived', version: 1, publisher: 'gate-db', domain: 'coordination', guarantee: '', consumers: [] },
    ],
    subscriptions: [], clients: [],
  }
  const model = nerveModel(contract, [{ name: 'headrelease', declared: true, receipts: 0, status: 'active' }])
  const base = {
    model, domainOfType: new Map([['ci.run.finished', 'verification'], ['seat.mail.arrived', 'coordination']]),
    seats: ['thread:local_aaaaaaaa', 'owner'], beat: new Map(), observed: new Map(), teamLinks: [], names: new Map(),
    dustByDomain: new Map([['verification', [1, 2, 3]]]), now: Date.parse('2026-11-17T21:00:00Z'),
  }
  it('is the bus and one cloud per domain when folded', () => {
    const b = buildBody({ ...base, expanded: new Set() })
    expect(b.nodes.map(n => n.id).sort()).toEqual(['core:bus', 'd:coordination', 'd:foundation', 'd:owner', 'd:verification'])
    const v = b.nodes.find(n => n.id === 'd:verification')!
    expect(v.cloud).toEqual({ parts: 1, dust: [1, 2, 3] })
  })
  it('brings out a domain seats when it is opened, and only them', () => {
    const b = buildBody({ ...base, expanded: new Set(['coordination']) })
    const ids = b.nodes.map(n => n.id)
    expect(ids).toContain('s:thread:local_aaaaaaaa')
    expect(ids).not.toContain('s:owner')
    expect(ids).not.toContain('r:headrelease')
  })
})
