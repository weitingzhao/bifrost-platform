import { describe, expect, it } from 'vitest'
import {
  slotSchedulerKind,
  slotSchedulerLabel,
} from '@/lib/market-data/slotScheduler'

describe('slotScheduler', () => {
  it('marks Massive husbandry slots as Dagster', () => {
    expect(slotSchedulerKind('stock-snapshot')).toBe('dagster')
    expect(slotSchedulerKind('fundamentals-rotate')).toBe('dagster')
    expect(slotSchedulerKind('option-refresh')).toBe('dagster')
    expect(slotSchedulerKind('trim')).toBe('dagster')
    expect(slotSchedulerLabel('dagster')).toBe('Dagster')
  })

  it('marks analytics slots as Research', () => {
    expect(slotSchedulerKind('max-pain')).toBe('research')
    expect(slotSchedulerKind('readiness-refresh')).toBe('research')
  })
})
