import { describe, expect, it } from 'vitest'
import { OWN_CONSUMER, dustBrightness, isDeaf, nerveModel, warmColor, readerOf, replaySchedule, seatLabel, seatMail, seatState, shortSeconds, type ObserveSeat } from '../busNerve'
import type { BusContract, BusNextgen } from '../busNextgen'
import type { BusEnvelope } from '../busTrace'
import type { BusClient } from '../useBusStream'

// Origin: 2026-11-17, the Bus page's nerve map (the coordinator relaying the
// Owner): organs come from the contract, the roster and the source switch;
// the Console's own receipt is not a reader; a replay keeps the events'
// order and shortens long quiet gaps so the map does not stand still.

const contract: BusContract = {
  topics: [
    { tenant: 'rocket', type: 'git.main.advanced', version: 1, publisher: 'gate-git', guarantee: 'at-least-once', consumers: ['seat:*'] },
    { tenant: 'rocket', type: 'ci.run.finished', version: 1, publisher: 'gate-cluster', guarantee: 'at-least-once', consumers: ['seat:*'] },
  ],
  subscriptions: [],
  clients: [],
}
const clients: BusClient[] = [
  { name: OWN_CONSUMER, declared: true, receipts: 9, status: 'active' },
  { name: 'headrelease', declared: true, receipts: 4, status: 'lost' },
  { name: 'gate:events:seat-ci', declared: false, receipts: 1, status: 'new' },
]
const ev = (id: string, time: string, type = 'git.main.advanced'): BusEnvelope => ({
  specversion: '1.0', id, source: 's', type, time, subject: id, tenant: 'rocket', env: 'prod', cursor: '1.1', ref: '', actor: '',
})

describe('nerveModel', () => {
  it('takes publishers and topics from the contract and readers from the roster, the Console aside', () => {
    const nextgen = { readers: [{ id: 'runs', title: 'Runs', counts: {}, readers: [
      { id: 'moved-one', what: 'x', status: 'moved' },
      { id: 'still-polling', what: 'lists runs', status: 'open', owner: 'ci' },
    ] }] } as unknown as BusNextgen
    const m = nerveModel(contract, clients, nextgen)
    expect(m.publishers.map(p => p.id)).toEqual(['gate-cluster', 'gate-git'])
    expect(m.topics.map(t => t.id)).toEqual(['ci.run.finished', 'git.main.advanced'])
    expect(m.publisherOf.get('ci.run.finished')).toBe('gate-cluster')
    expect(m.readers.map(r => [r.id, r.tone])).toEqual([
      ['gate:events:seat-ci', 'new'], ['headrelease', 'lost'], ['old:still-polling', 'old'],
    ])
  })
})

// Origin: 2026-11-18, the coordinator: the Owner reads the switch's progress off this map, and a reader
// marked moved in the register with no run on its new source must not look finished.
describe('a reader marked moved', () => {
  const model = (evidence?: object) => nerveModel(contract, clients, { readers: [{ id: 'runs', title: 'Runs', counts: {}, readers: [
    { id: 'r', what: 'x', status: 'moved', evidence },
  ] }] } as unknown as BusNextgen).readers.map(r => r.id)
  it('stays an old road until it is seen running', () => {
    expect(model({ state: 'running', runs_7d: 9, last_seen: '2026-11-18T00:00:00Z' })).not.toContain('old:r')
    expect(model({ state: 'stale', runs_7d: 9, last_seen: '2026-11-10T00:00:00Z' })).toContain('old:r')
    expect(model({ state: 'none', runs_7d: 0 })).toContain('old:r')
  })
  it('is left as it is when the evidence could not be read, or the platform has none yet', () => {
    expect(model({ state: 'unmeasured', runs_7d: 0 })).not.toContain('old:r')
    expect(model()).not.toContain('old:r')
  })
})

describe('readerOf', () => {
  it('is no reader for the Console itself, and the consumer for anyone else', () => {
    const m = nerveModel(contract, clients)
    expect(readerOf(OWN_CONSUMER, m.readers)).toBeNull()
    expect(readerOf('headrelease', m.readers)).toBe('headrelease')
    expect(readerOf('gate:events:somebody', m.readers)).toBe('gate:events:somebody')
  })
})

