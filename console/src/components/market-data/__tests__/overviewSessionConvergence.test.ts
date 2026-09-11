import { describe, expect, it } from 'vitest'
import { computeVerdict, freshnessToday } from '@/components/market-data/dataVitalsModel'
import { buildAnalyticsDemand } from '@/components/market-data/analyticsDemandModel'

/**
 * 2026-09-11 01:52 UTC is 21:52 on the 10th in New York — after that session's
 * EOD batch completed. The overview strips read `Missing` and `6/20 today`
 * anyway, because they were asking the UTC calendar date.
 */
const AFTER_EOD_UTC_ROLLED_OVER = new Date('2026-09-11T01:52:00Z')

describe('the session, not the calendar day', () => {
  it('calls a feed that ran for the session current, whatever the date has done', () => {
    const v = computeVerdict('2026-09-10T22:00:12Z', undefined, AFTER_EOD_UTC_ROLLED_OVER, '2026-09-10')
    expect(v).toEqual({ text: 'Session OK', kind: 'ok' })
  })

  it('is exactly the reading that used to say Missing', () => {
    // Same inputs, no session: the old behaviour, kept as the fallback.
    const v = computeVerdict('2026-09-10T22:00:12Z', undefined, AFTER_EOD_UTC_ROLLED_OVER)
    expect(v.kind).toBe('missing')
  })

  it('still says Missing when the feed genuinely skipped the session', () => {
    const v = computeVerdict('2026-09-08T22:00:12Z', undefined, AFTER_EOD_UTC_ROLLED_OVER, '2026-09-10')
    expect(v.kind).toBe('missing')
  })

  it('counts freshness against the session too', () => {
    const items = [
      { last_run_at: '2026-09-10T22:00:00Z' },
      { last_run_at: '2026-09-10T23:15:00Z' },
      { last_run_at: '2026-09-08T22:00:00Z' },
    ]
    const withSession = freshnessToday(items, AFTER_EOD_UTC_ROLLED_OVER, '2026-09-10')
    expect(withSession.todayCount).toBe(2)
    expect(withSession.text).toBe('2/3 for session')
    // Without it, the two that ran for the session read as stale.
    expect(freshnessToday(items, AFTER_EOD_UTC_ROLLED_OVER).todayCount).toBe(0)
  })

  it('does not treat a null session as today', () => {
    // The plugin answers null when it cannot resolve one. Falling back to the
    // calendar is the old behaviour; inventing a session would be worse.
    expect(computeVerdict('2026-09-10T22:00:12Z', undefined, AFTER_EOD_UTC_ROLLED_OVER, null).kind).toBe(
      'missing',
    )
  })
})

describe('unmeasured is not zero', () => {
  it('reads a product whose input has not reported as unknown, not blocked', () => {
    // SEPA Technical's one required input comes from the four-axis payload,
    // which takes about 220 seconds on a cold cache. Eight minutes after a
    // deploy it read `blocked — Stock daily` over a table holding 13.7M rows.
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, computing: false, option: { snapshot_symbols: 570, oi_symbols: 570 } },
      incomeStatementSymbols: 4467,
      // dimensions absent — the source SEPA Technical reads
    })
    expect(view.rows.find(r => r.id === 'sepa-technical')?.level).toBe('unknown')
  })

  it('still says blocked when the count is a measured zero', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, computing: false, option: { snapshot_symbols: 0, oi_symbols: 0 } },
      incomeStatementSymbols: 0,
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('blocked')
  })
})
