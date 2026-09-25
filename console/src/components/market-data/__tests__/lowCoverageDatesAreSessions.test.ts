import { describe, expect, it } from 'vitest'
import { filterActionableLowCoverageDates } from '@/components/market-data/quality/readinessChecks'

/**
 * A small symbol count means one of two opposite things, and thinness alone
 * cannot tell them apart. The plugin now says which (`session`), measured after
 * fourteen real June 2025 sessions holding one symbol each were being dropped
 * here as "thin days ignored" — the biggest gap in the window, filtered out by
 * the check that exists to find gaps.
 */
describe('filterActionableLowCoverageDates', () => {
  it('keeps a session the store almost entirely missed', () => {
    const out = filterActionableLowCoverageDates([
      { date: '2025-06-02', symbol_count: 1, session: true },
    ])
    expect(out.map(d => d.date)).toEqual(['2025-06-02'])
  })

  it('drops a day the calendar says was closed, however many stray rows it has', () => {
    const out = filterActionableLowCoverageDates([
      { date: '2025-06-19', symbol_count: 3, session: false },
      { date: '2025-07-04', symbol_count: 9_000, session: false },
    ])
    expect(out).toEqual([])
  })

  it('falls back to thinness when the plugin did not say', () => {
    // An older plugin, or one whose holiday calendar could not be read: a
    // guess is still better than calling every holiday a producer failure.
    const out = filterActionableLowCoverageDates([
      { date: '2026-01-02', symbol_count: 9_000 },
      { date: '2026-01-05', symbol_count: 12 },
      { date: '2026-01-06', symbol_count: 4, session: null },
    ])
    expect(out.map(d => d.date)).toEqual(['2026-01-02'])
  })

  it('takes the flag over the threshold in both directions', () => {
    const out = filterActionableLowCoverageDates([
      { date: 'thin-but-a-session', symbol_count: 1, session: true },
      { date: 'full-but-not-a-session', symbol_count: 11_000, session: false },
    ])
    expect(out.map(d => d.date)).toEqual(['thin-but-a-session'])
  })

  it('returns nothing for a reading that never arrived', () => {
    expect(filterActionableLowCoverageDates(null)).toEqual([])
    expect(filterActionableLowCoverageDates(undefined)).toEqual([])
  })
})