describe('replaySchedule', () => {
  it('keeps order and spacing at speed, and shortens a long gap', () => {
    const plan = replaySchedule([
      ev('ev-3', '2026-11-17T10:00:00Z'), ev('ev-1', '2026-11-17T09:00:00Z'), ev('ev-2', '2026-11-17T09:00:06Z'),
    ], 60, 1500)
    expect(plan.map(p => p.event.id)).toEqual(['ev-1', 'ev-2', 'ev-3'])
    expect(plan.map(p => p.at)).toEqual([0, 100, 1600])
  })
})

describe('seatMail', () => {
  it('is the seats letters only', () => {
    expect(seatMail([ev('ev-1', 't', 'seat.mail.arrived'), ev('ev-2', 't')]).map(e => e.id)).toEqual(['ev-1'])
  })
})

describe('seatState', () => {
  const now = Date.parse('2026-11-17T21:00:00Z')
  const seat: ObserveSeat = { id: 'seat:b', letters_out: 0, letters_in: 2, unacked: 1, last_status: '2026-11-17T20:50:00Z' }
  it('is stuck while a letter to it waits past the limit, ok before', () => {
    expect(seatState(seat, [{ from: 'seat:a', to: 'seat:b', kind: 'letter', count: 2, unacked: 1, oldest_open: '2026-11-17T20:40:00Z' }], now)).toBe('stuck')
    expect(seatState(seat, [{ from: 'seat:a', to: 'seat:b', kind: 'letter', count: 2, unacked: 1, oldest_open: '2026-11-17T20:55:00Z' }], now)).toBe('ok')
  })
  it('is quiet when its last report is old', () => {
    expect(seatState({ ...seat, last_status: '2026-11-17T17:00:00Z' }, [], now)).toBe('quiet')
  })
})

describe('labels', () => {
  it('shortens a thread to its session and a seat to its name', () => {
    expect(seatLabel('thread:local_ab850773-fe26-40fa-ac01-b3b590b73164')).toBe('ab850773')
    expect(seatLabel('seat:chief_of_staff')).toBe('chief_of_staff')
    expect(seatLabel('owner')).toBe('owner')
    expect([shortSeconds(4.8), shortSeconds(134.8), shortSeconds(32785.8)]).toEqual(['4.8s', '2.2m', '9.1h'])
  })
})

// Origin: 2026-11-17, the Owner: nebulae of small warm lights, not balls.
// Each domain a warm hue fixed by its id; an event's dust dims with age.
describe('warmColor and dustBrightness', () => {
  it('gives each domain its own fixed warm colour, and dims dust over a day', () => {
    expect(warmColor('coordination')).toBe(warmColor('coordination'))
    expect(warmColor('coordination')).toMatch(/^#[0-9a-f]{6}$/)
    const all = ['foundation', 'plan', 'lineage', 'verification', 'delivery', 'owner', 'compute', 'tenant', 'coordination'].map(d => warmColor(d))
    expect(new Set(all).size).toBeGreaterThan(5)
    expect(dustBrightness(0)).toBe(1)
    expect(dustBrightness(24)).toBeCloseTo(0.1)
    expect(dustBrightness(12)).toBeGreaterThan(dustBrightness(20))
  })
})

// 2026-11-17, the coordinator: ci.run.started is published to be watched
// (display only in the register); it must not be called deaf.
describe('isDeaf', () => {
  it('names a published, unheard topic, but never a display-only one', () => {
    const t = { type: 'alert.rule.fired', published: 3, readers: [], heard: 0, followed: 0 }
    expect(isDeaf(t)).toBe(true)
    expect(isDeaf({ ...t, display_only: true })).toBe(false)
    expect(isDeaf({ ...t, heard: 1 })).toBe(false)
    expect(isDeaf({ ...t, published: 0 })).toBe(false)
    expect(isDeaf(undefined)).toBe(false)
  })
})
