import { describe, expect, it } from 'vitest'
import { etaClockLabel, etaMinutesAtNet, formatEtaMinutes, netPerMinute } from '../queueEta'

describe('formatEtaMinutes', () => {
  it('turns "1695.4m" into something a person reads', () => {
    expect(formatEtaMinutes(1695.4)).toBe('1d 4h')
    expect(formatEtaMinutes(1695.4 - 24 * 60)).toBe('4h 15m')
    expect(formatEtaMinutes(45)).toBe('45m')
    expect(formatEtaMinutes(0.4)).toBe('24s')
  })
  it('is null without a rate', () => {
    expect(formatEtaMinutes(null)).toBeNull()
    expect(formatEtaMinutes(undefined)).toBeNull()
    expect(formatEtaMinutes(-1)).toBeNull()
    expect(formatEtaMinutes(Number.NaN)).toBeNull()
  })
})

describe('etaClockLabel', () => {
  const now = new Date(2026, 8, 8, 12, 0, 0).getTime() // local noon, Tue 8 Sep 2026
  it('gives the clock time when it lands today', () => {
    expect(etaClockLabel(90, now)).toBe('13:30')
  })
  it('names the day when it lands on another day', () => {
    expect(etaClockLabel(1695.4, now)).toMatch(/^Wed \d{2}:\d{2}$/)
  })
  it('falls back to a date a week out', () => {
    expect(etaClockLabel(9 * 24 * 60, now)).toMatch(/Sep\s+17|17\s+Sep/)
  })
  it('is null without a rate', () => {
    expect(etaClockLabel(null, now)).toBeNull()
  })
})

describe('net drain', () => {
  it('is done minus fed, per minute, positive when shrinking', () => {
    // Ready fell by 5,431 over fifteen minutes.
    expect(netPerMinute(-5431, 15 * 60_000)).toBeCloseTo(362.07, 1)
    // Ready grew: the queue is being fed faster than it drains.
    expect(netPerMinute(1200, 15 * 60_000)).toBeCloseTo(-80, 1)
  })
  it('has no opinion without two distinct checks', () => {
    expect(netPerMinute(-5431, 0)).toBeNull()
    expect(netPerMinute(null, 15 * 60_000)).toBeNull()
  })
  it('gives an ETA at the net rate only while the queue shrinks', () => {
    expect(etaMinutesAtNet(727_876, 362.07)).toBeCloseTo(2010.3, 0)
    expect(etaMinutesAtNet(727_876, -80)).toBeNull()
    expect(etaMinutesAtNet(0, 362)).toBeNull()
  })
})
