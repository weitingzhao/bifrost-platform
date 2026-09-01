import { describe, expect, it } from 'vitest'
import {
  SIGNAL_HEALTH_FRESH_SLA_HOURS,
  SIGNAL_HEALTH_WEEKEND_SLA_HOURS,
  ageToFillPct,
  freshnessSlaHours,
  freshnessStatusTone,
  stackFreshnessStatuses,
} from '@/lib/research/signalHealthAgeMeters'
import {
  formatReadinessRollupLine,
  massiveReadinessHref,
} from '@/lib/research/massiveNav'

describe('signalHealthAgeMeters', () => {
  it('widens SLA to 72h Sat/Sun/Mon before 22:00 UTC', () => {
    expect(freshnessSlaHours(new Date('2026-08-31T17:25:00Z'))).toBe(
      SIGNAL_HEALTH_WEEKEND_SLA_HOURS,
    )
    expect(freshnessSlaHours(new Date('2026-08-31T22:30:00Z'))).toBe(
      SIGNAL_HEALTH_FRESH_SLA_HOURS,
    )
    expect(freshnessSlaHours(new Date('2026-09-01T17:00:00Z'))).toBe(
      SIGNAL_HEALTH_FRESH_SLA_HOURS,
    )
  })

  it('maps age to fill pct against 36h SLA', () => {
    expect(ageToFillPct(0)).toBe(0)
    expect(ageToFillPct(18)).toBe(50)
    expect(ageToFillPct(36)).toBe(100)
    expect(ageToFillPct(72)).toBe(100)
    expect(ageToFillPct(null)).toBe(0)
  })

  it('tones freshness status for meters', () => {
    expect(freshnessStatusTone('fresh')).toBe('success')
    expect(freshnessStatusTone('stale')).toBe('danger')
    expect(freshnessStatusTone('missing')).toBe('danger')
    expect(freshnessStatusTone('empty')).toBe('warning')
  })

  it('stacks freshness rows for StackedBar', () => {
    const s = stackFreshnessStatuses([
      { status: 'fresh' },
      { status: 'fresh' },
      { status: 'stale' },
      { status: 'unknown' },
    ])
    expect(s.fresh).toBe(2)
    expect(s.stale).toBe(1)
    expect(s.other).toBe(1)
    expect(s.readyPct).toBe(50)
    expect(s.blockedPct).toBe(25)
    expect(s.thinPct).toBe(25)
  })
})

describe('massiveNav', () => {
  it('builds Massive Readiness deep link', () => {
    expect(massiveReadinessHref()).toContain('tab=coverage')
    expect(massiveReadinessHref()).toContain('panel=readiness')
    expect(massiveReadinessHref()).toContain('#market-data-manage')
  })

  it('formats readiness_rollup one-liner without SQL jargon', () => {
    const line = formatReadinessRollupLine({
      universe: 100,
      snapshot_rows: 90,
      snapshot_covered: 88,
      vendor_gap_count: 3,
      as_of: '2026-08-29T12:00:00Z',
    })
    expect(line).toMatch(/snap 88\/100/)
    expect(line).toMatch(/vendor_gap 3/)
    expect(line).not.toMatch(/ops_dagster/)
  })
})
