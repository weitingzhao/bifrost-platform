import { describe, expect, it } from 'vitest'
import {
  continuityDetail,
  continuityLabel,
  continuityVerdict,
} from '@/components/market-data/dimensionsModel'
import type { DatasetDimensions } from '@/api/marketDataDimensions'

const base = (continuity: unknown, error: string | null = null) =>
  ({
    dataset: 'raw_market.stock_daily',
    tier: 'whole-market',
    slots: [],
    breadth_window: 'ever',
    error,
    breadth: { held: 0, held_total: 0, outside_scope: 0, of: null, pct: null, entitlement_pct: null },
    depth: { target: { kind: 'rolling_days', value: 1825, why: '' }, measured: false },
    freshness: { newest: null, deadline_hours: 2, measured: false },
    continuity,
  }) as unknown as DatasetDimensions

describe('continuity verdict', () => {
  it('is clean when every session is there and full', () => {
    const d = base({ measured: true, days_present: 81, days_absent: 0, days_thin: 0 })
    expect(continuityVerdict(d)).toBe('ok')
    expect(continuityLabel(d)).toBe('81 sessions clean')
  })

  it('counts a session that never landed alongside one that landed empty', () => {
    // For a reader both are a day of missing data; the label keeps them apart
    // because for whoever fixes it they are different faults.
    const d = base({ measured: true, days_present: 24, days_absent: 1, days_thin: 1 })
    expect(continuityLabel(d)).toBe('1 missing · 1 thin of 25')
    expect(continuityVerdict(d)).toBe('partial')
  })

  it('calls a tenth of the window thin', () => {
    const d = base({ measured: true, days_present: 56, days_absent: 0, days_thin: 8 })
    expect(continuityVerdict(d)).toBe('thin')
  })

  it('treats a dataset with no session cadence as a boundary, not a gap', () => {
    // A catalogue has no cadence and a quarterly filing is not a daily series.
    const d = base({ measured: false, why: 'catalogue has no session cadence' })
    expect(continuityVerdict(d)).toBe('boundary')
    expect(continuityLabel(d)).toBe('catalogue has no session cadence')
  })

  it('says nothing when the dataset itself was unreadable', () => {
    expect(continuityVerdict(base({ measured: true, days_present: 5 }, 'boom'))).toBe('unknown')
  })

  it('names the worst hole and the settlement cadence in the detail', () => {
    const d = base({
      measured: true,
      cadence: 'settlement',
      days_present: 7,
      days_absent: 0,
      days_thin: 0,
      worst: [{ date: '2026-07-18', rows: 1, neighbours: 45534 }],
      absent_sample: ['2026-08-05'],
    })
    const detail = continuityDetail(d) ?? ''
    expect(detail).toContain('2026-07-18')
    expect(detail).toContain('45,534')
    expect(detail).toContain('missing 2026-08-05')
    expect(detail).toContain('per settlement')
  })
})

describe("rows outside the trading calendar", () => {
  it("names them — they are the opposite of a hole and nothing else reports them", () => {
    // option_open_interest carried thousands of rows on 2026-08-22 and 08-23,
    // a Saturday and a Sunday, while option-bars and eod-pipeline both sit in
    // the skip-on-holiday set. The fourth axis used to call those days thin.
    const d = base({
      measured: true,
      cadence: "session",
      days_present: 56,
      days_absent: 0,
      days_thin: 0,
      days_off_calendar: 4,
      off_calendar_sample: ["2026-08-22", "2026-08-23"],
    });
    expect(continuityVerdict(d)).toBe("ok");
    expect(continuityDetail(d)).toContain("4 day(s) of rows outside the calendar");
    expect(continuityDetail(d)).toContain("2026-08-22");
  });
});

describe("the session being written", () => {
  it("is present, not clean, and not thin", () => {
    // Measured 2026-09-10: between 22:00 and 22:10 UTC the EOD chain was
    // writing stock_daily and this axis read the fraction as thin — ok →
    // partial → ok inside seven minutes. Correct as a reading, useless as a
    // verdict; it would repeat every night.
    const d = base({
      measured: true,
      cadence: "session",
      days_present: 82,
      days_absent: 0,
      days_thin: 0,
      days_not_due: 1,
      not_due_sample: ["2026-09-10"],
    });
    expect(continuityVerdict(d)).toBe("ok");
    // Not "82 sessions clean" — one of them has not closed its books.
    expect(continuityLabel(d)).toBe("81 clean · 1 still writing");
    expect(continuityDetail(d)).toContain("still being written");
    expect(continuityDetail(d)).toContain("2026-09-10");
  });

  it("says nothing extra once every session is due", () => {
    const d = base({ measured: true, days_present: 82, days_absent: 0, days_thin: 0 });
    expect(continuityLabel(d)).toBe("82 sessions clean");
  });
});
