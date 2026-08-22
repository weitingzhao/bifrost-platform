import { describe, expect, it } from 'vitest'
import {
  qualityCheckCaption,
  qualityCheckFill,
} from '@/components/market-data/qualityScoreModel'

describe('qualityCheckFill', () => {
  it('is full when the check passed', () => {
    expect(qualityCheckFill({ check: 'freshness', ok: true, detail: 'ok' })).toBe(100)
  })

  it('parses missing N/M coverage', () => {
    expect(
      qualityCheckFill({
        check: 'option_snapshot_coverage',
        ok: false,
        detail: 'missing 5/17 optionable symbols',
      }),
    ).toBeCloseTo((12 / 17) * 100)
  })

  it('parses gaps over trading days', () => {
    expect(
      qualityCheckFill({
        check: 'stock_daily_coverage',
        ok: false,
        detail: '33 gaps over 30 trading days',
      }),
    ).toBe(0)
  })
})

describe('qualityCheckCaption', () => {
  it('compacts stock daily plugin detail', () => {
    expect(
      qualityCheckCaption({
        check: 'stock_daily_coverage',
        ok: false,
        detail: 'symbols=14776 (need >4000); gaps=33 over 30 trading days × 17 watchlist',
      }),
    ).toBe('14,776 sym · need >4000 · 33 gaps / 30d · × 17')
  })

  it('compacts option snapshot missing clause', () => {
    expect(
      qualityCheckCaption({
        check: 'option_snapshot_coverage',
        ok: false,
        detail: 'target=2026-08-21; missing=5/17 optionable (skipped 0 equity-only)',
      }),
    ).toBe('miss 5/17 · 08-21')
  })

  it('keeps freshness ok short', () => {
    expect(qualityCheckCaption({ check: 'freshness', ok: true, detail: 'ok' })).toBe('Fresh')
  })
})
