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

  it('marks upcoming next_run within 12h as Scheduled on a weekday', () => {
    const now = new Date('2026-08-27T15:00:00.000Z') // Thursday
    const next = '2026-08-27T22:00:00.000Z'
    expect(computeVerdict('2026-08-26T22:00:00.000Z', next, now)).toEqual({
      text: 'Scheduled ~7h',
      kind: 'scheduled',
    })
  })

  it('marks Mon afternoon next_run tonight as Scheduled when last_run too old for session', () => {
    const now = new Date('2026-08-31T15:00:00.000Z') // Monday
    const next = '2026-08-31T22:00:00.000Z'
    expect(computeVerdict('2026-08-20T05:00:00.000Z', next, now).kind).toBe('scheduled')
  })

  it('marks Fri EOD on Monday morning as Session OK', () => {
    const now = new Date('2026-08-31T15:00:00.000Z') // Monday
    expect(computeVerdict('2026-08-30T05:31:00.000Z', '2026-08-31T22:00:00.000Z', now)).toEqual({
      text: 'Session OK',
      kind: 'ok',
    })
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
