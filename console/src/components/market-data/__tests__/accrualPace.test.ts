import { describe, expect, it } from 'vitest'
import { accrualPace, accrualStalled } from '@/components/market-data/dimensionsModel'
import { accrualSummary } from '@/components/market-data/coverageMatrixModel'
import { buildCoverageMatrixPack } from '@/components/market-data/coverageMatrixPack'
import type { DatasetDimensions } from '@/api/marketDataDimensions'

const ds = (accrual: unknown, dataset = 'raw_market.option_snapshot') =>
  ({
    dataset,
    tier: 'universe',
    grain: 'snapshot',
    slots: ['eod-pipeline'],
    breadth_window: 'session',
    error: null,
    breadth: { held: 570, held_total: 570, outside_scope: 0, of: 575, pct: 99.1, entitlement_pct: null },
    depth: {
      target: { kind: 'forward_only', value: null, why: '' },
      measured: false,
      accrual,
    },
    freshness: { newest: '2026-09-10', deadline_hours: 30, measured: true, days_behind: 0 },
    continuity: { measured: true, days_present: 60, days_absent: 0, days_thin: 0 },
  }) as unknown as DatasetDimensions

const climbing = {
  sessions_held: 45, accrues_to: 90, pct: 50, since: '2026-07-01',
  rate: 1, stalled: false, sessions_remaining: 45, rate_window_days: 14,
}
const stopped = {
  sessions_held: 54, accrues_to: 90, pct: 60, since: '2026-07-01',
  rate: 0, stalled: true, sessions_remaining: null, rate_window_days: 14,
}

describe('a percentage cannot be acted on; a pace can', () => {
  it('says how fast and how much longer while it is climbing', () => {
    expect(accrualPace(ds(climbing))).toBe('1 session per trading day · ~45 sessions to go')
    expect(accrualStalled(ds(climbing))).toBe(false)
  })

  it('names the stall instead of leaving it at 60%', () => {
    // The two states a fraction alone renders identically.
    expect(accrualPace(ds(stopped))).toBe('stalled — nothing gained in 14d')
    expect(accrualStalled(ds(stopped))).toBe(true)
  })

  it('says nothing rather than guessing when the rate is not measurable', () => {
    const noCalendar = { ...climbing, rate: null, stalled: null, sessions_remaining: null }
    expect(accrualPace(ds(noCalendar))).toBeNull()
    expect(accrualStalled(ds(noCalendar))).toBeNull()
  })
})

describe('the summary above the grid', () => {
  it('counts stalls and names them, without a fifth colour', () => {
    const s = accrualSummary([ds(climbing, 'raw_market.option_snapshot'), ds(stopped, 'raw_market.option_open_interest')])
    expect(s).toMatchObject({ accruing: 2, stalled: 1 })
    expect(s.stalledNames).toEqual(['option_open_interest'])
  })

  it('ignores datasets that are not accruing toward anything', () => {
    const notAccruing = ds(undefined, 'raw_market.ticker')
    expect(accrualSummary([notAccruing])).toMatchObject({ accruing: 0, stalled: 0 })
  })
})

describe('the agent brief', () => {
  it('tells a stalled accrual apart from a gap it could backfill', () => {
    const text = buildCoverageMatrixPack(
      { datasets: [ds(stopped)], memory: { recorded: true, changes: [], previous_at: '2026-09-09T00:00:00Z' } },
      '2026-09-11T00:00:00Z',
    )
    expect(text).toContain('## Accruing boundaries — 1, 1 stalled')
    expect(text).toContain('cannot be backfilled')
    expect(text).toContain('54/90 sessions · stalled')
  })

  it('says so plainly when they are all still gaining', () => {
    const text = buildCoverageMatrixPack(
      { datasets: [ds(climbing)], memory: { recorded: true, changes: [], previous_at: '2026-09-09T00:00:00Z' } },
      '2026-09-11T00:00:00Z',
    )
    expect(text).toContain('0 stalled')
    expect(text).toContain('All of them gained ground')
  })
})
