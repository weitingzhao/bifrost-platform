import { describe, expect, it } from 'vitest'
import { SseParser } from '../sseParser'
import { formatDelta, traceTimeline, type BusTraceResponse } from '../busTrace'
import { BUS_LIST_CAP, mergeEvents } from '../useBusStream'
import type { BusEnvelope } from '../busTrace'

// 2026-11-16, Bus page (LANE-agent-tool-comm): the gate's stream keeps the viewer bearer, so the
// Console reads it with fetch and parses SSE itself; these pin the shapes gate.go HandleStream writes.
describe('SseParser', () => {
  it('reads the gate stream: retry, comments, id+data messages split across chunks', () => {
    const p = new SseParser()
    const a = p.push('retry: 2000\n: connected\n\nid: 77.12\ndata: {"id":"ev-12"')
    expect(a.retry).toBe(2000)
    expect(a.comments).toEqual(['connected'])
    expect(a.messages).toEqual([])
    const b = p.push('}\n\n: heartbeat\r\n\r\n')
    expect(b.messages).toEqual([{ id: '77.12', data: '{"id":"ev-12"}' }])
    expect(b.comments).toEqual(['heartbeat'])
    expect(p.lastEventId).toBe('77.12')
  })

  it('joins multi-line data and surfaces the gate error comment', () => {
    const p = new SseParser()
    const out = p.push('data: a\ndata: b\n\n: error ledger down\n\n')
    expect(out.messages).toEqual([{ data: 'a\nb' }])
    expect(out.comments).toEqual(['error ledger down'])
  })
})

const env = (n: number): BusEnvelope => ({
  specversion: '1.0', id: `ev-${n}`, source: 's', type: 't', time: '', subject: '', tenant: 'rocket', env: 'dev',
  cursor: `1.${n}`, ref: '', actor: '',
})

describe('mergeEvents', () => {
  it('puts the newest first, drops repeats, keeps the cap', () => {
    const list = mergeEvents([], [env(1), env(2)])
    expect(list.map(e => e.id)).toEqual(['ev-2', 'ev-1'])
    expect(mergeEvents(list, [env(2), env(3)]).map(e => e.id)).toEqual(['ev-3', 'ev-2', 'ev-1'])
    const many = mergeEvents([], Array.from({ length: BUS_LIST_CAP + 5 }, (_, i) => env(i)))
    expect(many).toHaveLength(BUS_LIST_CAP)
    expect(many[0].id).toBe(`ev-${BUS_LIST_CAP + 4}`)
  })
})

const base: Omit<BusTraceResponse, 'steps'> = { id: 'ev-9', type: 'git.main.advanced', subject: 's', actor: 'a', cursor: '1.9', ref: '' }

describe('traceTimeline', () => {
  it('times each step from the previous one: ledger → stream → receipts', () => {
    const rows = traceTimeline({
      ...base,
      steps: [
        { step: 'ledger', at: '2026-11-16T10:00:00Z' },
        { step: 'stream', at: '2026-11-16T10:00:00.042Z', since: 'ledger', seconds: 0.042, seq: 5 },
        { step: 'receipt gate:stream:console', at: '2026-11-16T10:00:01.5Z', since: 'stream', seconds: 1.458 },
        { step: 'receipt platform-workers', at: '2026-11-16T10:00:03.5Z', since: 'stream', seconds: 3.458 },
      ],
    })
    expect(rows.map(r => [r.label, r.consumer ?? '', r.delta, r.missing])).toEqual([
      ['Ledger written', '', null, false],
      ['Published to NATS', '', '+0.042s', false],
      ['Receipt', 'gate:stream:console', '+1.46s', false],
      ['Receipt', 'platform-workers', '+2.00s', false],
    ])
    expect(rows[1].seq).toBe(5)
  })

  it('keeps missing steps as rows with the reason, timing receipts from the last present step', () => {
    const rows = traceTimeline({
      ...base,
      nats: 'not on the stream within 10 minutes of the event',
      steps: [
        { step: 'ledger', at: '2026-11-16T10:00:00Z' },
        { step: 'receipt x', at: '2026-11-16T10:00:12Z', since: 'ledger', seconds: 12 },
      ],
    })
    expect(rows[1]).toMatchObject({ label: 'Published to NATS', missing: true, note: 'not on the stream within 10 minutes of the event' })
    expect(rows[2]).toMatchObject({ consumer: 'x', delta: '+12.0s', missing: false })
  })

  it('shows a missing receipt row when nothing received it', () => {
    const rows = traceTimeline({ ...base, nats: 'not configured: only the ledger\'s step is known', steps: [{ step: 'ledger', at: '2026-11-16T10:00:00Z' }] })
    expect(rows.map(r => r.missing)).toEqual([false, true, true])
    expect(rows[2].note).toBe('NATS not configured: receipts are not read')
  })

  it('formats gaps with a sign', () => {
    expect(formatDelta(0.0042)).toBe('+0.004s')
    expect(formatDelta(73.44)).toBe('+73.4s')
    expect(formatDelta(-0.2)).toBe('−0.200s')
  })
})
