import { afterEach, expect, it, vi } from 'vitest'
import { CLOCK_SHIFT_MS } from '@/test/clockShift'

// performance is not Date: it still tells the real time.
const realNow = () => performance.timeOrigin + performance.now()
const NINE_YEARS = 9 * 365 * 86_400_000

afterEach(() => { vi.useRealTimers() })

it('the suite runs with the clock about ten years ahead', () => {
  expect(CLOCK_SHIFT_MS).toBeGreaterThan(NINE_YEARS)
  expect(Date.now() - realNow()).toBeGreaterThan(NINE_YEARS)
  expect(new Date().getTime() - realNow()).toBeGreaterThan(NINE_YEARS)
})

it('anything that expires within the next nine years has already expired for a test on the real clock', () => {
  // The failure this guards against (2026-10-12): a contract fixture's expires_at passed, and 23 tests that read
  // the real clock went red with no change in the code. Here such a fixture is expired from the first run.
  const expiresNextYear = realNow() + 365 * 86_400_000
  expect(Date.now()).toBeGreaterThan(expiresNextYear)
  expect(new Date().getTime()).toBeGreaterThan(realNow() + NINE_YEARS)
})

it('a date built from a value is not moved', () => {
  expect(new Date('2026-10-12T06:00:00Z').toISOString()).toBe('2026-10-12T06:00:00.000Z')
  expect(new Date(0).getTime()).toBe(0)
  expect(new Date(2026, 9, 13, 12, 34, 56).getSeconds()).toBe(56)
})

it('a pinned clock is what the test set, and the shift is back afterwards', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  expect(Date.now()).toBe(Date.parse('2026-10-11T12:00:00Z'))
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  vi.useRealTimers()
  expect(Date.now() - realNow()).toBeGreaterThan(NINE_YEARS)
})
