import { afterEach, describe, expect, it, vi } from 'vitest'
import { CLOCK_SHIFT_MS } from '@/test/clockShift'

// performance is not Date: it still tells the real time.
const realNow = () => performance.timeOrigin + performance.now()
const NINE_YEARS = 9 * 365 * 86_400_000
const ONE_DAY = 86_400_000

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
  expect(Date.UTC(2026, 9, 13)).toBe(Date.parse('2026-10-13T00:00:00Z'))
  expect(Date.parse('1970-01-02T00:00:00Z')).toBe(ONE_DAY)
})

it('the shifted Date behaves like Date for the code under test', () => {
  // Date() without new returns a string. A class would throw here.
  const text = (Date as unknown as () => string)()
  expect(typeof text).toBe('string')
  expect(Date.parse(text) - realNow()).toBeGreaterThan(NINE_YEARS)
  // A date that did not come from this constructor is still a Date, and the other way round.
  const cloned = structuredClone(new Date(0))
  expect(cloned instanceof Date).toBe(true)
  expect(new Date() instanceof Date).toBe(true)
  expect(Object.prototype.toString.call(new Date())).toBe('[object Date]')
  expect(structuredClone(new Date()).getTime() - realNow()).toBeGreaterThan(NINE_YEARS)
})

it('a pinned clock is what the test set, and the shift is back afterwards', () => {
  vi.useFakeTimers()
  vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
  expect(Date.now()).toBe(Date.parse('2026-10-11T12:00:00Z'))
  expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
  vi.useRealTimers()
  expect(Date.now() - realNow()).toBeGreaterThan(NINE_YEARS)
})

it('fake timers without a pinned date start from the shifted clock', () => {
  // Fake timers also replace performance, so the real time is read before they start.
  const before = realNow()
  vi.useFakeTimers()
  expect(Date.now() - before).toBeGreaterThan(NINE_YEARS)
  vi.advanceTimersByTime(1000)
  expect(new Date().getTime() - before).toBeGreaterThan(NINE_YEARS)
})

describe('a test that pins only the date, without fake timers', () => {
  it('is back on the real clock when it lets go: vitest restores the Date it saw at start-up', () => {
    vi.setSystemTime(new Date('2026-10-11T12:00:00Z'))
    expect(new Date().toISOString()).toBe('2026-10-11T12:00:00.000Z')
    vi.useRealTimers()
    // If this stops holding, vitest changed and the beforeEach in clockShift.ts is no longer needed.
    expect(Math.abs(Date.now() - realNow())).toBeLessThan(ONE_DAY)
  })

  it('and the next test is ahead again', () => {
    expect(Date.now() - realNow()).toBeGreaterThan(NINE_YEARS)
    expect(new Date().getTime() - realNow()).toBeGreaterThan(NINE_YEARS)
  })
})
