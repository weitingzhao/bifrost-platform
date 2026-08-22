import { describe, expect, it } from 'vitest'
import {
  buildAnalyticsDemand,
  coverPct,
  CS_FUND_TARGET,
  meterPct,
} from '@/components/market-data/analyticsDemandModel'

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
    })
    expect(view.rows.find(r => r.id === 'max-pain')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'pcr')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'sepa-technical')?.level).toBe('ready')
    expect(view.rows.find(r => r.id === 'sepa-fundamental')?.level).toBe('thin')
    expect(view.ready).toBeGreaterThanOrEqual(3)
    expect(view.thin).toBeGreaterThanOrEqual(1)
    expect(view.rows.find(r => r.id === 'max-pain')?.inputs[0]?.target).toBe(120)
    expect(view.rows.find(r => r.id === 'sepa-fundamental')?.inputs[0]?.target).toBe(CS_FUND_TARGET)
    expect(view.equityFeed.find(f => f.label === 'Income')?.fillPct).toBe(meterPct(40, CS_FUND_TARGET))
  })

  it('clamps meter and cover percentages', () => {
    expect(meterPct(null, 10)).toBe(0)
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
