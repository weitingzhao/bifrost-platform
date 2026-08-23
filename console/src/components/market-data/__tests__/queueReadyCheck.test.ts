import { describe, expect, it } from 'vitest'
import {
  formatReadyCheckCaption,
  formatSignedDelta,
  readyCheckDelta,
  shiftReadyCheck,
} from '@/components/market-data/queueReadyCheck'

describe('shiftReadyCheck', () => {
  it('keeps the first sample as current without a previous', () => {
    const next = shiftReadyCheck({ previous: null, current: null }, { count: 1083, atMs: 1000 })
    expect(next.previous).toBeNull()
    expect(next.current).toEqual({ count: 1083, atMs: 1000 })
  })

  it('promotes current to previous on a new check timestamp', () => {
    const first = shiftReadyCheck({ previous: null, current: null }, { count: 1083, atMs: 1000 })
    const second = shiftReadyCheck(first, { count: 1054, atMs: 16000 })
    expect(second.previous).toEqual({ count: 1083, atMs: 1000 })
    expect(second.current).toEqual({ count: 1054, atMs: 16000 })
    expect(readyCheckDelta(second)).toBe(-29)
  })

  it('ignores identical timestamp + count', () => {
    const first = shiftReadyCheck({ previous: null, current: null }, { count: 10, atMs: 1 })
    expect(shiftReadyCheck(first, { count: 10, atMs: 1 })).toBe(first)
  })
})

describe('formatReadyCheckCaption', () => {
  it('shows previous count, age, and delta', () => {
    const caption = formatReadyCheckCaption({
      hist: {
        previous: { count: 1083, atMs: 0 },
        current: { count: 1054, atMs: 15_000 },
      },
      nowMs: 15_000,
      oldestLabel: 'oldest 43h',
    })
    expect(caption).toBe('was 1,083 · 15s ago · Δ −29 · oldest 43h')
  })

  it('formats signed deltas', () => {
    expect(formatSignedDelta(-29)).toBe('Δ −29')
    expect(formatSignedDelta(12)).toBe('Δ +12')
    expect(formatSignedDelta(0)).toBe('Δ 0')
  })
})
