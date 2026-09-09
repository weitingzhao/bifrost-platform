import { describe, expect, it } from 'vitest'
import {
  buildAnalyticsDemand,
  coverPct,
  meterPct,
} from '@/components/market-data/analyticsDemandModel'

// stock_daily holds every symbol it has ever seen; the contract endpoint is
// what pairs a numerator with its own denominator.
const DIMENSIONS = {
  denominators: undefined as never,
  datasets: [
    {
      dataset: 'raw_market.stock_daily',
      breadth: { held: 5182, held_total: 20695, outside_scope: 7336, of: 5317, pct: 97.5 },
    },
  ],
} as never

const DENOMINATORS = {
  'whole-market': 5317,
  universe: { total: 575, by_tier: { resident: 27, core: 527, edge: 21 }, months: {} },
  'benchmark-only': 91,
  global: 1,
}

describe('buildAnalyticsDemand', () => {
  it('marks option analytics ready when snapshot + OI exist and freshness is ok', () => {
    const view = buildAnalyticsDemand({
      freshness: [
        { dimension: 'option_snapshot', rows_written: 100, age_hours: 2, verdict: 'ok' },
        { dimension: 'option_open_interest', rows_written: 80, age_hours: 2, verdict: 'ok' },
        { dimension: 'stock_daily', rows_written: 5000, age_hours: 12, verdict: 'ok' },
      ],
      inventory: {
        ok: true,
        option: { snapshot_symbols: 120, oi_symbols: 110 },
        stock_daily: { symbols: 5200, total_rows: 800000 },
        analytics: { max_pain: { symbols: 0 } },
      },
      incomeStatementSymbols: 40,
      denominators: DENOMINATORS,
      dimensions: DIMENSIONS,
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'pcr')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'sepa-technical')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'sepa-fundamental')?.level).toBe('thin')
    expect(view.ready).toBeGreaterThanOrEqual(3)
    expect(view.thin).toBeGreaterThanOrEqual(1)
    // Denominators come from the contract table, not from the measurement.
    expect(view.rows.find(r => r.id === 'max-pain')?.inputs[0]?.target).toBe(575)
    expect(view.rows.find(r => r.id === 'sepa-fundamental')?.inputs[0]?.target).toBe(5317)
    expect(view.equityFeed.find(f => f.label === 'Income')?.fillPct).toBe(meterPct(40, 5317))
    // Stock daily used to fill the bar whenever the count was above zero, then
    // briefly read "20,695 / 5,317" — symbols ever, over tickers active today.
    const stockMeter = view.equityFeed.find(f => f.label === 'Stock daily')
    expect(stockMeter?.count).toBe(5182)
    expect(stockMeter?.target).toBe(5317)
    expect(stockMeter?.fillPct).toBe(meterPct(5182, 5317))
  })

  it('draws no meter when no contract declares a denominator', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: {
        ok: true,
        option: { snapshot_symbols: 120, oi_symbols: 110 },
        stock_daily: { symbols: 5200, total_rows: 800000 },
      },
      incomeStatementSymbols: 40,
    })
    expect(view.optionUniverse).toBeNull()
    for (const meter of [...view.optionFeed, ...view.equityFeed]) {
      expect(meter.fillPct).toBeNull()
    }
  })

  it('never divides a feed by a target it helped set', () => {
    // The old target was max(watchlist, snapshot, oi, 1), so collecting one
    // symbol of 575 still filled the bar.
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, option: { snapshot_symbols: 1, oi_symbols: 1 } },
      incomeStatementSymbols: 0,
      denominators: DENOMINATORS,
    })
    expect(view.optionFeed.find(f => f.label === 'Snapshot')?.fillPct).toBeCloseTo(
      (1 / 575) * 100,
      6,
    )
  })

  it('treats a still-counting inventory as unknown, not blocked', () => {
    // The inventory takes about 150s behind its cache. Reading absent counts as
    // zero marked all six products blocked while it was simply not done yet.
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, computing: true } as never,
      incomeStatementSymbols: null,
      denominators: DENOMINATORS,
    })
    expect(view.pending).toBe(true)
    expect(view.blocked).toBe(0)
    expect(view.unknown).toBe(view.rows.length)
    expect(view.rows[0]?.detail).toContain('still being counted')
  })

  it('reports blocked once the inventory has actually been counted', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, computing: false, option: { snapshot_symbols: 0, oi_symbols: 0 } },
      incomeStatementSymbols: 0,
      denominators: DENOMINATORS,
    })
    expect(view.pending).toBe(false)
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('blocked')
  })

  it('clamps meter and cover percentages', () => {
    expect(meterPct(null, 10)).toBeNull()
    expect(meterPct(12, null)).toBeNull()
    expect(meterPct(12, 10)).toBe(100)
    expect(coverPct(22, 22)).toBe(100)
    expect(coverPct(11, 22)).toBe(50)
    expect(coverPct(0, 22)).toBe(0)
    expect(coverPct(5, null)).toBeNull()
  })

  it('blocks when required Massive inputs are empty', () => {
    const view = buildAnalyticsDemand({
      freshness: [],
      inventory: { ok: true, option: { snapshot_symbols: 0, oi_symbols: 0 } },
      incomeStatementSymbols: 0,
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('blocked')
    expect(view.rows.find(r => r.id === 'sepa-fundamental')?.level).toBe('blocked')
  })
})
