import { describe, expect, it } from 'vitest'
import type { SourceVoidEntry } from '@/api/marketDataPlugin'
import {
  judgeGaps,
  shouldCollapse,
  verdictDetail,
  verdictLabel,
  verdictTone,
  VOID_DATA_TYPE,
} from '@/components/market-data/quality/sourceVoidModel'

const READ = { loading: false, error: null, count: 0, note: null, limit: 200 }

function ack(partial: Partial<SourceVoidEntry>): SourceVoidEntry {
  return {
    is_void: true,
    acked_gap_count: null,
    note: null,
    void_reason: null,
    updated_at: null,
    acked_at: null,
    ...partial,
  }
}

describe('judgeGaps — a zero the plugin could not produce is not a zero', () => {
  it('calls a zero with a note unknown, never ok', () => {
    // Measured 2026-09-25: all six types answered exactly this while the
    // universe-view gate was short-circuiting, and all six read green.
    const v = judgeGaps({ ...READ, note: 'v_us_equity_universe view not found' }, null)
    expect(v.kind).toBe('unknown')
    expect(verdictTone(v)).not.toBe('success')
    expect(verdictLabel(v)).toBe('Cannot check')
  })

  it('calls a real zero ok', () => {
    expect(judgeGaps(READ, null)).toEqual({ kind: 'ok' })
  })

  it('treats a transport error as unknown', () => {
    const v = judgeGaps({ ...READ, error: 'HTTP 502', count: null }, null)
    expect(v).toEqual({ kind: 'unknown', reason: 'HTTP 502' })
  })

  it('treats a missing count as unknown rather than zero', () => {
    expect(judgeGaps({ ...READ, count: null }, null).kind).toBe('unknown')
  })

  it('is pending while either read is in flight', () => {
    const v = judgeGaps({ ...READ, loading: true, count: null }, null)
    expect(v).toEqual({ kind: 'pending' })
    expect(verdictLabel(v)).toBe('…')
  })
})

describe('judgeGaps — an acknowledged gap is a boundary, not work', () => {
  it('reads gaps fully inside the acknowledgement as void', () => {
    const v = judgeGaps({ ...READ, count: 1_877, limit: 5_000 }, ack({ acked_gap_count: 1_877 }))
    expect(v).toEqual({ kind: 'void', acked: 1_877, total: 1_877, capped: false })
    expect(verdictTone(v)).toBe('neutral')
    expect(verdictLabel(v)).toBe('Vendor void')
    expect(shouldCollapse(v)).toBe(true)
  })

  it('keeps the remainder actionable when the gap outgrew the acknowledgement', () => {
    const v = judgeGaps({ ...READ, count: 2_000, limit: 5_000 }, ack({ acked_gap_count: 1_877 }))
    expect(v).toEqual({ kind: 'gaps', actionable: 123, acked: 1_877, total: 2_000, capped: false })
    expect(verdictTone(v)).toBe('warning')
    expect(verdictLabel(v)).toBe('123 gaps')
    expect(shouldCollapse(v)).toBe(false)
    expect(verdictDetail(v)).toBe('2,000 missing − 1,877 acknowledged')
  })

  it('forgives nothing when no acknowledgement exists', () => {
    const v = judgeGaps({ ...READ, count: 40 }, null)
    expect(v).toEqual({ kind: 'gaps', actionable: 40, acked: 0, total: 40, capped: false })
    expect(verdictDetail(v)).toBeNull()
  })

  it('ignores a cleared acknowledgement', () => {
    const v = judgeGaps({ ...READ, count: 40 }, ack({ is_void: false, acked_gap_count: 900 }))
    expect(v).toEqual({ kind: 'gaps', actionable: 40, acked: 0, total: 40, capped: false })
  })

  it('still says void when the gap has since closed', () => {
    const v = judgeGaps(READ, ack({ acked_gap_count: 1_971 }))
    expect(v.kind).toBe('void')
    expect(verdictDetail(v)).toBe('acknowledged as never published by the vendor')
  })

  it('survives an acknowledgement with no recorded count', () => {
    const v = judgeGaps({ ...READ, count: 5 }, ack({ acked_gap_count: null }))
    expect(v).toEqual({ kind: 'gaps', actionable: 5, acked: 0, total: 5, capped: false })
  })
})

describe('judgeGaps — a count that reaches the limit is a floor, not a total', () => {
  it('says "at least" instead of reporting the cap as the number', () => {
    // Measured 2026-09-25: four of the six sections rendered "200 missing"
    // where 200 was the limit the console asked for, not a count of anything.
    const v = judgeGaps({ ...READ, count: 200 }, ack({ acked_gap_count: 3_136 }))
    expect(v).toEqual({ kind: 'void', acked: 3_136, total: 200, capped: true })
    expect(verdictDetail(v)).toBe('at least 200 missing · 3,136 acknowledged as vendor-void')
  })

  it('does not claim containment it cannot know', () => {
    // A floor of 200 against an acknowledgement of 50 could be inside it or
    // far outside; the standing judgement decides, not the arithmetic.
    const v = judgeGaps({ ...READ, count: 200 }, ack({ acked_gap_count: 50 }))
    expect(v.kind).toBe('void')
    expect(verdictLabel(v)).toBe('Vendor void')
  })

  it('marks an uncapped count honestly when nothing is acknowledged', () => {
    const v = judgeGaps({ ...READ, count: 200 }, null)
    expect(v).toEqual({ kind: 'gaps', actionable: 200, acked: 0, total: 200, capped: true })
    expect(verdictLabel(v)).toBe('200+ gaps')
    expect(verdictDetail(v)).toBe('at least 200 missing')
  })
})

describe('VOID_DATA_TYPE', () => {
  it('maps every gap report_type onto the void table vocabulary', () => {
    // The two endpoints do not use the same words: the gap report says
    // income_statement, the void table says income_statements.
    expect(VOID_DATA_TYPE).toEqual({
      income_statement: 'income_statements',
      balance_sheet: 'balance_sheets',
      cash_flow_statement: 'cash_flows',
      ratios: 'ratios',
      short_interest: 'short_interest',
      short_volume: 'short_volume',
    })
  })
})
