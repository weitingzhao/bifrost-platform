import { describe, expect, it } from 'vitest'
import {
  describeCronSchedule,
  formatCountdownTo,
  formatDurationParts,
  formatNextRunAt,
  iterCronFiresUtc,
} from '@/lib/patrol/cronSchedule'

describe('describeCronSchedule', () => {
  it('describes the three stock patrol crons', () => {
    expect(describeCronSchedule('0 3 * * *')).toBe('Daily at 03:00 UTC')
    expect(describeCronSchedule('0 4 * * *')).toBe('Daily at 04:00 UTC')
    expect(describeCronSchedule('0 6 * * 1')).toBe('Weekly on Monday at 06:00 UTC')
  })

  it('covers common step / hourly shapes', () => {
    expect(describeCronSchedule('*/15 * * * *')).toBe('Every 15 minutes')
    expect(describeCronSchedule('0 */2 * * *')).toBe('Every 2 hours')
    expect(describeCronSchedule('30 * * * *')).toBe('Hourly at :30')
    expect(describeCronSchedule('0 0 1 * *')).toBe('Monthly on day 1 at 00:00 UTC')
  })

  it('falls back to raw expr when shape is unknown', () => {
    expect(describeCronSchedule('0 8,20 * * *')).toBe('0 8,20 * * *')
    expect(describeCronSchedule('not-a-cron')).toBe('not-a-cron')
  })
})

describe('formatCountdownTo', () => {
  const now = Date.parse('2026-08-10T12:00:00.000Z')

  it('formats future and overdue', () => {
    expect(formatCountdownTo('2026-08-10T14:30:00.000Z', now)).toBe('in 2h 30m')
    expect(formatCountdownTo('2026-08-10T12:00:20.000Z', now)).toBe('in 20s')
    expect(formatCountdownTo('2026-08-10T11:59:30.000Z', now)).toBe('due now')
    expect(formatCountdownTo('2026-08-10T10:00:00.000Z', now)).toBe('overdue 2h')
  })

  it('handles missing', () => {
    expect(formatCountdownTo(undefined, now)).toBe('—')
    expect(formatCountdownTo('', now)).toBe('—')
  })
})

describe('iterCronFiresUtc', () => {
  it('matches Plugin stock-eod and option-refresh', () => {
    const after = new Date('2026-08-17T15:00:00Z')
    const eod = iterCronFiresUtc('30 21 * * *', after, new Date('2026-08-19T00:00:00Z'))
    expect(eod.map(d => d.toISOString())).toEqual([
      '2026-08-17T21:30:00.000Z',
      '2026-08-18T21:30:00.000Z',
    ])
    const refresh = iterCronFiresUtc(
      '20 */6 * * *',
      new Date('2026-08-17T00:00:00Z'),
      new Date('2026-08-18T00:00:00Z'),
    )
    expect(refresh.map(d => d.toISOString())).toEqual([
      '2026-08-17T00:20:00.000Z',
      '2026-08-17T06:20:00.000Z',
      '2026-08-17T12:20:00.000Z',
      '2026-08-17T18:20:00.000Z',
    ])
  })

  it('matches weekly Saturday oi-gap-heal', () => {
    const fires = iterCronFiresUtc(
      '0 4 * * 6',
      new Date('2026-08-10T00:00:00Z'),
      new Date('2026-08-17T12:00:00Z'),
    )
    expect(fires.map(d => d.toISOString())).toEqual(['2026-08-15T04:00:00.000Z'])
  })
})

describe('formatDurationParts / formatNextRunAt', () => {
  it('omits zero units', () => {
    expect(formatDurationParts(90_000)).toBe('1m 30s')
    expect(formatDurationParts(3_600_000)).toBe('1h')
  })

  it('formats locale datetime or dash', () => {
    expect(formatNextRunAt(undefined)).toBe('—')
    expect(formatNextRunAt('2026-08-10T03:00:00.000Z')).not.toBe('—')
  })
})
