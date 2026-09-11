import { describe, expect, it } from 'vitest'
import { buildAnalyticsDemand } from '@/components/market-data/analyticsDemandModel'
import {
  countByKind,
  freshnessToday,
  judgeVital,
} from '@/components/market-data/dataVitalsModel'

/**
 * 2026-09-11, a hard reload of the Massive Overview on data that held the
 * session: at 4 s the Stock summary read `missing 4`, at 14 s `missing 3`, at
 * 24 s `4/4 Session OK`; Analytics demand read `thin 5 · 0/6` at 14 s and
 * `ready 4 · thin 1` once loaded. Nothing about the data changed — the page was
 * stating verdicts about values that had not arrived yet.
 */
const SESSION = '2026-09-10'
const NOON_NY = new Date('2026-09-11T16:44:00Z')
const RAN_FOR_SESSION = '2026-09-10T22:00:12Z'
const RAN_DAY_BEFORE = '2026-09-09T22:00:12Z'

describe('Stock summary: a value that has not arrived is not a finding', () => {
  it('does not count unknown or pending as missing', () => {
    expect(countByKind(['unknown', 'unknown', 'pending', 'pending'])).toEqual({
      ok: 0,
      scheduled: 0,
      missing: 0,
      pending: 2,
      unknown: 2,
    })
  })

  it('reads an empty freshness list as unknown, not missing', () => {
    expect(freshnessToday([], NOON_NY, SESSION).kind).toBe('unknown')
  })

  it('waits while the card has nothing to judge on', () => {
    const v = judgeVital(undefined, undefined, { inputs: 'pending', session: SESSION, schedule: 'pending' }, NOON_NY)
    expect(v.kind).toBe('pending')
  })

  it('waits for the session rather than judging against the UTC calendar', () => {
    // This exact input used to read Missing: ran for the 10th, calendar says the 11th.
    const v = judgeVital(RAN_FOR_SESSION, undefined, { inputs: 'arrived', session: undefined, schedule: 'arrived' }, NOON_NY)
    expect(v.kind).toBe('pending')
  })

  it('says unknown, not Missing, when the session cannot be had', () => {
    const v = judgeVital(RAN_FOR_SESSION, undefined, { inputs: 'arrived', session: null, schedule: 'arrived' }, NOON_NY)
    expect(v).toEqual({ text: '—', kind: 'unknown' })
  })

  it('says unknown when every read failed', () => {
    const v = judgeVital(undefined, undefined, { inputs: 'failed', session: SESSION, schedule: 'failed' }, NOON_NY)
    expect(v.kind).toBe('unknown')
  })

  it('judges the session once both are in', () => {
    const v = judgeVital(RAN_FOR_SESSION, undefined, { inputs: 'arrived', session: SESSION, schedule: 'arrived' }, NOON_NY)
    expect(v).toEqual({ text: 'Session OK', kind: 'ok' })
  })

  it('holds a Missing until the read that could make it Scheduled is in', () => {
    const early = judgeVital(RAN_DAY_BEFORE, undefined, { inputs: 'arrived', session: SESSION, schedule: 'pending' }, NOON_NY)
    expect(early.kind).toBe('pending')
    const settled = judgeVital(RAN_DAY_BEFORE, undefined, { inputs: 'arrived', session: SESSION, schedule: 'arrived' }, NOON_NY)
    expect(settled.kind).toBe('missing')
  })
})

describe('Analytics demand: rows wait for their reads', () => {
  const counted = { ok: true, option: { snapshot_symbols: 570, oi_symbols: 570 } }
  const fresh = [
    { dimension: 'option_snapshot', verdict: 'ok', last_run_at: RAN_FOR_SESSION },
    { dimension: 'option_open_interest', verdict: 'ok', last_run_at: RAN_FOR_SESSION },
    { dimension: 'stock_daily', verdict: 'ok', last_run_at: RAN_FOR_SESSION },
    { dimension: 'financials', verdict: 'ok', last_run_at: RAN_FOR_SESSION },
  ]

  it('shows a row whose stock read is still in flight as waiting, not thin', () => {
    const view = buildAnalyticsDemand({
      freshness: fresh,
      inventory: counted,
      incomeStatementSymbols: 4467,
      awaiting: { dimensions: true },
    })
    const atmIv = view.rows.find(r => r.id === 'atm-iv')
    expect(atmIv?.waiting).toBe(true)
    expect(atmIv?.level).toBe('unknown')
    expect(view.thin).toBe(0)
    // Rows that do not need that read are judged meanwhile.
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('ready')
  })

  it('does not call a row ready or stale before the freshness read is in', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: counted,
      incomeStatementSymbols: 4467,
      awaiting: { freshness: true },
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.waiting).toBe(true)
    expect(view.ready).toBe(0)
    expect(view.thin).toBe(0)
  })

  it('still reports what the counts already prove while freshness loads', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, option: { snapshot_symbols: 0, oi_symbols: 0 } },
      incomeStatementSymbols: 4467,
      awaiting: { freshness: true },
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('blocked')
  })

  it('reads an unmeasured input beside a measured zero as thin, not blocked', () => {
    // ATM IV: snapshot counted at zero, stock daily not reported (and not awaited).
    const view = buildAnalyticsDemand({
      freshness: fresh,
      inventory: { ok: true, option: { snapshot_symbols: 0, oi_symbols: 570 } },
      incomeStatementSymbols: 4467,
    })
    expect(view.rows.find(r => r.id === 'atm-iv')?.level).toBe('thin')
  })

  it('is exactly the loaded reading once nothing is awaited', () => {
    const view = buildAnalyticsDemand({
      freshness: fresh,
      inventory: counted,
      incomeStatementSymbols: 4467,
      awaiting: {},
    })
    expect(view.waiting).toBe(0)
    expect(view.rows.find(r => r.id === 'pcr')?.level).toBe('ready')
  })
})
