import { describe, expect, it } from 'vitest'
import {
  areaPath,
  buildQueueSeries,
  compactCount,
  isolatedPoints,
  linePath,
  niceTicks,
  type QueueSeriesPoint,
} from '@/lib/market-data/queueHistoryModel'

const P = (ts: string, extra: Record<string, unknown> = {}) => ({
  sample_ts: ts,
  kind: null,
  pending: 100,
  running: 4,
  created_delta: 0,
  done_delta: 15,
  failed_delta: 0,
  oldest_pending_age_sec: 3600,
  p95_sec: 0.9,
  ...extra,
})

describe('buildQueueSeries', () => {
  it('turns a per-interval count into a rate the reader can compare', () => {
    // 15 jobs in a 5-minute sample is 3 a minute, whatever the interval becomes.
    const s = buildQueueSeries([P('2026-09-09T15:00:00Z')], 300)
    expect(s.points[0].donePerMin).toBe(3)
    expect(s.points[0].oldestPendingHours).toBe(1)
    expect(s.latest?.pending).toBe(100)
  })

  it('keeps reconstructed rows but leaves their depth unknown', () => {
    const s = buildQueueSeries(
      [P('2026-09-09T15:00:00Z', { pending: null, running: null })],
      300,
    )
    expect(s.points[0].pending).toBeNull()
    expect(s.maxPending).toBe(0)
    expect(s.withDepth).toBe(0)
  })

  it('scales the consumption axis to consumption, not to an enqueue burst', () => {
    // One backfill burst queued 725k jobs in a five-minute bucket — 145k a
    // minute. Putting that on the consumed axis flattened every real reading.
    const s = buildQueueSeries(
      [
        P('2026-09-09T15:00:00Z', { created_delta: 724745, done_delta: 3702 }),
        P('2026-09-09T15:05:00Z', { created_delta: 0, done_delta: 9144 }),
      ],
      300,
    )
    expect(s.maxDonePerMin).toBeCloseTo(9144 / 5, 6)
    expect(s.maxCreatedPerMin).toBeCloseTo(724745 / 5, 6)
  })

  it('orders by time whatever order the rows arrived in', () => {
    const s = buildQueueSeries(
      [P('2026-09-09T15:05:00Z'), P('2026-09-09T15:00:00Z')],
      300,
    )
    expect(s.points.map(p => p.t)).toEqual([...s.points.map(p => p.t)].sort((a, b) => a - b))
  })
})

describe('paths', () => {
  const box = { w: 100, h: 50, max: 10, t0: 0, t1: 100 }
  const pt = (t: number, v: number | null): QueueSeriesPoint => ({
    t,
    pending: v,
    running: null,
    donePerMin: 0,
    createdPerMin: 0,
    failedPerMin: 0,
    oldestPendingHours: null,
    p95Sec: null,
  })

  it('breaks the line where the sampler was not running', () => {
    // A gap is a gap. Drawing through it invents a rate nobody recorded.
    const d = linePath([pt(0, 5), pt(10, 5), pt(90, 5)], p => p.pending, box, { gapMs: 20 })
    expect(d.split('M')).toHaveLength(3) // leading '' + two segments
  })

  it('joins points that are within the sampling cadence', () => {
    const d = linePath([pt(0, 5), pt(10, 5), pt(20, 5)], p => p.pending, box, { gapMs: 20 })
    expect(d.split('M')).toHaveLength(2)
  })

  it('closes each area segment to the baseline, not across the gap', () => {
    const d = areaPath([pt(0, 5), pt(10, 5), pt(90, 5)], p => p.pending, box, { gapMs: 20 })
    expect(d.match(/Z/g)).toHaveLength(2)
  })

  it('marks points no line can reach', () => {
    // One sample is a reading, not an empty chart. A freshly started series and
    // a broken one must not look the same.
    expect(isolatedPoints([pt(0, 5)], p => p.pending, 20)).toHaveLength(1)
    expect(isolatedPoints([pt(0, 5), pt(10, 5)], p => p.pending, 20)).toHaveLength(0)
    // First point after a gap wide enough to break the line.
    expect(isolatedPoints([pt(0, 5), pt(90, 5), pt(95, 5)], p => p.pending, 20)).toEqual([
      pt(0, 5),
    ])
  })

  it('draws nothing when there is nothing to draw', () => {
    expect(linePath([], p => p.pending, box)).toBe('')
    expect(areaPath([], p => p.pending, box)).toBe('')
  })
})

describe('axis and labels', () => {
  it('puts ticks on round numbers', () => {
    expect(niceTicks(2396642)).toEqual([0, 1000000, 2000000])
    expect(niceTicks(0)).toEqual([0])
  })

  it('shortens counts without pretending to precision it lacks', () => {
    expect(compactCount(2396642)).toBe('2.4M')
    expect(compactCount(2800)).toBe('2.8k')
    expect(compactCount(37)).toBe('37')
    expect(compactCount(null)).toBe('—')
  })
})
