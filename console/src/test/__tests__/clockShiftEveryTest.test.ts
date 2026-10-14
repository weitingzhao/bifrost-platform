import { describe, expect, it, vi } from 'vitest'
import { CLOCK_SHIFT_MS } from '@/test/clockShift'

const MINUTE = 60_000

// No afterEach here on purpose: nothing but the setup file's beforeEach can put the shift back between these tests.
// Both tests do the same thing, so the order does not matter: whichever runs second starts from the native Date.
describe('the clock is shifted at the start of every test, whatever the previous one left behind', () => {
  for (const n of [1, 2]) {
    it(`test ${n} starts shifted, then leaves the native Date in place`, () => {
      expect(Math.abs(Date.now() - vi.getRealSystemTime() - CLOCK_SHIFT_MS)).toBeLessThan(MINUTE)
      globalThis.Date = Object.getPrototypeOf(Date) as DateConstructor
      expect(Math.abs(Date.now() - vi.getRealSystemTime())).toBeLessThan(MINUTE)
    })
  }
})
