import { describe, expect, it } from 'vitest'
import {
  qualityCheckCaption,
  qualityCheckFill,
  qualityVerdict,
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

describe('the verdict while the checks are still running', () => {
  it('is null, not PASS', () => {
    // Plugin 0.24.0 moved the score behind a background cache because it cost
    // 12 seconds. Its first answer is ok:true with nothing in it, and reading
    // `ok` alone painted a green 4/4 over a run that had not happened.
    expect(qualityVerdict({ ok: true, summary: null, checks: [], computing: true })).toBeNull()
  })

  it('is null before any answer at all', () => {
    expect(qualityVerdict(null)).toBeNull()
    expect(qualityVerdict({ ok: true, summary: null, checks: [] })).toBeNull()
  })

  it('reads a real answer once there is one', () => {
    expect(qualityVerdict({ ok: true, summary: 'PASS', checks: [{}, {}] })).toBe('PASS')
    expect(qualityVerdict({ ok: true, summary: 'FAIL', checks: [{}] })).toBe('FAIL')
  })

  it('still answers from a stale pass while the next one runs', () => {
    // computing with checks in hand means "this is last pass's answer", which
    // is exactly what a background cache is for.
    expect(
      qualityVerdict({ ok: true, summary: 'PASS', checks: [{}, {}], computing: true }),
    ).toBe('PASS')
  })

  it('falls back to ok only when checks are actually present', () => {
    expect(qualityVerdict({ ok: true, summary: null, checks: [{}] })).toBe('PASS')
    expect(qualityVerdict({ ok: false, summary: null, checks: [{}] })).toBe('FAIL')
  })
})
