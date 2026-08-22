import { describe, expect, it } from 'vitest'
import {
  classifyVitalText,
  computeVerdict,
  countByKind,
  freshnessToday,
  vitalFill,
} from '@/components/market-data/dataVitalsModel'

describe('dataVitalsModel', () => {
  it('marks last_run today as Today OK', () => {
    const now = new Date('2026-08-22T15:00:00.000Z')
    expect(computeVerdict('2026-08-22T12:00:00.000Z', undefined, now)).toEqual({
      text: 'Today OK',
      kind: 'ok',
    })
  })

  it('marks upcoming next_run within 6h as Scheduled', () => {
    const now = new Date('2026-08-22T15:00:00.000Z')
    const next = '2026-08-22T18:00:00.000Z'
    expect(computeVerdict('2026-08-21T22:00:00.000Z', next, now).kind).toBe('scheduled')
  })

  it('counts freshness today ratio', () => {
    const now = new Date('2026-08-22T15:00:00.000Z')
    const view = freshnessToday(
      [
        { last_run_at: '2026-08-22T01:00:00.000Z' },
        { last_run_at: '2026-08-21T01:00:00.000Z' },
        { last_run_at: null },
      ],
      now,
    )
    expect(view.todayCount).toBe(1)
    expect(view.total).toBe(3)
    expect(view.kind).toBe('scheduled')
    expect(vitalFill(view.kind, view.ratio)).toBeCloseTo(100 / 3)
  })

  it('rolls four vitals into score buckets', () => {
    expect(countByKind(['ok', 'ok', 'scheduled', 'missing'])).toEqual({
      ok: 2,
      scheduled: 1,
      missing: 1,
    })
    expect(classifyVitalText('Today OK')).toBe('ok')
    expect(vitalFill('ok')).toBe(100)
    expect(vitalFill('missing')).toBe(0)
  })
})
