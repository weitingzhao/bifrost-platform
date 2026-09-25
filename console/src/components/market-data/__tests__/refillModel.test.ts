import { describe, expect, it } from 'vitest'
import {
  FUNDAMENTALS_REFILL,
  MARKET_FUNDAMENTALS_REFILL,
  SESSION_FILL_KIND,
  SESSION_FILL_MAX,
  SNAPSHOT_REFILL,
  sessionFillMessage,
  slotMessage,
  slotNote,
} from '@/components/market-data/quality/refillModel'

describe('the six retired Trade buttons over four plugin calls', () => {
  it('covers all six gap reports between the two fundamentals slots', () => {
    // Trade drew one button per report type; the plugin refills three at a
    // time, so the mapping has to be stated rather than implied by six labels.
    const covered = [...FUNDAMENTALS_REFILL.covers, ...MARKET_FUNDAMENTALS_REFILL.covers]
    expect(covered.sort()).toEqual([
      'balance sheet',
      'cash flow statement',
      'income statement',
      'ratios',
      'short interest',
      'short volume',
    ])
  })

  it('keeps the two slots disjoint', () => {
    // fundamentals-rotate ships with include_ratios / include_short_interest /
    // include_short_volume all false: the market-wide three belong to the other
    // slot, and a button claiming both would double-count them.
    const overlap = FUNDAMENTALS_REFILL.covers.filter(c =>
      (MARKET_FUNDAMENTALS_REFILL.covers as readonly string[]).includes(c),
    )
    expect(overlap).toEqual([])
    expect(FUNDAMENTALS_REFILL.slot).toBe('fundamentals-rotate')
    expect(MARKET_FUNDAMENTALS_REFILL.slot).toBe('fundamentals-market')
  })

  it('names a slot the plugin has for the snapshot refill', () => {
    expect(SNAPSHOT_REFILL.slot).toBe('stock-snapshot')
  })

  it('fills sessions with the one kind that has no slot', () => {
    // The grouped-history and price-gap buttons were two labels over one kind;
    // no slot takes a date range, which is why this one fans out console-side.
    expect(SESSION_FILL_KIND).toBe('stock_daily_grouped')
  })
})

describe('slotMessage', () => {
  it('says what one press covers and that a repeat is deduped', () => {
    const msg = slotMessage(MARKET_FUNDAMENTALS_REFILL, null)
    expect(msg).toContain('fundamentals-market')
    expect(msg).toContain('ratios, short interest, short volume')
    expect(msg).toContain('deduped')
  })

  it('carries the date when one is pinned', () => {
    expect(slotMessage(SNAPSHOT_REFILL, '2026-09-24')).toContain('for 2026-09-24')
  })
})

describe('sessionFillMessage', () => {
  it('states the count and the span before any load is put on the workers', () => {
    const msg = sessionFillMessage(['2025-06-02', '2025-06-03', '2025-06-20'])
    expect(msg).toContain('3 sessions')
    expect(msg).toContain('2025-06-02 … 2025-06-20')
  })

  it('reads singular for one session', () => {
    const msg = sessionFillMessage(['2026-09-24'])
    expect(msg).toContain('1 session')
    expect(msg).not.toContain('sessions')
    expect(msg).toContain('(2026-09-24)')
  })

  it('says plainly that there is nothing to do', () => {
    expect(sessionFillMessage([])).toContain('Nothing to fill')
  })

  it('has a ceiling the caller can show rather than truncate silently', () => {
    expect(SESSION_FILL_MAX).toBeGreaterThan(0)
  })
})

describe('slotNote — a skip is not a queue of zero', () => {
  it('reports a skipped slot with its reason', () => {
    expect(slotNote({ skipped: true, reason: 'holiday', detail: 'not a trading day' })).toBe(
      'skipped — not a trading day',
    )
  })

  it('falls back to the reason when no detail is given', () => {
    expect(slotNote({ skipped: true, reason: 'retired' })).toBe('skipped — retired')
  })

  it('reports the fan-out size for a slot that ran', () => {
    expect(slotNote({ symbols: 5321 })).toBe('5,321 symbols')
  })

  it('says nothing when there is nothing to add', () => {
    expect(slotNote({ symbols: 0 })).toBeUndefined()
  })
})
